//go:build integration

package identity

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

func startNativeEmailFixture(t *testing.T, f *nativeIntegrationFixture, email, kind string, allowed bool) SignupChallengeResult {
	t.Helper()
	start := f.service.StartEmailCodeLogin
	if kind == "recovery" {
		start = f.service.StartPasswordRecovery
	}
	result, err := start(f.ctx, SignupStartInput{Email: email, DestinationAllowed: allowed, RemoteAddress: "127.0.0.1:12345"})
	if err != nil {
		t.Fatal(err)
	}
	var transactionID uuid.UUID
	if f.pool.QueryRow(f.ctx, `SELECT transaction_id FROM tutorhub.auth_challenges WHERE id=$1`, result.ChallengeID).Scan(&transactionID) != nil {
		t.Fatal("read email transaction fixture")
	}
	f.transactions = append(f.transactions, transactionID)
	return result
}

func seedNativeEmailOIDCUser(t *testing.T, f *nativeIntegrationFixture) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	email := "email-oidc-" + id.String() + "@example.test"
	f.emails = append(f.emails, email)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES($1,$2,'Existing OIDC Person')`, id, email); err != nil {
		t.Fatal("seed OIDC user")
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.identities(user_id,provider,subject,email_at_provider,email_verified) VALUES($1,'https://issuer.example.test',$2,$3,true)`, id, id.String(), email); err != nil {
		t.Fatal("seed OIDC identity")
	}
	return id, email
}

func TestNativeEmailPostgresLoginOIDCPasswordDummyAndAdminCap(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	userID, email := seedNativeEmailOIDCUser(t, f)
	start := startNativeEmailFixture(t, f, email, "login", true)
	result, err := f.service.VerifyEmailCodeLogin(f.ctx, start.Cookie.Value, EmailCodeVerifyInput{ChallengeID: start.ChallengeID, Code: "420381", KeepSignedIn: true})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeIntegrationSession(t, f, result, true, 30*24*time.Hour, []string{"email_code"})
	var verified bool
	var identities, credentials int
	if f.pool.QueryRow(f.ctx, `SELECT email_verified_at IS NOT NULL FROM tutorhub.users WHERE id=$1`, userID).Scan(&verified) != nil || !verified {
		t.Fatal("email OTP did not verify OIDC user's email")
	}
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.identities WHERE user_id=$1 AND kind='email' AND provider='urn:tutorhub:email' AND subject=$1::text AND email_verified AND status='active'`, userID).Scan(&identities) != nil || identities != 1 {
		t.Fatal("email OTP did not create exactly one native identity")
	}
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.user_password_credentials WHERE user_id=$1`, userID).Scan(&credentials) != nil || credentials != 0 {
		t.Fatal("passwordless login created an unsolicited credential")
	}
	if _, err := f.service.VerifyEmailCodeLogin(f.ctx, start.Cookie.Value, EmailCodeVerifyInput{ChallengeID: start.ChallengeID, Code: "420381"}); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("completed email transaction replayed")
	}

	nativeEmail := "email-password-" + uuid.NewString() + "@example.test"
	signup := f.start(t, nativeEmail)
	if _, err := f.service.CompleteEmailSignup(f.ctx, signup.Cookie.Value, nativeIntegrationCompleteInput(signup.ChallengeID)); err != nil {
		t.Fatal(err)
	}
	nativeStart := startNativeEmailFixture(t, f, nativeEmail, "login", true)
	if _, err := f.service.VerifyEmailCodeLogin(f.ctx, nativeStart.Cookie.Value, EmailCodeVerifyInput{ChallengeID: nativeStart.ChallengeID, Code: "420381"}); err != nil {
		t.Fatal("password user could not use email OTP")
	}

	tenantID := uuid.New()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Email admin fixture')`, tenantID, "email-admin-"+uuid.NewString()); err != nil {
		t.Fatal("seed email admin tenant")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := f.pool.Exec(ctx, `DELETE FROM tutorhub.sessions WHERE active_tenant_id=$1`, tenantID); err != nil {
			t.Error("clean email admin tenant sessions")
		}
		if _, err := f.pool.Exec(ctx, `DELETE FROM tutorhub.tenants WHERE id=$1`, tenantID); err != nil {
			t.Error("clean email admin tenant")
		}
	})
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'org_admin','active')`, tenantID, userID); err != nil {
		t.Fatal("seed email admin membership")
	}
	adminStart := startNativeEmailFixture(t, f, email, "login", true)
	admin, err := f.service.VerifyEmailCodeLogin(f.ctx, adminStart.Cookie.Value, EmailCodeVerifyInput{ChallengeID: adminStart.ChallengeID, Code: "420381", KeepSignedIn: true})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeIntegrationSession(t, f, admin, false, 24*time.Hour, []string{"email_code"})

	for _, scenario := range []string{"missing", "inactive", "throttled"} {
		target := email
		if scenario == "missing" {
			target = "email-missing-" + uuid.NewString() + "@example.test"
		}
		if scenario == "inactive" {
			if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.users SET status='suspended' WHERE id=$1`, userID); err != nil {
				t.Fatal("suspend email fixture")
			}
		}
		dummy := startNativeEmailFixture(t, f, target, "login", scenario != "throttled")
		var deliverable bool
		if f.pool.QueryRow(f.ctx, `SELECT deliverable FROM tutorhub.auth_challenges WHERE id=$1`, dummy.ChallengeID).Scan(&deliverable) != nil || deliverable || dummy.CooldownSeconds != start.CooldownSeconds || dummy.ExpiresInSeconds != start.ExpiresInSeconds {
			t.Fatal("dummy response or storage differs")
		}
		if _, err := f.service.VerifyEmailCodeLogin(f.ctx, dummy.Cookie.Value, EmailCodeVerifyInput{ChallengeID: dummy.ChallengeID, Code: "420381"}); !errors.Is(err, ErrNativeInvalidCode) {
			t.Fatal("dummy email challenge authenticated")
		}
		if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.users SET status='active' WHERE id=$1`, userID); err != nil {
			t.Fatal("restore email fixture")
		}
	}

	// A real challenge issued before a suspension must not bypass the current
	// account state or create an email identity/session after the suspension.
	suspendedID, suspendedEmail := seedNativeEmailOIDCUser(t, f)
	pending := startNativeEmailFixture(t, f, suspendedEmail, "login", true)
	if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.users SET status='suspended' WHERE id=$1`, suspendedID); err != nil {
		t.Fatal("suspend account with a pending real challenge")
	}
	if _, err := f.service.VerifyEmailCodeLogin(f.ctx, pending.Cookie.Value, EmailCodeVerifyInput{ChallengeID: pending.ChallengeID, Code: "420381"}); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("pending challenge authenticated a newly suspended account")
	}
	var accountArtifacts int
	if f.pool.QueryRow(f.ctx, `SELECT
        (SELECT count(*) FROM tutorhub.identities WHERE user_id=$1 AND kind='email') +
        (SELECT count(*) FROM tutorhub.sessions WHERE user_id=$1)`, suspendedID).Scan(&accountArtifacts) != nil || accountArtifacts != 0 {
		t.Fatal("suspended account gained an email identity or session")
	}
}

func TestNativeEmailPostgresSameCodeRaceResendAndBoundAccount(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	_, email := seedNativeEmailOIDCUser(t, f)
	start := startNativeEmailFixture(t, f, email, "login", true)
	var successes atomic.Int32
	var group sync.WaitGroup
	barrier := make(chan struct{})
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-barrier
			_, err := f.service.VerifyEmailCodeLogin(f.ctx, start.Cookie.Value, EmailCodeVerifyInput{ChallengeID: start.ChallengeID, Code: "420381"})
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrNativeInvalidCode) {
				t.Error("OTP race returned storage error")
			}
		}()
	}
	close(barrier)
	group.Wait()
	if successes.Load() != 1 {
		t.Fatal("same OTP must have exactly one winner")
	}

	resendStart := startNativeEmailFixture(t, f, email, "login", true)
	old := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.auth_challenges SET created_at=$2,expires_at=$3 WHERE id=$1`, resendStart.ChallengeID, old, old.Add(EmailChallengeTTL)); err != nil {
		t.Fatal("age email challenge")
	}
	var destination string
	resend, err := f.service.ResendEmailChallenge(f.ctx, resendStart.Cookie.Value, AuthMethodFlags{EmailCodeEnabled: true}, func(_ context.Context, email string) (bool, error) { destination = email; return true, nil })
	if err != nil || destination != email || resend.ChallengeID == resendStart.ChallengeID {
		t.Fatal("resend changed immutable destination")
	}
	if _, err := f.service.VerifyEmailCodeLogin(f.ctx, resendStart.Cookie.Value, EmailCodeVerifyInput{ChallengeID: resendStart.ChallengeID, Code: "420381"}); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("old email code survived resend")
	}
	if _, err := f.service.VerifyEmailCodeLogin(f.ctx, resendStart.Cookie.Value, EmailCodeVerifyInput{ChallengeID: resend.ChallengeID, Code: "420381"}); err != nil {
		t.Fatal("resend code was unusable")
	}

	missingEmail := "email-later-" + uuid.NewString() + "@example.test"
	dummy := startNativeEmailFixture(t, f, missingEmail, "login", true)
	f.emails = append(f.emails, missingEmail)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.users(email,display_name) VALUES($1,'Later account')`, missingEmail); err != nil {
		t.Fatal("seed later account")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.auth_challenges SET created_at=$2,expires_at=$3 WHERE id=$1`, dummy.ChallengeID, old, old.Add(EmailChallengeTTL)); err != nil {
		t.Fatal("age dummy challenge")
	}
	dummyResend, err := f.service.ResendEmailChallenge(f.ctx, dummy.Cookie.Value, AuthMethodFlags{EmailCodeEnabled: true}, func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		t.Fatal(err)
	}
	var deliverable bool
	if f.pool.QueryRow(f.ctx, `SELECT deliverable FROM tutorhub.auth_challenges WHERE id=$1`, dummyResend.ChallengeID).Scan(&deliverable) != nil || deliverable {
		t.Fatal("dummy upgraded to a newly-created account")
	}
}

func TestNativeRecoveryPostgresAtomicRevocationUnlockAndNotice(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	email := "email-recovery-" + uuid.NewString() + "@example.test"
	signup := f.start(t, email)
	if _, err := f.service.CompleteEmailSignup(f.ctx, signup.Cookie.Value, nativeIntegrationCompleteInput(signup.ChallengeID)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword}); err != nil {
		t.Fatal(err)
	}
	var userID uuid.UUID
	if f.pool.QueryRow(f.ctx, `SELECT id FROM tutorhub.users WHERE email=$1`, email).Scan(&userID) != nil {
		t.Fatal("read recovery user")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.user_password_credentials SET failed_attempts=8,last_failed_at=$2,locked_until=$3,compromised_at=$2 WHERE user_id=$1`, userID, time.Now().UTC(), time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal("lock recovery user")
	}
	inbox, err := devinbox.New("test", 10, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inbox.Close)
	dispatcher, err := NewDeliveryDispatcher(f.pool, f.service.crypto, NewDevinboxSender(inbox, DeliveryEmail), DisabledSender{}, 10, foundationTestBudget(true))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = dispatcher.Close(ctx)
	})
	f.service.challenges.dispatcher = dispatcher
	start := startNativeEmailFixture(t, f, email, "recovery", true)
	input := PasswordRecoveryCompleteInput{ChallengeID: start.ChallengeID, Code: "420381", NewPassword: "New cedar sunset 75!"}
	weak := input
	weak.NewPassword = "short"
	if err := f.service.CompletePasswordRecovery(f.ctx, start.Cookie.Value, weak); !errors.Is(err, ErrPasswordPolicy) {
		t.Fatal("weak recovery password accepted")
	}
	var attempts int
	if f.pool.QueryRow(f.ctx, `SELECT attempts FROM tutorhub.auth_challenges WHERE id=$1`, start.ChallengeID).Scan(&attempts) != nil || attempts != 0 {
		t.Fatal("password policy consumed OTP attempt")
	}
	faulty := *f.service
	faulty.database = nativeFaultDatabase{Database: f.pool, fragment: "UPDATE tutorhub.sessions"}
	if err := faulty.CompletePasswordRecovery(f.ctx, start.Cookie.Value, input); !errors.Is(err, ErrAuthFoundationStorage) {
		t.Fatal("injected session revocation failure did not fail recovery")
	}
	var consumed bool
	var activeSessions int
	if f.pool.QueryRow(f.ctx, `SELECT consumed_at IS NOT NULL FROM tutorhub.auth_challenges WHERE id=$1`, start.ChallengeID).Scan(&consumed) != nil || consumed {
		t.Fatal("failed recovery consumed OTP")
	}
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.sessions WHERE user_id=$1 AND revoked_at IS NULL`, userID).Scan(&activeSessions) != nil || activeSessions != 2 {
		t.Fatal("failed recovery partially revoked sessions")
	}
	if err := f.service.CompletePasswordRecovery(f.ctx, start.Cookie.Value, input); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Close(f.ctx); err != nil {
		t.Fatal("drain password_changed notice")
	}
	var unlocked bool
	var hash string
	if f.pool.QueryRow(f.ctx, `SELECT password_hash,status='active' AND failed_attempts=0 AND locked_until IS NULL AND last_failed_at IS NULL AND compromised_at IS NULL FROM tutorhub.user_password_credentials WHERE user_id=$1`, userID).Scan(&hash, &unlocked) != nil || !unlocked {
		t.Fatal("recovery failed to reset credential lock/compromise state")
	}
	matched, _, err := f.service.passwords.Verify(f.ctx, input.NewPassword, hash)
	if err != nil || !matched {
		t.Fatal("recovery did not replace password")
	}
	var sessions, revoked int
	if f.pool.QueryRow(f.ctx, `SELECT count(*),count(*) FILTER(WHERE revoked_at IS NOT NULL AND revoked_reason='password_reset') FROM tutorhub.sessions WHERE user_id=$1`, userID).Scan(&sessions, &revoked) != nil || sessions != 2 || revoked != sessions {
		t.Fatal("recovery must revoke all existing sessions without adding one")
	}
	notices := 0
	for _, message := range inbox.List() {
		if message.NoticeKind == "password_changed" {
			notices++
			if message.Code != "" || message.Destination != email {
				t.Fatal("password notice leaked a secret or had wrong recipient")
			}
		}
	}
	if notices != 1 {
		t.Fatal("recovery must deliver one password_changed notice after successful commit")
	}
	if err := f.service.CompletePasswordRecovery(f.ctx, start.Cookie.Value, input); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("recovery replayed")
	}

	// OIDC-only and removed credentials are both recoverable after fresh OTP.
	for _, removed := range []bool{false, true} {
		id, target := seedNativeEmailOIDCUser(t, f)
		if removed {
			if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.user_password_credentials(user_id,status,created_at,updated_at) VALUES($1,'removed',$2,$2)`, id, time.Now().UTC()); err != nil {
				t.Fatal("seed removed credential")
			}
		}
		start := startNativeEmailFixture(t, f, target, "recovery", true)
		if err := f.service.CompletePasswordRecovery(f.ctx, start.Cookie.Value, PasswordRecoveryCompleteInput{ChallengeID: start.ChallengeID, Code: "420381", NewPassword: "Recovered fir woodland 95!"}); err != nil {
			t.Fatal("OIDC/removed credential recovery failed")
		}
		var active bool
		if f.pool.QueryRow(f.ctx, `SELECT status='active' FROM tutorhub.user_password_credentials WHERE user_id=$1`, id).Scan(&active) != nil || !active {
			t.Fatal("recovery did not activate credential")
		}
	}
	unknown := startNativeEmailFixture(t, f, "unknown-recovery-"+uuid.NewString()+"@example.test", "recovery", true)
	if err := f.service.CompletePasswordRecovery(f.ctx, unknown.Cookie.Value, PasswordRecoveryCompleteInput{ChallengeID: unknown.ChallengeID, Code: "420381", NewPassword: "Recovered fir woodland 95!"}); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("unregistered email recovery was accepted")
	}
}
