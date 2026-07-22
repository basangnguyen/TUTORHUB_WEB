package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

const maximumWorkspaceRequestBytes = 16 * 1024

const (
	tenantsCollectionPath     = "/api/v1/tenants"
	tenantsResourcePathPrefix = "/api/v1/tenants/"
)

type createTenantRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type switchActiveTenantRequest struct {
	TenantID string `json:"tenant_id"`
}

type updateTenantRequest struct {
	Name            *string `json:"name"`
	Slug            *string `json:"slug"`
	Locale          *string `json:"locale"`
	Timezone        *string `json:"timezone"`
	ExpectedVersion int64   `json:"expected_version"`
}

type archiveTenantRequest struct {
	ExpectedVersion int64 `json:"expected_version"`
}

type tenantResponse struct {
	ID         uuid.UUID  `json:"id"`
	Slug       string     `json:"slug"`
	Name       string     `json:"name"`
	Locale     string     `json:"locale"`
	Timezone   string     `json:"timezone"`
	Status     string     `json:"status"`
	Version    int64      `json:"version"`
	Role       string     `json:"role"`
	IsActive   bool       `json:"is_active"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	ArchivedAt *time.Time `json:"archived_at"`
}

type tenantListResponse struct {
	Items []tenantResponse `json:"items"`
}

func (handlers authHandlers) tenantCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.listTenants(w, r)
	case http.MethodPost:
		handlers.createTenant(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"The workspace collection supports GET and POST requests.",
		)
	}
}

func (handlers authHandlers) tenantResource(w http.ResponseWriter, r *http.Request) {
	tenantID, archive, ok := parseTenantResourcePath(r.URL.Path)
	if !ok {
		handlers.writeIdentityProblem(w, r, identity.ErrTenantNotFound)
		return
	}
	if archive {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeProblem(
				w,
				r,
				http.StatusMethodNotAllowed,
				"Method not allowed",
				"Workspace archive supports POST requests.",
			)
			return
		}
		handlers.archiveTenant(w, r, tenantID)
		return
	}

	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.getTenant(w, r, tenantID)
	case http.MethodPatch:
		handlers.updateTenant(w, r, tenantID)
	default:
		w.Header().Set("Allow", "GET, HEAD, PATCH")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Workspace details support GET and PATCH requests.",
		)
	}
}

func (handlers authHandlers) listTenants(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	tenants, err := handlers.identity.ListTenants(r.Context(), principal)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}
	items := make([]tenantResponse, 0, len(tenants))
	for _, tenant := range tenants {
		items = append(items, newTenantResponse(tenant))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, http.StatusOK, tenantListResponse{Items: items})
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
	if err := decodeJSONRequest(w, r, &request, maximumWorkspaceRequestBytes); err != nil {
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
	if result.Principal.ActiveTenant != nil {
		w.Header().Set("Location", tenantsResourcePathPrefix+result.Principal.ActiveTenant.ID.String())
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	handlers.writePrincipal(w, http.StatusCreated, result.Principal)
}

func (handlers authHandlers) getTenant(
	w http.ResponseWriter,
	r *http.Request,
	tenantID uuid.UUID,
) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	tenant, err := handlers.identity.GetTenant(r.Context(), principal, tenantID)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}
	handlers.writeTenant(w, http.StatusOK, tenant)
}

func (handlers authHandlers) updateTenant(
	w http.ResponseWriter,
	r *http.Request,
	tenantID uuid.UUID,
) {
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
	var request updateTenantRequest
	if err := decodeJSONRequest(w, r, &request, maximumWorkspaceRequestBytes); err != nil {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"Invalid workspace update",
			"Provide expected_version and at least one supported workspace field.",
		)
		return
	}
	tenant, err := handlers.identity.UpdateTenant(
		r.Context(),
		principal,
		tenantID,
		identity.UpdateTenantInput{
			Name:            request.Name,
			Slug:            request.Slug,
			Locale:          request.Locale,
			Timezone:        request.Timezone,
			ExpectedVersion: request.ExpectedVersion,
		},
	)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}
	handlers.writeTenant(w, http.StatusOK, tenant)
}

func (handlers authHandlers) archiveTenant(
	w http.ResponseWriter,
	r *http.Request,
	tenantID uuid.UUID,
) {
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
	var request archiveTenantRequest
	if err := decodeJSONRequest(w, r, &request, maximumWorkspaceRequestBytes); err != nil {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"Invalid workspace archive request",
			"Provide the current expected_version for this workspace.",
		)
		return
	}
	result, err := handlers.identity.ArchiveTenant(
		r.Context(),
		principal,
		tenantID,
		request.ExpectedVersion,
	)
	if err != nil {
		handlers.writeIdentityProblem(w, r, err)
		return
	}
	handlers.setArchiveSessionCookies(w, result)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	handlers.writePrincipal(w, http.StatusOK, result.Principal)
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
	if err := decodeJSONRequest(w, r, &request, maximumWorkspaceRequestBytes); err != nil {
		writeProblem(
			w,
			r,
			http.StatusBadRequest,
			"Invalid workspace selection",
			"Provide one valid workspace identifier.",
		)
		return
	}
	tenantID, ok := parseResourceUUID(strings.TrimSpace(request.TenantID))
	if !ok {
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
	if result.SessionToken == "" || result.CSRFToken == "" {
		return
	}
	handlers.setCookie(
		w,
		handlers.cookieNames.session,
		result.SessionToken,
		result.ExpiresAt,
	)
	handlers.setCookie(
		w,
		handlers.cookieNames.csrf,
		result.CSRFToken,
		result.ExpiresAt,
	)
}

func (handlers authHandlers) setArchiveSessionCookies(
	w http.ResponseWriter,
	result identity.TenantArchiveResult,
) {
	handlers.setCookie(w, handlers.cookieNames.session, result.SessionToken, result.ExpiresAt)
	handlers.setCookie(w, handlers.cookieNames.csrf, result.CSRFToken, result.ExpiresAt)
}

func (handlers authHandlers) writeTenant(w http.ResponseWriter, status int, tenant identity.Tenant) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.logger, w, status, newTenantResponse(tenant))
}

func newTenantResponse(tenant identity.Tenant) tenantResponse {
	return tenantResponse{
		ID: tenant.ID, Slug: tenant.Slug, Name: tenant.Name,
		Locale: tenant.Locale, Timezone: tenant.Timezone, Status: tenant.Status,
		Version: tenant.Version, Role: tenant.Role, IsActive: tenant.IsActive,
		CreatedAt: tenant.CreatedAt, UpdatedAt: tenant.UpdatedAt, ArchivedAt: tenant.ArchivedAt,
	}
}

func parseTenantResourcePath(path string) (uuid.UUID, bool, bool) {
	if !strings.HasPrefix(path, tenantsResourcePathPrefix) {
		return uuid.Nil, false, false
	}
	remainder := strings.TrimPrefix(path, tenantsResourcePathPrefix)
	parts := strings.Split(remainder, "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		return uuid.Nil, false, false
	}
	tenantID, ok := parseResourceUUID(parts[0])
	if !ok {
		return uuid.Nil, false, false
	}
	if len(parts) == 1 {
		return tenantID, false, true
	}
	if parts[1] != "archive" {
		return uuid.Nil, false, false
	}
	return tenantID, true, true
}
