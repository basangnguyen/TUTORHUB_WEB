//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const p405DisposableReadOnlyConfirmation = "I_UNDERSTAND_P4_05_DISPOSABLE_READ_ONLY"

var p405DisposableConflictingConfirmations = []string{
	"P4_02_DISPOSABLE_CONFIRM",
	"P4_02_OWNER_PREFLIGHT",
	"P4_02_PROVIDER_SMOKE_CONFIRM",
	"P4_02_SHARED_CONFIRM",
	"P4_02_ACL_PROVISION_CONFIRM",
	"P4_02_SHARED_ACL_PROVISION_CONFIRM",
	"P4_04_DISPOSABLE_CONFIRM",
	"P4_04_OWNER_PREFLIGHT",
	"P4_04_ACL_PROVISION_CONFIRM",
	"P4_04_SHARED_CONFIRM",
	"P4_04_SHARED_ACL_PROVISION_CONFIRM",
	"P4_04_SHARED_SNAPSHOT_CONFIRM",
	"P4_05_PROVIDER_CONFIRM",
	"P4_05_BROWSER_PROVIDER_CONFIRM",
	"P4_05_PROVIDER_ROOM_NAME",
	"P4_05_SHARED_CONFIRM",
	"P4_05_SHARED_SNAPSHOT_CONFIRM",
	"P4_05_SHARED_ACL_PROVISION_CONFIRM",
}

// TestPostgresP405DisposableReadOnlySnapshot records only identity, ledger,
// feature-off and privilege booleans. Both sessions are enclosed in explicit
// read-only transactions; P4-05 never runs migrations, ACL provisioning or
// fixture mutations against the retained disposable branch.
func TestPostgresP405DisposableReadOnlySnapshot(t *testing.T) {
	migrationURL, runtimeURL := requireP405DisposableReadOnlyBoundary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)

	migrationTx := beginP405ReadOnlyTransaction(t, ctx, migrationPool)
	defer func() { _ = migrationTx.Rollback(context.Background()) }()
	runtimeTx := beginP405ReadOnlyTransaction(t, ctx, runtimePool)
	defer func() { _ = runtimeTx.Rollback(context.Background()) }()

	var migrationRole, migrationDatabase string
	if err := migrationTx.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&migrationRole,
		&migrationDatabase,
	); err != nil {
		t.Fatal("inspect P4-05 disposable owner identity")
	}
	var runtimeRole, runtimeDatabase string
	if err := runtimeTx.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&runtimeRole,
		&runtimeDatabase,
	); err != nil {
		t.Fatal("inspect P4-05 disposable runtime identity")
	}
	if migrationRole == runtimeRole || migrationDatabase != runtimeDatabase {
		t.Fatal("P4-05 disposable read-only gate requires distinct roles on one database")
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
	mediaFeatureKeys := []string{"classroom_media_rooms", "instant_study_rooms"}
	var version int
	var dirty bool
	var relationCount, enabledMediaOverrides int
	if err := migrationTx.QueryRow(ctx, `SELECT
    ledger.version,
    ledger.dirty,
    (SELECT count(*)
       FROM pg_class AS relation
       JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
      WHERE namespace.nspname = 'tutorhub'
        AND relation.relname = ANY($1::text[])),
    (SELECT count(*)
       FROM tutorhub.tenant_feature_overrides
      WHERE feature_key = ANY($2::text[])
        AND enabled)
FROM public.tutorhub_schema_migrations AS ledger`, requiredRelations, mediaFeatureKeys).Scan(
		&version,
		&dirty,
		&relationCount,
		&enabledMediaOverrides,
	); err != nil {
		t.Fatal("inspect P4-05 disposable ledger and feature state")
	}
	if version != 31 || dirty {
		t.Fatal("P4-05 disposable read-only gate requires ledger 31 false")
	}
	if relationCount != len(requiredRelations) {
		t.Fatal("P4-05 disposable read-only gate requires the exact media relation set")
	}
	forcedOffCatalog, err := featurecontrol.NewCatalog(featurecontrol.Guardrails{
		ForcedOffFeatures: map[featurecontrol.FeatureKey]bool{
			featurecontrol.FeatureClassroomMediaRooms: true,
			featurecontrol.FeatureInstantStudyRooms:   true,
		},
	})
	if err != nil {
		t.Fatal("construct P4-05 media deployment guardrail")
	}
	configuredEnabled := true
	for _, key := range []featurecontrol.FeatureKey{
		featurecontrol.FeatureClassroomMediaRooms,
		featurecontrol.FeatureInstantStudyRooms,
	} {
		effective, evaluateErr := forcedOffCatalog.EvaluateFeature(key, &configuredEnabled)
		if evaluateErr != nil || effective.Enabled ||
			effective.Source != featurecontrol.ValueSourceDeploymentGuardrail {
			t.Fatal("P4-05 media feature did not remain deployment-force-off")
		}
	}

	var schemaUsage, noSchemaCreate, noDatabaseCreate, noMigrationLedgerRead bool
	if err := runtimeTx.QueryRow(ctx, `SELECT
    has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
    NOT has_schema_privilege(current_user, 'tutorhub', 'CREATE'),
    NOT has_database_privilege(current_user, current_database(), 'CREATE'),
    NOT has_table_privilege(current_user, 'public.tutorhub_schema_migrations', 'SELECT')`).Scan(
		&schemaUsage,
		&noSchemaCreate,
		&noDatabaseCreate,
		&noMigrationLedgerRead,
	); err != nil {
		t.Fatal("inspect P4-05 disposable runtime DDL boundary")
	}
	if !schemaUsage || !noSchemaCreate || !noDatabaseCreate || !noMigrationLedgerRead {
		t.Fatal("P4-05 disposable runtime retained schema, database or migration authority")
	}

	var safeRole, noMigrationMembership, noBroadMembership bool
	if err := migrationTx.QueryRow(ctx, `SELECT
    NOT runtime.rolsuper
        AND NOT runtime.rolcreaterole
        AND NOT runtime.rolcreatedb
        AND NOT runtime.rolreplication
        AND NOT runtime.rolbypassrls,
    NOT pg_has_role(runtime.oid, migration.oid, 'MEMBER'),
    NOT EXISTS (
        SELECT 1
          FROM pg_roles AS inherited
         WHERE inherited.rolname = ANY($3::text[])
           AND pg_has_role(runtime.oid, inherited.oid, 'MEMBER')
    )
FROM pg_roles AS runtime
JOIN pg_roles AS migration ON migration.rolname = $2
WHERE runtime.rolname = $1`,
		runtimeRole,
		migrationRole,
		[]string{"pg_database_owner", "pg_read_all_data", "pg_write_all_data"},
	).Scan(&safeRole, &noMigrationMembership, &noBroadMembership); err != nil {
		t.Fatal("inspect P4-05 disposable runtime role boundary")
	}
	if !safeRole || !noMigrationMembership || !noBroadMembership {
		t.Fatal("P4-05 disposable runtime retained unsafe role membership")
	}

	t.Logf(
		"P4_05_DISPOSABLE_READ_ONLY PASS ledger=31 dirty=false media_features=false retained_enabled_media_override_count=%d",
		enabledMediaOverrides,
	)
}

// TestPostgresMediaLifecycleRuntimeExactACLDisposableReadOnly reuses the exact
// media column-grant matrix, but deliberately selects the non-migrating,
// read-only branch of the helper.
func TestPostgresMediaLifecycleRuntimeExactACLDisposableReadOnly(t *testing.T) {
	requireP405DisposableReadOnlyBoundary(t)
	runPostgresMediaLifecycleRuntimeExactACL(t, false)
}

func requireP405DisposableReadOnlyBoundary(t *testing.T) (string, string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_05_DISPOSABLE_CONFIRM")) !=
		p405DisposableReadOnlyConfirmation {
		t.Fatal("P4_05_DISPOSABLE_CONFIRM is not set to the read-only confirmation")
	}
	for _, key := range p405DisposableConflictingConfirmations {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			t.Fatalf("P4-05 disposable read-only gate refuses confirmation %s", key)
		}
	}
	migrationURL := requireP405DisposableEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireP405DisposableEnvironment(t, "DATABASE_POOL_URL")
	requireP404NeonURLBoundary(t, migrationURL, runtimeURL)
	return migrationURL, runtimeURL
}

func requireP405DisposableEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for the P4-05 disposable read-only gate", key)
	}
	return value
}

func beginP405ReadOnlyTransaction(
	t *testing.T,
	ctx context.Context,
	pool interface {
		BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	},
) pgx.Tx {
	t.Helper()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		t.Fatal("begin P4-05 disposable read-only transaction")
	}
	var readOnly string
	if err := tx.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&readOnly); err != nil {
		_ = tx.Rollback(context.Background())
		t.Fatal("inspect P4-05 disposable transaction mode")
	}
	if readOnly != "on" {
		_ = tx.Rollback(context.Background())
		t.Fatal("P4-05 disposable transaction is not read-only")
	}
	return tx
}
