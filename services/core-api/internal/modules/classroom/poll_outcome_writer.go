package classroom

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

// CreatePollOutcomeInTransaction exposes only the narrow operation needed by
// availability-poll finalize. The caller owns transaction commit/rollback.
func (repository *PostgresRepository) CreatePollOutcomeInTransaction(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	classID uuid.UUID,
	input calendar.ClassSessionOutcomeInput,
) (uuid.UUID, error) {
	if repository == nil || transaction == nil || classID == uuid.Nil ||
		scope.Validate() != nil {
		return uuid.Nil, calendar.ErrClassSessionOutcomeUnavailable
	}
	params, err := (CreateSessionParams{
		Title: input.Title, Description: input.Description,
		StartsAt: input.StartsAt, EndsAt: input.EndsAt, Timezone: input.Timezone,
		CreatedBy: scope.ActorID,
	}).normalized()
	if err != nil {
		return uuid.Nil, calendar.ErrAvailabilityPollInvalid
	}
	if err := repository.requireSessionSchedulingFeature(
		ctx, transaction, scope.TenantID,
	); err != nil {
		return uuid.Nil, err
	}
	locked, membership, err := repository.lockClassMutation(ctx, transaction, scope, classID)
	if err != nil {
		return uuid.Nil, mapPollClassSessionOutcomeError(err)
	}
	if err := repository.authorizeLockedClass(
		scope, membership, locked.Class, policy.ActionSessionSchedule,
	); err != nil {
		return uuid.Nil, mapPollClassSessionOutcomeError(err)
	}
	if err := requireNoClassSessionConflict(
		ctx, transaction, scope.TenantID, classID, uuid.Nil, params.StartsAt, params.EndsAt,
	); err != nil {
		return uuid.Nil, mapPollClassSessionOutcomeError(err)
	}
	if err := repository.requireNoRecurringClassSessionConflict(
		ctx, transaction, scope.TenantID, classID, params.StartsAt, params.EndsAt,
	); err != nil {
		return uuid.Nil, mapPollClassSessionOutcomeError(err)
	}
	session, err := scanClassSession(transaction.QueryRow(
		ctx,
		`INSERT INTO tutorhub.class_sessions (
    tenant_id, class_id, title, description, starts_at, ends_at, timezone,
    created_by, updated_by, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9, $9)
RETURNING id, tenant_id, class_id, title, description, starts_at, ends_at,
          timezone, status, version, created_by, updated_by, cancelled_at,
          cancelled_by, created_at, updated_at`,
		scope.TenantID, classID, params.Title, params.Description,
		params.StartsAt, params.EndsAt, params.Timezone, scope.ActorID, input.CreatedAt.UTC(),
	))
	if err != nil {
		return uuid.Nil, mapPollClassSessionOutcomeError(
			mapSessionPostgresError("create poll class session outcome", err),
		)
	}
	if err := insertClassSessionEvent(
		ctx, transaction, session, scope.ActorID, "class_session.scheduled.v1", input.CreatedAt.UTC(),
	); err != nil {
		return uuid.Nil, fmt.Errorf("append poll class session outcome: %w", err)
	}
	return session.ID, nil
}

func mapPollClassSessionOutcomeError(err error) error {
	switch {
	case errors.Is(err, featurecontrol.ErrFeatureDisabled),
		errors.Is(err, featurecontrol.ErrQuotaExceeded):
		return err
	case errors.Is(err, ErrClassNotFound):
		return calendar.ErrClassSessionOutcomeNotFound
	case errors.Is(err, ErrClassAccessDenied), errors.Is(err, ErrSessionAccessDenied):
		return calendar.ErrClassSessionOutcomeAccessDenied
	case errors.Is(err, ErrSessionScheduleConflict):
		return calendar.ErrClassSessionOutcomeConflict
	default:
		return fmt.Errorf("%w: %v", calendar.ErrClassSessionOutcomeUnavailable, err)
	}
}

var _ calendar.ClassSessionOutcomeWriter = (*PostgresRepository)(nil)
