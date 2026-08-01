package audit

import "testing"

func TestAvailabilityPollAndStudyMeetingDomainEventsMapToAuditableActions(t *testing.T) {
	t.Parallel()

	tests := map[string]Action{
		"availability_poll.created.v1":            ActionAvailabilityPollCreate,
		"availability_poll.updated.v1":            ActionAvailabilityPollUpdate,
		"availability_poll.opened.v1":             ActionAvailabilityPollOpen,
		"availability_poll.reopened.v1":           ActionAvailabilityPollReopen,
		"availability_poll.response_recorded.v1":  ActionAvailabilityPollRespond,
		"availability_poll.closed.v1":             ActionAvailabilityPollClose,
		"availability_poll.cancelled.v1":          ActionAvailabilityPollCancel,
		"availability_poll.capability_issued.v1":  ActionAvailabilityPollShare,
		"availability_poll.capability_revoked.v1": ActionAvailabilityPollRevoke,
		"availability_poll.finalized.v1":          ActionAvailabilityPollFinalize,
		"study_meeting.scheduled.v1":              ActionStudyMeetingCreate,
		"study_meeting.rescheduled.v1":            ActionStudyMeetingUpdate,
		"study_meeting.cancelled.v1":              ActionStudyMeetingCancel,
	}
	for eventType, want := range tests {
		got, ok := ActionForDomainEvent(eventType)
		if !ok || got != want {
			t.Fatalf("event %q = %q, %t; want %q", eventType, got, ok, want)
		}
	}
}

func TestAvailabilityPollAndStudyMeetingActionsAreCataloged(t *testing.T) {
	t.Parallel()

	cataloged := make(map[Action]struct{}, len(Actions()))
	for _, action := range Actions() {
		cataloged[action] = struct{}{}
	}

	want := []Action{
		ActionAvailabilityPollCreate,
		ActionAvailabilityPollUpdate,
		ActionAvailabilityPollOpen,
		ActionAvailabilityPollClose,
		ActionAvailabilityPollReopen,
		ActionAvailabilityPollCancel,
		ActionAvailabilityPollRespond,
		ActionAvailabilityPollShare,
		ActionAvailabilityPollRevoke,
		ActionAvailabilityPollFinalize,
		ActionStudyMeetingCreate,
		ActionStudyMeetingUpdate,
		ActionStudyMeetingCancel,
	}
	for _, action := range want {
		if _, ok := cataloged[action]; !ok {
			t.Fatalf("action %q is not present in the audit catalog", action)
		}
	}
}
