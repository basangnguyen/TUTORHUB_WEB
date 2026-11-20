//go:build integration

package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

func TestMFAPostgresRollingAccountAndIPLimits(t *testing.T) {
	url := os.Getenv("AUTH_TEST_DATABASE_URL")
	if url == "" || os.Getenv("AUTH_TEST_DISPOSABLE") != "true" {
		t.Skip("requires a disposable AUTH_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal("apply disposable schema", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	limiter, err := NewPostgresAuthRateLimiter(pool, 5*time.Second, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		action InvitationRateLimitAction
		limit  int
	}{{AuthRateLimitMFAUser, 5}, {AuthRateLimitMFAIP, 20}} {
		key := uuid.NewString()
		bucket := invitationRateLimitBucketHash(string(tc.action), key)
		run := func() InvitationRateLimitDecision { return limiter.Allow(ctx, tc.action, key, time.Now()) }
		// All callers use independent pooled transactions for the same key.
		decisions := make(chan InvitationRateLimitDecision, tc.limit+4)
		for range tc.limit + 4 {
			go func() { decisions <- run() }()
		}
		accepted := 0
		for range tc.limit + 4 {
			d := <-decisions
			if d.Err != nil {
				t.Fatal(d.Err)
			}
			if d.Allowed {
				accepted++
			} else if d.RetryAfter <= 0 || d.RetryAfter > 5*time.Minute {
				t.Fatal("invalid rolling retry interval")
			}
		}
		if accepted != tc.limit {
			t.Fatalf("%s accepted %d", tc.action, accepted)
		}
		// Expire the fixture's hits individually, so there is no dependence on a
		// fixed wall-clock epoch or on a sleep in the acceptance suite.
		if _, err = pool.Exec(ctx, `UPDATE tutorhub.rate_limit_windows SET window_started_at=window_started_at-interval '6 minutes',
          window_ends_at=window_ends_at-interval '6 minutes' WHERE purpose=$1 AND bucket_hash=$2`, string(tc.action), bucket[:]); err != nil {
			t.Fatal(err)
		}
		if d := run(); !d.Allowed || d.Err != nil {
			t.Fatal("expired attempts consumed quota", d.Err)
		}
		if _, err = pool.Exec(ctx, `DELETE FROM tutorhub.rate_limit_windows WHERE purpose=$1 AND bucket_hash=$2`, string(tc.action), bucket[:]); err != nil {
			t.Fatal(err)
		}
	}
}
