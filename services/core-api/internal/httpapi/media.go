package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	mediaTokenPathPattern              = "/api/v1/classes/{class_id}/media-token"
	mediaEventsPathPattern             = "/api/v1/classes/{class_id}/media-events"
	mediaSpaceCredentialPathPattern    = "/api/v1/media/spaces/{space_id}/join-credentials"
	liveKitWebhookPath                 = "/api/v1/webhooks/livekit"
	maximumMediaEventBytes             = 8 * 1024
	maximumMediaCredentialRequestBytes = 4 * 1024
	maximumWebhookBodyBytes            = 256 * 1024
)

type mediaHandlers struct {
	logger           *slog.Logger
	auth             authHandlers
	service          media.ServiceAPI
	credentials      media.InstanceCredentialServiceAPI
	webhookVerifier  media.WebhookVerifier
	webhookProcessor media.WebhookProcessor
}

type mediaSpaceCredentialRequest struct {
	JoinAttemptID *uuid.UUID `json:"join_attempt_id"`
}

type mediaTokenResponse struct {
	AccessToken         string    `json:"access_token"`
	ServerURL           string    `json:"server_url"`
	RoomName            string    `json:"room_name"`
	ParticipantIdentity string    `json:"participant_identity"`
	ParticipantName     string    `json:"participant_name"`
	AttemptID           uuid.UUID `json:"attempt_id"`
	CanPublish          bool      `json:"can_publish"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type mediaEventRequest struct {
	AttemptID  uuid.UUID `json:"attempt_id"`
	Stage      string    `json:"stage"`
	Outcome    string    `json:"outcome"`
	ErrorCode  string    `json:"error_code,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

func newMediaHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service media.ServiceAPI,
	credentials media.InstanceCredentialServiceAPI,
	webhookVerifier media.WebhookVerifier,
	webhookProcessor media.WebhookProcessor,
) mediaHandlers {
	return mediaHandlers{
		logger: logger, auth: auth, service: service, credentials: credentials,
		webhookVerifier: webhookVerifier, webhookProcessor: webhookProcessor,
	}
}

func (handlers mediaHandlers) issueInstanceCredential(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.instanceCredentialPrincipal(w, r)
	if !ok {
		return
	}
	spaceID, ok := parseResourceUUID(r.PathValue("space_id"))
	if !ok {
		handlers.writeInstanceCredentialProblem(w, r, media.ErrInvalidCredentialRequest)
		return
	}
	var request mediaSpaceCredentialRequest
	if err := decodeJSONRequest(
		w, r, &request, maximumMediaCredentialRequestBytes,
	); err != nil || request.JoinAttemptID == nil || *request.JoinAttemptID == uuid.Nil {
		handlers.writeInstanceCredentialProblem(w, r, media.ErrInvalidCredentialRequest)
		return
	}
	credential, err := handlers.credentials.IssueInstanceCredential(
		r.Context(),
		mediaAccess(principal),
		spaceID,
		media.IssueInstanceCredentialInput{JoinAttemptID: *request.JoinAttemptID},
	)
	if err != nil {
		handlers.writeInstanceCredentialProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, credential)
}

func (handlers mediaHandlers) issueJoinCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMediaMethodProblem(w, r)
		return
	}
	principal, classID, ok := handlers.authorizedRequest(w, r)
	if !ok {
		return
	}
	credential, err := handlers.service.IssueJoinCredential(
		r.Context(),
		mediaAccess(principal),
		classID,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, http.StatusOK, mediaTokenResponse{
		AccessToken:         credential.AccessToken,
		ServerURL:           credential.ServerURL,
		RoomName:            credential.RoomName,
		ParticipantIdentity: credential.ParticipantIdentity,
		ParticipantName:     credential.ParticipantName,
		AttemptID:           credential.AttemptID,
		CanPublish:          credential.CanPublish,
		ExpiresAt:           credential.ExpiresAt,
	})
}

func (handlers mediaHandlers) recordClientEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMediaMethodProblem(w, r)
		return
	}
	principal, classID, ok := handlers.authorizedRequest(w, r)
	if !ok {
		return
	}
	var request mediaEventRequest
	if err := decodeJSONRequest(w, r, &request, maximumMediaEventBytes); err != nil {
		handlers.writeProblem(w, r, media.ErrInvalidRequest)
		return
	}
	if err := handlers.service.RecordClientEvent(
		r.Context(),
		mediaAccess(principal),
		classID,
		media.ClientEventInput{
			AttemptID: request.AttemptID, Stage: request.Stage, Outcome: request.Outcome,
			ErrorCode: request.ErrorCode, DurationMS: request.DurationMS,
		},
	); err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	w.WriteHeader(http.StatusNoContent)
}

func (handlers mediaHandlers) receiveWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w, r, http.StatusMethodNotAllowed, "Method not allowed",
			"The LiveKit webhook endpoint accepts POST requests.",
		)
		return
	}
	processor := handlers.webhookProcessor
	if processor == nil {
		processor = handlers.service
	}
	if processor == nil || handlers.webhookVerifier == nil {
		handlers.writeProblem(w, r, media.ErrUnavailable)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/webhook+json" {
		handlers.writeWebhookAuthenticationProblem(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximumWebhookBodyBytes)
	event, err := handlers.webhookVerifier.Receive(r)
	if err != nil {
		handlers.writeWebhookAuthenticationProblem(w, r)
		return
	}
	result, err := processor.RecordWebhook(r.Context(), event)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	handlers.logger.Info(
		"LiveKit webhook processed",
		"request_id", RequestIDFromContext(r.Context()),
		"event_type", logsafe.String(event.EventType),
		"recorded", result.Recorded,
		"duplicate", result.Duplicate,
		"ignored", result.Ignored,
	)
	w.WriteHeader(http.StatusNoContent)
}

func (handlers mediaHandlers) instanceCredentialPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	if !handlers.auth.available(w, r) {
		return identity.Principal{}, false
	}
	if handlers.credentials == nil {
		handlers.writeInstanceCredentialProblem(w, r, media.ErrLifecycleUnavailable)
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
		handlers.writeInstanceCredentialProblem(w, r, media.ErrSpaceAccessDenied)
		return identity.Principal{}, false
	}
	expectedTenantID, valid := parseResourceUUID(
		strings.TrimSpace(r.Header.Get(mediaSpaceTenantHeader)),
	)
	if !valid {
		handlers.writeInstanceCredentialProblem(w, r, media.ErrInvalidCredentialRequest)
		return identity.Principal{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeInstanceCredentialProblem(w, r, errMediaSpaceScopeChanged)
		return identity.Principal{}, false
	}
	return principal, true
}

func (handlers mediaHandlers) writeInstanceCredentialProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusServiceUnavailable, "media_credential_unavailable"
	title, detail := "Media credential unavailable", "The media credential could not be issued safely."
	var rateLimit *media.CredentialRateLimitError
	switch {
	case errors.As(err, &rateLimit):
		w.Header().Set("Retry-After", retryAfterSeconds(rateLimit.RetryAfter))
		status, code = http.StatusTooManyRequests, "media_credential_rate_limited"
		title, detail = "Too many media credential requests", "Wait before requesting another media credential."
	case errors.Is(err, media.ErrInvalidCredentialRequest):
		status, code = http.StatusBadRequest, "media_credential_invalid"
		title, detail = "Invalid media credential request", "Review the space, tenant assertion, and join attempt."
	case errors.Is(err, media.ErrSpaceAccessDenied):
		status, code = http.StatusForbidden, "media_credential_forbidden"
		title, detail = "Media credential access denied", "The active workspace cannot authorize this media credential."
	case errors.Is(err, media.ErrSpaceNotFound), errors.Is(err, media.ErrSourceUnavailable):
		status, code = http.StatusNotFound, "media_space_not_found"
		title, detail = "Media space unavailable", "The media space is unavailable in the active workspace."
	case errors.Is(err, media.ErrRoomNotOpen):
		status, code = http.StatusConflict, "room_not_open"
		title, detail = "Media room is not open", "Wait for an active room instance before joining."
	case errors.Is(err, media.ErrRoomLocked):
		status, code = http.StatusConflict, "room_locked"
		title, detail = "Media room is locked", "The room is not accepting new participants."
	case errors.Is(err, media.ErrAdmissionRequired):
		status, code = http.StatusConflict, "admission_required"
		title, detail = "Admission is required", "Wait for admission before requesting a media credential."
	case errors.Is(err, media.ErrParticipantConflict):
		status, code = http.StatusConflict, "participant_session_conflict"
		title, detail = "Participant session changed", "Start a new join attempt after reloading the media space."
	case errors.Is(err, errMediaSpaceScopeChanged):
		status, code = http.StatusConflict, "media_space_scope_changed"
		title, detail = "Active workspace changed", "Reload the current workspace before retrying."
	case errors.Is(err, media.ErrMediaProviderUnavailable):
		status, code = http.StatusServiceUnavailable, "media_provider_unavailable"
		title, detail = "Media provider unavailable", "Try joining again later."
	case errors.Is(err, media.ErrLifecycleUnavailable), errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusServiceUnavailable, "media_credential_unavailable"
		title, detail = "Media credential unavailable", "Try joining again later."
	default:
		handlers.logger.Error(
			"media credential request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}

func (handlers mediaHandlers) authorizedRequest(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, uuid.UUID, bool) {
	if !handlers.auth.available(w, r) {
		return identity.Principal{}, uuid.Nil, false
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, media.ErrUnavailable)
		return identity.Principal{}, uuid.Nil, false
	}
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, uuid.Nil, false
	}
	principal, ok := handlers.auth.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return identity.Principal{}, uuid.Nil, false
	}
	classID, ok := parseResourceUUID(r.PathValue("class_id"))
	if !ok {
		handlers.writeProblem(w, r, media.ErrInvalidRequest)
		return identity.Principal{}, uuid.Nil, false
	}

	return principal, classID, true
}

func (handlers mediaHandlers) writeWebhookAuthenticationProblem(
	w http.ResponseWriter,
	r *http.Request,
) {
	handlers.logger.Warn(
		"LiveKit webhook rejected",
		"request_id", RequestIDFromContext(r.Context()),
		"error_code", "invalid_webhook_signature_or_payload",
	)
	writeProblem(
		w, r, http.StatusUnauthorized, "Webhook verification failed",
		"The LiveKit webhook signature or payload is invalid.",
	)
}

func (handlers mediaHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	title := "Media request failed"
	detail := "The classroom media request could not be completed."

	switch {
	case errors.Is(err, media.ErrInvalidRequest), errors.Is(err, media.ErrInvalidWebhook):
		status = http.StatusBadRequest
		title = "Invalid media request"
		detail = "Check the classroom identifier and media request fields."
	case errors.Is(err, media.ErrAccessDenied), errors.Is(err, classroom.ErrClassAccessDenied):
		status = http.StatusForbidden
		title = "Media access denied"
		detail = "Your active workspace membership does not allow this classroom media action."
	case errors.Is(err, classroom.ErrClassNotFound):
		status = http.StatusNotFound
		title = "Class not found"
		detail = "The class does not exist in the active workspace."
	case errors.Is(err, media.ErrClassUnavailable):
		status = http.StatusConflict
		title = "Classroom media unavailable"
		detail = "This class is not active and cannot start or continue a media request."
	case errors.Is(err, media.ErrLegacyMediaDisabled):
		status = http.StatusGone
		title = "Legacy classroom media disabled"
		detail = "Use the room-instance media credential flow."
	case errors.Is(err, media.ErrUnavailable):
		status = http.StatusServiceUnavailable
		title = "Media service unavailable"
		detail = "Live classroom media is not configured for this environment."
	case errors.Is(err, media.ErrMediaProviderUnavailable),
		errors.Is(err, media.ErrLifecycleUnavailable),
		errors.Is(err, context.DeadlineExceeded):
		status = http.StatusServiceUnavailable
		title = "Media service unavailable"
		detail = "The media request could not be completed safely."
	}

	if status >= http.StatusInternalServerError {
		handlers.logger.Error(
			"classroom media request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeProblem(w, r, status, title, detail)
}

func mediaAccess(principal identity.Principal) media.AccessContext {
	access := media.AccessContext{
		ActorID: principal.User.ID, SessionID: principal.SessionID,
		DisplayName: strings.TrimSpace(principal.User.DisplayName),
	}
	if principal.ActiveTenant != nil {
		access.TenantID = principal.ActiveTenant.ID
		access.Role = principal.ActiveTenant.Role
		access.MembershipActive = principal.ActiveTenant.IsActive
		access.OrganizationRoles = []policy.OrganizationRole{
			policy.OrganizationRole(principal.ActiveTenant.Role),
		}
	}

	return access
}

func writeMediaMethodProblem(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Allow", http.MethodPost)
	writeProblem(
		w, r, http.StatusMethodNotAllowed, "Method not allowed",
		"Classroom media endpoints accept POST requests.",
	)
}
