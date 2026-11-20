package httpapi

import (
	"context"
	"encoding/binary"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// A fixed window admits two bursts at a boundary. Serialize decisions for this
// purpose/account (or IP prefix) and count every accepted attempt for five minutes.
// Expired bucket rows remain eligible for ordinary rate_limit_windows retention.
func (limiter *postgresInvitationRateLimiter) allowRollingMFA(ctx context.Context, purpose string, bucket []byte, policy invitationRateLimitPolicy) InvitationRateLimitDecision {
	database, ok := limiter.database.(interface {
		Begin(context.Context) (pgx.Tx, error)
	})
	if !ok {
		return InvitationRateLimitDecision{Err: errors.New("transactional MFA rate limiter unavailable")}
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return InvitationRateLimitDecision{Err: err}
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Pipeline the advisory lock and atomic check-and-insert on the server so a
	// multi-attempt burst never holds the lock across extra network round-trips.
	batch := &pgx.Batch{}
	batch.Queue(`SELECT pg_advisory_xact_lock($1)`, int64(binary.BigEndian.Uint64(bucket)))
	batch.Queue(`WITH now_clock AS MATERIALIZED (
		SELECT clock_timestamp() AS ts
	),
	usage AS MATERIALIZED (
		SELECT COALESCE(SUM(w.used_count), 0)::bigint AS used,
		       MIN(w.window_started_at) AS oldest,
		       c.ts
		FROM now_clock c
		LEFT JOIN tutorhub.rate_limit_windows w
		  ON w.purpose = $1
		 AND w.bucket_hash = $2
		 AND w.window_started_at > c.ts - ($3::bigint * interval '1 microsecond')
		GROUP BY c.ts
	),
	recorded AS (
		INSERT INTO tutorhub.rate_limit_windows (
			purpose, bucket_hash, window_started_at, window_ends_at, used_count, updated_at
		)
		SELECT $1, $2, u.ts, u.ts + ($3::bigint * interval '1 microsecond'), 1, u.ts
		FROM usage u
		WHERE u.used < $4
		ON CONFLICT (purpose, bucket_hash, window_started_at)
		DO UPDATE SET
			used_count = tutorhub.rate_limit_windows.used_count + 1,
			updated_at = EXCLUDED.updated_at
	)
	SELECT (u.used < $4) AS allowed,
	       CASE
	         WHEN u.used >= $4 AND u.oldest IS NOT NULL THEN
	           GREATEST(CEIL(EXTRACT(EPOCH FROM ((u.oldest + ($3::bigint * interval '1 microsecond')) - u.ts)) * 1000000)::bigint, 1)
	         ELSE 0::bigint
	       END AS retry_micros
	FROM usage u`, purpose, bucket, policy.Window.Microseconds(), policy.Limit)
	results := tx.SendBatch(ctx, batch)
	if _, err = results.Exec(); err != nil {
		_ = results.Close()
		return InvitationRateLimitDecision{Err: err}
	}
	var allowed bool
	var retryMicros int64
	if err = results.QueryRow().Scan(&allowed, &retryMicros); err != nil {
		_ = results.Close()
		return InvitationRateLimitDecision{Err: err}
	}
	if err = results.Close(); err != nil {
		return InvitationRateLimitDecision{Err: err}
	}
	if !allowed {
		return InvitationRateLimitDecision{RetryAfter: time.Duration(retryMicros) * time.Microsecond}
	}
	if err = tx.Commit(ctx); err != nil {
		return InvitationRateLimitDecision{Err: err}
	}
	return InvitationRateLimitDecision{Allowed: true}
}
