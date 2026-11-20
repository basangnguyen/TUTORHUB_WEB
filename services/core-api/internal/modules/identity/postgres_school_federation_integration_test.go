//go:build integration

package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type schoolTestCapacity struct {
	featurecontrol.Enforcer
	full bool
}

func (c *schoolTestCapacity) RequireMemberCapacity(context.Context, featurecontrol.Transaction, uuid.UUID) error {
	if c.full {
		return featurecontrol.ErrQuotaExceeded
	}
	return nil
}

func TestSchoolSSOFederationRuntimeJIT(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	db := createPasskeyRuntimeRole(t, f).(passkeyRuntimeDB)
	// The production membership runtime already inserts invitation memberships.
	if _, err := f.pool.Exec(f.ctx, "GRANT INSERT ON tutorhub.memberships TO "+db.quotedRole); err != nil {
		t.Fatal(err)
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	exec := func(stmt string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(f.ctx, stmt, args...); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	f.service.database = tx
	f.service.config.SSO = SSORealmConfig{Enabled: true, GoogleEnabled: true, MicrosoftEnabled: true}
	f.service.now = func() time.Time { return now }
	fx := &settingsIntegrationFixture{ctx: f.ctx, tx: tx, service: f.service, now: now}
	capacity := &schoolTestCapacity{}
	f.service.ConfigureSSOMemberCapacity(capacity)
	repo := NewPostgresRepository(tx, 20*time.Second, policy.NewEngine(), capacity)
	repo.ConfigureAccountAssurance(f.service, false)
	for _, kind := range []string{"google_workspace", "entra"} {
		t.Run(kind, func(t *testing.T) {
			tenant, connection := uuid.New(), uuid.New()
			domain := tenant.String() + ".example"
			slug, issuer, org := "google", GoogleIssuerURL, domain
			if kind == "entra" {
				slug = "microsoft"
				org = uuid.NewString()
				issuer = microsoftIssuer(org)
			}
			exec("RESET ROLE")
			exec("INSERT INTO tutorhub.tenants(id,slug,name) VALUES($1,$2,'SSO fixture')", tenant, "school-"+tenant.String())
			exec("INSERT INTO tutorhub.auth_sso_connections(id,tenant_id,kind,status,allowed_org_ids) VALUES($1,$2,$3,'enabled',$4)", connection, tenant, kind, []string{org})
			exec("INSERT INTO tutorhub.auth_sso_domains(tenant_id,connection_id,domain,status,verified_at) VALUES($1,$2,$3,'verified',now())", tenant, connection, domain)
			exec("SET LOCAL ROLE " + db.quotedRole)
			p := FederationIdentityPayload{ProviderSlug: slug, Issuer: issuer, Subject: uuid.NewString(), Email: "learner@" + domain, EmailVerified: true, ProviderOrgID: org, DisplayName: "Learner", ReturnTo: "/app"}
			login := func(p FederationIdentityPayload, requested string) (FederationLoginRecord, error) {
				in := federationInputFromPayload(t, fx, p)
				in.SchoolDomain = requested
				in.RegistrationBindingHash = f.service.crypto.Digest(registrationBindingPurpose, uuid.NewString())
				return repo.CompleteFederatedLogin(f.ctx, in)
			}
			member, err := login(p, domain)
			if err != nil || member.Principal.SessionID == uuid.Nil || len(member.Principal.Memberships) != 0 {
				t.Fatal("default invitation gate", err)
			}
			user := member.Principal.User.ID
			exec("UPDATE tutorhub.auth_sso_connections SET auto_join_role='student' WHERE id=$1 AND tenant_id=$2", connection, tenant)
			capacity.full = true
			if _, err = login(p, domain); !errors.Is(err, featurecontrol.ErrQuotaExceeded) {
				t.Fatal("capacity bypass", err)
			}
			capacity.full = false
			for range 2 {
				member, err = login(p, domain)
				if err != nil || member.Principal.ActiveTenant == nil || member.Principal.ActiveTenant.ID != tenant || member.Principal.ActiveTenant.Role != "student" {
					t.Fatal("JIT login failed", err)
				}
			}
			for _, role := range []string{"teacher", "org_admin"} {
				exec("RESET ROLE")
				exec("UPDATE tutorhub.memberships SET role=$1 WHERE tenant_id=$2 AND user_id=$3", role, tenant, user)
				exec("SET LOCAL ROLE " + db.quotedRole)
				member, err = login(p, domain)
				if err != nil || member.Principal.ActiveTenant == nil || member.Principal.ActiveTenant.Role != role {
					t.Fatal("existing membership role changed", err)
				}
			}
			ok, err := schoolSessionProvesOrg(f.ctx, tx, member.Principal.SessionID, user, kind, domain, []string{org}, f.service.crypto)
			if err != nil || !ok {
				t.Fatal("fresh signed school proof missing", err)
			}
			ok, err = schoolSessionProvesOrg(f.ctx, tx, member.Principal.SessionID, user, kind, domain, []string{"unrelated.example"}, f.service.crypto)
			if err != nil || ok {
				t.Fatal("lockout guard accepted an unproven target configuration", err)
			}
			exec("RESET ROLE")
			exec("UPDATE tutorhub.memberships SET status='suspended' WHERE tenant_id=$1 AND user_id=$2", tenant, user)
			exec("SET LOCAL ROLE " + db.quotedRole)
			member, err = login(p, domain)
			if err != nil || len(member.Principal.Memberships) != 0 {
				t.Fatal("JIT reactivated suspended member", err)
			}

			wrong := p
			wrong.Subject = uuid.NewString()
			wrong.Email = "other@" + domain
			wrong.ProviderOrgID = "elsewhere.example"
			if kind == "entra" {
				wrong.ProviderOrgID = uuid.NewString()
				wrong.Issuer = microsoftIssuer(wrong.ProviderOrgID)
			}
			result, err := login(wrong, "")
			if err != nil || result.Principal.SessionID == uuid.Nil || len(result.Principal.Memberships) != 0 {
				t.Fatal("optional realm with different organization", err)
			}
			if _, err = login(wrong, domain); !errors.Is(err, ErrSSORequired) {
				t.Fatal("explicit school flow accepted mismatched signed organization", err)
			}
			exec("UPDATE tutorhub.auth_sso_connections SET enforce_sso=true WHERE id=$1 AND tenant_id=$2", connection, tenant)
			if _, err = login(wrong, ""); !errors.Is(err, ErrSSORequired) {
				t.Fatal("required realm accepted mismatched organization", err)
			}
			if kind == "google_workspace" {
				t.Run("MFA rechecks current school before session and JIT", func(t *testing.T) {
					exec("UPDATE tutorhub.auth_sso_connections SET auto_join_role=NULL WHERE id=$1", connection)
					mfaUser := p
					mfaUser.Subject, mfaUser.Email = uuid.NewString(), "mfa@"+domain
					record, err := login(mfaUser, domain)
					if err != nil || len(record.Principal.Memberships) != 0 {
						t.Fatal("seed school account with invitation-only policy", err)
					}
					uid := record.Principal.User.ID
					registry, err := NewProviderRegistry(ProviderRegistration{Slug: "google", Issuer: GoogleIssuerURL, Provider: &fakeProvider{}})
					if err != nil {
						t.Fatal(err)
					}
					f.service.ConfigureRegistrationProviders(registry)
					f.service.config.MFATOTPEnabled = true
					defer func() { f.service.config.MFATOTPEnabled = false }()
					exec("INSERT INTO tutorhub.mfa_totp_factors(user_id,secret_ciphertext,status,last_used_step,created_at,confirmed_at,updated_at) VALUES($1,$2,'active',1,$3,$3,$3)", uid, make([]byte, 48), now)
					codes, err := f.service.replaceRecoveryCodes(f.ctx, tx, uid, now)
					if err != nil {
						t.Fatal(err)
					}
					exec("UPDATE tutorhub.auth_sso_connections SET auto_join_role='student' WHERE id=$1", connection)
					record, err = login(mfaUser, domain)
					if err != nil || record.MFA == nil || record.Principal.SessionID != uuid.Nil {
						t.Fatal("missing second-factor challenge", err)
					}
					binding := record.MFA.Cookie.Value
					verify := MFAVerifyInput{Method: "recovery_code", Code: codes[0]}
					exec("UPDATE tutorhub.auth_sso_connections SET allowed_org_ids=ARRAY['other.example'] WHERE id=$1", connection)
					if _, err = f.service.VerifyMFA(f.ctx, binding, verify); !errors.Is(err, ErrSSORequired) {
						t.Fatal("MFA took stale allowlist", err)
					}
					var count int
					if err = tx.QueryRow(f.ctx, "SELECT count(*) FROM tutorhub.memberships WHERE user_id=$1 AND tenant_id=$2", uid, tenant).Scan(&count); err != nil || count != 0 {
						t.Fatal("membership issued before successful second factor", err)
					}
					exec("UPDATE tutorhub.auth_sso_connections SET allowed_org_ids=$1 WHERE id=$2", []string{org}, connection)
					result, err := f.service.VerifyMFA(f.ctx, binding, verify)
					if err != nil || result.Session.SessionToken == "" {
						t.Fatal("verified school MFA login", err)
					}
					var aal int
					var role string
					if err = tx.QueryRow(f.ctx, `SELECT s.assurance_level,m.role FROM tutorhub.sessions s JOIN tutorhub.memberships m
						ON m.user_id=s.user_id AND m.tenant_id=s.active_tenant_id WHERE s.user_id=$1 AND s.token_hash=$2 AND m.tenant_id=$3`, uid, f.service.crypto.Digest(sessionPurpose, result.Session.SessionToken), tenant).Scan(&aal, &role); err != nil || aal != 2 || role != "student" {
						t.Fatal("school MFA session missing idempotent membership", err)
					}
				})
			}
			if kind == "entra" {
				untrusted := p
				untrusted.EmailVerified = false
				if _, err = login(untrusted, ""); !errors.Is(err, ErrSSORequired) {
					t.Fatal("Entra callback with absent email proof passed", err)
				}
				untrusted.Email = ""
				if _, err = login(untrusted, ""); !errors.Is(err, ErrSSORequired) {
					t.Fatal("linked account evaded enforced realm via absent email", err)
				}
				exec("UPDATE tutorhub.auth_sso_connections SET enforce_sso=false WHERE id=$1 AND tenant_id=$2", connection, tenant)
				untrusted.Subject = uuid.NewString()
				untrusted.Email = "untrusted@" + domain
				result, err = login(untrusted, "")
				if err != nil || !result.RegistrationPending || result.Principal.SessionID != uuid.Nil {
					t.Fatal("untrusted Entra claims bypassed pending registration", err)
				}
			}
		})
	}
}
