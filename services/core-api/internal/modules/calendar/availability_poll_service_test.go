package calendar

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNormalizeAvailabilityPollInputAcceptsBoundedDSTTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		rangeDate  string
		deadlineAt time.Time
		startsAt   time.Time
		endsAt     time.Time
		workingEnd string
	}{
		{
			name:       "spring-forward skips the missing civil hour",
			rangeDate:  "2026-03-08",
			deadlineAt: time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC),
			startsAt:   time.Date(2026, time.March, 8, 6, 30, 0, 0, time.UTC),
			endsAt:     time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC),
			workingEnd: "04:00",
		},
		{
			name:       "fall-back keeps a real-hour interval across the repeated civil time",
			rangeDate:  "2026-11-01",
			deadlineAt: time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC),
			startsAt:   time.Date(2026, time.November, 1, 5, 30, 0, 0, time.UTC),
			endsAt:     time.Date(2026, time.November, 1, 6, 30, 0, 0, time.UTC),
			workingEnd: "03:00",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := validAvailabilityPollInput()
			input.Timezone = "America/New_York"
			input.RangeStart = test.rangeDate
			input.RangeEnd = test.rangeDate
			input.WorkingDayStart = "01:00"
			input.WorkingDayEnd = test.workingEnd
			input.DeadlineAt = test.deadlineAt
			input.DurationMinutes = 60
			input.SlotGranularityMinutes = 30
			input.Slots = []AvailabilityPollSlotInput{{
				StartsAt: test.startsAt,
				EndsAt:   test.endsAt,
			}}

			normalized, err := normalizeAvailabilityPollInput(
				input,
				time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
				true,
			)
			if err != nil {
				t.Fatalf("normalize DST poll: %v", err)
			}
			if len(normalized.Slots) != 1 ||
				!normalized.Slots[0].StartsAt.Equal(test.startsAt) ||
				!normalized.Slots[0].EndsAt.Equal(test.endsAt) {
				t.Fatalf("normalized DST slot = %+v", normalized.Slots)
			}
		})
	}
}

func TestNormalizeAvailabilityPollInputRejectsCivilAndStructuralAmbiguity(t *testing.T) {
	t.Parallel()

	duplicateUser := uuid.New()
	tests := []struct {
		name   string
		mutate func(*CreateAvailabilityPollInput)
	}{
		{
			name: "slot outside declared civil range",
			mutate: func(input *CreateAvailabilityPollInput) {
				input.RangeStart = "2026-08-03"
				input.RangeEnd = "2026-08-03"
				input.Slots[0].StartsAt = time.Date(2026, time.August, 4, 8, 0, 0, 0, time.UTC)
				input.Slots[0].EndsAt = time.Date(2026, time.August, 4, 9, 0, 0, 0, time.UTC)
			},
		},
		{
			name: "duplicate slot",
			mutate: func(input *CreateAvailabilityPollInput) {
				input.Slots = append(input.Slots, input.Slots[0])
			},
		},
		{
			name: "class mode without a class",
			mutate: func(input *CreateAvailabilityPollInput) {
				input.ShareMode = PollShareClassMembers
			},
		},
		{
			name: "duplicate internal participant",
			mutate: func(input *CreateAvailabilityPollInput) {
				input.Participants = []AvailabilityPollParticipantInput{
					{Kind: PollParticipantInternal, InternalUserID: &duplicateUser},
					{Kind: PollParticipantInternal, InternalUserID: &duplicateUser},
				}
			},
		},
		{
			name: "invalid idempotency key",
			mutate: func(input *CreateAvailabilityPollInput) {
				input.IdempotencyKey = "short"
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := validAvailabilityPollInput()
			test.mutate(&input)
			_, err := normalizeAvailabilityPollInput(
				input,
				time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC),
				true,
			)
			if !errors.Is(err, ErrAvailabilityPollInvalid) {
				t.Fatalf("error = %v, want invalid availability poll", err)
			}
		})
	}
}

func TestNormalizePollAnswersLeavesOmittedSlotsUnknown(t *testing.T) {
	t.Parallel()

	preferredSlot := uuid.New()
	availableSlot := uuid.New()
	answers, err := normalizePollAnswers([]AvailabilityPollAnswerInput{
		{SlotID: availableSlot, State: PollAnswerAvailable},
		{SlotID: preferredSlot, State: PollAnswerPreferred},
	})
	if err != nil {
		t.Fatalf("normalize answers: %v", err)
	}
	if len(answers) != 2 {
		t.Fatalf("answer count = %d, want only explicitly answered slots", len(answers))
	}
	if answers[0].SlotID.String() > answers[1].SlotID.String() {
		t.Fatalf("answers are not deterministically ordered: %+v", answers)
	}

	_, err = normalizePollAnswers([]AvailabilityPollAnswerInput{
		{SlotID: preferredSlot, State: PollAnswerPreferred},
		{SlotID: preferredSlot, State: PollAnswerUnavailable},
	})
	if !errors.Is(err, ErrAvailabilityPollInvalid) {
		t.Fatalf("duplicate answer error = %v, want invalid availability poll", err)
	}
}

func validAvailabilityPollInput() CreateAvailabilityPollInput {
	return CreateAvailabilityPollInput{
		Title:                  "Study group",
		Description:            "Choose one or more times.",
		Timezone:               "UTC",
		RangeStart:             "2026-08-03",
		RangeEnd:               "2026-08-03",
		WorkingDayStart:        "08:00",
		WorkingDayEnd:          "18:00",
		DurationMinutes:        60,
		SlotGranularityMinutes: 30,
		DeadlineAt:             time.Date(2026, time.August, 2, 0, 0, 0, 0, time.UTC),
		ShareMode:              PollShareInvitedOnly,
		Slots: []AvailabilityPollSlotInput{{
			StartsAt: time.Date(2026, time.August, 3, 8, 0, 0, 0, time.UTC),
			EndsAt:   time.Date(2026, time.August, 3, 9, 0, 0, 0, time.UTC),
		}},
		Participants:   []AvailabilityPollParticipantInput{},
		IdempotencyKey: "poll-create-test",
	}
}
