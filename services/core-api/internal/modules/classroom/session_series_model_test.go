package classroom

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
)

func TestNormalizeCreateSeriesInputAcceptsCalendarQuickCreatePayload(t *testing.T) {
	t.Parallel()

	params, occurrences, err := normalizeCreateSeriesInput(
		context.Background(),
		CreateSeriesInput{
			Title:       "P3-02B Recurrence Canary 20260728",
			Description: "staging recurrence acceptance",
			StartsAt:    "2026-07-29T13:00:00+07:00",
			EndsAt:      "2026-07-29T14:00:00+07:00",
			Timezone:    "Asia/Ho_Chi_Minh",
			Rule: recurrence.Rule{
				Frequency: recurrence.FrequencyWeekly,
				Interval:  1,
				End: recurrence.End{
					Type:  recurrence.EndAfterCount,
					Count: 3,
				},
			},
			OverlapPolicy: recurrence.OverlapReject,
		},
		uuid.MustParse("bcb94c1e-1984-4852-8a86-143620641587"),
	)
	if err != nil {
		t.Fatalf("normalize quick-create payload: %v", err)
	}
	if params.LocalStart != "2026-07-29T13:00:00" {
		t.Fatalf("local start = %q", params.LocalStart)
	}
	if len(occurrences) != 3 {
		t.Fatalf("occurrence count = %d, want 3", len(occurrences))
	}
}

func TestNormalizeCreateSeriesInputExpandsAcrossBoundedWindows(t *testing.T) {
	t.Parallel()

	_, occurrences, err := normalizeCreateSeriesInput(
		context.Background(),
		CreateSeriesInput{
			Title:    "Bounded recurrence expansion",
			StartsAt: "2026-01-01T09:00:00Z",
			EndsAt:   "2026-01-01T10:00:00Z",
			Timezone: "UTC",
			Rule: recurrence.Rule{
				Frequency: recurrence.FrequencyDaily,
				Interval:  1,
				End: recurrence.End{
					Type:  recurrence.EndAfterCount,
					Count: recurrence.MaxOccurrences,
				},
			},
			OverlapPolicy: recurrence.OverlapReject,
		},
		uuid.MustParse("bcb94c1e-1984-4852-8a86-143620641587"),
	)
	if err != nil {
		t.Fatalf("normalize long series: %v", err)
	}
	if len(occurrences) != recurrence.MaxOccurrences {
		t.Fatalf(
			"occurrence count = %d, want %d",
			len(occurrences),
			recurrence.MaxOccurrences,
		)
	}
	if !occurrences[len(occurrences)-1].StartsAt.Equal(
		time.Date(2027, time.May, 27, 9, 0, 0, 0, time.UTC),
	) {
		t.Fatalf("unexpected final occurrence: %s", occurrences[len(occurrences)-1].StartsAt)
	}
}

func TestNormalizeCreateSeriesInputIncludesExactHorizonBoundary(t *testing.T) {
	t.Parallel()

	_, occurrences, err := normalizeCreateSeriesInput(
		context.Background(),
		CreateSeriesInput{
			Title:    "Boundary recurrence expansion",
			StartsAt: "2026-01-01T09:00:00Z",
			EndsAt:   "2026-01-01T10:00:00Z",
			Timezone: "UTC",
			Rule: recurrence.Rule{
				Frequency: recurrence.FrequencyYearly,
				Interval:  1,
				MonthDays: []int{1},
				Months:    []int{1},
				End: recurrence.End{
					Type:  recurrence.EndAfterCount,
					Count: 3,
				},
			},
			OverlapPolicy: recurrence.OverlapReject,
		},
		uuid.MustParse("bcb94c1e-1984-4852-8a86-143620641587"),
	)
	if err != nil {
		t.Fatalf("normalize boundary series: %v", err)
	}
	if len(occurrences) != 3 {
		t.Fatalf("occurrence count = %d, want 3", len(occurrences))
	}
	if !occurrences[2].StartsAt.Equal(
		time.Date(2028, time.January, 1, 9, 0, 0, 0, time.UTC),
	) {
		t.Fatalf("unexpected boundary occurrence: %s", occurrences[2].StartsAt)
	}
}
