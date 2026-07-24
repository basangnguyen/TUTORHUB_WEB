package calendarrecurrence

import (
	"fmt"
	"sort"
	"time"
)

const civilLayout = "2006-01-02T15:04:05"

type civilDateTime struct {
	Year   int
	Month  time.Month
	Day    int
	Hour   int
	Minute int
	Second int
}

func parseCivilDateTime(value string) (civilDateTime, error) {
	parsed, err := time.Parse(civilLayout, value)
	if err != nil {
		return civilDateTime{}, fmt.Errorf("must use %s: %w", civilLayout, err)
	}
	return civilFromTime(parsed), nil
}

func civilFromTime(value time.Time) civilDateTime {
	year, month, day := value.Date()
	hour, minute, second := value.Clock()
	return civilDateTime{
		Year:   year,
		Month:  month,
		Day:    day,
		Hour:   hour,
		Minute: minute,
		Second: second,
	}
}

func (value civilDateTime) String() string {
	return time.Date(
		value.Year,
		value.Month,
		value.Day,
		value.Hour,
		value.Minute,
		value.Second,
		0,
		time.UTC,
	).Format(civilLayout)
}

// resolveCivil maps a wall-clock tuple to zero, one, or two instants without
// relying on time.Date's implementation-defined choice at zone transitions.
func resolveCivil(
	value civilDateTime,
	location *time.Location,
	policy OverlapPolicy,
) (time.Time, error) {
	candidates := civilCandidates(value, location)
	switch len(candidates) {
	case 0:
		return time.Time{}, fmt.Errorf("%w: %s in %s", ErrNonexistentCivilTime, value, location)
	case 1:
		return candidates[0], nil
	case 2:
		switch policy {
		case OverlapEarlier:
			return candidates[0], nil
		case OverlapLater:
			return candidates[1], nil
		default:
			return time.Time{}, fmt.Errorf("%w: %s in %s", ErrAmbiguousCivilTime, value, location)
		}
	default:
		return time.Time{}, fmt.Errorf(
			"%w: %s in %s resolved to %d instants",
			ErrInvalidSeries,
			value,
			location,
			len(candidates),
		)
	}
}

func civilCandidates(value civilDateTime, location *time.Location) []time.Time {
	wallAsUTC := time.Date(
		value.Year,
		value.Month,
		value.Day,
		value.Hour,
		value.Minute,
		value.Second,
		0,
		time.UTC,
	)

	offsets := make(map[int]struct{}, 4)
	for hours := -48; hours <= 48; hours += 6 {
		_, offset := wallAsUTC.Add(time.Duration(hours) * time.Hour).In(location).Zone()
		offsets[offset] = struct{}{}
	}

	candidates := make([]time.Time, 0, 2)
	for offset := range offsets {
		candidate := wallAsUTC.Add(-time.Duration(offset) * time.Second)
		if sameCivil(civilFromTime(candidate.In(location)), value) {
			candidates = append(candidates, candidate.In(location))
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		return candidates[left].Before(candidates[right])
	})
	return candidates
}

func sameCivil(left, right civilDateTime) bool {
	return left.Year == right.Year &&
		left.Month == right.Month &&
		left.Day == right.Day &&
		left.Hour == right.Hour &&
		left.Minute == right.Minute &&
		left.Second == right.Second
}
