//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const (
	p406OwnerPreflightConfirmation = "I_UNDERSTAND_P4_06_OWNER_PREFLIGHT_ONLY"
	p406FinalSnapshotConfirmation  = "I_UNDERSTAND_P4_06_FINAL_SNAPSHOT_READ_ONLY"
)

// TestPostgresP406DisposableOwnerPreflight authenticates all three database
// principals and proves the retained disposable branch is safe to migrate. All
// queries run in explicit read-only transactions and the test never logs a
// role, database, endpoint or connection string.
func TestPostgresP406DisposableOwnerPreflight(t *testing.T) {
	requireP406ReadOnlyConfirmation(
		t,
		"P4_06_OWNER_PREFLIGHT",
		p406OwnerPreflightConfirmation,
	)
	runPostgresP406ReadOnlySnapshot(t, 31, false)
}

// TestPostgresP406DisposableFinalSnapshot proves the forward migration, exact
// runtime/maintenance ACL and fixture cleanup without mutating the database.
func TestPostgresP406DisposableFinalSnapshot(t *testing.T) {
	requireP406ReadOnlyConfirmation(
		t,
		"P4_06_FINAL_SNAPSHOT_CONFIRM",
		p406FinalSnapshotConfirmation,
	)
	runPostgresP406ReadOnlySnapshot(t, 32, true)
}

func requireP406ReadOnlyConfirmation(t *testing.T, activeName string, expected string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(activeName)) != expected {
		t.Fatalf("%s is not set to the exact P4-06 read-only confirmation", activeName)
	}

	conflicting := append([]string(nil), p405DisposableConflictingConfirmations...)
	conflicting = append(
		conflicting,
		"P4_05_DISPOSABLE_CONFIRM",
		"P4_06_DISPOSABLE_CONFIRM",
		"P4_06_OWNER_PREFLIGHT",
		"P4_06_FINAL_SNAPSHOT_CONFIRM",
	)
	for _, name := range conflicting {
		if name == activeName {
			continue
		}
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-06 read-only database gate refuses confirmation %s", name)
		}
	}
}

func runPostgresP406ReadOnlySnapshot(
	t *testing.T,
	expectedVersion int,
	postflight bool,
) {
	t.Helper()
	migrationURL := requireP406ReadOnlyEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireP406ReadOnlyEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireP406ReadOnlyEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406ReadOnlyNeonURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	maintenancePool := openMediaIntegrationPool(t, ctx, maintenanceURL)
	t.Cleanup(maintenancePool.Close)

	migrationTx := beginP406ReadOnlyTransaction(t, ctx, migrationPool)
	defer func() { _ = migrationTx.Rollback(context.Background()) }()
	runtimeTx := beginP406ReadOnlyTransaction(t, ctx, runtimePool)
	defer func() { _ = runtimeTx.Rollback(context.Background()) }()
	maintenanceTx := beginP406ReadOnlyTransaction(t, ctx, maintenancePool)
	defer func() { _ = maintenanceTx.Rollback(context.Background()) }()

	for _, transaction := range []pgx.Tx{migrationTx, runtimeTx, maintenanceTx} {
		var readOnly string
		if err := transaction.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&readOnly); err != nil {
			t.Fatal("inspect P4-06 read-only transaction mode")
		}
		if readOnly != "on" {
			t.Fatal("P4-06 database gate transaction is not read-only")
		}
	}

	migrationRole, migrationDatabase := readP406DatabaseIdentity(t, ctx, migrationTx)
	runtimeRole, runtimeDatabase := readP406DatabaseIdentity(t, ctx, runtimeTx)
	maintenanceRole, maintenanceDatabase := readP406DatabaseIdentity(t, ctx, maintenanceTx)
	if migrationRole == runtimeRole || migrationRole == maintenanceRole || runtimeRole == maintenanceRole ||
		migrationDatabase != runtimeDatabase || migrationDatabase != maintenanceDatabase {
		t.Fatal("P4-06 read-only gate requires three distinct roles on one database")
	}

	var version int
	var dirty bool
	if err := migrationTx.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P4-06 migration ledger")
	}
	if version != expectedVersion || dirty {
		t.Fatal("P4-06 read-only gate found an unexpected migration ledger state")
	}

	targets := p406RelationsForVersion(expectedVersion)
	assertP406OwnerAuthority(t, ctx, migrationTx, targets)
	assertP406RoleSafety(
		t,
		ctx,
		migrationTx,
		runtimeTx,
		maintenanceTx,
		migrationRole,
		runtimeRole,
		maintenanceRole,
		targets,
	)
	assertP406MediaFeaturesForcedOff(t)
	logP406SafeAggregateSnapshot(t, ctx, migrationTx, expectedVersion)

	if !postflight {
		return
	}
	for _, expectation := range p402MediaACLExpectations() {
		assertExactMediaACL(t, ctx, runtimeTx, expectation)
	}
	assertP402ProvisionedPublicACL(t, ctx, migrationTx, targets)
	assertP406ProvisionedMaintenanceACL(
		t,
		ctx,
		migrationTx,
		runtimeTx,
		maintenanceTx,
		targets,
	)
	assertP406DependencyACL(t, ctx, runtimeTx)
	assertP406SideEffectsClean(t, ctx, migrationTx)
}

func requireP406ReadOnlyEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required for the P4-06 read-only database gate", name)
	}
	return value
}

func requireP406ReadOnlyNeonURLBoundary(
	t *testing.T,
	migrationURL string,
	runtimeURL string,
	maintenanceURL string,
) {
	t.Helper()
	migrationConfig, err := pgxpool.ParseConfig(migrationURL)
	if err != nil {
		t.Fatal("parse P4-06 migration database configuration")
	}
	runtimeConfig, err := pgxpool.ParseConfig(runtimeURL)
	if err != nil {
		t.Fatal("parse P4-06 runtime database configuration")
	}
	maintenanceConfig, err := pgxpool.ParseConfig(maintenanceURL)
	if err != nil {
		t.Fatal("parse P4-06 maintenance database configuration")
	}
	migrationEndpoint, migrationPooled, migrationValid := p404NeonEndpoint(
		migrationConfig.ConnConfig.Host,
	)
	runtimeEndpoint, runtimePooled, runtimeValid := p404NeonEndpoint(
		runtimeConfig.ConnConfig.Host,
	)
	maintenanceEndpoint, maintenancePooled, maintenanceValid := p404NeonEndpoint(
		maintenanceConfig.ConnConfig.Host,
	)
	sameEndpoint := migrationValid && runtimeValid && maintenanceValid &&
		migrationEndpoint == runtimeEndpoint && migrationEndpoint == maintenanceEndpoint
	sameDatabase := migrationConfig.ConnConfig.Database == runtimeConfig.ConnConfig.Database &&
		migrationConfig.ConnConfig.Database == maintenanceConfig.ConnConfig.Database
	distinctRoles := migrationConfig.ConnConfig.User != runtimeConfig.ConnConfig.User &&
		migrationConfig.ConnConfig.User != maintenanceConfig.ConnConfig.User &&
		runtimeConfig.ConnConfig.User != maintenanceConfig.ConnConfig.User
	tlsAll := migrationConfig.ConnConfig.TLSConfig != nil &&
		runtimeConfig.ConnConfig.TLSConfig != nil && maintenanceConfig.ConnConfig.TLSConfig != nil
	if !migrationValid || migrationPooled || !runtimeValid || !runtimePooled ||
		!maintenanceValid || maintenancePooled || !sameEndpoint || !sameDatabase ||
		!distinctRoles || !tlsAll {
		t.Fatalf(
			"P4-06 URL boundary booleans: migration_valid=%t migration_direct=%t runtime_valid=%t runtime_pooled=%t maintenance_valid=%t maintenance_direct=%t same_endpoint=%t same_database=%t distinct_roles=%t tls_all=%t",
			migrationValid,
			migrationValid && !migrationPooled,
			runtimeValid,
			runtimeValid && runtimePooled,
			maintenanceValid,
			maintenanceValid && !maintenancePooled,
			sameEndpoint,
			sameDatabase,
			distinctRoles,
			tlsAll,
		)
	}
}

func beginP406ReadOnlyTransaction(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) pgx.Tx {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		t.Fatal("begin P4-06 read-only transaction")
	}
	return transaction
}

func readP406DatabaseIdentity(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
) (string, string) {
	t.Helper()
	var role string
	var database string
	if err := transaction.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&role,
		&database,
	); err != nil {
		t.Fatal("inspect P4-06 database identity")
	}
	return role, database
}

func p406RelationsForVersion(version int) []string {
	targets := []string{
		"media_spaces",
		"media_room_instances",
		"media_space_members",
		"media_admission_requests",
		"media_participant_sessions",
		"media_space_mutation_receipts",
		"media_provider_webhook_receipts",
		"livekit_webhook_events",
	}
	if version >= 32 {
		targets = append(
			targets,
			"media_participant_hand_states",
			"media_reaction_events",
			"media_signal_mutation_receipts",
		)
	}
	return targets
}

func assertP406OwnerAuthority(
	t *testing.T,
	ctx context.Context,
	migrationTx pgx.Tx,
	targets []string,
) {
	t.Helper()
	var ownerSafe bool
	var requiredRelationsPresent bool
	var ownerAuthority bool
	if err := migrationTx.QueryRow(ctx, `WITH actor AS (
    SELECT oid, rolsuper
    FROM pg_roles
    WHERE rolname = current_user
), owned_scope AS (
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
CROSS JOIN owned_scope`, targets).Scan(
		&ownerSafe,
		&requiredRelationsPresent,
		&ownerAuthority,
	); err != nil {
		t.Fatal("inspect P4-06 owner authority")
	}
	if !ownerSafe || !requiredRelationsPresent || !ownerAuthority {
		t.Fatal("P4-06 owner authority preflight failed")
	}
}

func assertP406RoleSafety(
	t *testing.T,
	ctx context.Context,
	migrationTx pgx.Tx,
	runtimeTx pgx.Tx,
	maintenanceTx pgx.Tx,
	migrationRole string,
	runtimeRole string,
	maintenanceRole string,
	targets []string,
) {
	t.Helper()
	var runtimeSafe bool
	var maintenanceSafe bool
	var noUnsafeMembership bool
	var noTableOwnership bool
	if err := migrationTx.QueryRow(ctx, `SELECT
    NOT runtime.rolsuper AND NOT runtime.rolcreaterole AND NOT runtime.rolcreatedb
        AND NOT runtime.rolreplication AND NOT runtime.rolbypassrls,
    NOT maintenance.rolsuper AND NOT maintenance.rolcreaterole AND NOT maintenance.rolcreatedb
        AND NOT maintenance.rolreplication AND NOT maintenance.rolbypassrls,
    NOT pg_has_role(runtime.oid, migration.oid, 'MEMBER')
        AND NOT pg_has_role(maintenance.oid, migration.oid, 'MEMBER')
        AND NOT pg_has_role(maintenance.oid, runtime.oid, 'MEMBER')
        AND NOT pg_has_role(runtime.oid, maintenance.oid, 'MEMBER')
        AND NOT EXISTS (
            SELECT 1
            FROM pg_roles AS inherited
            WHERE inherited.rolname = ANY($4::text[])
              AND (
                  pg_has_role(runtime.oid, inherited.oid, 'MEMBER')
                  OR pg_has_role(maintenance.oid, inherited.oid, 'MEMBER')
              )
        ),
    NOT EXISTS (
        SELECT 1
        FROM pg_class AS relation
        JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = 'tutorhub'
          AND relation.relname = ANY($5::text[])
          AND relation.relowner IN (runtime.oid, maintenance.oid)
    )
FROM pg_roles AS migration
JOIN pg_roles AS runtime ON runtime.rolname = $2
JOIN pg_roles AS maintenance ON maintenance.rolname = $3
WHERE migration.rolname = $1`,
		migrationRole,
		runtimeRole,
		maintenanceRole,
		[]string{"pg_database_owner", "pg_read_all_data", "pg_write_all_data"},
		targets,
	).Scan(
		&runtimeSafe,
		&maintenanceSafe,
		&noUnsafeMembership,
		&noTableOwnership,
	); err != nil {
		t.Fatal("inspect P4-06 runtime and maintenance role safety")
	}
	if !runtimeSafe || !maintenanceSafe || !noUnsafeMembership || !noTableOwnership {
		t.Fatal("P4-06 runtime or maintenance role safety preflight failed")
	}

	var runtimeSchemaUsage bool
	var runtimeNoCreate bool
	if err := runtimeTx.QueryRow(ctx, `SELECT
    has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
    NOT has_schema_privilege(current_user, 'tutorhub', 'CREATE')
        AND NOT has_database_privilege(current_user, current_database(), 'CREATE')
        AND NOT has_table_privilege(current_user, 'public.tutorhub_schema_migrations', 'SELECT')`).Scan(
		&runtimeSchemaUsage,
		&runtimeNoCreate,
	); err != nil {
		t.Fatal("inspect P4-06 runtime DDL boundary")
	}
	if !runtimeSchemaUsage || !runtimeNoCreate {
		t.Fatal("P4-06 runtime retained unsafe schema, database or migration authority")
	}

	var maintenanceNoCreate bool
	if err := maintenanceTx.QueryRow(ctx, `SELECT
    NOT has_schema_privilege(current_user, 'tutorhub', 'CREATE')
        AND NOT has_database_privilege(current_user, current_database(), 'CREATE')
        AND NOT has_table_privilege(current_user, 'public.tutorhub_schema_migrations', 'SELECT')`).Scan(
		&maintenanceNoCreate,
	); err != nil {
		t.Fatal("inspect P4-06 maintenance DDL boundary")
	}
	if !maintenanceNoCreate {
		t.Fatal("P4-06 maintenance retained unsafe schema, database or migration authority")
	}
}

func assertP406MediaFeaturesForcedOff(t *testing.T) {
	t.Helper()
	catalog, err := featurecontrol.NewCatalog(featurecontrol.Guardrails{
		ForcedOffFeatures: map[featurecontrol.FeatureKey]bool{
			featurecontrol.FeatureClassroomMediaRooms: true,
			featurecontrol.FeatureInstantStudyRooms:   true,
		},
	})
	if err != nil {
		t.Fatal("construct P4-06 media deployment guardrail")
	}
	configuredEnabled := true
	for _, key := range []featurecontrol.FeatureKey{
		featurecontrol.FeatureClassroomMediaRooms,
		featurecontrol.FeatureInstantStudyRooms,
	} {
		effective, evaluateErr := catalog.EvaluateFeature(key, &configuredEnabled)
		if evaluateErr != nil || effective.Enabled ||
			effective.Source != featurecontrol.ValueSourceDeploymentGuardrail {
			t.Fatal("P4-06 media feature did not remain deployment-force-off")
		}
	}
}

func logP406SafeAggregateSnapshot(
	t *testing.T,
	ctx context.Context,
	migrationTx pgx.Tx,
	version int,
) {
	t.Helper()
	counts := make([]int64, 14)
	if err := migrationTx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_spaces),
    (SELECT count(*) FROM tutorhub.media_room_instances),
    (SELECT count(*) FROM tutorhub.media_space_members),
    (SELECT count(*) FROM tutorhub.media_admission_requests),
    (SELECT count(*) FROM tutorhub.media_participant_sessions),
    (SELECT count(*) FROM tutorhub.media_space_mutation_receipts),
    (SELECT count(*) FROM tutorhub.media_provider_webhook_receipts),
    (SELECT count(*) FROM tutorhub.livekit_webhook_events),
    (SELECT count(*) FROM tutorhub.outbox_events
      WHERE aggregate_type IN ('media_space', 'media_space_member', 'media_admission')),
    (SELECT count(*) FROM tutorhub.audit_events
      WHERE action LIKE 'media_space.%'
         OR action LIKE 'media_space_member.%'
         OR action LIKE 'media_admission.%'),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
      WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms')
        AND enabled)`).Scan(
		&counts[0],
		&counts[1],
		&counts[2],
		&counts[3],
		&counts[4],
		&counts[5],
		&counts[6],
		&counts[7],
		&counts[8],
		&counts[9],
		&counts[10],
	); err != nil {
		t.Fatal("inspect P4-06 safe database aggregate snapshot")
	}
	if counts[10] != 0 {
		t.Fatal("P4-06 database snapshot found enabled media feature overrides")
	}
	if version >= 32 {
		if err := migrationTx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_participant_hand_states),
    (SELECT count(*) FROM tutorhub.media_reaction_events),
    (SELECT count(*) FROM tutorhub.media_signal_mutation_receipts)`).Scan(
			&counts[11],
			&counts[12],
			&counts[13],
		); err != nil {
			t.Fatal("inspect P4-06 participant-signal aggregate snapshot")
		}
	}
	t.Logf(
		"P4_06_DATABASE_SNAPSHOT version=%d dirty=false media_features=false media_spaces=%d media_room_instances=%d media_space_members=%d media_admission_requests=%d media_participant_sessions=%d media_space_mutation_receipts=%d media_provider_webhook_receipts=%d livekit_webhook_events=%d media_outbox=%d media_audit=%d enabled_media_feature_overrides=%d hand_states=%d reactions=%d signal_receipts=%d",
		version,
		counts[0],
		counts[1],
		counts[2],
		counts[3],
		counts[4],
		counts[5],
		counts[6],
		counts[7],
		counts[8],
		counts[9],
		counts[10],
		counts[11],
		counts[12],
		counts[13],
	)
}

func assertP406DependencyACL(t *testing.T, ctx context.Context, runtimeTx pgx.Tx) {
	t.Helper()
	var usersIDRead bool
	var usersDisplayNameRead bool
	var rateSelect bool
	var rateInsert bool
	var rateUpdate bool
	var rateDelete bool
	var rateTruncate bool
	var rateReferences bool
	var rateTrigger bool
	if err := runtimeTx.QueryRow(ctx, `SELECT
    has_column_privilege(current_user, 'tutorhub.users', 'id', 'SELECT'),
    has_column_privilege(current_user, 'tutorhub.users', 'display_name', 'SELECT'),
    has_table_privilege(current_user, 'tutorhub.rate_limit_windows', 'SELECT'),
    has_table_privilege(current_user, 'tutorhub.rate_limit_windows', 'INSERT'),
    has_table_privilege(current_user, 'tutorhub.rate_limit_windows', 'UPDATE'),
    has_table_privilege(current_user, 'tutorhub.rate_limit_windows', 'DELETE'),
    has_table_privilege(current_user, 'tutorhub.rate_limit_windows', 'TRUNCATE'),
    has_table_privilege(current_user, 'tutorhub.rate_limit_windows', 'REFERENCES'),
	    has_table_privilege(current_user, 'tutorhub.rate_limit_windows', 'TRIGGER')`).Scan(
		&usersIDRead,
		&usersDisplayNameRead,
		&rateSelect,
		&rateInsert,
		&rateUpdate,
		&rateDelete,
		&rateTruncate,
		&rateReferences,
		&rateTrigger,
	); err != nil {
		t.Fatal("inspect P4-06 shared dependency ACL")
	}
	if !usersIDRead || !usersDisplayNameRead || !rateSelect || !rateInsert || !rateUpdate ||
		rateDelete || rateTruncate || rateReferences || rateTrigger {
		t.Fatalf(
			"P4-06 dependency ACL booleans: users_id_read=%t users_display_name_read=%t rate_select=%t rate_insert=%t rate_update=%t rate_delete=%t rate_truncate=%t rate_references=%t rate_trigger=%t",
			usersIDRead,
			usersDisplayNameRead,
			rateSelect,
			rateInsert,
			rateUpdate,
			rateDelete,
			rateTruncate,
			rateReferences,
			rateTrigger,
		)
	}
}

func assertP406SideEffectsClean(
	t *testing.T,
	ctx context.Context,
	migrationTx pgx.Tx,
) {
	t.Helper()
	var handStates int
	var reactions int
	var signalReceipts int
	var fixtureSpaces int
	var fixtureRooms int
	var fixtureParticipants int
	if err := migrationTx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_participant_hand_states),
    (SELECT count(*) FROM tutorhub.media_reaction_events),
    (SELECT count(*) FROM tutorhub.media_signal_mutation_receipts),
    (SELECT count(*) FROM tutorhub.media_spaces
      WHERE create_idempotency_key LIKE 'p406-%'),
    (SELECT count(*) FROM tutorhub.media_room_instances
      WHERE provider_room_name LIKE 'p406room_%'
         OR provider_room_name LIKE 'p406_maintenance_%'),
    (SELECT count(*) FROM tutorhub.media_participant_sessions
      WHERE provider_participant_identity LIKE 'p406_participant_%')`).Scan(
		&handStates,
		&reactions,
		&signalReceipts,
		&fixtureSpaces,
		&fixtureRooms,
		&fixtureParticipants,
	); err != nil {
		t.Fatal("inspect P4-06 side-effect boundary")
	}
	if handStates != 0 || reactions != 0 || signalReceipts != 0 || fixtureSpaces != 0 ||
		fixtureRooms != 0 || fixtureParticipants != 0 {
		t.Fatal("P4-06 database retained participant-signal integration side effects")
	}
}
