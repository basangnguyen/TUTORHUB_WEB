package media

import (
	"os"
	"strings"
	"testing"
)

func TestP410SharedHarnessIsPhaseBoundAndFailClosed(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile("p410_shared_staging_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	aclContents, err := os.ReadFile("postgres_acl_provision_integration_test.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents) + "\n" + string(aclContents)
	for _, required := range []string{
		`TestPostgresP410SharedOwnerPreflight`,
		`TestPostgresP410SharedForwardMigration`,
		`TestProvisionPostgresMediaDiagnosticsExactACLShared`,
		`TestPostgresP410SharedFinalSnapshot`,
		`I_UNDERSTAND_P4_10_SHARED_STAGING_ONLY`,
		`I_UNDERSTAND_P4_10_SHARED_OWNER_PREFLIGHT_READ_ONLY`,
		`I_UNDERSTAND_P4_10_FORWARD_SHARED_STAGING_ONLY`,
		`I_UNDERSTAND_P4_10_ACL_PROVISION_SHARED_STAGING_ONLY`,
		`I_UNDERSTAND_P4_10_SHARED_FINAL_SNAPSHOT_READ_ONLY`,
		`runPostgresMediaReadOnlySnapshot(t, "P4_10_SHARED", 35, false, nil)`,
		`runP410ForwardMigration(t)`,
		`runPostgresMediaReadOnlySnapshot(t, "P4_10_SHARED", 36, true, logP410FinalSnapshot)`,
		`P4_10_DISPOSABLE_CONFIRM`,
		`P4_09_SHARED_CONFIRM`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("P4-10 shared harness is missing safety boundary %q", required)
		}
	}
	if strings.Contains(source, "os.Setenv") {
		t.Fatal("P4-10 shared harness must not synthesize its own confirmation")
	}
}
