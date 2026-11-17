//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

type settingsIntegrationFixture struct {
	ctx                       context.Context
	tx                        pgx.Tx
	service                   *NativeAuthService
	passwords                 *PasswordService
	principal                 Principal
	otherSession              uuid.UUID
	now                       time.Time
	oldTokenHash, oldCSRFHash []byte
	inbox                     *devinbox.Inbox
	dispatcher                *DeliveryDispatcher
}

func newSettingsIntegrationFixture(t *testing.T, verified bool) *settingsIntegrationFixture {
	t.Helper()
	ctx, pool, _ := authSchemaTestDatabase(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal("begin isolated settings fixture")
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	crypto, err := NewCrypto(bytes.Repeat([]byte{0x73}, 32))
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := NewPasswordService(PasswordOptions{MinimumLength: 8, MaxConcurrency: 2, BreachMode: "off"})
	if err != nil {
		t.Fatal(err)
	}
	inbox, err := devinbox.New("test", 20, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inbox.Close)
	dispatcher, err := NewDeliveryDispatcher(pool, crypto, NewDevinboxSender(inbox, DeliveryEmail), DisabledSender{}, 20, settingsAllowBudget{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = dispatcher.Close(closeCtx)
	})
	service, err := NewNativeAuthService(tx, crypto, passwords, dispatcher, NativeAuthConfig{LegalTermsVersion: "test", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 24 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour, QueryTimeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := &settingsIntegrationFixture{ctx: ctx, tx: tx, service: service, passwords: passwords, now: now, inbox: inbox, dispatcher: dispatcher}
	service.now = func() time.Time { return f.now }
	userID, identityID, sessionID, otherSession := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	email := "settings-" + userID.String() + "@example.test"
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.users(id,email,display_name) VALUES($1,$2,'Settings fixture')`, userID, email); err != nil {
		t.Fatal("seed settings user")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.identities(id,user_id,provider,subject,email_at_provider,email_verified,last_authenticated_at)
        VALUES($1,$2,'urn:fixture:oidc',$3,$4,$5,$6)`, identityID, userID, userID.String(), email, verified, now); err != nil {
		t.Fatal("seed verified OIDC identity")
	}
	f.oldTokenHash = crypto.Digest(sessionPurpose, "settings-old-"+sessionID.String())
	f.oldCSRFHash = crypto.Digest(csrfPurpose, "settings-old-"+sessionID.String())
	for _, id := range []uuid.UUID{sessionID, otherSession} {
		hash, csrf := crypto.Digest(sessionPurpose, id.String()), crypto.Digest(csrfPurpose, id.String())
		if id == sessionID {
			hash, csrf = f.oldTokenHash, f.oldCSRFHash
		}
		if _, err := tx.Exec(ctx, `INSERT INTO tutorhub.sessions(id,user_id,identity_id,token_hash,csrf_token_hash,created_at,last_seen_at,expires_at,absolute_expires_at,auth_time,persistent,auth_methods)
            VALUES($1,$2,$3,$4,$5,$6,$6,$7,$8,$6,true,ARRAY['fed'])`, id, userID, identityID, hash, csrf, now, now.Add(8*time.Hour), now.Add(30*24*time.Hour)); err != nil {
			t.Fatal("seed settings sessions")
		}
	}
	f.principal = Principal{User: User{ID: userID, Email: email, DisplayName: "Settings fixture"}, SessionID: sessionID, IdentityID: identityID, ContextVersion: 1, AuthenticatedAt: now}
	f.otherSession = otherSession
	return f
}

type settingsAllowBudget struct{}

func (settingsAllowBudget) AllowDelivery(context.Context, DeliveryChannel) bool { return true }

func TestNativePostgresPasswordSettingsFirstPasswordRotatesAndNotifies(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	before, err := f.service.GetMyPasswordStatus(f.ctx, f.principal)
	if err != nil || before.HasPassword || before.UpdatedAt != nil {
		t.Fatal("OIDC-only account reported an active password")
	}
	password := "Cedar river 43! Orbit"
	result, err := f.service.UpdateMyPassword(f.ctx, f.principal, UpdateMyPasswordInput{NewPassword: password})
	if err != nil || !result.HasPassword || !result.CookieExpiresAt.Equal(f.now.Add(30*24*time.Hour)) {
		t.Fatal("first password failed or lost persistent expiry")
	}
	var hash, status string
	var attempts int
	var verified bool
	if f.tx.QueryRow(f.ctx, `SELECT c.password_hash,c.status,c.failed_attempts,u.email_verified_at IS NOT NULL
        FROM tutorhub.user_password_credentials c JOIN tutorhub.users u ON u.id=c.user_id WHERE u.id=$1`, f.principal.User.ID).
		Scan(&hash, &status, &attempts, &verified) != nil || status != "active" || attempts != 0 || !verified {
		t.Fatal("credential or email verification not persisted")
	}
	if matched, _, err := f.passwords.Verify(f.ctx, password, hash); err != nil || !matched {
		t.Fatal("new credential does not verify")
	}
	var identities int
	if f.tx.QueryRow(f.ctx, `SELECT count(*) FROM tutorhub.identities WHERE user_id=$1 AND provider='urn:tutorhub:email'
        AND kind='email' AND subject=$2 AND email_verified=true AND status='active'`, f.principal.User.ID, f.principal.User.ID.String()).Scan(&identities) != nil || identities != 1 {
		t.Fatal("native email identity missing")
	}
	var tokenHash, csrfHash []byte
	var revoked *time.Time
	var methods []string
	var version int64
	if f.tx.QueryRow(f.ctx, `SELECT token_hash,csrf_token_hash,revoked_at,auth_methods,context_version FROM tutorhub.sessions WHERE id=$1`, f.principal.SessionID).
		Scan(&tokenHash, &csrfHash, &revoked, &methods, &version) != nil || revoked != nil || version != 2 || !slices.Contains(methods, "fed") || !slices.Contains(methods, "pwd") ||
		bytes.Equal(tokenHash, f.oldTokenHash) || bytes.Equal(csrfHash, f.oldCSRFHash) || !bytes.Equal(tokenHash, f.service.crypto.Digest(sessionPurpose, result.SessionToken)) || !bytes.Equal(csrfHash, f.service.crypto.Digest(csrfPurpose, result.CSRFToken)) {
		t.Fatal("current session was not preserved and rotated")
	}
	var reason string
	if f.tx.QueryRow(f.ctx, `SELECT revoked_reason FROM tutorhub.sessions WHERE id=$1 AND revoked_at IS NOT NULL`, f.otherSession).Scan(&reason) != nil || reason != "password_changed" {
		t.Fatal("other session was not revoked")
	}
	if _, err := f.service.UpdateMyPassword(f.ctx, f.principal, UpdateMyPasswordInput{NewPassword: "Replayed candidate 44!"}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("old session context replayed password mutation")
	}
	f.principal.ContextVersion = version
	after, err := f.service.GetMyPasswordStatus(f.ctx, f.principal)
	if err != nil || !after.HasPassword || after.UpdatedAt == nil || !after.UpdatedAt.Equal(f.now) {
		t.Fatal("password status did not expose committed update time")
	}
	if err := f.dispatcher.Close(f.ctx); err != nil {
		t.Fatal("drain password change notice")
	}
	messages := f.inbox.List()
	if len(messages) != 1 || messages[0].Kind != "notice" || messages[0].NoticeKind != "password_changed" || messages[0].Code != "" {
		t.Fatal("security notice missing or contains a code")
	}
}

func TestNativePostgresPasswordSettingsWrongCurrentLockoutAndChange(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	oldPassword := "First river 43! Orbit"
	hash, err := f.passwords.Hash(f.ctx, oldPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertNativePassword(f.ctx, f.tx, f.principal.User.ID, hash, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.user_password_credentials SET failed_attempts=4,compromised_at=$2 WHERE user_id=$1`, f.principal.User.ID, f.now); err != nil {
		t.Fatal("seed failed-attempt state")
	}
	wrong := "Wrong current 44!"
	input := UpdateMyPasswordInput{CurrentPassword: &wrong, NewPassword: "Second valley 45! Orbit"}
	if _, err := f.service.UpdateMyPassword(f.ctx, f.principal, input); !errors.Is(err, ErrNativeInvalidCurrentPassword) {
		t.Fatal("wrong current password accepted")
	}
	var attempts int
	var until *time.Time
	if f.tx.QueryRow(f.ctx, `SELECT failed_attempts,locked_until FROM tutorhub.user_password_credentials WHERE user_id=$1`, f.principal.User.ID).Scan(&attempts, &until) != nil || attempts != 5 || until == nil || !until.Equal(f.now.Add(time.Minute)) {
		t.Fatal("failed current password did not lock credential")
	}
	input.CurrentPassword = &oldPassword
	if _, err := f.service.UpdateMyPassword(f.ctx, f.principal, input); !errors.Is(err, ErrNativeInvalidCurrentPassword) {
		t.Fatal("current-password lockout was bypassed")
	}
	f.now = f.now.Add(time.Minute + time.Second)
	result, err := f.service.UpdateMyPassword(f.ctx, f.principal, input)
	if err != nil || !result.HasPassword {
		t.Fatal("correct current password failed after bounded lock expired")
	}
	var compromised *time.Time
	if f.tx.QueryRow(f.ctx, `SELECT password_hash,failed_attempts,locked_until,compromised_at FROM tutorhub.user_password_credentials WHERE user_id=$1`, f.principal.User.ID).
		Scan(&hash, &attempts, &until, &compromised) != nil || attempts != 0 || until != nil || compromised != nil {
		t.Fatal("successful change did not reset failure/compromise state")
	}
	if matched, _, err := f.passwords.Verify(f.ctx, input.NewPassword, hash); err != nil || !matched {
		t.Fatal("changed password does not verify")
	}
	if matched, _, err := f.passwords.Verify(f.ctx, oldPassword, hash); err != nil || matched {
		t.Fatal("old password still verifies")
	}
}

func TestNativePostgresPasswordSettingsRecentAuthVerifiedEmailAndAdminCap(t *testing.T) {
	f := newSettingsIntegrationFixture(t, false)
	input := UpdateMyPasswordInput{NewPassword: "Glacier valley 45! Orbit"}
	if _, err := f.service.UpdateMyPassword(f.ctx, f.principal, input); !errors.Is(err, ErrNativeVerifiedEmailRequired) {
		t.Fatal("unverified OIDC email enrolled a password")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.identities SET email_verified=true WHERE id=$1`, f.principal.IdentityID); err != nil {
		t.Fatal("verify settings fixture email")
	}
	stale := f.principal
	stale.AuthenticatedAt = f.now.Add(-11 * time.Minute)
	if _, err := f.service.UpdateMyPassword(f.ctx, stale, input); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatal("old principal bypassed recent authentication")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET auth_time=$2 WHERE id=$1`, f.principal.SessionID, f.now.Add(-11*time.Minute)); err != nil {
		t.Fatal("age authoritative authentication")
	}
	if _, err := f.service.UpdateMyPassword(f.ctx, f.principal, input); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatal("stale authoritative auth time was ignored")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET reauthenticated_at=$2 WHERE id=$1`, f.principal.SessionID, f.now); err != nil {
		t.Fatal("refresh authoritative authentication")
	}
	tenantID := uuid.New()
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Settings admin')`, tenantID, "settings-"+tenantID.String()); err != nil {
		t.Fatal("seed settings admin tenant")
	}
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'org_admin','active')`, tenantID, f.principal.User.ID); err != nil {
		t.Fatal("seed settings administrator")
	}
	result, err := f.service.UpdateMyPassword(f.ctx, f.principal, input)
	if err != nil {
		t.Fatal("recent administrator password enrollment failed")
	}
	var absolute, expires time.Time
	var persistent bool
	if f.tx.QueryRow(f.ctx, `SELECT absolute_expires_at,expires_at,persistent FROM tutorhub.sessions WHERE id=$1`, f.principal.SessionID).Scan(&absolute, &expires, &persistent) != nil || persistent || !absolute.Equal(f.now.Add(24*time.Hour)) || !result.CookieExpiresAt.Equal(expires) {
		t.Fatal("administrator change retained persistent lifetime")
	}
}
