package outboxworker

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateClaimRequestRequiresExactAllowlistAndBounds(t *testing.T) {
	t.Parallel()

	valid := ClaimRequest{
		EventTypes:    []string{"worker.test.v1"},
		OwnerID:       uuid.New(),
		BatchSize:     25,
		LeaseDuration: 30 * time.Second,
		MaxAttempts:   8,
	}
	if err := validateClaimRequest(valid); err != nil {
		t.Fatalf("validate claim request: %v", err)
	}

	tests := []ClaimRequest{
		{EventTypes: []string{"worker.test"}, OwnerID: uuid.New(), BatchSize: 1, LeaseDuration: time.Second, MaxAttempts: 1},
		{EventTypes: []string{"worker.test.v1", "worker.test.v1"}, OwnerID: uuid.New(), BatchSize: 1, LeaseDuration: time.Second, MaxAttempts: 1},
		{EventTypes: valid.EventTypes, OwnerID: uuid.Nil, BatchSize: 1, LeaseDuration: time.Second, MaxAttempts: 1},
		{EventTypes: valid.EventTypes, OwnerID: uuid.New(), BatchSize: 0, LeaseDuration: time.Second, MaxAttempts: 1},
		{EventTypes: valid.EventTypes, OwnerID: uuid.New(), BatchSize: 1, LeaseDuration: 0, MaxAttempts: 1},
		{EventTypes: valid.EventTypes, OwnerID: uuid.New(), BatchSize: 1, LeaseDuration: time.Second, MaxAttempts: 0},
	}
	for index, request := range tests {
		if err := validateClaimRequest(request); err == nil {
			t.Fatalf("case %d: expected validation error", index)
		}
	}
}

func TestStoreQueriesUseDatabaseClockSkipLockedAndFencing(t *testing.T) {
	t.Parallel()

	for name, query := range map[string]string{
		"claim": claimSQL,
		"ack":   ackSQL,
		"retry": retrySQL,
		"dead":  deadLetterSQL,
		"sweep": sweepExhaustedSQL,
		"stats": backlogSQL,
	} {
		if query == "" {
			t.Fatalf("%s query is empty", name)
		}
	}
	for _, expected := range []string{
		"FOR UPDATE SKIP LOCKED",
		"event_type = ANY($1::text[])",
		"lease_token = event.lease_token + 1",
		"clock_timestamp()",
	} {
		if !containsSQL(claimSQL, expected) {
			t.Fatalf("claim query missing %q", expected)
		}
	}
	if containsSQL(claimSQL, "attempts = event.attempts + 1") {
		t.Fatal("claim must not count as a failed delivery attempt")
	}
	if !containsSQL(retrySQL, "attempts = attempts + 1") ||
		!containsSQL(deadLetterSQL, "attempts = attempts + 1") {
		t.Fatal("retry and dead-letter must count the persisted handler failure")
	}
	for _, query := range []string{ackSQL, retrySQL, deadLetterSQL} {
		for _, expected := range []string{
			"lease_owner = $2",
			"lease_token = $3",
			"leased_until > clock_timestamp()",
		} {
			if !containsSQL(query, expected) {
				t.Fatalf("fenced completion query missing %q", expected)
			}
		}
	}
	for _, expected := range []string{
		"event_type = ANY($1::text[])",
		"clock_timestamp()",
		"attempts < $2",
		"oldest_pending_milliseconds",
		"due_lag_milliseconds",
	} {
		if !containsSQL(backlogSQL, expected) {
			t.Fatalf("backlog query missing %q", expected)
		}
	}
}

func containsSQL(query string, value string) bool {
	return len(query) >= len(value) && findSQL(query, value) >= 0
}

func findSQL(query string, value string) int {
	for index := 0; index+len(value) <= len(query); index++ {
		if query[index:index+len(value)] == value {
			return index
		}
	}
	return -1
}
