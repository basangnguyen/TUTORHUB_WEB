//go:build integration

package identity

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
)

func TestMFAPostgresPasskeyRecoveryAndFirstFactorPolicy(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	s := f.service
	const origin = "https://passkeys.example.test"
	rp, err := NewPasskeyProtocol("passkeys.example.test", "TutorHub", []string{origin})
	if err != nil {
		t.Fatal(err)
	}
	s.webauthn, s.config.MFATOTPEnabled = rp, true
	_, email := seedNativeEmailOIDCUser(t, f)
	principal := func(token string) Principal {
		t.Helper()
		var p Principal
		if err := f.pool.QueryRow(f.ctx, `SELECT s.id,s.user_id,s.context_version,s.assurance_level,u.display_name,
			GREATEST(s.auth_time,COALESCE(s.reauthenticated_at,s.auth_time)) FROM tutorhub.sessions s JOIN tutorhub.users u ON u.id=s.user_id WHERE s.token_hash=$1`, s.crypto.Digest(sessionPurpose, token)).
			Scan(&p.SessionID, &p.User.ID, &p.ContextVersion, &p.AssuranceLevel, &p.User.DisplayName, &p.AuthenticatedAt); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mailboxLogin := func() Principal {
		t.Helper()
		start := startNativeEmailFixture(t, f, email, "login", true)
		login, err := s.VerifyEmailCodeLogin(f.ctx, start.Cookie.Value, EmailCodeVerifyInput{ChallengeID: start.ChallengeID, Code: "420381"})
		if err != nil || login.MFA != nil {
			t.Fatal("email login before TOTP enrollment", err)
		}
		p := principal(login.SessionToken)
		if p.AssuranceLevel != 1 {
			t.Fatal("expected an AAL1 mailbox session")
		}
		return p
	}
	p := mailboxLogin()
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM tutorhub.outbox_events WHERE aggregate_id=$1`, `DELETE FROM tutorhub.webauthn_credentials WHERE user_id=$1`, `DELETE FROM tutorhub.mfa_recovery_codes WHERE user_id=$1`, `DELETE FROM tutorhub.mfa_totp_factors WHERE user_id=$1`} {
			if _, err := f.pool.Exec(f.ctx, q, p.User.ID); err != nil {
				t.Error(err)
			}
		}
	})
	s.database = createPasskeyRuntimeRole(t, f)
	begin := func(purpose PasskeyPurpose, owner *Principal) PasskeyOptions {
		t.Helper()
		o, err := s.BeginPasskey(f.ctx, purpose, owner)
		if err != nil {
			t.Fatal(err)
		}
		// Discoverable ceremonies are initially ownerless; track their cleanup explicitly.
		var id uuid.UUID
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM tutorhub.auth_transactions WHERE binding_hash=$1`, s.crypto.Digest("auth-tx-binding", o.Cookie.Value)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		f.transactions = append(f.transactions, id)
		return o
	}
	key := newVirtualPasskey(t)
	pendingTOTP, err := s.EnrollTOTP(f.ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	spare := newVirtualPasskey(t)
	outstanding := begin(PasskeyRegistration, &p)
	o := begin(PasskeyRegistration, &p)
	original, err := s.FinishPasskeyRegistration(f.ctx, p, o.Cookie.Value, key.Register(t, o.Options, origin, true, true, false, 0), "Recovery device")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishPasskeyRegistration(f.ctx, p, outstanding.Cookie.Value, spare.Register(t, outstanding.Options, origin, true, true, false, 0), "Spare"); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("registration finish ignored a newly enrolled factor", err)
	}
	p = mailboxLogin()
	if _, err = s.BeginPasskey(f.ctx, PasskeyRegistration, &p); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("mailbox session started additional passkey enrollment", err)
	}
	if err = s.RevokeMyPasskey(f.ctx, p, original.ID); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("mailbox session revoked the original passkey", err)
	}
	if _, err = s.RenameMyPasskey(f.ctx, p, original.ID, "Original"); err != nil {
		t.Fatal("recent mailbox session could not rename a passkey", err)
	}
	if _, err = s.EnrollTOTP(f.ctx, p); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("mailbox session replaced pending TOTP", err)
	}
	pendingCode, err := totp.GenerateCode(pendingTOTP.Secret, s.now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConfirmTOTP(f.ctx, p, pendingCode); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("mailbox session activated a previously pending TOTP", err)
	}
	firstPassword := UpdateMyPasswordInput{NewPassword: nativeIntegrationPassword}
	if _, err = s.UpdateMyPassword(f.ctx, p, firstPassword); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("mailbox session created a first password", err)
	}
	s.config.MFATOTPEnabled = false
	compat := begin(PasskeyRegistration, &p)
	other, err := s.FinishPasskeyRegistration(f.ctx, p, compat.Cookie.Value, spare.Register(t, compat.Options, origin, true, true, false, 0), "Secondary")
	if err != nil {
		t.Fatal("flag-off passkey registration", err)
	}
	if err = s.RevokeMyPasskey(f.ctx, p, other.ID); err != nil {
		t.Fatal("flag-off passkey revocation", err)
	}
	if _, err = s.UpdateMyPassword(f.ctx, p, firstPassword); err != nil {
		t.Fatal("flag-off first password", err)
	}
	s.config.MFATOTPEnabled = true
	const replacement = "Fir orchard sunset 674!"
	recovery := startNativeEmailFixture(t, f, email, "recovery", true)
	pending, err := s.CompletePasswordRecovery(f.ctx, recovery.Cookie.Value, PasswordRecoveryCompleteInput{ChallengeID: recovery.ChallengeID, Code: "420381", NewPassword: replacement})
	if err != nil || pending == nil || !slices.Equal(pending.Methods, []string{"passkey"}) {
		t.Fatal("passkey-only recovery must defer password mutation", err)
	}
	checkPassword := func(expected string) {
		t.Helper()
		var hash string
		if err := f.pool.QueryRow(f.ctx, `SELECT password_hash FROM tutorhub.user_password_credentials WHERE user_id=$1`, p.User.ID).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		valid, _, err := s.passwords.Verify(f.ctx, expected, hash)
		if err != nil || !valid {
			t.Fatal("unexpected password generation", err)
		}
	}
	checkPassword(nativeIntegrationPassword)
	mfaOptions := func(binding string) json.RawMessage {
		t.Helper()
		options, err := s.MFAPasskeyOptions(f.ctx, binding)
		if err != nil {
			t.Fatal(err)
		}
		var parsed protocol.PublicKeyCredentialRequestOptions
		if err = json.Unmarshal(options, &parsed); err != nil || parsed.UserVerification != protocol.VerificationRequired ||
			len(parsed.AllowedCredentials) != 1 || !bytes.Equal(parsed.AllowedCredentials[0].CredentialID, key.ID) {
			t.Fatal("MFA assertion options must require UV and the account's credential", err)
		}
		return options
	}
	verify := func(binding string, verified bool) (MFAVerifyResult, error) {
		t.Helper()
		return s.VerifyMFA(f.ctx, binding, MFAVerifyInput{Method: "passkey", Assertion: key.Assert(t, mfaOptions(binding), origin, verified, true, false, 0)})
	}
	if _, err = verify(pending.Cookie.Value, false); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("MFA accepted unverified passkey", err)
	}
	checkPassword(nativeIntegrationPassword)
	reset, err := verify(pending.Cookie.Value, true)
	if err != nil || !reset.PasswordReset || reset.Session.SessionToken != "" {
		t.Fatal("passkey recovery failed or created a session", err)
	}
	checkPassword(replacement)
	var sessions int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.sessions WHERE user_id=$1 AND revoked_at IS NULL`, p.User.ID).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("password reset retained an active session", err)
	}
	login, err := s.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: replacement})
	if err != nil || login.MFA != nil {
		t.Fatal("ordinary login before TOTP enrollment", err)
	}
	p = principal(login.SessionToken)
	stepOptions := begin(PasskeyReauth, &p)
	strong, err := s.FinishPasskeyReauth(f.ctx, p, stepOptions.Cookie.Value, key.Assert(t, stepOptions.Options, origin, true, true, false, 0))
	if err != nil {
		t.Fatal("existing passkey failed step-up for factor management", err)
	}
	p = principal(strong.SessionToken)
	if p.AssuranceLevel != 2 {
		t.Fatal("verified original passkey failed to establish AAL2")
	}
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	enroll, err := s.EnrollTOTP(f.ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(enroll.Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := s.ConfirmTOTP(f.ctx, p, code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reauthenticate(f.ctx, principal(confirmed.Session.SessionToken), "", ReauthenticateInput{Method: "totp", Code: code}); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("confirmation proof reused for step-up", err)
	}
	now = now.Add(30 * time.Second)
	later, err := totp.GenerateCode(enroll.Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	step, err := s.Reauthenticate(f.ctx, principal(confirmed.Session.SessionToken), "", ReauthenticateInput{Method: "totp", Code: later})
	if err != nil || step.SessionToken == confirmed.Session.SessionToken || step.CSRFToken == confirmed.Session.CSRFToken || principal(step.SessionToken).AssuranceLevel != 2 {
		t.Fatal("TOTP step-up did not replace the AAL2 session", err)
	}
	wantMethods := []string{"totp", "recovery_code", "passkey"}
	emailStart := startNativeEmailFixture(t, f, email, "login", true)
	proof, err := s.VerifyEmailCodeLogin(f.ctx, emailStart.Cookie.Value, EmailCodeVerifyInput{ChallengeID: emailStart.ChallengeID, Code: "420381", KeepSignedIn: true})
	if err != nil || proof.MFA == nil || proof.SessionToken != "" || !slices.Equal(proof.MFA.Methods, wantMethods) {
		t.Fatal("email first factor bypassed MFA", err)
	}
	if _, err = s.VerifyMFA(f.ctx, proof.MFA.Cookie.Value, MFAVerifyInput{Method: "totp", Code: later}); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("step-up counter reused to finish email login", err)
	}
	authed, err := verify(proof.MFA.Cookie.Value, true)
	if err != nil || !authed.Session.Persistent || principal(authed.Session.SessionToken).AssuranceLevel != 2 {
		t.Fatal("account-bound passkey failed to complete email login", err)
	}
	for _, uv := range []bool{false, true} {
		opts := begin(PasskeyLogin, nil)
		proof, err = s.FinishPasskeyLogin(f.ctx, opts.Cookie.Value, key.Assert(t, opts.Options, origin, uv, true, false, 0), true, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if uv {
			if proof.MFA != nil || principal(proof.SessionToken).AssuranceLevel != 2 {
				t.Fatal("UV passkey lost its direct AAL2 grant")
			}
		} else if proof.MFA == nil || proof.SessionToken != "" || !slices.Equal(proof.MFA.Methods, wantMethods) {
			t.Fatal("non-UV passkey bypassed account MFA")
		}
	}
}
