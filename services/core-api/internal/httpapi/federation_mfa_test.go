package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func TestMFACallbackSetsOnlyTransactionAndRedirects(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/callback", "/api/v1/auth/oauth/google/callback"} {
		t.Run(path, func(t *testing.T) {
			cookie := &http.Cookie{Name: "__Host-tutorhub_auth_tx", Value: strings.Repeat("b", 43), Path: "/", MaxAge: 300,
				SameSite: http.SameSiteStrictMode, HttpOnly: true, Secure: true}
			result := identity.LoginResult{MFA: &identity.MFAChallenge{Cookie: cookie}}
			service := &fakeFederationService{fakeIdentityService: fakeIdentityService{completeLogin: &result}, callbackResult: identity.OAuthResult{LoginResult: result}}
			r := httptest.NewRequest("GET", path+"?state=test&code=test", nil)
			r.AddCookie(&http.Cookie{Name: "__Host-tutorhub_oidc", Value: "test-binding"})
			w := httptest.NewRecorder()
			federationTestHandler(service, true, true, nil, nil).ServeHTTP(w, r)
			if w.Code != 303 || w.Header().Get("Location") != "https://app.example/sign-in?mfa=pending" {
				t.Fatalf("pending status=%d redirect=%q", w.Code, w.Header().Get("Location"))
			}
			for _, c := range w.Result().Cookies() {
				if c.Name != cookie.Name && !(c.Name == "__Host-tutorhub_oidc" && c.MaxAge < 0) {
					t.Fatalf("unexpected cookie %q", c.Name)
				}
			}
			got := findCookie(t, w.Result().Cookies(), cookie.Name)
			if got.MaxAge != 300 || !got.HttpOnly || !got.Secure || got.SameSite != http.SameSiteStrictMode {
				t.Fatal("lost transaction binding")
			}
		})
	}
}

func TestAdminMFAWorkspaceAccessAfterPromotion(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		aal     int16
		role    string
		want    int
	}{{true, 1, "org_admin", 403}, {true, 2, "org_admin", 200}, {true, 1, "student", 200}, {false, 1, "org_admin", 200}} {
		cfg := authFoundationConfig()
		cfg.Authentication.RequireAdminMFA = tc.enabled
		tenant := identity.Tenant{ID: uuid.New(), Role: tc.role, IsActive: true}
		p := identity.Principal{SessionID: uuid.New(), User: identity.User{ID: uuid.New()}, AssuranceLevel: tc.aal,
			ActiveTenant: &tenant, Memberships: []identity.Tenant{tenant}, Permissions: []string{"class.view"}}
		service := &fakeIdentityService{principal: p}
		h := NewHandlerWithOptions(cfg, nil, Options{Identity: service})
		r := httptest.NewRequest("GET", "/api/v1/tenants", nil)
		r.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want || (tc.want == 403 && !strings.Contains(w.Body.String(), "auth_admin_mfa_required")) {
			t.Fatalf("enabled=%t level=%d role=%s status=%d", tc.enabled, tc.aal, tc.role, w.Code)
		}
		r = httptest.NewRequest("GET", "/api/v1/me", nil)
		r.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var me meResponse
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &me) != nil || me.User.ID != p.User.ID || len(me.Memberships) != 1 {
			t.Fatal("account bootstrap unavailable")
		}
		granted := tc.want == 200
		if (me.ActiveTenant != nil) != granted || (len(me.Permissions) > 0) != granted || me.Memberships[0].IsActive != granted {
			t.Fatalf("incorrect workspace projection for enabled=%t level=%d role=%s", tc.enabled, tc.aal, tc.role)
		}
		r = httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
		r.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
		r.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
		r.Header.Set(csrfHeader, "csrf-token")
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !service.logoutCalled {
			t.Fatalf("account logout unavailable, status=%d", w.Code)
		}
	}
}
