package classroom

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type SessionSeriesServiceAPI interface {
	CreateSeries(
		context.Context,
		AccessContext,
		uuid.UUID,
		CreateSeriesInput,
	) (ClassSessionSeries, error)
	GetSeries(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
	) (ClassSessionSeries, error)
	PreviewSeriesMutation(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		OccurrenceMutationInput,
	) (SeriesScopePreview, error)
	UpdateSeriesOccurrence(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		OccurrenceMutationInput,
	) (SeriesMutationResult, error)
	CancelSeriesOccurrence(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		OccurrenceMutationInput,
	) (SeriesMutationResult, error)
}

type SessionSeriesService struct {
	repository      SessionSeriesRepository
	classAuthorizer ClassActionAuthorizer
	clock           func() time.Time
}

func NewSessionSeriesService(
	repository SessionSeriesRepository,
	classAuthorizer ClassActionAuthorizer,
	clock func() time.Time,
) (*SessionSeriesService, error) {
	if repository == nil || classAuthorizer == nil {
		return nil, fmt.Errorf("series repository and class authorizer are required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &SessionSeriesService{
		repository: repository, classAuthorizer: classAuthorizer, clock: clock,
	}, nil
}

func (service *SessionSeriesService) CreateSeries(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input CreateSeriesInput,
) (ClassSessionSeries, error) {
	class, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionSessionSchedule)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	params, _, err := normalizeCreateSeriesInput(ctx, input, access.ActorID)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	series, err := service.repository.CreateSeries(
		ctx, tenantContext, classID, params, service.clock().UTC(),
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	return projectSeriesViewerAccess(series, class), nil
}

func (service *SessionSeriesService) GetSeries(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	seriesID uuid.UUID,
) (ClassSessionSeries, error) {
	class, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionClassView)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if seriesID == uuid.Nil {
		return ClassSessionSeries{}, ErrSeriesNotFound
	}
	series, err := service.repository.GetSeries(ctx, tenantContext, classID, seriesID)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	return projectSeriesViewerAccess(series, class), nil
}

func (service *SessionSeriesService) PreviewSeriesMutation(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	seriesID uuid.UUID,
	input OccurrenceMutationInput,
) (SeriesScopePreview, error) {
	_, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionSessionSchedule)
	if err != nil {
		return SeriesScopePreview{}, err
	}
	// The preview endpoint is shared by the update and cancellation dialogs.
	// An empty mutation payload is therefore the explicit cancellation-preview
	// shape; it still validates scope/version and computes the affected range,
	// while the mutation endpoint remains responsible for applying the cancel.
	operation := "update"
	if input.Title == nil &&
		input.Description == nil &&
		input.StartsAt == nil &&
		input.EndsAt == nil &&
		input.Timezone == nil &&
		input.Rule == nil &&
		input.OverlapPolicy == nil {
		operation = "cancel"
	}
	params, err := normalizeSeriesMutationInput(input, access.ActorID, operation)
	if err != nil {
		return SeriesScopePreview{}, err
	}
	return service.repository.PreviewSeriesMutation(
		ctx, tenantContext, classID, seriesID, params,
	)
}

func (service *SessionSeriesService) UpdateSeriesOccurrence(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	seriesID uuid.UUID,
	input OccurrenceMutationInput,
) (SeriesMutationResult, error) {
	class, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionSessionSchedule)
	if err != nil {
		return SeriesMutationResult{}, err
	}
	params, err := normalizeSeriesMutationInput(input, access.ActorID, "update")
	if err != nil {
		return SeriesMutationResult{}, err
	}
	result, err := service.repository.UpdateSeriesOccurrence(
		ctx, tenantContext, classID, seriesID, params, service.clock().UTC(),
	)
	if err != nil {
		return SeriesMutationResult{}, err
	}
	result.Series = projectSeriesViewerAccess(result.Series, class)
	return result, nil
}

func (service *SessionSeriesService) CancelSeriesOccurrence(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	seriesID uuid.UUID,
	input OccurrenceMutationInput,
) (SeriesMutationResult, error) {
	class, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionSessionSchedule)
	if err != nil {
		return SeriesMutationResult{}, err
	}
	params, err := normalizeSeriesMutationInput(input, access.ActorID, "cancel")
	if err != nil {
		return SeriesMutationResult{}, err
	}
	result, err := service.repository.CancelSeriesOccurrence(
		ctx, tenantContext, classID, seriesID, params, service.clock().UTC(),
	)
	if err != nil {
		return SeriesMutationResult{}, err
	}
	result.Series = projectSeriesViewerAccess(result.Series, class)
	return result, nil
}

func (service *SessionSeriesService) authorize(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) (Class, tenancy.Context, error) {
	if classID == uuid.Nil {
		return Class{}, tenancy.Context{}, ErrClassNotFound
	}
	class, err := service.classAuthorizer.AuthorizeClass(ctx, access, classID, action)
	if err != nil {
		return Class{}, tenancy.Context{}, err
	}
	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return Class{}, tenancy.Context{}, ErrSessionAccessDenied
	}
	return class, tenantContext, nil
}

func projectSeriesViewerAccess(series ClassSessionSeries, class Class) ClassSessionSeries {
	canMutate := class.ViewerAccess.CanScheduleSessions &&
		series.Status == SeriesStatusScheduled
	series.ViewerAccess = SessionViewerAccess{
		CanUpdate: canMutate,
		CanCancel: canMutate,
	}
	return series
}
