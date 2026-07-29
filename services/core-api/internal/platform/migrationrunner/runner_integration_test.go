//go:build integration

package migrationrunner

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/tutorhub-v2/core-api/migrations"
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
	latestVersion := latestEmbeddedMigrationVersion(t)
	if version.Number != latestVersion || version.Dirty {
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
	assertOutboxWorkerSchema(t, ctx, database, true)
	assertOutboxWriterCompatibility(t, ctx, database)
}

func TestOutboxWorkerMigrationRollbackGuardAndCompatibility(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	if databaseURL == "" {
		t.Fatal("DATABASE_MIGRATION_URL is required for integration tests")
	}
	requireDisposableMigrationDatabase(t, databaseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("apply migrations before destructive compatibility test: %v", err)
	}
	latestVersion := latestEmbeddedMigrationVersion(t)
	if latestVersion <= outboxWorkerMigrationVersion {
		t.Fatalf(
			"latest migration version %d must be newer than outbox worker migration %d",
			latestVersion,
			outboxWorkerMigrationVersion,
		)
	}

	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := Up(cleanupContext, databaseURL); err != nil {
			t.Errorf("restore latest migrations after destructive compatibility test: %v", err)
		}
	})

	if err := Down(
		ctx,
		databaseURL,
		int(latestVersion-outboxWorkerMigrationVersion),
	); err != nil {
		t.Fatalf(
			"roll back latest migrations to outbox worker migration %d: %v",
			outboxWorkerMigrationVersion,
			err,
		)
	}
	version, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read outbox worker migration version: %v", err)
	}
	if version.Number != outboxWorkerMigrationVersion || version.Dirty {
		t.Fatalf("unexpected outbox worker migration version: %+v", version)
	}

	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	defer database.Close()

	assertOutboxWorkerSchema(t, ctx, database, true)
	assertOutboxWriterCompatibility(t, ctx, database)
	blockedRollbackEventID := insertBlockedOutboxRollbackFixture(t, ctx, database)
	if err := Down(ctx, databaseURL, 1); !errors.Is(err, ErrOutboxWorkerRollbackBlocked) {
		t.Fatalf("guarded outbox worker rollback error = %v", err)
	}
	guardedVersion, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read version after guarded rollback: %v", err)
	}
	if guardedVersion.Number != outboxWorkerMigrationVersion || guardedVersion.Dirty {
		t.Fatalf("guarded rollback dirtied migration history: %+v", guardedVersion)
	}

	if _, err := database.ExecContext(
		ctx,
		`DELETE FROM tutorhub.outbox_events WHERE id = $1::uuid`,
		blockedRollbackEventID,
	); err != nil {
		t.Fatalf("delete guarded rollback fixture: %v", err)
	}

	if err := Down(ctx, databaseURL, 1); err != nil {
		t.Fatalf("roll back outbox worker migration: %v", err)
	}
	rolledBackVersion, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read rolled-back migration version: %v", err)
	}
	if rolledBackVersion.Number != outboxWorkerMigrationVersion-1 || rolledBackVersion.Dirty {
		t.Fatalf("unexpected rolled-back migration version: %+v", rolledBackVersion)
	}
	assertLegacyImportTables(t, ctx, database, true)
	assertClassSessionTable(t, ctx, database, true)
	assertOutboxWorkerSchema(t, ctx, database, false)
	legacyOutboxEventID := insertLegacyOutboxError(t, ctx, database)

	if err := migrateToVersion(ctx, databaseURL, outboxWorkerMigrationVersion); err != nil {
		t.Fatalf("reapply outbox worker migration over legacy error text: %v", err)
	}
	legacyReappliedVersion, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read legacy-error reapplied migration version: %v", err)
	}
	if legacyReappliedVersion.Number != outboxWorkerMigrationVersion ||
		legacyReappliedVersion.Dirty {
		t.Fatalf("unexpected legacy-error reapplied version: %+v", legacyReappliedVersion)
	}
	assertLegacyOutboxErrorRedacted(t, ctx, database, legacyOutboxEventID)
	if _, err := database.ExecContext(
		ctx,
		`DELETE FROM tutorhub.outbox_events WHERE id = $1::uuid`,
		legacyOutboxEventID,
	); err != nil {
		t.Fatalf("delete legacy outbox compatibility fixture: %v", err)
	}

	if err := Down(ctx, databaseURL, 1); err != nil {
		t.Fatalf("roll back reapplied outbox worker migration: %v", err)
	}
	legacyCheckVersion, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read post-legacy-check migration version: %v", err)
	}
	if legacyCheckVersion.Number != outboxWorkerMigrationVersion-1 || legacyCheckVersion.Dirty {
		t.Fatalf("unexpected post-legacy-check migration version: %+v", legacyCheckVersion)
	}
	assertOutboxWorkerSchema(t, ctx, database, false)

	if err := Down(ctx, databaseURL, 1); err != nil {
		t.Fatalf("roll back class session migration: %v", err)
	}
	classSessionRolledBackVersion, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read class-session rolled-back migration version: %v", err)
	}
	if classSessionRolledBackVersion.Number != outboxWorkerMigrationVersion-2 ||
		classSessionRolledBackVersion.Dirty {
		t.Fatalf(
			"unexpected class-session rolled-back migration version: %+v",
			classSessionRolledBackVersion,
		)
	}
	assertLegacyImportTables(t, ctx, database, true)
	assertClassSessionTable(t, ctx, database, false)
	assertOutboxWorkerSchema(t, ctx, database, false)

	if err := Up(ctx, databaseURL); err != nil {
		t.Fatalf("reapply class session and outbox worker migrations: %v", err)
	}
	reappliedVersion, err := CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read reapplied migration version: %v", err)
	}
	if reappliedVersion.Number != latestVersion || reappliedVersion.Dirty {
		t.Fatalf("unexpected reapplied migration version: %+v", reappliedVersion)
	}
	assertLegacyImportTables(t, ctx, database, true)
	assertClassSessionTable(t, ctx, database, true)
	assertOutboxWorkerSchema(t, ctx, database, true)
	assertOutboxWriterCompatibility(t, ctx, database)
}

func latestEmbeddedMigrationVersion(t *testing.T) uint {
	t.Helper()

	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	var latest uint64
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		versionText, _, found := strings.Cut(name, "_")
		if !found {
			t.Fatalf("migration filename has no version prefix: %q", name)
		}
		version, err := strconv.ParseUint(versionText, 10, 32)
		if err != nil {
			t.Fatalf("parse migration version from %q: %v", name, err)
		}
		if version > latest {
			latest = version
		}
	}
	if latest == 0 {
		t.Fatal("no embedded up migrations found")
	}
	return uint(latest)
}

func requireDisposableMigrationDatabase(t *testing.T, databaseURL string) {
	t.Helper()

	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse DATABASE_MIGRATION_URL for destructive-test safety: %v", err)
	}
	host := strings.ToLower(parsed.Hostname())
	ip := net.ParseIP(host)
	isLoopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	databaseName := strings.ToLower(strings.Trim(parsed.Path, "/"))
	isTestDatabase := databaseName == "test" ||
		strings.HasPrefix(databaseName, "test_") ||
		strings.HasSuffix(databaseName, "_test") ||
		strings.Contains(databaseName, "_test_")
	if !isLoopback || !isTestDatabase {
		t.Skipf(
			"destructive migration compatibility test requires a loopback disposable test database; host=%q database=%q",
			host,
			databaseName,
		)
	}
}

func migrateToVersion(ctx context.Context, databaseURL string, version uint) error {
	return execute(ctx, databaseURL, func(instance *migrate.Migrate, _ *sql.DB) error {
		return instance.Migrate(version)
	})
}

func insertBlockedOutboxRollbackFixture(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
) string {
	t.Helper()

	var eventID string
	if err := database.QueryRowContext(ctx, `
INSERT INTO tutorhub.outbox_events (
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    attempts,
    last_error,
    dead_lettered_at
)
VALUES (
    'migration_probe',
    gen_random_uuid(),
    'migration.rollback_guard.v1',
    '{}'::jsonb,
    1,
    'rollback_guard_probe',
    clock_timestamp()
)
RETURNING id::text`).Scan(&eventID); err != nil {
		t.Fatalf("insert guarded rollback fixture: %v", err)
	}
	return eventID
}

func assertOutboxWorkerSchema(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	expected bool,
) {
	t.Helper()

	var columnCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM information_schema.columns
WHERE table_schema = 'tutorhub'
  AND table_name = 'outbox_events'
  AND column_name IN (
      'lease_owner',
      'lease_token',
      'leased_at',
      'leased_until',
      'dead_lettered_at'
  )`).Scan(&columnCount); err != nil {
		t.Fatalf("inspect outbox worker columns: %v", err)
	}
	var constraintCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM pg_constraint constraint_definition
JOIN pg_class table_definition
  ON table_definition.oid = constraint_definition.conrelid
JOIN pg_namespace schema_definition
  ON schema_definition.oid = table_definition.relnamespace
WHERE schema_definition.nspname = 'tutorhub'
  AND table_definition.relname = 'outbox_events'
  AND constraint_definition.conname IN (
      'outbox_lease_token_non_negative',
      'outbox_lease_state_valid',
      'outbox_terminal_state_exclusive',
      'outbox_terminal_has_no_lease',
      'outbox_last_error_code_valid',
      'outbox_dead_letter_state_valid'
  )`).Scan(&constraintCount); err != nil {
		t.Fatalf("inspect outbox worker constraints: %v", err)
	}

	var readyClaim, expiredLeaseClaim, pendingAge, deadLettered, legacyPending sql.NullString
	if err := database.QueryRowContext(ctx, `
SELECT to_regclass('tutorhub.outbox_ready_claim_idx'),
       to_regclass('tutorhub.outbox_expired_lease_claim_idx'),
       to_regclass('tutorhub.outbox_pending_age_idx'),
       to_regclass('tutorhub.outbox_dead_lettered_idx'),
       to_regclass('tutorhub.outbox_pending_idx')`).Scan(
		&readyClaim,
		&expiredLeaseClaim,
		&pendingAge,
		&deadLettered,
		&legacyPending,
	); err != nil {
		t.Fatalf("inspect outbox worker indexes: %v", err)
	}

	if expected {
		if columnCount != 5 || constraintCount != 6 || !readyClaim.Valid || !expiredLeaseClaim.Valid ||
			!pendingAge.Valid || !deadLettered.Valid || legacyPending.Valid {
			t.Fatalf(
				"unexpected migrated outbox schema: columns=%d constraints=%d ready=%t expired=%t age=%t dead=%t legacy=%t",
				columnCount,
				constraintCount,
				readyClaim.Valid,
				expiredLeaseClaim.Valid,
				pendingAge.Valid,
				deadLettered.Valid,
				legacyPending.Valid,
			)
		}
	} else if columnCount != 0 || constraintCount != 0 || readyClaim.Valid || expiredLeaseClaim.Valid ||
		pendingAge.Valid || deadLettered.Valid || !legacyPending.Valid {
		t.Fatalf(
			"unexpected rolled-back outbox schema: columns=%d constraints=%d ready=%t expired=%t age=%t dead=%t legacy=%t",
			columnCount,
			constraintCount,
			readyClaim.Valid,
			expiredLeaseClaim.Valid,
			pendingAge.Valid,
			deadLettered.Valid,
			legacyPending.Valid,
		)
	}

	var tenantNullable string
	if err := database.QueryRowContext(ctx, `
SELECT is_nullable
FROM information_schema.columns
WHERE table_schema = 'tutorhub'
  AND table_name = 'outbox_events'
  AND column_name = 'tenant_id'`).Scan(&tenantNullable); err != nil {
		t.Fatalf("inspect outbox tenant nullability: %v", err)
	}
	if tenantNullable != "YES" {
		t.Fatalf("outbox tenant_id must remain nullable, got %q", tenantNullable)
	}
}

func insertLegacyOutboxError(t *testing.T, ctx context.Context, database *sql.DB) string {
	t.Helper()

	var eventID string
	if err := database.QueryRowContext(ctx, `
INSERT INTO tutorhub.outbox_events (
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    attempts,
    last_error
)
VALUES (
    'migration_probe',
    gen_random_uuid(),
    'migration.legacy_error.v1',
    '{}'::jsonb,
    1,
    'legacy free-form error that must not survive migration'
)
RETURNING id::text`).Scan(&eventID); err != nil {
		t.Fatalf("insert legacy outbox error fixture: %v", err)
	}
	return eventID
}

func assertLegacyOutboxErrorRedacted(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	eventID string,
) {
	t.Helper()

	var lastError string
	if err := database.QueryRowContext(ctx, `
SELECT last_error
FROM tutorhub.outbox_events
WHERE id = $1::uuid`, eventID).Scan(&lastError); err != nil {
		t.Fatalf("read migrated legacy outbox error: %v", err)
	}
	if lastError != "legacy_error_redacted" {
		t.Fatalf("legacy outbox error was not redacted: %q", lastError)
	}
}

func assertOutboxWriterCompatibility(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin outbox compatibility transaction: %v", err)
	}
	defer transaction.Rollback()

	var tenantID, leaseOwner, leasedAt, leasedUntil, deadLetteredAt sql.NullString
	var leaseToken int64
	if err := transaction.QueryRowContext(ctx, `
INSERT INTO tutorhub.outbox_events (
    aggregate_type,
    aggregate_id,
    event_type,
    payload
)
VALUES ('migration_probe', gen_random_uuid(), 'migration.probe.v1', '{}'::jsonb)
RETURNING tenant_id::text,
          lease_owner::text,
          lease_token,
          leased_at::text,
          leased_until::text,
          dead_lettered_at::text`).Scan(
		&tenantID,
		&leaseOwner,
		&leaseToken,
		&leasedAt,
		&leasedUntil,
		&deadLetteredAt,
	); err != nil {
		t.Fatalf("insert outbox event through the pre-worker writer shape: %v", err)
	}
	if tenantID.Valid || leaseOwner.Valid || leaseToken != 0 || leasedAt.Valid ||
		leasedUntil.Valid || deadLetteredAt.Valid {
		t.Fatalf(
			"unexpected new outbox defaults: tenant=%v owner=%v token=%d leased_at=%v leased_until=%v dead=%v",
			tenantID,
			leaseOwner,
			leaseToken,
			leasedAt,
			leasedUntil,
			deadLetteredAt,
		)
	}
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
