package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type reauthenticateRequest struct {
	Assertion   json.RawMessage `json:"assertion,omitempty"`
	Method      string          `json:"method"`
	Password    *string         `json:"password,omitempty"`
	ChallengeID *uuid.UUID      `json:"challenge_id,omitempty"`
	Code        *string         `json:"code,omitempty"`
}

type reauthenticateResponse struct {
	RecentAuthExpiresAt time.Time `json:"recent_auth_expires_at"`
}

type mySessionsResponse struct {
	Sessions []identity.MySession `json:"sessions"`
}

type revokeOtherSessionsResponse struct {
	RevokedCount int `json:"revoked_count"`
}

func (handlers nativeAuthHandlers) securityPrincipal(w http.ResponseWriter, r *http.Request, mutation bool) (identity.NativeSecurityAPI, identity.Principal, bool) {
	return handlers.securityPrincipalEnabled(w, r, mutation, handlers.auth.config.Authentication.DeviceManagementEnabled)
}

func (handlers nativeAuthHandlers) securityPrincipalEnabled(w http.ResponseWriter, r *http.Request, mutation, enabled bool) (identity.NativeSecurityAPI, identity.Principal, bool) {
	setPrivateResponseHeaders(w)
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !handlers.auth.available(w, r) || !handlers.available(w, r, enabled) {
		return nil, identity.Principal{}, false
	}
	service, ok := handlers.service.(identity.NativeSecurityAPI)
	if !ok {
		handlers.writeError(w, r, identity.ErrNativeMethodDisabled)
		return nil, identity.Principal{}, false
	}
	var principal identity.Principal
	if mutation {
		var token string
		token, ok = handlers.auth.sessionToken(w, r)
		if ok {
			principal, ok = handlers.auth.csrfPrincipal(w, r, token)
		}
	} else {
		principal, ok = handlers.auth.authenticatedPrincipal(w, r)
	}
	return service, principal, ok
}

func (handlers nativeAuthHandlers) allowSecurityMutation(w http.ResponseWriter, r *http.Request, principal identity.Principal, userAction, ipAction InvitationRateLimitAction) bool {
	if !handlers.allowIP(w, r, ipAction) {
		return false
	}
	decision := handlers.limiter.Allow(r.Context(), userAction, principal.User.ID.String(), handlers.auth.clock())
	if decision.Err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationStorage)
		return false
	}
	if !decision.Allowed {
		w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
		writeProblem(w, r, http.StatusTooManyRequests, "Too many requests", "Please wait before trying again.")
		return false
	}
	return true
}

func (handlers nativeAuthHandlers) mySecurity(w http.ResponseWriter, r *http.Request) {
	auth := handlers.auth.config.Authentication
	service, principal, ok := handlers.securityPrincipalEnabled(w, r, false, auth.DeviceManagementEnabled || auth.MFATOTPEnabled || auth.PhoneEnabled)
	if !ok {
		return
	}
	result, err := service.GetMySecurity(r.Context(), principal)
	if err != nil {
		handlers.writeSecurityError(w, r, err)
		return
	}
	writeJSON(handlers.auth.logger, w, http.StatusOK, result)
}

func (handlers nativeAuthHandlers) myLoginMethods(w http.ResponseWriter, r *http.Request) {
	auth := handlers.auth.config.Authentication
	service, principal, ok := handlers.securityPrincipalEnabled(w, r, false, auth.DeviceManagementEnabled || auth.PhoneEnabled)
	if !ok {
		return
	}
	result, err := service.ListMyLoginMethods(r.Context(), principal)
	if err != nil {
		handlers.writeSecurityError(w, r, err)
		return
	}
	writeJSON(handlers.auth.logger, w, http.StatusOK, result)
}

func (handlers nativeAuthHandlers) mySessions(w http.ResponseWriter, r *http.Request) {
	service, principal, ok := handlers.securityPrincipal(w, r, false)
	if !ok {
		return
	}
	result, err := service.ListMySessions(r.Context(), principal)
	if err != nil {
		handlers.writeSecurityError(w, r, err)
		return
	}
	if result == nil {
		result = []identity.MySession{}
	}
	writeJSON(handlers.auth.logger, w, http.StatusOK, mySessionsResponse{Sessions: result})
}

func (handlers nativeAuthHandlers) startReauthEmailCode(w http.ResponseWriter, r *http.Request) {
	service, principal, ok := handlers.securityPrincipal(w, r, true)
	if !ok {
		return
	}
	if !handlers.allowSecurityMutation(w, r, principal, AuthRateLimitReauthSendUser, AuthRateLimitReauthSendIP) {
		return
	}
	if err := decodeJSONRequest(w, r, &struct{}{}, 512); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	// A previous valid cookie allows resending within the same bounded transaction.
	// Ambiguous cookies are rejected; absent/expired cookies may start a new flow.
	binding := ""
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == identity.AuthTransactionCookieName(handlers.auth.config.Authentication.CookieSecure) {
			binding = cookie.Value
			count++
		}
	}
	if count > 1 || count == 1 && len(binding) != 43 {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := service.StartReauthEmailCode(r.Context(), principal, binding, handlers.allowDestination)
	if err != nil {
		handlers.writeSecurityError(w, r, err)
		return
	}
	handlers.writeChallenge(w, result)
}

func (handlers nativeAuthHandlers) reauthenticate(w http.ResponseWriter, r *http.Request) {
	var body reauthenticateRequest
	limit := int64(4096)
	if handlers.auth.config.Authentication.PasskeysAvailable() {
		limit = passkeyBodyLimit
	}
	if err := decodeJSONRequest(w, r, &body, limit); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	if body.Method == "passkey" {
		handlers.reauthenticatePasskey(w, r, body)
		return
	}
	if r.Header.Get("Origin") != handlers.auth.config.WebOrigin {
		writeProblem(w, r, http.StatusForbidden, "Forbidden", "The request origin is not allowed.")
		return
	}
	auth := handlers.auth.config.Authentication
	enabled := auth.DeviceManagementEnabled
	userRate, ipRate := AuthRateLimitReauthUser, AuthRateLimitReauthIP
	if body.Method == "totp" {
		enabled = auth.MFATOTPEnabled
		userRate, ipRate = AuthRateLimitMFAUser, AuthRateLimitMFAIP
	}
	service, principal, ok := handlers.securityPrincipalEnabled(w, r, true, enabled)
	if !ok {
		return
	}
	if !handlers.allowSecurityMutation(w, r, principal, userRate, ipRate) {
		return
	}
	if body.Assertion != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	input := identity.ReauthenticateInput{Method: body.Method}
	binding := ""
	switch body.Method {
	case "totp":
		if body.Password != nil || body.ChallengeID != nil || body.Code == nil || !validTOTPRequest(*body.Code) {
			handlers.writeError(w, r, identity.ErrAuthFoundationInput)
			return
		}
		input.Code = *body.Code
	case "password":
		if body.Password == nil || body.ChallengeID != nil || body.Code != nil {
			handlers.writeError(w, r, identity.ErrAuthFoundationInput)
			return
		}
		input.Password = *body.Password
	case "email_code":
		if body.Password != nil || body.ChallengeID == nil || *body.ChallengeID == uuid.Nil || body.Code == nil {
			handlers.writeError(w, r, identity.ErrAuthFoundationInput)
			return
		}
		input.ChallengeID = *body.ChallengeID
		input.Code = *body.Code
		binding, ok = handlers.accountTransactionBinding(w, r)
		if !ok {
			return
		}
	default:
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := service.Reauthenticate(r.Context(), principal, binding, input)
	if err != nil {
		handlers.writeSecurityError(w, r, err)
		return
	}
	if body.Method == "email_code" {
		handlers.clearTransactionCookie(w)
	}
	handlers.writeReauthentication(w, result)
}

func validTOTPRequest(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func (h nativeAuthHandlers) reauthenticatePasskey(w http.ResponseWriter, r *http.Request, body reauthenticateRequest) {
	svc, ok := h.passkeyService(w, r)
	if !ok {
		return
	}
	if !slices.Contains(h.auth.config.Authentication.WebAuthnRPOrigins, r.Header.Get("Origin")) {
		writeProblem(w, r, http.StatusForbidden, "Forbidden", "The request origin is not allowed.")
		return
	}
	p, ok := h.passkeyPrincipal(w, r, true)
	if !ok || !h.allowSecurityMutation(w, r, p, AuthRateLimitReauthUser, AuthRateLimitReauthIP) {
		return
	}
	if body.Password != nil || body.Code != nil || body.ChallengeID != nil || len(body.Assertion) == 0 {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	binding, ok := h.passkeyBinding(w, r)
	if !ok {
		return
	}
	result, err := svc.FinishPasskeyReauth(r.Context(), p, binding, body.Assertion)
	if err != nil {
		h.writePasskeyError(w, r, err)
		return
	}
	h.clearTransactionCookie(w)
	h.writeReauthentication(w, result)
}

func (h nativeAuthHandlers) writeReauthentication(w http.ResponseWriter, result identity.ReauthenticateResult) {
	h.auth.setCookie(w, h.auth.cookieNames.session, result.SessionToken, result.CookieExpiresAt)
	h.auth.setCookie(w, h.auth.cookieNames.csrf, result.CSRFToken, result.CookieExpiresAt)
	writeJSON(h.auth.logger, w, http.StatusOK, reauthenticateResponse{RecentAuthExpiresAt: result.RecentAuthExpiresAt})
}

func (handlers nativeAuthHandlers) revokeMySession(w http.ResponseWriter, r *http.Request) {
	service, principal, ok := handlers.securityPrincipal(w, r, true)
	if !ok {
		return
	}
	if !handlers.allowSecurityMutation(w, r, principal, AuthRateLimitSessionRevokeUser, AuthRateLimitSessionRevokeIP) {
		return
	}
	id, ok := parseResourceUUID(r.PathValue("session_id"))
	if !ok {
		handlers.writeSecurityError(w, r, identity.ErrManagedSessionNotFound)
		return
	}
	if err := decodeJSONRequest(w, r, &struct{}{}, 512); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	if err := service.RevokeMySession(r.Context(), principal, id); err != nil {
		handlers.writeSecurityError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handlers nativeAuthHandlers) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	service, principal, ok := handlers.securityPrincipal(w, r, true)
	if !ok {
		return
	}
	if !handlers.allowSecurityMutation(w, r, principal, AuthRateLimitSessionRevokeUser, AuthRateLimitSessionRevokeIP) {
		return
	}
	if err := decodeJSONRequest(w, r, &struct{}{}, 512); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	count, err := service.RevokeOtherSessions(r.Context(), principal)
	if err != nil {
		handlers.writeSecurityError(w, r, err)
		return
	}
	writeJSON(handlers.auth.logger, w, http.StatusOK, revokeOtherSessionsResponse{RevokedCount: count})
}

func (handlers nativeAuthHandlers) writeSecurityError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrCannotRevokeCurrentSession):
		writeCodedProblem(w, r, http.StatusBadRequest, "auth_cannot_revoke_current_session", "Cannot revoke current session", "Use logout to end the current session.")
	case errors.Is(err, identity.ErrManagedSessionNotFound):
		writeProblem(w, r, http.StatusNotFound, "Session unavailable", "The requested session is unavailable.")
	case errors.Is(err, identity.ErrAuthTransactionUnavailable):
		handlers.writeError(w, r, identity.ErrNativeInvalidCode)
	default:
		handlers.writeError(w, r, err)
	}
}
