package conversation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConversationMigrationLocksCanonicalTenantScopedShape(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000024_conversation_core.up.sql",
	))
	if err != nil {
		t.Fatalf("read conversation migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{
		"'conversations'",
		"CREATE TABLE tutorhub.conversations",
		"CREATE TABLE tutorhub.conversation_members",
		"conversations_shape_valid",
		"direct_user_low_id < direct_user_high_id",
		"conversations_direct_pair_unique",
		"WHERE kind = 'direct'",
		"conversations_class_unique",
		"WHERE kind = 'class'",
		"FOREIGN KEY (tenant_id, class_id)",
		"FOREIGN KEY (tenant_id, user_id)",
		"REVOKE ALL ON tutorhub.conversations FROM PUBLIC",
		"REVOKE ALL ON tutorhub.conversation_members FROM PUBLIC",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("conversation migration is missing %q", required)
		}
	}
	if strings.Contains(sql, "GRANT ") {
		t.Fatal("conversation migration must not hardcode an environment-specific runtime role")
	}
}

func TestConversationDownMigrationDropsMembersBeforeConversations(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000024_conversation_core.down.sql",
	))
	if err != nil {
		t.Fatalf("read conversation down migration: %v", err)
	}
	sql := string(contents)
	members := strings.Index(sql, "DROP TABLE tutorhub.conversation_members")
	conversations := strings.Index(sql, "DROP TABLE tutorhub.conversations")
	if members < 0 || conversations < 0 || members > conversations {
		t.Fatal("conversation members must be dropped before their parent table")
	}
	if !strings.Contains(sql, "WHERE feature_key = 'conversations'") {
		t.Fatal("conversation feature override cleanup is missing")
	}
}

func TestPersistentMessageMigrationLocksTenantScopedLifecycleAndReceipts(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000025_persistent_messages.up.sql",
	))
	if err != nil {
		t.Fatalf("read persistent message migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{
		"'messages_per_tenant'",
		"'message_sends_per_hour'",
		"CREATE TABLE tutorhub.messages",
		"CREATE TABLE tutorhub.tenant_message_usage",
		"CREATE TABLE tutorhub.message_receipts",
		"FOREIGN KEY (tenant_id, conversation_id)",
		"REFERENCES tutorhub.conversations (tenant_id, id)",
		"FOREIGN KEY (tenant_id, author_user_id)",
		"FOREIGN KEY (tenant_id, user_id)",
		"messages_client_idempotency_unique",
		"UNIQUE (tenant_id, author_user_id, client_message_id)",
		"messages_sequence_unique",
		"UNIQUE (tenant_id, conversation_id, sequence)",
		"messages_sequence_positive",
		"sequence > 0",
		"messages_request_fingerprint_valid",
		"octet_length(request_fingerprint) = 32",
		"messages_state_valid",
		"state IN ('active', 'deleted')",
		"messages_receipt_marker_unique",
		"UNIQUE (tenant_id, conversation_id, sequence, id)",
		"messages_content_lifecycle_valid",
		"messages_lifecycle_order_valid",
		"char_length(content) BETWEEN 1 AND 4000",
		"octet_length(content) <= 16384",
		"position(E'\\r' IN content) = 0",
		"deleted_at IS NOT NULL",
		"AND content IS NULL",
		"messages_author_rate_idx",
		"tenant_message_usage_tenant_fk",
		"tenant_message_usage_count_valid",
		"message_count >= 0",
		"message_receipts_last_read_message_fk",
		"last_read_sequence",
		"last_read_message_id",
		"message_receipts_user_list_idx",
		"REVOKE ALL ON tutorhub.messages FROM PUBLIC",
		"REVOKE ALL ON tutorhub.tenant_message_usage FROM PUBLIC",
		"REVOKE ALL ON tutorhub.message_receipts FROM PUBLIC",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("persistent message migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"GRANT ",
		"INSERT INTO tutorhub.audit_events",
		"INSERT INTO tutorhub.outbox_events",
		"sender_user_id",
		"last_read_created_at",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("persistent message migration must not contain %q", forbidden)
		}
	}
}

func TestPersistentMessageMigrationExtendsBoundedQuotaConstraints(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000025_persistent_messages.up.sql",
	))
	if err != nil {
		t.Fatalf("read persistent message migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{
		"DROP CONSTRAINT tenant_quota_overrides_key_valid",
		"DROP CONSTRAINT tenant_quota_overrides_limit_valid",
		"DROP CONSTRAINT tenant_quota_windows_key_valid",
		"'active_availability_polls'",
		"'availability_poll_range_days'",
		"'availability_poll_slots'",
		"'availability_poll_participants'",
		"'availability_poll_creations_per_hour'",
		"'availability_poll_capability_creations_per_hour'",
		"'active_study_meetings'",
		"'study_meeting_creations_per_hour'",
		"quota_key = 'messages_per_tenant' AND limit_value BETWEEN 1 AND 10000000",
		"quota_key = 'message_sends_per_hour' AND limit_value BETWEEN 1 AND 100000",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("persistent message quota extension is missing %q", required)
		}
	}

	windowConstraintStart := strings.Index(sql, "ADD CONSTRAINT tenant_quota_windows_key_valid")
	if windowConstraintStart < 0 {
		t.Fatal("tenant quota window key constraint extension is missing")
	}
	windowConstraint := sql[windowConstraintStart:]
	if !strings.Contains(windowConstraint, "'message_sends_per_hour'") {
		t.Fatal("message send hourly quota must be allowed in tenant quota windows")
	}
}

func TestPersistentMessageDownMigrationDropsChildrenAndRestoresQuotaShape(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000025_persistent_messages.down.sql",
	))
	if err != nil {
		t.Fatalf("read persistent message down migration: %v", err)
	}
	sql := string(contents)
	receipts := strings.Index(sql, "DROP TABLE tutorhub.message_receipts")
	messages := strings.Index(sql, "DROP TABLE tutorhub.messages")
	usage := strings.Index(sql, "DROP TABLE tutorhub.tenant_message_usage")
	if receipts < 0 || messages < 0 || receipts > messages {
		t.Fatal("message receipts must be dropped before their parent message table")
	}
	if usage < 0 {
		t.Fatal("persistent message usage counter must be removed by the down migration")
	}

	windowCleanup := strings.Index(sql, "DELETE FROM tutorhub.tenant_quota_windows")
	windowConstraint := strings.Index(sql, "ADD CONSTRAINT tenant_quota_windows_key_valid")
	if windowCleanup < 0 || windowConstraint < 0 || windowCleanup > windowConstraint {
		t.Fatal("message send quota windows must be removed before restoring the key constraint")
	}
	for _, required := range []string{
		"WHERE quota_key = 'message_sends_per_hour'",
		"'messages_per_tenant'",
		"'availability_poll_capability_creations_per_hour'",
		"'study_meeting_creations_per_hour'",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("persistent message down migration is missing %q", required)
		}
	}

	restoredWindows := sql[windowConstraint:]
	if strings.Contains(restoredWindows, "'message_sends_per_hour'") {
		t.Fatal("down migration must remove message send windows from the restored constraint")
	}
	overrideConstraint := strings.Index(sql, "ADD CONSTRAINT tenant_quota_overrides_key_valid")
	if overrideConstraint < 0 {
		t.Fatal("tenant quota override key constraint restoration is missing")
	}
	restoredOverrides := sql[overrideConstraint:]
	if strings.Contains(restoredOverrides, "'messages_per_tenant'") ||
		strings.Contains(restoredOverrides, "'message_sends_per_hour'") {
		t.Fatal("down migration must remove message quota keys from the restored constraints")
	}
}

func TestPersistentRoomConversationMigrationExtendsCanonicalAggregate(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000034_persistent_room_conversations.up.sql",
	))
	if err != nil {
		t.Fatalf("read persistent room conversation migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{
		"ADD COLUMN media_space_id uuid",
		"conversations_media_space_fk",
		"REFERENCES tutorhub.media_spaces (tenant_id, id)",
		"kind = 'room'",
		"conversations_media_space_unique",
		"ON tutorhub.conversations (tenant_id, media_space_id)",
		"REVOKE ALL ON tutorhub.conversations FROM PUBLIC",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("persistent room conversation migration is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"CREATE TABLE tutorhub.media_messages",
		"CREATE TABLE tutorhub.room_messages",
		"GRANT ",
		"provider_room_name",
		"participant_session_id",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("persistent room conversation migration must not contain %q", forbidden)
		}
	}
}

func TestPersistentRoomConversationDownMigrationRestoresTwoKindShape(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000034_persistent_room_conversations.down.sql",
	))
	if err != nil {
		t.Fatalf("read persistent room conversation down migration: %v", err)
	}
	sql := string(contents)
	deleteRooms := strings.Index(sql, "WHERE kind = 'room'")
	dropColumn := strings.Index(sql, "DROP COLUMN media_space_id")
	if deleteRooms < 0 || dropColumn < 0 || deleteRooms > dropColumn {
		t.Fatal("room conversations must be removed before dropping media_space_id")
	}
	restoredShape := sql[dropColumn:]
	if strings.Contains(restoredShape, "kind = 'room'") ||
		!strings.Contains(restoredShape, "kind = 'direct'") ||
		!strings.Contains(restoredShape, "kind = 'class'") {
		t.Fatal("down migration must restore the direct/class-only conversation shape")
	}
}
