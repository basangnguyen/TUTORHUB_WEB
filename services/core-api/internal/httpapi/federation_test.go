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
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type fakeFederationService struct {
	fakeIdentityService
	err                       error
	calls                     int
	provider, intent, binding string
	federationPrincipal       identity.Principal
	callbackInput             identity.CallbackInput
	callbackResult            identity.OAuthResult
}

func (s *fakeFederationService) BeginOAuth(_ context.Context, provider, returnTo, intent string, p identity.Principal, _ ...identity.OAuthStartHints) (identity.LoginStart, error) {
	s.calls++
	s.provider = provider
	s.intent = intent
	s.returnTo = returnTo
	s.federationPrincipal = p
	if _, err := identity.NormalizeReturnTo(returnTo); err != nil {
		return identity.LoginStart{}, err
	}
	return identity.LoginStart{AuthorizationURL: "https://provider.example/authorize", BrowserBinding: "private-flow-binding", ExpiresAt: fixedTime.Add(10 * time.Minute)}, s.err
}
func (s *fakeFederationService) CompleteOAuth(_ context.Context, provider string, input identity.CallbackInput) (identity.OAuthResult, error) {
	s.calls++
	s.provider = provider
	s.callbackInput = input
	return s.callbackResult, s.err
}
func (s *fakeFederationService) BeginProviderIdentityLink(ctx context.Context, p identity.Principal, provider string) (identity.LoginStart, error) {
	return s.BeginOAuth(ctx, provider, "/app/settings", "identity_link", p)
}
func (s *fakeFederationService) GetLinkIntent(_ context.Context, binding string) (identity.LinkIntentView, error) {
	s.calls++
	s.binding = binding
	return identity.LinkIntentView{Provider: "google", MaskedEmail: "s***@e***.test", ExpiresInSeconds: 600}, s.err
}
func (s *fakeFederationService) ConfirmLinkIntent(_ context.Context, p identity.Principal, binding string) (identity.LinkIntentResult, error) {
	s.calls++
	s.binding = binding
	s.federationPrincipal = p
	return identity.LinkIntentResult{SessionToken: "private-new-session", CSRFToken: "private-new-csrf", CookieExpiresAt: fixedTime.Add(8 * time.Hour), ReturnTo: "/app/home"}, s.err
}
func (s *fakeFederationService) CancelLinkIntent(_ context.Context, binding string) error {
	s.calls++
	s.binding = binding
	return s.err
}

func federationTestHandler(s *fakeFederationService, enabled, secure bool, limiter InvitationRateLimiter, logger *slog.Logger) http.Handler {
	cfg := authFoundationConfig()
	cfg.Authentication.CookieSecure = secure
	cfg.Authentication.OIDCEnabled = true
	cfg.Authentication.ProviderGoogleEnabled = enabled
	cfg.Authentication.GoogleClientID = "fixture-id"
	cfg.Authentication.GoogleClientSecret = "fixture-secret"
	return NewHandlerWithOptions(cfg, logger, Options{Identity: s, AuthRateLimiter: limiter, Clock: func() time.Time { return fixedTime }})
}

func federationRequest(method, path string, session bool) *http.Request {
	var r *http.Request
	if method == http.MethodPost {
		r = authDiscoveryRequest(`{}`)
		r.URL.Path = path
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if session {
		r.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
		r.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
		r.Header.Set(csrfHeader, "csrf-token")
	}
	r.AddCookie(&http.Cookie{Name: "tutorhub_link_tx", Value: strings.Repeat("l", 43)})
	return r
}

func TestFederationStartProviderAndSessionBoundary(t *testing.T) {
	for _, tc := range []struct {
		provider, intent string
		enabled, session bool
		status           int
	}{
		{"google", "login", true, false, 303}, {"google", "reauth", true, false, 401}, {"google", "reauth", true, true, 303},
		{"google", "login", false, false, 404}, {"facebook", "login", true, false, 404}, {"unknown", "login", true, false, 404},
		{"oidc", "login", true, false, 303}, {"google", "admin", true, true, 400},
	} {
		t.Run(tc.provider+tc.intent+fmt.Sprint(tc.enabled, tc.session), func(t *testing.T) {
			s := &fakeFederationService{}
			w := httptest.NewRecorder()
			r := federationRequest("GET", "/api/v1/auth/oauth/"+tc.provider+"/start?return_to=%2Fapp%2Fhome&intent="+tc.intent, tc.session)
			federationTestHandler(s, tc.enabled, false, nil, nil).ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal("missing privacy headers")
			}
			if tc.status == 303 {
				if s.provider != tc.provider || s.intent != tc.intent || s.returnTo != "/app/home" {
					t.Fatal("provider/intent route changed")
				}
				if tc.intent == "reauth" && s.federationPrincipal.User.ID == uuid.Nil {
					t.Fatal("reauth has no authenticated principal")
				}
				if findCookie(t, w.Result().Cookies(), "tutorhub_oidc").Value != "private-flow-binding" {
					t.Fatal("missing flow binding")
				}
			} else if s.calls != 0 {
				t.Fatal("rejected start reached service")
			}
		})
	}
	for _, query := range []string{"?return_to=https%3A%2F%2Fevil.example", "?intent=login&intent=reauth", "?return_to=%2Fapp&return_to=%2Fevil"} {
		w := httptest.NewRecorder()
		federationTestHandler(&fakeFederationService{}, true, false, nil, nil).ServeHTTP(w, federationRequest("GET", "/api/v1/auth/oauth/google/start"+query, false))
		if w.Code != 400 {
			t.Fatal("unsafe/ambiguous start accepted")
		}
	}
}

func TestFederationCallbackRedirectsAndSeparateLinkCookie(t *testing.T) {
	for _, pending := range []bool{false, true} {
		s := &fakeFederationService{callbackResult: identity.OAuthResult{LoginResult: identity.LoginResult{SessionToken: "private-session", CSRFToken: "private-csrf", ExpiresAt: fixedTime.Add(time.Hour), ReturnTo: "/app/home"}, LinkPending: pending, LinkBinding: strings.Repeat("l", 43), LinkExpiresAt: fixedTime.Add(15 * time.Minute)}}
		r := httptest.NewRequest("GET", "/api/v1/auth/oauth/google/callback?state=private-state&code=private-code&iss=https%3A%2F%2Fprovider.example", nil)
		r.AddCookie(&http.Cookie{Name: "__Host-tutorhub_oidc", Value: "private-flow-binding"})
		r.AddCookie(&http.Cookie{Name: "__Host-tutorhub_auth_tx", Value: "other-native-transaction"})
		w := httptest.NewRecorder()
		federationTestHandler(s, true, true, nil, nil).ServeHTTP(w, r)
		if w.Code != 303 || s.provider != "google" || s.callbackInput.Issuer != "https://provider.example" || s.callbackInput.Code != "private-code" {
			t.Fatal("callback input/redirect mismatch")
		}
		if strings.Contains(w.Body.String(), "private-") {
			t.Fatal("callback exposed secret")
		}
		if pending {
			if w.Header().Get("Location") != "https://app.example/sign-in?link=pending&provider=google" {
				t.Fatal("wrong pending redirect")
			}
			cookie := findCookie(t, w.Result().Cookies(), "__Host-tutorhub_link_tx")
			if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 900 || cookie.Path != "/" {
				t.Fatal("unsafe link cookie")
			}
			for _, c := range w.Result().Cookies() {
				if c.Name == "__Host-tutorhub_session" || c.Name == "__Host-tutorhub_csrf" || c.Name == "__Host-tutorhub_auth_tx" {
					t.Fatal("link intent created session or overwrote native transaction")
				}
			}
		} else if w.Header().Get("Location") != "https://app.example/app/home" || findCookie(t, w.Result().Cookies(), "__Host-tutorhub_session").Value != "private-session" {
			t.Fatal("successful callback did not set session")
		}
	}
}

func TestPendingSocialCallbackSetsOnlyBrowserRegistrationBinding(t *testing.T) {
	for _, provider := range []string{"facebook", "microsoft"} {
		t.Run(provider, func(t *testing.T) {
			cfg := authFoundationConfig()
			cfg.Authentication.ProviderFacebookEnabled = true
			cfg.Authentication.FacebookClientID = "fixture-id"
			cfg.Authentication.FacebookClientSecret = "fixture-secret"
			cfg.Authentication.ProviderMicrosoftEnabled = true
			cfg.Authentication.MicrosoftClientID = "fixture-id"
			cfg.Authentication.MicrosoftClientSecret = "fixture-secret"
			cfg.Authentication.CookieSecure = true
			binding := strings.Repeat("p", 43)
			service := &fakeFederationService{callbackResult: identity.OAuthResult{
				RegistrationPending: true, RegistrationBinding: binding, RegistrationExpiresAt: fixedTime.Add(15 * time.Minute),
			}}
			handler := NewHandlerWithOptions(cfg, nil, Options{Identity: service, Clock: func() time.Time { return fixedTime }})
			r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oauth/"+provider+"/callback?state=test&code=code", nil)
			r.AddCookie(&http.Cookie{Name: "__Host-tutorhub_oidc", Value: "flow-cookie"})
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "https://app.example/sign-in?registration=pending&provider="+provider ||
				service.provider != provider {
				t.Fatalf("unexpected pending callback status %d", w.Code)
			}
			for _, cookie := range w.Result().Cookies() {
				if cookie.Name != "__Host-tutorhub_oidc" && cookie.Name != "__Host-tutorhub_registration_tx" {
					t.Fatalf("unexpected cookie %q", cookie.Name)
				}
			}
			c := findCookie(t, w.Result().Cookies(), "__Host-tutorhub_registration_tx")
			if c.Value != binding || !c.HttpOnly || !c.Secure || c.Path != "/" || c.SameSite != http.SameSiteStrictMode || c.MaxAge != 900 {
				t.Fatal("invalid registration binding")
			}
		})
	}
}

func TestMicrosoftSettingsLinkRequiresConfiguredProviderAndAuthenticatedPrincipal(t *testing.T) {
	cfg := nativeTestConfig()
	cfg.Authentication.ProviderMicrosoftEnabled = true
	cfg.Authentication.MicrosoftClientID = "fixture-id"
	const path = "/api/v1/me/identities/link"
	const body = `{"provider":"microsoft"}`
	service := &fakeFederationService{}
	for _, configured := range []bool{false, true} {
		if configured {
			cfg.Authentication.MicrosoftClientSecret = "fixture-secret"
		}
		handler := NewHandlerWithOptions(cfg, nil, Options{Identity: service, Clock: func() time.Time { return fixedTime }})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, authenticatedRequest(http.MethodPost, path, body))
		if !configured {
			if w.Code != http.StatusNotFound || service.calls != 0 {
				t.Fatal("incomplete settings provider dispatched")
			}
			continue
		}
		if w.Code != http.StatusOK || service.provider != "microsoft" || service.federationPrincipal.User.ID == uuid.Nil {
			t.Fatalf("settings link not bound to principal: status %d", w.Code)
		}
		if findCookie(t, w.Result().Cookies(), "tutorhub_oidc").Value != "private-flow-binding" {
			t.Fatal("missing settings flow binding")
		}
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, federationRequest(http.MethodGet, "/api/v1/auth/oauth/microsoft/start?intent=reauth", true))
		if w.Code != http.StatusBadRequest || service.calls != 1 {
			t.Fatal("Microsoft cannot start federated reauthentication")
		}
	}
}

func TestFederationCallbackFailuresAreFixedAndRedacted(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{identity.ErrInvalidAuthFlow, "authentication_failed"}, {identity.ErrProviderExchange, "authentication_failed"},
		{identity.ErrFederatedIdentityMismatch, "authentication_failed"}, {identity.ErrFederationReauthRequired, "authentication_failed"},
		{identity.ErrVerifiedEmailRequired, "email_unverified"}, {identity.ErrFederationSignupDisabled, "signup_disabled"},
		{identity.ErrFederationProviderUnavailable, "provider_unavailable"}, {identity.ErrFederationAALRequired, "assurance_required"},
		{errors.New("storage"), "authentication_failed"},
	} {
		s := &fakeFederationService{err: fmt.Errorf("%w: private-code private-token private-claims", tc.err)}
		var logs bytes.Buffer
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/auth/oauth/google/callback?state=private-state&code=private-code", nil)
		r.AddCookie(&http.Cookie{Name: "tutorhub_oidc", Value: "binding"})
		federationTestHandler(s, true, false, nil, slog.New(slog.NewJSONHandler(&logs, nil))).ServeHTTP(w, r)
		if w.Code != 303 || w.Header().Get("Location") != "https://app.example/sign-in?error="+tc.code || strings.Contains(logs.String()+w.Body.String(), "private-") {
			t.Fatal("callback error leaked details or changed code")
		}
		for _, c := range w.Result().Cookies() {
			if c.Name == "tutorhub_session" || c.Name == "tutorhub_csrf" {
				t.Fatal("failed callback set session")
			}
		}
	}
	for _, query := range []string{"?error=private-detail", "?code=a&code=b", "?iss=", "?iss=a&iss=b", "?state=a&state=b"} {
		s := &fakeFederationService{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/v1/auth/oauth/google/callback"+query, nil)
		r.AddCookie(&http.Cookie{Name: "tutorhub_oidc", Value: "binding"})
		federationTestHandler(s, true, false, nil, nil).ServeHTTP(w, r)
		if w.Code != 303 || s.calls != 0 {
			t.Fatal("ambiguous callback reached exchange")
		}
	}
	for _, provider := range []string{"google", "unknown"} {
		w := httptest.NewRecorder()
		federationTestHandler(&fakeFederationService{}, false, false, nil, nil).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/auth/oauth/"+provider+"/callback", nil))
		if w.Code != 303 || !strings.HasSuffix(w.Header().Get("Location"), "error=provider_unavailable") {
			t.Fatal("disabled callback returned nonredirect")
		}
	}
}

func TestLinkIntentConfirmationAndCancellationBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		mutate     func(*http.Request)
		status     int
	}{
		{"confirm", "/confirm", nil, 200}, {"cancel", "/cancel", nil, 204},
		{"no session", "/confirm", func(r *http.Request) {
			r.Header.Del("Cookie")
			r.AddCookie(&http.Cookie{Name: "tutorhub_link_tx", Value: strings.Repeat("l", 43)})
		}, 401},
		{"no csrf", "/confirm", func(r *http.Request) { r.Header.Del(csrfHeader) }, 403},
		{"wrong csrf", "/confirm", func(r *http.Request) { r.Header.Set(csrfHeader, "wrong") }, 403},
		{"wrong origin", "/confirm", func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"cancel origin", "/cancel", func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"missing link", "/confirm", func(r *http.Request) {
			r.Header.Del("Cookie")
			r.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
			r.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
		}, 400},
		{"ambiguous link", "/confirm", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "tutorhub_link_tx", Value: strings.Repeat("x", 43)})
		}, 400},
		{"cross site", "/confirm", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeFederationService{}
			r := federationRequest("POST", "/api/v1/auth/link-intent"+tc.path, tc.path == "/confirm")
			r.AddCookie(&http.Cookie{Name: "tutorhub_auth_tx", Value: "native-flow"})
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			federationTestHandler(s, true, false, nil, nil).ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if tc.status >= 400 && s.calls != 0 {
				t.Fatal("rejected request reached link mutation")
			}
			if tc.status < 300 {
				if s.binding != strings.Repeat("l", 43) || findCookie(t, w.Result().Cookies(), "tutorhub_link_tx").MaxAge != -1 {
					t.Fatal("wrong link transaction binding")
				}
				for _, c := range w.Result().Cookies() {
					if c.Name == "tutorhub_auth_tx" {
						t.Fatal("native transaction cookie overwritten")
					}
				}
			}
			if tc.status == 200 {
				if s.federationPrincipal.User.ID == uuid.Nil || findCookie(t, w.Result().Cookies(), "tutorhub_csrf").Value != "private-new-csrf" || strings.Contains(w.Body.String(), "private-") {
					t.Fatal("confirmation rotation failed or leaked")
				}
			}
		})
	}
}

func TestLinkIntentPreviewAndDomainErrors(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		err          error
		status       int
		code         string
	}{
		{"GET", "", nil, 200, ""}, {"GET", "", identity.ErrLinkIntentUnavailable, 404, "auth_link_intent_unavailable"},
		{"POST", "/confirm", identity.ErrLinkIntentMismatch, 403, "auth_link_intent_mismatch"},
		{"POST", "/confirm", identity.ErrFederationAALRequired, 403, "auth_assurance_required"},
		{"POST", "/confirm", identity.ErrLinkIntentUnavailable, 400, "auth_link_intent_unavailable"},
		{"POST", "/confirm", identity.ErrSessionNotFound, 401, ""},
		{"POST", "/confirm", identity.ErrSessionContextConflict, 409, ""},
		{"POST", "/confirm", errors.New("storage"), 503, ""},
		{"POST", "/cancel", identity.ErrLinkIntentUnavailable, 400, "auth_link_intent_unavailable"},
	} {
		s := &fakeFederationService{err: tc.err}
		if tc.err != nil {
			s.err = fmt.Errorf("%w: private@example.test private-secret", tc.err)
		}
		var logs bytes.Buffer
		w := httptest.NewRecorder()
		r := federationRequest(tc.method, "/api/v1/auth/link-intent"+tc.path, true)
		federationTestHandler(s, true, false, nil, slog.New(slog.NewJSONHandler(&logs, nil))).ServeHTTP(w, r)
		if w.Code != tc.status || strings.Contains(w.Body.String()+logs.String(), "private") {
			t.Fatalf("domain error status=%d body=%s logs=%s", w.Code, w.Body.String(), logs.String())
		}
		if tc.status == 200 {
			if !strings.Contains(w.Body.String(), "s***@e***.test") {
				t.Fatal("preview missing masked email")
			}
		} else {
			var p Problem
			decodeJSON(t, w, &p)
			if p.Code != tc.code {
				t.Fatal("wrong problem code")
			}
		}
	}
}

type federationTestLimiter struct {
	denied  InvitationRateLimitAction
	failure bool
	buckets map[InvitationRateLimitAction]string
}

func (l *federationTestLimiter) Allow(_ context.Context, action InvitationRateLimitAction, bucket string, _ time.Time) InvitationRateLimitDecision {
	if l.buckets == nil {
		l.buckets = map[InvitationRateLimitAction]string{}
	}
	l.buckets[action] = bucket
	if action == l.denied {
		if l.failure {
			return InvitationRateLimitDecision{Err: errors.New("private-storage")}
		}
		return InvitationRateLimitDecision{RetryAfter: time.Minute}
	}
	return InvitationRateLimitDecision{Allowed: true}
}
func TestFederationRateLimitsBeforeService(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		action       InvitationRateLimitAction
		status       int
	}{
		{"GET", "/api/v1/auth/oauth/google/start", AuthRateLimitOAuthStartIP, 429},
		{"GET", "/api/v1/auth/oauth/google/callback", AuthRateLimitOAuthCallbackIP, 303},
		{"POST", "/api/v1/auth/link-intent/confirm", AuthRateLimitLinkIntentIP, 429},
		{"POST", "/api/v1/auth/link-intent/confirm", AuthRateLimitLinkIntentUser, 429},
	} {
		for _, failure := range []bool{false, true} {
			s := &fakeFederationService{}
			l := &federationTestLimiter{denied: tc.action, failure: failure}
			w := httptest.NewRecorder()
			r := federationRequest(tc.method, tc.path, true)
			federationTestHandler(s, true, false, l, nil).ServeHTTP(w, r)
			want := tc.status
			if failure && want != 303 {
				want = 503
			}
			if w.Code != want || s.calls != 0 {
				t.Fatal("rate limit did not prevent service")
			}
			if tc.action == AuthRateLimitLinkIntentUser && l.buckets[tc.action] != s.principal.User.ID.String() {
				t.Fatal("confirmation user bucket is not principal scoped")
			}
		}
	}
}

func TestSettingsIdentityLinkOptionalProviderPreservesLegacy(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		google bool
	}{
		{"", 200, false}, {`{}`, 200, false}, {`{"provider":"oidc"}`, 200, false}, {`{"provider":"google"}`, 200, true},
		{`{"provider":"facebook"}`, 404, false}, {`{"provider":"google","provider":"oidc"}`, 400, false}, {`{"provider":"google","email":"private@example.test"}`, 400, false}, {`null`, 400, false},
	} {
		s := &fakeFederationService{}
		w := httptest.NewRecorder()
		r := authenticatedRequest("POST", "/api/v1/me/identities/link", tc.body)
		federationTestHandler(s, true, false, nil, nil).ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("body=%s status=%d output=%s", tc.body, w.Code, w.Body.String())
		}
		if tc.status == 200 && (tc.google && s.provider != "google" || !tc.google && !s.beginLinkCalled) {
			t.Fatal("wrong settings provider")
		}
	}
	s := &fakeFederationService{err: identity.ErrRecentAuthenticationRequired}
	w := httptest.NewRecorder()
	federationTestHandler(s, true, false, nil, nil).ServeHTTP(w, authenticatedRequest("POST", "/api/v1/me/identities/link", `{"provider":"google"}`))
	var p Problem
	decodeJSON(t, w, &p)
	if w.Code != 403 || p.Code != "auth_recent_authentication_required" {
		t.Fatal("settings recent-auth contract lost")
	}
}

func TestGoogleCapabilityRequiresFlagAndCredentials(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, configured := range []bool{false, true} {
			cfg := authFoundationConfig()
			cfg.Authentication.ProviderGoogleEnabled = enabled
			if configured {
				cfg.Authentication.GoogleClientID = "fixture-id"
				cfg.Authentication.GoogleClientSecret = "fixture-secret"
			}
			w := performRequest(NewHandlerWithOptions(cfg, nil, Options{}), "GET", "/api/v1/auth/providers", "")
			var body authProvidersResponse
			decodeJSON(t, w, &body)
			if (len(body.OAuthProviders) == 1) != (enabled && configured) {
				t.Fatal("Google capability exposed without flag and configuration")
			}
		}
	}
}

func TestLegacyCallbackRejectsAmbiguousIssuerWithoutChangingOldFlow(t *testing.T) {
	for _, suffix := range []string{"&iss=", "&iss=a&iss=b"} {
		service := &fakeIdentityService{}
		r := httptest.NewRequest("GET", "/api/v1/auth/callback?state=state&code=code"+suffix, nil)
		r.AddCookie(&http.Cookie{Name: "tutorhub_oidc", Value: "binding"})
		w := httptest.NewRecorder()
		newAuthTestHandler(service, false).ServeHTTP(w, r)
		if w.Code != 400 || service.callback.Code != "" {
			t.Fatal("ambiguous issuer reached legacy callback exchange")
		}
	}
}
