package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultWorkerBatchSize       = 25
	defaultWorkerConcurrency     = 4
	defaultWorkerPollInterval    = 2 * time.Second
	defaultWorkerMetricsInterval = 30 * time.Second
	defaultWorkerLeaseDuration   = 30 * time.Second
	defaultWorkerHandlerTimeout  = 20 * time.Second
	defaultWorkerShutdownTimeout = 30 * time.Second
	minimumWorkerLeaseMargin     = 5 * time.Second
	defaultWorkerMaxAttempts     = 8
	defaultWorkerRetryBaseDelay  = time.Second
	defaultWorkerRetryMaxDelay   = 15 * time.Minute
	defaultWorkerRetryJitterPct  = 20
	defaultWorkerDBMaxConns      = 2
)

type WorkerConfig struct {
	Environment                   string
	LogLevel                      string
	Database                      DatabaseConfig
	EnableInAppNotificationCanary bool
	BatchSize                     int
	Concurrency                   int
	PollInterval                  time.Duration
	MetricsInterval               time.Duration
	LeaseDuration                 time.Duration
	HandlerTimeout                time.Duration
	ShutdownTimeout               time.Duration
	MaxAttempts                   int
	RetryBaseDelay                time.Duration
	RetryMaxDelay                 time.Duration
	RetryJitter                   float64
}

func LoadWorker() (WorkerConfig, error) {
	return loadWorker(os.LookupEnv)
}

func loadWorker(lookup lookupEnv) (WorkerConfig, error) {
	cfg := WorkerConfig{
		Environment: strings.ToLower(strings.TrimSpace(valueOrDefault(lookup, "APP_ENV", "development"))),
		LogLevel:    strings.ToLower(strings.TrimSpace(valueOrDefault(lookup, "LOG_LEVEL", "info"))),
	}

	var validationErrors []error
	if _, ok := validEnvironments[cfg.Environment]; !ok {
		validationErrors = append(validationErrors, fmt.Errorf(
			"APP_ENV must be one of development, test, staging, production",
		))
	}
	if _, ok := validLogLevels[cfg.LogLevel]; !ok {
		validationErrors = append(validationErrors, fmt.Errorf(
			"LOG_LEVEL must be one of debug, info, warn, error",
		))
	}

	cfg.Database = workerDatabaseConfig(lookup, cfg.Environment, &validationErrors)
	cfg.EnableInAppNotificationCanary = boolValue(
		lookup,
		"OUTBOX_ENABLE_IN_APP_NOTIFICATION_CANARY",
		false,
		&validationErrors,
	)
	cfg.BatchSize = intValue(
		lookup, "OUTBOX_BATCH_SIZE", defaultWorkerBatchSize, 1, 100, &validationErrors,
	)
	cfg.Concurrency = intValue(
		lookup, "OUTBOX_CONCURRENCY", defaultWorkerConcurrency, 1, 32, &validationErrors,
	)
	cfg.PollInterval = boundedDurationValue(
		lookup, "OUTBOX_POLL_INTERVAL", defaultWorkerPollInterval,
		100*time.Millisecond, 5*time.Minute, &validationErrors,
	)
	cfg.MetricsInterval = boundedDurationValue(
		lookup, "OUTBOX_METRICS_INTERVAL", defaultWorkerMetricsInterval,
		time.Second, time.Hour, &validationErrors,
	)
	cfg.LeaseDuration = boundedDurationValue(
		lookup, "OUTBOX_LEASE_DURATION", defaultWorkerLeaseDuration,
		5*time.Second, 30*time.Minute, &validationErrors,
	)
	cfg.HandlerTimeout = boundedDurationValue(
		lookup, "OUTBOX_HANDLER_TIMEOUT", defaultWorkerHandlerTimeout,
		time.Second, 29*time.Minute, &validationErrors,
	)
	cfg.ShutdownTimeout = boundedDurationValue(
		lookup, "OUTBOX_SHUTDOWN_TIMEOUT", defaultWorkerShutdownTimeout,
		time.Second, 30*time.Minute, &validationErrors,
	)
	cfg.MaxAttempts = intValue(
		lookup, "OUTBOX_MAX_ATTEMPTS", defaultWorkerMaxAttempts, 1, 100, &validationErrors,
	)
	cfg.RetryBaseDelay = boundedDurationValue(
		lookup, "OUTBOX_RETRY_BASE_DELAY", defaultWorkerRetryBaseDelay,
		100*time.Millisecond, 24*time.Hour, &validationErrors,
	)
	cfg.RetryMaxDelay = boundedDurationValue(
		lookup, "OUTBOX_RETRY_MAX_DELAY", defaultWorkerRetryMaxDelay,
		100*time.Millisecond, 7*24*time.Hour, &validationErrors,
	)
	jitterPercent := intValue(
		lookup, "OUTBOX_RETRY_JITTER_PERCENT", defaultWorkerRetryJitterPct,
		0, 99, &validationErrors,
	)
	cfg.RetryJitter = float64(jitterPercent) / 100

	if cfg.HandlerTimeout >= cfg.LeaseDuration {
		validationErrors = append(validationErrors, fmt.Errorf(
			"OUTBOX_HANDLER_TIMEOUT must be shorter than OUTBOX_LEASE_DURATION",
		))
	}
	if cfg.HandlerTimeout+cfg.Database.QueryTimeout+minimumWorkerLeaseMargin > cfg.LeaseDuration {
		validationErrors = append(validationErrors, fmt.Errorf(
			"OUTBOX_LEASE_DURATION must cover OUTBOX_HANDLER_TIMEOUT, DATABASE_QUERY_TIMEOUT, and the worker lease safety margin",
		))
	}
	if cfg.Concurrency > cfg.BatchSize {
		validationErrors = append(validationErrors, fmt.Errorf(
			"OUTBOX_CONCURRENCY must not exceed OUTBOX_BATCH_SIZE",
		))
	}
	if cfg.ShutdownTimeout < cfg.HandlerTimeout {
		validationErrors = append(validationErrors, fmt.Errorf(
			"OUTBOX_SHUTDOWN_TIMEOUT must not be shorter than OUTBOX_HANDLER_TIMEOUT",
		))
	}
	if cfg.ShutdownTimeout < cfg.HandlerTimeout+cfg.Database.QueryTimeout {
		validationErrors = append(validationErrors, fmt.Errorf(
			"OUTBOX_SHUTDOWN_TIMEOUT must cover OUTBOX_HANDLER_TIMEOUT plus DATABASE_QUERY_TIMEOUT",
		))
	}
	if cfg.RetryBaseDelay > cfg.RetryMaxDelay {
		validationErrors = append(validationErrors, fmt.Errorf(
			"OUTBOX_RETRY_BASE_DELAY must not exceed OUTBOX_RETRY_MAX_DELAY",
		))
	}

	if err := errors.Join(validationErrors...); err != nil {
		return WorkerConfig{}, fmt.Errorf("validate worker configuration: %w", err)
	}
	return cfg, nil
}

func workerDatabaseConfig(
	lookup lookupEnv,
	environment string,
	validationErrors *[]error,
) DatabaseConfig {
	poolURL := strings.TrimSpace(valueOrDefault(lookup, "DATABASE_WORKER_URL", ""))
	if poolURL == "" {
		*validationErrors = append(*validationErrors, fmt.Errorf("DATABASE_WORKER_URL is required"))
	} else if err := validatePostgresURL(environment, "DATABASE_WORKER_URL", poolURL); err != nil {
		*validationErrors = append(*validationErrors, err)
	}

	maximum := int32Value(
		lookup, "DATABASE_MAX_CONNECTIONS", defaultWorkerDBMaxConns, 1, 20, validationErrors,
	)
	minimum := int32Value(
		lookup, "DATABASE_MIN_CONNECTIONS", defaultDBMinConnections, 0, 20, validationErrors,
	)
	if minimum > maximum {
		*validationErrors = append(
			*validationErrors,
			fmt.Errorf("DATABASE_MIN_CONNECTIONS must not exceed DATABASE_MAX_CONNECTIONS"),
		)
	}

	return DatabaseConfig{
		PoolURL:        poolURL,
		MaxConnections: maximum,
		MinConnections: minimum,
		ConnectTimeout: durationValue(lookup, "DATABASE_CONNECT_TIMEOUT", defaultDBConnectTimeout, validationErrors),
		QueryTimeout: boundedDurationValue(
			lookup,
			"DATABASE_QUERY_TIMEOUT",
			defaultDBQueryTimeout,
			100*time.Millisecond,
			time.Minute,
			validationErrors,
		),
		MaxConnectionLifetime: durationValue(lookup, "DATABASE_MAX_CONNECTION_LIFETIME", defaultDBMaxLifetime, validationErrors),
		MaxConnectionIdleTime: durationValue(lookup, "DATABASE_MAX_CONNECTION_IDLE_TIME", defaultDBMaxIdleTime, validationErrors),
		HealthCheckPeriod:     durationValue(lookup, "DATABASE_HEALTH_CHECK_PERIOD", defaultDBHealthPeriod, validationErrors),
	}
}

func boundedDurationValue(
	lookup lookupEnv,
	key string,
	fallback time.Duration,
	minimum time.Duration,
	maximum time.Duration,
	validationErrors *[]error,
) time.Duration {
	raw := strings.TrimSpace(valueOrDefault(lookup, key, fallback.String()))
	value, err := time.ParseDuration(raw)
	if err != nil || value < minimum || value > maximum {
		*validationErrors = append(*validationErrors, fmt.Errorf(
			"%s must be a duration between %s and %s", key, minimum, maximum,
		))
		return fallback
	}
	return value
}
