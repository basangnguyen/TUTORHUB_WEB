//go:build integration

package identity

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func seedFacebookRegistration(t *testing.T, f *nativeIntegrationFixture, hint string) (string, FederationIdentityPayload) {
	t.Helper()
	binding, err := f.service.crypto.RandomToken()
	if err != nil {
		t.Fatal(err)
	}
	p := FederationIdentityPayload{ProviderSlug: "facebook", Issuer: "https://facebook.example.test", Subject: uuid.NewString(), Email: hint, ReturnTo: "/app/home"}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := f.service.crypto.Encrypt(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	now := f.service.now().UTC()
	hash := f.service.crypto.Digest(registrationBindingPurpose, binding)
	repository := NewPostgresRepository(f.pool, 20*time.Second, policy.NewEngine())
	result, err := repository.CompleteFederatedLogin(f.ctx, FederationLoginParams{
		ProviderSlug: "facebook", Flow: StoredFlow{Purpose: flowPurposeLogin},
		Claims:                  ProviderClaims{ProviderSlug: "facebook", Issuer: p.Issuer, Subject: p.Subject, Email: p.Email},
		RegistrationBindingHash: hash, LinkPayloadCiphertext: encrypted,
		ProviderSubjectDigest: f.service.crypto.ProviderSubjectDigest(p.Issuer, p.Subject),
		LinkExpiresAt:         now.Add(AuthTransactionTTL), Metadata: SessionMetadata{CreatedAt: now},
	})
	if err != nil || !result.RegistrationPending || result.LinkPending || result.Principal.SessionID != uuid.Nil {
		t.Fatalf("new subject must enter registration: %v", err)
	}
	var id uuid.UUID
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM tutorhub.auth_transactions WHERE binding_hash=$1`, hash).Scan(&id); err != nil {
		t.Fatal("read pending registration", err)
	}
	f.transactions = append(f.transactions, id)
	return binding, p
}

func TestFacebookRegistrationAtomicCompletionAndRecovery(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	binding, provider := seedFacebookRegistration(t, f, "suggested@example.test")
	view, err := f.service.PreviewRegistration(f.ctx, binding)
	if err != nil || view.MaskedEmail != "s***@example.test" || view.Provider != "facebook" || view.ExpiresInSeconds > 900 {
		t.Fatal("preview did not mask the Meta email")
	}
	email := "facebook-" + uuid.NewString() + "@example.test"
	f.emails = append(f.emails, email)
	start, err := f.service.StartRegistration(f.ctx, binding, SignupStartInput{Email: email, DestinationAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	input := RegistrationCompleteInput{ChallengeID: start.ChallengeID, Code: "000000", TermsVersion: f.service.config.LegalTermsVersion, KeepSignedIn: true}
	if _, err := f.service.CompleteRegistration(f.ctx, binding, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("wrong OTP accepted")
	}
	var attempts int
	if f.pool.QueryRow(f.ctx, `SELECT attempts FROM tutorhub.auth_challenges WHERE id=$1`, start.ChallengeID).Scan(&attempts) != nil || attempts != 1 {
		t.Fatal("failed OTP attempt rolled back")
	}
	input.Code = "420381"
	if _, err := f.service.CompleteRegistration(f.ctx, binding, input); !errors.Is(err, ErrFederationSignupDisabled) {
		t.Fatal("disabled signup allowed a new user")
	}
	input.SignupEnabled = true
	faulty := *f.service
	faulty.database = nativeFaultDatabase{Database: f.pool, fragment: "INSERT INTO tutorhub.user_legal_acceptances"}
	if _, err := faulty.CompleteRegistration(f.ctx, binding, input); !errors.Is(err, ErrAuthFoundationStorage) {
		t.Fatal("injected failure did not roll back registration")
	}
	var remaining int
	if f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM tutorhub.users WHERE email=$1)+(SELECT count(*) FROM tutorhub.identities WHERE provider=$2 AND subject=$3)
		+(SELECT count(*) FROM tutorhub.auth_challenges WHERE id=$4 AND consumed_at IS NOT NULL)`, email, provider.Issuer, provider.Subject, start.ChallengeID).Scan(&remaining) != nil || remaining != 0 {
		t.Fatal("partial signup persisted")
	}

	type completion struct {
		result RegistrationResult
		err    error
	}
	done := make(chan completion, 2)
	barrier := make(chan struct{})
	for range 2 {
		go func() {
			<-barrier
			r, err := f.service.CompleteRegistration(f.ctx, binding, input)
			done <- completion{r, err}
		}()
	}
	close(barrier)
	wins := 0
	for range 2 {
		run := <-done
		if run.err != nil {
			if !errors.Is(run.err, ErrAuthTransactionUnavailable) {
				t.Fatalf("parallel completion: %v", run.err)
			}
			continue
		}
		wins++
		if run.result.LinkBinding != "" {
			t.Fatal("new signup returned link")
		}
		assertNativeIntegrationSession(t, f, run.result.Session, true, 30*24*time.Hour, []string{"fed", "email_code"})
	}
	if wins != 1 {
		t.Fatalf("%d completions won", wins)
	}
	if _, err := f.service.CompleteRegistration(f.ctx, binding, input); !errors.Is(err, ErrAuthTransactionUnavailable) {
		t.Fatal("registration was reusable")
	}
	var emailVerified bool
	var methods, acceptances, sessions int
	err = f.pool.QueryRow(f.ctx, `SELECT u.email_verified_at IS NOT NULL,
		(SELECT count(*) FROM tutorhub.identities i WHERE i.user_id=u.id AND ((i.provider_slug='email' AND i.email_verified AND i.email_at_provider=$1)
		OR (i.provider_slug='facebook' AND NOT i.email_verified AND i.email_at_provider=$4 AND i.subject=$2))),
		(SELECT count(*) FROM tutorhub.user_legal_acceptances a WHERE a.user_id=u.id AND version=$3),
		(SELECT count(*) FROM tutorhub.sessions s JOIN tutorhub.identities i ON s.identity_id=i.id WHERE s.user_id=u.id AND i.provider_slug='facebook')
		FROM tutorhub.users u WHERE u.email=$1`, email, provider.Subject, input.TermsVersion, provider.Email).Scan(&emailVerified, &methods, &acceptances, &sessions)
	if err != nil || !emailVerified || methods != 2 || acceptances != 2 || sessions != 1 {
		t.Fatal("atomic registration records did not match expected provenance")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.identities SET status='unlinked',unlinked_at=now() WHERE provider=$1 AND subject=$2`, provider.Issuer, provider.Subject); err != nil {
		t.Fatal(err)
	}
	login := startNativeEmailFixture(t, f, email, "login", true)
	if _, err := f.service.VerifyEmailCodeLogin(f.ctx, login.Cookie.Value, EmailCodeVerifyInput{ChallengeID: login.ChallengeID, Code: "420381"}); err != nil {
		t.Fatal("verified email recovery lost after unlink", err)
	}
}

func TestFacebookRegistrationResendCollisionCancelAndExpiry(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	owner, email := seedNativeEmailOIDCUser(t, f)
	binding, _ := seedFacebookRegistration(t, f, "")
	view, err := f.service.PreviewRegistration(f.ctx, binding)
	if err != nil || view.MaskedEmail != "" {
		t.Fatal("missing hint failed")
	}
	start, err := f.service.StartRegistration(f.ctx, binding, SignupStartInput{Email: email, DestinationAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	f.service.now = func() time.Time { return now }
	f.service.challenges.now = f.service.now
	allowed := func(_ context.Context, dest string) (bool, error) {
		if dest != email {
			t.Fatal("resend used wrong address")
		}
		return true, nil
	}
	if _, err := f.service.ResendRegistration(f.ctx, binding, allowed); !errors.Is(err, ErrAuthChallengeThrottled) {
		t.Fatal("cooldown bypass")
	}
	now = now.Add(61 * time.Second)
	next, err := f.service.ResendRegistration(f.ctx, binding, allowed)
	if err != nil || next.ChallengeID == start.ChallengeID {
		t.Fatal("resend failed", err)
	}
	input := RegistrationCompleteInput{ChallengeID: start.ChallengeID, Code: "420381", TermsVersion: f.service.config.LegalTermsVersion}
	if _, err := f.service.CompleteRegistration(f.ctx, binding, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("old OTP remained active")
	}
	input.ChallengeID = next.ChallengeID
	result, err := f.service.CompleteRegistration(f.ctx, binding, input)
	if err != nil || result.LinkBinding == "" || result.Session.SessionToken != "" {
		t.Fatal("existing email must create only link intent, even when signup is disabled", err)
	}
	var link, linkedOwner uuid.UUID
	var sessions, fb int
	if f.pool.QueryRow(f.ctx, `SELECT t.id,t.user_id,(SELECT count(*) FROM tutorhub.sessions WHERE user_id=t.user_id),
		(SELECT count(*) FROM tutorhub.identities WHERE user_id=t.user_id AND provider_slug='facebook') FROM tutorhub.auth_transactions t
		WHERE binding_hash=$1 AND kind='link_intent' AND state='pending' AND octet_length(provider_subject_digest)=32`,
		f.service.crypto.Digest(linkBindingPurpose, result.LinkBinding)).Scan(&link, &linkedOwner, &sessions, &fb) != nil || linkedOwner != owner || sessions != 0 || fb != 0 {
		t.Fatal("link intent changed existing account")
	}
	f.transactions = append(f.transactions, link)

	binding, _ = seedFacebookRegistration(t, f, "invalid hint")
	if err := f.service.CancelRegistration(f.ctx, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.PreviewRegistration(f.ctx, binding); !errors.Is(err, ErrAuthTransactionUnavailable) {
		t.Fatal("cancel did not invalidate pending registration")
	}

	binding, _ = seedFacebookRegistration(t, f, "")
	dummy, err := f.service.StartRegistration(f.ctx, binding, SignupStartInput{Email: email, DestinationAllowed: false})
	if err != nil {
		t.Fatal(err)
	}
	input.ChallengeID = dummy.ChallengeID
	if _, err := f.service.CompleteRegistration(f.ctx, binding, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("throttled destination's OTP accepted")
	}
	binding, _ = seedFacebookRegistration(t, f, "")
	start, err = f.service.StartRegistration(f.ctx, binding, SignupStartInput{Email: email, DestinationAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	input.ChallengeID = start.ChallengeID
	now = now.Add(EmailChallengeTTL + time.Second)
	if _, err := f.service.CompleteRegistration(f.ctx, binding, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("expired OTP accepted")
	}
	now = now.Add(AuthTransactionTTL)
	if _, err := f.service.PreviewRegistration(f.ctx, binding); !errors.Is(err, ErrAuthTransactionUnavailable) {
		t.Fatal("expired registration exposed a hint")
	}
}
