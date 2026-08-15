//go:build integration

package media

import (
	"os"
	"strings"
	"testing"
)

const (
	p410SharedConfirmation               = "I_UNDERSTAND_P4_10_SHARED_STAGING_ONLY"
	p410SharedOwnerPreflightConfirmation = "I_UNDERSTAND_P4_10_SHARED_OWNER_PREFLIGHT_READ_ONLY"
	p410SharedMigrationConfirmation      = "I_UNDERSTAND_P4_10_FORWARD_SHARED_STAGING_ONLY"
	p410SharedACLProvisionConfirmation   = "I_UNDERSTAND_P4_10_ACL_PROVISION_SHARED_STAGING_ONLY"
	p410SharedFinalSnapshotConfirmation  = "I_UNDERSTAND_P4_10_SHARED_FINAL_SNAPSHOT_READ_ONLY"
)

var p410SharedActions = map[string]string{
	"P4_10_SHARED_OWNER_PREFLIGHT":       p410SharedOwnerPreflightConfirmation,
	"P4_10_SHARED_MIGRATION_CONFIRM":     p410SharedMigrationConfirmation,
	"P4_10_SHARED_ACL_PROVISION_CONFIRM": p410SharedACLProvisionConfirmation,
	"P4_10_SHARED_FINAL_CONFIRM":         p410SharedFinalSnapshotConfirmation,
}

func TestPostgresP410SharedOwnerPreflight(t *testing.T) {
	requireP410SharedConfirmation(t, "P4_10_SHARED_OWNER_PREFLIGHT")
	runPostgresMediaReadOnlySnapshot(t, "P4_10_SHARED", 35, false, nil)
}

func TestPostgresP410SharedForwardMigration(t *testing.T) {
	requireP410SharedConfirmation(t, "P4_10_SHARED_MIGRATION_CONFIRM")
	runP410ForwardMigration(t)
}

func TestPostgresP410SharedFinalSnapshot(t *testing.T) {
	requireP410SharedConfirmation(t, "P4_10_SHARED_FINAL_CONFIRM")
	runPostgresMediaReadOnlySnapshot(t, "P4_10_SHARED", 36, true, logP410FinalSnapshot)
}

func requireP410SharedConfirmation(t *testing.T, activeName string) {
	t.Helper()
	expected, ok := p410SharedActions[activeName]
	if !ok {
		t.Fatal("P4-10 shared gate received an unknown action")
	}
	if strings.TrimSpace(os.Getenv("P4_10_SHARED_CONFIRM")) != p410SharedConfirmation {
		t.Fatal("P4_10_SHARED_CONFIRM is not set to the exact shared-staging confirmation")
	}
	if strings.TrimSpace(os.Getenv(activeName)) != expected {
		t.Fatalf("%s is not set to the exact P4-10 shared action confirmation", activeName)
	}
	for name := range p410SharedActions {
		if name != activeName && strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-10 shared gate refuses conflicting action confirmation %s", name)
		}
	}
	for _, name := range []string{
		"P4_10_DISPOSABLE_CONFIRM",
		"P4_10_ACL_PROVISION_CONFIRM",
		"P4_10_FINAL_SNAPSHOT_CONFIRM",
		"P4_09_DISPOSABLE_CONFIRM",
		"P4_09_SHARED_CONFIRM",
		"P4_09_SHARED_OWNER_PREFLIGHT",
		"P4_09_SHARED_MIGRATION_CONFIRM",
		"P4_09_SHARED_ACL_PROVISION_CONFIRM",
		"P4_09_SHARED_FINAL_CONFIRM",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-10 shared gate refuses stale confirmation %s", name)
		}
	}
}
