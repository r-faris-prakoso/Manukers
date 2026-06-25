package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/eks"
)

// ListClusters returns all EKS cluster names in the region.
func (c *Client) ListClusters(ctx context.Context) ([]EKSCluster, error) {
	resp, err := c.EKS.ListClusters(ctx, &eks.ListClustersInput{})
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}

	var clusters []EKSCluster
	for _, name := range resp.Clusters {
		cluster, err := c.DescribeCluster(ctx, name)
		if err != nil {
			// Include partial data on describe failure
			clusters = append(clusters, EKSCluster{Name: name, Status: "unknown"})
			continue
		}
		clusters = append(clusters, *cluster)
	}
	return clusters, nil
}

// DescribeCluster returns full details for a single EKS cluster.
func (c *Client) DescribeCluster(ctx context.Context, name string) (*EKSCluster, error) {
	resp, err := c.EKS.DescribeCluster(ctx, &eks.DescribeClusterInput{
		Name: &name,
	})
	if err != nil {
		return nil, fmt.Errorf("describe cluster %s: %w", name, err)
	}
	cl := resp.Cluster
	cluster := &EKSCluster{
		Name:    ptrStr(cl.Name),
		Status:  string(cl.Status),
		Version: ptrStr(cl.Version),
		RoleARN: ptrStr(cl.RoleArn),
		Tags:    cl.Tags,
	}
	if cl.Endpoint != nil {
		cluster.Endpoint = *cl.Endpoint
	}
	if cl.ResourcesVpcConfig != nil {
		cluster.VpcID = ptrStr(cl.ResourcesVpcConfig.VpcId)
	}
	if cl.CreatedAt != nil {
		cluster.CreatedAt = *cl.CreatedAt
	}
	return cluster, nil
}

// ListNodeGroups returns node groups for an EKS cluster.
func (c *Client) ListNodeGroups(ctx context.Context, clusterName string) ([]NodeGroup, error) {
	resp, err := c.EKS.ListNodegroups(ctx, &eks.ListNodegroupsInput{
		ClusterName: &clusterName,
	})
	if err != nil {
		return nil, fmt.Errorf("list nodegroups for %s: %w", clusterName, err)
	}

	var nodeGroups []NodeGroup
	for _, ngName := range resp.Nodegroups {
		ng, err := c.describeNodeGroup(ctx, clusterName, ngName)
		if err != nil {
			nodeGroups = append(nodeGroups, NodeGroup{Name: ngName, Status: "unknown"})
			continue
		}
		nodeGroups = append(nodeGroups, *ng)
	}
	return nodeGroups, nil
}

func (c *Client) describeNodeGroup(ctx context.Context, clusterName, ngName string) (*NodeGroup, error) {
	resp, err := c.EKS.DescribeNodegroup(ctx, &eks.DescribeNodegroupInput{
		ClusterName:   &clusterName,
		NodegroupName: &ngName,
	})
	if err != nil {
		return nil, err
	}
	ng := resp.Nodegroup
	group := &NodeGroup{
		Name:          ptrStr(ng.NodegroupName),
		Status:        string(ng.Status),
		AmiType:       string(ng.AmiType),
		InstanceTypes: ng.InstanceTypes,
	}
	if ng.DiskSize != nil {
		group.DiskSize = *ng.DiskSize
	}
	if ng.ScalingConfig != nil {
		group.DesiredSize = ptrInt32(ng.ScalingConfig.DesiredSize)
		group.MinSize = ptrInt32(ng.ScalingConfig.MinSize)
		group.MaxSize = ptrInt32(ng.ScalingConfig.MaxSize)
	}
	return group, nil
}

// ListAddons returns add-ons for an EKS cluster.
func (c *Client) ListAddons(ctx context.Context, clusterName string) ([]Addon, error) {
	resp, err := c.EKS.ListAddons(ctx, &eks.ListAddonsInput{
		ClusterName: &clusterName,
	})
	if err != nil {
		return nil, fmt.Errorf("list addons for %s: %w", clusterName, err)
	}

	var addons []Addon
	for _, addonName := range resp.Addons {
		a, err := c.describeAddon(ctx, clusterName, addonName)
		if err != nil {
			addons = append(addons, Addon{Name: addonName, Status: "unknown"})
			continue
		}
		addons = append(addons, *a)
	}
	return addons, nil
}

func (c *Client) describeAddon(ctx context.Context, clusterName, addonName string) (*Addon, error) {
	resp, err := c.EKS.DescribeAddon(ctx, &eks.DescribeAddonInput{
		ClusterName: &clusterName,
		AddonName:   &addonName,
	})
	if err != nil {
		return nil, err
	}
	a := resp.Addon
	return &Addon{
		Name:    ptrStr(a.AddonName),
		Status:  string(a.Status),
		Version: ptrStr(a.AddonVersion),
	}, nil
}
