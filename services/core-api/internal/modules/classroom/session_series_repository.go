package classroom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var (
	ErrSeriesNotFound            = errors.New("class session series not found")
	ErrSeriesVersionConflict     = errors.New("class session series version is stale")
	ErrSeriesIdempotencyConflict = errors.New("class session mutation key was reused")
	ErrSeriesExceptionConflict   = errors.New("class session exceptions require an explicit safe policy")
	ErrSeriesLimitExceeded       = errors.New("class session series limit exceeded")
	ErrConflictOverrideDenied    = errors.New("class schedule conflict override denied")
)

type SessionSeriesRepository interface {
	CreateSeries(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		CreateSeriesParams,
		time.Time,
	) (ClassSessionSeries, error)
	GetSeries(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
	) (ClassSessionSeries, error)
	PreviewSeriesMutation(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		SeriesMutationParams,
	) (SeriesScopePreview, error)
	UpdateSeriesOccurrence(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		SeriesMutationParams,
		time.Time,
	) (SeriesMutationResult, error)
	CancelSeriesOccurrence(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		SeriesMutationParams,
		time.Time,
	) (SeriesMutationResult, error)
}
