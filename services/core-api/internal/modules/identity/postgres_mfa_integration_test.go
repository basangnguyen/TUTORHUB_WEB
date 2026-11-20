//go:build integration

package identity

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

func TestMFAMigrationRoundTripAndRollbackPreflight(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	if err := migrationrunner.Down(ctx, url, 2); err != nil {
		t.Fatal(err)
	}
	v, err := migrationrunner.CurrentVersion(ctx, url)
	if err != nil || v.Dirty || v.Number != 47 {
		t.Fatal("rollback to 47", err)
	}
	if err = migrationrunner.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	v, err = migrationrunner.CurrentVersion(ctx, url)
	if err != nil || v.Dirty || v.Number != 49 {
		t.Fatal("reapply through 49", err)
	}
	down, err := migrations.Files.ReadFile("000048_mfa_totp_recovery.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;")
	const newUser = `WITH u AS (INSERT INTO tutorhub.users(email,display_name) VALUES('mfa-fixture@example.test','Fixture') RETURNING id) `
	for _, fixture := range []string{
		newUser + `INSERT INTO tutorhub.mfa_totp_factors(user_id,status,secret_ciphertext,created_at,updated_at) SELECT id,'pending',decode(repeat('01',29),'hex'),now(),now() FROM u`,
		newUser + `INSERT INTO tutorhub.mfa_recovery_codes(user_id,code_hash,created_at) SELECT id,decode(repeat('01',32),'hex'),now() FROM u`,
		`INSERT INTO tutorhub.auth_transactions(binding_hash,kind,created_at,expires_at) VALUES(decode(repeat('01',32),'hex'),'mfa',now(),now()+interval '5 minutes')`,
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, fixture); err != nil {
			rollback(tx)
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, body)
		rollback(tx)
		if err == nil || !strings.Contains(err.Error(), "AUTH-09 rollback blocked") {
			t.Fatal("rollback preflight", err)
		}
	}
}

func TestMFAPostgresRuntimeLifecycleAndConcurrentReplay(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	s := f.service
	s.config.MFATOTPEnabled = true
	email := "mfa-" + uuid.NewString() + "@example.test"
	signup := f.start(t, email)
	initial, err := s.CompleteEmailSignup(f.ctx, signup.Cookie.Value, nativeIntegrationCompleteInput(signup.ChallengeID))
	if err != nil {
		t.Fatal(err)
	}
	principal := func(token string) Principal {
		t.Helper()
		var p Principal
		if err := f.pool.QueryRow(f.ctx, `SELECT id,user_id,context_version,assurance_level FROM tutorhub.sessions WHERE token_hash=$1`, s.crypto.Digest(sessionPurpose, token)).Scan(&p.SessionID, &p.User.ID, &p.ContextVersion, &p.AssuranceLevel); err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := principal(initial.SessionToken)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM tutorhub.outbox_events WHERE aggregate_id=$1`, `DELETE FROM tutorhub.mfa_recovery_codes WHERE user_id=$1`, `DELETE FROM tutorhub.mfa_totp_factors WHERE user_id=$1`, `DELETE FROM tutorhub.auth_transactions WHERE kind='mfa' AND user_id=$1`} {
			if _, err := f.pool.Exec(f.ctx, q, first.User.ID); err != nil {
				t.Error(err)
			}
		}
	})
	now := time.Now().UTC().Add(time.Second)
	s.now = func() time.Time { return now }
	s.database = createPasskeyRuntimeRole(t, f)
	if err := s.DisableTOTP(f.ctx, first); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("weak session reached factor management", err)
	}
	enrolled, err := s.EnrollTOTP(f.ctx, first)
	if err != nil || len(enrolled.Secret) != 32 || !strings.HasPrefix(enrolled.OTPAuthURI, "otpauth://") {
		t.Fatal("enroll", err)
	}
	var cipher []byte
	if err = f.pool.QueryRow(f.ctx, `SELECT secret_ciphertext FROM tutorhub.mfa_totp_factors WHERE user_id=$1`, first.User.ID).Scan(&cipher); err != nil || bytes.Contains(cipher, []byte(enrolled.Secret)) {
		t.Fatal("stored seed", err)
	}
	code := func() string {
		v, err := totp.GenerateCode(enrolled.Secret, now)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	confirmed, err := s.ConfirmTOTP(f.ctx, first, code())
	if err != nil || len(confirmed.RecoveryCodes) != 10 || confirmed.Session.CSRFToken == initial.CSRFToken {
		t.Fatal("confirm", err)
	}
	strong := principal(confirmed.Session.SessionToken)
	if strong.AssuranceLevel != 2 || strong.SessionID == first.SessionID {
		t.Fatal("rotated AAL2 session")
	}
	start := func() string {
		t.Helper()
		res, err := s.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword, KeepSignedIn: true})
		if err != nil || res.MFA == nil || res.SessionToken != "" {
			t.Fatal("password MFA gate", err)
		}
		state, err := s.PendingMFA(f.ctx, res.MFA.Cookie.Value)
		if err != nil || !slices.Equal(state.Methods, []string{"totp", "recovery_code"}) || state.ExpiresInSeconds != 300 {
			t.Fatal("pending", err)
		}
		return res.MFA.Cookie.Value
	}
	verify := func(binding, method, code string) (MFAVerifyResult, error) {
		return s.VerifyMFA(f.ctx, binding, MFAVerifyInput{Method: method, Code: code})
	}
	a, b := start(), start()
	if _, err = verify(a, "totp", code()); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("confirm counter was reused", err)
	}
	now = now.Add(30 * time.Second)
	var acceptedMFACode = code()
	type outcome struct {
		res MFAVerifyResult
		err error
	}
	results := make(chan outcome, 2)
	for _, binding := range []string{a, b} {
		go func() { res, err := verify(binding, "totp", acceptedMFACode); results <- outcome{res, err} }()
	}
	successes := 0
	for range 2 {
		r := <-results
		if r.err == nil {
			successes++
			var methods []string
			if !r.res.Session.Persistent || principal(r.res.Session.SessionToken).AssuranceLevel != 2 {
				t.Fatal("login assurance or persistence")
			}
			if err = f.pool.QueryRow(f.ctx, `SELECT auth_methods FROM tutorhub.sessions WHERE token_hash=$1`, s.crypto.Digest(sessionPurpose, r.res.Session.SessionToken)).Scan(&methods); err != nil || !slices.Equal(methods, []string{"pwd", "totp"}) {
				t.Fatal("login provenance", err)
			}
		} else if !errors.Is(r.err, ErrNativeInvalidCode) {
			t.Fatal("concurrent verify", r.err)
		}
	}
	if successes != 1 {
		t.Fatal("same counter across independent transactions accepted", successes)
	}
	if _, err = s.Reauthenticate(f.ctx, strong, "", ReauthenticateInput{Method: "totp", Code: acceptedMFACode}); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("step-up replay", err)
	}
	if _, err = verify(start(), "recovery_code", confirmed.RecoveryCodes[0]); err != nil {
		t.Fatal("recovery code", err)
	}
	if _, err = verify(start(), "recovery_code", confirmed.RecoveryCodes[0]); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("recovery code replay", err)
	}
	summary, err := s.GetMySecurity(f.ctx, strong)
	if err != nil || !summary.MFAEnabled || summary.RecoveryCodesRemaining != 9 || !slices.Equal(summary.ReauthMethods, []string{"totp"}) {
		t.Fatal("security overview", err)
	}
	fresh, err := s.RegenerateRecoveryCodes(f.ctx, strong)
	if err != nil || len(fresh) != 10 {
		t.Fatal("regenerate", err)
	}
	if _, err = verify(start(), "recovery_code", confirmed.RecoveryCodes[1]); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("old batch accepted", err)
	}
	now = now.Add(11 * time.Minute)
	if err := s.DisableTOTP(f.ctx, strong); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatal("stale AAL2 authentication permitted disabling TOTP", err)
	}
	stepped, err := s.Reauthenticate(f.ctx, strong, "", ReauthenticateInput{Method: "totp", Code: code()})
	if err != nil {
		t.Fatal("TOTP step up", err)
	}
	strong = principal(stepped.SessionToken)
	if err = s.DisableTOTP(f.ctx, strong); err != nil {
		t.Fatal("disable", err)
	}
	summary, err = s.GetMySecurity(f.ctx, strong)
	if err != nil || summary.MFAEnabled || summary.RecoveryCodesRemaining != 0 {
		t.Fatal("disabled factor remains", err)
	}
}
