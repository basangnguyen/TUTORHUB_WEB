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
	"github.com/tutorhub-v2/core-api/internal/modules/collaboration"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func TestWhiteboardCreateRequiresCSRFExpectedTenantAndStrictBody(t *testing.T) {
	t.Parallel()

	tenantID, actorID, spaceID, documentID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	identityService := classIdentityService(tenantID, actorID, nil)
	service := &fakeWhiteboardService{createResult: collaboration.CreateResult{
		Created:  true,
		Document: whiteboardTestDocument(documentID, spaceID),
	}}
	handler := newWhiteboardTestHandler(identityService, service)
	body := `{"media_space_id":"` + spaceID.String() + `","idempotency_key":"whiteboard-create-0001"}`

	missingCSRF := whiteboardRequest(http.MethodPost, whiteboardsCollectionPath, body, tenantID, false)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || service.createCalls != 0 {
		t.Fatalf("missing CSRF must fail closed: status=%d calls=%d", missingCSRFResponse.Code, service.createCalls)
	}

	missingTenant := whiteboardRequest(http.MethodPost, whiteboardsCollectionPath, body, uuid.Nil, true)
	missingTenantResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingTenantResponse, missingTenant)
	if missingTenantResponse.Code != http.StatusBadRequest || service.createCalls != 0 {
		t.Fatalf("missing tenant assertion must fail closed: status=%d calls=%d", missingTenantResponse.Code, service.createCalls)
	}

	unknownField := strings.TrimSuffix(body, "}") + `,"provider_document_name":"must-not-be-accepted"}`
	strictRequest := whiteboardRequest(http.MethodPost, whiteboardsCollectionPath, unknownField, tenantID, true)
	strictResponse := httptest.NewRecorder()
	handler.ServeHTTP(strictResponse, strictRequest)
	if strictResponse.Code != http.StatusBadRequest || service.createCalls != 0 {
		t.Fatalf("unknown field must fail before service: status=%d calls=%d", strictResponse.Code, service.createCalls)
	}

	request := whiteboardRequest(http.MethodPost, whiteboardsCollectionPath, body, tenantID, true)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || service.createCalls != 1 {
		t.Fatalf("create whiteboard: status=%d calls=%d body=%s", response.Code, service.createCalls, response.Body.String())
	}
	if service.createInput.MediaSpaceID != spaceID || service.createInput.IdempotencyKey != "whiteboard-create-0001" {
		t.Fatalf("unexpected create input: %+v", service.createInput)
	}
	assertWhiteboardPrivacyHeaders(t, response)
	assertWhiteboardAccess(t, service.access, identityService.principal)
	if strings.Contains(response.Body.String(), "provider_document") {
		t.Fatalf("provider identity leaked: %s", response.Body.String())
	}
}

func TestWhiteboardDeploymentGuardAuthenticatesThenFailsClosed(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	handler := newWhiteboardTestHandler(classIdentityService(tenantID, actorID, nil), nil)
	request := whiteboardRequest(http.MethodGet, whiteboardsCollectionPath+"/"+uuid.NewString(), "", tenantID, false)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("deployment-force-off route must return 503 after auth: status=%d body=%s", response.Code, response.Body.String())
	}
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode force-off problem: %v", err)
	}
	if problem.Code != "whiteboard_unavailable" {
		t.Fatalf("unexpected force-off problem: %+v", problem)
	}
	assertWhiteboardPrivacyHeaders(t, response)
}

func TestWhiteboardReadConcealsMissingForeignAndInaccessibleResources(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	documentID := uuid.New()
	service := &fakeWhiteboardService{requestError: collaboration.ErrNotFound}
	handler := newWhiteboardTestHandler(classIdentityService(tenantID, actorID, nil), service)

	request := whiteboardRequest(http.MethodGet, whiteboardsCollectionPath+"/"+documentID.String(), "", tenantID, false)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound || service.getCalls != 1 {
		t.Fatalf("concealed read: status=%d calls=%d body=%s", response.Code, service.getCalls, response.Body.String())
	}
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "whiteboard_not_found" ||
		strings.Contains(response.Body.String(), tenantID.String()) ||
		strings.Contains(response.Body.String(), "foreign") ||
		strings.Contains(response.Body.String(), "forbidden") {
		t.Fatalf("not-found response disclosed resource scope: %s", response.Body.String())
	}
	assertWhiteboardPrivacyHeaders(t, response)
}

func TestWhiteboardGrantExchangeForwardsExactAuthorityAndUsesSensitiveHeaders(t *testing.T) {
	t.Parallel()

	tenantID, actorID, documentID, spaceID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	expiresAt := fixedTime.Add(45 * time.Second)
	service := &fakeWhiteboardService{grantResult: collaboration.GrantCredential{
		Credential: "opaque-sensitive-credential", ProviderURL: "wss://collab.example.invalid",
		DocumentID: documentID, Generation: 4, RevokeGeneration: 7,
		Capability: collaboration.CapabilityEdit, ExpiresAt: expiresAt,
	}, getResult: whiteboardTestDocument(documentID, spaceID)}
	handler := newWhiteboardTestHandler(classIdentityService(tenantID, actorID, nil), service)
	body := `{"capability":"edit","expected_generation":4,"expected_revoke_generation":7}`
	request := whiteboardRequest(
		http.MethodPost,
		whiteboardsCollectionPath+"/"+documentID.String()+"/grant-exchanges",
		body,
		tenantID,
		true,
	)
	request.Header.Set("Origin", "https://classroom.example.invalid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.grantCalls != 1 {
		t.Fatalf("grant exchange: status=%d calls=%d body=%s", response.Code, service.grantCalls, response.Body.String())
	}
	if service.grantInput.Origin != "https://classroom.example.invalid" ||
		service.grantInput.Capability != collaboration.CapabilityEdit ||
		service.grantInput.ExpectedGeneration != 4 ||
		service.grantInput.ExpectedRevokeGeneration != 7 {
		t.Fatalf("unexpected grant authority: %+v", service.grantInput)
	}
	assertWhiteboardPrivacyHeaders(t, response)
	if strings.Contains(request.URL.String(), "opaque-sensitive-credential") ||
		!strings.Contains(response.Body.String(), "opaque-sensitive-credential") {
		t.Fatalf("credential must be response-only: url=%s body=%s", request.URL, response.Body.String())
	}
}

func TestWhiteboardImportValidationIsStrictAndBounded(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	service := &fakeWhiteboardService{importResult: collaboration.ImportValidation{
		Valid: true,
		Manifest: collaboration.ImportManifest{
			FormatVersion: "1", EngineVersion: "0.18.1", AuthorityVersion: "13.6.27",
			SchemaVersion: 1, ContentSHA256: strings.Repeat("a", 64), SizeBytes: 1024,
		},
		Problems: []string{},
	}}
	handler := newWhiteboardTestHandler(classIdentityService(tenantID, actorID, nil), service)
	validBody := `{"format_version":"1","engine_version":"0.18.1","authority_version":"13.6.27","schema_version":1,"content_sha256":"` +
		strings.Repeat("a", 64) + `","size_bytes":1024}`

	unknownRequest := whiteboardRequest(
		http.MethodPost,
		whiteboardImportValidatePath,
		strings.TrimSuffix(validBody, "}")+`,"object_key":"must-not-be-accepted"}`,
		tenantID,
		true,
	)
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknownRequest)
	if unknownResponse.Code != http.StatusBadRequest || service.importCalls != 0 {
		t.Fatalf("unknown import field must fail closed: status=%d calls=%d", unknownResponse.Code, service.importCalls)
	}

	oversizedRequest := whiteboardRequest(
		http.MethodPost,
		whiteboardImportValidatePath,
		`{"padding":"`+strings.Repeat("x", maximumWhiteboardImportBytes)+`"}`,
		tenantID,
		true,
	)
	oversizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(oversizedResponse, oversizedRequest)
	if oversizedResponse.Code != http.StatusBadRequest || service.importCalls != 0 {
		t.Fatalf("oversized import must fail closed: status=%d calls=%d", oversizedResponse.Code, service.importCalls)
	}

	request := whiteboardRequest(http.MethodPost, whiteboardImportValidatePath, validBody, tenantID, true)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.importCalls != 1 {
		t.Fatalf("valid import manifest: status=%d calls=%d body=%s", response.Code, service.importCalls, response.Body.String())
	}
	assertWhiteboardPrivacyHeaders(t, response)
}

func TestWhiteboardRestoreForwardsSnapshotCASAndIdempotency(t *testing.T) {
	t.Parallel()

	tenantID, actorID, documentID, spaceID, snapshotID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &fakeWhiteboardService{restoreResult: whiteboardTestDocument(documentID, spaceID)}
	handler := newWhiteboardTestHandler(classIdentityService(tenantID, actorID, nil), service)
	body := `{"snapshot_id":"` + snapshotID.String() + `","expected_version":8,"expected_generation":3,"idempotency_key":"whiteboard-restore-0001"}`
	request := whiteboardRequest(
		http.MethodPost,
		whiteboardsCollectionPath+"/"+documentID.String()+"/restore",
		body,
		tenantID,
		true,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.restoreCalls != 1 {
		t.Fatalf("restore: status=%d calls=%d body=%s", response.Code, service.restoreCalls, response.Body.String())
	}
	if service.restoreInput.SnapshotID != snapshotID || service.restoreInput.ExpectedVersion != 8 ||
		service.restoreInput.ExpectedGeneration != 3 || service.restoreInput.IdempotencyKey != "whiteboard-restore-0001" {
		t.Fatalf("unexpected restore CAS: %+v", service.restoreInput)
	}
}

func newWhiteboardTestHandler(identityService identity.ServiceAPI, service collaboration.ServiceAPI) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{Clock: fixedClock, Identity: identityService, Collaboration: service},
	)
}

func whiteboardRequest(method, path, body string, tenantID uuid.UUID, mutation bool) *http.Request {
	var request *http.Request
	if body == "" {
		request = httptest.NewRequest(method, path, nil)
	} else {
		request = httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	if tenantID != uuid.Nil {
		request.Header.Set(whiteboardExpectedTenantHeader, tenantID.String())
	}
	if mutation {
		addAuthenticatedMutationCookies(request)
	} else {
		addSessionCookie(request)
	}
	return request
}

func whiteboardTestDocument(documentID, spaceID uuid.UUID) collaboration.Document {
	return collaboration.Document{
		ID: documentID, MediaSpaceID: spaceID, Status: collaboration.DocumentOpen,
		Version: 1, CurrentGeneration: 1, RevokeGeneration: 1,
		Viewer: collaboration.ViewerCapabilities{
			Capability: collaboration.CapabilityEdit, CanExchangeGrant: true,
		},
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}
}

func assertWhiteboardPrivacyHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	cacheControl := response.Header().Get("Cache-Control")
	if (cacheControl != "private, no-store" && cacheControl != "no-store") ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing whiteboard privacy headers: %v", response.Header())
	}
}

func assertWhiteboardAccess(t *testing.T, access collaboration.AccessContext, principal identity.Principal) {
	t.Helper()
	if principal.ActiveTenant == nil || access.TenantID != principal.ActiveTenant.ID ||
		access.ActorID != principal.User.ID || access.SessionID != principal.SessionID || !access.MembershipActive {
		t.Fatalf("whiteboard access=%+v principal=%+v", access, principal)
	}
}

type fakeWhiteboardService struct {
	access        collaboration.AccessContext
	documentID    uuid.UUID
	createCalls   int
	getCalls      int
	grantCalls    int
	importCalls   int
	restoreCalls  int
	createInput   collaboration.CreateInput
	grantInput    collaboration.GrantExchangeInput
	restoreInput  collaboration.RestoreInput
	createResult  collaboration.CreateResult
	getResult     collaboration.Document
	grantResult   collaboration.GrantCredential
	importResult  collaboration.ImportValidation
	restoreResult collaboration.Document
	requestError  error
}

func (service *fakeWhiteboardService) Create(_ context.Context, access collaboration.AccessContext, input collaboration.CreateInput) (collaboration.CreateResult, error) {
	service.createCalls++
	service.access, service.createInput = access, input
	return service.createResult, service.requestError
}

func (service *fakeWhiteboardService) Get(_ context.Context, access collaboration.AccessContext, documentID uuid.UUID) (collaboration.Document, error) {
	service.getCalls++
	service.access, service.documentID = access, documentID
	return service.getResult, service.requestError
}

func (service *fakeWhiteboardService) Capabilities(_ context.Context, _ collaboration.AccessContext, _ uuid.UUID) (collaboration.ViewerCapabilities, error) {
	return service.getResult.Viewer, service.requestError
}

func (service *fakeWhiteboardService) Open(_ context.Context, _ collaboration.AccessContext, _ uuid.UUID, _ collaboration.TransitionInput) (collaboration.Document, error) {
	return service.getResult, service.requestError
}

func (service *fakeWhiteboardService) Suspend(_ context.Context, _ collaboration.AccessContext, _ uuid.UUID, _ collaboration.TransitionInput) (collaboration.Document, error) {
	return service.getResult, service.requestError
}

func (service *fakeWhiteboardService) Resume(_ context.Context, _ collaboration.AccessContext, _ uuid.UUID, _ collaboration.TransitionInput) (collaboration.Document, error) {
	return service.getResult, service.requestError
}

func (service *fakeWhiteboardService) Close(_ context.Context, _ collaboration.AccessContext, _ uuid.UUID, _ collaboration.TransitionInput) (collaboration.Document, error) {
	return service.getResult, service.requestError
}

func (service *fakeWhiteboardService) ExchangeGrant(_ context.Context, access collaboration.AccessContext, documentID uuid.UUID, input collaboration.GrantExchangeInput) (collaboration.GrantCredential, error) {
	service.grantCalls++
	service.access, service.documentID, service.grantInput = access, documentID, input
	return service.grantResult, service.requestError
}

func (service *fakeWhiteboardService) ListSnapshots(_ context.Context, _ collaboration.AccessContext, _ uuid.UUID, _ int) (collaboration.SnapshotList, error) {
	return collaboration.SnapshotList{Items: []collaboration.Snapshot{}}, service.requestError
}

func (service *fakeWhiteboardService) CreateSnapshot(_ context.Context, _ collaboration.AccessContext, _ uuid.UUID, _ collaboration.SnapshotCreateInput) (collaboration.ArtifactCommand, error) {
	return collaboration.ArtifactCommand{}, service.requestError
}

func (service *fakeWhiteboardService) Export(_ context.Context, _ collaboration.AccessContext, _ uuid.UUID, _ collaboration.ExportInput) (collaboration.ArtifactCommand, error) {
	return collaboration.ArtifactCommand{}, service.requestError
}

func (service *fakeWhiteboardService) ValidateImport(_ context.Context, manifest collaboration.ImportManifest) (collaboration.ImportValidation, error) {
	service.importCalls++
	service.importResult.Manifest = manifest
	return service.importResult, service.requestError
}

func (service *fakeWhiteboardService) Restore(_ context.Context, access collaboration.AccessContext, documentID uuid.UUID, input collaboration.RestoreInput) (collaboration.Document, error) {
	service.restoreCalls++
	service.access, service.documentID, service.restoreInput = access, documentID, input
	return service.restoreResult, service.requestError
}
