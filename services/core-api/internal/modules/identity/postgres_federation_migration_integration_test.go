//go:build integration

package identity

import (
	"context"
	"strings"
	"testing"

	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

func TestFederationMigrationRoundTripAndBindingConstraints(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	if err := migrationrunner.Down(ctx, url, 3); err != nil {
		t.Fatal("rollback empty federation schema")
	}
	version, err := migrationrunner.CurrentVersion(ctx, url)
	if err != nil || version.Number != 44 || version.Dirty {
		t.Fatal("federation down must restore ledger 44")
	}
	var oldBinding string
	if err := pool.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='tutorhub.auth_flows'::regclass AND conname='auth_flows_identity_link_binding_check'`).Scan(&oldBinding); err != nil || strings.Contains(oldBinding, "reauth") {
		t.Fatal("federation rollback did not restore original flow binding")
	}
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal("apply federation schema")
	}
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal("federation migration rerun")
	}
	version, err = migrationrunner.CurrentVersion(ctx, url)
	if err != nil || version.Number != 47 || version.Dirty {
		t.Fatal("federation must finish at clean ledger 47")
	}
	for _, item := range []struct {
		provider, purpose string
		bound, valid      bool
	}{
		{"oidc", "login", false, true}, {"google", "reauth", true, true}, {"google", "identity_link", true, true},
		{"google", "reauth", false, false}, {"google", "login", true, false}, {"unknown", "login", false, false},
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin rollback-only flow constraint fixture")
		}
		userID, sessionID := "NULL", "NULL"
		if item.bound {
			if _, err := tx.Exec(ctx, federationMigrationUserSessionFixture); err != nil {
				_ = tx.Rollback(context.Background())
				t.Fatal("seed bound flow fixture")
			}
			userID, sessionID = "'11111111-1111-4111-8111-111111111111'", "'22222222-2222-4222-8222-222222222222'"
		}
		_, err = tx.Exec(ctx, `INSERT INTO tutorhub.auth_flows(state_hash,browser_binding_hash,nonce_hash,code_verifier_ciphertext,return_to,created_at,expires_at,provider,purpose,user_id,session_id)
            VALUES(decode(repeat('91',32),'hex'),decode(repeat('92',32),'hex'),decode(repeat('93',32),'hex'),decode(repeat('94',32),'hex'),'/app',now(),now()+interval '10 minutes',$1,$2,`+userID+`,`+sessionID+`)`, item.provider, item.purpose)
		_ = tx.Rollback(context.Background())
		if (err == nil) != item.valid {
			t.Fatal("federation flow provider/purpose/binding constraint mismatch")
		}
	}
}

const federationMigrationUserSessionFixture = `
    INSERT INTO tutorhub.users(id,email,display_name) VALUES('11111111-1111-4111-8111-111111111111','federation-migration@example.test','Migration fixture');
    INSERT INTO tutorhub.sessions(id,user_id,token_hash,csrf_token_hash,created_at,last_seen_at,expires_at,absolute_expires_at,auth_time)
    VALUES('22222222-2222-4222-8222-222222222222','11111111-1111-4111-8111-111111111111',decode(repeat('95',32),'hex'),decode(repeat('96',32),'hex'),now(),now(),now()+interval '1 hour',now()+interval '24 hours',now());`

func TestFederationMigrationRollbackPreflight(t *testing.T) {
	ctx, pool, _ := authSchemaTestDatabase(t)
	down, err := migrations.Files.ReadFile("000045_federation_providers.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Keep all fixture data and even an unexpectedly successful DDL inside the
	// outer rollback-only transaction. Never execute the migration COMMIT here.
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;")), "COMMIT;"))
	for _, fixture := range []string{
		`INSERT INTO tutorhub.auth_flows(state_hash,browser_binding_hash,nonce_hash,code_verifier_ciphertext,return_to,created_at,expires_at,provider)
         VALUES(decode(repeat('91',32),'hex'),decode(repeat('92',32),'hex'),decode(repeat('93',32),'hex'),decode(repeat('94',32),'hex'),'/app',now(),now()+interval '10 minutes','google')`,
		`INSERT INTO tutorhub.auth_flows(state_hash,browser_binding_hash,nonce_hash,code_verifier_ciphertext,return_to,created_at,expires_at,provider,purpose,user_id,session_id)
         VALUES(decode(repeat('91',32),'hex'),decode(repeat('92',32),'hex'),decode(repeat('93',32),'hex'),decode(repeat('94',32),'hex'),'/app',now(),now()+interval '10 minutes','oidc','reauth','11111111-1111-4111-8111-111111111111','22222222-2222-4222-8222-222222222222')`,
		`INSERT INTO tutorhub.identities(user_id,provider,subject,kind,provider_slug) VALUES('11111111-1111-4111-8111-111111111111','https://accounts.google.com','fixture','oidc','google')`,
		`INSERT INTO tutorhub.identities(user_id,provider,subject,kind,provider_slug) VALUES('11111111-1111-4111-8111-111111111111','https://facebook.example','fixture','oauth2','facebook')`,
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin rollback preflight fixture")
		}
		if _, err = tx.Exec(ctx, federationMigrationUserSessionFixture); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatal("seed preflight account")
		}
		if _, err = tx.Exec(ctx, fixture); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatal("seed preflight artifact")
		}
		_, err = tx.Exec(ctx, body)
		_ = tx.Rollback(context.Background())
		if err == nil || !strings.Contains(err.Error(), "AUTH-05 rollback blocked") {
			t.Fatal("federation rollback must reject retained flows or identities")
		}
	}
}
