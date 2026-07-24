package calendarrecurrence

import (
	"context"
	"testing"
	"time"
)

func BenchmarkExpand500Occurrences(b *testing.B) {
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
				ID:            "benchmark-500",
				StartLocal:    "2026-01-01T12:00:00",
				TimeZone:      benchmark.zone,
				Duration:      time.Hour,
				Rule:          "FREQ=DAILY;COUNT=500",
				OverlapPolicy: benchmark.policy,
			})
			if err != nil {
				b.Fatalf("compile: %v", err)
			}
			window := Window{
				Start: plan.start,
				End:   plan.start.AddDate(0, 0, 501),
			}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				occurrences, expandErr := plan.Expand(context.Background(), window, ExpandOptions{})
				if expandErr != nil {
					b.Fatalf("expand: %v", expandErr)
				}
				if len(occurrences) != 500 {
					b.Fatalf("expected 500 occurrences, got %d", len(occurrences))
				}
			}
		})
	}
}
