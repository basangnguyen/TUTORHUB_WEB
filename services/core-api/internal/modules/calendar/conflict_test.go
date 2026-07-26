package calendar

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDetectClassConflictsUsesHalfOpenIntervals(t *testing.T) {
	t.Parallel()
	classID := uuid.New()
	existing := []BusyInterval{{
		ID: "existing", ClassID: classID,
		StartsAt: time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC),
		EndsAt:   time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC),
	}}
	candidates := []BusyInterval{
		{ID: "touching", ClassID: classID, StartsAt: existing[0].EndsAt, EndsAt: existing[0].EndsAt.Add(time.Hour)},
		{ID: "overlap", ClassID: classID, StartsAt: existing[0].StartsAt.Add(30 * time.Minute), EndsAt: existing[0].EndsAt.Add(time.Hour)},
	}
	conflicts := DetectClassConflicts(candidates, existing)
	if len(conflicts) != 1 || conflicts[0].CandidateID != "overlap" {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
	if err := RequireNoClassConflict(conflicts, false, ""); !errors.Is(err, ErrClassScheduleConflict) {
		t.Fatalf("expected hard conflict, got %v", err)
	}
	if err := RequireNoClassConflict(conflicts, false, "admin override reason"); !errors.Is(err, ErrClassScheduleConflict) {
		t.Fatalf("reason without capability must not authorize override, got %v", err)
	}
	if err := RequireNoClassConflict(conflicts, true, " "); !errors.Is(err, ErrClassScheduleConflict) {
		t.Fatalf("capability without a reason must not authorize override, got %v", err)
	}
	if err := RequireNoClassConflict(conflicts, true, "admin override reason"); err != nil {
		t.Fatalf("expected explicit override to pass, got %v", err)
	}
}

func TestDetectClassConflictsIsTenantNeutralAndStable(t *testing.T) {
	t.Parallel()
	classID := uuid.New()
	start := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	conflicts := DetectClassConflicts(
		[]BusyInterval{{ID: "b", ClassID: classID, StartsAt: start, EndsAt: start.Add(time.Hour)}},
		[]BusyInterval{
			{ID: "z", ClassID: classID, StartsAt: start.Add(15 * time.Minute), EndsAt: start.Add(2 * time.Hour)},
			{ID: "a", ClassID: classID, StartsAt: start.Add(10 * time.Minute), EndsAt: start.Add(90 * time.Minute)},
		},
	)
	if len(conflicts) != 2 || conflicts[0].ExistingID != "a" || conflicts[1].ExistingID != "z" {
		t.Fatalf("conflicts are not deterministic: %+v", conflicts)
	}
}
