package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

const (
	mediaSpacesCollectionPath     = "/api/v1/media/spaces"
	mediaSpaceResourcePattern     = "/api/v1/media/spaces/{space_id}"
	mediaSpaceStartPattern        = "/api/v1/media/spaces/{space_id}/start"
	mediaSpaceEndPattern          = "/api/v1/media/spaces/{space_id}/end"
	mediaSpaceCancelPattern       = "/api/v1/media/spaces/{space_id}/cancel"
	mediaSpaceTenantHeader        = "X-TutorHub-Expected-Tenant-ID"
	maximumMediaSpaceRequestBytes = 16 * 1024
)

var errMediaSpaceScopeChanged = errors.New("media space active tenant changed")

type mediaSpaceHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service media.LifecycleServiceAPI
}

type createMediaSpaceRequest struct {
	Source         *createMediaSpaceSourceRequest `json:"source"`
	IdempotencyKey *string                        `json:"idempotency_key"`
}

type createMediaSpaceSourceRequest struct {
	Kind            media.SourceKind `json:"kind"`
	ClassSessionID  *uuid.UUID       `json:"class_session_id"`
	SeriesID        *uuid.UUID       `json:"series_id"`
	OccurrenceKey   *string          `json:"occurrence_key"`
	StudyMeetingID  *uuid.UUID       `json:"study_meeting_id"`
	Title           *string          `json:"title"`
	DurationMinutes *int             `json:"duration_minutes"`
	Timezone        *string          `json:"timezone"`
}

type mediaSpaceTransitionRequest struct {
	ExpectedVersion *int64  `json:"expected_version"`
	IdempotencyKey  *string `json:"idempotency_key"`
	ReasonCode      *string `json:"reason_code"`
}

type mediaProviderConvergenceProblem struct {
	Problem
	BusinessCommitted    bool                       `json:"business_committed"`
	SpaceID              uuid.UUID                  `json:"space_id"`
	ResourceStatus       media.SpaceStatus          `json:"resource_status"`
	ResourceVersion      int64                      `json:"resource_version"`
	ProviderEffectStatus media.ProviderEffectStatus `json:"provider_effect_status"`
}

func newMediaSpaceHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service media.LifecycleServiceAPI,
) mediaSpaceHandlers {
	return mediaSpaceHandlers{logger: logger, auth: auth, service: service}
}

func mediaSpaceResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", mediaSpaceTenantHeader)
		next.ServeHTTP(w, r)
	})
}

func (handlers mediaSpaceHandlers) create(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r, true)
	if !ok {
		return
	}
	var request createMediaSpaceRequest
	if err := decodeJSONRequest(w, r, &request, maximumMediaSpaceRequestBytes); err != nil ||
		request.Source == nil || request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, media.ErrInvalidSpaceRequest)
		return
	}
	source, ok := request.Source.input()
	if !ok {
		handlers.writeProblem(w, r, media.ErrInvalidSpaceRequest)
		return
	}
	result, err := handlers.service.CreateSpace(r.Context(), mediaAccess(principal), media.CreateSpaceInput{
		Source: source, IdempotencyKey: *request.IdempotencyKey,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(handlers.logger, w, status, result.Space)
}

func (handlers mediaSpaceHandlers) writeProviderConvergenceProblem(
	w http.ResponseWriter,
	r *http.Request,
	err *media.MediaProviderConvergenceError,
) {
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	writeJSONBytes(w, http.StatusServiceUnavailable, mediaProviderConvergenceProblem{
		Problem: Problem{
			Type: problemType(http.StatusServiceUnavailable),
			Code: "media_provider_unavailable", Title: "Media provider unavailable",
			Status:   http.StatusServiceUnavailable,
			Detail:   "The media space ended, but provider cleanup is still converging.",
			Instance: r.URL.Path, RequestID: RequestIDFromContext(r.Context()),
		},
		BusinessCommitted: true, SpaceID: err.Space.ID,
		ResourceStatus: err.Space.Status, ResourceVersion: err.Space.Version,
		ProviderEffectStatus: err.ProviderEffectStatus,
	})
}

func (handlers mediaSpaceHandlers) get(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r, false)
	if !ok {
		return
	}
	spaceID, ok := parseResourceUUID(r.PathValue("space_id"))
	if !ok {
		handlers.writeProblem(w, r, media.ErrInvalidSpaceRequest)
		return
	}
	space, err := handlers.service.GetSpace(r.Context(), mediaAccess(principal), spaceID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, space)
}

func (handlers mediaSpaceHandlers) start(w http.ResponseWriter, r *http.Request) {
	handlers.transition(w, r, "start")
}

func (handlers mediaSpaceHandlers) end(w http.ResponseWriter, r *http.Request) {
	handlers.transition(w, r, "end")
}

func (handlers mediaSpaceHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	handlers.transition(w, r, "cancel")
}

func (handlers mediaSpaceHandlers) transition(w http.ResponseWriter, r *http.Request, operation string) {
	principal, ok := handlers.principal(w, r, true)
	if !ok {
		return
	}
	spaceID, ok := parseResourceUUID(r.PathValue("space_id"))
	if !ok {
		handlers.writeProblem(w, r, media.ErrInvalidSpaceRequest)
		return
	}
	var request mediaSpaceTransitionRequest
	if err := decodeJSONRequest(w, r, &request, maximumMediaSpaceRequestBytes); err != nil ||
		request.ExpectedVersion == nil || request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, media.ErrInvalidSpaceRequest)
		return
	}
	input := media.TransitionInput{
		ExpectedVersion: *request.ExpectedVersion, IdempotencyKey: *request.IdempotencyKey,
	}
	if request.ReasonCode != nil {
		input.ReasonCode = *request.ReasonCode
	}
	var space media.MediaSpace
	var err error
	switch operation {
	case "start":
		space, err = handlers.service.StartSpace(r.Context(), mediaAccess(principal), spaceID, input)
	case "end":
		space, err = handlers.service.EndSpace(r.Context(), mediaAccess(principal), spaceID, input)
	case "cancel":
		space, err = handlers.service.CancelSpace(r.Context(), mediaAccess(principal), spaceID, input)
	default:
		err = media.ErrInvalidSpaceRequest
	}
	if err != nil {
		var convergence *media.MediaProviderConvergenceError
		if operation == "end" && errors.As(err, &convergence) {
			handlers.writeProviderConvergenceProblem(w, r, convergence)
			return
		}
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, space)
}

func (handlers mediaSpaceHandlers) principal(
	w http.ResponseWriter,
	r *http.Request,
	csrf bool,
) (identity.Principal, bool) {
	if !handlers.auth.available(w, r) {
		return identity.Principal{}, false
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, media.ErrLifecycleUnavailable)
		return identity.Principal{}, false
	}
	var principal identity.Principal
	var ok bool
	if csrf {
		sessionToken, sessionOK := handlers.auth.sessionToken(w, r)
		if !sessionOK {
			return identity.Principal{}, false
		}
		principal, ok = handlers.auth.csrfPrincipal(w, r, sessionToken)
	} else {
		principal, ok = handlers.auth.authenticatedPrincipal(w, r)
	}
	if !ok {
		return identity.Principal{}, false
	}
	if principal.ActiveTenant == nil || !principal.ActiveTenant.IsActive {
		writeCodedProblem(
			w, r, http.StatusForbidden, "media_space_forbidden", "Media space access denied",
			"The active workspace cannot authorize this media-space request.",
		)
		return identity.Principal{}, false
	}
	expectedTenantID, valid := parseResourceUUID(
		strings.TrimSpace(r.Header.Get(mediaSpaceTenantHeader)),
	)
	if !valid {
		handlers.writeProblem(w, r, media.ErrInvalidSpaceRequest)
		return identity.Principal{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errMediaSpaceScopeChanged)
		return identity.Principal{}, false
	}
	return principal, true
}

func (request createMediaSpaceSourceRequest) input() (media.CreateSourceInput, bool) {
	input := media.CreateSourceInput{Kind: request.Kind}
	switch request.Kind {
	case media.SourceClassSession:
		if request.ClassSessionID == nil || request.SeriesID != nil || request.OccurrenceKey != nil ||
			request.StudyMeetingID != nil || request.hasInstantFields() {
			return media.CreateSourceInput{}, false
		}
		input.ClassSessionID = *request.ClassSessionID
	case media.SourceClassSessionOccurrence:
		if request.ClassSessionID != nil || request.SeriesID == nil || request.OccurrenceKey == nil ||
			request.StudyMeetingID != nil || request.hasInstantFields() {
			return media.CreateSourceInput{}, false
		}
		input.SeriesID, input.OccurrenceKey = *request.SeriesID, *request.OccurrenceKey
	case media.SourceStudyMeeting:
		if request.ClassSessionID != nil || request.SeriesID != nil || request.OccurrenceKey != nil ||
			request.StudyMeetingID == nil || request.hasInstantFields() {
			return media.CreateSourceInput{}, false
		}
		input.StudyMeetingID = *request.StudyMeetingID
	case media.SourceInstant:
		if request.ClassSessionID != nil || request.SeriesID != nil || request.OccurrenceKey != nil ||
			request.StudyMeetingID != nil || request.Title == nil || request.DurationMinutes == nil ||
			request.Timezone == nil {
			return media.CreateSourceInput{}, false
		}
		input.Instant = media.InstantSourceInput{
			Title: *request.Title, DurationMinutes: *request.DurationMinutes, Timezone: *request.Timezone,
		}
	default:
		return media.CreateSourceInput{}, false
	}
	return input, true
}

func (request createMediaSpaceSourceRequest) hasInstantFields() bool {
	return request.Title != nil || request.DurationMinutes != nil || request.Timezone != nil
}

func (handlers mediaSpaceHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, featurecontrol.ErrQuotaExceeded) {
		var quotaError *featurecontrol.QuotaExceededError
		if errors.As(err, &quotaError) && quotaError.RetryAfter > 0 {
			seconds := int64(quotaError.RetryAfter.Round(time.Second) / time.Second)
			w.Header().Set("Retry-After", strconv.FormatInt(maxInt64(1, seconds), 10))
		}
		writeCodedProblem(
			w, r, http.StatusTooManyRequests, "quota_exceeded", "Quota exceeded",
			"The operation would exceed the effective tenant quota.",
		)
		return
	}
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusServiceUnavailable, "media_space_unavailable"
	title, detail := "Media space request failed", "The media-space request could not be completed safely."
	switch {
	case errors.Is(err, media.ErrInvalidSpaceRequest):
		status, code = http.StatusBadRequest, "media_space_invalid"
		title, detail = "Invalid media space request", "Review the source, tenant assertion, idempotency key, and optimistic version."
	case errors.Is(err, media.ErrSpaceAccessDenied):
		status, code = http.StatusForbidden, "media_space_forbidden"
		title, detail = "Media space access denied", "The active workspace cannot authorize this media-space action."
	case errors.Is(err, media.ErrSpaceNotFound), errors.Is(err, media.ErrSourceUnavailable):
		status, code = http.StatusNotFound, "media_space_not_found"
		title, detail = "Media space unavailable", "The media space or its source is unavailable in the active workspace."
	case errors.Is(err, media.ErrSpaceVersionConflict):
		status, code = http.StatusConflict, "stale_version"
		title, detail = "Media space changed", "Reload the latest media space before retrying."
	case errors.Is(err, media.ErrSpaceIdempotency):
		status, code = http.StatusConflict, "media_space_idempotency_conflict"
		title, detail = "Media space retry conflict", "Use a new idempotency key after changing the requested operation."
	case errors.Is(err, media.ErrSpaceTransition):
		status, code = http.StatusConflict, "media_space_transition_conflict"
		title, detail = "Media space transition unavailable", "Reload the media space before choosing another lifecycle action."
	case errors.Is(err, errMediaSpaceScopeChanged):
		status, code = http.StatusConflict, "media_space_scope_changed"
		title, detail = "Active workspace changed", "Reload the current workspace before retrying."
	case errors.Is(err, media.ErrMediaProviderUnavailable):
		status, code = http.StatusServiceUnavailable, "media_provider_unavailable"
		title, detail = "Media provider unavailable", "Try the media-space operation again later."
	case errors.Is(err, media.ErrLifecycleUnavailable), errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusServiceUnavailable, "media_space_unavailable"
		title, detail = "Media spaces unavailable", "Try again later."
	default:
		handlers.logger.Error(
			"media space request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
