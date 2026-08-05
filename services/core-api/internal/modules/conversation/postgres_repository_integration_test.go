//go:build integration

package conversation

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresConversationAndMessageRuntimeExactACL(t *testing.T) {
	migrationURL := requireConversationEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireConversationEnvironment(t, "DATABASE_POOL_URL")
	aclPoolURL := strings.TrimSpace(os.Getenv("DATABASE_CONVERSATION_ACL_TEST_URL"))
	if aclPoolURL == "" {
		aclPoolURL = poolURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply conversation ACL migrations: %v", err)
	}
	migrationPool := openConversationPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CONVERSATION_ACL_TEST_BOOTSTRAP")), "true") {
		if !strings.EqualFold(strings.TrimSpace(os.Getenv("CI")), "true") {
			t.Fatal("conversation ACL test bootstrap is restricted to CI")
		}
		requireConversationACLBootstrapDatabase(t, migrationURL)
		provisionConversationACLTestRole(t, ctx, migrationPool)
	}
	runtimePool := openConversationPool(t, ctx, aclPoolURL)
	defer runtimePool.Close()

	var runtimeRole, migrationRole string
	if err := runtimePool.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatalf("read conversation runtime identity: %v", err)
	}
	if err := migrationPool.QueryRow(ctx, "SELECT current_user").Scan(&migrationRole); err != nil {
		t.Fatalf("read conversation migration identity: %v", err)
	}
	if runtimeRole == migrationRole {
		t.Fatal("exact conversation ACL requires distinct runtime and migration roles")
	}

	var schemaUsage, schemaCreate bool
	if err := runtimePool.QueryRow(ctx, `
SELECT
    has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
    has_schema_privilege(current_user, 'tutorhub', 'CREATE')`).Scan(
		&schemaUsage, &schemaCreate,
	); err != nil {
		t.Fatalf("inspect conversation runtime schema ACL: %v", err)
	}
	if !schemaUsage || schemaCreate {
		t.Fatal("conversation runtime schema ACL is not exact")
	}

	for _, expectation := range []struct {
		relation      string
		updateColumns []string
	}{
		{relation: "tutorhub.conversations", updateColumns: []string{"updated_at"}},
		{relation: "tutorhub.conversation_members"},
		{
			relation: "tutorhub.messages",
			updateColumns: []string{
				"content", "state", "version", "edited_at", "deleted_at", "updated_at",
			},
		},
		{
			relation: "tutorhub.message_receipts",
			updateColumns: []string{
				"last_read_message_id", "last_read_sequence", "updated_at",
			},
		},
		{
			relation:      "tutorhub.tenant_message_usage",
			updateColumns: []string{"message_count", "updated_at"},
		},
	} {
		var selected, inserted, updated, deleted, truncated, referenced, triggered bool
		var columnSelected, columnInserted, columnUpdated bool
		if err := runtimePool.QueryRow(ctx, `
SELECT
    has_table_privilege(current_user, $1, 'SELECT'),
    has_table_privilege(current_user, $1, 'INSERT'),
    has_table_privilege(current_user, $1, 'UPDATE'),
    has_table_privilege(current_user, $1, 'DELETE'),
    has_table_privilege(current_user, $1, 'TRUNCATE'),
    has_table_privilege(current_user, $1, 'REFERENCES'),
    has_table_privilege(current_user, $1, 'TRIGGER'),
    has_any_column_privilege(current_user, $1, 'SELECT'),
    has_any_column_privilege(current_user, $1, 'INSERT'),
    has_any_column_privilege(current_user, $1, 'UPDATE')`, expectation.relation).Scan(
			&selected, &inserted, &updated, &deleted, &truncated, &referenced, &triggered,
			&columnSelected, &columnInserted, &columnUpdated,
		); err != nil {
			t.Fatalf("inspect conversation runtime ACL for %s: %v", expectation.relation, err)
		}
		if !selected || !inserted || updated || deleted || truncated || referenced || triggered ||
			!columnSelected || !columnInserted || columnUpdated != (len(expectation.updateColumns) > 0) {
			t.Fatalf("conversation runtime ACL mismatch for %s", expectation.relation)
		}
		assertExactRuntimeUpdateColumns(
			t, ctx, runtimePool, expectation.relation, expectation.updateColumns,
		)
	}

	targetRelations := []string{
		"conversations", "conversation_members", "messages",
		"message_receipts", "tenant_message_usage",
	}
	var publicTableGrants, publicColumnGrants int
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*)
     FROM information_schema.table_privileges
     WHERE table_schema = 'tutorhub'
       AND table_name = ANY($1::text[])
       AND grantee = 'PUBLIC'),
    (SELECT count(*)
     FROM information_schema.column_privileges
     WHERE table_schema = 'tutorhub'
       AND table_name = ANY($1::text[])
       AND grantee = 'PUBLIC')`, targetRelations).Scan(
		&publicTableGrants, &publicColumnGrants,
	); err != nil {
		t.Fatalf("inspect PUBLIC conversation grants: %v", err)
	}
	if publicTableGrants != 0 || publicColumnGrants != 0 {
		t.Fatalf(
			"PUBLIC retained conversation privileges: table=%d column=%d",
			publicTableGrants, publicColumnGrants,
		)
	}

	var notSuperuser, noCreateRole, noCreateDatabase, noReplication, noBypassRLS bool
	var noMigrationInheritance, notTableOwner bool
	if err := migrationPool.QueryRow(ctx, `
SELECT
    NOT runtime.rolsuper,
    NOT runtime.rolcreaterole,
    NOT runtime.rolcreatedb,
    NOT runtime.rolreplication,
    NOT runtime.rolbypassrls,
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
WHERE runtime.rolname = $1`,
		runtimeRole,
		migrationRole,
		targetRelations,
	).Scan(
		&notSuperuser,
		&noCreateRole,
		&noCreateDatabase,
		&noReplication,
		&noBypassRLS,
		&noMigrationInheritance,
		&notTableOwner,
	); err != nil {
		t.Fatalf("inspect conversation runtime role safety: %v", err)
	}
	if !notSuperuser || !noCreateRole || !noCreateDatabase || !noReplication || !noBypassRLS ||
		!noMigrationInheritance || !notTableOwner {
		t.Fatal("conversation runtime role safety boundary is not exact")
	}
}

func requireConversationACLBootstrapDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("parse isolated CI conversation database URL")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	database := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if (host != "localhost" && host != "127.0.0.1") || database != "tutorhub_test" {
		t.Fatal("conversation ACL test bootstrap requires the isolated loopback CI database")
	}
}

func provisionConversationACLTestRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
DO $bootstrap$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_roles WHERE rolname = 'tutorhub_conversation_runtime_ci'
    ) THEN
        CREATE ROLE tutorhub_conversation_runtime_ci
            LOGIN
            PASSWORD 'tutorhub_ci'
            NOSUPERUSER
            NOCREATEDB
            NOCREATEROLE
            NOINHERIT
            NOREPLICATION
            NOBYPASSRLS;
    END IF;
END
$bootstrap$;

ALTER ROLE tutorhub_conversation_runtime_ci
    WITH LOGIN PASSWORD 'tutorhub_ci'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;

GRANT USAGE ON SCHEMA tutorhub TO tutorhub_conversation_runtime_ci;
REVOKE CREATE ON SCHEMA tutorhub FROM tutorhub_conversation_runtime_ci;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.conversations,
    tutorhub.conversation_members,
    tutorhub.messages,
    tutorhub.tenant_message_usage,
    tutorhub.message_receipts
FROM tutorhub_conversation_runtime_ci;

REVOKE ALL PRIVILEGES ON TABLE
    tutorhub.conversations,
    tutorhub.conversation_members,
    tutorhub.messages,
    tutorhub.tenant_message_usage,
    tutorhub.message_receipts
FROM PUBLIC;

GRANT SELECT, INSERT ON TABLE
    tutorhub.conversations,
    tutorhub.conversation_members,
    tutorhub.messages,
    tutorhub.tenant_message_usage,
    tutorhub.message_receipts
TO tutorhub_conversation_runtime_ci;

GRANT UPDATE (updated_at)
ON TABLE tutorhub.conversations
TO tutorhub_conversation_runtime_ci;

GRANT UPDATE (content, state, version, edited_at, deleted_at, updated_at)
ON TABLE tutorhub.messages
TO tutorhub_conversation_runtime_ci;

GRANT UPDATE (message_count, updated_at)
ON TABLE tutorhub.tenant_message_usage
TO tutorhub_conversation_runtime_ci;

GRANT UPDATE (last_read_sequence, last_read_message_id, updated_at)
ON TABLE tutorhub.message_receipts
TO tutorhub_conversation_runtime_ci;
`); err != nil {
		t.Fatalf("provision isolated CI conversation ACL role: %v", err)
	}
}

func assertExactRuntimeUpdateColumns(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	relation string,
	expected []string,
) {
	t.Helper()
	parts := strings.Split(relation, ".")
	if len(parts) != 2 {
		t.Fatalf("invalid relation name %q", relation)
	}
	rows, err := pool.Query(ctx, `
SELECT column_name, has_column_privilege(current_user, $1, column_name, 'UPDATE')
FROM information_schema.columns
WHERE table_schema = $2 AND table_name = $3
ORDER BY ordinal_position`, relation, parts[0], parts[1])
	if err != nil {
		t.Fatalf("inspect update columns for %s: %v", relation, err)
	}
	defer rows.Close()
	expectedSet := make(map[string]struct{}, len(expected))
	for _, column := range expected {
		expectedSet[column] = struct{}{}
	}
	seen := make(map[string]struct{}, len(expected))
	columnCount := 0
	for rows.Next() {
		columnCount++
		var column string
		var allowed bool
		if err := rows.Scan(&column, &allowed); err != nil {
			t.Fatalf("scan update column for %s: %v", relation, err)
		}
		_, shouldAllow := expectedSet[column]
		if allowed != shouldAllow {
			t.Fatalf(
				"runtime UPDATE privilege for %s.%s=%t, want %t",
				relation, column, allowed, shouldAllow,
			)
		}
		if allowed {
			seen[column] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate update columns for %s: %v", relation, err)
	}
	if columnCount == 0 || len(seen) != len(expectedSet) {
		t.Fatalf("runtime UPDATE column set for %s=%v, want %v", relation, seen, expectedSet)
	}
}

func TestPostgresPersistentMessageUsageConstraintsAndBoundedLookup(t *testing.T) {
	migrationURL := requireConversationEnvironment(t, "DATABASE_MIGRATION_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply persistent message usage migrations: %v", err)
	}
	migrationPool := openConversationPool(t, ctx, migrationURL)
	defer migrationPool.Close()

	transaction, err := migrationPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin persistent message usage proof: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()

	lookupTenantID := uuid.New()
	cascadeTenantID := uuid.New()
	lookupSlug := integrationSlug("p307a-usage-lookup")
	cascadeSlug := integrationSlug("p307a-usage-cascade")
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, 'P3-07A usage lookup'),
       ($3, $4, 'P3-07A usage cascade')`,
		lookupTenantID, lookupSlug, cascadeTenantID, cascadeSlug,
	); err != nil {
		t.Fatalf("insert persistent message usage proof tenants: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.tenant_message_usage (tenant_id, message_count)
VALUES ($1, 7), ($2, 11)`, lookupTenantID, cascadeTenantID); err != nil {
		t.Fatalf("insert persistent message usage proof rows: %v", err)
	}

	const planFixtureSize = 4096
	planSlugPrefix := integrationSlug("p307a-usage-plan")
	if _, err := transaction.Exec(ctx, `
WITH tenants AS (
    INSERT INTO tutorhub.tenants (id, slug, name)
    SELECT
        gen_random_uuid(),
        $1 || '-' || lpad(series::text, 4, '0'),
        'P3-07A usage plan fixture'
    FROM generate_series(1, $2) AS series
    RETURNING id
)
INSERT INTO tutorhub.tenant_message_usage (tenant_id, message_count)
SELECT id, 1
FROM tenants`, planSlugPrefix, planFixtureSize); err != nil {
		t.Fatalf("insert bounded usage lookup fixture: %v", err)
	}
	if _, err := transaction.Exec(ctx, "ANALYZE tutorhub.tenant_message_usage"); err != nil {
		t.Fatalf("analyze bounded usage lookup fixture: %v", err)
	}

	var lookupRows int
	if err := transaction.QueryRow(ctx, `
SELECT count(*)
FROM tutorhub.tenant_message_usage
WHERE tenant_id = $1`, lookupTenantID).Scan(&lookupRows); err != nil {
		t.Fatalf("count usage rows for one tenant: %v", err)
	}
	if lookupRows != 1 {
		t.Fatalf("usage rows for one tenant=%d, want 1", lookupRows)
	}

	duplicateTransaction, err := transaction.Begin(ctx)
	if err != nil {
		t.Fatalf("begin duplicate usage proof: %v", err)
	}
	_, duplicateErr := duplicateTransaction.Exec(ctx, `
INSERT INTO tutorhub.tenant_message_usage (tenant_id, message_count)
VALUES ($1, 1)`, lookupTenantID)
	var duplicatePostgresError *pgconn.PgError
	if !errors.As(duplicateErr, &duplicatePostgresError) ||
		duplicatePostgresError.Code != "23505" ||
		duplicatePostgresError.ConstraintName != "tenant_message_usage_pkey" {
		_ = duplicateTransaction.Rollback(ctx)
		t.Fatalf("duplicate usage error=%v, want tenant primary-key violation", duplicateErr)
	}
	if err := duplicateTransaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback duplicate usage proof: %v", err)
	}

	negativeTransaction, err := transaction.Begin(ctx)
	if err != nil {
		t.Fatalf("begin non-negative usage proof: %v", err)
	}
	_, negativeErr := negativeTransaction.Exec(ctx, `
UPDATE tutorhub.tenant_message_usage
SET message_count = -1
WHERE tenant_id = $1`, lookupTenantID)
	var negativePostgresError *pgconn.PgError
	if !errors.As(negativeErr, &negativePostgresError) ||
		negativePostgresError.Code != "23514" ||
		negativePostgresError.ConstraintName != "tenant_message_usage_count_valid" {
		_ = negativeTransaction.Rollback(ctx)
		t.Fatalf("negative usage error=%v, want counter check violation", negativeErr)
	}
	if negativePostgresError.Detail == "" {
		_ = negativeTransaction.Rollback(ctx)
		t.Fatal("negative usage violation omitted the failing-row detail needed for privacy proof")
	}
	safeError := safeMessageStoreError("update tenant message usage", negativeErr)
	if strings.Contains(safeError.Error(), negativePostgresError.Detail) ||
		strings.Contains(safeError.Error(), lookupTenantID.String()) ||
		!strings.Contains(safeError.Error(), "23514") {
		_ = negativeTransaction.Rollback(ctx)
		t.Fatalf("sanitized database error exposed failing-row detail: %v", safeError)
	}
	if err := negativeTransaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback non-negative usage proof: %v", err)
	}

	if _, err := transaction.Exec(
		ctx, "DELETE FROM tutorhub.tenants WHERE id = $1", cascadeTenantID,
	); err != nil {
		t.Fatalf("delete tenant for usage cascade proof: %v", err)
	}
	var cascadeRows int
	if err := transaction.QueryRow(ctx, `
SELECT count(*)
FROM tutorhub.tenant_message_usage
WHERE tenant_id = $1`, cascadeTenantID).Scan(&cascadeRows); err != nil {
		t.Fatalf("count usage rows after tenant cascade: %v", err)
	}
	if cascadeRows != 0 {
		t.Fatalf("usage rows after tenant cascade=%d, want 0", cascadeRows)
	}

	planRows, err := transaction.Query(ctx, `
EXPLAIN (COSTS OFF)
SELECT message_count
FROM tutorhub.tenant_message_usage
WHERE tenant_id = $1`, lookupTenantID)
	if err != nil {
		t.Fatalf("explain bounded usage lookup: %v", err)
	}
	var planLines []string
	for planRows.Next() {
		var line string
		if err := planRows.Scan(&line); err != nil {
			planRows.Close()
			t.Fatalf("scan bounded usage lookup plan: %v", err)
		}
		planLines = append(planLines, line)
	}
	if err := planRows.Err(); err != nil {
		planRows.Close()
		t.Fatalf("iterate bounded usage lookup plan: %v", err)
	}
	planRows.Close()
	plan := strings.Join(planLines, "\n")
	if strings.Contains(plan, "Seq Scan") ||
		!strings.Contains(plan, "tenant_message_usage_pkey") ||
		!strings.Contains(plan, "Index Cond") {
		t.Fatalf("tenant usage lookup is not bounded by its primary key:\n%s", plan)
	}
	if err := transaction.Rollback(ctx); err != nil {
		t.Fatalf("rollback persistent message usage proof: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, "ANALYZE tutorhub.tenant_message_usage"); err != nil {
		t.Fatalf("restore tenant message usage statistics: %v", err)
	}
}

func TestPostgresConversationCoreConcurrencyIsolationAndLifecycle(t *testing.T) {
	migrationURL := requireConversationEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireConversationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply conversation migrations: %v", err)
	}
	migrationPool := openConversationPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	apiPool := openConversationPool(t, ctx, poolURL)
	defer apiPool.Close()
	fixture := seedConversationFixture(t, ctx, migrationPool)
	// This concurrency suite is intended for an ephemeral CI/disposable database.
	// Successful creates append immutable audit rows whose tenant/user foreign keys
	// deliberately prevent fixture deletion; do not weaken that boundary for cleanup.

	authorizer := policy.NewEngine()
	catalog := featurecontrol.NewDefaultCatalog()
	controls, err := featurecontrol.NewPostgresRepository(
		apiPool, 20*time.Second, authorizer, catalog,
	)
	if err != nil {
		t.Fatalf("create conversation feature enforcer: %v", err)
	}
	repository, err := NewPostgresRepository(
		apiPool, 20*time.Second, authorizer, controls,
	)
	if err != nil {
		t.Fatalf("create conversation repository: %v", err)
	}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create conversation service: %v", err)
	}
	ownerAccess := integrationAccess(
		fixture.tenantID, fixture.ownerID, policy.OrganizationRoleTeacher,
	)
	studentAccess := integrationAccess(
		fixture.tenantID, fixture.studentID, policy.OrganizationRoleStudent,
	)

	directResults := runConcurrentCreates(t,
		func() (CreateResult, error) {
			return service.CreateDirect(ctx, ownerAccess, fixture.studentEmail)
		},
		func() (CreateResult, error) {
			return service.CreateDirect(ctx, studentAccess, fixture.ownerEmail)
		},
	)
	assertCanonicalResults(t, directResults)
	directID := directResults[0].result.Conversation.ID
	var directRows, directMembers, directAudits int
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.conversations
     WHERE tenant_id = $1 AND kind = 'direct'),
    (SELECT count(*) FROM tutorhub.conversation_members
     WHERE tenant_id = $1 AND conversation_id = $2),
    (SELECT count(*) FROM tutorhub.audit_events
     WHERE tenant_id = $1 AND action = 'conversation.create'
       AND resource_type = 'conversation' AND resource_id = $2)`,
		fixture.tenantID,
		directID,
	).Scan(&directRows, &directMembers, &directAudits); err != nil {
		t.Fatalf("inspect canonical direct conversation: %v", err)
	}
	if directRows != 1 || directMembers != 2 || directAudits != 1 {
		t.Fatalf(
			"direct invariant rows=%d members=%d audits=%d",
			directRows, directMembers, directAudits,
		)
	}

	classResults := runConcurrentCreates(t,
		func() (CreateResult, error) {
			return service.CreateClass(ctx, ownerAccess, fixture.classID)
		},
		func() (CreateResult, error) {
			return service.CreateClass(ctx, studentAccess, fixture.classID)
		},
	)
	assertCanonicalResults(t, classResults)
	classConversationID := classResults[0].result.Conversation.ID
	var classRows, classMembers int
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.conversations
     WHERE tenant_id = $1 AND kind = 'class' AND class_id = $2),
    (SELECT count(*) FROM tutorhub.conversation_members
     WHERE tenant_id = $1 AND conversation_id = $3)`,
		fixture.tenantID,
		fixture.classID,
		classConversationID,
	).Scan(&classRows, &classMembers); err != nil {
		t.Fatalf("inspect canonical class conversation: %v", err)
	}
	if classRows != 1 || classMembers != 0 {
		t.Fatalf("class invariant rows=%d snapshotted_members=%d", classRows, classMembers)
	}

	foreignAccess := integrationAccess(
		fixture.foreignTenantID,
		fixture.foreignOwnerID,
		policy.OrganizationRoleTeacher,
	)
	if _, err := service.Get(ctx, foreignAccess, directID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign direct get error=%v, want concealed not found", err)
	}
	if _, err := service.Get(ctx, foreignAccess, classConversationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign class get error=%v, want concealed not found", err)
	}

	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.class_enrollments
SET status = 'suspended',
    suspended_at = now(),
    left_at = NULL,
    removed_at = NULL,
    updated_at = now()
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		fixture.tenantID,
		fixture.classID,
		fixture.studentID,
	); err != nil {
		t.Fatalf("suspend authoritative class enrollment: %v", err)
	}
	if _, err := service.Get(
		ctx, studentAccess, classConversationID,
	); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("suspended class reader error=%v, want access denied", err)
	}
	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.class_enrollments
SET status = 'active',
    suspended_at = NULL,
    left_at = NULL,
    removed_at = NULL,
    updated_at = now()
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		fixture.tenantID,
		fixture.classID,
		fixture.studentID,
	); err != nil {
		t.Fatalf("restore authoritative class enrollment: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.classes
SET archived_from_status = status,
    status = 'archived',
    archived_at = now(),
    version = version + 1,
    updated_at = now()
WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		fixture.classID,
	); err != nil {
		t.Fatalf("archive conversation class: %v", err)
	}
	archived, err := service.Get(ctx, studentAccess, classConversationID)
	if err != nil {
		t.Fatalf("read archived class conversation history: %v", err)
	}
	if archived.ClassStatus == nil || *archived.ClassStatus != "archived" ||
		archived.ViewerAccess.CanPostMessages {
		t.Fatalf("unexpected archived projection: %+v", archived)
	}
	if _, err := service.CreateClass(
		ctx, studentAccess, fixture.classID,
	); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("archived class create error=%v, want read only", err)
	}

	if _, err := migrationPool.Exec(ctx, `
INSERT INTO tutorhub.tenant_feature_overrides (
    tenant_id, feature_key, enabled, updated_by, created_at, updated_at
)
VALUES ($1, 'conversations', false, $2, now(), now())`,
		fixture.tenantID,
		fixture.ownerID,
	); err != nil {
		t.Fatalf("disable conversations for integration tenant: %v", err)
	}
	if _, err := service.Get(ctx, ownerAccess, directID); err != nil {
		t.Fatalf("feature-off must preserve direct history read: %v", err)
	}
	page, err := service.List(ctx, ownerAccess, ListInput{})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("feature-off list=%+v error=%v, want retained history", page, err)
	}
	if _, err := service.CreateDirect(
		ctx, ownerAccess, fixture.thirdEmail,
	); !errors.Is(err, featurecontrol.ErrFeatureDisabled) {
		t.Fatalf("feature-off create error=%v, want feature disabled", err)
	}
	if _, err := service.CreateDirect(
		ctx, ownerAccess, fmt.Sprintf("absent-%s@example.test", uuid.NewString()),
	); !errors.Is(err, featurecontrol.ErrFeatureDisabled) {
		t.Fatalf("feature-off absent-email create error=%v, want feature disabled", err)
	}
	if _, err := service.CreateClass(
		ctx, ownerAccess, uuid.New(),
	); !errors.Is(err, featurecontrol.ErrFeatureDisabled) {
		t.Fatalf("feature-off absent-class create error=%v, want feature disabled", err)
	}
}

func TestPostgresPersistentMessagesIdempotencyLifecycleUnreadAuthorizationAndQuota(t *testing.T) {
	migrationURL := requireConversationEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireConversationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply persistent message migrations: %v", err)
	}
	migrationPool := openConversationPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	apiPool := openConversationPool(t, ctx, poolURL)
	defer apiPool.Close()
	fixture := seedConversationFixture(t, ctx, migrationPool)
	service := newConversationIntegrationService(t, apiPool)
	ownerAccess := integrationAccess(
		fixture.tenantID, fixture.ownerID, policy.OrganizationRoleTeacher,
	)
	studentAccess := integrationAccess(
		fixture.tenantID, fixture.studentID, policy.OrganizationRoleStudent,
	)
	foreignAccess := integrationAccess(
		fixture.foreignTenantID,
		fixture.foreignOwnerID,
		policy.OrganizationRoleTeacher,
	)

	direct, err := service.CreateDirect(ctx, ownerAccess, fixture.studentEmail)
	if err != nil {
		t.Fatalf("create direct conversation for messages: %v", err)
	}
	classConversation, err := service.CreateClass(ctx, ownerAccess, fixture.classID)
	if err != nil {
		t.Fatalf("create class conversation for messages: %v", err)
	}
	var baselineAuditRows, baselineOutboxRows int
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.audit_events WHERE tenant_id = $1),
    (SELECT count(*) FROM tutorhub.outbox_events WHERE tenant_id = $1)`,
		fixture.tenantID,
	).Scan(&baselineAuditRows, &baselineOutboxRows); err != nil {
		t.Fatalf("capture pre-message side-effect baseline: %v", err)
	}

	clientMessageID := uuid.New()
	privateContent := "p3-07a-private-" + uuid.NewString()
	concurrent := runConcurrentMessageSends(t,
		func() (MessageMutationResult, error) {
			return service.SendMessage(ctx, ownerAccess, direct.Conversation.ID, SendMessageInput{
				ClientMessageID: clientMessageID,
				Content:         privateContent,
			})
		},
		func() (MessageMutationResult, error) {
			return service.SendMessage(ctx, ownerAccess, direct.Conversation.ID, SendMessageInput{
				ClientMessageID: clientMessageID,
				Content:         privateContent,
			})
		},
	)
	created := 0
	for index, result := range concurrent {
		if result.err != nil {
			t.Fatalf("concurrent message send %d: %v", index, result.err)
		}
		if result.result.Created {
			created++
		}
	}
	if created != 1 || concurrent[0].result.Message.ID != concurrent[1].result.Message.ID ||
		concurrent[0].result.Message.Sequence != 1 ||
		concurrent[1].result.Message.Sequence != 1 {
		t.Fatalf("non-canonical idempotent sends: %+v", concurrent)
	}
	ownerMessage := concurrent[0].result.Message

	var directRows int
	var sendQuotaUsed int64
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.messages
     WHERE tenant_id = $1 AND conversation_id = $2),
    COALESCE((SELECT sum(used_count) FROM tutorhub.tenant_quota_windows
              WHERE tenant_id = $1 AND quota_key = 'message_sends_per_hour'), 0)`,
		fixture.tenantID,
		direct.Conversation.ID,
	).Scan(&directRows, &sendQuotaUsed); err != nil {
		t.Fatalf("inspect idempotent send persistence: %v", err)
	}
	if directRows != 1 || sendQuotaUsed != 1 {
		t.Fatalf("idempotent sends rows=%d quota=%d, want 1/1", directRows, sendQuotaUsed)
	}
	if reserved := assertTenantMessageUsageMatchesMessages(
		t, ctx, migrationPool, fixture.tenantID,
	); reserved != 1 {
		t.Fatalf("idempotent sends reserved messages=%d, want 1", reserved)
	}

	if _, err := service.SendMessage(ctx, ownerAccess, direct.Conversation.ID, SendMessageInput{
		ClientMessageID: clientMessageID,
		Content:         "changed retry payload",
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed idempotent payload error=%v, want conflict", err)
	}
	if _, err := service.SendMessage(
		ctx,
		ownerAccess,
		classConversation.Conversation.ID,
		SendMessageInput{ClientMessageID: clientMessageID, Content: privateContent},
	); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("cross-conversation client ID error=%v, want conflict", err)
	}
	if err := migrationPool.QueryRow(ctx, `
SELECT COALESCE(sum(used_count), 0)
FROM tutorhub.tenant_quota_windows
WHERE tenant_id = $1 AND quota_key = 'message_sends_per_hour'`,
		fixture.tenantID,
	).Scan(&sendQuotaUsed); err != nil {
		t.Fatalf("inspect quota after conflicts: %v", err)
	}
	if sendQuotaUsed != 1 {
		t.Fatalf("idempotency conflicts consumed quota: used=%d", sendQuotaUsed)
	}
	if reserved := assertTenantMessageUsageMatchesMessages(
		t, ctx, migrationPool, fixture.tenantID,
	); reserved != 1 {
		t.Fatalf("idempotency conflicts changed message counter: reserved=%d", reserved)
	}

	studentMessage, err := service.SendMessage(
		ctx,
		studentAccess,
		direct.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "student reply"},
	)
	if err != nil {
		t.Fatalf("send student reply: %v", err)
	}
	if !studentMessage.Created || studentMessage.Message.Sequence != 2 {
		t.Fatalf("unexpected student reply: %+v", studentMessage)
	}
	ownerPage, err := service.ListMessages(
		ctx, ownerAccess, direct.Conversation.ID, MessageListInput{},
	)
	if err != nil {
		t.Fatalf("list owner direct messages: %v", err)
	}
	if ownerPage.UnreadCount != 1 || ownerPage.UnreadCountCapped || len(ownerPage.Items) != 2 ||
		ownerPage.Items[0].Sequence != 2 || ownerPage.Items[1].Sequence != 1 {
		t.Fatalf("unexpected owner unread/order projection: %+v", ownerPage)
	}
	studentPage, err := service.ListMessages(
		ctx, studentAccess, direct.Conversation.ID, MessageListInput{},
	)
	if err != nil {
		t.Fatalf("list student direct messages: %v", err)
	}
	if studentPage.UnreadCount != 1 {
		t.Fatalf("student unread=%d, want only the owner's message", studentPage.UnreadCount)
	}
	newestPage, err := service.ListMessages(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		MessageListInput{Limit: 1},
	)
	if err != nil {
		t.Fatalf("list newest message page: %v", err)
	}
	if len(newestPage.Items) != 1 || newestPage.Items[0].Sequence != 2 ||
		newestPage.NextCursor == "" {
		t.Fatalf("unexpected newest message page: %+v", newestPage)
	}
	olderPage, err := service.ListMessages(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		MessageListInput{Limit: 1, Cursor: newestPage.NextCursor},
	)
	if err != nil {
		t.Fatalf("list older message page: %v", err)
	}
	if len(olderPage.Items) != 1 || olderPage.Items[0].Sequence != 1 ||
		olderPage.Items[0].ID == newestPage.Items[0].ID || olderPage.NextCursor != "" {
		t.Fatalf("unstable or duplicate older message page: newest=%+v older=%+v",
			newestPage, olderPage)
	}

	readResults := runConcurrentReadMarkers(t,
		func() (MessageReadState, error) {
			return service.MarkRead(ctx, ownerAccess, direct.Conversation.ID, ownerMessage.ID)
		},
		func() (MessageReadState, error) {
			return service.MarkRead(
				ctx, ownerAccess, direct.Conversation.ID, studentMessage.Message.ID,
			)
		},
	)
	for index, result := range readResults {
		if result.err != nil {
			t.Fatalf("concurrent read marker %d: %v", index, result.err)
		}
	}
	ownerPage, err = service.ListMessages(
		ctx, ownerAccess, direct.Conversation.ID, MessageListInput{},
	)
	if err != nil {
		t.Fatalf("list after concurrent read marker: %v", err)
	}
	if ownerPage.ReadState == nil || ownerPage.ReadState.LastReadSequence != 2 ||
		ownerPage.ReadState.LastReadMessageID != studentMessage.Message.ID ||
		ownerPage.UnreadCount != 0 {
		t.Fatalf("read marker moved backward: %+v", ownerPage)
	}
	olderState, err := service.MarkRead(
		ctx, ownerAccess, direct.Conversation.ID, ownerMessage.ID,
	)
	if err != nil || olderState.LastReadSequence != 2 {
		t.Fatalf("older read marker result=%+v error=%v, want sequence 2", olderState, err)
	}

	if _, err := service.ListMessages(
		ctx, foreignAccess, direct.Conversation.ID, MessageListInput{},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign-tenant message list error=%v, want concealed not found", err)
	}
	if _, err := service.MarkRead(
		ctx, foreignAccess, direct.Conversation.ID, studentMessage.Message.ID,
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign-tenant read marker error=%v, want concealed not found", err)
	}

	if _, err := service.EditMessage(
		ctx,
		studentAccess,
		direct.Conversation.ID,
		ownerMessage.ID,
		EditMessageInput{Content: "not mine", ExpectedVersion: 1},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-author edit error=%v, want concealed not found", err)
	}
	edited, err := service.EditMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		ownerMessage.ID,
		EditMessageInput{Content: "edited by author", ExpectedVersion: 1},
	)
	if err != nil {
		t.Fatalf("author CAS edit: %v", err)
	}
	if edited.Version != 2 || edited.EditedAt == nil || edited.Content == nil ||
		*edited.Content != "edited by author" {
		t.Fatalf("unexpected edited message: %+v", edited)
	}
	if _, err := service.EditMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		ownerMessage.ID,
		EditMessageInput{Content: "stale", ExpectedVersion: 1},
	); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale author edit error=%v, want version conflict", err)
	}
	if _, err := service.DeleteMessage(
		ctx,
		studentAccess,
		direct.Conversation.ID,
		ownerMessage.ID,
		DeleteMessageInput{ExpectedVersion: 2},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-author delete error=%v, want concealed not found", err)
	}
	deleted, err := service.DeleteMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		ownerMessage.ID,
		DeleteMessageInput{ExpectedVersion: 2},
	)
	if err != nil {
		t.Fatalf("author CAS delete: %v", err)
	}
	if deleted.State != MessageStateDeleted || deleted.Content != nil ||
		deleted.Version != 3 || deleted.DeletedAt == nil {
		t.Fatalf("unexpected message tombstone: %+v", deleted)
	}
	repeatedDelete, err := service.DeleteMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		ownerMessage.ID,
		DeleteMessageInput{ExpectedVersion: 1},
	)
	if err != nil || repeatedDelete.State != MessageStateDeleted || repeatedDelete.Version != 3 {
		t.Fatalf("repeated delete result=%+v error=%v", repeatedDelete, err)
	}
	if _, err := service.EditMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		ownerMessage.ID,
		EditMessageInput{Content: "restore", ExpectedVersion: 3},
	); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("edit tombstone error=%v, want version conflict", err)
	}
	studentPage, err = service.ListMessages(
		ctx, studentAccess, direct.Conversation.ID, MessageListInput{},
	)
	if err != nil {
		t.Fatalf("list after tombstone: %v", err)
	}
	if studentPage.UnreadCount != 0 || len(studentPage.Items) != 2 ||
		studentPage.Items[1].State != MessageStateDeleted || studentPage.Items[1].Content != nil {
		t.Fatalf("deleted message leaked into unread/content: %+v", studentPage)
	}

	var quotaBeforeCrossConversation int64
	if err := migrationPool.QueryRow(ctx, `
SELECT COALESCE(sum(used_count), 0)
FROM tutorhub.tenant_quota_windows
WHERE tenant_id = $1 AND quota_key = 'message_sends_per_hour'`,
		fixture.tenantID,
	).Scan(&quotaBeforeCrossConversation); err != nil {
		t.Fatalf("read quota before cross-conversation race: %v", err)
	}
	crossConversationClientID := uuid.New()
	crossConversationResults := runConcurrentMessageSends(t,
		func() (MessageMutationResult, error) {
			return service.SendMessage(ctx, ownerAccess, direct.Conversation.ID, SendMessageInput{
				ClientMessageID: crossConversationClientID,
				Content:         "cross-conversation retry",
			})
		},
		func() (MessageMutationResult, error) {
			return service.SendMessage(
				ctx,
				ownerAccess,
				classConversation.Conversation.ID,
				SendMessageInput{
					ClientMessageID: crossConversationClientID,
					Content:         "cross-conversation retry",
				},
			)
		},
	)
	crossCreated, crossConflicts := 0, 0
	for index, result := range crossConversationResults {
		switch {
		case result.err == nil:
			if !result.result.Created {
				t.Fatalf("cross-conversation send %d returned non-created success: %+v", index, result)
			}
			crossCreated++
		case errors.Is(result.err, ErrIdempotencyConflict):
			crossConflicts++
		default:
			t.Fatalf("cross-conversation send %d returned unsafe error: %v", index, result.err)
		}
	}
	var crossConversationRows int
	var quotaAfterCrossConversation int64
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.messages
     WHERE tenant_id = $1 AND author_user_id = $2 AND client_message_id = $3),
    COALESCE((SELECT sum(used_count) FROM tutorhub.tenant_quota_windows
              WHERE tenant_id = $1 AND quota_key = 'message_sends_per_hour'), 0)`,
		fixture.tenantID,
		fixture.ownerID,
		crossConversationClientID,
	).Scan(&crossConversationRows, &quotaAfterCrossConversation); err != nil {
		t.Fatalf("inspect cross-conversation idempotency race: %v", err)
	}
	if crossCreated != 1 || crossConflicts != 1 || crossConversationRows != 1 ||
		quotaAfterCrossConversation != quotaBeforeCrossConversation+1 {
		t.Fatalf(
			"cross-conversation race created=%d conflicts=%d rows=%d quota=%d/%d",
			crossCreated, crossConflicts, crossConversationRows,
			quotaAfterCrossConversation, quotaBeforeCrossConversation,
		)
	}

	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.memberships
SET status = 'suspended', updated_at = now()
WHERE tenant_id = $1 AND user_id = $2`, fixture.tenantID, fixture.studentID); err != nil {
		t.Fatalf("suspend direct participant membership: %v", err)
	}
	if _, err := service.ListMessages(
		ctx, ownerAccess, direct.Conversation.ID, MessageListInput{},
	); err != nil {
		t.Fatalf("direct history must remain readable when peer is inactive: %v", err)
	}
	if _, err := service.SendMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "blocked inactive peer"},
	); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("inactive peer send error=%v, want read only", err)
	}
	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.memberships
SET status = 'active', updated_at = now()
WHERE tenant_id = $1 AND user_id = $2`, fixture.tenantID, fixture.studentID); err != nil {
		t.Fatalf("restore direct participant membership: %v", err)
	}

	classMessage, err := service.SendMessage(
		ctx,
		ownerAccess,
		classConversation.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "class history"},
	)
	if err != nil {
		t.Fatalf("send active class message: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.class_enrollments
SET status = 'suspended', suspended_at = now(), left_at = NULL,
    removed_at = NULL, updated_at = now()
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		fixture.tenantID, fixture.classID, fixture.studentID,
	); err != nil {
		t.Fatalf("suspend class enrollment for messages: %v", err)
	}
	if _, err := service.ListMessages(
		ctx, studentAccess, classConversation.Conversation.ID, MessageListInput{},
	); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("suspended class history error=%v, want access denied", err)
	}
	if _, err := service.SendMessage(
		ctx,
		studentAccess,
		classConversation.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "blocked suspended class"},
	); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("suspended class send error=%v, want access denied", err)
	}
	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.class_enrollments
SET status = 'active', suspended_at = NULL, left_at = NULL,
    removed_at = NULL, updated_at = now()
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		fixture.tenantID, fixture.classID, fixture.studentID,
	); err != nil {
		t.Fatalf("restore class enrollment for messages: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.classes
SET archived_from_status = status, status = 'archived', archived_at = now(),
    version = version + 1, updated_at = now()
WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, fixture.classID); err != nil {
		t.Fatalf("archive class for messages: %v", err)
	}
	if _, err := service.ListMessages(
		ctx, ownerAccess, classConversation.Conversation.ID, MessageListInput{},
	); err != nil {
		t.Fatalf("archived class history must remain readable: %v", err)
	}
	if _, err := service.MarkRead(
		ctx,
		ownerAccess,
		classConversation.Conversation.ID,
		classMessage.Message.ID,
	); err != nil {
		t.Fatalf("archived class read marker must remain writable: %v", err)
	}
	if _, err := service.SendMessage(
		ctx,
		ownerAccess,
		classConversation.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "blocked archive"},
	); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("archived class send error=%v, want read only", err)
	}
	if _, err := service.EditMessage(
		ctx,
		ownerAccess,
		classConversation.Conversation.ID,
		classMessage.Message.ID,
		EditMessageInput{Content: "blocked archive edit", ExpectedVersion: 1},
	); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("archived class edit error=%v, want read only", err)
	}

	var messageCount int64
	if err := migrationPool.QueryRow(
		ctx, `SELECT count(*) FROM tutorhub.messages WHERE tenant_id = $1`, fixture.tenantID,
	).Scan(&messageCount); err != nil {
		t.Fatalf("count tenant messages for storage quota: %v", err)
	}
	var reservedMessageCount int64
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT message_count FROM tutorhub.tenant_message_usage WHERE tenant_id = $1`,
		fixture.tenantID,
	).Scan(&reservedMessageCount); err != nil {
		t.Fatalf("read O(1) tenant message usage: %v", err)
	}
	if reservedMessageCount != messageCount {
		t.Fatalf(
			"tenant message usage=%d, want committed message count=%d",
			reservedMessageCount,
			messageCount,
		)
	}
	if _, err := migrationPool.Exec(ctx, `
INSERT INTO tutorhub.tenant_quota_overrides (
    tenant_id, quota_key, limit_value, updated_by, created_at, updated_at
)
VALUES ($1, 'messages_per_tenant', $2, $3, now(), now())
ON CONFLICT (tenant_id, quota_key)
DO UPDATE SET limit_value = EXCLUDED.limit_value,
              updated_by = EXCLUDED.updated_by,
              updated_at = EXCLUDED.updated_at`,
		fixture.tenantID, messageCount, fixture.ownerID,
	); err != nil {
		t.Fatalf("set message storage quota: %v", err)
	}
	if _, err := service.SendMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "over storage quota"},
	); !errors.Is(err, featurecontrol.ErrQuotaExceeded) {
		t.Fatalf("storage quota error=%v, want quota exceeded", err)
	}
	if reserved := assertTenantMessageUsageMatchesMessages(
		t, ctx, migrationPool, fixture.tenantID,
	); reserved != messageCount {
		t.Fatalf(
			"storage quota failure changed message counter: reserved=%d want=%d",
			reserved, messageCount,
		)
	}
	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.tenant_quota_overrides
SET limit_value = $2, updated_at = now()
WHERE tenant_id = $1 AND quota_key = 'messages_per_tenant'`,
		fixture.tenantID, messageCount+100,
	); err != nil {
		t.Fatalf("raise message storage quota: %v", err)
	}
	if err := migrationPool.QueryRow(ctx, `
SELECT COALESCE(sum(used_count), 0)
FROM tutorhub.tenant_quota_windows
WHERE tenant_id = $1 AND quota_key = 'message_sends_per_hour'`,
		fixture.tenantID,
	).Scan(&sendQuotaUsed); err != nil {
		t.Fatalf("read current message send quota usage: %v", err)
	}
	if sendQuotaUsed < 1 {
		t.Fatalf("message send quota usage=%d, want positive", sendQuotaUsed)
	}
	if _, err := migrationPool.Exec(ctx, `
INSERT INTO tutorhub.tenant_quota_overrides (
    tenant_id, quota_key, limit_value, updated_by, created_at, updated_at
)
VALUES ($1, 'message_sends_per_hour', $2, $3, now(), now())
ON CONFLICT (tenant_id, quota_key)
DO UPDATE SET limit_value = EXCLUDED.limit_value,
              updated_by = EXCLUDED.updated_by,
              updated_at = EXCLUDED.updated_at`,
		fixture.tenantID, sendQuotaUsed, fixture.ownerID,
	); err != nil {
		t.Fatalf("set hourly message quota: %v", err)
	}
	_, err = service.SendMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "over hourly quota"},
	)
	var quotaFailure *featurecontrol.QuotaExceededError
	if !errors.As(err, &quotaFailure) || quotaFailure.RetryAfter <= 0 ||
		quotaFailure.Quota != featurecontrol.QuotaMessageSendsPerHour {
		t.Fatalf("hourly quota error=%v, want bounded retry metadata", err)
	}
	if reserved := assertTenantMessageUsageMatchesMessages(
		t, ctx, migrationPool, fixture.tenantID,
	); reserved != messageCount {
		t.Fatalf(
			"hourly quota failure changed message counter: reserved=%d want=%d",
			reserved, messageCount,
		)
	}

	replayed, err := service.SendMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		SendMessageInput{ClientMessageID: clientMessageID, Content: privateContent},
	)
	if err != nil || replayed.Created || replayed.Message.ID != ownerMessage.ID {
		t.Fatalf("quota-exhausted replay result=%+v error=%v", replayed, err)
	}
	if reserved := assertTenantMessageUsageMatchesMessages(
		t, ctx, migrationPool, fixture.tenantID,
	); reserved != messageCount {
		t.Fatalf(
			"quota-exhausted replay changed message counter: reserved=%d want=%d",
			reserved, messageCount,
		)
	}

	if _, err := migrationPool.Exec(ctx, `
INSERT INTO tutorhub.tenant_feature_overrides (
    tenant_id, feature_key, enabled, updated_by, created_at, updated_at
)
VALUES ($1, 'conversations', false, $2, now(), now())
ON CONFLICT (tenant_id, feature_key)
DO UPDATE SET enabled = EXCLUDED.enabled,
              updated_by = EXCLUDED.updated_by,
              updated_at = EXCLUDED.updated_at`, fixture.tenantID, fixture.ownerID); err != nil {
		t.Fatalf("disable conversations for message replay: %v", err)
	}
	replayed, err = service.SendMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		SendMessageInput{ClientMessageID: clientMessageID, Content: privateContent},
	)
	if err != nil || replayed.Created || replayed.Message.ID != ownerMessage.ID {
		t.Fatalf("feature-off replay result=%+v error=%v", replayed, err)
	}
	if _, err := service.SendMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "blocked feature"},
	); !errors.Is(err, featurecontrol.ErrFeatureDisabled) {
		t.Fatalf("feature-off new send error=%v, want feature disabled", err)
	}
	if reserved := assertTenantMessageUsageMatchesMessages(
		t, ctx, migrationPool, fixture.tenantID,
	); reserved != messageCount {
		t.Fatalf(
			"feature-off replay/failure changed message counter: reserved=%d want=%d",
			reserved, messageCount,
		)
	}
	if _, err := service.ListMessages(
		ctx, ownerAccess, direct.Conversation.ID, MessageListInput{},
	); err != nil {
		t.Fatalf("feature-off history read: %v", err)
	}
	if _, err := service.MarkRead(
		ctx, ownerAccess, direct.Conversation.ID, studentMessage.Message.ID,
	); err != nil {
		t.Fatalf("feature-off read marker: %v", err)
	}
	var auditRows, outboxRows int
	var auditContainsContent, outboxContainsContent bool
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.audit_events WHERE tenant_id = $1),
    (SELECT count(*) FROM tutorhub.outbox_events WHERE tenant_id = $1),
    EXISTS (
        SELECT 1 FROM tutorhub.audit_events
        WHERE tenant_id = $1 AND metadata::text LIKE '%' || $2 || '%'
    ),
    EXISTS (
        SELECT 1 FROM tutorhub.outbox_events
        WHERE tenant_id = $1 AND payload::text LIKE '%' || $2 || '%'
    )`, fixture.tenantID, privateContent).Scan(
		&auditRows, &outboxRows, &auditContainsContent, &outboxContainsContent,
	); err != nil {
		t.Fatalf("inspect message audit/outbox privacy boundary: %v", err)
	}
	if auditRows != baselineAuditRows || outboxRows != baselineOutboxRows ||
		auditContainsContent || outboxContainsContent {
		t.Fatalf(
			"message side-effect/privacy boundary changed: audit=%d/%d outbox=%d/%d content=%t/%t",
			auditRows, baselineAuditRows, outboxRows, baselineOutboxRows,
			auditContainsContent, outboxContainsContent,
		)
	}
}

func TestPostgresPersistentMessageActorRateLimitIsTransactional(t *testing.T) {
	migrationURL := requireConversationEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireConversationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply persistent message rate migrations: %v", err)
	}
	migrationPool := openConversationPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	apiPool := openConversationPool(t, ctx, poolURL)
	defer apiPool.Close()
	fixture := seedConversationFixture(t, ctx, migrationPool)
	service := newConversationIntegrationService(t, apiPool)
	ownerAccess := integrationAccess(
		fixture.tenantID, fixture.ownerID, policy.OrganizationRoleTeacher,
	)
	direct, err := service.CreateDirect(ctx, ownerAccess, fixture.studentEmail)
	if err != nil {
		t.Fatalf("create actor-rate direct conversation: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, `
INSERT INTO tutorhub.messages (
    id, tenant_id, conversation_id, author_user_id, client_message_id,
    sequence, request_fingerprint, content, state, version, created_at, updated_at
)
SELECT
    gen_random_uuid(), $1, $2, $3, gen_random_uuid(), series.sequence,
    decode(repeat('00', 32), 'hex'), 'rate seed ' || series.sequence,
    'active', 1, statement_timestamp(), statement_timestamp()
FROM generate_series(1, $4) AS series(sequence)`,
		fixture.tenantID,
		direct.Conversation.ID,
		fixture.ownerID,
		actorMessageRateLimit,
	); err != nil {
		t.Fatalf("seed actor-rate messages: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, `
INSERT INTO tutorhub.messages (
    id, tenant_id, conversation_id, author_user_id, client_message_id,
    sequence, request_fingerprint, content, state, version, created_at, updated_at
)
SELECT
    gen_random_uuid(), $1, $2, $3, gen_random_uuid(),
    $4 + series.sequence,
    decode(repeat('00', 32), 'hex'), 'unread seed ' || series.sequence,
    'active', 1, statement_timestamp(), statement_timestamp()
FROM generate_series(1, $5) AS series(sequence)`,
		fixture.tenantID,
		direct.Conversation.ID,
		fixture.studentID,
		actorMessageRateLimit,
		maximumUnreadCount+1,
	); err != nil {
		t.Fatalf("seed capped unread messages: %v", err)
	}
	seededMessageCount := actorMessageRateLimit + maximumUnreadCount + 1
	if _, err := migrationPool.Exec(ctx, `
INSERT INTO tutorhub.tenant_message_usage (tenant_id, message_count, updated_at)
VALUES ($1, $2, now())`, fixture.tenantID, seededMessageCount); err != nil {
		t.Fatalf("seed actor-rate message counter: %v", err)
	}
	unreadPage, err := service.ListMessages(
		ctx, ownerAccess, direct.Conversation.ID, MessageListInput{Limit: 1},
	)
	if err != nil {
		t.Fatalf("list capped unread messages: %v", err)
	}
	if unreadPage.UnreadCount != maximumUnreadCount || !unreadPage.UnreadCountCapped {
		t.Fatalf("unread cap projection=%+v, want %d capped", unreadPage, maximumUnreadCount)
	}

	_, err = service.SendMessage(
		ctx,
		ownerAccess,
		direct.Conversation.ID,
		SendMessageInput{ClientMessageID: uuid.New(), Content: "rate limited"},
	)
	var quotaFailure *featurecontrol.QuotaExceededError
	if !errors.As(err, &quotaFailure) || quotaFailure.RetryAfter <= 0 ||
		quotaFailure.Quota != featurecontrol.QuotaMessageSendsPerHour ||
		quotaFailure.Limit != actorMessageRateLimit {
		t.Fatalf("actor rate error=%v, want bounded 60/minute failure", err)
	}
	var messageCount, quotaWindows int
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.messages
     WHERE tenant_id = $1 AND conversation_id = $2),
    (SELECT count(*) FROM tutorhub.tenant_quota_windows
     WHERE tenant_id = $1 AND quota_key = 'message_sends_per_hour')`,
		fixture.tenantID,
		direct.Conversation.ID,
	).Scan(&messageCount, &quotaWindows); err != nil {
		t.Fatalf("inspect actor rate rollback: %v", err)
	}
	if messageCount != actorMessageRateLimit+maximumUnreadCount+1 || quotaWindows != 0 {
		t.Fatalf(
			"actor rate failure committed side effects: messages=%d quota_windows=%d",
			messageCount, quotaWindows,
		)
	}
	if reserved := assertTenantMessageUsageMatchesMessages(
		t, ctx, migrationPool, fixture.tenantID,
	); reserved != int64(seededMessageCount) {
		t.Fatalf(
			"actor rate failure changed message counter: reserved=%d want=%d",
			reserved, seededMessageCount,
		)
	}
}

func assertTenantMessageUsageMatchesMessages(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
) int64 {
	t.Helper()
	var messageCount, reservedMessageCount int64
	if err := pool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.messages WHERE tenant_id = $1),
    COALESCE((
        SELECT message_count
        FROM tutorhub.tenant_message_usage
        WHERE tenant_id = $1
    ), 0)`, tenantID).Scan(&messageCount, &reservedMessageCount); err != nil {
		t.Fatalf("inspect tenant message counter consistency: %v", err)
	}
	if reservedMessageCount != messageCount {
		t.Fatalf(
			"tenant message counter=%d, committed messages=%d",
			reservedMessageCount, messageCount,
		)
	}
	return reservedMessageCount
}

func newConversationIntegrationService(
	t *testing.T,
	pool *pgxpool.Pool,
) *Service {
	t.Helper()
	authorizer := policy.NewEngine()
	controls, err := featurecontrol.NewPostgresRepository(
		pool, 20*time.Second, authorizer, featurecontrol.NewDefaultCatalog(),
	)
	if err != nil {
		t.Fatalf("create conversation feature enforcer: %v", err)
	}
	repository, err := NewPostgresRepository(
		pool, 20*time.Second, authorizer, controls,
	)
	if err != nil {
		t.Fatalf("create conversation repository: %v", err)
	}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create conversation service: %v", err)
	}
	return service
}

type integrationMessageResult struct {
	result MessageMutationResult
	err    error
}

func runConcurrentMessageSends(
	t *testing.T,
	left func() (MessageMutationResult, error),
	right func() (MessageMutationResult, error),
) []integrationMessageResult {
	t.Helper()
	start := make(chan struct{})
	results := make([]integrationMessageResult, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for index, operation := range []func() (MessageMutationResult, error){left, right} {
		go func(index int, operation func() (MessageMutationResult, error)) {
			defer wait.Done()
			<-start
			results[index].result, results[index].err = operation()
		}(index, operation)
	}
	close(start)
	wait.Wait()
	return results
}

type integrationReadResult struct {
	state MessageReadState
	err   error
}

func runConcurrentReadMarkers(
	t *testing.T,
	left func() (MessageReadState, error),
	right func() (MessageReadState, error),
) []integrationReadResult {
	t.Helper()
	start := make(chan struct{})
	results := make([]integrationReadResult, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for index, operation := range []func() (MessageReadState, error){left, right} {
		go func(index int, operation func() (MessageReadState, error)) {
			defer wait.Done()
			<-start
			results[index].state, results[index].err = operation()
		}(index, operation)
	}
	close(start)
	wait.Wait()
	return results
}

type integrationCreateResult struct {
	result CreateResult
	err    error
}

func runConcurrentCreates(
	t *testing.T,
	left func() (CreateResult, error),
	right func() (CreateResult, error),
) []integrationCreateResult {
	t.Helper()
	start := make(chan struct{})
	results := make([]integrationCreateResult, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for index, operation := range []func() (CreateResult, error){left, right} {
		go func(index int, operation func() (CreateResult, error)) {
			defer wait.Done()
			<-start
			results[index].result, results[index].err = operation()
		}(index, operation)
	}
	close(start)
	wait.Wait()
	return results
}

func assertCanonicalResults(t *testing.T, results []integrationCreateResult) {
	t.Helper()
	if len(results) != 2 {
		t.Fatalf("result count=%d", len(results))
	}
	created := 0
	for index, result := range results {
		if result.err != nil {
			t.Fatalf("concurrent create %d: %v", index, result.err)
		}
		if result.result.Conversation.ID == uuid.Nil {
			t.Fatalf("concurrent create %d returned nil ID", index)
		}
		if result.result.Created {
			created++
		}
	}
	if results[0].result.Conversation.ID != results[1].result.Conversation.ID || created != 1 {
		t.Fatalf("non-canonical concurrent results: %+v", results)
	}
}

type conversationIntegrationFixture struct {
	tenantID        uuid.UUID
	foreignTenantID uuid.UUID
	ownerID         uuid.UUID
	studentID       uuid.UUID
	thirdID         uuid.UUID
	foreignOwnerID  uuid.UUID
	classID         uuid.UUID
	ownerEmail      string
	studentEmail    string
	thirdEmail      string
}

func seedConversationFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) conversationIntegrationFixture {
	t.Helper()
	fixture := conversationIntegrationFixture{
		tenantID: uuid.New(), foreignTenantID: uuid.New(),
		ownerID: uuid.New(), studentID: uuid.New(), thirdID: uuid.New(),
		foreignOwnerID: uuid.New(), classID: uuid.New(),
	}
	fixture.ownerEmail = integrationEmail("owner")
	fixture.studentEmail = integrationEmail("student")
	fixture.thirdEmail = integrationEmail("third")
	foreignEmail := integrationEmail("foreign")
	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin conversation fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	for _, user := range []struct {
		id    uuid.UUID
		email string
		name  string
	}{
		{fixture.ownerID, fixture.ownerEmail, "Conversation owner"},
		{fixture.studentID, fixture.studentEmail, "Conversation student"},
		{fixture.thirdID, fixture.thirdEmail, "Conversation third"},
		{fixture.foreignOwnerID, foreignEmail, "Conversation foreign"},
	} {
		if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.users (id, email, display_name)
VALUES ($1, $2, $3)`, user.id, user.email, user.name); err != nil {
			t.Fatalf("insert conversation fixture user: %v", err)
		}
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, 'Conversation integration'),
       ($3, $4, 'Conversation foreign integration')`,
		fixture.tenantID,
		integrationSlug("conversation"),
		fixture.foreignTenantID,
		integrationSlug("conversation-foreign"),
	); err != nil {
		t.Fatalf("insert conversation fixture tenants: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES
    ($1, $2, 'teacher', 'active', now()),
    ($1, $3, 'student', 'active', now()),
    ($1, $4, 'student', 'active', now()),
    ($5, $6, 'teacher', 'active', now())`,
		fixture.tenantID,
		fixture.ownerID,
		fixture.studentID,
		fixture.thirdID,
		fixture.foreignTenantID,
		fixture.foreignOwnerID,
	); err != nil {
		t.Fatalf("insert conversation fixture memberships: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES ($1, $2, $3, $4, 'Conversation integration class',
        'Asia/Ho_Chi_Minh', 'active')`,
		fixture.classID,
		fixture.tenantID,
		fixture.ownerID,
		"CV"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert conversation fixture class: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
)
VALUES ($1, $2, $3, 'student', 'active', $4, now())`,
		fixture.tenantID,
		fixture.classID,
		fixture.studentID,
		fixture.ownerID,
	); err != nil {
		t.Fatalf("insert conversation fixture enrollment: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit conversation fixture: %v", err)
	}
	return fixture
}

func integrationAccess(
	tenantID, actorID uuid.UUID,
	role policy.OrganizationRole,
) AccessContext {
	return AccessContext{
		TenantID: tenantID, ActorID: actorID, MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{role},
	}
}

func integrationEmail(prefix string) string {
	return fmt.Sprintf(
		"conversation-%s-%s@example.test",
		prefix,
		strings.ReplaceAll(uuid.NewString(), "-", ""),
	)
}

func integrationSlug(prefix string) string {
	return prefix + "-" + strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))[:12]
}

func requireConversationEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if strings.TrimSpace(os.Getenv("CI")) != "" {
			t.Fatalf("%s is required for conversation integration tests", key)
		}
		t.Skipf("%s is not configured; skipping PostgreSQL conversation integration test", key)
	}
	return value
}

func openConversationPool(
	t *testing.T,
	ctx context.Context,
	url string,
) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("create conversation integration pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping conversation integration pool: %v", err)
	}
	return pool
}
