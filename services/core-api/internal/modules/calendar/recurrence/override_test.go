package recurrence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestResolveOccurrenceUsesBoundedDSTRules(t *testing.T) {
	t.Parallel()

	_, err := ResolveOccurrence(context.Background(), Definition{
		ID:            "dst-gap",
		StartLocal:    "2026-03-08T02:30:00",
		TimeZone:      "America/New_York",
		Duration:      time.Hour,
		OverlapPolicy: OverlapReject,
	}, "2026-03-08T02:30:00")
	if !errors.Is(err, ErrNonexistentCivilTime) {
		t.Fatalf("DST gap error = %v, want nonexistent civil time", err)
	}
}

func TestProjectAppliesCancelAndOverrideWithoutChangingIdentity(t *testing.T) {
	t.Parallel()

	seriesID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	classID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	definition := Definition{
		ID:         seriesID.String(),
		StartLocal: "2026-07-20T09:00:00",
		TimeZone:   "Asia/Ho_Chi_Minh",
		Duration:   time.Hour,
		Rule: Rule{
			Frequency: FrequencyWeekly,
			Interval:  1,
			Weekdays:  []Weekday{Monday},
			End:       End{Type: EndAfterCount, Count: 3},
		},
		OverlapPolicy: OverlapReject,
	}
	window := Window{
		Start: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
	}
	base, err := Expand(context.Background(), definition, window, MaxOccurrences)
	if err != nil {
		t.Fatalf("expand base series: %v", err)
	}
	if len(base) != 3 {
		t.Fatalf("base occurrence count = %d, want 3", len(base))
	}
	overrideTitle := "Dời buổi học"
	overrideLocal := "2026-07-27T11:00:00"
	overrideDuration := 90 * time.Minute
	projected, err := Project(context.Background(), SeriesProjection{
		ID: seriesID, ClassID: classID, ClassTitle: "Toán 9", Title: "Toán",
		DisplayTimezone: "Asia/Ho_Chi_Minh", Definition: definition,
	}, window, []ExceptionProjection{
		{OccurrenceKey: base[0].Key, Type: ExceptionCancel},
		{
			OccurrenceKey:      base[1].Key,
			Type:               ExceptionOverride,
			OverrideLocalStart: &overrideLocal,
			OverrideDuration:   &overrideDuration,
			OverrideTitle:      &overrideTitle,
		},
	}, MaxOccurrencesPerRequest)
	if err != nil {
		t.Fatalf("project exceptions: %v", err)
	}
	if len(projected) != 2 {
		t.Fatalf("projected occurrence count = %d, want 2", len(projected))
	}
	if projected[0].OccurrenceKey != base[1].Key ||
		projected[0].Title != overrideTitle ||
		projected[0].OriginalLocal != base[1].OriginalLocal ||
		projected[0].EndsAt.Sub(projected[0].StartsAt) != overrideDuration {
		t.Fatalf("override changed identity or payload: %+v", projected[0])
	}
	if projected[1].OccurrenceKey != base[2].Key {
		t.Fatalf("cancelled occurrence was not removed: %+v", projected)
	}
}

func TestProjectRejectsDuplicateOrUnknownExceptionType(t *testing.T) {
	t.Parallel()

	seriesID := uuid.New()
	classID := uuid.New()
	definition := Definition{
		ID: seriesID.String(), StartLocal: "2026-07-20T09:00:00",
		TimeZone: "Asia/Ho_Chi_Minh", Duration: time.Hour,
		Rule: Rule{
			Frequency: FrequencyDaily, End: End{Type: EndAfterCount, Count: 1},
		},
	}
	window := Window{
		Start: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC),
	}
	_, err := Project(context.Background(), SeriesProjection{
		ID: seriesID, ClassID: classID, Definition: definition,
	}, window, []ExceptionProjection{
		{OccurrenceKey: "same-key", Type: ExceptionCancel},
		{OccurrenceKey: "same-key", Type: ExceptionOverride},
	}, MaxOccurrencesPerRequest)
	if !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("duplicate exception error = %v, want invalid rule", err)
	}
}
