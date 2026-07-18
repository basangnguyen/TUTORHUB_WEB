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
	if csrfCookie.Value != "csrf-token" || !csrfCookie.HttpOnly {
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

func TestWorkspaceOnboardingCreatesTenantAndRotatesSession(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{}
	handler := newAuthTestHandler(service, false)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tenants",
		strings.NewReader(`{"name":"Khoa Công nghệ thông tin","slug":"kma-lab"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated || !service.createTenantCalled {
		t.Fatalf("unexpected tenant creation: status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/api/v1/tenants/c91445df-bde0-44f2-83ed-33ec6148bb84" {
		t.Fatalf("unexpected tenant location: %q", response.Header().Get("Location"))
	}
	if service.createTenantInput.Name != "Khoa Công nghệ thông tin" ||
		service.createTenantInput.Slug != "kma-lab" {
		t.Fatalf("unexpected tenant input: %+v", service.createTenantInput)
	}
	if findCookie(t, response.Result().Cookies(), "tutorhub_session").Value != "rotated-session" ||
		findCookie(t, response.Result().Cookies(), "tutorhub_csrf").Value != "rotated-csrf" {
		t.Fatal("tenant creation must rotate both session cookies")
	}
	var currentUser meResponse
	if err := json.NewDecoder(response.Body).Decode(&currentUser); err != nil {
		t.Fatalf("decode tenant creation response: %v", err)
	}
	if currentUser.ActiveTenant == nil || currentUser.ActiveTenant.Role != "org_admin" {
		t.Fatalf("unexpected tenant creation response: %+v", currentUser)
	}
}

func TestWorkspaceMutationRejectsInvalidRequestsAndCrossTenantSwitch(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{}
	handler := newAuthTestHandler(service, false)

	invalid := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tenants",
		strings.NewReader(`{"name":"Workspace","slug":"workspace","unexpected":true}`),
	)
	invalid.Header.Set("Content-Type", "application/json")
	invalid.Header.Set(csrfHeader, "csrf-token")
	invalid.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	invalid.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest || service.createTenantCalled {
		t.Fatalf("unknown JSON fields must be rejected: status=%d", invalidResponse.Code)
	}

	foreignTenantID := uuid.MustParse("d53466c6-fb22-49bb-8dcb-e0399896a6c8")
	switchRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/session/active-tenant",
		strings.NewReader(`{"tenant_id":"`+foreignTenantID.String()+`"}`),
	)
	switchRequest.Header.Set("Content-Type", "application/json")
	switchRequest.Header.Set(csrfHeader, "csrf-token")
	switchRequest.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	switchRequest.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	switchResponse := httptest.NewRecorder()
	handler.ServeHTTP(switchResponse, switchRequest)
	if switchResponse.Code != http.StatusForbidden || service.switchTenantID != foreignTenantID {
		t.Fatalf("cross-tenant switch must be denied: status=%d body=%s", switchResponse.Code, switchResponse.Body.String())
	}
}

func TestWorkspaceLifecycleHTTPContract(t *testing.T) {
	t.Parallel()

	tenantID := uuid.MustParse("c91445df-bde0-44f2-83ed-33ec6148bb84")
	tenant := identity.Tenant{
		ID: tenantID, Slug: "tutorhub-test", Name: "TutorHub Test",
		Locale: "vi", Timezone: "Asia/Ho_Chi_Minh", Status: "active", Version: 3,
		Role: "org_admin", IsActive: true,
		CreatedAt: fixedTime.Add(-24 * time.Hour), UpdatedAt: fixedTime,
	}
	service := &fakeIdentityService{principal: identity.Principal{
		SessionID: uuid.MustParse("e27001f3-76db-4169-9dc0-bf451060ddf0"),
		User: identity.User{
			ID:    uuid.MustParse("be85eb92-0f18-4163-85ba-50e4d343d632"),
			Email: "admin@example.com", DisplayName: "Admin", Locale: "vi",
			Timezone: "Asia/Ho_Chi_Minh",
		},
		ActiveTenant: &tenant, Memberships: []identity.Tenant{tenant},
		Permissions: []string{"tenant.view", "tenant.manage"},
	}}
	handler := newAuthTestHandler(service, false)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	listRequest.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list tenants: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	var listed tenantListResponse
	if err := json.NewDecoder(listResponse.Body).Decode(&listed); err != nil ||
		len(listed.Items) != 1 || listed.Items[0].Version != 3 {
		t.Fatalf("unexpected tenant list: items=%+v error=%v", listed.Items, err)
	}

	detailRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/tenants/"+tenantID.String(),
		nil,
	)
	detailRequest.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	detailResponse := httptest.NewRecorder()
	handler.ServeHTTP(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("get tenant: status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
	}

	patchRequest := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/tenants/"+tenantID.String(),
		strings.NewReader(`{"name":"TutorHub Academy","expected_version":3}`),
	)
	patchRequest.Header.Set("Content-Type", "application/json")
	patchRequest.Header.Set(csrfHeader, "csrf-token")
	patchRequest.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	patchRequest.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	patchResponse := httptest.NewRecorder()
	handler.ServeHTTP(patchResponse, patchRequest)
	if patchResponse.Code != http.StatusOK || service.updatedTenantID != tenantID ||
		service.updateTenantInput.ExpectedVersion != 3 {
		t.Fatalf("update tenant: status=%d input=%+v body=%s", patchResponse.Code, service.updateTenantInput, patchResponse.Body.String())
	}

	archiveRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/tenants/"+tenantID.String()+"/archive",
		strings.NewReader(`{"expected_version":4}`),
	)
	archiveRequest.Header.Set("Content-Type", "application/json")
	archiveRequest.Header.Set(csrfHeader, "csrf-token")
	archiveRequest.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	archiveRequest.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	archiveResponse := httptest.NewRecorder()
	handler.ServeHTTP(archiveResponse, archiveRequest)
	if archiveResponse.Code != http.StatusOK || service.archivedTenantID != tenantID ||
		service.archiveVersion != 4 {
		t.Fatalf("archive tenant: status=%d version=%d body=%s", archiveResponse.Code, service.archiveVersion, archiveResponse.Body.String())
	}
	if findCookie(t, archiveResponse.Result().Cookies(), "tutorhub_session").Value != "rotated-session" {
		t.Fatal("archive must rotate the session cookie")
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
	returnTo           string
	callback           identity.CallbackInput
	completeLogin      *identity.LoginResult
	logoutCalled       bool
	createTenantCalled bool
	createTenantInput  identity.CreateTenantInput
	updatedTenantID    uuid.UUID
	updateTenantInput  identity.UpdateTenantInput
	archivedTenantID   uuid.UUID
	archiveVersion     int64
	switchTenantID     uuid.UUID
	principal          identity.Principal
	profilePatch       identity.ProfilePatch
	profileError       error
	identities         []identity.ExternalIdentity
	identityError      error
	beginLinkCalled    bool
	unlinkedIdentityID uuid.UUID
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
	if service.completeLogin != nil {
		return *service.completeLogin, nil
	}
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

func (service *fakeIdentityService) CreateTenant(
	_ context.Context,
	principal identity.Principal,
	input identity.CreateTenantInput,
) (identity.TenantSessionResult, error) {
	service.createTenantCalled = true
	service.createTenantInput = input
	tenant := identity.Tenant{
		ID:       uuid.MustParse("c91445df-bde0-44f2-83ed-33ec6148bb84"),
		Slug:     input.Slug,
		Name:     input.Name,
		Locale:   "vi",
		Timezone: "Asia/Ho_Chi_Minh",
		Status:   "active",
		Version:  1,
		Role:     "org_admin",
		IsActive: true,
	}
	principal.ActiveTenant = &tenant
	principal.Memberships = []identity.Tenant{tenant}
	principal.Permissions = []string{"tenant.manage"}
	service.principal = principal

	return identity.TenantSessionResult{
		Principal:    principal,
		SessionToken: "rotated-session",
		CSRFToken:    "rotated-csrf",
		ExpiresAt:    fixedTime.Add(8 * time.Hour),
	}, nil
}

func (service *fakeIdentityService) ListTenants(
	_ context.Context,
	principal identity.Principal,
) ([]identity.Tenant, error) {
	return append([]identity.Tenant(nil), principal.Memberships...), nil
}

func (service *fakeIdentityService) GetTenant(
	_ context.Context,
	principal identity.Principal,
	tenantID uuid.UUID,
) (identity.Tenant, error) {
	for _, tenant := range principal.Memberships {
		if tenant.ID == tenantID && tenant.Status == "active" {
			return tenant, nil
		}
	}
	return identity.Tenant{}, identity.ErrTenantNotFound
}

func (service *fakeIdentityService) UpdateTenant(
	_ context.Context,
	principal identity.Principal,
	tenantID uuid.UUID,
	input identity.UpdateTenantInput,
) (identity.Tenant, error) {
	service.updatedTenantID = tenantID
	service.updateTenantInput = input
	for index := range principal.Memberships {
		tenant := &principal.Memberships[index]
		if tenant.ID != tenantID {
			continue
		}
		if input.Name != nil {
			tenant.Name = *input.Name
		}
		if input.Slug != nil {
			tenant.Slug = *input.Slug
		}
		tenant.Version++
		service.principal = principal
		return *tenant, nil
	}
	return identity.Tenant{}, identity.ErrTenantNotFound
}

func (service *fakeIdentityService) ArchiveTenant(
	_ context.Context,
	principal identity.Principal,
	tenantID uuid.UUID,
	expectedVersion int64,
) (identity.TenantArchiveResult, error) {
	service.archivedTenantID = tenantID
	service.archiveVersion = expectedVersion
	principal.ActiveTenant = nil
	principal.Memberships = []identity.Tenant{}
	principal.Permissions = []string{}
	service.principal = principal
	return identity.TenantArchiveResult{
		Principal:    principal,
		SessionToken: "rotated-session",
		CSRFToken:    "rotated-csrf",
		ExpiresAt:    fixedTime.Add(8 * time.Hour),
	}, nil
}

func (service *fakeIdentityService) SwitchActiveTenant(
	_ context.Context,
	principal identity.Principal,
	tenantID uuid.UUID,
) (identity.TenantSessionResult, error) {
	service.switchTenantID = tenantID
	for index := range principal.Memberships {
		if principal.Memberships[index].ID == tenantID {
			principal.Memberships[index].IsActive = true
			selected := principal.Memberships[index]
			principal.ActiveTenant = &selected
			service.principal = principal
			return identity.TenantSessionResult{
				Principal:    principal,
				SessionToken: "rotated-session",
				CSRFToken:    "rotated-csrf",
				ExpiresAt:    fixedTime.Add(8 * time.Hour),
			}, nil
		}
	}
	return identity.TenantSessionResult{}, identity.ErrTenantAccessDenied
}

func (service *fakeIdentityService) GetProfile(
	ctx context.Context,
	principal identity.Principal,
) (identity.User, error) {
	if service.profileError != nil {
		return identity.User{}, service.profileError
	}
	if principal.User.ID == uuid.Nil {
		return identity.User{}, identity.ErrSessionNotFound
	}
	return principal.User, nil
}

func (service *fakeIdentityService) UpdateProfile(
	_ context.Context,
	principal identity.Principal,
	patch identity.ProfilePatch,
) (identity.User, error) {
	service.profilePatch = patch
	if service.profileError != nil {
		return identity.User{}, service.profileError
	}
	if patch.DisplayName != nil {
		principal.User.DisplayName = *patch.DisplayName
	}
	if patch.Locale != nil {
		principal.User.Locale = *patch.Locale
	}
	if patch.Timezone != nil {
		principal.User.Timezone = *patch.Timezone
	}
	if patch.AvatarObjectKey != nil {
		principal.User.AvatarObjectKey = *patch.AvatarObjectKey
	}
	service.principal = principal
	return principal.User, nil
}

func (service *fakeIdentityService) ListIdentities(
	_ context.Context,
	_ identity.Principal,
) ([]identity.ExternalIdentity, error) {
	if service.identityError != nil {
		return nil, service.identityError
	}
	return service.identities, nil
}

func (service *fakeIdentityService) BeginIdentityLink(
	_ context.Context,
	_ identity.Principal,
) (identity.LoginStart, error) {
	service.beginLinkCalled = true
	if service.identityError != nil {
		return identity.LoginStart{}, service.identityError
	}
	return identity.LoginStart{
		AuthorizationURL: "https://identity.example/link",
		BrowserBinding:   "link-browser-binding",
		ExpiresAt:        fixedTime.Add(10 * time.Minute),
	}, nil
}

func (service *fakeIdentityService) UnlinkIdentity(
	_ context.Context,
	_ identity.Principal,
	identityID uuid.UUID,
) error {
	service.unlinkedIdentityID = identityID
	return service.identityError
}
