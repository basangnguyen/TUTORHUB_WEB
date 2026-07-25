package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
)

func TestPoolConfigUsesExplicitApplicationName(t *testing.T) {
	t.Parallel()

	poolConfig, err := PoolConfig(config.DatabaseConfig{
		PoolURL:               "postgresql://worker:secret@localhost/tutorhub?sslmode=disable",
		MaxConnections:        2,
		MinConnections:        0,
		ConnectTimeout:        time.Second,
		QueryTimeout:          time.Second,
		MaxConnectionLifetime: time.Minute,
		MaxConnectionIdleTime: time.Minute,
		HealthCheckPeriod:     time.Minute,
	}, "tutorhub-outbox-worker")
	if err != nil {
		t.Fatalf("build pool config: %v", err)
	}
	if got := poolConfig.ConnConfig.RuntimeParams["application_name"]; got != "tutorhub-outbox-worker" {
		t.Fatalf("unexpected application name %q", got)
	}
}

func TestPoolConfigRejectsEmptyApplicationName(t *testing.T) {
	t.Parallel()

	_, err := PoolConfig(config.DatabaseConfig{
		PoolURL: "postgresql://worker:secret@localhost/tutorhub?sslmode=disable",
	}, "")
	if err == nil || !strings.Contains(err.Error(), "application name") {
		t.Fatalf("expected application-name validation error, got %v", err)
	}
}

func TestReadinessCheck(t *testing.T) {
	t.Parallel()

	check := NewReadinessCheck(fakePinger{}, time.Second)
	if check.Name() != "database" {
		t.Fatalf("unexpected readiness name %q", check.Name())
	}
	if err := check.Check(context.Background()); err != nil {
		t.Fatalf("expected successful readiness check, got %v", err)
	}
}

func TestReadinessCheckWrapsPingFailure(t *testing.T) {
	t.Parallel()

	check := NewReadinessCheck(fakePinger{err: errors.New("unavailable")}, time.Second)
	err := check.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "ping database readiness") {
		t.Fatalf("expected wrapped readiness error, got %v", err)
	}
}

func TestUnconfiguredReadinessCheckFails(t *testing.T) {
	t.Parallel()

	err := (UnconfiguredReadinessCheck{}).Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected unconfigured error, got %v", err)
	}
}

type fakePinger struct {
	err error
}

func (pinger fakePinger) Ping(context.Context) error {
	return pinger.err
}
