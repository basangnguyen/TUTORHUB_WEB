//go:build integration

package identity

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

// This suite never falls back to DATABASE_POOL_URL or reads local env files.
// AUTH_TEST_DATABASE_URL must be an owner connection to a disposable database.
func authSchemaTestDatabase(t *testing.T) (context.Context, *pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("AUTH_TEST_DATABASE_URL")
	if url == "" || os.Getenv("AUTH_TEST_DISPOSABLE") != "true" {
		t.Skip("requires AUTH_TEST_DATABASE_URL and AUTH_TEST_DISPOSABLE=true")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal("apply disposable authentication schema")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal("connect to disposable authentication schema")
	}
	t.Cleanup(pool.Close)
	return ctx, pool, url
}

func TestAuthFoundationMigrationRoundTripAndRollbackPreflight(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	version, err := migrationrunner.CurrentVersion(ctx, url)
	if err != nil || version.Number != 48 || version.Dirty {
		t.Fatal("AUTH-01 regression roundtrip requires clean ledger 48")
	}
	if err := migrationrunner.Down(ctx, url, 6); err != nil {
		t.Fatal("empty disposable AUTH-01 rollback")
	}
	if err := migrationrunner.Up(ctx, url); err != nil {
		t.Fatal("reapply AUTH-01 migration")
	}
	version, err = migrationrunner.CurrentVersion(ctx, url)
	if err != nil || version.Number != 48 || version.Dirty {
		t.Fatal("AUTH-01 roundtrip did not retain clean ledger")
	}
	down, err := migrations.Files.ReadFile("000043_auth_foundation.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Test blocked downs directly inside rollback-only fixtures to avoid dirtying
	// the migration ledger when a preflight deliberately raises an exception.
	for _, fixture := range []string{
		`INSERT INTO tutorhub.auth_transactions(binding_hash,kind,state,created_at,expires_at)
         VALUES (decode(repeat('01',32),'hex'),'login','pending',now(),now()+interval '10 minutes')`,
		`INSERT INTO tutorhub.auth_transactions(binding_hash,kind,state,created_at,expires_at,completed_at)
         VALUES (decode(repeat('02',32),'hex'),'login','completed',now()-interval '1 hour',now()-interval '50 minutes',now()-interval '55 minutes')`,
		`WITH u AS (INSERT INTO tutorhub.users(email,display_name) VALUES ('auth-rollback@example.test','Fixture') RETURNING id)
         INSERT INTO tutorhub.identities(user_id,provider,subject,kind) SELECT id,'urn:tutorhub:email','fixture','email' FROM u`,
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin rollback preflight fixture")
		}
		if _, err := tx.Exec(ctx, fixture); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("insert rollback preflight fixture")
		}
		_, err = tx.Exec(ctx, string(down))
		_ = tx.Rollback(ctx)
		if err == nil || !strings.Contains(err.Error(), "AUTH-01 rollback blocked") {
			t.Fatal("rollback must reject retained authentication data")
		}
	}
}

func TestAuthFoundationRuntimeExactACL(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	version, err := migrationrunner.CurrentVersion(ctx, url)
	if err != nil || version.Number != 48 || version.Dirty {
		t.Fatal("AUTH-06 ACL requires clean ledger 48")
	}
	roleName := "auth_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	role := pgx.Identifier{roleName}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+role+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT"); err != nil {
		t.Fatal("disposable ACL test requires owner with CREATEROLE")
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(cleanupCtx, "DROP OWNED BY "+role)
		_, _ = pool.Exec(cleanupCtx, "DROP ROLE "+role)
	})
	// Emulate previously materialized broad grants, including column grants.
	if _, err := pool.Exec(ctx, "GRANT ALL ON tutorhub.auth_transactions, tutorhub.auth_challenges, tutorhub.auth_challenge_deliveries, tutorhub.user_password_credentials, tutorhub.user_legal_acceptances, tutorhub.webauthn_credentials, tutorhub.mfa_totp_factors, tutorhub.mfa_recovery_codes TO "+role); err != nil {
		t.Fatal("seed overbroad runtime ACL")
	}
	if _, err := pool.Exec(ctx, "GRANT UPDATE (code_hash) ON tutorhub.auth_challenges TO "+role); err != nil {
		t.Fatal("seed overbroad column ACL")
	}
	if _, err := pool.Exec(ctx, "GRANT SELECT, INSERT, UPDATE (display_name) ON tutorhub.users TO "+role); err != nil {
		t.Fatal("seed established users ACL")
	}
	if _, err := pool.Exec(ctx, "GRANT SELECT, INSERT, UPDATE (active_tenant_id, token_hash, csrf_token_hash, last_seen_at, expires_at, absolute_expires_at, revoked_at, revoked_reason, context_version) ON tutorhub.sessions TO "+role); err != nil {
		t.Fatal("seed established sessions ACL")
	}
	if _, err := pool.Exec(ctx, "GRANT SELECT, INSERT, UPDATE ON tutorhub.auth_flows, tutorhub.identities TO "+role); err != nil {
		t.Fatal("seed established federation ACL")
	}
	if _, err := pool.Exec(ctx, "GRANT UPDATE (provider) ON tutorhub.auth_flows TO "+role); err != nil {
		t.Fatal("seed overbroad flow provider column ACL")
	}
	if err := ProvisionAuthFoundationRuntimeACL(ctx, pool, roleName); err != nil {
		t.Fatal(err)
	}
	// Keep expectations independent from the provisioning implementation so an
	// accidentally broadened grant cannot silently change the test's oracle.
	expectedUpdates := map[string][]string{
		"auth_transactions":               {"state", "completed_at", "payload_ciphertext"},
		"auth_challenges":                 {"attempts", "consumed_at", "invalidated_at"},
		"auth_challenge_deliveries":       {"status", "provider_message_ref", "error_code", "delivered_at"},
		"user_password_credentials":       {"password_hash", "hash_version", "status", "failed_attempts", "last_failed_at", "locked_until", "compromised_at", "updated_at"},
		"user_legal_acceptances":          {},
		"external_data_deletion_requests": {"status", "completed_at"},
		"mfa_totp_factors":                {"status", "last_used_step", "confirmed_at", "disabled_at", "updated_at"},
		"mfa_recovery_codes":              {"consumed_at", "invalidated_at"},
		"webauthn_credentials":            {"sign_count", "clone_warning", "backup_eligible", "backup_state", "transports", "label", "status", "last_used_at", "revoked_at", "updated_at"},
	}
	for table, updates := range expectedUpdates {
		var read, insert, remove bool
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege($1,$2,'SELECT'),has_table_privilege($1,$2,'INSERT'),has_table_privilege($1,$2,'DELETE')`, roleName, "tutorhub."+table).Scan(&read, &insert, &remove); err != nil || !read || !insert || remove {
			t.Fatal("runtime table privileges differ from SELECT/INSERT only")
		}
		rows, err := pool.Query(ctx, `SELECT column_name,has_column_privilege($1,'tutorhub.'||table_name,column_name,'UPDATE')
            FROM information_schema.columns WHERE table_schema='tutorhub' AND table_name=$2`, roleName, table)
		if err != nil {
			t.Fatal("inspect runtime update privileges")
		}
		for rows.Next() {
			var column string
			var granted bool
			if err := rows.Scan(&column, &granted); err != nil || granted != slices.Contains(updates, column) {
				t.Fatal("runtime update privileges are not exact")
			}
		}
		if rows.Err() != nil {
			t.Fatal("read runtime update privileges")
		}
		rows.Close()
	}
	var usersRead, usersInsert, displayNameUpdate, verificationRead, verificationInsert, verificationUpdate, unrelatedUpdate bool
	if err := pool.QueryRow(ctx, `SELECT
        has_table_privilege($1,'tutorhub.users','SELECT'),
        has_table_privilege($1,'tutorhub.users','INSERT'),
        has_column_privilege($1,'tutorhub.users','display_name','UPDATE'),
        has_column_privilege($1,'tutorhub.users','email_verified_at','SELECT') AND has_column_privilege($1,'tutorhub.users','webauthn_user_handle','SELECT'),
        has_column_privilege($1,'tutorhub.users','email_verified_at','INSERT') AND has_column_privilege($1,'tutorhub.users','webauthn_user_handle','INSERT'),
        has_column_privilege($1,'tutorhub.users','email_verified_at','UPDATE') AND has_column_privilege($1,'tutorhub.users','webauthn_user_handle','UPDATE'),
        has_column_privilege($1,'tutorhub.users','status','UPDATE')`, roleName).
		Scan(&usersRead, &usersInsert, &displayNameUpdate, &verificationRead, &verificationInsert, &verificationUpdate, &unrelatedUpdate); err != nil ||
		!usersRead || !usersInsert || !displayNameUpdate || !verificationRead || !verificationInsert || !verificationUpdate || unrelatedUpdate {
		t.Fatal("email verification grants must preserve the established users ACL without broadening unrelated updates")
	}
	var sessionsRead, sessionsInsert, sessionsDelete, sessionReauthUpdate, sessionRevokeUpdate, sessionImmutableUpdate bool
	if err := pool.QueryRow(ctx, `SELECT
        has_table_privilege($1,'tutorhub.sessions','SELECT'),
        has_table_privilege($1,'tutorhub.sessions','INSERT'),
        has_table_privilege($1,'tutorhub.sessions','DELETE'),
        has_column_privilege($1,'tutorhub.sessions','reauthenticated_at','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','auth_methods','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','persistent','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','device_label','INSERT'),
        has_column_privilege($1,'tutorhub.sessions','revoked_at','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','revoked_reason','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','token_hash','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','csrf_token_hash','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','context_version','UPDATE'),
        has_column_privilege($1,'tutorhub.sessions','user_id','UPDATE')
        OR has_column_privilege($1,'tutorhub.sessions','created_at','UPDATE')
        OR has_column_privilege($1,'tutorhub.sessions','device_label','UPDATE')
        OR has_column_privilege($1,'tutorhub.sessions','assurance_level','UPDATE')`, roleName).
		Scan(&sessionsRead, &sessionsInsert, &sessionsDelete, &sessionReauthUpdate, &sessionRevokeUpdate, &sessionImmutableUpdate); err != nil ||
		!sessionsRead || !sessionsInsert || sessionsDelete || !sessionReauthUpdate || !sessionRevokeUpdate || sessionImmutableUpdate {
		t.Fatal("session runtime grants must support reauth/revocation/device_label without granting DELETE or immutable column updates")
	}
	for _, item := range []struct {
		table               string
		foundation, updates []string
	}{
		{"auth_flows", []string{"provider"}, []string{"consumed_at"}},
		{"identities", []string{"kind", "provider_slug", "provider_org_id", "provider_stable_id"}, []string{"status", "unlinked_at", "email_at_provider", "email_verified", "last_authenticated_at", "updated_at"}},
	} {
		var deleteGranted bool
		if err := pool.QueryRow(ctx, `SELECT has_table_privilege($1,$2,'DELETE')`, roleName, "tutorhub."+item.table).Scan(&deleteGranted); err != nil || deleteGranted {
			t.Fatal("federation provisioning must not add DELETE")
		}
		rows, err := pool.Query(ctx, `SELECT column_name,has_column_privilege($1,'tutorhub.'||table_name,column_name,'SELECT'),
            has_column_privilege($1,'tutorhub.'||table_name,column_name,'INSERT'),has_column_privilege($1,'tutorhub.'||table_name,column_name,'UPDATE')
            FROM information_schema.columns WHERE table_schema='tutorhub' AND table_name=$2`, roleName, item.table)
		if err != nil {
			t.Fatal("inspect federation ACL")
		}
		for rows.Next() {
			var column string
			var read, insert, update bool
			if err := rows.Scan(&column, &read, &insert, &update); err != nil || update != slices.Contains(item.updates, column) ||
				(slices.Contains(item.foundation, column) && (!read || !insert)) {
				rows.Close()
				t.Fatal("federation ACL differs from independent expected column permissions")
			}
		}
		if rows.Err() != nil {
			rows.Close()
			t.Fatal("read federation ACL")
		}
		rows.Close()
	}
	testRuntimeFacebookErasure(t, ctx, pool, roleName)
	t.Run("native email insert and relink under runtime role", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal("begin rollback-only runtime fixture")
		}
		defer rollback(tx)
		userID := uuid.New()
		email := userID.String() + "@example.test"
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES($1,$2,'Runtime ACL fixture')`, userID, email); err != nil {
			t.Fatal("seed runtime user")
		}
		if _, err := tx.Exec(ctx, "GRANT USAGE ON SCHEMA tutorhub TO "+role); err != nil {
			t.Fatal("grant runtime schema usage")
		}
		// PostgreSQL 16+ gives a CREATEROLE creator ADMIN, not SET, on new roles.
		// Grant membership inside the rollback-only transaction so it never persists.
		if _, err := tx.Exec(ctx, "GRANT "+role+" TO CURRENT_USER"); err != nil {
			t.Fatal("grant rollback-only fixture role membership")
		}
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+role); err != nil {
			t.Fatal("disposable ACL test owner must be able to SET ROLE to its fixture role")
		}
		now := time.Now().UTC()
		identityID, err := ensureNativeEmailIdentity(ctx, tx, userID, email, now)
		if err != nil {
			t.Fatal("native email insert requires only the exact runtime ACL")
		}
		if _, err := tx.Exec(ctx, `UPDATE tutorhub.identities SET status='unlinked',unlinked_at=$2 WHERE id=$1`, identityID, now); err != nil {
			t.Fatal("unlink runtime email fixture")
		}
		relinked, err := ensureNativeEmailIdentity(ctx, tx, userID, email, now.Add(time.Second))
		if err != nil || relinked != identityID {
			t.Fatal("native email relink must not update immutable provider metadata")
		}
		var slug, kind, status string
		if err := tx.QueryRow(ctx, `SELECT provider_slug,kind,status FROM tutorhub.identities WHERE id=$1`, identityID).Scan(&slug, &kind, &status); err != nil || slug != "email" || kind != "email" || status != "active" {
			t.Fatal("runtime email relink lost identity metadata")
		}
	})
	// NOINHERIT only prevents implicit grants; SET ROLE can still recover a
	// parent's privileges. Dedicated API credentials must not have that escape.
	parentRole := pgx.Identifier{roleName + "_parent"}.Sanitize()
	if _, err := pool.Exec(ctx, "CREATE ROLE "+parentRole+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE"); err != nil {
		t.Fatal("create disposable ACL parent role")
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(cleanupCtx, "DROP OWNED BY "+parentRole)
		_, _ = pool.Exec(cleanupCtx, "DROP ROLE "+parentRole)
	})
	if _, err := pool.Exec(ctx, "GRANT DELETE ON tutorhub.auth_challenges TO "+parentRole); err != nil {
		t.Fatal("seed disposable ACL parent permission")
	}
	if _, err := pool.Exec(ctx, "GRANT "+parentRole+" TO "+role); err != nil {
		t.Fatal("seed NOINHERIT role membership")
	}
	if err := ProvisionAuthFoundationRuntimeACL(ctx, pool, roleName); err == nil {
		t.Fatal("runtime ACL must reject SET ROLE through a NOINHERIT membership")
	}
}

func TestAuthFoundationSessionMetadataAndFederatedIdentityFilter(t *testing.T) {
	ctx, pool, _ := authSchemaTestDatabase(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin session fixture")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	userID, sessionID := uuid.New(), uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES ($1,$2,'Auth fixture')`, userID, userID.String()+"@example.test"); err != nil {
		t.Fatal("insert session user")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.sessions(id,user_id,token_hash,csrf_token_hash,created_at,last_seen_at,expires_at,absolute_expires_at,auth_time,reauthenticated_at,assurance_level,auth_methods)
        VALUES ($1,$2,$3,$4,$5,$5,$6,$7,$8,$5,2,ARRAY['pwd','totp'])`,
		sessionID, userID, make([]byte, 32), make([]byte, 32), now, now.Add(time.Hour), now.Add(2*time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("insert assured session: %s", err.Error())
	}
	// Remote disposable databases need more than one second for savepoint round trips.
	repo := NewPostgresRepository(nil, 20*time.Second, nil)
	principal, err := repo.loadPrincipal(ctx, tx, sessionID, userID, uuid.Nil)
	if err != nil || !principal.AuthenticatedAt.Equal(now) || principal.AssuranceLevel != 2 || !slices.Equal(principal.AuthMethods, []string{"pwd", "totp"}) {
		t.Fatal("session metadata did not preserve reauthentication and assurance")
	}
	for _, kind := range []string{"oidc", "oauth2", "email", "phone", "passkey"} {
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.identities(user_id,provider,subject,kind) VALUES($1,$2,$3,$4)`, userID, "urn:test:"+kind, userID.String(), kind); err != nil {
			t.Fatal("insert identity kinds")
		}
	}
	// A nested pgx transaction commits a savepoint, leaving the fixture rollback-only.
	repo.database = tx
	identities, err := repo.ListIdentities(ctx, userID)
	if err != nil || len(identities) != 2 {
		t.Fatal("identity list must contain only federated identities")
	}
}
