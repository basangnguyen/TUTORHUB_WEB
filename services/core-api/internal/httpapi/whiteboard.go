package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/collaboration"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	whiteboardsCollectionPath      = "/api/v1/whiteboards"
	whiteboardResourcePattern      = "/api/v1/whiteboards/{document_id}"
	whiteboardOpenPattern          = "/api/v1/whiteboards/{document_id}/open"
	whiteboardSuspendPattern       = "/api/v1/whiteboards/{document_id}/suspend"
	whiteboardResumePattern        = "/api/v1/whiteboards/{document_id}/resume"
	whiteboardClosePattern         = "/api/v1/whiteboards/{document_id}/close"
	whiteboardCapabilitiesPattern  = "/api/v1/whiteboards/{document_id}/capabilities"
	whiteboardGrantExchangePattern = "/api/v1/whiteboards/{document_id}/grant-exchanges"
	whiteboardSnapshotsPattern     = "/api/v1/whiteboards/{document_id}/snapshots"
	whiteboardExportsPattern       = "/api/v1/whiteboards/{document_id}/exports"
	whiteboardRestorePattern       = "/api/v1/whiteboards/{document_id}/restore"
	whiteboardImportValidatePath   = "/api/v1/whiteboard-imports/validate"
	whiteboardExpectedTenantHeader = "X-TutorHub-Expected-Tenant-ID"
	maximumWhiteboardRequestBytes  = 16 * 1024
	maximumWhiteboardImportBytes   = 64 * 1024
)

var errWhiteboardScopeChanged = errors.New("whiteboard active tenant changed")

type whiteboardHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service collaboration.ServiceAPI
}

type createWhiteboardRequest struct {
	MediaSpaceID   *uuid.UUID `json:"media_space_id"`
	IdempotencyKey *string    `json:"idempotency_key"`
}

type whiteboardTransitionRequest struct {
	ExpectedVersion *int64  `json:"expected_version"`
	IdempotencyKey  *string `json:"idempotency_key"`
}

type whiteboardGrantExchangeRequest struct {
	Capability               *collaboration.Capability `json:"capability"`
	ExpectedGeneration       *int64                    `json:"expected_generation"`
	ExpectedRevokeGeneration *int64                    `json:"expected_revoke_generation"`
}

type whiteboardArtifactRequest struct {
	ExpectedGeneration *int64  `json:"expected_generation"`
	IdempotencyKey     *string `json:"idempotency_key"`
}

type whiteboardRestoreRequest struct {
	SnapshotID         *uuid.UUID `json:"snapshot_id"`
	ExpectedVersion    *int64     `json:"expected_version"`
	ExpectedGeneration *int64     `json:"expected_generation"`
	IdempotencyKey     *string    `json:"idempotency_key"`
}

func newWhiteboardHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service collaboration.ServiceAPI,
) whiteboardHandlers {
	return whiteboardHandlers{logger: logger, auth: auth, service: service}
}

func whiteboardResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", whiteboardExpectedTenantHeader)
		w.Header().Add("Vary", "Origin")
		next.ServeHTTP(w, r)
	})
}

func (handlers whiteboardHandlers) collection(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		handlers.resolve(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "The whiteboard collection does not support this HTTP method.")
		return
	}
	principal, ok := handlers.principal(w, r, true)
	if !ok {
		return
	}
	var request createWhiteboardRequest
	if err := decodeJSONRequest(w, r, &request, maximumWhiteboardRequestBytes); err != nil ||
		request.MediaSpaceID == nil || request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
		return
	}
	result, err := handlers.service.Create(r.Context(), whiteboardAccess(principal), collaboration.CreateInput{
		MediaSpaceID: *request.MediaSpaceID, IdempotencyKey: *request.IdempotencyKey,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(handlers.logger, w, status, result.Document)
}

func (handlers whiteboardHandlers) resolve(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r, false)
	if !ok {
		return
	}
	mediaSpaceID, valid := parseResourceUUID(strings.TrimSpace(r.URL.Query().Get("media_space_id")))
	if !valid {
		handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
		return
	}
	document, err := handlers.service.Resolve(r.Context(), whiteboardAccess(principal), mediaSpaceID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, document)
}

func (handlers whiteboardHandlers) resource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWhiteboardMethodProblem(w, r, http.MethodGet)
		return
	}
	principal, documentID, ok := handlers.resourcePrincipal(w, r, false)
	if !ok {
		return
	}
	document, err := handlers.service.Get(r.Context(), whiteboardAccess(principal), documentID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, document)
}

func (handlers whiteboardHandlers) capabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeWhiteboardMethodProblem(w, r, http.MethodGet)
		return
	}
	principal, documentID, ok := handlers.resourcePrincipal(w, r, false)
	if !ok {
		return
	}
	capabilities, err := handlers.service.Capabilities(r.Context(), whiteboardAccess(principal), documentID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, capabilities)
}

func (handlers whiteboardHandlers) transition(operation string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeWhiteboardMethodProblem(w, r, http.MethodPost)
			return
		}
		principal, documentID, ok := handlers.resourcePrincipal(w, r, true)
		if !ok {
			return
		}
		var request whiteboardTransitionRequest
		if err := decodeJSONRequest(w, r, &request, maximumWhiteboardRequestBytes); err != nil ||
			request.ExpectedVersion == nil || request.IdempotencyKey == nil {
			handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
			return
		}
		input := collaboration.TransitionInput{
			ExpectedVersion: *request.ExpectedVersion, IdempotencyKey: *request.IdempotencyKey,
		}
		var document collaboration.Document
		var err error
		switch operation {
		case "open":
			document, err = handlers.service.Open(r.Context(), whiteboardAccess(principal), documentID, input)
		case "suspend":
			document, err = handlers.service.Suspend(r.Context(), whiteboardAccess(principal), documentID, input)
		case "resume":
			document, err = handlers.service.Resume(r.Context(), whiteboardAccess(principal), documentID, input)
		case "close":
			document, err = handlers.service.Close(r.Context(), whiteboardAccess(principal), documentID, input)
		default:
			err = collaboration.ErrInvalidRequest
		}
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, document)
	})
}

func (handlers whiteboardHandlers) grantExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWhiteboardMethodProblem(w, r, http.MethodPost)
		return
	}
	principal, documentID, ok := handlers.resourcePrincipal(w, r, true)
	if !ok {
		return
	}
	var request whiteboardGrantExchangeRequest
	if err := decodeJSONRequest(w, r, &request, maximumWhiteboardRequestBytes); err != nil ||
		request.Capability == nil || request.ExpectedGeneration == nil ||
		request.ExpectedRevokeGeneration == nil {
		handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
		return
	}
	credential, err := handlers.service.ExchangeGrant(
		r.Context(), whiteboardAccess(principal), documentID, collaboration.GrantExchangeInput{
			Capability: *request.Capability, ExpectedGeneration: *request.ExpectedGeneration,
			ExpectedRevokeGeneration: *request.ExpectedRevokeGeneration,
			Origin:                   strings.TrimSpace(r.Header.Get("Origin")),
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, credential)
}

func (handlers whiteboardHandlers) snapshots(w http.ResponseWriter, r *http.Request) {
	principal, documentID, ok := handlers.resourcePrincipal(w, r, r.Method == http.MethodPost)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		limit := 0
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
				return
			}
			limit = parsed
		}
		result, err := handlers.service.ListSnapshots(r.Context(), whiteboardAccess(principal), documentID, limit)
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, result)
	case http.MethodPost:
		request, ok := handlers.artifactRequest(w, r)
		if !ok {
			return
		}
		result, err := handlers.service.CreateSnapshot(r.Context(), whiteboardAccess(principal), documentID, collaboration.SnapshotCreateInput{
			ExpectedGeneration: *request.ExpectedGeneration, IdempotencyKey: *request.IdempotencyKey,
		})
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusAccepted, result)
	default:
		writeWhiteboardMethodProblem(w, r, http.MethodGet+", "+http.MethodPost)
	}
}

func (handlers whiteboardHandlers) export(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWhiteboardMethodProblem(w, r, http.MethodPost)
		return
	}
	principal, documentID, ok := handlers.resourcePrincipal(w, r, true)
	if !ok {
		return
	}
	request, ok := handlers.artifactRequest(w, r)
	if !ok {
		return
	}
	result, err := handlers.service.Export(r.Context(), whiteboardAccess(principal), documentID, collaboration.ExportInput{
		ExpectedGeneration: *request.ExpectedGeneration, IdempotencyKey: *request.IdempotencyKey,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusAccepted, result)
}

func (handlers whiteboardHandlers) validateImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWhiteboardMethodProblem(w, r, http.MethodPost)
		return
	}
	if _, ok := handlers.principal(w, r, true); !ok {
		return
	}
	var request collaboration.ImportManifest
	if err := decodeJSONRequest(w, r, &request, maximumWhiteboardImportBytes); err != nil {
		handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
		return
	}
	result, err := handlers.service.ValidateImport(r.Context(), request)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers whiteboardHandlers) restore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeWhiteboardMethodProblem(w, r, http.MethodPost)
		return
	}
	principal, documentID, ok := handlers.resourcePrincipal(w, r, true)
	if !ok {
		return
	}
	var request whiteboardRestoreRequest
	if err := decodeJSONRequest(w, r, &request, maximumWhiteboardRequestBytes); err != nil ||
		request.SnapshotID == nil || request.ExpectedVersion == nil ||
		request.ExpectedGeneration == nil || request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
		return
	}
	document, err := handlers.service.Restore(r.Context(), whiteboardAccess(principal), documentID, collaboration.RestoreInput{
		SnapshotID: *request.SnapshotID, ExpectedVersion: *request.ExpectedVersion,
		ExpectedGeneration: *request.ExpectedGeneration, IdempotencyKey: *request.IdempotencyKey,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, document)
}

func (handlers whiteboardHandlers) artifactRequest(w http.ResponseWriter, r *http.Request) (whiteboardArtifactRequest, bool) {
	var request whiteboardArtifactRequest
	if err := decodeJSONRequest(w, r, &request, maximumWhiteboardRequestBytes); err != nil ||
		request.ExpectedGeneration == nil || request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
		return whiteboardArtifactRequest{}, false
	}
	return request, true
}

func (handlers whiteboardHandlers) resourcePrincipal(
	w http.ResponseWriter,
	r *http.Request,
	csrf bool,
) (identity.Principal, uuid.UUID, bool) {
	principal, ok := handlers.principal(w, r, csrf)
	if !ok {
		return identity.Principal{}, uuid.Nil, false
	}
	documentID, valid := parseResourceUUID(r.PathValue("document_id"))
	if !valid {
		handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
		return identity.Principal{}, uuid.Nil, false
	}
	return principal, documentID, true
}

func (handlers whiteboardHandlers) principal(
	w http.ResponseWriter,
	r *http.Request,
	csrf bool,
) (identity.Principal, bool) {
	if !handlers.auth.available(w, r) {
		return identity.Principal{}, false
	}
	var principal identity.Principal
	var ok bool
	if csrf {
		sessionToken, sessionOK := handlers.auth.sessionToken(w, r)
		if !sessionOK {
			return identity.Principal{}, false
		}
		principal, ok = handlers.auth.csrfPrincipal(w, r, sessionToken)
	} else {
		principal, ok = handlers.auth.authenticatedPrincipal(w, r)
	}
	if !ok {
		return identity.Principal{}, false
	}
	if principal.ActiveTenant == nil || !principal.ActiveTenant.IsActive {
		handlers.writeProblem(w, r, collaboration.ErrNotFound)
		return identity.Principal{}, false
	}
	expectedTenantID, valid := parseResourceUUID(strings.TrimSpace(r.Header.Get(whiteboardExpectedTenantHeader)))
	if !valid {
		handlers.writeProblem(w, r, collaboration.ErrInvalidRequest)
		return identity.Principal{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errWhiteboardScopeChanged)
		return identity.Principal{}, false
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, collaboration.ErrUnavailable)
		return identity.Principal{}, false
	}
	return principal, true
}

func whiteboardAccess(principal identity.Principal) collaboration.AccessContext {
	access := collaboration.AccessContext{ActorID: principal.User.ID, SessionID: principal.SessionID}
	if principal.ActiveTenant != nil {
		access.TenantID = principal.ActiveTenant.ID
		access.MembershipActive = principal.ActiveTenant.IsActive
		access.OrganizationRoles = []policy.OrganizationRole{policy.OrganizationRole(principal.ActiveTenant.Role)}
	}
	return access
}

func (handlers whiteboardHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusServiceUnavailable, "whiteboard_unavailable"
	title, detail := "Whiteboard unavailable", "The whiteboard request could not be completed safely."
	switch {
	case errors.Is(err, collaboration.ErrInvalidRequest):
		status, code = http.StatusBadRequest, "whiteboard_invalid"
		title, detail = "Invalid whiteboard request", "Review the tenant assertion, opaque identifiers, bounded fields, and optimistic versions."
	case errors.Is(err, collaboration.ErrNotFound):
		status, code = http.StatusNotFound, "whiteboard_not_found"
		title, detail = "Whiteboard unavailable", "The whiteboard is unavailable in the active workspace."
	case errors.Is(err, collaboration.ErrVersionConflict):
		status, code = http.StatusConflict, "stale_version"
		title, detail = "Whiteboard changed", "Reload the current whiteboard authority before retrying."
	case errors.Is(err, collaboration.ErrIdempotencyConflict):
		status, code = http.StatusConflict, "whiteboard_idempotency_conflict"
		title, detail = "Whiteboard retry conflict", "Use a new idempotency key after changing the command."
	case errors.Is(err, collaboration.ErrTransitionConflict):
		status, code = http.StatusConflict, "whiteboard_transition_conflict"
		title, detail = "Whiteboard transition unavailable", "Reload the whiteboard before choosing another lifecycle action."
	case errors.Is(err, errWhiteboardScopeChanged):
		status, code = http.StatusConflict, "whiteboard_scope_changed"
		title, detail = "Active workspace changed", "Reload the current workspace before retrying."
	case errors.Is(err, collaboration.ErrGrantUnavailable):
		status, code = http.StatusServiceUnavailable, "whiteboard_grant_unavailable"
		title, detail = "Whiteboard grant unavailable", "Credential exchange is not available in this environment."
	case errors.Is(err, collaboration.ErrGrantRateLimited):
		status, code = http.StatusTooManyRequests, "whiteboard_grant_rate_limited"
		title, detail = "Whiteboard grant rate limited", "Wait briefly before requesting another whiteboard credential."
	case errors.Is(err, collaboration.ErrGrantDenied):
		status, code = http.StatusNotFound, "whiteboard_not_found"
		title, detail = "Whiteboard unavailable", "The whiteboard is unavailable in the active workspace."
	case errors.Is(err, collaboration.ErrArtifactUnavailable):
		status, code = http.StatusServiceUnavailable, "whiteboard_artifact_unavailable"
		title, detail = "Whiteboard artifact workflow unavailable", "Snapshot and export processing is not available in this environment."
	case errors.Is(err, collaboration.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		// Keep the privacy-safe default.
	default:
		handlers.logger.Error(
			"whiteboard request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}

func writeWhiteboardMethodProblem(w http.ResponseWriter, r *http.Request, allow string) {
	w.Header().Set("Allow", allow)
	writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "The whiteboard resource does not support this HTTP method.")
}
