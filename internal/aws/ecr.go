package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
)

// ListRepositories returns all ECR repositories in the region.
func (c *Client) ListRepositories(ctx context.Context) ([]ECRRepository, error) {
	var repos []ECRRepository
	paginator := ecr.NewDescribeRepositoriesPaginator(c.ECR, &ecr.DescribeRepositoriesInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe repositories: %w", err)
		}
		for _, r := range page.Repositories {
			repo := ECRRepository{
				Name:       ptrStr(r.RepositoryName),
				URI:        ptrStr(r.RepositoryUri),
				ARN:        ptrStr(r.RepositoryArn),
				Mutability: string(r.ImageTagMutability),
			}
			if r.CreatedAt != nil {
				repo.CreatedAt = *r.CreatedAt
			}
			if r.ImageScanningConfiguration != nil {
				repo.ScanOnPush = r.ImageScanningConfiguration.ScanOnPush
			}
			repos = append(repos, repo)
		}
	}
	return repos, nil
}

// ListImages returns images in a given ECR repository.
func (c *Client) ListImages(ctx context.Context, repoName string) ([]ECRImage, error) {
	paginator := ecr.NewDescribeImagesPaginator(c.ECR, &ecr.DescribeImagesInput{
		RepositoryName: &repoName,
	})

	var images []ECRImage
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe images for %s: %w", repoName, err)
		}
		for _, img := range page.ImageDetails {
			i := ECRImage{
				Tags:   img.ImageTags,
				Digest: ptrStr(img.ImageDigest),
			}
			if img.ImagePushedAt != nil {
				i.PushedAt = *img.ImagePushedAt
			}
			if img.ImageSizeInBytes != nil {
				i.SizeBytes = *img.ImageSizeInBytes
			}
			if img.ImageScanStatus != nil {
				i.ScanStatus = string(img.ImageScanStatus.Status)
			}
			images = append(images, i)
		}
	}
	return images, nil
}
