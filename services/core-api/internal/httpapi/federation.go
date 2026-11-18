package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type federationHandlers struct {
	auth    authHandlers
	limiter InvitationRateLimiter
}

type linkIntentConfirmResponse struct {
	Status   string `json:"status"`
	ReturnTo string `json:"return_to"`
}

func googleFederationEnabled(cfg config.AuthenticationConfig) bool {
	return cfg.ProviderGoogleEnabled && strings.TrimSpace(cfg.GoogleClientID) != "" && strings.TrimSpace(cfg.GoogleClientSecret) != ""
}

func facebookFederationEnabled(cfg config.AuthenticationConfig) bool {
	return cfg.ProviderFacebookEnabled && strings.TrimSpace(cfg.FacebookClientID) != "" && strings.TrimSpace(cfg.FacebookClientSecret) != ""
}

func writeFederationProviderUnavailable(w http.ResponseWriter, r *http.Request) {
	writeCodedProblem(w, r, http.StatusNotFound, "auth_provider_unavailable", "Provider unavailable", "This authentication provider is unavailable.")
}

func (handlers federationHandlers) providerAvailable(provider string) bool {
	switch provider {
	case "google":
		return googleFederationEnabled(handlers.auth.config.Authentication)
	case "facebook":
		return facebookFederationEnabled(handlers.auth.config.Authentication)
	case "oidc":
		return handlers.auth.config.Authentication.ProviderOIDCDefaultEnabled && handlers.auth.config.Authentication.OIDCEnabled
	default:
		return false
	}
}

func (handlers federationHandlers) service() (identity.FederationAPI, bool) {
	service, ok := handlers.auth.identity.(identity.FederationAPI)
	return service, ok
}

func (handlers federationHandlers) rateDecision(r *http.Request, action InvitationRateLimitAction, bucket string) InvitationRateLimitDecision {
	if handlers.limiter == nil {
		return InvitationRateLimitDecision{Err: identity.ErrAuthFoundationStorage}
	}
	return handlers.limiter.Allow(r.Context(), action, bucket, handlers.auth.clock())
}

func federationIP(r *http.Request) string {
	prefix := identity.IPPrefix(r.RemoteAddr)
	if prefix == "" {
		return "unknown"
	}
	return prefix
}

func (handlers federationHandlers) allow(w http.ResponseWriter, r *http.Request, action InvitationRateLimitAction, bucket string) bool {
	decision := handlers.rateDecision(r, action, bucket)
	if decision.Err != nil {
		handlers.writeError(w, r, identity.ErrAuthFoundationStorage, false)
		return false
	}
	if !decision.Allowed {
		w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
		writeProblem(w, r, http.StatusTooManyRequests, "Too many requests", "Please wait before trying again.")
		return false
	}
	return true
}

func (handlers federationHandlers) start(w http.ResponseWriter, r *http.Request) {
	service, ok := handlers.service()
	provider := r.PathValue("provider")
	if !ok || !handlers.providerAvailable(provider) {
		writeFederationProviderUnavailable(w, r)
		return
	}
	if !handlers.allow(w, r, AuthRateLimitOAuthStartIP, federationIP(r)) {
		return
	}
	query := r.URL.Query()
	if len(query["return_to"]) > 1 || len(query["intent"]) > 1 {
		writeProblem(w, r, http.StatusBadRequest, "Invalid request", "Provide one return path and intent.")
		return
	}
	intent := query.Get("intent")
	if intent == "" {
		intent = "login"
	}
	if intent != "login" && intent != "reauth" {
		writeProblem(w, r, http.StatusBadRequest, "Invalid request", "Provide a supported authentication intent.")
		return
	}
	var principal identity.Principal
	if intent == "reauth" {
		principal, ok = handlers.auth.authenticatedPrincipal(w, r)
		if !ok {
			return
		}
	}
	result, err := service.BeginOAuth(r.Context(), provider, query.Get("return_to"), intent, principal)
	if err != nil {
		handlers.writeError(w, r, err, false)
		return
	}
	handlers.auth.setCookie(w, handlers.auth.cookieNames.flow, result.BrowserBinding, result.ExpiresAt)
	http.Redirect(w, r, result.AuthorizationURL, http.StatusSeeOther)
}

func (handlers federationHandlers) callback(w http.ResponseWriter, r *http.Request) {
	// The callback never writes provider errors or raw callback data to a log or response.
	handlers.auth.clearCookie(w, handlers.auth.cookieNames.flow)
	service, ok := handlers.service()
	provider := r.PathValue("provider")
	if !ok || !handlers.providerAvailable(provider) {
		handlers.redirectError(w, r, "provider_unavailable")
		return
	}
	decision := handlers.rateDecision(r, AuthRateLimitOAuthCallbackIP, federationIP(r))
	if decision.Err != nil {
		handlers.redirectError(w, r, "temporarily_unavailable")
		return
	}
	if !decision.Allowed {
		w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
		handlers.redirectError(w, r, "rate_limited")
		return
	}
	query := r.URL.Query()
	if len(query["state"]) > 1 || len(query["code"]) > 1 || len(query["iss"]) > 1 || (query.Has("iss") && strings.TrimSpace(query.Get("iss")) == "") || len(query["error"]) > 1 || query.Get("error") != "" {
		handlers.redirectError(w, r, "authentication_failed")
		return
	}
	binding, ok := singleCookieValue(r, handlers.auth.cookieNames.flow)
	if !ok {
		handlers.redirectError(w, r, "authentication_failed")
		return
	}
	result, err := service.CompleteOAuth(r.Context(), provider, identity.CallbackInput{
		State: query.Get("state"), Code: query.Get("code"), Issuer: query.Get("iss"), BrowserBinding: binding,
		UserAgent: r.UserAgent(), RemoteAddress: r.RemoteAddr,
	})
	if err != nil {
		handlers.redirectError(w, r, federationCallbackError(err))
		return
	}
	if result.LinkPending {
		handlers.setLinkCookie(w, result.LinkBinding, result.LinkExpiresAt)
		values := url.Values{"link": {"pending"}, "provider": {provider}}
		http.Redirect(w, r, strings.TrimRight(handlers.auth.config.WebOrigin, "/")+"/sign-in?"+values.Encode(), http.StatusSeeOther)
		return
	}
	if result.RegistrationPending {
		handlers.setTransactionCookie(w, identity.RegistrationTransactionCookieName(handlers.auth.config.Authentication.CookieSecure), result.RegistrationBinding, result.RegistrationExpiresAt)
		http.Redirect(w, r, strings.TrimRight(handlers.auth.config.WebOrigin, "/")+"/sign-in?registration=pending&provider=facebook", http.StatusSeeOther)
		return
	}
	if result.SessionToken != "" && result.CSRFToken != "" {
		handlers.auth.setCookie(w, handlers.auth.cookieNames.session, result.SessionToken, result.ExpiresAt)
		handlers.auth.setCookie(w, handlers.auth.cookieNames.csrf, result.CSRFToken, result.ExpiresAt)
	}
	http.Redirect(w, r, strings.TrimRight(handlers.auth.config.WebOrigin, "/")+result.ReturnTo, http.StatusSeeOther)
}

func federationCallbackError(err error) string {
	switch {
	case errors.Is(err, identity.ErrFederationSignupDisabled):
		return "signup_disabled"
	case errors.Is(err, identity.ErrVerifiedEmailRequired):
		return "email_unverified"
	case errors.Is(err, identity.ErrFederationProviderUnavailable):
		return "provider_unavailable"
	case errors.Is(err, identity.ErrFederationAALRequired):
		return "assurance_required"
	default:
		return "authentication_failed"
	}
}

func (handlers federationHandlers) redirectError(w http.ResponseWriter, r *http.Request, code string) {
	http.Redirect(w, r, strings.TrimRight(handlers.auth.config.WebOrigin, "/")+"/sign-in?error="+code, http.StatusSeeOther)
}

func singleCookieValue(r *http.Request, name string) (string, bool) {
	value := ""
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			value = cookie.Value
			count++
		}
	}
	return value, count == 1 && value != ""
}

func (handlers federationHandlers) linkBinding(r *http.Request) (string, bool) {
	value, ok := singleCookieValue(r, identity.LinkTransactionCookieName(handlers.auth.config.Authentication.CookieSecure))
	return value, ok && len(value) == 43
}

func (handlers federationHandlers) setLinkCookie(w http.ResponseWriter, binding string, expires time.Time) {
	handlers.setTransactionCookie(w, identity.LinkTransactionCookieName(handlers.auth.config.Authentication.CookieSecure), binding, expires)
}

func (handlers federationHandlers) setTransactionCookie(w http.ResponseWriter, name, binding string, expires time.Time) {
	maxAge := max(1, int(expires.Sub(handlers.auth.clock()).Seconds()))
	http.SetCookie(w, &http.Cookie{Name: name, Value: binding,
		Path: "/", Expires: expires, MaxAge: maxAge, HttpOnly: true, Secure: handlers.auth.config.Authentication.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func (handlers federationHandlers) clearLinkCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: identity.LinkTransactionCookieName(handlers.auth.config.Authentication.CookieSecure), Path: "/", Expires: time.Unix(1, 0).UTC(), MaxAge: -1,
		HttpOnly: true, Secure: handlers.auth.config.Authentication.CookieSecure, SameSite: http.SameSiteStrictMode})
}

func (handlers federationHandlers) preview(w http.ResponseWriter, r *http.Request) {
	service, ok := handlers.service()
	binding, hasBinding := handlers.linkBinding(r)
	if !ok || !hasBinding {
		handlers.writeError(w, r, identity.ErrLinkIntentUnavailable, true)
		return
	}
	result, err := service.GetLinkIntent(r.Context(), binding)
	if err != nil {
		handlers.writeError(w, r, err, true)
		return
	}
	writeJSON(handlers.auth.logger, w, http.StatusOK, result)
}

func (handlers federationHandlers) confirm(w http.ResponseWriter, r *http.Request) {
	service, ok := handlers.service()
	if !ok {
		handlers.writeError(w, r, identity.ErrLinkIntentUnavailable, false)
		return
	}
	token, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.auth.csrfPrincipal(w, r, token)
	if !ok {
		return
	}
	if !handlers.allow(w, r, AuthRateLimitLinkIntentIP, federationIP(r)) || !handlers.allow(w, r, AuthRateLimitLinkIntentUser, principal.User.ID.String()) {
		return
	}
	if decodeJSONRequest(w, r, &struct{}{}, 512) != nil {
		writeProblem(w, r, http.StatusBadRequest, "Invalid request", "Provide an empty JSON object.")
		return
	}
	binding, ok := handlers.linkBinding(r)
	if !ok {
		handlers.writeError(w, r, identity.ErrLinkIntentUnavailable, false)
		return
	}
	result, err := service.ConfirmLinkIntent(r.Context(), principal, binding)
	if err != nil {
		handlers.writeError(w, r, err, false)
		return
	}
	handlers.auth.setCookie(w, handlers.auth.cookieNames.session, result.SessionToken, result.CookieExpiresAt)
	handlers.auth.setCookie(w, handlers.auth.cookieNames.csrf, result.CSRFToken, result.CookieExpiresAt)
	handlers.clearLinkCookie(w)
	writeJSON(handlers.auth.logger, w, http.StatusOK, linkIntentConfirmResponse{Status: "linked", ReturnTo: result.ReturnTo})
}

func (handlers federationHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	service, ok := handlers.service()
	if !ok {
		handlers.writeError(w, r, identity.ErrLinkIntentUnavailable, false)
		return
	}
	if !handlers.allow(w, r, AuthRateLimitLinkIntentIP, federationIP(r)) {
		return
	}
	if decodeJSONRequest(w, r, &struct{}{}, 512) != nil {
		writeProblem(w, r, http.StatusBadRequest, "Invalid request", "Provide an empty JSON object.")
		return
	}
	binding, ok := handlers.linkBinding(r)
	if !ok {
		handlers.clearLinkCookie(w)
		handlers.writeError(w, r, identity.ErrLinkIntentUnavailable, false)
		return
	}
	err := service.CancelLinkIntent(r.Context(), binding)
	if err != nil {
		if errors.Is(err, identity.ErrLinkIntentUnavailable) {
			handlers.clearLinkCookie(w)
		}
		handlers.writeError(w, r, err, false)
		return
	}
	handlers.clearLinkCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (handlers federationHandlers) writeError(w http.ResponseWriter, r *http.Request, err error, preview bool) {
	switch {
	case errors.Is(err, identity.ErrFederationProviderUnavailable):
		writeFederationProviderUnavailable(w, r)
	case errors.Is(err, identity.ErrLinkIntentMismatch):
		writeCodedProblem(w, r, http.StatusForbidden, "auth_link_intent_mismatch", "Account mismatch", "Sign in to the account associated with this linking request.")
	case errors.Is(err, identity.ErrFederationAALRequired):
		writeCodedProblem(w, r, http.StatusForbidden, "auth_assurance_required", "Additional authentication required", "Use the authentication assurance required by this account.")
	case errors.Is(err, identity.ErrLinkIntentUnavailable):
		status := http.StatusBadRequest
		if preview {
			status = http.StatusNotFound
		}
		writeCodedProblem(w, r, status, "auth_link_intent_unavailable", "Linking request unavailable", "The linking request is invalid, expired, or already used.")
	case errors.Is(err, identity.ErrAuthFoundationStorage):
		writeProblem(w, r, http.StatusServiceUnavailable, "Service unavailable", "Authentication is temporarily unavailable.")
	case errors.Is(err, identity.ErrSessionNotFound), errors.Is(err, identity.ErrInvalidCSRFToken), errors.Is(err, identity.ErrSessionContextConflict), errors.Is(err, identity.ErrRecentAuthenticationRequired), errors.Is(err, identity.ErrInvalidReturnTo), errors.Is(err, identity.ErrInvalidAuthFlow), errors.Is(err, identity.ErrIdentityConflict), errors.Is(err, identity.ErrIdentityInactive), errors.Is(err, identity.ErrIdentityNotFound):
		handlers.auth.writeIdentityProblem(w, r, err)
	default:
		writeProblem(w, r, http.StatusServiceUnavailable, "Service unavailable", "Authentication is temporarily unavailable.")
	}
}
