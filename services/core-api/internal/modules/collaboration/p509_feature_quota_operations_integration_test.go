//go:build integration

package collaboration

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	p509DisposableConfirmation = "I_UNDERSTAND_P5_COLLAB_09_DISPOSABLE_ONLY"
	p509QueryTimeout           = 45 * time.Second
)

func TestP509RolePreflight(t *testing.T) {
	requireP509Disposable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	checks := []struct {
		environment string
		role        string
	}{
		{environment: "DATABASE_MIGRATION_URL", role: "neondb_owner"},
		{environment: "DATABASE_POOL_URL", role: "tutorhub_runtime"},
		{environment: "DATABASE_COLLABORATION_URL", role: "tutorhub_collab_worker"},
	}
	databaseName := ""
	for _, check := range checks {
		check := check
		t.Run(check.environment, func(t *testing.T) {
			pool := openP509Pool(t, ctx, check.environment)
			var currentRole, currentDatabase string
			if err := pool.QueryRow(ctx, `SELECT current_user, current_database()`).Scan(
				&currentRole,
				&currentDatabase,
			); err != nil {
				pool.Close()
				t.Fatal("query P5-COLLAB-09 disposable role identity")
			}
			pool.Close()
			if currentRole != check.role {
				t.Fatalf("P5-COLLAB-09 %s did not authenticate as the intended role", check.environment)
			}
			if databaseName == "" {
				databaseName = currentDatabase
			} else if currentDatabase != databaseName {
				t.Fatal("P5-COLLAB-09 credentials do not use the same disposable database")
			}
		})
	}
}

func TestP509FeatureQuotaOperationsPostgres(t *testing.T) {
	requireP509Disposable(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	ownerPool := openP509Pool(t, ctx, "DATABASE_MIGRATION_URL")
	defer ownerPool.Close()
	runtimePool := openP509Pool(t, ctx, "DATABASE_POOL_URL")
	defer runtimePool.Close()

	var version int
	var dirty bool
	if err := ownerPool.QueryRow(ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P5-COLLAB-09 migration ledger")
	}
	if version != 41 || dirty {
		t.Fatal("P5-COLLAB-09 database gates require clean ledger 41 false")
	}

	fixtureA := seedWhiteboardPostgresFixture(t, ctx, ownerPool)
	fixtureB := seedWhiteboardPostgresFixture(t, ctx, ownerPool)
	defer cleanupP509Fixture(ownerPool, fixtureA)
	defer cleanupP509Fixture(ownerPool, fixtureB)

	accessA := p509Access(fixtureA)
	accessB := p509Access(fixtureB)
	defaultControls := newP509Controls(t, ownerPool, featurecontrol.NewDefaultCatalog())
	defaultRepository := newP509Repository(t, ownerPool, defaultControls)
	if _, err := defaultRepository.Get(ctx, accessA, fixtureA.documentID); !errors.Is(err, ErrNotFound) {
		t.Fatal("compiled default-off must conceal whiteboard reads")
	}

	seedP509Overrides(t, ctx, ownerPool, fixtureA)
	seedP509Overrides(t, ctx, ownerPool, fixtureB)
	forcedOffCatalog, err := featurecontrol.NewCatalog(featurecontrol.Guardrails{
		ForcedOffFeatures: map[featurecontrol.FeatureKey]bool{
			featurecontrol.FeatureClassroomWhiteboards: true,
		},
	})
	if err != nil {
		t.Fatal("build P5-COLLAB-09 force-off catalog")
	}
	forcedOffRepository := newP509Repository(
		t,
		ownerPool,
		newP509Controls(t, ownerPool, forcedOffCatalog),
	)
	if _, err := forcedOffRepository.Get(ctx, accessA, fixtureA.documentID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deployment force-off must override a tenant enablement")
	}

	runtimeControls := newP509Controls(t, runtimePool, featurecontrol.NewDefaultCatalog())
	runtimeRepository := newP509Repository(t, runtimePool, runtimeControls)
	spacesA := []uuid.UUID{
		createP509MediaSpace(t, ctx, ownerPool, fixtureA, 24*time.Hour),
		createP509MediaSpace(t, ctx, ownerPool, fixtureA, 48*time.Hour),
	}
	assertP509ConcurrentDocumentQuota(t, ctx, runtimeRepository, accessA, spacesA)

	spaceB := createP509MediaSpace(t, ctx, ownerPool, fixtureB, 24*time.Hour)
	resultB, err := runtimeRepository.Create(ctx, accessB, p509CreateCommand(spaceB))
	if err != nil || !resultB.Created {
		t.Fatal("tenant B document creation must remain available after tenant A exhausts quota")
	}

	ownerControls := newP509Controls(t, ownerPool, featurecontrol.NewDefaultCatalog())
	assertP509FeatureControlUsageProjection(t, ctx, ownerControls, fixtureA)
	ownerRepository := newP509Repository(t, ownerPool, ownerControls)
	documentA, err := ownerRepository.Get(ctx, accessA, fixtureA.documentID)
	if err != nil {
		t.Fatal("load tenant A whiteboard document for storage quota gate")
	}
	documentB, err := ownerRepository.Get(ctx, accessB, fixtureB.documentID)
	if err != nil {
		t.Fatal("load tenant B whiteboard document for storage quota gate")
	}
	workflow, err := NewPostgresArtifactWorkflow(ownerPool, PostgresArtifactWorkflowConfig{
		QueryTimeout: p509QueryTimeout,
		Controls:     ownerControls,
	})
	if err != nil {
		t.Fatal("build P5-COLLAB-09 artifact workflow")
	}
	assertP509ArtifactEnqueuePrerequisites(t, ctx, ownerPool, ownerControls, accessA, documentA)
	assertP509ConcurrentStorageQuota(t, ctx, workflow, accessA, documentA)
	if _, err := workflow.RequestSnapshot(ctx, accessB, documentB, SnapshotCreateInput{
		ExpectedGeneration: documentB.CurrentGeneration,
		IdempotencyKey:     p502Key("p509-b-snapshot", uuid.New()),
	}); err != nil {
		t.Fatal("tenant B storage claim must remain available after tenant A exhausts quota")
	}

	var tenantACommands, tenantBCommands int
	if err := ownerPool.QueryRow(ctx,
		`SELECT count(*) FROM tutorhub.whiteboard_artifact_commands WHERE tenant_id = $1`,
		fixtureA.tenantID,
	).Scan(&tenantACommands); err != nil || tenantACommands != 1 {
		t.Fatal("tenant A storage gate must persist exactly one bounded reservation")
	}
	if err := ownerPool.QueryRow(ctx,
		`SELECT count(*) FROM tutorhub.whiteboard_artifact_commands WHERE tenant_id = $1`,
		fixtureB.tenantID,
	).Scan(&tenantBCommands); err != nil || tenantBCommands != 1 {
		t.Fatal("tenant B storage gate must remain isolated")
	}
}

func assertP509FeatureControlUsageProjection(
	t *testing.T,
	ctx context.Context,
	controls *featurecontrol.PostgresRepository,
	fixture whiteboardPostgresFixture,
) {
	t.Helper()
	capabilities, err := controls.GetCapabilities(ctx, tenancy.Context{
		TenantID: fixture.tenantID,
		ActorID:  fixture.actorID,
	}, time.Now().UTC())
	if err != nil {
		t.Fatal("read P5-COLLAB-09 feature/quota usage projection")
	}
	for _, quota := range capabilities.Quotas {
		if quota.Key == featurecontrol.QuotaWhiteboardStorageBytesPerTenant {
			return
		}
	}
	t.Fatal("P5-COLLAB-09 storage quota usage projection is missing")
}

func requireP509Disposable(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P5_COLLAB_09_DISPOSABLE_CONFIRM")) != p509DisposableConfirmation {
		t.Skip("P5_COLLAB_09_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
}

func openP509Pool(t *testing.T, ctx context.Context, environment string) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv(environment))
	if databaseURL == "" {
		t.Skip(environment + " is not configured")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("open P5-COLLAB-09 disposable database pool for " + environment)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			t.Fatalf(
				"ping P5-COLLAB-09 disposable database for %s failed with SQLSTATE %s",
				environment,
				postgresError.Code,
			)
		}
		var networkError *net.OpError
		if errors.As(err, &networkError) {
			t.Fatalf(
				"ping P5-COLLAB-09 disposable database for %s failed at the network boundary",
				environment,
			)
		}
		t.Fatalf(
			"ping P5-COLLAB-09 disposable database for %s failed with error type %T",
			environment,
			err,
		)
	}
	return pool
}

func newP509Controls(
	t *testing.T,
	pool *pgxpool.Pool,
	catalog *featurecontrol.Catalog,
) *featurecontrol.PostgresRepository {
	t.Helper()
	repository, err := featurecontrol.NewPostgresRepository(
		pool,
		p509QueryTimeout,
		policy.NewEngine(),
		catalog,
	)
	if err != nil {
		t.Fatal("build P5-COLLAB-09 feature controls")
	}
	return repository
}

func newP509Repository(
	t *testing.T,
	pool *pgxpool.Pool,
	controls *featurecontrol.PostgresRepository,
) *PostgresRepository {
	t.Helper()
	repository, err := NewPostgresRepository(pool, p509QueryTimeout, PostgresRepositoryConfig{
		Controls: controls,
		Quotas:   controls,
	})
	if err != nil {
		t.Fatal("build P5-COLLAB-09 collaboration repository")
	}
	return repository
}

func seedP509Overrides(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture whiteboardPostgresFixture,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin P5-COLLAB-09 control override fixture")
	}
	defer rollback(tx)
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.tenant_feature_control_revisions
		(tenant_id, version, updated_by) VALUES ($1, 1, $2)
		ON CONFLICT (tenant_id) DO UPDATE SET version = 1, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		fixture.tenantID, fixture.actorID,
	); err != nil {
		t.Fatal("seed P5-COLLAB-09 control revision")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.tenant_feature_overrides
		(tenant_id, feature_key, enabled, updated_by)
		VALUES ($1, 'classroom_whiteboards', true, $2)`,
		fixture.tenantID, fixture.actorID,
	); err != nil {
		t.Fatal("seed P5-COLLAB-09 feature override")
	}
	quotas := []struct {
		key   string
		limit int64
	}{
		{key: "whiteboard_documents_per_tenant", limit: 2},
		{key: "whiteboard_storage_bytes_per_tenant", limit: 24 * 1024 * 1024},
	}
	for _, quota := range quotas {
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.tenant_quota_overrides
			(tenant_id, quota_key, limit_value, updated_by)
			VALUES ($1, $2, $3, $4)`,
			fixture.tenantID, quota.key, quota.limit, fixture.actorID,
		); err != nil {
			t.Fatal("seed P5-COLLAB-09 quota override")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("commit P5-COLLAB-09 control overrides")
	}
}

func createP509MediaSpace(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture whiteboardPostgresFixture,
	offset time.Duration,
) uuid.UUID {
	t.Helper()
	var classID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT media_space.class_id
		FROM tutorhub.whiteboard_documents AS document
		JOIN tutorhub.media_spaces AS media_space
		  ON media_space.tenant_id = document.tenant_id
		 AND media_space.id = document.media_space_id
		WHERE document.tenant_id = $1 AND document.id = $2`,
		fixture.tenantID, fixture.documentID,
	).Scan(&classID); err != nil {
		t.Fatal("load P5-COLLAB-09 fixture class")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	sessionID, spaceID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.class_sessions
		(id, tenant_id, class_id, title, starts_at, ends_at, timezone, created_by, updated_by)
		VALUES ($1, $2, $3, 'P5-COLLAB-09 quota session', $4, $5,
		        'Asia/Ho_Chi_Minh', $6, $6)`,
		sessionID, fixture.tenantID, classID, now.Add(offset), now.Add(offset+time.Hour), fixture.actorID,
	); err != nil {
		t.Fatal("insert P5-COLLAB-09 quota class session")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_spaces
		(id, tenant_id, source_kind, class_id, source_class_session_id,
		 create_idempotency_key, create_request_fingerprint, created_by, updated_by)
		VALUES ($1, $2, 'class_session', $3, $4, $5, $6, $7, $7)`,
		spaceID, fixture.tenantID, classID, sessionID, p502Key("p509-space", spaceID),
		make([]byte, 32), fixture.actorID,
	); err != nil {
		t.Fatal("insert P5-COLLAB-09 quota media space")
	}
	return spaceID
}

func p509Access(fixture whiteboardPostgresFixture) AccessContext {
	return AccessContext{
		TenantID:         fixture.tenantID,
		ActorID:          fixture.actorID,
		SessionID:        uuid.New(),
		MembershipActive: true,
	}
}

func p509CreateCommand(mediaSpaceID uuid.UUID) CreateCommand {
	documentID := uuid.New()
	key := p502Key("p509-create", documentID)
	fingerprint := sha256.Sum256([]byte(key))
	return CreateCommand{
		DocumentID:           documentID,
		MediaSpaceID:         mediaSpaceID,
		ProviderDocumentName: p502ProviderName("p509", documentID),
		IdempotencyKey:       key,
		Fingerprint:          fingerprint[:],
		OccurredAt:           time.Now().UTC(),
	}
}

func assertP509ConcurrentDocumentQuota(
	t *testing.T,
	ctx context.Context,
	repository *PostgresRepository,
	access AccessContext,
	spaces []uuid.UUID,
) {
	t.Helper()
	results := make(chan error, len(spaces))
	var ready sync.WaitGroup
	ready.Add(len(spaces))
	start := make(chan struct{})
	for _, spaceID := range spaces {
		go func(spaceID uuid.UUID) {
			ready.Done()
			<-start
			_, err := repository.Create(ctx, access, p509CreateCommand(spaceID))
			results <- err
		}(spaceID)
	}
	ready.Wait()
	close(start)
	successes, exhausted := 0, 0
	for range spaces {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, ErrQuotaExceeded):
			exhausted++
		default:
			t.Fatalf("unexpected P5-COLLAB-09 document quota outcome: %v", err)
		}
	}
	if successes != 1 || exhausted != 1 {
		t.Fatal("concurrent document quota must allow one claim and fail one closed")
	}
}

func assertP509ConcurrentStorageQuota(
	t *testing.T,
	ctx context.Context,
	workflow *PostgresArtifactWorkflow,
	access AccessContext,
	document Document,
) {
	t.Helper()
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	go func() {
		ready.Done()
		<-start
		_, err := workflow.RequestSnapshot(ctx, access, document, SnapshotCreateInput{
			ExpectedGeneration: document.CurrentGeneration,
			IdempotencyKey:     p502Key("p509-snapshot", uuid.New()),
		})
		results <- err
	}()
	go func() {
		ready.Done()
		<-start
		_, err := workflow.RequestExport(ctx, access, document, ExportInput{
			ExpectedGeneration: document.CurrentGeneration,
			IdempotencyKey:     p502Key("p509-export", uuid.New()),
		})
		results <- err
	}()
	ready.Wait()
	close(start)
	successes, exhausted, unavailable := 0, 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, ErrQuotaExceeded):
			exhausted++
		case errors.Is(err, ErrArtifactUnavailable):
			unavailable++
		default:
			t.Fatalf("unexpected P5-COLLAB-09 storage quota outcome: %v", err)
		}
	}
	if successes != 1 || exhausted != 1 {
		t.Fatalf(
			"concurrent storage quota outcomes: success=%d quota=%d unavailable=%d",
			successes,
			exhausted,
			unavailable,
		)
	}
}

func assertP509ArtifactEnqueuePrerequisites(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	controls *featurecontrol.PostgresRepository,
	access AccessContext,
	document Document,
) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin P5-COLLAB-09 artifact prerequisite transaction")
	}
	defer rollback(tx)
	if err := controls.RequireFeature(ctx, tx, access.TenantID, featurecontrol.FeatureClassroomWhiteboards); err != nil {
		t.Fatal("P5-COLLAB-09 artifact feature prerequisite failed")
	}
	idempotencyKey := p502Key("p509-prerequisite", uuid.New())
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"whiteboard-artifact:"+access.TenantID.String()+":"+access.ActorID.String()+":"+idempotencyKey,
	); err != nil {
		t.Fatal("P5-COLLAB-09 artifact idempotency lock prerequisite failed")
	}
	var marker int
	err = tx.QueryRow(ctx, `SELECT 1 FROM tutorhub.whiteboard_artifact_commands
		WHERE tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3`,
		access.TenantID, access.ActorID, idempotencyKey,
	).Scan(&marker)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("P5-COLLAB-09 artifact replay prerequisite failed")
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"whiteboard-storage:"+access.TenantID.String(),
	); err != nil {
		t.Fatal("P5-COLLAB-09 artifact storage lock prerequisite failed")
	}
	var used, pending int64
	if err := tx.QueryRow(ctx, `SELECT
		COALESCE((SELECT sum(size_bytes) FROM tutorhub.whiteboard_snapshots WHERE tenant_id = $1), 0),
		COALESCE((SELECT count(*) FROM tutorhub.whiteboard_artifact_commands WHERE tenant_id = $1 AND command_kind IN ('snapshot', 'export') AND status IN ('pending', 'processing')), 0)`,
		access.TenantID,
	).Scan(&used, &pending); err != nil {
		t.Fatal("P5-COLLAB-09 artifact usage prerequisite failed")
	}
	if err := controls.RequireQuotaAtMost(
		ctx,
		tx,
		access.TenantID,
		featurecontrol.QuotaWhiteboardStorageBytesPerTenant,
		used+(pending+1)*maximumPortableImportBytes,
	); err != nil {
		t.Fatal("P5-COLLAB-09 artifact quota prerequisite failed")
	}
	fingerprint := artifactRequestFingerprint("snapshot", document.ID, document.CurrentGeneration)
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.whiteboard_artifact_commands
		(id, tenant_id, actor_user_id, document_id, generation, command_kind,
		 idempotency_key, request_fingerprint, source_snapshot_id,
		 target_generation, target_provider_document_name, requested_at,
		 available_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'snapshot', $6, $7, NULL, NULL, NULL, $8, $8, $8)`,
		uuid.New(), access.TenantID, access.ActorID, document.ID,
		document.CurrentGeneration, idempotencyKey, fingerprint, now,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) {
			t.Fatalf(
				"P5-COLLAB-09 artifact insert prerequisite failed: sqlstate=%s constraint=%s",
				postgresError.Code,
				postgresError.ConstraintName,
			)
		}
		t.Fatal("P5-COLLAB-09 artifact insert prerequisite failed")
	}
}

func cleanupP509Fixture(pool *pgxpool.Pool, fixture whiteboardPostgresFixture) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = pool.Exec(ctx, `DELETE FROM tutorhub.tenants WHERE id = $1`, fixture.tenantID)
	_, _ = pool.Exec(ctx, `DELETE FROM tutorhub.users WHERE id = $1`, fixture.actorID)
}
