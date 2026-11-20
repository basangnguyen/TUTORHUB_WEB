package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type nativeAuthHandlers struct {
	auth    authHandlers
	service identity.NativeAuthAPI
	limiter InvitationRateLimiter
}

type emailSignupStartRequest struct {
	Email string `json:"email"`
}

type emailSignupCompleteRequest struct {
	ChallengeID  uuid.UUID `json:"challenge_id"`
	Code         string    `json:"code"`
	DisplayName  string    `json:"display_name"`
	Password     string    `json:"password"`
	TermsVersion string    `json:"terms_version"`
	KeepSignedIn bool      `json:"keep_signed_in,omitempty"`
}

type passwordLoginRequest struct {
	Identifier   string `json:"identifier"`
	Password     string `json:"password"`
	KeepSignedIn bool   `json:"keep_signed_in,omitempty"`
}

type signupChallengeResponse struct {
	ChallengeID        uuid.UUID `json:"challenge_id"`
	CooldownSeconds    int       `json:"cooldown_seconds"`
	ExpiresInSeconds   int       `json:"expires_in_seconds"`
	MaskedDestination  string    `json:"masked_destination,omitempty"`
	ResendAfterSeconds int       `json:"resend_after_seconds,omitempty"`
}

type nativeAuthenticationResponse struct {
	Status  string   `json:"status"`
	Methods []string `json:"methods,omitempty"`
}

func (handlers nativeAuthHandlers) available(w http.ResponseWriter, r *http.Request, enabled bool) bool {
	if enabled && handlers.service != nil {
		return true
	}
	writeProblem(w, r, http.StatusServiceUnavailable, "Authentication unavailable", "This authentication method is disabled or unavailable.")
	return false
}

func (handlers nativeAuthHandlers) allowIP(w http.ResponseWriter, r *http.Request, actions ...InvitationRateLimitAction) bool {
	prefix := identity.IPPrefix(r.RemoteAddr)
	if prefix == "" {
		prefix = "unknown"
	}
	for _, action := range actions {
		decision := handlers.limiter.Allow(r.Context(), action, prefix, handlers.auth.clock())
		if decision.Err != nil {
			handlers.writeError(w, r, identity.ErrAuthFoundationStorage)
			return false
		}
		if !decision.Allowed {
			w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
			writeProblem(w, r, http.StatusTooManyRequests, "Too many requests", "Please wait before trying again.")
			return false
		}
	}
	return true
}

func (handlers nativeAuthHandlers) allowDestination(ctx context.Context, normalizedEmail string) (bool, error) {
	decision := handlers.limiter.Allow(ctx, AuthRateLimitOTPSendDestination, normalizedEmail, handlers.auth.clock())
	if decision.Err != nil {
		return false, identity.ErrAuthFoundationStorage
	}
	return decision.Allowed, nil
}

func (handlers nativeAuthHandlers) startEmailSignup(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r, handlers.auth.config.Authentication.SignupEnabled) ||
		!handlers.allowIP(w, r, AuthRateLimitSignupIP, AuthRateLimitOTPSendIP) {
		return
	}
	var input emailSignupStartRequest
	if err := decodeJSONRequest(w, r, &input, 2048); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	email, err := identity.NormalizeSignupEmail(input.Email)
	if err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	allowed, err := handlers.allowDestination(r.Context(), email)
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	result, err := handlers.service.StartEmailSignup(r.Context(), identity.SignupStartInput{
		Email: email, RemoteAddress: r.RemoteAddr, DestinationAllowed: allowed,
	})
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	handlers.writeChallenge(w, result)
}

func (handlers nativeAuthHandlers) resendChallenge(w http.ResponseWriter, r *http.Request) {
	auth := handlers.auth.config.Authentication
	if !handlers.available(w, r, auth.SignupEnabled || auth.EmailCodeEnabled || auth.PhoneEnabled) ||
		!handlers.allowIP(w, r, AuthRateLimitOTPSendIP) {
		return
	}
	// An empty object prevents changing the encrypted destination mid-transaction.
	if err := decodeJSONRequest(w, r, &struct{}{}, 512); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	binding, ok := handlers.transactionBinding(w, r)
	if !ok {
		return
	}
	if auth.PhoneEnabled && handlers.tryPhoneResend(w, r, binding) {
		return
	}
	service, ok := handlers.accountService(w, r, auth.SignupEnabled || auth.EmailCodeEnabled)
	if !ok {
		return
	}
	result, err := service.ResendEmailChallenge(r.Context(), binding, identity.AuthMethodFlags{
		SignupEnabled: auth.SignupEnabled, EmailCodeEnabled: auth.EmailCodeEnabled, PasswordEnabled: auth.PasswordEnabled,
	}, handlers.allowDestination)
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	handlers.writeChallenge(w, result)
}

func (handlers nativeAuthHandlers) completeEmailSignup(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r, handlers.auth.config.Authentication.SignupEnabled) ||
		!handlers.allowIP(w, r, AuthRateLimitOTPVerifyIP) {
		return
	}
	var input emailSignupCompleteRequest
	if err := decodeJSONRequest(w, r, &input, 4096); err != nil || input.ChallengeID == uuid.Nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	binding, ok := handlers.transactionBinding(w, r)
	if !ok {
		return
	}
	result, err := handlers.service.CompleteEmailSignup(r.Context(), binding, identity.SignupCompleteInput{
		ChallengeID: input.ChallengeID, Code: input.Code, DisplayName: input.DisplayName, Password: input.Password,
		TermsVersion: input.TermsVersion, KeepSignedIn: input.KeepSignedIn, UserAgent: r.UserAgent(), RemoteAddress: r.RemoteAddr,
	})
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	handlers.setSession(w, result)
	handlers.clearTransactionCookie(w)
	writeJSON(handlers.auth.logger, w, http.StatusCreated, nativeAuthenticationResponse{Status: "authenticated"})
}

func (handlers nativeAuthHandlers) passwordLogin(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r, handlers.auth.config.Authentication.PasswordEnabled) ||
		!handlers.allowIP(w, r, AuthRateLimitPasswordFailIP) {
		return
	}
	var input passwordLoginRequest
	if err := decodeJSONRequest(w, r, &input, 4096); err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := handlers.service.PasswordLogin(r.Context(), identity.PasswordLoginInput{
		Identifier: input.Identifier, Password: input.Password, KeepSignedIn: input.KeepSignedIn,
		UserAgent: r.UserAgent(), RemoteAddress: r.RemoteAddr,
	})
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	if handlers.writeMFAChallenge(w, result.MFA) {
		return
	}
	handlers.setSession(w, result)
	writeJSON(handlers.auth.logger, w, http.StatusOK, nativeAuthenticationResponse{Status: "authenticated"})
}

func (handlers nativeAuthHandlers) writeMFAChallenge(w http.ResponseWriter, challenge *identity.MFAChallenge) bool {
	if challenge == nil {
		return false
	}
	http.SetCookie(w, challenge.Cookie)
	writeJSON(handlers.auth.logger, w, http.StatusOK, nativeAuthenticationResponse{Status: "mfa_required", Methods: challenge.Methods})
	return true
}

func (handlers nativeAuthHandlers) transactionBinding(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := identity.AuthTransactionCookieName(handlers.auth.config.Authentication.CookieSecure)
	var binding string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			binding = cookie.Value
			count++
		}
	}
	if count != 1 || len(binding) != 43 {
		handlers.writeError(w, r, identity.ErrAuthTransactionUnavailable)
		return "", false
	}
	return binding, true
}

func (handlers nativeAuthHandlers) writeChallenge(w http.ResponseWriter, result identity.SignupChallengeResult) {
	if result.Cookie != nil {
		http.SetCookie(w, result.Cookie)
	}
	response := signupChallengeResponse{
		ChallengeID: result.ChallengeID, CooldownSeconds: result.CooldownSeconds, ExpiresInSeconds: result.ExpiresInSeconds,
	}
	if result.MaskedDestination != "" {
		response.MaskedDestination = result.MaskedDestination
		response.ResendAfterSeconds = result.CooldownSeconds
	}
	writeJSON(handlers.auth.logger, w, http.StatusAccepted, response)
}

func (handlers nativeAuthHandlers) setSession(w http.ResponseWriter, result identity.NativeSessionResult) {
	expires := result.ExpiresAt
	if result.Persistent {
		expires = result.AbsoluteExpiresAt
	}
	handlers.auth.setCookie(w, handlers.auth.cookieNames.session, result.SessionToken, expires)
	handlers.auth.setCookie(w, handlers.auth.cookieNames.csrf, result.CSRFToken, expires)
}

func (handlers nativeAuthHandlers) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, title, detail := http.StatusInternalServerError, "Authentication failed", "The authentication request could not be completed."
	code := ""
	switch {
	case errors.Is(err, identity.ErrSSORequired):
		status, title, detail, code = http.StatusForbidden, "School SSO required", "Sign in with your school's identity provider.", "auth_sso_required"
	case errors.Is(err, identity.ErrNativeInvalidCode):
		status, title, detail, code = http.StatusUnauthorized, "Verification failed", "Invalid or expired verification code.", "auth_invalid_verification_code"
	case errors.Is(err, identity.ErrFederationAALRequired):
		status, title, detail, code = http.StatusForbidden, "Additional verification required", "Verify with a strong sign-in method.", "auth_assurance_required"
	case errors.Is(err, identity.ErrNativeMethodDisabled):
		status, title, detail = http.StatusServiceUnavailable, "Authentication unavailable", "This authentication method is disabled or unavailable."
	case errors.Is(err, identity.ErrNativeInvalidCurrentPassword):
		status, title, detail, code = http.StatusUnauthorized, "Invalid current password", "The current password is invalid or temporarily unavailable.", "auth_invalid_current_password"
	case errors.Is(err, identity.ErrNativePasswordUnchanged):
		status, title, detail, code = http.StatusBadRequest, "Invalid password", "Choose a password different from the current password.", "auth_password_unchanged"
	case errors.Is(err, identity.ErrNativeVerifiedEmailRequired):
		status, title, detail, code = http.StatusForbidden, "Verified email required", "Verify your email before setting a password.", "auth_verified_email_required"
	case errors.Is(err, identity.ErrRecentAuthenticationRequired):
		status, title, detail, code = http.StatusForbidden, "Recent authentication required", "Sign in again before changing your password.", "auth_recent_authentication_required"
	case errors.Is(err, identity.ErrSessionNotFound), errors.Is(err, identity.ErrInvalidCSRFToken), errors.Is(err, identity.ErrSessionContextConflict):
		handlers.auth.writeIdentityProblem(w, r, err)
		return
	case errors.Is(err, identity.ErrNativeInvalidCredentials):
		status, title, detail = http.StatusUnauthorized, "Sign-in failed", "Invalid email or password."
	case errors.Is(err, identity.ErrNativeTermsVersion):
		status, title, detail, code = http.StatusBadRequest, "Terms have changed", "Review and accept the current user agreement and privacy policy.", "auth_terms_version_changed"
	case errors.Is(err, identity.ErrPasswordPolicy):
		status, title, detail, code = http.StatusBadRequest, "Invalid password", "The password does not meet the current password policy.", "auth_password_policy"
	case errors.Is(err, identity.ErrPasswordBusy):
		status, title, detail = http.StatusServiceUnavailable, "Authentication busy", "Please try again shortly."
		w.Header().Set("Retry-After", "2")
	case errors.Is(err, identity.ErrAuthChallengeThrottled):
		status, title, detail = http.StatusTooManyRequests, "Too many requests", "Please wait before requesting another code."
		w.Header().Set("Retry-After", "60")
	case errors.Is(err, identity.ErrNativeSignupUnavailable), errors.Is(err, identity.ErrAuthTransactionUnavailable):
		status, title, detail = http.StatusBadRequest, "Sign-up unavailable", "Unable to complete sign-up. Please try signing in."
	case errors.Is(err, identity.ErrAuthFoundationInput), errors.Is(err, identity.ErrInvalidProfile):
		status, title, detail = http.StatusBadRequest, "Invalid request", "Check the supplied authentication details."
	case errors.Is(err, identity.ErrAuthFoundationStorage):
		status, title, detail = http.StatusServiceUnavailable, "Authentication unavailable", "Please try again shortly."
		w.Header().Set("Retry-After", "2")
	}
	// Do not log or echo err: downstream errors may contain password/OTP/PII.
	writeCodedProblem(w, r, status, code, title, detail)
}

func (handlers nativeAuthHandlers) clearTransactionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: identity.AuthTransactionCookieName(handlers.auth.config.Authentication.CookieSecure),
		Path: "/", MaxAge: -1, Expires: time.Unix(1, 0).UTC(), HttpOnly: true,
		Secure: handlers.auth.config.Authentication.CookieSecure, SameSite: http.SameSiteStrictMode})
}
