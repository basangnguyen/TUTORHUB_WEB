package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

// These endpoints must reject the request before invoking any ceremony.
// Successful signed ceremonies run through the real protocol in the identity tests.
type guardedPasskeyService struct {
	fakeNativeAuth
	identity.PasskeyAPI
}

func TestPasskeyHTTPBoundaries(t *testing.T) {
	const login = "/api/v1/auth/passkeys/assertion"
	const me = "/api/v1/me/passkeys"
	const resource = me + "/38ce9dc8-e48d-4c41-be0e-64ecbe3f58ff"
	for _, tc := range []struct {
		method, path, body string
		preauth            bool
	}{
		{"POST", login + "/options", "{}", true},
		{"POST", login + "/verify", `{"assertion":{}}`, true},
		{"POST", "/api/v1/auth/reauth/passkey/options", "{}", false},
		{"POST", "/api/v1/auth/reauth", `{"method":"passkey","assertion":{}}`, false},
		{"GET", me, "", false},
		{"POST", me + "/registration/options", "{}", false},
		{"POST", me + "/registration/verify", `{"attestation":{},"label":"Desktop"}`, false},
		{"PATCH", resource, `{"label":"Desktop"}`, false},
		{"DELETE", resource, "", false},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			run := func(enabled bool, mutate func(*http.Request), deny InvitationRateLimitAction, want int) {
				t.Helper()
				cfg := authFoundationConfig()
				cfg.Authentication.PasskeysEnabled = enabled
				cfg.Authentication.WebAuthnRPID = "app.example"
				cfg.Authentication.WebAuthnRPName = "TutorHub"
				cfg.Authentication.WebAuthnRPOrigins = []string{cfg.WebOrigin}
				cfg.Authentication.Enabled = true
				limiter := &nativeTestLimiter{deny: deny}
				handler := NewHandlerWithOptions(cfg, nil, Options{Identity: &fakeIdentityService{}, NativeAuth: &guardedPasskeyService{}, AuthRateLimiter: limiter})
				r := securityRequest(tc.method, tc.path, tc.body)
				if mutate != nil {
					mutate(r)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != want || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
					t.Errorf("status=%d want=%d body=%s", w.Code, want, w.Body)
				}
				if strings.Contains(w.Body.String(), "unknown_credential") {
					t.Error("unexpected credential signal")
				}
			}
			run(false, nil, "", 404)
			if tc.method != "GET" {
				run(true, func(r *http.Request) { r.Header.Set("Origin", "https://wrong.example") }, "", 403)
			}
			if tc.preauth {
				action := AuthRateLimitPasskeyOptionsIP
				if strings.HasSuffix(tc.path, "verify") {
					action = AuthRateLimitPasskeyVerifyIP
				}
				run(true, nil, action, 429)
				if strings.HasSuffix(tc.path, "verify") {
					run(true, nil, "", 401)
				}
			} else {
				run(true, func(r *http.Request) { r.Header.Del("Cookie") }, "", 401)
				if tc.method != "GET" {
					run(true, func(r *http.Request) { r.Header.Del(csrfHeader) }, "", 403)
				}
			}
		})
	}
	policies := authRateLimitPolicies(0, 0)
	if p := policies[AuthRateLimitPasskeyOptionsIP]; p.Limit != 30 || p.Window != time.Minute {
		t.Fatal("options budget")
	}
	if p := policies[AuthRateLimitPasskeyVerifyIP]; p.Limit != 30 || p.Window != 15*time.Minute {
		t.Fatal("verify budget")
	}
}

func TestPasskeyCapabilityAndProblemSignal(t *testing.T) {
	cfg := authFoundationConfig()
	cfg.Authentication.PasskeysEnabled = true
	cfg.Authentication.WebAuthnRPName = "TutorHub"
	cfg.Authentication.WebAuthnRPOrigins = []string{cfg.WebOrigin}
	for _, tc := range []struct {
		id         string
		advertised bool
	}{{"wrong.example", false}, {"app.example", true}} {
		cfg.Authentication.WebAuthnRPID = tc.id
		w := performRequest(NewHandler(cfg, nil), http.MethodGet, "/api/v1/auth/providers", "")
		var caps authProvidersResponse
		decodeJSON(t, w, &caps)
		if caps.PasskeysEnabled != tc.advertised {
			t.Fatal("passkey advertisement differs from RP policy")
		}
	}
	h := nativeAuthHandlers{}
	for _, tc := range []struct {
		err    error
		status int
		code   string
		signal bool
	}{
		{identity.ErrPasskeyUnknown, 401, "auth_invalid_passkey", true},
		{identity.ErrInvalidPasskey, 401, "auth_invalid_passkey", false},
		{identity.ErrPasskeyClone, 403, "auth_passkey_clone_detected", false},
		{identity.ErrLastIdentity, 409, "auth_last_identity", false},
		{identity.ErrRecentAuthenticationRequired, 403, "auth_recent_authentication_required", false},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/v1/auth/passkeys/assertion/verify", nil)
		h.writePasskeyError(w, r, tc.err)
		var problem Problem
		decodeJSON(t, w, &problem)
		if w.Code != tc.status || problem.Code != tc.code || problem.UnknownCredential != tc.signal || problem.RequestID != RequestIDFromContext(r.Context()) {
			t.Errorf("unexpected problem %+v", problem)
		}
	}
}
