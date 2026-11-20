package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type mfaHTTPService struct {
	fakeNativeAuth
	verifyErr, manageErr error
	reset                bool
	verifications        int
}

func (s *mfaHTTPService) PendingMFA(_ context.Context, binding string) (identity.MFAState, error) {
	if binding != nativeTestBinding {
		return identity.MFAState{}, identity.ErrAuthTransactionUnavailable
	}
	return identity.MFAState{Methods: []string{"totp", "recovery_code", "passkey"}, UserID: nativeTestChallengeID, ExpiresInSeconds: 215}, nil
}
func (s *mfaHTTPService) MFAPasskeyOptions(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`{"userVerification":"required"}`), nil
}
func (s *mfaHTTPService) VerifyMFA(_ context.Context, _ string, input identity.MFAVerifyInput) (identity.MFAVerifyResult, error) {
	s.verifications++
	return identity.MFAVerifyResult{PasswordReset: s.reset, ReturnTo: "/app/classes", Session: s.session()}, s.verifyErr
}
func (s *mfaHTTPService) EnrollTOTP(context.Context, identity.Principal) (identity.TOTPEnrollment, error) {
	return identity.TOTPEnrollment{Secret: "test-enrollment", Issuer: "TutorHub", AccountName: "account", OTPAuthURI: "otpauth://totp/account"}, s.manageErr
}
func (s *mfaHTTPService) ConfirmTOTP(_ context.Context, _ identity.Principal, code string) (identity.TOTPConfirmation, error) {
	if code != "123456" {
		return identity.TOTPConfirmation{}, identity.ErrNativeInvalidCode
	}
	codes, _ := s.RegenerateRecoveryCodes(context.Background(), identity.Principal{})
	return identity.TOTPConfirmation{RecoveryCodes: codes, Session: identity.ReauthenticateResult{SessionToken: "new-session", CSRFToken: "new-csrf", CookieExpiresAt: fixedTime.Add(time.Hour)}}, s.manageErr
}
func (s *mfaHTTPService) DisableTOTP(context.Context, identity.Principal) error { return s.manageErr }
func (s *mfaHTTPService) RegenerateRecoveryCodes(context.Context, identity.Principal) ([]string, error) {
	codes := make([]string, 10)
	for i := range codes {
		codes[i] = "recovery-" + strconv.Itoa(i)
	}
	return codes, s.manageErr
}
func mfaTestHandler(s *mfaHTTPService, enabled bool, limiter InvitationRateLimiter, now *time.Time) http.Handler {
	cfg := authFoundationConfig()
	cfg.Authentication.Enabled = true
	cfg.Authentication.MFATOTPEnabled = enabled
	cfg.Authentication.PasskeysEnabled = true
	cfg.Authentication.WebAuthnRPID = "app.example"
	cfg.Authentication.WebAuthnRPName = "TutorHub"
	cfg.Authentication.WebAuthnRPOrigins = []string{cfg.WebOrigin}
	return NewHandlerWithOptions(cfg, nil, Options{NativeAuth: s, Identity: &fakeIdentityService{}, AuthRateLimiter: limiter, Clock: func() time.Time { return *now }})
}
func TestMFAPreviewAndPasskeyOptions(t *testing.T) {
	now := fixedTime
	h := mfaTestHandler(&mfaHTTPService{}, true, &nativeTestLimiter{}, &now)
	for _, tc := range []struct {
		binding string
		status  int
	}{{"", 404}, {strings.Repeat("z", 43), 404}, {nativeTestBinding, 200}} {
		r := httptest.NewRequest("GET", "/api/v1/auth/mfa", nil)
		if tc.binding != "" {
			r.AddCookie(&http.Cookie{Name: identity.AuthTransactionCookieName(false), Value: tc.binding})
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status || strings.Contains(w.Body.String(), nativeTestChallengeID.String()) || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("preview code=%d body=%s", w.Code, w.Body)
		}
		if tc.status == 200 && !strings.Contains(w.Body.String(), `"expires_in_seconds":215`) {
			t.Fatal("preview expiry missing")
		}
	}
	r := nativeRequest("/api/v1/auth/mfa/passkey/options", `{}`, false)
	r.AddCookie(&http.Cookie{Name: identity.AuthTransactionCookieName(false), Value: nativeTestBinding})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "required") || len(w.Result().Cookies()) != 0 {
		t.Fatalf("passkey options: %d %s", w.Code, w.Body)
	}
}

func TestMFAVerifyLimitFollowsAccountAcrossIPs(t *testing.T) {
	now := fixedTime.Truncate(5 * time.Minute)
	s := &mfaHTTPService{verifyErr: identity.ErrNativeInvalidCode}
	h := mfaTestHandler(s, true, newFixedWindowInvitationRateLimiter(100, authRateLimitPolicies(200, 50)), &now)
	request := func(ip int) *httptest.ResponseRecorder {
		r := nativeRequest("/api/v1/auth/mfa/verify", `{"method":"totp","code":"123456"}`, false)
		r.RemoteAddr = "203.0." + strconv.Itoa(ip) + ".10:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for i := 0; i < 5; i++ {
		if w := request(i); w.Code != 401 {
			t.Fatalf("failed attempt %d: %d %s", i+1, w.Code, w.Body)
		}
	}
	s.verifyErr = nil
	if w := request(6); w.Code != 429 || w.Header().Get("Retry-After") == "" || s.verifications != 5 {
		t.Fatalf("account limit: %d calls=%d", w.Code, s.verifications)
	}
	now = now.Add(5 * time.Minute)
	w := request(7)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"return_to":"/app/classes"`) || strings.Contains(w.Body.String(), "token") {
		t.Fatalf("login: %d %s", w.Code, w.Body)
	}
	if findCookie(t, w.Result().Cookies(), identity.AuthTransactionCookieName(false)).MaxAge >= 0 || findCookie(t, w.Result().Cookies(), "tutorhub_session").Value == "" {
		t.Fatal("wrong login cookies")
	}
	s.reset = true
	w = request(8)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "password_reset") || strings.Contains(w.Body.String(), "return_to") || len(w.Result().Cookies()) != 3 {
		t.Fatalf("reset: %d %s", w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.MaxAge >= 0 {
			t.Fatal("reset left active cookie")
		}
	}
}

func TestMFAManagementCookiesGuardsAndErrors(t *testing.T) {
	now := fixedTime
	s := &mfaHTTPService{}
	h := mfaTestHandler(s, true, &nativeTestLimiter{}, &now)
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/v1/me/mfa/totp/enroll", `{}`, 200},
		{"POST", "/api/v1/me/mfa/totp/confirm", `{"code":"123456"}`, 200},
		{"POST", "/api/v1/me/mfa/recovery-codes", `{}`, 200},
		{"DELETE", "/api/v1/me/mfa/totp", "", 204},
	} {
		for _, header := range []string{"Origin", "X-CSRF-Token", ""} {
			r := securityRequest(tc.method, tc.path, tc.body)
			if header != "" {
				r.Header.Del(header)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if header != "" {
				if w.Code != 403 {
					t.Fatalf("%s %s: %d", tc.path, header, w.Code)
				}
				continue
			}
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("%s code=%d body=%s", tc.path, w.Code, w.Body)
			}
			if strings.HasSuffix(tc.path, "confirm") || strings.HasSuffix(tc.path, "recovery-codes") {
				var resp recoveryCodesResponse
				if json.Unmarshal(w.Body.Bytes(), &resp) != nil || len(resp.RecoveryCodes) != 10 || strings.Contains(w.Body.String(), "test-enrollment") {
					t.Fatal("recovery codes not isolated")
				}
			}
			if strings.HasSuffix(tc.path, "confirm") && (len(w.Result().Cookies()) != 2 || strings.Contains(w.Body.String(), "new-session")) {
				t.Fatal("confirmation did not rotate cookies privately")
			}
		}
	}
	for _, err := range []error{identity.ErrRecentAuthenticationRequired, identity.ErrFederationAALRequired} {
		s.manageErr = err
		r := securityRequest("DELETE", "/api/v1/me/mfa/totp", "")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("disable without sufficient proof: %d", w.Code)
		}
	}
	h = mfaTestHandler(s, false, &nativeTestLimiter{}, &now)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, securityRequest("POST", "/api/v1/me/mfa/totp/enroll", `{}`))
	if w.Code != 404 {
		t.Fatalf("disabled MFA status=%d", w.Code)
	}
}
