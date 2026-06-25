package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// ListLoadBalancers returns all ALBs and NLBs in the region.
func (c *Client) ListLoadBalancers(ctx context.Context) ([]LoadBalancer, error) {
	var lbs []LoadBalancer
	paginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(c.ELB, &elasticloadbalancingv2.DescribeLoadBalancersInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe load balancers: %w", err)
		}
		for _, lb := range page.LoadBalancers {
			lbs = append(lbs, lbFromAWS(lb))
		}
	}
	return lbs, nil
}

// GetListeners returns all listeners for a load balancer ARN.
func (c *Client) GetListeners(ctx context.Context, lbARN string) ([]Listener, error) {
	resp, err := c.ELB.DescribeListeners(ctx, &elasticloadbalancingv2.DescribeListenersInput{
		LoadBalancerArn: &lbARN,
	})
	if err != nil {
		return nil, fmt.Errorf("describe listeners: %w", err)
	}

	var listeners []Listener
	for _, l := range resp.Listeners {
		listener := Listener{
			ARN:      ptrStr(l.ListenerArn),
			LBArn:    lbARN,
			Port:     ptrInt32(l.Port),
			Protocol: string(l.Protocol),
		}
		if l.SslPolicy != nil {
			listener.SSLPolicy = *l.SslPolicy
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}

// GetRules returns all routing rules for a listener ARN.
func (c *Client) GetRules(ctx context.Context, listenerARN string) ([]Rule, error) {
	resp, err := c.ELB.DescribeRules(ctx, &elasticloadbalancingv2.DescribeRulesInput{
		ListenerArn: &listenerARN,
	})
	if err != nil {
		return nil, fmt.Errorf("describe rules: %w", err)
	}

	var rules []Rule
	for _, r := range resp.Rules {
		rule := Rule{
			ARN:       ptrStr(r.RuleArn),
			Priority:  ptrStr(r.Priority),
			IsDefault: ptrBool(r.IsDefault),
		}
		for _, cond := range r.Conditions {
			rc := RuleCondition{Field: ptrStr(cond.Field)}
			if cond.Values != nil {
				rc.Values = cond.Values
			}
			rule.Conditions = append(rule.Conditions, rc)
		}
		for _, action := range r.Actions {
			ra := RuleAction{Type: string(action.Type)}
			if action.TargetGroupArn != nil {
				ra.TargetGroupARN = *action.TargetGroupArn
			}
			if action.ForwardConfig != nil {
				for _, tg := range action.ForwardConfig.TargetGroups {
					ra.TargetGroupARN = ptrStr(tg.TargetGroupArn)
					if tg.Weight != nil {
						ra.Weight = *tg.Weight
					}
				}
			}
			if action.RedirectConfig != nil {
				ra.RedirectConfig = &RedirectConfig{
					Protocol:   ptrStr(action.RedirectConfig.Protocol),
					Port:       ptrStr(action.RedirectConfig.Port),
					StatusCode: string(action.RedirectConfig.StatusCode),
				}
			}
			rule.Actions = append(rule.Actions, ra)
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ListTargetGroups returns all target groups in the region.
func (c *Client) ListTargetGroups(ctx context.Context) ([]TargetGroup, error) {
	var tgs []TargetGroup
	paginator := elasticloadbalancingv2.NewDescribeTargetGroupsPaginator(c.ELB, &elasticloadbalancingv2.DescribeTargetGroupsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe target groups: %w", err)
		}
		for _, tg := range page.TargetGroups {
			tgs = append(tgs, tgFromAWS(tg))
		}
	}
	return tgs, nil
}

// GetTargetHealth returns the health of all targets in a target group.
func (c *Client) GetTargetHealth(ctx context.Context, tgARN string) ([]Target, error) {
	resp, err := c.ELB.DescribeTargetHealth(ctx, &elasticloadbalancingv2.DescribeTargetHealthInput{
		TargetGroupArn: &tgARN,
	})
	if err != nil {
		return nil, fmt.Errorf("describe target health: %w", err)
	}

	var targets []Target
	for _, th := range resp.TargetHealthDescriptions {
		t := Target{}
		if th.Target != nil {
			t.ID = ptrStr(th.Target.Id)
			t.Port = ptrInt32(th.Target.Port)
			t.AZ = ptrStr(th.Target.AvailabilityZone)
		}
		if th.TargetHealth != nil {
			t.Health = string(th.TargetHealth.State)
			if th.TargetHealth.Description != nil {
				t.HealthDesc = *th.TargetHealth.Description
			}
			t.HealthReason = string(th.TargetHealth.Reason)
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// ─── Converters ──────────────────────────────────────────────────────────────

func lbFromAWS(lb elbtypes.LoadBalancer) LoadBalancer {
	l := LoadBalancer{
		Name:    ptrStr(lb.LoadBalancerName),
		ARN:     ptrStr(lb.LoadBalancerArn),
		Type:    string(lb.Type),
		Scheme:  string(lb.Scheme),
		State:   string(lb.State.Code),
		DNSName: ptrStr(lb.DNSName),
		VpcID:   ptrStr(lb.VpcId),
	}
	if lb.CreatedTime != nil {
		l.CreatedTime = *lb.CreatedTime
	}
	for _, az := range lb.AvailabilityZones {
		l.AZs = append(l.AZs, ptrStr(az.ZoneName))
	}
	return l
}

func tgFromAWS(tg elbtypes.TargetGroup) TargetGroup {
	t := TargetGroup{
		Name:       ptrStr(tg.TargetGroupName),
		ARN:        ptrStr(tg.TargetGroupArn),
		Protocol:   string(tg.Protocol),
		Port:       ptrInt32(tg.Port),
		VpcID:      ptrStr(tg.VpcId),
		TargetType: string(tg.TargetType),
	}
	for _, lb := range tg.LoadBalancerArns {
		t.LoadBalancers = append(t.LoadBalancers, lb)
	}
	if tg.HealthCheckProtocol != "" {
		t.HealthCheck = HealthCheckConfig{
			Protocol:           string(tg.HealthCheckProtocol),
			Path:               ptrStr(tg.HealthCheckPath),
			Port:               ptrStr(tg.HealthCheckPort),
			HealthyThreshold:   ptrInt32(tg.HealthyThresholdCount),
			UnhealthyThreshold: ptrInt32(tg.UnhealthyThresholdCount),
			Interval:           ptrInt32(tg.HealthCheckIntervalSeconds),
			Timeout:            ptrInt32(tg.HealthCheckTimeoutSeconds),
		}
	}
	return t
}

func ptrInt32(n *int32) int32 {
	if n == nil {
		return 0
	}
	return *n
}

func ptrBool(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}
