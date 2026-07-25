package outboxworker

import (
	"testing"
	"time"
)

func TestBackoffIsExponentialCappedAndJittered(t *testing.T) {
	t.Parallel()

	withoutJitter, err := newBackoff(time.Second, 8*time.Second, 0, func() float64 { return 0 })
	if err != nil {
		t.Fatalf("create backoff: %v", err)
	}
	for attempt, expected := range []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second,
	} {
		if got := withoutJitter.Delay(attempt + 1); got != expected {
			t.Fatalf("attempt %d: expected %s, got %s", attempt+1, expected, got)
		}
	}

	low, _ := newBackoff(10*time.Second, time.Minute, 0.2, func() float64 { return 0 })
	high, _ := newBackoff(10*time.Second, time.Minute, 0.2, func() float64 { return 1 })
	if got := low.Delay(1); got != 8*time.Second {
		t.Fatalf("unexpected low jitter %s", got)
	}
	if got := high.Delay(1); got != 12*time.Second {
		t.Fatalf("unexpected high jitter %s", got)
	}
	if got := high.Delay(10); got != time.Minute {
		t.Fatalf("jitter must remain capped, got %s", got)
	}
}

func TestBackoffRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		base    time.Duration
		maximum time.Duration
		jitter  float64
	}{
		{0, time.Second, 0},
		{2 * time.Second, time.Second, 0},
		{time.Second, time.Second, -0.1},
		{time.Second, time.Second, 1},
		{time.Second, time.Second, 1.1},
	} {
		if _, err := newBackoff(testCase.base, testCase.maximum, testCase.jitter, func() float64 { return 0 }); err == nil {
			t.Fatalf("expected invalid backoff rejection: %+v", testCase)
		}
	}
}
