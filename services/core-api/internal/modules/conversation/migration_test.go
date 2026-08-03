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
