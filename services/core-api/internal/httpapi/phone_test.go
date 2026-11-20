package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type phoneHTTPService struct {
	fakeSecurityAuth
	known, destinationAllowed, delivered bool
	managementResend                     bool
}

func (s *phoneHTTPService) StartPhoneLogin(_ context.Context, input identity.PhoneStartInput) (identity.SignupChallengeResult, error) {
	s.calls++
	number, err := identity.NormalizePhoneNumber(input.PhoneNumber, input.CountryCode, []string{"VN"})
	if err != nil {
		return identity.SignupChallengeResult{}, err
	}
	if input.Guard == nil {
		return identity.SignupChallengeResult{}, identity.ErrPhoneDeliveryUnavailable
	}
	s.delivered = s.known && s.destinationAllowed
	result := s.challenge()
	result.ExpiresInSeconds = 300
	result.MaskedDestination = number.MaskedNumber
	return result, s.err
}

func (s *phoneHTTPService) StartMyPhone(ctx context.Context, p identity.Principal, input identity.PhoneStartInput) (identity.SignupChallengeResult, error) {
	s.principal = p
	return s.StartPhoneLogin(ctx, input)
}
func (s *phoneHTTPService) ResendPhoneChallenge(_ context.Context, b string, p *identity.Principal, _ identity.PhoneSendGuard) (identity.SignupChallengeResult, error) {
	s.calls++
	s.bound = b
	if s.managementResend && p == nil {
		return identity.SignupChallengeResult{}, identity.ErrInvalidCSRFToken
	}
	if p != nil {
		s.principal = *p
	}
	result := s.challenge()
	result.MaskedDestination = "+84 ***5678"
	result.ExpiresInSeconds = 300
	return result, s.err
}
func (s *phoneHTTPService) GetMyPhone(context.Context, identity.Principal) (*identity.PhoneSummary, error) {
	s.calls++
	return nil, s.err
}
func (s *phoneHTTPService) RemoveMyPhone(context.Context, identity.Principal) (identity.ReauthenticateResult, error) {
	s.calls++
	return identity.ReauthenticateResult{SessionToken: "rotated-session", CSRFToken: "rotated-csrf", CookieExpiresAt: fixedTime.Add(time.Hour)}, s.err
}
func (s *phoneHTTPService) VerifyPhoneLogin(_ context.Context, b string, input identity.EmailCodeVerifyInput) (identity.NativeSessionResult, error) {
	s.calls++
	s.bound = b
	s.emailVerifyInput = input
	return s.session(), s.err
}
func (s *phoneHTTPService) VerifyMyPhone(_ context.Context, p identity.Principal, b string, input identity.EmailCodeVerifyInput) (*identity.PhoneSummary, error) {
	s.calls++
	s.principal = p
	s.bound = b
	s.emailVerifyInput = input
	return &identity.PhoneSummary{ID: nativeTestChallengeID, CountryCode: "VN"}, s.err
}
func phoneHTTPHandler(s *phoneHTTPService, phone, devices bool, limiter InvitationRateLimiter) http.Handler {
	cfg := authFoundationConfig()
	cfg.Authentication.PhoneEnabled = phone
	cfg.Authentication.DeviceManagementEnabled = devices
	cfg.Authentication.PhoneSender = "devinbox"
	return NewHandlerWithOptions(cfg, nil, Options{NativeAuth: s, Identity: &fakeIdentityService{}, AuthRateLimiter: limiter, Clock: func() time.Time { return fixedTime }})
}

func TestPhoneLoginStartGenericWireResponseAndRedaction(t *testing.T) {
	var response string
	var headers http.Header
	for _, tc := range []struct{ known, allowed bool }{{true, true}, {false, true}, {true, false}} {
		s := &phoneHTTPService{known: tc.known, destinationAllowed: tc.allowed}
		r := nativeRequest("/api/v1/auth/phone/start", `{"phone_number":"0912 345 678","country_code":"VN"}`, false)
		w := httptest.NewRecorder()
		phoneHTTPHandler(s, true, false, &nativeTestLimiter{}).ServeHTTP(w, r)
		if w.Code != 202 || s.calls != 1 || s.delivered != (tc.known && tc.allowed) {
			t.Fatalf("unexpected status %d or delivery", w.Code)
		}
		if response != "" && (w.Body.String() != response || !reflect.DeepEqual(w.Header(), headers)) {
			t.Fatal("known, unknown and destination-limited responses differ")
		}
		response, headers = w.Body.String(), w.Header().Clone()
		var body signupChallengeResponse
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.ExpiresInSeconds != 300 || body.ResendAfterSeconds != 60 || body.MaskedDestination == "" || strings.Contains(response, "912345678") {
			t.Fatal("invalid phone challenge metadata", err)
		}
		if headers.Get("Cache-Control") != "no-store" || headers.Get("Referrer-Policy") != "no-referrer" || !findCookie(t, w.Result().Cookies(), "tutorhub_auth_tx").HttpOnly {
			t.Fatal("missing privacy headers")
		}
	}
	for _, value := range []slog.LogValuer{phoneStartRequest{PhoneNumber: "+84912345678"}, phoneVerifyRequest{Code: "123456"}} {
		if !strings.Contains(fmt.Sprint(value), "redacted") || !strings.Contains(value.LogValue().String(), "redacted") {
			t.Fatal("unredacted phone input")
		}
	}
}

func TestPhoneLoginVerificationTerminalOutcomes(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
		code   string
		clear  bool
	}{
		{identity.ErrPhoneSecondFactorRequired, 403, "auth_phone_second_factor_required", true},
		{identity.ErrPhoneStale, 403, "auth_phone_stale", true},
		{identity.ErrNativeInvalidCode, 401, "auth_invalid_verification_code", false},
		{nil, 200, "authenticated", true},
	} {
		s := &phoneHTTPService{}
		s.err = tc.err
		r := nativeRequest("/api/v1/auth/phone/verify", `{"challenge_id":"`+nativeTestChallengeID.String()+`","code":"123456","keep_signed_in":true}`, false)
		w := httptest.NewRecorder()
		phoneHTTPHandler(s, true, false, &nativeTestLimiter{}).ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || s.bound != nativeTestBinding || !s.emailVerifyInput.KeepSignedIn {
			t.Fatal("incorrect verification outcome", w.Code, w.Body)
		}
		cleared := false
		for _, c := range w.Result().Cookies() {
			if c.Name == "tutorhub_auth_tx" {
				cleared = c.MaxAge < 0
			}
			if tc.err != nil && c.Name != "tutorhub_auth_tx" {
				t.Fatal("failed proof set a session cookie")
			}
		}
		if cleared != tc.clear {
			t.Fatal("transaction cookie lifecycle")
		}
	}
	s := &phoneHTTPService{}
	s.mfa = &identity.MFAChallenge{Methods: []string{"passkey"}, Cookie: &http.Cookie{Name: "tutorhub_auth_tx", Value: "new-mfa-binding", HttpOnly: true}}
	w := httptest.NewRecorder()
	phoneHTTPHandler(s, true, false, &nativeTestLimiter{}).ServeHTTP(w, nativeRequest("/api/v1/auth/phone/verify", `{"challenge_id":"`+nativeTestChallengeID.String()+`","code":"123456"}`, false))
	cookies := w.Result().Cookies()
	if w.Code != 200 || w.Body.String() != "{\"status\":\"mfa_required\",\"methods\":[\"passkey\"]}\n" || len(cookies) != 1 || cookies[0].Value != "new-mfa-binding" {
		t.Fatal("passkey-only second step", w.Code, w.Body)
	}
}

func TestPhoneManagementGuardsAndRotation(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/v1/me/phone/start", `{"phone_number":"+84912345678"}`, 202},
		{"POST", "/api/v1/me/phone/verify", `{"challenge_id":"` + nativeTestChallengeID.String() + `","code":"123456"}`, 200},
		{"DELETE", "/api/v1/me/phone", "", 204},
	} {
		for _, guard := range []string{"Origin", "X-CSRF-Token", ""} {
			s := &phoneHTTPService{}
			w := httptest.NewRecorder()
			r := securityRequest(tc.method, tc.path, tc.body)
			if strings.HasSuffix(tc.path, "verify") {
				r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: nativeTestBinding})
			}
			if guard != "" {
				r.Header.Del(guard)
			}
			phoneHTTPHandler(s, true, false, &nativeTestLimiter{}).ServeHTTP(w, r)
			if guard != "" {
				if w.Code != 403 || s.calls != 0 {
					t.Fatal("mutation guard", tc.path, guard, w.Code)
				}
				continue
			}
			if w.Code != tc.status || s.calls != 1 {
				t.Fatal("management response", tc.path, w.Code, w.Body)
			}
			if tc.method == "DELETE" && len(w.Result().Cookies()) != 2 {
				t.Fatal("missing rotated session and CSRF cookies")
			}
		}
	}
	s := &phoneHTTPService{}
	w := httptest.NewRecorder()
	phoneHTTPHandler(s, false, true, &nativeTestLimiter{}).ServeHTTP(w, securityRequest("GET", "/api/v1/me/phone", ""))
	if w.Code != 503 || s.calls != 0 {
		t.Fatal("disabled phone method exposed state", w.Code)
	}
	w = httptest.NewRecorder()
	phoneHTTPHandler(s, true, false, &nativeTestLimiter{}).ServeHTTP(w, securityRequest("GET", "/api/v1/me/phone", ""))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"phone":null}` {
		t.Fatal("missing nullable phone state", w.Code, w.Body)
	}
}

func TestPhoneProblemsAndIPLimits(t *testing.T) {
	proof := `{"challenge_id":"` + nativeTestChallengeID.String() + `","code":"123456"}`
	for _, tc := range []struct {
		path, body string
		ipAction   InvitationRateLimitAction
	}{
		{"/api/v1/auth/phone/start", `{"phone_number":"0912345678"}`, AuthRateLimitOTPSendIP},
		{"/api/v1/auth/phone/verify", proof, AuthRateLimitOTPVerifyIP},
	} {
		s := &phoneHTTPService{}
		w := httptest.NewRecorder()
		phoneHTTPHandler(s, true, false, &nativeTestLimiter{deny: tc.ipAction}).ServeHTTP(w, nativeRequest(tc.path, tc.body, false))
		if w.Code != 429 || w.Header().Get("Retry-After") == "" || s.calls != 0 {
			t.Fatal("IP limit", tc.path, w.Code)
		}
	}
	for _, tc := range []struct {
		path, body string
		err        error
		status     int
		code       string
	}{
		{"/api/v1/auth/phone/start", `{"phone_number":"02438223344"}`, nil, 400, "auth_invalid_phone_number"},
		{"/api/v1/auth/phone/start", `{"phone_number":"0912345678"}`, identity.ErrPhoneDeliveryUnavailable, 503, "auth_phone_delivery_unavailable"},
		{"/api/v1/me/phone/start", `{"phone_number":"0912345678"}`, identity.ErrFederationAALRequired, 403, "auth_assurance_required"},
		{"/api/v1/me/phone/verify", proof, identity.ErrPhoneInUse, 409, "auth_phone_in_use"},
		{"/api/v1/me/phone", "", identity.ErrLastIdentity, 409, "auth_last_identity"},
	} {
		s := &phoneHTTPService{}
		s.err = tc.err
		r := nativeRequest(tc.path, tc.body, false)
		if strings.HasPrefix(tc.path, "/api/v1/me/") {
			method := "POST"
			if tc.path == "/api/v1/me/phone" {
				method = "DELETE"
			}
			r = securityRequest(method, tc.path, tc.body)
			if strings.HasSuffix(tc.path, "verify") {
				r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: nativeTestBinding})
			}
		}
		w := httptest.NewRecorder()
		phoneHTTPHandler(s, true, false, &nativeTestLimiter{}).ServeHTTP(w, r)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatal("phone problem", tc.code, w.Code, w.Body)
		}
	}
}

func TestPhoneResendChecksManagementSessionAndCSRF(t *testing.T) {
	const path = "/api/v1/auth/challenges/resend"
	for _, tc := range []struct {
		manage, sendCSRF bool
		status           int
	}{
		{false, false, 202}, {true, false, 403}, {true, true, 202},
	} {
		s := &phoneHTTPService{managementResend: tc.manage}
		limiter := &nativeTestLimiter{}
		r := nativeRequest(path, "{}", false)
		if tc.sendCSRF {
			r = securityRequest("POST", path, "{}")
			r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: nativeTestBinding})
		}
		w := httptest.NewRecorder()
		phoneHTTPHandler(s, true, false, limiter).ServeHTTP(w, r)
		if w.Code != tc.status || s.bound != nativeTestBinding || len(limiter.calls) != 1 || limiter.calls[0].action != AuthRateLimitOTPSendIP {
			t.Fatal("resend guard", tc, w.Code, w.Body)
		}
		if tc.status == 202 && !strings.Contains(w.Body.String(), `"resend_after_seconds":60`) {
			t.Fatal("phone resend metadata")
		}
	}
}
