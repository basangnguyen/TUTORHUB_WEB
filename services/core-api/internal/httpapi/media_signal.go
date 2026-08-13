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
	mediaParticipantsPattern       = "/api/v1/media/spaces/{space_id}/participants"
	mediaSignalsPattern            = "/api/v1/media/spaces/{space_id}/signals"
	maximumMediaSignalRequestBytes = 8 * 1024
)

type mediaSignalHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service media.MediaSignalServiceAPI
}

type mediaSignalMutationRequest struct {
	ExpectedRoomInstanceID      *uuid.UUID             `json:"expected_room_instance_id"`
	ExpectedSpaceVersion        *int64                 `json:"expected_space_version"`
	ExpectedRoomInstanceVersion *int64                 `json:"expected_room_instance_version"`
	ExpectedProjectionVersion   *int64                 `json:"expected_projection_version"`
	IdempotencyKey              *string                `json:"idempotency_key"`
	Kind                        *media.MediaSignalKind `json:"kind"`
	TargetParticipantKey        *uuid.UUID             `json:"target_participant_key"`
	Reaction                    *media.MediaReaction   `json:"reaction"`
}

func newMediaSignalHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service media.MediaSignalServiceAPI,
) mediaSignalHandlers {
	return mediaSignalHandlers{logger: logger, auth: auth, service: service}
}

func (handlers mediaSignalHandlers) participants(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r, false)
	if !ok {
		return
	}
	spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
	roomID, roomOK := parseResourceUUID(strings.TrimSpace(r.URL.Query().Get("room_instance_id")))
	spaceVersion, spaceVersionOK := positiveInt64Query(r, "expected_space_version")
	roomVersion, roomVersionOK := positiveInt64Query(r, "expected_room_instance_version")
	if !spaceOK || !roomOK || !spaceVersionOK || !roomVersionOK {
		handlers.writeProblem(w, r, media.ErrInvalidMediaSignalRequest)
		return
	}
	snapshot, err := handlers.service.GetParticipantSnapshot(
		r.Context(), mediaAccess(principal), spaceID,
		media.GetMediaParticipantSnapshotInput{
			ExpectedSpaceVersion: spaceVersion, ExpectedRoomInstanceID: roomID,
			ExpectedRoomInstanceVersion: roomVersion,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, snapshot)
}

func (handlers mediaSignalHandlers) signal(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r, true)
	if !ok {
		return
	}
	spaceID, spaceOK := parseResourceUUID(r.PathValue("space_id"))
	var request mediaSignalMutationRequest
	if !spaceOK || decodeJSONRequest(
		w, r, &request, maximumMediaSignalRequestBytes,
	) != nil || request.ExpectedRoomInstanceID == nil ||
		request.ExpectedSpaceVersion == nil || request.ExpectedRoomInstanceVersion == nil ||
		request.ExpectedProjectionVersion == nil || request.IdempotencyKey == nil ||
		request.Kind == nil {
		handlers.writeProblem(w, r, media.ErrInvalidMediaSignalRequest)
		return
	}
	input := media.SendMediaSignalInput{
		ExpectedRoomInstanceID:      *request.ExpectedRoomInstanceID,
		ExpectedSpaceVersion:        *request.ExpectedSpaceVersion,
		ExpectedRoomInstanceVersion: *request.ExpectedRoomInstanceVersion,
		ExpectedProjectionVersion:   *request.ExpectedProjectionVersion,
		IdempotencyKey:              *request.IdempotencyKey, Kind: *request.Kind,
	}
	if request.TargetParticipantKey != nil {
		input.TargetParticipantKey = *request.TargetParticipantKey
	}
	if request.Reaction != nil {
		input.Reaction = *request.Reaction
	}
	snapshot, err := handlers.service.SendSignal(
		r.Context(), mediaAccess(principal), spaceID, input,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, snapshot)
}

func (handlers mediaSignalHandlers) principal(
	w http.ResponseWriter,
	r *http.Request,
	mutating bool,
) (identity.Principal, bool) {
	if !handlers.auth.available(w, r) {
		return identity.Principal{}, false
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, media.ErrMediaSignalUnavailable)
		return identity.Principal{}, false
	}
	var principal identity.Principal
	var ok bool
	if mutating {
		sessionToken, tokenOK := handlers.auth.sessionToken(w, r)
		if !tokenOK {
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
		handlers.writeProblem(w, r, media.ErrSpaceAccessDenied)
		return identity.Principal{}, false
	}
	expectedTenantID, valid := parseResourceUUID(
		strings.TrimSpace(r.Header.Get(mediaSpaceTenantHeader)),
	)
	if !valid {
		handlers.writeProblem(w, r, media.ErrInvalidMediaSignalRequest)
		return identity.Principal{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errMediaSpaceScopeChanged)
		return identity.Principal{}, false
	}
	return principal, true
}

func (handlers mediaSignalHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusServiceUnavailable, "media_signal_unavailable"
	title, detail := "Classroom signals unavailable", "The classroom signal request could not be completed safely."
	var rateLimit *media.MediaSignalRateLimitError
	switch {
	case errors.As(err, &rateLimit):
		w.Header().Set("Retry-After", retryAfterSeconds(rateLimit.RetryAfter))
		status, code = http.StatusTooManyRequests, "media_signal_rate_limited"
		title, detail = "Too many classroom signals", "Wait before sending another hand or reaction signal."
	case errors.Is(err, media.ErrInvalidMediaSignalRequest):
		status, code = http.StatusBadRequest, "participant_signal_invalid"
		title, detail = "Invalid classroom signal", "Review the current room versions, signal kind, and idempotency key."
	case errors.Is(err, media.ErrSpaceAccessDenied):
		status, code = http.StatusForbidden, "participant_signal_forbidden"
		title, detail = "Classroom signal denied", "The active workspace cannot authorize this classroom signal."
	case errors.Is(err, media.ErrMediaSignalNotFound),
		errors.Is(err, media.ErrMediaSignalTargetUnavailable),
		errors.Is(err, media.ErrSpaceNotFound), errors.Is(err, media.ErrSourceUnavailable):
		status, code = http.StatusNotFound, "participant_signal_not_found"
		title, detail = "Classroom participant unavailable", "The requested classroom participant projection is unavailable."
	case errors.Is(err, media.ErrMediaSignalVersionConflict),
		errors.Is(err, media.ErrRoomNotOpen), errors.Is(err, media.ErrRoomLocked):
		status, code = http.StatusConflict, "stale_version"
		title, detail = "Classroom projection changed", "Reload the latest participant snapshot before retrying."
	case errors.Is(err, media.ErrMediaSignalIdempotency):
		status, code = http.StatusConflict, "participant_signal_idempotency_conflict"
		title, detail = "Classroom signal retry conflict", "Use a new idempotency key after changing the signal command."
	case errors.Is(err, errMediaSpaceScopeChanged):
		status, code = http.StatusConflict, "media_space_scope_changed"
		title, detail = "Active workspace changed", "Reload the current workspace before retrying."
	case errors.Is(err, media.ErrMediaSignalUnavailable),
		errors.Is(err, context.DeadlineExceeded), errors.Is(err, featurecontrol.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, "media_signal_unavailable"
		title, detail = "Classroom signals unavailable", "Try the classroom signal again later."
	default:
		handlers.logger.Error(
			"classroom signal request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
