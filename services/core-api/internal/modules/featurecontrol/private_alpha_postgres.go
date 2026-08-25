package featurecontrol

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const privateAlphaEnrollmentUpsertSQL = `INSERT INTO tutorhub.tenant_private_alpha_enrollments (
    tenant_id, program, status, revision, notice_version,
    accepted_at, withdrawn_at, updated_by, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
ON CONFLICT (tenant_id, program) DO UPDATE SET
    status = EXCLUDED.status,
    revision = EXCLUDED.revision,
    notice_version = EXCLUDED.notice_version,
    accepted_at = EXCLUDED.accepted_at,
    withdrawn_at = EXCLUDED.withdrawn_at,
    updated_by = EXCLUDED.updated_by,
    updated_at = EXCLUDED.updated_at`

const privateAlphaEnrollmentSelectSQL = `SELECT
    status, revision, notice_version, accepted_at, withdrawn_at, updated_at
FROM tutorhub.tenant_private_alpha_enrollments
WHERE tenant_id = $1 AND program = $2`

func (repository *PostgresRepository) GetPrivateAlphaEnrollment(
	ctx context.Context,
	tenantContext tenancy.Context,
	now time.Time,
) (PrivateAlphaEnrollment, error) {
	if err := tenantContext.Validate(); err != nil {
		return PrivateAlphaEnrollment{}, ErrAccessDenied
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	defer rollbackFeatureControlTransaction(transaction)
	return repository.readAuthorizedPrivateAlphaEnrollment(
		queryContext, transaction, tenantContext,
	)
}

func (repository *PostgresRepository) readAuthorizedPrivateAlphaEnrollment(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
) (PrivateAlphaEnrollment, error) {
	if err := acquireTenantControlReadLock(ctx, transaction, tenantContext.TenantID); err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	authorization, err := repository.authorizeLockedTenant(
		ctx, transaction, tenantContext, policy.ActionTenantView,
	)
	if err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	enrollment, err := readPrivateAlphaEnrollment(
		ctx, transaction, tenantContext.TenantID, authorization.canManageControls, false,
	)
	if err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	return enrollment, nil
}

func (repository *PostgresRepository) UpdatePrivateAlphaEnrollment(
	ctx context.Context,
	tenantContext tenancy.Context,
	input UpdatePrivateAlphaEnrollmentInput,
	now time.Time,
) (PrivateAlphaEnrollment, error) {
	if err := tenantContext.Validate(); err != nil {
		return PrivateAlphaEnrollment{}, ErrAccessDenied
	}
	if input.ExpectedRevision < 0 || input.NoticeVersion != PrivateAlphaNoticeVersion {
		return PrivateAlphaEnrollment{}, ErrInvalidControl
	}
	return repository.updatePrivateAlphaEnrollment(ctx, tenantContext, input, now.UTC())
}

func (repository *PostgresRepository) updatePrivateAlphaEnrollment(
	ctx context.Context,
	tenantContext tenancy.Context,
	input UpdatePrivateAlphaEnrollmentInput,
	now time.Time,
) (PrivateAlphaEnrollment, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	defer rollbackFeatureControlTransaction(transaction)
	return repository.updateLockedPrivateAlphaEnrollment(
		queryContext, transaction, tenantContext, input, now,
	)
}

func (repository *PostgresRepository) updateLockedPrivateAlphaEnrollment(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	input UpdatePrivateAlphaEnrollmentInput,
	now time.Time,
) (PrivateAlphaEnrollment, error) {
	if err := acquireTenantControlLock(ctx, transaction, tenantContext.TenantID); err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	if _, err := repository.authorizeLockedTenant(
		ctx, transaction, tenantContext, policy.ActionTenantManageFeatures,
	); err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	current, err := readPrivateAlphaEnrollment(
		ctx, transaction, tenantContext.TenantID, true, true,
	)
	if err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	return repository.persistPrivateAlphaEnrollment(
		ctx, transaction, tenantContext, input, current, now,
	)
}

func (repository *PostgresRepository) persistPrivateAlphaEnrollment(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	input UpdatePrivateAlphaEnrollmentInput,
	current PrivateAlphaEnrollment,
	now time.Time,
) (PrivateAlphaEnrollment, error) {
	if current.Revision != input.ExpectedRevision {
		return PrivateAlphaEnrollment{}, &VersionConflictError{
			Expected: input.ExpectedRevision, Current: current.Revision,
		}
	}
	status := PrivateAlphaEnrollmentWithdrawn
	acceptedAt := current.AcceptedAt
	var withdrawnAt *time.Time
	if input.Enrolled {
		status = PrivateAlphaEnrollmentActive
		if acceptedAt == nil {
			acceptedAt = &now
		}
	} else {
		withdrawnAt = &now
	}
	return writePrivateAlphaEnrollment(
		ctx, transaction, tenantContext, status, current.Revision+1, acceptedAt, withdrawnAt, now,
	)
}

func writePrivateAlphaEnrollment(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	status PrivateAlphaEnrollmentStatus,
	revision int64,
	acceptedAt *time.Time,
	withdrawnAt *time.Time,
	now time.Time,
) (PrivateAlphaEnrollment, error) {
	_, err := transaction.Exec(ctx, privateAlphaEnrollmentUpsertSQL,
		tenantContext.TenantID, PrivateAlphaProgramClassroomWhiteboards, status, revision,
		PrivateAlphaNoticeVersion, acceptedAt, withdrawnAt,
		tenantContext.ActorID, now,
	)
	if err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	return newPrivateAlphaEnrollment(
		tenantContext.TenantID, status, revision,
		acceptedAt, withdrawnAt, now, true,
	), nil
}

func newPrivateAlphaEnrollment(
	tenantID uuid.UUID,
	status PrivateAlphaEnrollmentStatus,
	revision int64,
	acceptedAt *time.Time,
	withdrawnAt *time.Time,
	now time.Time,
	canManage bool,
) PrivateAlphaEnrollment {
	return PrivateAlphaEnrollment{
		TenantID:       tenantID,
		Program:        PrivateAlphaProgramClassroomWhiteboards,
		Status:         status,
		Revision:       revision,
		NoticeVersion:  PrivateAlphaNoticeVersion,
		AcceptedAt:     acceptedAt,
		WithdrawnAt:    withdrawnAt,
		AllowedActions: PrivateAlphaEnrollmentAllowedActions{ManageEnrollment: canManage},
	}
}

func readPrivateAlphaEnrollment(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	canManage bool,
	forUpdate bool,
) (PrivateAlphaEnrollment, error) {
	query := privateAlphaEnrollmentSelectSQL
	if forUpdate {
		query += ` FOR UPDATE`
	}
	enrollment := newPrivateAlphaEnrollment(
		tenantID, PrivateAlphaEnrollmentNotEnrolled,
		0, nil, nil, time.Time{}, canManage,
	)
	return scanPrivateAlphaEnrollment(ctx, transaction, query, enrollment)
}

func scanPrivateAlphaEnrollment(
	ctx context.Context,
	transaction pgx.Tx,
	query string,
	enrollment PrivateAlphaEnrollment,
) (PrivateAlphaEnrollment, error) {
	var updatedAt time.Time
	err := transaction.QueryRow(
		ctx, query, enrollment.TenantID, PrivateAlphaProgramClassroomWhiteboards,
	).Scan(
		&enrollment.Status, &enrollment.Revision, &enrollment.NoticeVersion,
		&enrollment.AcceptedAt, &enrollment.WithdrawnAt, &updatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return enrollment, nil
	}
	if err != nil {
		return PrivateAlphaEnrollment{}, err
	}
	return enrollment, nil
}
