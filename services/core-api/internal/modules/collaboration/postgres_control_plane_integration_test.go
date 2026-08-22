//go:build integration

package collaboration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

const p5Collab02DisposableConfirmation = "I_UNDERSTAND_P5_COLLAB_02_DISPOSABLE_ONLY"

type whiteboardPostgresFixture struct {
	tenantID   uuid.UUID
	actorID    uuid.UUID
	documentID uuid.UUID
	snapshotID uuid.UUID
}

func TestWhiteboardControlPlanePostgresGates(t *testing.T) {
	requireP5Collab02Disposable(t)
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	if databaseURL == "" {
		t.Skip("DATABASE_MIGRATION_URL is not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := migrationrunner.Up(ctx, databaseURL); err != nil {
		t.Fatalf("apply P5-COLLAB-02 migration: %v", err)
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal("open P5-COLLAB-02 migration pool")
	}
	defer pool.Close()

	var version int
	var dirty bool
	if err := pool.QueryRow(ctx,
		`SELECT version, dirty FROM public.tutorhub_schema_migrations`,
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P5-COLLAB-02 migration ledger")
	}
	if version != 40 || dirty {
		t.Fatal("whiteboard PostgreSQL gates require latest ledger 40 false")
	}

	fixture := seedWhiteboardPostgresFixture(t, ctx, pool)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM tutorhub.tenants WHERE id = $1`, fixture.tenantID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM tutorhub.users WHERE id = $1`, fixture.actorID)
	})

	assertWhiteboardLifecycleCASAndIdempotency(t, ctx, pool, fixture)
	assertWhiteboardRestoreGenerationSwap(t, ctx, pool, fixture)
	assertWhiteboardTenantBoundary(t, ctx, pool, fixture)
	assertWhiteboardDatabaseIsNotDocumentHistoryAuthority(t, ctx, pool)
}

func seedWhiteboardPostgresFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) whiteboardPostgresFixture {
	t.Helper()
	fixture := whiteboardPostgresFixture{
		tenantID:   uuid.New(),
		actorID:    uuid.New(),
		documentID: uuid.New(),
		snapshotID: uuid.New(),
	}
	classID, sessionID, spaceID := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin P5-COLLAB-02 fixture")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	statements := []struct {
		name string
		sql  string
		args []any
	}{
		{
			"user",
			`INSERT INTO tutorhub.users (id, email, display_name)
             VALUES ($1, $2, 'P5-COLLAB-02 actor')`,
			[]any{fixture.actorID, "p502-" + strings.ReplaceAll(fixture.actorID.String(), "-", "") + "@example.test"},
		},
		{
			"tenant",
			`INSERT INTO tutorhub.tenants (id, slug, name)
             VALUES ($1, $2, 'P5-COLLAB-02 disposable')`,
			[]any{fixture.tenantID, "p502-" + strings.ReplaceAll(fixture.tenantID.String(), "-", "")[:24]},
		},
		{
			"membership",
			`INSERT INTO tutorhub.memberships
                 (tenant_id, user_id, role, status, joined_at)
             VALUES ($1, $2, 'teacher', 'active', $3)`,
			[]any{fixture.tenantID, fixture.actorID, now},
		},
		{
			"class",
			`INSERT INTO tutorhub.classes
				 (id, tenant_id, owner_user_id, code, title, status, timezone)
			 VALUES ($1, $2, $3, $4, 'P5-COLLAB-02 class', 'active', 'Asia/Ho_Chi_Minh')`,
			[]any{classID, fixture.tenantID, fixture.actorID, "P502" + strings.ToUpper(strings.ReplaceAll(classID.String(), "-", "")[:8])},
		},
		{
			"class session",
			`INSERT INTO tutorhub.class_sessions
                 (id, tenant_id, class_id, title, starts_at, ends_at, timezone,
                  created_by, updated_by)
             VALUES ($1, $2, $3, 'P5-COLLAB-02 session', $4, $5,
                     'Asia/Ho_Chi_Minh', $6, $6)`,
			[]any{sessionID, fixture.tenantID, classID, now.Add(time.Hour), now.Add(2 * time.Hour), fixture.actorID},
		},
		{
			"media space",
			`INSERT INTO tutorhub.media_spaces
                 (id, tenant_id, source_kind, class_id, source_class_session_id,
                  create_idempotency_key, create_request_fingerprint, created_by, updated_by)
             VALUES ($1, $2, 'class_session', $3, $4, $5, $6, $7, $7)`,
			[]any{spaceID, fixture.tenantID, classID, sessionID, p502Key("space", spaceID), make([]byte, 32), fixture.actorID},
		},
		{
			"whiteboard document",
			`INSERT INTO tutorhub.whiteboard_documents
                 (id, tenant_id, media_space_id, create_idempotency_key,
                  create_request_fingerprint, created_by, updated_by)
             VALUES ($1, $2, $3, $4, $5, $6, $6)`,
			[]any{fixture.documentID, fixture.tenantID, spaceID, p502Key("document", fixture.documentID), make([]byte, 32), fixture.actorID},
		},
		{
			"initial generation",
			`INSERT INTO tutorhub.whiteboard_document_generations
                 (tenant_id, document_id, generation, provider_document_name,
                  reason, created_by)
             VALUES ($1, $2, 1, $3, 'initial', $4)`,
			[]any{fixture.tenantID, fixture.documentID, p502ProviderName("initial", fixture.documentID), fixture.actorID},
		},
		{
			"host capability",
			`INSERT INTO tutorhub.whiteboard_capability_policies
                 (tenant_id, document_id, audience, capability, created_by, updated_by)
             VALUES ($1, $2, 'host', 'present', $3, $3)`,
			[]any{fixture.tenantID, fixture.documentID, fixture.actorID},
		},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.sql, statement.args...); err != nil {
			t.Fatalf("insert P5-COLLAB-02 %s: %v", statement.name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit P5-COLLAB-02 fixture: %v", err)
	}
	return fixture
}

func assertWhiteboardLifecycleCASAndIdempotency(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture whiteboardPostgresFixture,
) {
	t.Helper()
	keys := []string{p502Key("open-a", uuid.New()), p502Key("open-b", uuid.New())}
	fingerprints := [][32]byte{sha256.Sum256([]byte(keys[0])), sha256.Sum256([]byte(keys[1]))}

	type result struct {
		key string
		won bool
		err error
	}
	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(2)
	for index := range keys {
		go func(index int) {
			start.Done()
			start.Wait()
			won, err := attemptWhiteboardOpen(
				ctx, pool, fixture, keys[index], fingerprints[index][:],
			)
			results <- result{key: keys[index], won: won, err: err}
		}(index)
	}
	winner := ""
	for range 2 {
		outcome := <-results
		if outcome.err != nil {
			t.Fatalf("concurrent whiteboard lifecycle CAS: %v", outcome.err)
		}
		if outcome.won {
			if winner != "" {
				t.Fatal("two lifecycle CAS writers won the same expected version")
			}
			winner = outcome.key
		}
	}
	if winner == "" {
		t.Fatal("no lifecycle CAS writer won")
	}

	var status string
	var version int64
	if err := pool.QueryRow(ctx, `SELECT status, version
        FROM tutorhub.whiteboard_documents
        WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, fixture.documentID,
	).Scan(&status, &version); err != nil {
		t.Fatal("read lifecycle CAS result")
	}
	if status != "open" || version != 2 {
		t.Fatalf("lifecycle CAS result = %s/%d, want open/2", status, version)
	}

	var receiptCount int
	if err := pool.QueryRow(ctx, `SELECT count(*)
        FROM tutorhub.whiteboard_document_mutation_receipts
        WHERE tenant_id = $1 AND document_id = $2 AND operation = 'open'`,
		fixture.tenantID, fixture.documentID,
	).Scan(&receiptCount); err != nil {
		t.Fatal("count lifecycle CAS receipts")
	}
	if receiptCount != 1 {
		t.Fatalf("lifecycle CAS receipt count = %d, want 1", receiptCount)
	}

	var replayVersion int64
	if err := pool.QueryRow(ctx, `SELECT result_document_version
        FROM tutorhub.whiteboard_document_mutation_receipts
        WHERE tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3`,
		fixture.tenantID, fixture.actorID, winner,
	).Scan(&replayVersion); err != nil {
		t.Fatal("read lifecycle idempotency receipt")
	}
	if replayVersion != 2 {
		t.Fatalf("lifecycle idempotency replay version = %d, want 2", replayVersion)
	}
}

func attemptWhiteboardOpen(
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture whiteboardPostgresFixture,
	key string,
	fingerprint []byte,
) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var version int64
	err = tx.QueryRow(ctx, `UPDATE tutorhub.whiteboard_documents
        SET status = 'open', version = version + 1, updated_by = $3,
            opened_at = now(), opened_by = $3, updated_at = now()
        WHERE tenant_id = $1 AND id = $2 AND status = 'created' AND version = 1
        RETURNING version`, fixture.tenantID, fixture.documentID, fixture.actorID,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO tutorhub.whiteboard_document_mutation_receipts
        (tenant_id, actor_user_id, idempotency_key, request_fingerprint,
         operation, document_id, result_document_version, result_generation,
         result_revoke_generation, result_status)
        VALUES ($1, $2, $3, $4, 'open', $5, $6, 1, 1, 'open')`,
		fixture.tenantID, fixture.actorID, key, fingerprint, fixture.documentID, version,
	)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func assertWhiteboardRestoreGenerationSwap(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture whiteboardPostgresFixture,
) {
	t.Helper()
	watermark, content := sha256.Sum256([]byte("watermark")), sha256.Sum256([]byte("content"))
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.whiteboard_snapshots
        (id, tenant_id, document_id, generation, snapshot_kind, format_version,
         schema_version, causal_watermark_sha256, content_sha256, size_bytes,
         object_key, object_version_id, verification_key_id, provenance_kind, created_by)
        VALUES ($1, $2, $3, 1, 'manual', '1', 1, $4, $5, 1024,
                $6, 'b2-file-v1', 'verify-key-v1', 'user', $7)`,
		fixture.snapshotID, fixture.tenantID, fixture.documentID, watermark[:], content[:],
		p502ObjectKey(content), fixture.actorID,
	); err != nil {
		t.Fatalf("insert restore snapshot: %v", err)
	}

	type outcome struct {
		won bool
		err error
	}
	results := make(chan outcome, 2)
	var start sync.WaitGroup
	start.Add(2)
	for range 2 {
		go func() {
			start.Done()
			start.Wait()
			won, err := attemptWhiteboardRestore(ctx, pool, fixture)
			results <- outcome{won: won, err: err}
		}()
	}
	winners := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent restore generation swap: %v", result.err)
		}
		if result.won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("restore generation swap winners = %d, want 1", winners)
	}

	var current, revoke, version int64
	if err := pool.QueryRow(ctx, `SELECT current_generation, revoke_generation, version
        FROM tutorhub.whiteboard_documents
        WHERE tenant_id = $1 AND id = $2`, fixture.tenantID, fixture.documentID,
	).Scan(&current, &revoke, &version); err != nil {
		t.Fatal("read restore generation result")
	}
	if current != 2 || revoke != 2 || version != 3 {
		t.Fatalf("restore result = generation %d revoke %d version %d, want 2/2/3", current, revoke, version)
	}
	var generations, restoreReceipts int
	if err := pool.QueryRow(ctx, `SELECT
        (SELECT count(*) FROM tutorhub.whiteboard_document_generations
         WHERE tenant_id = $1 AND document_id = $2),
        (SELECT count(*) FROM tutorhub.whiteboard_document_mutation_receipts
         WHERE tenant_id = $1 AND document_id = $2 AND operation = 'restore')`,
		fixture.tenantID, fixture.documentID,
	).Scan(&generations, &restoreReceipts); err != nil {
		t.Fatal("count restore generations and receipts")
	}
	if generations != 2 || restoreReceipts != 1 {
		t.Fatalf("restore catalogs = generations %d receipts %d, want 2/1", generations, restoreReceipts)
	}
}

func attemptWhiteboardRestore(
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture whiteboardPostgresFixture,
) (bool, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var version, generation, revoke int64
	if err := tx.QueryRow(ctx, `SELECT version, current_generation, revoke_generation
        FROM tutorhub.whiteboard_documents
        WHERE tenant_id = $1 AND id = $2
        FOR UPDATE`, fixture.tenantID, fixture.documentID,
	).Scan(&version, &generation, &revoke); err != nil {
		return false, err
	}
	if version != 2 || generation != 1 || revoke != 1 {
		return false, nil
	}
	next := generation + 1
	key := p502Key("restore", uuid.New())
	fingerprint := sha256.Sum256([]byte(key))
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.whiteboard_document_generations
        (tenant_id, document_id, generation, provider_document_name, reason,
         restored_from_snapshot_id, created_by)
        VALUES ($1, $2, $3, $4, 'restore', $5, $6)`,
		fixture.tenantID, fixture.documentID, next,
		p502ProviderName("restore", uuid.New()), fixture.snapshotID, fixture.actorID,
	); err != nil {
		return false, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	command, err := tx.Exec(ctx, `UPDATE tutorhub.whiteboard_documents
        SET current_generation = $3, revoke_generation = revoke_generation + 1,
            version = version + 1, updated_by = $4, updated_at = $5
        WHERE tenant_id = $1 AND id = $2 AND version = 2 AND current_generation = 1`,
		fixture.tenantID, fixture.documentID, next, fixture.actorID, now,
	)
	if err != nil {
		return false, err
	}
	if command.RowsAffected() != 1 {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.whiteboard_document_mutation_receipts
        (tenant_id, actor_user_id, idempotency_key, request_fingerprint,
         operation, document_id, result_document_version, result_generation,
         result_revoke_generation, result_status)
        VALUES ($1, $2, $3, $4, 'restore', $5, 3, $6, 2, 'open')`,
		fixture.tenantID, fixture.actorID, key, fingerprint[:], fixture.documentID, next,
	); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func assertWhiteboardTenantBoundary(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture whiteboardPostgresFixture,
) {
	t.Helper()
	foreignTenant := uuid.New()
	var visible bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM tutorhub.whiteboard_documents
        WHERE tenant_id = $1 AND id = $2
    )`, foreignTenant, fixture.documentID).Scan(&visible); err != nil {
		t.Fatal("run whiteboard foreign-tenant predicate")
	}
	if visible {
		t.Fatal("foreign tenant could select a whiteboard document through the exact predicate")
	}

	_, err := pool.Exec(ctx, `INSERT INTO tutorhub.whiteboard_document_mutation_receipts
        (tenant_id, actor_user_id, idempotency_key, request_fingerprint,
         operation, document_id, result_document_version, result_generation,
         result_revoke_generation, result_status)
        VALUES ($1, $2, $3, $4, 'close', $5, 4, 2, 3, 'closed')`,
		foreignTenant, fixture.actorID, p502Key("foreign", uuid.New()), make([]byte, 32), fixture.documentID,
	)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "23503" {
		t.Fatalf("cross-tenant whiteboard receipt error = %v, want foreign-key denial", err)
	}
}

func assertWhiteboardDatabaseIsNotDocumentHistoryAuthority(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	var forbiddenTables, forbiddenColumns int
	if err := pool.QueryRow(ctx, `SELECT
        (SELECT count(*)
         FROM information_schema.tables
         WHERE table_schema = 'tutorhub'
           AND table_name ~ '^whiteboard_.*(operation|history|awareness|undo|element)'),
        (SELECT count(*)
         FROM information_schema.columns
         WHERE table_schema = 'tutorhub'
           AND table_name LIKE 'whiteboard_%'
           AND (
               data_type IN ('json', 'jsonb')
               OR column_name ~ '(payload|update_bytes|document_state|undo|awareness)'
           ))`,
	).Scan(&forbiddenTables, &forbiddenColumns); err != nil {
		t.Fatal("inspect whiteboard authority boundary")
	}
	if forbiddenTables != 0 || forbiddenColumns != 0 {
		t.Fatalf("PostgreSQL crossed whiteboard authority boundary: tables=%d columns=%d", forbiddenTables, forbiddenColumns)
	}
}

func requireP5Collab02Disposable(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P5_COLLAB_02_DISPOSABLE_CONFIRM")) != p5Collab02DisposableConfirmation {
		t.Skip("P5_COLLAB_02_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
}

func p502Key(prefix string, id uuid.UUID) string {
	return fmt.Sprintf("p502-%s-%s", prefix, strings.ReplaceAll(id.String(), "-", ""))
}

func p502ProviderName(prefix string, id uuid.UUID) string {
	return fmt.Sprintf("wb_%s_%s", prefix, strings.ReplaceAll(id.String(), "-", ""))
}

func p502ObjectKey(content [32]byte) string {
	hexValue := fmt.Sprintf("%x", content)
	return "wb/" + hexValue[:2] + "/" + hexValue
}
