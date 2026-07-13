package classroom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	defaultListLimit = 50
	maximumListLimit = 100
)

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresRepository struct {
	database     DBTX
	queryTimeout time.Duration
}

func NewPostgresRepository(database DBTX, queryTimeout time.Duration) *PostgresRepository {
	return &PostgresRepository{database: database, queryTimeout: queryTimeout}
}

func (repository *PostgresRepository) Create(
	ctx context.Context,
	tenantContext tenancy.Context,
	params CreateClassParams,
) (Class, error) {
	if err := tenantContext.Validate(); err != nil {
		return Class{}, err
	}
	params, err := params.normalized()
	if err != nil {
		return Class{}, err
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	const query = `
WITH inserted_class AS (
    INSERT INTO tutorhub.classes (
        tenant_id,
        owner_user_id,
        code,
        title,
        description
    )
    VALUES ($1, $2, $3, $4, $5)
    RETURNING
        id,
        tenant_id,
        owner_user_id,
        code,
        title,
        description,
        status,
        created_at,
        updated_at,
        archived_at
), inserted_event AS (
    INSERT INTO tutorhub.outbox_events (
        tenant_id,
        aggregate_type,
        aggregate_id,
        event_type,
        payload
    )
    SELECT
        tenant_id,
        'class',
        id,
        'class.created',
        jsonb_build_object(
            'class_id', id,
            'tenant_id', tenant_id,
            'code', code,
            'title', title,
            'status', status
        )
    FROM inserted_class
)
SELECT
    id,
    tenant_id,
    owner_user_id,
    code,
    title,
    description,
    status,
    created_at,
    updated_at,
    archived_at
FROM inserted_class`

	class, err := scanClass(repository.database.QueryRow(
		queryContext,
		query,
		tenantContext.TenantID,
		params.OwnerUserID,
		params.Code,
		params.Title,
		params.Description,
	))
	if err != nil {
		return Class{}, mapPostgresError(err)
	}

	return class, nil
}

func (repository *PostgresRepository) Get(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
) (Class, error) {
	if err := tenantContext.Validate(); err != nil {
		return Class{}, err
	}
	if classID == uuid.Nil {
		return Class{}, ErrClassNotFound
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	const query = `
SELECT
    id,
    tenant_id,
    owner_user_id,
    code,
    title,
    description,
    status,
    created_at,
    updated_at,
    archived_at
FROM tutorhub.classes
WHERE tenant_id = $1 AND id = $2`

	class, err := scanClass(repository.database.QueryRow(
		queryContext,
		query,
		tenantContext.TenantID,
		classID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return Class{}, ErrClassNotFound
	}
	if err != nil {
		return Class{}, fmt.Errorf("get class: %w", err)
	}

	return class, nil
}

func (repository *PostgresRepository) List(
	ctx context.Context,
	tenantContext tenancy.Context,
	limit int,
) ([]Class, error) {
	if err := tenantContext.Validate(); err != nil {
		return nil, err
	}
	if limit == 0 {
		limit = defaultListLimit
	}
	if limit < 1 || limit > maximumListLimit {
		return nil, fmt.Errorf("class list limit must be between 1 and %d", maximumListLimit)
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	const query = `
SELECT
    id,
    tenant_id,
    owner_user_id,
    code,
    title,
    description,
    status,
    created_at,
    updated_at,
    archived_at
FROM tutorhub.classes
WHERE tenant_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2`

	rows, err := repository.database.Query(
		queryContext,
		query,
		tenantContext.TenantID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list classes: %w", err)
	}
	defer rows.Close()

	classes := make([]Class, 0)
	for rows.Next() {
		class, err := scanClass(rows)
		if err != nil {
			return nil, fmt.Errorf("scan class list: %w", err)
		}
		classes = append(classes, class)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate class list: %w", err)
	}

	return classes, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanClass(row rowScanner) (Class, error) {
	var class Class
	if err := row.Scan(
		&class.ID,
		&class.TenantID,
		&class.OwnerUserID,
		&class.Code,
		&class.Title,
		&class.Description,
		&class.Status,
		&class.CreatedAt,
		&class.UpdatedAt,
		&class.ArchivedAt,
	); err != nil {
		return Class{}, err
	}

	return class, nil
}

func (repository *PostgresRepository) contextWithTimeout(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if repository.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, repository.queryTimeout)
}

func mapPostgresError(err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("create class: %w", err)
	}

	switch postgresError.ConstraintName {
	case "classes_tenant_code_unique":
		return ErrDuplicateClassCode
	case "classes_owner_membership_fk":
		return ErrOwnerMembershipNeeded
	default:
		return fmt.Errorf("create class: %w", err)
	}
}
