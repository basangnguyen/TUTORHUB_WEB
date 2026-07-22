//go:build integration

package v1import

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func connectCleanup(t *testing.T, ctx context.Context, databaseURL string, sourceSystem string) func() {
	t.Helper()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect cleanup database: %v", err)
	}
	return func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		defer connection.Close(cleanupCtx)
		statements := []string{
			`DELETE FROM tutorhub.classes WHERE id IN (SELECT target_id FROM tutorhub.legacy_import_mappings WHERE source_system = $1 AND entity_type = 'class')`,
			`DELETE FROM tutorhub.memberships WHERE id IN (SELECT target_id FROM tutorhub.legacy_import_mappings WHERE source_system = $1 AND entity_type = 'membership')`,
			`DELETE FROM tutorhub.tenants WHERE id IN (SELECT target_id FROM tutorhub.legacy_import_mappings WHERE source_system = $1 AND entity_type = 'tenant')`,
			`DELETE FROM tutorhub.users WHERE id IN (SELECT target_id FROM tutorhub.legacy_import_mappings WHERE source_system = $1 AND entity_type = 'user')`,
			`DELETE FROM tutorhub.legacy_import_mappings WHERE source_system = $1`,
			`DELETE FROM tutorhub.legacy_import_runs WHERE source_system = $1`,
		}
		for _, statement := range statements {
			if _, err := connection.Exec(cleanupCtx, statement, sourceSystem); err != nil {
				t.Errorf("cleanup V1 fixture rows: %v", err)
				break
			}
		}
	}
}

func countImportedState(t *testing.T, ctx context.Context, databaseURL string, sourceSystem string) int {
	t.Helper()
	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect count database: %v", err)
	}
	defer connection.Close(context.Background())
	var count int
	if err := connection.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.legacy_import_mappings WHERE source_system = $1)
  + (SELECT count(*) FROM tutorhub.legacy_import_runs WHERE source_system = $1)
  + (SELECT count(*) FROM tutorhub.legacy_import_run_items item
     JOIN tutorhub.legacy_import_runs run ON run.id = item.run_id
     WHERE run.source_system = $1)`, sourceSystem).Scan(&count); err != nil {
		t.Fatalf("count imported state: %v", err)
	}
	return count
}
