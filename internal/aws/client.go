package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Client holds initialized AWS service clients.
type Client struct {
	EC2    *ec2.Client
	ELB    *elasticloadbalancingv2.Client
	EKS    *eks.Client
	ECR    *ecr.Client
	S3     *s3.Client
	Region string
}

// NewClient creates a new AWS client with the given region and optional profile.
func NewClient(region, profile string) (*Client, error) {
	var opts []func(*config.LoadOptions) error

	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &Client{
		EC2:    ec2.NewFromConfig(cfg),
		ELB:    elasticloadbalancingv2.NewFromConfig(cfg),
		EKS:    eks.NewFromConfig(cfg),
		ECR:    ecr.NewFromConfig(cfg),
		S3:     s3.NewFromConfig(cfg),
		Region: region,
	}, nil
}
