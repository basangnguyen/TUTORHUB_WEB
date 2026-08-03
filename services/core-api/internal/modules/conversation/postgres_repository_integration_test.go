//go:build integration

package conversation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresConversationCoreRuntimeExactACL(t *testing.T) {
	migrationURL := requireConversationEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireConversationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply conversation ACL migrations: %v", err)
	}
	migrationPool := openConversationPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	runtimePool := openConversationPool(t, ctx, poolURL)
	defer runtimePool.Close()

	var runtimeRole, migrationRole string
	if err := runtimePool.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatalf("read conversation runtime identity: %v", err)
	}
	if err := migrationPool.QueryRow(ctx, "SELECT current_user").Scan(&migrationRole); err != nil {
		t.Fatalf("read conversation migration identity: %v", err)
	}
	if runtimeRole == migrationRole {
		t.Skip("exact conversation ACL requires distinct runtime and migration roles")
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

	for _, relation := range []string{
		"tutorhub.conversations",
		"tutorhub.conversation_members",
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
    has_any_column_privilege(current_user, $1, 'UPDATE')`, relation).Scan(
			&selected, &inserted, &updated, &deleted, &truncated, &referenced, &triggered,
			&columnSelected, &columnInserted, &columnUpdated,
		); err != nil {
			t.Fatalf("inspect conversation runtime ACL for %s: %v", relation, err)
		}
		if !selected || !inserted || updated || deleted || truncated || referenced || triggered ||
			!columnSelected || !columnInserted || columnUpdated {
			t.Fatalf("conversation runtime ACL mismatch for %s", relation)
		}
	}

	var notSuperuser, noMigrationInheritance, notTableOwner bool
	if err := migrationPool.QueryRow(ctx, `
SELECT
    NOT runtime.rolsuper,
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
		[]string{"conversations", "conversation_members"},
	).Scan(&notSuperuser, &noMigrationInheritance, &notTableOwner); err != nil {
		t.Fatalf("inspect conversation runtime role safety: %v", err)
	}
	if !notSuperuser || !noMigrationInheritance || !notTableOwner {
		t.Fatal("conversation runtime role safety boundary is not exact")
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
