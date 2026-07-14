package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

const maximumWorkspaceRequestBytes = 16 * 1024

type createTenantRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type switchActiveTenantRequest struct {
	TenantID string `json:"tenant_id"`
}

func (handlers authHandlers) createTenant(w http.ResponseWriter, r *http.Request) {
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

	var request createTenantRequest
	if err := decodeWorkspaceRequest(w, r, &request); err != nil {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"Invalid workspace request",
			"Provide one JSON object with only the workspace name and address.",
		)
		return
	}
	result, err := handlers.identity.CreateTenant(
		r.Context(),
		principal,
		identity.CreateTenantInput{Name: request.Name, Slug: request.Slug},
	)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	handlers.setTenantSessionCookies(w, result)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	handlers.writePrincipal(w, http.StatusCreated, result.Principal)
}

func (handlers authHandlers) switchActiveTenant(w http.ResponseWriter, r *http.Request) {
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

	var request switchActiveTenantRequest
	if err := decodeWorkspaceRequest(w, r, &request); err != nil {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"Invalid workspace selection",
			"Provide one valid workspace identifier.",
		)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(request.TenantID))
	if err != nil || tenantID == uuid.Nil {
		handlers.writeIdentityProblem(w, r, identity.ErrInvalidTenant)
		return
	}
	result, err := handlers.identity.SwitchActiveTenant(r.Context(), principal, tenantID)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}

	handlers.setTenantSessionCookies(w, result)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	handlers.writePrincipal(w, http.StatusOK, result.Principal)
}

func (handlers authHandlers) setTenantSessionCookies(
	w http.ResponseWriter,
	result identity.TenantSessionResult,
) {
	handlers.setCookie(
		w,
		handlers.cookieNames.session,
		result.SessionToken,
		result.ExpiresAt,
		true,
	)
	handlers.setCookie(
		w,
		handlers.cookieNames.csrf,
		result.CSRFToken,
		result.ExpiresAt,
		false,
	)
}

func decodeWorkspaceRequest(
	w http.ResponseWriter,
	r *http.Request,
	destination any,
) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}

	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maximumWorkspaceRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}

	return nil
}
