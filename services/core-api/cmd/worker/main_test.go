package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/platform/outboxworker"
)

func TestWorkerRegistryStartsWithAnExactEmptyAllowlist(t *testing.T) {
	t.Parallel()

	registry := newWorkerRegistry()
	if allowlist := registry.Allowlist(); len(allowlist) != 0 {
		t.Fatalf("P3-03 must not claim historical events, got allowlist %v", allowlist)
	}
	historicalType := outboxworker.MustEventType("class_session.scheduled", 1)
	if _, registered := registry.Resolve(historicalType); registered {
		t.Fatal("P3-03 must not register a consumer before its owning task")
	}
}

func TestWorkerRunnerStopsWithoutTouchingStoreWhenRegistryIsEmpty(t *testing.T) {
	t.Parallel()

	store := &countingStore{}
	runner, err := newWorkerRunner(
		validWorkerConfig(),
		store,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("initialize worker runner: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("run empty worker: %v", err)
	}
	if calls := store.calls.Load(); calls != 0 {
		t.Fatalf("empty allowlist must not access the outbox store, got %d calls", calls)
	}
}

func TestEmptyWorkerEmitsPeriodicHeartbeatWithoutTouchingStore(t *testing.T) {
	t.Parallel()

	store := &countingStore{}
	var logs bytes.Buffer
	cfg := validWorkerConfig()
	cfg.MetricsInterval = 5 * time.Millisecond
	runner, err := newWorkerRunner(
		cfg,
		store,
		slog.New(slog.NewTextHandler(&logs, nil)),
		uuid.New(),
	)
	if err != nil {
		t.Fatalf("initialize empty worker: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Millisecond)
	defer cancel()
	if err := runner.Run(ctx); err != nil {
		t.Fatalf("run empty worker: %v", err)
	}
	if got := bytes.Count(logs.Bytes(), []byte("outbox worker heartbeat")); got < 2 {
		t.Fatalf("empty worker heartbeat count = %d, want at least 2: %s", got, logs.String())
	}
	if calls := store.calls.Load(); calls != 0 {
		t.Fatalf("empty allowlist must not access the outbox store, got %d calls", calls)
	}
}

func TestDatabaseCapabilityErrorCodeDistinguishesACLFromProbeFailure(t *testing.T) {
	t.Parallel()

	if got := databaseCapabilityErrorCode(outboxworker.ErrUnsafeDatabaseCapabilities); got != "unsafe_database_capabilities" {
		t.Fatalf("unsafe ACL error code = %q", got)
	}
	if got := databaseCapabilityErrorCode(errors.New("transport detail")); got != "database_capability_probe_failed" {
		t.Fatalf("probe failure error code = %q", got)
	}
}

func TestWorkerUsesDedicatedDatabaseApplicationName(t *testing.T) {
	t.Parallel()

	if workerDatabaseApplicationName != "tutorhub-outbox-worker" {
		t.Fatalf("unexpected worker database application name %q", workerDatabaseApplicationName)
	}
}

func validWorkerConfig() config.WorkerConfig {
	return config.WorkerConfig{
		BatchSize:       25,
		Concurrency:     4,
		PollInterval:    time.Second,
		MetricsInterval: 30 * time.Second,
		LeaseDuration:   30 * time.Second,
		HandlerTimeout:  20 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		MaxAttempts:     8,
		RetryBaseDelay:  time.Second,
		RetryMaxDelay:   time.Minute,
		RetryJitter:     0.2,
	}
}

type countingStore struct {
	calls atomic.Int64
}

func (store *countingStore) Claim(
	context.Context,
	outboxworker.ClaimRequest,
) ([]outboxworker.Event, error) {
	store.calls.Add(1)
	return nil, nil
}

func (store *countingStore) Ack(context.Context, outboxworker.LeaseRef) error {
	store.calls.Add(1)
	return nil
}

func (store *countingStore) Retry(
	context.Context,
	outboxworker.LeaseRef,
	time.Duration,
	string,
) error {
	store.calls.Add(1)
	return nil
}

func (store *countingStore) DeadLetter(
	context.Context,
	outboxworker.LeaseRef,
	string,
) error {
	store.calls.Add(1)
	return nil
}

func (store *countingStore) SweepExhausted(
	context.Context,
	[]string,
	int,
	int,
) (int64, error) {
	store.calls.Add(1)
	return 0, nil
}

func (store *countingStore) Backlog(
	context.Context,
	[]string,
	int,
) (outboxworker.BacklogStats, error) {
	store.calls.Add(1)
	return outboxworker.BacklogStats{}, nil
}
