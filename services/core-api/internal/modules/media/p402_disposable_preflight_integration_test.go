//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

const (
	p402OwnerPreflightConfirmation = "I_UNDERSTAND_P4_02_OWNER_PREFLIGHT_ONLY"
	p402SharedConfirmation         = "I_UNDERSTAND_P4_02_SHARED_STAGING_ONLY"
)

func TestPostgresP402DisposableOwnerPreflight(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_02_DISPOSABLE_CONFIRM")) != p402DisposableConfirmation {
		t.Skip("P4_02_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	requireP402OwnerPreflightConfirmation(t)
	runPostgresP402OwnerPreflight(t)
}

func TestPostgresP402SharedOwnerPreflight(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_02_SHARED_CONFIRM")) != p402SharedConfirmation {
		t.Skip("P4_02_SHARED_CONFIRM is not set to the shared-staging confirmation")
	}
	requireP402OwnerPreflightConfirmation(t)
	runPostgresP402OwnerPreflight(t)
}

func requireP402OwnerPreflightConfirmation(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_02_OWNER_PREFLIGHT")) != p402OwnerPreflightConfirmation {
		t.Fatal("P4_02_OWNER_PREFLIGHT is not set to the owner-preflight confirmation")
	}
}

func runPostgresP402OwnerPreflight(t *testing.T) {
	t.Helper()
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)

	var migrationRole string
	var runtimeRole string
	if err := migrationPool.QueryRow(ctx, `SELECT current_user`).Scan(&migrationRole); err != nil {
		t.Fatal("read P4-02 migration identity")
	}
	if err := runtimePool.QueryRow(ctx, `SELECT current_user`).Scan(&runtimeRole); err != nil {
		t.Fatal("read P4-02 runtime identity")
	}
	if migrationRole == runtimeRole {
		t.Fatal("P4-02 owner preflight requires distinct migration and runtime roles")
	}

	var notSuperuser bool
	var schemaUsage bool
	var schemaCreate bool
	var requiredRelationsPresent bool
	var ownerAuthority bool
	if err := migrationPool.QueryRow(ctx, `WITH required_relations(relname) AS (
    VALUES
        ('media_spaces'),
        ('media_room_instances'),
        ('media_participant_sessions')
),
actor AS (
    SELECT oid, rolsuper
    FROM pg_roles
    WHERE rolname = current_user
),
owned_scope AS (
    SELECT
        count(*) AS relation_count,
        bool_and(pg_has_role(actor.oid, relation.relowner, 'USAGE')) AS owner_authority
    FROM required_relations
    JOIN pg_class AS relation ON relation.relname = required_relations.relname
    JOIN pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
     AND namespace.nspname = 'tutorhub'
    CROSS JOIN actor
)
SELECT
    NOT actor.rolsuper,
    has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
    has_schema_privilege(current_user, 'tutorhub', 'CREATE'),
    owned_scope.relation_count = 3,
    COALESCE(owned_scope.owner_authority, false)
FROM actor
CROSS JOIN owned_scope`).Scan(
		&notSuperuser,
		&schemaUsage,
		&schemaCreate,
		&requiredRelationsPresent,
		&ownerAuthority,
	); err != nil {
		t.Fatal("run P4-02 owner authority preflight")
	}
	if !notSuperuser || !schemaUsage || !schemaCreate ||
		!requiredRelationsPresent || !ownerAuthority {
		t.Fatalf(
			"P4-02 owner preflight booleans = %t/%t/%t/%t/%t, want all true",
			notSuperuser,
			schemaUsage,
			schemaCreate,
			requiredRelationsPresent,
			ownerAuthority,
		)
	}

	var version int
	var dirty bool
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("read P4-02 migration ledger")
	}
	if version != 29 || dirty {
		t.Fatalf("P4-02 owner preflight ledger = %d/%t, want 29/false", version, dirty)
	}
}
