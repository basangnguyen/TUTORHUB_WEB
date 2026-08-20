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

const p410DisposableConfirmation = "I_UNDERSTAND_P4_10_DISPOSABLE_ONLY"

func TestPostgresMediaDiagnosticsForwardMigration(t *testing.T) {
	requireP410DisposableConfirmation(t)
	runP410ForwardMigration(t)
}

func runP410ForwardMigration(t *testing.T) {
	t.Helper()
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406ProvisionDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Dirty ||
		(version.Number != 35 && version.Number != 36 && version.Number != 37) {
		t.Fatal("P4-10 retained forward gate requires a clean disposable ledger from 35 through 37")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply P4-10 forward migration")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("rerun P4-10 forward migration idempotently")
	}
	version, err = migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Number != 37 || version.Dirty {
		t.Fatal("P4-10 retained forward gate must finish at latest ledger 37 false")
	}
}

func TestPostgresMediaDiagnosticsRetentionMetricsTenantAndSkipLockedPurge(t *testing.T) {
	requireP410DisposableConfirmation(t)
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406ProvisionDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Number != 37 || version.Dirty {
		t.Fatal("P4-10 retained diagnostics integration requires latest ledger 37 false")
	}
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	maintenancePool := openMediaIntegrationPool(t, ctx, maintenanceURL)
	t.Cleanup(maintenancePool.Close)
	assertP402SeparatedDatabaseRoles(t, ctx, migrationPool, runtimePool)

	base := seedMediaIntegrationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupMediaIntegrationFixture(t, migrationPool, base) })
	spaceID, roomID, participantID, joinAttemptID := seedP410DiagnosticParticipant(
		t, ctx, migrationPool, base,
	)
	lifecycle, _ := newMediaIntegrationServices(t, runtimePool)
	postgresLifecycle := lifecycle.repository.(*PostgresLifecycleRepository)
	repository, err := NewPostgresDiagnosticRepository(postgresLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	service, _ := NewDiagnosticService(repository, func() time.Time { return now })
	actor := mediaIntegrationAccess(base.tenantID, base.ownerID, "teacher")
	actor.SessionID = uuid.New()
	for index, event := range []RecordDiagnosticInput{
		{EventID: uuid.New(), RoomInstanceID: roomID, JoinAttemptID: joinAttemptID,
			Stage: DiagnosticStageJoinAttempt, Outcome: DiagnosticOutcomeSucceeded,
			NetworkQuality: DiagnosticNetworkGood, MediaPath: DiagnosticMediaAudioVideo, DurationMS: 120},
		{EventID: uuid.New(), RoomInstanceID: roomID, JoinAttemptID: joinAttemptID,
			Stage: DiagnosticStageMedia, Outcome: DiagnosticOutcomeSucceeded,
			NetworkQuality: DiagnosticNetworkGood, MediaPath: DiagnosticMediaAudioVideo, DurationMS: 2200},
		{EventID: uuid.New(), RoomInstanceID: roomID, JoinAttemptID: joinAttemptID,
			Stage: DiagnosticStageReconnected, Outcome: DiagnosticOutcomeSucceeded,
			NetworkQuality: DiagnosticNetworkDegraded, MediaPath: DiagnosticMediaAudioOnly, DurationMS: 800},
	} {
		if err := service.RecordDiagnostic(ctx, actor, spaceID, event); err != nil {
			t.Fatalf("record P4-10 diagnostic %d: %v", index, err)
		}
	}
	foreign := mediaIntegrationAccess(base.foreignTenantID, base.foreignOwnerID, "teacher")
	foreign.SessionID = uuid.New()
	foreignEvent := RecordDiagnosticInput{
		EventID: uuid.New(), RoomInstanceID: roomID, JoinAttemptID: joinAttemptID,
		Stage: DiagnosticStageMedia, Outcome: DiagnosticOutcomeSucceeded,
		NetworkQuality: DiagnosticNetworkGood, MediaPath: DiagnosticMediaAudioVideo,
	}
	if err := service.RecordDiagnostic(ctx, foreign, spaceID, foreignEvent); !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("foreign diagnostic was not concealed: %v", err)
	}

	admin := mediaIntegrationAccess(base.tenantID, base.adminID, "org_admin")
	admin.SessionID = uuid.New()
	export, err := service.ExportDiagnostics(ctx, admin, DiagnosticExportFilter{
		From: now.Add(-time.Hour), To: now.Add(time.Hour), Limit: 100,
	})
	if err != nil {
		t.Fatalf("export P4-10 diagnostics: %v", err)
	}
	if len(export.Items) != 3 || export.Metrics.JoinAttempts != 1 ||
		export.Metrics.SuccessfulJoins != 1 || export.Metrics.JoinSuccessRate != 1 ||
		export.Metrics.P95TimeToMediaMS != 2200 || export.Metrics.ReconnectSucceeded != 1 ||
		export.Items[0].SessionRef != diagnosticSessionRef(base.tenantID, participantID) {
		t.Fatalf("unexpected P4-10 metrics/export: %+v", export)
	}
	student := mediaIntegrationAccess(base.tenantID, base.studentID, "student")
	student.SessionID = uuid.New()
	if _, err := service.ExportDiagnostics(ctx, student, DiagnosticExportFilter{
		From: now.Add(-time.Hour), To: now.Add(time.Hour), Limit: 10,
	}); !errors.Is(err, ErrDiagnosticForbidden) {
		t.Fatalf("non-admin export error = %v, want forbidden", err)
	}

	for _, statement := range []string{
		"DELETE FROM tutorhub.media_join_diagnostics WHERE false",
		"SELECT tutorhub.purge_expired_media_join_diagnostics(1)",
	} {
		assertP410PostgresCode(t, ctx, runtimePool, statement, "42501")
	}
	assertP410PostgresCode(t, ctx, maintenancePool,
		"SELECT count(*) FROM tutorhub.media_join_diagnostics", "42501")

	expiredAt := now.Add(-31 * 24 * time.Hour)
	for _, eventID := range []uuid.UUID{uuid.New(), uuid.New()} {
		if _, err := migrationPool.Exec(ctx, `INSERT INTO tutorhub.media_join_diagnostics (
    id, tenant_id, space_id, room_instance_id, participant_session_id, join_attempt_id,
    stage, outcome, network_quality, media_path, duration_ms, recorded_at, retention_until
) VALUES ($1,$2,$3,$4,$5,$6,'media','failed','offline','listen_only',0,$7,$7::timestamptz + interval '30 days')`,
			eventID, base.tenantID, spaceID, roomID, participantID, joinAttemptID, expiredAt); err != nil {
			t.Fatal("insert expired P4-10 diagnostic")
		}
	}
	lockTx, err := migrationPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lockTx.Rollback(context.Background()) }()
	if _, err := lockTx.Exec(ctx, `SELECT 1 FROM tutorhub.media_join_diagnostics
WHERE retention_until <= clock_timestamp() ORDER BY retention_until, id LIMIT 1 FOR UPDATE`); err != nil {
		t.Fatal("lock expired P4-10 diagnostic")
	}
	if deleted := callP410Purge(t, ctx, maintenancePool, 1); deleted != 1 {
		t.Fatalf("P4-10 SKIP LOCKED purge deleted %d rows, want 1", deleted)
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatal("release P4-10 diagnostic lock")
	}
	if deleted := callP410Purge(t, ctx, maintenancePool, 1); deleted != 1 {
		t.Fatalf("P4-10 released purge deleted %d rows, want 1", deleted)
	}
}

func requireP410DisposableConfirmation(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_10_DISPOSABLE_CONFIRM")) != p410DisposableConfirmation {
		t.Skip("P4_10_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
}

func seedP410DiagnosticParticipant(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	base mediaIntegrationFixture,
) (uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	spaceID, roomID, participantID, joinAttemptID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_spaces (
    id, tenant_id, source_kind, class_id, source_class_session_id,
    create_idempotency_key, create_request_fingerprint, created_by, updated_by
) VALUES ($1,$2,'class_session',$3,$4,$5,$6,$7,$7)`,
		spaceID, base.tenantID, base.classID, base.sessionIDs["owner"],
		mediaIntegrationKey("p410-space"), make([]byte, 32), base.ownerID); err != nil {
		t.Fatal("insert P4-10 media space")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_room_instances (
    id, tenant_id, space_id, attempt_number, provider_room_name, created_by, updated_by
) VALUES ($1,$2,$3,1,$4,$5,$5)`, roomID, base.tenantID, spaceID,
		"p410_"+strings.ReplaceAll(uuid.NewString(), "-", ""), base.ownerID); err != nil {
		t.Fatal("insert P4-10 room instance")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_participant_sessions (
    id, tenant_id, space_id, room_instance_id, user_id, join_attempt_id,
    provider_participant_identity, participant_key, roster_sequence
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1)`, participantID, base.tenantID, spaceID,
		roomID, base.ownerID, joinAttemptID,
		"p410_"+strings.ReplaceAll(uuid.NewString(), "-", ""), uuid.New()); err != nil {
		t.Fatal("insert P4-10 participant session")
	}
	return spaceID, roomID, participantID, joinAttemptID
}

func callP410Purge(t *testing.T, ctx context.Context, pool *pgxpool.Pool, batch int) int {
	t.Helper()
	var deleted int
	if err := pool.QueryRow(ctx,
		"SELECT tutorhub.purge_expired_media_join_diagnostics($1)", batch,
	).Scan(&deleted); err != nil {
		t.Fatal("call P4-10 diagnostics purge")
	}
	return deleted
}

func assertP410PostgresCode(
	t *testing.T, ctx context.Context, pool *pgxpool.Pool, statement string, want string,
) {
	t.Helper()
	_, err := pool.Exec(ctx, statement)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != want {
		t.Fatalf("P4-10 PostgreSQL code = %v, want %s", err, want)
	}
}
