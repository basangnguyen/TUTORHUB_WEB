package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/conversation"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

var conversationTestTime = time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)

func TestConversationHandlersListUsesExpectedTenantAndOmitsDirectClassFields(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	userID := uuid.New()
	otherID := uuid.New()
	item := conversation.Conversation{
		ID: uuid.New(), TenantID: tenantID, Kind: conversation.KindDirect,
		Title: "Other member",
		Participants: []conversation.Participant{
			{UserID: userID, DisplayName: "Current member"},
			{UserID: otherID, DisplayName: "Other member"},
		},
		ViewerAccess: conversation.ViewerAccess{CanPostMessages: true},
		CreatedAt:    conversationTestTime, UpdatedAt: conversationTestTime,
	}
	service := &fakeConversationService{
		page: conversation.Page{Items: []conversation.Conversation{item}, NextCursor: "next"},
	}
	handler := conversationTestHandler(classIdentityService(tenantID, userID, nil), service)
	request := httptest.NewRequest(
		http.MethodGet,
		conversationsCollectionPath+"?kind=direct&limit=17&cursor=current",
		nil,
	)
	addSessionCookie(request)
	request.Header.Set(conversationTenantHeader, tenantID.String())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	assertConversationHeaders(t, response)
	if service.listCalls != 1 || service.listAccess.TenantID != tenantID ||
		service.listAccess.ActorID != userID || service.listInput.Limit != 17 ||
		service.listInput.Cursor != "current" || service.listInput.Kind == nil ||
		*service.listInput.Kind != conversation.KindDirect {
		t.Fatalf("unexpected list invocation: %+v", service)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected list body: %#v", body)
	}
	projected := items[0].(map[string]any)
	if _, exposed := projected["class_id"]; exposed {
		t.Fatalf("direct conversation exposed class_id: %#v", projected)
	}
	if _, exposed := projected["class_status"]; exposed {
		t.Fatalf("direct conversation exposed class_status: %#v", projected)
	}
	if _, exposed := projected["tenant_id"]; exposed {
		t.Fatalf("conversation exposed tenant_id: %#v", projected)
	}
	projectedParticipants, ok := projected["participants"].([]any)
	if !ok || len(projectedParticipants) != 2 {
		t.Fatalf("unexpected participant projection: %#v", projected)
	}
	for _, participant := range projectedParticipants {
		fields, ok := participant.(map[string]any)
		if !ok {
			t.Fatalf("unexpected participant shape: %#v", participant)
		}
		if _, exposed := fields["email"]; exposed {
			t.Fatalf("conversation participant exposed email: %#v", fields)
		}
	}
}

func TestConversationHandlersCreateDirectAndClassWithCSRF(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	userID := uuid.New()
	classID := uuid.New()
	directID := uuid.New()
	classConversationID := uuid.New()
	classStatus := "active"
	service := &fakeConversationService{
		directResult: conversation.CreateResult{
			Created: true,
			Conversation: conversation.Conversation{
				ID: directID, Kind: conversation.KindDirect, Title: "Student",
				Participants: []conversation.Participant{}, CreatedAt: conversationTestTime,
				UpdatedAt: conversationTestTime,
			},
		},
		classResult: conversation.CreateResult{
			Conversation: conversation.Conversation{
				ID: classConversationID, Kind: conversation.KindClass, ClassID: &classID,
				ClassStatus: &classStatus, Title: "Security class",
				Participants: []conversation.Participant{}, CreatedAt: conversationTestTime,
				UpdatedAt: conversationTestTime,
			},
		},
	}
	handler := conversationTestHandler(classIdentityService(tenantID, userID, nil), service)
	direct := conversationMutationRequest(
		conversationDirectPath,
		`{"target_member_email":"student@example.test"}`,
		tenantID,
	)
	directResponse := httptest.NewRecorder()
	handler.ServeHTTP(directResponse, direct)
	if directResponse.Code != http.StatusCreated {
		t.Fatalf("direct status=%d body=%s", directResponse.Code, directResponse.Body.String())
	}
	if service.directCalls != 1 || service.directEmail != "student@example.test" ||
		service.directAccess.ActorID != userID {
		t.Fatalf("unexpected direct invocation: %+v", service)
	}

	classRequest := conversationMutationRequest(
		"/api/v1/classes/"+classID.String()+"/conversation",
		"",
		tenantID,
	)
	classResponse := httptest.NewRecorder()
	handler.ServeHTTP(classResponse, classRequest)
	if classResponse.Code != http.StatusOK {
		t.Fatalf("class status=%d body=%s", classResponse.Code, classResponse.Body.String())
	}
	if service.classCalls != 1 || service.classID != classID ||
		service.classAccess.ActorID != userID {
		t.Fatalf("unexpected class invocation: %+v", service)
	}

	missingCSRF := httptest.NewRequest(
		http.MethodPost,
		conversationDirectPath,
		strings.NewReader(`{"target_member_email":"student@example.test"}`),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(conversationTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}
	if service.directCalls != 1 {
		t.Fatalf("missing CSRF reached service: calls=%d", service.directCalls)
	}
}

func TestConversationHandlersRejectScopeAndMapConcealedErrors(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	service := &fakeConversationService{getError: conversation.ErrUnavailable}
	handler := conversationTestHandler(
		classIdentityService(tenantID, uuid.New(), nil), service,
	)
	missing := httptest.NewRequest(http.MethodGet, conversationsCollectionPath, nil)
	addSessionCookie(missing)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	assertConversationProblem(t, missingResponse, http.StatusBadRequest, "conversation_invalid")

	stale := httptest.NewRequest(http.MethodGet, conversationsCollectionPath, nil)
	addSessionCookie(stale)
	stale.Header.Set(conversationTenantHeader, uuid.NewString())
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	assertConversationProblem(t, staleResponse, http.StatusConflict, "conversation_scope_changed")

	resource := httptest.NewRequest(
		http.MethodGet, "/api/v1/conversations/"+uuid.NewString(), nil,
	)
	addSessionCookie(resource)
	resource.Header.Set(conversationTenantHeader, tenantID.String())
	resourceResponse := httptest.NewRecorder()
	handler.ServeHTTP(resourceResponse, resource)
	assertConversationProblem(t, resourceResponse, http.StatusNotFound, "conversation_not_found")
}

type fakeConversationService struct {
	page         conversation.Page
	listError    error
	listCalls    int
	listAccess   conversation.AccessContext
	listInput    conversation.ListInput
	getResult    conversation.Conversation
	getError     error
	getCalls     int
	directResult conversation.CreateResult
	directError  error
	directCalls  int
	directAccess conversation.AccessContext
	directEmail  string
	classResult  conversation.CreateResult
	classError   error
	classCalls   int
	classAccess  conversation.AccessContext
	classID      uuid.UUID
}

func (service *fakeConversationService) List(
	_ context.Context,
	access conversation.AccessContext,
	input conversation.ListInput,
) (conversation.Page, error) {
	service.listCalls++
	service.listAccess = access
	service.listInput = input
	return service.page, service.listError
}

func (service *fakeConversationService) Get(
	_ context.Context,
	_ conversation.AccessContext,
	_ uuid.UUID,
) (conversation.Conversation, error) {
	service.getCalls++
	return service.getResult, service.getError
}

func (service *fakeConversationService) CreateDirect(
	_ context.Context,
	access conversation.AccessContext,
	email string,
) (conversation.CreateResult, error) {
	service.directCalls++
	service.directAccess = access
	service.directEmail = email
	return service.directResult, service.directError
}

func (service *fakeConversationService) CreateClass(
	_ context.Context,
	access conversation.AccessContext,
	classID uuid.UUID,
) (conversation.CreateResult, error) {
	service.classCalls++
	service.classAccess = access
	service.classID = classID
	return service.classResult, service.classError
}

func conversationTestHandler(
	identityService identity.ServiceAPI,
	service conversation.ServiceAPI,
) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{
			Clock:    func() time.Time { return conversationTestTime },
			Identity: identityService, Conversations: service,
		},
	)
}

func conversationMutationRequest(path, body string, tenantID uuid.UUID) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set(csrfHeader, "csrf-token")
	request.Header.Set(conversationTenantHeader, tenantID.String())
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	return request
}

func assertConversationHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		!headerContainsValue(response.Header().Values("Vary"), "Cookie") ||
		!headerContainsValue(response.Header().Values("Vary"), conversationTenantHeader) {
		t.Fatalf("unexpected conversation headers: %v", response.Header())
	}
}

func assertConversationProblem(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d, want %d: %s", response.Code, status, response.Body.String())
	}
	assertConversationHeaders(t, response)
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != code {
		t.Fatalf("problem=%+v, want code %q", problem, code)
	}
}

var _ conversation.ServiceAPI = (*fakeConversationService)(nil)
