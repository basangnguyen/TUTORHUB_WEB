package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
)

func TestMediaDiagnosticEndpointRejectsUnknownSecretFieldsAndBindsActor(t *testing.T) {
	t.Parallel()
	tenantID, actorID, spaceID := uuid.New(), uuid.New(), uuid.New()
	service := &fakeMediaDiagnosticService{}
	handler := newMediaDiagnosticTestHandler(classIdentityService(tenantID, actorID, nil), service, &fakeAuditService{})
	path := "/api/v1/media/spaces/" + spaceID.String() + "/diagnostics"
	valid := `{"event_id":"` + uuid.NewString() + `","room_instance_id":"` + uuid.NewString() +
		`","join_attempt_id":"` + uuid.NewString() + `","stage":"media","outcome":"succeeded",` +
		`"network_quality":"good","media_path":"audio_video","duration_ms":1234}`

	unknown := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.TrimSuffix(valid, "}")+`,"token":"secret"}`))
	unknown.Header.Set("Content-Type", "application/json")
	unknown.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(unknown)
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusBadRequest || service.recordCalls != 0 {
		t.Fatalf("unknown secret field was accepted: status=%d calls=%d", unknownResponse.Code, service.recordCalls)
	}

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(valid))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || service.recordCalls != 1 || service.spaceID != spaceID ||
		service.access.TenantID != tenantID || service.access.ActorID != actorID {
		t.Fatalf("diagnostic actor binding failed: status=%d service=%+v", response.Code, service)
	}
}

func TestMediaDiagnosticExportIsNoStoreAuditedAndFailsClosed(t *testing.T) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	service := &fakeMediaDiagnosticService{export: media.DiagnosticExport{
		From: fixedTime.Add(-time.Hour), To: fixedTime,
		Items: []media.DiagnosticExportItem{{
			EventID: uuid.New(), SessionRef: "ps_0123456789abcdef01234567",
			Stage: media.DiagnosticStageMedia, Outcome: media.DiagnosticOutcomeSucceeded,
			NetworkQuality: media.DiagnosticNetworkGood, MediaPath: media.DiagnosticMediaAudioVideo,
			DurationMS: 900, RecordedAt: fixedTime,
		}},
	}}
	auditService := &fakeAuditService{}
	handler := newMediaDiagnosticTestHandler(classIdentityService(tenantID, actorID, nil), service, auditService)
	body := `{"from":"` + fixedTime.Add(-time.Hour).Format(time.RFC3339) + `","to":"` +
		fixedTime.Format(time.RFC3339) + `","limit":100}`
	request := httptest.NewRequest(http.MethodPost, mediaDiagnosticsExportPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "private, no-store" ||
		auditService.recordCalls != 1 || auditService.recordedDrafts[0].Action != audit.ActionMediaDiagnosticsExport {
		t.Fatalf("export safety boundary failed: status=%d headers=%v audit=%+v", response.Code, response.Header(), auditService)
	}
	if strings.Contains(response.Body.String(), actorID.String()) || strings.Contains(response.Body.String(), tenantID.String()) {
		t.Fatalf("export leaked tenant or actor identity: %s", response.Body.String())
	}

	auditService.recordError = context.DeadlineExceeded
	failing := httptest.NewRequest(http.MethodPost, mediaDiagnosticsExportPath, strings.NewReader(body))
	failing.Header.Set("Content-Type", "application/json")
	failing.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(failing)
	failingResponse := httptest.NewRecorder()
	handler.ServeHTTP(failingResponse, failing)
	if failingResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("audit failure did not fail closed: %d", failingResponse.Code)
	}
}

func newMediaDiagnosticTestHandler(
	identityService identity.ServiceAPI,
	diagnostics media.DiagnosticServiceAPI,
	auditService audit.ServiceAPI,
) http.Handler {
	return NewHandlerWithOptions(config.Config{
		Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
		Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		Clock: fixedClock, Identity: identityService, MediaDiagnostics: diagnostics, Audit: auditService,
	})
}

type fakeMediaDiagnosticService struct {
	access      media.AccessContext
	spaceID     uuid.UUID
	recordCalls int
	exportCalls int
	export      media.DiagnosticExport
	err         error
}

func (service *fakeMediaDiagnosticService) RecordDiagnostic(
	_ context.Context, access media.AccessContext, spaceID uuid.UUID, _ media.RecordDiagnosticInput,
) error {
	service.recordCalls++
	service.access, service.spaceID = access, spaceID
	return service.err
}

func (service *fakeMediaDiagnosticService) ExportDiagnostics(
	_ context.Context, access media.AccessContext, _ media.DiagnosticExportFilter,
) (media.DiagnosticExport, error) {
	service.exportCalls++
	service.access = access
	return service.export, service.err
}
