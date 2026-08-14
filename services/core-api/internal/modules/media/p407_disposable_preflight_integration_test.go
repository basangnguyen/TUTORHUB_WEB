//go:build integration

package media

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	p407OwnerPreflightConfirmation = "I_UNDERSTAND_P4_07_OWNER_PREFLIGHT_ONLY"
	p407FinalSnapshotConfirmation  = "I_UNDERSTAND_P4_07_FINAL_SNAPSHOT_READ_ONLY"
)

// TestPostgresP407DisposableOwnerPreflight authenticates the three isolated
// database principals before migration 000033. It runs only explicit read-only
// transactions and never logs a role, database, endpoint, or connection string.
func TestPostgresP407DisposableOwnerPreflight(t *testing.T) {
	requireP407ReadOnlyConfirmation(
		t,
		"P4_07_OWNER_PREFLIGHT",
		p407OwnerPreflightConfirmation,
	)
	runPostgresP407ReadOnlySnapshot(t, 32, false)
}

// TestPostgresP407DisposableFinalSnapshot verifies ledger, ACL, feature-off,
// and durable-effect convergence without mutating the retained branch.
func TestPostgresP407DisposableFinalSnapshot(t *testing.T) {
	requireP407ReadOnlyConfirmation(
		t,
		"P4_07_FINAL_SNAPSHOT_CONFIRM",
		p407FinalSnapshotConfirmation,
	)
	runPostgresP407ReadOnlySnapshot(t, 33, true)
}

func requireP407ReadOnlyConfirmation(t *testing.T, activeName string, expected string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(activeName)) != expected {
		t.Fatalf("%s is not set to the exact P4-07 read-only confirmation", activeName)
	}
	conflicting := append([]string(nil), p405DisposableConflictingConfirmations...)
	conflicting = append(conflicting,
		"P4_05_DISPOSABLE_CONFIRM",
		"P4_06_DISPOSABLE_CONFIRM",
		"P4_06_OWNER_PREFLIGHT",
		"P4_06_FINAL_SNAPSHOT_CONFIRM",
		"P4_06_ACL_PROVISION_CONFIRM",
		"P4_07_DISPOSABLE_CONFIRM",
		"P4_07_ACL_PROVISION_CONFIRM",
		"P4_07_PROVIDER_CONFIRM",
		"P4_07_SHARED_CONFIRM",
		"P4_07_SHARED_OWNER_PREFLIGHT",
		"P4_07_SHARED_ACL_PROVISION_CONFIRM",
		"P4_07_SHARED_FINAL_SNAPSHOT_CONFIRM",
		"P4_07_OWNER_PREFLIGHT",
		"P4_07_FINAL_SNAPSHOT_CONFIRM",
	)
	for _, name := range conflicting {
		if name == activeName {
			continue
		}
		if strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-07 read-only database gate refuses confirmation %s", name)
		}
	}
}

func runPostgresP407ReadOnlySnapshot(
	t *testing.T,
	expectedVersion int,
	postflight bool,
	additionalPostflightAssertions ...func(*testing.T, context.Context, pgx.Tx),
) {
	t.Helper()
	if !postflight && len(additionalPostflightAssertions) != 0 {
		t.Fatal("P4-07 preflight cannot run postflight assertions")
	}
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
			t.Fatal("inspect P4-07 read-only transaction mode")
		}
		if readOnly != "on" {
			t.Fatal("P4-07 database gate transaction is not read-only")
		}
	}

	migrationRole, migrationDatabase := readP406DatabaseIdentity(t, ctx, migrationTx)
	runtimeRole, runtimeDatabase := readP406DatabaseIdentity(t, ctx, runtimeTx)
	maintenanceRole, maintenanceDatabase := readP406DatabaseIdentity(t, ctx, maintenanceTx)
	if migrationRole == runtimeRole || migrationRole == maintenanceRole ||
		runtimeRole == maintenanceRole || migrationDatabase != runtimeDatabase ||
		migrationDatabase != maintenanceDatabase {
		t.Fatal("P4-07 read-only gate requires three distinct roles on one database")
	}

	var version int
	var dirty bool
	if err := migrationTx.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P4-07 migration ledger")
	}
	if version != expectedVersion || dirty {
		t.Fatal("P4-07 read-only gate found an unexpected migration ledger state")
	}

	targets := p406RelationsForVersion(expectedVersion)
	if expectedVersion >= 33 {
		targets = p407RelationNames()
	}
	assertP406OwnerAuthority(t, ctx, migrationTx, targets)
	assertP406RoleSafety(
		t, ctx, migrationTx, runtimeTx, maintenanceTx,
		migrationRole, runtimeRole, maintenanceRole, targets,
	)
	assertP406MediaFeaturesForcedOff(t)

	if !postflight {
		t.Log("P4_07_OWNER_PREFLIGHT PASS ledger=32 dirty=false three_principals=true url_boundary=true media_features=false")
		return
	}

	for _, expectation := range p407MediaACLExpectations() {
		assertExactMediaACL(t, ctx, runtimeTx, expectation)
	}
	assertP402ProvisionedPublicACL(t, ctx, migrationTx, targets)
	assertP406ProvisionedMaintenanceACL(
		t, ctx, migrationTx, runtimeTx, maintenanceTx, targets,
	)
	assertP406DependencyACL(t, ctx, runtimeTx)
	logP407FinalSnapshot(t, ctx, migrationTx)
	for _, assertion := range additionalPostflightAssertions {
		assertion(t, ctx, migrationTx)
	}
}

func p407RelationNames() []string {
	expectations := p407MediaACLExpectations()
	targets := make([]string, 0, len(expectations))
	for _, expectation := range expectations {
		parts := strings.Split(expectation.relation, ".")
		if len(parts) == 2 && parts[0] == "tutorhub" {
			targets = append(targets, parts[1])
		}
	}
	return targets
}

func logP407FinalSnapshot(t *testing.T, ctx context.Context, ownerTx pgx.Tx) {
	t.Helper()
	var roleAssignments int64
	var moderationReceipts int64
	var retainedPendingFixtureEnds int64
	var unsafeUnresolvedEffects int64
	var moderationRateRows int64
	var enabledMediaOverrides int64
	if err := ownerTx.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_room_role_assignments),
    (SELECT count(*) FROM tutorhub.media_space_mutation_receipts
      WHERE operation IN (
        'lock', 'unlock', 'participant_promote', 'participant_demote',
        'participant_mute', 'participant_remove'
      )),
    (SELECT count(*)
       FROM tutorhub.media_space_mutation_receipts AS receipt
       JOIN tutorhub.tenants AS tenant ON tenant.id = receipt.tenant_id
      WHERE receipt.provider_effect_required
        AND receipt.provider_effect_status = 'pending'
        AND receipt.operation = 'end'
        AND tenant.slug LIKE 'p401-media-%'),
    (SELECT count(*)
       FROM tutorhub.media_space_mutation_receipts AS receipt
       JOIN tutorhub.tenants AS tenant ON tenant.id = receipt.tenant_id
      WHERE receipt.provider_effect_required
        AND receipt.provider_effect_status IN ('pending', 'applying', 'retryable_failed')
        AND NOT (
          receipt.provider_effect_status = 'pending'
          AND receipt.operation = 'end'
          AND tenant.slug LIKE 'p401-media-%'
        )),
    (SELECT count(*) FROM tutorhub.rate_limit_windows
      WHERE purpose LIKE 'media.moderation.%'),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
      WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms')
        AND enabled)`).Scan(
		&roleAssignments,
		&moderationReceipts,
		&retainedPendingFixtureEnds,
		&unsafeUnresolvedEffects,
		&moderationRateRows,
		&enabledMediaOverrides,
	); err != nil {
		t.Fatal("inspect P4-07 final aggregate snapshot")
	}
	if unsafeUnresolvedEffects != 0 {
		t.Fatalf(
			"P4-07 final snapshot found unsafe unresolved effects: unsafe_unresolved_effects=%d retained_pending_fixture_ends=%d retained_enabled_media_overrides=%d",
			unsafeUnresolvedEffects,
			retainedPendingFixtureEnds,
			enabledMediaOverrides,
		)
	}
	t.Logf(
		"P4_07_FINAL_SNAPSHOT PASS ledger=33 dirty=false media_features=false retained_enabled_media_overrides=%d role_assignments=%d moderation_receipts=%d unsafe_unresolved_effects=0 retained_pending_fixture_ends=%d moderation_rate_rows=%d",
		enabledMediaOverrides,
		roleAssignments,
		moderationReceipts,
		retainedPendingFixtureEnds,
		moderationRateRows,
	)
}
