//go:build integration

package collaboration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

const (
	p502SharedConfirmation               = "I_UNDERSTAND_P5_COLLAB_02_SHARED_STAGING_ONLY"
	p502SharedOwnerPreflightConfirmation = "I_UNDERSTAND_P5_COLLAB_02_SHARED_OWNER_PREFLIGHT_READ_ONLY"
	p502SharedMigrationConfirmation      = "I_UNDERSTAND_P5_COLLAB_02_FORWARD_SHARED_STAGING_ONLY"
	p502SharedACLProvisionConfirmation   = "I_UNDERSTAND_P5_COLLAB_02_ACL_PROVISION_SHARED_STAGING_ONLY"
	p502SharedFinalSnapshotConfirmation  = "I_UNDERSTAND_P5_COLLAB_02_SHARED_FINAL_SNAPSHOT_READ_ONLY"
)

var p502SharedActions = map[string]string{
	"P5_COLLAB_02_SHARED_OWNER_PREFLIGHT":       p502SharedOwnerPreflightConfirmation,
	"P5_COLLAB_02_SHARED_MIGRATION_CONFIRM":     p502SharedMigrationConfirmation,
	"P5_COLLAB_02_SHARED_ACL_PROVISION_CONFIRM": p502SharedACLProvisionConfirmation,
	"P5_COLLAB_02_SHARED_FINAL_CONFIRM":         p502SharedFinalSnapshotConfirmation,
}

func TestPostgresP502SharedOwnerPreflight(t *testing.T) {
	requireP502SharedConfirmation(t, "P5_COLLAB_02_SHARED_OWNER_PREFLIGHT")
	runP502SharedReadOnlySnapshot(t, 36, false)
}

func TestPostgresP502SharedForwardMigration(t *testing.T) {
	requireP502SharedConfirmation(t, "P5_COLLAB_02_SHARED_MIGRATION_CONFIRM")
	runP502SharedForwardMigration(t)
}

func TestProvisionWhiteboardControlPlaneExactSharedACL(t *testing.T) {
	requireP502SharedConfirmation(t, "P5_COLLAB_02_SHARED_ACL_PROVISION_CONFIRM")
	migrationURL, runtimeURL, maintenanceURL := requireP502SharedDatabaseURLs(t)
	requireP502SharedNeonBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	runWhiteboardControlPlaneExactACLProvision(t, false)
}

func TestPostgresP502SharedFinalSnapshot(t *testing.T) {
	requireP502SharedConfirmation(t, "P5_COLLAB_02_SHARED_FINAL_CONFIRM")
	runP502SharedReadOnlySnapshot(t, 37, true)
}

func requireP502SharedConfirmation(t *testing.T, activeName string) {
	t.Helper()
	expected, ok := p502SharedActions[activeName]
	if !ok {
		t.Fatal("P5-COLLAB-02 shared gate received an unknown action")
	}
	sharedConfirmation := strings.TrimSpace(os.Getenv("P5_COLLAB_02_SHARED_CONFIRM"))
	if sharedConfirmation == "" {
		t.Skip("P5_COLLAB_02_SHARED_CONFIRM is not configured")
	}
	if sharedConfirmation != p502SharedConfirmation {
		t.Fatal("P5_COLLAB_02_SHARED_CONFIRM is not set to the exact shared-staging confirmation")
	}
	if strings.TrimSpace(os.Getenv(activeName)) != expected {
		t.Fatalf("%s is not set to the exact P5-COLLAB-02 shared action confirmation", activeName)
	}
	for name := range p502SharedActions {
		if name != activeName && strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P5-COLLAB-02 shared gate refuses conflicting action confirmation %s", name)
		}
	}
	for _, name := range []string{
		"P5_COLLAB_02_DISPOSABLE_CONFIRM",
		"P5_COLLAB_02_ACL_PROVISION_CONFIRM",
		"P4_10_DISPOSABLE_CONFIRM",
		"P4_10_ACL_PROVISION_CONFIRM",
		"P4_10_FINAL_SNAPSHOT_CONFIRM",
		"P4_10_SHARED_CONFIRM",
		"P4_10_SHARED_OWNER_PREFLIGHT",
		"P4_10_SHARED_MIGRATION_CONFIRM",
		"P4_10_SHARED_ACL_PROVISION_CONFIRM",
		"P4_10_SHARED_FINAL_CONFIRM",
	} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P5-COLLAB-02 shared gate refuses stale confirmation %s", name)
		}
	}
}

func runP502SharedForwardMigration(t *testing.T) {
	t.Helper()
	migrationURL, runtimeURL, maintenanceURL := requireP502SharedDatabaseURLs(t)
	requireP502SharedNeonBoundary(t, migrationURL, runtimeURL, maintenanceURL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Number != 36 || version.Dirty {
		t.Fatal("P5-COLLAB-02 shared forward migration requires ledger 36 false")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply P5-COLLAB-02 shared forward migration")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("rerun P5-COLLAB-02 shared forward migration idempotently")
	}
	version, err = migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Number != 37 || version.Dirty {
		t.Fatal("P5-COLLAB-02 shared forward migration must finish at ledger 37 false")
	}
}

func runP502SharedReadOnlySnapshot(t *testing.T, expectedVersion uint, postflight bool) {
	t.Helper()
	migrationURL, runtimeURL, maintenanceURL := requireP502SharedDatabaseURLs(t)
	requireP502SharedNeonBoundary(t, migrationURL, runtimeURL, maintenanceURL)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrationPool := openWhiteboardACLPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openWhiteboardACLPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	maintenancePool := openWhiteboardACLPool(t, ctx, maintenanceURL)
	t.Cleanup(maintenancePool.Close)

	migrationTx := beginP502SharedReadOnlyTransaction(t, ctx, migrationPool)
	defer func() { _ = migrationTx.Rollback(context.Background()) }()
	runtimeTx := beginP502SharedReadOnlyTransaction(t, ctx, runtimePool)
	defer func() { _ = runtimeTx.Rollback(context.Background()) }()
	maintenanceTx := beginP502SharedReadOnlyTransaction(t, ctx, maintenancePool)
	defer func() { _ = maintenanceTx.Rollback(context.Background()) }()

	for _, tx := range []pgx.Tx{migrationTx, runtimeTx, maintenanceTx} {
		var readOnly string
		if err := tx.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&readOnly); err != nil {
			t.Fatal("inspect P5-COLLAB-02 shared read-only transaction mode")
		}
		if readOnly != "on" {
			t.Fatal("P5-COLLAB-02 shared snapshot transaction is not read-only")
		}
	}

	migrationRole, migrationDatabase := readP502SharedDatabaseIdentity(t, ctx, migrationTx)
	runtimeRole, runtimeDatabase := readP502SharedDatabaseIdentity(t, ctx, runtimeTx)
	maintenanceRole, maintenanceDatabase := readP502SharedDatabaseIdentity(t, ctx, maintenanceTx)
	if migrationRole == runtimeRole || migrationRole == maintenanceRole || runtimeRole == maintenanceRole ||
		migrationDatabase != runtimeDatabase || migrationDatabase != maintenanceDatabase {
		t.Fatal("P5-COLLAB-02 shared snapshot requires three distinct roles on one database")
	}

	var version uint
	var dirty bool
	if err := migrationTx.QueryRow(ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P5-COLLAB-02 shared migration ledger")
	}
	if version != expectedVersion || dirty {
		t.Fatal("P5-COLLAB-02 shared snapshot found an unexpected migration ledger state")
	}

	assertP502SharedOwnerAuthority(t, ctx, migrationTx, postflight)
	assertP502SharedPrincipalSafety(
		t, ctx, migrationTx, runtimeTx, maintenanceTx,
		migrationRole, runtimeRole, maintenanceRole,
	)

	var relationCount int
	if err := migrationTx.QueryRow(ctx, `SELECT count(*)
		FROM pg_class AS relation
		JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'tutorhub'
		  AND relation.relkind IN ('r', 'p')
		  AND relation.relname = ANY($1::text[])`, whiteboardACLTables).Scan(&relationCount); err != nil {
		t.Fatal("inspect P5-COLLAB-02 shared relation count")
	}
	if (!postflight && relationCount != 0) || (postflight && relationCount != len(whiteboardACLTables)) {
		t.Fatal("P5-COLLAB-02 shared snapshot found an unexpected whiteboard relation count")
	}
	if !postflight {
		return
	}

	assertWhiteboardRoleSafety(t, ctx, migrationPool, migrationRole, runtimeRole, maintenanceRole)
	assertWhiteboardExactACL(t, ctx, migrationPool, runtimeRole, maintenanceRole)
	assertWhiteboardDatabaseIsNotDocumentHistoryAuthority(t, ctx, migrationPool)
	assertP502SharedSideEffectsClean(t, ctx, migrationTx)
}

func requireP502SharedDatabaseURLs(t *testing.T) (string, string, string) {
	t.Helper()
	migrationURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	runtimeURL := strings.TrimSpace(os.Getenv("DATABASE_POOL_URL"))
	maintenanceURL := strings.TrimSpace(os.Getenv("DATABASE_POLL_MAINTENANCE_URL"))
	if migrationURL == "" || runtimeURL == "" || maintenanceURL == "" {
		t.Fatal("P5-COLLAB-02 shared owner/runtime/maintenance URLs are required")
	}
	return migrationURL, runtimeURL, maintenanceURL
}

func requireP502SharedNeonBoundary(
	t *testing.T,
	migrationURL string,
	runtimeURL string,
	maintenanceURL string,
) {
	t.Helper()
	configs := make([]*pgxpool.Config, 0, 3)
	for _, databaseURL := range []string{migrationURL, runtimeURL, maintenanceURL} {
		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			t.Fatal("parse P5-COLLAB-02 shared database configuration")
		}
		configs = append(configs, config)
	}
	migrationEndpoint, migrationPooled, migrationValid := p502NeonEndpoint(configs[0].ConnConfig.Host)
	runtimeEndpoint, runtimePooled, runtimeValid := p502NeonEndpoint(configs[1].ConnConfig.Host)
	maintenanceEndpoint, maintenancePooled, maintenanceValid := p502NeonEndpoint(configs[2].ConnConfig.Host)
	sameEndpoint := migrationValid && runtimeValid && maintenanceValid &&
		migrationEndpoint == runtimeEndpoint && migrationEndpoint == maintenanceEndpoint
	sameDatabase := configs[0].ConnConfig.Database == configs[1].ConnConfig.Database &&
		configs[0].ConnConfig.Database == configs[2].ConnConfig.Database
	distinctRoles := configs[0].ConnConfig.User != configs[1].ConnConfig.User &&
		configs[0].ConnConfig.User != configs[2].ConnConfig.User &&
		configs[1].ConnConfig.User != configs[2].ConnConfig.User
	tlsAll := configs[0].ConnConfig.TLSConfig != nil && configs[1].ConnConfig.TLSConfig != nil &&
		configs[2].ConnConfig.TLSConfig != nil
	if !migrationValid || migrationPooled || !runtimeValid || !runtimePooled ||
		!maintenanceValid || maintenancePooled || !sameEndpoint || !sameDatabase ||
		!distinctRoles || !tlsAll {
		t.Fatal("P5-COLLAB-02 shared gate requires direct owner/maintenance and pooled runtime URLs for one Neon database with distinct roles")
	}
}

func p502NeonEndpoint(host string) (string, bool, bool) {
	normalized := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if !strings.HasSuffix(normalized, ".neon.tech") {
		return "", false, false
	}
	labels := strings.Split(normalized, ".")
	if len(labels) < 3 || !strings.HasPrefix(labels[0], "ep-") {
		return "", false, false
	}
	pooled := strings.HasSuffix(labels[0], "-pooler")
	labels[0] = strings.TrimSuffix(labels[0], "-pooler")
	if labels[0] == "ep-" {
		return "", false, false
	}
	return strings.Join(labels, "."), pooled, true
}

func beginP502SharedReadOnlyTransaction(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) pgx.Tx {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		t.Fatal("begin P5-COLLAB-02 shared read-only transaction")
	}
	return tx
}

func readP502SharedDatabaseIdentity(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
) (string, string) {
	t.Helper()
	var role, database string
	if err := tx.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(&role, &database); err != nil {
		t.Fatal("inspect P5-COLLAB-02 shared database identity")
	}
	return role, database
}

func assertP502SharedOwnerAuthority(t *testing.T, ctx context.Context, tx pgx.Tx, postflight bool) {
	t.Helper()
	var ownerSafe bool
	if err := tx.QueryRow(ctx, `SELECT
		NOT role.rolsuper
		AND has_schema_privilege(current_user, 'tutorhub', 'USAGE')
		AND has_schema_privilege(current_user, 'tutorhub', 'CREATE')
		AND pg_has_role(role.oid, namespace.nspowner, 'USAGE')
	FROM pg_roles AS role
	JOIN pg_namespace AS namespace ON namespace.nspname = 'tutorhub'
	WHERE role.rolname = current_user`).Scan(&ownerSafe); err != nil {
		t.Fatal("inspect P5-COLLAB-02 shared owner authority")
	}
	if !ownerSafe {
		t.Fatal("P5-COLLAB-02 shared owner authority preflight failed")
	}
	if !postflight {
		return
	}
	var ownsAll bool
	if err := tx.QueryRow(ctx, `SELECT count(*) = cardinality($1::text[])
		AND bool_and(pg_has_role(current_user, relation.relowner, 'USAGE'))
	FROM pg_class AS relation
	JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
	WHERE namespace.nspname = 'tutorhub'
	  AND relation.relname = ANY($1::text[])`, whiteboardACLTables).Scan(&ownsAll); err != nil {
		t.Fatal("inspect P5-COLLAB-02 shared relation ownership")
	}
	if !ownsAll {
		t.Fatal("P5-COLLAB-02 shared owner lacks relation authority")
	}
}

func assertP502SharedPrincipalSafety(
	t *testing.T,
	ctx context.Context,
	migrationTx pgx.Tx,
	runtimeTx pgx.Tx,
	maintenanceTx pgx.Tx,
	migrationRole string,
	runtimeRole string,
	maintenanceRole string,
) {
	t.Helper()
	var runtimeSafe, maintenanceSafe, noUnsafeMembership bool
	if err := migrationTx.QueryRow(ctx, `SELECT
		NOT runtime.rolsuper AND NOT runtime.rolcreaterole AND NOT runtime.rolcreatedb
			AND NOT runtime.rolreplication AND NOT runtime.rolbypassrls,
		NOT maintenance.rolsuper AND NOT maintenance.rolcreaterole AND NOT maintenance.rolcreatedb
			AND NOT maintenance.rolreplication AND NOT maintenance.rolbypassrls,
		NOT pg_has_role(runtime.oid, migration.oid, 'MEMBER')
			AND NOT pg_has_role(maintenance.oid, migration.oid, 'MEMBER')
			AND NOT pg_has_role(runtime.oid, maintenance.oid, 'MEMBER')
			AND NOT pg_has_role(maintenance.oid, runtime.oid, 'MEMBER')
			AND NOT pg_has_role(runtime.oid, 'pg_database_owner', 'MEMBER')
			AND NOT pg_has_role(maintenance.oid, 'pg_database_owner', 'MEMBER')
	FROM pg_roles AS migration
	JOIN pg_roles AS runtime ON runtime.rolname = $2
	JOIN pg_roles AS maintenance ON maintenance.rolname = $3
	WHERE migration.rolname = $1`, migrationRole, runtimeRole, maintenanceRole).Scan(
		&runtimeSafe, &maintenanceSafe, &noUnsafeMembership,
	); err != nil {
		t.Fatal("inspect P5-COLLAB-02 shared principal safety")
	}
	if !runtimeSafe || !maintenanceSafe || !noUnsafeMembership {
		t.Fatal("P5-COLLAB-02 shared runtime or maintenance principal is unsafe")
	}
	for label, tx := range map[string]pgx.Tx{
		"runtime":     runtimeTx,
		"maintenance": maintenanceTx,
	} {
		var safe bool
		if err := tx.QueryRow(ctx, `SELECT
			has_schema_privilege(current_user, 'tutorhub', 'USAGE')
			AND NOT has_schema_privilege(current_user, 'tutorhub', 'CREATE')
			AND NOT has_database_privilege(current_user, current_database(), 'CREATE')
			AND NOT has_table_privilege(current_user, 'public.tutorhub_schema_migrations', 'SELECT')`).Scan(&safe); err != nil {
			t.Fatalf("inspect P5-COLLAB-02 shared %s DDL boundary", label)
		}
		if !safe {
			t.Fatalf("P5-COLLAB-02 shared %s retained unsafe DDL or migration authority", label)
		}
	}
}

func assertP502SharedSideEffectsClean(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	for _, table := range whiteboardACLTables {
		var count int
		query := `SELECT count(*) FROM ` + pgx.Identifier{"tutorhub", table}.Sanitize()
		if err := tx.QueryRow(ctx, query).Scan(&count); err != nil {
			t.Fatal("count P5-COLLAB-02 shared side effects")
		}
		if count != 0 {
			t.Fatalf("P5-COLLAB-02 shared relation %s contains unexpected rows", table)
		}
	}
}
