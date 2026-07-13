package migrationrunner

import (
	"context"
	"strings"
	"testing"
)

func TestUpRequiresDatabaseURL(t *testing.T) {
	t.Parallel()

	err := Up(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "DATABASE_MIGRATION_URL") {
		t.Fatalf("expected missing URL error, got %v", err)
	}
}

func TestDownRequiresPositiveStepCount(t *testing.T) {
	t.Parallel()

	err := Down(context.Background(), "postgresql://not-used", 0)
	if err == nil || !strings.Contains(err.Error(), "greater than zero") {
		t.Fatalf("expected invalid step error, got %v", err)
	}
}
