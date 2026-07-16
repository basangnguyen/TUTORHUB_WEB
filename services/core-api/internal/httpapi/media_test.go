package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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

func TestMediaTokenRequiresCSRFAndUsesAuthenticatedPrincipal(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	classID := uuid.New()
	identityService := classIdentityService(
		tenantID,
		userID,
		[]string{"class.view", "session.join", "media.publish"},
	)
	credential := media.JoinCredential{
		AccessToken:         "signed-livekit-token",
		ServerURL:           "wss://staging.example.test",
		RoomName:            media.RoomName(tenantID, classID),
		ParticipantIdentity: media.ParticipantIdentity(userID, identityService.principal.SessionID),
		ParticipantName:     identityService.principal.User.DisplayName,
		AttemptID:           uuid.New(),
		CanPublish:          true,
		ExpiresAt:           fixedTime.Add(5 * time.Minute),
	}
	mediaService := &fakeMediaService{credential: credential}
	handler := newMediaTestHandler(identityService, mediaService, nil)
	path := "/api/v1/classes/" + classID.String() + "/media-token"

	missingCSRF := httptest.NewRequest(http.MethodPost, path, nil)
	addSessionCookie(missingCSRF)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || mediaService.issueCalled {
		t.Fatalf(
			"missing CSRF must be denied: status=%d called=%t",
			missingCSRFResponse.Code,
			mediaService.issueCalled,
		)
	}

	request := httptest.NewRequest(http.MethodPost, path, nil)
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("issue media token: status=%d body=%s", response.Code, response.Body.String())
	}
	var body mediaTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode media token: %v", err)
	}
	if body.AccessToken != credential.AccessToken || body.AttemptID != credential.AttemptID ||
		body.ServerURL != credential.ServerURL || !body.CanPublish {
		t.Fatalf("unexpected media token response: %+v", body)
	}
	if !mediaService.issueCalled || mediaService.classID != classID {
		t.Fatalf("media service was not called correctly: %+v", mediaService)
	}
	assertMediaAccess(t, mediaService.access, identityService.principal)
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("media token must not be cached: %q", response.Header().Get("Cache-Control"))
	}
}

func TestMediaEventValidatesJSONAndRecordsBoundedTelemetry(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	classID := uuid.New()
	attemptID := uuid.New()
	identityService := classIdentityService(
		tenantID,
		userID,
		[]string{"class.view", "session.join"},
	)
	mediaService := &fakeMediaService{}
	handler := newMediaTestHandler(identityService, mediaService, nil)
	path := "/api/v1/classes/" + classID.String() + "/media-events"
	body := `{"attempt_id":"` + attemptID.String() +
		`","stage":"connect","outcome":"succeeded","error_code":"","duration_ms":842}`

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("record media event: status=%d body=%s", response.Code, response.Body.String())
	}
	if !mediaService.eventCalled || mediaService.classID != classID ||
		mediaService.eventInput.AttemptID != attemptID ||
		mediaService.eventInput.Stage != "connect" || mediaService.eventInput.DurationMS != 842 {
		t.Fatalf("unexpected media event call: %+v", mediaService)
	}

	invalidRequest := httptest.NewRequest(
		http.MethodPost,
		path,
		strings.NewReader(`{"attempt_id":"`+attemptID.String()+`","stage":"connect","outcome":"succeeded","secret":"not-allowed"}`),
	)
	invalidRequest.Header.Set("Content-Type", "application/json")
	addAuthenticatedMutationCookies(invalidRequest)
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("unknown telemetry fields must be rejected: status=%d", invalidResponse.Code)
	}
}

func TestLiveKitWebhookRequiresSignedWebhookContentType(t *testing.T) {
	t.Parallel()

	event := media.WebhookEvent{
		ID: "EV_room_started_01", EventType: "room_started",
		RoomName: media.RoomName(uuid.New(), uuid.New()), OccurredAt: fixedTime,
	}
	mediaService := &fakeMediaService{webhookResult: media.WebhookResult{Duplicate: true}}
	verifier := &fakeWebhookVerifier{event: event}
	handler := newMediaTestHandler(nil, mediaService, verifier)

	invalidRequest := httptest.NewRequest(
		http.MethodPost,
		liveKitWebhookPath,
		strings.NewReader(`{"event":"room_started"}`),
	)
	invalidRequest.Header.Set("Content-Type", "application/json")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalidRequest)
	if invalidResponse.Code != http.StatusUnauthorized || verifier.called {
		t.Fatalf(
			"ordinary JSON must not reach webhook verifier: status=%d called=%t",
			invalidResponse.Code,
			verifier.called,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		liveKitWebhookPath,
		strings.NewReader(`{"event":"room_started"}`),
	)
	request.Header.Set("Content-Type", "application/webhook+json")
	request.Header.Set("Authorization", "Bearer signed-webhook-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("receive LiveKit webhook: status=%d body=%s", response.Code, response.Body.String())
	}
	if !verifier.called || !mediaService.webhookCalled || mediaService.webhookEvent.ID != event.ID {
		t.Fatalf("webhook pipeline was not completed: verifier=%+v service=%+v", verifier, mediaService)
	}
}

func TestLiveKitWebhookHidesVerificationFailures(t *testing.T) {
	t.Parallel()

	mediaService := &fakeMediaService{}
	verifier := &fakeWebhookVerifier{requestError: errors.New("signature mismatch: secret detail")}
	handler := newMediaTestHandler(nil, mediaService, verifier)
	request := httptest.NewRequest(
		http.MethodPost,
		liveKitWebhookPath,
		strings.NewReader(`{"event":"room_started"}`),
	)
	request.Header.Set("Content-Type", "application/webhook+json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid signature status=%d body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret detail") || mediaService.webhookCalled {
		t.Fatalf("verification details leaked or service called: %s", response.Body.String())
	}
}

func newMediaTestHandler(
	identityService identity.ServiceAPI,
	mediaService media.ServiceAPI,
	webhookVerifier media.WebhookVerifier,
) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test",
			Port:        "8080",
			WebOrigin:   "http://localhost:5173",
			Authentication: config.AuthenticationConfig{
				SessionTTL: 8 * time.Hour,
			},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{
			Clock: fixedClock, Identity: identityService, Media: mediaService,
			LiveKitWebhook: webhookVerifier,
		},
	)
}

func fixedClock() time.Time {
	return fixedTime
}

func addAuthenticatedMutationCookies(request *http.Request) {
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	request.Header.Set(csrfHeader, "csrf-token")
}

func assertMediaAccess(t *testing.T, access media.AccessContext, principal identity.Principal) {
	t.Helper()
	if principal.ActiveTenant == nil || access.TenantID != principal.ActiveTenant.ID ||
		access.ActorID != principal.User.ID || access.SessionID != principal.SessionID ||
		access.Role != principal.ActiveTenant.Role || access.DisplayName != principal.User.DisplayName ||
		!access.MembershipActive || len(access.OrganizationRoles) != 1 ||
		string(access.OrganizationRoles[0]) != principal.ActiveTenant.Role {
		t.Fatalf("unexpected media access: access=%+v principal=%+v", access, principal)
	}
}

type fakeMediaService struct {
	access        media.AccessContext
	classID       uuid.UUID
	credential    media.JoinCredential
	issueCalled   bool
	eventCalled   bool
	eventInput    media.ClientEventInput
	webhookCalled bool
	webhookEvent  media.WebhookEvent
	webhookResult media.WebhookResult
	requestError  error
}

func (service *fakeMediaService) IssueJoinCredential(
	_ context.Context,
	access media.AccessContext,
	classID uuid.UUID,
) (media.JoinCredential, error) {
	service.issueCalled = true
	service.access = access
	service.classID = classID
	return service.credential, service.requestError
}

func (service *fakeMediaService) RecordClientEvent(
	_ context.Context,
	access media.AccessContext,
	classID uuid.UUID,
	input media.ClientEventInput,
) error {
	service.eventCalled = true
	service.access = access
	service.classID = classID
	service.eventInput = input
	return service.requestError
}

func (service *fakeMediaService) RecordWebhook(
	_ context.Context,
	event media.WebhookEvent,
) (media.WebhookResult, error) {
	service.webhookCalled = true
	service.webhookEvent = event
	return service.webhookResult, service.requestError
}

type fakeWebhookVerifier struct {
	called       bool
	event        media.WebhookEvent
	requestError error
}

func (verifier *fakeWebhookVerifier) Receive(_ *http.Request) (media.WebhookEvent, error) {
	verifier.called = true
	return verifier.event, verifier.requestError
}
