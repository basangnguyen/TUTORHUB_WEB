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
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
)

func TestMediaParticipantSnapshotUsesExactAuthenticatedProjectionAndPrivacyHeaders(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	spaceID, roomID, participantKey := uuid.New(), uuid.New(), uuid.New()
	service := &fakeMediaSignalService{snapshot: media.MediaParticipantSnapshot{
		RoomInstanceID: roomID, ProjectionVersion: 7, LastSignalSequence: 11,
		SelfParticipantKey: participantKey,
		ViewerOperations:   media.MediaSignalViewerOperations{CanRaiseHand: true, CanSendReaction: true},
		Participants: []media.MediaParticipant{{
			ParticipantKey: participantKey, RosterSequence: 1, DisplayName: "Teacher",
			InstanceRole: media.InstanceRoleHost, Connection: media.ParticipantConnectionConnected,
		}},
		RaisedHands: []media.RaisedHand{}, ReactionClusters: []media.ReactionCluster{}, ServerTime: fixedTime,
	}}
	identityService := classIdentityService(tenantID, actorID, nil)
	handler := newMediaSignalTestHandler(identityService, service)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/media/spaces/"+spaceID.String()+"/participants?room_instance_id="+
			roomID.String()+"&expected_space_version=5&expected_room_instance_version=3",
		nil,
	)
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addSessionCookie(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK || service.getCalls != 1 || service.spaceID != spaceID ||
		service.snapshotInput.ExpectedSpaceVersion != 5 ||
		service.snapshotInput.ExpectedRoomInstanceID != roomID ||
		service.snapshotInput.ExpectedRoomInstanceVersion != 3 {
		t.Fatalf("unexpected participant snapshot request: status=%d service=%+v body=%s", response.Code, service, response.Body.String())
	}
	assertMediaAccess(t, service.access, identityService.principal)
	if response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing participant projection privacy headers: %v", response.Header())
	}
	for _, forbidden := range []string{"email", "participant_session", "join_attempt", "provider", "access_token"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("participant snapshot leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestMediaSignalMutationRequiresCSRFAndPassesExactTypedCommand(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	spaceID, roomID, participantKey := uuid.New(), uuid.New(), uuid.New()
	service := &fakeMediaSignalService{snapshot: media.MediaParticipantSnapshot{
		RoomInstanceID: roomID, ProjectionVersion: 8, LastSignalSequence: 12,
		SelfParticipantKey: participantKey,
		Participants: []media.MediaParticipant{{
			ParticipantKey: participantKey, RosterSequence: 1, DisplayName: "Teacher",
			InstanceRole: media.InstanceRoleHost, Connection: media.ParticipantConnectionConnected,
		}},
		RaisedHands: []media.RaisedHand{}, ReactionClusters: []media.ReactionCluster{}, ServerTime: fixedTime,
	}}
	handler := newMediaSignalTestHandler(classIdentityService(tenantID, actorID, nil), service)
	body := `{"expected_room_instance_id":"` + roomID.String() +
		`","expected_space_version":5,"expected_room_instance_version":3,` +
		`"expected_projection_version":7,"idempotency_key":"p406-http-reaction-0001",` +
		`"kind":"reaction","reaction":"clap"}`
	path := "/api/v1/media/spaces/" + spaceID.String() + "/signals"

	missingCSRF := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden || service.sendCalls != 0 {
		t.Fatalf("missing CSRF reached signal service: status=%d calls=%d", missingResponse.Code, service.sendCalls)
	}

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.sendCalls != 1 || service.spaceID != spaceID ||
		service.signalInput.ExpectedRoomInstanceID != roomID ||
		service.signalInput.ExpectedSpaceVersion != 5 ||
		service.signalInput.ExpectedRoomInstanceVersion != 3 ||
		service.signalInput.ExpectedProjectionVersion != 7 ||
		service.signalInput.IdempotencyKey != "p406-http-reaction-0001" ||
		service.signalInput.Kind != media.MediaSignalReaction ||
		service.signalInput.Reaction != media.MediaReactionClap {
		t.Fatalf("unexpected signal mutation: status=%d service=%+v body=%s", response.Code, service, response.Body.String())
	}
}

func TestMediaSignalRejectsUnknownPayloadAndRedactsRateLimit(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	spaceID, roomID := uuid.New(), uuid.New()
	service := &fakeMediaSignalService{}
	handler := newMediaSignalTestHandler(classIdentityService(tenantID, actorID, nil), service)
	path := "/api/v1/media/spaces/" + spaceID.String() + "/signals"
	unknown := httptest.NewRequest(
		http.MethodPost, path,
		strings.NewReader(`{"expected_room_instance_id":"`+roomID.String()+`","expected_space_version":1,"expected_room_instance_version":1,"expected_projection_version":1,"idempotency_key":"p406-http-unknown-0001","kind":"reaction","reaction":"clap","provider_identity":"private"}`),
	)
	unknown.Header.Set("Content-Type", "application/json")
	unknown.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(unknown)
	unknownResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownResponse, unknown)
	if unknownResponse.Code != http.StatusBadRequest || service.sendCalls != 0 ||
		!strings.Contains(unknownResponse.Body.String(), `"code":"participant_signal_invalid"`) ||
		strings.Contains(unknownResponse.Body.String(), "private") {
		t.Fatalf("unknown signal payload was not rejected safely: status=%d calls=%d body=%s", unknownResponse.Code, service.sendCalls, unknownResponse.Body.String())
	}

	service.requestError = &media.MediaSignalRateLimitError{RetryAfter: 2500 * time.Millisecond}
	rateRequest := httptest.NewRequest(
		http.MethodPost, path,
		strings.NewReader(`{"expected_room_instance_id":"`+roomID.String()+`","expected_space_version":1,"expected_room_instance_version":1,"expected_projection_version":1,"idempotency_key":"p406-http-rate-000001","kind":"hand_raise"}`),
	)
	rateRequest.Header.Set("Content-Type", "application/json")
	rateRequest.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(rateRequest)
	rateResponse := httptest.NewRecorder()
	handler.ServeHTTP(rateResponse, rateRequest)
	if rateResponse.Code != http.StatusTooManyRequests || rateResponse.Header().Get("Retry-After") != "3" ||
		!strings.Contains(rateResponse.Body.String(), `"code":"media_signal_rate_limited"`) ||
		strings.Contains(strings.ToLower(rateResponse.Body.String()), "rate_limit_windows") {
		t.Fatalf("signal rate response is not bounded/redacted: status=%d headers=%v body=%s", rateResponse.Code, rateResponse.Header(), rateResponse.Body.String())
	}
	var problem Problem
	if err := json.Unmarshal(rateResponse.Body.Bytes(), &problem); err != nil || problem.Status != http.StatusTooManyRequests {
		t.Fatalf("invalid typed rate problem: err=%v problem=%+v", err, problem)
	}
}

func newMediaSignalTestHandler(identityService identity.ServiceAPI, service media.MediaSignalServiceAPI) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{Clock: fixedClock, Identity: identityService, MediaSignals: service},
	)
}

type fakeMediaSignalService struct {
	access        media.AccessContext
	spaceID       uuid.UUID
	snapshotInput media.GetMediaParticipantSnapshotInput
	signalInput   media.SendMediaSignalInput
	snapshot      media.MediaParticipantSnapshot
	requestError  error
	getCalls      int
	sendCalls     int
}

func (service *fakeMediaSignalService) GetParticipantSnapshot(
	_ context.Context, access media.AccessContext, spaceID uuid.UUID,
	input media.GetMediaParticipantSnapshotInput,
) (media.MediaParticipantSnapshot, error) {
	service.getCalls++
	service.access, service.spaceID, service.snapshotInput = access, spaceID, input
	return service.snapshot, service.requestError
}

func (service *fakeMediaSignalService) SendSignal(
	_ context.Context, access media.AccessContext, spaceID uuid.UUID,
	input media.SendMediaSignalInput,
) (media.MediaParticipantSnapshot, error) {
	service.sendCalls++
	service.access, service.spaceID, service.signalInput = access, spaceID, input
	return service.snapshot, service.requestError
}
