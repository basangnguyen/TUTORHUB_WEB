package calendar

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var schedulingTestNow = time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)

func TestPutWorkingScheduleNormalizesAndSortsIntervals(t *testing.T) {
	t.Parallel()
	repository := &recordingSchedulingRepository{}
	service, err := NewSchedulingService(repository, func() time.Time { return schedulingTestNow })
	if err != nil {
		t.Fatalf("new scheduling service: %v", err)
	}
	scope, _ := tenancy.New(uuid.New(), uuid.New())

	_, err = service.PutWorkingSchedule(context.Background(), scope, PutWorkingScheduleInput{
		Timezone: " Asia/Ho_Chi_Minh ", ExpectedVersion: 2,
		WeeklyIntervals: []WeeklyWorkingInterval{
			{Weekday: "tuesday", CivilTimeInterval: CivilTimeInterval{StartsAt: "13:00", EndsAt: "17:30"}},
			{Weekday: "monday", CivilTimeInterval: CivilTimeInterval{StartsAt: "9:00", EndsAt: "12:00"}},
		},
		Exceptions: []WorkingScheduleException{
			{Date: "2026-09-02", Kind: "holiday"},
			{Date: "2026-08-12", Kind: "special_hours", Intervals: []CivilTimeInterval{{StartsAt: "10:00", EndsAt: "12:00"}}},
		},
	})
	if err != nil {
		t.Fatalf("put working schedule: %v", err)
	}
	got := repository.putInput
	if got.Timezone != "Asia/Ho_Chi_Minh" || got.ExpectedVersion != 2 {
		t.Fatalf("normalized schedule = %+v", got)
	}
	if len(got.WeeklyIntervals) != 2 || got.WeeklyIntervals[0].Weekday != "monday" ||
		got.WeeklyIntervals[0].StartsAt != "09:00" {
		t.Fatalf("weekly intervals not canonical: %+v", got.WeeklyIntervals)
	}
	if len(got.Exceptions) != 2 || got.Exceptions[0].Date != "2026-08-12" {
		t.Fatalf("exceptions not sorted: %+v", got.Exceptions)
	}
}

func TestPutWorkingScheduleRejectsOverlapAndInvalidException(t *testing.T) {
	t.Parallel()
	service, _ := NewSchedulingService(&recordingSchedulingRepository{}, func() time.Time {
		return schedulingTestNow
	})
	scope, _ := tenancy.New(uuid.New(), uuid.New())
	tests := []PutWorkingScheduleInput{
		{
			Timezone: "UTC",
			WeeklyIntervals: []WeeklyWorkingInterval{
				{Weekday: "monday", CivilTimeInterval: CivilTimeInterval{StartsAt: "09:00", EndsAt: "12:00"}},
				{Weekday: "monday", CivilTimeInterval: CivilTimeInterval{StartsAt: "11:59", EndsAt: "13:00"}},
			},
		},
		{
			Timezone: "UTC",
			Exceptions: []WorkingScheduleException{{
				Date: "2026-09-02", Kind: "holiday",
				Intervals: []CivilTimeInterval{{StartsAt: "09:00", EndsAt: "10:00"}},
			}},
		},
		{Timezone: "local"},
	}
	for _, input := range tests {
		if _, err := service.PutWorkingSchedule(context.Background(), scope, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("input %+v error = %v, want invalid input", input, err)
		}
	}
}

func TestWorkingScheduleInputEnforcesPublishedCapacityBoundaries(t *testing.T) {
	t.Parallel()

	weekly := make([]WeeklyWorkingInterval, MaximumWorkingIntervalsDay)
	for index := range weekly {
		weekly[index] = WeeklyWorkingInterval{
			Weekday: "monday",
			CivilTimeInterval: CivilTimeInterval{
				StartsAt: fmt.Sprintf("%02d:00", index*2),
				EndsAt:   fmt.Sprintf("%02d:00", index*2+1),
			},
		}
	}
	normalized, err := normalizeWorkingScheduleInput(
		PutWorkingScheduleInput{
			Timezone:        "UTC",
			WeeklyIntervals: weekly,
		},
		schedulingTestNow,
	)
	if err != nil || len(normalized.WeeklyIntervals) != MaximumWorkingIntervalsDay {
		t.Fatalf("maximum weekly intervals rejected: value=%+v error=%v", normalized, err)
	}
	overWeekly := append(
		append([]WeeklyWorkingInterval(nil), weekly...),
		WeeklyWorkingInterval{
			Weekday: "monday",
			CivilTimeInterval: CivilTimeInterval{
				StartsAt: "16:00",
				EndsAt:   "17:00",
			},
		},
	)
	if _, err := normalizeWorkingScheduleInput(
		PutWorkingScheduleInput{
			Timezone:        "UTC",
			WeeklyIntervals: overWeekly,
		},
		schedulingTestNow,
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("over-limit weekly intervals error = %v, want invalid input", err)
	}

	exceptions := make([]WorkingScheduleException, MaximumWorkingExceptions)
	for index := range exceptions {
		exceptions[index] = WorkingScheduleException{
			Date: schedulingTestNow.AddDate(0, 0, index).Format("2006-01-02"),
			Kind: "holiday",
		}
	}
	normalized, err = normalizeWorkingScheduleInput(
		PutWorkingScheduleInput{
			Timezone:   "UTC",
			Exceptions: exceptions,
		},
		schedulingTestNow,
	)
	if err != nil || len(normalized.Exceptions) != MaximumWorkingExceptions {
		t.Fatalf("maximum exceptions rejected: count=%d error=%v", len(normalized.Exceptions), err)
	}
	overExceptions := append(
		append([]WorkingScheduleException(nil), exceptions...),
		WorkingScheduleException{
			Date: schedulingTestNow.
				AddDate(0, 0, MaximumWorkingExceptions).
				Format("2006-01-02"),
			Kind: "holiday",
		},
	)
	if _, err := normalizeWorkingScheduleInput(
		PutWorkingScheduleInput{
			Timezone:   "UTC",
			Exceptions: overExceptions,
		},
		schedulingTestNow,
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("over-limit exceptions error = %v, want invalid input", err)
	}
}

func TestAvailabilityInputRejectsDuplicateAndUnboundedQueries(t *testing.T) {
	t.Parallel()
	participantID := uuid.New().String()
	base := AvailabilityQueryInput{
		ClassID: uuid.New().String(),
		From:    "2026-07-28T09:00:00Z", To: "2026-07-29T09:00:00Z",
		Timezone: "UTC", DurationMinutes: 60, StepMinutes: 30,
		Required: []AvailabilityParticipantReference{{Kind: "internal_user", ID: participantID}},
	}
	duplicate := base
	duplicate.Optional = []AvailabilityParticipantReference{{Kind: "internal_user", ID: participantID}}
	if _, err := normalizeAvailabilityInput(duplicate); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate error = %v", err)
	}
	tooWide := base
	tooWide.To = "2026-09-28T09:00:00Z"
	if _, err := normalizeAvailabilityInput(tooWide); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("wide range error = %v", err)
	}
}

func TestAvailabilityInputEnforcesPublishedCapacityBoundaries(t *testing.T) {
	t.Parallel()

	required := make(
		[]AvailabilityParticipantReference,
		MaximumAvailabilityPeople,
	)
	for index := range required {
		required[index] = AvailabilityParticipantReference{
			Kind: "internal_user",
			ID:   uuid.New().String(),
		}
	}
	base := AvailabilityQueryInput{
		ClassID:         uuid.New().String(),
		From:            "2026-07-28T09:00:00Z",
		To:              "2026-07-29T09:00:00Z",
		Timezone:        "UTC",
		DurationMinutes: 15,
		StepMinutes:     15,
		MaxCandidates:   MaximumSuggestedCandidates,
		Required:        required,
	}
	params, err := normalizeAvailabilityInput(base)
	if err != nil {
		t.Fatalf("published maximum availability input rejected: %v", err)
	}
	if len(params.Participants) != MaximumAvailabilityPeople ||
		params.Duration != 15*time.Minute ||
		params.Step != 15*time.Minute ||
		params.MaxCandidates != MaximumSuggestedCandidates {
		t.Fatalf("unexpected maximum availability params: %+v", params)
	}

	maximumDuration := base
	maximumDuration.DurationMinutes = 480
	maximumDuration.StepMinutes = 60
	if _, err := normalizeAvailabilityInput(maximumDuration); err != nil {
		t.Fatalf("maximum duration and allowed step rejected: %v", err)
	}
	middleStep := base
	middleStep.StepMinutes = 30
	if _, err := normalizeAvailabilityInput(middleStep); err != nil {
		t.Fatalf("allowed 30-minute step rejected: %v", err)
	}
	defaultCandidates := base
	defaultCandidates.MaxCandidates = 0
	params, err = normalizeAvailabilityInput(defaultCandidates)
	if err != nil || params.MaxCandidates != 10 {
		t.Fatalf(
			"default candidate limit = %d, error=%v, want 10",
			params.MaxCandidates,
			err,
		)
	}

	overPeople := base
	overPeople.Optional = []AvailabilityParticipantReference{{
		Kind: "internal_user",
		ID:   uuid.New().String(),
	}}
	tests := []struct {
		name  string
		input AvailabilityQueryInput
	}{
		{name: "over people", input: overPeople},
		{
			name: "duration below minimum",
			input: func() AvailabilityQueryInput {
				value := base
				value.DurationMinutes = 14
				return value
			}(),
		},
		{
			name: "duration above maximum",
			input: func() AvailabilityQueryInput {
				value := base
				value.DurationMinutes = 481
				return value
			}(),
		},
		{
			name: "unsupported step",
			input: func() AvailabilityQueryInput {
				value := base
				value.StepMinutes = 45
				return value
			}(),
		},
		{
			name: "over candidate limit",
			input: func() AvailabilityQueryInput {
				value := base
				value.MaxCandidates = MaximumSuggestedCandidates + 1
				return value
			}(),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := normalizeAvailabilityInput(test.input); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want invalid input", err)
			}
		})
	}
}

func TestAvailabilityInputAllowsThirtyOneCalendarDaysAcrossDSTFallBack(t *testing.T) {
	t.Parallel()
	input := AvailabilityQueryInput{
		ClassID: uuid.New().String(),
		// Midnight Oct 2 through midnight Nov 2 is 31 civil days in New York,
		// but spans the November DST fall-back and therefore exceeds 31*24 hours.
		From:            "2026-10-02T04:00:00Z",
		To:              "2026-11-02T05:00:00Z",
		Timezone:        "America/New_York",
		DurationMinutes: 60,
		StepMinutes:     30,
		Required: []AvailabilityParticipantReference{{
			Kind: "internal_user",
			ID:   uuid.New().String(),
		}},
	}
	if _, err := normalizeAvailabilityInput(input); err != nil {
		t.Fatalf("31 civil-day New York range error = %v", err)
	}

	overMaximum := input
	overMaximum.To = "2026-11-02T05:00:01Z"
	if _, err := normalizeAvailabilityInput(overMaximum); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("range past 31 civil days error = %v, want invalid range", err)
	}
}

func TestQueryAvailabilityAppliesEndToEndExecutionBudget(t *testing.T) {
	t.Parallel()
	repository := &recordingSchedulingRepository{
		loadAvailability: func(ctx context.Context) ([]availabilitySource, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	service, err := NewSchedulingService(repository, func() time.Time { return schedulingTestNow })
	if err != nil {
		t.Fatalf("new scheduling service: %v", err)
	}
	scope, _ := tenancy.New(uuid.New(), uuid.New())
	startedAt := time.Now()
	_, err = service.QueryAvailability(context.Background(), scope, AvailabilityQueryInput{
		ClassID: uuid.New().String(),
		From:    "2026-07-28T09:00:00Z", To: "2026-07-28T12:00:00Z",
		Timezone: "UTC", DurationMinutes: 30, StepMinutes: 30,
		Required: []AvailabilityParticipantReference{{Kind: "internal_user", ID: uuid.New().String()}},
	})
	if !errors.Is(err, ErrSchedulingUnavailable) {
		t.Fatalf("deadline error = %v, want scheduling unavailable", err)
	}
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("availability budget elapsed = %v", elapsed)
	}
}

type recordingSchedulingRepository struct {
	putInput         PutWorkingScheduleInput
	sources          []availabilitySource
	err              error
	loadAvailability func(context.Context) ([]availabilitySource, error)
}

func (repository *recordingSchedulingRepository) GetWorkingSchedule(
	context.Context,
	tenancy.Context,
	time.Time,
) (WorkingSchedule, error) {
	return WorkingSchedule{Timezone: "UTC", Source: "tenant_default"}, repository.err
}

func (repository *recordingSchedulingRepository) PutWorkingSchedule(
	_ context.Context,
	_ tenancy.Context,
	input PutWorkingScheduleInput,
	_ time.Time,
) (WorkingSchedule, error) {
	repository.putInput = input
	return WorkingSchedule{Timezone: input.Timezone, Source: "user_override", Version: input.ExpectedVersion + 1}, repository.err
}

func (repository *recordingSchedulingRepository) LoadAvailability(
	ctx context.Context,
	_ tenancy.Context,
	_ availabilityParams,
) ([]availabilitySource, error) {
	if repository.loadAvailability != nil {
		return repository.loadAvailability(ctx)
	}
	return repository.sources, repository.err
}
