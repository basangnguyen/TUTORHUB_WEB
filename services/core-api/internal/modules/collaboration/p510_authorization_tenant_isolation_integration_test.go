//go:build integration

package collaboration

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const p510DisposableConfirmation = "I_UNDERSTAND_P5_COLLAB_10_DISPOSABLE_ONLY"

func TestP510RolePreflight(t *testing.T) {
	requireP510Disposable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	checks := []struct {
		environment string
		role        string
	}{
		{environment: "DATABASE_MIGRATION_URL", role: "neondb_owner"},
		{environment: "DATABASE_POOL_URL", role: "tutorhub_runtime"},
	}
	databaseName := ""
	for _, check := range checks {
		pool := openP510Pool(t, ctx, check.environment)
		var currentRole, currentDatabase string
		if err := pool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(&currentRole, &currentDatabase); err != nil {
			pool.Close()
			t.Fatal("query P5-COLLAB-10 disposable role identity")
		}
		pool.Close()
		if currentRole != check.role {
			t.Fatalf("P5-COLLAB-10 %s did not authenticate as the intended role", check.environment)
		}
		if databaseName == "" {
			databaseName = currentDatabase
		} else if currentDatabase != databaseName {
			t.Fatal("P5-COLLAB-10 credentials do not use the same disposable database")
		}
	}
}

func TestP510AuthorizationTenantIsolationPostgres(t *testing.T) {
	requireP510Disposable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	ownerPool := openP510Pool(t, ctx, "DATABASE_MIGRATION_URL")
	defer ownerPool.Close()
	runtimePool := openP510Pool(t, ctx, "DATABASE_POOL_URL")
	defer runtimePool.Close()

	var version int
	var dirty bool
	if err := ownerPool.QueryRow(ctx, `SELECT version, dirty FROM public.tutorhub_schema_migrations`).Scan(&version, &dirty); err != nil || version != 41 || dirty {
		t.Fatal("P5-COLLAB-10 requires clean disposable ledger 41 false")
	}

	fixtureA := seedWhiteboardPostgresFixture(t, ctx, ownerPool)
	fixtureB := seedWhiteboardPostgresFixture(t, ctx, ownerPool)
	defer cleanupP509Fixture(ownerPool, fixtureA)
	defer cleanupP509Fixture(ownerPool, fixtureB)
	seedP509Overrides(t, ctx, ownerPool, fixtureA)
	seedP509Overrides(t, ctx, ownerPool, fixtureB)
	seedP510Snapshots(t, ctx, ownerPool, fixtureA, 3)
	seedP510Snapshots(t, ctx, ownerPool, fixtureB, 1)

	controls := newP509Controls(t, runtimePool, featureCatalogForP510())
	repository := newP509Repository(t, runtimePool, controls)
	documentA, err := repository.Get(ctx, p510Access(fixtureA), fixtureA.documentID)
	if err != nil {
		t.Fatal("load tenant A document")
	}
	service := newTestService(
		t,
		repository,
		&fakeSpaceAuthority{space: manageableSpace(documentA.MediaSpaceID)},
		nil,
		nil,
		uuid.New(),
	)

	accessA := p510Access(fixtureA)
	first, err := service.ListSnapshots(ctx, accessA, fixtureA.documentID, SnapshotListInput{Limit: 2})
	if err != nil || len(first.Items) != 2 || first.NextCursor == nil {
		t.Fatalf("tenant A scoped snapshot page=%+v err=%v", first, err)
	}
	second, err := service.ListSnapshots(ctx, accessA, fixtureA.documentID, SnapshotListInput{
		Limit: 2, Cursor: *first.NextCursor,
	})
	if err != nil || len(second.Items) != 1 || second.NextCursor != nil {
		t.Fatalf("tenant A second snapshot page=%+v err=%v", second, err)
	}

	accessB := p510Access(fixtureB)
	if _, err := service.Get(ctx, accessB, fixtureA.documentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant document ID was not concealed: %v", err)
	}
	if _, err := service.ListSnapshots(ctx, accessB, fixtureA.documentID, SnapshotListInput{
		Limit: 2, Cursor: *first.NextCursor,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant snapshot cursor was not concealed: %v", err)
	}
	if items, err := repository.ListSnapshots(ctx, accessB, fixtureA.documentID, SnapshotPageCursor{}, 10); err != nil || len(items) != 0 {
		t.Fatalf("repository snapshot tenant predicate failed: items=%d err=%v", len(items), err)
	}
	if _, err := service.Export(ctx, accessB, fixtureA.documentID, ExportInput{
		ExpectedGeneration: 1, IdempotencyKey: p502Key("p510-cross-export", uuid.New()),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant export was not concealed before artifact dependency: %v", err)
	}
	if _, err := service.Restore(ctx, accessB, fixtureA.documentID, RestoreInput{
		SnapshotID: fixtureA.snapshotID, ExpectedVersion: 1, ExpectedGeneration: 1,
		IdempotencyKey: p502Key("p510-cross-restore", uuid.New()),
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant restore was not concealed before artifact dependency: %v", err)
	}

	forged := accessA
	forged.OrganizationRoles = []policy.OrganizationRole{policy.OrganizationRoleTeacher, "forged-admin"}
	if _, err := service.Get(ctx, forged, fixtureA.documentID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("forged role did not fail closed: %v", err)
	}
	inactive := accessA
	inactive.MembershipActive = false
	if _, err := service.Get(ctx, inactive, fixtureA.documentID); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("inactive membership did not fail closed: %v", err)
	}

	assertP510IdempotencyScope(t, ctx, ownerPool, repository, fixtureA, fixtureB)
}

func assertP510IdempotencyScope(
	t *testing.T,
	ctx context.Context,
	ownerPool *pgxpool.Pool,
	repository *PostgresRepository,
	fixtureA, fixtureB whiteboardPostgresFixture,
) {
	t.Helper()
	key := p502Key("p510-scope", uuid.New())
	fingerprint := sha256.Sum256([]byte("p510-scope-original"))
	command := func(documentID uuid.UUID) TransitionCommand {
		return TransitionCommand{
			DocumentID: documentID, Operation: "open", ExpectedVersion: 1,
			IdempotencyKey: key, Fingerprint: fingerprint[:], OccurredAt: time.Now().UTC(),
		}
	}
	if _, err := repository.Transition(ctx, p510Access(fixtureA), command(fixtureA.documentID)); err != nil {
		t.Fatal("tenant A scoped idempotency command failed")
	}
	if _, err := repository.Transition(ctx, p510Access(fixtureB), command(fixtureB.documentID)); err != nil {
		t.Fatal("same opaque key must remain isolated in tenant B")
	}
	changed := command(fixtureA.documentID)
	changedFingerprint := sha256.Sum256([]byte("p510-scope-forged-replay"))
	changed.Fingerprint = changedFingerprint[:]
	if _, err := repository.Transition(ctx, p510Access(fixtureA), changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("same-principal changed replay did not conflict: %v", err)
	}

	secondActor := uuid.New()
	if _, err := ownerPool.Exec(ctx, `INSERT INTO tutorhub.users (id, email, display_name)
		VALUES ($1, $2, 'P5-COLLAB-10 second actor')`, secondActor,
		"p510-"+strings.ReplaceAll(secondActor.String(), "-", "")+"@example.test"); err != nil {
		t.Fatal("seed P5-COLLAB-10 second principal")
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO tutorhub.memberships
		(tenant_id, user_id, role, status, joined_at) VALUES ($1, $2, 'teacher', 'active', now())`,
		fixtureA.tenantID, secondActor); err != nil {
		t.Fatal("seed P5-COLLAB-10 second principal membership")
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		_, _ = ownerPool.Exec(cleanupCtx,
			`DELETE FROM tutorhub.memberships WHERE tenant_id = $1 AND user_id = $2`,
			fixtureA.tenantID, secondActor)
		_, _ = ownerPool.Exec(cleanupCtx, `DELETE FROM tutorhub.users WHERE id = $1`, secondActor)
	}()
	secondAccess := p510Access(fixtureA)
	secondAccess.ActorID = secondActor
	secondFingerprint := sha256.Sum256([]byte("p510-second-principal"))
	if _, err := repository.Transition(ctx, secondAccess, TransitionCommand{
		DocumentID: fixtureA.documentID, Operation: "close", ExpectedVersion: 2,
		IdempotencyKey: key, Fingerprint: secondFingerprint[:], OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal("same opaque key must not return another principal's receipt")
	}
}

func seedP510Snapshots(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture whiteboardPostgresFixture, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		watermark := sha256.Sum256([]byte("p510-watermark-" + uuid.NewString()))
		content := sha256.Sum256([]byte("p510-content-" + uuid.NewString()))
		if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.whiteboard_snapshots
			(id, tenant_id, document_id, generation, snapshot_kind, format_version,
			 schema_version, causal_watermark_sha256, content_sha256, size_bytes,
			 object_key, object_version_id, verification_key_id, provenance_kind, created_by, created_at,
			 retention_until)
			VALUES ($1, $2, $3, 1, 'manual', '1', 1, $4, $5, 1024,
			        $6, $7, 'p510-verify-key', 'user', $8, $9::timestamptz,
			        $9::timestamptz + interval '14 days')`,
			uuid.New(), fixture.tenantID, fixture.documentID, watermark[:], content[:],
			p502ObjectKey(content), "p510-version-"+uuid.NewString(), fixture.actorID,
			time.Now().UTC().Add(-time.Duration(index)*time.Minute),
		); err != nil {
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) {
				t.Fatalf("seed P5-COLLAB-10 snapshot: SQLSTATE %s constraint %s", postgresError.Code, postgresError.ConstraintName)
			}
			t.Fatal("seed P5-COLLAB-10 snapshot")
		}
	}
}

func p510Access(fixture whiteboardPostgresFixture) AccessContext {
	return AccessContext{
		TenantID: fixture.tenantID, ActorID: fixture.actorID, SessionID: uuid.New(),
		MembershipActive: true, OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleTeacher},
	}
}

func featureCatalogForP510() *featurecontrol.Catalog {
	return featurecontrol.NewDefaultCatalog()
}

func requireP510Disposable(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P5_COLLAB_10_DISPOSABLE_CONFIRM")) != p510DisposableConfirmation {
		t.Skip("P5_COLLAB_10_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
}

func openP510Pool(t *testing.T, ctx context.Context, environment string) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv(environment))
	if databaseURL == "" {
		t.Skip(environment + " is not configured")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("open P5-COLLAB-10 disposable database pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			t.Fatalf("P5-COLLAB-10 database authentication failed with SQLSTATE %s", postgresError.Code)
		}
		t.Fatal("P5-COLLAB-10 disposable database is unavailable")
	}
	return pool
}
