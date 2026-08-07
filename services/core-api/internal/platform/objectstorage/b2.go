package objectstorage

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/tutorhub-v2/core-api/internal/config"
)

type s3API interface {
	PutObject(
		context.Context,
		*s3.PutObjectInput,
		...func(*s3.Options),
	) (*s3.PutObjectOutput, error)
	GetObject(
		context.Context,
		*s3.GetObjectInput,
		...func(*s3.Options),
	) (*s3.GetObjectOutput, error)
	DeleteObject(
		context.Context,
		*s3.DeleteObjectInput,
		...func(*s3.Options),
	) (*s3.DeleteObjectOutput, error)
	HeadBucket(
		context.Context,
		*s3.HeadBucketInput,
		...func(*s3.Options),
	) (*s3.HeadBucketOutput, error)
	HeadObject(
		context.Context,
		*s3.HeadObjectInput,
		...func(*s3.Options),
	) (*s3.HeadObjectOutput, error)
}

type B2Store struct {
	client s3API
	bucket string
}

func NewB2(ctx context.Context, cfg config.ObjectStorageConfig) (*B2Store, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("object storage is disabled")
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(
		ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.KeyID,
			cfg.ApplicationKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("load S3 client configuration: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.Endpoint)
		options.UsePathStyle = true
	})

	return newB2Store(client, cfg.Bucket)
}

func newB2Store(client s3API, bucket string) (*B2Store, error) {
	if client == nil {
		return nil, fmt.Errorf("S3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("B2 bucket is required")
	}

	return &B2Store{client: client, bucket: bucket}, nil
}

func (store *B2Store) Put(
	ctx context.Context,
	key string,
	body io.Reader,
	contentLength int64,
	contentType string,
) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if body == nil {
		return fmt.Errorf("object body is required")
	}
	if contentLength < 0 {
		return fmt.Errorf("object content length must not be negative")
	}

	input := &s3.PutObjectInput{
		Bucket:        aws.String(store.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(contentLength),
	}
	if contentType = strings.TrimSpace(contentType); contentType != "" {
		input.ContentType = aws.String(contentType)
	}

	if _, err := store.client.PutObject(ctx, input); err != nil {
		return fmt.Errorf("put object: %w", err)
	}

	return nil
}

func (store *B2Store) Get(ctx context.Context, key string) (Object, error) {
	if err := validateKey(key); err != nil {
		return Object{}, err
	}

	output, err := store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(store.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return Object{}, fmt.Errorf("get object: %w", err)
	}
	if output.Body == nil {
		return Object{}, fmt.Errorf("get object returned an empty body")
	}

	return Object{
		Body:          output.Body,
		ContentLength: aws.ToInt64(output.ContentLength),
		ContentType:   aws.ToString(output.ContentType),
		ETag:          aws.ToString(output.ETag),
	}, nil
}

func (store *B2Store) Head(ctx context.Context, key string) (Metadata, error) {
	if err := validateKey(key); err != nil {
		return Metadata{}, err
	}
	output, err := store.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket:       aws.String(store.bucket),
		Key:          aws.String(key),
		ChecksumMode: types.ChecksumModeEnabled,
	})
	if err != nil {
		return Metadata{}, fmt.Errorf("head object: %w", err)
	}
	var checksum []byte
	if encoded := strings.TrimSpace(aws.ToString(output.ChecksumSHA256)); encoded != "" {
		checksum, err = base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return Metadata{}, fmt.Errorf("decode object SHA-256 checksum: %w", err)
		}
	}
	return Metadata{
		ContentLength:  aws.ToInt64(output.ContentLength),
		ContentType:    strings.TrimSpace(aws.ToString(output.ContentType)),
		ChecksumSHA256: checksum,
		ETag:           strings.TrimSpace(aws.ToString(output.ETag)),
		VersionID:      strings.TrimSpace(aws.ToString(output.VersionId)),
	}, nil
}

func (store *B2Store) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}

	if _, err := store.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(store.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("delete object: %w", err)
	}

	return nil
}

func validateKey(key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return fmt.Errorf("object key is required")
	}
	if strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return fmt.Errorf("object key must be a relative slash-separated path")
	}

	return nil
}
