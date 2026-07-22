package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const auditEventsPattern = "/api/v1/tenants/{tenantId}/audit-events"

type auditHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service audit.ServiceAPI
}

func newAuditHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service audit.ServiceAPI,
) auditHandlers {
	return auditHandlers{logger: logger, auth: auth, service: service}
}

func auditResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Add("Vary", "Cookie")
		next.ServeHTTP(w, r)
	})
}

func (handlers auditHandlers) list(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", http.MethodGet)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Audit events support GET requests.",
		)
		return
	}
	if !handlers.auth.available(w, r) {
		return
	}
	if handlers.service == nil {
		writeProblem(
			w,
			r,
			http.StatusServiceUnavailable,
			"Audit unavailable",
			"Audit history is not configured for this environment.",
		)
		return
	}
	tenantID, ok := parseResourceUUID(r.PathValue("tenantId"))
	if !ok {
		handlers.writeProblem(w, r, audit.ErrNotFound)
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	if principal.ActiveTenant == nil {
		handlers.writeProblem(w, r, audit.ErrNotFound)
		return
	}
	tenantContext, err := tenancy.New(principal.ActiveTenant.ID, principal.User.ID)
	if err != nil {
		handlers.writeProblem(w, r, audit.ErrNotFound)
		return
	}
	filter, err := parseAuditFilter(r)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	page, err := handlers.service.List(r.Context(), tenantContext, tenantID, filter)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, page)
}

func parseAuditFilter(r *http.Request) (audit.Filter, error) {
	query := r.URL.Query()
	filter := audit.Filter{
		Action:       audit.Action(strings.TrimSpace(query.Get("action"))),
		ResourceType: strings.TrimSpace(query.Get("resource_type")),
		Outcome:      audit.Outcome(strings.TrimSpace(query.Get("outcome"))),
		Cursor:       strings.TrimSpace(query.Get("cursor")),
	}
	if value := strings.TrimSpace(query.Get("occurred_from")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return audit.Filter{}, audit.ErrInvalidFilter
		}
		filter.OccurredFrom = &parsed
	}
	if value := strings.TrimSpace(query.Get("occurred_to")); value != "" {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return audit.Filter{}, audit.ErrInvalidFilter
		}
		filter.OccurredTo = &parsed
	}
	if value := strings.TrimSpace(query.Get("resource_id")); value != "" {
		resourceID, ok := parseResourceUUID(value)
		if !ok {
			return audit.Filter{}, audit.ErrInvalidFilter
		}
		filter.ResourceID = resourceID
	}
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		limit, err := strconv.Atoi(value)
		if err != nil {
			return audit.Filter{}, audit.ErrInvalidFilter
		}
		filter.Limit = limit
	}
	return filter, nil
}

func (handlers auditHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	title := "Audit request failed"
	detail := "The audit request could not be completed."
	switch {
	case errors.Is(err, audit.ErrInvalidFilter):
		status = http.StatusBadRequest
		title = "Invalid audit filter"
		detail = "Review the time, action, resource, outcome, limit, and cursor filters."
	case errors.Is(err, audit.ErrAccessDenied):
		status = http.StatusForbidden
		title = "Audit access denied"
		detail = "Only an organization administrator can view workspace audit history."
	case errors.Is(err, audit.ErrNotFound):
		status = http.StatusNotFound
		title = "Workspace not found"
		detail = "The workspace does not exist in the active tenant scope."
	case errors.Is(err, identity.ErrSessionNotFound):
		status = http.StatusUnauthorized
		title = "Authentication required"
		detail = "The session is missing, expired, or has been revoked."
	}
	writeProblem(w, r, status, title, detail)
}
