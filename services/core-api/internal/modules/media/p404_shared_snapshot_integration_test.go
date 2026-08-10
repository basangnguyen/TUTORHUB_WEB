//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const p404SharedSnapshotConfirmation = "I_UNDERSTAND_P4_04_SHARED_READ_ONLY_SNAPSHOT"

// TestPostgresP404SharedReadOnlySnapshot records only schema-ledger state and
// aggregate row counts. The explicit read-only transaction prevents this probe
// from mutating shared staging before or after live acceptance.
func TestPostgresP404SharedReadOnlySnapshot(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_04_SHARED_CONFIRM")) != p404SharedPreflightConfirmation {
		t.Fatal("P4_04_SHARED_CONFIRM is not set to the shared-staging confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_04_SHARED_SNAPSHOT_CONFIRM")) !=
		p404SharedSnapshotConfirmation {
		t.Fatal("P4_04_SHARED_SNAPSHOT_CONFIRM is not set to the shared read-only confirmation")
	}
	if p404AnyConflictingSharedConfirmationIsSet() {
		t.Fatal("P4-04 shared read-only snapshot refuses disposable confirmations")
	}

	migrationURL := requireP404SharedIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireP404SharedIntegrationEnvironment(t, "DATABASE_POOL_URL")
	requireP404NeonURLBoundary(t, migrationURL, runtimeURL)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(pool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	var migrationRole, migrationDatabase string
	if err := pool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&migrationRole,
		&migrationDatabase,
	); err != nil {
		t.Fatal("inspect P4-04 shared snapshot owner identity")
	}
	var runtimeRole, runtimeDatabase string
	if err := runtimePool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&runtimeRole,
		&runtimeDatabase,
	); err != nil {
		t.Fatal("inspect P4-04 shared snapshot runtime identity")
	}
	if migrationRole == runtimeRole || migrationDatabase != runtimeDatabase {
		t.Fatal("P4-04 shared snapshot requires distinct roles on the same database")
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		t.Fatal("begin P4-04 shared read-only snapshot")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var transactionReadOnly string
	if err := tx.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&transactionReadOnly); err != nil {
		t.Fatal("inspect P4-04 shared snapshot transaction mode")
	}
	if transactionReadOnly != "on" {
		t.Fatal("P4-04 shared snapshot transaction is not read-only")
	}

	var version int
	var dirty bool
	counts := make([]int64, 11)
	if err := tx.QueryRow(ctx, `SELECT
    ledger.version,
    ledger.dirty,
    (SELECT count(*) FROM tutorhub.media_spaces),
    (SELECT count(*) FROM tutorhub.media_room_instances),
    (SELECT count(*) FROM tutorhub.media_space_members),
    (SELECT count(*) FROM tutorhub.media_admission_requests),
    (SELECT count(*) FROM tutorhub.media_participant_sessions),
    (SELECT count(*) FROM tutorhub.media_space_mutation_receipts),
    (SELECT count(*) FROM tutorhub.media_provider_webhook_receipts),
    (SELECT count(*) FROM tutorhub.livekit_webhook_events),
    (SELECT count(*) FROM tutorhub.outbox_events
      WHERE aggregate_type IN ('media_space', 'media_space_member', 'media_admission')),
    (SELECT count(*) FROM tutorhub.audit_events
      WHERE action LIKE 'media_space.%'
         OR action LIKE 'media_space_member.%'
         OR action LIKE 'media_admission.%'),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
      WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms')
        AND enabled)
FROM public.tutorhub_schema_migrations AS ledger`).Scan(
		&version,
		&dirty,
		&counts[0],
		&counts[1],
		&counts[2],
		&counts[3],
		&counts[4],
		&counts[5],
		&counts[6],
		&counts[7],
		&counts[8],
		&counts[9],
		&counts[10],
	); err != nil {
		t.Fatal("read P4-04 shared snapshot")
	}
	if version != 31 || dirty {
		t.Fatal("P4-04 shared read-only snapshot requires ledger 31 false")
	}
	if counts[10] != 0 {
		t.Fatal("P4-04 shared read-only snapshot found enabled media feature overrides")
	}

	t.Logf(
		"P4_04_SHARED_SNAPSHOT version=%d dirty=%t media_spaces=%d media_room_instances=%d media_space_members=%d media_admission_requests=%d media_participant_sessions=%d media_space_mutation_receipts=%d media_provider_webhook_receipts=%d livekit_webhook_events=%d media_outbox=%d media_audit=%d enabled_media_feature_overrides=%d",
		version,
		dirty,
		counts[0],
		counts[1],
		counts[2],
		counts[3],
		counts[4],
		counts[5],
		counts[6],
		counts[7],
		counts[8],
		counts[9],
		counts[10],
	)
}
