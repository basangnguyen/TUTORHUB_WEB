//go:build integration

package identity

import (
	"strings"
	"testing"

	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

func TestResendMigrationRoundTripAndRollbackPreflight(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	const insertReceipt = `WITH t AS (
		INSERT INTO tutorhub.auth_transactions(binding_hash,kind,created_at,expires_at)
		VALUES(decode(repeat('45',32),'hex'),'login',now(),now()+interval '5 minutes') RETURNING id
	), c AS (
		INSERT INTO tutorhub.auth_challenges(transaction_id,purpose,channel,destination_hash,code_hash,deliverable,created_at,expires_at)
		SELECT id,'login_code','email',decode(repeat('01',32),'hex'),decode(repeat('02',32),'hex'),true,now(),now()+interval '5 minutes'
		FROM t RETURNING id
	)
	INSERT INTO tutorhub.auth_challenge_deliveries(challenge_id,channel,provider,status,created_at)
	SELECT id,'email','resend','accepted',now() FROM c`

	migrateAuthSchemaDownTo(t, ctx, url, 51)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	requireRetentionSQLState(t, ctx, tx, insertReceipt, "23514")
	rollback(tx)

	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	requireAuthSchemaVersion(t, ctx, url, latestAuthTestVersion)
	down, err := migrations.Files.ReadFile("000052_auth_resend_provider.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Execute inside a savepoint so an expected preflight failure leaves the
	// real migration ledger clean and the fixture available for inspection.
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;")
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	retentionExec(t, ctx, tx, insertReceipt)
	requireRetentionSQLState(t, ctx, tx, body, "55000")
	retentionExec(t, ctx, tx, `DELETE FROM tutorhub.auth_transactions WHERE binding_hash=decode(repeat('45',32),'hex')`)
	requireRetentionSQLState(t, ctx, tx, body, "55000")
}
