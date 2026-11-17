package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func (service *fakeNativeAuth) StartEmailCodeLogin(ctx context.Context, input identity.SignupStartInput) (identity.SignupChallengeResult, error) {
	return service.StartEmailSignup(ctx, input)
}
func (service *fakeNativeAuth) StartPasswordRecovery(ctx context.Context, input identity.SignupStartInput) (identity.SignupChallengeResult, error) {
	return service.StartEmailSignup(ctx, input)
}
func (service *fakeNativeAuth) VerifyEmailCodeLogin(_ context.Context, binding string, input identity.EmailCodeVerifyInput) (identity.NativeSessionResult, error) {
	service.calls++
	service.bound = binding
	service.emailVerifyInput = input
	return service.session(), service.err
}
func (service *fakeNativeAuth) CompletePasswordRecovery(_ context.Context, binding string, input identity.PasswordRecoveryCompleteInput) error {
	service.calls++
	service.bound = binding
	service.recoveryInput = input
	return service.err
}
func (service *fakeNativeAuth) ResendEmailChallenge(ctx context.Context, binding string, flags identity.AuthMethodFlags, allowed func(context.Context, string) (bool, error)) (identity.SignupChallengeResult, error) {
	service.methodFlags = flags
	return service.ResendSignupChallenge(ctx, binding, allowed)
}
func (service *fakeNativeAuth) GetMyPasswordStatus(_ context.Context, principal identity.Principal) (identity.MyPasswordStatus, error) {
	service.calls++
	service.passwordPrincipal = principal
	updated := fixedTime
	return identity.MyPasswordStatus{HasPassword: true, UpdatedAt: &updated}, service.err
}
func (service *fakeNativeAuth) UpdateMyPassword(_ context.Context, principal identity.Principal, input identity.UpdateMyPasswordInput) (identity.UpdateMyPasswordResult, error) {
	service.calls++
	service.passwordPrincipal = principal
	service.passwordInput = input
	return identity.UpdateMyPasswordResult{HasPassword: true, UpdatedAt: fixedTime, SessionToken: "new-private-session", CSRFToken: "new-private-csrf", CookieExpiresAt: fixedTime.Add(30 * 24 * time.Hour)}, service.err
}

func accountTestHandler(service *fakeNativeAuth, passwordEnabled bool) http.Handler {
	cfg := nativeTestConfig()
	cfg.Authentication.Enabled = true
	cfg.Authentication.PasswordEnabled = passwordEnabled
	cfg.Authentication.EmailCodeEnabled = true
	return NewHandlerWithOptions(cfg, nil, Options{NativeAuth: service, Identity: &fakeIdentityService{}, AuthRateLimiter: &nativeTestLimiter{}, Clock: func() time.Time { return fixedTime }})
}

func accountCodeBody() string {
	return `{"challenge_id":"` + nativeTestChallengeID.String() + `","code":"123456","keep_signed_in":true}`
}
func recoveryBody() string {
	return `{"challenge_id":"` + nativeTestChallengeID.String() + `","code":"123456","new_password":"new-private-password"}`
}

func TestAccountEmailRoutesRequireFlagsAndPreAuthGuard(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/email-code/start", "/api/v1/auth/email-code/verify", "/api/v1/auth/password/recovery/start", "/api/v1/auth/password/recovery/complete"} {
		service := &fakeNativeAuth{}
		w := httptest.NewRecorder()
		nativeTestHandler(authFoundationConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest(path, `{}`, false))
		if w.Code != 503 || service.calls != 0 {
			t.Fatalf("disabled method %s reached engine", path)
		}
		for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Origin") }, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }} {
			r := nativeRequest(path, `{}`, false)
			mutate(r)
			w = httptest.NewRecorder()
			accountTestHandler(service, true).ServeHTTP(w, r)
			if (w.Code != 403 && w.Code != 415) || service.calls != 0 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("pre-auth boundary bypass")
			}
		}
	}
	for _, path := range []string{"/api/v1/auth/password/recovery/start", "/api/v1/auth/password/recovery/complete"} {
		service := &fakeNativeAuth{}
		w := httptest.NewRecorder()
		accountTestHandler(service, false).ServeHTTP(w, nativeRequest(path, `{}`, false))
		if w.Code != 503 || service.calls != 0 {
			t.Fatal("recovery must require both password and email-code")
		}
	}
}

func TestAccountEmailStartGenericResponseAndRateLimits(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/email-code/start", "/api/v1/auth/password/recovery/start"} {
		var expected string
		for _, email := range []string{"active@example.test", "unknown@example.test"} {
			for _, deny := range []InvitationRateLimitAction{"", AuthRateLimitOTPSendDestination} {
				cfg := nativeTestConfig()
				cfg.Authentication.EmailCodeEnabled = true
				service := &fakeNativeAuth{}
				limiter := &nativeTestLimiter{deny: deny}
				w := httptest.NewRecorder()
				nativeTestHandler(cfg, service, limiter, nil).ServeHTTP(w, nativeRequest(path, `{"email":"`+email+`"}`, false))
				if w.Code != 202 || service.calls != 1 || service.startInput.DestinationAllowed != (deny == "") {
					t.Fatal("start did not preserve generic destination quota result")
				}
				if expected == "" {
					expected = w.Body.String()
				}
				if expected != w.Body.String() || strings.Contains(expected, email) {
					t.Fatal("email enumeration response")
				}
				if len(limiter.calls) != 2 || limiter.calls[0].action != AuthRateLimitOTPSendIP || limiter.calls[1].action != AuthRateLimitOTPSendDestination {
					t.Fatal("start quotas missing")
				}
			}
		}
		cfg := nativeTestConfig()
		cfg.Authentication.EmailCodeEnabled = true
		service := &fakeNativeAuth{}
		w := httptest.NewRecorder()
		nativeTestHandler(cfg, service, &nativeTestLimiter{deny: AuthRateLimitOTPSendIP}, nil).ServeHTTP(w, nativeRequest(path, `{"email":"person@example.test"}`, false))
		if w.Code != 429 || w.Header().Get("Retry-After") == "" || service.calls != 0 {
			t.Fatal("IP quota bypass")
		}
	}
}

func TestAccountEmailVerificationCookiesAndGenericFailures(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/email-code/verify", "/api/v1/auth/password/recovery/complete"} {
		body := accountCodeBody()
		if strings.Contains(path, "recovery") {
			body = recoveryBody()
		}
		for _, err := range []error{identity.ErrNativeInvalidCode, identity.ErrAuthTransactionUnavailable, fmt.Errorf("%w: secret@example.test", identity.ErrNativeSignupUnavailable)} {
			service := &fakeNativeAuth{err: err}
			w := httptest.NewRecorder()
			accountTestHandler(service, true).ServeHTTP(w, nativeRequest(path, body, false))
			var problem Problem
			decodeJSON(t, w, &problem)
			if w.Code != 401 || problem.Detail != "Invalid or expired verification code." || len(w.Result().Cookies()) != 0 {
				t.Fatal("verification failure leaked state or changed cookies")
			}
		}
		for _, mutation := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Cookie") }, func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: nativeTestBinding}) }} {
			service := &fakeNativeAuth{}
			r := nativeRequest(path, body, false)
			mutation(r)
			w := httptest.NewRecorder()
			accountTestHandler(service, true).ServeHTTP(w, r)
			if w.Code != 401 || service.calls != 0 {
				t.Fatal("missing/duplicate transaction binding reached service")
			}
		}
		service := &fakeNativeAuth{persistent: true}
		w := httptest.NewRecorder()
		accountTestHandler(service, true).ServeHTTP(w, nativeRequest(path, body, false))
		if w.Code != 200 || service.bound != nativeTestBinding {
			t.Fatal("completion failed")
		}
		if findCookie(t, w.Result().Cookies(), "tutorhub_auth_tx").MaxAge != -1 {
			t.Fatal("auth transaction not cleared")
		}
		if strings.Contains(path, "recovery") {
			if findCookie(t, w.Result().Cookies(), "tutorhub_session").MaxAge != -1 || findCookie(t, w.Result().Cookies(), "tutorhub_csrf").MaxAge != -1 || !strings.Contains(w.Body.String(), "password_reset") {
				t.Fatal("recovery must clear every browser auth cookie without signing in")
			}
		} else if findCookie(t, w.Result().Cookies(), "tutorhub_session").MaxAge != 30*24*60*60 || !service.emailVerifyInput.KeepSignedIn {
			t.Fatal("email login must use server persistence and forward request")
		}
	}
}

func TestAccountCodeVerificationStrictInputAndVerifyQuota(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/email-code/verify", "/api/v1/auth/password/recovery/complete"} {
		for _, body := range []string{`[]`, `{"code":"1","code":"2"}`, `{"challenge_id":"` + nativeTestChallengeID.String() + `","email":"changed@example.test"}`, `{} {}`} {
			service := &fakeNativeAuth{}
			w := httptest.NewRecorder()
			accountTestHandler(service, true).ServeHTTP(w, nativeRequest(path, body, false))
			if w.Code != 400 || service.calls != 0 {
				t.Fatal("invalid or mutable destination input reached service")
			}
		}
		cfg := nativeTestConfig()
		cfg.Authentication.EmailCodeEnabled = true
		service := &fakeNativeAuth{}
		w := httptest.NewRecorder()
		nativeTestHandler(cfg, service, &nativeTestLimiter{deny: AuthRateLimitOTPVerifyIP}, nil).ServeHTTP(w, nativeRequest(path, `{}`, false))
		if w.Code != 429 || service.calls != 0 {
			t.Fatal("verify quota missing")
		}
	}
}

func TestResendUsesTransactionMethodFlags(t *testing.T) {
	cfg := nativeTestConfig()
	cfg.Authentication.SignupEnabled = false
	cfg.Authentication.EmailCodeEnabled = true
	cfg.Authentication.PasswordEnabled = false
	service := &fakeNativeAuth{}
	w := httptest.NewRecorder()
	nativeTestHandler(cfg, service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/challenges/resend", `{}`, false))
	if w.Code != 202 || service.methodFlags.SignupEnabled || !service.methodFlags.EmailCodeEnabled || service.methodFlags.PasswordEnabled {
		t.Fatal("resend still relies on signup flag")
	}
	service.err = identity.ErrNativeMethodDisabled
	w = httptest.NewRecorder()
	nativeTestHandler(cfg, service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/challenges/resend", `{}`, false))
	if w.Code != 503 {
		t.Fatal("transaction's disabled method must remain unavailable")
	}
}

func passwordSettingsRequest(method, body string, session, csrf bool) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/me/password", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if session {
		r.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	}
	if csrf {
		r.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
		r.Header.Set("X-CSRF-Token", "csrf-token")
	}
	return r
}

func TestPasswordSettingsSessionCSRFAndFeatureBoundary(t *testing.T) {
	for _, tc := range []struct {
		method                 string
		session, csrf, enabled bool
		status                 int
	}{{"GET", false, false, true, 401}, {"POST", false, false, true, 401}, {"POST", true, false, true, 403}, {"POST", true, true, false, 503}} {
		service := &fakeNativeAuth{}
		w := httptest.NewRecorder()
		accountTestHandler(service, tc.enabled).ServeHTTP(w, passwordSettingsRequest(tc.method, `{"new_password":"secret"}`, tc.session, tc.csrf))
		if w.Code != tc.status || service.calls != 0 {
			t.Fatalf("settings boundary got %d want %d", w.Code, tc.status)
		}
	}
	service := &fakeNativeAuth{}
	w := httptest.NewRecorder()
	accountTestHandler(service, false).ServeHTTP(w, passwordSettingsRequest("GET", "", true, false))
	var result myPasswordStatusResponse
	decodeJSON(t, w, &result)
	if w.Code != 200 || result.PasswordEnabled || !result.HasPassword || result.UpdatedAt == nil || w.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("GET must show stored status with feature disabled")
	}
}

func TestPasswordSettingsRotationAndDomainErrors(t *testing.T) {
	service := &fakeNativeAuth{}
	w := httptest.NewRecorder()
	accountTestHandler(service, true).ServeHTTP(w, passwordSettingsRequest("POST", `{"current_password":"old-private","new_password":"new-private"}`, true, true))
	var body map[string]json.RawMessage
	decodeJSON(t, w, &body)
	if w.Code != 200 || len(body) != 2 || service.passwordInput.CurrentPassword == nil || *service.passwordInput.CurrentPassword != "old-private" || service.passwordPrincipal.User.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("password update input/principal mismatch")
	}
	if findCookie(t, w.Result().Cookies(), "tutorhub_session").MaxAge != 30*24*60*60 || findCookie(t, w.Result().Cookies(), "tutorhub_csrf").Value != "new-private-csrf" {
		t.Fatal("password update must rotate cookies with preserved lifetime")
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{identity.ErrRecentAuthenticationRequired, 403, ""}, {identity.ErrNativeInvalidCurrentPassword, 401, "auth_invalid_current_password"}, {identity.ErrNativePasswordUnchanged, 400, "auth_password_unchanged"}, {identity.ErrPasswordPolicy, 400, "auth_password_policy"}, {identity.ErrNativeVerifiedEmailRequired, 403, "auth_verified_email_required"}, {identity.ErrPasswordBusy, 503, ""}} {
		service := &fakeNativeAuth{err: fmt.Errorf("%w: private-secret", tc.err)}
		w := httptest.NewRecorder()
		accountTestHandler(service, true).ServeHTTP(w, passwordSettingsRequest("POST", `{"new_password":"new-private"}`, true, true))
		var problem Problem
		decodeJSON(t, w, &problem)
		if w.Code != tc.status || problem.Code != tc.code || strings.Contains(problem.Detail, "private") || len(w.Result().Cookies()) != 0 {
			t.Fatal("settings error exposed details or mutated cookies")
		}
	}
}

func TestPasswordRecoveryCapabilityRequiresBothFlags(t *testing.T) {
	for _, password := range []bool{false, true} {
		for _, email := range []bool{false, true} {
			cfg := authFoundationConfig()
			cfg.Authentication.PasswordEnabled = password
			cfg.Authentication.EmailCodeEnabled = email
			w := performRequest(NewHandlerWithOptions(cfg, nil, Options{}), http.MethodGet, "/api/v1/auth/providers", "")
			var result authProvidersResponse
			decodeJSON(t, w, &result)
			if result.PasswordRecoveryEnabled != (password && email) {
				t.Fatal("recovery capability must require both flags")
			}
		}
	}
}
