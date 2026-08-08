package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/tutorhub-v2/core-api/internal/modules/discovery"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	homeRecentFilesPath   = "/api/v1/home/recent-files"
	resourceSearchPath    = "/api/v1/search"
	discoveryTenantHeader = "X-TutorHub-Expected-Tenant-ID"
)

var errDiscoveryScopeChanged = errors.New("discovery active tenant changed")

type discoveryHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service discovery.ServiceAPI
}

type recentFilePageResponse struct {
	Items []discovery.RecentFile `json:"items"`
}

func newDiscoveryHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service discovery.ServiceAPI,
) discoveryHandlers {
	return discoveryHandlers{logger: logger, auth: auth, service: service}
}

func discoveryResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", discoveryTenantHeader)
		next.ServeHTTP(w, r)
	})
}

func (handlers discoveryHandlers) recentFiles(w http.ResponseWriter, r *http.Request) {
	if !handlers.readRequest(w, r) {
		return
	}
	access, ok := handlers.access(w, r)
	if !ok {
		return
	}
	limit, err := parseOptionalLimit(r)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	items, err := handlers.service.RecentFiles(r.Context(), access, limit)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, recentFilePageResponse{Items: items})
}

func (handlers discoveryHandlers) search(w http.ResponseWriter, r *http.Request) {
	if !handlers.readRequest(w, r) {
		return
	}
	access, ok := handlers.access(w, r)
	if !ok {
		return
	}
	limit, err := parseOptionalLimit(r)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	page, err := handlers.service.Search(
		r.Context(), access, strings.TrimSpace(r.URL.Query().Get("q")), limit,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, page)
}

func (handlers discoveryHandlers) readRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Discovery resources support GET requests.")
		return false
	}
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.service == nil {
		writeCodedProblem(w, r, http.StatusServiceUnavailable, "discovery_unavailable", "Home and search unavailable", "Home and search are not configured for this environment.")
		return false
	}
	return true
}

func (handlers discoveryHandlers) access(
	w http.ResponseWriter,
	r *http.Request,
) (discovery.AccessContext, bool) {
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return discovery.AccessContext{}, false
	}
	if principal.ActiveTenant == nil {
		handlers.writeProblem(w, r, discovery.ErrAccessDenied)
		return discovery.AccessContext{}, false
	}
	expectedTenantID, ok := parseResourceUUID(
		strings.TrimSpace(r.Header.Get(discoveryTenantHeader)),
	)
	if !ok {
		handlers.writeProblem(w, r, discovery.ErrInvalidInput)
		return discovery.AccessContext{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errDiscoveryScopeChanged)
		return discovery.AccessContext{}, false
	}
	return discovery.AccessContext{
		TenantID: principal.ActiveTenant.ID, ActorID: principal.User.ID,
		MembershipActive: principal.ActiveTenant.IsActive,
		OrganizationRoles: []policy.OrganizationRole{
			policy.OrganizationRole(principal.ActiveTenant.Role),
		},
	}, true
}

func parseOptionalLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, discovery.ErrInvalidInput
	}
	return limit, nil
}

func (handlers discoveryHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusServiceUnavailable, "discovery_unavailable"
	title, detail := "Home or search request failed", "The request could not be completed safely."
	switch {
	case errors.Is(err, discovery.ErrInvalidInput):
		status, code = http.StatusBadRequest, "discovery_invalid"
		title, detail = "Invalid search request", "Use a search query from 2 to 100 characters and a supported result limit."
	case errors.Is(err, discovery.ErrAccessDenied):
		status, code = http.StatusForbidden, "discovery_forbidden"
		title, detail = "Home and search access denied", "The active workspace cannot authorize this request."
	case errors.Is(err, errDiscoveryScopeChanged):
		status, code = http.StatusConflict, "discovery_scope_changed"
		title, detail = "Active workspace changed", "Reload the current session before retrying this request."
	default:
		handlers.logger.Error(
			"discovery request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
