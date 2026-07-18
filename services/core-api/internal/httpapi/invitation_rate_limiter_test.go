package httpapi

import (
	"sync"
	"testing"
	"time"
)

func TestFixedWindowInvitationRateLimiterIsScopedByActionAndPrefix(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 2, 3, 4, 0, time.UTC)
	limiter := newFixedWindowInvitationRateLimiter(
		16,
		map[InvitationRateLimitAction]invitationRateLimitPolicy{
			InvitationRateLimitPreview: {Limit: 2, Window: time.Minute},
			InvitationRateLimitAccept:  {Limit: 1, Window: time.Minute},
		},
	)

	for attempt := 1; attempt <= 2; attempt++ {
		if decision := limiter.Allow(InvitationRateLimitPreview, "203.0.113.0/24", now); !decision.Allowed {
			t.Fatalf("expected preview attempt %d to be allowed: %+v", attempt, decision)
		}
	}
	denied := limiter.Allow(
		InvitationRateLimitPreview,
		"203.0.113.0/24",
		now.Add(20*time.Second),
	)
	if denied.Allowed || denied.RetryAfter != 40*time.Second {
		t.Fatalf("unexpected denied decision: %+v", denied)
	}

	if decision := limiter.Allow(
		InvitationRateLimitPreview,
		"198.51.100.0/24",
		now.Add(20*time.Second),
	); !decision.Allowed {
		t.Fatalf("a different prefix must have an independent budget: %+v", decision)
	}
	if decision := limiter.Allow(
		InvitationRateLimitAccept,
		"203.0.113.0/24",
		now.Add(20*time.Second),
	); !decision.Allowed {
		t.Fatalf("a different action must have an independent budget: %+v", decision)
	}
}

func TestFixedWindowInvitationRateLimiterResetsAtBoundaryAndClockRollback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 2, 3, 4, 0, time.UTC)
	limiter := newFixedWindowInvitationRateLimiter(
		4,
		map[InvitationRateLimitAction]invitationRateLimitPolicy{
			InvitationRateLimitPreview: {Limit: 1, Window: time.Minute},
		},
	)

	if decision := limiter.Allow(InvitationRateLimitPreview, "203.0.113.0/24", now); !decision.Allowed {
		t.Fatalf("expected first request to be allowed: %+v", decision)
	}
	if decision := limiter.Allow(
		InvitationRateLimitPreview,
		"203.0.113.0/24",
		now.Add(time.Minute-time.Nanosecond),
	); decision.Allowed {
		t.Fatalf("request before boundary must be denied: %+v", decision)
	}
	if decision := limiter.Allow(
		InvitationRateLimitPreview,
		"203.0.113.0/24",
		now.Add(time.Minute),
	); !decision.Allowed {
		t.Fatalf("request at boundary must start a new window: %+v", decision)
	}
	if decision := limiter.Allow(
		InvitationRateLimitPreview,
		"203.0.113.0/24",
		now.Add(-time.Second),
	); !decision.Allowed {
		t.Fatalf("clock rollback must start a safe new window: %+v", decision)
	}
}

func TestFixedWindowInvitationRateLimiterKeepsBoundedState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 2, 3, 4, 0, time.UTC)
	limiter := newFixedWindowInvitationRateLimiter(
		2,
		map[InvitationRateLimitAction]invitationRateLimitPolicy{
			InvitationRateLimitPreview: {Limit: 1, Window: time.Minute},
		},
	)

	limiter.Allow(InvitationRateLimitPreview, "192.0.2.0/24", now)
	limiter.Allow(InvitationRateLimitPreview, "198.51.100.0/24", now.Add(time.Second))
	limiter.Allow(InvitationRateLimitPreview, "203.0.113.0/24", now.Add(2*time.Second))
	if len(limiter.entries) != 2 {
		t.Fatalf("expected bounded map with 2 entries, got %d", len(limiter.entries))
	}
	if _, retained := limiter.entries[invitationRateLimitKey{
		action:       InvitationRateLimitPreview,
		clientPrefix: "192.0.2.0/24",
	}]; retained {
		t.Fatal("expected least recently seen entry to be evicted")
	}

	limiter.Allow(InvitationRateLimitPreview, "192.0.2.0/24", now.Add(2*time.Minute))
	if len(limiter.entries) > 2 {
		t.Fatalf("expired cleanup must preserve the state bound, got %d", len(limiter.entries))
	}
}

func TestFixedWindowInvitationRateLimiterEnforcesLimitConcurrently(t *testing.T) {
	t.Parallel()

	const (
		limit    = 25
		attempts = 100
	)
	now := time.Date(2026, time.July, 18, 2, 3, 4, 0, time.UTC)
	limiter := newFixedWindowInvitationRateLimiter(
		4,
		map[InvitationRateLimitAction]invitationRateLimitPolicy{
			InvitationRateLimitAccept: {Limit: limit, Window: time.Minute},
		},
	)

	var waitGroup sync.WaitGroup
	results := make(chan bool, attempts)
	for index := 0; index < attempts; index++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			results <- limiter.Allow(
				InvitationRateLimitAccept,
				"203.0.113.0/24",
				now,
			).Allowed
		}()
	}
	waitGroup.Wait()
	close(results)

	allowed := 0
	for result := range results {
		if result {
			allowed++
		}
	}
	if allowed != limit {
		t.Fatalf("expected exactly %d allowed requests, got %d", limit, allowed)
	}
}

func TestFixedWindowInvitationRateLimiterFailsOpenForUnconfiguredPolicy(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 2, 3, 4, 0, time.UTC)
	limiter := newFixedWindowInvitationRateLimiter(
		0,
		map[InvitationRateLimitAction]invitationRateLimitPolicy{
			InvitationRateLimitPreview: {Limit: 0, Window: time.Minute},
		},
	)

	if decision := limiter.Allow(InvitationRateLimitPreview, "unknown", now); !decision.Allowed {
		t.Fatalf("invalid policy must not block traffic: %+v", decision)
	}
	if decision := limiter.Allow(InvitationRateLimitAction("unknown"), "unknown", now); !decision.Allowed {
		t.Fatalf("unconfigured action must not block traffic: %+v", decision)
	}

	var nilLimiter *fixedWindowInvitationRateLimiter
	if decision := nilLimiter.Allow(InvitationRateLimitPreview, "unknown", now); !decision.Allowed {
		t.Fatalf("nil limiter must fail open: %+v", decision)
	}
}

func TestRetryAfterSecondsRoundsUpAndHasMinimum(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		duration time.Duration
		expected string
	}{
		{duration: -time.Second, expected: "1"},
		{duration: 0, expected: "1"},
		{duration: time.Millisecond, expected: "1"},
		{duration: time.Second, expected: "1"},
		{duration: time.Second + time.Nanosecond, expected: "2"},
	}
	for _, testCase := range testCases {
		if actual := retryAfterSeconds(testCase.duration); actual != testCase.expected {
			t.Fatalf(
				"retryAfterSeconds(%s): expected %q, got %q",
				testCase.duration,
				testCase.expected,
				actual,
			)
		}
	}
}
