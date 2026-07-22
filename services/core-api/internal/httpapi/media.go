package httpapi

import (
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
	mediaTokenPathPattern   = "/api/v1/classes/{class_id}/media-token"
	mediaEventsPathPattern  = "/api/v1/classes/{class_id}/media-events"
	liveKitWebhookPath      = "/api/v1/webhooks/livekit"
	maximumMediaEventBytes  = 8 * 1024
	maximumWebhookBodyBytes = 256 * 1024
)

type mediaHandlers struct {
	logger   *slog.Logger
	auth     authHandlers
	service  media.ServiceAPI
	webhooks media.WebhookVerifier
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
	webhooks media.WebhookVerifier,
) mediaHandlers {
	return mediaHandlers{
		logger: logger, auth: auth, service: service, webhooks: webhooks,
	}
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
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w, r, http.StatusMethodNotAllowed, "Method not allowed",
			"The LiveKit webhook endpoint accepts POST requests.",
		)
		return
	}
	if handlers.service == nil || handlers.webhooks == nil {
		handlers.writeProblem(w, r, media.ErrUnavailable)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/webhook+json" {
		handlers.writeWebhookAuthenticationProblem(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximumWebhookBodyBytes)
	event, err := handlers.webhooks.Receive(r)
	if err != nil {
		handlers.writeWebhookAuthenticationProblem(w, r)
		return
	}
	result, err := handlers.service.RecordWebhook(r.Context(), event)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}

	handlers.logger.Info(
		"LiveKit webhook processed",
		"request_id", RequestIDFromContext(r.Context()),
		"event_id", logsafe.String(event.ID),
		"event_type", logsafe.String(event.EventType),
		"recorded", result.Recorded,
		"duplicate", result.Duplicate,
		"ignored", result.Ignored,
	)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
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
	case errors.Is(err, media.ErrUnavailable):
		status = http.StatusServiceUnavailable
		title = "Media service unavailable"
		detail = "Live classroom media is not configured for this environment."
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
		access.MembershipActive = true
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
