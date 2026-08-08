package objectstorage

import (
	"context"
	"io"
	"net/http"
	"time"
)

type Metadata struct {
	ContentLength  int64
	ContentType    string
	ChecksumSHA256 []byte
	ETag           string
	VersionID      string
}

type MetadataReader interface {
	Head(context.Context, string) (Metadata, error)
	HeadVersion(context.Context, string, string) (Metadata, error)
}

type UploadPresignInput struct {
	Key           string
	ContentLength int64
	ContentType   string
	Expires       time.Duration
}

type DownloadPresignInput struct {
	Key                string
	VersionID          string
	ContentType        string
	ContentDisposition string
	Expires            time.Duration
}

type PresignedRequest struct {
	URL          string
	Method       string
	SignedHeader http.Header
}

type TransferPresigner interface {
	PresignUpload(context.Context, UploadPresignInput) (PresignedRequest, error)
	PresignDownload(context.Context, DownloadPresignInput) (PresignedRequest, error)
}

type MultipartCreateInput struct {
	Key         string
	ContentType string
}

type MultipartPartPresignInput struct {
	Key           string
	UploadID      string
	PartNumber    int32
	ContentLength int64
	Expires       time.Duration
}

type CompletedPart struct {
	PartNumber int32
	ETag       string
}

type MultipartCompleteInput struct {
	Key      string
	UploadID string
	Parts    []CompletedPart
}

type MultipartCompleteResult struct {
	ETag      string
	VersionID string
}

type MultipartAbortInput struct {
	Key      string
	UploadID string
}

type MultipartTransfer interface {
	CreateMultipart(context.Context, MultipartCreateInput) (string, error)
	PresignMultipartPart(context.Context, MultipartPartPresignInput) (PresignedRequest, error)
	CompleteMultipart(context.Context, MultipartCompleteInput) (MultipartCompleteResult, error)
	AbortMultipart(context.Context, MultipartAbortInput) error
}

type Object struct {
	Body          io.ReadCloser
	ContentLength int64
	ContentType   string
	ETag          string
}

type Store interface {
	Put(
		context.Context,
		string,
		io.Reader,
		int64,
		string,
	) error
	Get(context.Context, string) (Object, error)
	Delete(context.Context, string) error
}
