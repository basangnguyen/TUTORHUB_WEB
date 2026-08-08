package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const defaultQueryTimeout = 10 * time.Second

type transactionDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     transactionDatabase
	queryTimeout time.Duration
}

func NewPostgresRepository(
	database transactionDatabase,
	queryTimeout time.Duration,
) (*PostgresRepository, error) {
	if database == nil {
		return nil, fmt.Errorf("discovery database is required")
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresRepository{database: database, queryTimeout: queryTimeout}, nil
}

func (repository *PostgresRepository) RecentFiles(
	ctx context.Context,
	access AccessContext,
	limit int,
) ([]RecentFile, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, fmt.Errorf("begin recent file query: %w", err)
	}
	defer rollback(transaction)
	if err := requireActiveScope(queryContext, transaction, access); err != nil {
		return nil, err
	}
	rows, err := transaction.Query(queryContext, authorizedResourceCTE+`
SELECT file.id, file.class_id, class.title, file.display_name,
       file.declared_media_type, file.expected_size_bytes, file.updated_at
FROM tutorhub.content_files AS file
JOIN authorized_classes AS class ON class.id = file.class_id
WHERE file.tenant_id = $1
  AND file.status = 'ready'
  AND file.deleted_at IS NULL
ORDER BY file.updated_at DESC, file.id DESC
LIMIT $3`, access.TenantID, access.ActorID, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent files: %w", err)
	}
	defer rows.Close()
	items := make([]RecentFile, 0, limit)
	for rows.Next() {
		var item RecentFile
		if err := rows.Scan(
			&item.ID, &item.ClassID, &item.ClassTitle, &item.DisplayName,
			&item.DeclaredMediaType, &item.SizeBytes, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recent file: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent files: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, fmt.Errorf("commit recent file query: %w", err)
	}
	return items, nil
}

func (repository *PostgresRepository) Search(
	ctx context.Context,
	access AccessContext,
	query string,
	limit int,
) ([]SearchResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, fmt.Errorf("begin resource search: %w", err)
	}
	defer rollback(transaction)
	if err := requireActiveScope(queryContext, transaction, access); err != nil {
		return nil, err
	}
	rows, err := transaction.Query(queryContext, authorizedResourceCTE+`,
session_results AS (
    SELECT 'session'::text AS kind, session.id, session.class_id,
           session.title, class.title AS context, session.updated_at AS occurred_at
    FROM tutorhub.class_sessions AS session
    JOIN authorized_classes AS class ON class.id = session.class_id
    WHERE session.tenant_id = $1
      AND position($3 IN lower(session.title || ' ' || class.title)) > 0
    ORDER BY session.updated_at DESC, session.id DESC
    LIMIT $4
),
conversation_results AS (
    SELECT 'conversation'::text AS kind, conversation.id, conversation.class_id,
           CASE
             WHEN conversation.kind = 'class' THEN class.title
             WHEN conversation.direct_user_low_id = $2 THEN high_user.display_name
             ELSE low_user.display_name
           END AS title,
           CASE WHEN conversation.kind = 'class' THEN 'class' ELSE 'direct' END AS context,
           conversation.updated_at AS occurred_at
    FROM tutorhub.conversations AS conversation
    LEFT JOIN authorized_classes AS class ON class.id = conversation.class_id
    LEFT JOIN tutorhub.users AS low_user ON low_user.id = conversation.direct_user_low_id
    LEFT JOIN tutorhub.users AS high_user ON high_user.id = conversation.direct_user_high_id
    WHERE conversation.tenant_id = $1
      AND (
        (conversation.kind = 'direct' AND $2 IN (
            conversation.direct_user_low_id, conversation.direct_user_high_id
        ))
        OR (conversation.kind = 'class' AND class.id IS NOT NULL)
      )
      AND position($3 IN lower(
        CASE
          WHEN conversation.kind = 'class' THEN class.title
          WHEN conversation.direct_user_low_id = $2 THEN high_user.display_name
          ELSE low_user.display_name
        END
      )) > 0
    ORDER BY conversation.updated_at DESC, conversation.id DESC
    LIMIT $4
),
file_results AS (
    SELECT 'file'::text AS kind, file.id, file.class_id,
           file.display_name AS title, class.title AS context, file.updated_at AS occurred_at
    FROM tutorhub.content_files AS file
    JOIN authorized_classes AS class ON class.id = file.class_id
    WHERE file.tenant_id = $1
      AND file.status = 'ready'
      AND file.deleted_at IS NULL
      AND position($3 IN lower(file.display_name || ' ' || class.title)) > 0
    ORDER BY file.updated_at DESC, file.id DESC
    LIMIT $4
)
SELECT kind, id, class_id, title, context, occurred_at
FROM (
    SELECT * FROM session_results
    UNION ALL
    SELECT * FROM conversation_results
    UNION ALL
    SELECT * FROM file_results
) AS result
ORDER BY occurred_at DESC, kind, id
LIMIT $4`, access.TenantID, access.ActorID, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query resource search: %w", err)
	}
	defer rows.Close()
	items := make([]SearchResult, 0, limit)
	for rows.Next() {
		var item SearchResult
		var classID uuid.NullUUID
		if err := rows.Scan(
			&item.Kind, &item.ID, &classID, &item.Title, &item.Context, &item.OccurredAt,
		); err != nil {
			return nil, fmt.Errorf("scan resource search: %w", err)
		}
		if classID.Valid {
			item.ClassID = &classID.UUID
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate resource search: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, fmt.Errorf("commit resource search: %w", err)
	}
	return items, nil
}

const authorizedResourceCTE = `WITH active_scope AS MATERIALIZED (
    SELECT member.role
    FROM tutorhub.tenants AS tenant
    JOIN tutorhub.memberships AS member ON member.tenant_id = tenant.id
    JOIN tutorhub.users AS actor ON actor.id = member.user_id
    WHERE tenant.id = $1
      AND member.user_id = $2
      AND tenant.status = 'active'
      AND member.status = 'active'
      AND actor.status = 'active'
),
authorized_classes AS MATERIALIZED (
    SELECT class.id, class.title
    FROM tutorhub.classes AS class
    CROSS JOIN active_scope AS scope
    LEFT JOIN tutorhub.class_enrollments AS enrollment
      ON enrollment.tenant_id = class.tenant_id
     AND enrollment.class_id = class.id
     AND enrollment.user_id = $2
    WHERE class.tenant_id = $1
      AND (
        scope.role IN ('org_admin', 'teacher')
        OR class.owner_user_id = $2
        OR enrollment.status = 'active'
      )
)`

func requireActiveScope(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
) error {
	var active bool
	err := transaction.QueryRow(ctx, `SELECT EXISTS (
    SELECT 1
    FROM tutorhub.tenants AS tenant
    JOIN tutorhub.memberships AS member ON member.tenant_id = tenant.id
    JOIN tutorhub.users AS actor ON actor.id = member.user_id
    WHERE tenant.id = $1 AND member.user_id = $2
      AND tenant.status = 'active'
      AND member.status = 'active'
      AND actor.status = 'active'
)`, access.TenantID, access.ActorID).Scan(&active)
	if err != nil {
		return fmt.Errorf("authorize discovery scope: %w", err)
	}
	if !active {
		return ErrAccessDenied
	}
	return nil
}

func rollback(transaction pgx.Tx) {
	if transaction != nil {
		_ = transaction.Rollback(context.Background())
	}
}
