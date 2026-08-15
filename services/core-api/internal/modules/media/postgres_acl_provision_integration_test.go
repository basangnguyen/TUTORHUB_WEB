//go:build integration

package media

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	p404ACLProvisionConfirmation       = "I_UNDERSTAND_P4_04_ACL_PROVISION_DISPOSABLE_ONLY"
	p404SharedACLProvisionConfirmation = "I_UNDERSTAND_P4_04_ACL_PROVISION_SHARED_STAGING_ONLY"
	p406ACLProvisionConfirmation       = "I_UNDERSTAND_P4_06_ACL_PROVISION_DISPOSABLE_ONLY"
	p407ACLProvisionConfirmation       = "I_UNDERSTAND_P4_07_ACL_PROVISION_DISPOSABLE_ONLY"
	p410ACLProvisionConfirmation       = "I_UNDERSTAND_P4_10_ACL_PROVISION_DISPOSABLE_ONLY"
	p410SharedACLProvisionConfirmation = "I_UNDERSTAND_P4_10_ACL_PROVISION_SHARED_STAGING_ONLY"
)

type mediaACLProvisionConfiguration struct {
	expectedVersion int
	expectations    []mediaACLExpectation
}

// TestProvisionPostgresMediaLifecycleRuntimeExactACL is an explicitly opted-in
// disposable-only acceptance gate. It derives the runtime database identity
// from the authenticated runtime connection and never prints that identity or
// either database URL.
func TestProvisionPostgresMediaLifecycleRuntimeExactACL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_04_ACL_PROVISION_CONFIRM")) != p404ACLProvisionConfirmation {
		t.Skip("P4_04_ACL_PROVISION_CONFIRM is not set to the disposable-only ACL confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_02_DISPOSABLE_CONFIRM")) != p402DisposableConfirmation {
		t.Skip("P4_02_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t)
}

// TestProvisionPostgresMediaSignalsExactACL is the P4-06 disposable-only
// wrapper around the reviewed media ACL provisioner. It requires fresh P4-06
// confirmations so an older phase's opt-in cannot authorize the new signal
// tables and maintenance functions.
func TestProvisionPostgresMediaSignalsExactACL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_06_DISPOSABLE_CONFIRM")) != p406DisposableConfirmation {
		t.Skip("P4_06_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_06_ACL_PROVISION_CONFIRM")) != p406ACLProvisionConfirmation {
		t.Skip("P4_06_ACL_PROVISION_CONFIRM is not set to the disposable-only ACL confirmation")
	}
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t)
}

// TestProvisionPostgresMediaLifecycleRuntimeExactACLShared is retained only to
// fail closed when an old P4-04 runbook is used against the expanded P4-06 ACL
// union.
func TestProvisionPostgresMediaLifecycleRuntimeExactACLShared(t *testing.T) {
	t.Fatal("P4-04 shared ACL provisioning is retired; use the P4-06 shared wrapper")
}

// TestProvisionPostgresMediaSignalsExactACLShared is the only shared-staging
// mutation wrapper for the P4-06 ACL union. It requires a fresh P4-06 action
// confirmation and refuses every disposable or older-phase confirmation.
func TestProvisionPostgresMediaSignalsExactACLShared(t *testing.T) {
	requireP406SharedConfirmation(
		t,
		"P4_06_SHARED_ACL_PROVISION_CONFIRM",
		p406SharedACLProvisionConfirmation,
	)
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t)
}

// TestProvisionPostgresMediaModerationExactACL provisions only the P4-07 ACL
// union on the current P4-08-compatible ledger. A new confirmation prevents
// an older phase's opt-in from authorizing the moderation table and receipt
// columns.
func TestProvisionPostgresMediaModerationExactACL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_07_DISPOSABLE_CONFIRM")) != "I_UNDERSTAND_P4_07_DISPOSABLE_ONLY" {
		t.Skip("P4_07_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_07_ACL_PROVISION_CONFIRM")) != p407ACLProvisionConfirmation {
		t.Skip("P4_07_ACL_PROVISION_CONFIRM is not set to the disposable-only ACL confirmation")
	}
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t, mediaACLProvisionConfiguration{
		expectedVersion: 35,
		expectations:    p407MediaACLExpectations(),
	})
}

// TestProvisionPostgresMediaModerationExactACLShared is the P4-07
// shared-staging mutation wrapper. It is deliberately independent from the
// P4-06 confirmation helper so stale P4-06 authorization cannot cross the
// phase boundary.
func TestProvisionPostgresMediaModerationExactACLShared(t *testing.T) {
	requireP407SharedConfirmation(
		t,
		"P4_07_SHARED_ACL_PROVISION_CONFIRM",
		p407SharedACLProvisionConfirmation,
	)
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t, mediaACLProvisionConfiguration{
		expectedVersion: 35,
		expectations:    p407MediaACLExpectations(),
	})
}

func TestProvisionPostgresMediaDiagnosticsExactACL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_10_DISPOSABLE_CONFIRM")) != "I_UNDERSTAND_P4_10_DISPOSABLE_ONLY" {
		t.Skip("P4_10_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_10_ACL_PROVISION_CONFIRM")) != p410ACLProvisionConfirmation {
		t.Skip("P4_10_ACL_PROVISION_CONFIRM is not set to the disposable-only ACL confirmation")
	}
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t, mediaACLProvisionConfiguration{
		expectedVersion: 36,
		expectations:    p410MediaACLExpectations(),
	})
}

func TestProvisionPostgresMediaDiagnosticsExactACLShared(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_10_SHARED_ACL_PROVISION_CONFIRM")) != p410SharedACLProvisionConfirmation {
		t.Skip("P4_10_SHARED_ACL_PROVISION_CONFIRM is not set to the shared-staging confirmation")
	}
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t, mediaACLProvisionConfiguration{
		expectedVersion: 36,
		expectations:    p410MediaACLExpectations(),
	})
}

func runProvisionPostgresMediaLifecycleRuntimeExactACL(
	t *testing.T,
	configurations ...mediaACLProvisionConfiguration,
) {
	t.Helper()
	configuration := mediaACLProvisionConfiguration{
		expectedVersion: 32,
		expectations:    p402MediaACLExpectations(),
	}
	if len(configurations) > 1 {
		t.Fatal("media ACL provisioner accepts at most one configuration")
	}
	if len(configurations) == 1 {
		configuration = configurations[0]
	}
	if configuration.expectedVersion < 1 || len(configuration.expectations) == 0 {
		t.Fatal("media ACL provisioner configuration is invalid")
	}
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406ProvisionDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	maintenancePool := openMediaIntegrationPool(t, ctx, maintenanceURL)
	t.Cleanup(maintenancePool.Close)

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
		t.Fatal("P4-04 ACL provisioning requires distinct roles on the same database")
	}
	var maintenanceRole, maintenanceDatabase string
	if err := maintenancePool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&maintenanceRole,
		&maintenanceDatabase,
	); err != nil {
		t.Fatal("inspect P4-06 maintenance database identity")
	}
	if maintenanceRole == migrationRole || maintenanceRole == runtimeRole ||
		maintenanceDatabase != migrationDatabase {
		t.Fatal("P4-06 ACL provisioning requires three distinct roles on the same database")
	}

	var version int
	var dirty bool
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P4-04 migration ledger")
	}
	if configuration.expectedVersion == 32 {
		if version != 32 || dirty {
			t.Fatal("P4-06 ACL provisioning requires ledger 32 false")
		}
	} else if version != configuration.expectedVersion || dirty {
		t.Fatalf("media ACL provisioning requires ledger %d false", configuration.expectedVersion)
	}

	expectations := configuration.expectations
	targets := make([]string, 0, len(expectations))
	for _, expectation := range expectations {
		parts := strings.Split(expectation.relation, ".")
		if len(parts) != 2 || parts[0] != "tutorhub" || strings.TrimSpace(parts[1]) == "" {
			t.Fatal("P4-04 ACL expectation contains an invalid relation")
		}
		targets = append(targets, parts[1])
	}

	var safeRole, noMigrationMembership, noTableOwnership bool
	if err := migrationPool.QueryRow(ctx, `SELECT
    NOT runtime.rolsuper AND NOT runtime.rolcreaterole AND NOT runtime.rolcreatedb
        AND NOT runtime.rolreplication AND NOT runtime.rolbypassrls,
    NOT pg_has_role(runtime.oid, migration.oid, 'MEMBER'),
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
WHERE runtime.rolname = $1`, runtimeRole, migrationRole, targets).Scan(
		&safeRole,
		&noMigrationMembership,
		&noTableOwnership,
	); err != nil {
		t.Fatal("inspect P4-04 runtime role safety")
	}
	if !safeRole || !noMigrationMembership || !noTableOwnership {
		t.Fatal("P4-04 runtime role safety preflight failed")
	}
	var safeMaintenanceRole, noUnsafeMaintenanceMembership, noMaintenanceTableOwnership bool
	if err := migrationPool.QueryRow(ctx, `SELECT
    NOT maintenance.rolsuper AND NOT maintenance.rolcreaterole AND NOT maintenance.rolcreatedb
        AND NOT maintenance.rolreplication AND NOT maintenance.rolbypassrls,
    NOT pg_has_role(maintenance.oid, migration.oid, 'MEMBER')
        AND NOT pg_has_role(maintenance.oid, runtime.oid, 'MEMBER')
        AND NOT pg_has_role(runtime.oid, maintenance.oid, 'MEMBER'),
    NOT EXISTS (
        SELECT 1
        FROM pg_class AS relation
        JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = 'tutorhub'
          AND relation.relname = ANY($4::text[])
          AND relation.relowner = maintenance.oid
    )
FROM pg_roles AS maintenance
JOIN pg_roles AS migration ON migration.rolname = $2
JOIN pg_roles AS runtime ON runtime.rolname = $3
WHERE maintenance.rolname = $1`, maintenanceRole, migrationRole, runtimeRole, targets).Scan(
		&safeMaintenanceRole,
		&noUnsafeMaintenanceMembership,
		&noMaintenanceTableOwnership,
	); err != nil {
		t.Fatal("inspect P4-06 maintenance role safety")
	}
	if !safeMaintenanceRole || !noUnsafeMaintenanceMembership || !noMaintenanceTableOwnership {
		t.Fatal("P4-06 maintenance role safety preflight failed")
	}

	runtimeIdentifier := pgx.Identifier{runtimeRole}.Sanitize()
	maintenanceIdentifier := pgx.Identifier{maintenanceRole}.Sanitize()
	tx, err := migrationPool.Begin(ctx)
	if err != nil {
		t.Fatal("begin P4-04 ACL provisioning transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	execP402ACLStatement(
		t,
		ctx,
		tx,
		fmt.Sprintf(`GRANT USAGE ON SCHEMA tutorhub TO %s`, runtimeIdentifier),
		"grant P4-04 runtime schema usage",
	)
	execP402ACLStatement(
		t,
		ctx,
		tx,
		fmt.Sprintf(`REVOKE CREATE ON SCHEMA tutorhub FROM %s`, runtimeIdentifier),
		"revoke P4-04 runtime schema create",
	)
	execP402ACLStatement(
		t,
		ctx,
		tx,
		fmt.Sprintf(`GRANT USAGE ON SCHEMA tutorhub TO %s`, maintenanceIdentifier),
		"grant P4-06 maintenance schema usage",
	)
	execP402ACLStatement(
		t,
		ctx,
		tx,
		fmt.Sprintf(`REVOKE CREATE ON SCHEMA tutorhub FROM %s`, maintenanceIdentifier),
		"revoke P4-06 maintenance schema create",
	)

	for _, expectation := range expectations {
		parts := strings.Split(expectation.relation, ".")
		relationIdentifier := pgx.Identifier{parts[0], parts[1]}.Sanitize()
		columns := p402ACLRelationColumns(t, ctx, tx, parts[0], parts[1])
		columnIdentifiers := make([]string, 0, len(columns))
		for _, column := range columns {
			columnIdentifiers = append(columnIdentifiers, pgx.Identifier{column}.Sanitize())
		}
		allColumns := strings.Join(columnIdentifiers, ", ")

		execP402ACLStatement(
			t,
			ctx,
			tx,
			fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %s FROM %s`, relationIdentifier, runtimeIdentifier),
			"revoke P4-04 runtime table privileges",
		)
		execP402ACLStatement(
			t,
			ctx,
			tx,
			fmt.Sprintf(
				`REVOKE SELECT (%[1]s), INSERT (%[1]s), UPDATE (%[1]s), REFERENCES (%[1]s) ON TABLE %[2]s FROM %[3]s`,
				allColumns,
				relationIdentifier,
				runtimeIdentifier,
			),
			"revoke P4-04 runtime column privileges",
		)
		execP402ACLStatement(
			t,
			ctx,
			tx,
			fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %s FROM %s`, relationIdentifier, maintenanceIdentifier),
			"revoke P4-06 maintenance table privileges",
		)
		execP402ACLStatement(
			t,
			ctx,
			tx,
			fmt.Sprintf(
				`REVOKE SELECT (%[1]s), INSERT (%[1]s), UPDATE (%[1]s), REFERENCES (%[1]s) ON TABLE %[2]s FROM %[3]s`,
				allColumns,
				relationIdentifier,
				maintenanceIdentifier,
			),
			"revoke P4-06 maintenance column privileges",
		)
		execP402ACLStatement(
			t,
			ctx,
			tx,
			fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %s FROM PUBLIC`, relationIdentifier),
			"revoke P4-04 PUBLIC table privileges",
		)
		execP402ACLStatement(
			t,
			ctx,
			tx,
			fmt.Sprintf(
				`REVOKE SELECT (%[1]s), INSERT (%[1]s), UPDATE (%[1]s), REFERENCES (%[1]s) ON TABLE %[2]s FROM PUBLIC`,
				allColumns,
				relationIdentifier,
			),
			"revoke P4-04 PUBLIC column privileges",
		)

		grantP402ACLColumns(t, ctx, tx, relationIdentifier, runtimeIdentifier, "SELECT", expectation.selectColumns)
		grantP402ACLColumns(t, ctx, tx, relationIdentifier, runtimeIdentifier, "INSERT", expectation.insertColumns)
		grantP402ACLColumns(t, ctx, tx, relationIdentifier, runtimeIdentifier, "UPDATE", expectation.updateColumns)
	}

	for _, signature := range mediaPurgeFunctionSignatures(targets) {
		for _, grantee := range []string{"PUBLIC", runtimeIdentifier, maintenanceIdentifier} {
			execP402ACLStatement(
				t,
				ctx,
				tx,
				fmt.Sprintf(`REVOKE ALL ON FUNCTION %s FROM %s`, signature, grantee),
				"revoke P4-06 media purge function privileges",
			)
		}
		execP402ACLStatement(
			t,
			ctx,
			tx,
			fmt.Sprintf(`GRANT EXECUTE ON FUNCTION %s TO %s`, signature, maintenanceIdentifier),
			"grant P4-06 maintenance function execution",
		)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit P4-04 ACL provisioning transaction")
	}

	for _, expectation := range expectations {
		assertExactMediaACL(t, ctx, runtimePool, expectation)
	}
	assertP402ProvisionedPublicACL(t, ctx, migrationPool, targets)
	assertP406ProvisionedMaintenanceACL(t, ctx, migrationPool, runtimePool, maintenancePool, targets)
}

func p402MediaACLExpectations() []mediaACLExpectation {
	return []mediaACLExpectation{
		{
			relation: "tutorhub.media_spaces",
			selectColumns: []string{
				"class_id", "create_idempotency_key", "create_request_fingerprint",
				"created_at", "created_by", "id", "lobby_enabled", "locked",
				"source_class_session_id", "source_kind", "source_occurrence_key",
				"source_series_id", "source_study_meeting_id", "status", "tenant_id",
				"updated_at", "version",
			},
			insertColumns: []string{
				"class_id", "create_idempotency_key", "create_request_fingerprint",
				"created_at", "created_by", "id", "source_class_session_id", "source_kind",
				"source_occurrence_key", "source_series_id", "source_study_meeting_id",
				"tenant_id", "updated_at", "updated_by",
			},
			updateColumns: []string{
				"cancelled_at", "cancelled_by", "ended_at", "ended_by", "locked", "opened_at",
				"opened_by", "status", "updated_at", "updated_by", "version",
			},
		},
		{
			relation: "tutorhub.media_room_instances",
			selectColumns: []string{
				"activated_at", "attempt_number", "closing_at", "created_at", "ended_at", "failed_at", "id",
				"last_signal_sequence", "next_roster_sequence", "projection_version", "provider_kind",
				"provider_room_name", "provider_room_sid", "space_id", "status", "tenant_id", "updated_at", "version",
			},
			insertColumns: []string{
				"created_at", "created_by", "id", "provider_room_name", "space_id", "tenant_id",
				"updated_at", "updated_by", "attempt_number",
			},
			updateColumns: []string{
				"activated_at", "closing_at", "ended_at", "failed_at", "failure_code",
				"last_signal_sequence", "next_roster_sequence", "projection_version",
				"provider_room_sid", "status", "updated_at", "updated_by", "version",
			},
		},
		{
			relation: "tutorhub.media_space_members",
			selectColumns: []string{
				"created_at", "space_id", "status", "tenant_id", "updated_at", "user_id", "version",
			},
			insertColumns: []string{
				"created_at", "invited_by", "space_id", "status", "tenant_id", "updated_at",
				"user_id", "version",
			},
			updateColumns: []string{
				"revoked_at", "revoked_by", "status", "updated_at", "version",
			},
		},
		{
			relation: "tutorhub.media_admission_requests",
			selectColumns: []string{
				"created_at", "id", "idempotency_key", "request_fingerprint", "resolution_code",
				"room_instance_id", "space_id", "status", "tenant_id", "updated_at", "user_id", "version",
			},
			insertColumns: []string{
				"created_at", "id", "idempotency_key", "request_fingerprint",
				"room_instance_id", "space_id", "status", "tenant_id", "updated_at",
				"user_id", "version",
			},
			updateColumns: []string{
				"resolution_code", "resolved_at", "resolved_by", "status", "updated_at", "version",
			},
		},
		{
			relation: "tutorhub.media_participant_sessions",
			selectColumns: []string{
				"admission_request_id", "admitted_at", "capacity_reserved", "connected_at",
				"created_at", "failure_code", "id", "instance_role", "join_attempt_id",
				"joining_at", "participant_key", "provider_participant_identity", "reconnecting_at", "removed_by",
				"rejoin_restored_at", "room_instance_id", "space_id", "status",
				"tenant_id", "terminal_at", "updated_at", "user_id", "version", "roster_sequence",
			},
			insertColumns: []string{
				"admission_request_id", "admitted_at", "capacity_reserved", "created_at",
				"id", "instance_role", "join_attempt_id", "participant_key", "provider_participant_identity",
				"room_instance_id", "space_id", "status", "tenant_id", "updated_at", "user_id",
				"version", "roster_sequence",
			},
			updateColumns: []string{
				"admitted_at", "capacity_reserved", "connected_at", "failure_code", "instance_role",
				"joining_at", "reconnecting_at", "rejoin_restored_at", "rejoin_restored_by",
				"removed_by", "status", "terminal_at", "updated_at", "version",
			},
		},
		{
			relation: "tutorhub.media_space_mutation_receipts",
			selectColumns: []string{
				"actor_user_id", "idempotency_key", "operation", "request_fingerprint", "space_id",
				"tenant_id",
			},
			insertColumns: []string{
				"actor_user_id", "created_at", "idempotency_key", "operation", "request_fingerprint",
				"result_room_instance_id", "result_space_version", "space_id", "tenant_id",
			},
		},
		{
			relation: "tutorhub.media_provider_webhook_receipts",
			selectColumns: []string{
				"disposition", "event_id", "event_type", "occurred_at", "participant_session_id",
				"provider_kind", "received_at", "retention_until", "room_instance_id", "space_id",
				"tenant_id",
			},
			insertColumns: []string{
				"disposition", "event_id", "event_type", "occurred_at", "participant_session_id",
				"provider_kind", "received_at", "retention_until", "room_instance_id", "space_id",
				"tenant_id",
			},
		},
		{
			relation: "tutorhub.media_participant_hand_states",
			selectColumns: []string{
				"is_raised", "participant_session_id", "raised_at", "room_instance_id", "signal_sequence",
				"space_id", "tenant_id",
			},
			insertColumns: []string{
				"participant_session_id", "raised_at", "room_instance_id", "signal_sequence",
				"space_id", "tenant_id",
			},
			updateColumns: []string{"is_raised", "raised_at", "signal_sequence"},
		},
		{
			relation: "tutorhub.media_reaction_events",
			selectColumns: []string{
				"accepted_at", "expires_at", "reaction", "room_instance_id", "signal_sequence",
				"space_id", "tenant_id",
			},
			insertColumns: []string{
				"accepted_at", "expires_at", "id", "participant_session_id", "reaction",
				"room_instance_id", "signal_sequence", "space_id", "tenant_id",
			},
		},
		{
			relation: "tutorhub.media_signal_mutation_receipts",
			selectColumns: []string{
				"actor_user_id", "idempotency_key", "request_fingerprint", "room_instance_id", "tenant_id",
			},
			insertColumns: []string{
				"actor_user_id", "created_at", "idempotency_key", "kind", "request_fingerprint",
				"result_projection_version", "result_signal_sequence", "retention_until", "room_instance_id", "space_id", "tenant_id",
			},
		},
		{relation: "tutorhub.livekit_webhook_events"},
	}
}

func p407MediaACLExpectations() []mediaACLExpectation {
	expectations := p402MediaACLExpectations()
	for index := range expectations {
		if expectations[index].relation != "tutorhub.media_space_mutation_receipts" {
			continue
		}
		expectations[index].selectColumns = []string{
			"actor_user_id", "created_at", "idempotency_key", "operation",
			"provider_effect_attempts", "provider_effect_error_code", "provider_effect_lease_until",
			"provider_effect_required", "provider_effect_status", "provider_effect_updated_at",
			"request_fingerprint", "result_instance_role", "result_locked",
			"result_participant_version", "result_projection_version",
			"result_role_assignment_version", "result_room_instance_id",
			"result_room_instance_version", "result_space_version", "space_id",
			"target_participant_session_id", "tenant_id",
		}
		expectations[index].insertColumns = []string{
			"actor_user_id", "created_at", "idempotency_key", "operation",
			"provider_effect_required", "provider_effect_status", "provider_effect_updated_at",
			"request_fingerprint", "result_instance_role", "result_locked",
			"result_participant_version", "result_projection_version",
			"result_role_assignment_version", "result_room_instance_id",
			"result_room_instance_version", "result_space_version", "space_id",
			"target_participant_session_id", "tenant_id",
		}
		expectations[index].updateColumns = []string{
			"provider_effect_attempts", "provider_effect_error_code", "provider_effect_lease_until",
			"provider_effect_status", "provider_effect_updated_at",
		}
	}

	return append(expectations, mediaACLExpectation{
		relation: "tutorhub.media_room_role_assignments",
		selectColumns: []string{
			"assigned_at", "assigned_by", "assigned_role", "reason_code", "revoked_at",
			"revoked_by", "room_instance_id", "space_id", "status", "tenant_id",
			"updated_at", "user_id", "version",
		},
		insertColumns: []string{
			"assigned_at", "assigned_by", "assigned_role", "reason_code", "room_instance_id",
			"space_id", "status", "tenant_id", "updated_at", "user_id", "version",
		},
		updateColumns: []string{
			"assigned_at", "assigned_by", "assigned_role", "reason_code", "revoked_at",
			"revoked_by", "status", "updated_at", "version",
		},
	})
}

func p410MediaACLExpectations() []mediaACLExpectation {
	return append(p407MediaACLExpectations(), mediaACLExpectation{
		relation: "tutorhub.media_join_diagnostics",
		selectColumns: []string{
			"duration_ms", "error_code", "id", "join_attempt_id", "media_path",
			"network_quality", "outcome", "participant_session_id", "recorded_at",
			"retention_until", "room_instance_id", "space_id", "stage", "tenant_id",
		},
		insertColumns: []string{
			"duration_ms", "error_code", "id", "join_attempt_id", "media_path",
			"network_quality", "outcome", "participant_session_id", "recorded_at",
			"retention_until", "room_instance_id", "space_id", "stage", "tenant_id",
		},
	})
}

func p402ACLRelationColumns(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	schema string,
	relation string,
) []string {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT column_name
FROM information_schema.columns
WHERE table_schema = $1
  AND table_name = $2
ORDER BY ordinal_position`, schema, relation)
	if err != nil {
		t.Fatal("inspect P4-04 ACL relation columns")
	}
	defer rows.Close()

	columns := make([]string, 0)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal("read P4-04 ACL relation column")
		}
		columns = append(columns, column)
	}
	if rows.Err() != nil {
		t.Fatal("iterate P4-04 ACL relation columns")
	}
	if len(columns) == 0 {
		t.Fatal("P4-04 ACL target relation has no columns")
	}
	return columns
}

func grantP402ACLColumns(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	relationIdentifier string,
	runtimeIdentifier string,
	privilege string,
	columns []string,
) {
	t.Helper()
	if len(columns) == 0 {
		return
	}
	columnIdentifiers := make([]string, 0, len(columns))
	for _, column := range columns {
		columnIdentifiers = append(columnIdentifiers, pgx.Identifier{column}.Sanitize())
	}
	execP402ACLStatement(
		t,
		ctx,
		tx,
		fmt.Sprintf(
			`GRANT %s (%s) ON TABLE %s TO %s`,
			privilege,
			strings.Join(columnIdentifiers, ", "),
			relationIdentifier,
			runtimeIdentifier,
		),
		"grant exact P4-04 runtime column privileges",
	)
}

func execP402ACLStatement(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	statement string,
	failure string,
) {
	t.Helper()
	if _, err := tx.Exec(ctx, statement); err != nil {
		t.Fatal(failure)
	}
}

func assertP402ProvisionedPublicACL(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	targets []string,
) {
	t.Helper()
	var tableGrants, columnGrants int
	if err := pool.QueryRow(ctx, `SELECT
    (SELECT count(*)
     FROM information_schema.table_privileges
     WHERE table_schema = 'tutorhub'
       AND table_name = ANY($1::text[])
       AND grantee = 'PUBLIC'),
    (SELECT count(*)
     FROM information_schema.column_privileges
     WHERE table_schema = 'tutorhub'
       AND table_name = ANY($1::text[])
       AND grantee = 'PUBLIC')`, targets).Scan(&tableGrants, &columnGrants); err != nil {
		t.Fatal("inspect provisioned P4-04 PUBLIC ACL")
	}
	if tableGrants != 0 || columnGrants != 0 {
		t.Fatal("provisioned P4-04 PUBLIC ACL is not empty")
	}
}

func assertP406ProvisionedMaintenanceACL(
	t *testing.T,
	ctx context.Context,
	migrationPool mediaACLRowQuerier,
	runtimePool mediaACLRowQuerier,
	maintenancePool mediaACLRowQuerier,
	targets []string,
) {
	t.Helper()
	var migrationRole string
	if err := migrationPool.QueryRow(ctx, `SELECT current_user`).Scan(&migrationRole); err != nil {
		t.Fatal("inspect P4-06 migration role")
	}

	var maintenanceSchemaUsage, maintenanceSchemaCreate bool
	var maintenanceRelationsDenied bool
	if err := maintenancePool.QueryRow(ctx, `SELECT
    bool_and(has_schema_privilege(current_user, 'tutorhub', 'USAGE')),
    bool_or(has_schema_privilege(current_user, 'tutorhub', 'CREATE')),
    bool_and(
        NOT has_table_privilege(
            current_user,
            format('tutorhub.%I', relation_name),
            'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'
        )
        AND NOT has_any_column_privilege(
            current_user,
            format('tutorhub.%I', relation_name),
            'SELECT,INSERT,UPDATE,REFERENCES'
        )
    )
FROM unnest($1::text[]) AS relation(relation_name)`, targets).Scan(
		&maintenanceSchemaUsage,
		&maintenanceSchemaCreate,
		&maintenanceRelationsDenied,
	); err != nil {
		t.Fatal("inspect exact P4-06 maintenance relation ACL")
	}
	if !maintenanceSchemaUsage || maintenanceSchemaCreate || !maintenanceRelationsDenied {
		t.Fatal("P4-06 maintenance role must have schema USAGE and no media relation privileges")
	}

	signatures := mediaPurgeFunctionSignatures(targets)
	var runtimeExecute, maintenanceExecute bool
	if err := runtimePool.QueryRow(ctx, `SELECT bool_or(
    has_function_privilege(current_user, signature, 'EXECUTE')
)
FROM unnest($1::text[]) AS function_signature(signature)`, signatures).Scan(
		&runtimeExecute,
	); err != nil {
		t.Fatal("inspect P4-06 runtime purge ACL")
	}
	if err := maintenancePool.QueryRow(ctx, `SELECT bool_and(
    has_function_privilege(current_user, signature, 'EXECUTE')
)
FROM unnest($1::text[]) AS function_signature(signature)`, signatures).Scan(
		&maintenanceExecute,
	); err != nil {
		t.Fatal("inspect P4-06 maintenance purge ACL")
	}
	if runtimeExecute || !maintenanceExecute {
		t.Fatal("P4-06 purge functions must be executable only by the maintenance role")
	}

	var functionCount int
	var reviewedMetadata bool
	if err := migrationPool.QueryRow(ctx, `SELECT
    count(*),
    bool_and(
        function.prosecdef
        AND function.proconfig = ARRAY['search_path=pg_catalog, pg_temp']::text[]
        AND owner.rolname = $2
        AND NOT EXISTS (
            SELECT 1
            FROM aclexplode(COALESCE(
                function.proacl,
                acldefault('f'::"char", function.proowner)
            )) AS privilege
            WHERE privilege.grantee = 0
              AND privilege.privilege_type = 'EXECUTE'
        )
    )
FROM unnest($1::text[]) AS function_signature(signature)
JOIN pg_proc AS function
  ON function.oid = function_signature.signature::regprocedure
JOIN pg_roles AS owner ON owner.oid = function.proowner`, signatures, migrationRole).Scan(
		&functionCount,
		&reviewedMetadata,
	); err != nil {
		t.Fatal("inspect P4-06 purge function metadata")
	}
	if functionCount != len(signatures) || !reviewedMetadata {
		t.Fatal("P4-06 purge functions violate the reviewed SECURITY DEFINER boundary")
	}
}

func mediaPurgeFunctionSignatures(targets []string) []string {
	signatures := []string{
		"tutorhub.purge_expired_media_reactions(integer)",
		"tutorhub.purge_expired_media_signal_receipts(integer)",
	}
	for _, target := range targets {
		if target == "media_join_diagnostics" {
			return append(signatures, "tutorhub.purge_expired_media_join_diagnostics(integer)")
		}
	}
	return signatures
}

func requireP406MaintenanceNeonURLBoundary(
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
	migrationEndpoint, migrationPooled, migrationValid := p404NeonEndpoint(migrationConfig.ConnConfig.Host)
	runtimeEndpoint, runtimePooled, runtimeValid := p404NeonEndpoint(runtimeConfig.ConnConfig.Host)
	maintenanceEndpoint, maintenancePooled, maintenanceValid := p404NeonEndpoint(maintenanceConfig.ConnConfig.Host)
	if !migrationValid || !runtimeValid || !maintenanceValid ||
		migrationPooled || !runtimePooled || maintenancePooled ||
		migrationEndpoint != runtimeEndpoint || migrationEndpoint != maintenanceEndpoint ||
		migrationConfig.ConnConfig.Database != runtimeConfig.ConnConfig.Database ||
		migrationConfig.ConnConfig.Database != maintenanceConfig.ConnConfig.Database ||
		maintenanceConfig.ConnConfig.User == migrationConfig.ConnConfig.User ||
		maintenanceConfig.ConnConfig.User == runtimeConfig.ConnConfig.User ||
		maintenanceConfig.ConnConfig.TLSConfig == nil {
		t.Fatal("P4-06 requires direct migration/maintenance and pooled runtime URLs with distinct roles on one Neon database")
	}
}

// requireP406ProvisionDatabaseURLBoundary keeps real acceptance runs pinned to
// one Neon endpoint while permitting only the explicitly named ephemeral
// PostgreSQL database and roles created by the GitHub Actions workflow. The CI
// exception cannot be enabled by the disposable confirmation variables alone.
func requireP406ProvisionDatabaseURLBoundary(
	t *testing.T,
	migrationURL string,
	runtimeURL string,
	maintenanceURL string,
) {
	t.Helper()
	if p406IsolatedGitHubActionsDatabaseBoundary(migrationURL, runtimeURL, maintenanceURL) {
		return
	}
	requireP404NeonURLBoundary(t, migrationURL, runtimeURL)
	requireP406MaintenanceNeonURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
}

func p406IsolatedGitHubActionsDatabaseBoundary(
	migrationURL string,
	runtimeURL string,
	maintenanceURL string,
) bool {
	return p406IsolatedGitHubActionsDatabaseBoundaryForRuntimeRole(
		migrationURL,
		runtimeURL,
		maintenanceURL,
		"tutorhub_conversation_runtime_ci",
	)
}

func requireP406SignalFixtureDatabaseURLBoundary(
	t *testing.T,
	migrationURL string,
	runtimeURL string,
	maintenanceURL string,
) {
	t.Helper()
	if p406IsolatedGitHubActionsSignalFixtureDatabaseBoundary(
		migrationURL,
		runtimeURL,
		maintenanceURL,
	) {
		return
	}
	requireP406ProvisionDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
}

func p406IsolatedGitHubActionsSignalFixtureDatabaseBoundary(
	migrationURL string,
	runtimeURL string,
	maintenanceURL string,
) bool {
	return p406IsolatedGitHubActionsDatabaseBoundaryForRuntimeRole(
		migrationURL,
		runtimeURL,
		maintenanceURL,
		"tutorhub_media_fixture_ci",
	)
}

func p406IsolatedGitHubActionsDatabaseBoundaryForRuntimeRole(
	migrationURL string,
	runtimeURL string,
	maintenanceURL string,
	runtimeRole string,
) bool {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("CI")), "true") ||
		!strings.EqualFold(strings.TrimSpace(os.Getenv("GITHUB_ACTIONS")), "true") {
		return false
	}
	configs := make([]*pgxpool.Config, 0, 3)
	for _, databaseURL := range []string{migrationURL, runtimeURL, maintenanceURL} {
		config, err := pgxpool.ParseConfig(databaseURL)
		if err != nil {
			return false
		}
		configs = append(configs, config)
	}
	wantUsers := []string{
		"tutorhub",
		runtimeRole,
		"tutorhub_media_maintenance_ci",
	}
	for index, config := range configs {
		if !strings.EqualFold(strings.TrimSpace(config.ConnConfig.Host), "localhost") ||
			config.ConnConfig.Port != 5432 ||
			config.ConnConfig.Database != "tutorhub_test" ||
			config.ConnConfig.User != wantUsers[index] ||
			config.ConnConfig.TLSConfig != nil {
			return false
		}
	}
	return true
}

func TestP406IsolatedGitHubActionsDatabaseBoundaryIsExact(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("GITHUB_ACTIONS", "true")
	migrationURL := "postgresql://tutorhub:ignored@localhost:5432/tutorhub_test?sslmode=disable"
	runtimeURL := "postgresql://tutorhub_conversation_runtime_ci:ignored@localhost:5432/tutorhub_test?sslmode=disable"
	maintenanceURL := "postgresql://tutorhub_media_maintenance_ci:ignored@localhost:5432/tutorhub_test?sslmode=disable"
	if !p406IsolatedGitHubActionsDatabaseBoundary(migrationURL, runtimeURL, maintenanceURL) {
		t.Fatal("expected exact GitHub Actions database boundary to pass")
	}

	for name, candidate := range map[string]struct {
		githubActions  bool
		migrationURL   string
		runtimeURL     string
		maintenanceURL string
	}{
		"non GitHub Actions": {
			migrationURL: migrationURL, runtimeURL: runtimeURL, maintenanceURL: maintenanceURL,
		},
		"remote host": {
			githubActions:  true,
			migrationURL:   migrationURL,
			runtimeURL:     strings.Replace(runtimeURL, "localhost", "database.example", 1),
			maintenanceURL: maintenanceURL,
		},
		"wrong database": {
			githubActions:  true,
			migrationURL:   migrationURL,
			runtimeURL:     strings.Replace(runtimeURL, "tutorhub_test", "postgres", 1),
			maintenanceURL: maintenanceURL,
		},
		"stateful fixture role": {
			githubActions:  true,
			migrationURL:   migrationURL,
			runtimeURL:     strings.Replace(runtimeURL, "tutorhub_conversation_runtime_ci", "tutorhub_media_fixture_ci", 1),
			maintenanceURL: maintenanceURL,
		},
		"reused role": {
			githubActions: true,
			migrationURL:  migrationURL,
			runtimeURL:    runtimeURL,
			maintenanceURL: strings.Replace(
				maintenanceURL,
				"tutorhub_media_maintenance_ci",
				"tutorhub",
				1,
			),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CI", "true")
			t.Setenv("GITHUB_ACTIONS", fmt.Sprintf("%t", candidate.githubActions))
			if p406IsolatedGitHubActionsDatabaseBoundary(
				candidate.migrationURL,
				candidate.runtimeURL,
				candidate.maintenanceURL,
			) {
				t.Fatal("unexpectedly accepted an inexact GitHub Actions database boundary")
			}
		})
	}
}

func TestP406IsolatedGitHubActionsSignalFixtureDatabaseBoundaryIsExact(t *testing.T) {
	t.Setenv("CI", "true")
	t.Setenv("GITHUB_ACTIONS", "true")
	migrationURL := "postgresql://tutorhub:ignored@localhost:5432/tutorhub_test?sslmode=disable"
	runtimeURL := "postgresql://tutorhub_media_fixture_ci:ignored@localhost:5432/tutorhub_test?sslmode=disable"
	maintenanceURL := "postgresql://tutorhub_media_maintenance_ci:ignored@localhost:5432/tutorhub_test?sslmode=disable"
	if !p406IsolatedGitHubActionsSignalFixtureDatabaseBoundary(
		migrationURL,
		runtimeURL,
		maintenanceURL,
	) {
		t.Fatal("expected exact GitHub Actions stateful signal fixture boundary to pass")
	}
	if p406IsolatedGitHubActionsDatabaseBoundary(migrationURL, runtimeURL, maintenanceURL) {
		t.Fatal("stateful signal fixture role must not satisfy the exact ACL provision boundary")
	}
	conversationRuntimeURL := strings.Replace(
		runtimeURL,
		"tutorhub_media_fixture_ci",
		"tutorhub_conversation_runtime_ci",
		1,
	)
	if p406IsolatedGitHubActionsSignalFixtureDatabaseBoundary(
		migrationURL,
		conversationRuntimeURL,
		maintenanceURL,
	) {
		t.Fatal("exact ACL runtime role must not satisfy the stateful signal fixture boundary")
	}
}
