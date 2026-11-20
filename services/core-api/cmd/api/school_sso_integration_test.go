//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/httpapi"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type schoolFixtureDNS struct{ name, value string }

func (d *schoolFixtureDNS) LookupTXT(_ context.Context, name string) ([]string, error) {
	if name != d.name {
		return nil, nil
	}
	return []string{d.value}, nil
}

func TestSchoolSSOPostgresHTTP(t *testing.T) {
	owner, runtime := newAuthTestDatabase(t, "AUTH12_TEST_POSTGRES_ADMIN_URL", "auth12_http_")
	ctx := t.Context()
	const origin = "https://school.example"
	cfg := config.Config{Environment: "test", WebOrigin: origin, Authentication: config.AuthenticationConfig{Enabled: true, SSODomainsEnabled: true, PasswordEnabled: true, EmailCodeEnabled: true, ProviderGoogleEnabled: true, EmailSender: "devinbox", LegalTermsVersion: "fixture", SessionKey: bytes.Repeat([]byte{0x35}, 32), SessionTTL: time.Hour, SessionAbsoluteTTL: 24 * time.Hour, SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour}}
	limiter := &deliveryBudgetLimiter{result: httpapi.InvitationRateLimitDecision{Allowed: true}}
	foundation, err := newAuthFoundation(cfg, runtime, limiter)
	if err != nil {
		t.Fatal(err)
	}
	defer foundation.close()
	native := foundation.native.(*identity.NativeAuthService)
	crypto, err := identity.NewCrypto(cfg.Authentication.SessionKey)
	if err != nil {
		t.Fatal(err)
	}
	authz := policy.NewEngine()
	sessionService, err := identity.NewService(identity.NewPostgresRepository(runtime, 5*time.Second, authz), nil, crypto, authz, identity.ServiceConfig{FlowTTL: time.Minute, SessionTTL: time.Hour, SessionAbsoluteTTL: 24 * time.Hour}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	auditService, err := audit.NewService(runtime, 5*time.Second, authz, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	dns := &schoolFixtureDNS{}
	handler := httpapi.NewHandlerWithOptions(cfg, nil, httpapi.Options{Identity: sessionService, NativeAuth: native, Audit: auditService, SchoolSSO: identity.NewSchoolSSOService(native, dns, true, false), AuthRateLimiter: limiter})
	var runtimeRole string
	if err = runtime.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatal(err)
	}
	role := pgx.Identifier{runtimeRole}.Sanitize()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := owner.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("GRANT SELECT,INSERT ON tutorhub.audit_events TO " + role)
	exec("GRANT UPDATE(name) ON tutorhub.tenants TO " + role)
	exec("GRANT UPDATE(role) ON tutorhub.memberships TO " + role)

	tenantID, adminID, identityID := uuid.New(), uuid.New(), uuid.New()
	sessionToken, csrfToken := uuid.NewString(), uuid.NewString()
	exec("INSERT INTO tutorhub.tenants(id,slug,name) VALUES ($1,'school-http','Fixture')", tenantID)
	exec("INSERT INTO tutorhub.users(id,email,display_name,email_verified_at) VALUES($1,'admin@other.example','Admin',now())", adminID)
	exec("INSERT INTO tutorhub.memberships(tenant_id,user_id,role) VALUES ($1,$2,'org_admin')", tenantID, adminID)
	exec("INSERT INTO tutorhub.identities(id,user_id,provider,subject) VALUES ($1,$2,'urn:test:org-admin',$3)", identityID, adminID, identityID.String())
	exec(`INSERT INTO tutorhub.sessions(user_id,identity_id,active_tenant_id,token_hash,csrf_token_hash,auth_time,expires_at,absolute_expires_at)
	  VALUES($1,$2,$3,$4,$5,now(),now()+interval '1 hour',now()+interval '2 hours')`, adminID, identityID, tenantID, crypto.Digest("session-token", sessionToken), crypto.Digest("csrf-token", csrfToken))
	exec("INSERT INTO tutorhub.users(email,display_name) VALUES('existing@school.example','Student')")

	call := func(method, path, body string, want int, csrf bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		req.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: sessionToken})
		req.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: csrfToken})
		if csrf {
			req.Header.Set("X-CSRF-Token", csrfToken)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			var problem httpapi.Problem
			_ = json.Unmarshal(rec.Body.Bytes(), &problem)
			t.Fatalf("%s %s: got %d (%s, %s), want %d", method, path, rec.Code, problem.Code, problem.Title, want)
		}
		return rec
	}
	base := "/api/v1/tenants/" + tenantID.String()
	connPath := base + "/sso-connections"
	const configBody = `{"kind":"google_workspace","allowed_org_ids":["school.example"]}`
	call("POST", connPath, configBody, 403, false)
	call("GET", "/api/v1/tenants/"+uuid.NewString()+"/sso-connections", "", 404, false)
	created := call("POST", connPath, configBody, 201, true)
	var connection identity.SSOConnection
	if err = json.Unmarshal(created.Body.Bytes(), &connection); err != nil || connection.TenantID != tenantID {
		t.Fatal("incorrect scoped connection", err)
	}
	connPath += "/" + connection.ID.String()
	reg := call("POST", base+"/sso-domains", fmt.Sprintf(`{"connection_id":%q,"domain":"school.example"}`, connection.ID), 201, true)
	var challenge identity.SSOChallenge
	if err = json.Unmarshal(reg.Body.Bytes(), &challenge); err != nil || challenge.TXTName != "_tutorhub-challenge.school.example." || !strings.HasPrefix(challenge.TXTValue, "tutorhub-site-verification=") {
		t.Fatal("incorrect challenge")
	}
	domainPath := base + "/sso-domains/" + challenge.Domain.ID.String()
	call("POST", domainPath+"/verify", "{}", 409, true)
	dns.name, dns.value = challenge.TXTName, challenge.TXTValue
	call("POST", domainPath+"/verify", "{}", 200, true)
	call("PATCH", connPath, `{"status":"enabled","enforce_sso":true}`, 200, true)
	for _, email := range []string{"existing@school.example", "absent@school.example"} {
		route := call("POST", "/api/v1/auth/identifier/discover", fmt.Sprintf(`{"identifier":%q}`, email), 200, false)
		var found struct {
			Kind string
			SSO  identity.SSORoute
		}
		if json.Unmarshal(route.Body.Bytes(), &found) != nil || found.Kind != "email" || !found.SSO.Required || !found.SSO.Available || found.SSO.Provider != "google" || found.SSO.Domain != "school.example" {
			t.Fatal("incorrect domain routing")
		}
		login := call("POST", "/api/v1/auth/password/login", fmt.Sprintf(`{"identifier":%q,"password":"fixture-example"}`, email), 403, false)
		var problem httpapi.Problem
		if json.Unmarshal(login.Body.Bytes(), &problem) != nil || problem.Code != "auth_sso_required" {
			t.Fatal("incorrect enforcement response")
		}
	}
	exec("UPDATE tutorhub.memberships SET role='teacher' WHERE tenant_id=$1 AND user_id=$2", tenantID, adminID)
	call("POST", domainPath+"/disable", "{}", 403, true)
	for _, want := range []struct{ action, outcome string }{
		{"tenant.sso_connection.create", "succeeded"}, {"tenant.sso_connection.update", "succeeded"}, {"tenant.sso_domain.verify", "failed"}, {"tenant.sso_domain.verify", "succeeded"}, {"tenant.sso_domain.disable", "denied"},
	} {
		var count int
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM tutorhub.audit_events WHERE tenant_id=$1 AND actor_user_id=$2 AND action=$3 AND outcome=$4", tenantID, adminID, want.action, want.outcome).Scan(&count); err != nil || count != 1 {
			t.Fatalf("audit %v count=%d, err=%v", want, count, err)
		}
	}
}
