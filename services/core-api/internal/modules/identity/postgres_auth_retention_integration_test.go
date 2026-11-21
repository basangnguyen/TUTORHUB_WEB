//go:build integration

package identity

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

func TestAuthRetentionPostgresConcurrentBatches(t *testing.T) {
	ctx, pool, _ := authSchemaTestDatabase(t)
	rows := []uuid.UUID{uuid.New(), uuid.New()}
	for i, id := range rows {
		hash := sha256.Sum256(id[:])
		_, err := pool.Exec(ctx, `INSERT INTO tutorhub.auth_transactions(id,binding_hash,kind,created_at,expires_at)
			VALUES ($1,$2,'login',timestamptz '1900-01-01 00:00:00+00' + $3*interval '1 day',
			timestamptz '1900-01-01 00:05:00+00' + $3*interval '1 day')`, id, hash[:], i)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, "DELETE FROM tutorhub.auth_transactions WHERE id=$1", id); err != nil {
				t.Error(err)
			}
		})
	}
	// Leave the first purge uncommitted while a second connection scans the
	// same batch. Both see the pre-delete rows, but lock disjoint candidates.
	for i := range rows {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		retentionExec(t, ctx, tx, "SET LOCAL lock_timeout='2s'")
		var removed int
		err = tx.QueryRow(ctx, "SELECT (tutorhub.purge_expired_auth_artifacts(1)->>'auth_transactions')::int").Scan(&removed)
		if err != nil || removed != 1 {
			t.Fatal("overlapping batch failed", i, err)
		}
		var remaining uuid.UUID
		err = tx.QueryRow(ctx, "SELECT id FROM tutorhub.auth_transactions WHERE id=ANY($1::uuid[])", rows).Scan(&remaining)
		if err != nil || remaining != rows[1-i] {
			t.Fatal("batch did not skip the other worker's lock", i, err)
		}
	}
}

func TestAuthRetentionPostgresMigrationRoundTrip(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	requireAuthSchemaVersion(t, ctx, url, latestAuthTestVersion)
	migrateAuthSchemaDownTo(t, ctx, url, 50)
	// Model an existing grant and a future-function default just before 51.
	name := "auth_retention_default_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	role := pgx.Identifier{name}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+role); err != nil {
		t.Fatal("create disposable default-grant role", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, "ALTER DEFAULT PRIVILEGES IN SCHEMA tutorhub REVOKE EXECUTE ON FUNCTIONS FROM "+role+
			"; DROP OWNED BY "+role+"; DROP ROLE "+role); err != nil {
			t.Error("remove disposable default-grant role", err)
		}
	})
	if _, err := pool.Exec(ctx, "ALTER DEFAULT PRIVILEGES IN SCHEMA tutorhub GRANT EXECUTE ON FUNCTIONS TO "+role+
		"; GRANT EXECUTE ON FUNCTION tutorhub.purge_expired_deletion_receipts() TO "+role); err != nil {
		t.Fatal("seed default and legacy function grants", err)
	}
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	requireAuthSchemaVersion(t, ctx, url, latestAuthTestVersion)
	var canExecute bool
	if err := pool.QueryRow(ctx, `SELECT has_function_privilege($1,'tutorhub.require_delivery_challenge()','EXECUTE')
		OR has_function_privilege($1,'tutorhub.purge_expired_auth_artifacts(integer)','EXECUTE')
		OR has_function_privilege($1,'tutorhub.purge_expired_deletion_receipts()','EXECUTE')`, name).Scan(&canExecute); err != nil || canExecute {
		t.Fatal("retention routines retained a default or legacy grant", err)
	}
}

func retentionExec(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}

func requireRetentionSQLState(t *testing.T, ctx context.Context, tx pgx.Tx, query, state string) {
	t.Helper()
	sub, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(sub)
	_, err = sub.Exec(ctx, query)
	var dbErr *pgconn.PgError
	if !errors.As(err, &dbErr) || dbErr.Code != state {
		t.Fatalf("want SQLSTATE %s, got %v", state, err)
	}
}

func TestAuthRetentionPostgresRolesAndWindows(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	ctx := f.ctx
	runtime := createPasskeyRuntimeRole(t, f).(passkeyRuntimeDB)
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	maintenanceName := "auth_retention_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	maintenance := pgx.Identifier{maintenanceName}.Sanitize()
	retentionExec(t, ctx, tx, "CREATE ROLE "+maintenance+" NOLOGIN NOINHERIT")
	retentionExec(t, ctx, tx, "GRANT "+runtime.quotedRole+" TO "+maintenance)
	if err := ProvisionAuthMaintenanceACL(ctx, tx, maintenanceName); err == nil {
		t.Fatal("maintenance role with an existing membership passed preflight")
	}
	retentionExec(t, ctx, tx, "REVOKE "+runtime.quotedRole+" FROM "+maintenance)
	retentionExec(t, ctx, tx, "GRANT "+maintenance+" TO CURRENT_USER")
	retentionExec(t, ctx, tx, "GRANT ALL ON tutorhub.auth_transactions TO "+maintenance)
	retentionExec(t, ctx, tx, "GRANT UPDATE (email) ON tutorhub.users TO "+maintenance)
	retentionExec(t, ctx, tx, "GRANT EXECUTE ON FUNCTION "+eraseProviderProcedure+" TO "+maintenance)
	if err := ProvisionAuthMaintenanceACL(ctx, tx, maintenanceName); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionAuthMaintenanceACL(ctx, tx, maintenanceName); err != nil {
		t.Fatal("repeat maintenance provisioning", err)
	}
	retentionExec(t, ctx, tx, authRetentionSeed)
	const insertDelivery = `INSERT INTO tutorhub.auth_challenge_deliveries(challenge_id,channel,provider,status,provider_message_ref,error_code,created_at,delivered_at)
		VALUES(%s,'sms','twilio','accepted','fixture_ref','fixture_code',now()-interval '2 days',now()-interval '2 days')`
	retentionExec(t, ctx, tx, "SET LOCAL ROLE "+runtime.quotedRole)
	requireRetentionSQLState(t, ctx, tx, "SELECT tutorhub.purge_expired_auth_artifacts(500)", "42501")
	requireRetentionSQLState(t, ctx, tx, strings.Replace(insertDelivery, "%s", "NULL", 1), "23502")
	retentionExec(t, ctx, tx, strings.Replace(insertDelivery, "%s", "(SELECT id FROM tutorhub.auth_challenges WHERE code_hash=decode(repeat('02',32),'hex'))", 1))
	retentionExec(t, ctx, tx, "SET LOCAL ROLE "+maintenance)
	requireRetentionSQLState(t, ctx, tx, "SELECT * FROM tutorhub.auth_transactions", "42501")
	requireRetentionSQLState(t, ctx, tx, "DELETE FROM tutorhub.auth_transactions WHERE false", "42501")
	requireRetentionSQLState(t, ctx, tx, "SELECT tutorhub.purge_expired_auth_artifacts(NULL)", "22023")
	requireRetentionSQLState(t, ctx, tx, "SELECT tutorhub.purge_expired_auth_artifacts(0)", "22023")
	requireRetentionSQLState(t, ctx, tx, "SELECT tutorhub.purge_expired_auth_artifacts(5001)", "22023")
	groups := []string{"auth_transactions", "auth_challenge_deliveries", "user_phone_numbers", "mfa_recovery_codes", "auth_flows", "external_data_deletion_requests"}
	checkCounts := func(batch int, want ...int) {
		t.Helper()
		var actual map[string]int
		if err := tx.QueryRow(ctx, "SELECT tutorhub.purge_expired_auth_artifacts($1)", batch).Scan(&actual); err != nil {
			t.Fatal(err)
		}
		expected := make(map[string]int)
		for i, group := range groups {
			expected[group] = want[i]
		}
		if !reflect.DeepEqual(actual, expected) {
			t.Fatalf("want %v, received %v", expected, actual)
		}
	}
	checkCounts(1, 1, 1, 1, 1, 1, 1)
	checkCounts(500, 0, 0, 1, 1, 0, 0)
	checkCounts(500, 0, 0, 0, 0, 0, 0)
	retentionExec(t, ctx, tx, "RESET ROLE")
	var evidenceOK bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM tutorhub.auth_challenge_deliveries WHERE challenge_id IS NULL
			AND channel='sms' AND provider='twilio' AND status='accepted' AND provider_message_ref='fixture_ref'
			AND error_code='fixture_code' AND created_at=now()-interval '2 days' AND delivered_at=created_at)
		AND NOT EXISTS(SELECT FROM tutorhub.auth_challenges WHERE code_hash=decode(repeat('02',32),'hex'))
		AND EXISTS(SELECT FROM tutorhub.auth_transactions WHERE expires_at=now()-interval '23 hours')
		AND EXISTS(SELECT FROM tutorhub.auth_flows WHERE expires_at=now()-interval '23 hours')
		AND EXISTS(SELECT FROM tutorhub.external_data_deletion_requests WHERE created_at=now()-interval '364 days')
		AND (SELECT count(*)=3 FROM tutorhub.user_phone_numbers p JOIN tutorhub.users u ON u.id=p.user_id
		     WHERE u.email='auth-retention@example.test' AND (p.status='active' OR COALESCE(released_at,removed_at)=now()-interval '89 days'))
		AND (SELECT count(*)=2 FROM tutorhub.mfa_recovery_codes c JOIN tutorhub.users u ON u.id=c.user_id
		     WHERE u.email='auth-retention@example.test' AND (COALESCE(consumed_at,invalidated_at) IS NULL OR consumed_at=now()-interval '29 days'))`).Scan(&evidenceOK); err != nil || !evidenceOK {
		t.Fatal("retirement windows or detached receipt metadata incorrect", err)
	}
	down, err := migrations.Files.ReadFile("000051_auth_retention.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;")
	requireRetentionSQLState(t, ctx, tx, body, "55000")
	retentionExec(t, ctx, tx, `UPDATE tutorhub.auth_challenge_deliveries SET created_at=now()-interval '31 days' WHERE provider_message_ref='fixture_ref'`)
	retentionExec(t, ctx, tx, "SET LOCAL ROLE "+maintenance)
	checkCounts(500, 0, 1, 0, 0, 0, 0)
	var receipts int64
	if err := tx.QueryRow(ctx, "SELECT tutorhub.purge_expired_deletion_receipts()").Scan(&receipts); err != nil || receipts != 0 {
		t.Fatal("legacy maintenance receipt entry point", err)
	}
}

const authRetentionSeed = `
INSERT INTO tutorhub.users(email,display_name) VALUES('auth-retention@example.test','Retention Fixture');
INSERT INTO tutorhub.auth_transactions(binding_hash,kind,created_at,expires_at)
SELECT decode(repeat(h,32),'hex'),'login',now()-age-interval '5 minutes',now()-age
FROM (VALUES ('01',interval '2 days'), ('02',interval '23 hours')) AS v(h,age);
INSERT INTO tutorhub.auth_challenges(transaction_id,purpose,channel,destination_hash,code_hash,deliverable,created_at,expires_at)
SELECT id,'login_code','email',decode(repeat('01',32),'hex'),decode(repeat('02',32),'hex'),false,created_at,expires_at
FROM tutorhub.auth_transactions WHERE binding_hash=decode(repeat('01',32),'hex');
INSERT INTO tutorhub.auth_challenge_deliveries(challenge_id,channel,provider,status,created_at)
SELECT id,'email','disabled','skipped',now()-interval '31 days' FROM tutorhub.auth_challenges WHERE code_hash=decode(repeat('02',32),'hex');
WITH bindings(n,state,age) AS (VALUES (1,'released',91),(2,'removed',91),(3,'released',89),(4,'removed',89),(5,'active',0)),
identities AS (INSERT INTO tutorhub.identities(user_id,provider,subject,kind)
    SELECT u.id,'urn:test:phone',n::text,'phone' FROM tutorhub.users u CROSS JOIN bindings WHERE u.email='auth-retention@example.test'
    RETURNING id,user_id,subject)
INSERT INTO tutorhub.user_phone_numbers(user_id,identity_id,e164_lookup_hash,e164_ciphertext,country_code,calling_code,
    masked_number,last4,status,release_reason,verified_at,last_used_at,released_at,removed_at,created_at,updated_at)
SELECT i.user_id,i.id,decode(repeat('05',32),'hex'),CASE WHEN state='active' THEN decode(repeat('03',32),'hex') END,'VN',84,
    '***1234','1234',state,CASE WHEN state='released' THEN 'reassigned' END,now()-interval '92 days',now()-interval '92 days',
    CASE WHEN state='released' THEN now()-age*interval '1 day' END,CASE WHEN state='removed' THEN now()-age*interval '1 day' END,
    now()-interval '92 days',now() FROM identities i JOIN bindings b ON b.n::text=i.subject;
INSERT INTO tutorhub.mfa_recovery_codes(user_id,code_hash,created_at,consumed_at,invalidated_at)
SELECT u.id,decode(repeat(h,32),'hex'),now()-interval '40 days',now()-c*interval '1 day',now()-i*interval '1 day'
FROM tutorhub.users u CROSS JOIN (VALUES ('01',31,NULL::int),('02',NULL,31),('03',29,NULL),('04',NULL,NULL)) AS v(h,c,i)
WHERE u.email='auth-retention@example.test';
INSERT INTO tutorhub.auth_flows(state_hash,browser_binding_hash,nonce_hash,code_verifier_ciphertext,return_to,created_at,expires_at)
SELECT decode(repeat(h,32),'hex'),decode(repeat('01',32),'hex'),decode(repeat('01',32),'hex'),decode(repeat('01',32),'hex'),'/app',
    now()-age-interval '5 minutes',now()-age FROM (VALUES ('01',interval '2 days'),('02',interval '23 hours')) AS v(h,age);
INSERT INTO tutorhub.external_data_deletion_requests(provider,idempotency_key,confirmation_code,status,created_at)
SELECT 'facebook',decode(repeat(h,32),'hex'),repeat(h,16),'received',now()-age
FROM (VALUES('01',interval '367 days'),('02',interval '364 days')) AS v(h,age);`
