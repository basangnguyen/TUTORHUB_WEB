package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadWorkerDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := loadWorker(mapLookup(map[string]string{
		"DATABASE_WORKER_URL": "postgresql://worker:secret@localhost/tutorhub?sslmode=disable",
	}))
	if err != nil {
		t.Fatalf("load worker defaults: %v", err)
	}
	if cfg.Environment != "development" || cfg.LogLevel != "info" {
		t.Fatalf("unexpected worker identity config: %+v", cfg)
	}
	if cfg.Database.MaxConnections != defaultWorkerDBMaxConns || cfg.Database.MinConnections != 0 {
		t.Fatalf("unexpected worker database defaults: %+v", cfg.Database)
	}
	if cfg.BatchSize != defaultWorkerBatchSize || cfg.Concurrency != defaultWorkerConcurrency ||
		cfg.PollInterval != defaultWorkerPollInterval ||
		cfg.MetricsInterval != defaultWorkerMetricsInterval ||
		cfg.LeaseDuration != defaultWorkerLeaseDuration ||
		cfg.HandlerTimeout != defaultWorkerHandlerTimeout ||
		cfg.ShutdownTimeout != defaultWorkerShutdownTimeout ||
		cfg.MaxAttempts != defaultWorkerMaxAttempts ||
		cfg.RetryJitter != 0.2 {
		t.Fatalf("unexpected worker defaults: %+v", cfg)
	}
}

func TestLoadWorkerCustomValues(t *testing.T) {
	t.Parallel()

	cfg, err := loadWorker(mapLookup(map[string]string{
		"APP_ENV":                     "staging",
		"LOG_LEVEL":                   "debug",
		"DATABASE_WORKER_URL":         "postgresql://worker:secret@db.example/tutorhub?sslmode=require",
		"DATABASE_MAX_CONNECTIONS":    "3",
		"OUTBOX_BATCH_SIZE":           "50",
		"OUTBOX_CONCURRENCY":          "6",
		"OUTBOX_POLL_INTERVAL":        "3s",
		"OUTBOX_METRICS_INTERVAL":     "45s",
		"OUTBOX_LEASE_DURATION":       "45s",
		"OUTBOX_HANDLER_TIMEOUT":      "30s",
		"OUTBOX_SHUTDOWN_TIMEOUT":     "35s",
		"OUTBOX_MAX_ATTEMPTS":         "12",
		"OUTBOX_RETRY_BASE_DELAY":     "2s",
		"OUTBOX_RETRY_MAX_DELAY":      "30m",
		"OUTBOX_RETRY_JITTER_PERCENT": "35",
	}))
	if err != nil {
		t.Fatalf("load custom worker config: %v", err)
	}
	if cfg.BatchSize != 50 || cfg.Concurrency != 6 || cfg.Database.MaxConnections != 3 ||
		cfg.PollInterval != 3*time.Second || cfg.MetricsInterval != 45*time.Second ||
		cfg.LeaseDuration != 45*time.Second ||
		cfg.HandlerTimeout != 30*time.Second || cfg.ShutdownTimeout != 35*time.Second ||
		cfg.MaxAttempts != 12 || cfg.RetryBaseDelay != 2*time.Second ||
		cfg.RetryMaxDelay != 30*time.Minute || cfg.RetryJitter != 0.35 {
		t.Fatalf("unexpected custom worker config: %+v", cfg)
	}
}

func TestLoadWorkerRequiresDedicatedDatabaseURL(t *testing.T) {
	t.Parallel()

	_, err := loadWorker(mapLookup(map[string]string{
		"DATABASE_POOL_URL": "postgresql://api:secret@localhost/tutorhub?sslmode=disable",
	}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_WORKER_URL is required") {
		t.Fatalf("expected dedicated worker URL requirement, got %v", err)
	}
}

func TestLoadWorkerRequiresTLSWhenHosted(t *testing.T) {
	t.Parallel()

	_, err := loadWorker(mapLookup(map[string]string{
		"APP_ENV":             "production",
		"DATABASE_WORKER_URL": "postgresql://worker:secret@db.example/tutorhub?sslmode=disable",
	}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_WORKER_URL must require TLS") {
		t.Fatalf("expected hosted TLS validation error, got %v", err)
	}
}

func TestLoadWorkerRejectsOneHundredPercentRetryJitter(t *testing.T) {
	t.Parallel()

	_, err := loadWorker(mapLookup(map[string]string{
		"DATABASE_WORKER_URL":         "postgresql://worker:secret@localhost/tutorhub?sslmode=disable",
		"OUTBOX_RETRY_JITTER_PERCENT": "100",
	}))
	if err == nil || !strings.Contains(err.Error(), "OUTBOX_RETRY_JITTER_PERCENT") {
		t.Fatalf("expected retry jitter bound validation, got %v", err)
	}
}

func TestLoadWorkerRejectsUnsafeTimingRelationships(t *testing.T) {
	t.Parallel()

	_, err := loadWorker(mapLookup(map[string]string{
		"DATABASE_WORKER_URL":     "postgresql://worker:secret@localhost/tutorhub?sslmode=disable",
		"OUTBOX_LEASE_DURATION":   "10s",
		"OUTBOX_HANDLER_TIMEOUT":  "10s",
		"OUTBOX_SHUTDOWN_TIMEOUT": "5s",
		"OUTBOX_RETRY_BASE_DELAY": "2m",
		"OUTBOX_RETRY_MAX_DELAY":  "1m",
	}))
	if err == nil {
		t.Fatal("expected timing validation errors")
	}
	for _, expected := range []string{
		"OUTBOX_HANDLER_TIMEOUT must be shorter",
		"OUTBOX_LEASE_DURATION must cover OUTBOX_HANDLER_TIMEOUT, DATABASE_QUERY_TIMEOUT, and the worker lease safety margin",
		"OUTBOX_SHUTDOWN_TIMEOUT must not be shorter",
		"OUTBOX_SHUTDOWN_TIMEOUT must cover OUTBOX_HANDLER_TIMEOUT plus DATABASE_QUERY_TIMEOUT",
		"OUTBOX_RETRY_BASE_DELAY must not exceed",
	} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected error to contain %q, got %v", expected, err)
		}
	}
}

func TestLoadWorkerRequiresDatabaseCompletionWithinLease(t *testing.T) {
	t.Parallel()

	_, err := loadWorker(mapLookup(map[string]string{
		"DATABASE_WORKER_URL":     "postgresql://worker:secret@localhost/tutorhub?sslmode=disable",
		"DATABASE_QUERY_TIMEOUT":  "4s",
		"OUTBOX_LEASE_DURATION":   "10s",
		"OUTBOX_HANDLER_TIMEOUT":  "7s",
		"OUTBOX_SHUTDOWN_TIMEOUT": "15s",
	}))
	if err == nil || !strings.Contains(
		err.Error(),
		"OUTBOX_LEASE_DURATION must cover OUTBOX_HANDLER_TIMEOUT, DATABASE_QUERY_TIMEOUT, and the worker lease safety margin",
	) {
		t.Fatalf("expected lease completion budget validation, got %v", err)
	}
}

func TestLoadWorkerRequiresLeaseSafetyMargin(t *testing.T) {
	t.Parallel()

	_, err := loadWorker(mapLookup(map[string]string{
		"DATABASE_WORKER_URL":    "postgresql://worker:secret@localhost/tutorhub?sslmode=disable",
		"DATABASE_QUERY_TIMEOUT": "4s",
		"OUTBOX_LEASE_DURATION":  "14s",
		"OUTBOX_HANDLER_TIMEOUT": "6s",
	}))
	if err == nil || !strings.Contains(err.Error(), "worker lease safety margin") {
		t.Fatalf("expected explicit lease safety margin validation, got %v", err)
	}
}

func TestLoadWorkerRequiresShutdownToCoverDatabaseCompletion(t *testing.T) {
	t.Parallel()

	_, err := loadWorker(mapLookup(map[string]string{
		"DATABASE_WORKER_URL":     "postgresql://worker:secret@localhost/tutorhub?sslmode=disable",
		"DATABASE_QUERY_TIMEOUT":  "4s",
		"OUTBOX_LEASE_DURATION":   "20s",
		"OUTBOX_HANDLER_TIMEOUT":  "7s",
		"OUTBOX_SHUTDOWN_TIMEOUT": "9s",
	}))
	if err == nil || !strings.Contains(
		err.Error(),
		"OUTBOX_SHUTDOWN_TIMEOUT must cover OUTBOX_HANDLER_TIMEOUT plus DATABASE_QUERY_TIMEOUT",
	) {
		t.Fatalf("expected shutdown completion budget validation, got %v", err)
	}
}
