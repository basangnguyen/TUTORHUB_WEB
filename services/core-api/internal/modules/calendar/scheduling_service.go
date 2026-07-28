package calendar

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var calendarWeekdayOrder = map[string]int{
	"monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4,
	"friday": 5, "saturday": 6, "sunday": 7,
}

type SchedulingService struct {
	repository SchedulingRepository
	clock      func() time.Time
}

func NewSchedulingService(
	repository SchedulingRepository,
	clock func() time.Time,
) (*SchedulingService, error) {
	if repository == nil {
		return nil, fmt.Errorf("calendar scheduling repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &SchedulingService{repository: repository, clock: clock}, nil
}

func (service *SchedulingService) GetWorkingSchedule(
	ctx context.Context,
	scope tenancy.Context,
) (WorkingSchedule, error) {
	if service == nil {
		return WorkingSchedule{}, ErrSchedulingUnavailable
	}
	if err := scope.Validate(); err != nil {
		return WorkingSchedule{}, ErrAccessDenied
	}
	return service.repository.GetWorkingSchedule(ctx, scope, service.clock().UTC())
}

func (service *SchedulingService) PutWorkingSchedule(
	ctx context.Context,
	scope tenancy.Context,
	input PutWorkingScheduleInput,
) (WorkingSchedule, error) {
	if service == nil {
		return WorkingSchedule{}, ErrSchedulingUnavailable
	}
	if err := scope.Validate(); err != nil {
		return WorkingSchedule{}, ErrAccessDenied
	}
	normalized, err := normalizeWorkingScheduleInput(input, service.clock().UTC())
	if err != nil {
		return WorkingSchedule{}, err
	}
	return service.repository.PutWorkingSchedule(
		ctx,
		scope,
		normalized,
		service.clock().UTC(),
	)
}

func (service *SchedulingService) QueryAvailability(
	ctx context.Context,
	scope tenancy.Context,
	input AvailabilityQueryInput,
) (AvailabilityResult, error) {
	if service == nil {
		return AvailabilityResult{}, ErrSchedulingUnavailable
	}
	if err := scope.Validate(); err != nil {
		return AvailabilityResult{}, ErrAccessDenied
	}
	queryContext, cancel := context.WithTimeout(ctx, AvailabilityExecutionBudget)
	defer cancel()
	params, err := normalizeAvailabilityInput(input)
	if err != nil {
		return AvailabilityResult{}, err
	}
	sources, err := service.repository.LoadAvailability(queryContext, scope, params)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) ||
			errors.Is(queryContext.Err(), context.DeadlineExceeded) {
			return AvailabilityResult{}, ErrSchedulingUnavailable
		}
		return AvailabilityResult{}, err
	}
	result, err := buildAvailabilityResult(queryContext, params, sources)
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(queryContext.Err(), context.DeadlineExceeded) {
		return AvailabilityResult{}, ErrSchedulingUnavailable
	}
	return result, err
}

func normalizeWorkingScheduleInput(
	input PutWorkingScheduleInput,
	now time.Time,
) (PutWorkingScheduleInput, error) {
	if input.ExpectedVersion < 0 {
		return PutWorkingScheduleInput{}, ErrInvalidInput
	}
	timezone, err := normalizeTimezone(input.Timezone)
	if err != nil {
		return PutWorkingScheduleInput{}, err
	}
	input.Timezone = timezone
	if len(input.WeeklyIntervals) > 7*MaximumWorkingIntervalsDay ||
		len(input.Exceptions) > MaximumWorkingExceptions {
		return PutWorkingScheduleInput{}, ErrInvalidInput
	}
	weeklyByDay := make(map[string][]WeeklyWorkingInterval, 7)
	for _, interval := range input.WeeklyIntervals {
		interval.Weekday = strings.ToLower(strings.TrimSpace(interval.Weekday))
		if _, ok := calendarWeekdayOrder[interval.Weekday]; !ok {
			return PutWorkingScheduleInput{}, ErrInvalidInput
		}
		start, end, err := normalizeCivilInterval(interval.CivilTimeInterval)
		if err != nil {
			return PutWorkingScheduleInput{}, err
		}
		interval.StartsAt, interval.EndsAt = start, end
		weeklyByDay[interval.Weekday] = append(weeklyByDay[interval.Weekday], interval)
	}
	input.WeeklyIntervals = input.WeeklyIntervals[:0]
	for weekday, intervals := range weeklyByDay {
		if len(intervals) > MaximumWorkingIntervalsDay || hasCivilOverlap(intervals) {
			return PutWorkingScheduleInput{}, ErrInvalidInput
		}
		input.WeeklyIntervals = append(input.WeeklyIntervals, intervals...)
		_ = weekday
	}
	sort.Slice(input.WeeklyIntervals, func(left, right int) bool {
		leftDay := calendarWeekdayOrder[input.WeeklyIntervals[left].Weekday]
		rightDay := calendarWeekdayOrder[input.WeeklyIntervals[right].Weekday]
		if leftDay != rightDay {
			return leftDay < rightDay
		}
		return input.WeeklyIntervals[left].StartsAt < input.WeeklyIntervals[right].StartsAt
	})

	today := now.UTC().Format("2006-01-02")
	minimumDate, _ := time.Parse("2006-01-02", today)
	minimumDate = minimumDate.AddDate(0, 0, -31)
	maximumDate := minimumDate.AddDate(0, 0, 761)
	seenDates := make(map[string]struct{}, len(input.Exceptions))
	for index := range input.Exceptions {
		exception := &input.Exceptions[index]
		exception.Kind = strings.ToLower(strings.TrimSpace(exception.Kind))
		date, err := time.Parse("2006-01-02", strings.TrimSpace(exception.Date))
		if err != nil || date.Before(minimumDate) || date.After(maximumDate) {
			return PutWorkingScheduleInput{}, ErrInvalidInput
		}
		exception.Date = date.Format("2006-01-02")
		if _, duplicate := seenDates[exception.Date]; duplicate {
			return PutWorkingScheduleInput{}, ErrInvalidInput
		}
		seenDates[exception.Date] = struct{}{}
		switch exception.Kind {
		case "holiday", "out_of_office":
			if len(exception.Intervals) != 0 {
				return PutWorkingScheduleInput{}, ErrInvalidInput
			}
			exception.Intervals = []CivilTimeInterval{}
		case "special_hours":
			if len(exception.Intervals) == 0 || len(exception.Intervals) > MaximumWorkingIntervalsDay {
				return PutWorkingScheduleInput{}, ErrInvalidInput
			}
			for intervalIndex := range exception.Intervals {
				start, end, err := normalizeCivilInterval(exception.Intervals[intervalIndex])
				if err != nil {
					return PutWorkingScheduleInput{}, err
				}
				exception.Intervals[intervalIndex].StartsAt = start
				exception.Intervals[intervalIndex].EndsAt = end
			}
			if hasSimpleCivilOverlap(exception.Intervals) {
				return PutWorkingScheduleInput{}, ErrInvalidInput
			}
		default:
			return PutWorkingScheduleInput{}, ErrInvalidInput
		}
	}
	sort.Slice(input.Exceptions, func(left, right int) bool {
		return input.Exceptions[left].Date < input.Exceptions[right].Date
	})
	return input, nil
}

func normalizeAvailabilityInput(input AvailabilityQueryInput) (availabilityParams, error) {
	classID, err := uuid.Parse(strings.TrimSpace(input.ClassID))
	if err != nil || classID == uuid.Nil {
		return availabilityParams{}, ErrInvalidInput
	}
	from, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.From))
	if err != nil {
		return availabilityParams{}, ErrInvalidRange
	}
	to, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.To))
	if err != nil {
		return availabilityParams{}, ErrInvalidRange
	}
	timezone, err := normalizeTimezone(input.Timezone)
	if err != nil {
		return availabilityParams{}, err
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return availabilityParams{}, ErrInvalidInput
	}
	from, to = from.UTC(), to.UTC()
	if !to.After(from) || to.After(from.In(location).AddDate(0, 0, MaximumAvailabilityRangeDays)) {
		return availabilityParams{}, ErrInvalidRange
	}
	if input.DurationMinutes < 15 || input.DurationMinutes > 480 ||
		(input.StepMinutes != 15 && input.StepMinutes != 30 && input.StepMinutes != 60) {
		return availabilityParams{}, ErrInvalidInput
	}
	if input.MaxCandidates == 0 {
		input.MaxCandidates = 10
	}
	if input.MaxCandidates < 1 || input.MaxCandidates > MaximumSuggestedCandidates {
		return availabilityParams{}, ErrInvalidInput
	}
	if len(input.Required)+len(input.Optional) == 0 ||
		len(input.Required)+len(input.Optional) > MaximumAvailabilityPeople {
		return availabilityParams{}, ErrInvalidInput
	}
	participants := make([]schedulingParticipant, 0, len(input.Required)+len(input.Optional))
	seen := make(map[string]struct{}, cap(participants))
	appendParticipants := func(values []AvailabilityParticipantReference, role string) error {
		for _, value := range values {
			value.Kind = strings.ToLower(strings.TrimSpace(value.Kind))
			if value.Kind != "internal_user" && value.Kind != "external_guest" {
				return ErrInvalidInput
			}
			participantID, err := uuid.Parse(strings.TrimSpace(value.ID))
			if err != nil || participantID == uuid.Nil {
				return ErrInvalidInput
			}
			value.ID = participantID.String()
			key := value.Kind + ":" + value.ID
			if _, duplicate := seen[key]; duplicate {
				return ErrInvalidInput
			}
			seen[key] = struct{}{}
			participants = append(participants, schedulingParticipant{
				Reference: value, ID: participantID, Role: role,
			})
		}
		return nil
	}
	if err := appendParticipants(input.Required, "required"); err != nil {
		return availabilityParams{}, err
	}
	if err := appendParticipants(input.Optional, "optional"); err != nil {
		return availabilityParams{}, err
	}
	return availabilityParams{
		ClassID: classID, From: from, To: to, Timezone: timezone,
		Duration:      time.Duration(input.DurationMinutes) * time.Minute,
		Step:          time.Duration(input.StepMinutes) * time.Minute,
		MaxCandidates: input.MaxCandidates, Participants: participants,
	}, nil
}

func normalizeCivilInterval(interval CivilTimeInterval) (string, string, error) {
	start, startMinute, err := parseCivilMinute(interval.StartsAt)
	if err != nil {
		return "", "", err
	}
	end, endMinute, err := parseCivilMinute(interval.EndsAt)
	if err != nil || endMinute <= startMinute {
		return "", "", ErrInvalidInput
	}
	return start, end, nil
}

func parseCivilMinute(value string) (string, int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return "", 0, ErrInvalidInput
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return "", 0, ErrInvalidInput
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return "", 0, ErrInvalidInput
	}
	return fmt.Sprintf("%02d:%02d", hour, minute), hour*60 + minute, nil
}

func hasCivilOverlap(intervals []WeeklyWorkingInterval) bool {
	values := make([]CivilTimeInterval, len(intervals))
	for index := range intervals {
		values[index] = intervals[index].CivilTimeInterval
	}
	return hasSimpleCivilOverlap(values)
}

func hasSimpleCivilOverlap(intervals []CivilTimeInterval) bool {
	ordered := append([]CivilTimeInterval(nil), intervals...)
	sort.Slice(ordered, func(left, right int) bool {
		return ordered[left].StartsAt < ordered[right].StartsAt
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index].StartsAt < ordered[index-1].EndsAt {
			return true
		}
	}
	return false
}
