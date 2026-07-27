package classroom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
)

const (
	maximumSeriesPerClass = 128
	civilDateTimeLayout   = "2006-01-02T15:04:05"
)

type SeriesStatus string

const (
	SeriesStatusScheduled SeriesStatus = "scheduled"
	SeriesStatusCancelled SeriesStatus = "cancelled"
)

type ClassSessionSeries struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ClassID         uuid.UUID
	Title           string
	Description     string
	LocalStart      string
	Timezone        string
	DurationMinutes int
	Rule            recurrence.Rule
	NormalizedRule  string
	OverlapPolicy   recurrence.OverlapPolicy
	Status          SeriesStatus
	Version         int64
	Sequence        int64
	ICalUID         string
	SplitFrom       *uuid.UUID
	CreatedBy       uuid.UUID
	UpdatedBy       uuid.UUID
	CancelledAt     *time.Time
	CancelledBy     *uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ViewerAccess    SessionViewerAccess
}

type SeriesOccurrence struct {
	SeriesID      uuid.UUID
	OccurrenceKey string
	OriginalLocal string
	StartsAt      time.Time
	EndsAt        time.Time
}

type CreateSeriesInput struct {
	Title         string
	Description   string
	StartsAt      string
	EndsAt        string
	Timezone      string
	Rule          recurrence.Rule
	OverlapPolicy recurrence.OverlapPolicy
}

type CreateSeriesParams struct {
	ID              uuid.UUID
	Title           string
	Description     string
	LocalStart      string
	Timezone        string
	DurationMinutes int
	Rule            recurrence.Rule
	NormalizedRule  string
	OverlapPolicy   recurrence.OverlapPolicy
	CreatedBy       uuid.UUID
}

type OccurrenceMutationInput struct {
	Scope                    recurrence.EditScope
	OccurrenceKey            string
	ExpectedVersion          int64
	IdempotencyKey           string
	FollowingExceptionPolicy recurrence.FutureExceptionPolicy
	Title                    *string
	Description              *string
	StartsAt                 *string
	EndsAt                   *string
	Timezone                 *string
	Rule                     *recurrence.Rule
	OverlapPolicy            *recurrence.OverlapPolicy
	OverrideScheduleConflict bool
	ScheduleConflictReason   string
}

type SeriesMutationParams struct {
	OccurrenceMutationInput
	Fingerprint string
	ActorID     uuid.UUID
}

type SeriesScopePreview struct {
	recurrence.ScopePreview
	Conflicts []ScheduleConflict
}

type ScheduleConflict struct {
	ClassID       uuid.UUID
	SeriesID      *uuid.UUID
	OccurrenceKey string
	StartsAt      time.Time
	EndsAt        time.Time
}

type SeriesMutationResult struct {
	Series ClassSessionSeries
	Replay bool
}

func normalizeCreateSeriesInput(
	ctx context.Context,
	input CreateSeriesInput,
	actorID uuid.UUID,
) (CreateSeriesParams, []recurrence.Occurrence, error) {
	title := strings.TrimSpace(input.Title)
	description := strings.TrimSpace(input.Description)
	timezone := strings.TrimSpace(input.Timezone)
	if actorID == uuid.Nil {
		return CreateSeriesParams{}, nil, ErrSessionAccessDenied
	}
	if err := validateSessionText(title, description); err != nil {
		return CreateSeriesParams{}, nil, err
	}
	startsAt, err := parseSessionTimestamp(input.StartsAt, timezone)
	if err != nil {
		return CreateSeriesParams{}, nil, err
	}
	endsAt, err := parseSessionTimestamp(input.EndsAt, timezone)
	if err != nil {
		return CreateSeriesParams{}, nil, err
	}
	if err := validateSessionTimeRange(startsAt, endsAt); err != nil {
		return CreateSeriesParams{}, nil, err
	}
	duration := endsAt.Sub(startsAt)
	if duration%time.Minute != 0 {
		return CreateSeriesParams{}, nil, fmt.Errorf(
			"%w: recurring session duration must use whole minutes",
			ErrInvalidSessionInput,
		)
	}
	if input.OverlapPolicy == "" {
		input.OverlapPolicy = recurrence.OverlapReject
	}
	id := uuid.New()
	localStart := startsAt.In(mustLocation(timezone)).Format(civilDateTimeLayout)
	normalizedRule, err := recurrence.NormalizeRule(input.Rule)
	if err != nil {
		return CreateSeriesParams{}, nil, mapRecurrenceError(err)
	}
	params := CreateSeriesParams{
		ID: id, Title: title, Description: description, LocalStart: localStart,
		Timezone: timezone, DurationMinutes: int(duration / time.Minute),
		Rule: input.Rule, NormalizedRule: normalizedRule,
		OverlapPolicy: input.OverlapPolicy, CreatedBy: actorID,
	}
	occurrences, err := expandCompleteSeries(ctx, seriesDefinition(params))
	if err != nil {
		return CreateSeriesParams{}, nil, err
	}
	if err := requireNoSelfOverlap(occurrences); err != nil {
		return CreateSeriesParams{}, nil, err
	}
	return params, occurrences, nil
}

func normalizeSeriesMutationInput(
	input OccurrenceMutationInput,
	actorID uuid.UUID,
	operation string,
) (SeriesMutationParams, error) {
	input.OccurrenceKey = strings.TrimSpace(input.OccurrenceKey)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.ScheduleConflictReason = strings.TrimSpace(input.ScheduleConflictReason)
	if actorID == uuid.Nil || input.ExpectedVersion < 1 ||
		len(input.OccurrenceKey) < 8 || len(input.OccurrenceKey) > 128 ||
		len(input.IdempotencyKey) < 16 || len(input.IdempotencyKey) > 128 {
		return SeriesMutationParams{}, ErrInvalidSessionInput
	}
	if input.Scope != recurrence.ScopeThisOccurrence &&
		input.Scope != recurrence.ScopeFollowing &&
		input.Scope != recurrence.ScopeEntireSeries {
		return SeriesMutationParams{}, ErrInvalidSessionInput
	}
	if input.Scope == recurrence.ScopeFollowing {
		if input.FollowingExceptionPolicy != recurrence.ExceptionCarry &&
			input.FollowingExceptionPolicy != recurrence.ExceptionRebase &&
			input.FollowingExceptionPolicy != recurrence.ExceptionDiscard {
			return SeriesMutationParams{}, ErrInvalidSessionInput
		}
	} else if input.FollowingExceptionPolicy != "" {
		return SeriesMutationParams{}, ErrInvalidSessionInput
	}
	if input.OverrideScheduleConflict {
		if utf8.RuneCountInString(input.ScheduleConflictReason) < 3 ||
			utf8.RuneCountInString(input.ScheduleConflictReason) > 500 {
			return SeriesMutationParams{}, fmt.Errorf(
				"%w: conflict override requires a reason between 3 and 500 characters",
				ErrInvalidSessionInput,
			)
		}
	} else if input.ScheduleConflictReason != "" {
		return SeriesMutationParams{}, ErrInvalidSessionInput
	}
	for _, value := range []*string{input.Title, input.Description, input.Timezone} {
		if value != nil {
			trimmed := strings.TrimSpace(*value)
			*value = trimmed
		}
	}
	if input.Title != nil {
		if err := validateSessionText(*input.Title, ""); err != nil {
			return SeriesMutationParams{}, err
		}
	}
	if input.Description != nil && utf8.RuneCountInString(*input.Description) > 4000 {
		return SeriesMutationParams{}, ErrInvalidSessionInput
	}
	hasTime := input.StartsAt != nil || input.EndsAt != nil || input.Timezone != nil
	if hasTime && (input.StartsAt == nil || input.EndsAt == nil || input.Timezone == nil) {
		return SeriesMutationParams{}, fmt.Errorf(
			"%w: starts_at, ends_at, and timezone must change together",
			ErrInvalidSessionInput,
		)
	}
	if operation == "update" &&
		input.Title == nil && input.Description == nil && !hasTime &&
		input.Rule == nil && input.OverlapPolicy == nil {
		return SeriesMutationParams{}, ErrInvalidSessionInput
	}
	fingerprintInput := struct {
		Operation string
		Input     OccurrenceMutationInput
	}{Operation: operation, Input: input}
	encoded, err := json.Marshal(fingerprintInput)
	if err != nil {
		return SeriesMutationParams{}, fmt.Errorf("fingerprint recurring mutation: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return SeriesMutationParams{
		OccurrenceMutationInput: input,
		Fingerprint:             hex.EncodeToString(sum[:]),
		ActorID:                 actorID,
	}, nil
}

func seriesDefinition(params CreateSeriesParams) recurrence.Definition {
	return recurrence.Definition{
		ID: params.ID.String(), StartLocal: params.LocalStart,
		TimeZone: params.Timezone,
		Duration: time.Duration(params.DurationMinutes) * time.Minute,
		Rule:     params.Rule, OverlapPolicy: params.OverlapPolicy,
	}
}

func definitionFromSeries(series ClassSessionSeries) recurrence.Definition {
	return recurrence.Definition{
		ID: series.ID.String(), StartLocal: series.LocalStart,
		TimeZone: series.Timezone,
		Duration: time.Duration(series.DurationMinutes) * time.Minute,
		Rule:     series.Rule, OverlapPolicy: series.OverlapPolicy,
	}
}

func expandCompleteSeries(
	ctx context.Context,
	definition recurrence.Definition,
) ([]recurrence.Occurrence, error) {
	location := mustLocation(definition.TimeZone)
	local, err := time.ParseInLocation(civilDateTimeLayout, definition.StartLocal, location)
	if err != nil {
		return nil, ErrInvalidSessionInput
	}
	occurrences, err := recurrence.Expand(ctx, definition, recurrence.Window{
		Start: local.UTC().Add(-48 * time.Hour),
		End:   local.UTC().Add((recurrence.MaxSeriesHorizonDays + 2) * 24 * time.Hour),
	}, recurrence.MaxOccurrences)
	if err != nil {
		return nil, mapRecurrenceError(err)
	}
	if len(occurrences) == 0 {
		return nil, ErrInvalidSessionInput
	}
	return occurrences, nil
}

func requireNoSelfOverlap(occurrences []recurrence.Occurrence) error {
	for index := 1; index < len(occurrences); index++ {
		if occurrences[index].StartsAt.Before(occurrences[index-1].EndsAt) {
			return ErrSessionScheduleConflict
		}
	}
	return nil
}

func mapRecurrenceError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrInvalidSessionInput, err)
}

func mustLocation(value string) *time.Location {
	location, _ := time.LoadLocation(value)
	return location
}
