package calendarrecurrence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type goldenCase struct {
	Name            string   `json:"name"`
	SeriesID        string   `json:"series_id"`
	StartLocal      string   `json:"start_local"`
	TimeZone        string   `json:"time_zone"`
	DurationMinutes int      `json:"duration_minutes"`
	Rule            string   `json:"rule"`
	OverlapPolicy   string   `json:"overlap_policy"`
	WindowStart     string   `json:"window_start"`
	WindowEnd       string   `json:"window_end"`
	ExpectedStarts  []string `json:"expected_starts"`
	ExpectedError   string   `json:"expected_error"`
}

func TestGoldenRecurrenceCases(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "golden.json"))
	if err != nil {
		t.Fatalf("read golden fixtures: %v", err)
	}
	var fixtures []goldenCase
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("decode golden fixtures: %v", err)
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()
			series := Series{
				ID:            fixture.SeriesID,
				StartLocal:    fixture.StartLocal,
				TimeZone:      fixture.TimeZone,
				Duration:      time.Duration(fixture.DurationMinutes) * time.Minute,
				Rule:          fixture.Rule,
				OverlapPolicy: OverlapPolicy(fixture.OverlapPolicy),
			}
			window := Window{
				Start: mustParseRFC3339(t, fixture.WindowStart),
				End:   mustParseRFC3339(t, fixture.WindowEnd),
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			occurrences, err := Expand(ctx, series, window, ExpandOptions{})
			if fixture.ExpectedError != "" {
				expected := map[string]error{
					"nonexistent_civil_time": ErrNonexistentCivilTime,
					"ambiguous_civil_time":   ErrAmbiguousCivilTime,
				}[fixture.ExpectedError]
				if expected == nil {
					t.Fatalf("unknown expected_error %q", fixture.ExpectedError)
				}
				if !errors.Is(err, expected) {
					t.Fatalf("expected %v, got %v", expected, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expand: %v", err)
			}
			if len(occurrences) != len(fixture.ExpectedStarts) {
				t.Fatalf("expected %d occurrences, got %d: %#v", len(fixture.ExpectedStarts), len(occurrences), occurrences)
			}

			keys := make(map[string]struct{}, len(occurrences))
			for index, occurrence := range occurrences {
				expected := mustParseRFC3339(t, fixture.ExpectedStarts[index])
				if !occurrence.StartsAt.Equal(expected) {
					t.Errorf(
						"occurrence %d: expected %s, got %s",
						index,
						expected.Format(time.RFC3339),
						occurrence.StartsAt.Format(time.RFC3339),
					)
				}
				if occurrence.EndsAt.Sub(occurrence.StartsAt) != series.Duration {
					t.Errorf("occurrence %d: duration changed", index)
				}
				if occurrence.Key == "" {
					t.Errorf("occurrence %d: empty key", index)
				}
				if _, duplicate := keys[occurrence.Key]; duplicate {
					t.Errorf("occurrence %d: duplicate key %q", index, occurrence.Key)
				}
				keys[occurrence.Key] = struct{}{}
			}
		})
	}
}

func TestBoundedRuleValidation(t *testing.T) {
	t.Parallel()

	validSeries := func(rule string) Series {
		return Series{
			ID:            "series-validation",
			StartLocal:    "2026-07-20T08:00:00",
			TimeZone:      "Asia/Ho_Chi_Minh",
			Duration:      time.Hour,
			Rule:          rule,
			OverlapPolicy: OverlapReject,
		}
	}
	tests := []struct {
		name string
		rule string
		want error
	}{
		{name: "secondly", rule: "FREQ=SECONDLY;COUNT=10", want: ErrUnsupportedRule},
		{name: "minutely", rule: "FREQ=MINUTELY;COUNT=10", want: ErrUnsupportedRule},
		{name: "hourly", rule: "FREQ=HOURLY;COUNT=10", want: ErrUnsupportedRule},
		{name: "no_terminator", rule: "FREQ=DAILY", want: ErrInvalidRule},
		{name: "two_terminators", rule: "FREQ=DAILY;COUNT=2;UNTIL=20260722T080000", want: ErrInvalidRule},
		{name: "count_over_cap", rule: "FREQ=DAILY;COUNT=513", want: ErrInvalidRule},
		{name: "zero_interval", rule: "FREQ=DAILY;INTERVAL=0;COUNT=2", want: ErrInvalidRule},
		{name: "large_interval", rule: "FREQ=DAILY;INTERVAL=367;COUNT=2", want: ErrInvalidRule},
		{name: "raw_time_selector", rule: "FREQ=DAILY;BYHOUR=8;COUNT=2", want: ErrUnsupportedRule},
		{name: "numbered_weekday", rule: "FREQ=MONTHLY;BYDAY=2MO;COUNT=2", want: ErrUnsupportedRule},
		{name: "duplicate_property", rule: "FREQ=DAILY;COUNT=2;COUNT=3", want: ErrInvalidRule},
		{name: "space", rule: "FREQ=DAILY; COUNT=2", want: ErrInvalidRule},
		{name: "by_month_day_zero", rule: "FREQ=MONTHLY;BYMONTHDAY=0;COUNT=2", want: ErrInvalidRule},
		{name: "invalid_month", rule: "FREQ=YEARLY;BYMONTH=13;COUNT=2", want: ErrInvalidRule},
		{name: "date_only_until", rule: "FREQ=DAILY;UNTIL=20260722", want: ErrInvalidRule},
		{
			name: "until_over_horizon",
			rule: "FREQ=DAILY;UNTIL=20290722T080000",
			want: ErrSeriesHorizonExceeded,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(validSeries(test.rule))
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestCivilTimeValidation(t *testing.T) {
	t.Parallel()

	base := Series{
		ID:            "series-civil",
		StartLocal:    "2026-03-08T02:30:00",
		TimeZone:      "America/New_York",
		Duration:      time.Hour,
		Rule:          "FREQ=DAILY;COUNT=2",
		OverlapPolicy: OverlapReject,
	}
	if _, err := Compile(base); !errors.Is(err, ErrNonexistentCivilTime) {
		t.Fatalf("expected spring gap rejection, got %v", err)
	}

	base.StartLocal = "2026-11-01T01:30:00"
	if _, err := Compile(base); !errors.Is(err, ErrAmbiguousCivilTime) {
		t.Fatalf("expected overlap rejection, got %v", err)
	}
	base.OverlapPolicy = OverlapEarlier
	earlier, err := Compile(base)
	if err != nil {
		t.Fatalf("compile earlier policy: %v", err)
	}
	base.OverlapPolicy = OverlapLater
	later, err := Compile(base)
	if err != nil {
		t.Fatalf("compile later policy: %v", err)
	}
	if !earlier.start.Before(later.start) || later.start.Sub(earlier.start) != time.Hour {
		t.Fatalf("expected one-hour overlap, earlier=%s later=%s", earlier.start, later.start)
	}
}

func TestPropertyWallClockAndStableIdentity(t *testing.T) {
	t.Parallel()

	zones := []string{"Asia/Ho_Chi_Minh", "America/New_York", "Europe/London", "UTC"}
	random := rand.New(rand.NewPCG(0x504343414c3031, 0x5252554c45474f))
	for index := 0; index < 160; index++ {
		zone := zones[random.IntN(len(zones))]
		year := 2025 + random.IntN(3)
		month := time.Month(1 + random.IntN(12))
		day := 1 + random.IntN(27)
		hour := 10 + random.IntN(5)
		count := 1 + random.IntN(40)
		interval := 1 + random.IntN(4)
		startLocal := fmt.Sprintf("%04d-%02d-%02dT%02d:15:00", year, month, day, hour)
		series := Series{
			ID:            fmt.Sprintf("property-%d", index),
			StartLocal:    startLocal,
			TimeZone:      zone,
			Duration:      75 * time.Minute,
			Rule:          fmt.Sprintf("FREQ=DAILY;INTERVAL=%d;COUNT=%d", interval, count),
			OverlapPolicy: OverlapEarlier,
		}
		plan, err := Compile(series)
		if err != nil {
			t.Fatalf("case %d compile: %v", index, err)
		}
		window := Window{
			Start: plan.start.Add(-time.Hour),
			End:   plan.start.AddDate(0, 0, 200),
		}
		first, err := plan.Expand(context.Background(), window, ExpandOptions{})
		if err != nil {
			t.Fatalf("case %d first expansion: %v", index, err)
		}
		second, err := plan.Expand(context.Background(), window, ExpandOptions{})
		if err != nil {
			t.Fatalf("case %d second expansion: %v", index, err)
		}
		if len(first) != count || len(second) != count {
			t.Fatalf("case %d expected %d items, got %d and %d", index, count, len(first), len(second))
		}
		for occurrenceIndex := range first {
			current := first[occurrenceIndex]
			local := current.StartsAt.In(plan.location)
			gotHour, gotMinute, gotSecond := local.Clock()
			if gotHour != hour || gotMinute != 15 || gotSecond != 0 {
				t.Fatalf("case %d occurrence %d changed wall clock to %s", index, occurrenceIndex, local)
			}
			if current.Key != second[occurrenceIndex].Key ||
				current.OriginalLocal != second[occurrenceIndex].OriginalLocal {
				t.Fatalf("case %d occurrence %d identity is not stable", index, occurrenceIndex)
			}
			if occurrenceIndex > 0 && !first[occurrenceIndex-1].StartsAt.Before(current.StartsAt) {
				t.Fatalf("case %d occurrences are not strictly increasing", index)
			}
		}
	}
}

func TestPlanConcurrentExpansion(t *testing.T) {
	t.Parallel()

	plan, err := Compile(Series{
		ID:            "series-concurrent",
		StartLocal:    "2026-01-01T09:00:00",
		TimeZone:      "America/New_York",
		Duration:      time.Hour,
		Rule:          "FREQ=DAILY;COUNT=100",
		OverlapPolicy: OverlapEarlier,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	window := Window{Start: plan.start, End: plan.start.AddDate(0, 0, 120)}

	var wait sync.WaitGroup
	errorsChannel := make(chan error, 16)
	for worker := 0; worker < 16; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			occurrences, expandErr := plan.Expand(context.Background(), window, ExpandOptions{})
			if expandErr != nil {
				errorsChannel <- expandErr
				return
			}
			if len(occurrences) != 100 {
				errorsChannel <- fmt.Errorf("expected 100 occurrences, got %d", len(occurrences))
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for expandErr := range errorsChannel {
		t.Error(expandErr)
	}
}

func mustParseRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse RFC3339 %q: %v", value, err)
	}
	return parsed
}
