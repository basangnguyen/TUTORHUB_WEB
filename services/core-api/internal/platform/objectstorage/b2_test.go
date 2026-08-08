package objectstorage

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/tutorhub-v2/core-api/internal/config"
)

func TestB2StorePutGetDelete(t *testing.T) {
	t.Parallel()

	client := &fakeS3Client{
		getOutput: &s3.GetObjectOutput{
			Body:          io.NopCloser(strings.NewReader("stored payload")),
			ContentLength: aws.Int64(14),
			ContentType:   aws.String("text/plain"),
			ETag:          aws.String("etag"),
		},
		headObjectOutput: &s3.HeadObjectOutput{
			ContentLength: aws.Int64(14), ContentType: aws.String("text/plain"),
			ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(make([]byte, 32))),
			ETag:           aws.String("etag"), VersionId: aws.String("version-1"),
		},
	}
	store, err := newB2Store(client, "tutorhub-staging")
	if err != nil {
		t.Fatalf("create B2 store: %v", err)
	}

	if err := store.Put(
		context.Background(),
		"smoke/object.txt",
		bytes.NewBufferString("stored payload"),
		14,
		"text/plain",
	); err != nil {
		t.Fatalf("put object: %v", err)
	}
	if aws.ToString(client.putInput.Bucket) != "tutorhub-staging" ||
		aws.ToString(client.putInput.Key) != "smoke/object.txt" ||
		aws.ToInt64(client.putInput.ContentLength) != 14 {
		t.Fatalf("unexpected put input: %+v", client.putInput)
	}

	object, err := store.Get(context.Background(), "smoke/object.txt")
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	defer object.Body.Close()
	payload, err := io.ReadAll(object.Body)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if string(payload) != "stored payload" || object.ContentType != "text/plain" {
		t.Fatalf("unexpected object: %+v", object)
	}

	metadata, err := store.Head(context.Background(), "smoke/object.txt")
	if err != nil {
		t.Fatalf("head object: %v", err)
	}
	if metadata.ContentLength != 14 || metadata.ContentType != "text/plain" ||
		len(metadata.ChecksumSHA256) != 32 || metadata.VersionID != "version-1" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	if client.headObjectInput.ChecksumMode != types.ChecksumModeEnabled {
		t.Fatal("head object must request checksum metadata")
	}
	metadata, err = store.HeadVersion(context.Background(), "smoke/object.txt", "version-1")
	if err != nil || metadata.VersionID != "version-1" ||
		aws.ToString(client.headObjectInput.VersionId) != "version-1" {
		t.Fatalf("unexpected exact-version metadata: metadata=%+v input=%+v err=%v", metadata, client.headObjectInput, err)
	}

	if err := store.Delete(context.Background(), "smoke/object.txt"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if aws.ToString(client.deleteInput.Key) != "smoke/object.txt" {
		t.Fatalf("unexpected delete input: %+v", client.deleteInput)
	}
}

func TestB2StorePresignsExactUploadAndVersionedDownload(t *testing.T) {
	t.Parallel()

	client := &fakeS3Client{}
	presigner := &fakeS3Presigner{
		putOutput: &v4.PresignedHTTPRequest{
			URL:          "https://s3.us-east-005.backblazeb2.com/bucket/key?put=signature",
			Method:       http.MethodPut,
			SignedHeader: http.Header{"Content-Length": []string{"42"}},
		},
		getOutput: &v4.PresignedHTTPRequest{
			URL:          "https://s3.us-east-005.backblazeb2.com/bucket/key?get=signature",
			Method:       http.MethodGet,
			SignedHeader: http.Header{},
		},
	}
	store, err := newB2StoreWithPresigner(client, presigner, "tutorhub-staging")
	if err != nil {
		t.Fatalf("create B2 store: %v", err)
	}
	upload, err := store.PresignUpload(context.Background(), UploadPresignInput{
		Key: "tenants/tenant/files/file/original", ContentLength: 42,
		ContentType: "application/pdf", Expires: 2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	if upload.Method != http.MethodPut || upload.SignedHeader.Get("Content-Length") != "42" {
		t.Fatalf("unexpected upload capability: %+v", upload)
	}
	if aws.ToString(presigner.putInput.Bucket) != "tutorhub-staging" ||
		aws.ToString(presigner.putInput.Key) != "tenants/tenant/files/file/original" ||
		aws.ToInt64(presigner.putInput.ContentLength) != 42 ||
		aws.ToString(presigner.putInput.ContentType) != "application/pdf" ||
		presigner.putExpires != 2*time.Minute {
		t.Fatalf("unexpected presign upload input: %+v", presigner.putInput)
	}

	download, err := store.PresignDownload(context.Background(), DownloadPresignInput{
		Key: "tenants/tenant/files/file/original", VersionID: "version-1",
		ContentType: "application/pdf", ContentDisposition: `attachment; filename="file.pdf"`,
		Expires: time.Minute,
	})
	if err != nil {
		t.Fatalf("presign download: %v", err)
	}
	if download.Method != http.MethodGet || aws.ToString(presigner.getInput.VersionId) != "version-1" ||
		aws.ToString(presigner.getInput.ResponseContentType) != "application/pdf" ||
		aws.ToString(presigner.getInput.ResponseContentDisposition) != `attachment; filename="file.pdf"` ||
		presigner.getExpires != time.Minute {
		t.Fatalf("unexpected presign download input: %+v", presigner.getInput)
	}
}

func TestB2StoreUploadCapabilitySignsLengthAndTypeWithoutFalseChecksumClaim(t *testing.T) {
	t.Parallel()

	store, err := NewB2(context.Background(), config.ObjectStorageConfig{
		Enabled: true, Endpoint: "https://s3.us-east-005.backblazeb2.com",
		Region: "us-east-005", Bucket: "tutorhub-test",
		KeyID: "test-key-id", ApplicationKey: "test-application-key",
	})
	if err != nil {
		t.Fatalf("create B2 store: %v", err)
	}
	capability, err := store.PresignUpload(context.Background(), UploadPresignInput{
		Key: "smoke/version-bound-upload", ContentLength: 42,
		ContentType: "application/pdf", Expires: time.Minute,
	})
	if err != nil {
		t.Fatalf("presign upload: %v", err)
	}
	if capability.SignedHeader.Get("Content-Length") != "42" ||
		capability.SignedHeader.Get("Content-Type") != "application/pdf" {
		t.Fatalf("upload constraints are not signed: %v", capability.SignedHeader)
	}
	if capability.SignedHeader.Get("X-Amz-Checksum-Sha256") != "" ||
		capability.SignedHeader.Get("X-Amz-Content-Sha256") != "" {
		t.Fatalf("upload must not claim an unenforced checksum: %v", capability.SignedHeader)
	}
}

func TestB2StoreMultipartLifecycleKeepsProviderUploadIDServerSide(t *testing.T) {
	t.Parallel()
	client := &fakeS3Client{
		createMultipartOutput: &s3.CreateMultipartUploadOutput{UploadId: aws.String("provider-upload-id")},
		completeMultipartOutput: &s3.CompleteMultipartUploadOutput{
			ETag: aws.String("multipart-etag"), VersionId: aws.String("version-2"),
		},
	}
	presigner := &fakeS3Presigner{partOutput: &v4.PresignedHTTPRequest{
		URL:    "https://s3.us-east-005.backblazeb2.com/bucket/key?part=signature",
		Method: http.MethodPut, SignedHeader: http.Header{"Content-Length": []string{"5000000"}},
	}}
	store, err := newB2StoreWithPresigner(client, presigner, "tutorhub-staging")
	if err != nil {
		t.Fatalf("create B2 store: %v", err)
	}
	key := "tenants/tenant/files/file/original"
	uploadID, err := store.CreateMultipart(context.Background(), MultipartCreateInput{
		Key: key, ContentType: "application/pdf",
	})
	if err != nil || uploadID != "provider-upload-id" {
		t.Fatalf("create multipart: id=%q err=%v", uploadID, err)
	}
	capability, err := store.PresignMultipartPart(context.Background(), MultipartPartPresignInput{
		Key: key, UploadID: uploadID, PartNumber: 1, ContentLength: 5_000_000,
		Expires: 2 * time.Minute,
	})
	if err != nil || capability.Method != http.MethodPut ||
		aws.ToString(presigner.partInput.UploadId) != uploadID ||
		aws.ToInt32(presigner.partInput.PartNumber) != 1 ||
		aws.ToInt64(presigner.partInput.ContentLength) != 5_000_000 {
		t.Fatalf("presign multipart part: capability=%+v input=%+v err=%v", capability, presigner.partInput, err)
	}
	completed, err := store.CompleteMultipart(context.Background(), MultipartCompleteInput{
		Key: key, UploadID: uploadID,
		Parts: []CompletedPart{{PartNumber: 1, ETag: "part-etag"}},
	})
	if err != nil || completed.VersionID != "version-2" || completed.ETag != "multipart-etag" {
		t.Fatalf("complete multipart: result=%+v err=%v", completed, err)
	}
	if len(client.completeMultipartInput.MultipartUpload.Parts) != 1 ||
		aws.ToString(client.completeMultipartInput.MultipartUpload.Parts[0].ETag) != "part-etag" {
		t.Fatalf("unexpected completion manifest: %+v", client.completeMultipartInput)
	}
	if err := store.AbortMultipart(context.Background(), MultipartAbortInput{
		Key: key, UploadID: uploadID,
	}); err != nil || aws.ToString(client.abortMultipartInput.UploadId) != uploadID {
		t.Fatalf("abort multipart: input=%+v err=%v", client.abortMultipartInput, err)
	}
}

func TestB2StoreRejectsUnsafePresignConstraints(t *testing.T) {
	t.Parallel()

	store, err := newB2StoreWithPresigner(&fakeS3Client{}, &fakeS3Presigner{}, "bucket")
	if err != nil {
		t.Fatalf("create B2 store: %v", err)
	}
	for _, input := range []UploadPresignInput{
		{Key: "/absolute", ContentLength: 1, ContentType: "text/plain", Expires: time.Minute},
		{Key: "safe/key", ContentLength: 0, ContentType: "text/plain", Expires: time.Minute},
		{Key: "safe/key", ContentLength: 1, ContentType: "", Expires: time.Minute},
		{Key: "safe/key", ContentLength: 1, ContentType: "text/plain", Expires: 16 * time.Minute},
	} {
		if _, err := store.PresignUpload(context.Background(), input); err == nil {
			t.Fatalf("expected upload constraints to fail: %+v", input)
		}
	}
	if _, err := store.PresignDownload(context.Background(), DownloadPresignInput{
		Key: "safe/key", VersionID: "version", ContentType: "text/plain",
		ContentDisposition: "attachment\r\nInjected: true", Expires: time.Minute,
	}); err == nil {
		t.Fatal("expected unsafe content disposition to fail")
	}
}

func TestB2StoreRejectsUnsafeKeys(t *testing.T) {
	t.Parallel()

	store, err := newB2Store(&fakeS3Client{}, "tutorhub-staging")
	if err != nil {
		t.Fatalf("create B2 store: %v", err)
	}

	for _, key := range []string{"", " ", "/absolute", `windows\\path`} {
		if err := store.Put(
			context.Background(),
			key,
			bytes.NewReader(nil),
			0,
			"application/octet-stream",
		); err == nil {
			t.Fatalf("expected key %q to be rejected", key)
		}
	}
}

func TestObjectStorageReadinessChecksBucket(t *testing.T) {
	t.Parallel()

	client := &fakeS3Client{}
	store, err := newB2Store(client, "tutorhub-staging")
	if err != nil {
		t.Fatalf("create B2 store: %v", err)
	}

	check := NewReadinessCheck(store, time.Second)
	if err := check.Check(context.Background()); err != nil {
		t.Fatalf("check object storage readiness: %v", err)
	}
	if aws.ToString(client.headInput.Bucket) != "tutorhub-staging" {
		t.Fatalf("unexpected head bucket input: %+v", client.headInput)
	}

	client.headErr = errors.New("unavailable")
	if err := check.Check(context.Background()); err == nil {
		t.Fatal("expected readiness failure")
	}
}

type fakeS3Client struct {
	putInput                *s3.PutObjectInput
	getOutput               *s3.GetObjectOutput
	deleteInput             *s3.DeleteObjectInput
	headInput               *s3.HeadBucketInput
	headErr                 error
	headObjectInput         *s3.HeadObjectInput
	headObjectOutput        *s3.HeadObjectOutput
	createMultipartInput    *s3.CreateMultipartUploadInput
	createMultipartOutput   *s3.CreateMultipartUploadOutput
	completeMultipartInput  *s3.CompleteMultipartUploadInput
	completeMultipartOutput *s3.CompleteMultipartUploadOutput
	abortMultipartInput     *s3.AbortMultipartUploadInput
}

type fakeS3Presigner struct {
	putInput    *s3.PutObjectInput
	putOutput   *v4.PresignedHTTPRequest
	putErr      error
	putExpires  time.Duration
	getInput    *s3.GetObjectInput
	getOutput   *v4.PresignedHTTPRequest
	getErr      error
	getExpires  time.Duration
	partInput   *s3.UploadPartInput
	partOutput  *v4.PresignedHTTPRequest
	partErr     error
	partExpires time.Duration
}

func (presigner *fakeS3Presigner) PresignPutObject(
	_ context.Context,
	input *s3.PutObjectInput,
	options ...func(*s3.PresignOptions),
) (*v4.PresignedHTTPRequest, error) {
	presigner.putInput = input
	var resolved s3.PresignOptions
	for _, option := range options {
		option(&resolved)
	}
	presigner.putExpires = resolved.Expires
	return presigner.putOutput, presigner.putErr
}

func (presigner *fakeS3Presigner) PresignGetObject(
	_ context.Context,
	input *s3.GetObjectInput,
	options ...func(*s3.PresignOptions),
) (*v4.PresignedHTTPRequest, error) {
	presigner.getInput = input
	var resolved s3.PresignOptions
	for _, option := range options {
		option(&resolved)
	}
	presigner.getExpires = resolved.Expires
	return presigner.getOutput, presigner.getErr
}

func (presigner *fakeS3Presigner) PresignUploadPart(
	_ context.Context,
	input *s3.UploadPartInput,
	options ...func(*s3.PresignOptions),
) (*v4.PresignedHTTPRequest, error) {
	presigner.partInput = input
	var resolved s3.PresignOptions
	for _, option := range options {
		option(&resolved)
	}
	presigner.partExpires = resolved.Expires
	return presigner.partOutput, presigner.partErr
}

func (client *fakeS3Client) HeadObject(
	_ context.Context,
	input *s3.HeadObjectInput,
	_ ...func(*s3.Options),
) (*s3.HeadObjectOutput, error) {
	client.headObjectInput = input
	if client.headObjectOutput == nil {
		return &s3.HeadObjectOutput{}, nil
	}
	return client.headObjectOutput, nil
}

func (client *fakeS3Client) PutObject(
	_ context.Context,
	input *s3.PutObjectInput,
	_ ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	client.putInput = input
	return &s3.PutObjectOutput{}, nil
}

func (client *fakeS3Client) GetObject(
	_ context.Context,
	_ *s3.GetObjectInput,
	_ ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	if client.getOutput == nil {
		return &s3.GetObjectOutput{}, nil
	}
	return client.getOutput, nil
}

func (client *fakeS3Client) DeleteObject(
	_ context.Context,
	input *s3.DeleteObjectInput,
	_ ...func(*s3.Options),
) (*s3.DeleteObjectOutput, error) {
	client.deleteInput = input
	return &s3.DeleteObjectOutput{}, nil
}

func (client *fakeS3Client) HeadBucket(
	_ context.Context,
	input *s3.HeadBucketInput,
	_ ...func(*s3.Options),
) (*s3.HeadBucketOutput, error) {
	client.headInput = input
	return &s3.HeadBucketOutput{}, client.headErr
}

func (client *fakeS3Client) CreateMultipartUpload(
	_ context.Context,
	input *s3.CreateMultipartUploadInput,
	_ ...func(*s3.Options),
) (*s3.CreateMultipartUploadOutput, error) {
	client.createMultipartInput = input
	if client.createMultipartOutput == nil {
		return &s3.CreateMultipartUploadOutput{}, nil
	}
	return client.createMultipartOutput, nil
}

func (client *fakeS3Client) CompleteMultipartUpload(
	_ context.Context,
	input *s3.CompleteMultipartUploadInput,
	_ ...func(*s3.Options),
) (*s3.CompleteMultipartUploadOutput, error) {
	client.completeMultipartInput = input
	if client.completeMultipartOutput == nil {
		return &s3.CompleteMultipartUploadOutput{}, nil
	}
	return client.completeMultipartOutput, nil
}

func (client *fakeS3Client) AbortMultipartUpload(
	_ context.Context,
	input *s3.AbortMultipartUploadInput,
	_ ...func(*s3.Options),
) (*s3.AbortMultipartUploadOutput, error) {
	client.abortMultipartInput = input
	return &s3.AbortMultipartUploadOutput{}, nil
}
