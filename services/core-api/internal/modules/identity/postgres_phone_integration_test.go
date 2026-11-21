//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

const fixtureMobile = "+84912345678"

type phoneIntegrationFixture struct{ *nativeIntegrationFixture }

func newPhoneIntegrationFixture(t *testing.T) *phoneIntegrationFixture {
	t.Helper()
	f := &phoneIntegrationFixture{newNativeIntegrationFixture(t)}
	f.service.config.PhoneEnabled = true
	f.service.config.PhoneAllowedCountries = []string{"VN"}
	f.service.config.PhoneStaleAfter = 60 * 24 * time.Hour
	f.service.config.PhoneReclaimAfter = 90 * 24 * time.Hour
	f.service.config.PasswordEnabled = true
	f.service.database = createPasskeyRuntimeRole(t, f.nativeIntegrationFixture)
	f.service.challenges.database = f.service.database
	return f
}

func (f *phoneIntegrationFixture) account(t *testing.T) Principal {
	t.Helper()
	return f.accountWithEmail(t, "phone-"+uuid.NewString()+"@example.test")
}

func (f *phoneIntegrationFixture) accountWithEmail(t *testing.T, email string) Principal {
	t.Helper()
	c := f.start(t, email)
	session, err := f.service.CompleteEmailSignup(f.ctx, c.Cookie.Value, nativeIntegrationCompleteInput(c.ChallengeID))
	if err != nil {
		t.Fatal("seed phone account", err)
	}
	var p Principal
	p.User.Email = email
	err = f.pool.QueryRow(f.ctx, `SELECT id,user_id,context_version,assurance_level FROM tutorhub.sessions WHERE token_hash=$1`, f.service.crypto.Digest(sessionPurpose, session.SessionToken)).Scan(&p.SessionID, &p.User.ID, &p.ContextVersion, &p.AssuranceLevel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM tutorhub.outbox_events WHERE aggregate_id=$1`,
			`DELETE FROM tutorhub.user_phone_numbers WHERE user_id=$1`,
			`DELETE FROM tutorhub.mfa_recovery_codes WHERE user_id=$1`,
			`DELETE FROM tutorhub.mfa_totp_factors WHERE user_id=$1`,
			`DELETE FROM tutorhub.webauthn_credentials WHERE user_id=$1`,
			deleteFixtureDeliveries + "(SELECT id FROM tutorhub.auth_transactions WHERE user_id=$1)",
			`DELETE FROM tutorhub.auth_transactions WHERE user_id=$1`,
		} {
			if _, err := f.pool.Exec(f.ctx, q, p.User.ID); err != nil {
				t.Error("clean phone fixture", err)
			}
		}
	})
	return p
}

func phoneTestStartInput() PhoneStartInput {
	return PhoneStartInput{PhoneNumber: fixtureMobile, CountryCode: "VN", KeepSignedIn: true, Guard: func(context.Context, pgx.Tx, NormalizedPhoneNumber, bool) (bool, error) { return true, nil }}
}

func phoneProof(start SignupChallengeResult) EmailCodeVerifyInput {
	return EmailCodeVerifyInput{ChallengeID: start.ChallengeID, Code: "420381", UserAgent: "phone integration", RemoteAddress: "127.0.0.1:12345"}
}

func (f *phoneIntegrationFixture) startClaim(t *testing.T, p Principal) SignupChallengeResult {
	t.Helper()
	r, err := f.service.StartMyPhone(f.ctx, p, phoneTestStartInput())
	if err != nil {
		t.Fatal("start phone verification", err)
	}
	return r
}

func (f *phoneIntegrationFixture) bind(t *testing.T, p Principal) *PhoneSummary {
	t.Helper()
	c := f.startClaim(t, p)
	summary, err := f.service.VerifyMyPhone(f.ctx, p, c.Cookie.Value, phoneProof(c))
	if err != nil || summary == nil {
		t.Fatal("verify phone ownership", err)
	}
	return summary
}

func (f *phoneIntegrationFixture) login(t *testing.T) SignupChallengeResult {
	t.Helper()
	r, err := f.service.StartPhoneLogin(f.ctx, phoneTestStartInput())
	if err != nil {
		t.Fatal("start phone login", err)
	}
	return r
}

func (f *phoneIntegrationFixture) age(t *testing.T, id uuid.UUID, days int) (verified time.Time) {
	t.Helper()
	err := f.pool.QueryRow(f.ctx, `UPDATE tutorhub.user_phone_numbers SET created_at=now()-$2*interval '1 day',
		verified_at=now()-$2*interval '1 day',last_used_at=now()-$2*interval '1 day',updated_at=now()-$2*interval '1 day'
		WHERE id=$1 RETURNING verified_at`, id, days).Scan(&verified)
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func TestPhonePostgresRuntimeRefreshLoginAndConcurrentReclaim(t *testing.T) {
	f := newPhoneIntegrationFixture(t)
	s := f.service
	old := f.account(t)
	number := f.bind(t, old)
	verified := f.age(t, number.ID, 61)
	state, err := s.GetMyPhone(f.ctx, old)
	if err != nil || state == nil || !state.Stale {
		t.Fatal("expected stale phone", err)
	}
	c := f.login(t)
	if _, err = s.VerifyPhoneLogin(f.ctx, c.Cookie.Value, phoneProof(c)); !errors.Is(err, ErrPhoneStale) {
		t.Fatal("expected stale refusal", err)
	}
	if _, err = s.VerifyPhoneLogin(f.ctx, c.Cookie.Value, phoneProof(c)); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("stale proof replay", err)
	}
	refreshed := f.bind(t, old)
	if refreshed.ID != number.ID || !refreshed.VerifiedAt.Equal(verified) || !refreshed.LastUsedAt.After(verified) || refreshed.Stale {
		t.Fatal("same-owner refresh changed initial verification or failed to clear stale state")
	}

	c = f.login(t)
	type outcome struct {
		res NativeSessionResult
		err error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			res, err := s.VerifyPhoneLogin(f.ctx, c.Cookie.Value, phoneProof(c))
			results <- outcome{res, err}
		}()
	}
	success := 0
	for range 2 {
		r := <-results
		if r.err == nil {
			success++
			if !r.res.Persistent {
				t.Fatal("lost keep_signed_in")
			}
		} else if !errors.Is(r.err, ErrNativeInvalidCode) {
			t.Fatal("login race", r.err)
		}
	}
	if success != 1 {
		t.Fatal("expected exactly one successful verification", success)
	}
	var sessions int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.sessions WHERE user_id=$1 AND assurance_level=1 AND auth_methods=ARRAY['phone_otp']`, old.User.ID).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatal("phone session count", sessions, err)
	}
	contenders := []Principal{f.account(t), f.account(t)}
	inUse := f.startClaim(t, contenders[0])
	if _, err = s.VerifyMyPhone(f.ctx, contenders[0], inUse.Cookie.Value, phoneProof(inUse)); !errors.Is(err, ErrPhoneInUse) {
		t.Fatal("fresh binding conflict", err)
	}
	f.age(t, number.ID, 91)
	claims := []SignupChallengeResult{f.startClaim(t, contenders[0]), f.startClaim(t, contenders[1])}
	inbox, err := devinbox.New("test", 8, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	notices, err := NewDeliveryDispatcher(s.database, s.crypto, NewDevinboxSender(inbox, DeliveryEmail), DisabledSender{}, 8, foundationTestBudget(true))
	if err != nil {
		t.Fatal(err)
	}
	defer notices.Close(f.ctx)
	s.challenges.dispatcher = notices
	type claimResult struct {
		p   Principal
		err error
	}
	race := make(chan claimResult, 2)
	for i, p := range contenders {
		go func() {
			q := claims[i]
			_, err := s.VerifyMyPhone(f.ctx, p, q.Cookie.Value, phoneProof(q))
			race <- claimResult{p, err}
		}()
	}
	var winner Principal
	success = 0
	for range 2 {
		r := <-race
		if r.err == nil {
			winner = r.p
			success++
		} else if !errors.Is(r.err, ErrPhoneInUse) {
			t.Fatal("concurrent reclaim", r.err)
		}
	}
	if success != 1 {
		t.Fatal("expected one reclaim winner", success)
	}
	if err := notices.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	s.challenges.dispatcher = nil
	messages := inbox.List()
	if len(messages) != 1 || messages[0].NoticeKind != "phone_released" || messages[0].Destination != old.User.Email || messages[0].Code != "" {
		t.Fatal("release notice must go to the original account's email exactly once")
	}
	var released, revoked, ownerSession bool
	err = f.pool.QueryRow(f.ctx, `SELECT n.status='released' AND n.release_reason='reassigned' AND n.e164_ciphertext IS NULL AND i.status='unlinked',
		EXISTS(SELECT FROM tutorhub.sessions s WHERE s.identity_id=i.id AND s.revoked_at IS NOT NULL),
		EXISTS(SELECT FROM tutorhub.sessions s JOIN tutorhub.users u ON u.id=s.user_id WHERE s.id=$2 AND u.status='active' AND s.revoked_at IS NULL)
		FROM tutorhub.user_phone_numbers n JOIN tutorhub.identities i ON i.id=n.identity_id WHERE n.id=$1`, number.ID, old.SessionID).Scan(&released, &revoked, &ownerSession)
	if err != nil || !released || !revoked || !ownerSession {
		t.Fatal("reclaim ownership/session isolation", err)
	}
	c = f.login(t)
	newSession, err := s.VerifyPhoneLogin(f.ctx, c.Cookie.Value, phoneProof(c))
	if err != nil {
		t.Fatal(err)
	}
	var newUser uuid.UUID
	if err = f.pool.QueryRow(f.ctx, `SELECT user_id FROM tutorhub.sessions WHERE token_hash=$1`, s.crypto.Digest(sessionPurpose, newSession.SessionToken)).Scan(&newUser); err != nil || newUser != winner.User.ID {
		t.Fatal("reclaim logged in to wrong user", err)
	}
	if r, err := s.RemoveMyPhone(f.ctx, winner); err != nil || r.SessionToken == "" || r.CSRFToken == "" {
		t.Fatal("remove phone and rotate session", err)
	}
	var removed bool
	if err = f.pool.QueryRow(f.ctx, `SELECT status='removed' AND removed_at IS NOT NULL AND e164_ciphertext IS NULL FROM tutorhub.user_phone_numbers WHERE user_id=$1`, winner.User.ID).Scan(&removed); err != nil || !removed {
		t.Fatal("phone removal", err)
	}
}
