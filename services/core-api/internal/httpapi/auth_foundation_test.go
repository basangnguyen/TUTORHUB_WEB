package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

func authFoundationConfig() config.Config {
	return config.Config{Environment: "test", WebOrigin: "https://app.example", Authentication: config.AuthenticationConfig{
		ProviderOIDCDefaultEnabled: true, PasswordMinLength: 8, PhoneAllowedCountries: []string{"VN"}, EmailDailyQuota: 200, PhoneOTPDailyQuota: 50,
		LegalTermsVersion: "2026-11-16-v2.0",
	}}
}

func authDiscoveryRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/identifier/discover", strings.NewReader(body))
	r.Header.Set("Origin", "https://app.example")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return r
}

func TestAuthProvidersExposeOnlyPublicDeploymentCapabilities(t *testing.T) {
	cfg := authFoundationConfig()
	cfg.Authentication.Enabled = true
	cfg.Authentication.OIDCEnabled = true
	cfg.Authentication.ProviderGoogleEnabled = true
	cfg.Authentication.PasswordEnabled = true
	response := performRequest(NewHandlerWithOptions(cfg, nil, Options{}), http.MethodGet, "/api/v1/auth/providers", "")
	payload := append([]byte(nil), response.Body.Bytes()...)
	var got authProvidersResponse
	decodeJSON(t, response, &got)
	if response.Code != http.StatusOK || !got.OIDCDefaultEnabled || !got.PasswordEnabled || got.SignupEnabled || len(got.OAuthProviders) != 1 || got.OAuthProviders[0] != "google" || got.PasswordMinLength != 8 {
		t.Fatalf("unexpected public capabilities: %s", response.Body.String())
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 13 || got.PasswordRecoveryEnabled || got.LegalTermsVersion != "2026-11-16-v2.0" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("public capabilities changed shape or privacy headers")
	}
}

func TestAuthDiscoveryGuardAndStrictJSON(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		mutate func(*http.Request)
		status int
	}{
		{"missing origin", `{"identifier":"a@example.test"}`, func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"wrong origin", `{"identifier":"a@example.test"}`, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, 403},
		{"duplicate origin", `{"identifier":"a@example.test"}`, func(r *http.Request) { r.Header.Add("Origin", "https://app.example") }, 403},
		{"cross-site", `{"identifier":"a@example.test"}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"same-site", `{"identifier":"a@example.test"}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") }, 403},
		{"empty fetch site", `{"identifier":"a@example.test"}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "") }, 403},
		{"wrong content type", `{"identifier":"a@example.test"}`, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"unknown field", `{"identifier":"a@example.test","exists":true}`, nil, 400},
		{"duplicate field", `{"identifier":"a@example.test","IDENTIFIER":"b@example.test"}`, nil, 400},
		{"trailing object", `{"identifier":"a@example.test"}{}`, nil, 400},
		{"null", `null`, nil, 400},
		{"missing identifier", `{}`, nil, 400},
		{"oversized", `{"identifier":"` + strings.Repeat("a", 2050) + `"}`, nil, 400},
		{"absent fetch site", `{"identifier":"a@example.test"}`, func(r *http.Request) { r.Header.Del("Sec-Fetch-Site") }, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := authDiscoveryRequest(tc.body)
			if tc.mutate != nil {
				tc.mutate(r)
			}
			w := httptest.NewRecorder()
			NewHandlerWithOptions(authFoundationConfig(), nil, Options{}).ServeHTTP(w, r)
			if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatalf("unexpected guard result: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAuthDiscoveryClassifiesWithoutAccountLookupOrPII(t *testing.T) {
	var logs bytes.Buffer
	handler := NewHandlerWithOptions(authFoundationConfig(), slog.New(slog.NewJSONHandler(&logs, nil)), Options{})
	for _, tc := range []struct{ identifier, kind string }{
		{"existing@example.test", "email"}, {"nonexistent@example.test", "email"},
		{"  STUDENT@example.test  ", "email"}, {"Student <student@example.test>", "invalid"},
		{"+84 912 345 678", "phone"}, {"0912345678", "phone"}, {"invalid", "invalid"}, {"123", "invalid"},
	} {
		body, _ := json.Marshal(authIdentifierRequest{Identifier: tc.identifier})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, authDiscoveryRequest(string(body)))
		want := `{"kind":"` + tc.kind + `","sso":{"required":false}}` + "\n"
		if w.Code != http.StatusOK || w.Body.String() != want {
			t.Fatalf("classification mismatch: %d %s", w.Code, w.Body.String())
		}
		if strings.Contains(logs.String(), tc.identifier) {
			t.Fatal("identifier leaked into structured log")
		}
	}
}

func TestAuthDiscoveryRateLimitAndDatabaseFailure(t *testing.T) {
	handler := NewHandlerWithOptions(authFoundationConfig(), nil, Options{Clock: func() time.Time { return fixedTime }})
	for i := 0; i < 31; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, authDiscoveryRequest(`{"identifier":"a@example.test"}`))
		if i < 30 && w.Code != 200 {
			t.Fatalf("request %d rejected", i)
		}
		if i == 30 && (w.Code != 429 || w.Header().Get("Retry-After") != "60") {
			t.Fatalf("missing rate limit: %d", w.Code)
		}
	}
	db := &recordingInvitationRateLimitDatabase{rows: []pgx.Row{invitationRateLimitRow{err: errors.New("private database error")}}}
	limiter, err := NewPostgresAuthRateLimiter(db, time.Second, 200, 50)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	NewHandlerWithOptions(authFoundationConfig(), nil, Options{AuthRateLimiter: limiter}).ServeHTTP(w, authDiscoveryRequest(`{"identifier":"a@example.test"}`))
	if w.Code != 503 || strings.Contains(w.Body.String(), "private database error") {
		t.Fatal("database failure must fail closed and redact details")
	}
}

type loopbackAuthResolver struct{}

func (loopbackAuthResolver) ResolveRemoteAddress(*http.Request) string { return "127.0.0.1:1234" }

func TestDevAuthInboxRequiresEnvironmentSenderAndDirectLoopback(t *testing.T) {
	inbox, err := devinbox.New("test", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	for _, tc := range []struct {
		environment, sender, remote string
		enabled                     bool
		want                        int
	}{
		{"test", "devinbox", "127.0.0.1:1234", true, 200}, {"development", "devinbox", "[::1]:1234", true, 200},
		{"staging", "devinbox", "127.0.0.1:1234", true, 404}, {"production", "devinbox", "127.0.0.1:1234", true, 404},
		{"test", "disabled", "127.0.0.1:1234", true, 404}, {"test", "devinbox", "127.0.0.1:1234", false, 404},
		{"test", "devinbox", "203.0.113.20:1234", true, 404}, {"test", "devinbox", "127.0.0.2:1234", true, 404},
	} {
		t.Run(tc.environment+tc.sender+tc.remote, func(t *testing.T) {
			cfg := authFoundationConfig()
			cfg.Environment = tc.environment
			cfg.Authentication.EmailSender = tc.sender
			options := Options{RemoteAddressResolver: loopbackAuthResolver{}}
			if tc.enabled {
				options.AuthInbox = inbox
			}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/dev/auth-inbox", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("X-Real-IP", "127.0.0.1")
			w := httptest.NewRecorder()
			NewHandlerWithOptions(cfg, nil, options).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("expected %d got %d", tc.want, w.Code)
			}
			if tc.want == 404 && !strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatal("concealment must use Problem Details")
			}
		})
	}
}

func TestAuthRateLimitPurposesQuotasAndPrivacy(t *testing.T) {
	db := &recordingInvitationRateLimitDatabase{}
	limiter, err := NewPostgresAuthRateLimiter(db, time.Second, 7, 3)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for action, policy := range authRateLimitPolicies(7, 3) {
		db.rows = append(db.rows, invitationRateLimitRow{value: 1})
		decision := limiter.Allow(context.Background(), action, "private@example.test", fixedTime)
		if !decision.Allowed {
			t.Fatal("expected configured auth purpose to be allowed")
		}
		call := db.calls[len(db.calls)-1]
		if call.args[0] != string(action) || call.args[5] != policy.Limit {
			t.Fatal("incorrect purpose or quota")
		}
		hash := string(call.args[1].([]byte))
		if len(hash) != 32 || seen[hash] || strings.Contains(hash, "private@example.test") {
			t.Fatal("auth hashes must be private and purpose-separated")
		}
		seen[hash] = true
	}
}

func TestAuthDailyZeroQuotaDisablesDeliveryWithoutDatabaseAccess(t *testing.T) {
	db := &recordingInvitationRateLimitDatabase{}
	limiter, err := NewPostgresAuthRateLimiter(db, time.Second, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []InvitationRateLimitAction{AuthRateLimitDeliveryDailyEmail, AuthRateLimitDeliveryDailyPhone} {
		decision := limiter.Allow(context.Background(), action, "global", fixedTime)
		if decision.Allowed || decision.Err != nil || decision.RetryAfter <= 0 || len(db.calls) != 0 {
			t.Fatal("zero quota must disable delivery without touching the database")
		}
	}
}

func TestDevAuthInboxDoesNotLogOTPOrDestination(t *testing.T) {
	inbox, err := devinbox.New("test", 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	message := devinbox.Message{ChallengeID: "private-challenge", Purpose: "login_code", Channel: "email", Destination: "private-inbox@example.test", Code: "861027", ExpiresAt: time.Now().Add(time.Minute)}
	if err := inbox.Add(message); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	cfg := authFoundationConfig()
	cfg.Authentication.EmailSender = "devinbox"
	handler := NewHandlerWithOptions(cfg, slog.New(slog.NewJSONHandler(&logs, nil)), Options{AuthInbox: inbox})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/dev/auth-inbox", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), message.Code) {
		t.Fatal("local inbox must expose the test message")
	}
	if strings.Contains(logs.String(), message.Code) || strings.Contains(logs.String(), message.Destination) {
		t.Fatal("OTP or destination leaked into structured logs")
	}
}

func TestOIDCDefaultKillSwitchPreservesCurrentUser(t *testing.T) {
	cfg := authFoundationConfig()
	cfg.Authentication.ProviderOIDCDefaultEnabled = false
	service := &fakeIdentityService{}
	handler := NewHandlerWithOptions(cfg, nil, Options{Identity: service, Clock: func() time.Time { return fixedTime }})
	if response := performRequest(handler, http.MethodGet, "/api/v1/auth/login", ""); response.Code != 503 {
		t.Fatal("OIDC start must respect flag")
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	r.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("existing session stopped working: %d", w.Code)
	}
	for _, internal := range []string{"assurance_level", "auth_methods", "authenticated_at"} {
		if strings.Contains(w.Body.String(), internal) {
			t.Fatalf("/me leaked %s", internal)
		}
	}
}
