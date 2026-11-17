package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

const nativeTestBinding = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var nativeTestChallengeID = uuid.MustParse("d7b43df4-1b91-4d06-b106-bd34bdc806a5")

type fakeNativeAuth struct {
	startInput               identity.SignupStartInput
	completeInput            identity.SignupCompleteInput
	loginInput               identity.PasswordLoginInput
	bound                    string
	calls                    int
	err                      error
	secure                   bool
	persistent               bool
	resendDestinationAllowed bool
}

func (service *fakeNativeAuth) challenge() identity.SignupChallengeResult {
	return identity.SignupChallengeResult{ChallengeID: nativeTestChallengeID, CooldownSeconds: 60, ExpiresInSeconds: 600,
		Cookie: &http.Cookie{Name: identity.AuthTransactionCookieName(service.secure), Value: nativeTestBinding, Path: "/", HttpOnly: true,
			Secure: service.secure, SameSite: http.SameSiteStrictMode, MaxAge: 900, Expires: fixedTime.Add(15 * time.Minute)}}
}
func (service *fakeNativeAuth) session() identity.NativeSessionResult {
	return identity.NativeSessionResult{SessionToken: "private-session-token", CSRFToken: "private-csrf-token", ExpiresAt: fixedTime.Add(8 * time.Hour), AbsoluteExpiresAt: fixedTime.Add(30 * 24 * time.Hour), Persistent: service.persistent}
}
func (service *fakeNativeAuth) StartEmailSignup(_ context.Context, input identity.SignupStartInput) (identity.SignupChallengeResult, error) {
	service.calls++
	service.startInput = input
	return service.challenge(), service.err
}
func (service *fakeNativeAuth) ResendSignupChallenge(ctx context.Context, binding string, allowed func(context.Context, string) (bool, error)) (identity.SignupChallengeResult, error) {
	service.calls++
	service.bound = binding
	if service.err != nil {
		return identity.SignupChallengeResult{}, service.err
	}
	var err error
	service.resendDestinationAllowed, err = allowed(ctx, "immutable@example.test")
	return service.challenge(), err
}
func (service *fakeNativeAuth) CompleteEmailSignup(_ context.Context, binding string, input identity.SignupCompleteInput) (identity.NativeSessionResult, error) {
	service.calls++
	service.bound = binding
	service.completeInput = input
	return service.session(), service.err
}
func (service *fakeNativeAuth) PasswordLogin(_ context.Context, input identity.PasswordLoginInput) (identity.NativeSessionResult, error) {
	service.calls++
	service.loginInput = input
	return service.session(), service.err
}

type nativeRateCall struct {
	action InvitationRateLimitAction
	bucket string
}
type nativeTestLimiter struct {
	calls []nativeRateCall
	deny  InvitationRateLimitAction
	fail  InvitationRateLimitAction
}

func (limiter *nativeTestLimiter) Allow(_ context.Context, action InvitationRateLimitAction, bucket string, _ time.Time) InvitationRateLimitDecision {
	limiter.calls = append(limiter.calls, nativeRateCall{action, bucket})
	if limiter.fail == action {
		return InvitationRateLimitDecision{Err: errors.New("private database failure")}
	}
	if limiter.deny == action {
		return InvitationRateLimitDecision{RetryAfter: 90 * time.Second}
	}
	return InvitationRateLimitDecision{Allowed: true}
}

func nativeTestConfig() config.Config {
	cfg := authFoundationConfig()
	cfg.Authentication.SignupEnabled = true
	cfg.Authentication.PasswordEnabled = true
	return cfg
}
func nativeTestHandler(cfg config.Config, service identity.NativeAuthAPI, limiter InvitationRateLimiter, logger *slog.Logger) http.Handler {
	return NewHandlerWithOptions(cfg, logger, Options{NativeAuth: service, AuthRateLimiter: limiter, Clock: func() time.Time { return fixedTime }})
}
func nativeRequest(path, body string, secure bool) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("X-Request-ID", "native-auth-test")
	r.RemoteAddr = "203.0.113.42:1234"
	if strings.HasSuffix(path, "complete") || strings.HasSuffix(path, "resend") {
		r.AddCookie(&http.Cookie{Name: identity.AuthTransactionCookieName(secure), Value: nativeTestBinding})
	}
	return r
}
func signupCompleteBody() string {
	return `{"challenge_id":"` + nativeTestChallengeID.String() + `","code":"123456","display_name":"Student","password":"private-strong-passphrase","terms_version":"2026-11-16-v2.0","keep_signed_in":true}`
}

func TestNativeAuthRoutesHonorFlagsAndPreAuthBoundary(t *testing.T) {
	for _, path := range []string{"/api/v1/auth/signup/email/start", "/api/v1/auth/signup/email/complete", "/api/v1/auth/challenges/resend", "/api/v1/auth/password/login"} {
		t.Run(path, func(t *testing.T) {
			service := &fakeNativeAuth{}
			w := httptest.NewRecorder()
			nativeTestHandler(authFoundationConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest(path, `{}`, false))
			if w.Code != 503 || service.calls != 0 {
				t.Fatal("disabled native method reached service")
			}
			for _, mutation := range []func(*http.Request){
				func(r *http.Request) { r.Header.Del("Origin") },
				func(r *http.Request) { r.Header.Set("Origin", "https://attacker.example") },
				func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") },
				func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
			} {
				r := nativeRequest(path, `{}`, false)
				mutation(r)
				w = httptest.NewRecorder()
				nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, r)
				if (w.Code != 403 && w.Code != 415) || service.calls != 0 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
					t.Fatal("pre-auth guard bypassed")
				}
			}
		})
	}
}

func TestNativeSignupStartIsGenericAndDestinationThrottleRemains202(t *testing.T) {
	var expected string
	for _, tc := range []struct {
		email string
		deny  bool
	}{{"existing@example.test", false}, {"new@example.test", false}, {"new@example.test", true}} {
		service := &fakeNativeAuth{}
		limiter := &nativeTestLimiter{}
		if tc.deny {
			limiter.deny = AuthRateLimitOTPSendDestination
		}
		w := httptest.NewRecorder()
		body, _ := json.Marshal(emailSignupStartRequest{Email: tc.email})
		nativeTestHandler(nativeTestConfig(), service, limiter, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/signup/email/start", string(body), false))
		if w.Code != 202 || service.startInput.DestinationAllowed == tc.deny || service.calls != 1 {
			t.Fatal("start enumeration or destination quota regression")
		}
		if expected == "" {
			expected = w.Body.String()
		}
		if w.Body.String() != expected {
			t.Fatal("known/unknown/throttled response shapes differ")
		}
		cookie := findCookie(t, w.Result().Cookies(), "tutorhub_auth_tx")
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 900 {
			t.Fatal("transaction cookie not hardened")
		}
		want := []InvitationRateLimitAction{AuthRateLimitSignupIP, AuthRateLimitOTPSendIP, AuthRateLimitOTPSendDestination}
		for i, action := range want {
			if limiter.calls[i].action != action {
				t.Fatal("missing signup quota")
			}
		}
		if limiter.calls[0].bucket != "203.0.113.0/24" || limiter.calls[2].bucket != tc.email {
			t.Fatal("incorrect rate bucket")
		}
		if strings.Contains(w.Body.String(), tc.email) {
			t.Fatal("response leaked destination")
		}
	}
}

func TestNativeSignupStartNormalizesDestinationBeforeThrottling(t *testing.T) {
	service := &fakeNativeAuth{}
	limiter := &nativeTestLimiter{}
	w := httptest.NewRecorder()
	nativeTestHandler(nativeTestConfig(), service, limiter, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/signup/email/start", `{"email":"  STUDENT@Example.Test "}`, false))
	if w.Code != 202 || service.startInput.Email != "student@example.test" || limiter.calls[2].bucket != "student@example.test" {
		t.Fatal("normalization differs between rate and identity")
	}
}

func TestNativeAuthIPLimitsFailBeforeServiceAndNeverLeakStorageErrors(t *testing.T) {
	for _, tc := range []struct {
		path   string
		action InvitationRateLimitAction
	}{
		{"/api/v1/auth/signup/email/start", AuthRateLimitSignupIP}, {"/api/v1/auth/signup/email/start", AuthRateLimitOTPSendIP},
		{"/api/v1/auth/challenges/resend", AuthRateLimitOTPSendIP}, {"/api/v1/auth/signup/email/complete", AuthRateLimitOTPVerifyIP},
		{"/api/v1/auth/password/login", AuthRateLimitPasswordFailIP},
	} {
		for _, storageFailure := range []bool{false, true} {
			service := &fakeNativeAuth{}
			limiter := &nativeTestLimiter{deny: tc.action}
			want := 429
			if storageFailure {
				limiter.fail = tc.action
				want = 503
			}
			w := httptest.NewRecorder()
			nativeTestHandler(nativeTestConfig(), service, limiter, nil).ServeHTTP(w, nativeRequest(tc.path, `{}`, false))
			if w.Code != want || service.calls != 0 || w.Header().Get("Retry-After") == "" || strings.Contains(w.Body.String(), "private database") {
				t.Fatal("rate limit failure not private/fail-closed")
			}
		}
	}
}

func TestNativeAuthRejectsAmbiguousBodiesAndTransactionCookies(t *testing.T) {
	for _, tc := range []struct{ path, body string }{
		{"/api/v1/auth/signup/email/start", `{"email":"a@example.test","EMAIL":"b@example.test"}`},
		{"/api/v1/auth/signup/email/start", `{"email":"a@example.test","extra":true}`},
		{"/api/v1/auth/password/login", `{"identifier":"a@example.test","password":"one","password":"two"}`},
		{"/api/v1/auth/password/login", `null`}, {"/api/v1/auth/password/login", `{} {}`},
		{"/api/v1/auth/challenges/resend", `{"email":"attacker@example.test"}`},
		{"/api/v1/auth/signup/email/complete", `{"challenge_id":"not-a-uuid"}`},
	} {
		service := &fakeNativeAuth{}
		w := httptest.NewRecorder()
		nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest(tc.path, tc.body, false))
		if w.Code != 400 || service.calls != 0 {
			t.Fatal("ambiguous JSON reached authentication service")
		}
	}
	for _, duplicate := range []bool{false, true} {
		r := nativeRequest("/api/v1/auth/challenges/resend", `{}`, false)
		if duplicate {
			r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: nativeTestBinding})
		} else {
			r.Header.Del("Cookie")
		}
		service := &fakeNativeAuth{}
		w := httptest.NewRecorder()
		nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, r)
		if w.Code != 400 || service.calls != 0 {
			t.Fatal("missing or ambiguous transaction cookie accepted")
		}
	}
}

func TestNativeResendUsesImmutableServerDestinationAndCooldown(t *testing.T) {
	service := &fakeNativeAuth{}
	limiter := &nativeTestLimiter{deny: AuthRateLimitOTPSendDestination}
	w := httptest.NewRecorder()
	nativeTestHandler(nativeTestConfig(), service, limiter, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/challenges/resend", `{}`, false))
	if w.Code != 202 || service.bound != nativeTestBinding || service.resendDestinationAllowed || limiter.calls[1].bucket != "immutable@example.test" {
		t.Fatal("resend destination binding or generic quota response broken")
	}
	service.err = identity.ErrAuthChallengeThrottled
	w = httptest.NewRecorder()
	nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/challenges/resend", `{}`, false))
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatal("resend cooldown not surfaced")
	}
}

func TestNativeSignupCompletionSetsHardenedNewCookiesAndClearsTransaction(t *testing.T) {
	for _, secure := range []bool{false, true} {
		cfg := nativeTestConfig()
		cfg.Authentication.CookieSecure = secure
		service := &fakeNativeAuth{secure: secure, persistent: true}
		var logs bytes.Buffer
		w := httptest.NewRecorder()
		nativeTestHandler(cfg, service, &nativeTestLimiter{}, slog.New(slog.NewJSONHandler(&logs, nil))).ServeHTTP(w, nativeRequest("/api/v1/auth/signup/email/complete", signupCompleteBody(), secure))
		if w.Code != 201 || w.Body.String() != "{\"status\":\"authenticated\"}\n" || service.bound != nativeTestBinding || !service.completeInput.KeepSignedIn {
			t.Fatal("signup completion not authenticated")
		}
		prefix := ""
		if secure {
			prefix = "__Host-"
		}
		for _, name := range []string{"tutorhub_session", "tutorhub_csrf"} {
			cookie := findCookie(t, w.Result().Cookies(), prefix+name)
			if !cookie.HttpOnly || cookie.Secure != secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.MaxAge != 30*24*60*60 {
				t.Fatal("persistent session cookie is not hardened")
			}
		}
		cookie := findCookie(t, w.Result().Cookies(), prefix+"tutorhub_auth_tx")
		if cookie.MaxAge != -1 || cookie.SameSite != http.SameSiteStrictMode || cookie.Secure != secure {
			t.Fatal("auth transaction cookie not cleared")
		}
		for _, secret := range []string{"private-strong-passphrase", "123456", "private-session-token", "private-csrf-token"} {
			if strings.Contains(logs.String(), secret) || strings.Contains(w.Body.String(), secret) {
				t.Fatal("native authentication leaked secret")
			}
		}
	}
}

func TestNativePasswordLoginFailuresAreIndistinguishableAndBusyRetryable(t *testing.T) {
	var expected string
	for _, identityState := range []string{"unknown", "no-password", "locked", "incorrect-password"} {
		service := &fakeNativeAuth{err: fmt.Errorf("%w: private-%s", identity.ErrNativeInvalidCredentials, identityState)}
		w := httptest.NewRecorder()
		nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/password/login", `{"identifier":"`+identityState+`@example.test","password":"private-password"}`, false))
		if w.Code != 401 || !strings.Contains(w.Body.String(), "Invalid email or password.") || len(w.Result().Cookies()) != 0 {
			t.Fatal("invalid login disclosure")
		}
		if expected == "" {
			expected = w.Body.String()
		}
		if expected != w.Body.String() {
			t.Fatal("login states have distinct responses")
		}
	}
	service := &fakeNativeAuth{err: identity.ErrPasswordBusy}
	w := httptest.NewRecorder()
	nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/password/login", `{"identifier":"a@example.test","password":"private-password"}`, false))
	if w.Code != 503 || w.Header().Get("Retry-After") != "2" {
		t.Fatal("password capacity exhaustion must be retryable")
	}
}

func TestNativePasswordLoginUsesServerPersistenceDecision(t *testing.T) {
	service := &fakeNativeAuth{persistent: false}
	w := httptest.NewRecorder()
	nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/password/login", `{"identifier":"admin@example.test","password":"private-password","keep_signed_in":true}`, false))
	if w.Code != 200 || !service.loginInput.KeepSignedIn {
		t.Fatal("login request not forwarded")
	}
	cookie := findCookie(t, w.Result().Cookies(), "tutorhub_session")
	if cookie.MaxAge != 8*60*60 {
		t.Fatal("HTTP must obey service persistence decision, never caller's requested TTL")
	}
}

func TestNativeTermsAndPasswordPolicyErrorsAreActionableWithoutSecrets(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{identity.ErrNativeTermsVersion, "auth_terms_version_changed"}, {identity.ErrPasswordPolicy, "auth_password_policy"}, {identity.ErrNativeSignupUnavailable, ""}} {
		service := &fakeNativeAuth{err: fmt.Errorf("%w: private-email@example.test", tc.err)}
		w := httptest.NewRecorder()
		nativeTestHandler(nativeTestConfig(), service, &nativeTestLimiter{}, nil).ServeHTTP(w, nativeRequest("/api/v1/auth/signup/email/complete", signupCompleteBody(), false))
		var problem Problem
		decodeJSON(t, w, &problem)
		if w.Code != 400 || problem.Code != tc.code || strings.Contains(problem.Detail, "private-email") || len(w.Result().Cookies()) != 0 {
			t.Fatal("signup error disclosure or wrong status")
		}
	}
}

func TestNativePersistenceCookiesSurviveTenantAndArchiveRotation(t *testing.T) {
	handlers := newAuthHandlers(nativeTestConfig(), normalizeLogger(nil), nil, func() time.Time { return fixedTime })
	for _, lifetime := range []time.Duration{8 * time.Hour, 30 * 24 * time.Hour} {
		for _, archive := range []bool{false, true} {
			w := httptest.NewRecorder()
			if archive {
				handlers.setArchiveSessionCookies(w, identity.TenantArchiveResult{SessionToken: "new-session", CSRFToken: "new-csrf", ExpiresAt: fixedTime.Add(8 * time.Hour), CookieExpiresAt: fixedTime.Add(lifetime)})
			} else {
				handlers.setTenantSessionCookies(w, identity.TenantSessionResult{SessionToken: "new-session", CSRFToken: "new-csrf", ExpiresAt: fixedTime.Add(8 * time.Hour), CookieExpiresAt: fixedTime.Add(lifetime)})
			}
			for _, name := range []string{"tutorhub_session", "tutorhub_csrf"} {
				cookie := findCookie(t, w.Result().Cookies(), name)
				if cookie.MaxAge != int(lifetime.Seconds()) || !cookie.Expires.Equal(fixedTime.Add(lifetime)) {
					t.Fatal("rotation collapsed the server-supplied browser cookie lifetime")
				}
			}
		}
	}
	// Existing OIDC/fake-service results without the new field keep their prior TTL.
	if !authCookieExpiry(time.Time{}, fixedTime.Add(8*time.Hour)).Equal(fixedTime.Add(8 * time.Hour)) {
		t.Fatal("legacy cookie expiry fallback changed")
	}
}
