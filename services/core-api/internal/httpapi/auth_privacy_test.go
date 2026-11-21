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

const (
	privacyEmail    = "fixture@campus.test"
	privacyPassword = "fixture-private-passphrase"
	privacyPhone    = "+84912345678"
	privacyDNS      = "tutorhub-site-verification=fixture-proof"
	privacyOAuth    = "fixture-oauth-code"
	privacyVerifier = "fixture-pkce-verifier"
	privacyProvider = "fixture-provider-credential"
	privacyRecovery = "ABCD-EFGH-2345"
	privacyPasskey  = "fixture-credential-id"
)

var privacyMarkers = []string{
	privacyEmail, privacyPassword, privacyPhone, privacyDNS, privacyOAuth, privacyVerifier,
	privacyProvider, privacyRecovery, privacyPasskey, "123456", "test-enrollment",
	"new-private-password", "session-token", "csrf-token", nativeTestBinding,
	"fixture@unavailable.test", "private database diagnostic", "private-flow-binding",
}

func authPrivacyConfig() config.Config {
	cfg := nativeTestConfig()
	auth := &cfg.Authentication
	auth.Enabled = true
	auth.EmailCodeEnabled = true
	auth.PhoneEnabled = true
	auth.PhoneSender = "devinbox"
	auth.MFATOTPEnabled = true
	auth.PasskeysEnabled = true
	auth.WebAuthnRPID = "app.example"
	auth.WebAuthnRPName = "TutorHub"
	auth.WebAuthnRPOrigins = []string{cfg.WebOrigin}
	auth.ProviderGoogleEnabled, auth.GoogleClientID, auth.GoogleClientSecret = true, "fixture-id", privacyProvider
	auth.ProviderMicrosoftEnabled, auth.MicrosoftClientID, auth.MicrosoftClientSecret = true, "fixture-id", privacyProvider
	auth.ProviderFacebookEnabled, auth.FacebookClientID, auth.FacebookClientSecret = true, "fixture-id", privacyProvider
	auth.SSODomainsEnabled = true
	return cfg
}

// Include credentials in an adapter diagnostic as well as the normal request. Sanitizing
// newlines alone cannot protect either a downstream error or a formatted request object.
func privacyDiagnostic(cause error) error {
	return fmt.Errorf("%w: %s", cause, strings.Join(privacyMarkers, " "))
}

func checkPrivacy(t *testing.T, opts Options, r *http.Request, want int) *httptest.ResponseRecorder {
	t.Helper()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			// Fractional seconds could coincidentally contain a six-digit OTP.
			if a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	}))
	r.Header.Set(requestIDHeader, "privacy-audit")
	opts.AuthRateLimiter = &nativeTestLimiter{}
	opts.Clock = func() time.Time { return fixedTime }
	w := httptest.NewRecorder()
	NewHandlerWithOptions(authPrivacyConfig(), logger, opts).ServeHTTP(w, r)
	if w.Code != want || !strings.Contains(logs.String(), `"msg":"request completed"`) {
		t.Fatalf("expected audited status %d, got %d", want, w.Code)
	}
	observed := logs.String()
	if w.Code >= 300 {
		observed += w.Body.String() + w.Header().Get("Location")
	}
	if w.Code >= 400 {
		var p Problem
		if json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Status != want || p.RequestID != "privacy-audit" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") {
			t.Fatal("missing correlated Problem Details")
		}
	}
	for i, marker := range privacyMarkers {
		if strings.Contains(observed, marker) {
			t.Fatalf("private fixture %d appeared in log or error response", i)
		}
	}
	return w
}

func TestAuthPrivacyNativeAndMFA(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		success          int
		reject           error
		rejected         int
	}{
		{"password", "password/login", `{"identifier":"` + privacyEmail + `","password":"` + privacyPassword + `"}`, 200, identity.ErrNativeInvalidCredentials, 401},
		{"email", "email-code/verify", accountCodeBody(), 200, identity.ErrNativeInvalidCode, 401},
		{"recovery", "password/recovery/complete", recoveryBody(), 200, identity.ErrNativeInvalidCode, 401},
		{"phone start", "phone/start", `{"phone_number":"` + privacyPhone + `"}`, 202, identity.ErrPhoneDeliveryUnavailable, 503},
		{"phone verify", "phone/verify", accountCodeBody(), 200, identity.ErrSSORequired, 403},
		{"passkey", "passkeys/assertion/verify", `{"assertion":{"id":"` + privacyPasskey + `"}}`, 200, identity.ErrSSORequired, 403},
		{"totp", "mfa/verify", `{"method":"totp","code":"123456"}`, 200, identity.ErrNativeInvalidCode, 401},
		{"recovery code", "mfa/verify", `{"method":"recovery_code","code":"` + privacyRecovery + `"}`, 200, identity.ErrNativeInvalidCode, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reject := range []bool{false, true} {
				var err error
				want := tc.success
				if reject {
					err = privacyDiagnostic(tc.reject)
					want = tc.rejected
				}
				native := fakeNativeAuth{err: err}
				opts := Options{NativeAuth: &native}
				switch tc.path {
				case "phone/start", "phone/verify":
					opts.NativeAuth = &phoneHTTPService{fakeSecurityAuth: fakeSecurityAuth{fakeNativeAuth: native}, known: true, destinationAllowed: true}
				case "passkeys/assertion/verify":
					opts.NativeAuth = &mfaFirstFactorService{fakeNativeAuth: native}
				case "mfa/verify":
					opts.NativeAuth = &mfaHTTPService{verifyErr: err}
				}
				w := checkPrivacy(t, opts, nativeRequest("/api/v1/auth/"+tc.path, tc.body, false), want)
				switch {
				case reject && errors.Is(err, identity.ErrSSORequired):
					var p Problem
					decodeJSON(t, w, &p)
					cookies := w.Result().Cookies()
					if p.Code != "auth_sso_required" || p.UnknownCredential || len(cookies) != 1 || cookies[0].Name != identity.AuthTransactionCookieName(false) || cookies[0].MaxAge >= 0 {
						t.Fatal("school policy must clear only the completed transaction")
					}
				case !reject && tc.path == "password/recovery/complete":
					if !strings.Contains(w.Body.String(), `"status":"password_reset"`) {
						t.Fatal("expected password reset")
					}
				case !reject && want == http.StatusOK:
					if !strings.Contains(w.Body.String(), `"status":"authenticated"`) || findCookie(t, w.Result().Cookies(), "tutorhub_session").Value != "private-session-token" {
						t.Fatal("expected successful sign-in")
					}
				}
			}
		})
	}
}

func TestAuthPrivacyFederatedCallbacks(t *testing.T) {
	for _, provider := range []string{"google", "facebook", "microsoft"} {
		t.Run(provider, func(t *testing.T) {
			result := identity.OAuthResult{LoginResult: identity.LoginResult{SessionToken: "private-session-token", CSRFToken: "private-csrf-token", ReturnTo: "/app/classes", ExpiresAt: fixedTime.Add(time.Hour)}}
			for _, err := range []error{nil, privacyDiagnostic(identity.ErrProviderExchange)} {
				s := &fakeFederationService{callbackResult: result, err: err}
				r := httptest.NewRequest("GET", "/api/v1/auth/oauth/"+provider+"/callback?state=fixture-state&code="+privacyOAuth, nil)
				r.AddCookie(&http.Cookie{Name: "tutorhub_oidc", Value: nativeTestBinding})
				w := checkPrivacy(t, Options{Identity: s}, r, 303)
				if s.calls != 1 || s.provider != provider || s.callbackInput.Code != privacyOAuth {
					t.Fatal("callback did not reach the selected provider")
				}
				want := "/sign-in?error=authentication_failed"
				if err == nil {
					want = "/app/classes"
					if findCookie(t, w.Result().Cookies(), "tutorhub_session").Value != result.SessionToken {
						t.Fatal("missing authenticated session")
					}
				}
				if w.Header().Get("Location") != authPrivacyConfig().WebOrigin+want {
					t.Fatal("unexpected sign-in destination")
				}
			}
		})
	}
}

type privacySchoolSSO struct {
	SchoolSSOAdmin
	err error
}

type privacySchoolFederation struct {
	fakeFederationService
	hints []identity.OAuthStartHints
}

func (s *privacySchoolFederation) BeginOAuth(ctx context.Context, provider, returnTo, intent string, principal identity.Principal, hints ...identity.OAuthStartHints) (identity.LoginStart, error) {
	s.hints = hints
	return s.fakeFederationService.BeginOAuth(ctx, provider, returnTo, intent, principal, hints...)
}

func TestAuthPrivacySchoolOAuth(t *testing.T) {
	service := &privacySchoolFederation{}
	opts := Options{Identity: service}
	r := httptest.NewRequest("GET", "/api/v1/auth/oauth/google/start?return_to=%2Fapp&domain=campus.test&login_hint=fixture%40campus.test", nil)
	w := checkPrivacy(t, opts, r, 303)
	if len(service.hints) != 1 || service.hints[0].LoginHint != privacyEmail || service.hints[0].Domain != "campus.test" {
		t.Fatal("school OAuth route lost its organization hints")
	}
	flow := findCookie(t, w.Result().Cookies(), "tutorhub_oidc")
	service.callbackResult.MFA = &identity.MFAChallenge{Cookie: &http.Cookie{
		Name: "tutorhub_auth_tx", Value: nativeTestBinding, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
	}}
	for _, policyDenied := range []bool{false, true} {
		want := "/sign-in?mfa=pending"
		if policyDenied {
			service.err = privacyDiagnostic(identity.ErrSSORequired)
			want = "/sign-in?error=auth_sso_required"
		}
		r := httptest.NewRequest("GET", "/api/v1/auth/oauth/google/callback?state=fixture-state&code="+privacyOAuth, nil)
		r.AddCookie(flow)
		w = checkPrivacy(t, opts, r, 303)
		if w.Header().Get("Location") != authPrivacyConfig().WebOrigin+want || service.callbackInput.BrowserBinding != flow.Value {
			t.Fatal("unexpected school callback")
		}
		for _, cookie := range w.Result().Cookies() {
			if cookie.Name != flow.Name && (policyDenied || cookie.Name != "tutorhub_auth_tx") {
				t.Fatal("session granted before school policy and MFA completion")
			}
		}
	}
}

func (s privacySchoolSSO) RotateDomain(context.Context, identity.Principal, uuid.UUID, uuid.UUID) (identity.SSOChallenge, error) {
	return identity.SSOChallenge{TXTValue: privacyDNS}, s.err
}

func TestAuthPrivacySchoolAndTOTPSecrets(t *testing.T) {
	school := &schoolDiscoveryStub{}
	for _, tc := range []struct {
		email string
		want  int
	}{{privacyEmail, 200}, {"fixture@unavailable.test", 503}} {
		w := checkPrivacy(t, Options{NativeAuth: school}, authDiscoveryRequest(`{"identifier":"`+tc.email+`"}`), tc.want)
		if tc.want == 200 && !strings.Contains(w.Body.String(), `"required":true`) {
			t.Fatal("expected enforced school route")
		}
	}
	tenant := nativeTestChallengeID
	admin := &fakeIdentityService{principal: identity.Principal{
		SessionID: tenant, User: identity.User{ID: tenant, Email: privacyEmail},
		ActiveTenant: &identity.Tenant{ID: tenant, Role: "org_admin"}, Permissions: []string{"tenant.manage"},
	}}
	for _, fail := range []bool{false, true} {
		var err error
		dnsStatus, totpStatus := 200, 200
		if fail {
			err = privacyDiagnostic(identity.ErrSSODNSUnavailable)
			dnsStatus, totpStatus = 503, 500
		}
		w := checkPrivacy(t, Options{Identity: admin, SchoolSSO: privacySchoolSSO{err: err}},
			securityRequest("POST", "/api/v1/tenants/"+tenant.String()+"/sso-domains/"+tenant.String()+"/rotate", "{}"), dnsStatus)
		if !fail && !strings.Contains(w.Body.String(), privacyDNS) {
			t.Fatal("expected one-time DNS challenge for authenticated admin")
		}
		w = checkPrivacy(t, Options{Identity: admin, NativeAuth: &mfaHTTPService{manageErr: err}},
			securityRequest("POST", "/api/v1/me/mfa/totp/enroll", "{}"), totpStatus)
		if !fail && !strings.Contains(w.Body.String(), "test-enrollment") {
			t.Fatal("expected one-time TOTP enrollment for authenticated user")
		}
	}
}

type privacyFailureService struct {
	fakeIdentityService
	panics bool
}

func (s privacyFailureService) CompleteLogin(context.Context, identity.CallbackInput) (identity.LoginResult, error) {
	err := privacyDiagnostic(identity.ErrProviderExchange)
	if s.panics {
		panic(err)
	}
	return identity.LoginResult{}, err
}

func TestAuthPrivacyOIDCFailureDiagnostics(t *testing.T) {
	for _, panics := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/api/v1/auth/callback?state=fixture-state&code="+privacyOAuth, nil)
		r.AddCookie(&http.Cookie{Name: "tutorhub_oidc", Value: nativeTestBinding})
		want := http.StatusBadGateway
		if panics {
			want = http.StatusInternalServerError
		}
		checkPrivacy(t, Options{Identity: &privacyFailureService{panics: panics}}, r, want)
	}
}

type privateMarshalError struct{}

func (privateMarshalError) MarshalJSON() ([]byte, error) {
	return nil, privacyDiagnostic(errors.New("fixture encoding failure"))
}

func TestAuthPrivacyResponseEncodingFailure(t *testing.T) {
	var logs bytes.Buffer
	w := httptest.NewRecorder()
	writeJSON(slog.New(slog.NewJSONHandler(&logs, nil)), w, 200, privateMarshalError{})
	var p Problem
	if w.Code != 500 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Status != 500 ||
		!strings.Contains(logs.String(), "marshal response") || strings.Contains(logs.String()+w.Body.String(), privacyVerifier) {
		t.Fatal("encoding failure must emit only safe diagnostics")
	}
}
