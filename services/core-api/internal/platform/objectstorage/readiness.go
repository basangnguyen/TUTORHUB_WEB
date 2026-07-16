package objectstorage

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type ReadinessCheck struct {
	client  s3API
	bucket  string
	timeout time.Duration
}

func NewReadinessCheck(store *B2Store, timeout time.Duration) ReadinessCheck {
	if store == nil {
		return ReadinessCheck{timeout: timeout}
	}

	return ReadinessCheck{
		client:  store.client,
		bucket:  store.bucket,
		timeout: timeout,
	}
}

func (ReadinessCheck) Name() string {
	return "object_storage"
}

func (check ReadinessCheck) Check(ctx context.Context) error {
	if check.client == nil || check.bucket == "" {
		return fmt.Errorf("object storage is not configured")
	}

	checkContext, cancel := context.WithTimeout(ctx, check.timeout)
	defer cancel()

	if _, err := check.client.HeadBucket(checkContext, &s3.HeadBucketInput{
		Bucket: aws.String(check.bucket),
	}); err != nil {
		return fmt.Errorf("head object storage bucket: %w", err)
	}

	return nil
}
