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
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
)

func TestMediaModerationLockRequiresCSRFAndPassesExactCommand(t *testing.T) {
	t.Parallel()

	tenantID, actorID, spaceID, roomID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &fakeMediaModerationService{result: media.ModerationResult{
		SpaceID: spaceID, RoomInstanceID: roomID, SpaceVersion: 6,
		RoomInstanceVersion: 4, ProjectionVersion: 8,
		ProviderEffectStatus: media.ProviderEffectNone,
	}}
	handler := newMediaModerationTestHandler(classIdentityService(tenantID, actorID, nil), service)
	body := `{"expected_room_instance_id":"` + roomID.String() +
		`","expected_space_version":5,"expected_room_instance_version":3,` +
		`"expected_projection_version":7,"idempotency_key":"p407-http-lock-000001",` +
		`"locked":true,"reason_code":"host_request"}`
	path := "/api/v1/media/spaces/" + spaceID.String() + "/lock"

	missingCSRF := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden || service.lockCalls != 0 {
		t.Fatalf("missing CSRF reached moderation service: status=%d calls=%d", missingResponse.Code, service.lockCalls)
	}

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.lockCalls != 1 || service.spaceID != spaceID ||
		service.lockInput.Expected.RoomInstanceID != roomID ||
		service.lockInput.Expected.SpaceVersion != 5 || service.lockInput.Expected.RoomVersion != 3 ||
		service.lockInput.Expected.ProjectionVersion != 7 ||
		service.lockInput.IdempotencyKey != "p407-http-lock-000001" || !service.lockInput.Locked ||
		service.lockInput.ReasonCode != "host_request" {
		t.Fatalf("unexpected moderation lock: status=%d service=%+v body=%s", response.Code, service, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("missing moderation privacy headers: %v", response.Header())
	}
}

func TestMediaModerationParticipantRoleUsesOpaqueTargetAndRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	tenantID, actorID, spaceID, roomID, participantKey :=
		uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &fakeMediaModerationService{}
	handler := newMediaModerationTestHandler(classIdentityService(tenantID, actorID, nil), service)
	path := "/api/v1/media/spaces/" + spaceID.String() + "/participants/" + participantKey.String() + "/role"
	base := `{"expected_room_instance_id":"` + roomID.String() +
		`","expected_space_version":5,"expected_room_instance_version":3,` +
		`"expected_projection_version":7,"idempotency_key":"p407-http-role-000001",` +
		`"desired_role":"co_host"`
	unknown := httptest.NewRequest(
		http.MethodPost, path, strings.NewReader(base+`,"provider_identity":"private"}`),
	)
	unknown.Header.Set("Content-Type", "application/json")
	unknown.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(unknown)
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusBadRequest || service.roleCalls != 0 ||
		!strings.Contains(unknownResponse.Body.String(), `"code":"media_moderation_invalid"`) ||
		strings.Contains(unknownResponse.Body.String(), "private") {
		t.Fatalf("unknown moderation field was not rejected safely: status=%d calls=%d body=%s", unknownResponse.Code, service.roleCalls, unknownResponse.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(base+`}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.roleCalls != 1 ||
		service.participantKey != participantKey || service.roleInput.DesiredRole != media.InstanceRoleCoHost {
		t.Fatalf("unexpected participant-role moderation: status=%d service=%+v body=%s", response.Code, service, response.Body.String())
	}
}

func TestMediaModerationRateLimitIsTypedRedactedAndRetryable(t *testing.T) {
	t.Parallel()

	tenantID, actorID, spaceID, roomID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &fakeMediaModerationService{
		requestError: &media.ModerationRateLimitError{RetryAfter: 2500 * time.Millisecond},
	}
	handler := newMediaModerationTestHandler(classIdentityService(tenantID, actorID, nil), service)
	body := `{"expected_room_instance_id":"` + roomID.String() +
		`","expected_space_version":5,"expected_room_instance_version":3,` +
		`"expected_projection_version":7,"idempotency_key":"p407-http-rate-000001",` +
		`"locked":true}`
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/media/spaces/"+spaceID.String()+"/lock", strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "3" ||
		!strings.Contains(response.Body.String(), `"code":"media_moderation_rate_limited"`) {
		t.Fatalf("unexpected moderation rate response: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	for _, private := range []string{tenantID.String(), actorID.String(), roomID.String(), "rate_limit_windows"} {
		if strings.Contains(response.Body.String(), private) {
			t.Fatalf("moderation rate response leaked private value %q: %s", private, response.Body.String())
		}
	}
}

func newMediaModerationTestHandler(
	identityService identity.ServiceAPI,
	service media.ModerationServiceAPI,
) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{Clock: fixedClock, Identity: identityService, MediaModeration: service},
	)
}

type fakeMediaModerationService struct {
	spaceID        uuid.UUID
	participantKey uuid.UUID
	lockInput      media.LockMediaSpaceInput
	roleInput      media.ChangeParticipantRoleInput
	result         media.ModerationResult
	requestError   error
	lockCalls      int
	roleCalls      int
}

func (service *fakeMediaModerationService) SetLocked(
	_ context.Context, _ media.AccessContext, spaceID uuid.UUID, input media.LockMediaSpaceInput,
) (media.ModerationResult, error) {
	service.lockCalls++
	service.spaceID, service.lockInput = spaceID, input
	return service.result, service.requestError
}

func (service *fakeMediaModerationService) ChangeParticipantRole(
	_ context.Context, _ media.AccessContext, spaceID uuid.UUID, participantKey uuid.UUID,
	input media.ChangeParticipantRoleInput,
) (media.ModerationResult, error) {
	service.roleCalls++
	service.spaceID, service.participantKey, service.roleInput = spaceID, participantKey, input
	return service.result, service.requestError
}

func (service *fakeMediaModerationService) MuteParticipant(
	context.Context, media.AccessContext, uuid.UUID, uuid.UUID, media.ModerateParticipantInput,
) (media.ModerationResult, error) {
	return service.result, service.requestError
}

func (service *fakeMediaModerationService) RemoveParticipant(
	context.Context, media.AccessContext, uuid.UUID, uuid.UUID, media.ModerateParticipantInput,
) (media.ModerationResult, error) {
	return service.result, service.requestError
}
