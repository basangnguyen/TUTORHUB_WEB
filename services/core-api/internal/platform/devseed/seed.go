package devseed

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

var (
	ErrDatabaseURLRequired = errors.New("development seed database URL is required")
	ErrEnvironmentBlocked  = errors.New("development seed is only allowed in development or test")
)

const seedSQL = `
INSERT INTO tutorhub.tenants (
    id, slug, name, locale, timezone, status, created_at, updated_at
) VALUES (
    '10000000-0000-4000-8000-000000000001',
    'tutorhub-demo',
    'Trường học mẫu TutorHub',
    'vi',
    'Asia/Ho_Chi_Minh',
    'active',
    '2026-01-01T00:00:00Z',
    '2026-01-01T00:00:00Z'
)
ON CONFLICT (slug) DO UPDATE SET
    name = EXCLUDED.name,
    locale = EXCLUDED.locale,
    timezone = EXCLUDED.timezone,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at;

INSERT INTO tutorhub.users (
    id, email, display_name, locale, timezone, status, created_at, updated_at
) VALUES
    (
        '20000000-0000-4000-8000-000000000001',
        'giangvien.demo@tutorhub.local',
        'Nguyễn Minh Anh',
        'vi',
        'Asia/Ho_Chi_Minh',
        'active',
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z'
    ),
    (
        '20000000-0000-4000-8000-000000000002',
        'hocsinh.demo@tutorhub.local',
        'Trần Gia Hân',
        'vi',
        'UTC',
        'active',
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z'
    )
ON CONFLICT (lower(email)) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    locale = EXCLUDED.locale,
    timezone = EXCLUDED.timezone,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at;

INSERT INTO tutorhub.identities (
    id,
    user_id,
    provider,
    subject,
    email_at_provider,
    email_verified,
    last_authenticated_at,
    created_at,
    updated_at
) VALUES
    (
        '30000000-0000-4000-8000-000000000001',
        '20000000-0000-4000-8000-000000000001',
        'local-seed',
        'teacher-demo',
        'giangvien.demo@tutorhub.local',
        true,
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z'
    ),
    (
        '30000000-0000-4000-8000-000000000002',
        '20000000-0000-4000-8000-000000000002',
        'local-seed',
        'student-demo',
        'hocsinh.demo@tutorhub.local',
        true,
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z'
    )
ON CONFLICT (provider, subject) DO UPDATE SET
    user_id = EXCLUDED.user_id,
    email_at_provider = EXCLUDED.email_at_provider,
    email_verified = EXCLUDED.email_verified,
    last_authenticated_at = EXCLUDED.last_authenticated_at,
    updated_at = EXCLUDED.updated_at;

INSERT INTO tutorhub.memberships (
    id, tenant_id, user_id, role, status, joined_at, created_at, updated_at
) VALUES
    (
        '40000000-0000-4000-8000-000000000001',
        '10000000-0000-4000-8000-000000000001',
        '20000000-0000-4000-8000-000000000001',
        'teacher',
        'active',
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z'
    ),
    (
        '40000000-0000-4000-8000-000000000002',
        '10000000-0000-4000-8000-000000000001',
        '20000000-0000-4000-8000-000000000002',
        'student',
        'active',
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z',
        '2026-01-01T00:00:00Z'
    )
ON CONFLICT (tenant_id, user_id) DO UPDATE SET
    role = EXCLUDED.role,
    status = EXCLUDED.status,
    joined_at = EXCLUDED.joined_at,
    updated_at = EXCLUDED.updated_at;

INSERT INTO tutorhub.classes (
    id,
    tenant_id,
    owner_user_id,
    code,
    title,
    description,
    timezone,
    status,
    created_at,
    updated_at
) VALUES (
    '50000000-0000-4000-8000-000000000001',
    '10000000-0000-4000-8000-000000000001',
    '20000000-0000-4000-8000-000000000001',
    'DEMO-VI-01',
    'Lớp học trực tuyến mẫu',
    'Dữ liệu phát triển cục bộ cho luồng lớp học TutorHub.',
    'Asia/Ho_Chi_Minh',
    'active',
    '2026-01-01T00:00:00Z',
    '2026-01-01T00:00:00Z'
)
ON CONFLICT (tenant_id, code) DO UPDATE SET
    owner_user_id = EXCLUDED.owner_user_id,
    title = EXCLUDED.title,
    description = EXCLUDED.description,
    timezone = EXCLUDED.timezone,
    status = EXCLUDED.status,
    archived_from_status = NULL,
    archived_at = NULL,
    updated_at = EXCLUDED.updated_at;
`

func ValidateEnvironment(environment string) error {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "development", "test":
		return nil
	default:
		return ErrEnvironmentBlocked
	}
}

func Run(ctx context.Context, databaseURL string, environment string) error {
	if err := ValidateEnvironment(environment); err != nil {
		return err
	}
	if strings.TrimSpace(databaseURL) == "" {
		return ErrDatabaseURLRequired
	}

	connectionConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse development database configuration: %w", err)
	}
	connectionConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	connection, err := pgx.ConnectConfig(ctx, connectionConfig)
	if err != nil {
		return fmt.Errorf("connect to development database: %w", err)
	}
	defer connection.Close(context.Background())

	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin development seed transaction: %w", err)
	}
	defer transaction.Rollback(context.Background())

	if _, err := transaction.Exec(ctx, seedSQL); err != nil {
		return fmt.Errorf("apply development seed: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit development seed: %w", err)
	}

	return nil
}
