//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const p410FinalSnapshotConfirmation = "I_UNDERSTAND_P4_10_FINAL_SNAPSHOT_READ_ONLY"

// TestPostgresP410DisposableFinalSnapshot verifies the clean post-migration
// ledger, exact diagnostics ACL, deployment feature-off state, and retention
// invariant using only explicit read-only transactions.
func TestPostgresP410DisposableFinalSnapshot(t *testing.T) {
	requireP410FinalSnapshotConfirmation(t)
	runPostgresMediaReadOnlySnapshot(t, "P4_10", 36, true, logP410FinalSnapshot)
}

func requireP410FinalSnapshotConfirmation(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_10_FINAL_SNAPSHOT_CONFIRM")) != p410FinalSnapshotConfirmation {
		t.Fatal("P4_10_FINAL_SNAPSHOT_CONFIRM is not set to the exact P4-10 read-only confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_10_DISPOSABLE_CONFIRM")) != p410DisposableConfirmation ||
		strings.TrimSpace(os.Getenv("P4_10_ACL_PROVISION_CONFIRM")) != p410ACLProvisionConfirmation {
		t.Fatal("P4-10 final snapshot requires the exact disposable and ACL confirmations")
	}
}

func logP410FinalSnapshot(t *testing.T, ctx context.Context, ownerTx pgx.Tx) {
	t.Helper()
	var diagnostics int64
	var expiredDiagnostics int64
	var retentionViolations int64
	var enabledMediaOverrides int64
	if err := ownerTx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_join_diagnostics),
    (SELECT count(*) FROM tutorhub.media_join_diagnostics
      WHERE retention_until <= clock_timestamp()),
    (SELECT count(*) FROM tutorhub.media_join_diagnostics
      WHERE retention_until <> recorded_at + interval '30 days'),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
      WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms')
        AND enabled)`).Scan(
		&diagnostics,
		&expiredDiagnostics,
		&retentionViolations,
		&enabledMediaOverrides,
	); err != nil {
		t.Fatal("inspect P4-10 final aggregate snapshot")
	}
	if retentionViolations != 0 {
		t.Fatalf("P4-10 final snapshot found retention violations: %d", retentionViolations)
	}
	t.Logf(
		"P4_10_FINAL_SNAPSHOT PASS ledger=36 dirty=false media_features=false diagnostics=%d expired_diagnostics=%d retention_violations=0 retained_enabled_media_overrides=%d",
		diagnostics,
		expiredDiagnostics,
		enabledMediaOverrides,
	)
}
