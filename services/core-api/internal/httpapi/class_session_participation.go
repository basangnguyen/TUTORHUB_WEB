package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

const (
	classSessionAttendeesPattern           = "/api/v1/classes/{class_id}/sessions/{session_id}/attendees"
	classSessionResponsesPattern           = "/api/v1/classes/{class_id}/sessions/{session_id}/responses"
	classSessionSeriesAttendeesPattern     = "/api/v1/classes/{class_id}/session-series/{series_id}/attendees"
	classSessionSeriesResponsesPattern     = "/api/v1/classes/{class_id}/session-series/{series_id}/responses"
	classSessionOccurrenceAttendeesPattern = "/api/v1/classes/{class_id}/session-series/{series_id}/occurrences/{occurrence_key}/attendees"
	classSessionOccurrenceResponsesPattern = "/api/v1/classes/{class_id}/session-series/{series_id}/occurrences/{occurrence_key}/responses"
	classSessionOrganizerPattern           = "/api/v1/classes/{class_id}/sessions/{session_id}/organizer"
	classSessionSeriesOrganizerPattern     = "/api/v1/classes/{class_id}/session-series/{series_id}/organizer"
	maximumSessionParticipationBodySize    = 64 * 1024
)

var errSessionParticipationScopeChanged = errors.New("active workspace changed")

type classSessionParticipationHandlers struct {
	logger           *slog.Logger
	auth             authHandlers
	service          classroom.SessionParticipationServiceAPI
	sourceService    classroom.ParticipationSourceServiceAPI
	organizerService classroom.ParticipationOrganizerServiceAPI
}

type replaceSessionAudienceAttendeeRequest struct {
	UserID            uuid.UUID                   `json:"user_id"`
	ParticipationRole classroom.ParticipationRole `json:"participation_role"`
}

type replaceSessionAudienceExternalAttendeeRequest struct {
	Email             string                      `json:"email"`
	DisplayName       string                      `json:"display_name"`
	ParticipationRole classroom.ParticipationRole `json:"participation_role"`
	Locale            string                      `json:"locale"`
	ViewerTimezone    string                      `json:"viewer_timezone"`
}

type replaceSessionAudienceRequest struct {
	ExpectedAudienceRevision *int64                                          `json:"expected_audience_revision"`
	IdempotencyKey           *string                                         `json:"idempotency_key"`
	ResponseRequested        *bool                                           `json:"response_requested"`
	Attendees                *[]replaceSessionAudienceAttendeeRequest        `json:"attendees"`
	ExternalAttendees        []replaceSessionAudienceExternalAttendeeRequest `json:"external_attendees"`
}

func (request replaceSessionAudienceRequest) complete() bool {
	return request.ExpectedAudienceRevision != nil &&
		request.IdempotencyKey != nil &&
		request.ResponseRequested != nil &&
		request.Attendees != nil
}

type transferSessionOrganizerRequest struct {
	NewOrganizerUserID    *uuid.UUID `json:"new_organizer_user_id"`
	ExpectedSourceVersion *int64     `json:"expected_source_version"`
	IdempotencyKey        *string    `json:"idempotency_key"`
}

func (request transferSessionOrganizerRequest) complete() bool {
	return request.NewOrganizerUserID != nil &&
		request.ExpectedSourceVersion != nil &&
		request.IdempotencyKey != nil
}

type respondToClassSessionRequest struct {
	State                   *classroom.RSVPState `json:"state"`
	Note                    *string              `json:"note"`
	ExpectedAttendeeVersion *int64               `json:"expected_attendee_version"`
	IdempotencyKey          *string              `json:"idempotency_key"`
}

func (request respondToClassSessionRequest) complete() bool {
	return request.State != nil &&
		request.ExpectedAttendeeVersion != nil &&
		request.IdempotencyKey != nil
}

type sessionAudienceViewerAccessResponse struct {
	CanManageAttendees bool `json:"can_manage_attendees"`
	CanRespond         bool `json:"can_respond"`
	CanSeeGuestList    bool `json:"can_see_guest_list"`
}

type sessionAudienceAttendeeResponse struct {
	ID                uuid.UUID                   `json:"id"`
	UserID            uuid.UUID                   `json:"user_id"`
	ParticipationRole classroom.ParticipationRole `json:"participation_role"`
	BusinessRole      string                      `json:"business_role"`
	RSVPState         classroom.RSVPState         `json:"rsvp_state"`
	RespondedAt       *time.Time                  `json:"responded_at"`
	Version           int64                       `json:"version"`
	IsSelf            bool                        `json:"is_self"`
}

type sessionAudienceExternalAttendeeResponse struct {
	ID                uuid.UUID                   `json:"id"`
	Email             string                      `json:"email"`
	DisplayName       string                      `json:"display_name"`
	ParticipationRole classroom.ParticipationRole `json:"participation_role"`
	Locale            string                      `json:"locale"`
	ViewerTimezone    string                      `json:"viewer_timezone"`
	RSVPState         classroom.RSVPState         `json:"rsvp_state"`
	RespondedAt       *time.Time                  `json:"responded_at"`
	Version           int64                       `json:"version"`
}

type sessionAudienceResponse struct {
	AudienceRevision  int64                                     `json:"audience_revision"`
	ResponseRequested bool                                      `json:"response_requested"`
	Attendees         []sessionAudienceAttendeeResponse         `json:"attendees"`
	ExternalAttendees []sessionAudienceExternalAttendeeResponse `json:"external_attendees"`
	ViewerAccess      sessionAudienceViewerAccessResponse       `json:"viewer_access"`
}

type replaceSessionAudienceResponse struct {
	Audience sessionAudienceResponse `json:"audience"`
	Replayed bool                    `json:"replayed"`
}

type selfRSVPResponse struct {
	Attendee sessionAudienceAttendeeResponse `json:"attendee"`
	Replayed bool                            `json:"replayed"`
}

type transferSessionOrganizerResponse struct {
	Audience        sessionAudienceResponse `json:"audience"`
	OrganizerUserID uuid.UUID               `json:"organizer_user_id"`
	SourceVersion   int64                   `json:"source_version"`
	Replayed        bool                    `json:"replayed"`
}

func newClassSessionParticipationHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service classroom.SessionParticipationServiceAPI,
) classSessionParticipationHandlers {
	sourceService, _ := service.(classroom.ParticipationSourceServiceAPI)
	organizerService, _ := service.(classroom.ParticipationOrganizerServiceAPI)
	return classSessionParticipationHandlers{
		logger:           logger,
		auth:             auth,
		service:          service,
		sourceService:    sourceService,
		organizerService: organizerService,
	}
}

// attendeesHandler and responsesHandler own the same private, tenant-varying
// response boundary as the rest of calendar scheduling.
func (handlers classSessionParticipationHandlers) attendeesHandler() http.Handler {
	return calendarResponseHeaders(http.HandlerFunc(handlers.attendees))
}

func (handlers classSessionParticipationHandlers) responsesHandler() http.Handler {
	return calendarResponseHeaders(http.HandlerFunc(handlers.responses))
}

func (handlers classSessionParticipationHandlers) organizerHandler() http.Handler {
	return calendarResponseHeaders(http.HandlerFunc(handlers.organizer))
}

func (handlers classSessionParticipationHandlers) attendees(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !handlers.available(w, r) {
		return
	}
	classID, source, ok := handlers.pathSource(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.getAudience(w, r, classID, source)
	case http.MethodPut:
		handlers.replaceAudience(w, r, classID, source)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class session attendees support GET and PUT requests.",
		)
	}
}

func (handlers classSessionParticipationHandlers) organizer(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !handlers.available(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class session organizer transfer supports POST requests.",
		)
		return
	}
	if handlers.organizerService == nil {
		handlers.writeProblem(w, r, classroom.ErrSessionParticipationUnavailable)
		return
	}
	classID, source, ok := handlers.pathSource(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok || !handlers.expectedTenant(w, r, principal) {
		return
	}
	var request transferSessionOrganizerRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumSessionParticipationBodySize,
	); err != nil || !request.complete() {
		handlers.writeProblem(w, r, classroom.ErrInvalidParticipationInput)
		return
	}
	result, err := handlers.organizerService.TransferOrganizer(
		r.Context(),
		classAccess(principal),
		classID,
		source,
		classroom.TransferOrganizerInput{
			NewOrganizerUserID:    *request.NewOrganizerUserID,
			ExpectedSourceVersion: *request.ExpectedSourceVersion,
			IdempotencyKey:        *request.IdempotencyKey,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, transferSessionOrganizerResponse{
		Audience:        newSessionAudienceResponse(result.Audience),
		OrganizerUserID: result.OrganizerUserID,
		SourceVersion:   result.SourceVersion,
		Replayed:        result.Replayed,
	})
}

func (handlers classSessionParticipationHandlers) responses(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !handlers.available(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Class session responses support POST requests.",
		)
		return
	}
	classID, source, ok := handlers.pathSource(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok || !handlers.expectedTenant(w, r, principal) {
		return
	}
	var request respondToClassSessionRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumSessionParticipationBodySize,
	); err != nil || !request.complete() {
		handlers.writeProblem(w, r, classroom.ErrInvalidParticipationInput)
		return
	}
	note := ""
	if request.Note != nil {
		note = *request.Note
	}
	input := classroom.SelfRSVPInput{
		State:                   *request.State,
		Note:                    note,
		ExpectedAttendeeVersion: *request.ExpectedAttendeeVersion,
		IdempotencyKey:          *request.IdempotencyKey,
	}
	var (
		result classroom.SelfRSVPMutationResult
		err    error
	)
	if source.Kind == classroom.ParticipationSourceSession {
		result, err = handlers.service.RespondToSession(
			r.Context(),
			classAccess(principal),
			classID,
			source.SessionID,
			input,
		)
	} else if handlers.sourceService != nil {
		result, err = handlers.sourceService.Respond(
			r.Context(),
			classAccess(principal),
			classID,
			source,
			input,
		)
	} else {
		err = classroom.ErrSessionParticipationUnavailable
	}
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, selfRSVPResponse{
		Attendee: newSessionAudienceAttendeeResponse(result.Attendee),
		Replayed: result.Replayed,
	})
}

func (handlers classSessionParticipationHandlers) getAudience(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
	source classroom.ParticipationSourceRef,
) {
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok || !handlers.expectedTenant(w, r, principal) {
		return
	}
	var (
		audience classroom.SessionAudience
		err      error
	)
	if source.Kind == classroom.ParticipationSourceSession {
		audience, err = handlers.service.GetSessionAudience(
			r.Context(),
			classAccess(principal),
			classID,
			source.SessionID,
		)
	} else if handlers.sourceService != nil {
		audience, err = handlers.sourceService.GetAudience(
			r.Context(),
			classAccess(principal),
			classID,
			source,
		)
	} else {
		err = classroom.ErrSessionParticipationUnavailable
	}
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, newSessionAudienceResponse(audience))
}

func (handlers classSessionParticipationHandlers) replaceAudience(
	w http.ResponseWriter,
	r *http.Request,
	classID uuid.UUID,
	source classroom.ParticipationSourceRef,
) {
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok || !handlers.expectedTenant(w, r, principal) {
		return
	}
	var request replaceSessionAudienceRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumSessionParticipationBodySize,
	); err != nil || !request.complete() {
		handlers.writeProblem(w, r, classroom.ErrInvalidParticipationInput)
		return
	}
	attendees := make(
		[]classroom.InternalAudienceAttendeeInput,
		0,
		len(*request.Attendees),
	)
	for _, attendee := range *request.Attendees {
		attendees = append(attendees, classroom.InternalAudienceAttendeeInput{
			UserID:            attendee.UserID,
			ParticipationRole: attendee.ParticipationRole,
		})
	}
	externalAttendees := make(
		[]classroom.ExternalAudienceAttendeeInput,
		0,
		len(request.ExternalAttendees),
	)
	for _, attendee := range request.ExternalAttendees {
		externalAttendees = append(externalAttendees, classroom.ExternalAudienceAttendeeInput{
			Email:             attendee.Email,
			DisplayName:       attendee.DisplayName,
			ParticipationRole: attendee.ParticipationRole,
			Locale:            attendee.Locale,
			ViewerTimezone:    attendee.ViewerTimezone,
		})
	}
	input := classroom.ReplaceAudienceInput{
		ExpectedAudienceRevision: *request.ExpectedAudienceRevision,
		IdempotencyKey:           *request.IdempotencyKey,
		ResponseRequested:        *request.ResponseRequested,
		Attendees:                attendees,
		ExternalAttendees:        externalAttendees,
	}
	var (
		result classroom.SessionAudienceMutationResult
		err    error
	)
	if source.Kind == classroom.ParticipationSourceSession {
		result, err = handlers.service.ReplaceSessionAudience(
			r.Context(),
			classAccess(principal),
			classID,
			source.SessionID,
			input,
		)
	} else if handlers.sourceService != nil {
		result, err = handlers.sourceService.ReplaceAudience(
			r.Context(),
			classAccess(principal),
			classID,
			source,
			input,
		)
	} else {
		err = classroom.ErrSessionParticipationUnavailable
	}
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, replaceSessionAudienceResponse{
		Audience: newSessionAudienceResponse(result.Audience),
		Replayed: result.Replayed,
	})
}

func (handlers classSessionParticipationHandlers) pathSource(
	w http.ResponseWriter,
	r *http.Request,
) (uuid.UUID, classroom.ParticipationSourceRef, bool) {
	classID, classOK := parseResourceUUID(r.PathValue("class_id"))
	if !classOK {
		handlers.writeProblem(w, r, classroom.ErrSessionParticipationNotFound)
		return uuid.Nil, classroom.ParticipationSourceRef{}, false
	}

	if rawSessionID := r.PathValue("session_id"); rawSessionID != "" {
		sessionID, ok := parseResourceUUID(rawSessionID)
		if ok {
			return classID, classroom.SessionParticipationSource(sessionID), true
		}
		handlers.writeProblem(w, r, classroom.ErrSessionParticipationNotFound)
		return uuid.Nil, classroom.ParticipationSourceRef{}, false
	}

	seriesID, seriesOK := parseResourceUUID(r.PathValue("series_id"))
	if !seriesOK {
		handlers.writeProblem(w, r, classroom.ErrSessionParticipationNotFound)
		return uuid.Nil, classroom.ParticipationSourceRef{}, false
	}
	if occurrenceKey := r.PathValue("occurrence_key"); occurrenceKey != "" {
		// PathValue is already URL-decoded by net/http. Keep this value opaque;
		// source normalization and validation belong to the classroom domain.
		return classID,
			classroom.OccurrenceParticipationSource(seriesID, occurrenceKey),
			true
	}
	return classID, classroom.SeriesParticipationSource(seriesID), true
}

func (handlers classSessionParticipationHandlers) available(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.service != nil {
		return true
	}
	handlers.writeProblem(w, r, classroom.ErrSessionParticipationUnavailable)
	return false
}

func (handlers classSessionParticipationHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers classSessionParticipationHandlers) expectedTenant(
	w http.ResponseWriter,
	r *http.Request,
	principal identity.Principal,
) bool {
	if principal.ActiveTenant == nil {
		handlers.writeProblem(w, r, classroom.ErrSessionParticipationAccessDenied)
		return false
	}
	expectedTenantID, ok := parseResourceUUID(
		strings.TrimSpace(r.Header.Get(calendarTenantHeader)),
	)
	if !ok {
		handlers.writeProblem(w, r, classroom.ErrInvalidParticipationInput)
		return false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errSessionParticipationScopeChanged)
		return false
	}
	return true
}

func (handlers classSessionParticipationHandlers) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusInternalServerError, "class_session_participation_failed"
	title := "Class session participation failed"
	detail := "The class session participation request could not be completed safely."
	switch {
	case errors.Is(err, classroom.ErrInvalidParticipationInput):
		status, code = http.StatusBadRequest, "class_session_participation_invalid"
		title = "Invalid class session participation request"
		detail = "Review the attendee list, response, expected version, and idempotency key."
	case errors.Is(err, classroom.ErrSessionParticipationAccessDenied),
		errors.Is(err, classroom.ErrClassAccessDenied):
		status, code = http.StatusForbidden, "class_session_participation_forbidden"
		title = "Class session participation access denied"
		detail = "Your active workspace membership does not allow this participation action."
	case errors.Is(err, classroom.ErrSessionParticipationNotFound),
		errors.Is(err, classroom.ErrClassNotFound):
		status, code = http.StatusNotFound, "class_session_participation_not_found"
		title = "Class session participation not found"
		detail = "The class session or attendee does not exist in the active workspace."
	case errors.Is(err, classroom.ErrSessionAudienceVersionConflict):
		status, code = http.StatusConflict, "class_session_audience_conflict"
		title = "Class session audience changed"
		detail = "Reload the latest attendee list before saving this change."
	case errors.Is(err, classroom.ErrSessionOrganizerUnavailable):
		status, code = http.StatusConflict, "class_session_organizer_unavailable"
		title = "Class session organizer unavailable"
		detail = "Select an active teacher, co-teacher, administrator, or class owner before retrying."
	case errors.Is(err, classroom.ErrSessionAttendeeVersionConflict):
		status, code = http.StatusConflict, "class_session_rsvp_conflict"
		title = "Class session response changed"
		detail = "Reload your latest response state before trying again."
	case errors.Is(err, classroom.ErrSessionParticipationIdempotencyConflict):
		status, code = http.StatusConflict, "class_session_participation_idempotency_conflict"
		title = "Mutation key already used"
		detail = "Use a new idempotency key for a different participation request."
	case errors.Is(err, classroom.ErrSessionRSVPUnavailable):
		status, code = http.StatusConflict, "class_session_rsvp_unavailable"
		title = "Class session response unavailable"
		detail = "This session is not currently accepting responses."
	case errors.Is(err, classroom.ErrInvalidSessionTransition):
		status, code = http.StatusConflict, "class_session_participation_state_conflict"
		title = "Class session participation state conflict"
		detail = "The class session cannot accept that participation change in its current state."
	case errors.Is(err, errSessionParticipationScopeChanged):
		status, code = http.StatusConflict, "calendar_scope_changed"
		title = "Active workspace changed"
		detail = "Reload the current session before retrying the calendar request."
	case errors.Is(err, classroom.ErrSessionParticipationUnavailable),
		errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusServiceUnavailable, "class_session_participation_unavailable"
		title = "Class session participation unavailable"
		detail = "Class session participation is temporarily unavailable. Try again later."
	default:
		handlers.logger.Error(
			"class session participation request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}

func newSessionAudienceResponse(
	audience classroom.SessionAudience,
) sessionAudienceResponse {
	attendees := make([]sessionAudienceAttendeeResponse, 0, len(audience.Attendees))
	for _, attendee := range audience.Attendees {
		attendees = append(attendees, newSessionAudienceAttendeeResponse(attendee))
	}
	externalAttendees := make(
		[]sessionAudienceExternalAttendeeResponse,
		0,
		len(audience.ExternalAttendees),
	)
	for _, attendee := range audience.ExternalAttendees {
		externalAttendees = append(
			externalAttendees,
			newSessionAudienceExternalAttendeeResponse(attendee),
		)
	}
	return sessionAudienceResponse{
		AudienceRevision:  audience.AudienceRevision,
		ResponseRequested: audience.ResponseRequested,
		Attendees:         attendees,
		ExternalAttendees: externalAttendees,
		ViewerAccess: sessionAudienceViewerAccessResponse{
			CanManageAttendees: audience.ViewerAccess.CanManageAttendees,
			CanRespond:         audience.ViewerAccess.CanRespond,
			CanSeeGuestList:    audience.ViewerAccess.CanSeeGuestList,
		},
	}
}

func newSessionAudienceExternalAttendeeResponse(
	attendee classroom.SessionAudienceExternalAttendee,
) sessionAudienceExternalAttendeeResponse {
	var respondedAt *time.Time
	if attendee.RespondedAt != nil {
		value := attendee.RespondedAt.UTC()
		respondedAt = &value
	}
	return sessionAudienceExternalAttendeeResponse{
		ID:                attendee.ID,
		Email:             attendee.Email,
		DisplayName:       attendee.DisplayName,
		ParticipationRole: attendee.ParticipationRole,
		Locale:            attendee.Locale,
		ViewerTimezone:    attendee.ViewerTimezone,
		RSVPState:         attendee.RSVPState,
		RespondedAt:       respondedAt,
		Version:           attendee.Version,
	}
}

func newSessionAudienceAttendeeResponse(
	attendee classroom.SessionAudienceAttendee,
) sessionAudienceAttendeeResponse {
	var respondedAt *time.Time
	if attendee.RespondedAt != nil {
		value := attendee.RespondedAt.UTC()
		respondedAt = &value
	}
	return sessionAudienceAttendeeResponse{
		ID:                attendee.ID,
		UserID:            attendee.UserID,
		ParticipationRole: attendee.ParticipationRole,
		BusinessRole:      attendee.BusinessRole,
		RSVPState:         attendee.RSVPState,
		RespondedAt:       respondedAt,
		Version:           attendee.Version,
		IsSelf:            attendee.IsSelf,
	}
}
