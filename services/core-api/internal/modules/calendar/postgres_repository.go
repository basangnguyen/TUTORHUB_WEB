package calendar

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const defaultQueryTimeout = 10 * time.Second

type transactionDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     transactionDatabase
	queryTimeout time.Duration
	authorizer   policy.Authorizer
}

type scopeDefaults struct {
	OrganizationRole policy.OrganizationRole
	Timezone         string
	Locale           string
	UpdatedAt        time.Time
}

func NewPostgresRepository(
	database transactionDatabase,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
) (*PostgresRepository, error) {
	if database == nil || authorizer == nil {
		return nil, fmt.Errorf("calendar database and policy authorizer are required")
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer,
	}, nil
}

func (repository *PostgresRepository) ListItems(
	ctx context.Context,
	scope tenancy.Context,
	params listParams,
) ([]Item, bool, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, false, fmt.Errorf("begin calendar item query: %w", err)
	}
	defer rollback(transaction)
	defaults, err := repository.authorizeScope(queryContext, transaction, scope)
	if err != nil {
		return nil, false, err
	}
	subject := policy.Subject{
		ActorID: scope.ActorID, ActiveTenantID: scope.TenantID, MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{defaults.OrganizationRole},
	}
	includeAll := repository.authorizer.Authorize(policy.Input{
		Subject:  subject,
		Action:   policy.ActionClassView,
		Resource: policy.Resource{TenantID: scope.TenantID, State: policy.ResourceStateUnknown},
	}).Allowed

	arguments := []any{
		scope.TenantID, scope.ActorID, includeAll, params.From.UTC(), params.To.UTC(),
	}
	var query strings.Builder
	query.WriteString(`SELECT
    session.id,
    session.class_id,
    session.title,
    session.starts_at,
    session.ends_at,
    session.timezone,
    session.status,
    session.version,
    class.title,
    class.status,
    class.owner_user_id,
    enrollment.class_role,
    enrollment.status
FROM tutorhub.class_sessions AS session
JOIN tutorhub.classes AS class
  ON class.tenant_id = session.tenant_id
 AND class.id = session.class_id
LEFT JOIN tutorhub.class_enrollments AS enrollment
  ON enrollment.tenant_id = session.tenant_id
 AND enrollment.class_id = session.class_id
 AND enrollment.user_id = $2
WHERE session.tenant_id = $1
  AND session.starts_at < $5
  AND session.ends_at > $4
  AND (
      $3::boolean
      OR class.owner_user_id = $2
      OR enrollment.status = 'active'
  )`)
	if len(params.ClassIDs) > 0 {
		arguments = append(arguments, params.ClassIDs)
		query.WriteString(fmt.Sprintf(" AND session.class_id = ANY($%d::uuid[])", len(arguments)))
	}
	if len(params.Statuses) > 0 {
		arguments = append(arguments, params.Statuses)
		query.WriteString(fmt.Sprintf(" AND session.status = ANY($%d::text[])", len(arguments)))
	}
	if params.Search != "" {
		arguments = append(arguments, params.Search)
		query.WriteString(fmt.Sprintf(
			" AND position($%d in lower(session.title || ' ' || class.title || ' ' || class.code)) > 0",
			len(arguments),
		))
	}
	if !params.After.StartsAt.IsZero() {
		occurrenceID, parseErr := uuid.Parse(params.After.OccurrenceKey)
		if parseErr != nil || occurrenceID == uuid.Nil {
			return nil, false, ErrInvalidCursor
		}
		arguments = append(arguments, params.After.StartsAt.UTC(), occurrenceID)
		query.WriteString(fmt.Sprintf(
			" AND (session.starts_at, session.id) > ($%d::timestamptz, $%d::uuid)",
			len(arguments)-1,
			len(arguments),
		))
	}
	arguments = append(arguments, params.Limit+1)
	query.WriteString(fmt.Sprintf(" ORDER BY session.starts_at, session.id LIMIT $%d", len(arguments)))

	rows, err := transaction.Query(queryContext, query.String(), arguments...)
	if err != nil {
		return nil, false, fmt.Errorf("query calendar items: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0, params.Limit+1)
	for rows.Next() {
		item, classState, ownerID, enrollmentRole, enrollmentStatus, scanErr :=
			scanItem(rows, params.ViewerTimezone)
		if scanErr != nil {
			return nil, false, fmt.Errorf("scan calendar item: %w", scanErr)
		}
		item.ViewerCapabilities = repository.projectCapabilities(
			scope,
			defaults.OrganizationRole,
			item,
			classState,
			ownerID,
			enrollmentRole,
			enrollmentStatus,
		)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate calendar items: %w", err)
	}
	rows.Close()
	hasMore := len(items) > params.Limit
	if hasMore {
		items = items[:params.Limit]
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, false, fmt.Errorf("commit calendar item query: %w", err)
	}
	return items, hasMore, nil
}

func (repository *PostgresRepository) GetPreference(
	ctx context.Context,
	scope tenancy.Context,
) (DisplayPreference, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return DisplayPreference{}, fmt.Errorf("begin calendar preference query: %w", err)
	}
	defer rollback(transaction)
	defaults, err := repository.authorizeScope(queryContext, transaction, scope)
	if err != nil {
		return DisplayPreference{}, err
	}
	preference, err := scanPreference(transaction.QueryRow(
		queryContext,
		`SELECT
    viewer_timezone,
    locale,
    time_format,
    week_start,
    default_view,
    density,
    time_scale_minutes,
    secondary_timezone,
    version,
    updated_at
FROM tutorhub.calendar_display_preferences
WHERE tenant_id = $1 AND user_id = $2`,
		scope.TenantID,
		scope.ActorID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		preference = defaultPreference(defaults.Timezone, defaults.Locale, defaults.UpdatedAt)
	} else if err != nil {
		return DisplayPreference{}, fmt.Errorf("query calendar preference: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return DisplayPreference{}, fmt.Errorf("commit calendar preference query: %w", err)
	}
	return preference, nil
}

func (repository *PostgresRepository) UpdatePreference(
	ctx context.Context,
	scope tenancy.Context,
	input UpdatePreferenceInput,
	updatedAt time.Time,
) (DisplayPreference, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return DisplayPreference{}, fmt.Errorf("begin calendar preference update: %w", err)
	}
	defer rollback(transaction)
	if _, err := repository.authorizeScope(queryContext, transaction, scope); err != nil {
		return DisplayPreference{}, err
	}
	var row pgx.Row
	if input.ExpectedVersion == 0 {
		row = transaction.QueryRow(
			queryContext,
			`INSERT INTO tutorhub.calendar_display_preferences (
    tenant_id,
    user_id,
    viewer_timezone,
    locale,
    time_format,
    week_start,
    default_view,
    density,
    time_scale_minutes,
    secondary_timezone,
    version,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1, $11, $11)
ON CONFLICT (tenant_id, user_id) DO NOTHING
RETURNING
    viewer_timezone, locale, time_format, week_start, default_view,
    density, time_scale_minutes, secondary_timezone, version, updated_at`,
			scope.TenantID,
			scope.ActorID,
			input.ViewerTimezone,
			input.Locale,
			input.TimeFormat,
			input.WeekStart,
			input.DefaultView,
			input.Density,
			input.TimeScaleMinutes,
			input.SecondaryTimezone,
			updatedAt.UTC(),
		)
	} else {
		row = transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.calendar_display_preferences
SET viewer_timezone = $3,
    locale = $4,
    time_format = $5,
    week_start = $6,
    default_view = $7,
    density = $8,
    time_scale_minutes = $9,
    secondary_timezone = $10,
    version = version + 1,
    updated_at = GREATEST($11, updated_at)
WHERE tenant_id = $1
  AND user_id = $2
  AND version = $12
RETURNING
    viewer_timezone, locale, time_format, week_start, default_view,
    density, time_scale_minutes, secondary_timezone, version, updated_at`,
			scope.TenantID,
			scope.ActorID,
			input.ViewerTimezone,
			input.Locale,
			input.TimeFormat,
			input.WeekStart,
			input.DefaultView,
			input.Density,
			input.TimeScaleMinutes,
			input.SecondaryTimezone,
			updatedAt.UTC(),
			input.ExpectedVersion,
		)
	}
	preference, err := scanPreference(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return DisplayPreference{}, ErrConflict
	}
	if err != nil {
		return DisplayPreference{}, fmt.Errorf("update calendar preference: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return DisplayPreference{}, fmt.Errorf("commit calendar preference update: %w", err)
	}
	return preference, nil
}

func (repository *PostgresRepository) authorizeScope(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
) (scopeDefaults, error) {
	if err := scope.Validate(); err != nil {
		return scopeDefaults{}, ErrAccessDenied
	}
	var defaults scopeDefaults
	err := transaction.QueryRow(
		ctx,
		`SELECT membership.role, user_account.timezone, user_account.locale, user_account.updated_at
FROM tutorhub.tenants AS tenant
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = tenant.id
 AND membership.user_id = $2
JOIN tutorhub.users AS user_account ON user_account.id = membership.user_id
WHERE tenant.id = $1
  AND tenant.status = 'active'
  AND membership.status = 'active'
  AND user_account.status = 'active'
FOR SHARE OF tenant, membership, user_account`,
		scope.TenantID,
		scope.ActorID,
	).Scan(
		&defaults.OrganizationRole,
		&defaults.Timezone,
		&defaults.Locale,
		&defaults.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return scopeDefaults{}, ErrAccessDenied
	}
	if err != nil {
		return scopeDefaults{}, fmt.Errorf("authorize calendar scope: %w", err)
	}
	return defaults, nil
}

func (repository *PostgresRepository) projectCapabilities(
	scope tenancy.Context,
	organizationRole policy.OrganizationRole,
	item Item,
	classState policy.ResourceState,
	ownerID uuid.UUID,
	enrollmentRole sql.NullString,
	enrollmentStatus sql.NullString,
) ViewerCapabilities {
	classRoles := make([]policy.ClassRole, 0, 2)
	if enrollmentStatus.Valid && enrollmentStatus.String == "active" && enrollmentRole.Valid {
		classRoles = append(classRoles, policy.ClassRole(enrollmentRole.String))
	}
	if ownerID == scope.ActorID {
		classRoles = append(classRoles, policy.ClassRoleOwner)
	}
	allowed := repository.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: scope.ActorID, ActiveTenantID: scope.TenantID, MembershipActive: true,
			OrganizationRoles: []policy.OrganizationRole{organizationRole},
			ClassRoles:        classRoles,
		},
		Action: policy.ActionSessionSchedule,
		Resource: policy.Resource{
			TenantID: scope.TenantID, ClassID: item.ClassID, State: classState,
		},
	}).Allowed && item.Status == "scheduled"
	return ViewerCapabilities{
		CanView: true, CanEdit: allowed, CanCancel: allowed, CanReschedule: allowed,
	}
}

type rowScanner interface {
	Scan(...any) error
}

func scanItem(
	row rowScanner,
	displayTimezone string,
) (Item, policy.ResourceState, uuid.UUID, sql.NullString, sql.NullString, error) {
	var item Item
	var sourceTimezone string
	var classState policy.ResourceState
	var ownerID uuid.UUID
	var enrollmentRole sql.NullString
	var enrollmentStatus sql.NullString
	if err := row.Scan(
		&item.SourceID,
		&item.ClassID,
		&item.Title,
		&item.StartsAt,
		&item.EndsAt,
		&sourceTimezone,
		&item.Status,
		&item.Version,
		&item.ClassTitle,
		&classState,
		&ownerID,
		&enrollmentRole,
		&enrollmentStatus,
	); err != nil {
		return Item{}, "", uuid.Nil, sql.NullString{}, sql.NullString{}, err
	}
	item.SourceType = SourceClassSession
	item.OccurrenceKey = item.SourceID.String()
	item.ID = SourceClassSession + ":" + item.OccurrenceKey
	item.StartsAt = item.StartsAt.UTC()
	item.EndsAt = item.EndsAt.UTC()
	item.DisplayTimezone = displayTimezone
	item.ColorToken = "class_session"
	return item, classState, ownerID, enrollmentRole, enrollmentStatus, nil
}

func scanPreference(row rowScanner) (DisplayPreference, error) {
	var preference DisplayPreference
	var secondaryTimezone sql.NullString
	if err := row.Scan(
		&preference.ViewerTimezone,
		&preference.Locale,
		&preference.TimeFormat,
		&preference.WeekStart,
		&preference.DefaultView,
		&preference.Density,
		&preference.TimeScaleMinutes,
		&secondaryTimezone,
		&preference.Version,
		&preference.UpdatedAt,
	); err != nil {
		return DisplayPreference{}, err
	}
	if secondaryTimezone.Valid {
		value := secondaryTimezone.String
		preference.SecondaryTimezone = &value
	}
	return preference, nil
}

func rollback(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}
