package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	MaximumAvailabilityRangeDays = 31
	MaximumAvailabilityPeople    = 50
	MaximumAvailabilityStarts    = 2_000
	MaximumSuggestedCandidates   = 20
	MaximumWorkingIntervalsDay   = 8
	MaximumWorkingExceptions     = 366
	AvailabilityExecutionBudget  = 250 * time.Millisecond
)

var (
	ErrSchedulingUnavailable = errors.New("calendar scheduling is unavailable")
	ErrAvailabilityCapacity  = errors.New("calendar availability candidate capacity exceeded")
	ErrSchedulingNotFound    = errors.New("calendar scheduling resource not found")
	ErrWorkingScheduleStale  = errors.New("calendar working schedule version conflict")
	ErrAudienceStale         = errors.New("calendar audience version conflict")
	ErrRSVPStale             = errors.New("calendar RSVP version conflict")
	ErrIdempotencyConflict   = errors.New("calendar idempotency key conflict")
)

type CivilTimeInterval struct {
	StartsAt string `json:"starts_at"`
	EndsAt   string `json:"ends_at"`
}

type WeeklyWorkingInterval struct {
	Weekday string `json:"weekday"`
	CivilTimeInterval
}

type WorkingScheduleException struct {
	Date      string              `json:"date"`
	Kind      string              `json:"kind"`
	Intervals []CivilTimeInterval `json:"intervals"`
}

type WorkingSchedule struct {
	Timezone        string                     `json:"timezone"`
	WeeklyIntervals []WeeklyWorkingInterval    `json:"weekly_intervals"`
	Exceptions      []WorkingScheduleException `json:"exceptions"`
	Source          string                     `json:"source"`
	Version         int64                      `json:"version"`
	UpdatedAt       time.Time                  `json:"updated_at"`
}

type PutWorkingScheduleInput struct {
	Timezone        string
	WeeklyIntervals []WeeklyWorkingInterval
	Exceptions      []WorkingScheduleException
	ExpectedVersion int64
}

type AvailabilityParticipantReference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type AvailabilityQueryInput struct {
	ClassID         string
	From            string
	To              string
	Timezone        string
	DurationMinutes int
	StepMinutes     int
	MaxCandidates   int
	Required        []AvailabilityParticipantReference
	Optional        []AvailabilityParticipantReference
}

type AvailabilityStatusInterval struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Status   string    `json:"status"`
}

type AvailabilityWorkingInterval struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type ParticipantAvailability struct {
	Participant      AvailabilityParticipantReference `json:"participant"`
	Role             string                           `json:"role"`
	Intervals        []AvailabilityStatusInterval     `json:"intervals"`
	WorkingIntervals []AvailabilityWorkingInterval    `json:"working_intervals"`
}

type SuggestedTimeReasonBreakdown struct {
	RequiredOutOfOffice    int `json:"required_out_of_office"`
	RequiredBusy           int `json:"required_busy"`
	RequiredUnknown        int `json:"required_unknown"`
	RequiredTentative      int `json:"required_tentative"`
	RequiredOutsideWorking int `json:"required_outside_working"`
	OptionalOutOfOffice    int `json:"optional_out_of_office"`
	OptionalBusy           int `json:"optional_busy"`
	OptionalUnknown        int `json:"optional_unknown"`
	OptionalTentative      int `json:"optional_tentative"`
	OptionalOutsideWorking int `json:"optional_outside_working"`
}

type SuggestedTime struct {
	StartsAt        time.Time                    `json:"starts_at"`
	EndsAt          time.Time                    `json:"ends_at"`
	StableSlotKey   string                       `json:"stable_slot_key"`
	ReasonBreakdown SuggestedTimeReasonBreakdown `json:"reason_breakdown"`
}

type AvailabilityResult struct {
	Timezone               string                    `json:"timezone"`
	Participants           []ParticipantAvailability `json:"participants"`
	Suggestions            []SuggestedTime           `json:"suggestions"`
	EmptySuggestionsReason *string                   `json:"empty_suggestions_reason"`
}

type SchedulingServiceAPI interface {
	GetWorkingSchedule(context.Context, tenancy.Context) (WorkingSchedule, error)
	PutWorkingSchedule(
		context.Context,
		tenancy.Context,
		PutWorkingScheduleInput,
	) (WorkingSchedule, error)
	QueryAvailability(
		context.Context,
		tenancy.Context,
		AvailabilityQueryInput,
	) (AvailabilityResult, error)
}

type schedulingParticipant struct {
	Reference AvailabilityParticipantReference
	ID        uuid.UUID
	Role      string
}

type availabilityParams struct {
	ClassID       uuid.UUID
	From          time.Time
	To            time.Time
	Timezone      string
	Duration      time.Duration
	Step          time.Duration
	MaxCandidates int
	Participants  []schedulingParticipant
}

type availabilitySource struct {
	Participant      schedulingParticipant
	Intervals        []AvailabilityStatusInterval
	WorkingIntervals []AvailabilityWorkingInterval
	Unknown          bool
}
