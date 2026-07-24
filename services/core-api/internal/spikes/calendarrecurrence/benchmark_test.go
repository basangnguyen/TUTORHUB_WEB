package calendarrecurrence

import (
	"context"
	"testing"
	"time"
)

func BenchmarkExpandMaxQueryWindow(b *testing.B) {
	benchmarks := []struct {
		name   string
		zone   string
		policy OverlapPolicy
	}{
		{name: "UTC", zone: "UTC", policy: OverlapReject},
		{name: "HoChiMinh", zone: "Asia/Ho_Chi_Minh", policy: OverlapReject},
		{name: "NewYork", zone: "America/New_York", policy: OverlapEarlier},
	}
	for _, benchmark := range benchmarks {
		benchmark := benchmark
		b.Run(benchmark.name, func(b *testing.B) {
			plan, err := Compile(Series{
				ID:            "benchmark-max-query-window",
				StartLocal:    "2026-01-01T12:00:00",
				TimeZone:      benchmark.zone,
				Duration:      time.Hour,
				Rule:          "FREQ=DAILY;COUNT=366",
				OverlapPolicy: benchmark.policy,
			})
			if err != nil {
				b.Fatalf("compile: %v", err)
			}
			window := Window{
				Start: plan.start,
				End:   plan.start.AddDate(0, 0, MaxQueryWindowDays),
			}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				occurrences, expandErr := plan.Expand(context.Background(), window, ExpandOptions{})
				if expandErr != nil {
					b.Fatalf("expand: %v", expandErr)
				}
				if len(occurrences) != 366 {
					b.Fatalf("expected 366 occurrences, got %d", len(occurrences))
				}
			}
		})
	}
}
