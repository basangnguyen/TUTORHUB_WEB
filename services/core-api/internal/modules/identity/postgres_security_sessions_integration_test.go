//go:build integration

package identity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestNativePostgresSecurityOverviewAndAllLoginKinds(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true) // Outer transaction always rolls back, including audit rows.
	f.service.config.EmailCodeAvailable = true
	if err := upsertNativePassword(f.ctx, f.tx, f.principal.User.ID, dummyPasswordPHC, f.now); err != nil {
		t.Fatal("seed active credential")
	}
	for _, kind := range []string{"email", "oauth2", "phone", "passkey"} {
		if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.identities(user_id,provider,subject,kind,provider_slug,email_at_provider,email_verified)
        VALUES($1,$2,$3,$4,$4,$5,true)`, f.principal.User.ID, "urn:fixture:"+kind, uuid.NewString(), kind, f.principal.User.Email); err != nil {
			t.Fatal("seed login method")
		}
	}
	security, err := f.service.GetMySecurity(f.ctx, f.principal)
	if err != nil || !security.HasPassword || !security.EmailVerified || security.MFAEnabled || !slices.Equal(security.ReauthMethods, []string{"password", "email_code"}) || security.RecentAuthExpiresAt == nil {
		t.Fatal("incorrect security overview")
	}
	methods, err := f.service.ListMyLoginMethods(f.ctx, f.principal)
	if err != nil || !methods.Password.HasPassword || methods.Password.UpdatedAt == nil || len(methods.Identities) != 5 {
		t.Fatal("login methods incomplete")
	}
	for _, kind := range []string{"email", "oidc", "oauth2", "phone", "passkey"} {
		if !slices.ContainsFunc(methods.Identities, func(item LoginMethodIdentity) bool { return item.Kind == kind }) {
			t.Fatal("missing login kind")
		}
	}
	f.principal.AuthenticatedAt = f.now.Add(-11 * time.Minute)
	security, err = f.service.GetMySecurity(f.ctx, f.principal)
	if err != nil || security.RecentAuthExpiresAt != nil {
		t.Fatal("expired recent authentication exposed")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET context_version=context_version+1 WHERE id=$1`, f.principal.SessionID); err != nil {
		t.Fatal("rotate fixture context")
	}
	if _, err := f.service.ListMyLoginMethods(f.ctx, f.principal); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("stale session read login methods")
	}
}

func TestNativePostgresSessionListAndRevocationIsolation(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	foreign, foreignSession := uuid.New(), uuid.New()
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.users(id,email,display_name)VALUES($1,$2,'Foreign')`, foreign, foreign.String()+"@example.test"); err != nil {
		t.Fatal("seed foreign user")
	}
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.sessions(id,user_id,token_hash,csrf_token_hash,created_at,last_seen_at,expires_at,absolute_expires_at,auth_time)
        VALUES($1,$2,$3,$4,$5,$5,$6,$6,$5)`, foreignSession, foreign, f.service.crypto.Digest(sessionPurpose, foreignSession.String()), f.service.crypto.Digest(csrfPurpose, foreignSession.String()), f.now, f.now.Add(time.Hour)); err != nil {
		t.Fatal("seed foreign session")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET last_seen_at=$2 WHERE id=$1`, f.otherSession, f.now.Add(time.Minute)); err != nil {
		t.Fatal("set session ordering")
	}
	sessions, err := f.service.ListMySessions(f.ctx, f.principal)
	if err != nil || len(sessions) != 2 || sessions[0].ID != f.otherSession || sessions[0].IsCurrent || sessions[1].ID != f.principal.SessionID || !sessions[1].IsCurrent {
		t.Fatal("session scope/order/current flag failed")
	}
	if err := f.service.RevokeMySession(f.ctx, f.principal, f.principal.SessionID); !errors.Is(err, ErrCannotRevokeCurrentSession) {
		t.Fatal("current session revoked")
	}
	for _, id := range []uuid.UUID{foreignSession, uuid.New()} {
		if err := f.service.RevokeMySession(f.ctx, f.principal, id); !errors.Is(err, ErrManagedSessionNotFound) {
			t.Fatal("foreign session not concealed")
		}
	}
	if err := f.service.RevokeMySession(f.ctx, f.principal, f.otherSession); err != nil {
		t.Fatal("owned session revoke failed")
	}
	if err := f.service.RevokeMySession(f.ctx, f.principal, f.otherSession); !errors.Is(err, ErrManagedSessionNotFound) {
		t.Fatal("revoked session replay not concealed")
	}
	var reason string
	if f.tx.QueryRow(f.ctx, `SELECT revoked_reason FROM tutorhub.sessions WHERE id=$1`, f.otherSession).Scan(&reason) != nil || reason != "user_revoked" {
		t.Fatal("wrong single revoke reason")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET revoked_at=NULL,revoked_reason=NULL WHERE id=$1`, f.otherSession); err != nil {
		t.Fatal("reset owned session fixture")
	}
	count, err := f.service.RevokeOtherSessions(f.ctx, f.principal)
	if err != nil || count != 1 {
		t.Fatal("revoke others count incorrect")
	}
	if f.tx.QueryRow(f.ctx, `SELECT revoked_reason FROM tutorhub.sessions WHERE id=$1`, f.otherSession).Scan(&reason) != nil || reason != "user_revoked_others" {
		t.Fatal("wrong bulk revoke reason")
	}
	count, err = f.service.RevokeOtherSessions(f.ctx, f.principal)
	if err != nil || count != 0 {
		t.Fatal("revoke others not idempotent")
	}
	var foreignRevoked bool
	if f.tx.QueryRow(f.ctx, `SELECT revoked_at IS NOT NULL FROM tutorhub.sessions WHERE id=$1`, foreignSession).Scan(&foreignRevoked) != nil || foreignRevoked {
		t.Fatal("foreign session changed")
	}
	sessions, err = f.service.ListMySessions(f.ctx, f.principal)
	if err != nil || len(sessions) != 1 || !sessions[0].IsCurrent {
		t.Fatal("revoked sessions listed")
	}
}

func TestNativePostgresSessionListAppliesAdministratorCap(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	tenant := uuid.New()
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name)VALUES($1,$2,'Admin')`, tenant, tenant.String()); err != nil {
		t.Fatal("seed tenant")
	}
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status,joined_at)VALUES($1,$2,'org_admin','active',$3)`, tenant, f.principal.User.ID, f.now); err != nil {
		t.Fatal("seed admin membership")
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET created_at=$2,last_seen_at=$2 WHERE id=$1`, f.otherSession, f.now.Add(-25*time.Hour)); err != nil {
		t.Fatal("age admin session")
	}
	sessions, err := f.service.ListMySessions(f.ctx, f.principal)
	if err != nil || len(sessions) != 1 || !sessions[0].IsCurrent || sessions[0].Persistent {
		t.Fatal("expired admin session or persistent flag leaked")
	}
}

func TestNativePostgresDeviceLabelsPersistForAllLoginPaths(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	f.service.challenges.code = func() (string, error) { return "420381", nil }
	email := "device-" + uuid.NewString() + "@example.test"
	start, err := f.service.StartEmailSignup(f.ctx, SignupStartInput{Email: email, DestinationAllowed: true})
	if err != nil {
		t.Fatal("start label signup")
	}
	input := SignupCompleteInput{ChallengeID: start.ChallengeID, Code: "420381", DisplayName: "Device fixture", Password: nativeIntegrationPassword, TermsVersion: "test", UserAgent: "Mozilla/5.0 (Windows NT) Chrome/130.0"}
	signup, err := f.service.CompleteEmailSignup(f.ctx, start.Cookie.Value, input)
	if err != nil {
		t.Fatal("complete label signup")
	}
	check := func(token, want string) {
		t.Helper()
		var label string
		if f.tx.QueryRow(f.ctx, `SELECT device_label FROM tutorhub.sessions WHERE token_hash=$1`, f.service.crypto.Digest(sessionPurpose, token)).Scan(&label) != nil || label != want {
			t.Fatal("incorrect persisted native device label")
		}
	}
	check(signup.SessionToken, "Chrome · Windows")
	login, err := f.service.PasswordLogin(f.ctx, PasswordLoginInput{Identifier: email, Password: nativeIntegrationPassword, UserAgent: "Mozilla/5.0 (Linux; Android 15) Zalo/25.0"})
	if err != nil {
		t.Fatal("password login label")
	}
	check(login.SessionToken, "Zalo in-app · Android")
	code, err := f.service.StartEmailCodeLogin(f.ctx, SignupStartInput{Email: email, DestinationAllowed: true})
	if err != nil {
		t.Fatal("start email code label")
	}
	verified, err := f.service.VerifyEmailCodeLogin(f.ctx, code.Cookie.Value, EmailCodeVerifyInput{ChallengeID: code.ChallengeID, Code: "420381", UserAgent: "Mozilla/5.0 (iPhone) Version/18.0 Mobile Safari/604.1"})
	if err != nil {
		t.Fatal("email code label")
	}
	check(verified.SessionToken, "Safari · iOS")
	repository := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	provider := &fakeProvider{}
	service := newTestService(t, repository, provider, f.now)
	flow, err := service.BeginLogin(f.ctx, "/app")
	if err != nil {
		t.Fatal("start OIDC label")
	}
	provider.claims = validClaims(f.now, provider.nonce)
	provider.claims.Email = "oidc-device-" + uuid.NewString() + "@example.test"
	provider.claims.Subject = uuid.NewString()
	oidc, err := service.CompleteLogin(f.ctx, CallbackInput{State: provider.state, BrowserBinding: flow.BrowserBinding, Code: "code", UserAgent: "Mozilla/5.0 (Macintosh) Firefox/131.0"})
	if err != nil {
		t.Fatal("OIDC callback label")
	}
	var label string
	if f.tx.QueryRow(context.Background(), `SELECT device_label FROM tutorhub.sessions WHERE token_hash=$1`, service.crypto.Digest(sessionPurpose, oidc.SessionToken)).Scan(&label) != nil || label != "Firefox · macOS" {
		t.Fatal("OIDC label did not persist")
	}
}
