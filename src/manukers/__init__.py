"""manukers - AWS CLI Dashboard

Usage:
  manukers lb-rules [lb-name ...] [--region REGION]
  manukers lb-health [lb-name] [--region REGION]
  manukers overview [--region REGION]
  manukers eks-status [cluster-name] [--namespace NS] [--region REGION]
"""

import sys
import argparse
import concurrent.futures
from collections import defaultdict

try:
    import boto3
    from botocore.exceptions import ClientError, NoCredentialsError, NoRegionError
except ImportError:
    print("ERROR: boto3 is required. Run: pip install boto3")
    sys.exit(1)

# ─── ANSI / icons ────────────────────────────────────────────────────────────

GREEN  = "\033[92m"
YELLOW = "\033[93m"
RED    = "\033[91m"
DIM    = "\033[2m"
RESET  = "\033[0m"
BOLD   = "\033[1m"


def _c(color: str, text: str) -> str:
    return f"{color}{text}{RESET}"


def weight_bar(pct: float) -> str:
    filled = round(pct / 10)
    return "█" * filled + "░" * (10 - filled)


def state_icon(state: str) -> str:
    s = (state or "").lower()
    if s in ("active", "running", "healthy", "ready"):
        return _c(GREEN, "🟢")
    if s in ("provisioning", "degraded", "mixed"):
        return _c(YELLOW, "🟡")
    return _c(RED, "🔴")


def health_icon(healthy: int, total: int) -> str:
    if total == 0:
        return _c(DIM, "—")
    if healthy == total:
        return _c(GREEN, f"🟢 {healthy}/{total}")
    if healthy == 0:
        return _c(RED, f"🔴 {healthy}/{total}")
    return _c(YELLOW, f"🟡 {healthy}/{total}")


def tg_name_from_arn(arn: str) -> str:
    """Extract TG name from ARN."""
    # arn:aws:elasticloadbalancing:...:targetgroup/<name>/<id>
    parts = arn.split(":")
    if len(parts) >= 6:
        tg_part = parts[-1]  # targetgroup/<name>/<id>
        segments = tg_part.split("/")
        if len(segments) >= 2:
            return segments[1]
    return arn


def format_conditions(conditions: list) -> str:
    parts = []
    for c in conditions:
        field = c.get("Field", "")
        if field == "path-pattern":
            vals = c.get("PathPatternConfig", {}).get("Values", [])
            parts.append("path: " + ", ".join(vals))
        elif field == "host-header":
            vals = c.get("HostHeaderConfig", {}).get("Values", [])
            parts.append("host: " + ", ".join(vals))
        elif field == "source-ip":
            vals = c.get("SourceIpConfig", {}).get("Values", [])
            parts.append("src-ip: " + ", ".join(vals))
        elif field == "http-header":
            hc = c.get("HttpHeaderConfig", {})
            name = hc.get("HttpHeaderName", "")
            vals = hc.get("Values", [])
            parts.append(f"header {name}={','.join(vals)}")
        elif field == "http-request-method":
            vals = c.get("HttpRequestMethodConfig", {}).get("Values", [])
            parts.append("method: " + ", ".join(vals))
        elif field == "query-string":
            qs = c.get("QueryStringConfig", {}).get("Values", [])
            kv = [f"{q.get('Key','')}={q.get('Value','')}" for q in qs]
            parts.append("query: " + ", ".join(kv))
    return " AND ".join(parts) if parts else "—"


# ─── AWS helpers ─────────────────────────────────────────────────────────────

def make_client(service: str, region: str):
    return boto3.client(service, region_name=region)


def get_albs(elb, names=None) -> list:
    """Return list of ALB dicts. If names provided, filter by name."""
    kwargs = {}
    if names:
        kwargs["Names"] = names
    lbs = []
    paginator = elb.get_paginator("describe_load_balancers")
    for page in paginator.paginate(**kwargs):
        lbs.extend(page["LoadBalancers"])
    return lbs


def get_listeners(elb, lb_arn: str) -> list:
    listeners = []
    paginator = elb.get_paginator("describe_listeners")
    for page in paginator.paginate(LoadBalancerArn=lb_arn):
        listeners.extend(page["Listeners"])
    return listeners


def get_rules(elb, listener_arn: str) -> list:
    resp = elb.describe_rules(ListenerArn=listener_arn)
    rules = resp.get("Rules", [])
    # sort: numeric priorities first, then DEFAULT last
    def sort_key(r):
        p = r.get("Priority", "default")
        return (1, 0) if p == "default" else (0, int(p))
    return sorted(rules, key=sort_key)


def get_tg_health(elb, tg_arn: str) -> tuple:
    """Returns (healthy_count, total_count, descriptions_list)."""
    try:
        resp = elb.describe_target_health(TargetGroupArn=tg_arn)
        descs = resp.get("TargetHealthDescriptions", [])
        total = len(descs)
        healthy = sum(
            1 for d in descs
            if d.get("TargetHealth", {}).get("State") == "healthy"
        )
        return healthy, total, descs
    except ClientError:
        return 0, 0, []


def get_tg_details(elb, tg_arns: list) -> dict:
    """Return dict of tg_arn -> tg_detail."""
    if not tg_arns:
        return {}
    result = {}
    # API accepts up to 400 ARNs at once
    for i in range(0, len(tg_arns), 100):
        chunk = tg_arns[i:i+100]
        try:
            resp = elb.describe_target_groups(TargetGroupArns=chunk)
            for tg in resp.get("TargetGroups", []):
                result[tg["TargetGroupArn"]] = tg
        except ClientError:
            pass
    return result


# ─── Rendering helpers ────────────────────────────────────────────────────────

def _ssl_policy(listener: dict) -> str:
    pol = listener.get("SslPolicy")
    return f"  [SSL: {pol}]" if pol else ""


def _render_rule_rows(rules: list, tg_health: dict, tg_details: dict) -> list:
    """Return list of row-dicts for the rules table."""
    rows = []
    for rule in rules:
        priority = rule.get("Priority", "default")
        prio_label = "DEFAULT" if priority == "default" else str(priority)
        conditions = rule.get("Conditions", [])
        cond_str = format_conditions(conditions)
        actions = rule.get("Actions", [])

        for action in actions:
            atype = action.get("Type", "")

            if atype == "forward":
                fwd = action.get("ForwardConfig", {})
                tg_list = fwd.get("TargetGroups", [])
                if not tg_list:
                    # simple forward
                    tg_arn = action.get("TargetGroupArn", "")
                    tg_list = [{"TargetGroupArn": tg_arn, "Weight": 1}]

                total_weight = sum(t.get("Weight", 1) for t in tg_list)
                multi = len(tg_list) > 1

                for idx, tg_entry in enumerate(tg_list):
                    tg_arn = tg_entry.get("TargetGroupArn", "")
                    weight = tg_entry.get("Weight", 1)
                    pct = (weight / total_weight * 100) if total_weight > 0 else 0

                    tg_n = tg_name_from_arn(tg_arn)
                    detail = tg_details.get(tg_arn, {})
                    proto = detail.get("Protocol", "")
                    port  = detail.get("Port", "")
                    ttype = detail.get("TargetType", "")
                    proto_port = f"{proto}:{port}" if proto and port else "—"

                    h, t, _ = tg_health.get(tg_arn, (0, 0, []))
                    health_str = health_icon(h, t)
                    weight_str = f"{pct:.0f}% {weight_bar(pct)}"

                    rows.append({
                        "priority": "↳" if (multi and idx > 0) else prio_label,
                        "condition": cond_str if idx == 0 else "",
                        "tg": tg_n,
                        "proto_port": proto_port,
                        "ttype": ttype or "—",
                        "weight": weight_str,
                        "health": health_str,
                    })

            elif atype == "redirect":
                rc = action.get("RedirectConfig", {})
                host   = rc.get("Host", "#{host}")
                path   = rc.get("Path", "/#{path}")
                query  = rc.get("Query", "#{query}")
                proto  = rc.get("Protocol", "#{protocol}")
                port   = rc.get("Port", "#{port}")
                code   = rc.get("StatusCode", "HTTP_301").replace("HTTP_", "")
                dest   = f"→ {proto}://{host}:{port}{path}"
                if query and query != "#{query}":
                    dest += f"?{query}"
                dest += f" ({code})"
                rows.append({
                    "priority": prio_label,
                    "condition": cond_str,
                    "tg": dest,
                    "proto_port": "—",
                    "ttype": "REDIRECT",
                    "weight": "—",
                    "health": "—",
                })

            elif atype == "fixed-response":
                fr = action.get("FixedResponseConfig", {})
                code = fr.get("StatusCode", "")
                msg  = fr.get("MessageBody", "")
                label = f"FIXED:{code}"
                if msg:
                    label += f" ({msg[:30]})"
                rows.append({
                    "priority": prio_label,
                    "condition": cond_str,
                    "tg": label,
                    "proto_port": "—",
                    "ttype": "FIXED",
                    "weight": "—",
                    "health": "—",
                })

            else:
                rows.append({
                    "priority": prio_label,
                    "condition": cond_str,
                    "tg": atype,
                    "proto_port": "—",
                    "ttype": "—",
                    "weight": "—",
                    "health": "—",
                })
    return rows


def _print_rules_table(rows: list):
    if not rows:
        print("  (no rules)")
        return

    # Strip ANSI for column width calc
    import re
    ansi_re = re.compile(r"\033\[[0-9;]*m")

    def vlen(s):
        return len(ansi_re.sub("", s))

    headers = ["Priority", "Condition", "Target Group", "Proto:Port", "Type", "Weight", "Health"]
    keys    = ["priority", "condition", "tg", "proto_port", "ttype", "weight", "health"]

    col_w = [len(h) for h in headers]
    for row in rows:
        for i, k in enumerate(keys):
            col_w[i] = max(col_w[i], vlen(row.get(k, "")))

    def fmt_row(cells):
        padded = []
        for i, cell in enumerate(cells):
            pad = col_w[i] - vlen(cell)
            padded.append(cell + " " * pad)
        return "| " + " | ".join(padded) + " |"

    sep = "|-" + "-|-".join("-" * w for w in col_w) + "-|"
    print(fmt_row(headers))
    print(sep)
    for row in rows:
        print(fmt_row([row.get(k, "") for k in keys]))


# ─── Command: lb-rules ───────────────────────────────────────────────────────

def cmd_lb_rules(args):
    elb = make_client("elbv2", args.region)
    names = args.names or []

    try:
        lbs = get_albs(elb, names if names else None)
    except ClientError as e:
        print(f"ERROR: {e}")
        return
    except NoCredentialsError:
        print("ERROR: No AWS credentials found.")
        return

    if not lbs:
        print("No load balancers found.")
        return

    summary_rows = []

    for lb in lbs:
        lb_name  = lb["LoadBalancerName"]
        lb_arn   = lb["LoadBalancerArn"]
        lb_dns   = lb["DNSName"]
        lb_state = lb.get("State", {}).get("Code", "unknown")
        lb_scheme = lb.get("Scheme", "")
        lb_type  = lb.get("Type", "")

        print()
        print("━" * 65)
        print(f"ALB: {BOLD}{lb_name}{RESET}    {state_icon(lb_state)} {lb_state}")
        print(f"DNS: {DIM}{lb_dns}{RESET}")
        print(f"Scheme: {lb_scheme}    Type: {lb_type}")
        print("━" * 65)

        listeners = get_listeners(elb, lb_arn)
        if not listeners:
            print("  (no listeners)")
            continue

        # Collect all TG ARNs across all rules for this LB
        all_rules_by_listener = {}
        for lst in listeners:
            lst_arn = lst["ListenerArn"]
            all_rules_by_listener[lst_arn] = get_rules(elb, lst_arn)

        all_tg_arns = set()
        for rules in all_rules_by_listener.values():
            for rule in rules:
                for action in rule.get("Actions", []):
                    if action.get("Type") == "forward":
                        fwd = action.get("ForwardConfig", {})
                        for tg_e in fwd.get("TargetGroups", []):
                            all_tg_arns.add(tg_e["TargetGroupArn"])
                        direct = action.get("TargetGroupArn")
                        if direct:
                            all_tg_arns.add(direct)

        # Fetch TG details + health in parallel
        tg_arns_list = list(all_tg_arns)
        tg_details = get_tg_details(elb, tg_arns_list)

        tg_health = {}
        with concurrent.futures.ThreadPoolExecutor(max_workers=20) as ex:
            futures = {ex.submit(get_tg_health, elb, arn): arn for arn in tg_arns_list}
            for fut in concurrent.futures.as_completed(futures):
                arn = futures[fut]
                tg_health[arn] = fut.result()

        total_rules = 0
        unique_tgs = set()
        unhealthy_tgs = []

        for lst in listeners:
            lst_arn   = lst["ListenerArn"]
            proto     = lst.get("Protocol", "")
            port      = lst.get("Port", "")
            ssl_info  = _ssl_policy(lst)
            print()
            print(f"▶ Listener: {proto} :{port}{ssl_info}")
            print()

            rules = all_rules_by_listener[lst_arn]
            total_rules += len(rules)

            rows = _render_rule_rows(rules, tg_health, tg_details)

            # Track TGs
            for rule in rules:
                for action in rule.get("Actions", []):
                    if action.get("Type") == "forward":
                        fwd = action.get("ForwardConfig", {})
                        for tg_e in fwd.get("TargetGroups", []):
                            arn = tg_e["TargetGroupArn"]
                            unique_tgs.add(arn)
                            h, t, _ = tg_health.get(arn, (0, 0, []))
                            if t > 0 and h < t:
                                unhealthy_tgs.append((tg_name_from_arn(arn), h, t))
                        direct = action.get("TargetGroupArn")
                        if direct:
                            unique_tgs.add(direct)

            _print_rules_table(rows)

        print()
        print(f"Rules: {total_rules} | Unique Target Groups: {len(unique_tgs)} | Unhealthy TGs: {len(unhealthy_tgs)}")
        for tg_n, h, t in unhealthy_tgs:
            print(_c(YELLOW, f"⚠️  {tg_n}: {h}/{t} healthy — check target health"))

        summary_rows.append({
            "name": lb_name,
            "listeners": str(len(listeners)),
            "total_rules": str(total_rules),
            "unique_tgs": str(len(unique_tgs)),
            "unhealthy_tgs": str(len(unhealthy_tgs)),
        })

    # Cross-LB summary if multiple
    if len(lbs) > 1:
        print()
        print("━" * 65)
        print("CROSS-LB SUMMARY")
        print("━" * 65)
        hdrs = ["Load Balancer", "Listeners", "Total Rules", "Unique TGs", "Unhealthy TGs"]
        keys = ["name", "listeners", "total_rules", "unique_tgs", "unhealthy_tgs"]
        col_w = [len(h) for h in hdrs]
        for row in summary_rows:
            for i, k in enumerate(keys):
                col_w[i] = max(col_w[i], len(row.get(k, "")))

        def fmt(cells):
            return "| " + " | ".join(c.ljust(w) for c, w in zip(cells, col_w)) + " |"

        print(fmt(hdrs))
        print("|-" + "-|-".join("-" * w for w in col_w) + "-|")
        for row in summary_rows:
            print(fmt([row.get(k, "") for k in keys]))


# ─── Command: lb-health ──────────────────────────────────────────────────────

def cmd_lb_health(args):
    elb = make_client("elbv2", args.region)
    name = args.name

    try:
        lbs = get_albs(elb, [name] if name else None)
    except ClientError as e:
        print(f"ERROR: {e}")
        return
    except NoCredentialsError:
        print("ERROR: No AWS credentials found.")
        return

    if not lbs:
        print("No load balancers found.")
        return

    for lb in lbs:
        lb_name   = lb["LoadBalancerName"]
        lb_arn    = lb["LoadBalancerArn"]
        lb_dns    = lb["DNSName"]
        lb_state  = lb.get("State", {}).get("Code", "unknown")
        lb_scheme = lb.get("Scheme", "")
        lb_vpc    = lb.get("VpcId", "")

        print()
        print("┌" + "─" * 63 + "┐")
        print(f"│  ALB: {lb_name:<30} State: {lb_state:<12}│")
        print(f"│  DNS: {lb_dns:<56}│")
        print(f"│  Scheme: {lb_scheme:<22}   VPC: {lb_vpc:<20}│")
        print("└" + "─" * 63 + "┘")

        listeners = get_listeners(elb, lb_arn)
        lst_summary = ", ".join(
            f"{l.get('Protocol')}:{l.get('Port')}" for l in listeners
        )

        # Collect all TG ARNs
        all_tg_arns = set()
        for lst in listeners:
            for action in lst.get("DefaultActions", []):
                if action.get("Type") == "forward":
                    arn = action.get("TargetGroupArn")
                    if arn:
                        all_tg_arns.add(arn)

        # Also scan rules
        for lst in listeners:
            rules = get_rules(elb, lst["ListenerArn"])
            for rule in rules:
                for action in rule.get("Actions", []):
                    if action.get("Type") == "forward":
                        fwd = action.get("ForwardConfig", {})
                        for tg_e in fwd.get("TargetGroups", []):
                            all_tg_arns.add(tg_e["TargetGroupArn"])
                        direct = action.get("TargetGroupArn")
                        if direct:
                            all_tg_arns.add(direct)

        tg_arns_list = list(all_tg_arns)
        tg_details = get_tg_details(elb, tg_arns_list)

        tg_health_map = {}
        with concurrent.futures.ThreadPoolExecutor(max_workers=20) as ex:
            futures = {ex.submit(get_tg_health, elb, arn): arn for arn in tg_arns_list}
            for fut in concurrent.futures.as_completed(futures):
                arn = futures[fut]
                tg_health_map[arn] = fut.result()

        print()
        scheme_label = "Internet" if lb_scheme == "internet-facing" else "Internal"
        print(scheme_label)
        print("│")
        print("▼")
        print(f"[ALB: {lb_name}]  ({lst_summary})")
        print("│")

        unhealthy_targets = []
        tg_list = list(tg_arns_list)

        for idx, tg_arn in enumerate(tg_list):
            is_last = idx == len(tg_list) - 1
            branch = "└──►" if is_last else "├──►"

            tg_n = tg_name_from_arn(tg_arn)
            h, t, descs = tg_health_map.get(tg_arn, (0, 0, []))
            hi = health_icon(h, t)
            print(f"{branch} [TG: {tg_n}]  {hi}")

            cont = "     " if is_last else "│    "
            for di, desc in enumerate(descs):
                target = desc.get("Target", {})
                ip   = target.get("Id", "?")
                port = target.get("Port", "")
                state = desc.get("TargetHealth", {}).get("State", "")
                reason = desc.get("TargetHealth", {}).get("Description", "")
                icon = _c(GREEN, "🟢") if state == "healthy" else _c(RED, "🔴")
                addr = f"{ip}:{port}"
                suffix = f"  ({reason})" if (state != "healthy" and reason) else ""
                branch2 = "└──" if di == len(descs) - 1 else "├──"
                print(f"{cont}{branch2} {addr}  {icon}{suffix}")
                if state != "healthy":
                    unhealthy_targets.append((tg_n, addr, reason))

        if unhealthy_targets:
            print()
            print(_c(RED, "⚠️  UNHEALTHY TARGETS DETECTED"))
            for tg_n, addr, reason in unhealthy_targets:
                print(f"TG: {tg_n} → {addr} — unhealthy ({reason})")


# ─── Command: overview ───────────────────────────────────────────────────────

def cmd_overview(args):
    region = args.region

    elb = make_client("elbv2", region)
    ec2 = make_client("ec2",   region)
    s3  = make_client("s3",    region)
    eks = make_client("eks",   region)

    print()
    print("╔" + "═" * 62 + "╗")
    print(f"║{'AWS INFRASTRUCTURE DASHBOARD — ' + region:^62}║")
    print("╚" + "═" * 62 + "╝")

    # Parallel data collection
    def get_ec2_instances():
        resp = ec2.describe_instances(
            Filters=[{"Name": "instance-state-name", "Values": ["running", "stopped", "pending"]}]
        )
        instances = []
        for res in resp.get("Reservations", []):
            for inst in res.get("Instances", []):
                name = next(
                    (t["Value"] for t in inst.get("Tags", []) if t["Key"] == "Name"),
                    "(no name)"
                )
                instances.append({
                    "name": name,
                    "id": inst["InstanceId"],
                    "state": inst["State"]["Name"],
                    "type": inst.get("InstanceType", ""),
                    "ip": inst.get("PrivateIpAddress", ""),
                })
        return instances

    def get_s3_buckets():
        resp = s3.list_buckets()
        return [b["Name"] for b in resp.get("Buckets", [])]

    def get_all_lbs():
        lbs = []
        paginator = elb.get_paginator("describe_load_balancers")
        for page in paginator.paginate():
            lbs.extend(page["LoadBalancers"])
        return lbs

    def get_all_tgs():
        tgs = []
        paginator = elb.get_paginator("describe_target_groups")
        for page in paginator.paginate():
            tgs.extend(page["TargetGroups"])
        return tgs

    def get_eks_clusters():
        names = eks.list_clusters().get("clusters", [])
        clusters = []
        for name in names:
            try:
                desc = eks.describe_cluster(name=name)
                c = desc["cluster"]
                clusters.append({
                    "name": c["name"],
                    "status": c["status"],
                    "version": c.get("version", ""),
                })
            except ClientError:
                clusters.append({"name": name, "status": "unknown", "version": ""})
        return clusters

    results = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=5) as ex:
        fut_ec2  = ex.submit(get_ec2_instances)
        fut_s3   = ex.submit(get_s3_buckets)
        fut_lbs  = ex.submit(get_all_lbs)
        fut_tgs  = ex.submit(get_all_tgs)
        fut_eks  = ex.submit(get_eks_clusters)
        results["ec2"]  = fut_ec2.result()
        results["s3"]   = fut_s3.result()
        results["lbs"]  = fut_lbs.result()
        results["tgs"]  = fut_tgs.result()
        results["eks"]  = fut_eks.result()

    # TG health (parallel)
    tg_health_map = {}
    tg_list = results["tgs"]
    with concurrent.futures.ThreadPoolExecutor(max_workers=20) as ex:
        futures = {ex.submit(get_tg_health, elb, tg["TargetGroupArn"]): tg["TargetGroupArn"] for tg in tg_list}
        for fut in concurrent.futures.as_completed(futures):
            arn = futures[fut]
            tg_health_map[arn] = fut.result()

    # EC2 table
    print()
    print(f"{BOLD}EC2 Instances{RESET}")
    ec2_instances = results["ec2"]
    if ec2_instances:
        hdrs = ["Name", "Instance ID", "State", "Type", "Private IP"]
        keys = ["name", "id", "state", "type", "ip"]
        col_w = [len(h) for h in hdrs]
        rows_display = []
        for inst in ec2_instances:
            icon = state_icon(inst["state"])
            display = {**inst, "state": f"{icon} {inst['state']}"}
            rows_display.append(display)
            for i, k in enumerate(keys):
                col_w[i] = max(col_w[i], len(inst[k]))  # use raw for width

        def fmt(cells):
            return "| " + " | ".join(c.ljust(w) for c, w in zip(cells, col_w)) + " |"

        print(fmt(hdrs))
        print("|-" + "-|-".join("-" * w for w in col_w) + "-|")
        for row in rows_display:
            print(fmt([row.get(k, "") for k in keys]))
    else:
        print("  (none)")

    # S3 table
    print()
    print(f"{BOLD}S3 Buckets{RESET}")
    buckets = results["s3"]
    if buckets:
        max_w = max(len(b) for b in buckets)
        max_w = max(max_w, 4)
        print(f"| {'Name':<{max_w}} |")
        print(f"|-{'-' * max_w}-|")
        for b in buckets:
            print(f"| {b:<{max_w}} |")
    else:
        print("  (none)")

    # Load balancers table
    print()
    print(f"{BOLD}Load Balancers{RESET}")
    lbs = results["lbs"]
    if lbs:
        hdrs = ["Name", "Type", "State", "Scheme", "DNS"]
        col_w = [4, 4, 5, 6, 3]
        rows_lb = []
        for lb in lbs:
            state = lb.get("State", {}).get("Code", "")
            row = {
                "name":   lb["LoadBalancerName"],
                "type":   lb.get("Type", ""),
                "state":  f"{state_icon(state)} {state}",
                "scheme": lb.get("Scheme", ""),
                "dns":    lb["DNSName"],
            }
            rows_lb.append(row)
        for row in rows_lb:
            col_w[0] = max(col_w[0], len(row["name"]))
            col_w[1] = max(col_w[1], len(row["type"]))
            col_w[2] = max(col_w[2], len(row["state"]) - 4)  # strip icon width
            col_w[3] = max(col_w[3], len(row["scheme"]))
            col_w[4] = max(col_w[4], len(row["dns"]))

        keys = ["name", "type", "state", "scheme", "dns"]

        import re
        ansi_re = re.compile(r"\033\[[0-9;]*m")
        def vlen(s): return len(ansi_re.sub("", s))
        col_w2 = [max(len(h), max(vlen(row[k]) for row in rows_lb)) for h, k in zip(hdrs, keys)]

        def fmtlb(cells):
            padded = []
            for i, cell in enumerate(cells):
                pad = col_w2[i] - vlen(cell)
                padded.append(cell + " " * pad)
            return "| " + " | ".join(padded) + " |"

        print(fmtlb(hdrs))
        print("|-" + "-|-".join("-" * w for w in col_w2) + "-|")
        for row in rows_lb:
            print(fmtlb([row[k] for k in keys]))
    else:
        print("  (none)")

    # Target groups table
    print()
    print(f"{BOLD}Target Groups{RESET}")
    if tg_list:
        hdrs = ["Name", "Protocol:Port", "Targets", "Health"]
        rows_tg = []
        for tg in tg_list:
            arn   = tg["TargetGroupArn"]
            name  = tg["TargetGroupName"]
            proto = tg.get("Protocol", "")
            port  = tg.get("Port", "")
            pp    = f"{proto}:{port}" if proto else "—"
            h, t, _ = tg_health_map.get(arn, (0, 0, []))
            rows_tg.append({
                "name":    name,
                "pp":      pp,
                "targets": str(t),
                "health":  health_icon(h, t),
            })

        import re
        ansi_re = re.compile(r"\033\[[0-9;]*m")
        def vlen(s): return len(ansi_re.sub("", s))
        keys = ["name", "pp", "targets", "health"]
        col_w = [max(len(h), max(vlen(row[k]) for row in rows_tg)) for h, k in zip(hdrs, keys)]

        def fmttg(cells):
            padded = []
            for i, cell in enumerate(cells):
                pad = col_w[i] - vlen(cell)
                padded.append(cell + " " * pad)
            return "| " + " | ".join(padded) + " |"

        print(fmttg(hdrs))
        print("|-" + "-|-".join("-" * w for w in col_w) + "-|")
        for row in rows_tg:
            print(fmttg([row[k] for k in keys]))
    else:
        print("  (none)")

    # EKS table
    print()
    print(f"{BOLD}EKS Clusters{RESET}")
    eks_clusters = results["eks"]
    if eks_clusters:
        hdrs = ["Name", "Status", "Version"]
        keys = ["name", "status", "version"]
        col_w = [max(len(h), max(len(c[k]) for c in eks_clusters)) for h, k in zip(hdrs, keys)]

        def fmteks(cells):
            return "| " + " | ".join(c.ljust(w) for c, w in zip(cells, col_w)) + " |"

        print(fmteks(hdrs))
        print("|-" + "-|-".join("-" * w for w in col_w) + "-|")
        for c in eks_clusters:
            row = [f"{state_icon(c['status'])} {c[k]}" if k == 'status' else c[k] for k in keys]
            print(fmteks(row))
    else:
        print("  (none)")

    # Summary
    running = sum(1 for i in results["ec2"] if i["state"] == "running")
    print()
    print(
        f"Summary: {len(ec2_instances)} EC2 ({running} running) | "
        f"{len(buckets)} S3 buckets | "
        f"{len(lbs)} load balancers | "
        f"{len(tg_list)} target groups | "
        f"{len(eks_clusters)} EKS clusters"
    )


# ─── Command: eks-status ─────────────────────────────────────────────────────

def cmd_eks_status(args):
    region = args.region
    cluster_name = getattr(args, "cluster_name", None) or getattr(args, "cluster", None)
    namespace = getattr(args, "namespace", None)

    eks_client = make_client("eks", region)

    # Resolve cluster
    if not cluster_name:
        clusters = eks_client.list_clusters().get("clusters", [])
        if not clusters:
            print("No EKS clusters found.")
            return
        if len(clusters) == 1:
            cluster_name = clusters[0]
        else:
            print("Available clusters:")
            for i, c in enumerate(clusters, 1):
                print(f"  {i}. {c}")
            try:
                idx = int(input("Select cluster number: ")) - 1
                cluster_name = clusters[idx]
            except (ValueError, IndexError):
                print("Invalid selection.")
                return

    # Cluster details
    try:
        cluster_info = eks_client.describe_cluster(name=cluster_name)["cluster"]
    except ClientError as e:
        print(f"ERROR: {e}")
        return

    k8s_ver = cluster_info.get("version", "")
    status  = cluster_info.get("status", "")

    print()
    print("╔" + "═" * 58 + "╗")
    print(f"║  EKS CLUSTER: {cluster_name:<28} K8s: {k8s_ver:<8}║")
    print(f"║  Status: {status:<16}         Region: {region:<12}║")
    print("╚" + "═" * 58 + "╝")

    # Use EKS MCP or boto3 for k8s resources?
    # boto3 doesn't have direct k8s API access — use eks managed node groups as proxy for nodes
    # For actual pod/deployment data we need kubectl / k8s client
    # We'll use boto3 EKS API for node groups, and note k8s API requires kubeconfig

    # Node groups
    print()
    print(f"{BOLD}Node Groups{RESET}")
    try:
        ng_names = eks_client.list_nodegroups(clusterName=cluster_name).get("nodegroups", [])
        if ng_names:
            hdrs = ["Node Group", "Status", "Instance Type", "Desired", "Min", "Max"]
            rows_ng = []
            def fetch_ng(ng_name):
                desc = eks_client.describe_nodegroup(clusterName=cluster_name, nodegroupName=ng_name)
                ng = desc["nodegroup"]
                sc = ng.get("scalingConfig", {})
                it = ", ".join(ng.get("instanceTypes", []))
                return {
                    "name":   ng_name,
                    "status": ng.get("status", ""),
                    "itype":  it,
                    "desired": str(sc.get("desiredSize", "")),
                    "min":    str(sc.get("minSize", "")),
                    "max":    str(sc.get("maxSize", "")),
                }
            with concurrent.futures.ThreadPoolExecutor(max_workers=10) as ex:
                futs = [ex.submit(fetch_ng, n) for n in ng_names]
                for f in concurrent.futures.as_completed(futs):
                    rows_ng.append(f.result())

            keys = ["name", "status", "itype", "desired", "min", "max"]
            col_w = [max(len(h), max(len(row[k]) for row in rows_ng)) for h, k in zip(hdrs, keys)]

            def fmtng(cells):
                return "| " + " | ".join(c.ljust(w) for c, w in zip(cells, col_w)) + " |"

            print(fmtng(hdrs))
            print("|-" + "-|-".join("-" * w for w in col_w) + "-|")
            for row in rows_ng:
                st = row["status"]
                display_status = f"{state_icon(st)} {st}"
                print(fmtng([
                    row["name"],
                    display_status,
                    row["itype"],
                    row["desired"],
                    row["min"],
                    row["max"],
                ]))
        else:
            print("  (no node groups)")
    except ClientError as e:
        print(f"  Could not list node groups: {e}")

    # Fargate profiles
    try:
        fp_names = eks_client.list_fargate_profiles(clusterName=cluster_name).get("fargateProfileNames", [])
        if fp_names:
            print()
            print(f"{BOLD}Fargate Profiles{RESET}")
            for fp in fp_names:
                print(f"  • {fp}")
    except ClientError:
        pass

    # Add-ons
    print()
    print(f"{BOLD}Add-ons{RESET}")
    try:
        addon_names = eks_client.list_addons(clusterName=cluster_name).get("addons", [])
        if addon_names:
            hdrs = ["Add-on", "Status", "Version"]
            rows_a = []
            def fetch_addon(name):
                desc = eks_client.describe_addon(clusterName=cluster_name, addonName=name)
                a = desc["addon"]
                return {
                    "name":    name,
                    "status":  a.get("status", ""),
                    "version": a.get("addonVersion", ""),
                }
            with concurrent.futures.ThreadPoolExecutor(max_workers=10) as ex:
                futs = [ex.submit(fetch_addon, n) for n in addon_names]
                for f in concurrent.futures.as_completed(futs):
                    rows_a.append(f.result())

            rows_a.sort(key=lambda r: r["name"])
            keys = ["name", "status", "version"]
            col_w = [max(len(h), max(len(row[k]) for row in rows_a)) for h, k in zip(hdrs, keys)]

            def fmta(cells):
                return "| " + " | ".join(c.ljust(w) for c, w in zip(cells, col_w)) + " |"

            print(fmta(hdrs))
            print("|-" + "-|-".join("-" * w for w in col_w) + "-|")
            for row in rows_a:
                st = row["status"]
                display_status = f"{state_icon(st)} {st}"
                print(fmta([row["name"], display_status, row["version"]]))
        else:
            print("  (none)")
    except ClientError as e:
        print(f"  Could not list add-ons: {e}")

    print()
    print(f"Note: For pod/deployment status, use: kubectl get pods -A --context <cluster>")
    print(f"Summary: cluster={cluster_name} | status={status} | k8s={k8s_ver} | region={region}")


# ─── Main ─────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        prog="manukers",
        description="AWS CLI Dashboard — visualize ALB rules, health, and EKS status",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  manukers overview
  manukers overview --region us-east-1
  manukers lb-rules
  manukers lb-rules my-alb-name another-alb
  manukers lb-health my-alb-name
  manukers eks-status
  manukers eks-status my-cluster
  manukers eks-status my-cluster --namespace production
        """,
    )
    parser.add_argument(
        "--region", "-r",
        default="ap-northeast-1",
        help="AWS region (default: ap-northeast-1)",
    )

    sub = parser.add_subparsers(dest="command", required=True)

    # lb-rules
    p_lbr = sub.add_parser("lb-rules", help="Show ALB listener rules with traffic weights")
    p_lbr.add_argument("names", nargs="*", metavar="lb-name", help="Load balancer names (all if omitted)")
    p_lbr.set_defaults(func=cmd_lb_rules)

    # lb-health
    p_lbh = sub.add_parser("lb-health", help="Show ALB target health with ASCII flow diagram")
    p_lbh.add_argument("name", nargs="?", metavar="lb-name", help="Load balancer name (all if omitted)")
    p_lbh.set_defaults(func=cmd_lb_health)

    # overview
    p_ov = sub.add_parser("overview", help="Full AWS account dashboard (EC2, S3, LBs, EKS)")
    p_ov.set_defaults(func=cmd_overview)

    # eks-status
    p_eks = sub.add_parser("eks-status", help="EKS cluster dashboard (node groups, add-ons)")
    p_eks.add_argument("cluster_name", nargs="?", metavar="cluster-name", help="Cluster name (auto-detected if omitted)")
    p_eks.add_argument("--namespace", "-n", default=None, help="Kubernetes namespace filter")
    p_eks.set_defaults(func=cmd_eks_status)

    args = parser.parse_args()

    try:
        args.func(args)
    except KeyboardInterrupt:
        print("\nInterrupted.")
        sys.exit(0)
    except NoCredentialsError:
        print("ERROR: No AWS credentials found. Configure via ~/.aws/credentials, env vars, or IAM role.")
        sys.exit(1)
    except NoRegionError:
        print(f"ERROR: Region not found. Use --region or set AWS_DEFAULT_REGION.")
        sys.exit(1)


if __name__ == "__main__":
    main()
