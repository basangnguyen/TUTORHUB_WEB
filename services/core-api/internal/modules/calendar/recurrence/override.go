package recurrence

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const MaxOccurrencesPerRequest = 2000

// SeriesProjection contains only the data required to turn a persisted series
// into calendar read occurrences. Authorization and viewer capabilities remain
// outside this pure projection boundary.
type SeriesProjection struct {
	ID              uuid.UUID
	ClassID         uuid.UUID
	ClassTitle      string
	Title           string
	Definition      Definition
	DisplayTimezone string
}

type ExceptionProjection struct {
	OccurrenceKey       string
	Type                ExceptionType
	OverrideLocalStart  *string
	OverrideTimezone    *string
	OverrideDuration    *time.Duration
	OverrideTitle       *string
	OverrideDescription *string
}

type ProjectedOccurrence struct {
	SeriesID        uuid.UUID
	ClassID         uuid.UUID
	ClassTitle      string
	Title           string
	OccurrenceKey   string
	OriginalLocal   string
	StartsAt        time.Time
	EndsAt          time.Time
	DisplayTimezone string
}

// Project expands a bounded series and applies exceptions without changing
// the original occurrence key. It is deterministic and side-effect free.
func Project(
	ctx context.Context,
	series SeriesProjection,
	window Window,
	exceptions []ExceptionProjection,
	maxOccurrences int,
) ([]ProjectedOccurrence, error) {
	if series.ID == uuid.Nil || series.ClassID == uuid.Nil {
		return nil, fmt.Errorf("%w: series and class IDs are required", ErrInvalidSeries)
	}
	if maxOccurrences < 1 || maxOccurrences > MaxOccurrencesPerRequest {
		return nil, fmt.Errorf("%w: request occurrence cap is invalid", ErrInvalidRule)
	}
	occurrences, err := Expand(ctx, series.Definition, window, MaxOccurrences)
	if err != nil {
		return nil, err
	}
	exceptionByKey := make(map[string]ExceptionProjection, len(exceptions))
	for _, exception := range exceptions {
		key := strings.TrimSpace(exception.OccurrenceKey)
		if key == "" {
			return nil, fmt.Errorf("%w: exception occurrence key is required", ErrInvalidRule)
		}
		if _, duplicate := exceptionByKey[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate exception %q", ErrInvalidRule, key)
		}
		if exception.Type != ExceptionCancel && exception.Type != ExceptionOverride {
			return nil, fmt.Errorf("%w: unsupported exception type %q", ErrInvalidRule, exception.Type)
		}
		exception.OccurrenceKey = key
		exceptionByKey[key] = exception
	}
	projected := make([]ProjectedOccurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		exception, hasException := exceptionByKey[occurrence.Key]
		if hasException && exception.Type == ExceptionCancel {
			continue
		}
		startsAt, endsAt := occurrence.StartsAt, occurrence.EndsAt
		title, displayTimezone := series.Title, series.DisplayTimezone
		if hasException && exception.Type == ExceptionOverride {
			override := series.Definition
			localStart := occurrence.OriginalLocal
			if exception.OverrideLocalStart != nil {
				localStart = strings.TrimSpace(*exception.OverrideLocalStart)
			}
			if exception.OverrideTimezone != nil {
				displayTimezone = strings.TrimSpace(*exception.OverrideTimezone)
				override.TimeZone = displayTimezone
			}
			override.Duration = endsAt.Sub(startsAt)
			if exception.OverrideDuration != nil {
				override.Duration = *exception.OverrideDuration
			}
			resolved, resolveErr := ResolveOccurrence(ctx, override, localStart)
			if resolveErr != nil {
				return nil, fmt.Errorf("resolve exception %q: %w", occurrence.Key, resolveErr)
			}
			startsAt, endsAt = resolved.StartsAt, resolved.EndsAt
			if exception.OverrideTitle != nil {
				title = strings.TrimSpace(*exception.OverrideTitle)
			}
		}
		projected = append(projected, ProjectedOccurrence{
			SeriesID:        series.ID,
			ClassID:         series.ClassID,
			ClassTitle:      series.ClassTitle,
			Title:           title,
			OccurrenceKey:   occurrence.Key,
			OriginalLocal:   occurrence.OriginalLocal,
			StartsAt:        startsAt.UTC(),
			EndsAt:          endsAt.UTC(),
			DisplayTimezone: displayTimezone,
		})
		if len(projected) > maxOccurrences {
			return nil, fmt.Errorf("%w: request cap is %d", ErrOccurrenceLimit, maxOccurrences)
		}
	}
	sort.Slice(projected, func(left, right int) bool {
		if projected[left].StartsAt.Equal(projected[right].StartsAt) {
			return projected[left].OccurrenceKey < projected[right].OccurrenceKey
		}
		return projected[left].StartsAt.Before(projected[right].StartsAt)
	})
	return projected, nil
}
