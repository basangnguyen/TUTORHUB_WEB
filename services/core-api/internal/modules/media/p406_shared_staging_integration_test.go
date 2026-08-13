//go:build integration

package media

import (
	"os"
	"strings"
	"testing"
)

const (
	p406SharedConfirmation               = "I_UNDERSTAND_P4_06_SHARED_STAGING_ONLY"
	p406SharedOwnerPreflightConfirmation = "I_UNDERSTAND_P4_06_SHARED_OWNER_PREFLIGHT_READ_ONLY"
	p406SharedACLProvisionConfirmation   = "I_UNDERSTAND_P4_06_ACL_PROVISION_SHARED_STAGING_ONLY"
	p406SharedFinalSnapshotConfirmation  = "I_UNDERSTAND_P4_06_SHARED_FINAL_SNAPSHOT_READ_ONLY"
)

var p406SharedStaleConfirmations = []string{
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
}

var p406SharedActionConfirmations = []string{
	"P4_06_SHARED_OWNER_PREFLIGHT",
	"P4_06_SHARED_ACL_PROVISION_CONFIRM",
	"P4_06_SHARED_FINAL_SNAPSHOT_CONFIRM",
}

// TestPostgresP406SharedOwnerPreflight authenticates the three shared-staging
// principals and verifies the pre-migration ledger using read-only transactions.
func TestPostgresP406SharedOwnerPreflight(t *testing.T) {
	requireP406SharedConfirmation(
		t,
		"P4_06_SHARED_OWNER_PREFLIGHT",
		p406SharedOwnerPreflightConfirmation,
	)
	runPostgresP406ReadOnlySnapshot(t, 31, false)
}

// TestPostgresP406SharedFinalSnapshot proves the post-migration ledger, exact
// ACL, feature-off guardrail and zero P4-06 side effects without mutating shared
// staging.
func TestPostgresP406SharedFinalSnapshot(t *testing.T) {
	requireP406SharedConfirmation(
		t,
		"P4_06_SHARED_FINAL_SNAPSHOT_CONFIRM",
		p406SharedFinalSnapshotConfirmation,
	)
	runPostgresP406ReadOnlySnapshot(t, 32, true)
}

func requireP406SharedConfirmation(t *testing.T, actionName string, expectedAction string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_06_SHARED_CONFIRM")) != p406SharedConfirmation {
		t.Fatal("P4_06_SHARED_CONFIRM is not set to the exact shared-staging confirmation")
	}
	if strings.TrimSpace(os.Getenv(actionName)) != expectedAction {
		t.Fatalf("%s is not set to the exact P4-06 shared action confirmation", actionName)
	}
	for _, name := range p406SharedStaleConfirmations {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-06 shared gate refuses stale or disposable confirmation %s", name)
		}
	}
	for _, name := range p406SharedActionConfirmations {
		if name != actionName && strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-06 shared gate refuses conflicting action confirmation %s", name)
		}
	}
}
