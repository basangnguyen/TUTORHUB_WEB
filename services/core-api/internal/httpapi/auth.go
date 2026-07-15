package httpapi

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
)

const csrfHeader = "X-CSRF-Token"

type authHandlers struct {
	config      config.Config
	logger      *slog.Logger
	identity    identity.ServiceAPI
	cookieNames authCookieNames
	clock       func() time.Time
}

type authCookieNames struct {
	session string
	csrf    string
	flow    string
}

type csrfResponse struct {
	CSRFToken string `json:"csrf_token"`
}

type logoutResponse struct {
	LogoutURL string `json:"logout_url,omitempty"`
}

type meResponse struct {
	User         identity.User     `json:"user"`
	ActiveTenant *identity.Tenant  `json:"active_tenant"`
	Memberships  []identity.Tenant `json:"memberships"`
	Permissions  []string          `json:"permissions"`
}

func newAuthHandlers(
	cfg config.Config,
	logger *slog.Logger,
	service identity.ServiceAPI,
	clock func() time.Time,
) authHandlers {
	prefix := ""
	if cfg.Authentication.CookieSecure {
		prefix = "__Host-"
	}
	return authHandlers{
		config:   cfg,
		logger:   logger,
		identity: service,
		clock:    clock,
		cookieNames: authCookieNames{
			session: prefix + "tutorhub_session",
			csrf:    prefix + "tutorhub_csrf",
			flow:    prefix + "tutorhub_oidc",
		},
	}
}

func (handlers authHandlers) login(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}

	start, err := handlers.identity.BeginLogin(r.Context(), r.URL.Query().Get("return_to"))
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}
	handlers.setCookie(w, handlers.cookieNames.flow, start.BrowserBinding, start.ExpiresAt)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, start.AuthorizationURL, http.StatusSeeOther)
}

func (handlers authHandlers) callback(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	if providerError := strings.TrimSpace(r.URL.Query().Get("error")); providerError != "" {
		handlers.clearCookie(w, handlers.cookieNames.flow)
		writeProblem(
			w,
			r,
			http.StatusUnauthorized,
			"Sign-in was not completed",
			"The identity provider did not complete the sign-in request.",
		)
		return
	}

	flowCookie, err := r.Cookie(handlers.cookieNames.flow)
	if err != nil {
		handlers.writeIdentityProblem(w, r, identity.ErrInvalidAuthFlow)
		return
	}
	result, err := handlers.identity.CompleteLogin(r.Context(), identity.CallbackInput{
		State:          r.URL.Query().Get("state"),
		BrowserBinding: flowCookie.Value,
		Code:           r.URL.Query().Get("code"),
		UserAgent:      r.UserAgent(),
		RemoteAddress:  r.RemoteAddr,
	})
	handlers.clearCookie(w, handlers.cookieNames.flow)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	handlers.setCookie(w, handlers.cookieNames.session, result.SessionToken, result.ExpiresAt)
	handlers.setCookie(w, handlers.cookieNames.csrf, result.CSRFToken, result.ExpiresAt)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(
		w,
		r,
		strings.TrimRight(handlers.config.WebOrigin, "/")+result.ReturnTo,
		http.StatusSeeOther,
	)
}

func (handlers authHandlers) csrf(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	sessionToken, ok := handlers.sessionToken(w, r)
	if !ok {
		return
	}
	result, err := handlers.identity.RotateCSRF(r.Context(), sessionToken)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}
	handlers.setCookie(
		w,
		handlers.cookieNames.csrf,
		result.Token,
		result.ExpiresAt,
	)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(handlers.logger, w, http.StatusOK, csrfResponse{CSRFToken: result.Token})
}

func (handlers authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	sessionToken, ok := handlers.sessionToken(w, r)
	if !ok {
		return
	}
	if _, ok := handlers.csrfPrincipal(w, r, sessionToken); !ok {
		return
	}
	logoutURL, err := handlers.identity.Logout(r.Context(), sessionToken)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	handlers.clearCookie(w, handlers.cookieNames.session)
	handlers.clearCookie(w, handlers.cookieNames.csrf)
	handlers.clearCookie(w, handlers.cookieNames.flow)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(handlers.logger, w, http.StatusOK, logoutResponse{LogoutURL: logoutURL})
}

func (handlers authHandlers) me(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	handlers.writePrincipal(w, http.StatusOK, principal)
}

func (handlers authHandlers) authenticatedPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	principal, err := handlers.identity.Authenticate(r.Context(), sessionToken)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return identity.Principal{}, false
	}

	return principal, true
}

func (handlers authHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
	sessionToken string,
) (identity.Principal, bool) {
	csrfCookie, err := r.Cookie(handlers.cookieNames.csrf)
	csrfToken := r.Header.Get(csrfHeader)
	if err != nil || !constantTimeEqual(csrfCookie.Value, csrfToken) {
		handlers.writeIdentityProblem(w, r, identity.ErrInvalidCSRFToken)
		return identity.Principal{}, false
	}
	principal, err := handlers.identity.ValidateCSRF(r.Context(), sessionToken, csrfToken)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return identity.Principal{}, false
	}

	return principal, true
}

func (handlers authHandlers) writePrincipal(
	w http.ResponseWriter,
	status int,
	principal identity.Principal,
) {
	writeJSON(handlers.logger, w, status, meResponse{
		User:         principal.User,
		ActiveTenant: principal.ActiveTenant,
		Memberships:  principal.Memberships,
		Permissions:  principal.Permissions,
	})
}

func (handlers authHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if handlers.identity != nil {
		return true
	}
	writeProblem(
		w,
		r,
		http.StatusServiceUnavailable,
		"Authentication unavailable",
		"Authentication is not configured for this environment.",
	)
	return false
}

func (handlers authHandlers) sessionToken(
	w http.ResponseWriter,
	r *http.Request,
) (string, bool) {
	cookie, err := r.Cookie(handlers.cookieNames.session)
	if err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value, true
	}
	handlers.writeIdentityProblem(w, r, identity.ErrSessionNotFound)
	return "", false
}

func (handlers authHandlers) writeIdentityProblem(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	title := "Authentication failed"
	detail := "The authentication request could not be completed."

	switch {
	case errors.Is(err, identity.ErrAuthenticationDisabled):
		status = http.StatusServiceUnavailable
		title = "Authentication unavailable"
		detail = "Authentication is not configured for this environment."
	case errors.Is(err, identity.ErrInvalidReturnTo), errors.Is(err, identity.ErrInvalidAuthFlow):
		status = http.StatusBadRequest
		title = "Invalid authentication request"
		detail = "The sign-in request is invalid, expired, or has already been used."
	case errors.Is(err, identity.ErrVerifiedEmailRequired):
		status = http.StatusForbidden
		title = "Verified email required"
		detail = "A verified email address is required to use TutorHub."
	case errors.Is(err, identity.ErrProviderExchange):
		status = http.StatusBadGateway
		title = "Identity provider unavailable"
		detail = "TutorHub could not verify the identity provider response."
	case errors.Is(err, identity.ErrSessionNotFound):
		status = http.StatusUnauthorized
		title = "Authentication required"
		detail = "The session is missing, expired, or has been revoked."
	case errors.Is(err, identity.ErrInvalidCSRFToken):
		status = http.StatusForbidden
		title = "Request verification failed"
		detail = "The request verification token is missing or invalid."
	case errors.Is(err, identity.ErrInvalidTenant):
		status = http.StatusBadRequest
		title = "Invalid workspace"
		detail = "The workspace name, address, or identifier is invalid."
	case errors.Is(err, identity.ErrTenantSlugTaken):
		status = http.StatusConflict
		title = "Workspace address unavailable"
		detail = "Choose a different workspace address and try again."
	case errors.Is(err, identity.ErrTenantCreationDenied):
		status = http.StatusForbidden
		title = "Workspace creation not allowed"
		detail = "This onboarding session cannot create another workspace."
	case errors.Is(err, identity.ErrTenantAccessDenied):
		status = http.StatusForbidden
		title = "Workspace access denied"
		detail = "You do not have an active membership in this workspace."
	}

	if status >= http.StatusInternalServerError {
		handlers.logger.Error(
			"identity request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeProblem(w, r, status, title, detail)
}

func (handlers authHandlers) setCookie(
	w http.ResponseWriter,
	name string,
	value string,
	expires time.Time,
) {
	maxAge := int(expires.Sub(handlers.clock()).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		Expires:  expires.UTC(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   handlers.config.Authentication.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (handlers authHandlers) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   handlers.config.Authentication.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func constantTimeEqual(left string, right string) bool {
	if len(left) == 0 || len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}
