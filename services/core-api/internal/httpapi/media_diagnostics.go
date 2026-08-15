package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
)

const (
	mediaDiagnosticsPathPattern = "/api/v1/media/spaces/{space_id}/diagnostics"
	mediaDiagnosticsExportPath  = "/api/v1/media/diagnostics/export"
	maximumMediaDiagnosticBytes = 4 * 1024
	maximumDiagnosticExportBody = 2 * 1024
)

type mediaDiagnosticHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service media.DiagnosticServiceAPI
	audit   audit.ServiceAPI
}

type mediaDiagnosticRequest struct {
	EventID        uuid.UUID `json:"event_id"`
	RoomInstanceID uuid.UUID `json:"room_instance_id"`
	JoinAttemptID  uuid.UUID `json:"join_attempt_id"`
	Stage          string    `json:"stage"`
	Outcome        string    `json:"outcome"`
	ErrorCode      string    `json:"error_code,omitempty"`
	NetworkQuality string    `json:"network_quality"`
	MediaPath      string    `json:"media_path"`
	DurationMS     int       `json:"duration_ms"`
}

type mediaDiagnosticExportRequest struct {
	From  time.Time `json:"from"`
	To    time.Time `json:"to"`
	Limit int       `json:"limit"`
}

func newMediaDiagnosticHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service media.DiagnosticServiceAPI,
	auditService audit.ServiceAPI,
) mediaDiagnosticHandlers {
	return mediaDiagnosticHandlers{logger: logger, auth: auth, service: service, audit: auditService}
}

func (handlers mediaDiagnosticHandlers) record(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r)
	if !ok {
		return
	}
	spaceID, ok := parseResourceUUID(r.PathValue("space_id"))
	if !ok {
		handlers.writeProblem(w, r, media.ErrInvalidDiagnosticRequest)
		return
	}
	var request mediaDiagnosticRequest
	if err := decodeJSONRequest(w, r, &request, maximumMediaDiagnosticBytes); err != nil {
		handlers.writeProblem(w, r, media.ErrInvalidDiagnosticRequest)
		return
	}
	err := handlers.service.RecordDiagnostic(r.Context(), mediaAccess(principal), spaceID, media.RecordDiagnosticInput{
		EventID: request.EventID, RoomInstanceID: request.RoomInstanceID,
		JoinAttemptID: request.JoinAttemptID, Stage: media.DiagnosticStage(request.Stage),
		Outcome: media.DiagnosticOutcome(request.Outcome), ErrorCode: media.DiagnosticErrorCode(request.ErrorCode),
		NetworkQuality: media.DiagnosticNetworkQuality(request.NetworkQuality),
		MediaPath:      media.DiagnosticMediaPath(request.MediaPath), DurationMS: request.DurationMS,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handlers mediaDiagnosticHandlers) export(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.principal(w, r)
	if !ok {
		return
	}
	var request mediaDiagnosticExportRequest
	if err := decodeJSONRequest(w, r, &request, maximumDiagnosticExportBody); err != nil {
		handlers.writeProblem(w, r, media.ErrInvalidDiagnosticRequest)
		return
	}
	export, err := handlers.service.ExportDiagnostics(r.Context(), mediaAccess(principal), media.DiagnosticExportFilter{
		From: request.From, To: request.To, Limit: request.Limit,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	if handlers.audit == nil {
		handlers.writeProblem(w, r, media.ErrDiagnosticUnavailable)
		return
	}
	rangeHours := int(request.To.Sub(request.From).Hours())
	if err := handlers.audit.Record(context.WithoutCancel(r.Context()), audit.Draft{
		Action: audit.ActionMediaDiagnosticsExport, ResourceType: "media_diagnostics",
		Outcome: audit.OutcomeSucceeded,
		Metadata: audit.Metadata{
			"range_hours": strconv.Itoa(rangeHours),
			"row_count":   strconv.Itoa(len(export.Items)),
			"truncated":   strconv.FormatBool(export.Truncated),
		},
	}); err != nil {
		handlers.logger.Error(
			"media diagnostics audit failed",
			"request_id", RequestIDFromContext(r.Context()),
			"error_code", "audit_persistence_failed",
		)
		handlers.writeProblem(w, r, media.ErrDiagnosticUnavailable)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="tutorhub-media-diagnostics.json"`)
	writeJSON(handlers.logger, w, http.StatusOK, export)
}

func (handlers mediaDiagnosticHandlers) principal(w http.ResponseWriter, r *http.Request) (identity.Principal, bool) {
	if handlers.service == nil || !handlers.auth.available(w, r) {
		handlers.writeProblem(w, r, media.ErrDiagnosticUnavailable)
		return identity.Principal{}, false
	}
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	principal, ok := handlers.auth.csrfPrincipal(w, r, sessionToken)
	if !ok {
		return identity.Principal{}, false
	}
	if principal.ActiveTenant == nil || !principal.ActiveTenant.IsActive {
		handlers.writeProblem(w, r, media.ErrDiagnosticForbidden)
		return identity.Principal{}, false
	}
	expectedTenantID, valid := parseResourceUUID(strings.TrimSpace(r.Header.Get(mediaSpaceTenantHeader)))
	if !valid {
		handlers.writeProblem(w, r, media.ErrInvalidDiagnosticRequest)
		return identity.Principal{}, false
	}
	if expectedTenantID != principal.ActiveTenant.ID {
		handlers.writeProblem(w, r, errMediaSpaceScopeChanged)
		return identity.Principal{}, false
	}
	return principal, true
}

func (handlers mediaDiagnosticHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	status, code := http.StatusServiceUnavailable, "media_diagnostics_unavailable"
	title, detail := "Media diagnostics unavailable", "The diagnostics request could not be completed safely."
	switch {
	case errors.Is(err, media.ErrInvalidDiagnosticRequest):
		status, code = http.StatusBadRequest, "media_diagnostics_invalid"
		title, detail = "Invalid media diagnostics request", "Use the bounded diagnostics schema and range."
	case errors.Is(err, media.ErrDiagnosticForbidden), errors.Is(err, media.ErrSpaceAccessDenied):
		status, code = http.StatusForbidden, "media_diagnostics_forbidden"
		title, detail = "Media diagnostics access denied", "The active workspace cannot authorize this request."
	case errors.Is(err, media.ErrSpaceNotFound):
		status, code = http.StatusNotFound, "media_space_not_found"
		title, detail = "Media space unavailable", "The media space is unavailable in the active workspace."
	case errors.Is(err, errMediaSpaceScopeChanged):
		status, code = http.StatusConflict, "media_space_scope_changed"
		title, detail = "Active workspace changed", "Reload the current workspace before retrying."
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
