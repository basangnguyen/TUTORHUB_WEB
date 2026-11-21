//go:build integration

package identity

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type schoolTransitionFixture struct {
	*phoneIntegrationFixture
	connection uuid.UUID
	account    Principal
	claims     FederationIdentityPayload
	repo       *PostgresRepository
}

func newSchoolTransitionFixture(t *testing.T, kind string) *schoolTransitionFixture {
	t.Helper()
	n := newPhoneIntegrationFixture(t)
	s := n.service
	domain := uuid.NewString() + ".school.test"
	p := n.accountWithEmail(t, "member@"+domain)
	f := &schoolTransitionFixture{phoneIntegrationFixture: n, account: p, connection: uuid.New()}
	tenant := uuid.New()
	f.exec(t, "INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'School fixture')", tenant, "school-"+tenant.String())
	t.Cleanup(func() { f.exec(t, "DELETE FROM tutorhub.tenants WHERE id=$1", tenant) })
	provider, issuer, org := "google", GoogleIssuerURL, domain
	registration := ProviderRegistration{Slug: provider, Issuer: issuer, Provider: &fakeProvider{}}
	if kind == "entra" {
		provider, org = "microsoft", uuid.NewString()
		issuer = microsoftIssuer(org)
		registration = ProviderRegistration{Slug: provider, Issuer: MicrosoftAuthorityURL,
			Provider: &microsoftFederationStub{fakeProvider: &fakeProvider{}, policy: MicrosoftProvider{tenantMode: "common"}}}
	}
	f.exec(t, "INSERT INTO tutorhub.auth_sso_connections(id,tenant_id,kind,status,allowed_org_ids) VALUES($1,$2,$3,'enabled',ARRAY[$4]::text[])", f.connection, tenant, kind, org)
	f.exec(t, "INSERT INTO tutorhub.auth_sso_domains(tenant_id,connection_id,domain,status,verified_at) VALUES($1,$2,$3,'verified',now())", tenant, f.connection, domain)
	s.config.SSO = SSORealmConfig{Enabled: true, GoogleEnabled: true, MicrosoftEnabled: true}
	s.config.MFATOTPEnabled = true
	registry, err := NewProviderRegistry(registration)
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigureRegistrationProviders(registry)
	f.repo = NewPostgresRepository(s.database, 20*time.Second, policy.NewEngine())
	f.repo.ConfigureAccountAssurance(s, false)
	f.claims = FederationIdentityPayload{ProviderSlug: provider, Issuer: issuer, Subject: uuid.NewString(), Email: p.User.Email,
		EmailVerified: true, ProviderOrgID: org, DisplayName: "Member", ReturnTo: "/app"}
	return f
}

func (f *schoolTransitionFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(f.ctx, sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *schoolTransitionFixture) schoolLogin(t *testing.T) (FederationLoginRecord, error) {
	t.Helper()
	in := federationInputFromPayload(t, &settingsIntegrationFixture{service: f.service, now: f.service.now().UTC()}, f.claims)
	return f.repo.CompleteFederatedLogin(f.ctx, in)
}

func (f *schoolTransitionFixture) verifyLink(t *testing.T, events, live int, oldSession uuid.UUID) {
	t.Helper()
	var eventCount, sessionCount int
	var revoked bool
	err := f.pool.QueryRow(f.ctx, `SELECT
		(SELECT count(*) FROM tutorhub.outbox_events WHERE aggregate_id=$1 AND event_type='identity_linked'),
		(SELECT count(*) FROM tutorhub.sessions WHERE user_id=$1 AND revoked_at IS NULL),
		EXISTS(SELECT FROM tutorhub.sessions WHERE id=$2 AND user_id=$1 AND revoked_at IS NOT NULL AND revoked_reason='identity_linked')`,
		f.account.User.ID, oldSession).Scan(&eventCount, &sessionCount, &revoked)
	if err != nil || eventCount != events || sessionCount != live || !revoked {
		t.Fatalf("link effects: audit=%d live=%d revoked=%t error=%v", eventCount, sessionCount, revoked, err)
	}
}

func (f *schoolTransitionFixture) principalFor(t *testing.T, token string) Principal {
	t.Helper()
	p := f.account
	if err := f.pool.QueryRow(f.ctx, `SELECT id,context_version,assurance_level FROM tutorhub.sessions
		WHERE user_id=$1 AND token_hash=$2 AND revoked_at IS NULL`, p.User.ID, f.service.crypto.Digest(sessionPurpose, token)).
		Scan(&p.SessionID, &p.ContextVersion, &p.AssuranceLevel); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSchoolSSOPostgresPasskeyAndPhoneLoginBoundaries(t *testing.T) {
	f := newSchoolTransitionFixture(t, "google_workspace")
	s := f.service
	f.bind(t, f.account)
	const origin = "https://passkeys.example.test"
	rp, err := NewPasskeyProtocol("passkeys.example.test", "TutorHub", []string{origin})
	if err != nil {
		t.Fatal(err)
	}
	s.webauthn = rp
	key := newVirtualPasskey(t)
	reg, err := s.BeginPasskey(f.ctx, PasskeyRegistration, &f.account)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishPasskeyRegistration(f.ctx, f.account, reg.Cookie.Value, key.Register(t, reg.Options, origin, true, true, false, 0), "School device"); err != nil {
		t.Fatal(err)
	}
	pendingPhone := phoneMFAStart(t, f.phoneIntegrationFixture, []string{"passkey"})
	f.exec(t, "UPDATE tutorhub.auth_sso_connections SET enforce_sso=true WHERE id=$1", f.connection)
	for _, uv := range []bool{false, true} {
		o, err := s.BeginPasskey(f.ctx, PasskeyLogin, nil)
		if err != nil {
			t.Fatal(err)
		}
		binding := s.crypto.Digest("auth-tx-binding", o.Cookie.Value)
		t.Cleanup(func() { f.exec(t, "DELETE FROM tutorhub.auth_transactions WHERE binding_hash=$1", binding) })
		proof := key.Assert(t, o.Options, origin, uv, true, false, 0)
		result, err := s.FinishPasskeyLogin(f.ctx, o.Cookie.Value, proof, true, "", "")
		if !errors.Is(err, ErrSSORequired) || result.MFA != nil || result.SessionToken != "" {
			t.Fatalf("first-factor passkey UV=%t: %v", uv, err)
		}
		if _, err := s.FinishPasskeyLogin(f.ctx, o.Cookie.Value, proof, true, "", ""); !errors.Is(err, ErrInvalidPasskey) {
			t.Fatal("passkey transaction consumption", err)
		}
	}
	phone := f.login(t)
	result, err := s.VerifyPhoneLogin(f.ctx, phone.Cookie.Value, phoneProof(phone))
	if !errors.Is(err, ErrSSORequired) || result.SessionToken != "" || result.MFA != nil {
		t.Fatal("verified first-factor phone", err)
	}
	if _, err := s.VerifyPhoneLogin(f.ctx, phone.Cookie.Value, phoneProof(phone)); !errors.Is(err, ErrNativeInvalidCode) {
		t.Fatal("phone transaction consumption", err)
	}
	if _, err := s.VerifyMFA(f.ctx, pendingPhone, MFAVerifyInput{Method: "passkey"}); !errors.Is(err, ErrSSORequired) {
		t.Fatal("pending phone MFA after enforcement change", err)
	}
	s.config.MFATOTPEnabled = false
	if _, err := f.schoolLogin(t); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("unavailable factor at school transition", err)
	}
	s.config.MFATOTPEnabled = true
	var binding string
	for range 2 {
		record, err := f.schoolLogin(t)
		if err != nil || record.Principal.SessionID != uuid.Nil || record.LinkPending || record.MFA == nil || !slices.Equal(record.MFA.Methods, []string{"passkey"}) {
			t.Fatal("required school callback for passkey-only enrollment", err)
		}
		binding = record.MFA.Cookie.Value
		f.verifyLink(t, 1, 0, f.account.SessionID)
	}
	opts, err := s.MFAPasskeyOptions(f.ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	login, err := s.VerifyMFA(f.ctx, binding, MFAVerifyInput{Method: "passkey", Assertion: key.Assert(t, opts, origin, true, true, false, 0)})
	if err != nil {
		t.Fatal(err)
	}
	p := f.principalFor(t, login.Session.SessionToken)
	if p.AssuranceLevel != 2 {
		t.Fatal("school passkey MFA assurance")
	}
	reauth, err := s.BeginPasskey(f.ctx, PasskeyReauth, &p)
	if err != nil {
		t.Fatal(err)
	}
	step, err := s.FinishPasskeyReauth(f.ctx, p, reauth.Cookie.Value, key.Assert(t, reauth.Options, origin, true, true, false, 0))
	if err != nil || step.SessionToken == login.Session.SessionToken || f.principalFor(t, step.SessionToken).AssuranceLevel != 2 {
		t.Fatal("school passkey step-up", err)
	}
}

func TestSchoolSSOPostgresVerifiedLegacyLinkAndRelink(t *testing.T) {
	for _, kind := range []string{"google_workspace", "entra"} {
		t.Run(kind, func(t *testing.T) { testVerifiedSchoolLinkAndRelink(t, kind) })
	}
}

func testVerifiedSchoolLinkAndRelink(t *testing.T, kind string) {
	f := newSchoolTransitionFixture(t, kind)
	s := f.service
	expectIntent := func() {
		t.Helper()
		record, err := f.schoolLogin(t)
		if err != nil || !record.LinkPending || record.Principal.SessionID != uuid.Nil || record.MFA != nil {
			t.Fatal("expected explicit confirmation", err)
		}
	}
	expectIntent()
	f.exec(t, "UPDATE tutorhub.auth_sso_connections SET enforce_sso=true WHERE id=$1", f.connection)
	f.exec(t, "UPDATE tutorhub.users SET email_verified_at=NULL WHERE id=$1", f.account.User.ID)
	expectIntent()
	f.exec(t, "UPDATE tutorhub.users SET email_verified_at=now() WHERE id=$1", f.account.User.ID)
	f.exec(t, "UPDATE tutorhub.sessions SET assurance_level=2 WHERE id=$1", f.account.SessionID)
	if _, err := f.schoolLogin(t); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("no second factor available to meet existing AAL2 floor", err)
	}
	f.exec(t, "UPDATE tutorhub.sessions SET assurance_level=1 WHERE id=$1", f.account.SessionID)
	first, err := f.schoolLogin(t)
	if err != nil || first.Principal.User.ID != f.account.User.ID || first.Principal.SessionID == uuid.Nil || first.MFA != nil || first.LinkPending {
		t.Fatal("verified password-only legacy account", err)
	}
	f.verifyLink(t, 1, 1, f.account.SessionID)

	p := first.Principal
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	enrolled, err := s.EnrollTOTP(f.ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	code := func() string {
		t.Helper()
		value, err := totp.GenerateCode(enrolled.Secret, now)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	confirmation, err := s.ConfirmTOTP(f.ctx, p, code())
	if err != nil {
		t.Fatal(err)
	}
	old := f.principalFor(t, confirmation.Session.SessionToken)
	f.exec(t, "UPDATE tutorhub.identities SET status='unlinked',unlinked_at=now() WHERE id=$1", p.IdentityID)
	f.claims.Email = "other@" + SSOEmailDomain(f.account.User.Email)
	expectIntent()
	f.claims.Email = f.account.User.Email
	now = now.Add(30 * time.Second)
	record, err := f.schoolLogin(t)
	if err != nil || record.MFA == nil || !slices.Contains(record.MFA.Methods, "totp") || record.Principal.SessionID != uuid.Nil || record.LinkPending {
		t.Fatal("school relink requires TOTP", err)
	}
	f.verifyLink(t, 2, 0, old.SessionID)
	login, err := s.VerifyMFA(f.ctx, record.MFA.Cookie.Value, MFAVerifyInput{Method: "totp", Code: code()})
	if err != nil {
		t.Fatal("school TOTP completion", err)
	}
	now = now.Add(30 * time.Second)
	second := f.principalFor(t, login.Session.SessionToken)
	step, err := s.Reauthenticate(f.ctx, second, "", ReauthenticateInput{Method: "totp", Code: code()})
	if err != nil || step.SessionToken == login.Session.SessionToken || f.principalFor(t, step.SessionToken).AssuranceLevel != 2 {
		t.Fatal("school TOTP step-up", err)
	}
}
