package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type fakeSecurityAuth struct {
	fakeNativeAuth
	principal   identity.Principal
	reauthInput identity.ReauthenticateInput
	revokedID   uuid.UUID
}

func (service *fakeSecurityAuth) GetMySecurity(_ context.Context, p identity.Principal) (identity.MySecurity, error) {
	service.calls++
	service.principal = p
	return identity.MySecurity{HasPassword: true, EmailVerified: true, ReauthMethods: []string{"password", "email_code"}}, service.err
}
func (service *fakeSecurityAuth) ListMyLoginMethods(_ context.Context, p identity.Principal) (identity.MyLoginMethods, error) {
	service.calls++
	service.principal = p
	return identity.MyLoginMethods{Password: identity.LoginMethodPassword{HasPassword: true}, Identities: []identity.LoginMethodIdentity{}}, service.err
}
func (service *fakeSecurityAuth) ListMySessions(_ context.Context, p identity.Principal) ([]identity.MySession, error) {
	service.calls++
	service.principal = p
	return []identity.MySession{{ID: p.SessionID, AuthMethods: []string{"oidc"}, IsCurrent: true}}, service.err
}
func (service *fakeSecurityAuth) StartReauthEmailCode(ctx context.Context, p identity.Principal, binding string, allowed func(context.Context, string) (bool, error)) (identity.SignupChallengeResult, error) {
	service.calls++
	service.principal = p
	service.bound = binding
	if _, err := allowed(ctx, p.User.Email); err != nil {
		return identity.SignupChallengeResult{}, err
	}
	return service.challenge(), service.err
}
func (service *fakeSecurityAuth) Reauthenticate(_ context.Context, p identity.Principal, binding string, input identity.ReauthenticateInput) (identity.ReauthenticateResult, error) {
	service.calls++
	service.principal = p
	service.bound = binding
	service.reauthInput = input
	return identity.ReauthenticateResult{RecentAuthExpiresAt: fixedTime.Add(10 * time.Minute), CookieExpiresAt: fixedTime.Add(30 * 24 * time.Hour), SessionToken: "private-session", CSRFToken: "private-csrf"}, service.err
}
func (service *fakeSecurityAuth) RevokeMySession(_ context.Context, p identity.Principal, id uuid.UUID) error {
	service.calls++
	service.principal = p
	service.revokedID = id
	return service.err
}
func (service *fakeSecurityAuth) RevokeOtherSessions(_ context.Context, p identity.Principal) (int, error) {
	service.calls++
	service.principal = p
	return 3, service.err
}

func securityTestHandler(service *fakeSecurityAuth, enabled bool, limiter InvitationRateLimiter, logger *slog.Logger) http.Handler {
	cfg := authFoundationConfig()
	cfg.Authentication.Enabled = true
	cfg.Authentication.DeviceManagementEnabled = enabled
	return NewHandlerWithOptions(cfg, logger, Options{NativeAuth: service, Identity: &fakeIdentityService{}, AuthRateLimiter: limiter, Clock: func() time.Time { return fixedTime }})
}
func securityRequest(method, path, body string) *http.Request {
	r := passwordSettingsRequest(method, body, true, true)
	r.URL.Path = path
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return r
}

var securityMutationCases = []struct{ path, body string }{
	{"/api/v1/auth/reauth/email-code/start", `{}`},
	{"/api/v1/auth/reauth", `{"method":"password","password":"private-password"}`},
	{"/api/v1/me/sessions/7dd14656-fcfb-4ce9-a11c-b3e412593cf3/revoke", `{}`},
	{"/api/v1/me/sessions/revoke-others", `{}`},
}

func TestSecurityRoutesFeatureSessionCSRFOriginBoundary(t *testing.T) {
	for _, path := range []string{"/api/v1/me/security", "/api/v1/me/login-methods", "/api/v1/me/sessions"} {
		for _, state := range []struct {
			enabled, session bool
			status           int
		}{{false, true, 503}, {true, false, 401}, {true, true, 200}} {
			t.Run(path+fmt.Sprint(state), func(t *testing.T) {
				service := &fakeSecurityAuth{}
				r := securityRequest(http.MethodGet, path, "")
				if !state.session {
					r.Header.Del("Cookie")
				}
				w := httptest.NewRecorder()
				securityTestHandler(service, state.enabled, &nativeTestLimiter{}, nil).ServeHTTP(w, r)
				if w.Code != state.status {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
				if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
					t.Fatal("missing privacy headers")
				}
				if state.status != 200 && service.calls != 0 {
					t.Fatal("boundary reached service")
				}
			})
		}
	}
	for _, route := range securityMutationCases {
		for _, tc := range []struct {
			name    string
			enabled bool
			mutate  func(*http.Request)
			status  int
		}{
			{"disabled", false, func(*http.Request) {}, 503},
			{"no session", true, func(r *http.Request) { r.Header.Del("Cookie") }, 401},
			{"no csrf", true, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403},
			{"wrong csrf", true, func(r *http.Request) { r.Header.Set("X-CSRF-Token", "wrong") }, 403},
			{"no origin", true, func(r *http.Request) { r.Header.Del("Origin") }, 403},
			{"wrong origin", true, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
			{"duplicate origin", true, func(r *http.Request) { r.Header.Add("Origin", "https://app.example") }, 403},
			{"same site", true, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, 403},
			{"wrong media", true, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		} {
			t.Run(route.path+tc.name, func(t *testing.T) {
				service := &fakeSecurityAuth{}
				r := securityRequest(http.MethodPost, route.path, route.body)
				tc.mutate(r)
				w := httptest.NewRecorder()
				securityTestHandler(service, tc.enabled, &nativeTestLimiter{}, nil).ServeHTTP(w, r)
				if w.Code != tc.status || service.calls != 0 {
					t.Fatalf("boundary status=%d calls=%d body=%s", w.Code, service.calls, w.Body.String())
				}
				if tc.status == 401 {
					var p Problem
					decodeJSON(t, w, &p)
					if p.Code != "" {
						t.Fatal("expired session should have no proof code")
					}
				}
			})
		}
	}
}

func TestSecurityMutationsRateLimitUserAndIP(t *testing.T) {
	for index, route := range securityMutationCases {
		actions := []InvitationRateLimitAction{AuthRateLimitSessionRevokeUser, AuthRateLimitSessionRevokeIP}
		if index == 0 {
			actions = []InvitationRateLimitAction{AuthRateLimitReauthSendUser, AuthRateLimitReauthSendIP}
		}
		if index == 1 {
			actions = []InvitationRateLimitAction{AuthRateLimitReauthUser, AuthRateLimitReauthIP}
		}
		for _, action := range actions {
			for _, fail := range []bool{false, true} {
				t.Run(string(action)+route.path+fmt.Sprint(fail), func(t *testing.T) {
					limiter := &nativeTestLimiter{deny: action}
					want := 429
					if fail {
						limiter.deny = ""
						limiter.fail = action
						want = 503
					}
					service := &fakeSecurityAuth{}
					w := httptest.NewRecorder()
					securityTestHandler(service, true, limiter, nil).ServeHTTP(w, securityRequest(http.MethodPost, route.path, route.body))
					if w.Code != want || w.Header().Get("Retry-After") == "" || service.calls != 0 || strings.Contains(w.Body.String(), "private") {
						t.Fatalf("rate boundary: %d %s", w.Code, w.Body.String())
					}
				})
			}
		}
	}
	service := &fakeSecurityAuth{}
	limiter := &nativeTestLimiter{}
	w := httptest.NewRecorder()
	securityTestHandler(service, true, limiter, nil).ServeHTTP(w, securityRequest(http.MethodPost, securityMutationCases[1].path, securityMutationCases[1].body))
	if len(limiter.calls) != 2 || limiter.calls[1].bucket != service.principal.User.ID.String() {
		t.Fatal("user rate limit did not use authenticated principal")
	}
}

func TestSecurityResponsesCookiesAndPrincipalScope(t *testing.T) {
	for _, route := range securityMutationCases {
		service := &fakeSecurityAuth{}
		w := httptest.NewRecorder()
		securityTestHandler(service, true, &nativeTestLimiter{}, nil).ServeHTTP(w, securityRequest(http.MethodPost, route.path, route.body))
		want := 200
		if strings.HasSuffix(route.path, "/start") {
			want = 202
		}
		if strings.HasSuffix(route.path, "/revoke") {
			want = 204
		}
		if w.Code != want || service.calls != 1 || service.principal.User.ID == uuid.Nil {
			t.Fatalf("result=%d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-") {
			t.Fatal("response leaked secrets")
		}
		if route.path == "/api/v1/auth/reauth" {
			if findCookie(t, w.Result().Cookies(), "tutorhub_session").MaxAge != 30*24*3600 || findCookie(t, w.Result().Cookies(), "tutorhub_csrf").Value != "private-csrf" {
				t.Fatal("rotated cookies/lifetime missing")
			}
		}
	}
	service := &fakeSecurityAuth{}
	r := securityRequest(http.MethodPost, "/api/v1/auth/reauth", `{"method":"email_code","challenge_id":"`+nativeTestChallengeID.String()+`","code":"123456"}`)
	r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: nativeTestBinding})
	w := httptest.NewRecorder()
	securityTestHandler(service, true, &nativeTestLimiter{}, nil).ServeHTTP(w, r)
	if w.Code != 200 || service.bound != nativeTestBinding || service.reauthInput.Code != "123456" || findCookie(t, w.Result().Cookies(), "tutorhub_auth_tx").MaxAge != -1 {
		t.Fatal("email reauth binding or consume cookie failed")
	}
}

func TestReauthenticationStrictBodiesAndMissingBinding(t *testing.T) {
	for _, body := range []string{`{}`, `{"method":"totp","code":"123456"}`, `{"method":"password","password":"x","code":"123456"}`, `{"method":"email_code","password":"x","code":"123456"}`, `{"method":"password","password":"x","email":"foreign@example.test"}`, `{"method":"password","method":"email_code","password":"x"}`} {
		service := &fakeSecurityAuth{}
		w := httptest.NewRecorder()
		securityTestHandler(service, true, &nativeTestLimiter{}, nil).ServeHTTP(w, securityRequest(http.MethodPost, "/api/v1/auth/reauth", body))
		if w.Code != 400 || service.calls != 0 {
			t.Fatalf("invalid body accepted: %s => %d", body, w.Code)
		}
	}
	for _, duplicate := range []bool{false, true} {
		service := &fakeSecurityAuth{}
		w := httptest.NewRecorder()
		r := securityRequest(http.MethodPost, "/api/v1/auth/reauth", `{"method":"email_code","challenge_id":"`+nativeTestChallengeID.String()+`","code":"123456"}`)
		if duplicate {
			r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: nativeTestBinding})
			r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: nativeTestBinding})
		}
		securityTestHandler(service, true, &nativeTestLimiter{}, nil).ServeHTTP(w, r)
		var p Problem
		decodeJSON(t, w, &p)
		if w.Code != 401 || p.Code != "auth_invalid_verification_code" || service.calls != 0 {
			t.Fatal("missing/ambiguous binding lost proof code")
		}
	}
	service := &fakeSecurityAuth{}
	w := httptest.NewRecorder()
	securityTestHandler(service, true, &nativeTestLimiter{}, nil).ServeHTTP(w, securityRequest(http.MethodPost, "/api/v1/auth/reauth/email-code/start", `{"email":"foreign@example.test"}`))
	if w.Code != 400 || service.calls != 0 {
		t.Fatal("client-controlled reauth email accepted")
	}
}

func TestSecurityErrorCodesAndRedaction(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{identity.ErrNativeInvalidCurrentPassword, 401, "auth_invalid_current_password"},
		{identity.ErrNativeInvalidCode, 401, "auth_invalid_verification_code"},
		{identity.ErrAuthTransactionUnavailable, 401, "auth_invalid_verification_code"},
		{identity.ErrSessionNotFound, 401, ""},
		{identity.ErrSessionContextConflict, 409, ""},
		{identity.ErrCannotRevokeCurrentSession, 400, "auth_cannot_revoke_current_session"},
		{identity.ErrManagedSessionNotFound, 404, ""},
		{identity.ErrAuthFoundationStorage, 503, ""},
		{errors.New("private-password"), 500, ""},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			var logs bytes.Buffer
			service := &fakeSecurityAuth{}
			service.err = fmt.Errorf("%w: private-password private@example.test 123456", tc.err)
			w := httptest.NewRecorder()
			securityTestHandler(service, true, &nativeTestLimiter{}, slog.New(slog.NewJSONHandler(&logs, nil))).ServeHTTP(w, securityRequest(http.MethodPost, "/api/v1/auth/reauth", securityMutationCases[1].body))
			var p Problem
			decodeJSON(t, w, &p)
			if w.Code != tc.status || p.Code != tc.code || len(w.Result().Cookies()) != 0 {
				t.Fatalf("status/code/cookies: %d %#v", w.Code, p)
			}
			for _, secret := range []string{"private-password", "private@example.test", "123456"} {
				if strings.Contains(w.Body.String()+logs.String(), secret) {
					t.Fatal("response/log leaked secret")
				}
			}
		})
	}
}

func TestRecentAuthenticationCodeConsistentAcrossHandlers(t *testing.T) {
	for _, write := range []func(http.ResponseWriter, *http.Request){
		func(w http.ResponseWriter, r *http.Request) {
			(authHandlers{}).writeIdentityProblem(w, r, identity.ErrRecentAuthenticationRequired)
		},
		func(w http.ResponseWriter, r *http.Request) {
			(classHandlers{}).writeProblem(w, r, classroom.ErrRecentAuthenticationRequired)
		},
		func(w http.ResponseWriter, r *http.Request) {
			(nativeAuthHandlers{}).writeError(w, r, identity.ErrRecentAuthenticationRequired)
		},
		func(w http.ResponseWriter, r *http.Request) {
			(membershipInvitationHandlers{}).writeProblem(w, r, identity.ErrRecentAuthenticationRequired, membershipInvitationAdminProblem)
		},
		func(w http.ResponseWriter, r *http.Request) {
			(membershipInvitationHandlers{}).writeProblem(w, r, identity.ErrRecentAuthenticationRequired, membershipInvitationTokenProblem)
		},
	} {
		w := httptest.NewRecorder()
		write(w, httptest.NewRequest(http.MethodPost, "/", nil))
		var p Problem
		decodeJSON(t, w, &p)
		if w.Code != 403 || p.Code != "auth_recent_authentication_required" {
			t.Fatal("inconsistent recent-auth code")
		}
	}
}

func TestDeviceManagementCapabilityFlag(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		cfg := authFoundationConfig()
		cfg.Authentication.DeviceManagementEnabled = enabled
		w := performRequest(NewHandlerWithOptions(cfg, nil, Options{}), http.MethodGet, "/api/v1/auth/providers", "")
		var result authProvidersResponse
		decodeJSON(t, w, &result)
		if result.DeviceManagementEnabled != enabled {
			t.Fatal("device capability ignored")
		}
	}
}
