//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const nativeIntegrationPassword = "Cedar sunrise 42! Orbit"

type nativeIntegrationFixture struct {
	ctx          context.Context
	pool         *pgxpool.Pool
	service      *NativeAuthService
	emails       []string
	transactions []uuid.UUID
}

func newNativeIntegrationFixture(t *testing.T) *nativeIntegrationFixture {
	t.Helper()
	ctx, pool, _ := authSchemaTestDatabase(t)
	crypto, err := NewCrypto(bytes.Repeat([]byte{8}, 32))
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := NewPasswordService(PasswordOptions{MinimumLength: 8, MaxConcurrency: 2, BreachMode: "off"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewNativeAuthService(pool, crypto, passwords, nil, NativeAuthConfig{CookieSecure: true, LegalTermsVersion: "2026-11-16-v2.0", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 48 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour, QueryTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	service.challenges.code = func() (string, error) { return "420381", nil }
	f := &nativeIntegrationFixture{ctx: ctx, pool: pool, service: service}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, id := range f.transactions {
			if _, err := pool.Exec(cleanup, `DELETE FROM tutorhub.auth_transactions WHERE id=$1`, id); err != nil {
				t.Error("clean disposable signup transaction")
			}
		}
		for _, email := range f.emails {
			if _, err := pool.Exec(cleanup, `DELETE FROM tutorhub.users WHERE email=$1`, email); err != nil {
				t.Error("clean disposable native user")
			}
		}
	})
	return f
}

func (f *nativeIntegrationFixture) start(t *testing.T, email string) SignupChallengeResult {
	t.Helper()
	f.emails = append(f.emails, email)
	result, err := f.service.StartEmailSignup(f.ctx, SignupStartInput{Email: email, RemoteAddress: "127.0.0.1:12345", DestinationAllowed: true})
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if f.pool.QueryRow(f.ctx, `SELECT id FROM tutorhub.auth_transactions WHERE binding_hash=$1`, f.service.crypto.Digest("auth-tx-binding", result.Cookie.Value)).Scan(&id) != nil {
		t.Fatal("read signup transaction")
	}
	f.transactions = append(f.transactions, id)
	return result
}

func nativeIntegrationCompleteInput(challengeID uuid.UUID) SignupCompleteInput {
	return SignupCompleteInput{ChallengeID: challengeID, Code: "420381", DisplayName: "Native Student", Password: nativeIntegrationPassword, TermsVersion: "2026-11-16-v2.0", KeepSignedIn: true, UserAgent: "native-auth-integration", RemoteAddress: "127.0.0.1:12345"}
}

func TestNativePostgresSignupSessionLoginAndAdministratorCap(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	email := "native-" + uuid.NewString() + "@example.test"
	start := f.start(t, email)
	result, err := f.service.CompleteEmailSignup(f.ctx, start.Cookie.Value, nativeIntegrationCompleteInput(start.ChallengeID))
	if err != nil {
		t.Fatal(err)
	}
	var userID, identityID uuid.UUID
	var verified bool
	if f.pool.QueryRow(f.ctx, `SELECT u.id,i.id,u.email_verified_at IS NOT NULL FROM tutorhub.users u JOIN tutorhub.identities i ON i.user_id=u.id
		WHERE u.email=$1 AND i.kind='email' AND i.provider='urn:tutorhub:email' AND i.email_verified=true AND i.subject=u.id::text AND i.provider_slug='email'`, email).Scan(&userID, &identityID, &verified) != nil || !verified {
		t.Fatal("native verified identity was not created")
	}
	var legalCount int
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.user_legal_acceptances WHERE user_id=$1 AND version=$2 AND octet_length(ip_bucket_hash)=32`, userID, "2026-11-16-v2.0").Scan(&legalCount) != nil || legalCount != 2 {
		t.Fatal("both legal acceptances must commit")
	}
	assertNativeIntegrationSession(t, f, result, true, 30*24*time.Hour, []string{"pwd", "email_code"})
	if _, err := f.service.CompleteEmailSignup(f.ctx, start.Cookie.Value, nativeIntegrationCompleteInput(start.ChallengeID)); !errors.Is(err, ErrAuthTransactionUnavailable) {
		t.Fatal("signup completion replayed")
	}
	login, err := f.service.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword, KeepSignedIn: true})
	if err != nil {
		t.Fatal(err)
	}
	assertNativeIntegrationSession(t, f, login, true, 30*24*time.Hour, []string{"pwd"})
	if login.SessionToken == result.SessionToken || login.CSRFToken == result.CSRFToken {
		t.Fatal("login reused a session credential")
	}

	tenantID := uuid.New()
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Native auth test')`, tenantID, "native-"+uuid.NewString()); err != nil {
		t.Fatal("insert native test tenant")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = f.pool.Exec(ctx, `DELETE FROM tutorhub.tenants WHERE id=$1`, tenantID)
	})
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'org_admin','active')`, tenantID, userID); err != nil {
		t.Fatal("insert native test admin")
	}
	adminLogin, err := f.service.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword, KeepSignedIn: true})
	if err != nil {
		t.Fatal(err)
	}
	// Config above allows 48 hours for ordinary sessions; administrators remain
	// hard-capped at 24 hours and never receive a persistent session.
	assertNativeIntegrationSession(t, f, adminLogin, false, 24*time.Hour, []string{"pwd"})

	for range 5 {
		if _, err := f.service.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: "Wrong credential 293!"}); !errors.Is(err, ErrNativeInvalidCredentials) {
			t.Fatal("incorrect password was accepted")
		}
	}
	var attempts int
	var locked bool
	if f.pool.QueryRow(f.ctx, `SELECT failed_attempts,locked_until>now() FROM tutorhub.user_password_credentials WHERE user_id=$1`, userID).Scan(&attempts, &locked) != nil || attempts != 5 || !locked {
		t.Fatal("password account lockout was not persisted")
	}
	if _, err := f.service.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword}); !errors.Is(err, ErrNativeInvalidCredentials) {
		t.Fatal("locked account bypassed dummy rejection")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.user_password_credentials SET locked_until=$2 WHERE user_id=$1`, userID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal("expire fixture lock")
	}
	if _, err := f.service.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword}); err != nil {
		t.Fatal(err)
	}
	if f.pool.QueryRow(f.ctx, `SELECT failed_attempts,locked_until IS NOT NULL FROM tutorhub.user_password_credentials WHERE user_id=$1`, userID).Scan(&attempts, &locked) != nil || attempts != 0 || locked {
		t.Fatal("successful password login did not reset lock state")
	}
}

func assertNativeIntegrationSession(t *testing.T, f *nativeIntegrationFixture, result NativeSessionResult, persistent bool, absolute time.Duration, methods []string) {
	t.Helper()
	var storedPersistent bool
	var seconds float64
	var storedMethods []string
	var assurance int
	if f.pool.QueryRow(f.ctx, `SELECT persistent,extract(epoch FROM absolute_expires_at-created_at)::float8,auth_methods,assurance_level FROM tutorhub.sessions WHERE token_hash=$1`, f.service.crypto.Digest(sessionPurpose, result.SessionToken)).Scan(&storedPersistent, &seconds, &storedMethods, &assurance) != nil {
		t.Fatal("read native session")
	}
	if storedPersistent != persistent || result.Persistent != persistent || seconds != absolute.Seconds() || strings.Join(storedMethods, ",") != strings.Join(methods, ",") || assurance != 1 {
		t.Fatal("native session policy mismatch")
	}
}

type nativeFaultDatabase struct {
	Database
	fragment string
}
type nativeFaultTransaction struct {
	pgx.Tx
	fragment string
}

func (db nativeFaultDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := db.Database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return nativeFaultTransaction{Tx: tx, fragment: db.fragment}, nil
}
func (tx nativeFaultTransaction) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, tx.fragment) {
		return pgconn.CommandTag{}, errors.New("injected atomic write failure")
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

func TestNativePostgresAtomicRollbackAndSameEmailRace(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	email := "native-rollback-" + uuid.NewString() + "@example.test"
	start := f.start(t, email)
	faulty := *f.service
	faulty.database = nativeFaultDatabase{Database: f.pool, fragment: "INSERT INTO tutorhub.user_legal_acceptances"}
	if _, err := faulty.CompleteEmailSignup(f.ctx, start.Cookie.Value, nativeIntegrationCompleteInput(start.ChallengeID)); !errors.Is(err, ErrAuthFoundationStorage) {
		t.Fatal("injected legal write failure did not fail signup")
	}
	var users int
	var consumed bool
	var state string
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.users WHERE email=$1`, email).Scan(&users) != nil || users != 0 {
		t.Fatal("failed signup left a user/credential/identity graph")
	}
	if f.pool.QueryRow(f.ctx, `SELECT c.consumed_at IS NOT NULL,t.state FROM tutorhub.auth_challenges c JOIN tutorhub.auth_transactions t ON t.id=c.transaction_id WHERE c.id=$1`, start.ChallengeID).Scan(&consumed, &state) != nil || consumed || state != "pending" {
		t.Fatal("failed signup consumed its OTP/transaction")
	}
	if _, err := f.service.CompleteEmailSignup(f.ctx, start.Cookie.Value, nativeIntegrationCompleteInput(start.ChallengeID)); err != nil {
		t.Fatal("rolled-back OTP must remain usable")
	}

	raceEmail := "native-race-" + uuid.NewString() + "@example.test"
	first, second := f.start(t, raceEmail), f.start(t, raceEmail)
	var successes atomic.Int32
	var group sync.WaitGroup
	barrier := make(chan struct{})
	for _, challenge := range []SignupChallengeResult{first, second} {
		group.Add(1)
		go func(challenge SignupChallengeResult) {
			defer group.Done()
			<-barrier
			_, err := f.service.CompleteEmailSignup(f.ctx, challenge.Cookie.Value, nativeIntegrationCompleteInput(challenge.ChallengeID))
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrNativeSignupUnavailable) {
				t.Error("same-email race returned a non-generic error")
			}
		}(challenge)
	}
	close(barrier)
	group.Wait()
	if successes.Load() != 1 {
		t.Fatal("same-email signup race must have exactly one winner")
	}
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.users WHERE email=$1`, raceEmail).Scan(&users) != nil || users != 1 {
		t.Fatal("same-email signup race created duplicate users")
	}
}

func TestNativePostgresResendDummyAndVerifiedInvitationAcceptance(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	email := "native-invite-" + uuid.NewString() + "@example.test"
	start := f.start(t, email)
	// Advance creation timestamps only in the disposable fixture to test resend
	// without wall-clock sleeps or SQL unknown-parameter interval arithmetic.
	old := time.Now().Add(-2 * time.Minute)
	if _, err := f.pool.Exec(f.ctx, `UPDATE tutorhub.auth_challenges SET created_at=$2,expires_at=$3 WHERE id=$1`, start.ChallengeID, old, old.Add(10*time.Minute)); err != nil {
		t.Fatal("age resend fixture")
	}
	var destination string
	resend, err := f.service.ResendSignupChallenge(f.ctx, start.Cookie.Value, func(_ context.Context, value string) (bool, error) { destination = value; return true, nil })
	if err != nil || destination != email || resend.ChallengeID == start.ChallengeID {
		t.Fatal("resend lost encrypted immutable destination")
	}
	if _, err := f.service.CompleteEmailSignup(f.ctx, start.Cookie.Value, nativeIntegrationCompleteInput(start.ChallengeID)); !errors.Is(err, ErrNativeSignupUnavailable) {
		t.Fatal("resend did not invalidate original code")
	}
	result, err := f.service.CompleteEmailSignup(f.ctx, start.Cookie.Value, nativeIntegrationCompleteInput(resend.ChallengeID))
	if err != nil {
		t.Fatal(err)
	}
	existing := f.start(t, email)
	if existing.CooldownSeconds != start.CooldownSeconds || existing.ExpiresInSeconds != start.ExpiresInSeconds {
		t.Fatal("existing-account response shape differs")
	}
	var deliverable bool
	if f.pool.QueryRow(f.ctx, `SELECT deliverable FROM tutorhub.auth_challenges WHERE id=$1`, existing.ChallengeID).Scan(&deliverable) != nil || deliverable {
		t.Fatal("existing account did not get dummy challenge")
	}
	if _, err := f.service.CompleteEmailSignup(f.ctx, existing.Cookie.Value, nativeIntegrationCompleteInput(existing.ChallengeID)); !errors.Is(err, ErrNativeSignupUnavailable) {
		t.Fatal("existing email account was auto-merged")
	}

	var userID, sessionID uuid.UUID
	if f.pool.QueryRow(f.ctx, `SELECT user_id,id FROM tutorhub.sessions WHERE token_hash=$1`, f.service.crypto.Digest(sessionPurpose, result.SessionToken)).Scan(&userID, &sessionID) != nil {
		t.Fatal("load invited native session")
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal("begin invitation acceptance transaction")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	tenantID, adminID, invitationID := uuid.New(), uuid.New(), uuid.New()
	adminEmail := "native-inviter-" + uuid.NewString() + "@example.test"
	if _, err := tx.Exec(f.ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES($1,$2,'Inviter')`, adminID, adminEmail); err != nil {
		t.Fatal("insert inviter")
	}
	if _, err := tx.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Invitation native test')`, tenantID, "native-"+uuid.NewString()); err != nil {
		t.Fatal("insert invitation tenant")
	}
	if _, err := tx.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'org_admin','active')`, tenantID, adminID); err != nil {
		t.Fatal("insert inviter membership")
	}
	tokenHash := f.service.crypto.Digest(membershipInvitationTokenPurpose, "native-invitation-test")
	now := time.Now().UTC()
	if _, err := tx.Exec(f.ctx, `INSERT INTO tutorhub.membership_invitations(id,tenant_id,email,intended_role,token_hash,expires_at,invited_by,created_at,updated_at)
		VALUES($1,$2,$3,'student',$4,$5,$6,$7,$7)`, invitationID, tenantID, email, tokenHash, now.Add(time.Hour), adminID, now); err != nil {
		t.Fatal("insert native invitation")
	}
	requestContext, _ := requestmeta.New(f.ctx, "native-accept-test", "", "", now)
	requestmeta.SetPrincipal(requestContext, userID, uuid.Nil)
	repository := NewPostgresRepository(tx, 5*time.Second, policy.NewEngine())
	accepted, err := repository.AcceptMembershipInvitation(requestContext, AcceptMembershipInvitationParams{SessionID: sessionID, UserID: userID, TokenHash: tokenHash, AcceptedAt: now})
	if err != nil || accepted.Invitation.Status != MembershipInvitationAccepted {
		t.Fatal("verified urn:tutorhub:email identity could not accept membership invitation")
	}
	if accepted.Principal.IdentityID == uuid.Nil || strings.Join(accepted.Principal.AuthMethods, ",") != "pwd,email_code" {
		t.Fatal("native identity authentication metadata was not preserved through invitation acceptance")
	}
	var memberships int
	if tx.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.memberships WHERE tenant_id=$1 AND user_id=$2 AND role='student' AND status='active'`, tenantID, userID).Scan(&memberships) != nil || memberships != 1 {
		t.Fatal("native invitation membership missing")
	}
}
