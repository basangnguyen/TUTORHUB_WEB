package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/platform/objectstorage"
)

const (
	smokeTimeout           = 45 * time.Second
	cleanupTimeout         = 15 * time.Second
	maxSmokeBytes          = 1 << 20
	multipartPartBytes     = 5_000_000
	maxMultipartSmokeBytes = 6 << 20
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	os.Exit(run(ctx, os.Stdout, os.Stderr))
}

func run(ctx context.Context, stdout io.Writer, stderr io.Writer) int {
	cfg, err := config.LoadObjectStorage()
	if err != nil {
		fmt.Fprintf(stderr, "B2 smoke configuration failed: %v\n", err)
		return 1
	}
	if !cfg.Enabled {
		fmt.Fprintln(stderr, "B2 smoke configuration failed: object storage is disabled")
		return 1
	}

	setupContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	store, err := objectstorage.NewB2(setupContext, cfg)
	cancel()
	if err != nil {
		fmt.Fprintf(stderr, "B2 smoke client setup failed: %v\n", err)
		return 1
	}

	payload := []byte("TutorHub B2 staging smoke " + uuid.NewString())
	key := "smoke/p3-09-" + uuid.NewString() + ".txt"
	smokeContext, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()

	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("presigned transfer redirect denied")
		},
	}
	if err := smoke(smokeContext, client, store, key, payload); err != nil {
		fmt.Fprintf(stderr, "B2 smoke failed: %v\n", err)
		return 1
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("B2_SMOKE_MULTIPART")), "true") {
		origin := strings.TrimSpace(os.Getenv("B2_SMOKE_ORIGIN"))
		if origin == "" {
			fmt.Fprintln(stderr, "B2 multipart smoke configuration failed: B2_SMOKE_ORIGIN is required")
			return 1
		}
		if err := multipartSmoke(smokeContext, client, store, origin); err != nil {
			fmt.Fprintf(stderr, "B2 multipart smoke failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "B2 P3-09 multipart smoke passed: part PUT/CORS, complete, exact-version GET, abort, and cleanup succeeded.")
	}

	fmt.Fprintln(stdout, "B2 P3-09 smoke passed: exact presigned PUT, metadata proof, versioned GET, and cleanup succeeded.")
	return 0
}

type transferSmokeStore interface {
	objectstorage.MetadataReader
	objectstorage.TransferPresigner
	Delete(context.Context, string) error
	DeleteVersion(context.Context, string, string) error
	objectstorage.MultipartTransfer
}

func smoke(
	ctx context.Context,
	client *http.Client,
	store transferSmokeStore,
	key string,
	payload []byte,
) (resultErr error) {
	uploaded := false
	versionID := ""
	defer func() {
		if !uploaded {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		var err error
		if versionID != "" {
			err = store.DeleteVersion(cleanupContext, key, versionID)
		} else {
			err = store.Delete(cleanupContext, key)
		}
		if err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("cleanup smoke object: %w", err))
		}
	}()

	if client == nil || len(payload) == 0 {
		return fmt.Errorf("presigned smoke client and payload are required")
	}
	upload, err := store.PresignUpload(ctx, objectstorage.UploadPresignInput{
		Key: key, ContentLength: int64(len(payload)), ContentType: "text/plain",
		Expires: 2 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("create exact upload capability")
	}
	uploadRequest, err := http.NewRequestWithContext(
		ctx, upload.Method, upload.URL, bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("create presigned upload request")
	}
	applySignedHeaders(uploadRequest, upload.SignedHeader)
	uploadResponse, err := client.Do(uploadRequest)
	if err != nil {
		return fmt.Errorf("execute presigned upload request")
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(uploadResponse.Body, maxSmokeBytes+1))
	closeErr := uploadResponse.Body.Close()
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("consume presigned upload response")
	}
	if uploadResponse.StatusCode < 200 || uploadResponse.StatusCode >= 300 {
		return fmt.Errorf("presigned upload returned HTTP %d", uploadResponse.StatusCode)
	}
	uploaded = true
	versionID = uploadResponse.Header.Get("x-amz-version-id")

	if versionID == "" {
		return fmt.Errorf("presigned upload response omitted object version")
	}
	metadata, err := store.HeadVersion(ctx, key, versionID)
	if err != nil {
		return fmt.Errorf("read uploaded object proof")
	}
	proofFailures := make([]string, 0, 5)
	if metadata.ContentLength != int64(len(payload)) {
		proofFailures = append(proofFailures, "size")
	}
	if metadata.ContentType != "text/plain" {
		proofFailures = append(proofFailures, "type")
	}
	if metadata.ETag == "" {
		proofFailures = append(proofFailures, "etag")
	}
	if metadata.VersionID == "" {
		proofFailures = append(proofFailures, "version")
	}
	if len(proofFailures) != 0 {
		return fmt.Errorf("uploaded object proof failed fields: %s", strings.Join(proofFailures, ","))
	}
	if metadata.VersionID != versionID {
		proofFailures = append(proofFailures, "version-selector")
	}

	download, err := store.PresignDownload(ctx, objectstorage.DownloadPresignInput{
		Key: key, VersionID: versionID, ContentType: "text/plain",
		ContentDisposition: `attachment; filename="tutorhub-p3-09-smoke.txt"`,
		Expires:            time.Minute,
	})
	if err != nil {
		return fmt.Errorf("create versioned download capability")
	}
	downloadRequest, err := http.NewRequestWithContext(ctx, download.Method, download.URL, nil)
	if err != nil {
		return fmt.Errorf("create presigned download request")
	}
	applySignedHeaders(downloadRequest, download.SignedHeader)
	downloadResponse, err := client.Do(downloadRequest)
	if err != nil {
		return fmt.Errorf("execute presigned download request")
	}
	downloaded, readErr := io.ReadAll(io.LimitReader(downloadResponse.Body, maxSmokeBytes+1))
	closeErr = downloadResponse.Body.Close()
	if readErr != nil || closeErr != nil {
		return fmt.Errorf("consume presigned download response")
	}
	if downloadResponse.StatusCode < 200 || downloadResponse.StatusCode >= 300 {
		return fmt.Errorf("presigned download returned HTTP %d", downloadResponse.StatusCode)
	}
	if len(downloaded) > maxSmokeBytes {
		return fmt.Errorf("downloaded smoke object exceeds size limit")
	}
	if !bytes.Equal(downloaded, payload) {
		return fmt.Errorf("downloaded smoke object does not match uploaded payload")
	}

	if err := store.DeleteVersion(ctx, key, versionID); err != nil {
		return fmt.Errorf("delete smoke object version")
	}
	uploaded = false

	return nil
}

func applySignedHeaders(request *http.Request, headers http.Header) {
	for name, values := range headers {
		if strings.EqualFold(name, "Host") || strings.EqualFold(name, "Content-Length") {
			continue
		}
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}
}

func multipartSmoke(
	ctx context.Context,
	client *http.Client,
	store transferSmokeStore,
	origin string,
) (resultErr error) {
	key := "smoke/p3-09-multipart-" + uuid.NewString() + ".bin"
	first := bytes.Repeat([]byte{0x54}, multipartPartBytes)
	second := []byte("TutorHub multipart final part")
	payload := append(append(make([]byte, 0, len(first)+len(second)), first...), second...)
	uploadID, err := store.CreateMultipart(ctx, objectstorage.MultipartCreateInput{
		Key: key, ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("initiate provider multipart upload")
	}
	completed := false
	versionID := ""
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if !completed {
			if err := store.AbortMultipart(cleanupContext, objectstorage.MultipartAbortInput{
				Key: key, UploadID: uploadID,
			}); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("abort incomplete multipart smoke"))
			}
			return
		}
		if versionID != "" {
			if err := store.DeleteVersion(cleanupContext, key, versionID); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("cleanup multipart smoke version"))
			}
		}
	}()

	parts := [][]byte{first, second}
	completedParts := make([]objectstorage.CompletedPart, len(parts))
	for index, part := range parts {
		partNumber := int32(index + 1)
		capability, err := store.PresignMultipartPart(ctx, objectstorage.MultipartPartPresignInput{
			Key: key, UploadID: uploadID, PartNumber: partNumber,
			ContentLength: int64(len(part)), Expires: 2 * time.Minute,
		})
		if err != nil {
			return fmt.Errorf("create multipart part capability")
		}
		request, err := http.NewRequestWithContext(
			ctx, capability.Method, capability.URL, bytes.NewReader(part),
		)
		if err != nil {
			return fmt.Errorf("create multipart part request")
		}
		applySignedHeaders(request, capability.SignedHeader)
		request.Header.Set("Origin", origin)
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("execute multipart part request")
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, maxSmokeBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return fmt.Errorf("consume multipart part response")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("multipart part returned HTTP %d", response.StatusCode)
		}
		if response.Header.Get("Access-Control-Allow-Origin") != origin {
			return fmt.Errorf("multipart part CORS did not allow the TutorHub origin")
		}
		if !headerTokenContains(response.Header.Values("Access-Control-Expose-Headers"), "ETag") {
			return fmt.Errorf("multipart part CORS did not expose ETag")
		}
		etag := strings.TrimSpace(response.Header.Get("ETag"))
		if etag == "" {
			return fmt.Errorf("multipart part response omitted ETag")
		}
		completedParts[index] = objectstorage.CompletedPart{PartNumber: partNumber, ETag: etag}
	}
	proof, err := store.CompleteMultipart(ctx, objectstorage.MultipartCompleteInput{
		Key: key, UploadID: uploadID, Parts: completedParts,
	})
	if err != nil {
		return fmt.Errorf("complete provider multipart upload")
	}
	completed = true
	versionID = proof.VersionID
	metadata, err := store.HeadVersion(ctx, key, versionID)
	if err != nil {
		return fmt.Errorf("read completed multipart proof")
	}
	if metadata.ContentLength != int64(len(payload)) ||
		metadata.ContentType != "application/octet-stream" ||
		metadata.ETag == "" || metadata.VersionID != versionID {
		return fmt.Errorf("completed multipart proof mismatch")
	}
	download, err := store.PresignDownload(ctx, objectstorage.DownloadPresignInput{
		Key: key, VersionID: versionID, ContentType: "application/octet-stream",
		ContentDisposition: `attachment; filename="tutorhub-p3-09-multipart.bin"`,
		Expires:            time.Minute,
	})
	if err != nil {
		return fmt.Errorf("create multipart version download capability")
	}
	request, err := http.NewRequestWithContext(ctx, download.Method, download.URL, nil)
	if err != nil {
		return fmt.Errorf("create multipart version download request")
	}
	applySignedHeaders(request, download.SignedHeader)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("execute multipart version download")
	}
	downloaded, readErr := io.ReadAll(io.LimitReader(response.Body, maxMultipartSmokeBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("consume multipart version download")
	}
	if len(downloaded) > maxMultipartSmokeBytes || !bytes.Equal(downloaded, payload) {
		return fmt.Errorf("multipart downloaded bytes mismatch")
	}

	abortKey := "smoke/p3-09-abort-" + uuid.NewString() + ".bin"
	abortID, err := store.CreateMultipart(ctx, objectstorage.MultipartCreateInput{
		Key: abortKey, ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("initiate abort multipart smoke")
	}
	if err := store.AbortMultipart(ctx, objectstorage.MultipartAbortInput{
		Key: abortKey, UploadID: abortID,
	}); err != nil {
		return fmt.Errorf("abort provider multipart upload")
	}
	return nil
}

func headerTokenContains(values []string, expected string) bool {
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), expected) {
				return true
			}
		}
	}
	return false
}
