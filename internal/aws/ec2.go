package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// ListInstances returns all EC2 instances in the region.
func (c *Client) ListInstances(ctx context.Context) ([]Instance, error) {
	var instances []Instance
	paginator := ec2.NewDescribeInstancesPaginator(c.EC2, &ec2.DescribeInstancesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe instances: %w", err)
		}
		for _, r := range page.Reservations {
			for _, inst := range r.Instances {
				instances = append(instances, instanceFromAWS(inst))
			}
		}
	}
	return instances, nil
}

// ListSecurityGroups returns all security groups in the region.
func (c *Client) ListSecurityGroups(ctx context.Context) ([]SecurityGroup, error) {
	resp, err := c.EC2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{})
	if err != nil {
		return nil, fmt.Errorf("describe security groups: %w", err)
	}

	var sgs []SecurityGroup
	for _, sg := range resp.SecurityGroups {
		sgs = append(sgs, sgFromAWS(sg))
	}
	return sgs, nil
}

// GetSecurityGroup returns details for a single security group by ID.
func (c *Client) GetSecurityGroup(ctx context.Context, sgID string) (*SecurityGroup, error) {
	resp, err := c.EC2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		GroupIds: []string{sgID},
	})
	if err != nil {
		return nil, fmt.Errorf("describe security group %s: %w", sgID, err)
	}
	if len(resp.SecurityGroups) == 0 {
		return nil, fmt.Errorf("security group %s not found", sgID)
	}
	sg := sgFromAWS(resp.SecurityGroups[0])
	return &sg, nil
}

// ─── Converters ──────────────────────────────────────────────────────────────

func instanceFromAWS(inst ec2types.Instance) Instance {
	i := Instance{
		ID:        ptrStr(inst.InstanceId),
		State:     string(inst.State.Name),
		Type:      string(inst.InstanceType),
		PrivateIP: ptrStr(inst.PrivateIpAddress),
		PublicIP:  ptrStr(inst.PublicIpAddress),
		VpcID:     ptrStr(inst.VpcId),
		SubnetID:  ptrStr(inst.SubnetId),
		ImageID:   ptrStr(inst.ImageId),
		KeyName:   ptrStr(inst.KeyName),
		Tags:      tagsToMap(inst.Tags),
	}
	if inst.Placement != nil {
		i.AZ = ptrStr(inst.Placement.AvailabilityZone)
	}
	if inst.LaunchTime != nil {
		i.LaunchTime = *inst.LaunchTime
	}
	for _, sg := range inst.SecurityGroups {
		i.SecurityGroups = append(i.SecurityGroups, SecurityGroupRef{
			ID:   ptrStr(sg.GroupId),
			Name: ptrStr(sg.GroupName),
		})
	}
	if name, ok := i.Tags["Name"]; ok && name != "" {
		i.Name = name
	} else {
		i.Name = i.ID
	}
	return i
}

func sgFromAWS(sg ec2types.SecurityGroup) SecurityGroup {
	s := SecurityGroup{
		ID:          ptrStr(sg.GroupId),
		Name:        ptrStr(sg.GroupName),
		Description: ptrStr(sg.Description),
		VpcID:       ptrStr(sg.VpcId),
		Tags:        tagsToMap(sg.Tags),
	}
	for _, rule := range sg.IpPermissions {
		s.InboundRules = append(s.InboundRules, ipPermToRule(rule))
	}
	for _, rule := range sg.IpPermissionsEgress {
		s.OutboundRules = append(s.OutboundRules, ipPermToRule(rule))
	}
	return s
}

func ipPermToRule(perm ec2types.IpPermission) SGRule {
	rule := SGRule{
		Protocol: ptrStr(perm.IpProtocol),
	}
	if rule.Protocol == "-1" {
		rule.Protocol = "All"
		rule.PortRange = "All"
	} else if perm.FromPort != nil && perm.ToPort != nil {
		if *perm.FromPort == *perm.ToPort {
			rule.PortRange = fmt.Sprintf("%d", *perm.FromPort)
		} else {
			rule.PortRange = fmt.Sprintf("%d-%d", *perm.FromPort, *perm.ToPort)
		}
	}

	var sources []string
	for _, r := range perm.IpRanges {
		sources = append(sources, ptrStr(r.CidrIp))
		if r.Description != nil && *r.Description != "" {
			rule.Description = *r.Description
		}
	}
	for _, r := range perm.Ipv6Ranges {
		sources = append(sources, ptrStr(r.CidrIpv6))
	}
	for _, r := range perm.UserIdGroupPairs {
		src := ptrStr(r.GroupId)
		if r.GroupName != nil && *r.GroupName != "" {
			src = fmt.Sprintf("%s (%s)", src, *r.GroupName)
		}
		sources = append(sources, src)
	}
	rule.Source = strings.Join(sources, ", ")
	return rule
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func tagsToMap(tags []ec2types.Tag) map[string]string {
	m := make(map[string]string)
	for _, t := range tags {
		if t.Key != nil && t.Value != nil {
			m[*t.Key] = *t.Value
		}
	}
	return m
}
