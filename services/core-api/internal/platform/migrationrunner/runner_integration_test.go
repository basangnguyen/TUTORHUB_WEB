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
	if version.Number < 8 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}

	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatalf("open migration database: %v", err)
	}
	defer database.Close()

	var publicHistory, applicationHistory, invitationTable sql.NullString
	if err := database.QueryRowContext(
		ctx,
		`SELECT to_regclass('public.tutorhub_schema_migrations'),
                to_regclass('tutorhub.tutorhub_schema_migrations'),
                to_regclass('tutorhub.membership_invitations')`,
	).Scan(&publicHistory, &applicationHistory, &invitationTable); err != nil {
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
}
