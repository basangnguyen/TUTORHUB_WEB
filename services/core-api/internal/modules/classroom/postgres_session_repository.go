package classroom

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func (repository *PostgresRepository) CreateSession(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params CreateSessionParams,
	createdAt time.Time,
) (ClassSession, error) {
	if err := tenantContext.Validate(); err != nil {
		return ClassSession{}, ErrSessionAccessDenied
	}
	if classID == uuid.Nil {
		return ClassSession{}, ErrClassNotFound
	}
	params, err := params.normalized()
	if err != nil {
		return ClassSession{}, err
	}
	if params.CreatedBy != tenantContext.ActorID {
		return ClassSession{}, ErrSessionAccessDenied
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ClassSession{}, fmt.Errorf("begin class session creation: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if err := repository.requireSessionSchedulingFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return ClassSession{}, err
	}
	locked, membership, err := repository.lockClassMutation(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return ClassSession{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext, membership, locked.Class, policy.ActionSessionSchedule,
	); err != nil {
		return ClassSession{}, err
	}

	session, err := scanClassSession(transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.class_sessions (
    tenant_id, class_id, title, description, starts_at, ends_at, timezone,
    created_by, updated_by, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9, $9)
RETURNING id, tenant_id, class_id, title, description, starts_at, ends_at,
          timezone, status, version, created_by, updated_by, cancelled_at,
          cancelled_by, created_at, updated_at`,
		tenantContext.TenantID,
		classID,
		params.Title,
		params.Description,
		params.StartsAt,
		params.EndsAt,
		params.Timezone,
		params.CreatedBy,
		createdAt,
	))
	if err != nil {
		return ClassSession{}, mapSessionPostgresError("create class session", err)
	}
	if err := insertClassSessionEvent(
		queryContext,
		transaction,
		session,
		tenantContext.ActorID,
		"class_session.scheduled.v1",
		createdAt,
	); err != nil {
		return ClassSession{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ClassSession{}, fmt.Errorf("commit class session creation: %w", err)
	}
	return session, nil
}

func (repository *PostgresRepository) GetSession(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (ClassSession, error) {
	if err := tenantContext.Validate(); err != nil ||
		classID == uuid.Nil || sessionID == uuid.Nil {
		return ClassSession{}, ErrSessionNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	session, err := scanClassSession(repository.database.QueryRow(
		queryContext,
		`SELECT id, tenant_id, class_id, title, description, starts_at, ends_at,
       timezone, status, version, created_by, updated_by, cancelled_at,
       cancelled_by, created_at, updated_at
FROM tutorhub.class_sessions
WHERE tenant_id = $1 AND class_id = $2 AND id = $3`,
		tenantContext.TenantID,
		classID,
		sessionID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSession{}, ErrSessionNotFound
	}
	if err != nil {
		return ClassSession{}, fmt.Errorf("get class session: %w", err)
	}
	return session, nil
}

func (repository *PostgresRepository) ListSessions(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params ListSessionsParams,
) (ListSessionsResult, error) {
	if err := tenantContext.Validate(); err != nil || classID == uuid.Nil {
		return ListSessionsResult{}, ErrClassNotFound
	}
	if err := validateSessionListParams(params); err != nil {
		return ListSessionsResult{}, err
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	afterStartsAt := time.Time{}
	afterID := uuid.Nil
	hasAfter := params.After != nil
	if params.After != nil {
		afterStartsAt = params.After.StartsAt.UTC()
		afterID = params.After.ID
	}
	rows, err := repository.database.Query(
		queryContext,
		`SELECT id, tenant_id, class_id, title, description, starts_at, ends_at,
       timezone, status, version, created_by, updated_by, cancelled_at,
       cancelled_by, created_at, updated_at
FROM tutorhub.class_sessions
WHERE tenant_id = $1
  AND class_id = $2
  AND starts_at < $4
  AND ends_at > $3
  AND (
      NOT $5::boolean
      OR (starts_at, id) > ($6::timestamptz, $7::uuid)
  )
ORDER BY starts_at ASC, id ASC
LIMIT $8`,
		tenantContext.TenantID,
		classID,
		params.From,
		params.To,
		hasAfter,
		afterStartsAt,
		afterID,
		params.Limit+1,
	)
	if err != nil {
		return ListSessionsResult{}, fmt.Errorf("list class sessions: %w", err)
	}
	defer rows.Close()

	items := make([]ClassSession, 0, params.Limit)
	for rows.Next() {
		session, scanErr := scanClassSession(rows)
		if scanErr != nil {
			return ListSessionsResult{}, fmt.Errorf("scan class session: %w", scanErr)
		}
		items = append(items, session)
	}
	if err := rows.Err(); err != nil {
		return ListSessionsResult{}, fmt.Errorf("iterate class sessions: %w", err)
	}
	hasMore := len(items) > params.Limit
	if hasMore {
		items = items[:params.Limit]
	}
	return ListSessionsResult{Items: items, HasMore: hasMore}, nil
}

func (repository *PostgresRepository) UpdateSession(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	params UpdateSessionParams,
	updatedAt time.Time,
) (ClassSession, error) {
	if err := tenantContext.Validate(); err != nil {
		return ClassSession{}, ErrSessionAccessDenied
	}
	if classID == uuid.Nil {
		return ClassSession{}, ErrClassNotFound
	}
	if sessionID == uuid.Nil {
		return ClassSession{}, ErrSessionNotFound
	}
	params, err := params.normalized()
	if err != nil {
		return ClassSession{}, err
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ClassSession{}, fmt.Errorf("begin class session update: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if err := repository.requireSessionSchedulingFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return ClassSession{}, err
	}
	locked, membership, err := repository.lockClassMutation(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return ClassSession{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext, membership, locked.Class, policy.ActionSessionSchedule,
	); err != nil {
		return ClassSession{}, err
	}
	current, err := lockClassSession(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID,
	)
	if err != nil {
		return ClassSession{}, err
	}
	if current.Status != SessionStatusScheduled {
		return ClassSession{}, ErrInvalidSessionTransition
	}
	if current.Version != params.ExpectedVersion {
		return ClassSession{}, ErrSessionVersionConflict
	}

	session, err := scanClassSession(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.class_sessions
SET title = COALESCE($4, title),
    description = COALESCE($5, description),
    starts_at = COALESCE($6, starts_at),
    ends_at = COALESCE($7, ends_at),
    timezone = COALESCE($8, timezone),
    version = version + 1,
    updated_by = $9,
    updated_at = $10
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND version = $11
RETURNING id, tenant_id, class_id, title, description, starts_at, ends_at,
          timezone, status, version, created_by, updated_by, cancelled_at,
          cancelled_by, created_at, updated_at`,
		tenantContext.TenantID,
		classID,
		sessionID,
		nullableClassString(params.Title),
		nullableClassString(params.Description),
		nullableSessionTime(params.StartsAt),
		nullableSessionTime(params.EndsAt),
		nullableClassString(params.Timezone),
		tenantContext.ActorID,
		updatedAt,
		params.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSession{}, ErrSessionVersionConflict
	}
	if err != nil {
		return ClassSession{}, mapSessionPostgresError("update class session", err)
	}
	if err := insertClassSessionEvent(
		queryContext,
		transaction,
		session,
		tenantContext.ActorID,
		"class_session.rescheduled.v1",
		updatedAt,
	); err != nil {
		return ClassSession{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ClassSession{}, fmt.Errorf("commit class session update: %w", err)
	}
	return session, nil
}

func (repository *PostgresRepository) CancelSession(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	params CancelSessionParams,
	cancelledAt time.Time,
) (ClassSession, error) {
	if err := tenantContext.Validate(); err != nil {
		return ClassSession{}, ErrSessionAccessDenied
	}
	if classID == uuid.Nil {
		return ClassSession{}, ErrClassNotFound
	}
	if sessionID == uuid.Nil {
		return ClassSession{}, ErrSessionNotFound
	}
	params, err := params.normalized()
	if err != nil {
		return ClassSession{}, err
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ClassSession{}, fmt.Errorf("begin class session cancellation: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if err := repository.requireSessionSchedulingFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return ClassSession{}, err
	}
	locked, membership, err := repository.lockClassMutation(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return ClassSession{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext, membership, locked.Class, policy.ActionSessionSchedule,
	); err != nil {
		return ClassSession{}, err
	}
	current, err := lockClassSession(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID,
	)
	if err != nil {
		return ClassSession{}, err
	}
	if current.Status == SessionStatusCancelled {
		if err := transaction.Commit(queryContext); err != nil {
			return ClassSession{}, fmt.Errorf(
				"commit idempotent class session cancellation: %w",
				err,
			)
		}
		return current, nil
	}
	if current.Status != SessionStatusScheduled {
		return ClassSession{}, ErrInvalidSessionTransition
	}
	if current.Version != params.ExpectedVersion {
		return ClassSession{}, ErrSessionVersionConflict
	}

	session, err := scanClassSession(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.class_sessions
SET status = 'cancelled',
    version = version + 1,
    updated_by = $4,
    cancelled_by = $4,
    cancelled_at = $5,
    updated_at = $5
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND version = $6
RETURNING id, tenant_id, class_id, title, description, starts_at, ends_at,
          timezone, status, version, created_by, updated_by, cancelled_at,
          cancelled_by, created_at, updated_at`,
		tenantContext.TenantID,
		classID,
		sessionID,
		tenantContext.ActorID,
		cancelledAt,
		params.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSession{}, ErrSessionVersionConflict
	}
	if err != nil {
		return ClassSession{}, mapSessionPostgresError("cancel class session", err)
	}
	if err := insertClassSessionEvent(
		queryContext,
		transaction,
		session,
		tenantContext.ActorID,
		"class_session.cancelled.v1",
		cancelledAt,
	); err != nil {
		return ClassSession{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ClassSession{}, fmt.Errorf("commit class session cancellation: %w", err)
	}
	return session, nil
}

func (repository *PostgresRepository) requireSessionSchedulingFeature(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
) error {
	if repository.controls == nil {
		return nil
	}
	return repository.controls.RequireFeature(
		ctx,
		transaction,
		tenantID,
		featurecontrol.FeatureClassSessionScheduling,
	)
}

func validateSessionListParams(params ListSessionsParams) error {
	if params.From.IsZero() || params.To.IsZero() ||
		!params.To.After(params.From) ||
		params.To.Sub(params.From) > maximumSessionQueryRange {
		return ErrInvalidSessionRange
	}
	if params.Limit < 1 || params.Limit > maximumSessionListLimit {
		return ErrInvalidSessionListLimit
	}
	if params.After != nil &&
		(params.After.StartsAt.IsZero() || params.After.ID == uuid.Nil) {
		return ErrInvalidSessionCursor
	}
	return nil
}

func lockClassSession(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (ClassSession, error) {
	session, err := scanClassSession(transaction.QueryRow(
		ctx,
		`SELECT id, tenant_id, class_id, title, description, starts_at, ends_at,
       timezone, status, version, created_by, updated_by, cancelled_at,
       cancelled_by, created_at, updated_at
FROM tutorhub.class_sessions
WHERE tenant_id = $1 AND class_id = $2 AND id = $3
FOR UPDATE`,
		tenantID,
		classID,
		sessionID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return ClassSession{}, ErrSessionNotFound
	}
	if err != nil {
		return ClassSession{}, fmt.Errorf("lock class session: %w", err)
	}
	return session, nil
}

func scanClassSession(row rowScanner) (ClassSession, error) {
	var session ClassSession
	var cancelledBy uuid.NullUUID
	if err := row.Scan(
		&session.ID,
		&session.TenantID,
		&session.ClassID,
		&session.Title,
		&session.Description,
		&session.StartsAt,
		&session.EndsAt,
		&session.Timezone,
		&session.Status,
		&session.Version,
		&session.CreatedBy,
		&session.UpdatedBy,
		&session.CancelledAt,
		&cancelledBy,
		&session.CreatedAt,
		&session.UpdatedAt,
	); err != nil {
		return ClassSession{}, err
	}
	if cancelledBy.Valid {
		value := cancelledBy.UUID
		session.CancelledBy = &value
	}
	return session, nil
}

func insertClassSessionEvent(
	ctx context.Context,
	transaction pgx.Tx,
	session ClassSession,
	actorID uuid.UUID,
	eventType string,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'class_session', $2, $3,
    jsonb_build_object(
        'session_id', $2::uuid,
        'class_id', $4::uuid,
        'actor_user_id', $5::uuid,
        'starts_at', $6::timestamptz,
        'ends_at', $7::timestamptz,
        'timezone', $8::text,
        'status', $9::text,
        'version', $10::bigint
    ),
    $11, $11
)`,
		session.TenantID,
		session.ID,
		eventType,
		session.ClassID,
		actorID,
		session.StartsAt,
		session.EndsAt,
		session.Timezone,
		string(session.Status),
		session.Version,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID:      session.TenantID,
		ActorID:       actorID,
		EventType:     eventType,
		AggregateType: "class_session",
		AggregateID:   session.ID,
		Metadata: audit.Metadata{
			"effect":   strings.TrimSuffix(strings.TrimPrefix(eventType, "class_session."), ".v1"),
			"class_id": session.ClassID.String(),
			"status":   string(session.Status),
			"version":  fmt.Sprintf("%d", session.Version),
		},
		OccurredAt: occurredAt,
	}); err != nil {
		return fmt.Errorf("insert %s audit event: %w", eventType, err)
	}
	return nil
}

func nullableSessionTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}

func mapSessionPostgresError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch postgresError.ConstraintName {
	case "class_sessions_title_valid",
		"class_sessions_description_valid",
		"class_sessions_time_range_valid",
		"class_sessions_timezone_valid",
		"class_sessions_status_valid",
		"class_sessions_version_positive",
		"class_sessions_updated_after_created",
		"class_sessions_cancellation_consistent":
		return fmt.Errorf("%w: %s", ErrInvalidSessionInput, postgresError.ConstraintName)
	case "class_sessions_class_fk":
		return ErrClassNotFound
	case "class_sessions_creator_membership_fk",
		"class_sessions_updater_membership_fk",
		"class_sessions_canceller_membership_fk":
		return ErrSessionAccessDenied
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
