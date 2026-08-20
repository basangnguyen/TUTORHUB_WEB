//go:build integration

package collaboration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

const (
	p5Collab02ACLConfirmation = "I_UNDERSTAND_P5_COLLAB_02_ACL_PROVISION_DISPOSABLE_ONLY"
)

var whiteboardACLColumns = map[string][]string{
	"whiteboard_documents": {
		"status", "version", "current_generation", "revoke_generation",
		"updated_by", "opened_at", "opened_by", "suspended_at", "suspended_by",
		"resumed_at", "resumed_by", "closed_at", "closed_by", "updated_at",
	},
	"whiteboard_capability_policies": {
		"capability", "version", "updated_by", "updated_at",
	},
}

var whiteboardACLInsertColumns = map[string][]string{
	"whiteboard_documents": {
		"id", "tenant_id", "media_space_id", "create_idempotency_key",
		"create_request_fingerprint", "created_by", "updated_by",
	},
	"whiteboard_document_generations": {
		"tenant_id", "document_id", "generation", "provider_document_name",
		"reason", "restored_from_snapshot_id", "created_by",
	},
	"whiteboard_capability_policies": {
		"tenant_id", "document_id", "audience", "capability", "created_by", "updated_by",
	},
	"whiteboard_snapshots": {
		"id", "tenant_id", "document_id", "generation", "snapshot_kind", "format_version",
		"schema_version", "causal_watermark_sha256", "content_sha256", "size_bytes",
		"object_key", "object_version_id", "verification_key_id", "provenance_kind", "created_by",
	},
	"whiteboard_document_mutation_receipts": {
		"tenant_id", "actor_user_id", "idempotency_key", "request_fingerprint", "operation",
		"document_id", "result_document_version", "result_generation",
		"result_revoke_generation", "result_status",
	},
}

var whiteboardACLTables = []string{
	"whiteboard_documents",
	"whiteboard_document_generations",
	"whiteboard_capability_policies",
	"whiteboard_snapshots",
	"whiteboard_document_mutation_receipts",
}

func TestProvisionWhiteboardControlPlaneExactACL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P5_COLLAB_02_ACL_PROVISION_CONFIRM")) != p5Collab02ACLConfirmation {
		t.Skip("P5_COLLAB_02_ACL_PROVISION_CONFIRM is not set to the disposable-only confirmation")
	}
	requireP5Collab02Disposable(t)

	migrationURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	runtimeURL := strings.TrimSpace(os.Getenv("DATABASE_POOL_URL"))
	maintenanceURL := strings.TrimSpace(os.Getenv("DATABASE_POLL_MAINTENANCE_URL"))
	if migrationURL == "" || runtimeURL == "" || maintenanceURL == "" {
		t.Skip("P5-COLLAB-02 owner/runtime/maintenance URLs are not all configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply P5-COLLAB-02 migration before ACL provisioning")
	}
	migrationPool := openWhiteboardACLPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	runtimePool := openWhiteboardACLPool(t, ctx, runtimeURL)
	defer runtimePool.Close()
	maintenancePool := openWhiteboardACLPool(t, ctx, maintenanceURL)
	defer maintenancePool.Close()

	migrationRole, databaseName := whiteboardDatabaseIdentity(t, ctx, migrationPool)
	runtimeRole, runtimeDatabase := whiteboardDatabaseIdentity(t, ctx, runtimePool)
	maintenanceRole, maintenanceDatabase := whiteboardDatabaseIdentity(t, ctx, maintenancePool)
	if migrationRole == runtimeRole || migrationRole == maintenanceRole || runtimeRole == maintenanceRole ||
		databaseName != runtimeDatabase || databaseName != maintenanceDatabase {
		t.Fatal("P5-COLLAB-02 ACL provisioning requires three distinct roles on one database")
	}

	assertWhiteboardRoleSafety(t, ctx, migrationPool, migrationRole, runtimeRole, maintenanceRole)
	runtimeIdentifier := pgx.Identifier{runtimeRole}.Sanitize()
	maintenanceIdentifier := pgx.Identifier{maintenanceRole}.Sanitize()
	tx, err := migrationPool.Begin(ctx)
	if err != nil {
		t.Fatal("begin P5-COLLAB-02 ACL transaction")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	for _, statement := range []string{
		fmt.Sprintf(`GRANT USAGE ON SCHEMA tutorhub TO %s`, runtimeIdentifier),
		fmt.Sprintf(`REVOKE CREATE ON SCHEMA tutorhub FROM %s`, runtimeIdentifier),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA tutorhub TO %s`, maintenanceIdentifier),
		fmt.Sprintf(`REVOKE CREATE ON SCHEMA tutorhub FROM %s`, maintenanceIdentifier),
	} {
		execWhiteboardACL(t, ctx, tx, statement)
	}

	for _, table := range whiteboardACLTables {
		relation := pgx.Identifier{"tutorhub", table}.Sanitize()
		columns := whiteboardRelationColumns(t, ctx, tx, table)
		quotedColumns := make([]string, 0, len(columns))
		for _, column := range columns {
			quotedColumns = append(quotedColumns, pgx.Identifier{column}.Sanitize())
		}
		allColumns := strings.Join(quotedColumns, ", ")
		for _, role := range []string{runtimeIdentifier, maintenanceIdentifier} {
			execWhiteboardACL(t, ctx, tx,
				fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %s FROM %s`, relation, role))
			execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
				`REVOKE SELECT (%[1]s), INSERT (%[1]s), UPDATE (%[1]s), REFERENCES (%[1]s) ON TABLE %[2]s FROM %[3]s`,
				allColumns, relation, role,
			))
		}
		execWhiteboardACL(t, ctx, tx,
			fmt.Sprintf(`GRANT SELECT ON TABLE %s TO %s`, relation, runtimeIdentifier))
		quotedInserts := make([]string, 0, len(whiteboardACLInsertColumns[table]))
		for _, column := range whiteboardACLInsertColumns[table] {
			quotedInserts = append(quotedInserts, pgx.Identifier{column}.Sanitize())
		}
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
			`GRANT INSERT (%s) ON TABLE %s TO %s`,
			strings.Join(quotedInserts, ", "), relation, runtimeIdentifier,
		))
		if updates := whiteboardACLColumns[table]; len(updates) > 0 {
			quotedUpdates := make([]string, 0, len(updates))
			for _, column := range updates {
				quotedUpdates = append(quotedUpdates, pgx.Identifier{column}.Sanitize())
			}
			execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
				`GRANT UPDATE (%s) ON TABLE %s TO %s`,
				strings.Join(quotedUpdates, ", "), relation, runtimeIdentifier,
			))
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit P5-COLLAB-02 ACL transaction")
	}

	assertWhiteboardExactACL(t, ctx, migrationPool, runtimeRole, maintenanceRole)
}

func openWhiteboardACLPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("open P5-COLLAB-02 ACL pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatal("ping P5-COLLAB-02 ACL pool")
	}
	return pool
}

func whiteboardDatabaseIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) (string, string) {
	t.Helper()
	var role, database string
	if err := pool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(&role, &database); err != nil {
		t.Fatal("inspect P5-COLLAB-02 database identity")
	}
	return role, database
}

func assertWhiteboardRoleSafety(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	migrationRole string,
	runtimeRole string,
	maintenanceRole string,
) {
	t.Helper()
	var runtimeSafe, maintenanceSafe, noUnsafeMembership, noOwnership bool
	if err := pool.QueryRow(ctx, `SELECT
    NOT runtime.rolsuper AND NOT runtime.rolcreaterole AND NOT runtime.rolcreatedb
        AND NOT runtime.rolreplication AND NOT runtime.rolbypassrls,
    NOT maintenance.rolsuper AND NOT maintenance.rolcreaterole AND NOT maintenance.rolcreatedb
        AND NOT maintenance.rolreplication AND NOT maintenance.rolbypassrls,
    NOT pg_has_role(runtime.oid, migration.oid, 'MEMBER')
        AND NOT pg_has_role(maintenance.oid, migration.oid, 'MEMBER')
        AND NOT pg_has_role(runtime.oid, maintenance.oid, 'MEMBER')
        AND NOT pg_has_role(maintenance.oid, runtime.oid, 'MEMBER'),
    NOT EXISTS (
        SELECT 1 FROM pg_class AS relation
        JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = 'tutorhub'
          AND relation.relname = ANY($4::text[])
          AND relation.relowner IN (runtime.oid, maintenance.oid)
    )
FROM pg_roles AS migration
JOIN pg_roles AS runtime ON runtime.rolname = $2
JOIN pg_roles AS maintenance ON maintenance.rolname = $3
WHERE migration.rolname = $1`, migrationRole, runtimeRole, maintenanceRole, whiteboardACLTables,
	).Scan(&runtimeSafe, &maintenanceSafe, &noUnsafeMembership, &noOwnership); err != nil {
		t.Fatal("inspect P5-COLLAB-02 role safety")
	}
	if !runtimeSafe || !maintenanceSafe || !noUnsafeMembership || !noOwnership {
		t.Fatal("P5-COLLAB-02 runtime or maintenance role safety preflight failed")
	}
}

func whiteboardRelationColumns(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	table string,
) []string {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT column_name
        FROM information_schema.columns
        WHERE table_schema = 'tutorhub' AND table_name = $1
        ORDER BY ordinal_position`, table)
	if err != nil {
		t.Fatal("list P5-COLLAB-02 relation columns")
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal("scan P5-COLLAB-02 relation column")
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil || len(columns) == 0 {
		t.Fatal("read P5-COLLAB-02 relation columns")
	}
	return columns
}

func execWhiteboardACL(t *testing.T, ctx context.Context, tx pgx.Tx, statement string) {
	t.Helper()
	if _, err := tx.Exec(ctx, statement); err != nil {
		t.Fatal("apply P5-COLLAB-02 exact ACL statement")
	}
}

func assertWhiteboardExactACL(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	runtimeRole string,
	maintenanceRole string,
) {
	t.Helper()
	for _, table := range whiteboardACLTables {
		relation := "tutorhub." + table
		var runtimeSelect, runtimeInsert, runtimeUpdate, runtimeDelete bool
		var maintenanceAny, publicDenied bool
		if err := pool.QueryRow(ctx, `SELECT
            has_table_privilege($1, $3, 'SELECT'),
            has_table_privilege($1, $3, 'INSERT'),
            has_table_privilege($1, $3, 'UPDATE'),
            has_table_privilege($1, $3, 'DELETE'),
            has_table_privilege($2, $3, 'SELECT')
                OR has_table_privilege($2, $3, 'INSERT')
                OR has_table_privilege($2, $3, 'UPDATE')
                OR has_table_privilege($2, $3, 'DELETE')
                OR has_table_privilege($2, $3, 'TRUNCATE')
                OR has_table_privilege($2, $3, 'REFERENCES')
                OR has_table_privilege($2, $3, 'TRIGGER'),
			NOT EXISTS (
				SELECT 1
				FROM pg_class AS relation
				CROSS JOIN LATERAL aclexplode(COALESCE(
					relation.relacl,
					acldefault('r'::"char", relation.relowner)
				)) AS privilege
				WHERE relation.oid = $3::regclass
				  AND privilege.grantee = 0
			)`,
			runtimeRole, maintenanceRole, relation,
		).Scan(&runtimeSelect, &runtimeInsert, &runtimeUpdate, &runtimeDelete, &maintenanceAny, &publicDenied); err != nil {
			t.Fatal("probe P5-COLLAB-02 table ACL")
		}
		if !runtimeSelect || runtimeInsert || runtimeUpdate || runtimeDelete || maintenanceAny || !publicDenied {
			t.Fatalf("P5-COLLAB-02 table ACL mismatch for %s", table)
		}

		allowedInsert := make(map[string]bool)
		for _, column := range whiteboardACLInsertColumns[table] {
			allowedInsert[column] = true
		}
		allowedUpdate := make(map[string]bool)
		for _, column := range whiteboardACLColumns[table] {
			allowedUpdate[column] = true
		}
		rows, err := pool.Query(ctx, `SELECT column_name,
			has_column_privilege($1::text, format('tutorhub.%I', $2::text), column_name::text, 'INSERT'),
			has_column_privilege($1::text, format('tutorhub.%I', $2::text), column_name::text, 'UPDATE')
            FROM information_schema.columns
            WHERE table_schema = 'tutorhub' AND table_name = $2
            ORDER BY ordinal_position`, runtimeRole, table)
		if err != nil {
			t.Fatal("probe P5-COLLAB-02 column ACL")
		}
		for rows.Next() {
			var column string
			var canInsert, canUpdate bool
			if err := rows.Scan(&column, &canInsert, &canUpdate); err != nil {
				rows.Close()
				t.Fatal("scan P5-COLLAB-02 column ACL")
			}
			if canInsert != allowedInsert[column] || canUpdate != allowedUpdate[column] {
				rows.Close()
				t.Fatalf("P5-COLLAB-02 column ACL mismatch for %s.%s", table, column)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal("read P5-COLLAB-02 column ACL")
		}
		rows.Close()
	}

	var runtimeUsage, runtimeCreate, maintenanceUsage, maintenanceCreate bool
	if err := pool.QueryRow(ctx, `SELECT
        has_schema_privilege($1, 'tutorhub', 'USAGE'),
        has_schema_privilege($1, 'tutorhub', 'CREATE'),
        has_schema_privilege($2, 'tutorhub', 'USAGE'),
        has_schema_privilege($2, 'tutorhub', 'CREATE')`, runtimeRole, maintenanceRole,
	).Scan(&runtimeUsage, &runtimeCreate, &maintenanceUsage, &maintenanceCreate); err != nil {
		t.Fatal("probe P5-COLLAB-02 schema ACL")
	}
	if !runtimeUsage || runtimeCreate || !maintenanceUsage || maintenanceCreate {
		t.Fatal("P5-COLLAB-02 schema ACL mismatch")
	}
}
