package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

const (
	classSessionsCollectionPattern = "/api/v1/classes/{class_id}/sessions"
	classSessionResourcePattern    = "/api/v1/classes/{class_id}/sessions/{session_id}"
	classSessionCancelPattern      = "/api/v1/classes/{class_id}/sessions/{session_id}/cancel"
	classSessionsPathPrefix        = "/api/v1/classes/"
	maximumClassSessionRequestSize = 32 * 1024
)

type classSessionHandlers struct {
	logger   *slog.Logger
	auth     authHandlers
	sessions classroom.SessionServiceAPI
}

type createClassSessionRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	StartsAt    string `json:"starts_at"`
	EndsAt      string `json:"ends_at"`
	Timezone    string `json:"timezone"`
}

type updateClassSessionRequest struct {
	Title           *string `json:"title"`
	Description     *string `json:"description"`
	StartsAt        *string `json:"starts_at"`
	EndsAt          *string `json:"ends_at"`
	Timezone        *string `json:"timezone"`
	ExpectedVersion int64   `json:"expected_version"`
}

type cancelClassSessionRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type classSessionViewerAccessResponse struct {
	CanUpdate bool `json:"can_update"`
	CanCancel bool `json:"can_cancel"`
}

type classSessionResponse struct {
	ID           uuid.UUID                        `json:"id"`
	ClassID      uuid.UUID                        `json:"class_id"`
	Title        string                           `json:"title"`
	Description  string                           `json:"description"`
	StartsAt     time.Time                        `json:"starts_at"`
	EndsAt       time.Time                        `json:"ends_at"`
	Timezone     string                           `json:"timezone"`
	Status       classroom.SessionStatus          `json:"status"`
	Version      int64                            `json:"version"`
	CreatedBy    uuid.UUID                        `json:"created_by"`
	UpdatedBy    uuid.UUID                        `json:"updated_by"`
	CancelledAt  *time.Time                       `json:"cancelled_at"`
	CancelledBy  *uuid.UUID                       `json:"cancelled_by"`
	CreatedAt    time.Time                        `json:"created_at"`
	UpdatedAt    time.Time                        `json:"updated_at"`
	ViewerAccess classSessionViewerAccessResponse `json:"viewer_access"`
}

type classSessionListResponse struct {
	Items      []classSessionResponse `json:"items"`
	NextCursor *string                `json:"next_cursor"`
}

func classSessionResourceAuditMutation(r *http.Request) (audit.Draft, bool) {
	if r.Method != http.MethodPatch {
		return audit.Draft{}, false
	}
	sessionID, ok := parseResourceUUID(r.PathValue("session_id"))
	if !ok {
		return audit.Draft{}, false
	}
	return audit.Draft{
		Action:       audit.ActionClassSessionUpdate,
		ResourceType: "class_session",
		ResourceID:   sessionID,
	}, true
}

func newClassSessionHandlers(
	logger *slog.Logger,
	auth authHandlers,
	sessions classroom.SessionServiceAPI,
) classSessionHandlers {
	return classSessionHandlers{logger: logger, auth: auth, sessions: sessions}
}

func (handlers classSessionHandlers) collection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.list(w, r)
	case http.MethodPost:
		handlers.create(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class session collections support GET and POST requests.",
		)
	}
}

func (handlers classSessionHandlers) resource(w http.ResponseWriter, r *http.Request) {
	classID, sessionID, ok := handlers.pathIDs(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.get(w, r, classID, sessionID)
	case http.MethodPatch:
		handlers.update(w, r, classID, sessionID)
	default:
		w.Header().Set("Allow", "GET, HEAD, PATCH")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class session details support GET and PATCH requests.",
		)
	}
}

func (handlers classSessionHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	classID, sessionID, ok := handlers.pathIDs(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class session cancellation accepts POST requests.",
		)
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	var request cancelClassSessionRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumClassSessionRequestSize,
	); err != nil {
		handlers.writeProblem(w, r, classroom.ErrInvalidSessionInput)
		return
	}
	session, err := handlers.sessions.CancelSession(
		r.Context(),
		classAccess(principal),
		classID,
		sessionID,
		request.ExpectedVersion,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	handlers.writeSession(w, http.StatusOK, session)
}

func (handlers classSessionHandlers) list(w http.ResponseWriter, r *http.Request) {
	classID, ok := parseResourceUUID(r.PathValue("class_id"))
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrClassNotFound)
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	from, to := strings.TrimSpace(query.Get("range_start")), strings.TrimSpace(query.Get("range_end"))
	if from == "" || to == "" {
		handlers.writeProblem(w, r, classroom.ErrInvalidSessionRange)
		return
	}
	limit := 0
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			handlers.writeProblem(w, r, classroom.ErrInvalidSessionListLimit)
			return
		}
		limit = parsed
	}
	page, err := handlers.sessions.ListSessions(
		r.Context(),
		classAccess(principal),
		classID,
		classroom.ListSessionsInput{
			From: from, To: to, Limit: limit, Cursor: strings.TrimSpace(query.Get("cursor")),
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	items := make([]classSessionResponse, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, newClassSessionResponse(item))
	}
	var nextCursor *string
	if page.NextCursor != "" {
		value := page.NextCursor
		nextCursor = &value
	}
	handlers.writeSessionList(w, http.StatusOK, classSessionListResponse{
		Items: items, NextCursor: nextCursor,
	})
}

func (handlers classSessionHandlers) create(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	classID, ok := parseResourceUUID(r.PathValue("class_id"))
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrClassNotFound)
		return
	}
	var request createClassSessionRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumClassSessionRequestSize,
	); err != nil {
		handlers.writeProblem(w, r, classroom.ErrInvalidSessionInput)
		return
	}
	session, err := handlers.sessions.CreateSession(
		r.Context(),
		classAccess(principal),
		classID,
		classroom.CreateSessionInput{
			Title: request.Title, Description: request.Description,
			StartsAt: request.StartsAt, EndsAt: request.EndsAt, Timezone: request.Timezone,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	w.Header().Set(
		"Location",
		classSessionsPathPrefix+classID.String()+"/sessions/"+session.ID.String(),
	)
	handlers.writeSession(w, http.StatusCreated, session)
}

func (handlers classSessionHandlers) get(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
	sessionID uuid.UUID,
) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	session, err := handlers.sessions.GetSession(
		r.Context(), classAccess(principal), classID, sessionID,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	handlers.writeSession(w, http.StatusOK, session)
}

func (handlers classSessionHandlers) update(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
	sessionID uuid.UUID,
) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	var request updateClassSessionRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumClassSessionRequestSize,
	); err != nil {
		handlers.writeProblem(w, r, classroom.ErrInvalidSessionInput)
		return
	}
	session, err := handlers.sessions.UpdateSession(
		r.Context(),
		classAccess(principal),
		classID,
		sessionID,
		classroom.UpdateSessionInput{
			Title: request.Title, Description: request.Description,
			StartsAt: request.StartsAt, EndsAt: request.EndsAt,
			Timezone: request.Timezone, ExpectedVersion: request.ExpectedVersion,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	handlers.writeSession(w, http.StatusOK, session)
}

func (handlers classSessionHandlers) pathIDs(
	w http.ResponseWriter,
	r *http.Request,
) (uuid.UUID, uuid.UUID, bool) {
	classID, classOK := parseResourceUUID(r.PathValue("class_id"))
	sessionID, sessionOK := parseResourceUUID(r.PathValue("session_id"))
	if classOK && sessionOK {
		return classID, sessionID, true
	}
	handlers.writeProblem(w, r, classroom.ErrSessionNotFound)
	return uuid.Nil, uuid.Nil, false
}

func (handlers classSessionHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.sessions != nil {
		return true
	}
	writeProblem(
		w,
		r,
		http.StatusServiceUnavailable,
		"Class sessions unavailable",
		"Class session storage is not configured for this environment.",
	)
	return false
}

func (handlers classSessionHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers classSessionHandlers) writeSession(
	w http.ResponseWriter,
	status int,
	session classroom.ClassSession,
) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, status, newClassSessionResponse(session))
}

func (handlers classSessionHandlers) writeSessionList(
	w http.ResponseWriter,
	status int,
	response classSessionListResponse,
) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, status, response)
}

func (handlers classSessionHandlers) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code, title, detail := http.StatusInternalServerError,
		"class_session_failed", "Class session request failed",
		"The class session request could not be completed."
	switch {
	case errors.Is(err, classroom.ErrInvalidSessionInput),
		errors.Is(err, classroom.ErrInvalidSessionTimezone),
		errors.Is(err, classroom.ErrSessionDSTGap),
		errors.Is(err, classroom.ErrSessionTimezoneOffsetMismatch),
		errors.Is(err, classroom.ErrInvalidSessionRange),
		errors.Is(err, classroom.ErrInvalidSessionListLimit),
		errors.Is(err, classroom.ErrInvalidSessionCursor):
		status, code, title, detail = http.StatusBadRequest,
			"class_session_invalid", "Invalid class session request",
			"Check the title, RFC 3339 timestamps, IANA timezone, range, cursor, and expected version."
	case errors.Is(err, classroom.ErrSessionAccessDenied),
		errors.Is(err, classroom.ErrClassAccessDenied):
		status, code, title, detail = http.StatusForbidden,
			"class_session_forbidden", "Class session access denied",
			"Your active workspace membership does not allow this class session action."
	case errors.Is(err, classroom.ErrSessionNotFound),
		errors.Is(err, classroom.ErrClassNotFound):
		status, code, title, detail = http.StatusNotFound,
			"class_session_not_found", "Class session not found",
			"The class session does not exist in the active workspace."
	case errors.Is(err, classroom.ErrSessionVersionConflict):
		status, code, title, detail = http.StatusConflict,
			"class_session_conflict", "Class session changed",
			"Reload the latest session before trying this change again."
	case errors.Is(err, classroom.ErrInvalidSessionTransition):
		status, code, title, detail = http.StatusConflict,
			"class_session_state_conflict", "Class session state conflict",
			"The class session cannot make that lifecycle transition."
	}
	if status >= http.StatusInternalServerError {
		handlers.logger.Error(
			"class session request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}

func newClassSessionResponse(session classroom.ClassSession) classSessionResponse {
	return classSessionResponse{
		ID: session.ID, ClassID: session.ClassID, Title: session.Title,
		Description: session.Description, StartsAt: session.StartsAt.UTC(),
		EndsAt: session.EndsAt.UTC(), Timezone: session.Timezone, Status: session.Status,
		Version: session.Version, CreatedBy: session.CreatedBy, UpdatedBy: session.UpdatedBy,
		CancelledAt: session.CancelledAt, CancelledBy: session.CancelledBy,
		CreatedAt: session.CreatedAt, UpdatedAt: session.UpdatedAt,
		ViewerAccess: classSessionViewerAccessResponse{
			CanUpdate: session.ViewerAccess.CanUpdate,
			CanCancel: session.ViewerAccess.CanCancel,
		},
	}
}
