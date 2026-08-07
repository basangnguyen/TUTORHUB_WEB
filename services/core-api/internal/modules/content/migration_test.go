package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentFileMigrationLocksTenantLifecycleQuotaAndPrivacy(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000026_content_files.up.sql",
	))
	if err != nil {
		t.Fatalf("read content file migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{
		"'file_uploads'", "DROP CONSTRAINT tenant_feature_overrides_key_valid",
		"'files_per_tenant'", "'file_bytes_per_tenant'", "'single_file_bytes'",
		"'file_upload_intents_per_hour'", "CREATE TABLE tutorhub.content_files",
		"CREATE TABLE tutorhub.tenant_file_usage", "FOREIGN KEY (tenant_id, class_id)",
		"FOREIGN KEY (tenant_id, creator_user_id)",
		"UNIQUE (tenant_id, creator_user_id, client_request_id)",
		"UNIQUE (object_key)", "octet_length(request_fingerprint) = 32",
		"display_name !~ '[[:cntrl:]]'",
		"octet_length(expected_checksum_sha256) = 32",
		"status IN ('pending', 'uploaded', 'processing', 'ready', 'rejected')",
		"stored_checksum_sha256 = expected_checksum_sha256",
		"content_files_pending_expiry_idx", "reserved_bytes >= 0",
		"committed_bytes >= 0", "REVOKE ALL ON tutorhub.content_files FROM PUBLIC",
		"REVOKE ALL ON tutorhub.tenant_file_usage FROM PUBLIC",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("content migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"GRANT ", "INSERT INTO tutorhub.audit_events", "INSERT INTO tutorhub.outbox_events",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("content migration must not contain %q", forbidden)
		}
	}
}

func TestContentFileDownMigrationRestoresPersistentMessageQuotaShape(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000026_content_files.down.sql",
	))
	if err != nil {
		t.Fatalf("read content file down migration: %v", err)
	}
	sql := string(contents)
	fileDrop := strings.Index(sql, "DROP TABLE tutorhub.content_files")
	usageDrop := strings.Index(sql, "DROP TABLE tutorhub.tenant_file_usage")
	if fileDrop < 0 || usageDrop < 0 || fileDrop > usageDrop {
		t.Fatal("content files must be dropped before tenant file usage")
	}
	for _, required := range []string{
		"WHERE feature_key = 'file_uploads'",
		"WHERE quota_key = 'file_upload_intents_per_hour'",
		"'messages_per_tenant'", "'message_sends_per_hour'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("content down migration is missing %q", required)
		}
	}
	overrides := sql[strings.Index(sql, "ADD CONSTRAINT tenant_quota_overrides_key_valid"):]
	for _, removed := range []string{
		"'files_per_tenant'", "'file_bytes_per_tenant'", "'single_file_bytes'",
		"'file_upload_intents_per_hour'",
	} {
		if strings.Contains(overrides, removed) {
			t.Fatalf("content down migration must remove %s", removed)
		}
	}
}
