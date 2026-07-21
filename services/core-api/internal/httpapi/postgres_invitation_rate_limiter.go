package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type invitationRateLimitDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type postgresInvitationRateLimiter struct {
	database     invitationRateLimitDatabase
	queryTimeout time.Duration
	policies     map[InvitationRateLimitAction]invitationRateLimitPolicy
}

func NewPostgresInvitationRateLimiter(
	database invitationRateLimitDatabase,
	queryTimeout time.Duration,
) (InvitationRateLimiter, error) {
	if database == nil || queryTimeout <= 0 {
		return nil, fmt.Errorf("shared invitation rate limiter dependencies are required")
	}
	return &postgresInvitationRateLimiter{
		database:     database,
		queryTimeout: queryTimeout,
		policies: map[InvitationRateLimitAction]invitationRateLimitPolicy{
			InvitationRateLimitPreview:   {Limit: 30, Window: time.Minute},
			InvitationRateLimitAccept:    {Limit: 10, Window: time.Minute},
			InvitationRateLimitClassJoin: {Limit: 10, Window: time.Minute},
		},
	}, nil
}

func (limiter *postgresInvitationRateLimiter) Allow(
	ctx context.Context,
	action InvitationRateLimitAction,
	clientPrefix string,
	now time.Time,
) InvitationRateLimitDecision {
	if limiter == nil {
		return InvitationRateLimitDecision{Err: errors.New("shared invitation rate limiter is unavailable")}
	}
	policy, ok := limiter.policies[action]
	if !ok || policy.Limit < 1 || policy.Window <= 0 || clientPrefix == "" {
		return InvitationRateLimitDecision{Err: errors.New("shared invitation rate limiter input is invalid")}
	}
	queryContext, cancel := context.WithTimeout(ctx, limiter.queryTimeout)
	defer cancel()
	now = now.UTC()
	windowStartedAt := now.Truncate(policy.Window)
	windowEndsAt := windowStartedAt.Add(policy.Window)
	purpose := invitationRateLimitPurpose(action)
	bucketHash := invitationRateLimitBucketHash(purpose, clientPrefix)

	var used int64
	err := limiter.database.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.rate_limit_windows (
    purpose,
    bucket_hash,
    window_started_at,
    window_ends_at,
    used_count,
    updated_at
)
VALUES ($1, $2, $3, $4, 1, $5)
ON CONFLICT (purpose, bucket_hash, window_started_at)
DO UPDATE SET
    used_count = tutorhub.rate_limit_windows.used_count + 1,
    updated_at = EXCLUDED.updated_at
WHERE tutorhub.rate_limit_windows.used_count < $6
RETURNING used_count`,
		purpose,
		bucketHash[:],
		windowStartedAt,
		windowEndsAt,
		now,
		policy.Limit,
	).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		if scanErr := limiter.database.QueryRow(
			queryContext,
			`SELECT used_count
FROM tutorhub.rate_limit_windows
WHERE purpose = $1 AND bucket_hash = $2 AND window_started_at = $3`,
			purpose,
			bucketHash[:],
			windowStartedAt,
		).Scan(&used); scanErr != nil {
			return InvitationRateLimitDecision{Err: fmt.Errorf("read shared rate limit: %w", scanErr)}
		}
		retryAfter := windowEndsAt.Sub(now)
		if retryAfter <= 0 {
			retryAfter = time.Second
		}
		return InvitationRateLimitDecision{RetryAfter: retryAfter}
	}
	if err != nil {
		return InvitationRateLimitDecision{Err: fmt.Errorf("consume shared rate limit: %w", err)}
	}
	return InvitationRateLimitDecision{Allowed: true}
}

func invitationRateLimitBucketHash(purpose string, clientPrefix string) [sha256.Size]byte {
	const domain = "tutorhub.invitation-rate-limit.bucket.v1"

	return sha256.Sum256([]byte(domain + "\x00" + purpose + "\x00" + clientPrefix))
}

func invitationRateLimitPurpose(action InvitationRateLimitAction) string {
	switch action {
	case InvitationRateLimitPreview:
		return "membership_invitation.preview"
	case InvitationRateLimitAccept:
		return "membership_invitation.accept"
	case InvitationRateLimitClassJoin:
		return "class_invite.join"
	default:
		return "invalid"
	}
}
