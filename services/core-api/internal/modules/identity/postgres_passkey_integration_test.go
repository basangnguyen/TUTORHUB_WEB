//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Begin each ceremony on a real pooled connection using the provisioned
// role. SET LOCAL resets automatically on commit/rollback and permits races.
type passkeyRuntimeDB struct {
	pool       *pgxpool.Pool
	quotedRole string
}

func (db passkeyRuntimeDB) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+db.quotedRole); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

func createPasskeyRuntimeRole(t *testing.T, f *nativeIntegrationFixture) Database {
	t.Helper()
	roleName := "auth_passkeys_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	role := pgx.Identifier{roleName}.Sanitize()
	if _, err := f.pool.Exec(f.ctx, "CREATE ROLE "+role+" NOLOGIN NOSUPERUSER NOCREATEROLE NOCREATEDB NOINHERIT"); err != nil {
		t.Fatal("create disposable runtime role", err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(f.ctx, "DROP OWNED BY "+role); err != nil {
			t.Error("clean disposable runtime ACL", err)
		}
		if _, err := f.pool.Exec(f.ctx, "DROP ROLE "+role); err != nil {
			t.Error("clean disposable runtime role", err)
		}
	})
	for _, statement := range []string{
		"GRANT USAGE ON SCHEMA tutorhub TO " + role,
		"GRANT SELECT,INSERT,UPDATE(display_name) ON tutorhub.users TO " + role,
		"GRANT SELECT,INSERT,UPDATE(active_tenant_id,token_hash,csrf_token_hash,last_seen_at,expires_at,absolute_expires_at,revoked_at,revoked_reason,context_version) ON tutorhub.sessions TO " + role,
		"GRANT SELECT,INSERT ON tutorhub.identities,tutorhub.auth_flows TO " + role,
		"GRANT SELECT ON tutorhub.memberships,tutorhub.tenants TO " + role,
		"GRANT INSERT ON tutorhub.outbox_events TO " + role,
		"GRANT " + role + " TO CURRENT_USER",
	} {
		if _, err := f.pool.Exec(f.ctx, statement); err != nil {
			t.Fatal("seed established runtime ACL", err)
		}
	}
	if err := ProvisionAuthFoundationRuntimeACL(f.ctx, f.pool, roleName); err != nil {
		t.Fatal("provision disposable auth ACL", err)
	}
	return passkeyRuntimeDB{pool: f.pool, quotedRole: role}
}

func TestPasskeyPostgresRuntimeCeremoniesAndConcurrentVerify(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	const origin = "https://passkeys.example.test"
	rp, err := NewPasskeyProtocol("passkeys.example.test", "TutorHub", []string{origin})
	if err != nil {
		t.Fatal(err)
	}
	f.service.webauthn = rp
	email := "passkey-" + uuid.NewString() + "@example.test"
	start := f.start(t, email)
	login, err := f.service.CompleteEmailSignup(f.ctx, start.Cookie.Value, nativeIntegrationCompleteInput(start.ChallengeID))
	if err != nil {
		t.Fatal("seed native login", err)
	}
	hash := func(token string) []byte { return f.service.crypto.Digest(sessionPurpose, token) }
	var p Principal
	if err := f.pool.QueryRow(f.ctx, `SELECT s.id,s.user_id,s.context_version,u.display_name FROM tutorhub.sessions s JOIN tutorhub.users u ON u.id=s.user_id WHERE s.token_hash=$1`, hash(login.SessionToken)).Scan(&p.SessionID, &p.User.ID, &p.ContextVersion, &p.User.DisplayName); err != nil {
		t.Fatal("load fixture principal", err)
	}
	t.Cleanup(func() {
		for _, statement := range []string{`DELETE FROM tutorhub.outbox_events WHERE aggregate_id=$1`, `DELETE FROM tutorhub.webauthn_credentials WHERE user_id=$1`} {
			if _, err := f.pool.Exec(f.ctx, statement, p.User.ID); err != nil {
				t.Error("clean owner-only passkey fixtures", err)
			}
		}
	})
	f.service.database = createPasskeyRuntimeRole(t, f)
	begin := func(purpose PasskeyPurpose, principal *Principal) PasskeyOptions {
		t.Helper()
		options, err := f.service.BeginPasskey(f.ctx, purpose, principal)
		if err != nil {
			t.Fatal("begin", purpose, err)
		}
		var id uuid.UUID
		var cipher []byte
		var fiveMinutes bool
		if err := f.pool.QueryRow(f.ctx, `SELECT id,payload_ciphertext,expires_at-created_at=interval '5 minutes' FROM tutorhub.auth_transactions WHERE binding_hash=$1`, f.service.crypto.Digest("auth-tx-binding", options.Cookie.Value)).Scan(&id, &cipher, &fiveMinutes); err != nil || !fiveMinutes || len(cipher) == 0 || bytes.Contains(cipher, []byte("challenge")) {
			t.Fatal("challenge storage/expiry", err)
		}
		f.transactions = append(f.transactions, id)
		return options
	}
	key := newVirtualPasskey(t)
	o := begin(PasskeyRegistration, &p)
	registered, err := f.service.FinishPasskeyRegistration(f.ctx, p, o.Cookie.Value, key.Register(t, o.Options, origin, true, true, false, 0), "Laptop")
	if err != nil {
		t.Fatal("register passkey", err)
	}
	var handle []byte
	if err := f.pool.QueryRow(f.ctx, `SELECT webauthn_user_handle FROM tutorhub.users WHERE id=$1`, p.User.ID).Scan(&handle); err != nil || len(handle) != 64 || !bytes.Equal(handle, key.Handle) {
		t.Fatal("user handle", err)
	}
	o = begin(PasskeyRegistration, &p)
	attestation := key.Register(t, o.Options, origin, true, true, false, 0)
	if _, err := f.service.FinishPasskeyRegistration(f.ctx, p, o.Cookie.Value, attestation, "Duplicate"); !errors.Is(err, ErrPasskeyConflict) {
		t.Fatal("duplicate credential", err)
	}
	if _, err := f.service.FinishPasskeyRegistration(f.ctx, p, o.Cookie.Value, attestation, "Duplicate"); !errors.Is(err, ErrInvalidPasskey) {
		t.Fatal("duplicate transaction replay", err)
	}
	if item, err := f.service.RenameMyPasskey(f.ctx, p, registered.ID, "Renamed"); err != nil || item.Label != "Renamed" {
		t.Fatal("rename", err)
	}
	if items, err := f.service.ListMyPasskeys(f.ctx, p); err != nil || len(items) != 1 || items[0].ID != registered.ID {
		t.Fatal("list", err)
	}
	if overview, err := f.service.GetMySecurity(f.ctx, p); err != nil || overview.PasskeyCount != 1 || len(overview.ReauthMethods) == 0 || overview.ReauthMethods[0] != "passkey" {
		t.Fatal("security summary", err)
	}
	if methods, err := f.service.ListMyLoginMethods(f.ctx, p); err != nil || methods.PasskeyCount != 1 {
		t.Fatal("login methods summary", err)
	}
	unknown := newVirtualPasskey(t)
	unknown.Handle = key.Handle
	o = begin(PasskeyLogin, nil)
	if _, err := f.service.FinishPasskeyLogin(f.ctx, o.Cookie.Value, unknown.Assert(t, o.Options, origin, true, false, false, 0), false, "", ""); !errors.Is(err, ErrPasskeyUnknown) {
		t.Fatal("unknown credential signal", err)
	}
	o = begin(PasskeyLogin, nil)
	another := begin(PasskeyLogin, nil)
	if _, err := f.service.FinishPasskeyLogin(f.ctx, o.Cookie.Value, unknown.Assert(t, another.Options, origin, true, false, false, 0), false, "", ""); !errors.Is(err, ErrInvalidPasskey) {
		t.Fatal("wrong challenge must suppress unknown signal", err)
	}
	checkLogin := func(token string, level int) {
		t.Helper()
		var valid bool
		if err := f.pool.QueryRow(f.ctx, `SELECT assurance_level=$2 AND identity_id IS NULL AND auth_methods=ARRAY['passkey'] AND device_label IS NOT NULL FROM tutorhub.sessions WHERE token_hash=$1`, hash(token), level).Scan(&valid); err != nil || !valid {
			t.Fatal("passkey session assurance/metadata", err)
		}
	}
	o = begin(PasskeyLogin, nil)
	signed := key.Assert(t, o.Options, origin, true, true, true, 0)
	type outcome struct {
		session NativeSessionResult
		err     error
	}
	done := make(chan outcome, 2)
	for range 2 {
		go func() {
			result, err := f.service.FinishPasskeyLogin(f.ctx, o.Cookie.Value, signed, true, "Mozilla/5.0", "127.0.0.1:12345")
			done <- outcome{result, err}
		}()
	}
	var winner NativeSessionResult
	accepted, rejected := 0, 0
	for range 2 {
		r := <-done
		if r.err == nil {
			accepted++
			winner = r.session
		} else if errors.Is(r.err, ErrInvalidPasskey) {
			rejected++
		} else {
			t.Fatal("concurrent verify", r.err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatal("same challenge must create exactly one session")
	}
	checkLogin(winner.SessionToken, 2)
	o = begin(PasskeyLogin, nil)
	low, err := f.service.FinishPasskeyLogin(f.ctx, o.Cookie.Value, key.Assert(t, o.Options, origin, false, true, true, 0), true, "virtual passkey", "")
	if err != nil {
		t.Fatal("UV=false synced passkey", err)
	}
	checkLogin(low.SessionToken, 1)
	var lowP Principal
	lowP.User = p.User
	if err := f.pool.QueryRow(f.ctx, `SELECT id,context_version FROM tutorhub.sessions WHERE token_hash=$1`, hash(low.SessionToken)).Scan(&lowP.SessionID, &lowP.ContextVersion); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Passkey cap')`, tenant, "passkey-"+tenant.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = f.pool.Exec(f.ctx, `DELETE FROM tutorhub.tenants WHERE id=$1`, tenant) })
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'org_admin','active')`, tenant, p.User.ID); err != nil {
		t.Fatal(err)
	}
	o = begin(PasskeyReauth, &lowP)
	stepped, err := f.service.FinishPasskeyReauth(f.ctx, lowP, o.Cookie.Value, key.Assert(t, o.Options, origin, true, true, true, 1))
	if err != nil {
		t.Fatal("step up", err)
	}
	if stepped.CSRFToken == low.CSRFToken || stepped.SessionToken == low.SessionToken {
		t.Fatal("step-up didn't rotate both proofs")
	}
	var capOK bool
	if err := f.pool.QueryRow(f.ctx, `SELECT n.assurance_level=2 AND n.id<>old.id AND old.revoked_at IS NOT NULL AND n.reauthenticated_at IS NOT NULL
	    AND NOT n.persistent AND n.absolute_expires_at<=old.created_at+interval '24 hours' AND n.created_at=old.created_at AND n.expires_at=$3
	    FROM tutorhub.sessions n,tutorhub.sessions old WHERE n.token_hash=$1 AND old.id=$2`, hash(stepped.SessionToken), lowP.SessionID, stepped.CookieExpiresAt).Scan(&capOK); err != nil || !capOK {
		t.Fatal("administrator cap or session replacement", err)
	}
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM tutorhub.user_password_credentials WHERE user_id=$1`, p.User.ID); err != nil {
		t.Fatal(err)
	}
	var identityID uuid.UUID
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM tutorhub.identities WHERE user_id=$1 AND status='active'`, p.User.ID).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgresRepository(f.service.database, 20*time.Second, nil)
	if err := repo.UnlinkIdentity(f.ctx, p.User.ID, p.SessionID, identityID, time.Now().UTC()); !errors.Is(err, ErrLastIdentity) {
		t.Fatal("disabled passkeys counted as an alternate", err)
	}
	repo.SetPasskeysEnabled(true)
	if err := repo.UnlinkIdentity(f.ctx, p.User.ID, p.SessionID, identityID, time.Now().UTC()); err != nil {
		t.Fatal("active passkey should permit final identity unlink", err)
	}
	if err := f.service.RevokeMyPasskey(f.ctx, p, registered.ID); !errors.Is(err, ErrLastIdentity) {
		t.Fatal("last method guard", err)
	}
	if err := f.pool.QueryRow(f.ctx, `INSERT INTO tutorhub.identities(user_id,provider,subject) VALUES($1,'urn:fixture:oidc',$2) RETURNING id`, p.User.ID, uuid.NewString()).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	o = begin(PasskeyLogin, nil)
	if _, err := f.service.FinishPasskeyLogin(f.ctx, o.Cookie.Value, key.Assert(t, o.Options, origin, true, true, true, 1), false, "", ""); !errors.Is(err, ErrPasskeyClone) {
		t.Fatal("stagnant nonzero counter", err)
	}
	o = begin(PasskeyLogin, nil)
	if _, err := f.service.FinishPasskeyLogin(f.ctx, o.Cookie.Value, key.Assert(t, o.Options, origin, true, true, true, 3), false, "", ""); !errors.Is(err, ErrPasskeyClone) {
		t.Fatal("flagged credential regained access", err)
	}
	var cloneWarning bool
	if err := f.pool.QueryRow(f.ctx, `SELECT clone_warning FROM tutorhub.webauthn_credentials WHERE id=$1`, registered.ID).Scan(&cloneWarning); err != nil || !cloneWarning {
		t.Fatal("clone warning not durable", err)
	}
	if err := repo.UnlinkIdentity(f.ctx, p.User.ID, p.SessionID, identityID, time.Now().UTC()); !errors.Is(err, ErrLastIdentity) {
		t.Fatal("quarantined passkey counted as an alternate", err)
	}
	if security, err := f.service.GetMySecurity(f.ctx, p); err != nil || security.PasskeyCount != 1 || slices.Contains(security.ReauthMethods, "passkey") {
		t.Fatal("quarantined passkey offered for step-up", err)
	}
	if err := f.service.RevokeMyPasskey(f.ctx, p, registered.ID); err != nil {
		t.Fatal("revoke with active federated method", err)
	}
	o = begin(PasskeyLogin, nil)
	if _, err := f.service.FinishPasskeyLogin(f.ctx, o.Cookie.Value, key.Assert(t, o.Options, origin, true, true, true, 4), false, "", ""); !errors.Is(err, ErrInvalidPasskey) {
		t.Fatal("revoked credential exposed Signal or authenticated", err)
	}
	rows, err := f.pool.Query(f.ctx, `SELECT event_type FROM tutorhub.outbox_events WHERE aggregate_id=$1 AND event_type LIKE 'passkey_%' ORDER BY event_type`, p.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || !slices.Equal(events, []string{"passkey_added", "passkey_clone_detected", "passkey_removed"}) {
		t.Fatal("one outbox record per event", events, err)
	}
}
