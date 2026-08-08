//go:build integration

package discovery

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresDiscoveryAuthorizationIsolationAndLiteralSearch(t *testing.T) {
	migrationURL := requireDiscoveryEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireDiscoveryEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply discovery migrations: %v", err)
	}
	migrationPool := openDiscoveryPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openDiscoveryPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	cleanupStaleDiscoveryFixtures(t, ctx, migrationPool)
	fixture := seedDiscoveryFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupDiscoveryFixture(t, migrationPool, fixture) })

	repository, err := NewPostgresRepository(runtimePool, 10*time.Second)
	if err != nil {
		t.Fatalf("new discovery repository: %v", err)
	}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("new discovery service: %v", err)
	}
	studentAccess := integrationDiscoveryAccess(
		fixture.tenantID, fixture.studentID, policy.OrganizationRoleStudent,
	)

	files, err := service.RecentFiles(ctx, studentAccess, 10)
	if err != nil {
		t.Fatalf("student recent files: %v", err)
	}
	if len(files) != 1 || files[0].ID != fixture.visibleFileID ||
		files[0].ClassID != fixture.visibleClassID {
		t.Fatalf("student recent files leaked or missed authority: %+v", files)
	}

	page, err := service.Search(ctx, studentAccess, "100%_safe", 20)
	if err != nil {
		t.Fatalf("literal authorized search: %v", err)
	}
	assertDiscoveryResultIDs(t, page.Items, map[uuid.UUID]bool{
		fixture.visibleSessionID:     true,
		fixture.directConversationID: true,
		fixture.visibleFileID:        true,
	})
	for _, item := range page.Items {
		if item.ID == fixture.wildcardLookalikeSessionID ||
			item.ID == fixture.hiddenSessionID || item.ID == fixture.hiddenFileID ||
			item.ID == fixture.pendingFileID || item.ID == fixture.foreignFileID {
			t.Fatalf("literal/authorization boundary leaked result: %+v", item)
		}
	}

	teacherAccess := integrationDiscoveryAccess(
		fixture.tenantID, fixture.ownerID, policy.OrganizationRoleTeacher,
	)
	teacherPage, err := service.Search(ctx, teacherAccess, "hidden 100%_safe", 20)
	if err != nil {
		t.Fatalf("teacher organization search: %v", err)
	}
	if len(teacherPage.Items) != 3 {
		t.Fatalf("teacher organization authority results=%+v, want session/class conversation/file", teacherPage.Items)
	}

	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.memberships SET status = 'suspended' WHERE tenant_id = $1 AND user_id = $2`,
		fixture.tenantID,
		fixture.studentID,
	); err != nil {
		t.Fatalf("suspend discovery student: %v", err)
	}
	if _, err := service.Search(ctx, studentAccess, "100%_safe", 20); err != ErrAccessDenied {
		t.Fatalf("inactive membership error=%v, want access denied", err)
	}
}

type discoveryIntegrationFixture struct {
	tenantID                   uuid.UUID
	foreignTenantID            uuid.UUID
	ownerID                    uuid.UUID
	studentID                  uuid.UUID
	otherOwnerID               uuid.UUID
	foreignOwnerID             uuid.UUID
	visibleClassID             uuid.UUID
	hiddenClassID              uuid.UUID
	foreignClassID             uuid.UUID
	visibleSessionID           uuid.UUID
	hiddenSessionID            uuid.UUID
	wildcardLookalikeSessionID uuid.UUID
	visibleFileID              uuid.UUID
	hiddenFileID               uuid.UUID
	pendingFileID              uuid.UUID
	foreignFileID              uuid.UUID
	directConversationID       uuid.UUID
}

func seedDiscoveryFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) discoveryIntegrationFixture {
	t.Helper()
	fixture := discoveryIntegrationFixture{
		tenantID: uuid.New(), foreignTenantID: uuid.New(), ownerID: uuid.New(),
		studentID: uuid.New(), otherOwnerID: uuid.New(), foreignOwnerID: uuid.New(),
		visibleClassID: uuid.New(), hiddenClassID: uuid.New(), foreignClassID: uuid.New(),
		visibleSessionID: uuid.New(), hiddenSessionID: uuid.New(),
		wildcardLookalikeSessionID: uuid.New(), visibleFileID: uuid.New(),
		hiddenFileID: uuid.New(), pendingFileID: uuid.New(), foreignFileID: uuid.New(),
		directConversationID: uuid.New(),
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin discovery fixture: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, user := range []struct {
		id   uuid.UUID
		name string
	}{
		{fixture.ownerID, "Algebra Partner 100%_safe"},
		{fixture.studentID, "Discovery Student"},
		{fixture.otherOwnerID, "Hidden Owner"},
		{fixture.foreignOwnerID, "Foreign Owner"},
	} {
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.users (id, email, display_name)
VALUES ($1, $2, $3)`, user.id, discoveryEmail(), user.name); err != nil {
			t.Fatalf("insert discovery user: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, 'Discovery integration'), ($3, $4, 'Discovery foreign')`,
		fixture.tenantID, discoverySlug("discovery"),
		fixture.foreignTenantID, discoverySlug("discovery-foreign")); err != nil {
		t.Fatalf("insert discovery tenants: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.memberships
    (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, 'teacher', 'active', now()),
       ($1, $3, 'student', 'active', now()),
       ($1, $4, 'teacher', 'active', now()),
       ($5, $6, 'teacher', 'active', now())`,
		fixture.tenantID, fixture.ownerID, fixture.studentID, fixture.otherOwnerID,
		fixture.foreignTenantID, fixture.foreignOwnerID); err != nil {
		t.Fatalf("insert discovery memberships: %v", err)
	}
	for _, class := range []struct {
		id       uuid.UUID
		tenantID uuid.UUID
		ownerID  uuid.UUID
		title    string
	}{
		{fixture.visibleClassID, fixture.tenantID, fixture.ownerID, "Visible Algebra"},
		{fixture.hiddenClassID, fixture.tenantID, fixture.otherOwnerID, "Hidden 100%_safe"},
		{fixture.foreignClassID, fixture.foreignTenantID, fixture.foreignOwnerID, "Foreign 100%_safe"},
	} {
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.classes
    (id, tenant_id, owner_user_id, code, title, timezone, status)
VALUES ($1, $2, $3, $4, $5, 'Asia/Ho_Chi_Minh', 'active')`,
			class.id, class.tenantID, class.ownerID,
			"DS"+strings.ToUpper(uuid.NewString()[:8]), class.title); err != nil {
			t.Fatalf("insert discovery class: %v", err)
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.class_enrollments
    (tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at)
VALUES ($1, $2, $3, 'student', 'active', $4, now())`,
		fixture.tenantID, fixture.visibleClassID, fixture.studentID, fixture.ownerID); err != nil {
		t.Fatalf("insert discovery enrollment: %v", err)
	}
	for _, session := range []struct {
		id, tenantID, classID, actorID uuid.UUID
		title                          string
	}{
		{fixture.visibleSessionID, fixture.tenantID, fixture.visibleClassID, fixture.ownerID, "Algebra 100%_safe"},
		{fixture.hiddenSessionID, fixture.tenantID, fixture.hiddenClassID, fixture.otherOwnerID, "Hidden 100%_safe session"},
		{fixture.wildcardLookalikeSessionID, fixture.tenantID, fixture.visibleClassID, fixture.ownerID, "Algebra 100XXsafe"},
	} {
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.class_sessions
    (id, tenant_id, class_id, title, starts_at, ends_at, timezone, status, created_by, updated_by)
VALUES ($1, $2, $3, $4, now() + interval '1 day', now() + interval '2 days',
        'Asia/Ho_Chi_Minh', 'scheduled', $5, $5)`,
			session.id, session.tenantID, session.classID, session.title, session.actorID); err != nil {
			t.Fatalf("insert discovery session: %v", err)
		}
	}
	low, high := fixture.ownerID, fixture.studentID
	if high.String() < low.String() {
		low, high = high, low
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.conversations
    (id, tenant_id, kind, class_id, direct_user_low_id, direct_user_high_id, created_by_user_id)
VALUES ($1, $2, 'direct', NULL, $3, $4, $5),
       ($6, $2, 'class', $8, NULL, NULL, $7)`,
		fixture.directConversationID, fixture.tenantID, low, high, fixture.studentID,
		uuid.New(), fixture.otherOwnerID, fixture.hiddenClassID); err != nil {
		t.Fatalf("insert discovery conversations: %v", err)
	}
	for _, file := range []struct {
		id, tenantID, classID, creatorID uuid.UUID
		name                             string
		ready                            bool
	}{
		{fixture.visibleFileID, fixture.tenantID, fixture.visibleClassID, fixture.ownerID, "algebra-100%_safe.pdf", true},
		{fixture.hiddenFileID, fixture.tenantID, fixture.hiddenClassID, fixture.otherOwnerID, "hidden-100%_safe.pdf", true},
		{fixture.pendingFileID, fixture.tenantID, fixture.visibleClassID, fixture.ownerID, "pending-100%_safe.pdf", false},
		{fixture.foreignFileID, fixture.foreignTenantID, fixture.foreignClassID, fixture.foreignOwnerID, "foreign-100%_safe.pdf", true},
	} {
		seedDiscoveryFile(t, ctx, tx, file.id, file.tenantID, file.classID, file.creatorID, file.name, file.ready)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit discovery fixture: %v", err)
	}
	return fixture
}

func seedDiscoveryFile(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	fileID, tenantID, classID, creatorID uuid.UUID,
	name string,
	ready bool,
) {
	t.Helper()
	status := "pending"
	var storedSize any
	var storedType any
	var storedChecksum any
	var storageETag any
	var storageVersion any
	if ready {
		status = "ready"
		storedSize = int64(42)
		storedType = "application/pdf"
		storedChecksum = make([]byte, 32)
		storageETag = "etag"
		storageVersion = "version"
	}
	_, err := tx.Exec(ctx, `INSERT INTO tutorhub.content_files (
    id, tenant_id, class_id, creator_user_id, client_request_id,
    request_fingerprint, object_key, display_name, declared_media_type,
    expected_size_bytes, expected_checksum_sha256, status, upload_expires_at,
    stored_size_bytes, stored_media_type, stored_checksum_sha256,
    storage_etag, storage_version_id, uploaded_at, processing_at, ready_at,
    created_at, updated_at
) VALUES (
    $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6,
    'tenants/' || $2::uuid::text || '/files/' || $1::uuid::text || '/original',
    $7, 'application/pdf', 42, $8, $9, now() + interval '15 minutes',
    $10, $11, $12, $13, $14,
    CASE WHEN $9::text = 'ready' THEN now() - interval '2 minutes' END,
    CASE WHEN $9::text = 'ready' THEN now() - interval '1 minute' END,
    CASE WHEN $9::text = 'ready' THEN now() END,
    now() - interval '3 minutes', now()
)`, fileID, tenantID, classID, creatorID, uuid.New(), make([]byte, 32), name,
		make([]byte, 32), status, storedSize, storedType, storedChecksum, storageETag,
		storageVersion)
	if err != nil {
		t.Fatalf("insert discovery file: %v", err)
	}
}

func cleanupDiscoveryFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture discoveryIntegrationFixture,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := deleteDiscoveryFixtures(
		ctx,
		pool,
		[]uuid.UUID{fixture.tenantID, fixture.foreignTenantID},
		[]uuid.UUID{fixture.ownerID, fixture.studentID, fixture.otherOwnerID, fixture.foreignOwnerID},
	); err != nil {
		t.Errorf("delete discovery fixture: %v", err)
	}
}

func cleanupStaleDiscoveryFixtures(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT tenant.id, member.user_id
FROM tutorhub.tenants AS tenant
LEFT JOIN tutorhub.memberships AS member ON member.tenant_id = tenant.id
WHERE tenant.name IN ('Discovery integration', 'Discovery foreign')
  AND (tenant.slug LIKE 'discovery-%' OR tenant.slug LIKE 'discovery-foreign-%')`)
	if err != nil {
		t.Fatalf("query stale discovery fixtures: %v", err)
	}
	defer rows.Close()
	tenantSet := map[uuid.UUID]struct{}{}
	userSet := map[uuid.UUID]struct{}{}
	for rows.Next() {
		var tenantID uuid.UUID
		var userID uuid.NullUUID
		if err := rows.Scan(&tenantID, &userID); err != nil {
			t.Fatalf("scan stale discovery fixture: %v", err)
		}
		tenantSet[tenantID] = struct{}{}
		if userID.Valid {
			userSet[userID.UUID] = struct{}{}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stale discovery fixtures: %v", err)
	}
	rows.Close()
	if len(tenantSet) == 0 {
		return
	}
	tenantIDs := make([]uuid.UUID, 0, len(tenantSet))
	for id := range tenantSet {
		tenantIDs = append(tenantIDs, id)
	}
	userIDs := make([]uuid.UUID, 0, len(userSet))
	for id := range userSet {
		userIDs = append(userIDs, id)
	}
	if err := deleteDiscoveryFixtures(ctx, pool, tenantIDs, userIDs); err != nil {
		t.Fatalf("delete stale discovery fixtures: %v", err)
	}
}

func deleteDiscoveryFixtures(
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantIDs []uuid.UUID,
	userIDs []uuid.UUID,
) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	for _, relation := range []string{
		"content_files",
		"conversations",
		"class_sessions",
		"class_enrollments",
		"classes",
	} {
		if _, err := tx.Exec(
			ctx,
			"DELETE FROM tutorhub."+relation+" WHERE tenant_id = ANY($1::uuid[])",
			tenantIDs,
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(
		ctx,
		`DELETE FROM tutorhub.tenants WHERE id = ANY($1::uuid[])`,
		tenantIDs,
	); err != nil {
		return err
	}
	if len(userIDs) > 0 {
		if _, err := tx.Exec(
			ctx,
			`DELETE FROM tutorhub.users WHERE id = ANY($1::uuid[])`,
			userIDs,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func assertDiscoveryResultIDs(
	t *testing.T,
	items []SearchResult,
	expected map[uuid.UUID]bool,
) {
	t.Helper()
	actual := make(map[uuid.UUID]bool, len(items))
	for _, item := range items {
		actual[item.ID] = true
	}
	if len(actual) != len(expected) {
		t.Fatalf("search result count=%d, want %d: %+v", len(actual), len(expected), items)
	}
	for id := range expected {
		if !actual[id] {
			t.Fatalf("authorized search result %s missing: %+v", id, items)
		}
	}
}

func integrationDiscoveryAccess(
	tenantID, actorID uuid.UUID,
	role policy.OrganizationRole,
) AccessContext {
	return AccessContext{
		TenantID: tenantID, ActorID: actorID, MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{role},
	}
}

func requireDiscoveryEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Skipf("%s is required for PostgreSQL discovery integration", name)
	}
	return value
}

func openDiscoveryPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open discovery database pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping discovery database: %v", err)
	}
	return pool
}

func discoveryEmail() string {
	return "p3-12-" + strings.ReplaceAll(uuid.NewString(), "-", "") + "@example.test"
}

func discoverySlug(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:10]
}
