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
)

const (
	p5Collab07ACLConfirmation = "I_UNDERSTAND_P5_COLLAB_07_ACL_PROVISION_DISPOSABLE_ONLY"
)

var artifactCoreInsertColumns = []string{
	"id", "tenant_id", "actor_user_id", "document_id", "generation",
	"command_kind", "idempotency_key", "request_fingerprint",
	"source_snapshot_id", "target_generation", "target_provider_document_name",
	"available_at", "requested_at", "updated_at",
}

var artifactWorkerUpdateColumns = []string{
	"status", "lease_owner", "lease_token", "lease_until", "attempts",
	"available_at", "result_snapshot_id", "failure_code", "started_at",
	"completed_at", "updated_at",
}

var artifactWorkerSnapshotInsertColumns = []string{
	"id", "tenant_id", "document_id", "generation", "snapshot_kind",
	"format_version", "engine_version", "authority_version", "schema_version",
	"causal_watermark_sha256", "content_sha256", "size_bytes", "object_key",
	"object_version_id", "verification_key_id", "provenance_kind", "created_by",
}

var artifactCoreCheckpointSelectColumns = []string{
	"tenant_id", "document_id", "generation", "provider_document_name",
}

func TestProvisionWhiteboardArtifactWorkerExactACL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P5_COLLAB_07_ACL_PROVISION_CONFIRM")) != p5Collab07ACLConfirmation {
		t.Skip("P5_COLLAB_07_ACL_PROVISION_CONFIRM is not set to the disposable-only confirmation")
	}
	if strings.TrimSpace(os.Getenv("P5_COLLAB_02_DISPOSABLE_CONFIRM")) != p5Collab02DisposableConfirmation {
		t.Skip("P5_COLLAB_02_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	runWhiteboardControlPlaneExactACLProvision(t, true)

	migrationURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	coreRuntimeURL := strings.TrimSpace(os.Getenv("DATABASE_POOL_URL"))
	workerURL := strings.TrimSpace(os.Getenv("DATABASE_COLLABORATION_URL"))
	maintenanceURL := strings.TrimSpace(os.Getenv("DATABASE_POLL_MAINTENANCE_URL"))
	if migrationURL == "" || coreRuntimeURL == "" || workerURL == "" || maintenanceURL == "" {
		t.Skip("P5-COLLAB-07 owner/core/worker/maintenance URLs are not all configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	migrationPool := openWhiteboardACLPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	coreRuntimePool := openWhiteboardACLPool(t, ctx, coreRuntimeURL)
	defer coreRuntimePool.Close()
	workerPool := openWhiteboardACLPool(t, ctx, workerURL)
	defer workerPool.Close()
	maintenancePool := openWhiteboardACLPool(t, ctx, maintenanceURL)
	defer maintenancePool.Close()

	migrationRole, databaseName := whiteboardDatabaseIdentity(t, ctx, migrationPool)
	coreRuntimeRole, coreDatabase := whiteboardDatabaseIdentity(t, ctx, coreRuntimePool)
	workerRole, workerDatabase := whiteboardDatabaseIdentity(t, ctx, workerPool)
	maintenanceRole, maintenanceDatabase := whiteboardDatabaseIdentity(t, ctx, maintenancePool)
	roles := map[string]bool{
		migrationRole: true, coreRuntimeRole: true, workerRole: true, maintenanceRole: true,
	}
	if len(roles) != 4 || databaseName != coreDatabase || databaseName != workerDatabase || databaseName != maintenanceDatabase {
		t.Fatal("P5-COLLAB-07 ACL provisioning requires four distinct roles on one database")
	}

	coreIdentifier := pgx.Identifier{coreRuntimeRole}.Sanitize()
	workerIdentifier := pgx.Identifier{workerRole}.Sanitize()
	maintenanceIdentifier := pgx.Identifier{maintenanceRole}.Sanitize()
	tx, err := migrationPool.Begin(ctx)
	if err != nil {
		t.Fatal("begin P5-COLLAB-07 ACL transaction")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	for _, role := range []string{coreIdentifier, workerIdentifier, maintenanceIdentifier} {
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`GRANT USAGE ON SCHEMA tutorhub TO %s`, role))
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`REVOKE CREATE ON SCHEMA tutorhub FROM %s`, role))
	}
	for _, table := range []string{"whiteboard_artifact_commands", "whiteboard_artifact_purge_queue"} {
		relation := pgx.Identifier{"tutorhub", table}.Sanitize()
		columns := whiteboardRelationColumns(t, ctx, tx, table)
		quoted := quoteWhiteboardColumns(columns)
		for _, role := range []string{coreIdentifier, workerIdentifier, maintenanceIdentifier} {
			execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %s FROM %s`, relation, role))
			execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
				`REVOKE SELECT (%[1]s), INSERT (%[1]s), UPDATE (%[1]s), REFERENCES (%[1]s) ON TABLE %[2]s FROM %[3]s`,
				quoted, relation, role,
			))
		}
	}

	commandRelation := pgx.Identifier{"tutorhub", "whiteboard_artifact_commands"}.Sanitize()
	execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`GRANT SELECT ON TABLE %s TO %s`, commandRelation, coreIdentifier))
	execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
		`GRANT INSERT (%s) ON TABLE %s TO %s`,
		quoteWhiteboardColumns(artifactCoreInsertColumns), commandRelation, coreIdentifier,
	))
	execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`GRANT SELECT ON TABLE %s TO %s`, commandRelation, workerIdentifier))
	execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
		`GRANT UPDATE (%s) ON TABLE %s TO %s`,
		quoteWhiteboardColumns(artifactWorkerUpdateColumns), commandRelation, workerIdentifier,
	))

	for _, table := range []string{"whiteboard_documents", "whiteboard_document_generations", "whiteboard_snapshots"} {
		relation := pgx.Identifier{"tutorhub", table}.Sanitize()
		columns := whiteboardRelationColumns(t, ctx, tx, table)
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %s FROM %s`, relation, workerIdentifier))
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
			`REVOKE SELECT (%[1]s), INSERT (%[1]s), UPDATE (%[1]s), REFERENCES (%[1]s) ON TABLE %[2]s FROM %[3]s`,
			quoteWhiteboardColumns(columns), relation, workerIdentifier,
		))
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`GRANT SELECT ON TABLE %s TO %s`, relation, workerIdentifier))
	}
	execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
		`GRANT INSERT (%s) ON TABLE tutorhub.whiteboard_snapshots TO %s`,
		quoteWhiteboardColumns(artifactWorkerSnapshotInsertColumns), workerIdentifier,
	))

	checkpoint := pgx.Identifier{"tutorhub", "whiteboard_document_checkpoints"}.Sanitize()
	checkpointColumns := whiteboardRelationColumnsInSchema(t, ctx, tx, "tutorhub", "whiteboard_document_checkpoints")
	for _, role := range []string{coreIdentifier, workerIdentifier, maintenanceIdentifier} {
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`REVOKE ALL PRIVILEGES ON TABLE %s FROM %s`, checkpoint, role))
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
			`REVOKE SELECT (%[1]s), INSERT (%[1]s), UPDATE (%[1]s), REFERENCES (%[1]s) ON TABLE %[2]s FROM %[3]s`,
			quoteWhiteboardColumns(checkpointColumns), checkpoint, role,
		))
	}
	execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE ON TABLE %s TO %s`, checkpoint, workerIdentifier))
	execWhiteboardACL(t, ctx, tx, fmt.Sprintf(
		`GRANT SELECT (%s) ON TABLE %s TO %s`,
		quoteWhiteboardColumns(artifactCoreCheckpointSelectColumns), checkpoint, coreIdentifier,
	))

	functions := []string{
		"tutorhub.enqueue_whiteboard_snapshot_purge(integer)",
		"tutorhub.claim_whiteboard_snapshot_purge(text,integer,integer)",
		"tutorhub.complete_whiteboard_snapshot_purge(uuid,uuid)",
		"tutorhub.fail_whiteboard_snapshot_purge(uuid,uuid,text)",
	}
	for _, function := range functions {
		for _, role := range []string{coreIdentifier, workerIdentifier, maintenanceIdentifier} {
			execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`REVOKE ALL ON FUNCTION %s FROM %s`, function, role))
		}
	}
	for _, function := range functions[1:] {
		execWhiteboardACL(t, ctx, tx, fmt.Sprintf(`GRANT EXECUTE ON FUNCTION %s TO %s`, function, maintenanceIdentifier))
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit P5-COLLAB-07 ACL transaction")
	}

	assertWhiteboardArtifactExactACL(
		t, ctx, migrationPool, migrationRole, coreRuntimeRole, workerRole, maintenanceRole,
	)
}

func quoteWhiteboardColumns(columns []string) string {
	quoted := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted = append(quoted, pgx.Identifier{column}.Sanitize())
	}
	return strings.Join(quoted, ", ")
}

func whiteboardRelationColumnsInSchema(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	schema string,
	table string,
) []string {
	t.Helper()
	rows, err := tx.Query(ctx, `SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
	if err != nil {
		t.Fatal("list P5-COLLAB-07 relation columns")
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal("scan P5-COLLAB-07 relation column")
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil || len(columns) == 0 {
		t.Fatal("read P5-COLLAB-07 relation columns")
	}
	return columns
}

func assertWhiteboardArtifactExactACL(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	migrationRole string,
	coreRuntimeRole string,
	workerRole string,
	maintenanceRole string,
) {
	t.Helper()
	var ledger int
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM public.tutorhub_schema_migrations`).Scan(&ledger, &dirty); err != nil || ledger != 40 || dirty {
		t.Fatal("P5-COLLAB-07 exact ACL requires ledger 40 false")
	}
	var coreSelect, coreInsert, coreUpdate, workerSelect, workerInsert, workerUpdate bool
	var maintenanceAny bool
	if err := pool.QueryRow(ctx, `SELECT
		has_table_privilege($1, 'tutorhub.whiteboard_artifact_commands', 'SELECT'),
		has_table_privilege($1, 'tutorhub.whiteboard_artifact_commands', 'INSERT'),
		has_table_privilege($1, 'tutorhub.whiteboard_artifact_commands', 'UPDATE'),
		has_table_privilege($2, 'tutorhub.whiteboard_artifact_commands', 'SELECT'),
		has_table_privilege($2, 'tutorhub.whiteboard_artifact_commands', 'INSERT'),
		has_table_privilege($2, 'tutorhub.whiteboard_artifact_commands', 'UPDATE'),
		has_table_privilege($3, 'tutorhub.whiteboard_artifact_commands', 'SELECT')
		 OR has_table_privilege($3, 'tutorhub.whiteboard_artifact_purge_queue', 'SELECT')`,
		coreRuntimeRole, workerRole, maintenanceRole,
	).Scan(&coreSelect, &coreInsert, &coreUpdate, &workerSelect, &workerInsert, &workerUpdate, &maintenanceAny); err != nil {
		t.Fatal("probe P5-COLLAB-07 artifact table ACL")
	}
	if !coreSelect || coreInsert || coreUpdate || !workerSelect || workerInsert || workerUpdate || maintenanceAny {
		t.Fatal("P5-COLLAB-07 artifact table-level ACL mismatch")
	}
	var coreCheckpoint, workerCheckpoint, workerCheckpointForbidden, maintenanceCheckpoint bool
	if err := pool.QueryRow(ctx, `SELECT
		has_table_privilege($1, 'tutorhub.whiteboard_document_checkpoints', 'SELECT,INSERT,UPDATE'),
		has_table_privilege($2, 'tutorhub.whiteboard_document_checkpoints', 'SELECT')
		 AND has_table_privilege($2, 'tutorhub.whiteboard_document_checkpoints', 'INSERT')
		 AND has_table_privilege($2, 'tutorhub.whiteboard_document_checkpoints', 'UPDATE'),
		has_table_privilege($2, 'tutorhub.whiteboard_document_checkpoints', 'DELETE,TRUNCATE,REFERENCES,TRIGGER'),
		has_table_privilege($3, 'tutorhub.whiteboard_document_checkpoints', 'SELECT,INSERT,UPDATE,DELETE')`,
		coreRuntimeRole, workerRole, maintenanceRole,
	).Scan(&coreCheckpoint, &workerCheckpoint, &workerCheckpointForbidden, &maintenanceCheckpoint); err != nil {
		t.Fatal("probe P5-COLLAB-07 checkpoint ACL")
	}
	if coreCheckpoint || !workerCheckpoint || workerCheckpointForbidden || maintenanceCheckpoint {
		t.Fatal("P5-COLLAB-07 checkpoint ACL mismatch")
	}
	assertArtifactColumnACL(t, ctx, pool, coreRuntimeRole, "whiteboard_artifact_commands", "INSERT", artifactCoreInsertColumns)
	assertArtifactColumnACL(t, ctx, pool, coreRuntimeRole, "whiteboard_document_checkpoints", "SELECT", artifactCoreCheckpointSelectColumns)
	assertArtifactColumnACL(t, ctx, pool, workerRole, "whiteboard_artifact_commands", "UPDATE", artifactWorkerUpdateColumns)
	assertArtifactColumnACL(t, ctx, pool, workerRole, "whiteboard_snapshots", "INSERT", artifactWorkerSnapshotInsertColumns)

	for index, function := range []string{
		"tutorhub.enqueue_whiteboard_snapshot_purge(integer)",
		"tutorhub.claim_whiteboard_snapshot_purge(text,integer,integer)",
		"tutorhub.complete_whiteboard_snapshot_purge(uuid,uuid)",
		"tutorhub.fail_whiteboard_snapshot_purge(uuid,uuid,text)",
	} {
		var owner string
		var securityDefiner, searchPathPinned bool
		var coreExecute, workerExecute, maintenanceExecute, publicExecute bool
		if err := pool.QueryRow(ctx, `SELECT owner.rolname, procedure.prosecdef,
			COALESCE(procedure.proconfig @> ARRAY['search_path=pg_catalog, tutorhub']::text[], false),
			has_function_privilege($2, $1, 'EXECUTE'),
			has_function_privilege($3, $1, 'EXECUTE'),
			has_function_privilege($4, $1, 'EXECUTE'),
			has_function_privilege('public', $1, 'EXECUTE')
		FROM pg_proc AS procedure
		JOIN pg_roles AS owner ON owner.oid = procedure.proowner
		WHERE procedure.oid = $1::regprocedure`,
			function, coreRuntimeRole, workerRole, maintenanceRole,
		).Scan(&owner, &securityDefiner, &searchPathPinned, &coreExecute, &workerExecute, &maintenanceExecute, &publicExecute); err != nil {
			t.Fatal("probe P5-COLLAB-07 maintenance function ACL")
		}
		wantMaintenance := index > 0
		if owner != migrationRole || !securityDefiner || !searchPathPinned || coreExecute || workerExecute || maintenanceExecute != wantMaintenance || publicExecute {
			t.Fatal("P5-COLLAB-07 maintenance function ACL mismatch")
		}
	}
}

func assertArtifactColumnACL(
	t *testing.T,
	ctx context.Context,
	pool interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	role string,
	table string,
	privilege string,
	allowed []string,
) {
	t.Helper()
	var actual []string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(array_agg(column_name ORDER BY ordinal_position)
		FILTER (WHERE has_column_privilege($1, format('tutorhub.%I', $2::text), column_name, $3)), ARRAY[]::text[])
		FROM information_schema.columns
		WHERE table_schema = 'tutorhub' AND table_name = $2::text`, role, table, privilege,
	).Scan(&actual); err != nil {
		t.Fatalf("probe P5-COLLAB-07 column ACL: %v", err)
	}
	if strings.Join(actual, ",") != strings.Join(allowed, ",") {
		t.Fatalf("P5-COLLAB-07 %s column ACL mismatch for %s", privilege, table)
	}
}
