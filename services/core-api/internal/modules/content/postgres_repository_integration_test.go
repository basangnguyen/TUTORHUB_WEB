//go:build integration

package content

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/objectstorage"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresContentRuntimeExactACL(t *testing.T) {
	migrationURL := requireContentEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireContentEnvironment(t, "DATABASE_POOL_URL")
	runtimeURL := strings.TrimSpace(os.Getenv("DATABASE_CONVERSATION_ACL_TEST_URL"))
	if runtimeURL == "" {
		runtimeURL = poolURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply content migrations: %v", err)
	}
	migrationPool := openContentPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CONVERSATION_ACL_TEST_BOOTSTRAP")), "true") {
		requireContentACLBootstrapDatabase(t, migrationURL)
		provisionContentACLTestRole(t, ctx, migrationPool)
	}
	runtimePool := openContentPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	var runtimeRole, migrationRole string
	if err := runtimePool.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatalf("read content runtime identity: %v", err)
	}
	if err := migrationPool.QueryRow(ctx, "SELECT current_user").Scan(&migrationRole); err != nil {
		t.Fatalf("read content migration identity: %v", err)
	}
	if runtimeRole == migrationRole {
		t.Fatal("exact content ACL requires distinct runtime and migration roles")
	}
	var schemaUsage, schemaCreate bool
	if err := runtimePool.QueryRow(ctx, `
SELECT
    has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
    has_schema_privilege(current_user, 'tutorhub', 'CREATE')`).Scan(
		&schemaUsage, &schemaCreate,
	); err != nil {
		t.Fatalf("inspect content runtime schema ACL: %v", err)
	}
	if !schemaUsage || schemaCreate {
		t.Fatal("content runtime schema ACL is not exact")
	}

	for _, expectation := range []struct {
		relation      string
		insertColumns []string
		updateColumns []string
	}{
		{
			relation: "tutorhub.content_files",
			insertColumns: []string{
				"class_id", "client_request_id", "created_at", "creator_user_id",
				"declared_media_type", "display_name", "expected_checksum_sha256",
				"expected_size_bytes", "id", "object_key", "request_fingerprint",
				"tenant_id", "updated_at", "upload_expires_at",
			},
			updateColumns: []string{
				"deleted_at", "deletion_reason", "status", "storage_etag",
				"storage_version_id", "stored_checksum_sha256", "stored_media_type",
				"stored_size_bytes", "updated_at", "uploaded_at", "version",
			},
		},
		{
			relation:      "tutorhub.tenant_file_usage",
			insertColumns: []string{"tenant_id", "updated_at"},
			updateColumns: []string{
				"committed_bytes", "file_count", "reserved_bytes", "updated_at",
			},
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
			t.Fatalf("inspect content runtime ACL for %s: %v", expectation.relation, err)
		}
		if !selected || inserted || updated || deleted || truncated || referenced || triggered ||
			!columnSelected || !columnInserted || !columnUpdated {
			t.Fatalf("content runtime ACL mismatch for %s", expectation.relation)
		}
		assertExactContentUpdateColumns(
			t, ctx, runtimePool, expectation.relation, expectation.updateColumns,
		)
		assertExactContentColumns(
			t, ctx, runtimePool, expectation.relation, "INSERT", expectation.insertColumns,
		)
	}

	var publicTableGrants, publicColumnGrants int
	if err := migrationPool.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM information_schema.table_privileges
     WHERE table_schema = 'tutorhub'
       AND table_name = ANY($1::text[]) AND grantee = 'PUBLIC'),
    (SELECT count(*) FROM information_schema.column_privileges
     WHERE table_schema = 'tutorhub'
       AND table_name = ANY($1::text[]) AND grantee = 'PUBLIC')`,
		[]string{"content_files", "tenant_file_usage"},
	).Scan(&publicTableGrants, &publicColumnGrants); err != nil {
		t.Fatalf("inspect PUBLIC content grants: %v", err)
	}
	if publicTableGrants != 0 || publicColumnGrants != 0 {
		t.Fatalf("PUBLIC retained content privileges: table=%d column=%d", publicTableGrants, publicColumnGrants)
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
WHERE runtime.rolname = $1`, runtimeRole, migrationRole,
		[]string{"content_files", "tenant_file_usage"},
	).Scan(
		&notSuperuser, &noCreateRole, &noCreateDatabase, &noReplication,
		&noBypassRLS, &noMigrationInheritance, &notTableOwner,
	); err != nil {
		t.Fatalf("inspect content runtime role safety: %v", err)
	}
	if !notSuperuser || !noCreateRole || !noCreateDatabase || !noReplication || !noBypassRLS ||
		!noMigrationInheritance || !notTableOwner {
		t.Fatal("content runtime role safety boundary is not exact")
	}
	assertContentRuntimeDependencies(t, ctx, runtimePool)
}

func TestPostgresContentIntentFinalizeIsolationAndConcurrency(t *testing.T) {
	migrationURL := requireContentEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireContentEnvironment(t, "DATABASE_POOL_URL")
	runtimeURL := strings.TrimSpace(os.Getenv("DATABASE_CONVERSATION_ACL_TEST_URL"))
	if runtimeURL == "" {
		runtimeURL = poolURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply content migrations: %v", err)
	}
	migrationPool := openContentPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CONVERSATION_ACL_TEST_BOOTSTRAP")), "true") {
		provisionContentACLTestRole(t, ctx, migrationPool)
	}
	runtimePool := openContentPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	fixture := seedContentFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupContentFixture(t, migrationPool, fixture) })

	catalog := featurecontrol.NewDefaultCatalog()
	controls, err := featurecontrol.NewPostgresRepository(
		runtimePool, 10*time.Second, policy.NewEngine(), catalog,
	)
	if err != nil {
		t.Fatalf("create content controls: %v", err)
	}
	repository, err := NewPostgresRepository(
		runtimePool, 10*time.Second, policy.NewEngine(), controls,
	)
	if err != nil {
		t.Fatalf("create content repository: %v", err)
	}
	checksum := bytes.Repeat([]byte{0x5a}, 32)
	storage := &integrationMetadataReader{metadata: objectstorage.Metadata{
		ContentLength: 128, ContentType: "application/pdf",
		ChecksumSHA256: checksum, ETag: "etag-1", VersionID: "version-1",
	}}
	service, err := NewService(repository, storage)
	if err != nil {
		t.Fatalf("create content service: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	service.now = func() time.Time { return now }
	access := integrationContentAccess(fixture.tenantID, fixture.ownerID, policy.OrganizationRoleTeacher)
	input := CreateIntentInput{
		ClassID: fixture.classID, DisplayName: "lesson.pdf", DeclaredMediaType: "application/pdf",
		ExpectedSizeBytes: 128, ChecksumSHA256: hex.EncodeToString(checksum),
		ClientRequestID: uuid.New(),
	}
	type createOutcome struct {
		result CreateIntentResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan createOutcome, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, createErr := service.CreateIntent(context.Background(), access, input)
			outcomes <- createOutcome{result: result, err: createErr}
		}()
	}
	close(start)
	wait.Wait()
	close(outcomes)
	var created int
	var file File
	for outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent upload intent: %v", outcome.err)
		}
		if outcome.result.Created {
			created++
		}
		if file.ID == uuid.Nil {
			file = outcome.result.File
		} else if file.ID != outcome.result.File.ID {
			t.Fatal("concurrent idempotent requests returned different files")
		}
	}
	if created != 1 || file.Status != StatusPending || file.Version != 1 {
		t.Fatalf("unexpected concurrent intent result: created=%d file=%+v", created, file)
	}

	changed := input
	changed.DisplayName = "changed.pdf"
	if _, err := service.CreateIntent(ctx, access, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed idempotency retry error=%v, want conflict", err)
	}
	studentAccess := integrationContentAccess(
		fixture.tenantID, fixture.studentID, policy.OrganizationRoleStudent,
	)
	studentInput := input
	studentInput.ClientRequestID = uuid.New()
	if _, err := service.CreateIntent(ctx, studentAccess, studentInput); !errors.Is(err, ErrNotFound) {
		t.Fatalf("student upload error=%v, want concealed not found", err)
	}
	if _, err := service.Get(ctx, studentAccess, file.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("student pending metadata error=%v, want concealed not found", err)
	}
	foreignAccess := integrationContentAccess(
		fixture.foreignTenantID, fixture.foreignOwnerID, policy.OrganizationRoleTeacher,
	)
	if _, err := service.Get(ctx, foreignAccess, file.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign tenant file read error=%v, want not found", err)
	}

	type finalizeOutcome struct {
		file File
		err  error
	}
	start = make(chan struct{})
	finalized := make(chan finalizeOutcome, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			item, finalizeErr := service.Finalize(
				context.Background(), access, file.ID, FinalizeInput{ExpectedVersion: 1},
			)
			finalized <- finalizeOutcome{file: item, err: finalizeErr}
		}()
	}
	close(start)
	wait.Wait()
	close(finalized)
	for outcome := range finalized {
		if outcome.err != nil || outcome.file.Status != StatusUploaded || outcome.file.Version != 2 {
			t.Fatalf("concurrent finalize outcome=%+v err=%v", outcome.file, outcome.err)
		}
	}
	var count, reserved, committed int64
	if err := migrationPool.QueryRow(ctx, `
SELECT file_count, reserved_bytes, committed_bytes
FROM tutorhub.tenant_file_usage
WHERE tenant_id = $1`, fixture.tenantID).Scan(&count, &reserved, &committed); err != nil {
		t.Fatalf("read content usage: %v", err)
	}
	if count != 1 || reserved != 0 || committed != 128 {
		t.Fatalf("content usage count=%d reserved=%d committed=%d", count, reserved, committed)
	}
	if _, err := service.Get(ctx, studentAccess, file.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("student uploaded metadata error=%v, want concealed not found", err)
	}

	pendingInput := input
	pendingInput.ClientRequestID = uuid.New()
	pendingInput.DisplayName = "pending.pdf"
	pendingInput.ExpectedSizeBytes = 64
	now = now.Add(time.Minute)
	if _, err := service.CreateIntent(ctx, access, pendingInput); err != nil {
		t.Fatalf("create expiring upload intent: %v", err)
	}
	now = now.Add(16 * time.Minute)
	replacementInput := input
	replacementInput.ClientRequestID = uuid.New()
	replacementInput.DisplayName = "replacement.pdf"
	replacementInput.ExpectedSizeBytes = 32
	if _, err := service.CreateIntent(ctx, access, replacementInput); err != nil {
		t.Fatalf("create upload intent after expiry cleanup: %v", err)
	}
	if _, err := service.CreateIntent(ctx, access, pendingInput); !errors.Is(err, ErrIntentExpired) {
		t.Fatalf("expired idempotency replay error=%v, want expired", err)
	}
	if err := migrationPool.QueryRow(ctx, `
SELECT file_count, reserved_bytes, committed_bytes
FROM tutorhub.tenant_file_usage
WHERE tenant_id = $1`, fixture.tenantID).Scan(&count, &reserved, &committed); err != nil {
		t.Fatalf("read content usage after expiry: %v", err)
	}
	if count != 2 || reserved != 32 || committed != 128 {
		t.Fatalf("content usage after expiry count=%d reserved=%d committed=%d", count, reserved, committed)
	}

	if _, err := migrationPool.Exec(ctx, `
UPDATE tutorhub.classes
SET status = 'archived', archived_at = $2, updated_at = $2
WHERE tenant_id = $1 AND id = $3`, fixture.tenantID, now, fixture.classID); err != nil {
		t.Fatalf("archive content fixture class: %v", err)
	}
	archivedInput := input
	archivedInput.ClientRequestID = uuid.New()
	if _, err := service.CreateIntent(ctx, access, archivedInput); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("archived class upload error=%v, want read only", err)
	}
}

func provisionContentACLTestRole(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
DO $bootstrap$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'tutorhub_conversation_runtime_ci') THEN
        RAISE EXCEPTION 'run the conversation ACL bootstrap before the content ACL bootstrap';
    END IF;
END
$bootstrap$;

REVOKE ALL ON TABLE tutorhub.content_files, tutorhub.tenant_file_usage
FROM tutorhub_conversation_runtime_ci;

-- The isolated CI role starts with only the conversation grants. Recreate the
-- previously accepted Core API dependencies that the content flow reads and
-- the quota window it mutates; production roles already receive these grants
-- from their earlier phase provisioning.
GRANT SELECT ON TABLE
    tutorhub.tenants,
    tutorhub.memberships,
    tutorhub.users,
    tutorhub.classes,
    tutorhub.class_enrollments,
    tutorhub.tenant_feature_overrides,
    tutorhub.tenant_quota_overrides,
    tutorhub.tenant_quota_windows
TO tutorhub_conversation_runtime_ci;

GRANT INSERT, UPDATE ON TABLE tutorhub.tenant_quota_windows
TO tutorhub_conversation_runtime_ci;

GRANT SELECT ON TABLE tutorhub.content_files, tutorhub.tenant_file_usage
TO tutorhub_conversation_runtime_ci;

GRANT INSERT (
    id, tenant_id, class_id, creator_user_id, client_request_id,
    request_fingerprint, object_key, display_name, declared_media_type,
    expected_size_bytes, expected_checksum_sha256, upload_expires_at,
    created_at, updated_at
) ON TABLE tutorhub.content_files TO tutorhub_conversation_runtime_ci;

GRANT INSERT (tenant_id, updated_at)
ON TABLE tutorhub.tenant_file_usage TO tutorhub_conversation_runtime_ci;

GRANT UPDATE (
    status, version, stored_size_bytes, stored_media_type,
    stored_checksum_sha256, storage_etag, storage_version_id,
    uploaded_at, deleted_at, deletion_reason, updated_at
) ON TABLE tutorhub.content_files TO tutorhub_conversation_runtime_ci;

GRANT UPDATE (file_count, reserved_bytes, committed_bytes, updated_at)
ON TABLE tutorhub.tenant_file_usage TO tutorhub_conversation_runtime_ci;
`); err != nil {
		t.Fatalf("provision content ACL test role: %v", err)
	}
}

func assertContentRuntimeDependencies(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	for _, expectation := range []struct {
		relation   string
		privileges []string
	}{
		{relation: "tutorhub.tenants", privileges: []string{"SELECT"}},
		{relation: "tutorhub.memberships", privileges: []string{"SELECT"}},
		{relation: "tutorhub.users", privileges: []string{"SELECT"}},
		{relation: "tutorhub.classes", privileges: []string{"SELECT"}},
		{relation: "tutorhub.class_enrollments", privileges: []string{"SELECT"}},
		{relation: "tutorhub.tenant_feature_overrides", privileges: []string{"SELECT"}},
		{relation: "tutorhub.tenant_quota_overrides", privileges: []string{"SELECT"}},
		{
			relation:   "tutorhub.tenant_quota_windows",
			privileges: []string{"SELECT", "INSERT", "UPDATE"},
		},
	} {
		for _, privilege := range expectation.privileges {
			var allowed bool
			if err := pool.QueryRow(
				ctx,
				"SELECT has_table_privilege(current_user, $1, $2)",
				expectation.relation,
				privilege,
			).Scan(&allowed); err != nil {
				t.Fatalf(
					"inspect content runtime dependency %s %s: %v",
					expectation.relation,
					privilege,
					err,
				)
			}
			if !allowed {
				t.Fatalf(
					"content runtime is missing %s on %s",
					privilege,
					expectation.relation,
				)
			}
		}
	}
}

func assertExactContentUpdateColumns(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	relation string,
	expected []string,
) {
	t.Helper()
	assertExactContentColumns(t, ctx, pool, relation, "UPDATE", expected)
}

func assertExactContentColumns(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	relation string,
	privilege string,
	expected []string,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `
SELECT column_name
FROM information_schema.columns
WHERE table_schema = split_part($1, '.', 1)
  AND table_name = split_part($1, '.', 2)
	  AND has_column_privilege(current_user, $1, column_name, $2)
ORDER BY column_name`, relation, privilege)
	if err != nil {
		t.Fatalf("query content %s columns for %s: %v", privilege, relation, err)
	}
	defer rows.Close()
	actual := make([]string, 0, len(expected))
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scan content update column: %v", err)
		}
		actual = append(actual, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate content update columns: %v", err)
	}
	if strings.Join(actual, ",") != strings.Join(expected, ",") {
		t.Fatalf("runtime %s columns for %s=%v, want %v", privilege, relation, actual, expected)
	}
}

type contentIntegrationFixture struct {
	tenantID        uuid.UUID
	foreignTenantID uuid.UUID
	ownerID         uuid.UUID
	studentID       uuid.UUID
	foreignOwnerID  uuid.UUID
	classID         uuid.UUID
}

func seedContentFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) contentIntegrationFixture {
	t.Helper()
	fixture := contentIntegrationFixture{
		tenantID: uuid.New(), foreignTenantID: uuid.New(), ownerID: uuid.New(),
		studentID: uuid.New(), foreignOwnerID: uuid.New(), classID: uuid.New(),
	}
	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin content fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	for _, user := range []struct {
		id   uuid.UUID
		name string
	}{
		{fixture.ownerID, "Content owner"},
		{fixture.studentID, "Content student"},
		{fixture.foreignOwnerID, "Content foreign owner"},
	} {
		if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.users (id, email, display_name)
VALUES ($1, $2, $3)`, user.id, contentIntegrationEmail(), user.name); err != nil {
			t.Fatalf("insert content user: %v", err)
		}
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, 'Content integration'), ($3, $4, 'Content foreign integration')`,
		fixture.tenantID, contentIntegrationSlug("content"),
		fixture.foreignTenantID, contentIntegrationSlug("content-foreign"),
	); err != nil {
		t.Fatalf("insert content tenants: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, 'teacher', 'active', now()),
       ($1, $3, 'student', 'active', now()),
       ($4, $5, 'teacher', 'active', now())`,
		fixture.tenantID, fixture.ownerID, fixture.studentID,
		fixture.foreignTenantID, fixture.foreignOwnerID,
	); err != nil {
		t.Fatalf("insert content memberships: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES ($1, $2, $3, $4, 'Content integration class', 'Asia/Ho_Chi_Minh', 'active')`,
		fixture.classID, fixture.tenantID, fixture.ownerID,
		"FI"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert content class: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
)
VALUES ($1, $2, $3, 'student', 'active', $4, now())`,
		fixture.tenantID, fixture.classID, fixture.studentID, fixture.ownerID,
	); err != nil {
		t.Fatalf("insert content enrollment: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit content fixture: %v", err)
	}
	return fixture
}

func cleanupContentFixture(t *testing.T, pool *pgxpool.Pool, fixture contentIntegrationFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.content_files WHERE tenant_id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.tenantID, fixture.foreignTenantID}); err != nil {
		t.Errorf("delete content fixture files: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.tenant_file_usage WHERE tenant_id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.tenantID, fixture.foreignTenantID}); err != nil {
		t.Errorf("delete content fixture usage: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.class_enrollments WHERE tenant_id = $1 AND class_id = $2`,
		fixture.tenantID, fixture.classID); err != nil {
		t.Errorf("delete content fixture enrollments: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.classes WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID, fixture.classID); err != nil {
		t.Errorf("delete content fixture class: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.tenants WHERE id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.tenantID, fixture.foreignTenantID}); err != nil {
		t.Errorf("delete content fixture tenants: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.users WHERE id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.ownerID, fixture.studentID, fixture.foreignOwnerID}); err != nil {
		t.Errorf("delete content fixture users: %v", err)
	}
}

func integrationContentAccess(
	tenantID, actorID uuid.UUID,
	role policy.OrganizationRole,
) AccessContext {
	return AccessContext{
		TenantID: tenantID, ActorID: actorID, MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{role},
	}
}

type integrationMetadataReader struct {
	metadata objectstorage.Metadata
}

func (reader *integrationMetadataReader) Head(context.Context, string) (objectstorage.Metadata, error) {
	return reader.metadata, nil
}

func contentIntegrationEmail() string {
	return fmt.Sprintf("content-%s@example.test", strings.ReplaceAll(uuid.NewString(), "-", ""))
}

func contentIntegrationSlug(prefix string) string {
	return prefix + "-" + strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))[:12]
}

func requireContentEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if strings.TrimSpace(os.Getenv("CI")) != "" {
			t.Fatalf("%s is required for content integration tests", key)
		}
		t.Skipf("%s is not configured", key)
	}
	return value
}

func openContentPool(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open content database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping content database: %v", err)
	}
	return pool
}

func requireContentACLBootstrapDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("CI")), "true") {
		t.Fatal("content ACL bootstrap is restricted to CI")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal("parse isolated CI content database URL")
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	database := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if (host != "localhost" && host != "127.0.0.1") || database != "tutorhub_test" {
		t.Fatal("content ACL bootstrap requires the isolated loopback CI database")
	}
}
