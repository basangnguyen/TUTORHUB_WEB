package classroom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassSessionMigrationDefinesBoundedTenantScopedTable(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000014_class_sessions.up.sql",
	))
	if err != nil {
		t.Fatalf("read class session migration: %v", err)
	}
	sql := string(contents)
	for _, required := range []string{
		"CREATE TABLE tutorhub.class_sessions",
		"starts_at timestamptz",
		"ends_at timestamptz",
		"timezone text",
		"class_sessions_class_fk",
		"version bigint",
		"class_session_scheduling",
		"REVOKE ALL ON tutorhub.class_sessions FROM PUBLIC",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("migration is missing %q", required)
		}
	}
}
