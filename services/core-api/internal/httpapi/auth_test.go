package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestAuthenticationRoutesAreUnavailableWithoutIdentityService(t *testing.T) {
	t.Parallel()

	response := performRequest(
		newTestHandler(Options{}),
		http.MethodGet,
		"/api/v1/auth/login",
		"",
	)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, response.Code)
	}
}

func TestAuthenticationHTTPFlowSetsAndClearsHardenedCookies(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{}
	handler := newAuthTestHandler(service, false)

	login := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/auth/login?return_to=%2Fapp%2Fclasses",
		nil,
	)
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusSeeOther ||
		loginResponse.Header().Get("Location") != "https://identity.example/authorize" {
		t.Fatalf("unexpected login response: %d %s", loginResponse.Code, loginResponse.Header().Get("Location"))
	}
	flowCookie := findCookie(t, loginResponse.Result().Cookies(), "tutorhub_oidc")
	if flowCookie.Value != "browser-binding" || !flowCookie.HttpOnly ||
		flowCookie.SameSite != http.SameSiteLaxMode || flowCookie.Secure {
		t.Fatalf("unexpected flow cookie: %+v", flowCookie)
	}
	if service.returnTo != "/app/classes" {
		t.Fatalf("unexpected return_to: %q", service.returnTo)
	}

	callback := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/auth/callback?state=state-123&code=code-123",
		nil,
	)
	callback.AddCookie(flowCookie)
	callback.Header.Set("User-Agent", "TutorHub browser")
	callback.RemoteAddr = "203.0.113.5:44000"
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callback)
	if callbackResponse.Code != http.StatusSeeOther ||
		callbackResponse.Header().Get("Location") != "http://localhost:5173/app/classes" {
		t.Fatalf("unexpected callback response: %d %s", callbackResponse.Code, callbackResponse.Header().Get("Location"))
	}
	if service.callback.State != "state-123" ||
		service.callback.Code != "code-123" ||
		service.callback.BrowserBinding != "browser-binding" ||
		service.callback.UserAgent != "TutorHub browser" {
		t.Fatalf("unexpected callback input: %+v", service.callback)
	}
	sessionCookie := findCookie(t, callbackResponse.Result().Cookies(), "tutorhub_session")
	csrfCookie := findCookie(t, callbackResponse.Result().Cookies(), "tutorhub_csrf")
	if sessionCookie.Value != "session-token" || !sessionCookie.HttpOnly {
		t.Fatalf("unexpected session cookie: %+v", sessionCookie)
	}
	if csrfCookie.Value != "csrf-token" || csrfCookie.HttpOnly {
		t.Fatalf("unexpected CSRF cookie: %+v", csrfCookie)
	}

	me := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	me.AddCookie(sessionCookie)
	meResponseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(meResponseRecorder, me)
	if meResponseRecorder.Code != http.StatusOK {
		t.Fatalf("expected /me status 200, got %d", meResponseRecorder.Code)
	}
	if strings.Contains(meResponseRecorder.Body.String(), "SessionID") ||
		strings.Contains(meResponseRecorder.Body.String(), service.principal.SessionID.String()) {
		t.Fatalf("/me leaked internal session ID: %s", meResponseRecorder.Body.String())
	}
	var currentUser meResponse
	if err := json.NewDecoder(meResponseRecorder.Body).Decode(&currentUser); err != nil {
		t.Fatalf("decode /me: %v", err)
	}
	if currentUser.User.Email != "student@example.com" || len(currentUser.Permissions) != 1 {
		t.Fatalf("unexpected /me response: %+v", currentUser)
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logout.AddCookie(sessionCookie)
	logout.AddCookie(csrfCookie)
	logout.Header.Set(csrfHeader, csrfCookie.Value)
	logoutResponseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponseRecorder, logout)
	if logoutResponseRecorder.Code != http.StatusOK || !service.logoutCalled {
		t.Fatalf("unexpected logout result: status=%d called=%t", logoutResponseRecorder.Code, service.logoutCalled)
	}
	for _, name := range []string{"tutorhub_session", "tutorhub_csrf", "tutorhub_oidc"} {
		cookie := findCookie(t, logoutResponseRecorder.Result().Cookies(), name)
		if cookie.MaxAge != -1 {
			t.Fatalf("expected %s to be cleared, got %+v", name, cookie)
		}
	}
}

func TestLogoutRejectsMismatchedCSRFWithoutRevokingSession(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{}
	handler := newAuthTestHandler(service, false)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	request.Header.Set(csrfHeader, "attacker-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden || service.logoutCalled {
		t.Fatalf("expected CSRF rejection without logout, status=%d called=%t", response.Code, service.logoutCalled)
	}
}

func TestSecureAuthenticationCookiesUseHostPrefix(t *testing.T) {
	t.Parallel()

	handler := newAuthTestHandler(&fakeIdentityService{}, true)
	response := httptest.NewRecorder()
	handler.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil),
	)
	cookie := findCookie(t, response.Result().Cookies(), "__Host-tutorhub_oidc")
	if !cookie.Secure || cookie.Path != "/" || !cookie.HttpOnly {
		t.Fatalf("unexpected secure flow cookie: %+v", cookie)
	}
}

func newAuthTestHandler(service identity.ServiceAPI, secure bool) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test",
			Port:        "8080",
			WebOrigin:   "http://localhost:5173",
			Authentication: config.AuthenticationConfig{
				CookieSecure: secure,
				SessionTTL:   8 * time.Hour,
			},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{
			Clock:    func() time.Time { return fixedTime },
			Identity: service,
		},
	)
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %q not found in %+v", name, cookies)
	return nil
}

type fakeIdentityService struct {
	returnTo     string
	callback     identity.CallbackInput
	logoutCalled bool
	principal    identity.Principal
}

func (service *fakeIdentityService) BeginLogin(
	_ context.Context,
	returnTo string,
) (identity.LoginStart, error) {
	service.returnTo = returnTo
	return identity.LoginStart{
		AuthorizationURL: "https://identity.example/authorize",
		BrowserBinding:   "browser-binding",
		ExpiresAt:        fixedTime.Add(10 * time.Minute),
	}, nil
}

func (service *fakeIdentityService) CompleteLogin(
	_ context.Context,
	input identity.CallbackInput,
) (identity.LoginResult, error) {
	service.callback = input
	return identity.LoginResult{
		SessionToken: "session-token",
		CSRFToken:    "csrf-token",
		ExpiresAt:    fixedTime.Add(8 * time.Hour),
		ReturnTo:     "/app/classes",
	}, nil
}

func (service *fakeIdentityService) Authenticate(
	_ context.Context,
	token string,
) (identity.Principal, error) {
	if token != "session-token" {
		return identity.Principal{}, identity.ErrSessionNotFound
	}
	if service.principal.SessionID == uuid.Nil {
		service.principal = identity.Principal{
			SessionID: uuid.MustParse("e27001f3-76db-4169-9dc0-bf451060ddf0"),
			User: identity.User{
				ID:          uuid.MustParse("be85eb92-0f18-4163-85ba-50e4d343d632"),
				Email:       "student@example.com",
				DisplayName: "Student",
				Locale:      "vi",
				Timezone:    "Asia/Ho_Chi_Minh",
			},
			Memberships: []identity.Tenant{},
			Permissions: []string{"class.view"},
		}
	}
	return service.principal, nil
}

func (service *fakeIdentityService) RotateCSRF(
	ctx context.Context,
	token string,
) (identity.CSRFResult, error) {
	principal, err := service.Authenticate(ctx, token)
	return identity.CSRFResult{
		Token:     "rotated-csrf",
		Principal: principal,
		ExpiresAt: fixedTime.Add(8 * time.Hour),
	}, err
}

func (service *fakeIdentityService) ValidateCSRF(
	ctx context.Context,
	sessionToken string,
	csrfToken string,
) (identity.Principal, error) {
	if csrfToken != "csrf-token" {
		return identity.Principal{}, identity.ErrInvalidCSRFToken
	}
	return service.Authenticate(ctx, sessionToken)
}

func (service *fakeIdentityService) Logout(
	_ context.Context,
	token string,
) (string, error) {
	if token != "session-token" {
		return "", errors.New("unexpected token")
	}
	service.logoutCalled = true
	return "https://identity.example/logout", nil
}
