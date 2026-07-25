//go:build integration

package outboxworker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

const integrationEventAggregateType = "outbox_worker_integration"

func TestPostgresStoreConcurrentClaimsDoNotOverlap(t *testing.T) {
	ctx, pool, store := openLocalOutboxIntegrationStore(t)
	eventType := uniqueIntegrationEventType("claim")

	wantIDs := make(map[uuid.UUID]struct{}, 8)
	for range 8 {
		eventID := insertOutboxIntegrationEvent(t, ctx, pool, eventType)
		wantIDs[eventID] = struct{}{}
	}

	start := make(chan struct{})
	results := make(chan []Event, 2)
	errorsFound := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for range 2 {
		ownerID := uuid.New()
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			events, err := store.Claim(ctx, ClaimRequest{
				EventTypes:    []string{eventType},
				OwnerID:       ownerID,
				BatchSize:     4,
				LeaseDuration: 5 * time.Second,
				MaxAttempts:   3,
			})
			if err != nil {
				errorsFound <- err
				return
			}
			results <- events
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)
	close(errorsFound)

	for err := range errorsFound {
		t.Fatalf("claim outbox events concurrently: %v", err)
	}

	claimedIDs := make(map[uuid.UUID]struct{}, len(wantIDs))
	for events := range results {
		if len(events) != 4 {
			t.Fatalf("each bounded claimant should receive four rows, got %d", len(events))
		}
		for _, event := range events {
			if _, exists := claimedIDs[event.ID]; exists {
				t.Fatalf("event %s was claimed by more than one worker", event.ID)
			}
			if _, expected := wantIDs[event.ID]; !expected {
				t.Fatalf("worker claimed an unexpected event %s", event.ID)
			}
			claimedIDs[event.ID] = struct{}{}
		}
	}
	if len(claimedIDs) != len(wantIDs) {
		t.Fatalf("claimed %d unique rows, want %d", len(claimedIDs), len(wantIDs))
	}
}

func TestPostgresStoreReclaimsExpiredLeaseAndRejectsStaleAck(t *testing.T) {
	ctx, pool, store := openLocalOutboxIntegrationStore(t)
	eventType := uniqueIntegrationEventType("reclaim")
	eventID := insertOutboxIntegrationEvent(t, ctx, pool, eventType)

	first := claimOneIntegrationEvent(
		t,
		ctx,
		store,
		eventType,
		uuid.New(),
		125*time.Millisecond,
	)
	if first.ID != eventID || first.Lease.Token != 1 {
		t.Fatalf("unexpected initial lease: event=%s token=%d", first.ID, first.Lease.Token)
	}

	secondOwner := uuid.New()
	second := awaitIntegrationClaim(
		t,
		ctx,
		store,
		eventType,
		secondOwner,
		2*time.Second,
		3*time.Second,
	)
	if second.ID != eventID || second.Lease.Token != first.Lease.Token+1 {
		t.Fatalf(
			"unexpected reclaimed lease: event=%s token=%d initial_token=%d",
			second.ID,
			second.Lease.Token,
			first.Lease.Token,
		)
	}

	if err := store.Ack(ctx, first.Lease); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale worker ack error = %v, want ErrLeaseLost", err)
	}
	if err := store.Ack(ctx, second.Lease); err != nil {
		t.Fatalf("ack reclaimed lease: %v", err)
	}

	var published bool
	var leaseCleared bool
	if err := pool.QueryRow(ctx, `
SELECT published_at IS NOT NULL,
       lease_owner IS NULL AND leased_at IS NULL AND leased_until IS NULL
FROM tutorhub.outbox_events
WHERE id = $1`, eventID).Scan(&published, &leaseCleared); err != nil {
		t.Fatalf("read reclaimed event state: %v", err)
	}
	if !published || !leaseCleared {
		t.Fatalf("unexpected reclaimed event state: published=%t lease_cleared=%t", published, leaseCleared)
	}
}

func TestPostgresStoreRetryIncrementsFailureAttemptsAndDefersAvailability(t *testing.T) {
	ctx, pool, store := openLocalOutboxIntegrationStore(t)
	eventType := uniqueIntegrationEventType("retry")
	eventID := insertOutboxIntegrationEvent(t, ctx, pool, eventType)
	ownerID := uuid.New()
	first := claimOneIntegrationEvent(t, ctx, store, eventType, ownerID, 3*time.Second)

	if first.Attempts != 0 {
		t.Fatalf("claim must not increment failure attempts, got %d", first.Attempts)
	}
	if err := store.Retry(ctx, first.Lease, 2*time.Second, ErrorCodeHandlerFailed); err != nil {
		t.Fatalf("schedule retry: %v", err)
	}

	var attempts int
	var availableLater bool
	var leaseCleared bool
	if err := pool.QueryRow(ctx, `
SELECT attempts,
       available_at > clock_timestamp(),
       lease_owner IS NULL AND leased_at IS NULL AND leased_until IS NULL
FROM tutorhub.outbox_events
WHERE id = $1`, eventID).Scan(&attempts, &availableLater, &leaseCleared); err != nil {
		t.Fatalf("read retry state: %v", err)
	}
	if attempts != 1 || !availableLater || !leaseCleared {
		t.Fatalf(
			"unexpected retry state: attempts=%d available_later=%t lease_cleared=%t",
			attempts,
			availableLater,
			leaseCleared,
		)
	}

	tooEarly, err := store.Claim(ctx, integrationClaimRequest(eventType, ownerID, 2*time.Second))
	if err != nil {
		t.Fatalf("claim before retry availability: %v", err)
	}
	if len(tooEarly) != 0 {
		t.Fatalf("retry event was claimable before available_at: %+v", tooEarly)
	}

	second := awaitIntegrationClaim(
		t,
		ctx,
		store,
		eventType,
		ownerID,
		2*time.Second,
		5*time.Second,
	)
	if second.Attempts != 1 || second.Lease.Token != first.Lease.Token+1 {
		t.Fatalf(
			"unexpected retry claim: attempts=%d token=%d initial_token=%d",
			second.Attempts,
			second.Lease.Token,
			first.Lease.Token,
		)
	}
	if err := store.Ack(ctx, second.Lease); err != nil {
		t.Fatalf("ack retry delivery: %v", err)
	}
}

func TestPostgresStoreDeadLettersPoisonEvent(t *testing.T) {
	ctx, pool, store := openLocalOutboxIntegrationStore(t)
	eventType := uniqueIntegrationEventType("poison")
	eventID := insertOutboxIntegrationEvent(t, ctx, pool, eventType)
	ownerID := uuid.New()
	claimed := claimOneIntegrationEvent(t, ctx, store, eventType, ownerID, 3*time.Second)

	if err := store.DeadLetter(ctx, claimed.Lease, ErrorCodeInvalidPayload); err != nil {
		t.Fatalf("dead-letter poison event: %v", err)
	}

	var attempts int
	var deadLettered bool
	var errorCode string
	var leaseCleared bool
	if err := pool.QueryRow(ctx, `
SELECT attempts,
       dead_lettered_at IS NOT NULL,
       last_error,
       lease_owner IS NULL AND leased_at IS NULL AND leased_until IS NULL
FROM tutorhub.outbox_events
WHERE id = $1`, eventID).Scan(
		&attempts,
		&deadLettered,
		&errorCode,
		&leaseCleared,
	); err != nil {
		t.Fatalf("read dead-letter state: %v", err)
	}
	if attempts != 1 || !deadLettered || errorCode != ErrorCodeInvalidPayload || !leaseCleared {
		t.Fatalf(
			"unexpected dead-letter state: attempts=%d dead=%t code=%q lease_cleared=%t",
			attempts,
			deadLettered,
			errorCode,
			leaseCleared,
		)
	}

	afterDeadLetter, err := store.Claim(
		ctx,
		integrationClaimRequest(eventType, ownerID, 2*time.Second),
	)
	if err != nil {
		t.Fatalf("claim after dead-letter: %v", err)
	}
	if len(afterDeadLetter) != 0 {
		t.Fatalf("dead-lettered event was delivered again: %+v", afterDeadLetter)
	}
}

func TestPostgresRunnerDuplicateDeliveryUsesIdempotentEventIDSink(t *testing.T) {
	ctx, pool, store := openLocalOutboxIntegrationStore(t)
	eventType := uniqueIntegrationEventType("runner_duplicate")
	eventID := insertOutboxIntegrationEvent(t, ctx, pool, eventType)
	sinkTable := createOutboxIntegrationSinkTable(t, ctx, pool)

	var handlerCalls atomic.Int32
	firstEffectCommitted := make(chan struct{})
	releaseFirstHandler := make(chan struct{})
	firstHandlerExited := make(chan struct{})
	handler := func(handlerCtx context.Context, event Event, _ integrationRunnerPayload) error {
		call := handlerCalls.Add(1)
		if _, err := pool.Exec(
			handlerCtx,
			fmt.Sprintf(`
INSERT INTO tutorhub.%s (source_event_id)
VALUES ($1)
ON CONFLICT (source_event_id) DO NOTHING`, sinkTable),
			event.ID,
		); err != nil {
			return Retryable("integration_sink_failed")
		}
		if call == 1 {
			close(firstEffectCommitted)
			<-releaseFirstHandler
			close(firstHandlerExited)
		}
		return nil
	}

	firstRunner := newPostgresIntegrationRunner(t, store, eventType, handler)
	firstRunner.config.LeaseDuration = 500 * time.Millisecond
	firstRunner.config.HandlerTimeout = 125 * time.Millisecond
	firstRunner.config.ShutdownTimeout = 150 * time.Millisecond
	firstCtx, cancelFirst := context.WithCancel(ctx)
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- firstRunner.Run(firstCtx)
	}()
	awaitIntegrationSignal(t, firstEffectCommitted, 3*time.Second, "first sink effect")
	cancelFirst()
	awaitIntegrationRunnerExit(t, firstDone, 3*time.Second)
	close(releaseFirstHandler)
	awaitIntegrationSignal(t, firstHandlerExited, time.Second, "first handler exit")

	secondRunner := newPostgresIntegrationRunner(t, store, eventType, handler)
	secondRunner.config.LeaseDuration = 500 * time.Millisecond
	secondCtx, cancelSecond := context.WithCancel(ctx)
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- secondRunner.Run(secondCtx)
	}()
	awaitIntegrationEventState(t, ctx, pool, eventID, 5*time.Second, func(
		published bool,
		deadLettered bool,
		attempts int,
		leaseToken int64,
	) bool {
		return published && !deadLettered && attempts == 0 && leaseToken >= 2
	})
	cancelSecond()
	awaitIntegrationRunnerExit(t, secondDone, 3*time.Second)

	var sinkRows int
	if err := pool.QueryRow(
		ctx,
		fmt.Sprintf(
			`SELECT count(*) FROM tutorhub.%s WHERE source_event_id = $1`,
			sinkTable,
		),
		eventID,
	).Scan(&sinkRows); err != nil {
		t.Fatalf("count idempotent sink effects: %v", err)
	}
	if sinkRows != 1 {
		t.Fatalf("idempotent sink rows = %d, want 1", sinkRows)
	}
	if calls := handlerCalls.Load(); calls != 2 {
		t.Fatalf("handler calls = %d, want duplicate delivery count 2", calls)
	}
}

func TestPostgresRunnerRetryablePoisonReachesMaxAttemptsAndDeadLetter(t *testing.T) {
	ctx, pool, store := openLocalOutboxIntegrationStore(t)
	eventType := uniqueIntegrationEventType("runner_poison")
	eventID := insertOutboxIntegrationEvent(t, ctx, pool, eventType)

	var handlerCalls atomic.Int32
	runner := newPostgresIntegrationRunner(
		t,
		store,
		eventType,
		func(context.Context, Event, integrationRunnerPayload) error {
			handlerCalls.Add(1)
			return Retryable("integration_poison")
		},
	)
	runner.config.MaxAttempts = 3
	runner.config.PollInterval = 5 * time.Millisecond
	runner.config.Backoff = mustIntegrationBackoff(t, 10*time.Millisecond)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(runCtx)
	}()
	awaitIntegrationEventState(t, ctx, pool, eventID, 5*time.Second, func(
		published bool,
		deadLettered bool,
		attempts int,
		leaseToken int64,
	) bool {
		return !published && deadLettered && attempts == 3 && leaseToken == 3
	})
	cancel()
	awaitIntegrationRunnerExit(t, done, 3*time.Second)

	var errorCode string
	var leaseCleared bool
	if err := pool.QueryRow(ctx, `
SELECT last_error,
       lease_owner IS NULL AND leased_at IS NULL AND leased_until IS NULL
FROM tutorhub.outbox_events
WHERE id = $1`, eventID).Scan(&errorCode, &leaseCleared); err != nil {
		t.Fatalf("read runner poison state: %v", err)
	}
	if errorCode != ErrorCodeAttemptsExhausted || !leaseCleared {
		t.Fatalf(
			"unexpected runner poison state: code=%q lease_cleared=%t",
			errorCode,
			leaseCleared,
		)
	}
	if calls := handlerCalls.Load(); calls != 3 {
		t.Fatalf("poison handler calls = %d, want 3", calls)
	}
}

func TestPostgresRunnerShutdownLeavesUnfinishedEventReclaimable(t *testing.T) {
	ctx, pool, store := openLocalOutboxIntegrationStore(t)
	eventType := uniqueIntegrationEventType("runner_shutdown")
	eventID := insertOutboxIntegrationEvent(t, ctx, pool, eventType)

	handlerStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	handlerExited := make(chan struct{})
	runner := newPostgresIntegrationRunner(
		t,
		store,
		eventType,
		func(context.Context, Event, integrationRunnerPayload) error {
			close(handlerStarted)
			<-releaseHandler
			close(handlerExited)
			return nil
		},
	)
	runner.config.LeaseDuration = 500 * time.Millisecond
	runner.config.HandlerTimeout = 125 * time.Millisecond
	runner.config.ShutdownTimeout = 150 * time.Millisecond

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(runCtx)
	}()
	awaitIntegrationSignal(t, handlerStarted, 3*time.Second, "blocked handler start")
	cancel()
	awaitIntegrationRunnerExit(t, done, 3*time.Second)
	close(releaseHandler)
	awaitIntegrationSignal(t, handlerExited, time.Second, "blocked handler exit")

	var published bool
	var deadLettered bool
	var attempts int
	if err := pool.QueryRow(ctx, `
SELECT published_at IS NOT NULL,
       dead_lettered_at IS NOT NULL,
       attempts
FROM tutorhub.outbox_events
WHERE id = $1`, eventID).Scan(&published, &deadLettered, &attempts); err != nil {
		t.Fatalf("read shutdown-abandoned event: %v", err)
	}
	if published || deadLettered || attempts != 0 {
		t.Fatalf(
			"shutdown completed unfinished event: published=%t dead=%t attempts=%d",
			published,
			deadLettered,
			attempts,
		)
	}

	reclaimed := awaitIntegrationClaim(
		t,
		ctx,
		store,
		eventType,
		uuid.New(),
		2*time.Second,
		5*time.Second,
	)
	if reclaimed.ID != eventID || reclaimed.Lease.Token != 2 || reclaimed.Attempts != 0 {
		t.Fatalf(
			"unexpected reclaimed event: id=%s token=%d attempts=%d",
			reclaimed.ID,
			reclaimed.Lease.Token,
			reclaimed.Attempts,
		)
	}
	if err := store.Ack(ctx, reclaimed.Lease); err != nil {
		t.Fatalf("ack shutdown-reclaimed event: %v", err)
	}
}

func openLocalOutboxIntegrationStore(
	t *testing.T,
) (context.Context, *pgxpool.Pool, *PostgresStore) {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_MIGRATION_URL is not configured")
	}
	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse DATABASE_MIGRATION_URL: %v", err)
	}
	if !isLoopbackDatabaseHost(parsedURL.Hostname()) {
		t.Fatalf(
			"outbox integration tests refuse non-loopback DATABASE_MIGRATION_URL host %q",
			parsedURL.Hostname(),
		)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	if err := migrationrunner.Up(ctx, databaseURL); err != nil {
		t.Fatalf("apply local database migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open local integration pool: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping local integration database: %v", err)
	}

	store, err := NewPostgresStore(pool, 3*time.Second)
	if err != nil {
		t.Fatalf("create PostgreSQL outbox store: %v", err)
	}
	return ctx, pool, store
}

func isLoopbackDatabaseHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func uniqueIntegrationEventType(scenario string) string {
	return fmt.Sprintf(
		"p3.integration.%s_%s.v1",
		scenario,
		strings.ReplaceAll(uuid.NewString(), "-", ""),
	)
}

func insertOutboxIntegrationEvent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	eventType string,
) uuid.UUID {
	t.Helper()

	var eventID uuid.UUID
	if err := pool.QueryRow(ctx, `
INSERT INTO tutorhub.outbox_events (
    aggregate_type,
    aggregate_id,
    event_type,
    payload
)
VALUES ($1, $2, $3, '{}'::jsonb)
RETURNING id`, integrationEventAggregateType, uuid.New(), eventType).Scan(&eventID); err != nil {
		t.Fatalf("insert outbox integration event: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(
			cleanupCtx,
			`DELETE FROM tutorhub.outbox_events WHERE id = $1`,
			eventID,
		); err != nil {
			t.Errorf("delete outbox integration event %s: %v", eventID, err)
		}
	})
	return eventID
}

type integrationRunnerPayload struct{}

func newPostgresIntegrationRunner(
	t *testing.T,
	store Store,
	eventTypeValue string,
	handler func(context.Context, Event, integrationRunnerPayload) error,
) *Runner {
	t.Helper()

	eventType, err := ParseEventType(eventTypeValue)
	if err != nil {
		t.Fatalf("parse integration runner event type: %v", err)
	}
	registry := NewRegistry()
	if err := RegisterJSON(registry, HandlerSpec[integrationRunnerPayload]{
		EventType:     eventType,
		HandlerName:   "integration_handler",
		AggregateType: integrationEventAggregateType,
		TenantMode:    TenantOptional,
		Handle:        handler,
	}); err != nil {
		t.Fatalf("register integration runner handler: %v", err)
	}
	runner, err := NewRunner(RunnerConfig{
		OwnerID:         uuid.New(),
		BatchSize:       1,
		Concurrency:     1,
		PollInterval:    10 * time.Millisecond,
		MetricsInterval: time.Hour,
		LeaseDuration:   500 * time.Millisecond,
		HandlerTimeout:  125 * time.Millisecond,
		ShutdownTimeout: 150 * time.Millisecond,
		MaxAttempts:     3,
		Backoff:         mustIntegrationBackoff(t, 10*time.Millisecond),
		Store:           store,
		Registry:        registry,
		Metrics:         NewMetrics(registry.HandlerNames()),
	})
	if err != nil {
		t.Fatalf("create PostgreSQL integration runner: %v", err)
	}
	return runner
}

func mustIntegrationBackoff(t *testing.T, delay time.Duration) Backoff {
	t.Helper()
	backoff, err := newBackoff(delay, delay, 0, func() float64 { return 0.5 })
	if err != nil {
		t.Fatalf("create integration backoff: %v", err)
	}
	return backoff
}

func createOutboxIntegrationSinkTable(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) string {
	t.Helper()

	tableName := "outbox_worker_sink_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := pool.Exec(
		ctx,
		fmt.Sprintf(`
CREATE TABLE tutorhub.%s (
    source_event_id uuid PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
)`, tableName),
	); err != nil {
		t.Fatalf("create outbox integration sink table: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := pool.Exec(
			cleanupCtx,
			fmt.Sprintf(`DROP TABLE IF EXISTS tutorhub.%s`, tableName),
		); err != nil {
			t.Errorf("drop outbox integration sink table %s: %v", tableName, err)
		}
	})
	return tableName
}

func awaitIntegrationSignal(
	t *testing.T,
	signal <-chan struct{},
	timeout time.Duration,
	description string,
) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func awaitIntegrationRunnerExit(t *testing.T, done <-chan error, timeout time.Duration) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("outbox integration runner exited with error: %v", err)
		}
	case <-time.After(timeout):
		t.Fatal("outbox integration runner did not exit before timeout")
	}
}

func awaitIntegrationEventState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	eventID uuid.UUID,
	timeout time.Duration,
	matches func(bool, bool, int, int64) bool,
) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var published bool
		var deadLettered bool
		var attempts int
		var leaseToken int64
		if err := pool.QueryRow(ctx, `
SELECT published_at IS NOT NULL,
       dead_lettered_at IS NOT NULL,
       attempts,
       lease_token
FROM tutorhub.outbox_events
WHERE id = $1`, eventID).Scan(
			&published,
			&deadLettered,
			&attempts,
			&leaseToken,
		); err != nil {
			t.Fatalf("poll outbox integration event state: %v", err)
		}
		if matches(published, deadLettered, attempts, leaseToken) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("outbox event %s did not reach expected state within %s", eventID, timeout)
}

func integrationClaimRequest(
	eventType string,
	ownerID uuid.UUID,
	leaseDuration time.Duration,
) ClaimRequest {
	return ClaimRequest{
		EventTypes:    []string{eventType},
		OwnerID:       ownerID,
		BatchSize:     1,
		LeaseDuration: leaseDuration,
		MaxAttempts:   3,
	}
}

func claimOneIntegrationEvent(
	t *testing.T,
	ctx context.Context,
	store *PostgresStore,
	eventType string,
	ownerID uuid.UUID,
	leaseDuration time.Duration,
) Event {
	t.Helper()

	events, err := store.Claim(
		ctx,
		integrationClaimRequest(eventType, ownerID, leaseDuration),
	)
	if err != nil {
		t.Fatalf("claim outbox integration event: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("claimed %d outbox integration events, want 1", len(events))
	}
	return events[0]
}

func awaitIntegrationClaim(
	t *testing.T,
	ctx context.Context,
	store *PostgresStore,
	eventType string,
	ownerID uuid.UUID,
	leaseDuration time.Duration,
	timeout time.Duration,
) Event {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		events, err := store.Claim(
			ctx,
			integrationClaimRequest(eventType, ownerID, leaseDuration),
		)
		if err != nil {
			t.Fatalf("poll outbox integration claim: %v", err)
		}
		if len(events) == 1 {
			return events[0]
		}
		if len(events) > 1 {
			t.Fatalf("polled %d outbox integration events, want at most 1", len(events))
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("event type %s was not claimable within %s", eventType, timeout)
	return Event{}
}
