package classroom

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type persistedSeriesException struct {
	OccurrenceKey       string
	Type                recurrence.ExceptionType
	OriginalLocal       string
	OriginalTimezone    string
	OriginalOffset      int
	OverrideLocalStart  *string
	OverrideTimezone    *string
	OverrideDuration    *int
	OverrideTitle       *string
	OverrideDescription *string
	Reason              string
	Version             int64
}

func (repository *PostgresRepository) CreateSeries(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params CreateSeriesParams,
	createdAt time.Time,
) (ClassSessionSeries, error) {
	if err := tenantContext.Validate(); err != nil || params.CreatedBy != tenantContext.ActorID {
		return ClassSessionSeries{}, ErrSessionAccessDenied
	}
	if classID == uuid.Nil {
		return ClassSessionSeries{}, ErrClassNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ClassSessionSeries{}, fmt.Errorf("begin recurring session creation: %w", err)
	}
	defer rollbackClassTransaction(transaction)
	if err := repository.requireSessionRecurrenceFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	locked, membership, err := repository.lockClassMutation(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext, membership, locked.Class, policy.ActionSessionSchedule,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	var seriesCount int
	if err := transaction.QueryRow(
		queryContext,
		`SELECT count(*) FROM tutorhub.class_session_series
WHERE tenant_id = $1 AND class_id = $2 AND status = 'scheduled'`,
		tenantContext.TenantID,
		classID,
	).Scan(&seriesCount); err != nil {
		return ClassSessionSeries{}, fmt.Errorf("count recurring session series: %w", err)
	}
	if seriesCount >= maximumSeriesPerClass {
		return ClassSessionSeries{}, ErrSeriesLimitExceeded
	}
	occurrences, err := expandCompleteSeries(queryContext, seriesDefinition(params))
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if conflict, conflictErr := repository.findClassScheduleConflict(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		uuid.Nil,
		occurrences,
	); conflictErr != nil {
		return ClassSessionSeries{}, conflictErr
	} else if conflict != nil {
		return ClassSessionSeries{}, ErrSessionScheduleConflict
	}
	series, err := insertClassSessionSeries(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		params,
		nil,
		createdAt,
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if err := insertClassSessionSeriesEvent(
		queryContext,
		transaction,
		series,
		tenantContext.ActorID,
		"class_session_series.scheduled.v1",
		"",
		createdAt,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ClassSessionSeries{}, fmt.Errorf("commit recurring session creation: %w", err)
	}
	return series, nil
}

func (repository *PostgresRepository) GetSeries(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	seriesID uuid.UUID,
) (ClassSessionSeries, error) {
	if err := tenantContext.Validate(); err != nil ||
		classID == uuid.Nil || seriesID == uuid.Nil {
		return ClassSessionSeries{}, ErrSeriesNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	series, err := scanClassSessionSeries(repository.database.QueryRow(
		queryContext,
		selectClassSessionSeriesSQL+`
WHERE series.tenant_id = $1 AND series.class_id = $2 AND series.id = $3`,
		tenantContext.TenantID,
		classID,
		seriesID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSessionSeries{}, ErrSeriesNotFound
	}
	if err != nil {
		return ClassSessionSeries{}, fmt.Errorf("get recurring session series: %w", err)
	}
	return series, nil
}

func (repository *PostgresRepository) PreviewSeriesMutation(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	seriesID uuid.UUID,
	params SeriesMutationParams,
) (SeriesScopePreview, error) {
	if err := tenantContext.Validate(); err != nil {
		return SeriesScopePreview{}, ErrSessionAccessDenied
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SeriesScopePreview{}, fmt.Errorf("begin recurring mutation preview: %w", err)
	}
	defer rollbackClassTransaction(transaction)
	if err := repository.requireSessionRecurrenceFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return SeriesScopePreview{}, err
	}
	locked, membership, err := repository.lockClassMutation(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return SeriesScopePreview{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext, membership, locked.Class, policy.ActionSessionSchedule,
	); err != nil {
		return SeriesScopePreview{}, err
	}
	series, err := lockClassSessionSeries(
		queryContext, transaction, tenantContext.TenantID, classID, seriesID,
	)
	if err != nil {
		return SeriesScopePreview{}, err
	}
	if series.Version != params.ExpectedVersion || series.Status != SeriesStatusScheduled {
		return SeriesScopePreview{}, ErrSeriesVersionConflict
	}
	occurrences, err := expandCompleteSeries(queryContext, definitionFromSeries(series))
	if err != nil {
		return SeriesScopePreview{}, err
	}
	exceptions, err := listSeriesExceptions(
		queryContext, transaction, tenantContext.TenantID, classID, seriesID,
	)
	if err != nil {
		return SeriesScopePreview{}, err
	}
	preview, err := recurrence.PreviewScope(
		occurrences,
		exceptionSummaries(exceptions),
		params.OccurrenceKey,
		params.Scope,
		params.FollowingExceptionPolicy,
	)
	if err != nil {
		return SeriesScopePreview{}, ErrInvalidSessionInput
	}
	boundaryIndex := occurrenceIndex(occurrences, params.OccurrenceKey)
	if boundaryIndex < 0 {
		return SeriesScopePreview{}, ErrInvalidSessionInput
	}
	proposed, err := repository.previewMutationOccurrences(
		queryContext, transaction, series, occurrences, boundaryIndex, exceptions, params,
	)
	if err != nil {
		return SeriesScopePreview{}, err
	}
	conflicts := make([]ScheduleConflict, 0)
	if len(proposed) > 0 {
		conflict, conflictErr := repository.findClassScheduleConflict(
			queryContext, transaction, tenantContext.TenantID, classID, seriesID, proposed,
		)
		if conflictErr != nil {
			return SeriesScopePreview{}, conflictErr
		}
		if conflict != nil {
			conflicts = append(conflicts, *conflict)
		}
	}
	return SeriesScopePreview{ScopePreview: preview, Conflicts: conflicts}, nil
}

func (repository *PostgresRepository) previewMutationOccurrences(
	ctx context.Context,
	transaction pgx.Tx,
	series ClassSessionSeries,
	occurrences []recurrence.Occurrence,
	boundaryIndex int,
	exceptions []persistedSeriesException,
	params SeriesMutationParams,
) ([]recurrence.Occurrence, error) {
	switch params.Scope {
	case recurrence.ScopeThisOccurrence:
		if params.StartsAt == nil {
			return nil, nil
		}
		exception := persistedSeriesException{
			OccurrenceKey:    params.OccurrenceKey,
			OriginalLocal:    occurrences[boundaryIndex].OriginalLocal,
			OriginalTimezone: series.Timezone,
		}
		var err error
		exception, err = applyOccurrenceOverride(
			exception, series, occurrences[boundaryIndex], params,
		)
		if err != nil {
			return nil, err
		}
		projected, err := projectOverrideOccurrence(ctx, series, exception)
		if err != nil {
			return nil, err
		}
		return []recurrence.Occurrence{projected}, nil
	case recurrence.ScopeEntireSeries:
		updated, identityChanged, err := applySeriesUpdate(series, params)
		if err != nil {
			return nil, err
		}
		if identityChanged && len(exceptions) > 0 {
			return nil, ErrSeriesExceptionConflict
		}
		proposed, err := expandCompleteSeries(ctx, definitionFromSeries(updated))
		if err != nil {
			return nil, err
		}
		if err := requireNoSelfOverlap(proposed); err != nil {
			return nil, err
		}
		return proposed, nil
	case recurrence.ScopeFollowing:
		child := series
		child.ID = uuid.New()
		child.LocalStart = occurrences[boundaryIndex].OriginalLocal
		child.Version = 1
		child.Sequence = 0
		child.SplitFrom = &series.ID
		if child.Rule.End.Type == recurrence.EndAfterCount {
			child.Rule.End.Count = len(occurrences) - boundaryIndex
		}
		child.NormalizedRule, _ = recurrence.NormalizeRule(child.Rule)
		updated, _, err := applySeriesUpdate(child, params)
		if err != nil {
			return nil, err
		}
		proposed, err := expandCompleteSeries(ctx, definitionFromSeries(updated))
		if err != nil {
			return nil, err
		}
		if err := requireNoSelfOverlap(proposed); err != nil {
			return nil, err
		}
		if params.FollowingExceptionPolicy == recurrence.ExceptionDiscard {
			return proposed, nil
		}
		return applyFutureExceptionsToOccurrences(
			ctx, series, updated, occurrences[boundaryIndex:], proposed,
			exceptions, params.FollowingExceptionPolicy,
		)
	default:
		return nil, ErrInvalidSessionInput
	}
}

func applyFutureExceptionsToOccurrences(
	ctx context.Context,
	parent ClassSessionSeries,
	child ClassSessionSeries,
	parentOccurrences []recurrence.Occurrence,
	childOccurrences []recurrence.Occurrence,
	exceptions []persistedSeriesException,
	policyValue recurrence.FutureExceptionPolicy,
) ([]recurrence.Occurrence, error) {
	parentIndex := make(map[string]int, len(parentOccurrences))
	parentLocal := make(map[string]string, len(parentOccurrences))
	for index, occurrence := range parentOccurrences {
		parentIndex[occurrence.Key] = index
		parentLocal[occurrence.Key] = occurrence.OriginalLocal
	}
	childByLocal := make(map[string]recurrence.Occurrence, len(childOccurrences))
	for _, occurrence := range childOccurrences {
		childByLocal[occurrence.OriginalLocal] = occurrence
	}
	byKey := make(map[string]persistedSeriesException, len(childOccurrences))
	for _, exception := range exceptions {
		index, future := parentIndex[exception.OccurrenceKey]
		if !future {
			continue
		}
		var target recurrence.Occurrence
		var ok bool
		if policyValue == recurrence.ExceptionCarry {
			target, ok = childByLocal[parentLocal[exception.OccurrenceKey]]
		} else if index < len(childOccurrences) {
			target, ok = childOccurrences[index], true
		}
		if !ok {
			return nil, ErrSeriesExceptionConflict
		}
		copied := exception
		copied.OccurrenceKey = target.Key
		copied.OriginalLocal = target.OriginalLocal
		copied.OriginalTimezone = child.Timezone
		byKey[target.Key] = copied
	}
	result := make([]recurrence.Occurrence, 0, len(childOccurrences))
	for _, occurrence := range childOccurrences {
		exception, found := byKey[occurrence.Key]
		if !found {
			result = append(result, occurrence)
			continue
		}
		if exception.Type == recurrence.ExceptionCancel {
			continue
		}
		projected, err := projectOverrideOccurrence(ctx, child, exception)
		if err != nil {
			return nil, err
		}
		projected.Key = occurrence.Key
		projected.OriginalLocal = occurrence.OriginalLocal
		result = append(result, projected)
	}
	_ = parent
	return result, nil
}

func (repository *PostgresRepository) UpdateSeriesOccurrence(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	seriesID uuid.UUID,
	params SeriesMutationParams,
	updatedAt time.Time,
) (SeriesMutationResult, error) {
	return repository.mutateSeriesOccurrence(
		ctx, tenantContext, classID, seriesID, params, updatedAt, false,
	)
}

func (repository *PostgresRepository) CancelSeriesOccurrence(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	seriesID uuid.UUID,
	params SeriesMutationParams,
	updatedAt time.Time,
) (SeriesMutationResult, error) {
	return repository.mutateSeriesOccurrence(
		ctx, tenantContext, classID, seriesID, params, updatedAt, true,
	)
}

func (repository *PostgresRepository) mutateSeriesOccurrence(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	seriesID uuid.UUID,
	params SeriesMutationParams,
	updatedAt time.Time,
	cancelMutation bool,
) (SeriesMutationResult, error) {
	if err := tenantContext.Validate(); err != nil {
		return SeriesMutationResult{}, ErrSessionAccessDenied
	}
	if classID == uuid.Nil || seriesID == uuid.Nil {
		return SeriesMutationResult{}, ErrSeriesNotFound
	}
	operation := "update"
	if cancelMutation {
		operation = "cancel"
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SeriesMutationResult{}, fmt.Errorf("begin recurring %s: %w", operation, err)
	}
	defer rollbackClassTransaction(transaction)
	if err := repository.requireSessionRecurrenceFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return SeriesMutationResult{}, err
	}
	locked, membership, err := repository.lockClassMutation(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return SeriesMutationResult{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext, membership, locked.Class, policy.ActionSessionSchedule,
	); err != nil {
		return SeriesMutationResult{}, err
	}
	if params.OverrideScheduleConflict && membership.Role != string(policy.OrganizationRoleAdmin) {
		return SeriesMutationResult{}, ErrConflictOverrideDenied
	}
	series, err := lockClassSessionSeries(
		queryContext, transaction, tenantContext.TenantID, classID, seriesID,
	)
	if err != nil {
		return SeriesMutationResult{}, err
	}
	replay, replaySeriesID, err := loadSeriesMutationReceipt(
		queryContext,
		transaction,
		tenantContext.TenantID,
		params.IdempotencyKey,
		params.Fingerprint,
		operation,
	)
	if err != nil {
		return SeriesMutationResult{}, err
	}
	if replay {
		result, err := lockClassSessionSeries(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			replaySeriesID,
		)
		if err != nil {
			return SeriesMutationResult{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return SeriesMutationResult{}, fmt.Errorf("commit recurring replay: %w", err)
		}
		return SeriesMutationResult{Series: result, Replay: true}, nil
	}
	if series.Status != SeriesStatusScheduled || series.Version != params.ExpectedVersion {
		return SeriesMutationResult{}, ErrSeriesVersionConflict
	}
	occurrences, err := expandCompleteSeries(queryContext, definitionFromSeries(series))
	if err != nil {
		return SeriesMutationResult{}, err
	}
	boundaryIndex := occurrenceIndex(occurrences, params.OccurrenceKey)
	if boundaryIndex < 0 {
		return SeriesMutationResult{}, ErrInvalidSessionInput
	}
	exceptions, err := listSeriesExceptions(
		queryContext, transaction, tenantContext.TenantID, classID, seriesID,
	)
	if err != nil {
		return SeriesMutationResult{}, err
	}
	if _, err := recurrence.PreviewScope(
		occurrences,
		exceptionSummaries(exceptions),
		params.OccurrenceKey,
		params.Scope,
		params.FollowingExceptionPolicy,
	); err != nil {
		return SeriesMutationResult{}, ErrInvalidSessionInput
	}

	var result ClassSessionSeries
	switch params.Scope {
	case recurrence.ScopeThisOccurrence:
		result, err = repository.mutateOneOccurrence(
			queryContext, transaction, tenantContext, series, occurrences[boundaryIndex],
			params, updatedAt, cancelMutation,
		)
	case recurrence.ScopeFollowing:
		result, err = repository.mutateFollowingOccurrences(
			queryContext, transaction, tenantContext, series, occurrences,
			boundaryIndex, exceptions, params, updatedAt, cancelMutation,
		)
	case recurrence.ScopeEntireSeries:
		result, err = repository.mutateEntireSeries(
			queryContext, transaction, tenantContext, membership, series,
			exceptions, params, updatedAt, cancelMutation,
		)
	default:
		err = ErrInvalidSessionInput
	}
	if err != nil {
		return SeriesMutationResult{}, err
	}
	if err := insertSeriesMutationReceipt(
		queryContext,
		transaction,
		tenantContext,
		params,
		operation,
		classID,
		seriesID,
		result.ID,
		updatedAt,
	); err != nil {
		return SeriesMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return SeriesMutationResult{}, fmt.Errorf("commit recurring %s: %w", operation, err)
	}
	return SeriesMutationResult{Series: result}, nil
}

func (repository *PostgresRepository) mutateOneOccurrence(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	series ClassSessionSeries,
	occurrence recurrence.Occurrence,
	params SeriesMutationParams,
	updatedAt time.Time,
	cancelMutation bool,
) (ClassSessionSeries, error) {
	exception := persistedSeriesException{
		OccurrenceKey:    params.OccurrenceKey,
		OriginalLocal:    occurrence.OriginalLocal,
		OriginalTimezone: series.Timezone,
		Reason:           params.ScheduleConflictReason,
	}
	_, exception.OriginalOffset = occurrence.StartsAt.In(mustLocation(series.Timezone)).Zone()
	var cancellationSource lockedTypedParticipationSource
	if cancelMutation {
		var err error
		cancellationSource, err = repository.lockTypedParticipationSource(
			ctx,
			transaction,
			series.TenantID,
			series.ClassID,
			OccurrenceParticipationSource(series.ID, occurrence.Key),
			true,
		)
		if err != nil {
			return ClassSessionSeries{}, err
		}
		exception.Type = recurrence.ExceptionCancel
	} else {
		exception.Type = recurrence.ExceptionOverride
		var err error
		exception, err = applyOccurrenceOverride(exception, series, occurrence, params)
		if err != nil {
			return ClassSessionSeries{}, err
		}
		projected, projectErr := projectOverrideOccurrence(ctx, series, exception)
		if projectErr != nil {
			return ClassSessionSeries{}, projectErr
		}
		if conflict, conflictErr := repository.findClassScheduleConflict(
			ctx, transaction, series.TenantID, series.ClassID, series.ID,
			[]recurrence.Occurrence{projected},
		); conflictErr != nil {
			return ClassSessionSeries{}, conflictErr
		} else if conflict != nil && !params.OverrideScheduleConflict {
			return ClassSessionSeries{}, ErrSessionScheduleConflict
		}
	}
	if err := upsertSeriesException(
		ctx, transaction, series, exception, tenantContext.ActorID, updatedAt,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	result, err := bumpClassSessionSeries(
		ctx, transaction, series, tenantContext.ActorID, updatedAt,
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if cancelMutation {
		if err := repository.insertTypedCancellationSnapshot(
			ctx,
			transaction,
			tenantContext,
			cancellationSource,
			result.Version,
			nextCancellationSequence(cancellationSource.Sequence, result.Sequence),
			updatedAt,
			"occurrence_cancelled",
		); err != nil {
			return ClassSessionSeries{}, err
		}
		if err := closeParticipationForSource(
			ctx,
			transaction,
			series.TenantID,
			series.ClassID,
			OccurrenceParticipationSource(series.ID, occurrence.Key),
			tenantContext.ActorID,
			updatedAt,
			"occurrence_cancelled",
		); err != nil {
			return ClassSessionSeries{}, err
		}
	}
	eventType := "class_session_occurrence.updated.v1"
	if cancelMutation {
		eventType = "class_session_occurrence.cancelled.v1"
	}
	if err := insertClassSessionSeriesEvent(
		ctx, transaction, result, tenantContext.ActorID, eventType,
		params.ScheduleConflictReason, updatedAt,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	return result, nil
}

func (repository *PostgresRepository) mutateEntireSeries(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	membership lockedClassMembership,
	series ClassSessionSeries,
	exceptions []persistedSeriesException,
	params SeriesMutationParams,
	updatedAt time.Time,
	cancelMutation bool,
) (ClassSessionSeries, error) {
	if cancelMutation {
		result, err := repository.cancelSeriesWithParticipationSnapshot(
			ctx,
			transaction,
			tenantContext,
			series,
			updatedAt,
			"series_cancelled",
		)
		if err != nil {
			return ClassSessionSeries{}, err
		}
		if err := insertClassSessionSeriesEvent(
			ctx, transaction, result, tenantContext.ActorID,
			"class_session_series.cancelled.v1",
			params.ScheduleConflictReason,
			updatedAt,
		); err != nil {
			return ClassSessionSeries{}, err
		}
		return result, nil
	}
	updated, identityChanged, err := applySeriesUpdate(series, params)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if identityChanged && len(exceptions) > 0 {
		return ClassSessionSeries{}, ErrSeriesExceptionConflict
	}
	occurrences, err := expandCompleteSeries(ctx, definitionFromSeries(updated))
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if err := requireNoSelfOverlap(occurrences); err != nil {
		return ClassSessionSeries{}, err
	}
	if conflict, conflictErr := repository.findClassScheduleConflict(
		ctx, transaction, series.TenantID, series.ClassID, series.ID, occurrences,
	); conflictErr != nil {
		return ClassSessionSeries{}, conflictErr
	} else if conflict != nil && !params.OverrideScheduleConflict {
		return ClassSessionSeries{}, ErrSessionScheduleConflict
	}
	if params.OverrideScheduleConflict &&
		membership.Role != string(policy.OrganizationRoleAdmin) {
		return ClassSessionSeries{}, ErrConflictOverrideDenied
	}
	result, err := updateClassSessionSeries(
		ctx, transaction, updated, tenantContext.ActorID, updatedAt,
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if err := insertClassSessionSeriesEvent(
		ctx, transaction, result, tenantContext.ActorID,
		"class_session_series.updated.v1",
		params.ScheduleConflictReason,
		updatedAt,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	return result, nil
}

func (repository *PostgresRepository) mutateFollowingOccurrences(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	series ClassSessionSeries,
	occurrences []recurrence.Occurrence,
	boundaryIndex int,
	exceptions []persistedSeriesException,
	params SeriesMutationParams,
	updatedAt time.Time,
	cancelMutation bool,
) (ClassSessionSeries, error) {
	if cancelMutation {
		var result ClassSessionSeries
		var err error
		if boundaryIndex == 0 {
			result, err = repository.cancelSeriesWithParticipationSnapshot(
				ctx,
				transaction,
				tenantContext,
				series,
				updatedAt,
				"series_following_cancelled",
			)
		} else {
			master, lockErr := repository.lockTypedParticipationSource(
				ctx,
				transaction,
				series.TenantID,
				series.ClassID,
				SeriesParticipationSource(series.ID),
				true,
			)
			if lockErr != nil {
				return ClassSessionSeries{}, lockErr
			}
			occurrenceKeys := make([]string, 0, len(occurrences)-boundaryIndex)
			for _, occurrence := range occurrences[boundaryIndex:] {
				occurrenceKeys = append(occurrenceKeys, occurrence.Key)
			}
			occurrenceSources, captureErr :=
				repository.captureActiveOccurrenceCancellationSources(
					ctx,
					transaction,
					series.TenantID,
					series.ClassID,
					series.ID,
					occurrenceKeys,
				)
			if captureErr != nil {
				return ClassSessionSeries{}, captureErr
			}
			result = series
			result.Rule.End = recurrence.End{
				Type: recurrence.EndAfterCount, Count: boundaryIndex,
			}
			result.NormalizedRule, err = recurrence.NormalizeRule(result.Rule)
			if err == nil {
				result, err = updateClassSessionSeries(
					ctx, transaction, result, tenantContext.ActorID, updatedAt,
				)
			}
			if err == nil {
				err = repository.snapshotFollowingCancellation(
					ctx,
					transaction,
					tenantContext,
					master,
					occurrenceSources,
					result,
					updatedAt,
					"series_following_cancelled",
				)
			}
		}
		if err != nil {
			return ClassSessionSeries{}, err
		}
		if boundaryIndex > 0 {
			occurrenceKeys := make([]string, 0, len(occurrences)-boundaryIndex)
			for _, occurrence := range occurrences[boundaryIndex:] {
				occurrenceKeys = append(occurrenceKeys, occurrence.Key)
			}
			if err := closeParticipationForSeriesOccurrenceKeys(
				ctx,
				transaction,
				series.TenantID,
				series.ClassID,
				series.ID,
				occurrenceKeys,
				tenantContext.ActorID,
				updatedAt,
				"series_following_cancelled",
			); err != nil {
				return ClassSessionSeries{}, err
			}
		}
		if err := insertClassSessionSeriesEvent(
			ctx, transaction, result, tenantContext.ActorID,
			"class_session_series.following_cancelled.v1",
			params.ScheduleConflictReason,
			updatedAt,
		); err != nil {
			return ClassSessionSeries{}, err
		}
		return result, nil
	}

	child := series
	child.ID = uuid.New()
	child.ICalUID = child.ID.String() + "@calendar.tutorhub"
	child.SplitFrom = &series.ID
	child.Version = 1
	child.Sequence = 0
	child.Status = SeriesStatusScheduled
	child.CreatedBy = tenantContext.ActorID
	child.UpdatedBy = tenantContext.ActorID
	child.CreatedAt = updatedAt
	child.UpdatedAt = updatedAt
	child.CancelledAt = nil
	child.CancelledBy = nil
	child.LocalStart = occurrences[boundaryIndex].OriginalLocal
	if series.Rule.End.Type == recurrence.EndAfterCount {
		child.Rule.End.Count = len(occurrences) - boundaryIndex
	}
	child.NormalizedRule, _ = recurrence.NormalizeRule(child.Rule)
	var identityChanged bool
	var err error
	child, identityChanged, err = applySeriesUpdate(child, params)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	_ = identityChanged
	childOccurrences, err := expandCompleteSeries(ctx, definitionFromSeries(child))
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if err := requireNoSelfOverlap(childOccurrences); err != nil {
		return ClassSessionSeries{}, err
	}
	if conflict, conflictErr := repository.findClassScheduleConflict(
		ctx, transaction, series.TenantID, series.ClassID, series.ID, childOccurrences,
	); conflictErr != nil {
		return ClassSessionSeries{}, conflictErr
	} else if conflict != nil && !params.OverrideScheduleConflict {
		return ClassSessionSeries{}, ErrSessionScheduleConflict
	}

	if boundaryIndex == 0 {
		if _, err := cancelClassSessionSeries(
			ctx,
			transaction,
			series,
			tenantContext.ActorID,
			updatedAt,
			"series_split",
		); err != nil {
			return ClassSessionSeries{}, err
		}
	} else {
		parent := series
		parent.Rule.End = recurrence.End{
			Type: recurrence.EndAfterCount, Count: boundaryIndex,
		}
		parent.NormalizedRule, err = recurrence.NormalizeRule(parent.Rule)
		if err != nil {
			return ClassSessionSeries{}, mapRecurrenceError(err)
		}
		if _, err := updateClassSessionSeries(
			ctx, transaction, parent, tenantContext.ActorID, updatedAt,
		); err != nil {
			return ClassSessionSeries{}, err
		}
	}
	childParams := createParamsFromSeries(child)
	inserted, err := insertClassSessionSeries(
		ctx,
		transaction,
		series.TenantID,
		series.ClassID,
		childParams,
		&series.ID,
		updatedAt,
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if params.FollowingExceptionPolicy != recurrence.ExceptionDiscard {
		if err := copyFutureExceptions(
			ctx, transaction, series, inserted, occurrences[boundaryIndex:],
			childOccurrences, exceptions, params.FollowingExceptionPolicy,
			tenantContext.ActorID, updatedAt,
		); err != nil {
			return ClassSessionSeries{}, err
		}
	}
	if err := insertClassSessionSeriesEvent(
		ctx, transaction, inserted, tenantContext.ActorID,
		"class_session_series.split.v1",
		params.ScheduleConflictReason,
		updatedAt,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	return inserted, nil
}

func applySeriesUpdate(
	series ClassSessionSeries,
	params SeriesMutationParams,
) (ClassSessionSeries, bool, error) {
	identityChanged := false
	if params.Title != nil {
		series.Title = *params.Title
	}
	if params.Description != nil {
		series.Description = *params.Description
	}
	if params.StartsAt != nil {
		start, err := parseSessionTimestamp(*params.StartsAt, *params.Timezone)
		if err != nil {
			return ClassSessionSeries{}, false, err
		}
		end, err := parseSessionTimestamp(*params.EndsAt, *params.Timezone)
		if err != nil {
			return ClassSessionSeries{}, false, err
		}
		if err := validateSessionTimeRange(start, end); err != nil ||
			end.Sub(start)%time.Minute != 0 {
			return ClassSessionSeries{}, false, ErrInvalidSessionInput
		}
		local := start.In(mustLocation(*params.Timezone)).Format(civilDateTimeLayout)
		identityChanged = local != series.LocalStart || *params.Timezone != series.Timezone
		series.LocalStart = local
		series.Timezone = *params.Timezone
		series.DurationMinutes = int(end.Sub(start) / time.Minute)
	}
	if params.Rule != nil {
		normalized, err := recurrence.NormalizeRule(*params.Rule)
		if err != nil {
			return ClassSessionSeries{}, false, mapRecurrenceError(err)
		}
		identityChanged = true
		series.Rule = *params.Rule
		series.NormalizedRule = normalized
	}
	if params.OverlapPolicy != nil {
		series.OverlapPolicy = *params.OverlapPolicy
	}
	return series, identityChanged, nil
}

func applyOccurrenceOverride(
	exception persistedSeriesException,
	series ClassSessionSeries,
	occurrence recurrence.Occurrence,
	params SeriesMutationParams,
) (persistedSeriesException, error) {
	if params.Title != nil {
		exception.OverrideTitle = params.Title
	}
	if params.Description != nil {
		exception.OverrideDescription = params.Description
	}
	if params.StartsAt != nil {
		start, err := parseSessionTimestamp(*params.StartsAt, *params.Timezone)
		if err != nil {
			return persistedSeriesException{}, err
		}
		end, err := parseSessionTimestamp(*params.EndsAt, *params.Timezone)
		if err != nil {
			return persistedSeriesException{}, err
		}
		if err := validateSessionTimeRange(start, end); err != nil ||
			end.Sub(start)%time.Minute != 0 {
			return persistedSeriesException{}, ErrInvalidSessionInput
		}
		local := start.In(mustLocation(*params.Timezone)).Format(civilDateTimeLayout)
		duration := int(end.Sub(start) / time.Minute)
		exception.OverrideLocalStart = &local
		exception.OverrideTimezone = params.Timezone
		exception.OverrideDuration = &duration
	}
	if exception.OverrideTitle == nil && exception.OverrideDescription == nil &&
		exception.OverrideLocalStart == nil && exception.OverrideTimezone == nil &&
		exception.OverrideDuration == nil {
		return persistedSeriesException{}, ErrInvalidSessionInput
	}
	_ = series
	_ = occurrence
	return exception, nil
}

func projectOverrideOccurrence(
	ctx context.Context,
	series ClassSessionSeries,
	exception persistedSeriesException,
) (recurrence.Occurrence, error) {
	definition := definitionFromSeries(series)
	local := exception.OriginalLocal
	if exception.OverrideLocalStart != nil {
		local = *exception.OverrideLocalStart
	}
	if exception.OverrideTimezone != nil {
		definition.TimeZone = *exception.OverrideTimezone
	}
	if exception.OverrideDuration != nil {
		definition.Duration = time.Duration(*exception.OverrideDuration) * time.Minute
	}
	resolved, err := recurrence.ResolveOccurrence(ctx, definition, local)
	if err != nil {
		return recurrence.Occurrence{}, mapRecurrenceError(err)
	}
	resolved.Key = exception.OccurrenceKey
	resolved.OriginalLocal = exception.OriginalLocal
	return resolved, nil
}

func createParamsFromSeries(series ClassSessionSeries) CreateSeriesParams {
	return CreateSeriesParams{
		ID: series.ID, Title: series.Title, Description: series.Description,
		LocalStart: series.LocalStart, Timezone: series.Timezone,
		DurationMinutes: series.DurationMinutes, Rule: series.Rule,
		NormalizedRule: series.NormalizedRule, OverlapPolicy: series.OverlapPolicy,
		CreatedBy: series.CreatedBy,
	}
}

func occurrenceIndex(occurrences []recurrence.Occurrence, key string) int {
	for index, occurrence := range occurrences {
		if occurrence.Key == key {
			return index
		}
	}
	return -1
}

func exceptionSummaries(
	exceptions []persistedSeriesException,
) []recurrence.Exception {
	result := make([]recurrence.Exception, 0, len(exceptions))
	for _, exception := range exceptions {
		result = append(result, recurrence.Exception{
			OccurrenceKey: exception.OccurrenceKey, Type: exception.Type,
		})
	}
	return result
}

const selectClassSessionSeriesSQL = `SELECT
    series.id, series.tenant_id, series.class_id, series.title, series.description,
    series.local_start, series.timezone, series.duration_minutes,
    series.recurrence_frequency, series.recurrence_interval,
    series.recurrence_weekdays, series.recurrence_month_days, series.recurrence_months,
    series.recurrence_end_type, series.recurrence_until_date, series.recurrence_count,
    series.normalized_rule, series.overlap_policy, series.status, series.version,
    series.sequence, series.ical_uid, series.split_from_series_id,
    series.created_by, series.updated_by, series.cancelled_at, series.cancelled_by,
    series.created_at, series.updated_at
FROM tutorhub.class_session_series AS series`

const loadSeriesMutationReceiptSQL = `SELECT request_fingerprint, operation, result_series_id
FROM tutorhub.class_session_mutation_receipts
WHERE tenant_id = $1 AND idempotency_key = $2`

func lockClassSessionSeries(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	seriesID uuid.UUID,
) (ClassSessionSeries, error) {
	series, err := scanClassSessionSeries(transaction.QueryRow(
		ctx,
		selectClassSessionSeriesSQL+`
WHERE series.tenant_id = $1 AND series.class_id = $2 AND series.id = $3
FOR UPDATE`,
		tenantID, classID, seriesID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSessionSeries{}, ErrSeriesNotFound
	}
	if err != nil {
		return ClassSessionSeries{}, fmt.Errorf("lock recurring session series: %w", err)
	}
	return series, nil
}

func scanClassSessionSeries(row rowScanner) (ClassSessionSeries, error) {
	var series ClassSessionSeries
	var localStart time.Time
	var frequency recurrence.Frequency
	var weekdays []string
	var monthDays []int16
	var months []int16
	var endType recurrence.EndType
	var untilDate sql.NullTime
	var recurrenceCount sql.NullInt32
	var splitFrom uuid.NullUUID
	var cancelledBy uuid.NullUUID
	if err := row.Scan(
		&series.ID, &series.TenantID, &series.ClassID, &series.Title,
		&series.Description, &localStart, &series.Timezone, &series.DurationMinutes,
		&frequency, &series.Rule.Interval, &weekdays, &monthDays, &months,
		&endType, &untilDate, &recurrenceCount, &series.NormalizedRule,
		&series.OverlapPolicy, &series.Status, &series.Version, &series.Sequence,
		&series.ICalUID, &splitFrom, &series.CreatedBy, &series.UpdatedBy,
		&series.CancelledAt, &cancelledBy, &series.CreatedAt, &series.UpdatedAt,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	series.LocalStart = localStart.Format(civilDateTimeLayout)
	series.Rule.Frequency = frequency
	series.Rule.Weekdays = make([]recurrence.Weekday, 0, len(weekdays))
	for _, weekday := range weekdays {
		series.Rule.Weekdays = append(series.Rule.Weekdays, recurrence.Weekday(weekday))
	}
	series.Rule.MonthDays = int16sToInts(monthDays)
	series.Rule.Months = int16sToInts(months)
	series.Rule.End.Type = endType
	if untilDate.Valid {
		series.Rule.End.Date = untilDate.Time.Format("2006-01-02")
	}
	if recurrenceCount.Valid {
		series.Rule.End.Count = int(recurrenceCount.Int32)
	}
	if splitFrom.Valid {
		value := splitFrom.UUID
		series.SplitFrom = &value
	}
	if cancelledBy.Valid {
		value := cancelledBy.UUID
		series.CancelledBy = &value
	}
	return series, nil
}

func insertClassSessionSeries(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	params CreateSeriesParams,
	splitFrom *uuid.UUID,
	createdAt time.Time,
) (ClassSessionSeries, error) {
	localStart, _ := time.Parse(civilDateTimeLayout, params.LocalStart)
	icalUID := params.ID.String() + "@calendar.tutorhub"
	series, err := scanClassSessionSeries(transaction.QueryRow(
		ctx,
		`INSERT INTO tutorhub.class_session_series (
    id, tenant_id, class_id, title, description, local_start, timezone,
    duration_minutes, recurrence_frequency, recurrence_interval,
    recurrence_weekdays, recurrence_month_days, recurrence_months,
    recurrence_end_type, recurrence_until_date, recurrence_count,
    normalized_rule, overlap_policy, ical_uid, split_from_series_id,
    created_by, updated_by, created_at, updated_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
    $14, $15, $16, $17, $18, $19, $20, $21, $21, $22, $22
)
RETURNING
    id, tenant_id, class_id, title, description, local_start, timezone,
    duration_minutes, recurrence_frequency, recurrence_interval,
    recurrence_weekdays, recurrence_month_days, recurrence_months,
    recurrence_end_type, recurrence_until_date, recurrence_count,
    normalized_rule, overlap_policy, status, version, sequence, ical_uid,
    split_from_series_id, created_by, updated_by, cancelled_at, cancelled_by,
    created_at, updated_at`,
		params.ID, tenantID, classID, params.Title, params.Description,
		localStart, params.Timezone, params.DurationMinutes,
		params.Rule.Frequency, params.Rule.Interval,
		weekdaysToStrings(params.Rule.Weekdays), intsToInt16s(params.Rule.MonthDays),
		intsToInt16s(params.Rule.Months), params.Rule.End.Type,
		nullableEndDate(params.Rule.End), nullableEndCount(params.Rule.End),
		params.NormalizedRule, params.OverlapPolicy, icalUID, splitFrom,
		params.CreatedBy, createdAt,
	))
	if err != nil {
		return ClassSessionSeries{}, mapSeriesPostgresError("insert recurring session series", err)
	}
	return series, nil
}

func updateClassSessionSeries(
	ctx context.Context,
	transaction pgx.Tx,
	series ClassSessionSeries,
	actorID uuid.UUID,
	updatedAt time.Time,
) (ClassSessionSeries, error) {
	localStart, _ := time.Parse(civilDateTimeLayout, series.LocalStart)
	updated, err := scanClassSessionSeries(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.class_session_series
SET title = $4, description = $5, local_start = $6, timezone = $7,
    duration_minutes = $8, recurrence_frequency = $9,
    recurrence_interval = $10, recurrence_weekdays = $11,
    recurrence_month_days = $12, recurrence_months = $13,
    recurrence_end_type = $14, recurrence_until_date = $15,
    recurrence_count = $16, normalized_rule = $17, overlap_policy = $18,
    version = version + 1, sequence = sequence + 1,
    updated_by = $19, updated_at = $20
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND version = $21
RETURNING
    id, tenant_id, class_id, title, description, local_start, timezone,
    duration_minutes, recurrence_frequency, recurrence_interval,
    recurrence_weekdays, recurrence_month_days, recurrence_months,
    recurrence_end_type, recurrence_until_date, recurrence_count,
    normalized_rule, overlap_policy, status, version, sequence, ical_uid,
    split_from_series_id, created_by, updated_by, cancelled_at, cancelled_by,
    created_at, updated_at`,
		series.TenantID, series.ClassID, series.ID, series.Title, series.Description,
		localStart, series.Timezone, series.DurationMinutes, series.Rule.Frequency,
		series.Rule.Interval, weekdaysToStrings(series.Rule.Weekdays),
		intsToInt16s(series.Rule.MonthDays), intsToInt16s(series.Rule.Months),
		series.Rule.End.Type, nullableEndDate(series.Rule.End),
		nullableEndCount(series.Rule.End), series.NormalizedRule,
		series.OverlapPolicy, actorID, updatedAt, series.Version,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSessionSeries{}, ErrSeriesVersionConflict
	}
	if err != nil {
		return ClassSessionSeries{}, mapSeriesPostgresError("update recurring session series", err)
	}
	return updated, nil
}

func bumpClassSessionSeries(
	ctx context.Context,
	transaction pgx.Tx,
	series ClassSessionSeries,
	actorID uuid.UUID,
	updatedAt time.Time,
) (ClassSessionSeries, error) {
	result, err := scanClassSessionSeries(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.class_session_series
SET version = version + 1, sequence = sequence + 1,
    updated_by = $4, updated_at = $5
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND version = $6
RETURNING
    id, tenant_id, class_id, title, description, local_start, timezone,
    duration_minutes, recurrence_frequency, recurrence_interval,
    recurrence_weekdays, recurrence_month_days, recurrence_months,
    recurrence_end_type, recurrence_until_date, recurrence_count,
    normalized_rule, overlap_policy, status, version, sequence, ical_uid,
    split_from_series_id, created_by, updated_by, cancelled_at, cancelled_by,
    created_at, updated_at`,
		series.TenantID, series.ClassID, series.ID, actorID, updatedAt, series.Version,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSessionSeries{}, ErrSeriesVersionConflict
	}
	return result, err
}

func cancelClassSessionSeries(
	ctx context.Context,
	transaction pgx.Tx,
	series ClassSessionSeries,
	actorID uuid.UUID,
	cancelledAt time.Time,
	reason string,
) (ClassSessionSeries, error) {
	if series.Status == SeriesStatusCancelled {
		if err := closeParticipationForSource(
			ctx,
			transaction,
			series.TenantID,
			series.ClassID,
			SeriesParticipationSource(series.ID),
			actorID,
			cancelledAt,
			reason,
		); err != nil {
			return ClassSessionSeries{}, err
		}
		return series, nil
	}
	result, err := scanClassSessionSeries(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.class_session_series
SET status = 'cancelled', version = version + 1, sequence = sequence + 1,
    updated_by = $4, cancelled_by = $4, cancelled_at = $5, updated_at = $5
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND version = $6
RETURNING
    id, tenant_id, class_id, title, description, local_start, timezone,
    duration_minutes, recurrence_frequency, recurrence_interval,
    recurrence_weekdays, recurrence_month_days, recurrence_months,
    recurrence_end_type, recurrence_until_date, recurrence_count,
    normalized_rule, overlap_policy, status, version, sequence, ical_uid,
    split_from_series_id, created_by, updated_by, cancelled_at, cancelled_by,
    created_at, updated_at`,
		series.TenantID, series.ClassID, series.ID, actorID, cancelledAt, series.Version,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSessionSeries{}, ErrSeriesVersionConflict
	}
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if err := closeParticipationForSource(
		ctx,
		transaction,
		series.TenantID,
		series.ClassID,
		SeriesParticipationSource(series.ID),
		actorID,
		cancelledAt,
		reason,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	return result, nil
}

func listSeriesExceptions(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	seriesID uuid.UUID,
) ([]persistedSeriesException, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT occurrence_key, exception_type, original_local_start,
       original_timezone, original_overlap_offset_seconds,
       override_local_start, override_timezone, override_duration_minutes,
       override_title, override_description, reason, version
FROM tutorhub.class_session_exceptions
WHERE tenant_id = $1 AND class_id = $2 AND series_id = $3
ORDER BY original_local_start, occurrence_key`,
		tenantID, classID, seriesID,
	)
	if err != nil {
		return nil, fmt.Errorf("list recurring session exceptions: %w", err)
	}
	defer rows.Close()
	result := make([]persistedSeriesException, 0)
	for rows.Next() {
		var exception persistedSeriesException
		var originalLocal time.Time
		var overrideLocal sql.NullTime
		var overrideTimezone, overrideTitle, overrideDescription sql.NullString
		var overrideDuration sql.NullInt32
		if err := rows.Scan(
			&exception.OccurrenceKey, &exception.Type, &originalLocal,
			&exception.OriginalTimezone, &exception.OriginalOffset,
			&overrideLocal, &overrideTimezone, &overrideDuration,
			&overrideTitle, &overrideDescription, &exception.Reason,
			&exception.Version,
		); err != nil {
			return nil, fmt.Errorf("scan recurring session exception: %w", err)
		}
		exception.OriginalLocal = originalLocal.Format(civilDateTimeLayout)
		if overrideLocal.Valid {
			value := overrideLocal.Time.Format(civilDateTimeLayout)
			exception.OverrideLocalStart = &value
		}
		exception.OverrideTimezone = nullStringPointer(overrideTimezone)
		exception.OverrideTitle = nullStringPointer(overrideTitle)
		exception.OverrideDescription = nullStringPointer(overrideDescription)
		if overrideDuration.Valid {
			value := int(overrideDuration.Int32)
			exception.OverrideDuration = &value
		}
		result = append(result, exception)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recurring session exceptions: %w", err)
	}
	return result, nil
}

func upsertSeriesException(
	ctx context.Context,
	transaction pgx.Tx,
	series ClassSessionSeries,
	exception persistedSeriesException,
	actorID uuid.UUID,
	updatedAt time.Time,
) error {
	originalLocal, _ := time.Parse(civilDateTimeLayout, exception.OriginalLocal)
	var overrideLocal any
	if exception.OverrideLocalStart != nil {
		value, _ := time.Parse(civilDateTimeLayout, *exception.OverrideLocalStart)
		overrideLocal = value
	}
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.class_session_exceptions (
    series_id, occurrence_key, tenant_id, class_id, exception_type,
    original_local_start, original_timezone, original_overlap_offset_seconds,
    override_local_start, override_timezone, override_duration_minutes,
    override_title, override_description, reason, created_by, updated_by,
    created_at, updated_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
    $15, $15, $16, $16
)
ON CONFLICT (series_id, occurrence_key) DO UPDATE SET
    exception_type = EXCLUDED.exception_type,
    override_local_start = EXCLUDED.override_local_start,
    override_timezone = EXCLUDED.override_timezone,
    override_duration_minutes = EXCLUDED.override_duration_minutes,
    override_title = EXCLUDED.override_title,
    override_description = EXCLUDED.override_description,
    reason = EXCLUDED.reason,
    version = tutorhub.class_session_exceptions.version + 1,
    updated_by = EXCLUDED.updated_by,
    updated_at = EXCLUDED.updated_at`,
		series.ID, exception.OccurrenceKey, series.TenantID, series.ClassID,
		exception.Type, originalLocal, exception.OriginalTimezone,
		exception.OriginalOffset, overrideLocal, exception.OverrideTimezone,
		exception.OverrideDuration, exception.OverrideTitle,
		exception.OverrideDescription, exception.Reason, actorID, updatedAt,
	)
	if err != nil {
		return mapSeriesPostgresError("upsert recurring session exception", err)
	}
	return nil
}

func copyFutureExceptions(
	ctx context.Context,
	transaction pgx.Tx,
	parent ClassSessionSeries,
	child ClassSessionSeries,
	parentOccurrences []recurrence.Occurrence,
	childOccurrences []recurrence.Occurrence,
	exceptions []persistedSeriesException,
	policyValue recurrence.FutureExceptionPolicy,
	actorID uuid.UUID,
	updatedAt time.Time,
) error {
	parentIndex := make(map[string]int, len(parentOccurrences))
	parentLocal := make(map[string]string, len(parentOccurrences))
	for index, occurrence := range parentOccurrences {
		parentIndex[occurrence.Key] = index
		parentLocal[occurrence.Key] = occurrence.OriginalLocal
	}
	childByLocal := make(map[string]recurrence.Occurrence, len(childOccurrences))
	for _, occurrence := range childOccurrences {
		childByLocal[occurrence.OriginalLocal] = occurrence
	}
	for _, exception := range exceptions {
		index, future := parentIndex[exception.OccurrenceKey]
		if !future {
			continue
		}
		var target recurrence.Occurrence
		var ok bool
		if policyValue == recurrence.ExceptionCarry {
			target, ok = childByLocal[parentLocal[exception.OccurrenceKey]]
		} else if index < len(childOccurrences) {
			target, ok = childOccurrences[index], true
		}
		if !ok {
			return ErrSeriesExceptionConflict
		}
		copied := exception
		copied.OccurrenceKey = target.Key
		copied.OriginalLocal = target.OriginalLocal
		copied.OriginalTimezone = child.Timezone
		_, copied.OriginalOffset = target.StartsAt.In(mustLocation(child.Timezone)).Zone()
		if err := upsertSeriesException(
			ctx, transaction, child, copied, actorID, updatedAt,
		); err != nil {
			return err
		}
	}
	_ = parent
	return nil
}

func loadSeriesMutationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	idempotencyKey string,
	fingerprint string,
	operation string,
) (bool, uuid.UUID, error) {
	var storedFingerprint, storedOperation string
	var resultSeriesID uuid.UUID
	err := transaction.QueryRow(
		ctx,
		// mutateSeries already locks the target series row. The receipt primary key
		// serializes cross-series key collisions, so this read must remain compatible
		// with the append-only runtime role (SELECT + INSERT, without UPDATE).
		loadSeriesMutationReceiptSQL,
		tenantID, idempotencyKey,
	).Scan(&storedFingerprint, &storedOperation, &resultSeriesID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, uuid.Nil, nil
	}
	if err != nil {
		return false, uuid.Nil, fmt.Errorf("load recurring mutation receipt: %w", err)
	}
	if storedFingerprint != fingerprint || storedOperation != operation {
		return false, uuid.Nil, ErrSeriesIdempotencyConflict
	}
	return true, resultSeriesID, nil
}

func insertSeriesMutationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	params SeriesMutationParams,
	operation string,
	classID uuid.UUID,
	seriesID uuid.UUID,
	resultSeriesID uuid.UUID,
	createdAt time.Time,
) error {
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.class_session_mutation_receipts (
    tenant_id, idempotency_key, request_fingerprint, operation,
    class_id, series_id, result_series_id, created_by, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		tenantContext.TenantID, params.IdempotencyKey, params.Fingerprint,
		operation, classID, seriesID, resultSeriesID, tenantContext.ActorID, createdAt,
	)
	if err != nil {
		return mapSeriesPostgresError("insert recurring mutation receipt", err)
	}
	return nil
}

func (repository *PostgresRepository) findClassScheduleConflict(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	excludedSeriesID uuid.UUID,
	proposed []recurrence.Occurrence,
) (*ScheduleConflict, error) {
	if len(proposed) == 0 {
		return nil, nil
	}
	sort.Slice(proposed, func(left, right int) bool {
		return proposed[left].StartsAt.Before(proposed[right].StartsAt)
	})
	from, to := proposed[0].StartsAt, proposed[0].EndsAt
	for _, occurrence := range proposed[1:] {
		if occurrence.StartsAt.Before(from) {
			from = occurrence.StartsAt
		}
		if occurrence.EndsAt.After(to) {
			to = occurrence.EndsAt
		}
	}
	rows, err := transaction.Query(
		ctx,
		`SELECT id, starts_at, ends_at
FROM tutorhub.class_sessions
WHERE tenant_id = $1 AND class_id = $2
  AND status IN ('scheduled', 'live')
  AND starts_at < $4 AND ends_at > $3
  AND ($5::uuid = '00000000-0000-0000-0000-000000000000'
       OR series_id IS NULL OR series_id <> $5)
ORDER BY starts_at, id`,
		tenantID, classID, from, to, excludedSeriesID,
	)
	if err != nil {
		return nil, fmt.Errorf("query materialized schedule conflicts: %w", err)
	}
	var materialized []recurrence.Occurrence
	for rows.Next() {
		var id uuid.UUID
		var startsAt, endsAt time.Time
		if err := rows.Scan(&id, &startsAt, &endsAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan materialized schedule conflict: %w", err)
		}
		materialized = append(materialized, recurrence.Occurrence{
			Key: id.String(), StartsAt: startsAt.UTC(), EndsAt: endsAt.UTC(),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate materialized schedule conflicts: %w", err)
	}
	if conflict := firstOverlap(classID, nil, proposed, materialized); conflict != nil {
		return conflict, nil
	}

	seriesRows, err := transaction.Query(
		ctx,
		selectClassSessionSeriesSQL+`
WHERE series.tenant_id = $1 AND series.class_id = $2
  AND series.status = 'scheduled'
  AND ($3::uuid = '00000000-0000-0000-0000-000000000000' OR series.id <> $3)
ORDER BY series.local_start, series.id
LIMIT $4`,
		tenantID, classID, excludedSeriesID, maximumSeriesPerClass+1,
	)
	if err != nil {
		return nil, fmt.Errorf("query recurring schedule conflicts: %w", err)
	}
	otherSeries := make([]ClassSessionSeries, 0)
	for seriesRows.Next() {
		value, scanErr := scanClassSessionSeries(seriesRows)
		if scanErr != nil {
			seriesRows.Close()
			return nil, fmt.Errorf("scan recurring schedule series: %w", scanErr)
		}
		otherSeries = append(otherSeries, value)
	}
	seriesRows.Close()
	if err := seriesRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recurring schedule series: %w", err)
	}
	if len(otherSeries) > maximumSeriesPerClass {
		return nil, ErrSeriesLimitExceeded
	}
	for _, other := range otherSeries {
		occurrences, expandErr := expandCompleteSeries(ctx, definitionFromSeries(other))
		if expandErr != nil {
			return nil, mapRecurrenceError(expandErr)
		}
		exceptions, loadErr := listSeriesExceptions(
			ctx, transaction, tenantID, classID, other.ID,
		)
		if loadErr != nil {
			return nil, loadErr
		}
		occurrences, applyErr := applyPersistedExceptions(ctx, other, occurrences, exceptions)
		if applyErr != nil {
			return nil, applyErr
		}
		if conflict := firstOverlap(classID, &other.ID, proposed, occurrences); conflict != nil {
			return conflict, nil
		}
	}
	return nil, nil
}

func (repository *PostgresRepository) requireNoRecurringClassSessionConflict(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	startsAt time.Time,
	endsAt time.Time,
) error {
	rows, err := transaction.Query(
		ctx,
		selectClassSessionSeriesSQL+`
WHERE series.tenant_id = $1 AND series.class_id = $2
  AND series.status = 'scheduled'
ORDER BY series.local_start, series.id
LIMIT $3`,
		tenantID, classID, maximumSeriesPerClass+1,
	)
	if err != nil {
		return fmt.Errorf("query recurring conflicts for class session: %w", err)
	}
	seriesValues := make([]ClassSessionSeries, 0)
	for rows.Next() {
		value, scanErr := scanClassSessionSeries(rows)
		if scanErr != nil {
			rows.Close()
			return fmt.Errorf("scan recurring conflict for class session: %w", scanErr)
		}
		seriesValues = append(seriesValues, value)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate recurring conflicts for class session: %w", err)
	}
	if len(seriesValues) > maximumSeriesPerClass {
		return ErrSeriesLimitExceeded
	}
	proposed := []recurrence.Occurrence{{
		Key: uuid.NewString(), StartsAt: startsAt.UTC(), EndsAt: endsAt.UTC(),
	}}
	for _, series := range seriesValues {
		occurrences, expandErr := expandCompleteSeries(ctx, definitionFromSeries(series))
		if expandErr != nil {
			return expandErr
		}
		exceptions, loadErr := listSeriesExceptions(
			ctx, transaction, tenantID, classID, series.ID,
		)
		if loadErr != nil {
			return loadErr
		}
		occurrences, applyErr := applyPersistedExceptions(
			ctx, series, occurrences, exceptions,
		)
		if applyErr != nil {
			return applyErr
		}
		if firstOverlap(classID, &series.ID, proposed, occurrences) != nil {
			return ErrSessionScheduleConflict
		}
	}
	return nil
}

func applyPersistedExceptions(
	ctx context.Context,
	series ClassSessionSeries,
	occurrences []recurrence.Occurrence,
	exceptions []persistedSeriesException,
) ([]recurrence.Occurrence, error) {
	byKey := make(map[string]persistedSeriesException, len(exceptions))
	for _, exception := range exceptions {
		byKey[exception.OccurrenceKey] = exception
	}
	result := make([]recurrence.Occurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		exception, found := byKey[occurrence.Key]
		if !found {
			result = append(result, occurrence)
			continue
		}
		if exception.Type == recurrence.ExceptionCancel {
			continue
		}
		projected, err := projectOverrideOccurrence(ctx, series, exception)
		if err != nil {
			return nil, err
		}
		result = append(result, projected)
	}
	return result, nil
}

func firstOverlap(
	classID uuid.UUID,
	seriesID *uuid.UUID,
	proposed []recurrence.Occurrence,
	existing []recurrence.Occurrence,
) *ScheduleConflict {
	for _, candidate := range proposed {
		for _, current := range existing {
			if candidate.StartsAt.Before(current.EndsAt) &&
				current.StartsAt.Before(candidate.EndsAt) {
				return &ScheduleConflict{
					ClassID: classID, SeriesID: seriesID,
					OccurrenceKey: current.Key, StartsAt: current.StartsAt,
					EndsAt: current.EndsAt,
				}
			}
		}
	}
	return nil
}

func insertClassSessionSeriesEvent(
	ctx context.Context,
	transaction pgx.Tx,
	series ClassSessionSeries,
	actorID uuid.UUID,
	eventType string,
	overrideReason string,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'class_session_series', $2, $3,
    jsonb_build_object(
        'series_id', $2::uuid, 'class_id', $4::uuid,
        'actor_user_id', $5::uuid, 'status', $6::text,
        'version', $7::bigint, 'sequence', $8::bigint,
        'ical_uid', $9::text
    ),
    $10, $10
)`,
		series.TenantID, series.ID, eventType, series.ClassID, actorID,
		series.Status, series.Version, series.Sequence, series.ICalUID, occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}
	metadata := audit.Metadata{
		"class_id": series.ClassID.String(),
		"status":   string(series.Status),
		"version":  fmt.Sprintf("%d", series.Version),
		"sequence": fmt.Sprintf("%d", series.Sequence),
	}
	if overrideReason != "" {
		metadata["conflict_override"] = "true"
		metadata["override_reason"] = overrideReason
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: series.TenantID, ActorID: actorID, EventType: eventType,
		AggregateType: "class_session_series", AggregateID: series.ID,
		Metadata: metadata, OccurredAt: occurredAt,
	}); err != nil {
		return fmt.Errorf("insert %s audit event: %w", eventType, err)
	}
	return nil
}

func (repository *PostgresRepository) requireSessionRecurrenceFeature(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
) error {
	if repository.controls == nil {
		return nil
	}
	return repository.controls.RequireFeature(
		ctx, transaction, tenantID, featurecontrol.FeatureClassSessionRecurrence,
	)
}

func weekdaysToStrings(values []recurrence.Weekday) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, string(value))
	}
	return result
}

func intsToInt16s(values []int) []int16 {
	result := make([]int16, 0, len(values))
	for _, value := range values {
		result = append(result, int16(value))
	}
	return result
}

func int16sToInts(values []int16) []int {
	result := make([]int, 0, len(values))
	for _, value := range values {
		result = append(result, int(value))
	}
	return result
}

func nullableEndDate(end recurrence.End) any {
	if end.Type != recurrence.EndOnDate {
		return nil
	}
	value, _ := time.Parse("2006-01-02", end.Date)
	return value
}

func nullableEndCount(end recurrence.End) any {
	if end.Type != recurrence.EndAfterCount {
		return nil
	}
	return end.Count
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	result := value.String
	return &result
}

func mapSeriesPostgresError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch postgresError.ConstraintName {
	case "class_session_mutation_receipts_pkey":
		return ErrSeriesIdempotencyConflict
	case "class_session_series_class_fk":
		return ErrClassNotFound
	case "class_session_series_creator_membership_fk",
		"class_session_series_updater_membership_fk",
		"class_session_series_canceller_membership_fk",
		"class_session_exceptions_creator_membership_fk",
		"class_session_exceptions_updater_membership_fk",
		"class_session_mutation_receipts_creator_fk":
		return ErrSessionAccessDenied
	default:
		if strings.HasPrefix(postgresError.ConstraintName, "class_session_series_") ||
			strings.HasPrefix(postgresError.ConstraintName, "class_session_exceptions_") {
			return fmt.Errorf("%w: %s", ErrInvalidSessionInput, postgresError.ConstraintName)
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
}
