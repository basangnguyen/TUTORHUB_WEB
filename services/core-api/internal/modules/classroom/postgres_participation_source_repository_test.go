package classroom

import (
	"testing"

	"github.com/google/uuid"
)

func TestMergeInheritedParticipationAttendeesUsesOnlyScopedOverrides(t *testing.T) {
	t.Parallel()

	firstUser := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	secondUser := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	staleUser := uuid.MustParse("30000000-0000-4000-8000-000000000003")
	inherited := []persistedSessionAttendee{
		{UserID: firstUser, RSVPState: RSVPStateAccepted, Version: 3},
		{UserID: secondUser, RSVPState: RSVPStateNeedsAction, Version: 4},
	}
	override := persistedSessionAttendee{
		UserID: firstUser, RSVPState: RSVPStateDeclined, Version: 7,
	}
	merged := mergeInheritedParticipationAttendees(
		inherited,
		[]persistedSessionAttendee{
			override,
			{UserID: staleUser, RSVPState: RSVPStateTentative, Version: 9},
		},
	)

	if len(merged) != 2 {
		t.Fatalf("merged attendee count = %d, want 2", len(merged))
	}
	if merged[0].UserID != firstUser ||
		merged[0].RSVPState != RSVPStateDeclined ||
		merged[0].Version != 7 {
		t.Fatalf("first inherited attendee was not overridden: %+v", merged[0])
	}
	if merged[1].UserID != secondUser ||
		merged[1].RSVPState != RSVPStateNeedsAction ||
		merged[1].Version != 4 {
		t.Fatalf("second inherited attendee changed unexpectedly: %+v", merged[1])
	}
}
