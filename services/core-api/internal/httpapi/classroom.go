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
	maximumClassCursorLength  = 512
	maximumClassRequestBytes  = 32 * 1024
	classesCollectionPath     = "/api/v1/classes"
	classesResourcePathPrefix = "/api/v1/classes/"
	classArchivePathPattern   = "/api/v1/classes/{class_id}/archive"
	classRestorePathPattern   = "/api/v1/classes/{class_id}/restore"
	classTransferPathPattern  = "/api/v1/classes/{class_id}/transfer-ownership"
	classArchiveRoute         = "archive"
	classRestoreRoute         = "restore"
	classTransferRoute        = "transfer-ownership"
)

type classHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	classes classroom.ServiceAPI
}

type createClassRequest struct {
	Code        string  `json:"code"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Timezone    *string `json:"timezone"`
}

type updateClassRequest struct {
	Code            *string                `json:"code"`
	Title           *string                `json:"title"`
	Description     *string                `json:"description"`
	Timezone        *string                `json:"timezone"`
	Status          *classroom.ClassStatus `json:"status"`
	ExpectedVersion int64                  `json:"expected_version"`
}

type classVersionRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type transferClassOwnershipRequest struct {
	ExpectedVersion int64     `json:"expected_version"`
	NewOwnerUserID  uuid.UUID `json:"new_owner_user_id"`
}

type classResponse struct {
	ID           uuid.UUID             `json:"id"`
	OwnerUserID  uuid.UUID             `json:"owner_user_id"`
	Code         string                `json:"code"`
	Title        string                `json:"title"`
	Description  string                `json:"description"`
	Timezone     string                `json:"timezone"`
	Status       classroom.ClassStatus `json:"status"`
	Version      int64                 `json:"version"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
	ArchivedAt   *time.Time            `json:"archived_at"`
	ViewerAccess classViewerAccess     `json:"viewer_access"`
}

type classViewerAccess struct {
	ClassRole            *policy.ClassRole           `json:"class_role"`
	EnrollmentStatus     *classroom.EnrollmentStatus `json:"enrollment_status"`
	CanUpdateClass       bool                        `json:"can_update_class"`
	CanArchiveClass      bool                        `json:"can_archive_class"`
	CanTransferOwnership bool                        `json:"can_transfer_ownership"`
	CanManageEnrollments bool                        `json:"can_manage_enrollments"`
	CanJoinRoom          bool                        `json:"can_join_room"`
	CanPublishMedia      bool                        `json:"can_publish_media"`
	CanLeave             bool                        `json:"can_leave"`
}

type classListResponse struct {
	Items      []classResponse `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

type classRoute struct {
	ID     uuid.UUID
	Action string
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
	route, ok := parseClassRoute(r.URL.Path)
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrClassNotFound)
		return
	}

	switch route.Action {
	case "":
		handlers.classResource(w, r, route.ID)
	case classArchiveRoute:
		handlers.classVersionMutation(w, r, route.ID, classArchiveRoute)
	case classRestoreRoute:
		handlers.classVersionMutation(w, r, route.ID, classRestoreRoute)
	case classTransferRoute:
		handlers.transferOwnership(w, r, route.ID)
	default:
		handlers.writeProblem(w, r, classroom.ErrClassNotFound)
	}
}

func (handlers classHandlers) classResource(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.get(w, r, classID)
	case http.MethodPatch:
		handlers.update(w, r, classID)
	default:
		w.Header().Set("Allow", "GET, HEAD, PATCH")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class details support GET and PATCH requests.",
		)
	}
}

func (handlers classHandlers) get(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}

	class, err := handlers.classes.Get(r.Context(), classAccess(principal), classID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	handlers.writeClass(w, http.StatusOK, class)
}

func (handlers classHandlers) update(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}

	var request updateClassRequest
	if err := decodeJSONRequest(w, r, &request, maximumClassRequestBytes); err != nil {
		handlers.writeInvalidMutationProblem(w, r)
		return
	}
	updated, err := handlers.classes.Update(
		r.Context(),
		classAccess(principal),
		classID,
		classroom.UpdateClassInput{
			Code:            request.Code,
			Title:           request.Title,
			Description:     request.Description,
			Timezone:        request.Timezone,
			Status:          request.Status,
			ExpectedVersion: request.ExpectedVersion,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	handlers.writeClass(w, http.StatusOK, updated)
}

func (handlers classHandlers) classVersionMutation(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
	action string,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class lifecycle mutations accept POST requests.",
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

	var request classVersionRequest
	if err := decodeJSONRequest(w, r, &request, maximumClassRequestBytes); err != nil {
		handlers.writeInvalidMutationProblem(w, r)
		return
	}

	var class classroom.Class
	var err error
	switch action {
	case classArchiveRoute:
		class, err = handlers.classes.Archive(
			r.Context(),
			classAccess(principal),
			classID,
			request.ExpectedVersion,
		)
	case classRestoreRoute:
		class, err = handlers.classes.Restore(
			r.Context(),
			classAccess(principal),
			classID,
			request.ExpectedVersion,
		)
	default:
		err = classroom.ErrClassNotFound
	}
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	handlers.writeClass(w, http.StatusOK, class)
}

func (handlers classHandlers) transferOwnership(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class ownership transfer accepts POST requests.",
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

	var request transferClassOwnershipRequest
	if err := decodeJSONRequest(w, r, &request, maximumClassRequestBytes); err != nil {
		handlers.writeInvalidMutationProblem(w, r)
		return
	}
	class, err := handlers.classes.TransferOwnership(
		r.Context(),
		classAccess(principal),
		classID,
		classroom.TransferClassOwnershipInput{
			NewOwnerUserID:  request.NewOwnerUserID,
			ExpectedVersion: request.ExpectedVersion,
		},
	)
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
	input, err := parseClassListInput(r)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	page, err := handlers.classes.List(r.Context(), classAccess(principal), input)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	items := make([]classResponse, 0, len(page.Items))
	for _, class := range page.Items {
		items = append(items, newClassResponse(class))
	}
	var nextCursor *string
	if page.NextCursor != "" {
		nextCursor = &page.NextCursor
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, http.StatusOK, classListResponse{
		Items:      items,
		NextCursor: nextCursor,
	})
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
			Timezone:    request.Timezone,
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

func (handlers classHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers classHandlers) writeInvalidMutationProblem(
	w http.ResponseWriter,
	r *http.Request,
) {
	writeProblem(
		w,
		r,
		http.StatusBadRequest,
		"Invalid class request",
		"Provide one JSON object with valid class fields and an expected version.",
	)
}

func (handlers classHandlers) writeClass(w http.ResponseWriter, status int, class classroom.Class) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, status, newClassResponse(class))
}

func (handlers classHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status := http.StatusInternalServerError
	title := "Classroom request failed"
	detail := "The classroom request could not be completed."

	switch {
	case errors.Is(err, classroom.ErrInvalidClassInput),
		errors.Is(err, classroom.ErrInvalidListLimit),
		errors.Is(err, classroom.ErrInvalidClassCursor):
		status = http.StatusBadRequest
		title = "Invalid classroom request"
		detail = "Check the class fields, status filter, cursor, list limit, and expected version."
	case errors.Is(err, classroom.ErrRecentAuthenticationRequired):
		status = http.StatusForbidden
		title = "Recent authentication required"
		detail = "Sign in again before transferring class ownership."
	case errors.Is(err, classroom.ErrClassAccessDenied):
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
	case errors.Is(err, classroom.ErrClassVersionConflict):
		status = http.StatusConflict
		title = "Class changed"
		detail = "Reload the latest class before trying this change again."
	case errors.Is(err, classroom.ErrInvalidClassTransition):
		status = http.StatusConflict
		title = "Class state conflict"
		detail = "The class cannot make that lifecycle transition from its current state."
	case errors.Is(err, classroom.ErrClassOwnerUnavailable):
		status = http.StatusConflict
		title = "Class owner unavailable"
		detail = "Choose another eligible active member in this workspace."
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
		ActorID:         principal.User.ID,
		AuthenticatedAt: principal.AuthenticatedAt,
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
		Timezone:    class.Timezone,
		Status:      class.Status,
		Version:     class.Version,
		CreatedAt:   class.CreatedAt,
		UpdatedAt:   class.UpdatedAt,
		ArchivedAt:  class.ArchivedAt,
		ViewerAccess: classViewerAccess{
			ClassRole:            class.ViewerAccess.ClassRole,
			EnrollmentStatus:     class.ViewerAccess.EnrollmentStatus,
			CanUpdateClass:       class.ViewerAccess.CanUpdateClass,
			CanArchiveClass:      class.ViewerAccess.CanArchiveClass,
			CanTransferOwnership: class.ViewerAccess.CanTransferOwnership,
			CanManageEnrollments: class.ViewerAccess.CanManageEnrollments,
			CanJoinRoom:          class.ViewerAccess.CanJoinRoom,
			CanPublishMedia:      class.ViewerAccess.CanPublishMedia,
			CanLeave:             class.ViewerAccess.CanLeave,
		},
	}
}

func parseClassRoute(path string) (classRoute, bool) {
	value := strings.TrimPrefix(path, classesResourcePathPrefix)
	if value == path || value == "" {
		return classRoute{}, false
	}
	parts := strings.Split(value, "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		return classRoute{}, false
	}
	classID, err := uuid.Parse(parts[0])
	if err != nil || classID == uuid.Nil {
		return classRoute{}, false
	}
	route := classRoute{ID: classID}
	if len(parts) == 2 {
		if parts[1] == "" {
			return classRoute{}, false
		}
		route.Action = parts[1]
	}
	return route, true
}

func parseClassListInput(r *http.Request) (classroom.ListClassesInput, error) {
	query := r.URL.Query()
	input := classroom.ListClassesInput{Limit: defaultClassListLimit}

	if query.Has("status") {
		value := classroom.ClassStatus(strings.TrimSpace(query.Get("status")))
		switch value {
		case classroom.ClassStatusDraft,
			classroom.ClassStatusActive,
			classroom.ClassStatusArchived:
			input.Status = &value
		default:
			return classroom.ListClassesInput{}, classroom.ErrInvalidClassInput
		}
	}

	if query.Has("limit") {
		value := strings.TrimSpace(query.Get("limit"))
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > maximumClassListLimit {
			return classroom.ListClassesInput{}, classroom.ErrInvalidListLimit
		}
		input.Limit = limit
	}

	if query.Has("cursor") {
		cursor := strings.TrimSpace(query.Get("cursor"))
		if cursor == "" || len(cursor) > maximumClassCursorLength {
			return classroom.ListClassesInput{}, classroom.ErrInvalidClassCursor
		}
		input.Cursor = cursor
	}

	return input, nil
}
