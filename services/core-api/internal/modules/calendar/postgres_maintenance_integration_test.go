//go:build integration

package calendar

import (
	"context"
	"errors"
	"fmt"
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

func TestAvailabilityPollMaintenanceSecurityDefinerACLAndSkipLocked(t *testing.T) {
	maintenanceURL := strings.TrimSpace(os.Getenv("DATABASE_POLL_MAINTENANCE_URL"))
	if maintenanceURL == "" {
		t.Skip("DATABASE_POLL_MAINTENANCE_URL is not configured")
	}
	migrationURL := requireCalendarEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireCalendarEnvironment(t, "DATABASE_POOL_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply maintenance security migration: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read maintenance migration version: %v", err)
	}
	if version.Number < 23 || version.Dirty {
		t.Fatalf("unexpected maintenance migration version: %+v", version)
	}

	migrationPool := openCalendarPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	runtimePool := openCalendarPool(t, ctx, runtimeURL)
	defer runtimePool.Close()
	maintenancePool := openCalendarPool(t, ctx, maintenanceURL)
	defer maintenancePool.Close()

	assertMaintenanceFunctionMetadata(t, ctx, migrationPool, runtimePool, maintenancePool)
	assertMaintenanceEffectiveACL(t, ctx, runtimePool, maintenancePool)
	assertPermissionDenied(t, ctx, runtimePool,
		"SELECT tutorhub.purge_expired_availability_polls(1)")
	assertPermissionDenied(t, ctx, runtimePool,
		"DELETE FROM tutorhub.availability_polls WHERE false")
	assertPermissionDenied(t, ctx, maintenancePool,
		"SELECT count(*) FROM tutorhub.availability_polls")
	assertPermissionDenied(t, ctx, maintenancePool,
		"DELETE FROM tutorhub.availability_polls WHERE false")
	assertPermissionDenied(t, ctx, maintenancePool,
		"UPDATE tutorhub.study_meetings SET source_poll_id = source_poll_id WHERE false")
	assertPermissionDenied(t, ctx, maintenancePool,
		"DELETE FROM tutorhub.availability_poll_slots WHERE false")

	for _, expression := range []string{"NULL::integer", "0", "-1", "1001"} {
		assertPostgresCode(t, ctx, maintenancePool,
			"SELECT tutorhub.purge_expired_availability_polls("+expression+")", "22023")
	}

	fixture := seedMaintenanceFixture(t, ctx, migrationPool)
	defer cleanupMaintenanceFixture(t, migrationPool, fixture)

	lockTransaction, err := migrationPool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin SKIP LOCKED fixture transaction: %v", err)
	}
	defer func() { _ = lockTransaction.Rollback(context.Background()) }()
	if _, err := lockTransaction.Exec(ctx, `
SELECT 1
FROM tutorhub.availability_polls
WHERE tenant_id = $1 AND id = $2
FOR UPDATE`, fixture.tenantID, fixture.lockedPollID); err != nil {
		t.Fatalf("lock oldest maintenance fixture: %v", err)
	}

	if deleted := callMaintenancePurge(t, ctx, maintenancePool, 1); deleted != 1 {
		t.Fatalf("SKIP LOCKED purge deleted %d polls, want 1", deleted)
	}
	assertCascadeAndDetach(t, ctx, migrationPool, fixture)

	if err := lockTransaction.Rollback(ctx); err != nil {
		t.Fatalf("release SKIP LOCKED fixture: %v", err)
	}
	if deleted := callMaintenancePurge(t, ctx, maintenancePool, 1); deleted != 1 {
		t.Fatalf("released-row purge deleted %d polls, want 1", deleted)
	}

	var lockedExists, futureExists bool
	if err := migrationPool.QueryRow(ctx, `
SELECT
    EXISTS (
        SELECT 1 FROM tutorhub.availability_polls
        WHERE tenant_id = $1 AND id = $2
    ),
    EXISTS (
        SELECT 1 FROM tutorhub.availability_polls
        WHERE tenant_id = $1 AND id = $3
    )`, fixture.tenantID, fixture.lockedPollID, fixture.futurePollID).Scan(
		&lockedExists,
		&futureExists,
	); err != nil {
		t.Fatalf("inspect final maintenance fixture: %v", err)
	}
	if lockedExists || !futureExists {
		t.Fatalf("purge must delete the released expired poll and preserve the future poll")
	}
}

func assertMaintenanceFunctionMetadata(
	t *testing.T,
	ctx context.Context,
	migrationPool *pgxpool.Pool,
	runtimePool *pgxpool.Pool,
	maintenancePool *pgxpool.Pool,
) {
	t.Helper()
	var runtimeRole, maintenanceRole string
	if err := runtimePool.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatalf("read runtime role identity: %v", err)
	}
	if err := maintenancePool.QueryRow(ctx, "SELECT current_user").Scan(&maintenanceRole); err != nil {
		t.Fatalf("read maintenance role identity: %v", err)
	}

	var securityDefiner, exactSearchPath, ownerSafe, publicExecuteRevoked bool
	if err := migrationPool.QueryRow(ctx, `
SELECT
    function.prosecdef,
    function.proconfig = ARRAY['search_path=pg_catalog, pg_temp']::text[],
    NOT owner.rolsuper
        AND owner.rolname <> $1
        AND owner.rolname <> $2,
    NOT EXISTS (
        SELECT 1
        FROM aclexplode(COALESCE(
            function.proacl,
            acldefault('f'::"char", function.proowner)
        )) AS privilege
        WHERE privilege.grantee = 0
          AND privilege.privilege_type = 'EXECUTE'
    )
FROM pg_proc AS function
JOIN pg_roles AS owner ON owner.oid = function.proowner
WHERE function.oid =
    'tutorhub.purge_expired_availability_polls(integer)'::regprocedure`,
		runtimeRole,
		maintenanceRole,
	).Scan(&securityDefiner, &exactSearchPath, &ownerSafe, &publicExecuteRevoked); err != nil {
		t.Fatalf("inspect maintenance function metadata: %v", err)
	}
	if !securityDefiner || !exactSearchPath || !ownerSafe || !publicExecuteRevoked {
		t.Fatal("maintenance function metadata violates the reviewed security boundary")
	}
}

func assertMaintenanceEffectiveACL(
	t *testing.T,
	ctx context.Context,
	runtimePool *pgxpool.Pool,
	maintenancePool *pgxpool.Pool,
) {
	t.Helper()
	var runtimeExecute, runtimeDelete bool
	if err := runtimePool.QueryRow(ctx, `
SELECT
    has_function_privilege(
        current_user,
        'tutorhub.purge_expired_availability_polls(integer)',
        'EXECUTE'
    ),
    has_table_privilege(
        current_user,
        'tutorhub.availability_polls',
        'DELETE'
    )`).Scan(&runtimeExecute, &runtimeDelete); err != nil {
		t.Fatalf("inspect runtime maintenance privileges: %v", err)
	}
	if runtimeExecute || runtimeDelete {
		t.Fatal("Core API runtime must not execute purge or delete polls")
	}

	var schemaUsage, schemaCreate, functionExecute, noRelationOrColumnPrivileges bool
	if err := maintenancePool.QueryRow(ctx, `
SELECT
    has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
    has_schema_privilege(current_user, 'tutorhub', 'CREATE'),
    has_function_privilege(
        current_user,
        'tutorhub.purge_expired_availability_polls(integer)',
        'EXECUTE'
    ),
    (
        SELECT bool_and(
            NOT has_table_privilege(
                current_user,
                relation_name,
                'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER'
            )
            AND NOT has_any_column_privilege(
                current_user,
                relation_name,
                'SELECT,INSERT,UPDATE,REFERENCES'
            )
        )
        FROM unnest(ARRAY[
            'tutorhub.availability_polls',
            'tutorhub.availability_poll_slots',
            'tutorhub.availability_poll_participants',
            'tutorhub.availability_poll_capabilities',
            'tutorhub.availability_poll_responses',
            'tutorhub.availability_poll_answers',
            'tutorhub.availability_poll_mutation_receipts',
            'tutorhub.study_meetings'
        ]) AS relation(relation_name)
    )`).Scan(
		&schemaUsage,
		&schemaCreate,
		&functionExecute,
		&noRelationOrColumnPrivileges,
	); err != nil {
		t.Fatalf("inspect exact maintenance privileges: %v", err)
	}
	if !schemaUsage || schemaCreate || !functionExecute || !noRelationOrColumnPrivileges {
		t.Fatal("maintenance role must have schema USAGE and function EXECUTE only")
	}
}

type maintenanceFixture struct {
	tenantID      uuid.UUID
	ownerID       uuid.UUID
	lockedPollID  uuid.UUID
	cascadePollID uuid.UUID
	futurePollID  uuid.UUID
	slotID        uuid.UUID
	participantID uuid.UUID
	capabilityID  uuid.UUID
	responseID    uuid.UUID
	meetingID     uuid.UUID
}

func seedMaintenanceFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) maintenanceFixture {
	t.Helper()
	fixture := maintenanceFixture{
		tenantID:      uuid.New(),
		ownerID:       uuid.New(),
		lockedPollID:  uuid.New(),
		cascadePollID: uuid.New(),
		futurePollID:  uuid.New(),
		slotID:        uuid.New(),
		participantID: uuid.New(),
		capabilityID:  uuid.New(),
		responseID:    uuid.New(),
		meetingID:     uuid.New(),
	}
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin maintenance fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.users (id, email, display_name, locale, timezone)
VALUES ($1, $2, 'Maintenance fixture', 'vi', 'Asia/Ho_Chi_Minh')`,
		fixture.ownerID,
		fmt.Sprintf("poll-maintenance-%s@example.test", unique),
	); err != nil {
		t.Fatalf("insert maintenance fixture user: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, 'Poll maintenance fixture')`,
		fixture.tenantID,
		"poll-maintenance-"+unique,
	); err != nil {
		t.Fatalf("insert maintenance fixture tenant: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, 'teacher', 'active', now())`, fixture.tenantID, fixture.ownerID); err != nil {
		t.Fatalf("insert maintenance fixture membership: %v", err)
	}

	insertPoll := func(id uuid.UUID, label string, deadlineAt, retentionUntil time.Time) {
		t.Helper()
		if _, insertErr := transaction.Exec(ctx, `
INSERT INTO tutorhub.availability_polls (
    id, tenant_id, owner_user_id, title, timezone,
    range_start, range_end, working_day_start, working_day_end,
    duration_minutes, slot_granularity_minutes, deadline_at, share_mode,
    retention_until, create_idempotency_key, create_request_fingerprint
) VALUES (
    $1, $2, $3, $4, 'Asia/Ho_Chi_Minh',
    $5, $5, TIME '08:00', TIME '17:00',
    60, 30, $6, 'anyone_with_link',
    $7, $8, $9
)`,
			id,
			fixture.tenantID,
			fixture.ownerID,
			"Maintenance "+label,
			deadlineAt,
			deadlineAt,
			retentionUntil,
			"maintenance-"+label+"-"+unique,
			make([]byte, 32),
		); insertErr != nil {
			t.Fatalf("insert %s maintenance poll: %v", label, insertErr)
		}
	}
	historicalDeadline := time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
	insertPoll(
		fixture.lockedPollID,
		"locked",
		historicalDeadline,
		historicalDeadline.Add(150*24*time.Hour),
	)
	insertPoll(
		fixture.cascadePollID,
		"cascade",
		historicalDeadline,
		historicalDeadline.Add(151*24*time.Hour),
	)
	futureDeadline := time.Date(2099, time.January, 1, 0, 0, 0, 0, time.UTC)
	insertPoll(
		fixture.futurePollID,
		"future",
		futureDeadline,
		futureDeadline.Add(150*24*time.Hour),
	)

	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.availability_poll_slots (
    id, tenant_id, poll_id, starts_at, ends_at, ordinal
) VALUES ($1, $2, $3, '2099-01-02T01:00:00Z', '2099-01-02T02:00:00Z', 0)`,
		fixture.slotID, fixture.tenantID, fixture.cascadePollID,
	); err != nil {
		t.Fatalf("insert maintenance slot: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.availability_poll_participants (
    id, tenant_id, poll_id, kind, internal_user_id
) VALUES ($1, $2, $3, 'internal_user', $4)`,
		fixture.participantID, fixture.tenantID, fixture.cascadePollID, fixture.ownerID,
	); err != nil {
		t.Fatalf("insert maintenance participant: %v", err)
	}
	tokenDigest := make([]byte, 32)
	tokenDigest[0] = 23
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.availability_poll_capabilities (
    id, tenant_id, poll_id, participant_id, purpose, scope,
    token_version, token_digest, expires_at, created_by
) VALUES (
    $1, $2, $3, $4, 'poll_access', 'invited_participant',
    1, $5, now() + interval '1 day', $6
)`, fixture.capabilityID, fixture.tenantID, fixture.cascadePollID,
		fixture.participantID, tokenDigest, fixture.ownerID,
	); err != nil {
		t.Fatalf("insert maintenance capability: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.availability_poll_responses (
    id, tenant_id, poll_id, participant_id, internal_user_id,
    actor_type, submitted_at
) VALUES ($1, $2, $3, $4, $5, 'internal_member', now())`,
		fixture.responseID, fixture.tenantID, fixture.cascadePollID,
		fixture.participantID, fixture.ownerID,
	); err != nil {
		t.Fatalf("insert maintenance response: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.availability_poll_answers (
    tenant_id, poll_id, response_id, slot_id, state
) VALUES ($1, $2, $3, $4, 'available')`,
		fixture.tenantID, fixture.cascadePollID, fixture.responseID, fixture.slotID,
	); err != nil {
		t.Fatalf("insert maintenance answer: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.availability_poll_mutation_receipts (
    tenant_id, poll_id, operation, actor_key, idempotency_key,
    request_fingerprint, result_version
) VALUES ($1, $2, 'respond', $3, $4, $5, 1)`,
		fixture.tenantID,
		fixture.cascadePollID,
		"user:"+fixture.ownerID.String(),
		"maintenance-receipt-"+unique,
		make([]byte, 32),
	); err != nil {
		t.Fatalf("insert maintenance receipt: %v", err)
	}
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.study_meetings (
    id, tenant_id, owner_user_id, source_poll_id, title,
    starts_at, ends_at, timezone, create_idempotency_key,
    create_request_fingerprint
) VALUES (
    $1, $2, $3, $4, 'Detached maintenance outcome',
    '2099-01-02T01:00:00Z', '2099-01-02T02:00:00Z', 'Asia/Ho_Chi_Minh',
    $5, $6
)`, fixture.meetingID, fixture.tenantID, fixture.ownerID, fixture.cascadePollID,
		"maintenance-meeting-"+unique, make([]byte, 32),
	); err != nil {
		t.Fatalf("insert maintenance Study Meeting: %v", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit maintenance fixture: %v", err)
	}
	return fixture
}

func assertCascadeAndDetach(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture maintenanceFixture,
) {
	t.Helper()
	var pollExists, slotExists, participantExists, capabilityExists bool
	var responseExists, answerExists, receiptExists, meetingDetached, lockedExists bool
	if err := pool.QueryRow(ctx, `
SELECT
    EXISTS (SELECT 1 FROM tutorhub.availability_polls WHERE id = $1),
    EXISTS (SELECT 1 FROM tutorhub.availability_poll_slots WHERE id = $2),
    EXISTS (SELECT 1 FROM tutorhub.availability_poll_participants WHERE id = $3),
    EXISTS (SELECT 1 FROM tutorhub.availability_poll_capabilities WHERE id = $4),
    EXISTS (SELECT 1 FROM tutorhub.availability_poll_responses WHERE id = $5),
    EXISTS (
        SELECT 1 FROM tutorhub.availability_poll_answers
        WHERE response_id = $5
    ),
    EXISTS (
        SELECT 1 FROM tutorhub.availability_poll_mutation_receipts
        WHERE tenant_id = $6 AND poll_id = $1
    ),
    EXISTS (
        SELECT 1 FROM tutorhub.study_meetings
        WHERE id = $7 AND source_poll_id IS NULL
    ),
    EXISTS (SELECT 1 FROM tutorhub.availability_polls WHERE id = $8)`,
		fixture.cascadePollID,
		fixture.slotID,
		fixture.participantID,
		fixture.capabilityID,
		fixture.responseID,
		fixture.tenantID,
		fixture.meetingID,
		fixture.lockedPollID,
	).Scan(
		&pollExists,
		&slotExists,
		&participantExists,
		&capabilityExists,
		&responseExists,
		&answerExists,
		&receiptExists,
		&meetingDetached,
		&lockedExists,
	); err != nil {
		t.Fatalf("inspect purge cascade fixture: %v", err)
	}
	if pollExists || slotExists || participantExists || capabilityExists || responseExists ||
		answerExists || receiptExists || !meetingDetached || !lockedExists {
		t.Fatal("purge did not preserve the exact cascade, detach and SKIP LOCKED contract")
	}
}

func callMaintenancePurge(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	batchSize int,
) int {
	t.Helper()
	var deleted int
	if err := pool.QueryRow(ctx,
		"SELECT tutorhub.purge_expired_availability_polls($1)", batchSize,
	).Scan(&deleted); err != nil {
		t.Fatalf("call maintenance purge: %v", err)
	}
	return deleted
}

func assertPermissionDenied(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	statement string,
) {
	t.Helper()
	assertPostgresCode(t, ctx, pool, statement, "42501")
}

func assertPostgresCode(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	statement string,
	wantCode string,
) {
	t.Helper()
	_, err := pool.Exec(ctx, statement)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != wantCode {
		t.Fatalf("statement SQLSTATE=%v, want %s", postgresErrorCode(err), wantCode)
	}
}

func postgresErrorCode(err error) string {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		return postgresError.Code
	}
	if err == nil {
		return "success"
	}
	return "non-postgresql-error"
}

func cleanupMaintenanceFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture maintenanceFixture,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx,
		"DELETE FROM tutorhub.tenants WHERE id = $1", fixture.tenantID,
	); err != nil {
		t.Errorf("delete maintenance fixture tenant: %v", err)
		return
	}
	if _, err := pool.Exec(ctx,
		"DELETE FROM tutorhub.users WHERE id = $1", fixture.ownerID,
	); err != nil {
		t.Errorf("delete maintenance fixture user: %v", err)
	}
}
