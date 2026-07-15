package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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
