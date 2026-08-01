package classroom

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeStudyMeetingBusyWindowsDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()
	firstUser := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	secondUser := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	startsAt := time.Date(2026, time.August, 4, 3, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(time.Hour)
	local := time.FixedZone("UTC+7", 7*60*60)

	normalized, userIDs, err := normalizeStudyMeetingBusyWindows([]studyMeetingBusyWindow{
		{UserID: secondUser, StartsAt: startsAt.Add(2 * time.Hour), EndsAt: endsAt.Add(2 * time.Hour)},
		{UserID: firstUser, StartsAt: startsAt.In(local), EndsAt: endsAt.In(local)},
		{UserID: firstUser, StartsAt: startsAt, EndsAt: endsAt},
		{UserID: secondUser, StartsAt: startsAt, EndsAt: endsAt},
	})
	if err != nil {
		t.Fatalf("normalize busy windows: %v", err)
	}
	if len(normalized) != 3 {
		t.Fatalf("normalized window count = %d, want 3", len(normalized))
	}
	if len(userIDs) != 2 || userIDs[0] != firstUser || userIDs[1] != secondUser {
		t.Fatalf("normalized user ids = %v, want sorted unique ids", userIDs)
	}
	if normalized[0].UserID != firstUser ||
		!normalized[0].StartsAt.Equal(startsAt) ||
		normalized[0].StartsAt.Location() != time.UTC {
		t.Fatalf("first normalized window = %+v", normalized[0])
	}
	if normalized[1].UserID != secondUser || !normalized[1].StartsAt.Equal(startsAt) ||
		normalized[2].UserID != secondUser ||
		!normalized[2].StartsAt.Equal(startsAt.Add(2*time.Hour)) {
		t.Fatalf("normalized windows are not deterministic: %+v", normalized)
	}
}

func TestNormalizeStudyMeetingBusyWindowsRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	userID := uuid.New()
	startsAt := time.Date(2026, time.August, 4, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		windows []studyMeetingBusyWindow
	}{
		{name: "empty"},
		{name: "missing user", windows: []studyMeetingBusyWindow{{
			StartsAt: startsAt, EndsAt: startsAt.Add(time.Hour),
		}}},
		{name: "missing start", windows: []studyMeetingBusyWindow{{
			UserID: userID, EndsAt: startsAt.Add(time.Hour),
		}}},
		{name: "empty range", windows: []studyMeetingBusyWindow{{
			UserID: userID, StartsAt: startsAt, EndsAt: startsAt,
		}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := normalizeStudyMeetingBusyWindows(test.windows); !errors.Is(
				err, ErrInvalidSessionInput,
			) {
				t.Fatalf("normalize error = %v, want invalid session input", err)
			}
		})
	}
}

func TestNewlyActiveOneTimeAudienceBusyWindowsUsesOnlyNewAttendees(t *testing.T) {
	t.Parallel()
	organizerID, currentID, newID := uuid.New(), uuid.New(), uuid.New()
	startsAt := time.Date(2026, time.August, 4, 3, 0, 0, 0, time.UTC)
	endsAt := startsAt.Add(time.Hour)
	windows := newlyActiveOneTimeAudienceBusyWindows(
		organizerID,
		[]persistedSessionAttendee{{UserID: currentID}},
		[]resolvedAudienceMember{
			{UserID: organizerID},
			{UserID: currentID},
			{UserID: newID},
			{UserID: newID},
		},
		startsAt,
		endsAt,
	)
	if len(windows) != 1 || windows[0].UserID != newID ||
		!windows[0].StartsAt.Equal(startsAt) || !windows[0].EndsAt.Equal(endsAt) {
		t.Fatalf("new busy windows = %+v, want only new attendee", windows)
	}
}
