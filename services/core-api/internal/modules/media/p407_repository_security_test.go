package media

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestP407ModerationReceiptOperationAllowlistIsExact(t *testing.T) {
	t.Parallel()

	sql := readP407Migration(t)
	constraint := regexp.MustCompile(
		`(?s)ADD CONSTRAINT media_space_mutation_receipts_operation_valid CHECK\s*` +
			`\(\s*operation IN\s*\(([^)]*)\)\s*\)`,
	).FindStringSubmatch(sql)
	if len(constraint) != 2 {
		t.Fatal("P4-07 migration lacks one parseable receipt operation constraint")
	}
	matches := regexp.MustCompile(`'([a-z_]+)'`).FindAllStringSubmatch(constraint[1], -1)
	operations := make([]string, 0, len(matches))
	for _, match := range matches {
		operations = append(operations, match[1])
	}
	want := []string{
		"start", "end", "cancel",
		"member_invite", "member_revoke", "member_restore",
		"admission_admit", "admission_deny", "admission_cancel", "admission_restore",
		"lock", "unlock", "participant_promote", "participant_demote",
		"participant_mute", "participant_remove",
	}
	if got := strings.Join(operations, ","); got != strings.Join(want, ",") {
		t.Fatalf("P4-07 receipt operations = %q, want exact %q", got, strings.Join(want, ","))
	}
}

func TestP407ModerationMigrationKeepsProviderAndPrivacyDataOutOfNewSchema(t *testing.T) {
	t.Parallel()

	sql := readP407Migration(t)
	lowerSQL := strings.ToLower(sql)
	for _, forbidden := range []string{
		"provider_room_name",
		"provider_room_sid",
		"provider_participant_identity",
		"participant_identity",
		"track_sid",
		"access_token",
		"refresh_token",
		"session_token",
		"email text",
		"payload json",
		"payload jsonb",
		"raw_error",
		"error_detail",
		"device_label",
		"unmute",
	} {
		if strings.Contains(lowerSQL, forbidden) {
			t.Fatalf("P4-07 migration persists or enables forbidden field/path %q", forbidden)
		}
	}
	upperSQL := strings.ToUpper(sql)
	for _, forbidden := range []string{
		"GRANT ",
		"CREATE ROLE ",
		"ALTER ROLE ",
		"BYPASSRLS",
		"SECURITY DEFINER",
		"EXECUTE ",
		"FORMAT(",
	} {
		if strings.Contains(upperSQL, forbidden) {
			t.Fatalf("P4-07 migration contains forbidden privilege/dynamic SQL expansion %q", forbidden)
		}
	}
}

func TestP407ProviderEffectsAreTargetBoundLeaseableAndFailClosed(t *testing.T) {
	t.Parallel()

	sql := readP407Migration(t)
	for _, fragment := range []string{
		"FOREIGN KEY (\n            tenant_id,\n            space_id,\n            result_room_instance_id,\n            target_participant_session_id\n        )",
		"provider_effect_required = true",
		"provider_effect_status <> 'none'",
		"provider_effect_status = 'applying' AND provider_effect_lease_until IS NOT NULL",
		"provider_effect_status IN ('retryable_failed', 'permanent_failed')",
		"provider_effect_error_code IS NOT NULL",
		"operation NOT IN (\n            'participant_promote'",
		"OR provider_effect_required = true",
		"WHERE provider_effect_required\n      AND provider_effect_status IN ('pending', 'applying', 'retryable_failed')",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("P4-07 provider-effect boundary is missing %q", fragment)
		}
	}
	if strings.Count(sql, "REVOKE ALL ON tutorhub.media_room_role_assignments FROM PUBLIC") != 1 {
		t.Fatal("P4-07 room role assignment must have one explicit PUBLIC revoke")
	}
}

func TestP407SharedHarnessIsFreshFailClosedAndReadOnlyOutsideProvision(t *testing.T) {
	t.Parallel()

	shared := readP407Source(t, "p407_shared_staging_integration_test.go")
	for _, required := range []string{
		"TestPostgresP407SharedOwnerPreflight",
		"TestPostgresP407SharedFinalSnapshot",
		"P4_07_SHARED_CONFIRM",
		"P4_07_SHARED_OWNER_PREFLIGHT",
		"P4_07_SHARED_ACL_PROVISION_CONFIRM",
		"P4_07_SHARED_FINAL_SNAPSHOT_CONFIRM",
		"P4_06_SHARED_CONFIRM",
		"P4_07_DISPOSABLE_CONFIRM",
		"P4_07_PROVIDER_CONFIRM",
		"runPostgresP407ReadOnlySnapshot(t, 32, false)",
		"runPostgresP407ReadOnlySnapshot(t, 33, true, assertP407SharedSideEffectsClean)",
		"moderation_side_effects=0",
	} {
		if !strings.Contains(shared, required) {
			t.Fatalf("P4-07 shared harness is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"migrationrunner",
		".Exec(",
		"INSERT INTO",
		"UPDATE tutorhub",
		"DELETE FROM",
		"TRUNCATE",
		"NewLiveKitRoomProvider",
	} {
		if strings.Contains(shared, forbidden) {
			t.Fatalf("P4-07 shared read-only harness contains forbidden boundary %q", forbidden)
		}
	}
}

func TestP407SharedACLProvisioningUsesFreshExactUnion(t *testing.T) {
	t.Parallel()

	provision := readP407Source(t, "postgres_acl_provision_integration_test.go")
	for _, required := range []string{
		"TestProvisionPostgresMediaModerationExactACLShared",
		"p407SharedACLProvisionConfirmation",
		"requireP407SharedConfirmation(",
		`"P4_07_SHARED_ACL_PROVISION_CONFIRM"`,
		"expectedVersion: 33",
		"expectations:    p407MediaACLExpectations()",
	} {
		if !strings.Contains(provision, required) {
			t.Fatalf("P4-07 shared ACL provisioner is missing %q", required)
		}
	}
}

func TestP407DisposableAndProviderHarnessesRefuseSharedActions(t *testing.T) {
	t.Parallel()

	for _, filename := range []string{
		"p407_disposable_preflight_integration_test.go",
		"p407_livekit_provider_integration_test.go",
	} {
		source := readP407Source(t, filename)
		for _, required := range []string{
			`"P4_07_SHARED_OWNER_PREFLIGHT"`,
			`"P4_07_SHARED_FINAL_SNAPSHOT_CONFIRM"`,
		} {
			if !strings.Contains(source, required) {
				t.Fatalf("%s does not refuse shared action %s", filename, required)
			}
		}
	}
	provider := readP407Source(t, "p407_livekit_provider_integration_test.go")
	for _, databaseEnvironment := range []string{
		"DATABASE_MIGRATION_URL",
		"DATABASE_POOL_URL",
		"DATABASE_POLL_MAINTENANCE_URL",
	} {
		if !strings.Contains(provider, databaseEnvironment) {
			t.Fatalf("P4-07 provider harness does not refuse %s", databaseEnvironment)
		}
	}
}

func readP407Migration(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000033_media_moderation_commands.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func readP407Source(t *testing.T, filename string) string {
	t.Helper()
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
