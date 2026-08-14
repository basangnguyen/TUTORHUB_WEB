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
	p407SharedConfirmation               = "I_UNDERSTAND_P4_07_SHARED_STAGING_ONLY"
	p407SharedOwnerPreflightConfirmation = "I_UNDERSTAND_P4_07_SHARED_OWNER_PREFLIGHT_READ_ONLY"
	p407SharedACLProvisionConfirmation   = "I_UNDERSTAND_P4_07_ACL_PROVISION_SHARED_STAGING_ONLY"
	p407SharedFinalSnapshotConfirmation  = "I_UNDERSTAND_P4_07_SHARED_FINAL_SNAPSHOT_READ_ONLY"
)

var p407SharedStaleConfirmations = []string{
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
	"P4_05_SHARED_CONFIRM",
	"P4_05_SHARED_ACL_PROVISION_CONFIRM",
	"P4_05_SHARED_SNAPSHOT_CONFIRM",
	"P4_06_DISPOSABLE_CONFIRM",
	"P4_06_OWNER_PREFLIGHT",
	"P4_06_ACL_PROVISION_CONFIRM",
	"P4_06_FINAL_SNAPSHOT_CONFIRM",
	"P4_06_SHARED_CONFIRM",
	"P4_06_SHARED_OWNER_PREFLIGHT",
	"P4_06_SHARED_ACL_PROVISION_CONFIRM",
	"P4_06_SHARED_FINAL_SNAPSHOT_CONFIRM",
	"P4_07_DISPOSABLE_CONFIRM",
	"P4_07_OWNER_PREFLIGHT",
	"P4_07_ACL_PROVISION_CONFIRM",
	"P4_07_FINAL_SNAPSHOT_CONFIRM",
	"P4_07_PROVIDER_CONFIRM",
}

var p407SharedActionConfirmations = []string{
	"P4_07_SHARED_OWNER_PREFLIGHT",
	"P4_07_SHARED_ACL_PROVISION_CONFIRM",
	"P4_07_SHARED_FINAL_SNAPSHOT_CONFIRM",
}

// TestPostgresP407SharedOwnerPreflight authenticates the three shared-staging
// principals and verifies the pre-migration ledger using read-only transactions.
func TestPostgresP407SharedOwnerPreflight(t *testing.T) {
	requireP407SharedConfirmation(
		t,
		"P4_07_SHARED_OWNER_PREFLIGHT",
		p407SharedOwnerPreflightConfirmation,
	)
	runPostgresP407ReadOnlySnapshot(t, 32, false)
}

// TestPostgresP407SharedFinalSnapshot proves the post-migration ledger, exact
// ACL, feature-off guardrail and zero P4-07 side effects without mutating shared
// staging.
func TestPostgresP407SharedFinalSnapshot(t *testing.T) {
	requireP407SharedConfirmation(
		t,
		"P4_07_SHARED_FINAL_SNAPSHOT_CONFIRM",
		p407SharedFinalSnapshotConfirmation,
	)
	runPostgresP407ReadOnlySnapshot(t, 33, true, assertP407SharedSideEffectsClean)
}

func requireP407SharedConfirmation(t *testing.T, actionName string, expectedAction string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_07_SHARED_CONFIRM")) != p407SharedConfirmation {
		t.Fatal("P4_07_SHARED_CONFIRM is not set to the exact shared-staging confirmation")
	}
	if strings.TrimSpace(os.Getenv(actionName)) != expectedAction {
		t.Fatalf("%s is not set to the exact P4-07 shared action confirmation", actionName)
	}
	for _, name := range p407SharedStaleConfirmations {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-07 shared gate refuses stale or disposable confirmation %s", name)
		}
	}
	for _, name := range p407SharedActionConfirmations {
		if name != actionName && strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-07 shared gate refuses conflicting action confirmation %s", name)
		}
	}
}

func assertP407SharedSideEffectsClean(
	t *testing.T,
	ctx context.Context,
	ownerTx pgx.Tx,
) {
	t.Helper()
	counts := make([]int64, 6)
	if err := ownerTx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_room_role_assignments),
    (SELECT count(*) FROM tutorhub.media_space_mutation_receipts
      WHERE operation IN (
        'lock', 'unlock', 'participant_promote', 'participant_demote',
        'participant_mute', 'participant_remove'
      )),
    (SELECT count(*) FROM tutorhub.rate_limit_windows
      WHERE purpose LIKE 'media.moderation.%'),
    (SELECT count(*) FROM tutorhub.outbox_events
      WHERE event_type IN (
        'media_space.locked.v1', 'media_space.unlocked.v1',
        'media_participant.promoted.v1', 'media_participant.demoted.v1',
        'media_participant.muted.v1', 'media_participant.removed.v1'
      )),
    (SELECT count(*) FROM tutorhub.audit_events
      WHERE action IN (
        'media_space.lock', 'media_space.unlock',
        'media_participant.promote', 'media_participant.demote',
        'media_participant.mute', 'media_participant.remove'
      )),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
      WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms')
        AND enabled)`).Scan(
		&counts[0],
		&counts[1],
		&counts[2],
		&counts[3],
		&counts[4],
		&counts[5],
	); err != nil {
		t.Fatal("inspect P4-07 shared side-effect boundary")
	}
	for _, count := range counts {
		if count != 0 {
			t.Fatal("P4-07 shared staging retained a moderation or feature-enable side effect")
		}
	}
	t.Log("P4_07_SHARED_FINAL_SNAPSHOT PASS ledger=33 dirty=false media_features=false moderation_side_effects=0")
}
