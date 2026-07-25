package outboxworker

import (
	"sort"
	"sync"
	"time"
)

type Outcome string

const (
	OutcomeClaimed    Outcome = "claimed"
	OutcomeReclaimed  Outcome = "reclaimed"
	OutcomeSuccess    Outcome = "success"
	OutcomeRetry      Outcome = "retry"
	OutcomeDeadLetter Outcome = "dead_letter"
	OutcomeLeaseLost  Outcome = "lease_lost"
	OutcomeStoreError Outcome = "store_error"
	OutcomeAbandoned  Outcome = "abandoned"
)

var validOutcomes = map[Outcome]struct{}{
	OutcomeClaimed: {}, OutcomeReclaimed: {}, OutcomeSuccess: {}, OutcomeRetry: {}, OutcomeDeadLetter: {},
	OutcomeLeaseLost: {}, OutcomeStoreError: {}, OutcomeAbandoned: {},
}

type MetricPoint struct {
	EventType string
	Handler   string
	Outcome   Outcome
	Count     int64
	Duration  time.Duration
}

type Metrics struct {
	mu       sync.Mutex
	handlers map[string]string
	points   map[metricKey]metricValue
	backlog  BacklogStats
}

func (metrics *Metrics) SetBacklog(stats BacklogStats) {
	if metrics == nil {
		return
	}
	metrics.mu.Lock()
	metrics.backlog = stats
	metrics.mu.Unlock()
}

func (metrics *Metrics) BacklogSnapshot() BacklogStats {
	if metrics == nil {
		return BacklogStats{}
	}
	metrics.mu.Lock()
	stats := metrics.backlog
	metrics.mu.Unlock()
	return stats
}

type metricKey struct {
	eventType string
	handler   string
	outcome   Outcome
}

type metricValue struct {
	count    int64
	duration time.Duration
}

func NewMetrics(handlerNames map[string]string) *Metrics {
	boundedHandlers := make(map[string]string, len(handlerNames))
	for eventType, handler := range handlerNames {
		boundedHandlers[eventType] = handler
	}
	return &Metrics{
		handlers: boundedHandlers,
		points:   make(map[metricKey]metricValue),
	}
}

func (metrics *Metrics) Observe(
	eventType string,
	handler string,
	outcome Outcome,
	duration time.Duration,
) {
	if metrics == nil {
		return
	}
	expectedHandler, registered := metrics.handlers[eventType]
	_, validOutcome := validOutcomes[outcome]
	if !registered || expectedHandler != handler || !validOutcome {
		return
	}
	key := metricKey{eventType: eventType, handler: handler, outcome: outcome}
	metrics.mu.Lock()
	value := metrics.points[key]
	value.count++
	value.duration += duration
	metrics.points[key] = value
	metrics.mu.Unlock()
}

func (metrics *Metrics) Snapshot() []MetricPoint {
	if metrics == nil {
		return nil
	}
	metrics.mu.Lock()
	points := make([]MetricPoint, 0, len(metrics.points))
	for key, value := range metrics.points {
		points = append(points, MetricPoint{
			EventType: key.eventType,
			Handler:   key.handler,
			Outcome:   key.outcome,
			Count:     value.count,
			Duration:  value.duration,
		})
	}
	metrics.mu.Unlock()
	sort.Slice(points, func(left int, right int) bool {
		if points[left].EventType != points[right].EventType {
			return points[left].EventType < points[right].EventType
		}
		if points[left].Handler != points[right].Handler {
			return points[left].Handler < points[right].Handler
		}
		return points[left].Outcome < points[right].Outcome
	})
	return points
}
