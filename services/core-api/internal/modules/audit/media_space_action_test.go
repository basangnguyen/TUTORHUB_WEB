package audit

import "testing"

func TestMediaSpaceDomainEventsMapToAuditableActions(t *testing.T) {
	t.Parallel()

	tests := map[string]Action{
		"media_space.created.v1":   ActionMediaSpaceCreate,
		"media_space.started.v1":   ActionMediaSpaceStart,
		"media_space.ended.v1":     ActionMediaSpaceEnd,
		"media_space.cancelled.v1": ActionMediaSpaceCancel,
	}
	for eventType, want := range tests {
		got, ok := ActionForDomainEvent(eventType)
		if !ok || got != want {
			t.Fatalf("event %q mapped to %q, %t; want %q", eventType, got, ok, want)
		}
	}
}
