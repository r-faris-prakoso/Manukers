# manukers

A terminal UI dashboard for exploring AWS infrastructure. Navigate EC2, Load Balancers, EKS, ECR, S3, Target Groups, Security Groups, and connection graphs without leaving your terminal.

```
                        z$b
               .e$$$b.  $$$F  .d$$be
           .d$$$$$$$$$$e$$$be$$$$$$$$$$e.
       .e$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$$b.
     z$$$$$$$P**""**$$$$$$$$$$$P*""""***$$$$$b.
   z$$$$*"            "$$$$$$"            "*$$$$c
 z$$*"                 ^$$$$                  "*$$.
^"                      $$$F                      ^%
                        $$$b
                        $P*$
                       4P  *r
                       4    %

              MANUKERS
      AWS Infrastructure Explorer
```

## Requirements

- Go 1.21+
- AWS credentials configured (`~/.aws/credentials`, environment variables, or an IAM role)

## Install

```bash
go install github.com/your-org/manukers@latest
```

Or clone and install locally:

```bash
git clone <repo-url>
cd manukers-aws-cli-dashboard
make install
```

The binary is placed in `~/go/bin/manukers`. Make sure that directory is in your `$PATH`:

```bash
export PATH="$PATH:$HOME/go/bin"
```

Add the line above to your `~/.zshrc` or `~/.bashrc` to make it permanent.

## Usage

```bash
manukers [--region REGION] [--profile PROFILE]
```

| Flag | Default | Description |
|---|---|---|
| `--region` | `ap-northeast-1` | AWS region to query |
| `--profile` | _(default profile)_ | AWS named profile from `~/.aws/config` |

Examples:

```bash
manukers
manukers --region us-east-1
manukers --region eu-west-1 --profile staging
```

## Navigation

### Picker screen

The app opens on a resource picker. Use `↑↓` to select a resource type and `Enter` to open it. Start typing to filter the list.

### Global keys

| Key | Action |
|---|---|
| `↑ / ↓` | Move selection |
| `Enter` | Open / expand |
| `Esc` | Go back / return to picker |
| `/` | Open filter bar |
| `r` | Refresh current view (re-fetches from AWS) |
| `:` | Open command bar |
| `q` | Quit |

### Command bar (`:`)

Type a resource name and press `Enter` to jump directly to any view:

```
:ec2        :lb         :tg         :sg
:eks        :ecr        :s3         :graph
```

Tab-completion hints appear as you type.

## Views

### EC2 — Instances

Lists all EC2 instances in the region. Columns: name, instance ID, state, type, private IP, public IP, AZ, launch date.

- `Enter` — instance detail (VPC, subnet, tags, security groups)
- `/` — filter by name or instance ID

### Load Balancers

Lists ALB and NLB load balancers. Columns: name, type, state, scheme, VPC, DNS name.

- `Enter` — drill into listeners
- `Enter` on a listener — view routing rules and actions (forward, redirect, fixed-response)
- `/` — filter LBs by name; `/` inside rules filters by priority, condition, or target group

### Target Groups

Lists all target groups with live health counts. Columns: name, protocol:port, type, VPC, health, attached LBs.

- `Enter` — open the **Health Monitor** (auto-refreshes every 10 s)
- Health Monitor shows: target health check config, per-target state and description, last-updated timestamp
- `/` — filter by name

### Security Groups

Lists all security groups. Columns: name, group ID, VPC, description, inbound rule count, outbound rule count.

- `Enter` — view full inbound and outbound rules (protocol, port range, source/dest, description)
- `/` — filter by name or group ID

### EKS — Clusters

Lists EKS clusters. Columns: name, status, Kubernetes version, VPC, endpoint.

- `Enter` — cluster detail including node groups (desired/min/max, instance types) and add-ons with versions
- `/` — filter by name

### ECR — Repositories

Lists ECR repositories. Columns: name, URI, mutability, scan-on-push, created date.

- `Enter` — list images with digest, tags, size, push date, and scan status
- `/` — filter by repository name

### S3 — Buckets

Lists S3 buckets with creation date.

- `Enter` — bucket detail: region, S3 URI, ARN, and example AWS CLI commands
- `/` — filter by bucket name

### Connection Graph

Split-panel view showing the full routing chain: **LB → Listeners → Rules → Target Groups → Targets**.

The left panel lists all load balancers. Selecting one builds the tree on the right, fetching listeners, rules, and target health concurrently.

- `Tab` — switch focus between the LB list and the tree
- `Enter` — expand / collapse tree nodes
- `/` — filter the LB list by name (works from either panel)

Health is colour-coded: green (all healthy), yellow (degraded), red (all unhealthy).

## Loading indicator

A spinner in the footer bar (`⠋ Loading eks…`) appears during any AWS API call. The connection graph also animates the tree panel title while fetching. Both clear automatically when the load completes.

## Build from source

```bash
make build    # produces ./manukers binary
make run      # build + run with default region
make install  # install to ~/go/bin
make tidy     # go mod tidy
make clean    # remove local binary
```

Override region or profile at build-time:

```bash
make run REGION=us-west-2 PROFILE=prod
```

## AWS permissions

The minimum IAM permissions required:

```json
{
  "Effect": "Allow",
  "Action": [
    "ec2:DescribeInstances",
    "ec2:DescribeSecurityGroups",
    "elasticloadbalancing:DescribeLoadBalancers",
    "elasticloadbalancing:DescribeListeners",
    "elasticloadbalancing:DescribeRules",
    "elasticloadbalancing:DescribeTargetGroups",
    "elasticloadbalancing:DescribeTargetHealth",
    "eks:ListClusters",
    "eks:DescribeCluster",
    "eks:ListNodegroups",
    "eks:DescribeNodegroup",
    "eks:ListAddons",
    "eks:DescribeAddon",
    "ecr:DescribeRepositories",
    "ecr:DescribeImages",
    "s3:ListAllMyBuckets",
    "s3:GetBucketLocation"
  ],
  "Resource": "*"
}
```
