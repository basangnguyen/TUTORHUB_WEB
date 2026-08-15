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
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
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

func TestConversationHandlersCreateDirectClassAndRoomWithCSRF(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	userID := uuid.New()
	classID := uuid.New()
	directID := uuid.New()
	classConversationID := uuid.New()
	mediaSpaceID := uuid.New()
	roomConversationID := uuid.New()
	classStatus := "active"
	mediaSpaceStatus := "open"
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
		roomResult: conversation.CreateResult{
			Created: true,
			Conversation: conversation.Conversation{
				ID: roomConversationID, Kind: conversation.KindRoom,
				MediaSpaceID: &mediaSpaceID, MediaSpaceStatus: &mediaSpaceStatus,
				Title: "Study room", Participants: []conversation.Participant{},
				CreatedAt: conversationTestTime, UpdatedAt: conversationTestTime,
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

	roomRequest := conversationMutationRequest(
		"/api/v1/media/spaces/"+mediaSpaceID.String()+"/conversation",
		"",
		tenantID,
	)
	roomResponse := httptest.NewRecorder()
	handler.ServeHTTP(roomResponse, roomRequest)
	if roomResponse.Code != http.StatusCreated {
		t.Fatalf("room status=%d body=%s", roomResponse.Code, roomResponse.Body.String())
	}
	if service.roomCalls != 1 || service.mediaSpaceID != mediaSpaceID ||
		service.roomAccess.ActorID != userID {
		t.Fatalf("unexpected room invocation: %+v", service)
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

func TestConversationMessageHandlersListSendEditDeleteAndMarkRead(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	conversationID := uuid.New()
	messageID := uuid.New()
	clientMessageID := uuid.New()
	content := "Private message"
	message := conversation.Message{
		ID:              messageID,
		ConversationID:  conversationID,
		Sequence:        7,
		ClientMessageID: clientMessageID,
		Author: conversation.MessageAuthor{
			UserID: userID, DisplayName: "Teacher",
		},
		Content:   &content,
		State:     conversation.MessageStateActive,
		Version:   1,
		CreatedAt: conversationTestTime,
		UpdatedAt: conversationTestTime,
	}
	service := &fakeConversationService{
		messagePage: conversation.MessagePage{
			Items: []conversation.Message{message}, UnreadCount: 1,
		},
		messageResult: conversation.MessageMutationResult{Message: message, Created: true},
		editResult:    message,
		deleteResult:  message,
		readState: conversation.MessageReadState{
			LastReadMessageID: messageID,
			LastReadSequence:  7,
			UpdatedAt:         conversationTestTime,
		},
	}
	handler := conversationTestHandler(classIdentityService(tenantID, userID, nil), service)
	basePath := "/api/v1/conversations/" + conversationID.String()

	listRequest := httptest.NewRequest(
		http.MethodGet, basePath+"/messages?limit=25&cursor=older", nil,
	)
	addSessionCookie(listRequest)
	listRequest.Header.Set(conversationTenantHeader, tenantID.String())
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("message list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	assertConversationHeaders(t, listResponse)
	if service.messageListCalls != 1 || service.messageListConversationID != conversationID ||
		service.messageListAccess.ActorID != userID || service.messageListInput.Limit != 25 ||
		service.messageListInput.Cursor != "older" {
		t.Fatalf("unexpected message list invocation: %+v", service)
	}
	var listBody conversation.MessagePage
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode message list response: %v", err)
	}
	if len(listBody.Items) != 1 || listBody.Items[0].Content == nil ||
		*listBody.Items[0].Content != content || listBody.UnreadCount != 1 {
		t.Fatalf("unexpected message list body: %+v", listBody)
	}

	sendBody := `{"client_message_id":"` + clientMessageID.String() + `","content":"` + content + `"}`
	sendRequest := conversationAuthenticatedMutationRequest(
		http.MethodPost, basePath+"/messages", sendBody, tenantID,
	)
	sendResponse := httptest.NewRecorder()
	handler.ServeHTTP(sendResponse, sendRequest)
	if sendResponse.Code != http.StatusCreated {
		t.Fatalf("message send status=%d body=%s", sendResponse.Code, sendResponse.Body.String())
	}
	if service.messageCalls != 1 || service.messageConversationID != conversationID ||
		service.messageAccess.ActorID != userID ||
		service.messageInput.ClientMessageID != clientMessageID ||
		service.messageInput.Content != content {
		t.Fatalf("unexpected message send invocation: %+v", service)
	}

	service.messageResult.Created = false
	replayRequest := conversationAuthenticatedMutationRequest(
		http.MethodPost, basePath+"/messages", sendBody, tenantID,
	)
	replayResponse := httptest.NewRecorder()
	handler.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusOK {
		t.Fatalf("message replay status=%d body=%s", replayResponse.Code, replayResponse.Body.String())
	}

	resourcePath := basePath + "/messages/" + messageID.String()
	editRequest := conversationAuthenticatedMutationRequest(
		http.MethodPatch,
		resourcePath,
		`{"content":"Edited","expected_version":1}`,
		tenantID,
	)
	editResponse := httptest.NewRecorder()
	handler.ServeHTTP(editResponse, editRequest)
	if editResponse.Code != http.StatusOK {
		t.Fatalf("message edit status=%d body=%s", editResponse.Code, editResponse.Body.String())
	}
	if service.editConversationID != conversationID || service.editMessageID != messageID ||
		service.editAccess.ActorID != userID || service.editInput.Content != "Edited" ||
		service.editInput.ExpectedVersion != 1 {
		t.Fatalf("unexpected message edit invocation: %+v", service)
	}

	deleteRequest := conversationAuthenticatedMutationRequest(
		http.MethodDelete,
		resourcePath,
		`{"expected_version":1}`,
		tenantID,
	)
	deleteResponse := httptest.NewRecorder()
	handler.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("message delete status=%d body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if service.deleteConversationID != conversationID || service.deleteMessageID != messageID ||
		service.deleteAccess.ActorID != userID || service.deleteInput.ExpectedVersion != 1 {
		t.Fatalf("unexpected message delete invocation: %+v", service)
	}

	readRequest := conversationAuthenticatedMutationRequest(
		http.MethodPost,
		basePath+"/read",
		`{"message_id":"`+messageID.String()+`"}`,
		tenantID,
	)
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("message read status=%d body=%s", readResponse.Code, readResponse.Body.String())
	}
	if service.markReadConversationID != conversationID ||
		service.markReadMessageID != messageID || service.markReadAccess.ActorID != userID {
		t.Fatalf("unexpected mark-read invocation: %+v", service)
	}
}

func TestConversationMessageHandlersRequireCSRFAndMapConflictsAndRateLimit(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	conversationID := uuid.New()
	messageID := uuid.New()
	clientMessageID := uuid.New()
	service := &fakeConversationService{}
	handler := conversationTestHandler(
		classIdentityService(tenantID, uuid.New(), nil), service,
	)
	basePath := "/api/v1/conversations/" + conversationID.String()

	missingCSRF := httptest.NewRequest(
		http.MethodPost,
		basePath+"/messages",
		strings.NewReader(`{"client_message_id":"`+clientMessageID.String()+`","content":"secret"}`),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(conversationTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden || service.messageCalls != 0 {
		t.Fatalf("missing CSRF status=%d calls=%d body=%s",
			missingResponse.Code, service.messageCalls, missingResponse.Body.String())
	}

	service.messageError = conversation.ErrIdempotencyConflict
	conflictRequest := conversationAuthenticatedMutationRequest(
		http.MethodPost,
		basePath+"/messages",
		`{"client_message_id":"`+clientMessageID.String()+`","content":"secret"}`,
		tenantID,
	)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	assertConversationProblem(
		t, conflictResponse, http.StatusConflict, "message_idempotency_conflict",
	)
	if strings.Contains(conflictResponse.Body.String(), "secret") {
		t.Fatal("idempotency problem response exposed message content")
	}

	service.editError = conversation.ErrVersionConflict
	editRequest := conversationAuthenticatedMutationRequest(
		http.MethodPatch,
		basePath+"/messages/"+messageID.String(),
		`{"content":"secret edit","expected_version":1}`,
		tenantID,
	)
	editResponse := httptest.NewRecorder()
	handler.ServeHTTP(editResponse, editRequest)
	assertConversationProblem(t, editResponse, http.StatusConflict, "message_version_conflict")
	if strings.Contains(editResponse.Body.String(), "secret edit") {
		t.Fatal("version problem response exposed message content")
	}

	service.messageError = &featurecontrol.QuotaExceededError{
		Quota:      featurecontrol.QuotaMessageSendsPerHour,
		Limit:      60,
		Used:       60,
		RetryAfter: 3 * time.Second,
	}
	rateRequest := conversationAuthenticatedMutationRequest(
		http.MethodPost,
		basePath+"/messages",
		`{"client_message_id":"`+uuid.NewString()+`","content":"bounded"}`,
		tenantID,
	)
	rateResponse := httptest.NewRecorder()
	handler.ServeHTTP(rateResponse, rateRequest)
	assertConversationProblem(t, rateResponse, http.StatusTooManyRequests, "quota_exceeded")
	if rateResponse.Header().Get("Retry-After") != "3" {
		t.Fatalf("Retry-After=%q, want 3", rateResponse.Header().Get("Retry-After"))
	}
}

type fakeConversationService struct {
	page                      conversation.Page
	listError                 error
	listCalls                 int
	listAccess                conversation.AccessContext
	listInput                 conversation.ListInput
	getResult                 conversation.Conversation
	getError                  error
	getCalls                  int
	directResult              conversation.CreateResult
	directError               error
	directCalls               int
	directAccess              conversation.AccessContext
	directEmail               string
	classResult               conversation.CreateResult
	classError                error
	classCalls                int
	classAccess               conversation.AccessContext
	classID                   uuid.UUID
	roomResult                conversation.CreateResult
	roomError                 error
	roomCalls                 int
	roomAccess                conversation.AccessContext
	mediaSpaceID              uuid.UUID
	messagePage               conversation.MessagePage
	messagePageError          error
	messageListAccess         conversation.AccessContext
	messageListConversationID uuid.UUID
	messageListInput          conversation.MessageListInput
	messageListCalls          int
	messageResult             conversation.MessageMutationResult
	messageError              error
	messageAccess             conversation.AccessContext
	messageConversationID     uuid.UUID
	messageInput              conversation.SendMessageInput
	messageCalls              int
	editResult                conversation.Message
	editError                 error
	editAccess                conversation.AccessContext
	editConversationID        uuid.UUID
	editMessageID             uuid.UUID
	editInput                 conversation.EditMessageInput
	editCalls                 int
	deleteResult              conversation.Message
	deleteError               error
	deleteAccess              conversation.AccessContext
	deleteConversationID      uuid.UUID
	deleteMessageID           uuid.UUID
	deleteInput               conversation.DeleteMessageInput
	deleteCalls               int
	readState                 conversation.MessageReadState
	readError                 error
	markReadAccess            conversation.AccessContext
	markReadConversationID    uuid.UUID
	markReadMessageID         uuid.UUID
	markReadCalls             int
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

func (service *fakeConversationService) CreateRoom(
	_ context.Context,
	access conversation.AccessContext,
	mediaSpaceID uuid.UUID,
) (conversation.CreateResult, error) {
	service.roomCalls++
	service.roomAccess = access
	service.mediaSpaceID = mediaSpaceID
	return service.roomResult, service.roomError
}

func (service *fakeConversationService) ListMessages(
	_ context.Context,
	access conversation.AccessContext,
	conversationID uuid.UUID,
	input conversation.MessageListInput,
) (conversation.MessagePage, error) {
	service.messageListCalls++
	service.messageListAccess = access
	service.messageListConversationID = conversationID
	service.messageListInput = input
	return service.messagePage, service.messagePageError
}

func (service *fakeConversationService) SendMessage(
	_ context.Context,
	access conversation.AccessContext,
	conversationID uuid.UUID,
	input conversation.SendMessageInput,
) (conversation.MessageMutationResult, error) {
	service.messageCalls++
	service.messageAccess = access
	service.messageConversationID = conversationID
	service.messageInput = input
	return service.messageResult, service.messageError
}

func (service *fakeConversationService) EditMessage(
	_ context.Context,
	access conversation.AccessContext,
	conversationID uuid.UUID,
	messageID uuid.UUID,
	input conversation.EditMessageInput,
) (conversation.Message, error) {
	service.editCalls++
	service.editAccess = access
	service.editConversationID = conversationID
	service.editMessageID = messageID
	service.editInput = input
	return service.editResult, service.editError
}

func (service *fakeConversationService) DeleteMessage(
	_ context.Context,
	access conversation.AccessContext,
	conversationID uuid.UUID,
	messageID uuid.UUID,
	input conversation.DeleteMessageInput,
) (conversation.Message, error) {
	service.deleteCalls++
	service.deleteAccess = access
	service.deleteConversationID = conversationID
	service.deleteMessageID = messageID
	service.deleteInput = input
	return service.deleteResult, service.deleteError
}

func (service *fakeConversationService) MarkRead(
	_ context.Context,
	access conversation.AccessContext,
	conversationID uuid.UUID,
	messageID uuid.UUID,
) (conversation.MessageReadState, error) {
	service.markReadCalls++
	service.markReadAccess = access
	service.markReadConversationID = conversationID
	service.markReadMessageID = messageID
	return service.readState, service.readError
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
	return conversationAuthenticatedMutationRequest(http.MethodPost, path, body, tenantID)
}

func conversationAuthenticatedMutationRequest(
	method, path, body string,
	tenantID uuid.UUID,
) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
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
