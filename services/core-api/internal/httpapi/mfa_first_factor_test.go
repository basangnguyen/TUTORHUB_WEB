package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type mfaFirstFactorService struct {
	fakeNativeAuth
	identity.PasskeyAPI
}

func (s *mfaFirstFactorService) FinishPasskeyLogin(context.Context, string, json.RawMessage, bool, string, string) (identity.NativeSessionResult, error) {
	return s.session(), s.err
}

func TestMFAFirstFactorHTTPReturnsOnlyPendingTransaction(t *testing.T) {
	cfg := nativeTestConfig()
	cfg.Authentication.Enabled = true
	cfg.Authentication.EmailCodeEnabled = true
	cfg.Authentication.PasskeysEnabled = true
	cfg.Authentication.MFATOTPEnabled = true
	cfg.Authentication.WebAuthnRPID = "app.example"
	cfg.Authentication.WebAuthnRPName = "TutorHub"
	cfg.Authentication.WebAuthnRPOrigins = []string{cfg.WebOrigin}
	challenge := &identity.MFAChallenge{Methods: []string{"totp", "recovery_code", "passkey"},
		Cookie: &http.Cookie{Name: "tutorhub_auth_tx", Value: strings.Repeat("m", 43), HttpOnly: true, Path: "/", MaxAge: 300, SameSite: http.SameSiteStrictMode}}
	service := &mfaFirstFactorService{fakeNativeAuth: fakeNativeAuth{mfa: challenge}}
	handler := NewHandlerWithOptions(cfg, nil, Options{NativeAuth: service, Identity: &fakeIdentityService{}, AuthRateLimiter: &nativeTestLimiter{}})
	for _, request := range []struct{ path, body string }{
		{"/api/v1/auth/password/login", `{"identifier":"member@example.test","password":"fixture-password"}`},
		{"/api/v1/auth/email-code/verify", accountCodeBody()},
		{"/api/v1/auth/password/recovery/complete", recoveryBody()},
		{"/api/v1/auth/passkeys/assertion/verify", `{"assertion":{}}`},
	} {
		t.Run(request.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, nativeRequest(request.path, request.body, false))
			var response nativeAuthenticationResponse
			decodeJSON(t, w, &response)
			cookies := w.Result().Cookies()
			if w.Code != 200 || response.Status != "mfa_required" || !slices.Equal(response.Methods, challenge.Methods) ||
				len(cookies) != 1 || cookies[0].Name != challenge.Cookie.Name || cookies[0].Value != challenge.Cookie.Value || cookies[0].MaxAge != 300 {
				t.Fatalf("first factor status=%d, status field=%q, cookie count=%d", w.Code, response.Status, len(cookies))
			}
		})
	}
}

func TestTOTPReauthHTTPRequiresProofAndSharedAccountAndIPQuotas(t *testing.T) {
	cfg := authFoundationConfig()
	cfg.Authentication.Enabled = true
	const path = "/api/v1/auth/reauth"
	const valid = `{"method":"totp","code":"234567"}`
	for _, tc := range []struct {
		enabled bool
		body    string
		deny    InvitationRateLimitAction
		status  int
	}{
		{false, valid, "", 503}, {true, valid, "", 200},
		{true, valid, AuthRateLimitMFAUser, 429}, {true, valid, AuthRateLimitMFAIP, 429},
		{true, `{"method":"totp","code":"１２３４５６"}`, "", 400},
		{true, `{"method":"totp","code":"234567","password":"extra"}`, "", 400},
	} {
		cfg.Authentication.MFATOTPEnabled = tc.enabled
		limiter := &nativeTestLimiter{deny: tc.deny}
		service := &fakeSecurityAuth{}
		w := httptest.NewRecorder()
		NewHandlerWithOptions(cfg, nil, Options{NativeAuth: service, Identity: &fakeIdentityService{}, AuthRateLimiter: limiter}).ServeHTTP(w, securityRequest("POST", path, tc.body))
		if w.Code != tc.status || (service.calls == 1) != (tc.status == 200) {
			t.Fatalf("enabled=%t, quota=%s, want=%d got=%d", tc.enabled, tc.deny, tc.status, w.Code)
		}
		if tc.status == 200 && (service.reauthInput.Code != "234567" || len(limiter.calls) != 2 || limiter.calls[1].bucket != service.principal.User.ID.String() || len(w.Result().Cookies()) != 2) {
			t.Fatal("TOTP proof, account rate key or rotated cookies missing")
		}
	}
}
