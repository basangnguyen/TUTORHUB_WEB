package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/content"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	fileUploadIntentsPath          = "/api/v1/files/upload-intents"
	classFilesPattern              = "/api/v1/classes/{class_id}/files"
	fileResourcePattern            = "/api/v1/files/{file_id}"
	fileFinalizePattern            = "/api/v1/files/{file_id}/finalize"
	fileUploadCapabilityPattern    = "/api/v1/files/{file_id}/upload-capability"
	fileDownloadCapabilityPattern  = "/api/v1/files/{file_id}/download-capability"
	fileMultipartCollectionPattern = "/api/v1/files/{file_id}/multipart-uploads"
	fileMultipartPartPattern       = "/api/v1/files/{file_id}/multipart-uploads/{multipart_id}/parts/{part_number}/capability"
	fileMultipartCompletePattern   = "/api/v1/files/{file_id}/multipart-uploads/{multipart_id}/complete"
	fileMultipartAbortPattern      = "/api/v1/files/{file_id}/multipart-uploads/{multipart_id}/abort"
	fileTenantHeader               = "X-TutorHub-Expected-Tenant-ID"
	maximumFileBodySize            = 8 * 1024
)

var errFileScopeChanged = errors.New("file active tenant changed")

type contentHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service content.ServiceAPI
}

type createFileUploadIntentRequest struct {
	ClassID           *string `json:"class_id"`
	DisplayName       *string `json:"display_name"`
	DeclaredMediaType *string `json:"declared_media_type"`
	ExpectedSizeBytes *int64  `json:"expected_size_bytes"`
	ChecksumSHA256    *string `json:"checksum_sha256"`
	ClientRequestID   *string `json:"client_request_id"`
}

type finalizeFileRequest struct {
	ExpectedVersion  *int64  `json:"expected_version"`
	StorageVersionID *string `json:"storage_version_id"`
}

type fileUploadCapabilityRequest struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

type multipartCreateRequest struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

type multipartPartCapabilityRequest struct {
	ExpectedVersion    *int64 `json:"expected_version"`
	ContentLengthBytes *int64 `json:"content_length_bytes"`
}

type multipartCompletedPartRequest struct {
	PartNumber *int32  `json:"part_number"`
	ETag       *string `json:"etag"`
}

type multipartCompleteRequest struct {
	ExpectedVersion *int64                           `json:"expected_version"`
	Parts           *[]multipartCompletedPartRequest `json:"parts"`
}

type classFilePageResponse struct {
	Items        []content.File                 `json:"items"`
	NextCursor   *string                        `json:"next_cursor"`
	ViewerAccess content.ClassFilesViewerAccess `json:"viewer_access"`
}

func newContentHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service content.ServiceAPI,
) contentHandlers {
	return contentHandlers{logger: logger, auth: auth, service: service}
}

func fileResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", fileTenantHeader)
		next.ServeHTTP(w, r)
	})
}

func (handlers contentHandlers) createIntent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "File upload intent creation supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	var request createFileUploadIntentRequest
	if err := decodeJSONRequest(w, r, &request, maximumFileBodySize); err != nil ||
		request.ClassID == nil || request.DisplayName == nil ||
		request.DeclaredMediaType == nil || request.ExpectedSizeBytes == nil ||
		request.ChecksumSHA256 == nil || request.ClientRequestID == nil {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	classID, classOK := parseResourceUUID(*request.ClassID)
	clientRequestID, requestOK := parseResourceUUID(*request.ClientRequestID)
	if !classOK || !requestOK {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	result, err := handlers.service.CreateIntent(r.Context(), access, content.CreateIntentInput{
		ClassID: classID, DisplayName: *request.DisplayName,
		DeclaredMediaType: *request.DeclaredMediaType,
		ExpectedSizeBytes: *request.ExpectedSizeBytes,
		ChecksumSHA256:    *request.ChecksumSHA256, ClientRequestID: clientRequestID,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(handlers.logger, w, status, result.File)
}

func (handlers contentHandlers) resource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "File metadata supports GET and HEAD requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	fileID, ok := parseResourceUUID(r.PathValue("file_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return
	}
	item, err := handlers.service.Get(r.Context(), access, fileID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, item)
}

func (handlers contentHandlers) list(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Class file listing supports GET and HEAD requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	classID, ok := parseResourceUUID(r.PathValue("class_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return
	}
	limit := 0
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		value, err := strconv.Atoi(rawLimit)
		if err != nil {
			handlers.writeProblem(w, r, content.ErrInvalidInput)
			return
		}
		limit = value
	}
	page, err := handlers.service.List(r.Context(), access, classID, content.ListFilesInput{
		Limit: limit, Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	response := classFilePageResponse{Items: page.Items, ViewerAccess: page.ViewerAccess}
	if response.Items == nil {
		response.Items = []content.File{}
	}
	if page.NextCursor != "" {
		response.NextCursor = &page.NextCursor
	}
	writeJSON(handlers.logger, w, http.StatusOK, response)
}

func (handlers contentHandlers) finalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "File finalize supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	fileID, ok := parseResourceUUID(r.PathValue("file_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return
	}
	var request finalizeFileRequest
	if err := decodeJSONRequest(w, r, &request, maximumFileBodySize); err != nil ||
		request.ExpectedVersion == nil || request.StorageVersionID == nil {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	item, err := handlers.service.Finalize(
		r.Context(), access, fileID,
		content.FinalizeInput{
			ExpectedVersion:  *request.ExpectedVersion,
			StorageVersionID: *request.StorageVersionID,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, item)
}

func (handlers contentHandlers) uploadCapability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "File upload capability supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	fileID, ok := parseResourceUUID(r.PathValue("file_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return
	}
	var request fileUploadCapabilityRequest
	if err := decodeJSONRequest(w, r, &request, maximumFileBodySize); err != nil ||
		request.ExpectedVersion == nil {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	capability, err := handlers.service.IssueUploadCapability(
		r.Context(), access, fileID,
		content.UploadCapabilityInput{ExpectedVersion: *request.ExpectedVersion},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, capability)
}

func (handlers contentHandlers) downloadCapability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "File download capability supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return
	}
	fileID, ok := parseResourceUUID(r.PathValue("file_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return
	}
	capability, err := handlers.service.IssueDownloadCapability(r.Context(), access, fileID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, capability)
}

func (handlers contentHandlers) multipartCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Multipart upload creation supports POST requests.")
		return
	}
	principal, access, fileID, ok := handlers.multipartAccess(w, r)
	_ = principal
	if !ok {
		return
	}
	var request multipartCreateRequest
	if err := decodeJSONRequest(w, r, &request, maximumFileBodySize); err != nil || request.ExpectedVersion == nil {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	upload, err := handlers.service.CreateMultipart(
		r.Context(), access, fileID, content.CreateMultipartInput{ExpectedVersion: *request.ExpectedVersion},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusCreated, upload)
}

func (handlers contentHandlers) multipartPartCapability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Multipart part capability supports POST requests.")
		return
	}
	_, access, fileID, ok := handlers.multipartAccess(w, r)
	if !ok {
		return
	}
	multipartID, ok := parseResourceUUID(r.PathValue("multipart_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return
	}
	partValue, err := strconv.ParseInt(r.PathValue("part_number"), 10, 32)
	if err != nil || partValue < 1 || partValue > 10000 {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	var request multipartPartCapabilityRequest
	if err := decodeJSONRequest(w, r, &request, maximumFileBodySize); err != nil ||
		request.ExpectedVersion == nil || request.ContentLengthBytes == nil {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	capability, err := handlers.service.IssueMultipartPartCapability(
		r.Context(), access, fileID, multipartID, int32(partValue),
		content.MultipartPartCapabilityInput{
			ExpectedVersion:    *request.ExpectedVersion,
			ContentLengthBytes: *request.ContentLengthBytes,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, capability)
}

func (handlers contentHandlers) multipartComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Multipart completion supports POST requests.")
		return
	}
	_, access, fileID, ok := handlers.multipartAccess(w, r)
	if !ok {
		return
	}
	multipartID, ok := parseResourceUUID(r.PathValue("multipart_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return
	}
	var request multipartCompleteRequest
	if err := decodeJSONRequest(w, r, &request, maximumFileBodySize); err != nil ||
		request.ExpectedVersion == nil || request.Parts == nil {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	parts := make([]content.MultipartCompletedPart, len(*request.Parts))
	for index, part := range *request.Parts {
		if part.PartNumber == nil || part.ETag == nil {
			handlers.writeProblem(w, r, content.ErrInvalidInput)
			return
		}
		parts[index] = content.MultipartCompletedPart{PartNumber: *part.PartNumber, ETag: *part.ETag}
	}
	result, err := handlers.service.CompleteMultipart(
		r.Context(), access, fileID, multipartID,
		content.CompleteMultipartInput{ExpectedVersion: *request.ExpectedVersion, Parts: parts},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers contentHandlers) multipartAbort(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Multipart abort supports POST requests.")
		return
	}
	_, access, fileID, ok := handlers.multipartAccess(w, r)
	if !ok {
		return
	}
	multipartID, ok := parseResourceUUID(r.PathValue("multipart_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return
	}
	var request multipartCreateRequest
	if err := decodeJSONRequest(w, r, &request, maximumFileBodySize); err != nil || request.ExpectedVersion == nil {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return
	}
	upload, err := handlers.service.AbortMultipart(
		r.Context(), access, fileID, multipartID,
		content.AbortMultipartInput{ExpectedVersion: *request.ExpectedVersion},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, upload)
}

func (handlers contentHandlers) multipartAccess(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, content.AccessContext, uuid.UUID, bool) {
	if !handlers.available(w, r) {
		return identity.Principal{}, content.AccessContext{}, uuid.Nil, false
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return identity.Principal{}, content.AccessContext{}, uuid.Nil, false
	}
	access, ok := handlers.access(w, r, principal)
	if !ok {
		return identity.Principal{}, content.AccessContext{}, uuid.Nil, false
	}
	fileID, ok := parseResourceUUID(r.PathValue("file_id"))
	if !ok {
		handlers.writeProblem(w, r, content.ErrNotFound)
		return identity.Principal{}, content.AccessContext{}, uuid.Nil, false
	}
	return principal, access, fileID, true
}

func (handlers contentHandlers) access(
	w http.ResponseWriter,
	r *http.Request,
	principal identity.Principal,
) (content.AccessContext, bool) {
	if principal.ActiveTenant == nil {
		handlers.writeProblem(w, r, content.ErrAccessDenied)
		return content.AccessContext{}, false
	}
	expectedTenantID, ok := parseResourceUUID(strings.TrimSpace(r.Header.Get(fileTenantHeader)))
	if !ok {
		handlers.writeProblem(w, r, content.ErrInvalidInput)
		return content.AccessContext{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errFileScopeChanged)
		return content.AccessContext{}, false
	}
	return content.AccessContext{
		TenantID: principal.ActiveTenant.ID, ActorID: principal.User.ID,
		MembershipActive: principal.ActiveTenant.IsActive,
		OrganizationRoles: []policy.OrganizationRole{
			policy.OrganizationRole(principal.ActiveTenant.Role),
		},
	}, true
}

func (handlers contentHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers contentHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.service == nil {
		writeCodedProblem(w, r, http.StatusServiceUnavailable, "file_unavailable", "Files unavailable", "File metadata is not configured for this environment.")
		return false
	}
	return true
}

func (handlers contentHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusServiceUnavailable, "file_unavailable"
	title, detail := "File request failed", "The file request could not be completed safely."
	switch {
	case errors.Is(err, content.ErrInvalidInput):
		status, code = http.StatusBadRequest, "file_invalid"
		title, detail = "Invalid file request", "Review the class, filename, media type, byte length, checksum and request identifier."
	case errors.Is(err, content.ErrAccessDenied):
		status, code = http.StatusForbidden, "file_forbidden"
		title, detail = "File access denied", "The active workspace cannot authorize this file request."
	case errors.Is(err, content.ErrNotFound):
		status, code = http.StatusNotFound, "file_not_found"
		title, detail = "File unavailable", "The file is unavailable in the active workspace."
	case errors.Is(err, content.ErrReadOnly):
		status, code = http.StatusConflict, "file_class_read_only"
		title, detail = "Class is read only", "Archived classes cannot create or finalize file uploads."
	case errors.Is(err, content.ErrIdempotencyConflict):
		status, code = http.StatusConflict, "file_idempotency_conflict"
		title, detail = "Upload retry conflict", "Use a new client request identifier after changing file metadata."
	case errors.Is(err, content.ErrIntentExpired):
		status, code = http.StatusConflict, "file_intent_expired"
		title, detail = "Upload intent expired", "Create a new upload intent before retrying the transfer."
	case errors.Is(err, content.ErrStorageMismatch):
		status, code = http.StatusConflict, "file_storage_mismatch"
		title, detail = "Stored file does not match", "The uploaded object does not match the reserved file metadata."
	case errors.Is(err, content.ErrNotReady):
		status, code = http.StatusConflict, "file_not_ready"
		title, detail = "File is not ready", "Wait for file processing to finish before requesting a download."
	case errors.Is(err, content.ErrVersionConflict):
		status, code = http.StatusConflict, "file_version_conflict"
		title, detail = "File changed", "Reload the latest file metadata before retrying finalize."
	case errors.Is(err, content.ErrMultipartConflict):
		status, code = http.StatusConflict, "file_multipart_conflict"
		title, detail = "Multipart upload changed", "Reload the multipart upload before retrying the transfer."
	case errors.Is(err, content.ErrMultipartExpired):
		status, code = http.StatusConflict, "file_multipart_expired"
		title, detail = "Multipart upload expired", "Create a new upload intent before retrying the transfer."
	case errors.Is(err, errFileScopeChanged):
		status, code = http.StatusConflict, "file_scope_changed"
		title, detail = "Active workspace changed", "Reload the current session before retrying the file request."
	default:
		handlers.logger.Error(
			"file request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
