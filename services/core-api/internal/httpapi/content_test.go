package httpapi

import (
	"context"
	"encoding/hex"
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
	"github.com/tutorhub-v2/core-api/internal/modules/content"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func TestCreateFileUploadIntentUsesTenantCSRFAndPrivacyProjection(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	userID := uuid.New()
	classID := uuid.New()
	requestID := uuid.New()
	fileID := uuid.New()
	service := &fakeContentService{createResult: content.CreateIntentResult{
		Created: true,
		File: content.File{
			ID: fileID, ClassID: classID, CreatorUserID: userID,
			DisplayName: "lesson.pdf", DeclaredMediaType: "application/pdf",
			ExpectedSizeBytes: 42, ExpectedChecksumSHA256: hex.EncodeToString(make([]byte, 32)),
			Status: content.StatusPending, Version: 1,
			UploadExpiresAt: contentTestHTTPTime.Add(15 * time.Minute),
			CreatedAt:       contentTestHTTPTime, UpdatedAt: contentTestHTTPTime,
		},
	}}
	handler := contentTestHandler(classIdentityService(tenantID, userID, nil), service)
	body := `{"class_id":"` + classID.String() + `","display_name":"lesson.pdf","declared_media_type":"application/pdf","expected_size_bytes":42,"checksum_sha256":"` + hex.EncodeToString(make([]byte, 32)) + `","client_request_id":"` + requestID.String() + `"}`
	request := fileMutationRequest(fileUploadIntentsPath, body, tenantID)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d, want 201: %s", response.Code, response.Body.String())
	}
	assertFileHeaders(t, response)
	if service.createAccess.TenantID != tenantID || service.createAccess.ActorID != userID ||
		service.createInput.ClassID != classID || service.createInput.ClientRequestID != requestID {
		t.Fatalf("unexpected create access/input: access=%+v input=%+v", service.createAccess, service.createInput)
	}
	var projected map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &projected); err != nil {
		t.Fatalf("decode file: %v", err)
	}
	for _, privateField := range []string{"tenant_id", "object_key", "storage_etag", "storage_version_id"} {
		if _, exposed := projected[privateField]; exposed {
			t.Fatalf("file response exposed %s: %#v", privateField, projected)
		}
	}
}

func TestFileMutationRequiresCSRFAndExpectedTenant(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	service := &fakeContentService{}
	handler := contentTestHandler(classIdentityService(tenantID, uuid.New(), nil), service)
	body := `{"class_id":"` + uuid.NewString() + `","display_name":"lesson.pdf","declared_media_type":"application/pdf","expected_size_bytes":42,"checksum_sha256":"` + hex.EncodeToString(make([]byte, 32)) + `","client_request_id":"` + uuid.NewString() + `"}`

	missingCSRF := httptest.NewRequest(http.MethodPost, fileUploadIntentsPath, strings.NewReader(body))
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(fileTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, missingCSRF)
	if response.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d, want 403", response.Code)
	}

	wrongTenant := fileMutationRequest(fileUploadIntentsPath, body, uuid.New())
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, wrongTenant)
	assertFileProblem(t, response, http.StatusConflict, "file_scope_changed")
	if service.createCalls != 0 {
		t.Fatal("rejected mutation must not reach content service")
	}
}

func TestFinalizeFileMapsStorageMismatchWithoutLeakingDetails(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	fileID := uuid.New()
	service := &fakeContentService{finalizeErr: content.ErrStorageMismatch}
	handler := contentTestHandler(classIdentityService(tenantID, uuid.New(), nil), service)
	request := fileMutationRequest(
		"/api/v1/files/"+fileID.String()+"/finalize",
		`{"expected_version":1,"storage_version_id":"version-1"}`,
		tenantID,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertFileProblem(t, response, http.StatusConflict, "file_storage_mismatch")
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if strings.Contains(problem.Detail, fileID.String()) {
		t.Fatal("problem detail must not repeat private file identifiers")
	}
}

func TestFileTransferCapabilitiesRequireTenantCSRFAndProjectBearerURL(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	fileID := uuid.New()
	service := &fakeContentService{
		uploadCapability: content.UploadCapability{
			Method: http.MethodPut, URL: "https://storage.example/upload?signature=secret",
			ExpiresAt: contentTestHTTPTime.Add(time.Minute), ContentLengthBytes: 42,
			RequiredHeaders: map[string]string{"Content-Type": "application/pdf"},
		},
		downloadCapability: content.DownloadCapability{
			Method: http.MethodGet, URL: "https://storage.example/download?signature=secret",
			ExpiresAt: contentTestHTTPTime.Add(time.Minute),
		},
	}
	handler := contentTestHandler(classIdentityService(tenantID, uuid.New(), nil), service)
	uploadRequest := fileMutationRequest(
		"/api/v1/files/"+fileID.String()+"/upload-capability",
		`{"expected_version":1}`,
		tenantID,
	)
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, uploadRequest)
	if uploadResponse.Code != http.StatusOK ||
		!strings.Contains(uploadResponse.Body.String(), `"content_length_bytes":42`) {
		t.Fatalf("upload capability status=%d body=%s", uploadResponse.Code, uploadResponse.Body.String())
	}
	assertFileHeaders(t, uploadResponse)
	if service.uploadCapabilityFileID != fileID || service.uploadCapabilityInput.ExpectedVersion != 1 {
		t.Fatalf("unexpected upload capability input: %s %+v", service.uploadCapabilityFileID, service.uploadCapabilityInput)
	}

	downloadRequest := fileMutationRequest(
		"/api/v1/files/"+fileID.String()+"/download-capability", "{}", tenantID,
	)
	downloadResponse := httptest.NewRecorder()
	handler.ServeHTTP(downloadResponse, downloadRequest)
	if downloadResponse.Code != http.StatusOK ||
		!strings.Contains(downloadResponse.Body.String(), `"method":"GET"`) {
		t.Fatalf("download capability status=%d body=%s", downloadResponse.Code, downloadResponse.Body.String())
	}
	assertFileHeaders(t, downloadResponse)
}

func TestFileMetadataUnavailableWithoutService(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	handler := contentTestHandler(classIdentityService(tenantID, uuid.New(), nil), nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+uuid.NewString(), nil)
	request.Header.Set(fileTenantHeader, tenantID.String())
	addSessionCookie(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertFileProblem(t, response, http.StatusServiceUnavailable, "file_unavailable")
}

var contentTestHTTPTime = time.Date(2026, time.August, 7, 10, 0, 0, 0, time.UTC)

func contentTestHandler(identityService identity.ServiceAPI, service content.ServiceAPI) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{
			Clock:    func() time.Time { return contentTestHTTPTime },
			Identity: identityService, Content: service,
		},
	)
}

func fileMutationRequest(path, body string, tenantID uuid.UUID) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	request.Header.Set(fileTenantHeader, tenantID.String())
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	return request
}

func assertFileHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" ||
		!headerContainsValue(response.Header().Values("Vary"), "Cookie") ||
		!headerContainsValue(response.Header().Values("Vary"), fileTenantHeader) {
		t.Fatalf("unexpected file headers: %v", response.Header())
	}
}

func assertFileProblem(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d, want %d: %s", response.Code, status, response.Body.String())
	}
	assertFileHeaders(t, response)
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != code {
		t.Fatalf("problem=%+v, want code %q", problem, code)
	}
}

type fakeContentService struct {
	createResult           content.CreateIntentResult
	createErr              error
	createAccess           content.AccessContext
	createInput            content.CreateIntentInput
	createCalls            int
	getFile                content.File
	getErr                 error
	finalizeFile           content.File
	finalizeErr            error
	uploadCapability       content.UploadCapability
	uploadCapabilityErr    error
	uploadCapabilityFileID uuid.UUID
	uploadCapabilityInput  content.UploadCapabilityInput
	downloadCapability     content.DownloadCapability
	downloadCapabilityErr  error
}

func (service *fakeContentService) CreateIntent(
	_ context.Context,
	access content.AccessContext,
	input content.CreateIntentInput,
) (content.CreateIntentResult, error) {
	service.createCalls++
	service.createAccess = access
	service.createInput = input
	return service.createResult, service.createErr
}

func (service *fakeContentService) Get(
	context.Context, content.AccessContext, uuid.UUID,
) (content.File, error) {
	return service.getFile, service.getErr
}

func (service *fakeContentService) Finalize(
	context.Context, content.AccessContext, uuid.UUID, content.FinalizeInput,
) (content.File, error) {
	return service.finalizeFile, service.finalizeErr
}

func (service *fakeContentService) IssueUploadCapability(
	_ context.Context,
	_ content.AccessContext,
	fileID uuid.UUID,
	input content.UploadCapabilityInput,
) (content.UploadCapability, error) {
	service.uploadCapabilityFileID = fileID
	service.uploadCapabilityInput = input
	return service.uploadCapability, service.uploadCapabilityErr
}

func (service *fakeContentService) IssueDownloadCapability(
	context.Context, content.AccessContext, uuid.UUID,
) (content.DownloadCapability, error) {
	return service.downloadCapability, service.downloadCapabilityErr
}

var _ content.ServiceAPI = (*fakeContentService)(nil)
