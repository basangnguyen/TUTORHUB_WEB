package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/tutorhub-v2/core-api/internal/modules/conversation"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	conversationsCollectionPath = "/api/v1/conversations"
	conversationDirectPath      = "/api/v1/conversations/direct"
	conversationResourcePattern = "/api/v1/conversations/{conversation_id}"
	conversationMessagesPattern = "/api/v1/conversations/{conversation_id}/messages"
	conversationMessagePattern  = "/api/v1/conversations/{conversation_id}/messages/{message_id}"
	conversationReadPattern     = "/api/v1/conversations/{conversation_id}/read"
	classConversationPattern    = "/api/v1/classes/{class_id}/conversation"
	conversationTenantHeader    = "X-TutorHub-Expected-Tenant-ID"
	maximumConversationBodySize = 16 * 1024
	maximumMessageBodySize      = 64 * 1024
)

var errConversationScopeChanged = errors.New("conversation active tenant changed")

type conversationHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service conversation.ServiceAPI
}

type createDirectConversationRequest struct {
	TargetMemberEmail *string `json:"target_member_email"`
}

type sendMessageRequest struct {
	ClientMessageID *string `json:"client_message_id"`
	Content         *string `json:"content"`
}

type editMessageRequest struct {
	Content         *string `json:"content"`
	ExpectedVersion *int64  `json:"expected_version"`
}

type deleteMessageRequest struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

type markMessagesReadRequest struct {
	MessageID *string `json:"message_id"`
}

func newConversationHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service conversation.ServiceAPI,
) conversationHandlers {
	return conversationHandlers{logger: logger, auth: auth, service: service}
}

func conversationResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", conversationTenantHeader)
		next.ServeHTTP(w, r)
	})
}

func (handlers conversationHandlers) collection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "The conversation collection supports GET requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	input, err := parseConversationListInput(r)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	page, err := handlers.service.List(r.Context(), access, input)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, page)
}

func (handlers conversationHandlers) resource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Conversation details support GET requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	conversationID, ok := parseResourceUUID(r.PathValue("conversation_id"))
	if !ok {
		handlers.writeProblem(w, r, conversation.ErrNotFound)
		return
	}
	item, err := handlers.service.Get(r.Context(), access, conversationID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, item)
}

func (handlers conversationHandlers) createDirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Direct conversation creation supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	var request createDirectConversationRequest
	if err := decodeJSONRequest(
		w, r, &request, maximumConversationBodySize,
	); err != nil || request.TargetMemberEmail == nil {
		handlers.writeProblem(w, r, conversation.ErrInvalidInput)
		return
	}
	result, err := handlers.service.CreateDirect(
		r.Context(), access, *request.TargetMemberEmail,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(handlers.logger, w, status, result.Conversation)
}

func (handlers conversationHandlers) createClass(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Class conversation creation supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	classID, ok := parseResourceUUID(r.PathValue("class_id"))
	if !ok {
		handlers.writeProblem(w, r, conversation.ErrNotFound)
		return
	}
	if r.ContentLength != 0 {
		var request struct{}
		if err := decodeJSONRequest(
			w, r, &request, maximumConversationBodySize,
		); err != nil {
			handlers.writeProblem(w, r, conversation.ErrInvalidInput)
			return
		}
	}
	result, err := handlers.service.CreateClass(r.Context(), access, classID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(handlers.logger, w, status, result.Conversation)
}

func (handlers conversationHandlers) messages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Conversation messages support GET, HEAD, and POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	var principal identity.Principal
	var ok bool
	if r.Method == http.MethodPost {
		principal, ok = handlers.csrfPrincipal(w, r)
	} else {
		principal, ok = handlers.auth.authenticatedPrincipal(w, r)
	}
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	conversationID, ok := parseResourceUUID(r.PathValue("conversation_id"))
	if !ok {
		handlers.writeProblem(w, r, conversation.ErrNotFound)
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		input, err := parseMessageListInput(r)
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		page, err := handlers.service.ListMessages(
			r.Context(), access, conversationID, input,
		)
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, page)
		return
	}
	var request sendMessageRequest
	if err := decodeJSONRequest(w, r, &request, maximumMessageBodySize); err != nil ||
		request.ClientMessageID == nil || request.Content == nil {
		handlers.writeProblem(w, r, conversation.ErrInvalidInput)
		return
	}
	clientMessageID, ok := parseResourceUUID(*request.ClientMessageID)
	if !ok {
		handlers.writeProblem(w, r, conversation.ErrInvalidInput)
		return
	}
	result, err := handlers.service.SendMessage(
		r.Context(),
		access,
		conversationID,
		conversation.SendMessageInput{
			ClientMessageID: clientMessageID,
			Content:         *request.Content,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(handlers.logger, w, status, result.Message)
}

func (handlers conversationHandlers) message(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "PATCH, DELETE")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Message lifecycle supports PATCH and DELETE requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	conversationID, conversationOK := parseResourceUUID(r.PathValue("conversation_id"))
	messageID, messageOK := parseResourceUUID(r.PathValue("message_id"))
	if !conversationOK || !messageOK {
		handlers.writeProblem(w, r, conversation.ErrNotFound)
		return
	}
	if r.Method == http.MethodPatch {
		var request editMessageRequest
		if err := decodeJSONRequest(w, r, &request, maximumMessageBodySize); err != nil ||
			request.Content == nil || request.ExpectedVersion == nil {
			handlers.writeProblem(w, r, conversation.ErrInvalidInput)
			return
		}
		item, err := handlers.service.EditMessage(
			r.Context(),
			access,
			conversationID,
			messageID,
			conversation.EditMessageInput{
				Content:         *request.Content,
				ExpectedVersion: *request.ExpectedVersion,
			},
		)
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, item)
		return
	}
	var request deleteMessageRequest
	if err := decodeJSONRequest(w, r, &request, maximumMessageBodySize); err != nil ||
		request.ExpectedVersion == nil {
		handlers.writeProblem(w, r, conversation.ErrInvalidInput)
		return
	}
	item, err := handlers.service.DeleteMessage(
		r.Context(),
		access,
		conversationID,
		messageID,
		conversation.DeleteMessageInput{ExpectedVersion: *request.ExpectedVersion},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, item)
}

func (handlers conversationHandlers) markRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Conversation read state supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	conversationID, ok := parseResourceUUID(r.PathValue("conversation_id"))
	if !ok {
		handlers.writeProblem(w, r, conversation.ErrNotFound)
		return
	}
	var request markMessagesReadRequest
	if err := decodeJSONRequest(w, r, &request, maximumMessageBodySize); err != nil ||
		request.MessageID == nil {
		handlers.writeProblem(w, r, conversation.ErrInvalidInput)
		return
	}
	messageID, ok := parseResourceUUID(*request.MessageID)
	if !ok {
		handlers.writeProblem(w, r, conversation.ErrInvalidInput)
		return
	}
	state, err := handlers.service.MarkRead(
		r.Context(), access, conversationID, messageID,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, state)
}

func parseConversationListInput(r *http.Request) (conversation.ListInput, error) {
	query := r.URL.Query()
	input := conversation.ListInput{Cursor: strings.TrimSpace(query.Get("cursor"))}
	if raw := strings.TrimSpace(query.Get("kind")); raw != "" {
		kind := conversation.Kind(raw)
		input.Kind = &kind
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return conversation.ListInput{}, conversation.ErrInvalidInput
		}
		input.Limit = limit
	}
	return input, nil
}

func parseMessageListInput(r *http.Request) (conversation.MessageListInput, error) {
	query := r.URL.Query()
	input := conversation.MessageListInput{Cursor: strings.TrimSpace(query.Get("cursor"))}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return conversation.MessageListInput{}, conversation.ErrInvalidInput
		}
		input.Limit = limit
	}
	return input, nil
}

func (handlers conversationHandlers) access(
	w http.ResponseWriter,
	r *http.Request,
	principal identity.Principal,
) (conversation.AccessContext, bool) {
	if principal.ActiveTenant == nil {
		handlers.writeProblem(w, r, conversation.ErrAccessDenied)
		return conversation.AccessContext{}, false
	}
	expectedTenantID, ok := parseResourceUUID(
		strings.TrimSpace(r.Header.Get(conversationTenantHeader)),
	)
	if !ok {
		handlers.writeProblem(w, r, conversation.ErrInvalidInput)
		return conversation.AccessContext{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errConversationScopeChanged)
		return conversation.AccessContext{}, false
	}
	return conversation.AccessContext{
		TenantID:          principal.ActiveTenant.ID,
		ActorID:           principal.User.ID,
		MembershipActive:  principal.ActiveTenant.IsActive,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRole(principal.ActiveTenant.Role)},
	}, true
}

func (handlers conversationHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers conversationHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.service == nil {
		writeCodedProblem(w, r, http.StatusServiceUnavailable, "conversation_unavailable", "Conversations unavailable", "Conversations are not configured for this environment.")
		return false
	}
	return true
}

func (handlers conversationHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusServiceUnavailable, "conversation_unavailable"
	title, detail := "Conversation request failed", "The conversation request could not be completed safely."
	switch {
	case errors.Is(err, conversation.ErrInvalidInput):
		status, code = http.StatusBadRequest, "conversation_invalid"
		title, detail = "Invalid conversation request", "Review the conversation kind, pagination, or target member."
	case errors.Is(err, conversation.ErrAccessDenied):
		status, code = http.StatusForbidden, "conversation_forbidden"
		title, detail = "Conversation access denied", "The active workspace cannot authorize this conversation request."
	case errors.Is(err, conversation.ErrNotFound), errors.Is(err, conversation.ErrUnavailable):
		status, code = http.StatusNotFound, "conversation_not_found"
		title, detail = "Conversation unavailable", "The conversation or member is unavailable in the active workspace."
	case errors.Is(err, conversation.ErrReadOnly):
		status, code = http.StatusConflict, "conversation_read_only"
		title, detail = "Conversation is read only", "Archived classes keep conversation history but cannot create or receive new conversation content."
	case errors.Is(err, conversation.ErrIdempotencyConflict):
		status, code = http.StatusConflict, "message_idempotency_conflict"
		title, detail = "Message retry conflict", "Use a new client message identifier after changing the conversation or message content."
	case errors.Is(err, conversation.ErrVersionConflict):
		status, code = http.StatusConflict, "message_version_conflict"
		title, detail = "Message changed", "Reload the latest message state before retrying this change."
	case errors.Is(err, errConversationScopeChanged):
		status, code = http.StatusConflict, "conversation_scope_changed"
		title, detail = "Active workspace changed", "Reload the current session before retrying the conversation request."
	default:
		handlers.logger.Error(
			"conversation request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
