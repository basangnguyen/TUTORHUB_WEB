package objectstorage

import (
	"context"
	"io"
)

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
