//go:build integration

package migrationrunner

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestUpPinsMigrationHistoryToPublicSchema(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	if databaseURL == "" {
		t.Fatal("DATABASE_MIGRATION_URL is required for integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number < 14 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}

	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	defer database.Close()

	var publicHistory, applicationHistory, invitationTable sql.NullString
	var classEnrollmentTable, classInviteCodeTable, auditEventTable sql.NullString
	var legacyImportRunsTable, legacyImportMappingsTable, legacyImportItemsTable sql.NullString
	var classSessionsTable sql.NullString
	var classTimezone, classVersion, archivedFromStatus sql.NullString
	if err := database.QueryRowContext(
		ctx,
		`SELECT to_regclass('public.tutorhub_schema_migrations'),
                to_regclass('tutorhub.tutorhub_schema_migrations'),
                to_regclass('tutorhub.membership_invitations'),
                to_regclass('tutorhub.class_enrollments'),
                to_regclass('tutorhub.class_invite_codes'),
                to_regclass('tutorhub.audit_events'),
                to_regclass('tutorhub.legacy_import_runs'),
                to_regclass('tutorhub.legacy_import_mappings'),
                to_regclass('tutorhub.legacy_import_run_items'),
                to_regclass('tutorhub.class_sessions'),
                (
                    SELECT data_type
                    FROM information_schema.columns
                    WHERE table_schema = 'tutorhub'
                      AND table_name = 'classes'
                      AND column_name = 'timezone'
                ),
                (
                    SELECT data_type
                    FROM information_schema.columns
                    WHERE table_schema = 'tutorhub'
                      AND table_name = 'classes'
                      AND column_name = 'version'
                ),
                (
                    SELECT data_type
                    FROM information_schema.columns
                    WHERE table_schema = 'tutorhub'
                      AND table_name = 'classes'
                      AND column_name = 'archived_from_status'
                )`,
	).Scan(
		&publicHistory,
		&applicationHistory,
		&invitationTable,
		&classEnrollmentTable,
		&classInviteCodeTable,
		&auditEventTable,
		&legacyImportRunsTable,
		&legacyImportMappingsTable,
		&legacyImportItemsTable,
		&classSessionsTable,
		&classTimezone,
		&classVersion,
		&archivedFromStatus,
	); err != nil {
		t.Fatalf("inspect migration history tables: %v", err)
	}
	if !publicHistory.Valid {
		t.Fatal("migration history table must exist in the public schema")
	}
	if applicationHistory.Valid {
		t.Fatal("migration history table must not follow the role-named application schema")
	}
	if !invitationTable.Valid {
		t.Fatal("membership invitation migration must be applied at version 8")
	}
	if !classTimezone.Valid || !classVersion.Valid || !archivedFromStatus.Valid {
		t.Fatal("class lifecycle migration must be applied at version 9")
	}
	if !classEnrollmentTable.Valid || !classInviteCodeTable.Valid {
		t.Fatal("class enrollment migration must be applied at version 10")
	}
	if !auditEventTable.Valid {
		t.Fatal("audit events migration must be applied at version 11")
	}
	if !legacyImportRunsTable.Valid || !legacyImportMappingsTable.Valid || !legacyImportItemsTable.Valid {
		t.Fatal("legacy fixture import migration must be applied at version 13")
	}
	if !classSessionsTable.Valid {
		t.Fatal("class session migration must be applied at version 14")
	}

	if err := Down(ctx, databaseURL, 1); err != nil {
		t.Fatalf("roll back class session migration: %v", err)
	}
	rolledBackVersion, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read rolled-back migration version: %v", err)
	}
	if rolledBackVersion.Number != 13 || rolledBackVersion.Dirty {
		t.Fatalf("unexpected rolled-back migration version: %+v", rolledBackVersion)
	}
	assertLegacyImportTables(t, ctx, database, true)
	assertClassSessionTable(t, ctx, database, false)

	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply class session migration: %v", err)
	}
	reappliedVersion, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read reapplied migration version: %v", err)
	}
	if reappliedVersion.Number != 14 || reappliedVersion.Dirty {
		t.Fatalf("unexpected reapplied migration version: %+v", reappliedVersion)
	}
	assertLegacyImportTables(t, ctx, database, true)
	assertClassSessionTable(t, ctx, database, true)
}

func assertClassSessionTable(t *testing.T, ctx context.Context, database *sql.DB, expected bool) {
	t.Helper()
	var table sql.NullString
	if err := database.QueryRowContext(
		ctx, `SELECT to_regclass('tutorhub.class_sessions')`,
	).Scan(&table); err != nil {
		t.Fatalf("inspect class session table: %v", err)
	}
	if table.Valid != expected {
		t.Fatalf("unexpected class session table state: expected=%t actual=%t", expected, table.Valid)
	}
}

func assertLegacyImportTables(t *testing.T, ctx context.Context, database *sql.DB, expected bool) {
	t.Helper()
	var runs, mappings, items sql.NullString
	if err := database.QueryRowContext(ctx, `
SELECT to_regclass('tutorhub.legacy_import_runs'),
       to_regclass('tutorhub.legacy_import_mappings'),
       to_regclass('tutorhub.legacy_import_run_items')`).Scan(&runs, &mappings, &items); err != nil {
		t.Fatalf("inspect legacy fixture import tables: %v", err)
	}
	if runs.Valid != expected || mappings.Valid != expected || items.Valid != expected {
		t.Fatalf(
			"unexpected legacy fixture table state: expected=%t runs=%t mappings=%t items=%t",
			expected,
			runs.Valid,
			mappings.Valid,
			items.Valid,
		)
	}
}
