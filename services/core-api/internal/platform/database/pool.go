package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/config"
)

const applicationName = "tutorhub-core-api"

func Open(ctx context.Context, cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.PoolURL)
	if err != nil {
		return nil, fmt.Errorf("parse database pool configuration: %w", err)
	}

	poolConfig.MaxConns = cfg.MaxConnections
	poolConfig.MinConns = cfg.MinConnections
	poolConfig.MaxConnLifetime = cfg.MaxConnectionLifetime
	poolConfig.MaxConnIdleTime = cfg.MaxConnectionIdleTime
	poolConfig.HealthCheckPeriod = cfg.HealthCheckPeriod
	poolConfig.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	poolConfig.ConnConfig.RuntimeParams["application_name"] = applicationName

	connectContext, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectContext, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	if err := pool.Ping(connectContext); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

type Pinger interface {
	Ping(context.Context) error
}

type ReadinessCheck struct {
	pool    Pinger
	timeout time.Duration
}

func NewReadinessCheck(pool Pinger, timeout time.Duration) ReadinessCheck {
	return ReadinessCheck{pool: pool, timeout: timeout}
}

func (ReadinessCheck) Name() string {
	return "database"
}

func (check ReadinessCheck) Check(ctx context.Context) error {
	if check.pool == nil {
		return fmt.Errorf("database pool is not configured")
	}

	checkContext, cancel := context.WithTimeout(ctx, check.timeout)
	defer cancel()

	if err := check.pool.Ping(checkContext); err != nil {
		return fmt.Errorf("ping database readiness: %w", err)
	}

	return nil
}

type UnconfiguredReadinessCheck struct{}

func (UnconfiguredReadinessCheck) Name() string {
	return "database"
}

func (UnconfiguredReadinessCheck) Check(context.Context) error {
	return fmt.Errorf("database is not configured")
}
