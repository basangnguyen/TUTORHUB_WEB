package objectstorage

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
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
	CreateMultipartUpload(
		context.Context,
		*s3.CreateMultipartUploadInput,
		...func(*s3.Options),
	) (*s3.CreateMultipartUploadOutput, error)
	CompleteMultipartUpload(
		context.Context,
		*s3.CompleteMultipartUploadInput,
		...func(*s3.Options),
	) (*s3.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(
		context.Context,
		*s3.AbortMultipartUploadInput,
		...func(*s3.Options),
	) (*s3.AbortMultipartUploadOutput, error)
}

type s3PresignAPI interface {
	PresignPutObject(
		context.Context,
		*s3.PutObjectInput,
		...func(*s3.PresignOptions),
	) (*v4.PresignedHTTPRequest, error)
	PresignGetObject(
		context.Context,
		*s3.GetObjectInput,
		...func(*s3.PresignOptions),
	) (*v4.PresignedHTTPRequest, error)
	PresignUploadPart(
		context.Context,
		*s3.UploadPartInput,
		...func(*s3.PresignOptions),
	) (*v4.PresignedHTTPRequest, error)
}

type B2Store struct {
	client    s3API
	presigner s3PresignAPI
	bucket    string
}

const maximumPresignTTL = 15 * time.Minute

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

	return newB2StoreWithPresigner(client, s3.NewPresignClient(client), cfg.Bucket)
}

func newB2Store(client s3API, bucket string) (*B2Store, error) {
	return newB2StoreWithPresigner(client, nil, bucket)
}

func newB2StoreWithPresigner(
	client s3API,
	presigner s3PresignAPI,
	bucket string,
) (*B2Store, error) {
	if client == nil {
		return nil, fmt.Errorf("S3 client is required")
	}
	if strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("B2 bucket is required")
	}

	return &B2Store{client: client, presigner: presigner, bucket: bucket}, nil
}

func (store *B2Store) PresignUpload(
	ctx context.Context,
	input UploadPresignInput,
) (PresignedRequest, error) {
	if store == nil || store.presigner == nil {
		return PresignedRequest{}, fmt.Errorf("presign upload: object storage presigner is unavailable")
	}
	if err := validateKey(input.Key); err != nil {
		return PresignedRequest{}, err
	}
	contentType := strings.TrimSpace(input.ContentType)
	if input.ContentLength < 1 || contentType == "" ||
		!validPresignTTL(input.Expires) {
		return PresignedRequest{}, fmt.Errorf("presign upload: invalid transfer constraints")
	}

	output, err := store.presigner.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(store.bucket),
		Key:           aws.String(input.Key),
		ContentLength: aws.Int64(input.ContentLength),
		ContentType:   aws.String(contentType),
	}, func(options *s3.PresignOptions) { options.Expires = input.Expires })
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign upload request: %w", err)
	}
	return validatedPresignedRequest(output, http.MethodPut)
}

func (store *B2Store) PresignDownload(
	ctx context.Context,
	input DownloadPresignInput,
) (PresignedRequest, error) {
	if store == nil || store.presigner == nil {
		return PresignedRequest{}, fmt.Errorf("presign download: object storage presigner is unavailable")
	}
	if err := validateKey(input.Key); err != nil {
		return PresignedRequest{}, err
	}
	versionID := strings.TrimSpace(input.VersionID)
	contentType := strings.TrimSpace(input.ContentType)
	disposition := strings.TrimSpace(input.ContentDisposition)
	if versionID == "" || contentType == "" || disposition == "" ||
		containsHeaderControl(disposition) || !validPresignTTL(input.Expires) {
		return PresignedRequest{}, fmt.Errorf("presign download: invalid transfer constraints")
	}

	output, err := store.presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(store.bucket),
		Key:                        aws.String(input.Key),
		VersionId:                  aws.String(versionID),
		ResponseContentType:        aws.String(contentType),
		ResponseContentDisposition: aws.String(disposition),
	}, func(options *s3.PresignOptions) {
		options.Expires = input.Expires
	})
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign download request: %w", err)
	}
	return validatedPresignedRequest(output, http.MethodGet)
}

func (store *B2Store) CreateMultipart(
	ctx context.Context,
	input MultipartCreateInput,
) (string, error) {
	if store == nil || store.client == nil {
		return "", fmt.Errorf("create multipart upload: object storage is unavailable")
	}
	if err := validateKey(input.Key); err != nil {
		return "", err
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		return "", fmt.Errorf("create multipart upload: content type is required")
	}
	output, err := store.client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(store.bucket), Key: aws.String(input.Key),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("create multipart upload: %w", err)
	}
	uploadID := strings.TrimSpace(aws.ToString(output.UploadId))
	if uploadID == "" {
		return "", fmt.Errorf("create multipart upload returned an empty upload ID")
	}
	return uploadID, nil
}

func (store *B2Store) PresignMultipartPart(
	ctx context.Context,
	input MultipartPartPresignInput,
) (PresignedRequest, error) {
	if store == nil || store.presigner == nil {
		return PresignedRequest{}, fmt.Errorf("presign multipart part: object storage presigner is unavailable")
	}
	if err := validateKey(input.Key); err != nil {
		return PresignedRequest{}, err
	}
	uploadID := strings.TrimSpace(input.UploadID)
	if uploadID == "" || input.PartNumber < 1 || input.PartNumber > 10000 ||
		input.ContentLength < 1 || !validPresignTTL(input.Expires) {
		return PresignedRequest{}, fmt.Errorf("presign multipart part: invalid transfer constraints")
	}
	output, err := store.presigner.PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(store.bucket), Key: aws.String(input.Key),
		UploadId: aws.String(uploadID), PartNumber: aws.Int32(input.PartNumber),
		ContentLength: aws.Int64(input.ContentLength),
	}, func(options *s3.PresignOptions) { options.Expires = input.Expires })
	if err != nil {
		return PresignedRequest{}, fmt.Errorf("presign multipart part request: %w", err)
	}
	return validatedPresignedRequest(output, http.MethodPut)
}

func (store *B2Store) CompleteMultipart(
	ctx context.Context,
	input MultipartCompleteInput,
) (MultipartCompleteResult, error) {
	if store == nil || store.client == nil {
		return MultipartCompleteResult{}, fmt.Errorf("complete multipart upload: object storage is unavailable")
	}
	if err := validateKey(input.Key); err != nil {
		return MultipartCompleteResult{}, err
	}
	uploadID := strings.TrimSpace(input.UploadID)
	if uploadID == "" || len(input.Parts) == 0 || len(input.Parts) > 10000 {
		return MultipartCompleteResult{}, fmt.Errorf("complete multipart upload: invalid manifest")
	}
	parts := make([]types.CompletedPart, len(input.Parts))
	for index, part := range input.Parts {
		etag := strings.TrimSpace(part.ETag)
		if part.PartNumber != int32(index+1) || etag == "" || containsHeaderControl(etag) {
			return MultipartCompleteResult{}, fmt.Errorf("complete multipart upload: invalid part")
		}
		parts[index] = types.CompletedPart{PartNumber: aws.Int32(part.PartNumber), ETag: aws.String(etag)}
	}
	output, err := store.client.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket: aws.String(store.bucket), Key: aws.String(input.Key), UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	})
	if err != nil {
		return MultipartCompleteResult{}, fmt.Errorf("complete multipart upload: %w", err)
	}
	result := MultipartCompleteResult{
		ETag:      strings.TrimSpace(aws.ToString(output.ETag)),
		VersionID: strings.TrimSpace(aws.ToString(output.VersionId)),
	}
	if result.ETag == "" || result.VersionID == "" {
		return MultipartCompleteResult{}, fmt.Errorf("complete multipart upload returned incomplete proof")
	}
	return result, nil
}

func (store *B2Store) AbortMultipart(
	ctx context.Context,
	input MultipartAbortInput,
) error {
	if store == nil || store.client == nil {
		return fmt.Errorf("abort multipart upload: object storage is unavailable")
	}
	if err := validateKey(input.Key); err != nil {
		return err
	}
	uploadID := strings.TrimSpace(input.UploadID)
	if uploadID == "" {
		return fmt.Errorf("abort multipart upload: upload ID is required")
	}
	_, err := store.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(store.bucket), Key: aws.String(input.Key), UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("abort multipart upload: %w", err)
	}
	return nil
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
	return store.head(ctx, key, "")
}

func (store *B2Store) HeadVersion(
	ctx context.Context,
	key string,
	versionID string,
) (Metadata, error) {
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return Metadata{}, fmt.Errorf("object version ID is required")
	}
	return store.head(ctx, key, versionID)
}

func (store *B2Store) head(ctx context.Context, key string, versionID string) (Metadata, error) {
	if err := validateKey(key); err != nil {
		return Metadata{}, err
	}
	input := &s3.HeadObjectInput{
		Bucket:       aws.String(store.bucket),
		Key:          aws.String(key),
		ChecksumMode: types.ChecksumModeEnabled,
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	output, err := store.client.HeadObject(ctx, input)
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

func (store *B2Store) DeleteVersion(ctx context.Context, key string, versionID string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	versionID = strings.TrimSpace(versionID)
	if versionID == "" {
		return fmt.Errorf("object version ID is required")
	}

	if _, err := store.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket:    aws.String(store.bucket),
		Key:       aws.String(key),
		VersionId: aws.String(versionID),
	}); err != nil {
		return fmt.Errorf("delete object version: %w", err)
	}

	return nil
}

func validPresignTTL(value time.Duration) bool {
	return value > 0 && value <= maximumPresignTTL
}

func containsHeaderControl(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func validatedPresignedRequest(
	request *v4.PresignedHTTPRequest,
	expectedMethod string,
) (PresignedRequest, error) {
	if request == nil || request.Method != expectedMethod {
		return PresignedRequest{}, fmt.Errorf("presign request returned an invalid method")
	}
	parsedURL, err := url.Parse(request.URL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" ||
		parsedURL.User != nil || parsedURL.Fragment != "" {
		return PresignedRequest{}, fmt.Errorf("presign request returned an invalid URL")
	}
	if request.SignedHeader.Get("Authorization") != "" {
		return PresignedRequest{}, fmt.Errorf("presign request returned an authorization header")
	}

	return PresignedRequest{
		URL: request.URL, Method: request.Method, SignedHeader: request.SignedHeader.Clone(),
	}, nil
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
