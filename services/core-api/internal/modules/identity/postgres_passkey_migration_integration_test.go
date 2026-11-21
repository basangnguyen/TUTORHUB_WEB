//go:build integration

package identity

import (
	"strings"
	"testing"

	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

func TestPasskeyMigrationRoundTripAndRollbackPreflight(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	migrateAuthSchemaDownTo(t, ctx, url, 46)
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal("reapply passkey schema", err)
	}
	requireAuthSchemaVersion(t, ctx, url, latestAuthTestVersion)
	down, err := migrations.Files.ReadFile("000047_webauthn_credentials.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;")
	for _, fixture := range []string{
		`INSERT INTO tutorhub.users(email,display_name,webauthn_user_handle) VALUES('passkey-handle@example.test','Handle',decode(repeat('01',64),'hex'))`,
		`WITH u AS (INSERT INTO tutorhub.users(email,display_name) VALUES('passkey-row@example.test','Credential') RETURNING id)
		 INSERT INTO tutorhub.webauthn_credentials(user_id,credential_id,public_key,attestation_type,label,created_at,updated_at)
		 SELECT id,decode('01','hex'),decode('02','hex'),'none','Device',now(),now() FROM u`,
		`INSERT INTO tutorhub.auth_transactions(binding_hash,kind,state,created_at,expires_at) VALUES(decode(repeat('03',32),'hex'),'passkey','pending',now(),now()+interval '5 minutes')`,
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin preflight fixture", err)
		}
		if _, err := tx.Exec(ctx, fixture); err != nil {
			rollback(tx)
			t.Fatal("seed passkey preflight fixture", err)
		}
		_, err = tx.Exec(ctx, body)
		rollback(tx)
		if err == nil || !strings.Contains(err.Error(), "AUTH-08 rollback blocked") {
			t.Fatal("retained data escaped passkey rollback preflight", err)
		}
	}
}
