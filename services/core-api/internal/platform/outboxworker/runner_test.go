package outboxworker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

type runnerPayload struct {
	Mode string `json:"mode"`
}

func TestRunnerCompletesSuccessRetryPermanentAndExhausted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mode           string
		attempts       int
		wantAck        int
		wantRetryCode  string
		wantDeadCode   string
		wantRetryDelay time.Duration
	}{
		{name: "success", mode: "success", wantAck: 1},
		{
			name: "retry", mode: "retry", wantRetryCode: "temporary_failure",
			wantRetryDelay: time.Second,
		},
		{name: "permanent", mode: "permanent", wantDeadCode: "recipient_suppressed"},
		{
			name: "exhausted", mode: "retry", attempts: 2,
			wantDeadCode: ErrorCodeAttemptsExhausted,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &recordingRunnerStore{}
			runner, eventType := newTestRunner(t, store, func(
				_ context.Context,
				_ Event,
				payload runnerPayload,
			) error {
				switch payload.Mode {
				case "success":
					return nil
				case "retry":
					return Retryable("temporary_failure")
				case "permanent":
					return Permanent("recipient_suppressed")
				default:
					return errors.New("unexpected mode")
				}
			})
			event := newRunnerEvent(eventType, test.mode)
			event.Attempts = test.attempts
			runner.processEvent(context.Background(), event)

			store.mu.Lock()
			defer store.mu.Unlock()
			if len(store.acks) != test.wantAck {
				t.Fatalf("ack count = %d, want %d", len(store.acks), test.wantAck)
			}
			if test.wantRetryCode == "" {
				if len(store.retries) != 0 {
					t.Fatalf("unexpected retry completions: %+v", store.retries)
				}
			} else if len(store.retries) != 1 ||
				store.retries[0].code != test.wantRetryCode ||
				store.retries[0].delay != test.wantRetryDelay {
				t.Fatalf("unexpected retry completion: %+v", store.retries)
			}
			if test.wantDeadCode == "" {
				if len(store.deadLetters) != 0 {
					t.Fatalf("unexpected dead-letter completions: %+v", store.deadLetters)
				}
			} else if len(store.deadLetters) != 1 ||
				store.deadLetters[0].code != test.wantDeadCode {
				t.Fatalf("unexpected dead-letter completion: %+v", store.deadLetters)
			}
		})
	}
}

func TestRunnerMapsHandlerDeadlineAndPanicToSafeCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		handler  func(context.Context, Event, runnerPayload) error
		wantCode string
		secret   string
	}{
		{
			name: "deadline",
			handler: func(ctx context.Context, _ Event, _ runnerPayload) error {
				<-ctx.Done()
				return fmt.Errorf("provider credential secret-deadline: %w", ctx.Err())
			},
			wantCode: ErrorCodeHandlerTimeout,
			secret:   "secret-deadline",
		},
		{
			name: "panic",
			handler: func(context.Context, Event, runnerPayload) error {
				panic("secret-panic")
			},
			wantCode: ErrorCodeHandlerPanicked,
			secret:   "secret-panic",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			store := &recordingRunnerStore{}
			var logs bytes.Buffer
			runner, eventType := newTestRunner(t, store, test.handler)
			runner.config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
			runner.config.HandlerTimeout = 10 * time.Millisecond
			runner.processEvent(context.Background(), newRunnerEvent(eventType, "run"))

			store.mu.Lock()
			if len(store.retries) != 1 || store.retries[0].code != test.wantCode {
				store.mu.Unlock()
				t.Fatalf("unexpected retry completion: %+v", store.retries)
			}
			store.mu.Unlock()
			if strings.Contains(logs.String(), test.secret) {
				t.Fatalf("logs exposed handler detail: %s", logs.String())
			}
		})
	}
}

func TestRunnerUsesFencedLeaseAndRedactsStoreErrors(t *testing.T) {
	t.Parallel()

	store := &recordingRunnerStore{ackError: errors.New("database secret-value")}
	var logs bytes.Buffer
	runner, eventType := newTestRunner(
		t,
		store,
		func(context.Context, Event, runnerPayload) error { return nil },
	)
	runner.config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	event := newRunnerEvent(eventType, "success")
	runner.processEvent(context.Background(), event)

	if strings.Contains(logs.String(), "secret-value") {
		t.Fatalf("logs exposed database error detail: %s", logs.String())
	}
	points := runner.config.Metrics.Snapshot()
	if metricCount(points, OutcomeStoreError) != 1 {
		t.Fatalf("expected one bounded store-error metric, got %+v", points)
	}
}

func TestRunnerCountsClaimAndReclaimWithBoundedLabels(t *testing.T) {
	t.Parallel()

	store := &recordingRunnerStore{}
	runner, eventType := newTestRunner(
		t,
		store,
		func(context.Context, Event, runnerPayload) error { return nil },
	)
	first := newRunnerEvent(eventType, "success")
	second := newRunnerEvent(eventType, "success")
	second.Lease.Token = 2
	second.Reclaimed = true
	retried := newRunnerEvent(eventType, "success")
	retried.Lease.Token = 3
	runner.observeClaims([]Event{first, second, retried})
	runner.config.Metrics.Observe("attacker.dynamic.v1", "dynamic", OutcomeSuccess, time.Hour)

	points := runner.config.Metrics.Snapshot()
	if metricCount(points, OutcomeClaimed) != 3 || metricCount(points, OutcomeReclaimed) != 1 {
		t.Fatalf("unexpected claim metrics: %+v", points)
	}
	if len(points) != 2 {
		t.Fatalf("unregistered metric labels must be dropped: %+v", points)
	}
}

func TestRunnerNeverClaimsMoreEventsThanItCanStartConcurrently(t *testing.T) {
	t.Parallel()

	store := &recordingRunnerStore{}
	runner, _ := newTestRunner(
		t,
		store,
		func(context.Context, Event, runnerPayload) error { return nil },
	)
	runner.config.BatchSize = 25
	runner.config.Concurrency = 4
	if got := runner.claimBatchSize(); got != 4 {
		t.Fatalf("claim batch size = %d, want concurrency bound 4", got)
	}
}

func TestRunnerDuplicateDeliveryCanDedupeBySourceEventID(t *testing.T) {
	t.Parallel()

	store := &recordingRunnerStore{}
	var sinkMu sync.Mutex
	effects := make(map[uuid.UUID]int)
	runner, eventType := newTestRunner(t, store, func(
		_ context.Context,
		event Event,
		_ runnerPayload,
	) error {
		sinkMu.Lock()
		defer sinkMu.Unlock()
		if _, exists := effects[event.ID]; !exists {
			effects[event.ID] = 1
		}
		return nil
	})
	event := newRunnerEvent(eventType, "success")
	runner.processEvent(context.Background(), event)
	event.Lease.Token++
	runner.processEvent(context.Background(), event)

	sinkMu.Lock()
	effectCount := len(effects)
	sinkMu.Unlock()
	store.mu.Lock()
	ackCount := len(store.acks)
	store.mu.Unlock()
	if effectCount != 1 || ackCount != 2 {
		t.Fatalf("duplicate delivery: effects=%d acknowledgements=%d", effectCount, ackCount)
	}
}

func TestRunnerShutdownDoesNotCompleteUnfinishedHandler(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	store := &recordingRunnerStore{}
	runner, eventType := newTestRunner(t, store, func(
		context.Context,
		Event,
		runnerPayload,
	) error {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return nil
	})
	event := newRunnerEvent(eventType, "blocked")
	store.claims = []Event{event}
	runner.config.HandlerTimeout = 60 * time.Millisecond
	runner.config.ShutdownTimeout = 80 * time.Millisecond
	runner.config.LeaseDuration = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			close(release)
			t.Fatalf("runner shutdown: %v", err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("runner did not honor shutdown deadline")
	}

	store.mu.Lock()
	completionCount := len(store.acks) + len(store.retries) + len(store.deadLetters)
	store.mu.Unlock()
	if completionCount != 0 {
		close(release)
		t.Fatalf("unfinished handler changed durable state %d times", completionCount)
	}
	close(release)
}

func TestRunnerRecordsBacklogHeartbeatWithoutSensitiveLabels(t *testing.T) {
	t.Parallel()

	want := BacklogStats{
		Pending: 9, Ready: 4, Leased: 2, DeadLettered: 1,
		OldestPendingAge: 2 * time.Minute, DueLag: 3 * time.Second,
	}
	store := &recordingRunnerStore{backlog: want}
	var logs bytes.Buffer
	runner, _ := newTestRunner(
		t,
		store,
		func(context.Context, Event, runnerPayload) error { return nil },
	)
	runner.config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	runner.reportMetrics(context.Background())

	if got := runner.config.Metrics.BacklogSnapshot(); got != want {
		t.Fatalf("backlog snapshot = %+v, want %+v", got, want)
	}
	for _, expected := range []string{`"pending":9`, `"ready":4`, `"due_lag_ms":3000`} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("heartbeat missing %s: %s", expected, logs.String())
		}
	}
}

func TestRunnerReportsExhaustedSweepCountWithoutEventPayload(t *testing.T) {
	t.Parallel()

	store := &recordingRunnerStore{sweepCount: 3}
	var logs bytes.Buffer
	runner, _ := newTestRunner(
		t,
		store,
		func(context.Context, Event, runnerPayload) error { return nil },
	)
	runner.config.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	runner.sweepExhausted(context.Background())

	for _, expected := range []string{
		`"msg":"outbox exhausted events dead-lettered"`,
		`"error_code":"attempts_exhausted"`,
		`"count":3`,
	} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("sweep log missing %s: %s", expected, logs.String())
		}
	}
}

func newTestRunner(
	t *testing.T,
	store Store,
	handler func(context.Context, Event, runnerPayload) error,
) (*Runner, EventType) {
	t.Helper()

	eventType := MustEventType("worker.runner", 1)
	registry := NewRegistry()
	if err := RegisterJSON(registry, HandlerSpec[runnerPayload]{
		EventType:     eventType,
		HandlerName:   "runner_handler",
		AggregateType: "worker_runner",
		TenantMode:    TenantOptional,
		Handle:        handler,
	}); err != nil {
		t.Fatalf("register runner handler: %v", err)
	}
	backoff, err := newBackoff(time.Second, time.Minute, 0, func() float64 { return 0.5 })
	if err != nil {
		t.Fatalf("create test backoff: %v", err)
	}
	runner, err := NewRunner(RunnerConfig{
		OwnerID:         uuid.New(),
		BatchSize:       4,
		Concurrency:     2,
		PollInterval:    5 * time.Millisecond,
		MetricsInterval: 10 * time.Millisecond,
		LeaseDuration:   time.Second,
		HandlerTimeout:  50 * time.Millisecond,
		ShutdownTimeout: 60 * time.Millisecond,
		MaxAttempts:     3,
		Backoff:         backoff,
		Store:           store,
		Registry:        registry,
		Metrics:         NewMetrics(registry.HandlerNames()),
	})
	if err != nil {
		t.Fatalf("create test runner: %v", err)
	}
	return runner, eventType
}

func newRunnerEvent(eventType EventType, mode string) Event {
	eventID := uuid.New()
	ownerID := uuid.New()
	return Event{
		ID:            eventID,
		AggregateType: "worker_runner",
		AggregateID:   uuid.New(),
		Type:          eventType,
		Payload:       []byte(fmt.Sprintf(`{"mode":%q}`, mode)),
		Lease: LeaseRef{
			EventID: eventID,
			OwnerID: ownerID,
			Token:   1,
		},
	}
}

func metricCount(points []MetricPoint, outcome Outcome) int64 {
	var count int64
	for _, point := range points {
		if point.Outcome == outcome {
			count += point.Count
		}
	}
	return count
}

type retryCompletion struct {
	lease LeaseRef
	delay time.Duration
	code  string
}

type deadLetterCompletion struct {
	lease LeaseRef
	code  string
}

type recordingRunnerStore struct {
	mu          sync.Mutex
	claims      []Event
	acks        []LeaseRef
	retries     []retryCompletion
	deadLetters []deadLetterCompletion
	backlog     BacklogStats
	ackError    error
	sweepCount  int64
}

func (store *recordingRunnerStore) Claim(
	ctx context.Context,
	_ ClaimRequest,
) ([]Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.claims) == 0 {
		return nil, nil
	}
	events := append([]Event(nil), store.claims...)
	store.claims = nil
	return events, nil
}

func (store *recordingRunnerStore) Ack(_ context.Context, lease LeaseRef) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.ackError != nil {
		return store.ackError
	}
	store.acks = append(store.acks, lease)
	return nil
}

func (store *recordingRunnerStore) Retry(
	_ context.Context,
	lease LeaseRef,
	delay time.Duration,
	code string,
) error {
	store.mu.Lock()
	store.retries = append(store.retries, retryCompletion{lease: lease, delay: delay, code: code})
	store.mu.Unlock()
	return nil
}

func (store *recordingRunnerStore) DeadLetter(
	_ context.Context,
	lease LeaseRef,
	code string,
) error {
	store.mu.Lock()
	store.deadLetters = append(
		store.deadLetters,
		deadLetterCompletion{lease: lease, code: code},
	)
	store.mu.Unlock()
	return nil
}

func (store *recordingRunnerStore) SweepExhausted(
	context.Context,
	[]string,
	int,
	int,
) (int64, error) {
	return store.sweepCount, nil
}

func (store *recordingRunnerStore) Backlog(
	context.Context,
	[]string,
	int,
) (BacklogStats, error) {
	store.mu.Lock()
	stats := store.backlog
	store.mu.Unlock()
	return stats, nil
}
