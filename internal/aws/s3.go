package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// ListBuckets returns all S3 buckets in the account (global, not region-scoped).
func (c *Client) ListBuckets(ctx context.Context) ([]S3Bucket, error) {
	resp, err := c.S3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, fmt.Errorf("list buckets: %w", err)
	}

	var buckets []S3Bucket
	for _, b := range resp.Buckets {
		bucket := S3Bucket{
			Name: ptrStr(b.Name),
		}
		if b.CreationDate != nil {
			bucket.CreatedAt = *b.CreationDate
		}
		buckets = append(buckets, bucket)
	}
	return buckets, nil
}

// GetBucketRegion returns the region for a specific S3 bucket.
func (c *Client) GetBucketRegion(ctx context.Context, bucket string) (string, error) {
	resp, err := c.S3.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: &bucket,
	})
	if err != nil {
		return "", fmt.Errorf("get bucket location for %s: %w", bucket, err)
	}
	region := string(resp.LocationConstraint)
	if region == "" {
		region = "us-east-1" // empty LocationConstraint means us-east-1
	}
	return region, nil
}
