//go:build integration

package v1import

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

func TestFixtureImportIsIdempotentAndResumable(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_MIGRATION_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, databaseURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	parsed := loadTestFixture(t)
	cleanup := connectCleanup(t, ctx, databaseURL, parsed.Fixture.SourceSystem)
	defer func() { cleanup() }()

	first, err := Execute(ctx, databaseURL, "test", parsed, ModeApply, Options{})
	if err != nil {
		t.Fatalf("first fixture import: %v\nreport=%+v", err, first)
	}
	assertCounts(t, first, Counts{Source: 12, Mapped: 10, Imported: 10, Skipped: 2})
	if first.Status != "completed" || first.CheckpointOrdinal != 12 {
		t.Fatalf("unexpected first run state: %+v", first)
	}

	second, err := Execute(ctx, databaseURL, "test", parsed, ModeApply, Options{})
	if err != nil {
		t.Fatalf("second fixture import: %v\nreport=%+v", err, second)
	}
	assertCounts(t, second, Counts{Source: 12, Mapped: 10, Unchanged: 10, Skipped: 2})

	beforeDryRun := countImportedState(t, ctx, databaseURL, parsed.Fixture.SourceSystem)
	dryRun, err := Execute(ctx, databaseURL, "test", parsed, ModeDryRun, Options{})
	if err != nil {
		t.Fatalf("fixture dry-run: %v\nreport=%+v", err, dryRun)
	}
	if dryRun.Status != "planned" {
		t.Fatalf("unexpected dry-run state: %+v", dryRun)
	}
	afterDryRun := countImportedState(t, ctx, databaseURL, parsed.Fixture.SourceSystem)
	if beforeDryRun != afterDryRun {
		t.Fatalf("dry-run changed database state: before=%d after=%d", beforeDryRun, afterDryRun)
	}

	cleanup()
	cleanup = func() {}

	resumeFixture := parsed
	resumeFixture.Fixture.SourceSystem = "tutorhub-v1-resume"
	resumeFixture.Fixture.FixtureKey = "p2-11-resume-test"
	resumeCleanup := connectCleanup(t, ctx, databaseURL, resumeFixture.Fixture.SourceSystem)
	defer resumeCleanup()
	failed, err := Execute(ctx, databaseURL, "test", resumeFixture, ModeApply, Options{StopAfter: 3})
	if !errors.Is(err, ErrInjectedInterruption) {
		t.Fatalf("expected injected interruption, got report=%+v err=%v", failed, err)
	}
	if failed.Status != "failed" || failed.CheckpointOrdinal != 3 || failed.FailureAttempts != 1 {
		t.Fatalf("unexpected failed run state: %+v", failed)
	}
	resumed, err := Execute(ctx, databaseURL, "test", resumeFixture, ModeApply, Options{})
	if err != nil {
		t.Fatalf("resume fixture import: %v\nreport=%+v", err, resumed)
	}
	assertCounts(t, resumed, Counts{Source: 12, Mapped: 10, Imported: 10, Skipped: 2})
	if resumed.Status != "completed" || resumed.FailureAttempts != 1 {
		t.Fatalf("unexpected resumed run state: %+v", resumed)
	}
}

func assertCounts(t *testing.T, report Report, want Counts) {
	t.Helper()
	if report.Total.Source != want.Source || report.Total.Mapped != want.Mapped ||
		report.Total.Imported != want.Imported || report.Total.Updated != want.Updated ||
		report.Total.Unchanged != want.Unchanged || report.Total.Skipped != want.Skipped ||
		report.Total.Failed != want.Failed {
		t.Fatalf("unexpected report counts: got=%+v want=%+v", report.Total, want)
	}
}
