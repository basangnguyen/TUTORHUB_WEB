package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

const (
	calendarInvitationResolvePath       = "/api/v1/calendar/invitations/resolve"
	calendarInvitationRespondPath       = "/api/v1/calendar/invitations/respond"
	maximumExternalCalendarRSVPBodySize = 16 * 1024
)

type externalCalendarRSVPHandlers struct {
	logger    *slog.Logger
	service   classroom.ExternalRSVPServiceAPI
	limiter   InvitationRateLimiter
	clock     func() time.Time
	webOrigin string
}

type resolveExternalCalendarRSVPRequest struct {
	Token *string `json:"token"`
}

type respondExternalCalendarRSVPRequest struct {
	Token                   *string              `json:"token"`
	State                   *classroom.RSVPState `json:"state"`
	Note                    *string              `json:"note"`
	ExpectedAttendeeVersion *int64               `json:"expected_attendee_version"`
	IdempotencyKey          *string              `json:"idempotency_key"`
}

type externalCalendarRSVPProjectionResponse struct {
	Title               string              `json:"title"`
	StartsAt            time.Time           `json:"starts_at"`
	EndsAt              time.Time           `json:"ends_at"`
	Timezone            string              `json:"timezone"`
	RSVPState           classroom.RSVPState `json:"rsvp_state"`
	ResponseRequested   bool                `json:"response_requested"`
	AttendeeVersion     int64               `json:"attendee_version"`
	InvitationSequence  int64               `json:"invitation_sequence"`
	CapabilityExpiresAt time.Time           `json:"capability_expires_at"`
}

type externalCalendarRSVPMutationResponse struct {
	Projection externalCalendarRSVPProjectionResponse `json:"projection"`
	Replayed   bool                                   `json:"replayed"`
}

func newExternalCalendarRSVPHandlers(
	logger *slog.Logger,
	service classroom.ExternalRSVPServiceAPI,
	limiter InvitationRateLimiter,
	clock func() time.Time,
	webOrigin string,
) externalCalendarRSVPHandlers {
	return externalCalendarRSVPHandlers{
		logger:    logger,
		service:   service,
		limiter:   limiter,
		clock:     clock,
		webOrigin: canonicalPublicOrigin(webOrigin),
	}
}

func externalCalendarRSVPResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'",
		)
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (handlers externalCalendarRSVPHandlers) resolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Calendar invitation resolution supports POST requests.",
		)
		return
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, classroom.ErrSessionParticipationUnavailable)
		return
	}
	var request resolveExternalCalendarRSVPRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumExternalCalendarRSVPBodySize,
	); err != nil || request.Token == nil || strings.TrimSpace(*request.Token) == "" {
		handlers.writeProblem(w, r, classroom.ErrInvalidParticipationInput)
		return
	}
	if !handlers.allow(
		w,
		r,
		*request.Token,
		InvitationRateLimitCalendarRSVPResolveIP,
		InvitationRateLimitCalendarRSVPResolveToken,
	) {
		return
	}
	projection, err := handlers.service.ResolveCapability(r.Context(), *request.Token)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		newExternalCalendarRSVPProjectionResponse(projection),
	)
}

func (handlers externalCalendarRSVPHandlers) respond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Calendar invitation responses support POST requests.",
		)
		return
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, classroom.ErrSessionParticipationUnavailable)
		return
	}
	if handlers.webOrigin == "" || canonicalPublicOrigin(r.Header.Get("Origin")) != handlers.webOrigin {
		writeCodedProblem(
			w,
			r,
			http.StatusForbidden,
			"calendar_invitation_response_forbidden",
			"Calendar invitation response denied",
			"Open the invitation from the TutorHub response page before confirming.",
		)
		return
	}
	var request respondExternalCalendarRSVPRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumExternalCalendarRSVPBodySize,
	); err != nil || request.Token == nil || request.State == nil ||
		request.ExpectedAttendeeVersion == nil || request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, classroom.ErrInvalidParticipationInput)
		return
	}
	if !handlers.allow(
		w,
		r,
		*request.Token,
		InvitationRateLimitCalendarRSVPRespondIP,
		InvitationRateLimitCalendarRSVPRespondToken,
	) {
		return
	}
	note := ""
	if request.Note != nil {
		note = *request.Note
	}
	result, err := handlers.service.RespondWithCapability(
		r.Context(),
		classroom.ExternalRSVPResponseInput{
			RawToken:                *request.Token,
			State:                   *request.State,
			Note:                    note,
			ExpectedAttendeeVersion: *request.ExpectedAttendeeVersion,
			IdempotencyKey:          *request.IdempotencyKey,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, externalCalendarRSVPMutationResponse{
		Projection: newExternalCalendarRSVPProjectionResponse(result.Projection),
		Replayed:   result.Replayed,
	})
}

func (handlers externalCalendarRSVPHandlers) allow(
	w http.ResponseWriter,
	r *http.Request,
	rawToken string,
	ipAction InvitationRateLimitAction,
	tokenAction InvitationRateLimitAction,
) bool {
	clientPrefix := identity.IPPrefix(r.RemoteAddr)
	if clientPrefix == "" {
		clientPrefix = "unknown"
	}
	tokenDigest := sha256.Sum256([]byte(rawToken))
	tokenBucket := "token:" + hex.EncodeToString(tokenDigest[:])
	now := handlers.clock().UTC()
	for _, check := range []struct {
		action InvitationRateLimitAction
		bucket string
	}{
		{action: ipAction, bucket: "ip:" + clientPrefix},
		{action: tokenAction, bucket: tokenBucket},
	} {
		decision := handlers.limiter.Allow(r.Context(), check.action, check.bucket, now)
		if decision.Err != nil {
			handlers.writeProblem(w, r, classroom.ErrSessionParticipationUnavailable)
			return false
		}
		if !decision.Allowed {
			w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
			writeCodedProblem(
				w,
				r,
				http.StatusTooManyRequests,
				"calendar_invitation_rate_limited",
				"Too many calendar invitation requests",
				"Wait before trying the invitation again.",
			)
			return false
		}
	}
	return true
}

func (handlers externalCalendarRSVPHandlers) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	status := http.StatusInternalServerError
	code := "calendar_invitation_failed"
	title := "Calendar invitation request failed"
	detail := "The calendar invitation request could not be completed safely."
	switch {
	case errors.Is(err, classroom.ErrInvalidParticipationInput):
		status, code = http.StatusBadRequest, "calendar_invitation_request_invalid"
		title = "Invalid calendar invitation request"
		detail = "Review the response state, expected version, and idempotency key."
	case errors.Is(err, classroom.ErrExternalRSVPCapabilityUnavailable):
		status, code = http.StatusNotFound, "calendar_invitation_unavailable"
		title = "Calendar invitation unavailable"
		detail = "This invitation cannot be used. Request a new invitation from the organizer."
	case errors.Is(err, classroom.ErrExternalRSVPVersionConflict),
		errors.Is(err, classroom.ErrSessionParticipationIdempotencyConflict),
		errors.Is(err, classroom.ErrSessionRSVPUnavailable):
		status, code = http.StatusConflict, "calendar_invitation_response_conflict"
		title = "Calendar invitation response changed"
		detail = "Reload the invitation before responding again."
	case errors.Is(err, classroom.ErrSessionParticipationUnavailable):
		status, code = http.StatusServiceUnavailable, "calendar_invitation_service_unavailable"
		title = "Calendar invitation service unavailable"
		detail = "Try the invitation again later."
	default:
		handlers.logger.Error(
			"external calendar RSVP request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}

func newExternalCalendarRSVPProjectionResponse(
	projection classroom.ExternalRSVPProjection,
) externalCalendarRSVPProjectionResponse {
	return externalCalendarRSVPProjectionResponse{
		Title:               projection.Title,
		StartsAt:            projection.StartsAt.UTC(),
		EndsAt:              projection.EndsAt.UTC(),
		Timezone:            projection.Timezone,
		RSVPState:           projection.RSVPState,
		ResponseRequested:   projection.ResponseRequested,
		AttendeeVersion:     projection.AttendeeVersion,
		InvitationSequence:  projection.InvitationSequence,
		CapabilityExpiresAt: projection.CapabilityExpiresAt.UTC(),
	}
}

func canonicalPublicOrigin(raw string) string {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}
