package httpapi

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresInvitationRateLimiterHashesPrefixAndAllowsWithinLimit(t *testing.T) {
	t.Parallel()

	database := &recordingInvitationRateLimitDatabase{
		rows: []pgx.Row{invitationRateLimitRow{value: 1}},
	}
	limiter, err := NewPostgresInvitationRateLimiter(database, time.Second)
	if err != nil {
		t.Fatalf("NewPostgresInvitationRateLimiter returned error: %v", err)
	}

	decision := limiter.Allow(
		context.Background(),
		InvitationRateLimitPreview,
		"203.0.113.0/24",
		time.Date(2026, time.July, 20, 10, 2, 30, 0, time.UTC),
	)
	if !decision.Allowed || decision.Err != nil {
		t.Fatalf("expected request to be allowed: %+v", decision)
	}
	if len(database.calls) != 1 {
		t.Fatalf("expected one database call, got %d", len(database.calls))
	}
	call := database.calls[0]
	if strings.Contains(call.query, "203.0.113.0/24") {
		t.Fatal("raw client prefix must not be embedded in the query")
	}
	if len(call.args) < 2 {
		t.Fatalf("expected purpose and bucket hash arguments, got %d", len(call.args))
	}
	if call.args[0] != "membership_invitation.preview" {
		t.Fatalf("unexpected purpose: %#v", call.args[0])
	}
	bucketHash, ok := call.args[1].([]byte)
	if !ok || len(bucketHash) != 32 {
		t.Fatalf("expected a SHA-256 bucket hash, got %#v", call.args[1])
	}
	if string(bucketHash) == "203.0.113.0/24" {
		t.Fatal("raw client prefix must not be stored as the bucket key")
	}
}

func TestPostgresInvitationRateLimiterSeparatesBucketHashByPurpose(t *testing.T) {
	t.Parallel()

	database := &recordingInvitationRateLimitDatabase{
		rows: []pgx.Row{
			invitationRateLimitRow{value: 1},
			invitationRateLimitRow{value: 1},
		},
	}
	limiter, err := NewPostgresInvitationRateLimiter(database, time.Second)
	if err != nil {
		t.Fatalf("NewPostgresInvitationRateLimiter returned error: %v", err)
	}

	for _, action := range []InvitationRateLimitAction{
		InvitationRateLimitPreview,
		InvitationRateLimitAccept,
	} {
		decision := limiter.Allow(
			context.Background(),
			action,
			"203.0.113.0/24",
			time.Date(2026, time.July, 20, 10, 2, 30, 0, time.UTC),
		)
		if !decision.Allowed || decision.Err != nil {
			t.Fatalf("expected %s request to be allowed: %+v", action, decision)
		}
	}

	if len(database.calls) != 2 {
		t.Fatalf("expected two database calls, got %d", len(database.calls))
	}
	previewHash, previewOK := database.calls[0].args[1].([]byte)
	acceptHash, acceptOK := database.calls[1].args[1].([]byte)
	if !previewOK || !acceptOK {
		t.Fatalf("expected byte slice bucket hashes, got %#v and %#v", database.calls[0].args[1], database.calls[1].args[1])
	}
	if bytes.Equal(previewHash, acceptHash) {
		t.Fatal("the same client prefix must produce different bucket hashes for different purposes")
	}
}

func TestAvailabilityPollRateLimitActionsUseDistinctStoragePurposesAndHashes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		action  InvitationRateLimitAction
		purpose string
	}{
		{
			action:  InvitationRateLimitAvailabilityPollResolveIP,
			purpose: "availability_poll.resolve_ip",
		},
		{
			action:  InvitationRateLimitAvailabilityPollResolveTokenDigest,
			purpose: "availability_poll.resolve_token_digest",
		},
		{
			action:  InvitationRateLimitAvailabilityPollResolvePublicID,
			purpose: "availability_poll.resolve_public_id",
		},
		{
			action:  InvitationRateLimitAvailabilityPollRespondIP,
			purpose: "availability_poll.respond_ip",
		},
		{
			action:  InvitationRateLimitAvailabilityPollRespondTokenDigest,
			purpose: "availability_poll.respond_token_digest",
		},
		{
			action:  InvitationRateLimitAvailabilityPollRespondPublicID,
			purpose: "availability_poll.respond_public_id",
		},
	}

	seenPurposes := make(map[string]struct{}, len(testCases))
	seenHashes := make(map[[32]byte]struct{}, len(testCases))
	for _, testCase := range testCases {
		actualPurpose := invitationRateLimitPurpose(testCase.action)
		if actualPurpose != testCase.purpose {
			t.Fatalf(
				"action %q: expected storage purpose %q, got %q",
				testCase.action,
				testCase.purpose,
				actualPurpose,
			)
		}
		if _, duplicate := seenPurposes[actualPurpose]; duplicate {
			t.Fatalf("duplicate storage purpose %q", actualPurpose)
		}
		seenPurposes[actualPurpose] = struct{}{}

		bucketHash := invitationRateLimitBucketHash(actualPurpose, "same-safe-bucket")
		if _, duplicate := seenHashes[bucketHash]; duplicate {
			t.Fatalf("purpose %q did not receive an independent storage hash", actualPurpose)
		}
		seenHashes[bucketHash] = struct{}{}
	}
}

func TestPostgresAvailabilityPollRateLimitUsesConfiguredDimensionPolicy(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		action         InvitationRateLimitAction
		expectedLimit  int
		expectedWindow time.Duration
	}{
		{
			action:         InvitationRateLimitAvailabilityPollResolveIP,
			expectedLimit:  availabilityPollResolveIPLimit,
			expectedWindow: availabilityPollResolveWindow,
		},
		{
			action:         InvitationRateLimitAvailabilityPollResolveTokenDigest,
			expectedLimit:  availabilityPollResolveTokenDigestLimit,
			expectedWindow: availabilityPollResolveWindow,
		},
		{
			action:         InvitationRateLimitAvailabilityPollResolvePublicID,
			expectedLimit:  availabilityPollResolvePublicIDLimit,
			expectedWindow: availabilityPollResolveWindow,
		},
		{
			action:         InvitationRateLimitAvailabilityPollRespondIP,
			expectedLimit:  availabilityPollRespondIPLimit,
			expectedWindow: availabilityPollRespondWindow,
		},
		{
			action:         InvitationRateLimitAvailabilityPollRespondTokenDigest,
			expectedLimit:  availabilityPollRespondTokenDigestLimit,
			expectedWindow: availabilityPollRespondWindow,
		},
		{
			action:         InvitationRateLimitAvailabilityPollRespondPublicID,
			expectedLimit:  availabilityPollRespondPublicIDLimit,
			expectedWindow: availabilityPollRespondWindow,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(string(testCase.action), func(t *testing.T) {
			t.Parallel()

			database := &recordingInvitationRateLimitDatabase{
				rows: []pgx.Row{invitationRateLimitRow{value: 1}},
			}
			limiter, err := NewPostgresInvitationRateLimiter(database, time.Second)
			if err != nil {
				t.Fatalf("NewPostgresInvitationRateLimiter returned error: %v", err)
			}
			now := time.Date(2026, time.August, 1, 10, 2, 30, 0, time.UTC)
			decision := limiter.Allow(
				context.Background(),
				testCase.action,
				"privacy-safe-bucket",
				now,
			)
			if !decision.Allowed || decision.Err != nil {
				t.Fatalf("expected request to be allowed: %+v", decision)
			}
			if len(database.calls) != 1 {
				t.Fatalf("expected one database call, got %d", len(database.calls))
			}
			call := database.calls[0]
			if len(call.args) != 6 {
				t.Fatalf("expected six storage arguments, got %d", len(call.args))
			}
			if call.args[0] != invitationRateLimitPurpose(testCase.action) {
				t.Fatalf("unexpected purpose argument: %#v", call.args[0])
			}
			if call.args[2] != now.Truncate(testCase.expectedWindow) ||
				call.args[3] != now.Truncate(testCase.expectedWindow).Add(testCase.expectedWindow) {
				t.Fatalf("unexpected fixed window: start=%#v end=%#v", call.args[2], call.args[3])
			}
			if call.args[5] != testCase.expectedLimit {
				t.Fatalf("expected storage limit %d, got %#v", testCase.expectedLimit, call.args[5])
			}
		})
	}
}

func TestPostgresInvitationRateLimiterReturnsRetryAfterAtLimit(t *testing.T) {
	t.Parallel()

	database := &recordingInvitationRateLimitDatabase{
		rows: []pgx.Row{
			invitationRateLimitRow{err: pgx.ErrNoRows},
			invitationRateLimitRow{value: 30},
		},
	}
	limiter, err := NewPostgresInvitationRateLimiter(database, time.Second)
	if err != nil {
		t.Fatalf("NewPostgresInvitationRateLimiter returned error: %v", err)
	}

	decision := limiter.Allow(
		context.Background(),
		InvitationRateLimitPreview,
		"203.0.113.0/24",
		time.Date(2026, time.July, 20, 10, 2, 30, 0, time.UTC),
	)
	if decision.Allowed || decision.Err != nil {
		t.Fatalf("expected request to be rate limited: %+v", decision)
	}
	if decision.RetryAfter != 30*time.Second {
		t.Fatalf("expected 30s retry delay, got %s", decision.RetryAfter)
	}
}

func TestPostgresInvitationRateLimiterFailsClosedWhenStorageFails(t *testing.T) {
	t.Parallel()

	database := &recordingInvitationRateLimitDatabase{
		rows: []pgx.Row{invitationRateLimitRow{err: errors.New("database unavailable")}},
	}
	limiter, err := NewPostgresInvitationRateLimiter(database, time.Second)
	if err != nil {
		t.Fatalf("NewPostgresInvitationRateLimiter returned error: %v", err)
	}

	decision := limiter.Allow(
		context.Background(),
		InvitationRateLimitAccept,
		"203.0.113.0/24",
		time.Now(),
	)
	if decision.Allowed || decision.Err == nil {
		t.Fatalf("storage failure must fail closed: %+v", decision)
	}
}

type invitationRateLimitQueryCall struct {
	query string
	args  []any
}

type recordingInvitationRateLimitDatabase struct {
	rows  []pgx.Row
	calls []invitationRateLimitQueryCall
}

func (database *recordingInvitationRateLimitDatabase) QueryRow(
	_ context.Context,
	query string,
	args ...any,
) pgx.Row {
	database.calls = append(database.calls, invitationRateLimitQueryCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	if len(database.rows) == 0 {
		return invitationRateLimitRow{err: errors.New("unexpected query")}
	}
	row := database.rows[0]
	database.rows = database.rows[1:]
	return row
}

type invitationRateLimitRow struct {
	value int64
	err   error
}

func (row invitationRateLimitRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected destination count")
	}
	destination, ok := destinations[0].(*int64)
	if !ok {
		return errors.New("unexpected destination type")
	}
	*destination = row.value
	return nil
}
