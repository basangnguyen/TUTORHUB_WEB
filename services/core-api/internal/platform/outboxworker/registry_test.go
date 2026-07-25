package outboxworker

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNilRegistryHandlerNamesIsSafe(t *testing.T) {
	t.Parallel()

	var registry *Registry
	if names := registry.HandlerNames(); len(names) != 0 {
		t.Fatalf("nil registry handler names = %v", names)
	}
}

type testPayload struct {
	Value string `json:"value"`
}

func TestRegistryDispatchesStrictTypedPayload(t *testing.T) {
	t.Parallel()

	eventType := MustEventType("worker.test", 1)
	registry := NewRegistry()
	var received testPayload
	err := RegisterJSON(registry, HandlerSpec[testPayload]{
		EventType:     eventType,
		HandlerName:   "test_handler",
		AggregateType: "worker_test",
		TenantMode:    TenantRequired,
		Validate: func(_ Event, payload testPayload) error {
			if payload.Value == "" {
				return errors.New("missing value")
			}
			return nil
		},
		Handle: func(_ context.Context, _ Event, payload testPayload) error {
			received = payload
			return nil
		},
	})
	if err != nil {
		t.Fatalf("register handler: %v", err)
	}

	handler, ok := registry.Resolve(eventType)
	if !ok {
		t.Fatal("expected registered handler")
	}
	err = handler.Handle(context.Background(), Event{
		TenantID:      uuid.NullUUID{UUID: uuid.New(), Valid: true},
		AggregateType: "worker_test",
		Type:          eventType,
		Payload:       []byte(`{"value":"accepted"}`),
	})
	if err != nil {
		t.Fatalf("handle event: %v", err)
	}
	if received.Value != "accepted" {
		t.Fatalf("unexpected payload: %+v", received)
	}
}

func TestRegistryRejectsDuplicateAndUnversionedEvents(t *testing.T) {
	t.Parallel()

	if _, err := ParseEventType("worker.test"); err == nil {
		t.Fatal("expected unversioned event rejection")
	}
	eventType := MustEventType("worker.test", 1)
	registry := NewRegistry()
	spec := HandlerSpec[testPayload]{
		EventType:     eventType,
		HandlerName:   "test_handler",
		AggregateType: "worker_test",
		TenantMode:    TenantOptional,
		Handle:        func(context.Context, Event, testPayload) error { return nil },
	}
	if err := RegisterJSON(registry, spec); err != nil {
		t.Fatalf("register first handler: %v", err)
	}
	if err := RegisterJSON(registry, spec); err == nil {
		t.Fatal("expected duplicate registration rejection")
	}
	allowlist := registry.Allowlist()
	if len(allowlist) != 1 || allowlist[0] != "worker.test.v1" {
		t.Fatalf("unexpected allowlist: %v", allowlist)
	}
}

func TestRegistryRejectsInvalidPayloadAndContextPermanently(t *testing.T) {
	t.Parallel()

	eventType := MustEventType("worker.test", 1)
	registry := NewRegistry()
	if err := RegisterJSON(registry, HandlerSpec[testPayload]{
		EventType:     eventType,
		HandlerName:   "test_handler",
		AggregateType: "worker_test",
		TenantMode:    TenantRequired,
		Handle:        func(context.Context, Event, testPayload) error { return nil },
	}); err != nil {
		t.Fatalf("register handler: %v", err)
	}
	handler, _ := registry.Resolve(eventType)

	tests := []Event{
		{AggregateType: "worker_test", Type: eventType, Payload: []byte(`{"value":"ok"}`)},
		{
			TenantID:      uuid.NullUUID{UUID: uuid.New(), Valid: true},
			AggregateType: "wrong",
			Type:          eventType,
			Payload:       []byte(`{"value":"ok"}`),
		},
		{
			TenantID:      uuid.NullUUID{UUID: uuid.New(), Valid: true},
			AggregateType: "worker_test",
			Type:          eventType,
			Payload:       []byte(`{"value":"ok","unknown":true}`),
		},
	}
	for index, event := range tests {
		err := handler.Handle(context.Background(), event)
		code, disposition := ClassifyFailure(err)
		if disposition != FailurePermanent {
			t.Fatalf("case %d: expected permanent failure, got %v", index, err)
		}
		if code != ErrorCodeInvalidEventContext && code != ErrorCodeInvalidPayload {
			t.Fatalf("case %d: unexpected code %q", index, code)
		}
	}
}

func TestFailureClassificationRedactsUnknownErrors(t *testing.T) {
	t.Parallel()

	code, disposition := ClassifyFailure(errors.New("secret provider response"))
	if code != ErrorCodeHandlerFailed || disposition != FailureRetry {
		t.Fatalf("unexpected untyped failure classification: %q %d", code, disposition)
	}
	code, disposition = ClassifyFailure(Permanent("INVALID RAW\nSECRET"))
	if code != ErrorCodeHandlerFailed || disposition != FailurePermanent {
		t.Fatalf("unexpected invalid-code normalization: %q %d", code, disposition)
	}
}
