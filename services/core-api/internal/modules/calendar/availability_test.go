package calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSuggestedTimeRanksUnknownBeforeBusyUsingLockedTuple(t *testing.T) {
	t.Parallel()
	required := schedulingParticipant{
		Reference: AvailabilityParticipantReference{Kind: "internal_user", ID: uuid.New().String()},
		ID:        uuid.New(), Role: "required",
	}
	params := availabilityParams{
		From:     time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC),
		Timezone: "UTC", Duration: time.Hour, Step: time.Hour,
		MaxCandidates: 3, Participants: []schedulingParticipant{required},
	}
	result, err := buildAvailabilityResult(context.Background(), params, []availabilitySource{{
		Participant:      required,
		WorkingIntervals: []AvailabilityWorkingInterval{{StartsAt: params.From, EndsAt: params.To}},
		Intervals: []AvailabilityStatusInterval{
			{StartsAt: params.From, EndsAt: params.From.Add(time.Hour), Status: "busy"},
			{StartsAt: params.From.Add(time.Hour), EndsAt: params.From.Add(2 * time.Hour), Status: "unknown"},
		},
	}})
	if err != nil {
		t.Fatalf("build availability: %v", err)
	}
	if len(result.Suggestions) != 3 {
		t.Fatalf("suggestions = %d", len(result.Suggestions))
	}
	if !result.Suggestions[0].StartsAt.Equal(params.From.Add(2*time.Hour)) ||
		result.Suggestions[1].ReasonBreakdown.RequiredUnknown != 1 ||
		result.Suggestions[2].ReasonBreakdown.RequiredBusy != 1 {
		t.Fatalf("unexpected deterministic order: %+v", result.Suggestions)
	}
}

func TestAvailabilityCivilGridSkipsDSTGapAndKeepsBothOverlapInstants(t *testing.T) {
	t.Parallel()
	gapParams := availabilityParams{
		From:     time.Date(2026, 3, 8, 6, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
		Timezone: "America/New_York", Duration: 30 * time.Minute, Step: 30 * time.Minute,
	}
	gapStarts, err := availabilityCivilStarts(context.Background(), gapParams)
	if err != nil {
		t.Fatalf("build gap grid: %v", err)
	}
	gapLocation, _ := time.LoadLocation("America/New_York")
	for _, start := range gapStarts {
		if start.In(gapLocation).Format("15:04") == "02:00" {
			t.Fatalf("nonexistent civil slot leaked: %v", start)
		}
	}

	overlapParams := availabilityParams{
		From:     time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC),
		Timezone: "America/New_York", Duration: 30 * time.Minute, Step: 30 * time.Minute,
	}
	overlapStarts, err := availabilityCivilStarts(context.Background(), overlapParams)
	if err != nil {
		t.Fatalf("build overlap grid: %v", err)
	}
	location, _ := time.LoadLocation("America/New_York")
	countOneThirty := 0
	for _, start := range overlapStarts {
		if start.In(location).Format("15:04") == "01:30" {
			countOneThirty++
		}
	}
	if countOneThirty != 2 {
		t.Fatalf("01:30 overlap count = %d, want 2 (%v)", countOneThirty, overlapStarts)
	}
}

func TestAvailabilityCivilGridRejectsCandidateCapacityOverflow(t *testing.T) {
	t.Parallel()
	params := availabilityParams{
		From:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		Timezone: "UTC", Duration: 15 * time.Minute, Step: 15 * time.Minute,
	}
	if _, err := availabilityCivilStarts(context.Background(), params); !errors.Is(err, ErrAvailabilityCapacity) {
		t.Fatalf("candidate overflow error = %v, want capacity error", err)
	}
}

func TestExternalParticipantIsAlwaysUnknown(t *testing.T) {
	t.Parallel()
	participant := schedulingParticipant{
		Reference: AvailabilityParticipantReference{Kind: "external_guest", ID: uuid.New().String()},
		ID:        uuid.New(), Role: "optional",
	}
	params := availabilityParams{
		From:     time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC),
		Timezone: "UTC", Duration: 30 * time.Minute, Step: 30 * time.Minute,
		MaxCandidates: 2, Participants: []schedulingParticipant{participant},
	}
	result, err := buildAvailabilityResult(context.Background(), params, []availabilitySource{{
		Participant: participant, Unknown: true,
	}})
	if err != nil {
		t.Fatalf("build availability: %v", err)
	}
	if len(result.Participants) != 1 || len(result.Participants[0].Intervals) != 1 ||
		result.Participants[0].Intervals[0].Status != "unknown" ||
		result.Suggestions[0].ReasonBreakdown.OptionalUnknown != 1 {
		t.Fatalf("external projection was not fail-closed: %+v", result)
	}
}
