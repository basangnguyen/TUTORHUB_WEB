package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

const (
	mediaSpaceLockPattern              = "/api/v1/media/spaces/{space_id}/lock"
	mediaParticipantRolePattern        = "/api/v1/media/spaces/{space_id}/participants/{participant_key}/role"
	mediaParticipantMutePattern        = "/api/v1/media/spaces/{space_id}/participants/{participant_key}/mute"
	mediaParticipantRemovePattern      = "/api/v1/media/spaces/{space_id}/participants/{participant_key}/remove"
	maximumMediaModerationRequestBytes = 8 * 1024
)

type mediaModerationHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service media.ModerationServiceAPI
}

type mediaModerationExpectedRequest struct {
	ExpectedRoomInstanceID      *uuid.UUID `json:"expected_room_instance_id"`
	ExpectedSpaceVersion        *int64     `json:"expected_space_version"`
	ExpectedRoomInstanceVersion *int64     `json:"expected_room_instance_version"`
	ExpectedProjectionVersion   *int64     `json:"expected_projection_version"`
	IdempotencyKey              *string    `json:"idempotency_key"`
	ReasonCode                  string     `json:"reason_code"`
}

type mediaSpaceLockRequest struct {
	mediaModerationExpectedRequest
	Locked *bool `json:"locked"`
}

type mediaParticipantRoleRequest struct {
	mediaModerationExpectedRequest
	DesiredRole *media.InstanceRole `json:"desired_role"`
}

func newMediaModerationHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service media.ModerationServiceAPI,
) mediaModerationHandlers {
	return mediaModerationHandlers{logger: logger, auth: auth, service: service}
}

func (handlers mediaModerationHandlers) lock(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r)
	if !ok {
		return
	}
	spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
	var request mediaSpaceLockRequest
	if !spaceOK || decodeJSONRequest(w, r, &request, maximumMediaModerationRequestBytes) != nil ||
		!validMediaModerationExpectedRequest(request.mediaModerationExpectedRequest) || request.Locked == nil {
		handlers.writeProblem(w, r, media.ErrInvalidModerationRequest)
		return
	}
	result, err := handlers.service.SetLocked(r.Context(), mediaAccess(principal), spaceID, media.LockMediaSpaceInput{
		Expected:       moderationExpected(request.mediaModerationExpectedRequest),
		IdempotencyKey: *request.IdempotencyKey,
		Locked:         *request.Locked,
		ReasonCode:     request.ReasonCode,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers mediaModerationHandlers) role(w http.ResponseWriter, r *http.Request) {
	principal, spaceID, participantKey, ok := handlers.participantMutation(w, r)
	if !ok {
		return
	}
	var request mediaParticipantRoleRequest
	if decodeJSONRequest(w, r, &request, maximumMediaModerationRequestBytes) != nil ||
		!validMediaModerationExpectedRequest(request.mediaModerationExpectedRequest) || request.DesiredRole == nil {
		handlers.writeProblem(w, r, media.ErrInvalidModerationRequest)
		return
	}
	result, err := handlers.service.ChangeParticipantRole(
		r.Context(), mediaAccess(principal), spaceID, participantKey,
		media.ChangeParticipantRoleInput{
			Expected:       moderationExpected(request.mediaModerationExpectedRequest),
			IdempotencyKey: *request.IdempotencyKey, DesiredRole: *request.DesiredRole,
			ReasonCode: request.ReasonCode,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers mediaModerationHandlers) mute(w http.ResponseWriter, r *http.Request) {
	handlers.moderateParticipant(w, r, true)
}

func (handlers mediaModerationHandlers) remove(w http.ResponseWriter, r *http.Request) {
	handlers.moderateParticipant(w, r, false)
}

func (handlers mediaModerationHandlers) moderateParticipant(
	w http.ResponseWriter,
	r *http.Request,
	mute bool,
) {
	principal, spaceID, participantKey, ok := handlers.participantMutation(w, r)
	if !ok {
		return
	}
	var request mediaModerationExpectedRequest
	if decodeJSONRequest(w, r, &request, maximumMediaModerationRequestBytes) != nil ||
		!validMediaModerationExpectedRequest(request) {
		handlers.writeProblem(w, r, media.ErrInvalidModerationRequest)
		return
	}
	input := media.ModerateParticipantInput{
		Expected: moderationExpected(request), IdempotencyKey: *request.IdempotencyKey,
		ReasonCode: request.ReasonCode,
	}
	var result media.ModerationResult
	var err error
	if mute {
		result, err = handlers.service.MuteParticipant(
			r.Context(), mediaAccess(principal), spaceID, participantKey, input,
		)
	} else {
		result, err = handlers.service.RemoveParticipant(
			r.Context(), mediaAccess(principal), spaceID, participantKey, input,
		)
	}
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers mediaModerationHandlers) participantMutation(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, uuid.UUID, uuid.UUID, bool) {
	principal, ok := handlers.principal(w, r)
	if !ok {
		return identity.Principal{}, uuid.Nil, uuid.Nil, false
	}
	spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
	participantKey, participantOK := parseResourceUUID(r.PathValue("participant_key"))
	if !spaceOK || !participantOK {
		handlers.writeProblem(w, r, media.ErrInvalidModerationRequest)
		return identity.Principal{}, uuid.Nil, uuid.Nil, false
	}
	return principal, spaceID, participantKey, true
}

func (handlers mediaModerationHandlers) principal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	if !handlers.auth.available(w, r) {
		return identity.Principal{}, false
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, media.ErrModerationUnavailable)
		return identity.Principal{}, false
	}
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	principal, ok := handlers.auth.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return identity.Principal{}, false
	}
	if principal.ActiveTenant == nil || !principal.ActiveTenant.IsActive {
		handlers.writeProblem(w, r, media.ErrModerationForbidden)
		return identity.Principal{}, false
	}
	expectedTenantID, valid := parseResourceUUID(strings.TrimSpace(r.Header.Get(mediaSpaceTenantHeader)))
	if !valid {
		handlers.writeProblem(w, r, media.ErrInvalidModerationRequest)
		return identity.Principal{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errMediaSpaceScopeChanged)
		return identity.Principal{}, false
	}
	return principal, true
}

func validMediaModerationExpectedRequest(request mediaModerationExpectedRequest) bool {
	return request.ExpectedRoomInstanceID != nil && request.ExpectedSpaceVersion != nil &&
		request.ExpectedRoomInstanceVersion != nil && request.ExpectedProjectionVersion != nil &&
		request.IdempotencyKey != nil
}

func moderationExpected(request mediaModerationExpectedRequest) media.ModerationExpectedVersions {
	return media.ModerationExpectedVersions{
		RoomInstanceID: *request.ExpectedRoomInstanceID,
		SpaceVersion:   *request.ExpectedSpaceVersion, RoomVersion: *request.ExpectedRoomInstanceVersion,
		ProjectionVersion: *request.ExpectedProjectionVersion,
	}
}

func (handlers mediaModerationHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusServiceUnavailable, "media_moderation_unavailable"
	title, detail := "Classroom moderation unavailable", "The moderation command could not be completed safely."
	var rateLimit *media.ModerationRateLimitError
	switch {
	case errors.As(err, &rateLimit):
		w.Header().Set("Retry-After", retryAfterSeconds(rateLimit.RetryAfter))
		status, code = http.StatusTooManyRequests, "media_moderation_rate_limited"
		title, detail = "Too many moderation commands", "Wait before sending another moderation command."
	case errors.Is(err, media.ErrInvalidModerationRequest):
		status, code = http.StatusBadRequest, "media_moderation_invalid"
		title, detail = "Invalid moderation command", "Review the target, expected versions, role, and idempotency key."
	case errors.Is(err, media.ErrModerationForbidden), errors.Is(err, media.ErrSpaceAccessDenied):
		status, code = http.StatusForbidden, "media_moderation_forbidden"
		title, detail = "Classroom moderation denied", "The active workspace cannot authorize this moderation command."
	case errors.Is(err, media.ErrModerationNotFound), errors.Is(err, media.ErrSpaceNotFound),
		errors.Is(err, media.ErrSourceUnavailable):
		status, code = http.StatusNotFound, "media_moderation_not_found"
		title, detail = "Classroom moderation target unavailable", "The requested classroom or participant is unavailable."
	case errors.Is(err, media.ErrModerationConflict), errors.Is(err, media.ErrRoomNotOpen),
		errors.Is(err, media.ErrRoomLocked):
		status, code = http.StatusConflict, "stale_version"
		title, detail = "Classroom projection changed", "Reload the latest classroom snapshot before retrying."
	case errors.Is(err, media.ErrModerationIdempotency):
		status, code = http.StatusConflict, "media_moderation_idempotency_conflict"
		title, detail = "Moderation retry conflict", "Use a new idempotency key after changing the moderation command."
	case errors.Is(err, errMediaSpaceScopeChanged):
		status, code = http.StatusConflict, "media_space_scope_changed"
		title, detail = "Active workspace changed", "Reload the current workspace before retrying."
	case errors.Is(err, media.ErrModerationUnavailable), errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, featurecontrol.ErrUnavailable):
	default:
		handlers.logger.Error(
			"classroom moderation request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path), "error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
