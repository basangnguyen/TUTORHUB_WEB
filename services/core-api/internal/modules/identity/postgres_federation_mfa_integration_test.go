//go:build integration

package identity

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestMFAFederationPostgresAccountFloorOutlivesAAL2Sessions(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	s := f.service
	s.config.MFATOTPEnabled = true
	repo := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	repo.ConfigureAccountAssurance(s, false)
	registry, err := NewProviderRegistry(ProviderRegistration{Slug: "google", Issuer: GoogleIssuerURL, Provider: &fakeProvider{}})
	if err != nil {
		t.Fatal(err)
	}
	s.ConfigureRegistrationProviders(registry)
	// Leave one valid AAL1 session for link/reauth and only an expired AAL2 session.
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET assurance_level=2, expires_at=$2 WHERE id=$1`, f.otherSession, f.now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(2 * time.Minute)
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.mfa_totp_factors(user_id,secret_ciphertext,status,last_used_step,created_at,confirmed_at,updated_at)
		VALUES($1,$2,'active',1,$3,$3,$3)`, f.principal.User.ID, make([]byte, 48), f.now); err != nil {
		t.Fatal(err)
	}
	codes, err := s.replaceRecoveryCodes(f.ctx, f.tx, f.principal.User.ID, f.now)
	if err != nil {
		t.Fatal(err)
	}
	pending := federationIntegrationInput(t, f, f.principal.User.Email)
	if record, err := repo.CompleteFederatedLogin(f.ctx, pending); err != nil || !record.LinkPending {
		t.Fatal("link intent", err)
	}
	confirm := federationConfirmInput(t, f, repo, pending)
	if _, err := repo.ConfirmLinkIntent(f.ctx, confirm); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("link accepted insufficient assurance", err)
	}
	input := federationIntegrationInput(t, f, f.principal.User.Email)
	input.Flow.ReturnTo = "/app/calendar"
	id, err := insertFederatedIdentity(f.ctx, f.tx, f.principal.User.ID, "google", input.Claims, f.now)
	if err != nil {
		t.Fatal(err)
	}
	reauth := FederationReauthParams{Flow: StoredFlow{Purpose: "reauth", ProviderSlug: "google", UserID: f.principal.User.ID, SessionID: f.principal.SessionID, CreatedAt: f.now}, Claims: input.Claims, Rotation: confirm.Rotation}
	if _, err := repo.ReauthenticateFederated(f.ctx, reauth); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("reauth accepted insufficient assurance", err)
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.identities SET email_at_provider='previous@example.test',email_verified=false WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	callback := func() string {
		t.Helper()
		record, err := repo.CompleteFederatedLogin(f.ctx, input)
		if err != nil || record.MFA == nil || record.Principal.SessionID != uuid.Nil {
			t.Fatal("callback issued pre-MFA session", err)
		}
		var issued bool
		if err := f.tx.QueryRow(f.ctx, `SELECT EXISTS(SELECT FROM tutorhub.sessions WHERE token_hash=$1)`, input.Metadata.TokenHash).Scan(&issued); err != nil || issued {
			t.Fatal("first factor persisted a session", err)
		}
		return record.MFA.Cookie.Value
	}
	second := func(binding, code string) (MFAVerifyResult, error) {
		return s.VerifyMFA(f.ctx, binding, MFAVerifyInput{Method: "recovery_code", Code: code})
	}
	firstBinding := callback()
	var freshMailbox bool
	if err := f.tx.QueryRow(f.ctx, `SELECT email_at_provider=$2 AND email_verified FROM tutorhub.identities WHERE id=$1`, id, input.Claims.Email).Scan(&freshMailbox); err != nil || !freshMailbox {
		t.Fatal("verified callback did not refresh the provider mailbox", err)
	}
	result, err := second(firstBinding, codes[0])
	if err != nil || result.ReturnTo != "/app/calendar" || result.Session.SessionToken == "" {
		t.Fatal("MFA lost return path or session", err)
	}
	var owner, linked uuid.UUID
	var aal int
	var methods []string
	if err := f.tx.QueryRow(f.ctx, `SELECT user_id,identity_id,assurance_level,auth_methods FROM tutorhub.sessions WHERE token_hash=$1`, s.crypto.Digest(sessionPurpose, result.Session.SessionToken)).Scan(&owner, &linked, &aal, &methods); err != nil || owner != f.principal.User.ID || linked != id || aal != 2 || !slices.Equal(methods, []string{"fed", "recovery_code"}) {
		t.Fatal("invalid federated session evidence", err)
	}
	binding := callback()
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.identities SET status='unlinked',unlinked_at=$2 WHERE id=$1`, id, f.now); err != nil {
		t.Fatal(err)
	}
	if _, err := second(binding, codes[1]); !errors.Is(err, ErrAuthTransactionUnavailable) {
		t.Fatal("withdrawn provider proof accepted", err)
	}
}

func TestMFAAdminPostgresDefaultAndExplicitTenantSelection(t *testing.T) {
	f := newSettingsIntegrationFixture(t, true)
	repo := NewPostgresRepository(f.tx, 20*time.Second, policy.NewEngine())
	repo.ConfigureAccountAssurance(nil, true)
	id := uuid.New()
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'Admin fixture')`, id, "admin-"+id.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tx.Exec(f.ctx, `INSERT INTO tutorhub.memberships(tenant_id,user_id,role,status,joined_at) VALUES($1,$2,'org_admin','active',$3)`, id, f.principal.User.ID, f.now); err != nil {
		t.Fatal(err)
	}
	if active, err := selectActiveTenant(f.ctx, f.tx, f.principal.User.ID, true); err != nil || active != uuid.Nil {
		t.Fatal("selected administrator tenant for weak session", err)
	}
	if active, err := selectActiveTenant(f.ctx, f.tx, f.principal.User.ID, false); err != nil || active != id {
		t.Fatal("administrator membership disappeared", err)
	}
	rotation := SessionRotation{TokenHash: f.service.crypto.Digest(sessionPurpose, uuid.NewString()), CSRFHash: f.service.crypto.Digest(csrfPurpose, uuid.NewString()), ExpectedContextVersion: 1, RotatedAt: f.now}
	attempt := func() (TenantMutationResult, error) {
		return repo.SwitchActiveTenant(f.ctx, f.principal.SessionID, f.principal.User.ID, id, rotation)
	}
	if _, err := attempt(); !errors.Is(err, ErrAdminMFARequired) {
		t.Fatal("AAL1 switched active administrator tenant", err)
	}
	if _, err := f.tx.Exec(f.ctx, `UPDATE tutorhub.sessions SET assurance_level=2 WHERE id=$1`, f.principal.SessionID); err != nil {
		t.Fatal(err)
	}
	got, err := attempt()
	if err != nil || got.Principal.ActiveTenant == nil || got.Principal.ActiveTenant.ID != id || got.Principal.ContextVersion != 2 {
		t.Fatal("AAL2 tenancy switch", err)
	}
}
