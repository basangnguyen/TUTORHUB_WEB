package outboxworker

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

type Backoff struct {
	base       time.Duration
	maximum    time.Duration
	jitter     float64
	randomUnit func() float64
}

func NewBackoff(base time.Duration, maximum time.Duration, jitter float64) (Backoff, error) {
	return newBackoff(base, maximum, jitter, rand.Float64)
}

func newBackoff(
	base time.Duration,
	maximum time.Duration,
	jitter float64,
	randomUnit func() float64,
) (Backoff, error) {
	if base <= 0 || maximum <= 0 || base > maximum {
		return Backoff{}, fmt.Errorf("backoff durations are invalid")
	}
	if jitter < 0 || jitter >= 1 {
		return Backoff{}, fmt.Errorf("backoff jitter must be at least zero and less than one")
	}
	if randomUnit == nil {
		return Backoff{}, fmt.Errorf("backoff random source is required")
	}
	return Backoff{base: base, maximum: maximum, jitter: jitter, randomUnit: randomUnit}, nil
}

func (backoff Backoff) Delay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exponent := attempt - 1
	if exponent > 62 {
		exponent = 62
	}
	multiplier := math.Pow(2, float64(exponent))
	delay := time.Duration(math.Min(float64(backoff.maximum), float64(backoff.base)*multiplier))
	if backoff.jitter == 0 {
		return delay
	}
	unit := backoff.randomUnit()
	if unit < 0 {
		unit = 0
	} else if unit > 1 {
		unit = 1
	}
	factor := 1 - backoff.jitter + (2 * backoff.jitter * unit)
	jittered := time.Duration(float64(delay) * factor)
	if jittered <= 0 {
		return time.Millisecond
	}
	if jittered > backoff.maximum {
		return backoff.maximum
	}
	return jittered
}
