package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type registrationCompleteRequest struct {
	ChallengeID  uuid.UUID `json:"challenge_id"`
	Code         string    `json:"code"`
	TermsVersion string    `json:"terms_version"`
	KeepSignedIn bool      `json:"keep_signed_in,omitempty"`
}

func (handlers nativeAuthHandlers) registrationRequest(w http.ResponseWriter, r *http.Request, actions ...InvitationRateLimitAction) (identity.PendingRegistrationAPI, string, bool) {
	cfg := handlers.auth.config.Authentication
	if !facebookFederationEnabled(cfg) && !microsoftFederationEnabled(cfg) {
		writeFederationProviderUnavailable(w, r)
		return nil, "", false
	}
	service, ok := handlers.service.(identity.PendingRegistrationAPI)
	if !ok || handlers.limiter == nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationStorage)
		return nil, "", false
	}
	if !handlers.allowIP(w, r, actions...) {
		return nil, "", false
	}
	binding, ok := singleCookieValue(r, identity.RegistrationTransactionCookieName(handlers.auth.config.Authentication.CookieSecure))
	if !ok || len(binding) != 43 {
		handlers.registrationError(w, r, identity.ErrAuthTransactionUnavailable)
		return nil, "", false
	}
	return service, binding, true
}

func (handlers nativeAuthHandlers) registrationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrAuthTransactionUnavailable):
		status := http.StatusBadRequest
		if r.Method == http.MethodGet {
			status = http.StatusNotFound
		}
		writeCodedProblem(w, r, status, "auth_registration_unavailable", "Registration unavailable", "This registration is invalid, expired, or already used.")
	case errors.Is(err, identity.ErrFederationSignupDisabled):
		writeCodedProblem(w, r, http.StatusForbidden, "auth_signup_disabled", "Registration unavailable", "Creating a new account is disabled.")
	default:
		handlers.writeError(w, r, err)
	}
}

func (handlers nativeAuthHandlers) previewRegistration(w http.ResponseWriter, r *http.Request) {
	service, binding, ok := handlers.registrationRequest(w, r, AuthRateLimitLinkIntentIP)
	if !ok {
		return
	}
	view, err := service.PreviewRegistration(r.Context(), binding)
	if err != nil {
		handlers.registrationError(w, r, err)
		return
	}
	writeJSON(handlers.auth.logger, w, http.StatusOK, view)
}

func (handlers nativeAuthHandlers) startRegistration(w http.ResponseWriter, r *http.Request) {
	service, binding, ok := handlers.registrationRequest(w, r, AuthRateLimitSignupIP, AuthRateLimitOTPSendIP)
	if !ok {
		return
	}
	var input emailSignupStartRequest
	if decodeJSONRequest(w, r, &input, 2048) != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	email, err := identity.NormalizeSignupEmail(input.Email)
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	allowed, err := handlers.allowDestination(r.Context(), email)
	if err != nil {
		handlers.writeError(w, r, err)
		return
	}
	result, err := service.StartRegistration(r.Context(), binding, identity.SignupStartInput{Email: email, DestinationAllowed: allowed, RemoteAddress: r.RemoteAddr})
	if err != nil {
		handlers.registrationError(w, r, err)
		return
	}
	handlers.writeChallenge(w, result)
}

func (handlers nativeAuthHandlers) resendRegistration(w http.ResponseWriter, r *http.Request) {
	service, binding, ok := handlers.registrationRequest(w, r, AuthRateLimitOTPSendIP)
	if !ok {
		return
	}
	if decodeJSONRequest(w, r, &struct{}{}, 512) != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := service.ResendRegistration(r.Context(), binding, handlers.allowDestination)
	if err != nil {
		handlers.registrationError(w, r, err)
		return
	}
	handlers.writeChallenge(w, result)
}

func (handlers nativeAuthHandlers) completeRegistration(w http.ResponseWriter, r *http.Request) {
	service, binding, ok := handlers.registrationRequest(w, r, AuthRateLimitOTPVerifyIP)
	if !ok {
		return
	}
	var input registrationCompleteRequest
	if decodeJSONRequest(w, r, &input, 2048) != nil || input.ChallengeID == uuid.Nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	result, err := service.CompleteRegistration(r.Context(), binding, identity.RegistrationCompleteInput{
		ChallengeID: input.ChallengeID, Code: input.Code, TermsVersion: input.TermsVersion, KeepSignedIn: input.KeepSignedIn,
		SignupEnabled: handlers.auth.config.Authentication.SignupEnabled, UserAgent: r.UserAgent(), RemoteAddress: r.RemoteAddr})
	if err != nil {
		handlers.registrationError(w, r, err)
		return
	}
	status := "authenticated"
	if result.LinkBinding != "" {
		status = "link_pending"
		federationHandlers{auth: handlers.auth}.setLinkCookie(w, result.LinkBinding, result.LinkExpiresAt)
	} else {
		handlers.setSession(w, result.Session)
	}
	handlers.clearRegistrationCookie(w)
	writeJSON(handlers.auth.logger, w, http.StatusOK, linkIntentConfirmResponse{Status: status, ReturnTo: result.ReturnTo})
}

func (handlers nativeAuthHandlers) cancelRegistration(w http.ResponseWriter, r *http.Request) {
	service, binding, ok := handlers.registrationRequest(w, r, AuthRateLimitLinkIntentIP)
	if !ok {
		return
	}
	if decodeJSONRequest(w, r, &struct{}{}, 512) != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationInput)
		return
	}
	if err := service.CancelRegistration(r.Context(), binding); err != nil {
		if errors.Is(err, identity.ErrAuthTransactionUnavailable) {
			handlers.clearRegistrationCookie(w)
		}
		handlers.registrationError(w, r, err)
		return
	}
	handlers.clearRegistrationCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (handlers nativeAuthHandlers) clearRegistrationCookie(w http.ResponseWriter) {
	secure := handlers.auth.config.Authentication.CookieSecure
	http.SetCookie(w, &http.Cookie{Name: identity.RegistrationTransactionCookieName(secure), Path: "/", Secure: secure, HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0).UTC()})
}
