package featurecontrol

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeatureControlMigrationKeepsTypedNormalizedAndHashedStorage(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000012_tenant_feature_controls.up.sql",
	))
	if err != nil {
		t.Fatalf("read feature control migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{
		"tenant_feature_control_revisions",
		"tenant_feature_overrides",
		"tenant_quota_overrides",
		"tenant_quota_windows",
		"rate_limit_windows",
		"octet_length(bucket_hash) = 32",
		"membership_invitations",
		"class_management",
		"class_invite_links",
		"invite_creations_per_hour",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration is missing %q", required)
		}
	}
	if strings.Contains(sql, "ip_address") || strings.Contains(sql, "remote_addr") {
		t.Fatal("rate-limit migration must not persist raw client addresses")
	}
}
