package outboxworker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrLeaseLost = errors.New("outbox lease is no longer valid")

type ClaimRequest struct {
	EventTypes    []string
	OwnerID       uuid.UUID
	BatchSize     int
	LeaseDuration time.Duration
	MaxAttempts   int
}

type BacklogStats struct {
	Pending          int64
	Ready            int64
	Leased           int64
	DeadLettered     int64
	OldestPendingAge time.Duration
	DueLag           time.Duration
}

type Store interface {
	Claim(context.Context, ClaimRequest) ([]Event, error)
	Ack(context.Context, LeaseRef) error
	Retry(context.Context, LeaseRef, time.Duration, string) error
	DeadLetter(context.Context, LeaseRef, string) error
	SweepExhausted(context.Context, []string, int, int) (int64, error)
	Backlog(context.Context, []string, int) (BacklogStats, error)
}

type postgresDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type PostgresStore struct {
	database     postgresDatabase
	queryTimeout time.Duration
}

func NewPostgresStore(database postgresDatabase, queryTimeout time.Duration) (*PostgresStore, error) {
	if database == nil {
		return nil, fmt.Errorf("outbox database is required")
	}
	if queryTimeout <= 0 {
		return nil, fmt.Errorf("outbox query timeout must be positive")
	}
	return &PostgresStore{database: database, queryTimeout: queryTimeout}, nil
}

func (store *PostgresStore) Claim(ctx context.Context, request ClaimRequest) ([]Event, error) {
	if err := validateClaimRequest(request); err != nil {
		return nil, err
	}
	if len(request.EventTypes) == 0 {
		return nil, nil
	}

	queryContext, cancel := context.WithTimeout(ctx, store.queryTimeout)
	defer cancel()
	transaction, err := store.database.Begin(queryContext)
	if err != nil {
		return nil, fmt.Errorf("begin outbox claim: %w", err)
	}
	defer func() {
		rollbackContext, cancelRollback := context.WithTimeout(
			context.Background(),
			store.queryTimeout,
		)
		defer cancelRollback()
		_ = transaction.Rollback(rollbackContext)
	}()

	rows, err := transaction.Query(
		queryContext,
		claimSQL,
		request.EventTypes,
		request.MaxAttempts,
		request.BatchSize,
		request.OwnerID,
		request.LeaseDuration.Milliseconds(),
	)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()

	events := make([]Event, 0, request.BatchSize)
	for rows.Next() {
		var event Event
		var eventType string
		if err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.AggregateType,
			&event.AggregateID,
			&eventType,
			&event.Payload,
			&event.OccurredAt,
			&event.AvailableAt,
			&event.Attempts,
			&event.Lease.OwnerID,
			&event.Lease.Token,
			&event.Reclaimed,
			&event.LeasedAt,
			&event.LeasedUntil,
		); err != nil {
			return nil, fmt.Errorf("scan claimed outbox event: %w", err)
		}
		parsedType, err := ParseEventType(eventType)
		if err != nil {
			return nil, fmt.Errorf("parse claimed outbox event type: %w", err)
		}
		event.Type = parsedType
		event.Lease.EventID = event.ID
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed outbox events: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return nil, fmt.Errorf("commit outbox claim: %w", err)
	}
	return events, nil
}

func (store *PostgresStore) Ack(ctx context.Context, lease LeaseRef) error {
	return store.complete(ctx, lease, ackSQL, nil)
}

func (store *PostgresStore) Retry(
	ctx context.Context,
	lease LeaseRef,
	delay time.Duration,
	errorCode string,
) error {
	if delay <= 0 {
		return fmt.Errorf("outbox retry delay must be positive")
	}
	return store.complete(
		ctx,
		lease,
		retrySQL,
		[]any{delay.Milliseconds(), normalizedErrorCode(errorCode)},
	)
}

func (store *PostgresStore) DeadLetter(
	ctx context.Context,
	lease LeaseRef,
	errorCode string,
) error {
	return store.complete(
		ctx,
		lease,
		deadLetterSQL,
		[]any{normalizedErrorCode(errorCode)},
	)
}

func (store *PostgresStore) complete(
	ctx context.Context,
	lease LeaseRef,
	query string,
	extra []any,
) error {
	if err := lease.Validate(); err != nil {
		return err
	}
	queryContext, cancel := context.WithTimeout(ctx, store.queryTimeout)
	defer cancel()
	arguments := []any{lease.EventID, lease.OwnerID, lease.Token}
	arguments = append(arguments, extra...)
	result, err := store.database.Exec(queryContext, query, arguments...)
	if err != nil {
		return fmt.Errorf("complete outbox lease: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (store *PostgresStore) SweepExhausted(
	ctx context.Context,
	eventTypes []string,
	maxAttempts int,
	limit int,
) (int64, error) {
	if err := validateEventTypes(eventTypes); err != nil {
		return 0, err
	}
	if maxAttempts < 1 || limit < 1 || limit > 1000 {
		return 0, fmt.Errorf("outbox sweep bounds are invalid")
	}
	if len(eventTypes) == 0 {
		return 0, nil
	}
	queryContext, cancel := context.WithTimeout(ctx, store.queryTimeout)
	defer cancel()
	result, err := store.database.Exec(
		queryContext, sweepExhaustedSQL, eventTypes, maxAttempts, limit,
	)
	if err != nil {
		return 0, fmt.Errorf("dead-letter exhausted outbox events: %w", err)
	}
	return result.RowsAffected(), nil
}

func (store *PostgresStore) Backlog(
	ctx context.Context,
	eventTypes []string,
	maxAttempts int,
) (BacklogStats, error) {
	if err := validateEventTypes(eventTypes); err != nil {
		return BacklogStats{}, err
	}
	if maxAttempts < 1 {
		return BacklogStats{}, fmt.Errorf("outbox max attempts must be positive")
	}
	if len(eventTypes) == 0 {
		return BacklogStats{}, nil
	}

	queryContext, cancel := context.WithTimeout(ctx, store.queryTimeout)
	defer cancel()
	var stats BacklogStats
	var oldestPendingMilliseconds int64
	var dueLagMilliseconds int64
	err := store.database.QueryRow(
		queryContext,
		backlogSQL,
		eventTypes,
		maxAttempts,
	).Scan(
		&stats.Pending,
		&stats.Ready,
		&stats.Leased,
		&stats.DeadLettered,
		&oldestPendingMilliseconds,
		&dueLagMilliseconds,
	)
	if err != nil {
		return BacklogStats{}, fmt.Errorf("inspect outbox backlog: %w", err)
	}
	stats.OldestPendingAge = time.Duration(oldestPendingMilliseconds) * time.Millisecond
	stats.DueLag = time.Duration(dueLagMilliseconds) * time.Millisecond
	return stats, nil
}

func validateClaimRequest(request ClaimRequest) error {
	if err := validateEventTypes(request.EventTypes); err != nil {
		return err
	}
	if request.OwnerID == uuid.Nil {
		return fmt.Errorf("outbox lease owner is required")
	}
	if request.BatchSize < 1 || request.BatchSize > 1000 {
		return fmt.Errorf("outbox batch size is invalid")
	}
	if request.LeaseDuration <= 0 {
		return fmt.Errorf("outbox lease duration must be positive")
	}
	if request.MaxAttempts < 1 {
		return fmt.Errorf("outbox max attempts must be positive")
	}
	return nil
}

func validateEventTypes(eventTypes []string) error {
	seen := make(map[string]struct{}, len(eventTypes))
	for _, value := range eventTypes {
		parsed, err := ParseEventType(value)
		if err != nil || parsed.String() != value {
			return fmt.Errorf("outbox allowlist contains invalid event type %q", value)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("outbox allowlist contains duplicate event type %q", value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

const claimSQL = `
WITH candidates AS (
    SELECT id,
           lease_owner IS NOT NULL AS reclaimed
    FROM tutorhub.outbox_events
    WHERE event_type = ANY($1::text[])
      AND published_at IS NULL
      AND dead_lettered_at IS NULL
      AND available_at <= clock_timestamp()
      AND (lease_owner IS NULL OR leased_until <= clock_timestamp())
      AND attempts < $2
    ORDER BY available_at, occurred_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT $3
)
UPDATE tutorhub.outbox_events AS event
SET lease_owner = $4,
    lease_token = event.lease_token + 1,
    leased_at = clock_timestamp(),
    leased_until = clock_timestamp() + ($5::bigint * interval '1 millisecond')
FROM candidates
WHERE event.id = candidates.id
RETURNING event.id,
          event.tenant_id,
          event.aggregate_type,
          event.aggregate_id,
          event.event_type,
          event.payload,
          event.occurred_at,
          event.available_at,
          event.attempts,
          event.lease_owner,
          event.lease_token,
          candidates.reclaimed,
          event.leased_at,
          event.leased_until`

const ackSQL = `
UPDATE tutorhub.outbox_events
SET published_at = clock_timestamp(),
    last_error = NULL,
    lease_owner = NULL,
    leased_at = NULL,
    leased_until = NULL
WHERE id = $1
  AND lease_owner = $2
  AND lease_token = $3
  AND leased_until > clock_timestamp()
  AND published_at IS NULL
  AND dead_lettered_at IS NULL`

const retrySQL = `
UPDATE tutorhub.outbox_events
SET available_at = clock_timestamp() + ($4::bigint * interval '1 millisecond'),
    last_error = $5,
    attempts = attempts + 1,
    lease_owner = NULL,
    leased_at = NULL,
    leased_until = NULL
WHERE id = $1
  AND lease_owner = $2
  AND lease_token = $3
  AND leased_until > clock_timestamp()
  AND published_at IS NULL
  AND dead_lettered_at IS NULL`

const deadLetterSQL = `
UPDATE tutorhub.outbox_events
SET dead_lettered_at = clock_timestamp(),
    last_error = $4,
    attempts = attempts + 1,
    lease_owner = NULL,
    leased_at = NULL,
    leased_until = NULL
WHERE id = $1
  AND lease_owner = $2
  AND lease_token = $3
  AND leased_until > clock_timestamp()
  AND published_at IS NULL
  AND dead_lettered_at IS NULL`

const sweepExhaustedSQL = `
WITH candidates AS (
    SELECT id
    FROM tutorhub.outbox_events
    WHERE event_type = ANY($1::text[])
      AND published_at IS NULL
      AND dead_lettered_at IS NULL
      AND attempts >= $2
      AND (lease_owner IS NULL OR leased_until <= clock_timestamp())
    ORDER BY occurred_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT $3
)
UPDATE tutorhub.outbox_events AS event
SET dead_lettered_at = clock_timestamp(),
    last_error = '` + ErrorCodeAttemptsExhausted + `',
    lease_owner = NULL,
    leased_at = NULL,
    leased_until = NULL
FROM candidates
WHERE event.id = candidates.id`

const backlogSQL = `
SELECT count(*) FILTER (
           WHERE published_at IS NULL
             AND dead_lettered_at IS NULL
       )::bigint AS pending,
       count(*) FILTER (
           WHERE published_at IS NULL
             AND dead_lettered_at IS NULL
             AND available_at <= clock_timestamp()
             AND (lease_owner IS NULL OR leased_until <= clock_timestamp())
             AND attempts < $2
       )::bigint AS ready,
       count(*) FILTER (
           WHERE published_at IS NULL
             AND dead_lettered_at IS NULL
             AND lease_owner IS NOT NULL
             AND leased_until > clock_timestamp()
       )::bigint AS leased,
       count(*) FILTER (
           WHERE dead_lettered_at IS NOT NULL
       )::bigint AS dead_lettered,
       COALESCE(
           GREATEST(
               0,
               extract(epoch FROM (
                   clock_timestamp() - min(occurred_at) FILTER (
                       WHERE published_at IS NULL
                         AND dead_lettered_at IS NULL
                   )
               )) * 1000
           ),
           0
       )::bigint AS oldest_pending_milliseconds,
       COALESCE(
           GREATEST(
               0,
               extract(epoch FROM (
                   clock_timestamp() - min(available_at) FILTER (
                       WHERE published_at IS NULL
                         AND dead_lettered_at IS NULL
                         AND available_at <= clock_timestamp()
                         AND (lease_owner IS NULL OR leased_until <= clock_timestamp())
                         AND attempts < $2
                   )
               )) * 1000
           ),
           0
       )::bigint AS due_lag_milliseconds
FROM tutorhub.outbox_events
WHERE event_type = ANY($1::text[])`
