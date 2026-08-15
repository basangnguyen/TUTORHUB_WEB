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
	p409SharedConfirmation               = "I_UNDERSTAND_P4_09_SHARED_STAGING_ONLY"
	p409SharedOwnerPreflightConfirmation = "I_UNDERSTAND_P4_09_SHARED_OWNER_PREFLIGHT_READ_ONLY"
	p409SharedMigrationConfirmation      = "I_UNDERSTAND_P4_09_FORWARD_SHARED_STAGING_ONLY"
	p409SharedACLProvisionConfirmation   = "I_UNDERSTAND_P4_09_ACL_PROVISION_SHARED_STAGING_ONLY"
	p409SharedFinalSnapshotConfirmation  = "I_UNDERSTAND_P4_09_SHARED_FINAL_SNAPSHOT_READ_ONLY"
)

var p409SharedActions = map[string]string{
	"P4_09_SHARED_OWNER_PREFLIGHT":       p409SharedOwnerPreflightConfirmation,
	"P4_09_SHARED_MIGRATION_CONFIRM":     p409SharedMigrationConfirmation,
	"P4_09_SHARED_ACL_PROVISION_CONFIRM": p409SharedACLProvisionConfirmation,
	"P4_09_SHARED_FINAL_CONFIRM":         p409SharedFinalSnapshotConfirmation,
}

func TestPostgresP409SharedOwnerPreflight(t *testing.T) {
	requireP409SharedConfirmation(t, "P4_09_SHARED_OWNER_PREFLIGHT")
	runPostgresMediaReadOnlySnapshot(t, "P4_09_SHARED", 34, false, nil)
}

func TestPostgresP409SharedForwardMigration(t *testing.T) {
	requireP409SharedConfirmation(t, "P4_09_SHARED_MIGRATION_CONFIRM")
	runP409ForwardMigration(t)
}

func TestPostgresP409SharedFinalSnapshot(t *testing.T) {
	requireP409SharedConfirmation(t, "P4_09_SHARED_FINAL_CONFIRM")
	runPostgresMediaReadOnlySnapshot(t, "P4_09_SHARED", 35, true, assertP409SharedSideEffectsClean)
}

func TestProvisionPostgresMediaRecoveryExactACLShared(t *testing.T) {
	requireP409SharedConfirmation(t, "P4_09_SHARED_ACL_PROVISION_CONFIRM")
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t, mediaACLProvisionConfiguration{
		expectedVersion: 35,
		expectations:    p407MediaACLExpectations(),
	})
}

func requireP409SharedConfirmation(t *testing.T, activeName string) {
	t.Helper()
	expected, ok := p409SharedActions[activeName]
	if !ok {
		t.Fatal("P4-09 shared gate received an unknown action")
	}
	sharedConfirmation := strings.TrimSpace(os.Getenv("P4_09_SHARED_CONFIRM"))
	if sharedConfirmation == "" {
		t.Skip("P4_09_SHARED_CONFIRM is not set; shared-staging gate was not requested")
	}
	if sharedConfirmation != p409SharedConfirmation {
		t.Fatal("P4_09_SHARED_CONFIRM is not set to the exact shared-staging confirmation")
	}
	if strings.TrimSpace(os.Getenv(activeName)) != expected {
		t.Fatalf("%s is not set to the exact P4-09 shared action confirmation", activeName)
	}
	for name := range p409SharedActions {
		if name != activeName && strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-09 shared gate refuses conflicting action confirmation %s", name)
		}
	}
	for _, name := range []string{
		"P4_09_DISPOSABLE_CONFIRM",
		"P4_09_OWNER_PREFLIGHT",
		"P4_09_FINAL_SNAPSHOT_CONFIRM",
		"P4_08_SHARED_CONFIRM",
		"P4_08_SHARED_PREFLIGHT_CONFIRM",
		"P4_08_SHARED_FINAL_CONFIRM",
		"P4_07_SHARED_CONFIRM",
		"P4_07_SHARED_OWNER_PREFLIGHT",
		"P4_07_SHARED_ACL_PROVISION_CONFIRM",
		"P4_07_SHARED_FINAL_SNAPSHOT_CONFIRM",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-09 shared gate refuses stale confirmation %s", name)
		}
	}
}

func assertP409SharedSideEffectsClean(t *testing.T, ctx context.Context, ownerTx pgx.Tx) {
	t.Helper()
	var recoveryReceipts, recoveryEvents, enabledMediaOverrides int64
	if err := ownerTx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_space_mutation_receipts
      WHERE operation = 'recover'),
    (SELECT count(*) FROM tutorhub.outbox_events
      WHERE aggregate_type = 'media_space'
        AND event_type = 'media_space.recovered.v1'),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
      WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms')
        AND enabled)`).Scan(&recoveryReceipts, &recoveryEvents, &enabledMediaOverrides); err != nil {
		t.Fatal("inspect P4-09 shared zero-side-effect snapshot")
	}
	if recoveryReceipts != 0 || recoveryEvents != 0 || enabledMediaOverrides != 0 {
		t.Fatal("P4-09 shared staging retained a recovery or feature-enable side effect")
	}
	t.Log("P4_09_SHARED_FINAL PASS ledger=35 dirty=false media_features=false recovery_side_effects=0")
}
