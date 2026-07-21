package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

var (
	membershipInvitationTenantID = uuid.MustParse("9504f96d-3390-42bd-861f-78542de5e859")
	membershipInvitationID       = uuid.MustParse("22e50ca8-bbfc-4b0c-a8fd-d514d06cc04b")
	membershipInvitationUserID   = uuid.MustParse("7580458e-cd84-4e10-9986-e4cb437d55a0")
	membershipInvitationToken    = "thinv1_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

func TestMembershipInvitationAdminEndpoints(t *testing.T) {
	t.Parallel()

	t.Run("list requires a session and returns tenant scoped items", func(t *testing.T) {
		t.Parallel()

		service := &fakeIdentityService{
			invitations: []identity.MembershipInvitation{
				membershipInvitationFixture(identity.MembershipInvitationPending),
			},
		}
		response := performMembershipInvitationRequest(
			newMembershipInvitationTestHandler(service, nil, nil),
			http.MethodGet,
			"/api/v1/tenants/"+membershipInvitationTenantID.String()+"/invitations",
			"",
			true,
			false,
			"",
		)

		if response.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
		}
		if service.listInvitationsTenantID != membershipInvitationTenantID {
			t.Fatalf("unexpected tenant scope: %s", service.listInvitationsTenantID)
		}
		var payload membershipInvitationListResponse
		decodeJSON(t, response, &payload)
		if len(payload.Items) != 1 ||
			payload.Items[0].ID != membershipInvitationID ||
			payload.Items[0].Email != "learner@example.com" ||
			payload.Items[0].Status != identity.MembershipInvitationPending {
			t.Fatalf("unexpected list response: %+v", payload)
		}
		assertMembershipInvitationSecurityHeaders(t, response, true)
	})

	t.Run("create requires csrf and returns the share url once", func(t *testing.T) {
		t.Parallel()

		invitation := membershipInvitationFixture(identity.MembershipInvitationPending)
		service := &fakeIdentityService{
			createInvitationResult: identity.CreateMembershipInvitationResult{
				Invitation: invitation,
				Token:      membershipInvitationToken,
			},
		}
		response := performMembershipInvitationRequest(
			newMembershipInvitationTestHandler(service, nil, nil),
			http.MethodPost,
			"/api/v1/tenants/"+membershipInvitationTenantID.String()+"/invitations",
			`{"email":" Learner@Example.com ","intended_role":"student"}`,
			true,
			true,
			"",
		)

		if response.Code != http.StatusCreated {
			t.Fatalf(
				"expected status %d, got %d: %s",
				http.StatusCreated,
				response.Code,
				response.Body.String(),
			)
		}
		if service.createInvitationTenantID != membershipInvitationTenantID ||
			service.createInvitationInput.Email != " Learner@Example.com " ||
			service.createInvitationInput.IntendedRole != "student" {
			t.Fatalf(
				"unexpected create call: tenant=%s input=%+v",
				service.createInvitationTenantID,
				service.createInvitationInput,
			)
		}
		var payload createMembershipInvitationResponse
		decodeJSON(t, response, &payload)
		expectedURL := "http://localhost:5173/invite#token=" + url.QueryEscape(membershipInvitationToken)
		if payload.Invitation.ID != invitation.ID || payload.AcceptURL != expectedURL {
			t.Fatalf("unexpected create response: %+v", payload)
		}
		assertMembershipInvitationSecurityHeaders(t, response, true)
	})

	t.Run("revoke requires csrf and returns the terminal representation", func(t *testing.T) {
		t.Parallel()

		invitation := membershipInvitationFixture(identity.MembershipInvitationRevoked)
		revokedAt := fixedTime
		invitation.RevokedAt = &revokedAt
		service := &fakeIdentityService{revokeInvitationResult: invitation}
		response := performMembershipInvitationRequest(
			newMembershipInvitationTestHandler(service, nil, nil),
			http.MethodPost,
			"/api/v1/tenants/"+membershipInvitationTenantID.String()+
				"/invitations/"+membershipInvitationID.String()+"/revoke",
			"",
			true,
			true,
			"",
		)

		if response.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
		}
		if service.revokeInvitationTenantID != membershipInvitationTenantID ||
			service.revokeInvitationID != membershipInvitationID {
			t.Fatalf(
				"unexpected revoke call: tenant=%s invitation=%s",
				service.revokeInvitationTenantID,
				service.revokeInvitationID,
			)
		}
		var payload membershipInvitationResponse
		decodeJSON(t, response, &payload)
		if payload.Status != identity.MembershipInvitationRevoked || payload.RevokedAt == nil {
			t.Fatalf("unexpected revoke response: %+v", payload)
		}
		assertMembershipInvitationSecurityHeaders(t, response, true)
	})
}

func TestMembershipInvitationAdminAuthenticationAndValidation(t *testing.T) {
	t.Parallel()

	handler := newMembershipInvitationTestHandler(&fakeIdentityService{}, nil, nil)
	path := "/api/v1/tenants/" + membershipInvitationTenantID.String() + "/invitations"

	missingSession := performMembershipInvitationRequest(
		handler,
		http.MethodGet,
		path,
		"",
		false,
		false,
		"",
	)
	if missingSession.Code != http.StatusUnauthorized {
		t.Fatalf("expected missing session status %d, got %d", http.StatusUnauthorized, missingSession.Code)
	}

	missingCSRF := performMembershipInvitationRequest(
		handler,
		http.MethodPost,
		path,
		`{"email":"learner@example.com","intended_role":"student"}`,
		true,
		false,
		"",
	)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("expected missing CSRF status %d, got %d", http.StatusForbidden, missingCSRF.Code)
	}

	invalidBody := performMembershipInvitationRequest(
		handler,
		http.MethodPost,
		path,
		`{"email":"learner@example.com","intended_role":"student","token":"not-allowed"}`,
		true,
		true,
		"",
	)
	if invalidBody.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid body status %d, got %d", http.StatusBadRequest, invalidBody.Code)
	}

	invalidTenant := performMembershipInvitationRequest(
		handler,
		http.MethodGet,
		"/api/v1/tenants/not-a-uuid/invitations",
		"",
		true,
		false,
		"",
	)
	if invalidTenant.Code != http.StatusNotFound {
		t.Fatalf("expected concealed invalid tenant status %d, got %d", http.StatusNotFound, invalidTenant.Code)
	}

	unsupportedMethod := performMembershipInvitationRequest(
		handler,
		http.MethodPut,
		path,
		"",
		true,
		true,
		"",
	)
	if unsupportedMethod.Code != http.StatusMethodNotAllowed ||
		unsupportedMethod.Header().Get("Allow") != "GET, HEAD, POST" {
		t.Fatalf(
			"unexpected unsupported method response: status=%d allow=%q",
			unsupportedMethod.Code,
			unsupportedMethod.Header().Get("Allow"),
		)
	}
}

func TestMembershipInvitationPreviewIsAnonymousAndMinimal(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{
		previewInvitationResult: identity.MembershipInvitationPreview{
			TenantName:   "Example Academy",
			MaskedEmail:  "l***@example.com",
			IntendedRole: "student",
			Status:       identity.MembershipInvitationPending,
			ExpiresAt:    fixedTime.Add(24 * time.Hour),
		},
	}
	response := performMembershipInvitationRequest(
		newMembershipInvitationTestHandler(service, nil, nil),
		http.MethodPost,
		membershipInvitationPreviewPath,
		`{"token":"`+membershipInvitationToken+`"}`,
		false,
		false,
		"203.0.113.27:4123",
	)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if service.previewInvitationToken != membershipInvitationToken {
		t.Fatalf("expected token to reach service body parameter, got %q", service.previewInvitationToken)
	}
	if strings.Contains(response.Body.String(), membershipInvitationToken) ||
		strings.Contains(response.Body.String(), "learner@example.com") {
		t.Fatalf("preview response exposed sensitive input: %s", response.Body.String())
	}
	var payload membershipInvitationPreviewResponse
	decodeJSON(t, response, &payload)
	if payload.TenantName != "Example Academy" ||
		payload.MaskedEmail != "l***@example.com" ||
		payload.IntendedRole != "student" ||
		payload.Status != identity.MembershipInvitationPending {
		t.Fatalf("unexpected preview response: %+v", payload)
	}
	assertMembershipInvitationSecurityHeaders(t, response, false)
}

func TestMembershipInvitationAcceptUsesAuthenticatedPrincipal(t *testing.T) {
	t.Parallel()

	authenticated := membershipInvitationPrincipal()
	updated := authenticated
	acceptedTenant := identity.Tenant{
		ID:       membershipInvitationTenantID,
		Slug:     "example-academy",
		Name:     "Example Academy",
		Locale:   "vi",
		Timezone: "Asia/Ho_Chi_Minh",
		Status:   "active",
		Role:     "student",
	}
	updated.Memberships = append(updated.Memberships, acceptedTenant)
	invitation := membershipInvitationFixture(identity.MembershipInvitationAccepted)
	acceptedAt := fixedTime
	invitation.AcceptedAt = &acceptedAt
	service := &fakeIdentityService{
		principal: authenticated,
		acceptInvitationResult: identity.AcceptMembershipInvitationResult{
			Invitation: invitation,
			Principal:  updated,
		},
	}
	response := performMembershipInvitationRequest(
		newMembershipInvitationTestHandler(service, nil, nil),
		http.MethodPost,
		membershipInvitationAcceptPath,
		`{"token":"`+membershipInvitationToken+`"}`,
		true,
		true,
		"2001:db8:abcd:1200::1",
	)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, response.Code, response.Body.String())
	}
	if service.acceptInvitationToken != membershipInvitationToken ||
		service.acceptInvitationPrincipal.User.ID != authenticated.User.ID ||
		service.acceptInvitationPrincipal.SessionID != authenticated.SessionID {
		t.Fatalf(
			"unexpected accept call: token=%q principal=%+v",
			service.acceptInvitationToken,
			service.acceptInvitationPrincipal,
		)
	}
	var payload acceptMembershipInvitationResponse
	decodeJSON(t, response, &payload)
	if payload.Invitation.Status != identity.MembershipInvitationAccepted ||
		payload.CurrentUser.User.ID != authenticated.User.ID ||
		len(payload.CurrentUser.Memberships) != len(updated.Memberships) {
		t.Fatalf("unexpected accept response: %+v", payload)
	}
	assertMembershipInvitationSecurityHeaders(t, response, true)
}

func TestMembershipInvitationProblemMappingDoesNotEnableEnumeration(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		endpoint      string
		serviceError  error
		expectedCode  int
		expectedTitle string
	}{
		{
			name:          "invalid token",
			endpoint:      "preview",
			serviceError:  identity.ErrInvalidMembershipInvitation,
			expectedCode:  http.StatusNotFound,
			expectedTitle: "Invitation unavailable",
		},
		{
			name:          "unknown token",
			endpoint:      "preview",
			serviceError:  identity.ErrMembershipInvitationUnavailable,
			expectedCode:  http.StatusNotFound,
			expectedTitle: "Invitation unavailable",
		},
		{
			name:          "expired token",
			endpoint:      "preview",
			serviceError:  identity.ErrMembershipInvitationExpired,
			expectedCode:  http.StatusNotFound,
			expectedTitle: "Invitation unavailable",
		},
		{
			name:          "identity mismatch",
			endpoint:      "accept",
			serviceError:  identity.ErrMembershipInvitationIdentityMismatch,
			expectedCode:  http.StatusForbidden,
			expectedTitle: "Invitation identity mismatch",
		},
		{
			name:          "accept state conflict",
			endpoint:      "accept",
			serviceError:  identity.ErrMembershipInvitationConflict,
			expectedCode:  http.StatusConflict,
			expectedTitle: "Invitation state conflict",
		},
		{
			name:          "invalid admin input",
			endpoint:      "create",
			serviceError:  identity.ErrInvalidMembershipInvitation,
			expectedCode:  http.StatusBadRequest,
			expectedTitle: "Invalid invitation request",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := &fakeIdentityService{}
			method := http.MethodPost
			path := membershipInvitationPreviewPath
			body := `{"token":"` + membershipInvitationToken + `"}`
			authenticated := false
			csrf := false
			switch testCase.endpoint {
			case "preview":
				service.previewInvitationError = testCase.serviceError
			case "accept":
				service.acceptInvitationError = testCase.serviceError
				path = membershipInvitationAcceptPath
				authenticated = true
				csrf = true
			case "create":
				service.createInvitationError = testCase.serviceError
				path = "/api/v1/tenants/" + membershipInvitationTenantID.String() + "/invitations"
				body = `{"email":"learner@example.com","intended_role":"student"}`
				authenticated = true
				csrf = true
			default:
				t.Fatalf("unknown endpoint fixture %q", testCase.endpoint)
			}

			response := performMembershipInvitationRequest(
				newMembershipInvitationTestHandler(service, nil, nil),
				method,
				path,
				body,
				authenticated,
				csrf,
				"",
			)
			if response.Code != testCase.expectedCode {
				t.Fatalf(
					"expected status %d, got %d: %s",
					testCase.expectedCode,
					response.Code,
					response.Body.String(),
				)
			}
			if strings.Contains(response.Body.String(), membershipInvitationToken) {
				t.Fatalf("problem response exposed raw token: %s", response.Body.String())
			}
			var payload Problem
			decodeJSON(t, response, &payload)
			if payload.Title != testCase.expectedTitle {
				t.Fatalf("unexpected problem response: %+v", payload)
			}
		})
	}
}

func TestMembershipInvitationTokenEndpointsRejectMalformedRequestsUniformly(t *testing.T) {
	t.Parallel()

	service := &fakeIdentityService{}
	handler := newMembershipInvitationTestHandler(service, nil, nil)

	for _, path := range []string{
		membershipInvitationPreviewPath,
		membershipInvitationAcceptPath,
	} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			response := performMembershipInvitationRequest(
				handler,
				http.MethodPost,
				path,
				`{"token":"`+membershipInvitationToken+`","unexpected":true}`,
				path == membershipInvitationAcceptPath,
				path == membershipInvitationAcceptPath,
				"",
			)
			if response.Code != http.StatusNotFound {
				t.Fatalf("expected uniform status %d, got %d", http.StatusNotFound, response.Code)
			}
			if strings.Contains(response.Body.String(), membershipInvitationToken) {
				t.Fatalf("malformed request response exposed raw token: %s", response.Body.String())
			}
			var payload Problem
			decodeJSON(t, response, &payload)
			if payload.Title != "Invitation unavailable" {
				t.Fatalf("unexpected malformed request response: %+v", payload)
			}
		})
	}

	unsupported := performMembershipInvitationRequest(
		handler,
		http.MethodGet,
		membershipInvitationPreviewPath,
		"",
		false,
		false,
		"",
	)
	if unsupported.Code != http.StatusMethodNotAllowed ||
		unsupported.Header().Get("Allow") != http.MethodPost {
		t.Fatalf(
			"unexpected method response: status=%d allow=%q",
			unsupported.Code,
			unsupported.Header().Get("Allow"),
		)
	}
}

func TestMembershipInvitationRateLimitUsesOnlyClientPrefixAndAction(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		path          string
		action        InvitationRateLimitAction
		authenticated bool
		csrf          bool
		expectedVary  bool
	}{
		{
			name:   "preview",
			path:   membershipInvitationPreviewPath,
			action: InvitationRateLimitPreview,
		},
		{
			name:          "accept",
			path:          membershipInvitationAcceptPath,
			action:        InvitationRateLimitAccept,
			authenticated: true,
			csrf:          true,
			expectedVary:  true,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			limiter := &recordingInvitationRateLimiter{
				decision: InvitationRateLimitDecision{
					Allowed:    false,
					RetryAfter: 1500 * time.Millisecond,
				},
			}
			service := &fakeIdentityService{}
			response := performMembershipInvitationRequest(
				newMembershipInvitationTestHandler(service, limiter, nil),
				http.MethodPost,
				testCase.path,
				`{"token":"`+membershipInvitationToken+`"}`,
				testCase.authenticated,
				testCase.csrf,
				"203.0.113.27:4123",
			)

			if response.Code != http.StatusTooManyRequests ||
				response.Header().Get("Retry-After") != "2" {
				t.Fatalf(
					"unexpected rate limit response: status=%d retry-after=%q body=%s",
					response.Code,
					response.Header().Get("Retry-After"),
					response.Body.String(),
				)
			}
			if len(limiter.calls) != 1 ||
				limiter.calls[0].action != testCase.action ||
				limiter.calls[0].clientPrefix != "203.0.113.0/24" {
				t.Fatalf("unexpected limiter calls: %+v", limiter.calls)
			}
			if strings.Contains(limiter.calls[0].clientPrefix, membershipInvitationToken) ||
				strings.Contains(response.Body.String(), membershipInvitationToken) {
				t.Fatal("raw invitation token must not be retained in limiter keys or returned")
			}
			if service.previewInvitationToken != "" || service.acceptInvitationToken != "" {
				t.Fatal("rate limited request must not reach the identity service")
			}
			assertMembershipInvitationSecurityHeaders(t, response, testCase.expectedVary)
		})
	}
}

func TestMembershipInvitationRateLimiterFailureIsUnavailable(t *testing.T) {
	t.Parallel()

	limiter := &recordingInvitationRateLimiter{
		decision: InvitationRateLimitDecision{Err: errors.New("database unavailable")},
	}
	service := &fakeIdentityService{}
	response := performMembershipInvitationRequest(
		newMembershipInvitationTestHandler(service, limiter, nil),
		http.MethodPost,
		membershipInvitationPreviewPath,
		`{"token":"`+membershipInvitationToken+`"}`,
		false,
		false,
		"203.0.113.27:4123",
	)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 response, got %d: %s", response.Code, response.Body.String())
	}
	var problem Problem
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "rate_limit_unavailable" {
		t.Fatalf("expected typed limiter problem, got %+v", problem)
	}
	if response.Header().Get("Retry-After") != "" {
		t.Fatalf("storage failure must not claim a fixed retry delay: %q", response.Header().Get("Retry-After"))
	}
	if service.previewInvitationToken != "" {
		t.Fatal("request must not reach the identity service when the limiter is unavailable")
	}
}

func TestMembershipInvitationRawTokenIsAbsentFromStructuredLogs(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	service := &fakeIdentityService{
		previewInvitationError: errors.New("storage unavailable"),
	}
	response := performMembershipInvitationRequest(
		newMembershipInvitationTestHandler(service, nil, logger),
		http.MethodPost,
		membershipInvitationPreviewPath,
		`{"token":"`+membershipInvitationToken+`"}`,
		false,
		false,
		"203.0.113.27:4123",
	)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusInternalServerError,
			response.Code,
			response.Body.String(),
		)
	}
	if strings.Contains(logs.String(), membershipInvitationToken) ||
		strings.Contains(response.Body.String(), membershipInvitationToken) {
		t.Fatalf("raw invitation token leaked: logs=%s response=%s", logs.String(), response.Body.String())
	}
}

type recordingInvitationRateLimiter struct {
	decision InvitationRateLimitDecision
	calls    []recordedInvitationRateLimitCall
}

type recordedInvitationRateLimitCall struct {
	action       InvitationRateLimitAction
	clientPrefix string
	now          time.Time
}

func (limiter *recordingInvitationRateLimiter) Allow(
	_ context.Context,
	action InvitationRateLimitAction,
	clientPrefix string,
	now time.Time,
) InvitationRateLimitDecision {
	limiter.calls = append(limiter.calls, recordedInvitationRateLimitCall{
		action:       action,
		clientPrefix: clientPrefix,
		now:          now,
	})
	return limiter.decision
}

func newMembershipInvitationTestHandler(
	service identity.ServiceAPI,
	limiter InvitationRateLimiter,
	logger *slog.Logger,
) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test",
			Port:        "8080",
			WebOrigin:   "http://localhost:5173",
			Authentication: config.AuthenticationConfig{
				CookieSecure: false,
				SessionTTL:   8 * time.Hour,
			},
		},
		logger,
		Options{
			Clock:                 func() time.Time { return fixedTime },
			Identity:              service,
			InvitationRateLimiter: limiter,
		},
	)
}

func performMembershipInvitationRequest(
	handler http.Handler,
	method string,
	path string,
	body string,
	authenticated bool,
	csrf bool,
	remoteAddress string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if remoteAddress != "" {
		request.RemoteAddr = remoteAddress
	}
	if authenticated {
		request.AddCookie(&http.Cookie{Name: "tutorhub_session", Value: "session-token"})
	}
	if csrf {
		request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
		request.Header.Set(csrfHeader, "csrf-token")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func membershipInvitationFixture(
	status identity.MembershipInvitationStatus,
) identity.MembershipInvitation {
	return identity.MembershipInvitation{
		ID:           membershipInvitationID,
		TenantID:     membershipInvitationTenantID,
		Email:        "learner@example.com",
		IntendedRole: "student",
		Status:       status,
		ExpiresAt:    fixedTime.Add(24 * time.Hour),
		CreatedAt:    fixedTime.Add(-time.Hour),
		UpdatedAt:    fixedTime.Add(-time.Hour),
		InvitedBy:    uuid.MustParse("3830d9b3-36ca-4c82-a82b-4954233ac6a6"),
	}
}

func membershipInvitationPrincipal() identity.Principal {
	activeTenant := identity.Tenant{
		ID:       uuid.MustParse("9e04b8ba-ad9d-4384-982e-dd4881806efb"),
		Slug:     "admin-workspace",
		Name:     "Admin Workspace",
		Locale:   "vi",
		Timezone: "Asia/Ho_Chi_Minh",
		Status:   "active",
		Role:     "org_admin",
		IsActive: true,
	}
	return identity.Principal{
		SessionID: uuid.MustParse("74144415-8082-49d7-b4e3-4443a1f31b5c"),
		User: identity.User{
			ID:          membershipInvitationUserID,
			Email:       "learner@example.com",
			DisplayName: "Learner",
			Locale:      "vi",
			Timezone:    "Asia/Ho_Chi_Minh",
		},
		ActiveTenant: &activeTenant,
		Memberships:  []identity.Tenant{activeTenant},
		Permissions:  []string{"tenant.manage_members"},
	}
}

func assertMembershipInvitationSecurityHeaders(
	t *testing.T,
	response *httptest.ResponseRecorder,
	varyCookie bool,
) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store cache policy, got %q", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf(
			"expected no-referrer policy, got %q",
			response.Header().Get("Referrer-Policy"),
		)
	}
	hasCookieVary := false
	for _, value := range response.Header().Values("Vary") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "Cookie") {
				hasCookieVary = true
			}
		}
	}
	if hasCookieVary != varyCookie {
		t.Fatalf(
			"expected Vary Cookie=%t, got headers %v",
			varyCookie,
			response.Header().Values("Vary"),
		)
	}
}
