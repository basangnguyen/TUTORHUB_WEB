//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const (
	p409OwnerPreflightConfirmation = "I_UNDERSTAND_P4_09_OWNER_PREFLIGHT_ONLY"
	p409FinalSnapshotConfirmation  = "I_UNDERSTAND_P4_09_FINAL_SNAPSHOT_READ_ONLY"
)

// TestPostgresP409DisposableOwnerPreflight proves the three supplied URLs are
// distinct least-privilege principals on one clean Neon disposable database at
// ledger 34. Every query runs inside an explicit read-only transaction.
func TestPostgresP409DisposableOwnerPreflight(t *testing.T) {
	requireP409ReadOnlyConfirmation(
		t,
		"P4_09_OWNER_PREFLIGHT",
		p409OwnerPreflightConfirmation,
	)
	runPostgresMediaReadOnlySnapshot(t, "P4_09", 34, false, nil)
}

// TestPostgresP409DisposableFinalSnapshot proves migration 000035, exact media
// ACL, feature-off state, and bounded retained recovery evidence without
// mutating the disposable branch.
func TestPostgresP409DisposableFinalSnapshot(t *testing.T) {
	requireP409ReadOnlyConfirmation(
		t,
		"P4_09_FINAL_SNAPSHOT_CONFIRM",
		p409FinalSnapshotConfirmation,
	)
	runPostgresMediaReadOnlySnapshot(t, "P4_09", 35, true, logP409FinalSnapshot)
}

func requireP409ReadOnlyConfirmation(t *testing.T, activeName string, expected string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(activeName)) != expected {
		t.Fatalf("%s is not set to the exact P4-09 read-only confirmation", activeName)
	}
	conflicting := append([]string(nil), p405DisposableConflictingConfirmations...)
	conflicting = append(conflicting,
		"P4_05_DISPOSABLE_CONFIRM",
		"P4_06_DISPOSABLE_CONFIRM",
		"P4_06_OWNER_PREFLIGHT",
		"P4_06_FINAL_SNAPSHOT_CONFIRM",
		"P4_06_ACL_PROVISION_CONFIRM",
		"P4_07_DISPOSABLE_CONFIRM",
		"P4_07_OWNER_PREFLIGHT",
		"P4_07_FINAL_SNAPSHOT_CONFIRM",
		"P4_07_ACL_PROVISION_CONFIRM",
		"P4_09_DISPOSABLE_CONFIRM",
		"P4_09_OWNER_PREFLIGHT",
		"P4_09_FINAL_SNAPSHOT_CONFIRM",
	)
	for _, name := range conflicting {
		if name == activeName {
			continue
		}
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-09 read-only database gate refuses confirmation %s", name)
		}
	}
}

func logP409FinalSnapshot(t *testing.T, ctx context.Context, ownerTx pgx.Tx) {
	t.Helper()
	var recoveryReceipts int64
	var recoveryEvents int64
	var activeIntents int64
	var enabledMediaOverrides int64
	if err := ownerTx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_space_mutation_receipts
      WHERE operation = 'recover'),
    (SELECT count(*) FROM tutorhub.outbox_events
      WHERE aggregate_type = 'media_space'
        AND event_type = 'media_space.recovered.v1'),
    (SELECT count(*) FROM tutorhub.media_room_instances
      WHERE status IN ('provisioning', 'active', 'closing')),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
      WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms')
        AND enabled)`).Scan(
		&recoveryReceipts,
		&recoveryEvents,
		&activeIntents,
		&enabledMediaOverrides,
	); err != nil {
		t.Fatal("inspect P4-09 final aggregate snapshot")
	}
	if recoveryReceipts != recoveryEvents {
		t.Fatalf(
			"P4-09 final snapshot invariants: recovery_receipts=%d recovery_events=%d",
			recoveryReceipts,
			recoveryEvents,
		)
	}
	t.Logf(
		"P4_09_FINAL_SNAPSHOT PASS ledger=35 dirty=false media_features=false retained_enabled_media_overrides=%d recovery_receipts=%d recovery_events=%d retained_active_intents=%d",
		enabledMediaOverrides,
		recoveryReceipts,
		recoveryEvents,
		activeIntents,
	)
}
