package audit

import "testing"

func TestClassSessionDomainEventsMapToAuditableActions(t *testing.T) {
	t.Parallel()
	tests := map[string]Action{
		"class_session.scheduled.v1":   ActionClassSessionCreate,
		"class_session.rescheduled.v1": ActionClassSessionUpdate,
		"class_session.cancelled.v1":   ActionClassSessionCancel,
	}
	for eventType, want := range tests {
		got, ok := ActionForDomainEvent(eventType)
		if !ok || got != want {
			t.Fatalf("event %q = %q, %t; want %q", eventType, got, ok, want)
		}
	}
}
