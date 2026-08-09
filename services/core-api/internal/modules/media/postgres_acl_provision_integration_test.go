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
)

const (
	p402ACLProvisionConfirmation       = "I_UNDERSTAND_P4_02_ACL_PROVISION_DISPOSABLE_ONLY"
	p402SharedACLProvisionConfirmation = "I_UNDERSTAND_P4_02_ACL_PROVISION_SHARED_STAGING_ONLY"
)

// TestProvisionPostgresMediaLifecycleRuntimeExactACL is an explicitly opted-in
// disposable-only acceptance gate. It derives the runtime database identity
// from the authenticated runtime connection and never prints that identity or
// either database URL.
func TestProvisionPostgresMediaLifecycleRuntimeExactACL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_02_ACL_PROVISION_CONFIRM")) != p402ACLProvisionConfirmation {
		t.Skip("P4_02_ACL_PROVISION_CONFIRM is not set to the disposable-only ACL confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_02_DISPOSABLE_CONFIRM")) != p402DisposableConfirmation {
		t.Skip("P4_02_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t)
}

// TestProvisionPostgresMediaLifecycleRuntimeExactACLShared is an explicitly
// opted-in shared-staging gate. It provisions only the reviewed exact runtime
// column allowlist and refuses to run without the separate shared confirmations.
func TestProvisionPostgresMediaLifecycleRuntimeExactACLShared(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_02_SHARED_ACL_PROVISION_CONFIRM")) !=
		p402SharedACLProvisionConfirmation {
		t.Skip("P4_02_SHARED_ACL_PROVISION_CONFIRM is not set to the shared-staging ACL confirmation")
	}
	if strings.TrimSpace(os.Getenv("P4_02_SHARED_CONFIRM")) != p402SharedConfirmation {
		t.Skip("P4_02_SHARED_CONFIRM is not set to the shared-staging confirmation")
	}
	runProvisionPostgresMediaLifecycleRuntimeExactACL(t)
}

func runProvisionPostgresMediaLifecycleRuntimeExactACL(t *testing.T) {
	t.Helper()
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
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
		t.Fatal("inspect P4-02 migration database identity")
	}
	var runtimeRole, runtimeDatabase string
	if err := runtimePool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
		&runtimeRole,
		&runtimeDatabase,
	); err != nil {
		t.Fatal("inspect P4-02 runtime database identity")
	}
	if migrationRole == runtimeRole || migrationDatabase != runtimeDatabase {
		t.Fatal("P4-02 ACL provisioning requires distinct roles on the same database")
	}

	var version int
	var dirty bool
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P4-02 migration ledger")
	}
	if version != 30 || dirty {
		t.Fatal("P4-02 ACL provisioning requires ledger 30 false")
	}

	expectations := p402MediaACLExpectations()
	targets := make([]string, 0, len(expectations))
	for _, expectation := range expectations {
		parts := strings.Split(expectation.relation, ".")
		if len(parts) != 2 || parts[0] != "tutorhub" || strings.TrimSpace(parts[1]) == "" {
			t.Fatal("P4-02 ACL expectation contains an invalid relation")
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
		t.Fatal("inspect P4-02 runtime role safety")
	}
	if !safeRole || !noMigrationMembership || !noTableOwnership {
		t.Fatal("P4-02 runtime role safety preflight failed")
	}

	runtimeIdentifier := pgx.Identifier{runtimeRole}.Sanitize()
	tx, err := migrationPool.Begin(ctx)
	if err != nil {
		t.Fatal("begin P4-02 ACL provisioning transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	execP402ACLStatement(
		t,
		ctx,
		tx,
		fmt.Sprintf(`GRANT USAGE ON SCHEMA tutorhub TO %s`, runtimeIdentifier),
		"grant P4-02 runtime schema usage",
	)
	execP402ACLStatement(
		t,
		ctx,
		tx,
		fmt.Sprintf(`REVOKE CREATE ON SCHEMA tutorhub FROM %s`, runtimeIdentifier),
		"revoke P4-02 runtime schema create",
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
			"revoke P4-02 runtime table privileges",
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
			"revoke P4-02 runtime column privileges",
		)
		execP402ACLStatement(
			t,
			ctx,
			tx,
			fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %s FROM PUBLIC`, relationIdentifier),
			"revoke P4-02 PUBLIC table privileges",
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
			"revoke P4-02 PUBLIC column privileges",
		)

		grantP402ACLColumns(t, ctx, tx, relationIdentifier, runtimeIdentifier, "SELECT", expectation.selectColumns)
		grantP402ACLColumns(t, ctx, tx, relationIdentifier, runtimeIdentifier, "INSERT", expectation.insertColumns)
		grantP402ACLColumns(t, ctx, tx, relationIdentifier, runtimeIdentifier, "UPDATE", expectation.updateColumns)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit P4-02 ACL provisioning transaction")
	}

	for _, expectation := range expectations {
		assertExactMediaACL(t, ctx, runtimePool, expectation)
	}
	assertP402ProvisionedPublicACL(t, ctx, migrationPool, targets)
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
				"provider_kind", "provider_room_name", "provider_room_sid", "space_id", "status",
				"tenant_id", "updated_at", "version",
			},
			insertColumns: []string{
				"created_at", "created_by", "id", "provider_room_name", "space_id", "tenant_id",
				"updated_at", "updated_by", "attempt_number",
			},
			updateColumns: []string{
				"activated_at", "closing_at", "ended_at", "failed_at", "failure_code",
				"provider_room_sid", "status", "updated_at", "updated_by", "version",
			},
		},
		{
			relation:      "tutorhub.media_space_members",
			selectColumns: []string{"space_id", "status", "tenant_id", "user_id"},
		},
		{
			relation: "tutorhub.media_admission_requests",
			selectColumns: []string{
				"id", "idempotency_key", "request_fingerprint", "room_instance_id",
				"space_id", "status", "tenant_id", "user_id", "version",
			},
			insertColumns: []string{
				"created_at", "id", "idempotency_key", "request_fingerprint",
				"room_instance_id", "space_id", "status", "tenant_id", "updated_at",
				"user_id", "version",
			},
		},
		{
			relation: "tutorhub.media_participant_sessions",
			selectColumns: []string{
				"admission_request_id", "admitted_at", "capacity_reserved", "connected_at",
				"created_at", "failure_code", "id", "instance_role", "join_attempt_id",
				"joining_at", "provider_participant_identity", "reconnecting_at", "removed_by",
				"room_instance_id", "space_id", "status", "tenant_id", "terminal_at",
				"updated_at", "user_id", "version",
			},
			insertColumns: []string{
				"admission_request_id", "admitted_at", "capacity_reserved", "created_at",
				"id", "instance_role", "join_attempt_id", "provider_participant_identity",
				"room_instance_id", "space_id", "status", "tenant_id", "updated_at", "user_id",
				"version",
			},
			updateColumns: []string{
				"capacity_reserved", "connected_at", "failure_code", "instance_role", "joining_at",
				"reconnecting_at", "status", "terminal_at", "updated_at", "version",
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
		{relation: "tutorhub.livekit_webhook_events"},
	}
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
		t.Fatal("inspect P4-02 ACL relation columns")
	}
	defer rows.Close()

	columns := make([]string, 0)
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal("read P4-02 ACL relation column")
		}
		columns = append(columns, column)
	}
	if rows.Err() != nil {
		t.Fatal("iterate P4-02 ACL relation columns")
	}
	if len(columns) == 0 {
		t.Fatal("P4-02 ACL target relation has no columns")
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
		"grant exact P4-02 runtime column privileges",
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
		t.Fatal("inspect provisioned P4-02 PUBLIC ACL")
	}
	if tableGrants != 0 || columnGrants != 0 {
		t.Fatal("provisioned P4-02 PUBLIC ACL is not empty")
	}
}
