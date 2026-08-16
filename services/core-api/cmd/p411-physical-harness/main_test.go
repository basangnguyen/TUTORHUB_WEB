package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/livekit/protocol/livekit"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
	"net/http"
	"net/http/httptest"
)

type fakeRoomService struct {
	roomExists   bool
	participants int
	deleted      bool
}

func (service *fakeRoomService) ListRooms(
	_ context.Context,
	_ *livekit.ListRoomsRequest,
) (*livekit.ListRoomsResponse, error) {
	response := &livekit.ListRoomsResponse{}
	if service.roomExists {
		response.Rooms = []*livekit.Room{{Name: "r_00000000000000000000000000000000"}}
	}
	return response, nil
}

func (service *fakeRoomService) ListParticipants(
	_ context.Context,
	_ *livekit.ListParticipantsRequest,
) (*livekit.ListParticipantsResponse, error) {
	participants := make([]*livekit.ParticipantInfo, service.participants)
	for index := range participants {
		participants[index] = &livekit.ParticipantInfo{Identity: "participant"}
	}
	return &livekit.ListParticipantsResponse{Participants: participants}, nil
}

func (service *fakeRoomService) DeleteRoom(
	_ context.Context,
	_ *livekit.DeleteRoomRequest,
) (*livekit.DeleteRoomResponse, error) {
	service.deleted = true
	service.roomExists = false
	return &livekit.DeleteRoomResponse{}, nil
}

type fakeTokenIssuer struct {
	grant media.TokenGrant
}

func (issuer *fakeTokenIssuer) Issue(grant media.TokenGrant) (string, error) {
	issuer.grant = grant
	return "sensitive-test-token", nil
}

func TestLoadConfigurationAcceptsOnlyIsolatedLiveKitValues(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, ".env.p4-11-livekit.local")
	contents := strings.Join([]string{
		"LIVEKIT_URL=wss://p411.example.test",
		"LIVEKIT_API_KEY=test-key",
		"LIVEKIT_API_SECRET=test-secret",
		"P4_11_PROVIDER_CONFIRM=" + confirmation,
		"P4_11_LOAD_PROFILE=50",
	}, "\n")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfiguration(path)
	if err != nil {
		t.Fatalf("load configuration: %v", err)
	}
	if cfg.serverURL != "wss://p411.example.test" || cfg.apiKey == "" || cfg.apiSecret == "" {
		t.Fatal("expected credential fields to load without transformation")
	}

	contents += "\nDATABASE_POOL_URL=postgresql://not-allowed"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfiguration(path); err == nil {
		t.Fatal("expected database credential rejection")
	}
}

func TestCredentialBoundaryLocksOriginMethodRoleAndCache(t *testing.T) {
	rooms := &fakeRoomService{}
	issuer := &fakeTokenIssuer{}
	harness := testHarness(rooms, issuer)

	forbidden := httptest.NewRequest(http.MethodPost, "/v1/credential", strings.NewReader(`{"role":"teacher"}`))
	forbiddenRecorder := httptest.NewRecorder()
	harness.routes().ServeHTTP(forbiddenRecorder, forbidden)
	if forbiddenRecorder.Code != http.StatusForbidden || strings.Contains(forbiddenRecorder.Body.String(), "sensitive-test-token") {
		t.Fatalf("unexpected forbidden response: %d", forbiddenRecorder.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/credential", strings.NewReader(`{"role":"teacher"}`))
	request.Header.Set("Origin", allowedOrigin)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	harness.routes().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Access-Control-Allow-Origin") != allowedOrigin {
		t.Fatal("missing exact no-store/CORS boundary")
	}
	var payload credentialResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Credential.AccessToken != "sensitive-test-token" {
		t.Fatal("expected issued token in credential response")
	}
	if !issuer.grant.CanPublishCameraMicrophone || !issuer.grant.CanShareScreen || issuer.grant.CanPublishData || !issuer.grant.CanSubscribe {
		t.Fatalf("unexpected host grant: %+v", issuer.grant)
	}
	if issuer.grant.RoomName != harness.roomName || issuer.grant.ParticipantKey != teacherParticipantKey {
		t.Fatal("token grant is not bound to the opaque room and participant key")
	}

	invalid := httptest.NewRequest(http.MethodPost, "/v1/credential", strings.NewReader(`{"role":"admin"}`))
	invalid.Header.Set("Origin", allowedOrigin)
	invalidRecorder := httptest.NewRecorder()
	harness.routes().ServeHTTP(invalidRecorder, invalid)
	if invalidRecorder.Code != http.StatusBadRequest || strings.Contains(invalidRecorder.Body.String(), "sensitive-test-token") {
		t.Fatalf("unexpected invalid-role response: %d", invalidRecorder.Code)
	}
}

func TestCleanupRefusesActiveParticipantsThenVerifiesZero(t *testing.T) {
	rooms := &fakeRoomService{roomExists: true, participants: 1}
	harness := testHarness(rooms, &fakeTokenIssuer{})

	active := httptest.NewRequest(http.MethodPost, "/v1/cleanup", nil)
	active.Header.Set("Origin", allowedOrigin)
	activeRecorder := httptest.NewRecorder()
	harness.routes().ServeHTTP(activeRecorder, active)
	if activeRecorder.Code != http.StatusConflict || rooms.deleted {
		t.Fatal("cleanup must fail closed while a participant is active")
	}

	rooms.participants = 0
	cleanup := httptest.NewRequest(http.MethodPost, "/v1/cleanup", nil)
	cleanup.Header.Set("Origin", allowedOrigin)
	cleanupRecorder := httptest.NewRecorder()
	harness.routes().ServeHTTP(cleanupRecorder, cleanup)
	if cleanupRecorder.Code != http.StatusOK || !rooms.deleted {
		t.Fatalf("expected verified cleanup, got %d", cleanupRecorder.Code)
	}
	var status statusResponse
	if err := json.Unmarshal(cleanupRecorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.RoomExists || status.ParticipantCount != 0 || !status.CleanupZero {
		t.Fatalf("unexpected cleanup status: %+v", status)
	}
}

func TestOutageTerminateUsesRecoveryCredentialWithActiveParticipants(t *testing.T) {
	primaryRooms := &fakeRoomService{roomExists: true, participants: 2}
	recoveryRooms := &fakeRoomService{roomExists: true, participants: 2}
	harness := testHarness(primaryRooms, &fakeTokenIssuer{})
	harness.recoveryRooms = recoveryRooms

	request := httptest.NewRequest(http.MethodPost, "/v1/outage/terminate", nil)
	request.Header.Set("Origin", allowedOrigin)
	recorder := httptest.NewRecorder()
	harness.routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK || !recoveryRooms.deleted || primaryRooms.deleted {
		t.Fatalf(
			"expected recovery-only termination, got code=%d recovery_deleted=%t primary_deleted=%t",
			recorder.Code,
			recoveryRooms.deleted,
			primaryRooms.deleted,
		)
	}
	var status statusResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.RoomExists || status.ParticipantCount != 0 || !status.CleanupZero {
		t.Fatalf("unexpected outage termination status: %+v", status)
	}
}

func testHarness(rooms roomService, issuer tokenIssuer) *physicalHarness {
	return &physicalHarness{
		serverURL:      "wss://p411.example.test",
		roomName:       "r_00000000000000000000000000000000",
		roomInstanceID: teacherParticipantKey,
		rooms:          rooms,
		recoveryRooms:  rooms,
		tokens:         issuer,
	}
}
