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

type phoneLoginActivity struct {
	lastUsed, lastAuthenticated time.Time
	sessions                    int
}

func phoneActivity(t *testing.T, f *phoneIntegrationFixture, id uuid.UUID) phoneLoginActivity {
	t.Helper()
	var activity phoneLoginActivity
	err := f.pool.QueryRow(f.ctx, `SELECT n.last_used_at,coalesce(i.last_authenticated_at,'epoch'::timestamptz),
		(SELECT count(*) FROM tutorhub.sessions s WHERE s.identity_id=i.id)
		FROM tutorhub.user_phone_numbers n JOIN tutorhub.identities i ON i.id=n.identity_id WHERE n.id=$1`, id).
		Scan(&activity.lastUsed, &activity.lastAuthenticated, &activity.sessions)
	if err != nil {
		t.Fatal(err)
	}
	return activity
}

func phoneMFAStart(t *testing.T, f *phoneIntegrationFixture, methods []string) string {
	t.Helper()
	start := f.login(t)
	result, err := f.service.VerifyPhoneLogin(f.ctx, start.Cookie.Value, phoneProof(start))
	if err != nil || result.SessionToken != "" || result.MFA == nil || result.MFA.Cookie == nil ||
		result.MFA.Cookie.Value == start.Cookie.Value || !slices.Equal(result.MFA.Methods, methods) {
		t.Fatal("phone proof must issue only an MFA transaction", err)
	}
	state, err := f.service.PendingMFA(f.ctx, result.MFA.Cookie.Value)
	if err != nil || state.ExpiresInSeconds < 1 || state.ExpiresInSeconds > 300 || !slices.Equal(state.Methods, methods) {
		t.Fatal("wrong MFA lifetime or methods", err)
	}
	var fiveMinutes bool
	err = f.pool.QueryRow(f.ctx, `SELECT expires_at=created_at+interval '5 minutes' FROM tutorhub.auth_transactions
		WHERE kind='mfa' AND binding_hash=$1`, f.service.crypto.Digest("auth-tx-binding", result.MFA.Cookie.Value)).Scan(&fiveMinutes)
	if err != nil || !fiveMinutes {
		t.Fatal("phone second factor must have a five-minute transaction", err)
	}
	return result.MFA.Cookie.Value
}

func assertPhoneMFASession(t *testing.T, f *phoneIntegrationFixture, number uuid.UUID, session NativeSessionResult, method string, before phoneLoginActivity) {
	t.Helper()
	var level int
	var provenance []string
	var persistent, bound bool
	err := f.pool.QueryRow(f.ctx, `SELECT s.assurance_level,s.auth_methods,s.persistent,s.identity_id=n.identity_id AND s.user_id=n.user_id
		FROM tutorhub.sessions s CROSS JOIN tutorhub.user_phone_numbers n WHERE n.id=$1 AND s.token_hash=$2`,
		number, f.service.crypto.Digest(sessionPurpose, session.SessionToken)).Scan(&level, &provenance, &persistent, &bound)
	if err != nil || level != 2 || !bound || !persistent || !slices.Equal(provenance, []string{"phone_otp", method}) {
		t.Fatal("AAL2 session lost phone identity, persistence or factor provenance", err)
	}
	after := phoneActivity(t, f, number)
	if !after.lastUsed.After(before.lastUsed) || !after.lastAuthenticated.After(before.lastAuthenticated) || after.sessions != before.sessions+1 {
		t.Fatal("MFA completion did not atomically persist phone activity with the session")
	}
}

func assertPhoneSecondFactorUnavailable(t *testing.T, f *phoneIntegrationFixture) {
	t.Helper()
	start := f.login(t)
	result, err := f.service.VerifyPhoneLogin(f.ctx, start.Cookie.Value, phoneProof(start))
	if !errors.Is(err, ErrPhoneSecondFactorRequired) || result.SessionToken != "" || result.MFA != nil {
		t.Fatal("unusable enrolled factor must block phone-only login", err)
	}
	var completed bool
	err = f.pool.QueryRow(f.ctx, `SELECT state='completed' AND completed_at IS NOT NULL AND payload_ciphertext IS NULL FROM tutorhub.auth_transactions WHERE binding_hash=$1`,
		f.service.crypto.Digest("auth-tx-binding", start.Cookie.Value)).Scan(&completed)
	if err != nil || !completed {
		t.Fatal("failed-closed phone transaction still pending", err)
	}
	result, err = f.service.VerifyPhoneLogin(f.ctx, start.Cookie.Value, phoneProof(start))
	if !errors.Is(err, ErrNativeInvalidCode) || result.SessionToken != "" {
		t.Fatal("consumed phone proof replayed", err)
	}
}

func assertPhoneWeakSettings(t *testing.T, f *phoneIntegrationFixture, p Principal, claim SignupChallengeResult) {
	t.Helper()
	s := f.service
	if _, err := s.StartMyPhone(f.ctx, p, phoneTestStartInput()); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("weak session accepted by settings start", err)
	}
	if _, err := s.VerifyMyPhone(f.ctx, p, claim.Cookie.Value, phoneProof(claim)); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("settings verify did not recheck a newly active factor", err)
	}
	if _, err := s.RemoveMyPhone(f.ctx, p); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("weak session could remove phone", err)
	}
}

func TestPhonePostgresPasskeyOnlyMFAAndUnavailableFactors(t *testing.T) {
	f := newPhoneIntegrationFixture(t)
	s := f.service
	p := f.account(t)
	number := f.bind(t, p)
	claim := f.startClaim(t, p)
	const origin = "https://passkeys.example.test"
	rp, err := NewPasskeyProtocol("passkeys.example.test", "TutorHub", []string{origin})
	if err != nil {
		t.Fatal(err)
	}
	s.webauthn, s.config.MFATOTPEnabled = rp, true
	key := newVirtualPasskey(t)
	reg, err := s.BeginPasskey(f.ctx, PasskeyRegistration, &p)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := s.FinishPasskeyRegistration(f.ctx, p, reg.Cookie.Value, key.Register(t, reg.Options, origin, true, true, false, 0), "Phone test device")
	if err != nil {
		t.Fatal(err)
	}
	assertPhoneWeakSettings(t, f, p, claim)
	before := phoneActivity(t, f, number.ID)
	s.config.MFATOTPEnabled = false
	assertPhoneSecondFactorUnavailable(t, f)
	s.config.MFATOTPEnabled = true
	s.webauthn = nil
	assertPhoneSecondFactorUnavailable(t, f)
	s.webauthn = rp
	for _, cloned := range []bool{true, false} {
		if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.webauthn_credentials SET clone_warning=$2 WHERE id=$1`, credential.ID, cloned); err != nil {
			t.Fatal(err)
		}
		if cloned {
			assertPhoneSecondFactorUnavailable(t, f)
		}
	}
	binding := phoneMFAStart(t, f, []string{"passkey"})
	verify := func(binding string, uv bool) (MFAVerifyResult, error) {
		t.Helper()
		options, err := s.MFAPasskeyOptions(f.ctx, binding)
		if err != nil {
			t.Fatal(err)
		}
		var parsed protocol.PublicKeyCredentialRequestOptions
		if err := json.Unmarshal(options, &parsed); err != nil || parsed.UserVerification != protocol.VerificationRequired ||
			len(parsed.AllowedCredentials) != 1 || !bytes.Equal(parsed.AllowedCredentials[0].CredentialID, key.ID) {
			t.Fatal("second factor must require UV for the owner's credential", err)
		}
		return s.VerifyMFA(f.ctx, binding, MFAVerifyInput{Method: "passkey", Assertion: key.Assert(t, options, origin, uv, true, false, 0)})
	}
	if _, err := verify(binding, false); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("accepted non-UV second factor", err)
	}
	if got := phoneActivity(t, f, number.ID); got != before {
		t.Fatal("phone activity changed before successful MFA")
	}
	authed, err := verify(binding, true)
	if err != nil {
		t.Fatal(err)
	}
	assertPhoneMFASession(t, f, number.ID, authed.Session, "passkey", before)

	// Cross the freshness boundary while the MFA transaction is pending.
	binding = phoneMFAStart(t, f, []string{"passkey"})
	options, err := s.MFAPasskeyOptions(f.ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	f.age(t, number.ID, 61)
	aged := phoneActivity(t, f, number.ID)
	if _, err = s.VerifyMFA(f.ctx, binding, MFAVerifyInput{Method: "passkey", Assertion: key.Assert(t, options, origin, true, true, false, 0)}); !errors.Is(err, ErrAuthTransactionUnavailable) {
		t.Fatal("MFA accepted a newly stale phone binding", err)
	}
	if got := phoneActivity(t, f, number.ID); got != aged {
		t.Fatal("refused stale MFA created phone activity")
	}
}

func TestPhonePostgresTOTPMFAAndWeakSettings(t *testing.T) {
	f := newPhoneIntegrationFixture(t)
	s := f.service
	p := f.account(t)
	number := f.bind(t, p)
	s.config.MFATOTPEnabled = true
	enrolled, err := s.EnrollTOTP(f.ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(time.Second)
	s.now = func() time.Time { return now }
	code := func() string {
		t.Helper()
		c, err := totp.GenerateCode(enrolled.Secret, now)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	if _, err = s.ConfirmTOTP(f.ctx, p, code()); err != nil {
		t.Fatal(err)
	}
	// An existing AAL1 session can survive a deployment with the MFA gate off.
	s.config.MFATOTPEnabled = false
	assertPhoneSecondFactorUnavailable(t, f)
	var email string
	if err = f.pool.QueryRow(f.ctx, `SELECT email FROM tutorhub.users WHERE id=$1`, p.User.ID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	weak, err := s.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword})
	if err != nil || weak.SessionToken == "" {
		t.Fatal("create weak session during gate-off interval", err)
	}
	if err = f.pool.QueryRow(f.ctx, `SELECT id,context_version,assurance_level FROM tutorhub.sessions WHERE token_hash=$1`,
		s.crypto.Digest(sessionPurpose, weak.SessionToken)).Scan(&p.SessionID, &p.ContextVersion, &p.AssuranceLevel); err != nil || p.AssuranceLevel != 1 {
		t.Fatal(err)
	}
	claim := f.startClaim(t, p)
	s.config.MFATOTPEnabled = true
	assertPhoneWeakSettings(t, f, p, claim)
	now = now.Add(30 * time.Second)
	before := phoneActivity(t, f, number.ID)
	binding := phoneMFAStart(t, f, []string{"totp", "recovery_code"})
	if got := phoneActivity(t, f, number.ID); got != before {
		t.Fatal("TOTP pending prematurely refreshed phone")
	}
	result, err := s.VerifyMFA(f.ctx, binding, MFAVerifyInput{Method: "totp", Code: code()})
	if err != nil {
		t.Fatal(err)
	}
	assertPhoneMFASession(t, f, number.ID, result.Session, "totp", before)
}
