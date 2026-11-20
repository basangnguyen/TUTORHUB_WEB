package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func (h nativeAuthHandlers) registerMFARoutes(mux *http.ServeMux) {
	const auth = "/api/v1/auth/mfa"
	const me = "/api/v1/me/mfa"
	register := func(method, path string, next http.HandlerFunc) {
		var handler http.Handler = next
		if method != http.MethodGet {
			handler = browserOriginGuard(h.auth.config.WebOrigin, method != http.MethodDelete, handler)
		}
		if !h.auth.config.Authentication.MFATOTPEnabled {
			handler = http.HandlerFunc(mfaUnavailable)
		}
		mux.Handle(method+" "+path, authFoundationResponseHeaders(handler))
	}
	register("GET", auth, h.pendingMFA)
	register("POST", auth+"/passkey/options", h.mfaPasskeyOptions)
	register("POST", auth+"/verify", h.verifyMFA)
	register("POST", me+"/totp/enroll", h.enrollTOTP)
	register("POST", me+"/totp/confirm", h.confirmTOTP)
	register("DELETE", me+"/totp", h.disableTOTP)
	register("POST", me+"/recovery-codes", h.regenerateRecoveryCodes)
}

func mfaUnavailable(w http.ResponseWriter, r *http.Request) {
	writeCodedProblem(w, r, 404, "auth_mfa_unavailable", "Verification unavailable", "There is no available verification request.")
}

func (h nativeAuthHandlers) mfaService(w http.ResponseWriter, r *http.Request) (identity.MFAAPI, bool) {
	service, ok := h.service.(identity.MFAAPI)
	if !ok {
		h.writeError(w, r, identity.ErrNativeMethodDisabled)
	}
	return service, ok
}

func (h nativeAuthHandlers) mfaBinding(w http.ResponseWriter, r *http.Request) (string, bool) {
	value, ok := singleCookieValue(r, identity.AuthTransactionCookieName(h.auth.config.Authentication.CookieSecure))
	if !ok || len(value) != 43 {
		mfaUnavailable(w, r)
		return "", false
	}
	return value, true
}

func (h nativeAuthHandlers) writeMFAError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrAuthTransactionUnavailable):
		mfaUnavailable(w, r)
	case errors.Is(err, identity.ErrMFAConflict):
		writeCodedProblem(w, r, 409, "auth_mfa_conflict", "Factor state changed", "Reload account security and try again.")
	case errors.Is(err, identity.ErrInvalidPasskey), errors.Is(err, identity.ErrPasskeyUnknown):
		h.writeError(w, r, identity.ErrNativeInvalidCode)
	case errors.Is(err, identity.ErrPasskeyClone):
		h.writePasskeyError(w, r, err)
	case errors.Is(err, identity.ErrSessionNotFound), errors.Is(err, identity.ErrSessionContextConflict):
		h.auth.writeIdentityProblem(w, r, err)
	default:
		h.writeError(w, r, err)
	}
}

func (h nativeAuthHandlers) pendingMFA(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.mfaService(w, r)
	if !ok {
		return
	}
	binding, ok := h.mfaBinding(w, r)
	if !ok {
		return
	}
	state, err := svc.PendingMFA(r.Context(), binding)
	if err != nil {
		h.writeMFAError(w, r, err)
		return
	}
	writeJSON(h.auth.logger, w, 200, state)
}

func (h nativeAuthHandlers) mfaPasskeyOptions(w http.ResponseWriter, r *http.Request) {
	if !h.auth.config.Authentication.PasskeysAvailable() {
		mfaUnavailable(w, r)
		return
	}
	svc, ok := h.mfaService(w, r)
	if !ok || !h.allowIP(w, r, AuthRateLimitPasskeyOptionsIP) {
		return
	}
	binding, ok := h.mfaBinding(w, r)
	if !ok {
		return
	}
	if decodeJSONRequest(w, r, &struct{}{}, 512) != nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	options, err := svc.MFAPasskeyOptions(r.Context(), binding)
	if err != nil {
		h.writeMFAError(w, r, err)
		return
	}
	writeJSON(h.auth.logger, w, 200, options)
}

func (h nativeAuthHandlers) verifyMFA(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.mfaService(w, r)
	if !ok || !h.allowIP(w, r, AuthRateLimitMFAIP) {
		return
	}
	binding, ok := h.mfaBinding(w, r)
	if !ok {
		return
	}
	var body struct {
		Method    string          `json:"method"`
		Code      *string         `json:"code"`
		Assertion json.RawMessage `json:"assertion"`
	}
	if decodeJSONRequest(w, r, &body, passkeyBodyLimit) != nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	input := identity.MFAVerifyInput{Method: body.Method, UserAgent: r.UserAgent(), RemoteAddress: r.RemoteAddr}
	switch body.Method {
	case "totp", "recovery_code":
		if body.Code == nil || body.Assertion != nil {
			h.writeError(w, r, identity.ErrAuthFoundationInput)
			return
		}
		input.Code = *body.Code
	case "passkey":
		if body.Code != nil || len(body.Assertion) == 0 || body.Assertion[0] != '{' || !h.auth.config.Authentication.PasskeysAvailable() {
			h.writeError(w, r, identity.ErrAuthFoundationInput)
			return
		}
		input.Assertion = body.Assertion
	default:
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	state, err := svc.PendingMFA(r.Context(), binding)
	if err != nil {
		h.writeMFAError(w, r, err)
		return
	}
	if state.UserID == uuid.Nil {
		h.writeError(w, r, identity.ErrAuthFoundationStorage)
		return
	}
	if !h.allowMFAAccount(w, r, state.UserID) {
		return
	}
	result, err := svc.VerifyMFA(r.Context(), binding, input)
	if err != nil {
		h.writeMFAError(w, r, err)
		return
	}
	h.clearTransactionCookie(w)
	if result.PasswordReset {
		h.auth.clearCookie(w, h.auth.cookieNames.session)
		h.auth.clearCookie(w, h.auth.cookieNames.csrf)
		writeJSON(h.auth.logger, w, 200, nativeAuthenticationResponse{Status: "password_reset"})
		return
	}
	h.setSession(w, result.Session)
	writeJSON(h.auth.logger, w, 200, struct {
		Status   string `json:"status"`
		ReturnTo string `json:"return_to,omitempty"`
	}{"authenticated", result.ReturnTo})
}

func (h nativeAuthHandlers) allowMFAAccount(w http.ResponseWriter, r *http.Request, userID uuid.UUID) bool {
	decision := h.limiter.Allow(r.Context(), AuthRateLimitMFAUser, userID.String(), h.auth.clock())
	if decision.Err != nil {
		h.writeError(w, r, identity.ErrAuthFoundationStorage)
		return false
	}
	if !decision.Allowed {
		w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
		writeProblem(w, r, http.StatusTooManyRequests, "Too many requests", "Please wait before trying again.")
	}
	return decision.Allowed
}

func (h nativeAuthHandlers) mfaPrincipal(w http.ResponseWriter, r *http.Request) (identity.MFAAPI, identity.Principal, bool) {
	svc, ok := h.mfaService(w, r)
	if !ok {
		return nil, identity.Principal{}, false
	}
	p, ok := h.passkeyPrincipal(w, r, true)
	return svc, p, ok
}

func (h nativeAuthHandlers) enrollTOTP(w http.ResponseWriter, r *http.Request) {
	svc, p, ok := h.mfaPrincipal(w, r)
	if !ok {
		return
	}
	if decodeJSONRequest(w, r, &struct{}{}, 512) != nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := svc.EnrollTOTP(r.Context(), p)
	if err != nil {
		h.writeMFAError(w, r, err)
		return
	}
	writeJSON(h.auth.logger, w, 200, result)
}

type recoveryCodesResponse struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func (h nativeAuthHandlers) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	svc, p, ok := h.mfaPrincipal(w, r)
	if !ok || !h.allowSecurityMutation(w, r, p, AuthRateLimitMFAUser, AuthRateLimitMFAIP) {
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if decodeJSONRequest(w, r, &body, 512) != nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := svc.ConfirmTOTP(r.Context(), p, body.Code)
	if err != nil {
		h.writeMFAError(w, r, err)
		return
	}
	h.auth.setCookie(w, h.auth.cookieNames.session, result.Session.SessionToken, result.Session.CookieExpiresAt)
	h.auth.setCookie(w, h.auth.cookieNames.csrf, result.Session.CSRFToken, result.Session.CookieExpiresAt)
	writeJSON(h.auth.logger, w, 200, recoveryCodesResponse{result.RecoveryCodes})
}

func (h nativeAuthHandlers) disableTOTP(w http.ResponseWriter, r *http.Request) {
	svc, p, ok := h.mfaPrincipal(w, r)
	if !ok {
		return
	}
	if err := svc.DisableTOTP(r.Context(), p); err != nil {
		h.writeMFAError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h nativeAuthHandlers) regenerateRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	svc, p, ok := h.mfaPrincipal(w, r)
	if !ok {
		return
	}
	if decodeJSONRequest(w, r, &struct{}{}, 512) != nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	codes, err := svc.RegenerateRecoveryCodes(r.Context(), p)
	if err != nil {
		h.writeMFAError(w, r, err)
		return
	}
	writeJSON(h.auth.logger, w, 200, recoveryCodesResponse{codes})
}
