package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func TestProfileRoutesReadAndUpdateCurrentUser(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{}
	handler := newAuthTestHandler(service, false)

	readRequest := authenticatedRequest(http.MethodGet, "/api/v1/me/profile", "")
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("read profile: status=%d body=%s", readResponse.Code, readResponse.Body.String())
	}
	var readPayload profileResponse
	if err := json.NewDecoder(readResponse.Body).Decode(&readPayload); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	if readPayload.User.Email != "student@example.com" {
		t.Fatalf("unexpected profile: %+v", readPayload.User)
	}

	updateRequest := authenticatedRequest(
		http.MethodPatch,
		"/api/v1/me/profile",
		`{"display_name":"Nguyen Ba Sang","locale":"en","timezone":"Asia/Bangkok"}`,
	)
	updateResponse := httptest.NewRecorder()
	handler.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update profile: status=%d body=%s", updateResponse.Code, updateResponse.Body.String())
	}
	if service.profilePatch.DisplayName == nil ||
		*service.profilePatch.DisplayName != "Nguyen Ba Sang" ||
		service.profilePatch.Locale == nil || *service.profilePatch.Locale != "en" {
		t.Fatalf("unexpected profile patch: %+v", service.profilePatch)
	}
}

func TestProfileUpdateRejectsUnknownFieldsAndInvalidProfile(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{}
	handler := newAuthTestHandler(service, false)

	unknownRequest := authenticatedRequest(
		http.MethodPatch,
		"/api/v1/me/profile",
		`{"display_name":"Student","role":"admin"}`,
	)
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknownRequest)
	if unknownResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown field: status=%d body=%s", unknownResponse.Code, unknownResponse.Body.String())
	}

	service.profileError = identity.ErrInvalidProfile
	invalidRequest := authenticatedRequest(
		http.MethodPatch,
		"/api/v1/me/profile",
		`{"locale":"unsupported"}`,
	)
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile: status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

func TestIdentityRoutesListLinkAndUnlink(t *testing.T) {
	t.Parallel()

	identityID := uuid.MustParse("4ac05f73-214e-47c3-b44d-88d5ec1a5907")
	service := &fakeIdentityService{identities: []identity.ExternalIdentity{{
		ID:                  identityID,
		Provider:            "zitadel",
		Email:               "student@example.com",
		EmailVerified:       true,
		CreatedAt:           fixedTime.Add(-24 * time.Hour),
		LastAuthenticatedAt: fixedTime,
	}}}
	handler := newAuthTestHandler(service, false)

	listRequest := authenticatedRequest(http.MethodGet, "/api/v1/me/identities", "")
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), identityID.String()) {
		t.Fatalf("list identities: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}

	linkRequest := authenticatedRequest(http.MethodPost, "/api/v1/me/identities/link", "")
	linkResponse := httptest.NewRecorder()
	handler.ServeHTTP(linkResponse, linkRequest)
	if linkResponse.Code != http.StatusOK || !service.beginLinkCalled {
		t.Fatalf("begin link: status=%d body=%s", linkResponse.Code, linkResponse.Body.String())
	}
	if findCookie(t, linkResponse.Result().Cookies(), "tutorhub_oidc").Value != "link-browser-binding" {
		t.Fatal("identity link must set a one-time flow cookie")
	}

	unlinkRequest := authenticatedRequest(
		http.MethodDelete,
		"/api/v1/me/identities/"+identityID.String(),
		"",
	)
	unlinkResponse := httptest.NewRecorder()
	handler.ServeHTTP(unlinkResponse, unlinkRequest)
	if unlinkResponse.Code != http.StatusNoContent || service.unlinkedIdentityID != identityID {
		t.Fatalf("unlink identity: status=%d id=%s", unlinkResponse.Code, service.unlinkedIdentityID)
	}
}

func TestIdentityRoutesRequireRecentAuthenticationAndPreserveSessionAfterLink(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{identityError: identity.ErrRecentAuthenticationRequired}
	handler := newAuthTestHandler(service, false)
	unlinkRequest := authenticatedRequest(
		http.MethodDelete,
		"/api/v1/me/identities/4ac05f73-214e-47c3-b44d-88d5ec1a5907",
		"",
	)
	unlinkResponse := httptest.NewRecorder()
	handler.ServeHTTP(unlinkResponse, unlinkRequest)
	if unlinkResponse.Code != http.StatusForbidden {
		t.Fatalf("recent auth: status=%d body=%s", unlinkResponse.Code, unlinkResponse.Body.String())
	}

	service.identityError = nil
	service.completeLogin = &identity.LoginResult{
		IdentityLinked: true,
		ReturnTo:       "/app/settings",
	}
	callback := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/auth/callback?state=state-123&code=code-123",
		nil,
	)
	callback.AddCookie(&http.Cookie{Name: "tutorhub_oidc", Value: "link-browser-binding"})
	callbackResponse := httptest.NewRecorder()
	handler.ServeHTTP(callbackResponse, callback)
	if callbackResponse.Code != http.StatusSeeOther ||
		callbackResponse.Header().Get("Location") != "http://localhost:5173/app/settings" {
		t.Fatalf("link callback: status=%d location=%s", callbackResponse.Code, callbackResponse.Header().Get("Location"))
	}
	for _, cookie := range callbackResponse.Result().Cookies() {
		if cookie.Name == "tutorhub_session" || cookie.Name == "tutorhub_csrf" {
			t.Fatalf("identity link callback replaced active session cookie: %+v", cookie)
		}
	}
}

func authenticatedRequest(method string, path string, body string) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	request.Header.Set(csrfHeader, "csrf-token")
	return request
}
