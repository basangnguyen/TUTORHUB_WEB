// Package calendarrecurrence is an isolated P3-CAL-01 recurrence spike.
//
// It deliberately exposes a smaller, bounded subset of RFC 5545 than the
// candidate recurrence library. The package is not wired to production
// handlers or persistence.
package calendarrecurrence

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	rrule "github.com/teambition/rrule-go"
)

const (
	// MaxOccurrences is the hard per-expansion item limit.
	MaxOccurrences = 512
	// MaxWindowDays bounds the query and civil-time calculation horizon.
	MaxWindowDays = 730
	// MaxIterations limits calls to the candidate iterator.
	MaxIterations = 2048
	// ExecutionBudget is the adapter-owned deadline for a single expansion.
	ExecutionBudget = 250 * time.Millisecond

	maxSeriesIDBytes = 128
	maxTimeZoneBytes = 128
	minDuration      = time.Minute
	maxDuration      = 24 * time.Hour
)

var (
	ErrInvalidSeries          = errors.New("invalid recurrence series")
	ErrInvalidRule            = errors.New("invalid recurrence rule")
	ErrUnsupportedRule        = errors.New("unsupported recurrence rule")
	ErrInvalidWindow          = errors.New("invalid recurrence window")
	ErrOccurrenceLimit        = errors.New("recurrence occurrence limit exceeded")
	ErrIterationLimit         = errors.New("recurrence iteration limit exceeded")
	ErrExecutionBudget        = errors.New("recurrence execution budget exceeded")
	ErrNonexistentCivilTime   = errors.New("nonexistent civil time")
	ErrAmbiguousCivilTime     = errors.New("ambiguous civil time")
	ErrInvalidOverlapPolicy   = errors.New("invalid overlap policy")
	ErrSeriesHorizonExceeded  = errors.New("recurrence series horizon exceeded")
	ErrCandidateIteratorFault = errors.New("recurrence candidate iterator fault")
)

// OverlapPolicy selects an instant when an IANA time zone repeats a wall time.
// Reject is the safe default and requires an explicit organizer choice.
type OverlapPolicy string

const (
	OverlapReject  OverlapPolicy = "reject"
	OverlapEarlier OverlapPolicy = "earlier"
	OverlapLater   OverlapPolicy = "later"
)

// Series is the civil-time source of truth for a recurring event.
// StartLocal must use YYYY-MM-DDTHH:MM:SS and must not contain an offset.
type Series struct {
	ID            string
	StartLocal    string
	TimeZone      string
	Duration      time.Duration
	Rule          string
	OverlapPolicy OverlapPolicy
}

// Window is a half-open instant interval [Start, End).
type Window struct {
	Start time.Time
	End   time.Time
}

// ExpandOptions can lower, but never raise, the adapter hard cap.
type ExpandOptions struct {
	MaxOccurrences int
}

// Occurrence preserves both the original civil tuple and its resolved instant.
type Occurrence struct {
	Key           string
	OriginalLocal string
	StartsAt      time.Time
	EndsAt        time.Time
}

// Plan is a validated recurrence definition. It is safe for concurrent use
// because each expansion constructs an independent candidate iterator.
type Plan struct {
	series     Series
	location   *time.Location
	startCivil civilDateTime
	start      time.Time
	option     rrule.ROption
}

// Compile validates and compiles the bounded recurrence subset.
func Compile(series Series) (*Plan, error) {
	if series.ID == "" {
		return nil, fmt.Errorf("%w: id is required", ErrInvalidSeries)
	}
	if len(series.ID) > maxSeriesIDBytes {
		return nil, fmt.Errorf("%w: id exceeds %d bytes", ErrInvalidSeries, maxSeriesIDBytes)
	}
	if len(series.TimeZone) == 0 || len(series.TimeZone) > maxTimeZoneBytes {
		return nil, fmt.Errorf(
			"%w: time zone must be between 1 and %d bytes",
			ErrInvalidSeries,
			maxTimeZoneBytes,
		)
	}
	if series.Duration < minDuration || series.Duration > maxDuration {
		return nil, fmt.Errorf(
			"%w: duration must be between %s and %s",
			ErrInvalidSeries,
			minDuration,
			maxDuration,
		)
	}
	if series.OverlapPolicy == "" {
		series.OverlapPolicy = OverlapReject
	}
	if !validOverlapPolicy(series.OverlapPolicy) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidOverlapPolicy, series.OverlapPolicy)
	}

	location, err := time.LoadLocation(series.TimeZone)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid IANA time zone %q: %v", ErrInvalidSeries, series.TimeZone, err)
	}
	startCivil, err := parseCivilDateTime(series.StartLocal)
	if err != nil {
		return nil, fmt.Errorf("%w: start_local: %v", ErrInvalidSeries, err)
	}
	start, err := resolveCivil(startCivil, location, series.OverlapPolicy)
	if err != nil {
		return nil, fmt.Errorf("%w: start_local: %w", ErrInvalidSeries, err)
	}

	parsed, err := parseAndValidateRule(series.Rule, startCivil, start, location, series.OverlapPolicy)
	if err != nil {
		return nil, err
	}
	parsed.option.Dtstart = start
	if _, err := rrule.NewRRule(parsed.option); err != nil {
		return nil, fmt.Errorf("%w: candidate rejected rule: %v", ErrInvalidRule, err)
	}

	return &Plan{
		series:     series,
		location:   location,
		startCivil: startCivil,
		start:      start,
		option:     parsed.option,
	}, nil
}

// Expand is a convenience wrapper for compile-and-expand.
func Expand(
	ctx context.Context,
	series Series,
	window Window,
	options ExpandOptions,
) ([]Occurrence, error) {
	plan, err := Compile(series)
	if err != nil {
		return nil, err
	}
	return plan.Expand(ctx, window, options)
}

// Expand evaluates a validated series through a bounded iterator.
func (p *Plan) Expand(
	ctx context.Context,
	window Window,
	options ExpandOptions,
) ([]Occurrence, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: nil plan", ErrInvalidSeries)
	}
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrExecutionBudget)
	}
	if err := validateWindow(p, window); err != nil {
		return nil, err
	}
	itemLimit, err := normalizeItemLimit(options.MaxOccurrences)
	if err != nil {
		return nil, err
	}

	boundedContext, cancel := context.WithTimeout(ctx, ExecutionBudget)
	defer cancel()
	if err := boundedContext.Err(); err != nil {
		return nil, executionError(err)
	}

	candidate, err := rrule.NewRRule(cloneOption(p.option))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCandidateIteratorFault, err)
	}
	next := candidate.Iterator()
	occurrences := make([]Occurrence, 0, min(itemLimit, 32))

	for iteration := 0; iteration < MaxIterations; iteration++ {
		if err := boundedContext.Err(); err != nil {
			return nil, executionError(err)
		}
		candidateStart, ok := next()
		if err := boundedContext.Err(); err != nil {
			return nil, executionError(err)
		}
		if !ok {
			return occurrences, nil
		}

		candidateLocal := candidateStart.In(p.location)
		year, month, day := candidateLocal.Date()
		original := civilDateTime{
			Year:   year,
			Month:  month,
			Day:    day,
			Hour:   p.startCivil.Hour,
			Minute: p.startCivil.Minute,
			Second: p.startCivil.Second,
		}
		resolvedStart, err := resolveCivil(original, p.location, p.series.OverlapPolicy)
		if err != nil {
			return nil, fmt.Errorf("resolve occurrence %s in %s: %w", original, p.series.TimeZone, err)
		}

		if !resolvedStart.Before(window.End) {
			return occurrences, nil
		}
		resolvedEnd := resolvedStart.Add(p.series.Duration)
		if resolvedEnd.After(window.Start) {
			if len(occurrences) == itemLimit {
				return nil, fmt.Errorf("%w: hard cap is %d", ErrOccurrenceLimit, itemLimit)
			}
			originalLocal := original.String()
			occurrences = append(occurrences, Occurrence{
				Key: occurrenceKey(
					p.series.ID,
					originalLocal,
					p.series.TimeZone,
					resolvedStart,
				),
				OriginalLocal: originalLocal,
				StartsAt:      resolvedStart,
				EndsAt:        resolvedEnd,
			})
		}
	}

	return nil, fmt.Errorf("%w: hard cap is %d", ErrIterationLimit, MaxIterations)
}

func validateWindow(plan *Plan, window Window) error {
	if window.Start.IsZero() || window.End.IsZero() || !window.Start.Before(window.End) {
		return fmt.Errorf("%w: expected a non-empty half-open interval", ErrInvalidWindow)
	}
	if window.End.Sub(window.Start) > (MaxWindowDays*24+2)*time.Hour {
		return fmt.Errorf("%w: maximum query span is %d civil days", ErrInvalidWindow, MaxWindowDays)
	}
	horizon := plan.start.AddDate(0, 0, MaxWindowDays)
	if window.End.After(horizon) {
		return fmt.Errorf(
			"%w: window end %s is after %s",
			ErrSeriesHorizonExceeded,
			window.End.Format(time.RFC3339),
			horizon.Format(time.RFC3339),
		)
	}
	return nil
}

func normalizeItemLimit(requested int) (int, error) {
	if requested == 0 {
		return MaxOccurrences, nil
	}
	if requested < 1 || requested > MaxOccurrences {
		return 0, fmt.Errorf(
			"%w: max_occurrences must be between 1 and %d",
			ErrInvalidWindow,
			MaxOccurrences,
		)
	}
	return requested, nil
}

func validOverlapPolicy(policy OverlapPolicy) bool {
	return policy == OverlapReject || policy == OverlapEarlier || policy == OverlapLater
}

func executionError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.Join(ErrExecutionBudget, err)
	}
	return err
}

func cloneOption(source rrule.ROption) rrule.ROption {
	cloned := source
	cloned.Bysetpos = append([]int(nil), source.Bysetpos...)
	cloned.Bymonth = append([]int(nil), source.Bymonth...)
	cloned.Bymonthday = append([]int(nil), source.Bymonthday...)
	cloned.Byyearday = append([]int(nil), source.Byyearday...)
	cloned.Byweekno = append([]int(nil), source.Byweekno...)
	cloned.Byweekday = append([]rrule.Weekday(nil), source.Byweekday...)
	cloned.Byhour = append([]int(nil), source.Byhour...)
	cloned.Byminute = append([]int(nil), source.Byminute...)
	cloned.Bysecond = append([]int(nil), source.Bysecond...)
	cloned.Byeaster = append([]int(nil), source.Byeaster...)
	return cloned
}

func occurrenceKey(
	seriesID string,
	originalLocal string,
	timeZone string,
	resolvedStart time.Time,
) string {
	_, offsetSeconds := resolvedStart.Zone()
	payload := seriesID + "\x00" + originalLocal + "\x00" +
		timeZone + "\x00" + fmt.Sprintf("%d", offsetSeconds)
	sum := sha256.Sum256([]byte(payload))
	return "occ_" + base64.RawURLEncoding.EncodeToString(sum[:18])
}
