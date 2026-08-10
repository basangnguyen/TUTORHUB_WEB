//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	p404OwnerPreflightConfirmation  = "I_UNDERSTAND_P4_04_OWNER_PREFLIGHT_ONLY"
	p404SharedPreflightConfirmation = "I_UNDERSTAND_P4_04_SHARED_STAGING_ONLY"
)

func TestPostgresP404DisposableOwnerPreflight(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_04_DISPOSABLE_CONFIRM")) != p404DisposableConfirmation {
		t.Skip("P4_04_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_04_SHARED_CONFIRM")) == p404SharedPreflightConfirmation {
		t.Fatal("P4-04 disposable and shared confirmations cannot be active together")
	}
	requireP404OwnerPreflightConfirmation(t)
	runPostgresP404OwnerPreflight(t)
}

func TestPostgresP404SharedOwnerPreflight(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_04_SHARED_CONFIRM")) != p404SharedPreflightConfirmation {
		t.Fatal("P4_04_SHARED_CONFIRM is not set to the shared-staging confirmation")
	}
	if p404AnyConflictingSharedConfirmationIsSet() {
		t.Fatal("P4-04 shared owner preflight refuses disposable confirmations")
	}
	requireP404SharedIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	requireP404SharedIntegrationEnvironment(t, "DATABASE_POOL_URL")
	requireP404OwnerPreflightConfirmation(t)
	runPostgresP404OwnerPreflight(t)
}

func p404AnyConflictingSharedConfirmationIsSet() bool {
	return strings.TrimSpace(os.Getenv("P4_04_DISPOSABLE_CONFIRM")) != "" ||
		strings.TrimSpace(os.Getenv("P4_04_ACL_PROVISION_CONFIRM")) != "" ||
		strings.TrimSpace(os.Getenv("P4_02_DISPOSABLE_CONFIRM")) != "" ||
		strings.TrimSpace(os.Getenv("P4_02_SHARED_CONFIRM")) != ""
}

func requireP404SharedIntegrationEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for the P4-04 shared-staging gate", key)
	}
	return value
}

func requireP404OwnerPreflightConfirmation(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_04_OWNER_PREFLIGHT")) != p404OwnerPreflightConfirmation {
		t.Fatal("P4_04_OWNER_PREFLIGHT is not set to the owner-preflight confirmation")
	}
}

func runPostgresP404OwnerPreflight(t *testing.T) {
	t.Helper()
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	requireP404NeonURLBoundary(t, migrationURL, runtimeURL)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)

	var migrationRole, migrationDatabase string
	if err := migrationPool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&migrationRole,
		&migrationDatabase,
	); err != nil {
		t.Fatal("inspect P4-04 migration database identity")
	}
	var runtimeRole, runtimeDatabase string
	if err := runtimePool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&runtimeRole,
		&runtimeDatabase,
	); err != nil {
		t.Fatal("inspect P4-04 runtime database identity")
	}
	if migrationRole == runtimeRole || migrationDatabase != runtimeDatabase {
		t.Fatal("P4-04 owner preflight requires distinct roles on the same database")
	}

	var version int
	var dirty bool
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P4-04 migration ledger")
	}
	if version != 30 || dirty {
		t.Fatal("P4-04 owner preflight requires ledger 30 false")
	}

	requiredRelations := []string{
		"media_spaces",
		"media_room_instances",
		"media_space_members",
		"media_admission_requests",
		"media_participant_sessions",
		"media_space_mutation_receipts",
		"media_provider_webhook_receipts",
		"livekit_webhook_events",
	}
	var ownerSafe, requiredRelationsPresent, ownerAuthority bool
	if err := migrationPool.QueryRow(ctx, `WITH actor AS (
    SELECT oid, rolsuper
    FROM pg_roles
    WHERE rolname = current_user
),
owned_scope AS (
    SELECT
        count(*) AS relation_count,
        bool_and(pg_has_role(actor.oid, relation.relowner, 'USAGE')) AS owner_authority
    FROM pg_class AS relation
    JOIN pg_namespace AS namespace
      ON namespace.oid = relation.relnamespace
     AND namespace.nspname = 'tutorhub'
    CROSS JOIN actor
    WHERE relation.relname = ANY($1::text[])
)
SELECT
    NOT actor.rolsuper
        AND has_schema_privilege(current_user, 'tutorhub', 'USAGE')
        AND has_schema_privilege(current_user, 'tutorhub', 'CREATE'),
    owned_scope.relation_count = cardinality($1::text[]),
    COALESCE(owned_scope.owner_authority, false)
FROM actor
CROSS JOIN owned_scope`, requiredRelations).Scan(
		&ownerSafe,
		&requiredRelationsPresent,
		&ownerAuthority,
	); err != nil {
		t.Fatal("inspect P4-04 owner authority")
	}
	if !ownerSafe || !requiredRelationsPresent || !ownerAuthority {
		t.Fatal("P4-04 owner authority preflight failed")
	}

	var runtimeSafe, noMigrationMembership, noBroadMembership, noTableOwnership bool
	if err := migrationPool.QueryRow(ctx, `SELECT
    NOT runtime.rolsuper AND NOT runtime.rolcreaterole AND NOT runtime.rolcreatedb
        AND NOT runtime.rolreplication AND NOT runtime.rolbypassrls,
    NOT pg_has_role(runtime.oid, migration.oid, 'MEMBER'),
    NOT EXISTS (
        SELECT 1
        FROM pg_roles AS inherited
        WHERE inherited.rolname = ANY($4::text[])
          AND pg_has_role(runtime.oid, inherited.oid, 'MEMBER')
    ),
    NOT EXISTS (
        SELECT 1
        FROM pg_class AS relation
        JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = 'tutorhub'
          AND relation.relname = ANY($3::text[])
          AND relation.relowner = runtime.oid
    )
FROM pg_roles AS runtime
JOIN pg_roles AS migration ON migration.rolname = $2
WHERE runtime.rolname = $1`,
		runtimeRole,
		migrationRole,
		requiredRelations,
		[]string{"pg_database_owner", "pg_read_all_data", "pg_write_all_data"},
	).Scan(
		&runtimeSafe,
		&noMigrationMembership,
		&noBroadMembership,
		&noTableOwnership,
	); err != nil {
		t.Fatal("inspect P4-04 runtime role safety")
	}
	if !runtimeSafe || !noMigrationMembership || !noBroadMembership || !noTableOwnership {
		t.Fatal("P4-04 runtime role safety preflight failed")
	}

	var unsafeFutureTableDefaultGrants int
	if err := migrationPool.QueryRow(ctx, `SELECT count(*)
FROM pg_default_acl AS defaults
JOIN pg_namespace AS namespace ON namespace.oid = defaults.defaclnamespace
CROSS JOIN LATERAL aclexplode(defaults.defaclacl) AS privilege
JOIN pg_roles AS runtime ON runtime.rolname = $1
WHERE namespace.nspname = 'tutorhub'
  AND defaults.defaclobjtype = 'r'
  AND (
      privilege.grantee = 0
      OR pg_has_role(runtime.oid, privilege.grantee, 'USAGE')
  )`, runtimeRole).Scan(&unsafeFutureTableDefaultGrants); err != nil {
		t.Fatal("inspect P4-04 future-table default ACL")
	}
	if unsafeFutureTableDefaultGrants != 0 {
		t.Fatal("P4-04 owner preflight found unsafe future-table default ACL")
	}
}

func requireP404NeonURLBoundary(t *testing.T, migrationURL string, runtimeURL string) {
	t.Helper()
	migrationConfig, err := pgxpool.ParseConfig(migrationURL)
	if err != nil {
		t.Fatal("parse P4-04 migration database configuration")
	}
	runtimeConfig, err := pgxpool.ParseConfig(runtimeURL)
	if err != nil {
		t.Fatal("parse P4-04 runtime database configuration")
	}
	migrationEndpoint, migrationPooled, migrationValid := p404NeonEndpoint(
		migrationConfig.ConnConfig.Host,
	)
	runtimeEndpoint, runtimePooled, runtimeValid := p404NeonEndpoint(
		runtimeConfig.ConnConfig.Host,
	)
	if !migrationValid || !runtimeValid || migrationPooled || !runtimePooled ||
		migrationEndpoint != runtimeEndpoint ||
		migrationConfig.ConnConfig.Database != runtimeConfig.ConnConfig.Database ||
		migrationConfig.ConnConfig.User == runtimeConfig.ConnConfig.User ||
		migrationConfig.ConnConfig.TLSConfig == nil || runtimeConfig.ConnConfig.TLSConfig == nil {
		t.Fatal("P4-04 owner preflight requires direct owner and pooled runtime URLs for one Neon endpoint")
	}
}

func p404NeonEndpoint(host string) (endpoint string, pooled bool, valid bool) {
	normalized := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if !strings.HasSuffix(normalized, ".neon.tech") {
		return "", false, false
	}
	labels := strings.Split(normalized, ".")
	if len(labels) < 3 || !strings.HasPrefix(labels[0], "ep-") {
		return "", false, false
	}
	pooled = strings.HasSuffix(labels[0], "-pooler")
	labels[0] = strings.TrimSuffix(labels[0], "-pooler")
	if labels[0] == "ep-" {
		return "", false, false
	}
	return strings.Join(labels, "."), pooled, true
}
