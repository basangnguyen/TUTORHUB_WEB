//go:build integration

package media

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

func TestPostgresMediaSignalMaintenanceACLRetentionAndSkipLocked(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_06_DISPOSABLE_CONFIRM")) != p406DisposableConfirmation {
		t.Skip("P4_06_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406ProvisionDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply P4-06 media signal migration")
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatal("read P4-06 migration version")
	}
	if version.Number != 36 || version.Dirty {
		t.Fatal("P4-06 maintenance gate requires latest ledger 36 false")
	}

	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	maintenancePool := openMediaIntegrationPool(t, ctx, maintenanceURL)
	t.Cleanup(maintenancePool.Close)

	targets := make([]string, 0, len(p410MediaACLExpectations()))
	for _, expectation := range p410MediaACLExpectations() {
		assertExactMediaACL(t, ctx, runtimePool, expectation)
		targets = append(targets, strings.TrimPrefix(expectation.relation, "tutorhub."))
	}
	assertP406ProvisionedMaintenanceACL(
		t,
		ctx,
		migrationPool,
		runtimePool,
		maintenancePool,
		targets,
	)

	for _, statement := range []string{
		"DELETE FROM tutorhub.media_participant_hand_states WHERE false",
		"DELETE FROM tutorhub.media_reaction_events WHERE false",
		"DELETE FROM tutorhub.media_signal_mutation_receipts WHERE false",
		"SELECT tutorhub.purge_expired_media_reactions(1)",
		"SELECT tutorhub.purge_expired_media_signal_receipts(1)",
	} {
		assertP406PostgresCode(t, ctx, runtimePool, statement, "42501")
	}
	for _, statement := range []string{
		"SELECT count(*) FROM tutorhub.media_reaction_events",
		"SELECT count(*) FROM tutorhub.media_signal_mutation_receipts",
		"DELETE FROM tutorhub.media_reaction_events WHERE false",
		"DELETE FROM tutorhub.media_signal_mutation_receipts WHERE false",
	} {
		assertP406PostgresCode(t, ctx, maintenancePool, statement, "42501")
	}
	for _, function := range []string{
		"tutorhub.purge_expired_media_reactions",
		"tutorhub.purge_expired_media_signal_receipts",
	} {
		for _, argument := range []string{"NULL::integer", "0", "-1", "1001"} {
			assertP406PostgresCode(
				t,
				ctx,
				maintenancePool,
				"SELECT "+function+"("+argument+")",
				"22023",
			)
		}
	}

	fixture := seedMediaIntegrationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupMediaIntegrationFixture(t, migrationPool, fixture) })
	maintenanceFixture := seedP406SignalMaintenanceFixture(t, ctx, migrationPool, fixture)

	lockTransaction, err := migrationPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal("begin P4-06 SKIP LOCKED fixture transaction")
	}
	defer func() { _ = lockTransaction.Rollback(context.Background()) }()
	if _, err := lockTransaction.Exec(ctx, `SELECT 1
FROM tutorhub.media_reaction_events
WHERE id = $1
FOR UPDATE`, maintenanceFixture.lockedReactionID); err != nil {
		t.Fatal("lock oldest P4-06 reaction")
	}
	if _, err := lockTransaction.Exec(ctx, `SELECT 1
FROM tutorhub.media_signal_mutation_receipts
WHERE tenant_id = $1
  AND room_instance_id = $2
  AND actor_user_id = $3
  AND idempotency_key = $4
FOR UPDATE`, fixture.tenantID, maintenanceFixture.roomID, fixture.ownerID,
		maintenanceFixture.lockedReceiptKey); err != nil {
		t.Fatal("lock oldest P4-06 signal receipt")
	}

	if deleted := callP406Purge(t, ctx, maintenancePool, "purge_expired_media_reactions", 1); deleted != 1 {
		t.Fatalf("P4-06 reaction SKIP LOCKED purge deleted %d rows, want 1", deleted)
	}
	if deleted := callP406Purge(t, ctx, maintenancePool, "purge_expired_media_signal_receipts", 1); deleted != 1 {
		t.Fatalf("P4-06 receipt SKIP LOCKED purge deleted %d rows, want 1", deleted)
	}

	if err := lockTransaction.Rollback(ctx); err != nil {
		t.Fatal("release P4-06 SKIP LOCKED rows")
	}
	if deleted := callP406Purge(t, ctx, maintenancePool, "purge_expired_media_reactions", 1); deleted != 1 {
		t.Fatalf("P4-06 released reaction purge deleted %d rows, want 1", deleted)
	}
	if deleted := callP406Purge(t, ctx, maintenancePool, "purge_expired_media_signal_receipts", 1); deleted != 1 {
		t.Fatalf("P4-06 released receipt purge deleted %d rows, want 1", deleted)
	}

	var reactions, receipts int
	if err := migrationPool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_reaction_events
     WHERE tenant_id = $1 AND room_instance_id = $2),
    (SELECT count(*) FROM tutorhub.media_signal_mutation_receipts
     WHERE tenant_id = $1 AND room_instance_id = $2)`,
		fixture.tenantID,
		maintenanceFixture.roomID,
	).Scan(&reactions, &receipts); err != nil {
		t.Fatal("inspect final P4-06 maintenance fixture")
	}
	if reactions != 1 || receipts != 1 {
		t.Fatalf("P4-06 purge retained reaction/receipt rows = %d/%d, want 1/1", reactions, receipts)
	}
}

type p406SignalMaintenanceFixture struct {
	roomID           uuid.UUID
	lockedReactionID uuid.UUID
	lockedReceiptKey string
}

func seedP406SignalMaintenanceFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture mediaIntegrationFixture,
) p406SignalMaintenanceFixture {
	t.Helper()
	spaceID := uuid.New()
	roomID := uuid.New()
	participantID := uuid.New()
	lockedReactionID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	lockedReceiptKey := mediaIntegrationKey("p406-maintenance-locked")
	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin P4-06 maintenance fixture")
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()

	if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.media_spaces (
    id, tenant_id, source_kind, class_id, source_class_session_id,
    create_idempotency_key, create_request_fingerprint, created_by, updated_by
) VALUES ($1, $2, 'class_session', $3, $4, $5, $6, $7, $7)`,
		spaceID,
		fixture.tenantID,
		fixture.classID,
		fixture.sessionIDs["owner"],
		mediaIntegrationKey("p406-maintenance-space"),
		make([]byte, 32),
		fixture.ownerID,
	); err != nil {
		t.Fatal("insert P4-06 maintenance media space")
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.media_room_instances (
    id, tenant_id, space_id, attempt_number, provider_room_name, created_by, updated_by
) VALUES ($1, $2, $3, 1, $4, $5, $5)`,
		roomID,
		fixture.tenantID,
		spaceID,
		"p406_maintenance_"+strings.ReplaceAll(uuid.NewString(), "-", ""),
		fixture.ownerID,
	); err != nil {
		t.Fatal("insert P4-06 maintenance room")
	}
	if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.media_participant_sessions (
    id, tenant_id, space_id, room_instance_id, user_id, join_attempt_id,
    provider_participant_identity, roster_sequence
) VALUES ($1, $2, $3, $4, $5, $6, $7, 1)`,
		participantID,
		fixture.tenantID,
		spaceID,
		roomID,
		fixture.ownerID,
		uuid.New(),
		"p406_participant_"+strings.ReplaceAll(uuid.NewString(), "-", ""),
	); err != nil {
		t.Fatal("insert P4-06 maintenance participant")
	}

	reactions := []struct {
		id         uuid.UUID
		sequence   int64
		acceptedAt time.Time
	}{
		{lockedReactionID, 1, now.Add(-30 * time.Second)},
		{uuid.New(), 2, now.Add(-20 * time.Second)},
		{uuid.New(), 3, now.Add(10 * time.Minute)},
	}
	for _, reaction := range reactions {
		if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.media_reaction_events (
    id, tenant_id, space_id, room_instance_id, participant_session_id,
    reaction, signal_sequence, accepted_at, expires_at
) VALUES ($1, $2, $3, $4, $5, 'clap', $6, $7, $7::timestamptz + interval '10 seconds')`,
			reaction.id,
			fixture.tenantID,
			spaceID,
			roomID,
			participantID,
			reaction.sequence,
			reaction.acceptedAt,
		); err != nil {
			var postgresError *pgconn.PgError
			if errors.As(err, &postgresError) {
				t.Fatalf(
					"insert P4-06 maintenance reaction: code=%s constraint=%s",
					postgresError.Code,
					postgresError.ConstraintName,
				)
			}
			t.Fatal("insert P4-06 maintenance reaction")
		}
	}

	receipts := []struct {
		key       string
		sequence  int64
		createdAt time.Time
	}{
		{lockedReceiptKey, 1, now.Add(-26 * time.Hour)},
		{mediaIntegrationKey("p406-maintenance-expired"), 2, now.Add(-25 * time.Hour)},
		{mediaIntegrationKey("p406-maintenance-future"), 3, now},
	}
	for _, receipt := range receipts {
		if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.media_signal_mutation_receipts (
    tenant_id, space_id, room_instance_id, actor_user_id, idempotency_key,
    request_fingerprint, kind, result_projection_version, result_signal_sequence,
    created_at, retention_until
) VALUES ($1, $2, $3, $4, $5, $6, 'reaction', 1, $7, $8, $8::timestamptz + interval '24 hours')`,
			fixture.tenantID,
			spaceID,
			roomID,
			fixture.ownerID,
			receipt.key,
			make([]byte, 32),
			receipt.sequence,
			receipt.createdAt,
		); err != nil {
			t.Fatal("insert P4-06 maintenance signal receipt")
		}
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatal("commit P4-06 maintenance fixture")
	}
	return p406SignalMaintenanceFixture{
		roomID:           roomID,
		lockedReactionID: lockedReactionID,
		lockedReceiptKey: lockedReceiptKey,
	}
}

func callP406Purge(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	function string,
	batchSize int,
) int {
	t.Helper()
	var deleted int
	query := "SELECT tutorhub." + function + "($1)"
	if err := pool.QueryRow(ctx, query, batchSize).Scan(&deleted); err != nil {
		t.Fatal("call P4-06 maintenance purge")
	}
	return deleted
}

func assertP406PostgresCode(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	statement string,
	want string,
) {
	t.Helper()
	_, err := pool.Exec(ctx, statement)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != want {
		t.Fatalf("P4-06 PostgreSQL code = %v, want %s", err, want)
	}
}
