package classroom

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
)

func TestRecurringEffectiveAudienceBusyWindowsAppliesPerUserOverrides(t *testing.T) {
	t.Parallel()
	organizerID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	baseUserID := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	occurrenceUserID := uuid.MustParse("30000000-0000-4000-8000-000000000003")
	removedUserID := uuid.MustParse("40000000-0000-4000-8000-000000000004")
	startsAt := time.Date(2026, time.August, 5, 3, 0, 0, 0, time.UTC)
	occurrences := []recurrence.Occurrence{
		{Key: "occurrence-0001", StartsAt: startsAt, EndsAt: startsAt.Add(time.Hour)},
		{
			Key: "occurrence-0002", StartsAt: startsAt.Add(24 * time.Hour),
			EndsAt: startsAt.Add(25 * time.Hour),
		},
	}
	snapshot := recurringParticipationConflictSnapshot{
		OrganizerUserID: organizerID,
		BaseAssignments: map[uuid.UUID]bool{
			organizerID: false, baseUserID: true, removedUserID: false,
		},
		OccurrenceAssignments: map[string]map[uuid.UUID]bool{
			"occurrence-0001": {
				organizerID: false, baseUserID: false, occurrenceUserID: true,
			},
		},
	}

	windows := recurringEffectiveAudienceBusyWindows(snapshot, occurrences)
	if len(windows) != 4 {
		t.Fatalf("busy window count = %d, want 4: %+v", len(windows), windows)
	}
	wantUsers := []uuid.UUID{organizerID, occurrenceUserID, organizerID, baseUserID}
	for index, wantUserID := range wantUsers {
		if windows[index].UserID != wantUserID {
			t.Fatalf("window %d user = %s, want %s", index, windows[index].UserID, wantUserID)
		}
	}
	if !windows[0].StartsAt.Equal(occurrences[0].StartsAt) ||
		!windows[2].StartsAt.Equal(occurrences[1].StartsAt) {
		t.Fatalf("windows do not retain occurrence times: %+v", windows)
	}
}

func TestNewlyActiveRecurringSeriesAudienceBusyWindowsUsesExactFinalEdges(t *testing.T) {
	t.Parallel()
	existingBaseID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	existingOccurrenceID := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	removedOccurrenceID := uuid.MustParse("30000000-0000-4000-8000-000000000003")
	startsAt := time.Date(2026, time.August, 5, 3, 0, 0, 0, time.UTC)
	occurrences := []recurrence.Occurrence{
		{Key: "occurrence-0001", StartsAt: startsAt, EndsAt: startsAt.Add(time.Hour)},
		{
			Key: "occurrence-0002", StartsAt: startsAt.Add(24 * time.Hour),
			EndsAt: startsAt.Add(25 * time.Hour),
		},
	}
	windows := newlyActiveRecurringSeriesAudienceBusyWindows(
		[]uuid.UUID{existingBaseID, existingOccurrenceID, removedOccurrenceID},
		occurrences,
		map[uuid.UUID]bool{existingBaseID: true},
		map[string]map[uuid.UUID]bool{
			"occurrence-0001": {
				existingOccurrenceID: true,
				removedOccurrenceID:  false,
			},
		},
	)

	if len(windows) != 2 {
		t.Fatalf("new busy window count = %d, want 2: %+v", len(windows), windows)
	}
	if windows[0].UserID != existingOccurrenceID ||
		windows[1].UserID != removedOccurrenceID ||
		!windows[0].StartsAt.Equal(occurrences[1].StartsAt) ||
		!windows[1].StartsAt.Equal(occurrences[1].StartsAt) {
		t.Fatalf("new busy windows = %+v", windows)
	}
}

func TestRecurringParticipantPresentUsesLatestOccurrenceAssignment(t *testing.T) {
	t.Parallel()
	organizerID, userID := uuid.New(), uuid.New()
	snapshot := recurringParticipationConflictSnapshot{
		OrganizerUserID: organizerID,
		BaseAssignments: map[uuid.UUID]bool{userID: true},
		OccurrenceAssignments: map[string]map[uuid.UUID]bool{
			"removed-occurrence": {userID: false},
			"added-occurrence":   {userID: true},
		},
	}
	if !recurringParticipantPresent(snapshot, organizerID, "removed-occurrence") {
		t.Fatal("organizer must remain present despite an occurrence removal")
	}
	if recurringParticipantPresent(snapshot, userID, "removed-occurrence") {
		t.Fatal("occurrence removal must override an active base assignment")
	}
	if !recurringParticipantPresent(snapshot, userID, "added-occurrence") {
		t.Fatal("occurrence active assignment must keep the participant present")
	}
	if !recurringParticipantPresent(snapshot, userID, "unmodified-occurrence") {
		t.Fatal("active base assignment must apply without an occurrence override")
	}
}

func TestSeriesMutationChangesScheduleRejectsMetadataOnlyReverseCheck(t *testing.T) {
	t.Parallel()
	title := "Updated title"
	startsAt := "2026-08-05T10:00:00"
	rule := recurrence.Rule{Frequency: recurrence.FrequencyWeekly}
	overlapPolicy := recurrence.OverlapEarlier

	if seriesMutationChangesSchedule(SeriesMutationParams{
		OccurrenceMutationInput: OccurrenceMutationInput{Title: &title},
	}) {
		t.Fatal("metadata-only mutation must not create an owner-time edge")
	}
	for name, params := range map[string]SeriesMutationParams{
		"start": {
			OccurrenceMutationInput: OccurrenceMutationInput{StartsAt: &startsAt},
		},
		"rule": {
			OccurrenceMutationInput: OccurrenceMutationInput{Rule: &rule},
		},
		"overlap policy": {
			OccurrenceMutationInput: OccurrenceMutationInput{OverlapPolicy: &overlapPolicy},
		},
	} {
		params := params
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !seriesMutationChangesSchedule(params) {
				t.Fatal("schedule mutation must require an owner-time reverse check")
			}
		})
	}
}
