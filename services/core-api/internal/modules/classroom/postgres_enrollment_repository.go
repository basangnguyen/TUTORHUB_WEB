package classroom

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const enrollmentSelectColumns = `
    id,
    tenant_id,
    class_id,
    user_id,
    class_role,
    status,
    enrolled_by,
    joined_at,
    suspended_at,
    left_at,
    removed_at,
    created_at,
    updated_at`

const inviteCodeSelectColumns = `
    id,
    tenant_id,
    class_id,
    status,
    expires_at,
    usage_limit,
    usage_count,
    created_by,
    revoked_at,
    revoked_by,
    created_at,
    updated_at`

func (repository *PostgresRepository) FindActorEnrollment(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
) (*Enrollment, error) {
	if err := tenantContext.Validate(); err != nil || classID == uuid.Nil {
		return nil, ErrClassNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	enrollment, err := scanEnrollment(repository.database.QueryRow(
		queryContext,
		`SELECT `+prefixedEnrollmentColumns("enrollment")+`
FROM tutorhub.class_enrollments AS enrollment
JOIN tutorhub.tenants AS tenant
  ON tenant.id = enrollment.tenant_id AND tenant.status = 'active'
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = enrollment.tenant_id
 AND membership.user_id = enrollment.user_id
 AND membership.status = 'active'
WHERE enrollment.tenant_id = $1
  AND enrollment.class_id = $2
  AND enrollment.user_id = $3`,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find actor class enrollment: %w", err)
	}
	return &enrollment, nil
}

func (repository *PostgresRepository) ListActorEnrollments(
	ctx context.Context,
	tenantContext tenancy.Context,
	classIDs []uuid.UUID,
) ([]Enrollment, error) {
	if err := tenantContext.Validate(); err != nil {
		return nil, ErrEnrollmentAccessDenied
	}
	classIDs, err := normalizeEnrollmentClassIDs(classIDs)
	if err != nil {
		return nil, err
	}
	if len(classIDs) == 0 {
		return []Enrollment{}, nil
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	rows, err := repository.database.Query(
		queryContext,
		`SELECT `+prefixedEnrollmentColumns("enrollment")+`
FROM tutorhub.class_enrollments AS enrollment
JOIN tutorhub.tenants AS tenant
  ON tenant.id = enrollment.tenant_id AND tenant.status = 'active'
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = enrollment.tenant_id
 AND membership.user_id = enrollment.user_id
 AND membership.status = 'active'
WHERE enrollment.tenant_id = $1
  AND enrollment.user_id = $2
  AND enrollment.status = 'active'
  AND enrollment.class_id = ANY($3::uuid[])
ORDER BY enrollment.class_id`,
		tenantContext.TenantID,
		tenantContext.ActorID,
		classIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list actor class enrollments: %w", err)
	}
	defer rows.Close()

	enrollments := make([]Enrollment, 0, len(classIDs))
	for rows.Next() {
		enrollment, err := scanEnrollment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan actor class enrollment: %w", err)
		}
		enrollments = append(enrollments, enrollment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate actor class enrollments: %w", err)
	}
	return enrollments, nil
}

func (repository *PostgresRepository) DirectEnroll(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params DirectEnrollmentParams,
) (EnrollmentMutationResult, error) {
	if err := tenantContext.Validate(); err != nil {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	email := strings.ToLower(strings.TrimSpace(params.MemberEmail))
	if classID == uuid.Nil || email == "" || email != params.MemberEmail ||
		len(email) > 320 || params.ChangedAt.IsZero() {
		return EnrollmentMutationResult{}, ErrInvalidEnrollmentInput
	}
	changedAt := params.ChangedAt.UTC()
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("begin direct enrollment: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if _, err := lockActiveClassTenant(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	class, err := lockEnrollmentClassForShare(
		queryContext, transaction, tenantContext.TenantID, classID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	preflightMembership, found, err := lockClassMembership(
		queryContext,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
	)
	if err != nil || !found || !preflightMembership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	preflightEnrollment, err := findEnrollmentWithoutLock(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	if !repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID,
		tenantContext.TenantID,
		preflightMembership,
		preflightEnrollment,
		class,
	) {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	if class.Status != ClassStatusActive {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	targetUserID, err := lookupTenantMemberByEmail(
		queryContext, transaction, tenantContext.TenantID, email,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	memberships, err := lockClassMemberships(
		queryContext,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
		targetUserID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	actorMembership, actorFound := memberships[tenantContext.ActorID]
	targetMembership, targetFound := memberships[targetUserID]
	if !actorFound || !actorMembership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	if !targetFound || !targetMembership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	enrollments, err := lockEnrollmentUsers(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
		targetUserID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	actorEnrollment := enrollments[tenantContext.ActorID]
	if !repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID, tenantContext.TenantID, actorMembership,
		actorEnrollment, class,
	) {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	targetEnrollment := enrollments[targetUserID]
	if class.OwnerUserID == targetUserID || repository.hasEnrollmentManagerPrivilege(
		targetUserID, tenantContext.TenantID, targetMembership,
		targetEnrollment, class,
	) {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	if targetEnrollment != nil && targetEnrollment.Status == EnrollmentStatusActive {
		if err := transaction.Commit(queryContext); err != nil {
			return EnrollmentMutationResult{}, fmt.Errorf("commit unchanged direct enrollment: %w", err)
		}
		return EnrollmentMutationResult{Enrollment: *targetEnrollment}, nil
	}

	var enrollment Enrollment
	eventType := "class.enrollment.created"
	if targetEnrollment == nil {
		enrollment, err = scanEnrollment(transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by,
    joined_at, created_at, updated_at
)
VALUES ($1, $2, $3, 'student', 'active', $4, $5, $5, $5)
RETURNING `+enrollmentSelectColumns,
			tenantContext.TenantID,
			classID,
			targetUserID,
			tenantContext.ActorID,
			changedAt,
		))
	} else {
		eventType = "class.enrollment.reactivated"
		enrollment, err = scanEnrollment(transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.class_enrollments
SET class_role = 'student',
    status = 'active',
    enrolled_by = $4,
    joined_at = COALESCE(joined_at, $5),
    suspended_at = NULL,
    left_at = NULL,
    removed_at = NULL,
    updated_at = $5
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3
RETURNING `+enrollmentSelectColumns,
			tenantContext.TenantID,
			classID,
			targetUserID,
			tenantContext.ActorID,
			changedAt,
		))
	}
	if err != nil {
		return EnrollmentMutationResult{}, mapEnrollmentPostgresError("persist direct enrollment", err)
	}
	if err := insertEnrollmentEvent(
		queryContext, transaction, enrollment, eventType,
		tenantContext.ActorID, "direct", changedAt,
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("commit direct enrollment: %w", err)
	}
	return EnrollmentMutationResult{Enrollment: enrollment, Changed: true}, nil
}

func (repository *PostgresRepository) SuspendEnrollment(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	userID uuid.UUID,
	changedAt time.Time,
) (EnrollmentMutationResult, error) {
	return repository.changeManagedEnrollmentStatus(
		ctx, tenantContext, classID, userID,
		EnrollmentStatusSuspended, changedAt,
	)
}

func (repository *PostgresRepository) RemoveEnrollment(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	userID uuid.UUID,
	changedAt time.Time,
) (EnrollmentMutationResult, error) {
	return repository.changeManagedEnrollmentStatus(
		ctx, tenantContext, classID, userID,
		EnrollmentStatusRemoved, changedAt,
	)
}

func (repository *PostgresRepository) changeManagedEnrollmentStatus(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	userID uuid.UUID,
	targetStatus EnrollmentStatus,
	changedAt time.Time,
) (EnrollmentMutationResult, error) {
	if err := tenantContext.Validate(); err != nil {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	if classID == uuid.Nil || userID == uuid.Nil || changedAt.IsZero() {
		return EnrollmentMutationResult{}, ErrInvalidEnrollmentInput
	}
	if targetStatus != EnrollmentStatusSuspended && targetStatus != EnrollmentStatusRemoved {
		return EnrollmentMutationResult{}, ErrInvalidEnrollmentInput
	}
	changedAt = changedAt.UTC()
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("begin enrollment status change: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if _, err := lockActiveClassTenant(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	class, err := lockEnrollmentClassForShare(
		queryContext, transaction, tenantContext.TenantID, classID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	preflightMembership, found, err := lockClassMembership(
		queryContext,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
	)
	if err != nil || !found || !preflightMembership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	preflightEnrollment, err := findEnrollmentWithoutLock(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	if !repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID,
		tenantContext.TenantID,
		preflightMembership,
		preflightEnrollment,
		class,
	) {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	if class.Status != ClassStatusActive {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	memberships, err := lockClassMemberships(
		queryContext,
		transaction,
		tenantContext.TenantID,
		tenantContext.ActorID,
		userID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	actorMembership, actorFound := memberships[tenantContext.ActorID]
	targetMembership, targetFound := memberships[userID]
	if !actorFound || !actorMembership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	if !targetFound {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	enrollments, err := lockEnrollmentUsers(
		queryContext, transaction, tenantContext.TenantID, classID,
		tenantContext.ActorID, userID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	actorEnrollment := enrollments[tenantContext.ActorID]
	if !repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID, tenantContext.TenantID, actorMembership,
		actorEnrollment, class,
	) {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	targetEnrollment := enrollments[userID]
	if targetEnrollment == nil {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	mutationAction := policy.RosterMutationRemove
	if targetStatus == EnrollmentStatusSuspended {
		mutationAction = policy.RosterMutationSuspend
		if !targetMembership.Active {
			return EnrollmentMutationResult{}, ErrEnrollmentConflict
		}
	}
	if err := repository.authorizeRosterTargetMutation(
		tenantContext,
		class,
		actorMembership,
		actorEnrollment,
		userID,
		targetMembership,
		targetEnrollment,
		mutationAction,
		"",
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	if targetEnrollment.Status == targetStatus {
		if err := transaction.Commit(queryContext); err != nil {
			return EnrollmentMutationResult{}, fmt.Errorf("commit unchanged enrollment status: %w", err)
		}
		return EnrollmentMutationResult{Enrollment: *targetEnrollment}, nil
	}

	var query string
	eventType := "class.enrollment.suspended"
	switch targetStatus {
	case EnrollmentStatusSuspended:
		if targetEnrollment.Status != EnrollmentStatusActive {
			return EnrollmentMutationResult{}, ErrEnrollmentConflict
		}
		query = `UPDATE tutorhub.class_enrollments
SET status = 'suspended', suspended_at = $4, left_at = NULL,
    removed_at = NULL, updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3
RETURNING ` + enrollmentSelectColumns
	case EnrollmentStatusRemoved:
		if targetEnrollment.Status != EnrollmentStatusActive &&
			targetEnrollment.Status != EnrollmentStatusSuspended &&
			targetEnrollment.Status != EnrollmentStatusLeft {
			return EnrollmentMutationResult{}, ErrEnrollmentConflict
		}
		eventType = "class.enrollment.removed"
		query = `UPDATE tutorhub.class_enrollments
SET status = 'removed', suspended_at = NULL, left_at = NULL,
    removed_at = $4, updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3
RETURNING ` + enrollmentSelectColumns
	}
	enrollment, err := scanEnrollment(transaction.QueryRow(
		queryContext, query,
		tenantContext.TenantID, classID, userID, changedAt,
	))
	if err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("change class enrollment status: %w", err)
	}
	if err := insertEnrollmentEvent(
		queryContext, transaction, enrollment, eventType,
		tenantContext.ActorID, "manager", changedAt,
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("commit enrollment status change: %w", err)
	}
	return EnrollmentMutationResult{Enrollment: enrollment, Changed: true}, nil
}

func (repository *PostgresRepository) CreateInviteCode(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params CreateInviteCodeParams,
) (ClassInviteCode, error) {
	if err := tenantContext.Validate(); err != nil {
		return ClassInviteCode{}, ErrEnrollmentAccessDenied
	}
	createdAt := params.CreatedAt.UTC()
	expiresAt := params.ExpiresAt.UTC()
	if classID == uuid.Nil || len(params.CodeHash) != 32 || createdAt.IsZero() ||
		expiresAt.IsZero() || params.UsageLimit < 1 || params.UsageLimit > 1000 ||
		expiresAt.Sub(createdAt) < 15*time.Minute ||
		expiresAt.Sub(createdAt) > 30*24*time.Hour {
		return ClassInviteCode{}, ErrInvalidEnrollmentInput
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ClassInviteCode{}, fmt.Errorf("begin class invite code creation: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	class, actorMembership, actorEnrollment, err := repository.lockEnrollmentManagerContext(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return ClassInviteCode{}, err
	}
	if !repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID, tenantContext.TenantID,
		actorMembership, actorEnrollment, class,
	) {
		return ClassInviteCode{}, ErrEnrollmentAccessDenied
	}
	if class.Status != ClassStatusActive {
		return ClassInviteCode{}, ErrClassInviteCodeConflict
	}
	code, err := scanInviteCode(transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.class_invite_codes (
    tenant_id, class_id, code_hash, status, expires_at,
    usage_limit, usage_count, created_by, created_at, updated_at
)
VALUES ($1, $2, $3, 'active', $4, $5, 0, $6, $7, $7)
RETURNING `+inviteCodeSelectColumns,
		tenantContext.TenantID,
		classID,
		params.CodeHash,
		expiresAt,
		params.UsageLimit,
		tenantContext.ActorID,
		createdAt,
	))
	if err != nil {
		return ClassInviteCode{}, mapEnrollmentPostgresError("create class invite code", err)
	}
	if err := insertInviteCodeEvent(
		queryContext, transaction, code, "class.invite_code.created",
		tenantContext.ActorID, createdAt,
	); err != nil {
		return ClassInviteCode{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ClassInviteCode{}, fmt.Errorf("commit class invite code creation: %w", err)
	}
	return code, nil
}

func (repository *PostgresRepository) ListInviteCodes(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	now time.Time,
) ([]ClassInviteCode, error) {
	if err := tenantContext.Validate(); err != nil || classID == uuid.Nil || now.IsZero() {
		return nil, ErrEnrollmentAccessDenied
	}
	now = now.UTC()
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, fmt.Errorf("begin class invite code list: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	class, actorMembership, actorEnrollment, err := repository.lockEnrollmentManagerContext(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return nil, err
	}
	if !repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID, tenantContext.TenantID,
		actorMembership, actorEnrollment, class,
	) {
		return nil, ErrEnrollmentAccessDenied
	}
	if err := expireClassInviteCodes(
		queryContext, transaction, tenantContext.TenantID, classID, now,
	); err != nil {
		return nil, err
	}
	rows, err := transaction.Query(
		queryContext,
		`SELECT `+inviteCodeSelectColumns+`
FROM tutorhub.class_invite_codes
WHERE tenant_id = $1 AND class_id = $2
ORDER BY created_at DESC, id DESC
LIMIT 100`,
		tenantContext.TenantID,
		classID,
	)
	if err != nil {
		return nil, fmt.Errorf("list class invite codes: %w", err)
	}
	defer rows.Close()
	codes := make([]ClassInviteCode, 0)
	for rows.Next() {
		code, err := scanInviteCode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan class invite code: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate class invite codes: %w", err)
	}
	rows.Close()
	if err := transaction.Commit(queryContext); err != nil {
		return nil, fmt.Errorf("commit class invite code list: %w", err)
	}
	return codes, nil
}

func (repository *PostgresRepository) RevokeInviteCode(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	codeID uuid.UUID,
	now time.Time,
) (ClassInviteCode, error) {
	if err := tenantContext.Validate(); err != nil || classID == uuid.Nil ||
		codeID == uuid.Nil || now.IsZero() {
		return ClassInviteCode{}, ErrClassInviteCodeUnavailable
	}
	now = now.UTC()
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ClassInviteCode{}, fmt.Errorf("begin class invite code revoke: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	class, actorMembership, actorEnrollment, err := repository.lockEnrollmentManagerContext(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return ClassInviteCode{}, err
	}
	if !repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID, tenantContext.TenantID,
		actorMembership, actorEnrollment, class,
	) {
		return ClassInviteCode{}, ErrEnrollmentAccessDenied
	}
	code, err := lockInviteCodeByID(
		queryContext, transaction, tenantContext.TenantID, classID, codeID,
	)
	if err != nil {
		return ClassInviteCode{}, err
	}
	if code.Status == ClassInviteCodeStatusActive && !now.Before(code.ExpiresAt) {
		code, err = expireLockedInviteCode(queryContext, transaction, code, now)
		if err != nil {
			return ClassInviteCode{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return ClassInviteCode{}, fmt.Errorf("commit expired class invite code: %w", err)
		}
		return ClassInviteCode{}, ErrClassInviteCodeConflict
	}
	if code.Status == ClassInviteCodeStatusRevoked {
		if err := transaction.Commit(queryContext); err != nil {
			return ClassInviteCode{}, fmt.Errorf("commit repeated class invite revoke: %w", err)
		}
		return code, nil
	}
	if code.Status != ClassInviteCodeStatusActive {
		return ClassInviteCode{}, ErrClassInviteCodeConflict
	}
	code, err = scanInviteCode(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.class_invite_codes
SET status = 'revoked', revoked_at = $4, revoked_by = $5, updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND status = 'active'
RETURNING `+inviteCodeSelectColumns,
		tenantContext.TenantID,
		classID,
		codeID,
		now,
		tenantContext.ActorID,
	))
	if err != nil {
		return ClassInviteCode{}, fmt.Errorf("revoke class invite code: %w", err)
	}
	if err := insertInviteCodeEvent(
		queryContext, transaction, code, "class.invite_code.revoked",
		tenantContext.ActorID, now,
	); err != nil {
		return ClassInviteCode{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ClassInviteCode{}, fmt.Errorf("commit class invite code revoke: %w", err)
	}
	return code, nil
}

func (repository *PostgresRepository) JoinByInviteCode(
	ctx context.Context,
	tenantContext tenancy.Context,
	codeHash []byte,
	now time.Time,
) (JoinClassInvitationResult, error) {
	if err := tenantContext.Validate(); err != nil || len(codeHash) != 32 || now.IsZero() {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	now = now.UTC()
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return JoinClassInvitationResult{}, fmt.Errorf("begin class invite code join: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	classID, err := lookupInviteCodeScope(
		queryContext, transaction, tenantContext.TenantID, codeHash,
	)
	if err != nil {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	if _, err := lockActiveClassTenant(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	class, err := lockEnrollmentClassForShare(
		queryContext, transaction, tenantContext.TenantID, classID,
	)
	if err != nil || class.Status != ClassStatusActive {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	actorMembership, found, err := lockClassMembership(
		queryContext, transaction, tenantContext.TenantID, tenantContext.ActorID,
	)
	if err != nil || !found || !actorMembership.Active {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	enrollments, err := lockEnrollmentUsers(
		queryContext, transaction, tenantContext.TenantID, classID,
		tenantContext.ActorID, tenantContext.ActorID,
	)
	if err != nil {
		return JoinClassInvitationResult{}, err
	}
	actorEnrollment := enrollments[tenantContext.ActorID]
	if repository.hasEnrollmentManagerPrivilege(
		tenantContext.ActorID, tenantContext.TenantID,
		actorMembership, actorEnrollment, class,
	) {
		if err := transaction.Commit(queryContext); err != nil {
			return JoinClassInvitationResult{}, fmt.Errorf("commit privileged class invite join: %w", err)
		}
		return JoinClassInvitationResult{Class: class}, nil
	}
	if actorEnrollment != nil {
		switch actorEnrollment.Status {
		case EnrollmentStatusActive:
			if err := transaction.Commit(queryContext); err != nil {
				return JoinClassInvitationResult{}, fmt.Errorf("commit repeated class invite join: %w", err)
			}
			copy := *actorEnrollment
			return JoinClassInvitationResult{Class: class, Enrollment: &copy}, nil
		case EnrollmentStatusInvited, EnrollmentStatusLeft:
			if actorEnrollment.ClassRole != policy.ClassRoleStudent {
				return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
			}
		default:
			return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
		}
	}
	code, err := lockInviteCodeByHash(
		queryContext, transaction, tenantContext.TenantID, classID, codeHash,
	)
	if err != nil {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	if code.Status == ClassInviteCodeStatusActive && !now.Before(code.ExpiresAt) {
		if _, err := expireLockedInviteCode(queryContext, transaction, code, now); err != nil {
			return JoinClassInvitationResult{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return JoinClassInvitationResult{}, fmt.Errorf("commit expired class invite join: %w", err)
		}
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	if code.Status != ClassInviteCodeStatusActive || code.UsageCount >= code.UsageLimit {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	code, err = scanInviteCode(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.class_invite_codes
SET usage_count = usage_count + 1,
    status = CASE
        WHEN usage_count + 1 = usage_limit THEN 'exhausted'
        ELSE 'active'
    END,
    updated_at = $4
WHERE tenant_id = $1
  AND class_id = $2
  AND id = $3
  AND status = 'active'
  AND expires_at > $4
  AND usage_count < usage_limit
RETURNING `+inviteCodeSelectColumns,
		tenantContext.TenantID,
		classID,
		code.ID,
		now,
	))
	if err != nil {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}

	var enrollment Enrollment
	eventType := "class.enrollment.joined"
	if actorEnrollment == nil {
		enrollment, err = scanEnrollment(transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by,
    joined_at, created_at, updated_at
)
VALUES ($1, $2, $3, 'student', 'active', $3, $4, $4, $4)
RETURNING `+enrollmentSelectColumns,
			tenantContext.TenantID,
			classID,
			tenantContext.ActorID,
			now,
		))
	} else {
		eventType = "class.enrollment.rejoined"
		enrollment, err = scanEnrollment(transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.class_enrollments
SET status = 'active',
    enrolled_by = $3,
    joined_at = COALESCE(joined_at, $4),
    suspended_at = NULL,
    left_at = NULL,
    removed_at = NULL,
    updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3
  AND status IN ('invited', 'left')
RETURNING `+enrollmentSelectColumns,
			tenantContext.TenantID,
			classID,
			tenantContext.ActorID,
			now,
		))
	}
	if err != nil {
		return JoinClassInvitationResult{}, mapEnrollmentPostgresError("persist class invite join", err)
	}
	if err := insertEnrollmentEvent(
		queryContext, transaction, enrollment, eventType,
		tenantContext.ActorID, "invite_code", now,
	); err != nil {
		return JoinClassInvitationResult{}, err
	}
	if code.Status == ClassInviteCodeStatusExhausted {
		if err := insertInviteCodeEvent(
			queryContext, transaction, code, "class.invite_code.exhausted",
			tenantContext.ActorID, now,
		); err != nil {
			return JoinClassInvitationResult{}, err
		}
	}
	if err := transaction.Commit(queryContext); err != nil {
		return JoinClassInvitationResult{}, fmt.Errorf("commit class invite join: %w", err)
	}
	copy := enrollment
	return JoinClassInvitationResult{
		Class: class, Enrollment: &copy, Joined: true,
	}, nil
}

func (repository *PostgresRepository) LeaveClass(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	now time.Time,
) (EnrollmentMutationResult, error) {
	if err := tenantContext.Validate(); err != nil || classID == uuid.Nil || now.IsZero() {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	now = now.UTC()
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("begin class leave: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if _, err := lockActiveClassTenant(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	class, err := lockEnrollmentClassForShare(
		queryContext, transaction, tenantContext.TenantID, classID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	if class.OwnerUserID == tenantContext.ActorID {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	membership, found, err := lockClassMembership(
		queryContext, transaction, tenantContext.TenantID, tenantContext.ActorID,
	)
	if err != nil || !found || !membership.Active {
		return EnrollmentMutationResult{}, ErrEnrollmentAccessDenied
	}
	enrollments, err := lockEnrollmentUsers(
		queryContext, transaction, tenantContext.TenantID, classID,
		tenantContext.ActorID, tenantContext.ActorID,
	)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	enrollment := enrollments[tenantContext.ActorID]
	if enrollment == nil {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	if enrollment.Status == EnrollmentStatusLeft {
		if err := transaction.Commit(queryContext); err != nil {
			return EnrollmentMutationResult{}, fmt.Errorf("commit repeated class leave: %w", err)
		}
		return EnrollmentMutationResult{Enrollment: *enrollment}, nil
	}
	if enrollment.Status != EnrollmentStatusActive {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	updated, err := scanEnrollment(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.class_enrollments
SET status = 'left', suspended_at = NULL, left_at = $4,
    removed_at = NULL, updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3 AND status = 'active'
RETURNING `+enrollmentSelectColumns,
		tenantContext.TenantID,
		classID,
		tenantContext.ActorID,
		now,
	))
	if err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("leave class enrollment: %w", err)
	}
	if err := insertEnrollmentEvent(
		queryContext, transaction, updated, "class.enrollment.left",
		tenantContext.ActorID, "self", now,
	); err != nil {
		return EnrollmentMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return EnrollmentMutationResult{}, fmt.Errorf("commit class leave: %w", err)
	}
	return EnrollmentMutationResult{Enrollment: updated, Changed: true}, nil
}

func (repository *PostgresRepository) lockEnrollmentManagerContext(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
) (Class, lockedClassMembership, *Enrollment, error) {
	if _, err := lockActiveClassTenant(
		ctx, transaction, tenantContext.TenantID,
	); err != nil {
		return Class{}, lockedClassMembership{}, nil, err
	}
	class, err := lockEnrollmentClassForShare(
		ctx, transaction, tenantContext.TenantID, classID,
	)
	if err != nil {
		return Class{}, lockedClassMembership{}, nil, err
	}
	membership, found, err := lockClassMembership(
		ctx, transaction, tenantContext.TenantID, tenantContext.ActorID,
	)
	if err != nil {
		return Class{}, lockedClassMembership{}, nil, err
	}
	if !found || !membership.Active {
		return Class{}, lockedClassMembership{}, nil, ErrEnrollmentAccessDenied
	}
	enrollments, err := lockEnrollmentUsers(
		ctx, transaction, tenantContext.TenantID, classID,
		tenantContext.ActorID, tenantContext.ActorID,
	)
	if err != nil {
		return Class{}, lockedClassMembership{}, nil, err
	}
	return class, membership, enrollments[tenantContext.ActorID], nil
}

func lockEnrollmentClassForShare(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
) (Class, error) {
	class, err := scanClass(transaction.QueryRow(
		ctx,
		`SELECT id, tenant_id, owner_user_id, code, title, description, timezone,
       status, version, created_at, updated_at, archived_at
FROM tutorhub.classes
WHERE tenant_id = $1 AND id = $2
FOR SHARE`,
		tenantID,
		classID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Class{}, ErrClassNotFound
	}
	if err != nil {
		return Class{}, fmt.Errorf("lock enrollment class: %w", err)
	}
	return class, nil
}

func lookupTenantMemberByEmail(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	email string,
) (uuid.UUID, error) {
	var userID uuid.UUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT membership.user_id
FROM tutorhub.memberships AS membership
JOIN tutorhub.users AS app_user ON app_user.id = membership.user_id
WHERE membership.tenant_id = $1 AND app_user.email = $2`,
		tenantID,
		email,
	).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrEnrollmentNotFound
		}
		return uuid.Nil, fmt.Errorf("find direct enrollment member: %w", err)
	}
	return userID, nil
}

func lockEnrollmentUsers(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	firstUserID uuid.UUID,
	secondUserID uuid.UUID,
) (map[uuid.UUID]*Enrollment, error) {
	userIDs := []uuid.UUID{firstUserID}
	if secondUserID != firstUserID {
		userIDs = append(userIDs, secondUserID)
	}
	sort.Slice(userIDs, func(i, j int) bool {
		return userIDs[i].String() < userIDs[j].String()
	})
	for _, userID := range userIDs {
		key := "class-enrollment:" + tenantID.String() + ":" +
			classID.String() + ":" + userID.String()
		if _, err := transaction.Exec(
			ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			key,
		); err != nil {
			return nil, fmt.Errorf("lock class enrollment actor: %w", err)
		}
	}
	enrollments := make(map[uuid.UUID]*Enrollment, len(userIDs))
	for _, userID := range userIDs {
		enrollment, err := scanEnrollment(transaction.QueryRow(
			ctx,
			`SELECT `+enrollmentSelectColumns+`
FROM tutorhub.class_enrollments
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3
FOR UPDATE`,
			tenantID,
			classID,
			userID,
		))
		if errors.Is(err, pgx.ErrNoRows) {
			enrollments[userID] = nil
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("lock class enrollment: %w", err)
		}
		copy := enrollment
		enrollments[userID] = &copy
	}
	return enrollments, nil
}

func findEnrollmentWithoutLock(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	userID uuid.UUID,
) (*Enrollment, error) {
	enrollment, err := scanEnrollment(transaction.QueryRow(
		ctx,
		`SELECT `+enrollmentSelectColumns+`
FROM tutorhub.class_enrollments
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		tenantID,
		classID,
		userID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read class enrollment preflight: %w", err)
	}
	return &enrollment, nil
}

func (repository *PostgresRepository) hasEnrollmentManagerPrivilege(
	actorID uuid.UUID,
	tenantID uuid.UUID,
	membership lockedClassMembership,
	enrollment *Enrollment,
	class Class,
) bool {
	if repository.authorizer == nil || !membership.Active {
		return false
	}
	classRoles := make([]policy.ClassRole, 0, 2)
	if class.OwnerUserID == actorID {
		classRoles = append(classRoles, policy.ClassRoleOwner)
	}
	if enrollment != nil && enrollment.Status == EnrollmentStatusActive {
		classRoles = append(classRoles, enrollment.ClassRole)
	}
	return repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: actorID, ActiveTenantID: tenantID,
			MembershipActive:  true,
			OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRole(membership.Role)},
			ClassRoles:        classRoles,
		},
		Action: policy.ActionEnrollmentManage,
		Resource: policy.Resource{
			TenantID: class.TenantID, ClassID: class.ID,
			// This helper answers whether the actor owns the management
			// permission. Each repository operation enforces its own lifecycle:
			// create/join/direct/suspend/remove require active, while list/revoke
			// remain available after archive.
			State: policy.ResourceStateActive,
		},
	}).Allowed
}

func lookupInviteCodeScope(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	codeHash []byte,
) (uuid.UUID, error) {
	var classID uuid.UUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT class_id
FROM tutorhub.class_invite_codes
	WHERE tenant_id = $1 AND code_hash = $2`,
		tenantID,
		codeHash,
	).Scan(&classID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrClassInviteCodeUnavailable
		}
		return uuid.Nil, fmt.Errorf("locate class invite code: %w", err)
	}
	return classID, nil
}

func lockInviteCodeByID(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	codeID uuid.UUID,
) (ClassInviteCode, error) {
	code, err := scanInviteCode(transaction.QueryRow(
		ctx,
		`SELECT `+inviteCodeSelectColumns+`
FROM tutorhub.class_invite_codes
WHERE tenant_id = $1 AND class_id = $2 AND id = $3
FOR UPDATE`,
		tenantID,
		classID,
		codeID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassInviteCode{}, ErrClassInviteCodeUnavailable
	}
	if err != nil {
		return ClassInviteCode{}, fmt.Errorf("lock class invite code: %w", err)
	}
	return code, nil
}

func lockInviteCodeByHash(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	codeHash []byte,
) (ClassInviteCode, error) {
	code, err := scanInviteCode(transaction.QueryRow(
		ctx,
		`SELECT `+inviteCodeSelectColumns+`
FROM tutorhub.class_invite_codes
WHERE tenant_id = $1 AND class_id = $2 AND code_hash = $3
FOR UPDATE`,
		tenantID,
		classID,
		codeHash,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassInviteCode{}, ErrClassInviteCodeUnavailable
	}
	if err != nil {
		return ClassInviteCode{}, fmt.Errorf("lock class invite code token: %w", err)
	}
	return code, nil
}

func expireClassInviteCodes(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	now time.Time,
) error {
	rows, err := transaction.Query(
		ctx,
		`SELECT `+inviteCodeSelectColumns+`
FROM tutorhub.class_invite_codes
WHERE tenant_id = $1 AND class_id = $2
  AND status = 'active' AND expires_at <= $3
ORDER BY id
FOR UPDATE`,
		tenantID,
		classID,
		now,
	)
	if err != nil {
		return fmt.Errorf("lock expired class invite codes: %w", err)
	}
	codes := make([]ClassInviteCode, 0)
	for rows.Next() {
		code, err := scanInviteCode(rows)
		if err != nil {
			rows.Close()
			return fmt.Errorf("scan expired class invite code: %w", err)
		}
		codes = append(codes, code)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate expired class invite codes: %w", err)
	}
	rows.Close()
	for _, code := range codes {
		if _, err := expireLockedInviteCode(ctx, transaction, code, now); err != nil {
			return err
		}
	}
	return nil
}

func expireLockedInviteCode(
	ctx context.Context,
	transaction pgx.Tx,
	code ClassInviteCode,
	now time.Time,
) (ClassInviteCode, error) {
	if code.Status != ClassInviteCodeStatusActive {
		return code, nil
	}
	updated, err := scanInviteCode(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.class_invite_codes
SET status = 'expired', updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND status = 'active'
RETURNING `+inviteCodeSelectColumns,
		code.TenantID,
		code.ClassID,
		code.ID,
		now,
	))
	if err != nil {
		return ClassInviteCode{}, fmt.Errorf("expire class invite code: %w", err)
	}
	if err := insertInviteCodeEvent(
		ctx, transaction, updated, "class.invite_code.expired", uuid.Nil, now,
	); err != nil {
		return ClassInviteCode{}, err
	}
	return updated, nil
}

func insertEnrollmentEvent(
	ctx context.Context,
	transaction pgx.Tx,
	enrollment Enrollment,
	eventType string,
	actorID uuid.UUID,
	source string,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'class_enrollment', $2, $3,
    jsonb_strip_nulls(jsonb_build_object(
        'class_id', $4::uuid,
        'user_id', $5::uuid,
        'actor_user_id', $6::uuid,
        'class_role', $7::text,
        'status', $8::text,
        'source', $9::text
    )),
    $10, $10
)`,
		enrollment.TenantID,
		enrollment.ID,
		eventType,
		enrollment.ClassID,
		enrollment.UserID,
		nullableClassUUID(actorID),
		string(enrollment.ClassRole),
		string(enrollment.Status),
		source,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID:      enrollment.TenantID,
		ActorID:       actorID,
		EventType:     eventType,
		AggregateType: "class_enrollment",
		AggregateID:   enrollment.ID,
		Metadata: audit.Metadata{
			"effect":                      enrollmentEventEffect(eventType),
			"class_role":                  string(enrollment.ClassRole),
			"status":                      string(enrollment.Status),
			"source":                      source,
			audit.MetadataKeyTargetUserID: enrollment.UserID.String(),
		},
		OccurredAt: occurredAt,
	}); err != nil {
		return fmt.Errorf("insert %s audit event: %w", eventType, err)
	}
	return nil
}

func insertInviteCodeEvent(
	ctx context.Context,
	transaction pgx.Tx,
	code ClassInviteCode,
	eventType string,
	actorID uuid.UUID,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'class_invite_code', $2, $3,
    jsonb_strip_nulls(jsonb_build_object(
        'class_id', $4::uuid,
        'actor_user_id', $5::uuid,
        'status', $6::text,
        'expires_at', $7::timestamptz,
        'usage_limit', $8::integer,
        'usage_count', $9::integer
    )),
    $10, $10
)`,
		code.TenantID,
		code.ID,
		eventType,
		code.ClassID,
		nullableClassUUID(actorID),
		string(code.Status),
		code.ExpiresAt,
		code.UsageLimit,
		code.UsageCount,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID:      code.TenantID,
		ActorID:       actorID,
		EventType:     eventType,
		AggregateType: "class_invite_code",
		AggregateID:   code.ID,
		Metadata: audit.Metadata{
			"effect":      inviteCodeEventEffect(eventType),
			"status":      string(code.Status),
			"usage_limit": fmt.Sprintf("%d", code.UsageLimit),
			"usage_count": fmt.Sprintf("%d", code.UsageCount),
		},
		OccurredAt: occurredAt,
	}); err != nil {
		return fmt.Errorf("insert %s audit event: %w", eventType, err)
	}
	return nil
}

func enrollmentEventEffect(eventType string) string {
	switch eventType {
	case "class.enrollment.created":
		return "created"
	case "class.enrollment.reactivated":
		return "reactivated"
	case "class.enrollment.suspended":
		return "suspended"
	case "class.enrollment.removed":
		return "removed"
	case "class.enrollment.joined":
		return "joined"
	case "class.enrollment.rejoined":
		return "rejoined"
	case "class.enrollment.left":
		return "left"
	default:
		return "updated"
	}
}

func inviteCodeEventEffect(eventType string) string {
	switch eventType {
	case "class.invite_code.created":
		return "created"
	case "class.invite_code.revoked":
		return "revoked"
	case "class.invite_code.expired":
		return "expired"
	case "class.invite_code.exhausted":
		return "exhausted"
	default:
		return "updated"
	}
}

type enrollmentRowScanner interface {
	Scan(...any) error
}

func scanEnrollment(row enrollmentRowScanner) (Enrollment, error) {
	var enrollment Enrollment
	if err := row.Scan(
		&enrollment.ID,
		&enrollment.TenantID,
		&enrollment.ClassID,
		&enrollment.UserID,
		&enrollment.ClassRole,
		&enrollment.Status,
		&enrollment.EnrolledBy,
		&enrollment.JoinedAt,
		&enrollment.SuspendedAt,
		&enrollment.LeftAt,
		&enrollment.RemovedAt,
		&enrollment.CreatedAt,
		&enrollment.UpdatedAt,
	); err != nil {
		return Enrollment{}, err
	}
	return enrollment, nil
}

func scanInviteCode(row enrollmentRowScanner) (ClassInviteCode, error) {
	var code ClassInviteCode
	var revokedBy uuid.NullUUID
	if err := row.Scan(
		&code.ID,
		&code.TenantID,
		&code.ClassID,
		&code.Status,
		&code.ExpiresAt,
		&code.UsageLimit,
		&code.UsageCount,
		&code.CreatedBy,
		&code.RevokedAt,
		&revokedBy,
		&code.CreatedAt,
		&code.UpdatedAt,
	); err != nil {
		return ClassInviteCode{}, err
	}
	if revokedBy.Valid {
		value := revokedBy.UUID
		code.RevokedBy = &value
	}
	return code, nil
}

func prefixedEnrollmentColumns(alias string) string {
	columns := []string{
		"id", "tenant_id", "class_id", "user_id", "class_role", "status",
		"enrolled_by", "joined_at", "suspended_at", "left_at", "removed_at",
		"created_at", "updated_at",
	}
	for index, column := range columns {
		columns[index] = alias + "." + column
	}
	return strings.Join(columns, ", ")
}

func normalizeEnrollmentClassIDs(classIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(classIDs) > maximumListLimit {
		return nil, ErrInvalidEnrollmentInput
	}
	seen := make(map[uuid.UUID]struct{}, len(classIDs))
	normalized := make([]uuid.UUID, 0, len(classIDs))
	for _, classID := range classIDs {
		if classID == uuid.Nil {
			return nil, ErrInvalidEnrollmentInput
		}
		if _, exists := seen[classID]; exists {
			continue
		}
		seen[classID] = struct{}{}
		normalized = append(normalized, classID)
	}
	return normalized, nil
}

func mapEnrollmentPostgresError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch postgresError.ConstraintName {
	case "class_invite_codes_hash_unique":
		return ErrClassInviteCodeConflict
	case "class_enrollments_tenant_class_user_unique":
		return ErrEnrollmentConflict
	case "class_enrollments_user_membership_fk",
		"class_enrollments_actor_membership_fk":
		return ErrEnrollmentNotFound
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
