package notification

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/outboxworker"
)

func TestCanaryHandlerProjectsStrictTenantScopedPayload(t *testing.T) {
	t.Parallel()
	projector := &canaryProjectorStub{}
	registry := outboxworker.NewRegistry()
	if err := RegisterCanaryHandler(registry, projector); err != nil {
		t.Fatalf("register canary: %v", err)
	}
	if got := registry.Allowlist(); len(got) != 1 || got[0] != CanaryEventType.String() {
		t.Fatalf("unexpected canary allowlist: %v", got)
	}
	handler, ok := registry.Resolve(CanaryEventType)
	if !ok {
		t.Fatal("canary handler was not registered")
	}
	tenantID, recipientID, eventID := uuid.New(), uuid.New(), uuid.New()
	occurredAt := time.Date(2026, 7, 25, 11, 0, 0, 0, time.UTC)
	payload, _ := json.Marshal(CanaryPayload{
		RecipientUserID: recipientID,
		Kind:            KindSystemWorkerCanary,
	})
	err := handler.Handle(context.Background(), outboxworker.Event{
		ID:            eventID,
		TenantID:      uuid.NullUUID{UUID: tenantID, Valid: true},
		AggregateType: canaryAggregateType,
		AggregateID:   uuid.New(),
		Type:          CanaryEventType,
		Payload:       payload,
		OccurredAt:    occurredAt,
	})
	if err != nil {
		t.Fatalf("handle canary: %v", err)
	}
	if projector.calls != 1 || projector.projection.SourceOutboxEventID != eventID ||
		projector.projection.TenantID != tenantID ||
		projector.projection.RecipientUserID != recipientID ||
		!projector.projection.OccurredAt.Equal(occurredAt) {
		t.Fatalf("unexpected projection: %+v", projector.projection)
	}
}

func TestCanaryHandlerRejectsUnknownOrUnsafePayload(t *testing.T) {
	t.Parallel()
	registry := outboxworker.NewRegistry()
	projector := &canaryProjectorStub{}
	if err := RegisterCanaryHandler(registry, projector); err != nil {
		t.Fatalf("register canary: %v", err)
	}
	handler, _ := registry.Resolve(CanaryEventType)
	tenantID := uuid.New()
	tests := []string{
		`{"recipient_user_id":"` + uuid.NewString() + `","kind":"system.worker_canary","body":"secret"}`,
		`{"recipient_user_id":"` + uuid.NewString() + `","kind":"arbitrary.message"}`,
		`{"recipient_user_id":"00000000-0000-0000-0000-000000000000","kind":"system.worker_canary"}`,
	}
	for index, payload := range tests {
		err := handler.Handle(context.Background(), outboxworker.Event{
			ID: uuid.New(), TenantID: uuid.NullUUID{UUID: tenantID, Valid: true},
			AggregateType: canaryAggregateType, AggregateID: uuid.New(),
			Type: CanaryEventType, Payload: []byte(payload), OccurredAt: time.Now(),
		})
		code, disposition := outboxworker.ClassifyFailure(err)
		if disposition != outboxworker.FailurePermanent ||
			code != outboxworker.ErrorCodeInvalidPayload {
			t.Fatalf("case %d failure = %q/%v", index, code, disposition)
		}
	}
	if projector.calls != 0 {
		t.Fatalf("unsafe payload reached projector %d times", projector.calls)
	}
}

func TestCanaryHandlerClassifiesProjectionFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err         error
		wantCode    string
		wantFailure outboxworker.FailureDisposition
	}{
		{ErrNotFound, outboxworker.ErrorCodeInvalidEventContext, outboxworker.FailurePermanent},
		{ErrInvalidInput, outboxworker.ErrorCodeInvalidPayload, outboxworker.FailurePermanent},
		{errors.New("database unavailable"), canaryProjectionErrorCode, outboxworker.FailureRetry},
	}
	for _, test := range tests {
		registry := outboxworker.NewRegistry()
		projector := &canaryProjectorStub{err: test.err}
		if err := RegisterCanaryHandler(registry, projector); err != nil {
			t.Fatalf("register canary: %v", err)
		}
		handler, _ := registry.Resolve(CanaryEventType)
		payload, _ := json.Marshal(CanaryPayload{
			RecipientUserID: uuid.New(), Kind: KindSystemWorkerCanary,
		})
		err := handler.Handle(context.Background(), outboxworker.Event{
			ID: uuid.New(), TenantID: uuid.NullUUID{UUID: uuid.New(), Valid: true},
			AggregateType: canaryAggregateType, AggregateID: uuid.New(),
			Type: CanaryEventType, Payload: payload, OccurredAt: time.Now(),
		})
		code, disposition := outboxworker.ClassifyFailure(err)
		if code != test.wantCode || disposition != test.wantFailure {
			t.Fatalf("failure = %q/%v, want %q/%v", code, disposition, test.wantCode, test.wantFailure)
		}
	}
}

func TestPostgresCanaryProjectorUsesIdempotentInsertWithoutRead(t *testing.T) {
	t.Parallel()
	database := &commandDatabaseStub{}
	projector, err := NewPostgresCanaryProjector(database, time.Second)
	if err != nil {
		t.Fatalf("create projector: %v", err)
	}
	projection := CanaryProjection{
		SourceOutboxEventID: uuid.New(), TenantID: uuid.New(), RecipientUserID: uuid.New(),
		OccurredAt: time.Now(),
	}
	if err := projector.ProjectCanary(context.Background(), projection); err != nil {
		t.Fatalf("project canary: %v", err)
	}
	if !strings.Contains(database.query, "ON CONFLICT") ||
		!strings.Contains(database.query, "DO NOTHING") ||
		strings.Contains(database.query, "ON CONFLICT (") ||
		strings.Contains(strings.ToUpper(database.query), "RETURNING") || len(database.arguments) != 6 {
		t.Fatalf("projection SQL is not least-privilege/idempotent: %s", database.query)
	}
}

type canaryProjectorStub struct {
	projection CanaryProjection
	err        error
	calls      int
}

func (projector *canaryProjectorStub) ProjectCanary(
	_ context.Context,
	projection CanaryProjection,
) error {
	projector.calls++
	projector.projection = projection
	return projector.err
}

type commandDatabaseStub struct {
	query     string
	arguments []any
	err       error
}

func (database *commandDatabaseStub) Exec(
	_ context.Context,
	query string,
	arguments ...any,
) (pgconn.CommandTag, error) {
	database.query = query
	database.arguments = arguments
	return pgconn.NewCommandTag("INSERT 0 1"), database.err
}
