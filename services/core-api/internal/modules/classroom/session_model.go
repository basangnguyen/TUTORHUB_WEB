package classroom

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maximumClassSessionDuration = 24 * time.Hour
	maximumSessionQueryRange    = 93 * 24 * time.Hour
	defaultSessionListLimit     = 50
	maximumSessionListLimit     = 100
	maximumSessionCursorLength  = 512
	classSessionCursorPrefix    = "thcs1_"
)

type SessionStatus string

const (
	SessionStatusScheduled SessionStatus = "scheduled"
	SessionStatusCancelled SessionStatus = "cancelled"
	SessionStatusLive      SessionStatus = "live"
	SessionStatusEnded     SessionStatus = "ended"
)

type SessionViewerAccess struct {
	CanUpdate bool
	CanCancel bool
}

type ClassSession struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	ClassID      uuid.UUID
	Title        string
	Description  string
	StartsAt     time.Time
	EndsAt       time.Time
	Timezone     string
	Status       SessionStatus
	Version      int64
	CreatedBy    uuid.UUID
	UpdatedBy    uuid.UUID
	CancelledAt  *time.Time
	CancelledBy  *uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
	ViewerAccess SessionViewerAccess
}

type CreateSessionParams struct {
	Title       string
	Description string
	StartsAt    time.Time
	EndsAt      time.Time
	Timezone    string
	CreatedBy   uuid.UUID
}

func (params CreateSessionParams) normalized() (CreateSessionParams, error) {
	params.Title = strings.TrimSpace(params.Title)
	params.Description = strings.TrimSpace(params.Description)
	params.Timezone = strings.TrimSpace(params.Timezone)
	params.StartsAt = params.StartsAt.UTC()
	params.EndsAt = params.EndsAt.UTC()

	if params.CreatedBy == uuid.Nil {
		return CreateSessionParams{}, fmt.Errorf(
			"%w: session creator is required",
			ErrInvalidSessionInput,
		)
	}
	if err := validateSessionText(params.Title, params.Description); err != nil {
		return CreateSessionParams{}, err
	}
	if err := validateSessionTimezone(params.Timezone); err != nil {
		return CreateSessionParams{}, err
	}
	if err := validateSessionTimeRange(params.StartsAt, params.EndsAt); err != nil {
		return CreateSessionParams{}, err
	}
	return params, nil
}

type UpdateSessionParams struct {
	Title           *string
	Description     *string
	StartsAt        *time.Time
	EndsAt          *time.Time
	Timezone        *string
	ExpectedVersion int64
}

func (params UpdateSessionParams) normalized() (UpdateSessionParams, error) {
	hasTimeChange := params.StartsAt != nil || params.EndsAt != nil || params.Timezone != nil
	hasCompleteTimeChange := params.StartsAt != nil && params.EndsAt != nil && params.Timezone != nil
	if params.ExpectedVersion < 1 ||
		(params.Title == nil && params.Description == nil && !hasTimeChange) ||
		(hasTimeChange && !hasCompleteTimeChange) {
		return UpdateSessionParams{}, fmt.Errorf(
			"%w: expected version, a mutable field, and a complete time triplet are required",
			ErrInvalidSessionInput,
		)
	}
	if params.Title != nil {
		title := strings.TrimSpace(*params.Title)
		if err := validateSessionText(title, ""); err != nil {
			return UpdateSessionParams{}, err
		}
		params.Title = &title
	}
	if params.Description != nil {
		description := strings.TrimSpace(*params.Description)
		if utf8.RuneCountInString(description) > 4000 {
			return UpdateSessionParams{}, fmt.Errorf(
				"%w: session description cannot exceed 4000 characters",
				ErrInvalidSessionInput,
			)
		}
		params.Description = &description
	}
	if hasCompleteTimeChange {
		timezone := strings.TrimSpace(*params.Timezone)
		if err := validateSessionTimezone(timezone); err != nil {
			return UpdateSessionParams{}, err
		}
		startsAt := params.StartsAt.UTC()
		endsAt := params.EndsAt.UTC()
		if err := validateSessionTimeRange(startsAt, endsAt); err != nil {
			return UpdateSessionParams{}, err
		}
		params.StartsAt = &startsAt
		params.EndsAt = &endsAt
		params.Timezone = &timezone
	}
	return params, nil
}

type CancelSessionParams struct {
	ExpectedVersion int64
}

func (params CancelSessionParams) normalized() (CancelSessionParams, error) {
	if params.ExpectedVersion < 1 {
		return CancelSessionParams{}, fmt.Errorf(
			"%w: expected version is required",
			ErrInvalidSessionInput,
		)
	}
	return params, nil
}

type SessionCursor struct {
	StartsAt time.Time
	ID       uuid.UUID
}

type ListSessionsParams struct {
	From  time.Time
	To    time.Time
	Limit int
	After *SessionCursor
}

type ListSessionsResult struct {
	Items   []ClassSession
	HasMore bool
}

func validateSessionText(title string, description string) error {
	titleLength := utf8.RuneCountInString(title)
	if titleLength < 1 || titleLength > 200 {
		return fmt.Errorf(
			"%w: session title must contain between 1 and 200 characters",
			ErrInvalidSessionInput,
		)
	}
	if utf8.RuneCountInString(description) > 4000 {
		return fmt.Errorf(
			"%w: session description cannot exceed 4000 characters",
			ErrInvalidSessionInput,
		)
	}
	return nil
}

func validateSessionTimezone(value string) error {
	if value == "" || len(value) > 100 || strings.EqualFold(value, "local") {
		return fmt.Errorf("%w: invalid session timezone", ErrInvalidSessionTimezone)
	}
	if _, err := time.LoadLocation(value); err != nil {
		return fmt.Errorf("%w: invalid session timezone", ErrInvalidSessionTimezone)
	}
	return nil
}

func validateSessionTimeRange(startsAt time.Time, endsAt time.Time) error {
	if startsAt.IsZero() || endsAt.IsZero() || !endsAt.After(startsAt) ||
		endsAt.Sub(startsAt) > maximumClassSessionDuration {
		return fmt.Errorf(
			"%w: session must end after it starts and cannot exceed 24 hours",
			ErrInvalidSessionRange,
		)
	}
	return nil
}

// parseSessionTimestamp verifies that the supplied RFC3339 offset represents
// the same local wall time in the supplied IANA zone. This rejects DST gaps and
// accidental browser/server offset drift while accepting either valid offset
// during a fall-back overlap.
func parseSessionTimestamp(value string, timezone string) (time.Time, error) {
	value = strings.TrimSpace(value)
	timezone = strings.TrimSpace(timezone)
	if err := validateSessionTimezone(timezone); err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"%w: timestamp must be RFC3339 with an explicit offset",
			ErrInvalidSessionInput,
		)
	}
	location, _ := time.LoadLocation(timezone)
	localized := parsed.In(location)
	_, parsedOffset := parsed.Zone()
	_, localizedOffset := localized.Zone()
	if sameWallTime(parsed, localized) && parsedOffset == localizedOffset {
		return parsed.UTC(), nil
	}

	wall := time.Date(
		parsed.Year(),
		parsed.Month(),
		parsed.Day(),
		parsed.Hour(),
		parsed.Minute(),
		parsed.Second(),
		parsed.Nanosecond(),
		location,
	)
	if !sameWallTime(parsed, wall) {
		return time.Time{}, fmt.Errorf(
			"%w: local time does not exist in the supplied timezone",
			ErrSessionDSTGap,
		)
	}
	return time.Time{}, fmt.Errorf(
		"%w: timestamp offset does not match the supplied timezone",
		ErrSessionTimezoneOffsetMismatch,
	)
}

func sameWallTime(left time.Time, right time.Time) bool {
	return left.Year() == right.Year() &&
		left.Month() == right.Month() &&
		left.Day() == right.Day() &&
		left.Hour() == right.Hour() &&
		left.Minute() == right.Minute() &&
		left.Second() == right.Second() &&
		left.Nanosecond() == right.Nanosecond()
}
