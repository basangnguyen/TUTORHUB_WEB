package calendarrecurrence

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResourceExhaustionGuards(t *testing.T) {
	t.Parallel()

	base := Series{
		ID:            "series-resource",
		StartLocal:    "2026-01-01T09:00:00",
		TimeZone:      "UTC",
		Duration:      time.Hour,
		Rule:          "FREQ=DAILY;COUNT=100",
		OverlapPolicy: OverlapReject,
	}
	plan, err := Compile(base)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	window := Window{Start: plan.start, End: plan.start.AddDate(0, 0, 120)}

	started := time.Now()
	_, err = plan.Expand(context.Background(), window, ExpandOptions{MaxOccurrences: 8})
	if !errors.Is(err, ErrOccurrenceLimit) {
		t.Fatalf("expected occurrence cap, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > ExecutionBudget {
		t.Fatalf("occurrence cap took %s, budget is %s", elapsed, ExecutionBudget)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := plan.Expand(cancelled, window, ExpandOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	expired, expire := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer expire()
	if _, err := plan.Expand(expired, window, ExpandOptions{}); !errors.Is(err, ErrExecutionBudget) ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected execution and context deadline errors, got %v", err)
	}

	base.Rule = "FREQ=DAILY;COUNT=512;" + strings.Repeat("BYSECOND=1;", 64)
	started = time.Now()
	if _, err := Compile(base); !errors.Is(err, ErrInvalidRule) && !errors.Is(err, ErrUnsupportedRule) {
		t.Fatalf("expected bounded validation error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("malicious rule validation took %s", elapsed)
	}
}

func TestWindowAndSeriesHorizonCaps(t *testing.T) {
	t.Parallel()

	plan, err := Compile(Series{
		ID:            "series-window",
		StartLocal:    "2026-01-01T09:00:00",
		TimeZone:      "UTC",
		Duration:      time.Hour,
		Rule:          "FREQ=DAILY;COUNT=2",
		OverlapPolicy: OverlapReject,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	tests := []struct {
		name   string
		window Window
		want   error
	}{
		{
			name:   "empty",
			window: Window{Start: plan.start, End: plan.start},
			want:   ErrInvalidWindow,
		},
		{
			name: "query_span",
			window: Window{
				Start: plan.start.AddDate(0, 0, -1),
				End:   plan.start.AddDate(0, 0, MaxQueryWindowDays+1),
			},
			want: ErrInvalidWindow,
		},
		{
			name: "series_horizon",
			window: Window{
				Start: plan.start.AddDate(0, 0, MaxSeriesHorizonDays-1),
				End:   plan.start.AddDate(0, 0, MaxSeriesHorizonDays+1),
			},
			want: ErrSeriesHorizonExceeded,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := plan.Expand(context.Background(), test.window, ExpandOptions{})
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestCountSeriesHorizonCaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rule    string
		wantErr error
	}{
		{
			name: "daily_exact_boundary",
			rule: "FREQ=DAILY;INTERVAL=2;COUNT=366",
		},
		{
			name:    "daily_beyond_boundary",
			rule:    "FREQ=DAILY;INTERVAL=2;COUNT=367",
			wantErr: ErrSeriesHorizonExceeded,
		},
		{
			name: "weekly_with_byday_inside_boundary",
			rule: "FREQ=WEEKLY;BYDAY=TH;COUNT=105",
		},
		{
			name:    "weekly_with_byday_beyond_boundary",
			rule:    "FREQ=WEEKLY;BYDAY=TH;COUNT=106",
			wantErr: ErrSeriesHorizonExceeded,
		},
		{
			name: "monthly_with_bymonthday_exact_boundary",
			rule: "FREQ=MONTHLY;BYMONTHDAY=1;COUNT=25",
		},
		{
			name:    "monthly_with_bymonthday_beyond_boundary",
			rule:    "FREQ=MONTHLY;BYMONTHDAY=1;COUNT=26",
			wantErr: ErrSeriesHorizonExceeded,
		},
		{
			name: "yearly_with_bymonth_exact_boundary",
			rule: "FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=1;COUNT=3",
		},
		{
			name:    "yearly_with_bymonth_beyond_boundary",
			rule:    "FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=1;COUNT=4",
			wantErr: ErrSeriesHorizonExceeded,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(Series{
				ID:            "series-count-horizon-" + test.name,
				StartLocal:    "2026-01-01T09:00:00",
				TimeZone:      "UTC",
				Duration:      time.Hour,
				Rule:          test.rule,
				OverlapPolicy: OverlapReject,
			})
			if test.wantErr == nil && err != nil {
				t.Fatalf("expected rule within horizon, got %v", err)
			}
			if test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("expected %v, got %v", test.wantErr, err)
			}
		})
	}
}

func FuzzCompileNeverPanics(f *testing.F) {
	seeds := []string{
		"FREQ=DAILY;COUNT=2",
		"FREQ=WEEKLY;BYDAY=MO,WE;COUNT=20",
		"FREQ=SECONDLY;COUNT=512",
		"",
		"RRULE:FREQ=MONTHLY;BYMONTHDAY=-1;COUNT=4",
	}
	for _, seed := range seeds {
		f.Add(seed, "2026-07-20T08:00:00", "Asia/Ho_Chi_Minh")
	}
	f.Fuzz(func(t *testing.T, rule string, startLocal string, zone string) {
		_, _ = Compile(Series{
			ID:            "fuzz-series",
			StartLocal:    startLocal,
			TimeZone:      zone,
			Duration:      time.Hour,
			Rule:          rule,
			OverlapPolicy: OverlapReject,
		})
	})
}

func FuzzExpandStaysWithinCap(f *testing.F) {
	f.Add(uint8(1), uint16(4), uint16(4))
	f.Add(uint8(4), uint16(8), uint16(3))
	f.Fuzz(func(t *testing.T, intervalRaw uint8, countRaw uint16, capRaw uint16) {
		interval := 1 + int(intervalRaw%8)
		count := 1 + int(countRaw%8)
		itemCap := 1 + int(capRaw%8)
		plan, err := Compile(Series{
			ID:            "fuzz-expand",
			StartLocal:    "2026-01-01T12:00:00",
			TimeZone:      "America/New_York",
			Duration:      time.Hour,
			Rule:          recurrenceRule(interval, count),
			OverlapPolicy: OverlapEarlier,
		})
		if err != nil {
			t.Fatalf("compile generated valid rule: %v", err)
		}
		window := Window{
			Start: plan.start,
			End:   plan.start.AddDate(0, 0, MaxQueryWindowDays),
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		occurrences, expandErr := plan.Expand(ctx, window, ExpandOptions{MaxOccurrences: itemCap})
		if expandErr != nil && !errors.Is(expandErr, ErrOccurrenceLimit) {
			t.Fatalf("unexpected expansion error: %v", expandErr)
		}
		if len(occurrences) > itemCap {
			t.Fatalf("expanded %d items above cap %d", len(occurrences), itemCap)
		}
	})
}

func recurrenceRule(interval int, count int) string {
	return "FREQ=DAILY;INTERVAL=" + itoa(interval) + ";COUNT=" + itoa(count)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	position := len(digits)
	for value > 0 {
		position--
		digits[position] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[position:])
}
