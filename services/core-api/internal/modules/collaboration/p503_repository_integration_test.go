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
	"github.com/jackc/pgx/v5/pgxpool"
)

const p5Collab03DisposableConfirmation = "I_UNDERSTAND_P5_COLLAB_03_DISPOSABLE_ONLY"

func TestP503PostgresRepositoryLifecycleAndRestore(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P5_COLLAB_03_DISPOSABLE_CONFIRM")) != p5Collab03DisposableConfirmation {
		t.Skip("P5_COLLAB_03_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	migrationURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	runtimeURL := strings.TrimSpace(os.Getenv("DATABASE_POOL_URL"))
	if migrationURL == "" || runtimeURL == "" {
		t.Skip("DATABASE_MIGRATION_URL and DATABASE_POOL_URL are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ownerPool, err := pgxpool.New(ctx, migrationURL)
	if err != nil {
		t.Fatal("open P5-COLLAB-03 owner pool")
	}
	t.Cleanup(ownerPool.Close)
	if err := ownerPool.Ping(ctx); err != nil {
		t.Fatal("P5-COLLAB-03 owner database is unavailable")
	}
	runtimePool, err := pgxpool.New(ctx, runtimeURL)
	if err != nil {
		t.Fatal("open P5-COLLAB-03 runtime pool")
	}
	t.Cleanup(runtimePool.Close)
	if err := runtimePool.Ping(ctx); err != nil {
		t.Fatal("P5-COLLAB-03 runtime database is unavailable")
	}

	fixture := seedWhiteboardPostgresFixture(t, ctx, ownerPool)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, cleanup := range []struct {
			name      string
			statement string
		}{
			{name: "mutation receipts", statement: `DELETE FROM tutorhub.whiteboard_document_mutation_receipts WHERE tenant_id = $1`},
			{name: "current generation reset", statement: `UPDATE tutorhub.whiteboard_documents SET current_generation = 1 WHERE tenant_id = $1`},
			{name: "restored generations", statement: `DELETE FROM tutorhub.whiteboard_document_generations WHERE tenant_id = $1 AND generation > 1`},
			{name: "snapshots", statement: `DELETE FROM tutorhub.whiteboard_snapshots WHERE tenant_id = $1`},
			{name: "whiteboard documents", statement: `DELETE FROM tutorhub.whiteboard_documents WHERE tenant_id = $1`},
			{name: "media spaces", statement: `DELETE FROM tutorhub.media_spaces WHERE tenant_id = $1`},
			{name: "class sessions", statement: `DELETE FROM tutorhub.class_sessions WHERE tenant_id = $1`},
			{name: "class enrollments", statement: `DELETE FROM tutorhub.class_enrollments WHERE tenant_id = $1`},
			{name: "classes", statement: `DELETE FROM tutorhub.classes WHERE tenant_id = $1`},
			{name: "memberships", statement: `DELETE FROM tutorhub.memberships WHERE tenant_id = $1`},
		} {
			if _, cleanupErr := ownerPool.Exec(cleanupCtx, cleanup.statement, fixture.tenantID); cleanupErr != nil {
				t.Errorf("P5-COLLAB-03 %s fixture cleanup failed", cleanup.name)
				return
			}
		}
		if tag, cleanupErr := ownerPool.Exec(cleanupCtx,
			`DELETE FROM tutorhub.tenants WHERE id = $1`, fixture.tenantID,
		); cleanupErr != nil || tag.RowsAffected() != 1 {
			t.Error("P5-COLLAB-03 fixture tenant cleanup failed")
		}
		if tag, cleanupErr := ownerPool.Exec(cleanupCtx,
			`DELETE FROM tutorhub.users WHERE id = $1`, fixture.actorID,
		); cleanupErr != nil || tag.RowsAffected() != 1 {
			t.Error("P5-COLLAB-03 fixture user cleanup failed")
		}
	})

	repository, err := NewPostgresRepository(runtimePool, 10*time.Second)
	if err != nil {
		t.Fatalf("create runtime repository: %v", err)
	}
	access := AccessContext{
		TenantID: fixture.tenantID, ActorID: fixture.actorID, SessionID: uuid.New(),
		MembershipActive: true,
	}
	assertP503RuntimeCreate(t, ctx, ownerPool, repository, access, fixture)

	document, err := repository.Get(ctx, access, fixture.documentID)
	if err != nil || document.Status != DocumentCreated || document.Version != 1 {
		t.Fatalf("runtime get: document=%+v err=%v", document, err)
	}
	policies, err := repository.CapabilityPolicies(ctx, access, fixture.documentID)
	if err != nil || policies[AudienceHost] != CapabilityPresent {
		t.Fatalf("runtime policies: policies=%+v err=%v", policies, err)
	}

	openedAt := time.Now().UTC().Truncate(time.Microsecond)
	openFingerprint := sha256.Sum256([]byte("p503-open"))
	openCommand := TransitionCommand{
		DocumentID: fixture.documentID, Operation: "open", ExpectedVersion: 1,
		IdempotencyKey: p502Key("p503-open", fixture.documentID),
		Fingerprint:    openFingerprint[:], OccurredAt: openedAt,
	}
	opened, err := repository.Transition(ctx, access, openCommand)
	if err != nil || opened.Status != DocumentOpen || opened.Version != 2 || opened.RevokeGeneration != 1 {
		t.Fatalf("open transition: document=%+v err=%v", opened, err)
	}

	suspendFingerprint := sha256.Sum256([]byte("p503-suspend"))
	suspended, err := repository.Transition(ctx, access, TransitionCommand{
		DocumentID: fixture.documentID, Operation: "suspend", ExpectedVersion: 2,
		IdempotencyKey: p502Key("p503-suspend", fixture.documentID),
		Fingerprint:    suspendFingerprint[:], OccurredAt: openedAt.Add(time.Second),
	})
	if err != nil || suspended.Status != DocumentSuspended || suspended.Version != 3 || suspended.RevokeGeneration != 2 {
		t.Fatalf("suspend transition: document=%+v err=%v", suspended, err)
	}

	replayedOpen, err := repository.Transition(ctx, access, openCommand)
	if err != nil || replayedOpen.Status != DocumentOpen || replayedOpen.Version != 2 || replayedOpen.RevokeGeneration != 1 {
		t.Fatalf("replay must return the original receipt projection: document=%+v err=%v", replayedOpen, err)
	}
	conflictingFingerprint := sha256.Sum256([]byte("p503-open-changed"))
	conflictingCommand := openCommand
	conflictingCommand.Fingerprint = conflictingFingerprint[:]
	if _, err := repository.Transition(ctx, access, conflictingCommand); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed idempotent command must conflict: %v", err)
	}
	staleFingerprint := sha256.Sum256([]byte("p503-stale-resume"))
	if _, err := repository.Transition(ctx, access, TransitionCommand{
		DocumentID: fixture.documentID, Operation: "resume", ExpectedVersion: 2,
		IdempotencyKey: p502Key("p503-stale", fixture.documentID),
		Fingerprint:    staleFingerprint[:], OccurredAt: openedAt.Add(2 * time.Second),
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale lifecycle CAS must conflict: %v", err)
	}

	watermark := sha256.Sum256([]byte("p503-watermark:" + fixture.documentID.String()))
	content := sha256.Sum256([]byte("p503-content:" + fixture.documentID.String()))
	if _, err := ownerPool.Exec(ctx, `INSERT INTO tutorhub.whiteboard_snapshots
		(id, tenant_id, document_id, generation, snapshot_kind, format_version,
		 schema_version, causal_watermark_sha256, content_sha256, size_bytes,
		 object_key, object_version_id, verification_key_id, provenance_kind, created_by)
		VALUES ($1, $2, $3, 1, 'manual', '1', 1, $4, $5, 1024,
		        $6, 'p503-b2-version', 'p503-verify-key', 'user', $7)`,
		fixture.snapshotID, fixture.tenantID, fixture.documentID, watermark[:], content[:],
		p502ObjectKey(content), fixture.actorID,
	); err != nil {
		t.Fatalf("insert P5-COLLAB-03 snapshot: %v", err)
	}
	snapshots, err := repository.ListSnapshots(ctx, access, fixture.documentID, 10)
	if err != nil || len(snapshots) != 1 || snapshots[0].ID != fixture.snapshotID {
		t.Fatalf("snapshot projection: snapshots=%+v err=%v", snapshots, err)
	}
	if snapshots[0].ContentSHA256 != strings.ToLower(hexSHA256(content)) ||
		snapshots[0].CausalWatermarkSHA256 != strings.ToLower(hexSHA256(watermark)) {
		t.Fatalf("snapshot hashes were not projected canonically: %+v", snapshots[0])
	}

	restoreFingerprint := sha256.Sum256([]byte("p503-restore"))
	restoreCommand := RestoreCommand{
		DocumentID: fixture.documentID, SnapshotID: fixture.snapshotID,
		ExpectedVersion: 3, ExpectedGeneration: 1,
		ProviderDocumentName: p502ProviderName("p503-restore", fixture.documentID),
		IdempotencyKey:       p502Key("p503-restore", fixture.documentID),
		Fingerprint:          restoreFingerprint[:], OccurredAt: openedAt.Add(3 * time.Second),
	}
	restored, err := repository.Restore(ctx, access, restoreCommand)
	if err != nil || restored.Version != 4 || restored.CurrentGeneration != 2 ||
		restored.RevokeGeneration != 3 || restored.Status != DocumentSuspended {
		t.Fatalf("restore generation swap: document=%+v err=%v", restored, err)
	}
	replayedRestore, err := repository.Restore(ctx, access, restoreCommand)
	if err != nil || replayedRestore.Version != 4 || replayedRestore.CurrentGeneration != 2 ||
		replayedRestore.RevokeGeneration != 3 {
		t.Fatalf("restore replay: document=%+v err=%v", replayedRestore, err)
	}

	foreignAccess := access
	foreignAccess.TenantID = uuid.New()
	if _, err := repository.Get(ctx, foreignAccess, fixture.documentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign tenant must be concealed as not found: %v", err)
	}
}

func assertP503RuntimeCreate(
	t *testing.T,
	ctx context.Context,
	ownerPool *pgxpool.Pool,
	repository *PostgresRepository,
	access AccessContext,
	fixture whiteboardPostgresFixture,
) {
	t.Helper()
	var classID uuid.UUID
	if err := ownerPool.QueryRow(ctx, `SELECT class_id
		FROM tutorhub.media_spaces
		WHERE tenant_id = $1 AND id = (
			SELECT media_space_id FROM tutorhub.whiteboard_documents
			WHERE tenant_id = $1 AND id = $2
		)`, fixture.tenantID, fixture.documentID).Scan(&classID); err != nil {
		t.Fatal("load P5-COLLAB-03 fixture class")
	}
	sessionID, mediaSpaceID, documentID := uuid.New(), uuid.New(), uuid.New()
	if _, err := ownerPool.Exec(ctx, `INSERT INTO tutorhub.class_sessions
		(id, tenant_id, class_id, title, starts_at, ends_at, timezone, created_by, updated_by)
		VALUES ($1, $2, $3, 'P5-COLLAB-03 create gate', now() + interval '3 hours',
		        now() + interval '4 hours', 'Asia/Ho_Chi_Minh', $4, $4)`,
		sessionID, fixture.tenantID, classID, fixture.actorID,
	); err != nil {
		t.Fatal("insert P5-COLLAB-03 create-gate class session")
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO tutorhub.media_spaces
		(id, tenant_id, source_kind, class_id, source_class_session_id,
		 create_idempotency_key, create_request_fingerprint, created_by, updated_by)
		VALUES ($1, $2, 'class_session', $3, $4, $5, $6, $7, $7)`,
		mediaSpaceID, fixture.tenantID, classID, sessionID,
		p502Key("p503-space", mediaSpaceID), make([]byte, 32), fixture.actorID,
	); err != nil {
		t.Fatal("insert P5-COLLAB-03 create-gate media space")
	}
	fingerprint := sha256.Sum256([]byte("p503-create"))
	result, err := repository.Create(ctx, access, CreateCommand{
		DocumentID: documentID, MediaSpaceID: mediaSpaceID,
		ProviderDocumentName: p502ProviderName("p503-create", documentID),
		IdempotencyKey:       p502Key("p503-create", documentID),
		Fingerprint:          fingerprint[:], OccurredAt: time.Now().UTC(),
	})
	if err != nil || !result.Created || result.Document.ID != documentID ||
		result.Document.Status != DocumentCreated || result.Document.Version != 1 ||
		result.Document.CreatedAt.IsZero() || result.Document.UpdatedAt.IsZero() {
		t.Fatalf("runtime create: result=%+v err=%v", result, err)
	}
	policies, err := repository.CapabilityPolicies(ctx, access, documentID)
	if err != nil || len(policies) != 5 || policies[AudienceHost] != CapabilityPresent {
		t.Fatalf("runtime create policies: policies=%+v err=%v", policies, err)
	}
}

func hexSHA256(value [32]byte) string {
	const digits = "0123456789abcdef"
	encoded := make([]byte, 64)
	for index, item := range value {
		encoded[index*2] = digits[item>>4]
		encoded[index*2+1] = digits[item&0x0f]
	}
	return string(encoded)
}
