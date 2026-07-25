package outboxworker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
)

type RunnerConfig struct {
	OwnerID         uuid.UUID
	BatchSize       int
	Concurrency     int
	PollInterval    time.Duration
	MetricsInterval time.Duration
	LeaseDuration   time.Duration
	HandlerTimeout  time.Duration
	ShutdownTimeout time.Duration
	MaxAttempts     int
	Backoff         Backoff
	Store           Store
	Registry        *Registry
	Logger          *slog.Logger
	Metrics         *Metrics
}

type Runner struct {
	config    RunnerConfig
	allowlist []string
}

func NewRunner(config RunnerConfig) (*Runner, error) {
	if config.OwnerID == uuid.Nil || config.Store == nil || config.Registry == nil {
		return nil, fmt.Errorf("outbox runner dependencies are incomplete")
	}
	if config.BatchSize < 1 || config.BatchSize > 100 ||
		config.Concurrency < 1 || config.Concurrency > config.BatchSize ||
		config.MaxAttempts < 1 {
		return nil, fmt.Errorf("outbox runner bounds are invalid")
	}
	if config.PollInterval <= 0 || config.MetricsInterval <= 0 || config.LeaseDuration <= 0 ||
		config.HandlerTimeout <= 0 || config.HandlerTimeout >= config.LeaseDuration ||
		config.ShutdownTimeout < config.HandlerTimeout {
		return nil, fmt.Errorf("outbox runner timing is invalid")
	}
	if config.Backoff.base <= 0 || config.Backoff.maximum <= 0 {
		return nil, fmt.Errorf("outbox runner backoff is invalid")
	}
	if config.Logger == nil {
		config.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if config.Metrics == nil {
		config.Metrics = NewMetrics(config.Registry.HandlerNames())
	}
	return &Runner{config: config, allowlist: config.Registry.Allowlist()}, nil
}

func (runner *Runner) Run(ctx context.Context) error {
	runner.config.Logger.Info(
		"outbox worker started",
		"owner_id", runner.config.OwnerID,
		"registered_event_types", len(runner.allowlist),
	)
	if len(runner.allowlist) == 0 {
		runner.config.Logger.Warn("outbox worker has no registered handlers; no events will be claimed")
		runner.logMetrics(BacklogStats{})
		ticker := time.NewTicker(runner.config.MetricsInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				runner.logMetrics(BacklogStats{})
			}
		}
	}

	nextMetricsAt := time.Time{}
	for {
		if ctx.Err() != nil {
			return nil
		}
		runner.sweepExhausted(ctx)
		if now := time.Now(); !now.Before(nextMetricsAt) {
			runner.reportMetrics(ctx)
			nextMetricsAt = now.Add(runner.config.MetricsInterval)
		}

		events, err := runner.config.Store.Claim(ctx, ClaimRequest{
			EventTypes:    runner.allowlist,
			OwnerID:       runner.config.OwnerID,
			BatchSize:     runner.claimBatchSize(),
			LeaseDuration: runner.config.LeaseDuration,
			MaxAttempts:   runner.config.MaxAttempts,
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			runner.config.Logger.Warn("outbox claim failed", "error_code", "claim_failed")
			if !waitForNextPoll(ctx, runner.config.PollInterval) {
				return nil
			}
			continue
		}
		if len(events) == 0 {
			if !waitForNextPoll(ctx, runner.config.PollInterval) {
				return nil
			}
			continue
		}
		runner.observeClaims(events)

		processingContext, cancelProcessing := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			runner.processBatch(processingContext, events)
			close(done)
		}()
		select {
		case <-done:
			cancelProcessing()
		case <-ctx.Done():
			timer := time.NewTimer(runner.config.ShutdownTimeout)
			select {
			case <-done:
				if !timer.Stop() {
					<-timer.C
				}
				cancelProcessing()
				return nil
			case <-timer.C:
				cancelProcessing()
				runner.config.Logger.Warn(
					"outbox worker drain deadline reached",
					"error_code", "shutdown_deadline",
				)
				return nil
			}
		}
	}
}

func (runner *Runner) sweepExhausted(ctx context.Context) {
	count, err := runner.config.Store.SweepExhausted(
		ctx, runner.allowlist, runner.config.MaxAttempts, runner.config.BatchSize,
	)
	if err != nil {
		if ctx.Err() == nil {
			runner.config.Logger.Warn(
				"outbox exhausted sweep failed", "error_code", "sweep_failed",
			)
		}
		return
	}
	if count > 0 {
		runner.config.Logger.Warn(
			"outbox exhausted events dead-lettered",
			"error_code", ErrorCodeAttemptsExhausted,
			"count", count,
		)
	}
}

func (runner *Runner) claimBatchSize() int {
	if runner.config.Concurrency < runner.config.BatchSize {
		return runner.config.Concurrency
	}
	return runner.config.BatchSize
}

func (runner *Runner) reportMetrics(ctx context.Context) {
	stats, err := runner.config.Store.Backlog(
		ctx,
		runner.allowlist,
		runner.config.MaxAttempts,
	)
	if err != nil {
		if ctx.Err() == nil {
			runner.config.Logger.Warn(
				"outbox backlog inspection failed",
				"error_code", "backlog_inspection_failed",
			)
		}
		return
	}
	runner.config.Metrics.SetBacklog(stats)
	runner.logMetrics(stats)
}

func (runner *Runner) logMetrics(stats BacklogStats) {
	runner.config.Logger.Info(
		"outbox worker heartbeat",
		"pending", stats.Pending,
		"ready", stats.Ready,
		"leased", stats.Leased,
		"dead_lettered", stats.DeadLettered,
		"oldest_pending_age_ms", stats.OldestPendingAge.Milliseconds(),
		"due_lag_ms", stats.DueLag.Milliseconds(),
	)
	for _, point := range runner.config.Metrics.Snapshot() {
		runner.config.Logger.Info(
			"outbox worker metric",
			"event_type", point.EventType,
			"handler", point.Handler,
			"outcome", point.Outcome,
			"count", point.Count,
			"duration_ms", point.Duration.Milliseconds(),
		)
	}
}

func (runner *Runner) processBatch(ctx context.Context, events []Event) {
	semaphore := make(chan struct{}, runner.config.Concurrency)
	var waitGroup sync.WaitGroup
	for _, event := range events {
		event := event
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				runner.observe(event, OutcomeAbandoned, 0)
				return
			}
			runner.processEvent(ctx, event)
		}()
	}
	waitGroup.Wait()
}

func (runner *Runner) observeClaims(events []Event) {
	for _, event := range events {
		handler, ok := runner.config.Registry.Resolve(event.Type)
		if !ok {
			continue
		}
		runner.config.Metrics.Observe(
			event.Type.String(), handler.HandlerName, OutcomeClaimed, 0,
		)
		if event.Reclaimed {
			runner.config.Metrics.Observe(
				event.Type.String(), handler.HandlerName, OutcomeReclaimed, 0,
			)
		}
	}
}

func (runner *Runner) processEvent(ctx context.Context, event Event) {
	startedAt := time.Now()
	handler, ok := runner.config.Registry.Resolve(event.Type)
	if !ok {
		runner.finishDeadLetter(ctx, event, "", ErrorCodeInvalidEventContext, startedAt)
		return
	}
	handlerContext, cancel := context.WithTimeout(ctx, runner.config.HandlerTimeout)
	err := invokeHandler(handlerContext, handler, event)
	handlerContextError := handlerContext.Err()
	cancel()
	if ctx.Err() != nil {
		runner.observeWithHandler(event, handler.HandlerName, OutcomeAbandoned, time.Since(startedAt))
		return
	}
	if errors.Is(handlerContextError, context.DeadlineExceeded) {
		err = Retryable(ErrorCodeHandlerTimeout)
	}
	if err == nil {
		if err := runner.config.Store.Ack(ctx, event.Lease); err != nil {
			runner.observeStoreCompletion(event, handler.HandlerName, err, startedAt)
			return
		}
		runner.observeWithHandler(event, handler.HandlerName, OutcomeSuccess, time.Since(startedAt))
		return
	}

	errorCode, disposition := ClassifyFailure(err)
	failureAttempt := event.Attempts + 1
	if disposition == FailurePermanent {
		runner.finishDeadLetter(ctx, event, handler.HandlerName, errorCode, startedAt)
		return
	}
	if failureAttempt >= runner.config.MaxAttempts {
		runner.finishDeadLetter(
			ctx, event, handler.HandlerName, ErrorCodeAttemptsExhausted, startedAt,
		)
		return
	}
	if err := runner.config.Store.Retry(
		ctx, event.Lease, runner.config.Backoff.Delay(failureAttempt), errorCode,
	); err != nil {
		runner.observeStoreCompletion(event, handler.HandlerName, err, startedAt)
		return
	}
	runner.observeWithHandler(event, handler.HandlerName, OutcomeRetry, time.Since(startedAt))
	runner.config.Logger.Warn(
		"outbox handler scheduled for retry",
		"event_id", event.ID,
		"event_type", event.Type.String(),
		"handler", handler.HandlerName,
		"error_code", errorCode,
	)
}

func (runner *Runner) finishDeadLetter(
	ctx context.Context,
	event Event,
	handlerName string,
	errorCode string,
	startedAt time.Time,
) {
	if err := runner.config.Store.DeadLetter(ctx, event.Lease, errorCode); err != nil {
		runner.observeStoreCompletion(event, handlerName, err, startedAt)
		return
	}
	runner.observeWithHandler(event, handlerName, OutcomeDeadLetter, time.Since(startedAt))
	runner.config.Logger.Warn(
		"outbox event dead-lettered",
		"event_id", event.ID,
		"event_type", event.Type.String(),
		"handler", handlerName,
		"error_code", normalizedErrorCode(errorCode),
	)
}

func (runner *Runner) observeStoreCompletion(
	event Event,
	handlerName string,
	err error,
	startedAt time.Time,
) {
	outcome := OutcomeStoreError
	if errors.Is(err, ErrLeaseLost) {
		outcome = OutcomeLeaseLost
	}
	runner.observeWithHandler(event, handlerName, outcome, time.Since(startedAt))
	runner.config.Logger.Warn(
		"outbox lease completion failed",
		"event_id", event.ID,
		"event_type", event.Type.String(),
		"handler", handlerName,
		"error_code", string(outcome),
	)
}

func (runner *Runner) observe(event Event, outcome Outcome, duration time.Duration) {
	handler, ok := runner.config.Registry.Resolve(event.Type)
	if !ok {
		return
	}
	runner.observeWithHandler(event, handler.HandlerName, outcome, duration)
}

func (runner *Runner) observeWithHandler(
	event Event,
	handlerName string,
	outcome Outcome,
	duration time.Duration,
) {
	runner.config.Metrics.Observe(event.Type.String(), handlerName, outcome, duration)
}

func invokeHandler(ctx context.Context, handler RegisteredHandler, event Event) (err error) {
	defer func() {
		if recover() != nil {
			err = Retryable(ErrorCodeHandlerPanicked)
		}
	}()
	return handler.Handle(ctx, event)
}

func waitForNextPoll(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
