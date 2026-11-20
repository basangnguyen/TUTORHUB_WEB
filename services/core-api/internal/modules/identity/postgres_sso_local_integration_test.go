//go:build integration

package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Set the policy after the read-only password snapshot and before the login
// transaction. Calling back here also detects accidental row locks across Argon2.
type passwordVerificationHook struct {
	NativePasswordService
	afterVerify func()
}

func (h passwordVerificationHook) Verify(ctx context.Context, password, hash string) (bool, bool, error) {
	ok, rehash, err := h.NativePasswordService.Verify(ctx, password, hash)
	h.afterVerify()
	return ok, rehash, err
}

func TestSSOPostgresLocalEnforcement(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	s := f.service
	domain := "school-" + uuid.NewString() + ".test"
	existing, missing := "existing@"+domain, "missing@"+domain
	initial := f.start(t, existing)
	if _, err := s.CompleteEmailSignup(f.ctx, initial.Cookie.Value, nativeIntegrationCompleteInput(initial.ChallengeID)); err != nil {
		t.Fatal("create existing local account", err)
	}
	var userID uuid.UUID
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM tutorhub.users WHERE email=$1`, existing).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	tenant, connection := uuid.New(), uuid.New()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'School')`, tenant, "school-"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(f.ctx, `DELETE FROM tutorhub.tenants WHERE id=$1`, tenant); err != nil {
			t.Error(err)
		}
	})
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.auth_sso_connections(id,tenant_id,kind,status,allowed_org_ids) VALUES($1,$2,'google_workspace','enabled',ARRAY[$3]::text[])`, connection, tenant, domain); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.auth_sso_domains(tenant_id,connection_id,domain,status,verified_at) VALUES($1,$2,$3,'verified',now())`, tenant, connection, domain); err != nil {
		t.Fatal(err)
	}
	s.database = createPasskeyRuntimeRole(t, f)
	s.config.SSO = SSORealmConfig{Enabled: true, GoogleEnabled: true}
	openRoute, err := s.DiscoverSSORealm(f.ctx, strings.ToUpper(domain))
	if err != nil || openRoute != (SSORoute{Available: true, Provider: "google", Domain: domain}) {
		t.Fatal("optional school route", openRoute, err)
	}
	t.Run("absent route remains stable through local commit", func(t *testing.T) {
		local, err := s.database.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(local)
		if err := s.allowLocalEmail(f.ctx, local, "new@unclaimed.test", true); err != nil {
			t.Fatal(err)
		}
		admin, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(admin)
		tryWrite := func() bool {
			t.Helper()
			var acquired bool
			if err := admin.QueryRow(f.ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('tutorhub-school-sso-policy-v1',0))`).Scan(&acquired); err != nil {
				t.Fatal(err)
			}
			return acquired
		}
		if tryWrite() {
			t.Fatal("domain verification could commit while local authentication still holds the old policy")
		}
		if err := local.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		if !tryWrite() {
			t.Fatal("completed authentication retained the policy lock")
		}
	})

	signup := f.start(t, missing)
	start := SignupStartInput{Email: existing, DestinationAllowed: true}
	login, err := s.StartEmailCodeLogin(f.ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	reset, err := s.StartPasswordRecovery(f.ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	// Existing TOTP makes both the first-factor password proof and password
	// recovery continue through MFA. Enforcing SSO must invalidate both paths.
	s.config.MFATOTPEnabled = true
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.mfa_totp_factors(user_id,secret_ciphertext,status,last_used_step,created_at,confirmed_at,updated_at)
	    VALUES($1,$2,'active',1,now(),now(),now())`, userID, make([]byte, 48)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(f.ctx, `DELETE FROM tutorhub.mfa_totp_factors WHERE user_id=$1`, userID); err != nil {
			t.Error(err)
		}
	})
	pwd, err := s.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: existing, Password: nativeIntegrationPassword})
	if err != nil || pwd.MFA == nil {
		t.Fatal("initial MFA proof", err)
	}
	resetMFAStart, err := s.StartPasswordRecovery(f.ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	resetMFA, err := s.CompletePasswordRecovery(f.ctx, resetMFAStart.Cookie.Value, PasswordRecoveryCompleteInput{ChallengeID: resetMFAStart.ChallengeID, Code: "420381", NewPassword: "Another Safe Password 394!"})
	if err != nil || resetMFA == nil {
		t.Fatal("initial recovery MFA", err)
	}

	original := s.passwords
	s.passwords = passwordVerificationHook{original, func() {
		tx, err := f.pool.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		if _, err = tx.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tutorhub-school-sso-policy-v1',0))`); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(f.ctx, `UPDATE tutorhub.auth_sso_connections SET enforce_sso=true WHERE tenant_id=$1 AND id=$2`, tenant, connection); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
	}}
	_, err = s.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: existing, Password: nativeIntegrationPassword})
	s.passwords = original
	if !errors.Is(err, ErrSSORequired) {
		t.Fatal("a password validated while enforcement changed reached session or MFA issuance", err)
	}
	route, err := s.DiscoverSSORealm(f.ctx, domain)
	if err != nil || route != (SSORoute{Required: true, Available: true, Provider: "google", Domain: domain}) {
		t.Fatal("required school route", route, err)
	}
	check := func(action string, err error) {
		t.Helper()
		if !errors.Is(err, ErrSSORequired) {
			t.Errorf("%s: expected school SSO requirement, got %v", action, err)
		}
	}
	for _, email := range []string{existing, missing} {
		in := SignupStartInput{Email: email, DestinationAllowed: true}
		_, err = s.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword})
		check("password", err)
		_, err = s.StartEmailSignup(f.ctx, in)
		check("signup", err)
		_, err = s.StartEmailCodeLogin(f.ctx, in)
		check("email login", err)
		_, err = s.StartPasswordRecovery(f.ctx, in)
		check("password recovery", err)
	}
	flags := AuthMethodFlags{SignupEnabled: true, EmailCodeEnabled: true, PasswordEnabled: true}
	for _, previous := range []SignupChallengeResult{signup, login, reset} {
		_, err = s.ResendEmailChallenge(f.ctx, previous.Cookie.Value, flags, nil)
		check("resend after policy change", err)
	}
	_, err = s.CompleteEmailSignup(f.ctx, signup.Cookie.Value, nativeIntegrationCompleteInput(signup.ChallengeID))
	check("pending signup", err)
	_, err = s.VerifyEmailCodeLogin(f.ctx, login.Cookie.Value, EmailCodeVerifyInput{ChallengeID: login.ChallengeID, Code: "420381"})
	check("pending email verification", err)
	_, err = s.CompletePasswordRecovery(f.ctx, reset.Cookie.Value, PasswordRecoveryCompleteInput{ChallengeID: reset.ChallengeID, Code: "420381", NewPassword: "Another Safe Password 394!"})
	check("pending reset", err)
	for _, mfa := range []*MFAChallenge{pwd.MFA, resetMFA} {
		_, err = s.VerifyMFA(f.ctx, mfa.Cookie.Value, MFAVerifyInput{Method: "totp", Code: "000000"})
		check("pending MFA", err)
	}
	var sessions int
	var created bool
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM tutorhub.sessions WHERE user_id=$1), EXISTS(SELECT FROM tutorhub.users WHERE email=$2)`, userID, missing).Scan(&sessions, &created); err != nil || sessions != 1 || created {
		t.Fatal("rejected operations created account or session", err)
	}

	for _, cfg := range []SSORealmConfig{{Enabled: true, MicrosoftEnabled: true}, {GoogleEnabled: true}} {
		s.config.SSO = cfg
		route, err = s.DiscoverSSORealm(f.ctx, domain)
		if err != nil || route != (SSORoute{}) {
			t.Fatal("disabled route", route, err)
		}
		pwd, err = s.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: existing, Password: nativeIntegrationPassword})
		if err != nil || pwd.MFA == nil {
			t.Fatal("local login still blocked after gate disabled", err)
		}
	}
}
