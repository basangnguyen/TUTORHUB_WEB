//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const (
	p405SharedReadOnlyConfirmation = "I_UNDERSTAND_P4_05_SHARED_READ_ONLY"
	p405SharedSnapshotConfirmation = "I_UNDERSTAND_P4_05_SHARED_READ_ONLY_SNAPSHOT"
)

var p405SharedConflictingConfirmations = []string{
	"P4_02_DISPOSABLE_CONFIRM",
	"P4_02_OWNER_PREFLIGHT",
	"P4_02_PROVIDER_SMOKE_CONFIRM",
	"P4_02_SHARED_CONFIRM",
	"P4_02_ACL_PROVISION_CONFIRM",
	"P4_02_SHARED_ACL_PROVISION_CONFIRM",
	"P4_04_DISPOSABLE_CONFIRM",
	"P4_04_OWNER_PREFLIGHT",
	"P4_04_ACL_PROVISION_CONFIRM",
	"P4_04_SHARED_CONFIRM",
	"P4_04_SHARED_ACL_PROVISION_CONFIRM",
	"P4_04_SHARED_SNAPSHOT_CONFIRM",
	"P4_05_DISPOSABLE_CONFIRM",
	"P4_05_PROVIDER_CONFIRM",
	"P4_05_BROWSER_PROVIDER_CONFIRM",
	"P4_05_PROVIDER_ROOM_NAME",
	"P4_05_ACL_PROVISION_CONFIRM",
	"P4_05_SHARED_ACL_PROVISION_CONFIRM",
}

// TestPostgresP405SharedReadOnlySnapshot records a bounded shared-staging
// snapshot before and after live acceptance. It never invokes migrations,
// provisioning, fixture mutation or the LiveKit provider.
func TestPostgresP405SharedReadOnlySnapshot(t *testing.T) {
	migrationURL, runtimeURL := requireP405SharedReadOnlyBoundary(t, true)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)

	migrationTx := beginP405ReadOnlyTransaction(t, ctx, migrationPool)
	defer func() { _ = migrationTx.Rollback(context.Background()) }()
	runtimeTx := beginP405ReadOnlyTransaction(t, ctx, runtimePool)
	defer func() { _ = runtimeTx.Rollback(context.Background()) }()

	var migrationRole, migrationDatabase string
	if err := migrationTx.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&migrationRole,
		&migrationDatabase,
	); err != nil {
		t.Fatal("inspect P4-05 shared owner identity")
	}
	var runtimeRole, runtimeDatabase string
	if err := runtimeTx.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&runtimeRole,
		&runtimeDatabase,
	); err != nil {
		t.Fatal("inspect P4-05 shared runtime identity")
	}
	if migrationRole == runtimeRole || migrationDatabase != runtimeDatabase {
		t.Fatal("P4-05 shared read-only gate requires distinct roles on one database")
	}

	var version int
	var dirty bool
	counts := make([]int64, 11)
	if err := migrationTx.QueryRow(ctx, `SELECT
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
		t.Fatal("read P4-05 shared snapshot")
	}
	if version != 31 || dirty {
		t.Fatal("P4-05 shared read-only snapshot requires ledger 31 false")
	}
	if counts[10] != 0 {
		t.Fatal("P4-05 shared read-only snapshot found enabled media feature overrides")
	}

	forcedOffCatalog, err := featurecontrol.NewCatalog(featurecontrol.Guardrails{
		ForcedOffFeatures: map[featurecontrol.FeatureKey]bool{
			featurecontrol.FeatureClassroomMediaRooms: true,
			featurecontrol.FeatureInstantStudyRooms:   true,
		},
	})
	if err != nil {
		t.Fatal("construct P4-05 shared deployment guardrail")
	}
	configuredEnabled := true
	for _, key := range []featurecontrol.FeatureKey{
		featurecontrol.FeatureClassroomMediaRooms,
		featurecontrol.FeatureInstantStudyRooms,
	} {
		effective, evaluateErr := forcedOffCatalog.EvaluateFeature(key, &configuredEnabled)
		if evaluateErr != nil || effective.Enabled ||
			effective.Source != featurecontrol.ValueSourceDeploymentGuardrail {
			t.Fatal("P4-05 shared media feature did not remain deployment-force-off")
		}
	}

	t.Logf(
		"P4_05_SHARED_SNAPSHOT version=%d dirty=%t media_features=false relations=8 media_spaces=%d media_room_instances=%d media_space_members=%d media_admission_requests=%d media_participant_sessions=%d media_space_mutation_receipts=%d media_provider_webhook_receipts=%d livekit_webhook_events=%d media_outbox=%d media_audit=%d enabled_media_feature_overrides=%d",
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

// TestPostgresMediaLifecycleRuntimeExactACLP405SharedReadOnly reuses the exact
// media grant matrix through the non-migrating, read-only helper path.
func TestPostgresMediaLifecycleRuntimeExactACLP405SharedReadOnly(t *testing.T) {
	requireP405SharedReadOnlyBoundary(t, false)
	runPostgresMediaLifecycleRuntimeExactACL(t, false)
}

func requireP405SharedReadOnlyBoundary(t *testing.T, requireSnapshot bool) (string, string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_05_SHARED_CONFIRM")) !=
		p405SharedReadOnlyConfirmation {
		t.Fatal("P4_05_SHARED_CONFIRM is not set to the shared read-only confirmation")
	}
	snapshotConfirmation := strings.TrimSpace(os.Getenv("P4_05_SHARED_SNAPSHOT_CONFIRM"))
	if requireSnapshot {
		if snapshotConfirmation != p405SharedSnapshotConfirmation {
			t.Fatal("P4_05_SHARED_SNAPSHOT_CONFIRM is not set to the snapshot confirmation")
		}
	} else if snapshotConfirmation != "" && snapshotConfirmation != p405SharedSnapshotConfirmation {
		t.Fatal("P4_05_SHARED_SNAPSHOT_CONFIRM does not match the exact snapshot confirmation")
	}
	for _, key := range p405SharedConflictingConfirmations {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			t.Fatalf("P4-05 shared read-only gate refuses confirmation %s", key)
		}
	}
	migrationURL := requireP405DisposableEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireP405DisposableEnvironment(t, "DATABASE_POOL_URL")
	requireP404NeonURLBoundary(t, migrationURL, runtimeURL)
	return migrationURL, runtimeURL
}
