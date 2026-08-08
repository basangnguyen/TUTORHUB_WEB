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
