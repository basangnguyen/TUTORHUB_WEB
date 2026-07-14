package media

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type PostgresRepository struct {
	database     DBTX
	queryTimeout time.Duration
}

func NewPostgresRepository(database DBTX, queryTimeout time.Duration) *PostgresRepository {
	return &PostgresRepository{database: database, queryTimeout: queryTimeout}
}

func (repository *PostgresRepository) RecordWebhookReceipt(
	ctx context.Context,
	receipt WebhookReceipt,
) (bool, error) {
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()

	const query = `
INSERT INTO tutorhub.livekit_webhook_events (
    event_id,
    event_type,
    tenant_id,
    class_id,
    room_name,
    participant_identity,
    occurred_at,
    received_at
)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)
ON CONFLICT (event_id) DO NOTHING`

	tag, err := repository.database.Exec(
		queryContext,
		query,
		receipt.EventID,
		receipt.EventType,
		receipt.TenantID,
		receipt.ClassID,
		receipt.RoomName,
		receipt.ParticipantIdentity,
		receipt.OccurredAt,
		receipt.ReceivedAt,
	)
	if err != nil {
		return false, fmt.Errorf("insert LiveKit webhook receipt: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func (repository *PostgresRepository) contextWithTimeout(
	ctx context.Context,
) (context.Context, context.CancelFunc) {
	if repository.queryTimeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, repository.queryTimeout)
}
