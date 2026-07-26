package recurrence

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTypedRuleCompilesAndKeepsCivilTime(t *testing.T) {
	t.Parallel()
	occurrences, err := Expand(context.Background(), Definition{
		ID:         "series-typed",
		StartLocal: "2026-07-20T19:30:00",
		TimeZone:   "Asia/Ho_Chi_Minh",
		Duration:   90 * time.Minute,
		Rule: Rule{
			Frequency: FrequencyWeekly,
			Interval:  1,
			Weekdays:  []Weekday{Wednesday, Monday},
			End:       End{Type: EndAfterCount, Count: 4},
		},
		OverlapPolicy: OverlapReject,
	}, Window{
		Start: time.Date(2026, 7, 19, 0, 0, 0, 0, time.FixedZone("ICT", 7*60*60)),
		End:   time.Date(2026, 8, 20, 0, 0, 0, 0, time.FixedZone("ICT", 7*60*60)),
	}, 0)
	if err != nil {
		t.Fatalf("expand typed rule: %v", err)
	}
	if len(occurrences) != 4 {
		t.Fatalf("expected four occurrences, got %d", len(occurrences))
	}
	for _, occurrence := range occurrences {
		if got := occurrence.StartsAt.In(time.FixedZone("ICT", 7*60*60)).Format("15:04:05"); got != "19:30:00" {
			t.Fatalf("wall clock changed: %s", got)
		}
	}
}

func TestTypedRuleRejectsUnboundedAndUnsupportedInput(t *testing.T) {
	t.Parallel()
	base := Definition{
		ID: "series-invalid", StartLocal: "2026-07-20T09:00:00",
		TimeZone: "UTC", Duration: time.Hour, OverlapPolicy: OverlapReject,
	}
	_, err := Compile(base)
	if !errors.Is(err, ErrInvalidRule) {
		t.Fatalf("expected missing end error, got %v", err)
	}
	base.Rule = Rule{Frequency: Frequency("hourly"), End: End{Type: EndAfterCount, Count: 2}}
	_, err = Compile(base)
	if !errors.Is(err, ErrUnsupportedRule) {
		t.Fatalf("expected unsupported frequency error, got %v", err)
	}
	base.Rule = Rule{Frequency: FrequencyDaily, End: End{Type: EndOnDate, Date: "2028-07-20"}}
	_, err = Compile(base)
	if !errors.Is(err, ErrSeriesHorizonExceeded) {
		t.Fatalf("expected horizon error, got %v", err)
	}
}
