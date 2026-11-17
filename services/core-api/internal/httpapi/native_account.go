package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type emailCodeVerifyRequest struct {
	ChallengeID  uuid.UUID `json:"challenge_id"`
	Code         string    `json:"code"`
	KeepSignedIn bool      `json:"keep_signed_in,omitempty"`
}

type passwordRecoveryCompleteRequest struct {
	ChallengeID uuid.UUID `json:"challenge_id"`
	Code        string    `json:"code"`
	NewPassword string    `json:"new_password"`
}

type myPasswordUpdateRequest struct {
	CurrentPassword *string `json:"current_password,omitempty"`
	NewPassword     string  `json:"new_password"`
}

type myPasswordStatusResponse struct {
	PasswordEnabled bool       `json:"password_enabled"`
	HasPassword     bool       `json:"has_password"`
	UpdatedAt       *time.Time `json:"updated_at,omitempty"`
}

type myPasswordUpdateResponse struct {
	HasPassword bool      `json:"has_password"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (handlers nativeAuthHandlers) accountService(w http.ResponseWriter, r *http.Request, enabled bool) (identity.NativeAccountAuthAPI, bool) {
	if !handlers.available(w, r, enabled) {
		return nil, false
	}
	service, ok := handlers.service.(identity.NativeAccountAuthAPI)
	if !ok {
		handlers.writeError(w, r, identity.ErrNativeMethodDisabled)
	}
	return service, ok
}

func (handlers nativeAuthHandlers) startEmailCodeLogin(w http.ResponseWriter, r *http.Request) {
	handlers.startAccountEmail(w, r, false)
}

func (handlers nativeAuthHandlers) startPasswordRecovery(w http.ResponseWriter, r *http.Request) {
	handlers.startAccountEmail(w, r, true)
}

func (handlers nativeAuthHandlers) startAccountEmail(w http.ResponseWriter, r *http.Request, recovery bool) {
	auth := handlers.auth.config.Authentication
	service, ok := handlers.accountService(w, r, auth.EmailCodeEnabled && (!recovery || auth.PasswordEnabled))
	if !ok || !handlers.allowIP(w, r, AuthRateLimitOTPSendIP) {
		return
	}
	var body emailSignupStartRequest
	if err := decodeJSONRequest(w, r, &body, 2048); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	email, err := identity.NormalizeSignupEmail(body.Email)
	if err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	allowed, err := handlers.allowDestination(r.Context(), email)
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	input := identity.SignupStartInput{Email: email, RemoteAddress: r.RemoteAddr, DestinationAllowed: allowed}
	var result identity.SignupChallengeResult
	if recovery {
		result, err = service.StartPasswordRecovery(r.Context(), input)
	} else {
		result, err = service.StartEmailCodeLogin(r.Context(), input)
	}
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	handlers.writeChallenge(w, result)
}

func (handlers nativeAuthHandlers) verifyEmailCodeLogin(w http.ResponseWriter, r *http.Request) {
	service, ok := handlers.accountService(w, r, handlers.auth.config.Authentication.EmailCodeEnabled)
	if !ok || !handlers.allowIP(w, r, AuthRateLimitOTPVerifyIP) {
		return
	}
	var input emailCodeVerifyRequest
	if err := decodeJSONRequest(w, r, &input, 2048); err != nil || input.ChallengeID == uuid.Nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	binding, ok := handlers.accountTransactionBinding(w, r)
	if !ok {
		return
	}
	result, err := service.VerifyEmailCodeLogin(r.Context(), binding, identity.EmailCodeVerifyInput{ChallengeID: input.ChallengeID, Code: input.Code, KeepSignedIn: input.KeepSignedIn, UserAgent: r.UserAgent(), RemoteAddress: r.RemoteAddr})
	if err != nil {
		handlers.writeAccountCodeError(w, r, err)
		return
	}
	handlers.setSession(w, result)
	handlers.clearTransactionCookie(w)
	writeJSON(handlers.auth.logger, w, http.StatusOK, nativeAuthenticationResponse{Status: "authenticated"})
}

func (handlers nativeAuthHandlers) completePasswordRecovery(w http.ResponseWriter, r *http.Request) {
	auth := handlers.auth.config.Authentication
	service, ok := handlers.accountService(w, r, auth.PasswordEnabled && auth.EmailCodeEnabled)
	if !ok || !handlers.allowIP(w, r, AuthRateLimitOTPVerifyIP) {
		return
	}
	var input passwordRecoveryCompleteRequest
	if err := decodeJSONRequest(w, r, &input, 4096); err != nil || input.ChallengeID == uuid.Nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	binding, ok := handlers.accountTransactionBinding(w, r)
	if !ok {
		return
	}
	err := service.CompletePasswordRecovery(r.Context(), binding, identity.PasswordRecoveryCompleteInput{ChallengeID: input.ChallengeID, Code: input.Code, NewPassword: input.NewPassword})
	if err != nil {
		handlers.writeAccountCodeError(w, r, err)
		return
	}
	handlers.clearTransactionCookie(w)
	handlers.auth.clearCookie(w, handlers.auth.cookieNames.session)
	handlers.auth.clearCookie(w, handlers.auth.cookieNames.csrf)
	writeJSON(handlers.auth.logger, w, http.StatusOK, nativeAuthenticationResponse{Status: "password_reset"})
}

// Verification failures, including missing/ambiguous cookies, have one response.
func (handlers nativeAuthHandlers) accountTransactionBinding(w http.ResponseWriter, r *http.Request) (string, bool) {
	var binding string
	count := 0
	name := identity.AuthTransactionCookieName(handlers.auth.config.Authentication.CookieSecure)
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			binding = cookie.Value
			count++
		}
	}
	if count != 1 || len(binding) != 43 {
		handlers.writeError(w, r, identity.ErrNativeInvalidCode)
		return "", false
	}
	return binding, true
}

func (handlers nativeAuthHandlers) writeAccountCodeError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, identity.ErrAuthTransactionUnavailable) || errors.Is(err, identity.ErrNativeSignupUnavailable) {
		err = identity.ErrNativeInvalidCode
	}
	handlers.writeError(w, r, err)
}

func (handlers nativeAuthHandlers) myPassword(w http.ResponseWriter, r *http.Request) {
	setPrivateResponseHeaders(w)
	w.Header().Set("Cache-Control", "private, no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "The requested resource does not support this HTTP method.")
		return
	}
	if !handlers.auth.available(w, r) {
		return
	}
	service, ok := handlers.accountService(w, r, r.Method != http.MethodPost || handlers.auth.config.Authentication.PasswordEnabled)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		principal, ok := handlers.auth.authenticatedPrincipal(w, r)
		if !ok {
			return
		}
		result, err := service.GetMyPasswordStatus(r.Context(), principal)
		if err != nil {
			handlers.writeError(w, r, err)
			return
		}
		writeJSON(handlers.auth.logger, w, http.StatusOK, myPasswordStatusResponse{PasswordEnabled: handlers.auth.config.Authentication.PasswordEnabled, HasPassword: result.HasPassword, UpdatedAt: result.UpdatedAt})
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
	var input myPasswordUpdateRequest
	if err := decodeJSONRequest(w, r, &input, 4096); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := service.UpdateMyPassword(r.Context(), principal, identity.UpdateMyPasswordInput{CurrentPassword: input.CurrentPassword, NewPassword: input.NewPassword})
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	handlers.auth.setCookie(w, handlers.auth.cookieNames.session, result.SessionToken, result.CookieExpiresAt)
	handlers.auth.setCookie(w, handlers.auth.cookieNames.csrf, result.CSRFToken, result.CookieExpiresAt)
	writeJSON(handlers.auth.logger, w, http.StatusOK, myPasswordUpdateResponse{HasPassword: result.HasPassword, UpdatedAt: result.UpdatedAt})
}
