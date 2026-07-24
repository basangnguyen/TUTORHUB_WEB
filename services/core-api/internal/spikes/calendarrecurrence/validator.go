package calendarrecurrence

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	rrule "github.com/teambition/rrule-go"
)

const (
	maxRuleBytes = 512
	maxInterval  = 366
)

var allowedProperties = map[string]struct{}{
	"FREQ":       {},
	"INTERVAL":   {},
	"COUNT":      {},
	"UNTIL":      {},
	"WKST":       {},
	"BYDAY":      {},
	"BYMONTHDAY": {},
	"BYMONTH":    {},
}

type parsedRule struct {
	option rrule.ROption
}

func parseAndValidateRule(
	raw string,
	startCivil civilDateTime,
	start time.Time,
	location *time.Location,
	overlapPolicy OverlapPolicy,
) (parsedRule, error) {
	canonical, properties, err := parseProperties(raw)
	if err != nil {
		return parsedRule{}, err
	}
	if err := validateFrequency(properties["FREQ"]); err != nil {
		return parsedRule{}, err
	}
	if value, exists := properties["INTERVAL"]; exists {
		if _, err := boundedPositiveInteger("INTERVAL", value, maxInterval); err != nil {
			return parsedRule{}, err
		}
	}
	if value, exists := properties["WKST"]; exists && !isWeekday(value) {
		return parsedRule{}, fmt.Errorf("%w: invalid WKST %q", ErrInvalidRule, value)
	}
	if value, exists := properties["BYDAY"]; exists {
		if err := validateWeekdays(value); err != nil {
			return parsedRule{}, err
		}
	}
	if value, exists := properties["BYMONTHDAY"]; exists {
		if err := validateIntegerList("BYMONTHDAY", value, -31, 31, true, 31); err != nil {
			return parsedRule{}, err
		}
	}
	if value, exists := properties["BYMONTH"]; exists {
		if err := validateIntegerList("BYMONTH", value, 1, 12, false, 12); err != nil {
			return parsedRule{}, err
		}
	}

	countValue, hasCount := properties["COUNT"]
	untilValue, hasUntil := properties["UNTIL"]
	if hasCount == hasUntil {
		return parsedRule{}, fmt.Errorf(
			"%w: exactly one of COUNT or UNTIL is required",
			ErrInvalidRule,
		)
	}
	count := 0
	if hasCount {
		count, err = boundedPositiveInteger("COUNT", countValue, MaxOccurrences)
		if err != nil {
			return parsedRule{}, err
		}
	}

	option, err := rrule.StrToROptionInLocation(canonical, location)
	if err != nil {
		return parsedRule{}, fmt.Errorf("%w: parse candidate rule: %v", ErrInvalidRule, err)
	}
	option.Dtstart = start

	maxUntil, err := seriesHorizon(startCivil, location, overlapPolicy)
	if err != nil {
		return parsedRule{}, fmt.Errorf("%w: calculate series horizon: %v", ErrInvalidRule, err)
	}
	if hasUntil {
		until, err := parseUntil(untilValue, location, overlapPolicy)
		if err != nil {
			return parsedRule{}, fmt.Errorf("%w: UNTIL: %v", ErrInvalidRule, err)
		}
		if until.Before(start) {
			return parsedRule{}, fmt.Errorf("%w: UNTIL is before DTSTART", ErrInvalidRule)
		}
		if until.After(maxUntil) {
			return parsedRule{}, fmt.Errorf(
				"%w: UNTIL exceeds %d-day horizon",
				ErrSeriesHorizonExceeded,
				MaxSeriesHorizonDays,
			)
		}
		option.Until = until
	}
	if hasCount {
		if err := validateCountHorizon(*option, count, maxUntil); err != nil {
			return parsedRule{}, err
		}
	}

	return parsedRule{option: *option}, nil
}

func seriesHorizon(
	startCivil civilDateTime,
	location *time.Location,
	overlapPolicy OverlapPolicy,
) (time.Time, error) {
	horizonCivil := time.Date(
		startCivil.Year,
		startCivil.Month,
		startCivil.Day,
		startCivil.Hour,
		startCivil.Minute,
		startCivil.Second,
		0,
		time.UTC,
	).AddDate(0, 0, MaxSeriesHorizonDays)
	return resolveCivil(civilFromTime(horizonCivil), location, overlapPolicy)
}

// validateCountHorizon proves that the complete COUNT-bounded recurrence fits
// inside the accepted civil-time horizon. Adding UNTIL to this temporary
// candidate bounds the library iterator even for sparse BY* combinations.
func validateCountHorizon(option rrule.ROption, count int, horizon time.Time) error {
	bounded := cloneOption(option)
	bounded.Until = horizon

	candidate, err := rrule.NewRRule(bounded)
	if err != nil {
		return fmt.Errorf("%w: candidate rejected bounded COUNT rule: %v", ErrInvalidRule, err)
	}
	next := candidate.Iterator()
	for occurrence := 0; occurrence < count; occurrence++ {
		if _, ok := next(); !ok {
			return fmt.Errorf(
				"%w: COUNT cannot be satisfied within %d-day horizon",
				ErrSeriesHorizonExceeded,
				MaxSeriesHorizonDays,
			)
		}
	}
	return nil
}

func parseProperties(raw string) (string, map[string]string, error) {
	rule := strings.TrimSpace(raw)
	if len(rule) == 0 || len(rule) > maxRuleBytes {
		return "", nil, fmt.Errorf(
			"%w: rule length must be between 1 and %d bytes",
			ErrInvalidRule,
			maxRuleBytes,
		)
	}
	for _, character := range []byte(rule) {
		if character < 0x21 || character > 0x7e {
			return "", nil, fmt.Errorf("%w: rule must be printable ASCII without spaces", ErrInvalidRule)
		}
	}

	rule = strings.ToUpper(rule)
	rule = strings.TrimPrefix(rule, "RRULE:")
	properties := make(map[string]string, 8)
	parts := strings.Split(rule, ";")
	for _, part := range parts {
		keyValue := strings.SplitN(part, "=", 2)
		if len(keyValue) != 2 || keyValue[0] == "" || keyValue[1] == "" {
			return "", nil, fmt.Errorf("%w: malformed property %q", ErrInvalidRule, part)
		}
		key, value := keyValue[0], keyValue[1]
		if _, allowed := allowedProperties[key]; !allowed {
			return "", nil, fmt.Errorf("%w: property %s", ErrUnsupportedRule, key)
		}
		if _, duplicate := properties[key]; duplicate {
			return "", nil, fmt.Errorf("%w: duplicate property %s", ErrInvalidRule, key)
		}
		properties[key] = value
	}
	if _, exists := properties["FREQ"]; !exists {
		return "", nil, fmt.Errorf("%w: FREQ is required", ErrInvalidRule)
	}
	return rule, properties, nil
}

func validateFrequency(value string) error {
	switch value {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
		return nil
	case "HOURLY", "MINUTELY", "SECONDLY":
		return fmt.Errorf("%w: frequency %s", ErrUnsupportedRule, value)
	default:
		return fmt.Errorf("%w: invalid FREQ %q", ErrInvalidRule, value)
	}
}

func validateWeekdays(value string) error {
	parts := strings.Split(value, ",")
	if len(parts) == 0 || len(parts) > 7 {
		return fmt.Errorf("%w: BYDAY accepts 1 to 7 weekdays", ErrInvalidRule)
	}
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if !isWeekday(part) {
			return fmt.Errorf(
				"%w: BYDAY only supports unnumbered weekdays, got %q",
				ErrUnsupportedRule,
				part,
			)
		}
		if _, duplicate := seen[part]; duplicate {
			return fmt.Errorf("%w: duplicate BYDAY value %q", ErrInvalidRule, part)
		}
		seen[part] = struct{}{}
	}
	return nil
}

func isWeekday(value string) bool {
	switch value {
	case "MO", "TU", "WE", "TH", "FR", "SA", "SU":
		return true
	default:
		return false
	}
}

func validateIntegerList(
	name string,
	value string,
	minimum int,
	maximum int,
	rejectZero bool,
	maxItems int,
) error {
	parts := strings.Split(value, ",")
	if len(parts) == 0 || len(parts) > maxItems {
		return fmt.Errorf("%w: %s accepts 1 to %d values", ErrInvalidRule, name, maxItems)
	}
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < minimum || number > maximum || (rejectZero && number == 0) {
			return fmt.Errorf("%w: invalid %s value %q", ErrInvalidRule, name, part)
		}
		if _, duplicate := seen[number]; duplicate {
			return fmt.Errorf("%w: duplicate %s value %d", ErrInvalidRule, name, number)
		}
		seen[number] = struct{}{}
	}
	return nil
}

func boundedPositiveInteger(name string, value string, maximum int) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("%w: %s is empty", ErrInvalidRule, name)
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, fmt.Errorf("%w: %s must be a positive integer", ErrInvalidRule, name)
		}
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 || number > maximum {
		return 0, fmt.Errorf(
			"%w: %s must be between 1 and %d",
			ErrInvalidRule,
			name,
			maximum,
		)
	}
	return number, nil
}

func parseUntil(
	value string,
	location *time.Location,
	overlapPolicy OverlapPolicy,
) (time.Time, error) {
	if len(value) == len("20060102T150405Z") && strings.HasSuffix(value, "Z") {
		parsed, err := time.Parse("20060102T150405Z", value)
		if err != nil {
			return time.Time{}, err
		}
		return parsed, nil
	}
	if len(value) != len("20060102T150405") {
		return time.Time{}, fmt.Errorf("must use YYYYMMDDTHHMMSS or YYYYMMDDTHHMMSSZ")
	}
	parsed, err := time.Parse("20060102T150405", value)
	if err != nil {
		return time.Time{}, err
	}
	return resolveCivil(civilFromTime(parsed), location, overlapPolicy)
}
