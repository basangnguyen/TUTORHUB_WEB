package httpapi

import (
	"net/http"
	"strings"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

const (
	maximumProfileRequestBytes = 16 * 1024
	identityResourcePathPrefix = "/api/v1/me/identities/"
)

type profileResponse struct {
	User identity.User `json:"user"`
}

type identitiesResponse struct {
	Identities []identity.ExternalIdentity `json:"identities"`
}

type identityLinkResponse struct {
	AuthorizationURL string `json:"authorization_url"`
}

func (handlers authHandlers) profile(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.getProfile(w, r)
	case http.MethodPatch:
		handlers.updateProfile(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPatch)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"The requested resource does not support this HTTP method.",
		)
	}
}

func (handlers authHandlers) getProfile(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	profile, err := handlers.identity.GetProfile(r.Context(), principal)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	setPrivateResponseHeaders(w)
	writeJSON(handlers.logger, w, http.StatusOK, profileResponse{User: profile})
}

func (handlers authHandlers) updateProfile(w http.ResponseWriter, r *http.Request) {
	sessionToken, ok := handlers.sessionToken(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return
	}

	var patch identity.ProfilePatch
	if err := decodeJSONRequest(w, r, &patch, maximumProfileRequestBytes); err != nil {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"Invalid profile request",
			"Provide one JSON object containing only supported profile fields.",
		)
		return
	}
	profile, err := handlers.identity.UpdateProfile(r.Context(), principal, patch)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	setPrivateResponseHeaders(w)
	writeJSON(handlers.logger, w, http.StatusOK, profileResponse{User: profile})
}

func (handlers authHandlers) identities(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	identities, err := handlers.identity.ListIdentities(r.Context(), principal)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	setPrivateResponseHeaders(w)
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		identitiesResponse{Identities: identities},
	)
}

func (handlers authHandlers) beginIdentityLink(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	sessionToken, ok := handlers.sessionToken(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return
	}
	start, err := handlers.identity.BeginIdentityLink(r.Context(), principal)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	handlers.setCookie(w, handlers.cookieNames.flow, start.BrowserBinding, start.ExpiresAt)
	setPrivateResponseHeaders(w)
	writeJSON(
		handlers.logger,
		w,
		http.StatusOK,
		identityLinkResponse{AuthorizationURL: start.AuthorizationURL},
	)
}

func (handlers authHandlers) identityResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"The requested resource does not support this HTTP method.",
		)
		return
	}
	if !handlers.available(w, r) {
		return
	}

	rawIdentityID := strings.TrimPrefix(r.URL.Path, identityResourcePathPrefix)
	if rawIdentityID == "" || strings.Contains(rawIdentityID, "/") {
		writeProblem(
			w,
			r,
			http.StatusNotFound,
			"Resource not found",
			"The requested identity does not exist.",
		)
		return
	}
	identityID, ok := parseResourceUUID(rawIdentityID)
	if !ok {
		handlers.writeIdentityProblem(w, r, identity.ErrIdentityNotFound)
		return
	}

	sessionToken, ok := handlers.sessionToken(w, r)
	if !ok {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return
	}
	if err := handlers.identity.UnlinkIdentity(r.Context(), principal, identityID); err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	setPrivateResponseHeaders(w)
	w.WriteHeader(http.StatusNoContent)
}

func setPrivateResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
}
