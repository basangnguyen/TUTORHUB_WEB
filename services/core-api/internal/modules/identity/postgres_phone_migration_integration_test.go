//go:build integration

package identity

import (
	"strings"
	"testing"

	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

func TestPhoneMigrationRoundTripAndRollbackPreflight(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	checkVersion := func(want uint) {
		t.Helper()
		v, err := migrationrunner.CurrentVersion(ctx, url)
		if err != nil || v.Dirty || v.Number != want {
			t.Fatalf("expected clean ledger %d, got %+v (%v)", want, v, err)
		}
	}
	// Start at 48, then exercise forward, reverse and forward again.
	for range 2 {
		if err := migrationrunner.Down(ctx, url, 3); err != nil {
			t.Fatal(err)
		}
		checkVersion(48)
		if err := migrationrunner.Up(ctx, url); err != nil {
			t.Fatal(err)
		}
		checkVersion(51)
	}
	down, err := migrations.Files.ReadFile("000049_phone_numbers.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;")
	const user = `WITH u AS (INSERT INTO tutorhub.users(email,display_name) VALUES('phone-migration@example.test','Fixture') RETURNING id) `
	// Each fixture activates just one predicate, including a retained binding
	// with a non-phone identity to isolate the table preflight.
	for name, statement := range map[string]string{
		"binding": user + `, i AS (INSERT INTO tutorhub.identities(user_id,kind,provider,subject) SELECT id,'email','urn:test','fixture' FROM u RETURNING id,user_id)
		INSERT INTO tutorhub.user_phone_numbers(user_id,identity_id,e164_lookup_hash,country_code,calling_code,masked_number,last4,status,removed_at,verified_at,last_used_at,created_at,updated_at)
		SELECT user_id,id,decode(repeat('00',32),'hex'),'VN',84,'+84 ***5678','5678','removed',now(),now(),now(),now(),now() FROM i`,
		"identity":    user + `INSERT INTO tutorhub.identities(user_id,kind,provider,subject) SELECT id,'phone','urn:tutorhub:phone','fixture' FROM u`,
		"transaction": `INSERT INTO tutorhub.auth_transactions(binding_hash,kind,created_at,expires_at) VALUES(decode(repeat('00',32),'hex'),'phone',now(),now()+interval '5 minutes')`,
		"login":       phonePreflightChallenge("phone_login"),
		"verify":      phonePreflightChallenge("phone_verify"),
	} {
		t.Run(name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(tx)
			if _, err := tx.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, body); err == nil || !strings.Contains(err.Error(), "AUTH-10 rollback blocked") {
				t.Fatal("preflight did not reject fixture", err)
			}
		})
	}
}

func phonePreflightChallenge(purpose string) string {
	return `WITH t AS (INSERT INTO tutorhub.auth_transactions(binding_hash,kind,created_at,expires_at)
	VALUES(decode(repeat('00',32),'hex'),'login',now(),now()+interval '5 minutes') RETURNING id)
	INSERT INTO tutorhub.auth_challenges(transaction_id,purpose,channel,destination_hash,code_hash,deliverable,created_at,expires_at)
	SELECT id,'` + purpose + `','sms',decode(repeat('00',32),'hex'),decode(repeat('00',32),'hex'),false,now(),now()+interval '5 minutes' FROM t`
}
