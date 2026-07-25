package notification

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const defaultQueryTimeout = 10 * time.Second

type transactionDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     transactionDatabase
	queryTimeout time.Duration
	controls     featurecontrol.Enforcer
}

func NewPostgresRepository(
	database transactionDatabase,
	queryTimeout time.Duration,
	controls featurecontrol.Enforcer,
) (*PostgresRepository, error) {
	if database == nil || controls == nil {
		return nil, fmt.Errorf("notification database and feature controls are required")
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresRepository{
		database: database, queryTimeout: queryTimeout, controls: controls,
	}, nil
}

func (repository *PostgresRepository) List(
	ctx context.Context,
	scope tenancy.Context,
	input ListInput,
	cursor listCursor,
) ([]Notification, bool, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, false, fmt.Errorf("begin notification feed query: %w", err)
	}
	defer rollback(transaction)
	if _, err := repository.requireScope(queryContext, transaction, scope); err != nil {
		return nil, false, err
	}

	arguments := []any{scope.TenantID, scope.ActorID}
	var query strings.Builder
	query.WriteString(`SELECT
    id,
    tenant_id,
    recipient_user_id,
    source_outbox_event_id,
    effect_key,
    kind,
    template_key,
    resource_type,
    resource_id,
    context,
    occurred_at,
    read_at,
    created_at
FROM tutorhub.notifications
WHERE tenant_id = $1
  AND recipient_user_id = $2
  AND kind <> 'system.worker_canary'`)
	if input.UnreadOnly {
		query.WriteString(" AND read_at IS NULL")
	}
	if !cursor.CreatedAt.IsZero() {
		arguments = append(arguments, cursor.CreatedAt.UTC(), cursor.ID)
		query.WriteString(fmt.Sprintf(
			" AND (created_at, id) < ($%d, $%d)",
			len(arguments)-1,
			len(arguments),
		))
	}
	arguments = append(arguments, input.Limit+1)
	query.WriteString(fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(arguments)))

	rows, err := transaction.Query(queryContext, query.String(), arguments...)
	if err != nil {
		return nil, false, fmt.Errorf("query notification feed: %w", err)
	}
	defer rows.Close()
	items := make([]Notification, 0, input.Limit+1)
	for rows.Next() {
		item, scanErr := scanNotification(rows)
		if scanErr != nil {
			return nil, false, fmt.Errorf("scan notification feed: %w", scanErr)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate notification feed: %w", err)
	}
	hasMore := len(items) > input.Limit
	if hasMore {
		items = items[:input.Limit]
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, false, fmt.Errorf("commit notification feed query: %w", err)
	}
	return items, hasMore, nil
}

func (repository *PostgresRepository) UnreadCount(
	ctx context.Context,
	scope tenancy.Context,
) (UnreadCount, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return UnreadCount{}, fmt.Errorf("begin unread notification count: %w", err)
	}
	defer rollback(transaction)
	if _, err := repository.requireScope(queryContext, transaction, scope); err != nil {
		return UnreadCount{}, err
	}
	var count int
	if err := transaction.QueryRow(
		queryContext,
		`SELECT count(*)
FROM (
    SELECT 1
    FROM tutorhub.notifications
    WHERE tenant_id = $1
      AND recipient_user_id = $2
      AND read_at IS NULL
      AND kind <> 'system.worker_canary'
    LIMIT $3
) AS bounded_unread`,
		scope.TenantID,
		scope.ActorID,
		maximumUnreadCount+1,
	).Scan(&count); err != nil {
		return UnreadCount{}, fmt.Errorf("count unread notifications: %w", err)
	}
	result := UnreadCount{Count: count}
	if count > maximumUnreadCount {
		result.Count = maximumUnreadCount
		result.IsCapped = true
	}
	if err := transaction.Commit(queryContext); err != nil {
		return UnreadCount{}, fmt.Errorf("commit unread notification count: %w", err)
	}
	return result, nil
}

func (repository *PostgresRepository) MarkRead(
	ctx context.Context,
	scope tenancy.Context,
	notificationID uuid.UUID,
	readAt time.Time,
) (Notification, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Notification{}, fmt.Errorf("begin mark notification read: %w", err)
	}
	defer rollback(transaction)
	if _, err := repository.requireScope(queryContext, transaction, scope); err != nil {
		return Notification{}, err
	}
	item, err := scanNotification(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.notifications
SET read_at = COALESCE(read_at, GREATEST($4, created_at))
WHERE tenant_id = $1
  AND recipient_user_id = $2
  AND id = $3
  AND kind <> 'system.worker_canary'
RETURNING
    id, tenant_id, recipient_user_id, source_outbox_event_id,
    effect_key, kind, template_key, resource_type, resource_id,
    context, occurred_at, read_at, created_at`,
		scope.TenantID,
		scope.ActorID,
		notificationID,
		readAt.UTC(),
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Notification{}, ErrNotFound
	}
	if err != nil {
		return Notification{}, fmt.Errorf("mark notification read: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Notification{}, fmt.Errorf("commit mark notification read: %w", err)
	}
	return item, nil
}

func (repository *PostgresRepository) MarkAllRead(
	ctx context.Context,
	scope tenancy.Context,
	readAt time.Time,
) (MarkAllResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MarkAllResult{}, fmt.Errorf("begin mark all notifications read: %w", err)
	}
	defer rollback(transaction)
	if _, err := repository.requireScope(queryContext, transaction, scope); err != nil {
		return MarkAllResult{}, err
	}
	command, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.notifications
SET read_at = GREATEST($3, created_at)
WHERE tenant_id = $1
  AND recipient_user_id = $2
  AND read_at IS NULL
  AND kind <> 'system.worker_canary'`,
		scope.TenantID,
		scope.ActorID,
		readAt.UTC(),
	)
	if err != nil {
		return MarkAllResult{}, fmt.Errorf("mark all notifications read: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MarkAllResult{}, fmt.Errorf("commit mark all notifications read: %w", err)
	}
	return MarkAllResult{UpdatedCount: command.RowsAffected()}, nil
}

func (repository *PostgresRepository) GetPreference(
	ctx context.Context,
	scope tenancy.Context,
) (Preference, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Preference{}, fmt.Errorf("begin notification preference query: %w", err)
	}
	defer rollback(transaction)
	timezone, err := repository.requireScope(queryContext, transaction, scope)
	if err != nil {
		return Preference{}, err
	}
	preference, err := scanPreference(transaction.QueryRow(
		queryContext,
		`SELECT
    tenant_id,
    user_id,
    in_app_enabled,
    email_enabled,
    reminder_offset_minutes,
    quiet_hours_enabled,
    quiet_hours_start,
    quiet_hours_end,
    quiet_hours_timezone,
    version,
    updated_at
FROM tutorhub.notification_preferences
WHERE tenant_id = $1 AND user_id = $2`,
		scope.TenantID,
		scope.ActorID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		preference = defaultPreference(scope, timezone)
	} else if err != nil {
		return Preference{}, fmt.Errorf("query notification preference: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Preference{}, fmt.Errorf("commit notification preference query: %w", err)
	}
	return preference, nil
}

func (repository *PostgresRepository) PutPreference(
	ctx context.Context,
	scope tenancy.Context,
	input PutPreferenceInput,
	updatedAt time.Time,
) (Preference, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return Preference{}, fmt.Errorf("begin notification preference update: %w", err)
	}
	defer rollback(transaction)
	if _, err := repository.requireScope(queryContext, transaction, scope); err != nil {
		return Preference{}, err
	}
	var row pgx.Row
	if input.ExpectedVersion == 0 {
		row = transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.notification_preferences (
    tenant_id,
    user_id,
    in_app_enabled,
    email_enabled,
    reminder_offset_minutes,
    quiet_hours_enabled,
    quiet_hours_start,
    quiet_hours_end,
    quiet_hours_timezone,
    version,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10, $10)
ON CONFLICT (tenant_id, user_id) DO NOTHING
RETURNING
    tenant_id, user_id, in_app_enabled, email_enabled,
    reminder_offset_minutes, quiet_hours_enabled, quiet_hours_start,
    quiet_hours_end, quiet_hours_timezone, version, updated_at`,
			scope.TenantID,
			scope.ActorID,
			input.InAppEnabled,
			input.EmailEnabled,
			input.ReminderOffsetMinutes,
			input.QuietHoursEnabled,
			input.QuietHoursStart,
			input.QuietHoursEnd,
			input.QuietHoursTimezone,
			updatedAt.UTC(),
		)
	} else {
		row = transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.notification_preferences
SET in_app_enabled = $3,
    email_enabled = $4,
    reminder_offset_minutes = $5,
    quiet_hours_enabled = $6,
    quiet_hours_start = $7,
    quiet_hours_end = $8,
    quiet_hours_timezone = $9,
    version = version + 1,
    updated_at = GREATEST($10, updated_at)
WHERE tenant_id = $1
  AND user_id = $2
  AND version = $11
RETURNING
    tenant_id, user_id, in_app_enabled, email_enabled,
    reminder_offset_minutes, quiet_hours_enabled, quiet_hours_start,
    quiet_hours_end, quiet_hours_timezone, version, updated_at`,
			scope.TenantID,
			scope.ActorID,
			input.InAppEnabled,
			input.EmailEnabled,
			input.ReminderOffsetMinutes,
			input.QuietHoursEnabled,
			input.QuietHoursStart,
			input.QuietHoursEnd,
			input.QuietHoursTimezone,
			updatedAt.UTC(),
			input.ExpectedVersion,
		)
	}
	preference, err := scanPreference(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Preference{}, ErrConflict
	}
	if err != nil {
		return Preference{}, fmt.Errorf("update notification preference: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return Preference{}, fmt.Errorf("commit notification preference update: %w", err)
	}
	return preference, nil
}

func (repository *PostgresRepository) requireScope(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
) (string, error) {
	if err := scope.Validate(); err != nil {
		return "", ErrAccessDenied
	}
	var timezone string
	err := transaction.QueryRow(
		ctx,
		`SELECT u.timezone
FROM tutorhub.tenants AS t
JOIN tutorhub.memberships AS m
  ON m.tenant_id = t.id AND m.user_id = $2
JOIN tutorhub.users AS u ON u.id = m.user_id
WHERE t.id = $1
  AND t.status = 'active'
  AND m.status = 'active'
FOR SHARE OF t, m, u`,
		scope.TenantID,
		scope.ActorID,
	).Scan(&timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAccessDenied
	}
	if err != nil {
		return "", fmt.Errorf("authorize notification scope: %w", err)
	}
	if err := repository.controls.RequireFeature(
		ctx,
		transaction,
		scope.TenantID,
		featurecontrol.FeatureInAppNotifications,
	); err != nil {
		return "", err
	}
	return strings.TrimSpace(timezone), nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanNotification(row rowScanner) (Notification, error) {
	var item Notification
	var resourceType sql.NullString
	var resourceID uuid.NullUUID
	var readAt pgtype.Timestamptz
	if err := row.Scan(
		&item.ID,
		&item.TenantID,
		&item.RecipientUserID,
		&item.SourceOutboxEventID,
		&item.EffectKey,
		&item.Kind,
		&item.TemplateKey,
		&resourceType,
		&resourceID,
		&item.Context,
		&item.OccurredAt,
		&readAt,
		&item.CreatedAt,
	); err != nil {
		return Notification{}, err
	}
	if resourceType.Valid {
		item.ResourceType = resourceType.String
	}
	if resourceID.Valid {
		value := resourceID.UUID
		item.ResourceID = &value
	}
	if readAt.Valid {
		value := readAt.Time
		item.ReadAt = &value
	}
	return item, nil
}

func scanPreference(row rowScanner) (Preference, error) {
	var preference Preference
	var quietStart sql.NullString
	var quietEnd sql.NullString
	if err := row.Scan(
		&preference.TenantID,
		&preference.UserID,
		&preference.InAppEnabled,
		&preference.EmailEnabled,
		&preference.ReminderOffsetMinutes,
		&preference.QuietHoursEnabled,
		&quietStart,
		&quietEnd,
		&preference.QuietHoursTimezone,
		&preference.Version,
		&preference.UpdatedAt,
	); err != nil {
		return Preference{}, err
	}
	if quietStart.Valid {
		preference.QuietHoursStart = &quietStart.String
	}
	if quietEnd.Valid {
		preference.QuietHoursEnd = &quietEnd.String
	}
	return preference, nil
}

func rollback(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}
