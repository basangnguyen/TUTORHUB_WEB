package collaboration

import (
	"strings"
	"testing"
)

const whiteboardArtifactMigrationName = "000038_whiteboard_artifact_worker.up.sql"
const whiteboardRestoreStagingMigrationName = "000039_whiteboard_restore_checkpoint_staging.up.sql"
const whiteboardTerminalRestorePurgeMigrationName = "000040_whiteboard_terminal_restore_purge_compatibility.up.sql"

func TestWhiteboardArtifactMigrationBoundsCheckpointAndKeepsContentOutOfCommands(t *testing.T) {
	t.Parallel()

	sql := readWhiteboardMigration(t, whiteboardMigrationName) + "\n" +
		readWhiteboardMigration(t, whiteboardArtifactMigrationName)
	for _, fragment := range []string{
		"CREATE TABLE tutorhub.whiteboard_document_checkpoints",
		"byte_length BETWEEN 1 AND 20971520",
		"octet_length(yjs_state) = byte_length",
		"octet_length(checksum) = 32",
		"CREATE TABLE tutorhub.whiteboard_artifact_commands",
		"CREATE TABLE tutorhub.whiteboard_artifact_purge_queue",
		"UNIQUE (tenant_id, actor_user_id, idempotency_key)",
		"octet_length(request_fingerprint) = 32",
		"command_kind IN ('snapshot', 'export', 'restore', 'import_validate')",
		"status IN ('pending', 'processing', 'succeeded', 'failed', 'quarantined')",
		"attempts BETWEEN 0 AND 5",
		"FOR UPDATE OF snapshot SKIP LOCKED",
		"SECURITY DEFINER",
		"SET search_path = pg_catalog, tutorhub",
		"CREATE FUNCTION tutorhub.claim_whiteboard_snapshot_purge",
		"CREATE FUNCTION tutorhub.complete_whiteboard_snapshot_purge",
		"CREATE FUNCTION tutorhub.fail_whiteboard_snapshot_purge",
		"REVOKE ALL ON tutorhub.whiteboard_artifact_commands FROM PUBLIC",
		"REVOKE ALL ON tutorhub.whiteboard_artifact_purge_queue FROM PUBLIC",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("whiteboard artifact migration is missing %q", fragment)
		}
	}

	normalized := strings.ToLower(stripSQLLineComments(sql))
	for _, forbidden := range []string{
		"document_payload",
		"snapshot_payload",
		"portable_scene",
		"provider_state bytea",
		"operation_payload",
		"awareness_payload",
		" json ",
		" jsonb ",
	} {
		if strings.Contains(normalized, forbidden) {
			t.Fatalf("whiteboard artifact migration stores forbidden content via %q", forbidden)
		}
	}
}

func TestWhiteboardArtifactMigrationUsesOpaqueVersionedB2Binding(t *testing.T) {
	t.Parallel()

	sql := readWhiteboardMigration(t, whiteboardMigrationName) + "\n" +
		readWhiteboardMigration(t, whiteboardArtifactMigrationName)
	for _, fragment := range []string{
		"CREATE FUNCTION tutorhub.fail_whiteboard_snapshot_purge",
		"CREATE FUNCTION tutorhub.complete_whiteboard_snapshot_purge",
		"CREATE FUNCTION tutorhub.claim_whiteboard_snapshot_purge",
		"object_key ~ '^wb/[a-f0-9]{2}/([a-f0-9]{48}|[a-f0-9]{64})$'",
		"object_version_id",
		"verification_key_id",
		"size_bytes BETWEEN 1 AND 33554432",
		"snapshot_kind IN (",
		"'automatic', 'checkpoint', 'manual', 'export', 'pre_restore', 'import'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("whiteboard artifact B2/catalog binding is missing %q", fragment)
		}
	}
}

func TestWhiteboardArtifactDevelopmentDownMigrationIsScoped(t *testing.T) {
	t.Parallel()

	sql := readWhiteboardMigration(t, "000038_whiteboard_artifact_worker.down.sql")
	for _, fragment := range []string{
		"DROP FUNCTION tutorhub.enqueue_whiteboard_snapshot_purge(integer)",
		"DROP TABLE tutorhub.whiteboard_artifact_purge_queue",
		"DROP TABLE tutorhub.whiteboard_artifact_commands",
		"DROP TABLE tutorhub.whiteboard_document_checkpoints",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("whiteboard artifact down migration is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"DROP TABLE tutorhub.whiteboard_snapshots",
		"DROP TABLE tutorhub.whiteboard_documents",
		"DROP SCHEMA tutorhub",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("whiteboard artifact down migration exceeds its scope via %q", forbidden)
		}
	}
}

func TestWhiteboardRestoreCheckpointCanStageBeforeGenerationSwap(t *testing.T) {
	t.Parallel()

	up := readWhiteboardMigration(t, whiteboardRestoreStagingMigrationName)
	if !strings.Contains(up, "DROP CONSTRAINT whiteboard_document_checkpoints_generation_fk") {
		t.Fatal("whiteboard restore staging migration must remove the premature generation foreign key")
	}
	for _, forbidden := range []string{
		"DROP TABLE tutorhub.whiteboard_document_checkpoints",
		"DROP TABLE tutorhub.whiteboard_document_generations",
		"DROP SCHEMA tutorhub",
	} {
		if strings.Contains(up, forbidden) {
			t.Fatalf("whiteboard restore staging migration exceeds its scope via %q", forbidden)
		}
	}

	down := readWhiteboardMigration(t, "000039_whiteboard_restore_checkpoint_staging.down.sql")
	for _, fragment := range []string{
		"DELETE FROM tutorhub.whiteboard_document_checkpoints AS checkpoint",
		"ADD CONSTRAINT whiteboard_document_checkpoints_generation_fk",
		"ON DELETE CASCADE",
	} {
		if !strings.Contains(down, fragment) {
			t.Fatalf("whiteboard restore staging down migration is missing %q", fragment)
		}
	}
}

func TestTerminalRestoreAllowsSourceArtifactPurge(t *testing.T) {
	t.Parallel()

	up := readWhiteboardMigration(t, whiteboardTerminalRestorePurgeMigrationName)
	for _, fragment := range []string{
		"DROP CONSTRAINT whiteboard_artifact_commands_restore_shape",
		"source_snapshot_id IS NOT NULL",
		"status IN ('succeeded', 'failed', 'quarantined')",
		"target_generation IS NOT NULL",
		"target_provider_document_name IS NOT NULL",
	} {
		if !strings.Contains(up, fragment) {
			t.Fatalf("terminal restore purge migration is missing %q", fragment)
		}
	}

	down := readWhiteboardMigration(t, "000040_whiteboard_terminal_restore_purge_compatibility.down.sql")
	for _, fragment := range []string{
		"DELETE FROM tutorhub.whiteboard_artifact_commands",
		"command_kind = 'restore'",
		"source_snapshot_id IS NULL",
		"ADD CONSTRAINT whiteboard_artifact_commands_restore_shape",
	} {
		if !strings.Contains(down, fragment) {
			t.Fatalf("terminal restore purge down migration is missing %q", fragment)
		}
	}
}
