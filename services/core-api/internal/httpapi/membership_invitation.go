package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
)

const maximumMembershipInvitationRequestBytes = 16 * 1024

const (
	membershipInvitationsAdminCollectionPattern = "/api/v1/tenants/{tenantId}/invitations"
	membershipInvitationsAdminRevokePattern     = "/api/v1/tenants/{tenantId}/invitations/{invitationId}/revoke"
	membershipInvitationPreviewPath             = "/api/v1/membership-invitations/preview"
	membershipInvitationAcceptPath              = "/api/v1/membership-invitations/accept"
)

type membershipInvitationHandlers struct {
	auth        authHandlers
	logger      *slog.Logger
	identity    identity.ServiceAPI
	webOrigin   string
	rateLimiter InvitationRateLimiter
	clock       func() time.Time
}

type createMembershipInvitationRequest struct {
	Email        string `json:"email"`
	IntendedRole string `json:"intended_role"`
}

type membershipInvitationTokenRequest struct {
	Token string `json:"token"`
}

type membershipInvitationResponse struct {
	ID           uuid.UUID                           `json:"id"`
	TenantID     uuid.UUID                           `json:"tenant_id"`
	Email        string                              `json:"email"`
	IntendedRole string                              `json:"intended_role"`
	Status       identity.MembershipInvitationStatus `json:"status"`
	ExpiresAt    time.Time                           `json:"expires_at"`
	AcceptedAt   *time.Time                          `json:"accepted_at"`
	RevokedAt    *time.Time                          `json:"revoked_at"`
	CreatedAt    time.Time                           `json:"created_at"`
	UpdatedAt    time.Time                           `json:"updated_at"`
}

type membershipInvitationListResponse struct {
	Items []membershipInvitationResponse `json:"items"`
}

type createMembershipInvitationResponse struct {
	Invitation membershipInvitationResponse `json:"invitation"`
	AcceptURL  string                       `json:"accept_url"`
}

type membershipInvitationPreviewResponse struct {
	TenantName   string                              `json:"tenant_name"`
	MaskedEmail  string                              `json:"masked_email"`
	IntendedRole string                              `json:"intended_role"`
	Status       identity.MembershipInvitationStatus `json:"status"`
	ExpiresAt    time.Time                           `json:"expires_at"`
}

type acceptMembershipInvitationResponse struct {
	Invitation  membershipInvitationResponse `json:"invitation"`
	CurrentUser meResponse                   `json:"current_user"`
}

type membershipInvitationProblemScope int

const (
	membershipInvitationAdminProblem membershipInvitationProblemScope = iota
	membershipInvitationTokenProblem
)

func newMembershipInvitationHandlers(
	cfg config.Config,
	logger *slog.Logger,
	auth authHandlers,
	service identity.ServiceAPI,
	rateLimiter InvitationRateLimiter,
	clock func() time.Time,
) membershipInvitationHandlers {
	return membershipInvitationHandlers{
		auth:        auth,
		logger:      logger,
		identity:    service,
		webOrigin:   strings.TrimRight(cfg.WebOrigin, "/"),
		rateLimiter: rateLimiter,
		clock:       clock,
	}
}

func membershipInvitationResponseHeaders(varyCookie bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if varyCookie {
			w.Header().Add("Vary", "Cookie")
		}
		next.ServeHTTP(w, r)
	})
}

func (handlers membershipInvitationHandlers) adminCollection(
	w http.ResponseWriter,
	r *http.Request,
) {
	tenantID, ok := parseInvitationPathUUID(r.PathValue("tenantId"))
	if !ok {
		handlers.writeProblem(w, r, identity.ErrTenantNotFound, membershipInvitationAdminProblem)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.list(w, r, tenantID)
	case http.MethodPost:
		handlers.create(w, r, tenantID)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Membership invitations support GET and POST requests.",
		)
	}
}

func (handlers membershipInvitationHandlers) adminRevoke(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Membership invitation revoke supports POST requests.",
		)
		return
	}

	tenantID, tenantOK := parseInvitationPathUUID(r.PathValue("tenantId"))
	invitationID, invitationOK := parseInvitationPathUUID(r.PathValue("invitationId"))
	if !tenantOK || !invitationOK {
		handlers.writeProblem(
			w,
			r,
			identity.ErrMembershipInvitationUnavailable,
			membershipInvitationAdminProblem,
		)
		return
	}
	handlers.revoke(w, r, tenantID, invitationID)
}

func (handlers membershipInvitationHandlers) list(
	w http.ResponseWriter,
	r *http.Request,
	tenantID uuid.UUID,
) {
	if !handlers.auth.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}

	invitations, err := handlers.identity.ListMembershipInvitations(
		r.Context(),
		principal,
		tenantID,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, membershipInvitationAdminProblem)
		return
	}
	items := make([]membershipInvitationResponse, 0, len(invitations))
	for _, invitation := range invitations {
		items = append(items, newMembershipInvitationResponse(invitation))
	}
	writeJSON(handlers.logger, w, http.StatusOK, membershipInvitationListResponse{Items: items})
}

func (handlers membershipInvitationHandlers) create(
	w http.ResponseWriter,
	r *http.Request,
	tenantID uuid.UUID,
) {
	if !handlers.auth.available(w, r) {
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

	var request createMembershipInvitationRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumMembershipInvitationRequestBytes,
	); err != nil {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"Invalid invitation request",
			"Provide one JSON object with only email and intended_role.",
		)
		return
	}
	result, err := handlers.identity.CreateMembershipInvitation(
		r.Context(),
		principal,
		tenantID,
		identity.CreateMembershipInvitationInput{
			Email:        request.Email,
			IntendedRole: request.IntendedRole,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err, membershipInvitationAdminProblem)
		return
	}

	writeJSON(handlers.logger, w, http.StatusCreated, createMembershipInvitationResponse{
		Invitation: newMembershipInvitationResponse(result.Invitation),
		AcceptURL:  handlers.acceptURL(result.Token),
	})
}

func (handlers membershipInvitationHandlers) revoke(
	w http.ResponseWriter,
	r *http.Request,
	tenantID uuid.UUID,
	invitationID uuid.UUID,
) {
	if !handlers.auth.available(w, r) {
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

	invitation, err := handlers.identity.RevokeMembershipInvitation(
		r.Context(),
		principal,
		tenantID,
		invitationID,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, membershipInvitationAdminProblem)
		return
	}
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		newMembershipInvitationResponse(invitation),
	)
}

func (handlers membershipInvitationHandlers) preview(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !handlers.auth.available(w, r) ||
		!handlers.allow(w, r, InvitationRateLimitPreview, false) {
		return
	}

	var request membershipInvitationTokenRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumMembershipInvitationRequestBytes,
	); err != nil {
		handlers.writeProblem(
			w,
			r,
			identity.ErrMembershipInvitationUnavailable,
			membershipInvitationTokenProblem,
		)
		return
	}
	preview, err := handlers.identity.PreviewMembershipInvitation(r.Context(), request.Token)
	if err != nil {
		handlers.writeProblem(w, r, err, membershipInvitationTokenProblem)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, membershipInvitationPreviewResponse{
		TenantName:   preview.TenantName,
		MaskedEmail:  preview.MaskedEmail,
		IntendedRole: preview.IntendedRole,
		Status:       preview.Status,
		ExpiresAt:    preview.ExpiresAt,
	})
}

func (handlers membershipInvitationHandlers) accept(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !handlers.auth.available(w, r) ||
		!handlers.allow(w, r, InvitationRateLimitAccept, true) {
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

	var request membershipInvitationTokenRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumMembershipInvitationRequestBytes,
	); err != nil {
		handlers.writeProblem(
			w,
			r,
			identity.ErrMembershipInvitationUnavailable,
			membershipInvitationTokenProblem,
		)
		return
	}
	result, err := handlers.identity.AcceptMembershipInvitation(
		r.Context(),
		principal,
		request.Token,
	)
	if err != nil {
		handlers.writeProblem(w, r, err, membershipInvitationTokenProblem)
		return
	}
	requestmeta.SetAuditTenant(r.Context(), result.Invitation.TenantID)
	writeJSON(handlers.logger, w, http.StatusOK, acceptMembershipInvitationResponse{
		Invitation:  newMembershipInvitationResponse(result.Invitation),
		CurrentUser: newMeResponse(result.Principal),
	})
}

func (handlers membershipInvitationHandlers) allow(
	w http.ResponseWriter,
	r *http.Request,
	action InvitationRateLimitAction,
	varyCookie bool,
) bool {
	clientPrefix := identity.IPPrefix(r.RemoteAddr)
	if clientPrefix == "" {
		clientPrefix = "unknown"
	}
	decision := handlers.rateLimiter.Allow(
		r.Context(),
		action,
		clientPrefix,
		handlers.clock().UTC(),
	)
	if decision.Allowed {
		return true
	}
	if varyCookie {
		w.Header().Set("Vary", "Cookie")
	}
	if decision.Err != nil {
		handlers.logger.Error(
			"membership invitation rate limiter unavailable",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error_class", "rate_limit_unavailable",
		)
		writeCodedProblem(
			w,
			r,
			http.StatusServiceUnavailable,
			"rate_limit_unavailable",
			"Invitation rate limit unavailable",
			"The invitation safety check is temporarily unavailable. Try again later.",
		)
		return false
	}
	w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
	writeCodedProblem(
		w,
		r,
		http.StatusTooManyRequests,
		"rate_limit_exceeded",
		"Too many invitation requests",
		"Wait before trying this invitation action again.",
	)
	return false
}

func (handlers membershipInvitationHandlers) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
	scope membershipInvitationProblemScope,
) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status := http.StatusInternalServerError
	title := "Invitation request failed"
	detail := "The membership invitation request could not be completed."

	switch {
	case errors.Is(err, identity.ErrAuthenticationDisabled):
		status = http.StatusServiceUnavailable
		title = "Authentication unavailable"
		detail = "Authentication is not configured for this environment."
	case errors.Is(err, identity.ErrSessionNotFound):
		status = http.StatusUnauthorized
		title = "Authentication required"
		detail = "The session is missing, expired, or has been revoked."
	case errors.Is(err, identity.ErrInvalidCSRFToken):
		status = http.StatusForbidden
		title = "Request verification failed"
		detail = "The request verification token is missing or invalid."
	case errors.Is(err, identity.ErrTenantNotFound):
		status = http.StatusNotFound
		title = "Workspace not found"
		detail = "The workspace does not exist in the active tenant scope."
	case errors.Is(err, identity.ErrTenantAccessDenied):
		status = http.StatusForbidden
		title = "Workspace access denied"
		detail = "Your active workspace membership does not allow this action."
	case errors.Is(err, identity.ErrMembershipInvitationIdentityMismatch):
		status = http.StatusForbidden
		title = "Invitation identity mismatch"
		detail = "Sign in with a verified identity matching the invited email address."
	case errors.Is(err, identity.ErrMembershipInvitationConflict):
		status = http.StatusConflict
		title = "Invitation state conflict"
		detail = "The invitation or membership state changed. Refresh and try again."
	case errors.Is(err, identity.ErrInvalidMembershipInvitation) &&
		scope == membershipInvitationAdminProblem:
		status = http.StatusBadRequest
		title = "Invalid invitation request"
		detail = "Provide a valid email address and intended organization role."
	case errors.Is(err, identity.ErrInvalidMembershipInvitation),
		errors.Is(err, identity.ErrMembershipInvitationUnavailable),
		errors.Is(err, identity.ErrMembershipInvitationExpired):
		status = http.StatusNotFound
		title = "Invitation unavailable"
		detail = "The invitation is invalid, unavailable, or no longer active."
	case errors.Is(err, identity.ErrRecentAuthenticationRequired):
		status = http.StatusForbidden
		title = "Recent authentication required"
		detail = "Sign in again before managing administrator invitations."
	}

	if status >= http.StatusInternalServerError {
		handlers.logger.Error(
			"membership invitation request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error_class", membershipInvitationErrorClass(err),
		)
	}
	writeProblem(w, r, status, title, detail)
}

func (handlers membershipInvitationHandlers) acceptURL(token string) string {
	return handlers.webOrigin + "/invite#token=" + url.QueryEscape(token)
}

func newMembershipInvitationResponse(
	invitation identity.MembershipInvitation,
) membershipInvitationResponse {
	return membershipInvitationResponse{
		ID:           invitation.ID,
		TenantID:     invitation.TenantID,
		Email:        invitation.Email,
		IntendedRole: invitation.IntendedRole,
		Status:       invitation.Status,
		ExpiresAt:    invitation.ExpiresAt,
		AcceptedAt:   invitation.AcceptedAt,
		RevokedAt:    invitation.RevokedAt,
		CreatedAt:    invitation.CreatedAt,
		UpdatedAt:    invitation.UpdatedAt,
	}
}

func parseInvitationPathUUID(value string) (uuid.UUID, bool) {
	identifier, err := uuid.Parse(value)
	return identifier, err == nil && identifier != uuid.Nil
}

func membershipInvitationErrorClass(err error) string {
	switch {
	case errors.Is(err, identity.ErrAuthenticationDisabled):
		return "authentication_unavailable"
	case errors.Is(err, identity.ErrSessionNotFound):
		return "session_unavailable"
	case errors.Is(err, identity.ErrInvalidCSRFToken):
		return "csrf_invalid"
	case errors.Is(err, identity.ErrTenantNotFound):
		return "tenant_not_found"
	case errors.Is(err, identity.ErrTenantAccessDenied):
		return "tenant_access_denied"
	case errors.Is(err, identity.ErrInvalidMembershipInvitation):
		return "invitation_invalid"
	case errors.Is(err, identity.ErrMembershipInvitationConflict):
		return "invitation_conflict"
	case errors.Is(err, identity.ErrMembershipInvitationUnavailable):
		return "invitation_unavailable"
	case errors.Is(err, identity.ErrMembershipInvitationExpired):
		return "invitation_expired"
	case errors.Is(err, identity.ErrMembershipInvitationIdentityMismatch):
		return "invitation_identity_mismatch"
	case errors.Is(err, identity.ErrRecentAuthenticationRequired):
		return "recent_authentication_required"
	default:
		return "internal"
	}
}
