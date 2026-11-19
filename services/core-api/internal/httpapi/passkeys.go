package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

// Attestation objects can include a certificate chain. Bound the JSON envelope
// before decoding binary fields or invoking the protocol parser.
const passkeyBodyLimit = 128 << 10

func (h nativeAuthHandlers) passkeyOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !slices.Contains(h.auth.config.Authentication.WebAuthnRPOrigins, origin) {
			writeProblem(w, r, http.StatusForbidden, "Forbidden", "The request origin is not allowed.")
			return
		}
		browserOriginGuard(origin, r.Method != http.MethodDelete, next).ServeHTTP(w, r)
	})
}

func (h nativeAuthHandlers) registerPasskeyRoutes(mux *http.ServeMux) {
	method := func(path, verb string, handler http.HandlerFunc) {
		var route http.Handler = handler
		if verb != http.MethodGet {
			route = h.passkeyOriginGuard(route)
		}
		if !h.auth.config.Authentication.PasskeysAvailable() {
			route = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { passkeyDisabled(w, r) })
		}
		mux.Handle(verb+" "+path, authFoundationResponseHeaders(route))
	}
	const auth = "/api/v1/auth/passkeys/assertion"
	const me = "/api/v1/me/passkeys"
	method(auth+"/options", "POST", h.passkeyOptions(identity.PasskeyLogin))
	method(auth+"/verify", "POST", h.passkeyLogin)
	method("/api/v1/auth/reauth/passkey/options", "POST", h.passkeyOptions(identity.PasskeyReauth))
	method(me, "GET", h.listPasskeys)
	method(me+"/registration/options", "POST", h.passkeyOptions(identity.PasskeyRegistration))
	method(me+"/registration/verify", "POST", h.registerPasskey)
	method(me+"/{passkey_id}", "PATCH", h.changePasskey)
	method(me+"/{passkey_id}", "DELETE", h.changePasskey)
}

func (h nativeAuthHandlers) reauthRoute() http.Handler {
	legacy := preAuthGuard(h.auth.config.WebOrigin, http.HandlerFunc(h.reauthenticate))
	route := legacy
	if h.auth.config.Authentication.PasskeysAvailable() {
		passkey := h.passkeyOriginGuard(http.HandlerFunc(h.reauthenticate))
		route = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Origin") == h.auth.config.WebOrigin {
				legacy.ServeHTTP(w, r)
				return
			}
			passkey.ServeHTTP(w, r)
		})
	}
	return authFoundationResponseHeaders(requireMethod(http.MethodPost, route))
}

func passkeyDisabled(w http.ResponseWriter, r *http.Request) {
	writeProblem(w, r, http.StatusNotFound, "Not found", "This authentication method is unavailable.")
}

func (h nativeAuthHandlers) passkeyService(w http.ResponseWriter, r *http.Request) (identity.PasskeyAPI, bool) {
	if !h.auth.config.Authentication.PasskeysAvailable() {
		passkeyDisabled(w, r)
		return nil, false
	}
	svc, ok := h.service.(identity.PasskeyAPI)
	if !ok {
		h.writeError(w, r, identity.ErrNativeMethodDisabled)
	}
	return svc, ok
}

func (h nativeAuthHandlers) passkeyPrincipal(w http.ResponseWriter, r *http.Request, mutation bool) (identity.Principal, bool) {
	if !h.auth.available(w, r) {
		return identity.Principal{}, false
	}
	if !mutation {
		return h.auth.authenticatedPrincipal(w, r)
	}
	token, ok := h.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return h.auth.csrfPrincipal(w, r, token)
}

func (h nativeAuthHandlers) passkeyBinding(w http.ResponseWriter, r *http.Request) (string, bool) {
	value, ok := singleCookieValue(r, identity.AuthTransactionCookieName(h.auth.config.Authentication.CookieSecure))
	if !ok || len(value) != 43 {
		h.writePasskeyError(w, r, identity.ErrInvalidPasskey)
		return "", false
	}
	return value, true
}

func (h nativeAuthHandlers) passkeyOptions(purpose identity.PasskeyPurpose) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc, ok := h.passkeyService(w, r)
		if !ok {
			return
		}
		var principal *identity.Principal
		if purpose == identity.PasskeyLogin {
			if !h.allowIP(w, r, AuthRateLimitPasskeyOptionsIP) {
				return
			}
		} else {
			p, ok := h.passkeyPrincipal(w, r, true)
			if !ok {
				return
			}
			if !h.allowSecurityMutation(w, r, p, AuthRateLimitReauthSendUser, AuthRateLimitReauthSendIP) {
				return
			}
			principal = &p
		}
		if decodeJSONRequest(w, r, &struct{}{}, 512) != nil {
			h.writeError(w, r, identity.ErrAuthFoundationInput)
			return
		}
		options, err := svc.BeginPasskey(r.Context(), purpose, principal)
		if err != nil {
			h.writePasskeyError(w, r, err)
			return
		}
		http.SetCookie(w, options.Cookie)
		writeJSON(h.auth.logger, w, http.StatusOK, options.Options)
	}
}

func (h nativeAuthHandlers) passkeyLogin(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.passkeyService(w, r)
	if !ok || !h.allowIP(w, r, AuthRateLimitPasskeyVerifyIP) {
		return
	}
	binding, ok := h.passkeyBinding(w, r)
	if !ok {
		return
	}
	var body struct {
		Assertion    json.RawMessage `json:"assertion"`
		KeepSignedIn bool            `json:"keep_signed_in,omitempty"`
	}
	if decodeJSONRequest(w, r, &body, passkeyBodyLimit) != nil {
		h.writePasskeyError(w, r, identity.ErrInvalidPasskey)
		return
	}
	result, err := svc.FinishPasskeyLogin(r.Context(), binding, body.Assertion, body.KeepSignedIn, r.UserAgent(), r.RemoteAddr)
	if err != nil {
		h.writePasskeyError(w, r, err)
		return
	}
	h.setSession(w, result)
	h.clearTransactionCookie(w)
	writeJSON(h.auth.logger, w, http.StatusOK, nativeAuthenticationResponse{Status: "authenticated"})
}

func (h nativeAuthHandlers) registerPasskey(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.passkeyService(w, r)
	if !ok {
		return
	}
	p, ok := h.passkeyPrincipal(w, r, true)
	if !ok {
		return
	}
	binding, ok := h.passkeyBinding(w, r)
	if !ok {
		return
	}
	var body struct {
		Attestation json.RawMessage `json:"attestation"`
		Label       string          `json:"label"`
	}
	if decodeJSONRequest(w, r, &body, passkeyBodyLimit) != nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := svc.FinishPasskeyRegistration(r.Context(), p, binding, body.Attestation, body.Label)
	if err != nil {
		h.writePasskeyError(w, r, err)
		return
	}
	h.clearTransactionCookie(w)
	writeJSON(h.auth.logger, w, http.StatusCreated, result)
}

func (h nativeAuthHandlers) listPasskeys(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.passkeyService(w, r)
	if !ok {
		return
	}
	p, ok := h.passkeyPrincipal(w, r, false)
	if !ok {
		return
	}
	items, err := svc.ListMyPasskeys(r.Context(), p)
	if err != nil {
		h.writePasskeyError(w, r, err)
		return
	}
	if items == nil {
		items = []identity.Passkey{}
	}
	writeJSON(h.auth.logger, w, http.StatusOK, struct {
		Passkeys []identity.Passkey `json:"passkeys"`
	}{items})
}

func (h nativeAuthHandlers) changePasskey(w http.ResponseWriter, r *http.Request) {
	svc, ok := h.passkeyService(w, r)
	if !ok {
		return
	}
	p, ok := h.passkeyPrincipal(w, r, true)
	if !ok {
		return
	}
	id, ok := parseResourceUUID(r.PathValue("passkey_id"))
	if !ok {
		h.writePasskeyError(w, r, identity.ErrPasskeyNotFound)
		return
	}
	if r.Method == http.MethodDelete {
		if err := svc.RevokeMyPasskey(r.Context(), p, id); err != nil {
			h.writePasskeyError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if decodeJSONRequest(w, r, &body, 2048) != nil {
		h.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := svc.RenameMyPasskey(r.Context(), p, id, body.Label)
	if err != nil {
		h.writePasskeyError(w, r, err)
		return
	}
	writeJSON(h.auth.logger, w, http.StatusOK, result)
}

func (h nativeAuthHandlers) writePasskeyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrPasskeyUnknown), errors.Is(err, identity.ErrInvalidPasskey):
		writeProblemWithSignal(w, r, 401, "auth_invalid_passkey", "Verification failed", "Unable to verify this passkey.", errors.Is(err, identity.ErrPasskeyUnknown))
	case errors.Is(err, identity.ErrPasskeyClone):
		writeCodedProblem(w, r, 403, "auth_passkey_clone_detected", "Verification failed", "This passkey could not be accepted.")
	case errors.Is(err, identity.ErrPasskeyConflict):
		writeCodedProblem(w, r, 409, "auth_passkey_exists", "Passkey already registered", "This passkey cannot be registered again.")
	case errors.Is(err, identity.ErrLastIdentity):
		writeCodedProblem(w, r, 409, "auth_last_identity", "Passkey cannot be removed", "Keep at least one sign-in method on your account.")
	case errors.Is(err, identity.ErrPasskeyNotFound):
		writeCodedProblem(w, r, 404, "auth_passkey_not_found", "Passkey unavailable", "The requested passkey is unavailable.")
	case errors.Is(err, identity.ErrRecentAuthenticationRequired):
		writeCodedProblem(w, r, 403, "auth_recent_authentication_required", "Recent authentication required", "Verify your identity before changing passkeys.")
	default:
		h.writeError(w, r, err)
	}
}
