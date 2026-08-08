package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/discovery"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func TestDiscoveryHandlersBindPrincipalTenantAndProjectMetadataOnly(t *testing.T) {
	t.Parallel()
	tenantID, actorID, classID := uuid.New(), uuid.New(), uuid.New()
	fileID, conversationID := uuid.New(), uuid.New()
	service := &fakeDiscoveryService{
		recentFiles: []discovery.RecentFile{{
			ID: fileID, ClassID: classID, ClassTitle: "Algebra",
			DisplayName: "lesson.pdf", DeclaredMediaType: "application/pdf",
			SizeBytes: 42, UpdatedAt: discoveryHTTPTestTime,
		}},
		searchPage: discovery.SearchPage{Items: []discovery.SearchResult{{
			Kind: discovery.ResultConversation, ID: conversationID,
			Title: "Study group", Context: "direct", OccurredAt: discoveryHTTPTestTime,
		}}},
	}
	handler := discoveryTestHandler(classIdentityService(tenantID, actorID, nil), service)

	recentRequest := httptest.NewRequest(http.MethodGet, homeRecentFilesPath+"?limit=4", nil)
	recentRequest.Header.Set(discoveryTenantHeader, tenantID.String())
	addSessionCookie(recentRequest)
	recentResponse := httptest.NewRecorder()
	handler.ServeHTTP(recentResponse, recentRequest)
	if recentResponse.Code != http.StatusOK {
		t.Fatalf("recent status=%d body=%s", recentResponse.Code, recentResponse.Body.String())
	}
	assertDiscoveryHeaders(t, recentResponse)
	if service.recentAccess.TenantID != tenantID || service.recentAccess.ActorID != actorID ||
		service.recentLimit != 4 {
		t.Fatalf("unexpected recent call: %+v", service)
	}
	if strings.Contains(recentResponse.Body.String(), "object_key") ||
		strings.Contains(recentResponse.Body.String(), "checksum") ||
		strings.Contains(recentResponse.Body.String(), "storage_version") {
		t.Fatalf("private file metadata leaked: %s", recentResponse.Body.String())
	}

	searchRequest := httptest.NewRequest(
		http.MethodGet, resourceSearchPath+"?q=Study%20group&limit=7", nil,
	)
	searchRequest.Header.Set(discoveryTenantHeader, tenantID.String())
	addSessionCookie(searchRequest)
	searchResponse := httptest.NewRecorder()
	handler.ServeHTTP(searchResponse, searchRequest)
	if searchResponse.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", searchResponse.Code, searchResponse.Body.String())
	}
	assertDiscoveryHeaders(t, searchResponse)
	if service.searchAccess.TenantID != tenantID || service.searchAccess.ActorID != actorID ||
		service.searchQuery != "Study group" || service.searchLimit != 7 {
		t.Fatalf("unexpected search call: %+v", service)
	}
	if strings.Contains(searchResponse.Body.String(), "message") ||
		strings.Contains(searchResponse.Body.String(), "description") ||
		strings.Contains(searchResponse.Body.String(), "participants") {
		t.Fatalf("private search snippet leaked: %s", searchResponse.Body.String())
	}
}

func TestDiscoveryHandlersFailClosedForTenantAssertionAndInput(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	service := &fakeDiscoveryService{}
	handler := discoveryTestHandler(
		classIdentityService(tenantID, uuid.New(), nil), service,
	)

	for _, test := range []struct {
		path   string
		header string
		status int
		code   string
	}{
		{path: resourceSearchPath + "?q=valid", status: 400, code: "discovery_invalid"},
		{path: resourceSearchPath + "?q=valid", header: uuid.NewString(), status: 409, code: "discovery_scope_changed"},
		{path: resourceSearchPath + "?q=x", header: tenantID.String(), status: 400, code: "discovery_invalid"},
		{path: homeRecentFilesPath + "?limit=wrong", header: tenantID.String(), status: 400, code: "discovery_invalid"},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		if test.header != "" {
			request.Header.Set(discoveryTenantHeader, test.header)
		}
		addSessionCookie(request)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assertDiscoveryProblem(t, response, test.status, test.code)
	}
	if service.recentCalls != 0 || service.searchCalls != 1 {
		t.Fatalf("unexpected service calls: recent=%d search=%d", service.recentCalls, service.searchCalls)
	}
}

var discoveryHTTPTestTime = time.Date(2026, time.August, 8, 7, 0, 0, 0, time.UTC)

type fakeDiscoveryService struct {
	recentFiles  []discovery.RecentFile
	recentError  error
	recentCalls  int
	recentAccess discovery.AccessContext
	recentLimit  int
	searchPage   discovery.SearchPage
	searchError  error
	searchCalls  int
	searchAccess discovery.AccessContext
	searchQuery  string
	searchLimit  int
}

func (service *fakeDiscoveryService) RecentFiles(
	_ context.Context,
	access discovery.AccessContext,
	limit int,
) ([]discovery.RecentFile, error) {
	service.recentCalls++
	service.recentAccess, service.recentLimit = access, limit
	return service.recentFiles, service.recentError
}

func (service *fakeDiscoveryService) Search(
	_ context.Context,
	access discovery.AccessContext,
	query string,
	limit int,
) (discovery.SearchPage, error) {
	service.searchCalls++
	service.searchAccess, service.searchQuery, service.searchLimit = access, query, limit
	if len([]rune(strings.TrimSpace(query))) < 2 {
		return discovery.SearchPage{}, discovery.ErrInvalidInput
	}
	return service.searchPage, service.searchError
}

func discoveryTestHandler(
	identityService identity.ServiceAPI,
	service discovery.ServiceAPI,
) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{Clock: func() time.Time { return discoveryHTTPTestTime }, Identity: identityService, Discovery: service},
	)
}

func assertDiscoveryHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	cacheControl := response.Header().Get("Cache-Control")
	if (cacheControl != "private, no-store" && cacheControl != "no-store") ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!headerContainsValue(response.Header().Values("Vary"), "Cookie") ||
		!headerContainsValue(response.Header().Values("Vary"), discoveryTenantHeader) {
		t.Fatalf("unexpected discovery headers: %v", response.Header())
	}
}

func assertDiscoveryProblem(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d, want %d: %s", response.Code, status, response.Body.String())
	}
	assertDiscoveryHeaders(t, response)
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != code {
		t.Fatalf("problem code=%q, want %q", problem.Code, code)
	}
}

var _ discovery.ServiceAPI = (*fakeDiscoveryService)(nil)
