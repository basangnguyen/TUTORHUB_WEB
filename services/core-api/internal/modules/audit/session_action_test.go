package audit

import "testing"

func TestClassSessionDomainEventsMapToAuditableActions(t *testing.T) {
	t.Parallel()
	tests := map[string]Action{
		"class_session.scheduled.v1":                  ActionClassSessionCreate,
		"class_session.rescheduled.v1":                ActionClassSessionUpdate,
		"class_session.cancelled.v1":                  ActionClassSessionCancel,
		"class_session_series.scheduled.v1":           ActionClassSessionCreate,
		"class_session_series.updated.v1":             ActionClassSessionUpdate,
		"class_session_series.split.v1":               ActionClassSessionUpdate,
		"class_session_series.cancelled.v1":           ActionClassSessionCancel,
		"class_session_series.following_cancelled.v1": ActionClassSessionCancel,
		"class_session_occurrence.updated.v1":         ActionClassSessionUpdate,
		"class_session_occurrence.cancelled.v1":       ActionClassSessionCancel,
		"class_session.audience_replaced.v1":          ActionClassSessionAudienceReplace,
		"class_session.rsvp_responded.v1":             ActionClassSessionRSVPRespond,
	}
	for eventType, want := range tests {
		got, ok := ActionForDomainEvent(eventType)
		if !ok || got != want {
			t.Fatalf("event %q = %q, %t; want %q", eventType, got, ok, want)
		}
	}
}
