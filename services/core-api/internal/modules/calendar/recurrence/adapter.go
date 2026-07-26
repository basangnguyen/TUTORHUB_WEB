// Package recurrence is the production boundary around the bounded recurrence
// engine selected by ADR-0019.
//
// Callers use a typed rule. The raw RRULE representation is produced only
// inside this package, so HTTP handlers and domain callers cannot silently
// accept an unbounded or unsupported rule.
package recurrence

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	spike "github.com/tutorhub-v2/core-api/internal/spikes/calendarrecurrence"
)

const (
	MaxOccurrences       = spike.MaxOccurrences
	MaxQueryWindowDays   = spike.MaxQueryWindowDays
	MaxSeriesHorizonDays = spike.MaxSeriesHorizonDays
	ExecutionBudget      = spike.ExecutionBudget
)

var (
	ErrInvalidSeries         = spike.ErrInvalidSeries
	ErrInvalidRule           = spike.ErrInvalidRule
	ErrUnsupportedRule       = spike.ErrUnsupportedRule
	ErrInvalidWindow         = spike.ErrInvalidWindow
	ErrOccurrenceLimit       = spike.ErrOccurrenceLimit
	ErrIterationLimit        = spike.ErrIterationLimit
	ErrExecutionBudget       = spike.ErrExecutionBudget
	ErrNonexistentCivilTime  = spike.ErrNonexistentCivilTime
	ErrAmbiguousCivilTime    = spike.ErrAmbiguousCivilTime
	ErrInvalidOverlapPolicy  = spike.ErrInvalidOverlapPolicy
	ErrSeriesHorizonExceeded = spike.ErrSeriesHorizonExceeded
)

type Frequency string

const (
	FrequencyDaily   Frequency = "daily"
	FrequencyWeekly  Frequency = "weekly"
	FrequencyMonthly Frequency = "monthly"
	FrequencyYearly  Frequency = "yearly"
)

type EndType string

const (
	EndOnDate     EndType = "on_date"
	EndAfterCount EndType = "after_count"
)

type Weekday string

const (
	Monday    Weekday = "MO"
	Tuesday   Weekday = "TU"
	Wednesday Weekday = "WE"
	Thursday  Weekday = "TH"
	Friday    Weekday = "FR"
	Saturday  Weekday = "SA"
	Sunday    Weekday = "SU"
)

// OverlapPolicy is explicit whenever a civil time occurs twice.
type OverlapPolicy = spike.OverlapPolicy

const (
	OverlapReject  = spike.OverlapReject
	OverlapEarlier = spike.OverlapEarlier
	OverlapLater   = spike.OverlapLater
)

type End struct {
	Type  EndType
	Date  string
	Count int
}

// Rule is the supported typed recurrence contract. Month fields are only
// meaningful for monthly/yearly rules and are bounded by the adapter.
type Rule struct {
	Frequency Frequency
	Interval  int
	Weekdays  []Weekday
	MonthDays []int
	Months    []int
	End       End
}

type Definition struct {
	ID            string
	StartLocal    string
	TimeZone      string
	Duration      time.Duration
	Rule          Rule
	OverlapPolicy OverlapPolicy
}

type Window = spike.Window

type Occurrence = spike.Occurrence

type Plan struct {
	delegate *spike.Plan
}

func (rule Rule) canonicalRRULE() (string, error) {
	frequency := strings.ToUpper(string(rule.Frequency))
	switch frequency {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
	case "":
		return "", fmt.Errorf("%w: frequency is required", ErrInvalidRule)
	default:
		return "", fmt.Errorf("%w: frequency %q", ErrUnsupportedRule, rule.Frequency)
	}
	if rule.Interval == 0 {
		rule.Interval = 1
	}
	if rule.Interval < 1 || rule.Interval > 366 {
		return "", fmt.Errorf("%w: interval must be between 1 and 366", ErrInvalidRule)
	}
	if (rule.End.Type == EndOnDate) == (rule.End.Type == EndAfterCount) {
		return "", fmt.Errorf("%w: exactly one recurrence end is required", ErrInvalidRule)
	}

	parts := []string{
		"FREQ=" + frequency,
		"INTERVAL=" + strconv.Itoa(rule.Interval),
	}
	if len(rule.Weekdays) > 0 {
		weekdays := append([]Weekday(nil), rule.Weekdays...)
		sort.Slice(weekdays, func(left, right int) bool {
			leftOrder, _ := weekdayOrder(weekdays[left])
			rightOrder, _ := weekdayOrder(weekdays[right])
			return leftOrder < rightOrder
		})
		seen := make(map[Weekday]struct{}, len(weekdays))
		values := make([]string, 0, len(weekdays))
		for _, weekday := range weekdays {
			if _, ok := weekdayOrder(weekday); !ok {
				return "", fmt.Errorf("%w: invalid weekday %q", ErrInvalidRule, weekday)
			}
			if _, duplicate := seen[weekday]; duplicate {
				return "", fmt.Errorf("%w: duplicate weekday %q", ErrInvalidRule, weekday)
			}
			seen[weekday] = struct{}{}
			values = append(values, string(weekday))
		}
		if len(values) > 7 {
			return "", fmt.Errorf("%w: at most seven weekdays are supported", ErrInvalidRule)
		}
		parts = append(parts, "BYDAY="+strings.Join(values, ","))
	}
	if len(rule.MonthDays) > 0 {
		values, err := canonicalIntegers("BYMONTHDAY", rule.MonthDays, -31, 31, true)
		if err != nil {
			return "", err
		}
		parts = append(parts, "BYMONTHDAY="+values)
	}
	if len(rule.Months) > 0 {
		values, err := canonicalIntegers("BYMONTH", rule.Months, 1, 12, false)
		if err != nil {
			return "", err
		}
		parts = append(parts, "BYMONTH="+values)
	}

	switch rule.End.Type {
	case EndAfterCount:
		if rule.End.Count < 1 || rule.End.Count > MaxOccurrences {
			return "", fmt.Errorf("%w: count must be between 1 and %d", ErrInvalidRule, MaxOccurrences)
		}
		parts = append(parts, "COUNT="+strconv.Itoa(rule.End.Count))
	case EndOnDate:
		if _, err := time.Parse("2006-01-02", strings.TrimSpace(rule.End.Date)); err != nil {
			return "", fmt.Errorf("%w: end date must use YYYY-MM-DD", ErrInvalidRule)
		}
		// The spike adapter interprets UNTIL in the series timezone. Keeping a
		// date-only typed boundary avoids clients accidentally supplying offsets.
		parts = append(
			parts,
			"UNTIL="+strings.ReplaceAll(strings.TrimSpace(rule.End.Date), "-", "")+"T235959",
		)
	default:
		return "", fmt.Errorf("%w: unsupported end type %q", ErrInvalidRule, rule.End.Type)
	}
	return strings.Join(parts, ";"), nil
}

func weekdayOrder(value Weekday) (int, bool) {
	switch value {
	case Monday:
		return 1, true
	case Tuesday:
		return 2, true
	case Wednesday:
		return 3, true
	case Thursday:
		return 4, true
	case Friday:
		return 5, true
	case Saturday:
		return 6, true
	case Sunday:
		return 7, true
	default:
		return 0, false
	}
}

func canonicalIntegers(name string, values []int, minimum int, maximum int, allowNegative bool) (string, error) {
	normalized := append([]int(nil), values...)
	sort.Ints(normalized)
	seen := make(map[int]struct{}, len(normalized))
	parts := make([]string, 0, len(normalized))
	for _, value := range normalized {
		if value < minimum || value > maximum || (!allowNegative && value < 0) || value == 0 {
			return "", fmt.Errorf("%w: %s value %d is outside the supported range", ErrInvalidRule, name, value)
		}
		if _, duplicate := seen[value]; duplicate {
			return "", fmt.Errorf("%w: duplicate %s value %d", ErrInvalidRule, name, value)
		}
		seen[value] = struct{}{}
		parts = append(parts, strconv.Itoa(value))
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("%w: %s cannot be empty", ErrInvalidRule, name)
	}
	return strings.Join(parts, ","), nil
}

func Compile(definition Definition) (*Plan, error) {
	rule, err := definition.Rule.canonicalRRULE()
	if err != nil {
		return nil, err
	}
	compiled, err := spike.Compile(spike.Series{
		ID:            definition.ID,
		StartLocal:    definition.StartLocal,
		TimeZone:      definition.TimeZone,
		Duration:      definition.Duration,
		Rule:          rule,
		OverlapPolicy: definition.OverlapPolicy,
	})
	if err != nil {
		return nil, err
	}
	return &Plan{delegate: compiled}, nil
}

func Expand(ctx context.Context, definition Definition, window Window, maxOccurrences int) ([]Occurrence, error) {
	plan, err := Compile(definition)
	if err != nil {
		return nil, err
	}
	return plan.Expand(ctx, window, maxOccurrences)
}

// ResolveOccurrence resolves one civil occurrence with the same bounded
// overlap/DST rules as series expansion. It is used for exception overrides;
// the returned occurrence key is intentionally ignored by callers because an
// override must retain the original occurrence identity.
func ResolveOccurrence(
	ctx context.Context,
	definition Definition,
	originalLocal string,
) (Occurrence, error) {
	originalLocal = strings.TrimSpace(originalLocal)
	if originalLocal == "" {
		return Occurrence{}, fmt.Errorf("%w: original local time is required", ErrInvalidSeries)
	}
	location, err := time.LoadLocation(strings.TrimSpace(definition.TimeZone))
	if err != nil {
		return Occurrence{}, fmt.Errorf("%w: invalid time zone: %v", ErrInvalidSeries, err)
	}
	local, err := time.ParseInLocation("2006-01-02T15:04:05", originalLocal, location)
	if err != nil {
		return Occurrence{}, fmt.Errorf("%w: original local time: %v", ErrInvalidSeries, err)
	}
	single := definition
	single.StartLocal = originalLocal
	single.Rule = Rule{
		Frequency: FrequencyDaily,
		Interval:  1,
		End:       End{Type: EndAfterCount, Count: 1},
	}
	// A civil timestamp can move by an offset change around DST. A two-day
	// instant envelope is deliberately wider than all IANA transitions while
	// remaining bounded by the adapter's normal query-window contract.
	window := Window{
		Start: local.UTC().Add(-48 * time.Hour),
		End:   local.UTC().Add(48 * time.Hour),
	}
	occurrences, err := Expand(ctx, single, window, 1)
	if err != nil {
		return Occurrence{}, err
	}
	if len(occurrences) != 1 || occurrences[0].OriginalLocal != originalLocal {
		return Occurrence{}, fmt.Errorf(
			"%w: occurrence %q was not resolved",
			ErrInvalidSeries,
			originalLocal,
		)
	}
	return occurrences[0], nil
}

func (plan *Plan) Expand(ctx context.Context, window Window, maxOccurrences int) ([]Occurrence, error) {
	if plan == nil || plan.delegate == nil {
		return nil, ErrInvalidSeries
	}
	return plan.delegate.Expand(ctx, window, spike.ExpandOptions{MaxOccurrences: maxOccurrences})
}

func IsBoundedError(err error) bool {
	return errors.Is(err, ErrOccurrenceLimit) ||
		errors.Is(err, ErrIterationLimit) ||
		errors.Is(err, ErrExecutionBudget) ||
		errors.Is(err, ErrSeriesHorizonExceeded)
}
