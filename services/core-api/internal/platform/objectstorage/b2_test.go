package objectstorage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

	if err := store.Delete(context.Background(), "smoke/object.txt"); err != nil {
		t.Fatalf("delete object: %v", err)
	}
	if aws.ToString(client.deleteInput.Key) != "smoke/object.txt" {
		t.Fatalf("unexpected delete input: %+v", client.deleteInput)
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
	putInput    *s3.PutObjectInput
	getOutput   *s3.GetObjectOutput
	deleteInput *s3.DeleteObjectInput
	headInput   *s3.HeadBucketInput
	headErr     error
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
