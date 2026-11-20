//go:build integration

package identity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/migrations"
)

func TestSchoolSSOMigrationAndExactACL(t *testing.T) {
	ctx, pool, url := authSchemaTestDatabase(t)
	version := func(want uint) {
		t.Helper()
		v, err := migrationrunner.CurrentVersion(ctx, url)
		if err != nil || v.Number != want || v.Dirty {
			t.Fatal("unclean migration ledger", want, v, err)
		}
	}
	for _, want := range []uint{49, 50, 49, 50} {
		var err error
		if want == 49 {
			err = migrationrunner.Down(ctx, url, 1)
		} else {
			err = migrationrunner.Up(ctx, url)
		}
		if err != nil {
			t.Fatal(err)
		}
		version(want)
	}
	down, err := migrations.Files.ReadFile("000050_sso_domains.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(string(down)), "BEGIN;"), "COMMIT;")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `WITH t AS (INSERT INTO tutorhub.tenants(slug,name) VALUES('sso-'||gen_random_uuid(),'Rollback fixture') RETURNING id)
        INSERT INTO tutorhub.auth_sso_connections(tenant_id,kind,allowed_org_ids) SELECT id,'google_workspace',ARRAY['school.test'] FROM t`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, body); err == nil || !strings.Contains(err.Error(), "AUTH-12 rollback blocked") {
		t.Fatal("down accepted disabled connection", err)
	}
	rollback(tx)
	t.Run("runtimeColumnPrivilegeMatrix", TestAuthFoundationRuntimeExactACL)
}

func ssoAdminFixture(t *testing.T, f *nativeIntegrationFixture, email, role string) Principal {
	t.Helper()
	p := Principal{SessionID: uuid.New(), IdentityID: uuid.New(), ContextVersion: 1, ActiveTenant: &Tenant{ID: uuid.New()}}
	p.User.ID, p.User.Email = uuid.New(), email
	run := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(f.ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	run(`INSERT INTO tutorhub.tenants(id,slug,name) VALUES ($1,$2,'SSO fixture')`, p.ActiveTenant.ID, "sso-"+uuid.NewString())
	run(`INSERT INTO tutorhub.users(id,email,display_name,email_verified_at) VALUES($1,$2,'SSO admin',now())`, p.User.ID, email)
	run(`INSERT INTO tutorhub.memberships(tenant_id,user_id,role) VALUES($1,$2,$3)`, p.ActiveTenant.ID, p.User.ID, role)
	run(`INSERT INTO tutorhub.identities(id,user_id,provider,subject,provider_slug,email_at_provider,email_verified)
        VALUES ($1,$2,'https://accounts.google.com',$3,'google',$4,true)`, p.IdentityID, p.User.ID, uuid.NewString(), email)
	run(`INSERT INTO tutorhub.sessions(id,user_id,identity_id,active_tenant_id,token_hash,csrf_token_hash,auth_time,expires_at,absolute_expires_at)
        VALUES($1,$2,$3,$4,$5,$6,now(),now()+interval '1 hour',now()+interval '2 hours')`, p.SessionID, p.User.ID, p.IdentityID, p.ActiveTenant.ID, f.service.crypto.Digest("test-session", p.SessionID.String()), f.service.crypto.Digest("test-csrf", p.SessionID.String()))
	// FK-bound audit actors/tenants remain for the outer harness's DROP DATABASE.
	t.Cleanup(func() {
		for _, query := range []string{`DELETE FROM tutorhub.auth_sso_domains WHERE tenant_id=$1`, `DELETE FROM tutorhub.auth_sso_connections WHERE tenant_id=$1`} {
			if _, err := f.pool.Exec(f.ctx, query, p.ActiveTenant.ID); err != nil {
				t.Error(err)
			}
		}
		for _, query := range []string{`DELETE FROM tutorhub.auth_transactions WHERE user_id=$1`, `DELETE FROM tutorhub.mfa_totp_factors WHERE user_id=$1`, `DELETE FROM tutorhub.sessions WHERE user_id=$1`, `DELETE FROM tutorhub.identities WHERE user_id=$1`} {
			if _, err := f.pool.Exec(f.ctx, query, p.User.ID); err != nil {
				t.Error(err)
			}
		}
	})
	return p
}

func TestSchoolSSOPostgresAdminPoliciesAndDNS(t *testing.T) {
	f := newNativeIntegrationFixture(t)
	f.service.config.SSO = SSORealmConfig{Enabled: true, GoogleEnabled: true, MicrosoftEnabled: true}
	runtime := createPasskeyRuntimeRole(t, f).(passkeyRuntimeDB)
	// The established tenant subsystem grants these operations. SHARE tuple
	// locks require some UPDATE column on the existing tenant/member tables.
	for _, grant := range []string{"GRANT UPDATE(name) ON tutorhub.tenants TO ", "GRANT UPDATE(role) ON tutorhub.memberships TO ", "GRANT INSERT ON tutorhub.audit_events TO "} {
		if _, err := f.pool.Exec(f.ctx, grant+runtime.quotedRole); err != nil {
			t.Fatal(err)
		}
	}
	f.service.database = runtime
	base := "sso-" + uuid.NewString() + ".test"
	a := ssoAdminFixture(t, f, "admin@"+base, "org_admin")
	b := ssoAdminFixture(t, f, uuid.NewString()+"@external.test", "org_admin")
	member := ssoAdminFixture(t, f, uuid.NewString()+"@external.test", "student")
	tenant := a.ActiveTenant.ID
	var records []string
	resolver := ssoResolverFunc(func(ctx context.Context, n string) ([]string, error) { return records, nil })
	s := NewSchoolSSOService(f.service, resolver, true, true)
	c, err := s.CreateConnection(f.ctx, a, tenant, "google_workspace", []string{base})
	if err != nil {
		t.Fatal("create connection", err)
	}
	c2, err := s.CreateConnection(f.ctx, b, b.ActiveTenant.ID, "google_workspace", []string{base})
	if err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{}, {"gmail.com"}, {base, base}, {"MixedCase.test"}} {
		if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.auth_sso_connections(tenant_id,kind,allowed_org_ids) VALUES($1,'google_workspace',$2)`, tenant, ids); err == nil {
			t.Fatal("database accepted invalid org IDs")
		}
	}
	if _, err := s.ListConnections(f.ctx, a, b.ActiveTenant.ID); !errors.Is(err, ErrSSOForbidden) {
		t.Fatal("cross tenant list", err)
	}
	if _, err := s.ListConnections(f.ctx, member, member.ActiveTenant.ID); !errors.Is(err, ErrSSOForbidden) {
		t.Fatal("student admin access", err)
	}
	if _, err := s.CreateDomain(f.ctx, a, tenant, c2.ID, base); !errors.Is(err, ErrSSONotFound) {
		t.Fatal("cross tenant connection", err)
	}
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO tutorhub.auth_sso_domains(tenant_id,connection_id,domain,status) VALUES($1,$2,$3,'disabled')`, tenant, c2.ID, base); err == nil {
		t.Fatal("composite FK accepted foreign connection")
	}
	enabled := "enabled"
	on := true
	patch := SSOConnectionPatch{Status: &enabled, EnforceSSO: &on}
	if _, err := s.UpdateConnection(f.ctx, a, tenant, c.ID, patch); !errors.Is(err, ErrSSOConflict) {
		t.Fatal("enforced unverified connection", err)
	}
	domain, err := s.CreateDomain(f.ctx, a, tenant, c.ID, base)
	if err != nil {
		t.Fatal(err)
	}
	records = []string{domain.TXTValue}
	var afterRotate SSOChallenge
	s.resolver = ssoResolverFunc(func(context.Context, string) ([]string, error) {
		var err error
		afterRotate, err = s.RotateDomain(f.ctx, a, tenant, domain.Domain.ID)
		if err != nil {
			return nil, err
		}
		return records, nil
	})
	if _, err := s.VerifyDomain(f.ctx, a, tenant, domain.Domain.ID); !errors.Is(err, ErrSSOConflict) {
		t.Fatal("stale DNS result survived rotation", err)
	}
	s.resolver = resolver
	if _, err := s.VerifyDomain(f.ctx, a, tenant, domain.Domain.ID); !errors.Is(err, ErrSSODNSMismatch) {
		t.Fatal("old token", err)
	}
	records = []string{afterRotate.TXTValue}
	s.resolver = ssoResolverFunc(func(ctx context.Context, _ string) ([]string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("DNS timeout not set")
		}
		return nil, context.DeadlineExceeded
	})
	if _, err := s.VerifyDomain(f.ctx, a, tenant, domain.Domain.ID); !errors.Is(err, ErrSSODNSUnavailable) {
		t.Fatal("DNS timeout", err)
	}
	s.resolver = resolver
	if _, err := s.VerifyDomain(f.ctx, a, tenant, domain.Domain.ID); err != nil {
		t.Fatal("verify", err)
	}
	if _, err := s.UpdateConnection(f.ctx, a, tenant, c.ID, patch); !errors.Is(err, ErrSSOLockout) {
		t.Fatal("lockout guard", err)
	}
	disabledProvider := NewSchoolSSOService(f.service, resolver, false, true)
	if _, err := disabledProvider.UpdateConnection(f.ctx, a, tenant, c.ID, patch); !errors.Is(err, ErrSSOConflict) {
		t.Fatal("unavailable provider permitted enforcement", err)
	}
	_, err = f.pool.Exec(f.ctx, `INSERT INTO tutorhub.mfa_totp_factors(user_id,secret_ciphertext,status,last_used_step,confirmed_at,created_at,updated_at)
        VALUES($1,decode(repeat('12',40),'hex'),'active',1,now(),now(),now())`, a.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.service.config.MFATOTPEnabled = true
	if _, err := s.UpdateConnection(f.ctx, a, tenant, c.ID, patch); !errors.Is(err, ErrFederationAALRequired) {
		t.Fatal("missing AAL2", err)
	}
	if _, err = f.pool.Exec(f.ctx, `UPDATE tutorhub.sessions SET assurance_level=2,auth_time=now()-interval '11 minutes' WHERE id=$1`, a.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConnection(f.ctx, a, tenant, c.ID, patch); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatal("stale auth", err)
	}
	if _, err = f.pool.Exec(f.ctx, `UPDATE tutorhub.sessions SET reauthenticated_at=now() WHERE id=$1`, a.SessionID); err != nil {
		t.Fatal(err)
	}
	tx, err := runtime.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveSchoolReceipt(f.ctx, tx, f.service.crypto, a.User.ID, a.SessionID, &schoolFederationProof{Provider: "google", Domain: base, OrgID: base}, time.Now()); err != nil {
		rollback(tx)
		t.Fatal(err)
	}
	if err := tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConnection(f.ctx, a, tenant, c.ID, patch); err != nil {
		t.Fatal("valid admin proof", err)
	}
	wrong := []string{"another.test"}
	if _, err := s.UpdateConnection(f.ctx, a, tenant, c.ID, SSOConnectionPatch{AllowedOrgIDs: &wrong}); !errors.Is(err, ErrSSOLockout) {
		t.Fatal("unsafe allowlist", err)
	}
	routes, err := f.service.DiscoverSSORealm(f.ctx, base)
	if err != nil || !routes.Required || !routes.Available {
		t.Fatal("HRD missing enforced route", routes, err)
	}
	var safe bool
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*)>=5 AND bool_and(metadata::text NOT LIKE '%'||$2||'%') FROM tutorhub.audit_events WHERE tenant_id=$1 AND action LIKE 'tenant.sso_%' AND outcome='succeeded'`, tenant, base).Scan(&safe); err != nil || !safe {
		t.Fatal("redacted success audit", err)
	}
	other := "conflict-" + uuid.NewString() + ".test"
	da, err := s.CreateDomain(f.ctx, a, tenant, c.ID, other)
	if err != nil {
		t.Fatal(err)
	}
	db, err := s.CreateDomain(f.ctx, b, b.ActiveTenant.ID, c2.ID, other)
	if err != nil {
		t.Fatal(err)
	}
	records = []string{da.TXTValue, db.TXTValue}
	var wg sync.WaitGroup
	var outcomes [2]error
	for i, tc := range []struct {
		p Principal
		d SSODomain
	}{{a, da.Domain}, {b, db.Domain}} {
		wg.Add(1)
		go func() { defer wg.Done(); _, outcomes[i] = s.VerifyDomain(f.ctx, tc.p, tc.p.ActiveTenant.ID, tc.d.ID) }()
	}
	wg.Wait()
	if (outcomes[0] == nil) == (outcomes[1] == nil) || !(errors.Is(outcomes[0], ErrSSOConflict) || errors.Is(outcomes[1], ErrSSOConflict)) {
		t.Fatalf("race outcomes %v", outcomes)
	}
	if _, err := s.DisableDomain(f.ctx, b, b.ActiveTenant.ID, da.Domain.ID); !errors.Is(err, ErrSSONotFound) {
		t.Fatal("foreign domain disabled", err)
	}
	if _, err := s.DisableDomain(f.ctx, a, tenant, da.Domain.ID); err != nil {
		t.Fatal(err)
	}
}
