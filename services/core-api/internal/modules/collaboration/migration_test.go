package collaboration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const whiteboardMigrationName = "000037_whiteboard_control_plane.up.sql"

func TestWhiteboardControlPlaneMigrationKeepsExactAuthorityBoundary(t *testing.T) {
	t.Parallel()

	sql := readWhiteboardMigration(t, whiteboardMigrationName)
	for _, table := range []string{
		"whiteboard_documents",
		"whiteboard_document_generations",
		"whiteboard_capability_policies",
		"whiteboard_snapshots",
		"whiteboard_document_mutation_receipts",
	} {
		definition := regexp.MustCompile(
			`(?s)CREATE TABLE tutorhub\.` + regexp.QuoteMeta(table) + ` \((.*?)\n\);`,
		).FindStringSubmatch(sql)
		if len(definition) != 2 {
			t.Fatalf("whiteboard migration is missing table %q", table)
		}
		if !strings.Contains(definition[1], "tenant_id uuid NOT NULL") {
			t.Fatalf("whiteboard table %q is missing tenant_id", table)
		}
		if !strings.Contains(sql, "REVOKE ALL ON tutorhub."+table+" FROM PUBLIC") {
			t.Fatalf("whiteboard table %q does not revoke PUBLIC access", table)
		}
	}

	for _, fragment := range []string{
		"CONSTRAINT whiteboard_documents_source_unique UNIQUE (tenant_id, media_space_id)",
		"FOREIGN KEY (tenant_id, media_space_id)",
		"REFERENCES tutorhub.media_spaces (tenant_id, id)",
		"current_generation bigint NOT NULL DEFAULT 1",
		"revoke_generation bigint NOT NULL DEFAULT 1",
		"FOREIGN KEY (tenant_id, id, current_generation)",
		"DEFERRABLE INITIALLY DEFERRED",
		"PRIMARY KEY (tenant_id, document_id, generation)",
		"authority_kind = 'yjs' AND authority_version = '13.6.27'",
		"provider_kind = 'hocuspocus' AND provider_version = '4.6.0'",
		"capability IN ('view', 'edit', 'present')",
		"octet_length(causal_watermark_sha256) = 32",
		"octet_length(content_sha256) = 32",
		"retention_until = created_at + interval '14 days'",
		"PRIMARY KEY (tenant_id, actor_user_id, idempotency_key)",
		"octet_length(request_fingerprint) = 32",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("whiteboard migration is missing %q", fragment)
		}
	}

	if strings.Contains(strings.ToUpper(sql), "GRANT ") {
		t.Fatal("whiteboard migration must not hardcode an environment role grant")
	}

	normalized := strings.ToLower(stripSQLLineComments(sql))
	for _, forbidden := range []string{
		"whiteboard_operations",
		"whiteboard_history",
		"yjs_updates",
		"document_payload",
		"operation_payload",
		"awareness_payload",
		"undo_stack",
		"redo_stack",
		" json ",
		" jsonb ",
	} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("whiteboard migration crosses the authority boundary with %q", forbidden)
		}
	}
}

func TestWhiteboardSnapshotAndGenerationCatalogRemainImmutableByShape(t *testing.T) {
	t.Parallel()

	sql := readWhiteboardMigration(t, whiteboardMigrationName)
	for _, fragment := range []string{
		"CREATE TABLE tutorhub.whiteboard_document_generations",
		"CREATE TABLE tutorhub.whiteboard_snapshots",
		"CONSTRAINT whiteboard_snapshots_object_key_unique UNIQUE (object_key)",
		"CONSTRAINT whiteboard_generations_restore_snapshot_fk",
		"reason = 'restore' AND generation > 1 AND restored_from_snapshot_id IS NOT NULL",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("whiteboard immutable catalog is missing %q", fragment)
		}
	}

	for _, forbidden := range []string{
		"CREATE TRIGGER",
		"CREATE FUNCTION",
		"ON CONFLICT DO UPDATE",
		"DELETE FROM tutorhub.whiteboard_snapshots",
		"UPDATE tutorhub.whiteboard_snapshots",
		"UPDATE tutorhub.whiteboard_document_generations",
	} {
		if strings.Contains(strings.ToUpper(sql), strings.ToUpper(forbidden)) {
			t.Fatalf("whiteboard immutable catalog has forbidden mutation path %q", forbidden)
		}
	}
}

func TestWhiteboardDevelopmentDownMigrationDoesNotTouchExistingAuthorities(t *testing.T) {
	t.Parallel()

	sql := readWhiteboardMigration(t, "000037_whiteboard_control_plane.down.sql")
	for _, table := range []string{
		"whiteboard_document_mutation_receipts",
		"whiteboard_snapshots",
		"whiteboard_capability_policies",
		"whiteboard_document_generations",
		"whiteboard_documents",
	} {
		if !strings.Contains(sql, "DROP TABLE tutorhub."+table) {
			t.Fatalf("whiteboard development down migration is missing %q", table)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE tutorhub.media_spaces",
		"DROP TABLE tutorhub.memberships",
		"DROP TABLE tutorhub.tenants",
		"DROP SCHEMA tutorhub",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("whiteboard development down migration touches an existing authority via %q", forbidden)
		}
	}
}

func readWhiteboardMigration(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func stripSQLLineComments(sql string) string {
	lines := strings.Split(sql, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
