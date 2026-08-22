package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/collaboration"
)

const (
	whiteboardInternalGrantExchangePath = "/internal/v1/collaboration/grants/exchange"
	whiteboardInternalGrantValidatePath = "/internal/v1/collaboration/grants/validate"
	whiteboardInternalRuntimeStatePath  = "/internal/v1/collaboration/runtime-state"
	maximumWhiteboardInternalBodyBytes  = 128 * 1024
	maximumWhiteboardValidationScopes   = 100
)

type whiteboardInternalHandlers struct {
	logger       *slog.Logger
	service      collaboration.InternalServiceAPI
	serviceToken [sha256.Size]byte
	runtimeMode  string
}

type whiteboardInternalGrantRequest struct {
	Grant                *string `json:"grant"`
	Origin               *string `json:"origin"`
	ProviderDocumentName *string `json:"provider_document_name"`
}

type whiteboardInternalValidationRequest struct {
	Scopes []whiteboardInternalScopeRequest `json:"scopes"`
}

type whiteboardInternalScopeRequest struct {
	AuthorityLease           *string                   `json:"authority_lease"`
	ActorID                  *uuid.UUID                `json:"actor_id"`
	Capability               *collaboration.Capability `json:"capability"`
	DocumentID               *uuid.UUID                `json:"document_id"`
	Generation               *int64                    `json:"generation"`
	MaxConnectionsPerTenant  *int64                    `json:"max_connections_per_tenant"`
	MaxOperationsPerMinute   *int64                    `json:"max_operations_per_minute"`
	MaxStorageBytesPerTenant *int64                    `json:"max_storage_bytes_per_tenant"`
	Origin                   *string                   `json:"origin"`
	ProviderDocumentName     *string                   `json:"provider_document_name"`
	SessionID                *uuid.UUID                `json:"session_id"`
	TenantID                 *uuid.UUID                `json:"tenant_id"`
	WriterFence              *int64                    `json:"writer_fence"`
}

type whiteboardInternalValidationResponse struct {
	ValidAuthorityLeases []string `json:"valid_authority_leases"`
}

func newWhiteboardInternalHandlers(
	logger *slog.Logger,
	service collaboration.InternalServiceAPI,
	serviceToken string,
	runtimeModes ...string,
) whiteboardInternalHandlers {
	runtimeMode := "enabled"
	if len(runtimeModes) > 0 && (runtimeModes[0] == "enabled" || runtimeModes[0] == "read_only" || runtimeModes[0] == "off") {
		runtimeMode = runtimeModes[0]
	}
	return whiteboardInternalHandlers{
		logger: logger, service: service,
		serviceToken: sha256.Sum256([]byte(serviceToken)),
		runtimeMode:  runtimeMode,
	}
}

func (handlers whiteboardInternalHandlers) runtimeState(w http.ResponseWriter, r *http.Request) {
	if !handlers.authorized(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		writeWhiteboardInternalProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, map[string]string{"mode": handlers.runtimeMode})
}

func (handlers whiteboardInternalHandlers) exchangeGrant(w http.ResponseWriter, r *http.Request) {
	if !handlers.authorized(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeWhiteboardInternalProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	var request whiteboardInternalGrantRequest
	if err := decodeJSONRequest(w, r, &request, maximumWhiteboardInternalBodyBytes); err != nil ||
		request.Grant == nil || request.Origin == nil || request.ProviderDocumentName == nil {
		writeWhiteboardInternalProblem(w, r, http.StatusBadRequest, "invalid_request")
		return
	}
	scope, err := handlers.service.ConsumeGrant(r.Context(), collaboration.GrantConsumeInput{
		Credential: *request.Grant, Origin: *request.Origin,
		ProviderDocumentName: *request.ProviderDocumentName,
	})
	if err != nil {
		handlers.writeGrantProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, scope)
}

func (handlers whiteboardInternalHandlers) validateGrants(w http.ResponseWriter, r *http.Request) {
	if !handlers.authorized(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		writeWhiteboardInternalProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	var request whiteboardInternalValidationRequest
	if err := decodeJSONRequest(w, r, &request, maximumWhiteboardInternalBodyBytes); err != nil ||
		len(request.Scopes) > maximumWhiteboardValidationScopes {
		writeWhiteboardInternalProblem(w, r, http.StatusBadRequest, "invalid_request")
		return
	}
	valid := make([]string, 0, len(request.Scopes))
	for _, candidate := range request.Scopes {
		input, ok := candidate.input()
		if !ok {
			writeWhiteboardInternalProblem(w, r, http.StatusBadRequest, "invalid_request")
			return
		}
		accepted, err := handlers.service.ValidateGrant(r.Context(), input)
		if err != nil && !errors.Is(err, collaboration.ErrGrantDenied) {
			handlers.writeGrantProblem(w, r, err)
			return
		}
		if accepted {
			valid = append(valid, input.Scope.AuthorityLease)
		}
	}
	writeJSON(handlers.logger, w, http.StatusOK, whiteboardInternalValidationResponse{
		ValidAuthorityLeases: valid,
	})
}

func (request whiteboardInternalScopeRequest) input() (collaboration.GrantValidationInput, bool) {
	if request.AuthorityLease == nil || request.ActorID == nil || request.Capability == nil ||
		request.DocumentID == nil || request.Generation == nil || request.Origin == nil ||
		request.ProviderDocumentName == nil || request.SessionID == nil || request.TenantID == nil ||
		request.WriterFence == nil {
		return collaboration.GrantValidationInput{}, false
	}
	return collaboration.GrantValidationInput{
		Origin: *request.Origin,
		Scope: collaboration.GrantScope{
			AuthorityLease: *request.AuthorityLease, ActorID: *request.ActorID,
			Capability: *request.Capability, DocumentID: *request.DocumentID,
			Generation: *request.Generation, ProviderDocumentName: *request.ProviderDocumentName,
			MaxConnectionsPerTenant:  optionalInt64(request.MaxConnectionsPerTenant),
			MaxOperationsPerMinute:   optionalInt64(request.MaxOperationsPerMinute),
			MaxStorageBytesPerTenant: optionalInt64(request.MaxStorageBytesPerTenant),
			SessionID:                *request.SessionID, TenantID: *request.TenantID,
			WriterFence: *request.WriterFence,
		},
	}, true
}

func optionalInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func (handlers whiteboardInternalHandlers) authorized(w http.ResponseWriter, r *http.Request) bool {
	whiteboardInternalResponseHeaders(w)
	header := r.Header.Get("Authorization")
	received, ok := strings.CutPrefix(header, "Bearer ")
	receivedDigest := sha256.Sum256([]byte(received))
	if !ok || subtle.ConstantTimeCompare(receivedDigest[:], handlers.serviceToken[:]) != 1 {
		writeWhiteboardInternalProblem(w, r, http.StatusUnauthorized, "unauthorized")
		return false
	}
	return true
}

func (handlers whiteboardInternalHandlers) writeGrantProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	switch {
	case errors.Is(err, collaboration.ErrGrantDenied), errors.Is(err, collaboration.ErrNotFound),
		errors.Is(err, collaboration.ErrVersionConflict):
		writeWhiteboardInternalProblem(w, r, http.StatusConflict, "grant_denied")
	case errors.Is(err, collaboration.ErrGrantRateLimited):
		writeWhiteboardInternalProblem(w, r, http.StatusTooManyRequests, "grant_rate_limited")
	default:
		writeWhiteboardInternalProblem(w, r, http.StatusServiceUnavailable, "control_plane_unavailable")
	}
}

func whiteboardInternalResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeWhiteboardInternalProblem(w http.ResponseWriter, r *http.Request, status int, code string) {
	whiteboardInternalResponseHeaders(w)
	writeCodedProblem(w, r, status, code, "Collaboration request denied", "The collaboration runtime request could not be authorized.")
}
