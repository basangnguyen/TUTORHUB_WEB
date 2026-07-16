package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	defaultClassListLimit     = 50
	maximumClassListLimit     = 100
	maximumClassRequestBytes  = 32 * 1024
	classesCollectionPath     = "/api/v1/classes"
	classesResourcePathPrefix = "/api/v1/classes/"
)

type classHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	classes classroom.ServiceAPI
}

type createClassRequest struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type classResponse struct {
	ID          uuid.UUID             `json:"id"`
	OwnerUserID uuid.UUID             `json:"owner_user_id"`
	Code        string                `json:"code"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	Status      classroom.ClassStatus `json:"status"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
}

type classListResponse struct {
	Items []classResponse `json:"items"`
}

func newClassHandlers(
	logger *slog.Logger,
	auth authHandlers,
	classes classroom.ServiceAPI,
) classHandlers {
	return classHandlers{logger: logger, auth: auth, classes: classes}
}

func (handlers classHandlers) collection(w http.ResponseWriter, r *http.Request) {
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
			"The class collection supports GET and POST requests.",
		)
	}
}

func (handlers classHandlers) detail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class details support GET requests.",
		)
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	classID, ok := parseClassID(r.URL.Path)
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrClassNotFound)
		return
	}

	class, err := handlers.classes.Get(r.Context(), classAccess(principal), classID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	handlers.writeClass(w, http.StatusOK, class)
}

func (handlers classHandlers) list(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	limit, err := parseClassListLimit(r)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	classes, err := handlers.classes.List(r.Context(), classAccess(principal), limit)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	items := make([]classResponse, 0, len(classes))
	for _, class := range classes {
		items = append(items, newClassResponse(class))
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, http.StatusOK, classListResponse{Items: items})
}

func (handlers classHandlers) create(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.auth.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return
	}

	var request createClassRequest
	if err := decodeJSONRequest(w, r, &request, maximumClassRequestBytes); err != nil {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"Invalid class request",
			"Provide one JSON object with a valid code, title, and optional description.",
		)
		return
	}
	created, err := handlers.classes.Create(
		r.Context(),
		classAccess(principal),
		classroom.CreateClassInput{
			Code:        request.Code,
			Title:       request.Title,
			Description: request.Description,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	w.Header().Set("Location", classesResourcePathPrefix+created.ID.String())
	handlers.writeClass(w, http.StatusCreated, created)
}

func (handlers classHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.classes != nil {
		return true
	}
	writeProblem(
		w,
		r,
		http.StatusServiceUnavailable,
		"Classroom unavailable",
		"Classroom storage is not configured for this environment.",
	)
	return false
}

func (handlers classHandlers) writeClass(w http.ResponseWriter, status int, class classroom.Class) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, status, newClassResponse(class))
}

func (handlers classHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	title := "Classroom request failed"
	detail := "The classroom request could not be completed."

	switch {
	case errors.Is(err, classroom.ErrInvalidClassInput),
		errors.Is(err, classroom.ErrInvalidListLimit):
		status = http.StatusBadRequest
		title = "Invalid classroom request"
		detail = "Check the class code, title, description, and list limit."
	case errors.Is(err, classroom.ErrClassAccessDenied),
		errors.Is(err, classroom.ErrOwnerMembershipNeeded):
		status = http.StatusForbidden
		title = "Classroom access denied"
		detail = "Your active workspace membership does not allow this action."
	case errors.Is(err, classroom.ErrClassNotFound):
		status = http.StatusNotFound
		title = "Class not found"
		detail = "The class does not exist in the active workspace."
	case errors.Is(err, classroom.ErrDuplicateClassCode):
		status = http.StatusConflict
		title = "Class code already exists"
		detail = "Choose a different class code in this workspace."
	}

	if status >= http.StatusInternalServerError {
		handlers.logger.Error(
			"classroom request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeProblem(w, r, status, title, detail)
}

func classAccess(principal identity.Principal) classroom.AccessContext {
	access := classroom.AccessContext{
		ActorID: principal.User.ID,
	}
	if principal.ActiveTenant != nil {
		access.TenantID = principal.ActiveTenant.ID
		access.MembershipActive = true
		access.OrganizationRoles = []policy.OrganizationRole{
			policy.OrganizationRole(principal.ActiveTenant.Role),
		}
	}
	return access
}

func newClassResponse(class classroom.Class) classResponse {
	return classResponse{
		ID:          class.ID,
		OwnerUserID: class.OwnerUserID,
		Code:        class.Code,
		Title:       class.Title,
		Description: class.Description,
		Status:      class.Status,
		CreatedAt:   class.CreatedAt,
		UpdatedAt:   class.UpdatedAt,
	}
}

func parseClassID(path string) (uuid.UUID, bool) {
	value := strings.TrimPrefix(path, classesResourcePathPrefix)
	if value == path || value == "" || strings.Contains(value, "/") {
		return uuid.Nil, false
	}
	classID, err := uuid.Parse(value)
	return classID, err == nil && classID != uuid.Nil
}

func parseClassListLimit(r *http.Request) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return defaultClassListLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maximumClassListLimit {
		return 0, classroom.ErrInvalidListLimit
	}
	return limit, nil
}
