package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/platform/database"
	"github.com/tutorhub-v2/core-api/internal/platform/observability"
	"github.com/tutorhub-v2/core-api/internal/platform/outboxworker"
)

const workerDatabaseApplicationName = "tutorhub-outbox-worker"

func main() {
	os.Exit(run())
}

func run() int {
	bootstrapLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.LoadWorker()
	if err != nil {
		bootstrapLogger.Error(
			"invalid outbox worker configuration",
			"error_code", "invalid_configuration",
		)
		return 1
	}

	logger, err := observability.NewLogger(os.Stdout, cfg.LogLevel)
	if err != nil {
		bootstrapLogger.Error(
			"create outbox worker logger",
			"error_code", "logger_initialization_failed",
		)
		return 1
	}
	logger = logger.With(
		"service", workerDatabaseApplicationName,
		"environment", cfg.Environment,
	)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	pool, err := database.OpenNamed(
		ctx,
		cfg.Database,
		workerDatabaseApplicationName,
	)
	if err != nil {
		logger.Error("open outbox worker database", "error_code", "database_open_failed")
		return 1
	}
	defer pool.Close()

	if err := outboxworker.VerifyDatabaseCapabilities(
		ctx,
		pool,
		cfg.Database.QueryTimeout,
	); err != nil {
		logger.Error(
			"verify outbox worker database capabilities",
			"error_code", databaseCapabilityErrorCode(err),
		)
		return 1
	}

	store, err := outboxworker.NewPostgresStore(pool, cfg.Database.QueryTimeout)
	if err != nil {
		logger.Error("initialize outbox store", "error_code", "store_initialization_failed")
		return 1
	}
	runner, err := newWorkerRunner(cfg, store, logger, uuid.New())
	if err != nil {
		logger.Error("initialize outbox runner", "error_code", "runner_initialization_failed")
		return 1
	}

	if err := runner.Run(ctx); err != nil {
		logger.Error("outbox worker stopped with error", "error_code", "runner_failed")
		return 1
	}
	logger.Info("outbox worker stopped")
	return 0
}

func databaseCapabilityErrorCode(err error) string {
	if errors.Is(err, outboxworker.ErrUnsafeDatabaseCapabilities) {
		return "unsafe_database_capabilities"
	}
	return "database_capability_probe_failed"
}

func newWorkerRegistry() *outboxworker.Registry {
	// P3-03 establishes the durable worker runtime only. Consumer registrations
	// are added by their owning tasks, beginning with P3-04. An empty exact
	// allowlist deliberately leaves every historical Phase 1/2 event untouched.
	return outboxworker.NewRegistry()
}

func newWorkerRunner(
	cfg config.WorkerConfig,
	store outboxworker.Store,
	logger *slog.Logger,
	ownerID uuid.UUID,
) (*outboxworker.Runner, error) {
	registry := newWorkerRegistry()
	backoff, err := outboxworker.NewBackoff(
		cfg.RetryBaseDelay,
		cfg.RetryMaxDelay,
		cfg.RetryJitter,
	)
	if err != nil {
		return nil, err
	}

	return outboxworker.NewRunner(outboxworker.RunnerConfig{
		OwnerID:         ownerID,
		BatchSize:       cfg.BatchSize,
		Concurrency:     cfg.Concurrency,
		PollInterval:    cfg.PollInterval,
		MetricsInterval: cfg.MetricsInterval,
		LeaseDuration:   cfg.LeaseDuration,
		HandlerTimeout:  cfg.HandlerTimeout,
		ShutdownTimeout: cfg.ShutdownTimeout,
		MaxAttempts:     cfg.MaxAttempts,
		Backoff:         backoff,
		Store:           store,
		Registry:        registry,
		Logger:          logger,
		Metrics:         outboxworker.NewMetrics(registry.HandlerNames()),
	})
}
