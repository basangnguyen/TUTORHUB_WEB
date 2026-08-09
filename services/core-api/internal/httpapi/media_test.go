package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/livekit/protocol/auth"
	livekitproto "github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/utils/protojson"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
)

func TestInstanceCredentialRequiresCSRFExpectedTenantAndStrictRequest(t *testing.T) {
	t.Parallel()

	tenantID, actorID, spaceID, joinAttemptID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	identityService := classIdentityService(tenantID, actorID, nil)
	credential := media.InstanceCredential{
		AccessToken: "signed-room-instance-token", ServerURL: "wss://staging.example.test",
		ParticipantSessionID: uuid.New(), RoomInstanceID: uuid.New(), JoinAttemptID: joinAttemptID,
		InstanceRole: media.InstanceRoleHost, CanPublishCameraMicrophone: true,
		CanShareScreen: true, CanSubscribe: true, ExpiresAt: fixedTime.Add(5 * time.Minute),
	}
	service := &fakeInstanceCredentialService{credential: credential}
	handler := newP4MediaTestHandler(identityService, service, nil, nil, nil, nil)
	path := "/api/v1/media/spaces/" + spaceID.String() + "/join-credentials"
	body := `{"join_attempt_id":"` + joinAttemptID.String() + `"}`

	missingCSRF := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || service.calls != 0 {
		t.Fatalf("missing CSRF must fail closed: status=%d calls=%d", missingCSRFResponse.Code, service.calls)
	}

	missingTenant := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	missingTenant.Header.Set("Content-Type", "application/json")
	addAuthenticatedMutationCookies(missingTenant)
	missingTenantResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingTenantResponse, missingTenant)
	if missingTenantResponse.Code != http.StatusBadRequest || service.calls != 0 {
		t.Fatalf("missing tenant assertion must fail closed: status=%d calls=%d", missingTenantResponse.Code, service.calls)
	}

	unknownField := httptest.NewRequest(
		http.MethodPost,
		path,
		strings.NewReader(`{"join_attempt_id":"`+joinAttemptID.String()+`","role":"host"}`),
	)
	unknownField.Header.Set("Content-Type", "application/json")
	unknownField.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(unknownField)
	unknownFieldResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownFieldResponse, unknownField)
	if unknownFieldResponse.Code != http.StatusBadRequest || service.calls != 0 {
		t.Fatalf("client-supplied grants must be rejected: status=%d calls=%d", unknownFieldResponse.Code, service.calls)
	}

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("issue instance credential: status=%d body=%s", response.Code, response.Body.String())
	}
	rawResponse := append([]byte(nil), response.Body.Bytes()...)
	var actual media.InstanceCredential
	if err := json.Unmarshal(rawResponse, &actual); err != nil {
		t.Fatalf("decode instance credential: %v", err)
	}
	if actual.AccessToken != credential.AccessToken || actual.JoinAttemptID != joinAttemptID ||
		actual.RoomInstanceID != credential.RoomInstanceID || service.calls != 1 ||
		service.spaceID != spaceID || service.input.JoinAttemptID != joinAttemptID {
		t.Fatalf("unexpected instance credential flow: body=%+v service=%+v", actual, service)
	}
	assertMediaAccess(t, service.access, identityService.principal)
	if response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		bytes.Contains(rawResponse, []byte("room_name")) ||
		bytes.Contains(rawResponse, []byte("participant_identity")) {
		t.Fatalf("credential response privacy boundary failed: headers=%v body=%s", response.Header(), rawResponse)
	}
}

func TestInstanceCredentialMapsTypedErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		status     int
		code       string
		retryAfter string
	}{
		{name: "invalid", err: media.ErrInvalidCredentialRequest, status: http.StatusBadRequest, code: "media_credential_invalid"},
		{name: "not open", err: media.ErrRoomNotOpen, status: http.StatusConflict, code: "room_not_open"},
		{name: "locked", err: media.ErrRoomLocked, status: http.StatusConflict, code: "room_locked"},
		{name: "admission", err: media.ErrAdmissionRequired, status: http.StatusConflict, code: "admission_required"},
		{name: "participant", err: media.ErrParticipantConflict, status: http.StatusConflict, code: "participant_session_conflict"},
		{name: "access", err: media.ErrSpaceAccessDenied, status: http.StatusForbidden, code: "media_credential_forbidden"},
		{name: "conceal", err: media.ErrSpaceNotFound, status: http.StatusNotFound, code: "media_space_not_found"},
		{name: "rate", err: &media.CredentialRateLimitError{RetryAfter: 2500 * time.Millisecond}, status: http.StatusTooManyRequests, code: "media_credential_rate_limited", retryAfter: "3"},
		{name: "provider", err: media.ErrMediaProviderUnavailable, status: http.StatusServiceUnavailable, code: "media_provider_unavailable"},
		{name: "lifecycle", err: media.ErrLifecycleUnavailable, status: http.StatusServiceUnavailable, code: "media_credential_unavailable"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tenantID, actorID, spaceID, attemptID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
			service := &fakeInstanceCredentialService{requestError: test.err}
			handler := newP4MediaTestHandler(
				classIdentityService(tenantID, actorID, nil), service, nil, nil, nil, nil,
			)
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/media/spaces/"+spaceID.String()+"/join-credentials",
				strings.NewReader(`{"join_attempt_id":"`+attemptID.String()+`"}`),
			)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
			addAuthenticatedMutationCookies(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status ||
				!strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) ||
				response.Header().Get("Retry-After") != test.retryAfter {
				t.Fatalf(
					"typed error mismatch: status=%d retry=%q body=%s",
					response.Code, response.Header().Get("Retry-After"), response.Body.String(),
				)
			}
		})
	}
}

func TestProviderWebhookUsesP4ProcessorWithoutIdentifierLogs(t *testing.T) {
	t.Parallel()

	event := media.WebhookEvent{
		ID: "EV_private_event_01", EventType: "participant_joined",
		RoomName: "opaque_private_room", RoomSID: "RM_private_sid",
		ParticipantIdentity: "opaque_private_participant", ParticipantSID: "PA_private_sid",
		OccurredAt: fixedTime,
	}
	legacy := &fakeMediaService{}
	processor := &fakeWebhookProcessor{result: media.WebhookResult{Recorded: true}}
	verifier := &fakeWebhookVerifier{event: event}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := newP4MediaTestHandler(nil, nil, legacy, verifier, processor, logger)
	request := httptest.NewRequest(
		http.MethodPost, liveKitWebhookPath, strings.NewReader(`{"event":"participant_joined"}`),
	)
	request.Header.Set("Content-Type", "application/webhook+json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || !verifier.called || !processor.called || legacy.webhookCalled {
		t.Fatalf("P4 webhook processor was not exclusive: status=%d verifier=%+v processor=%+v legacy=%+v", response.Code, verifier, processor, legacy)
	}
	for _, secret := range []string{
		event.ID, event.RoomName, event.RoomSID, event.ParticipantIdentity, event.ParticipantSID,
	} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("provider identifier leaked into logs: %q", secret)
		}
	}
}

func TestProviderWebhookUsesOfficialSignatureVerifierBeforeMutation(t *testing.T) {
	t.Parallel()

	const (
		apiKey    = "test-api-key"
		apiSecret = "test-api-secret-long-enough"
	)
	event := &livekitproto.WebhookEvent{
		Id: "EV_official_signature_01", Event: "room_started",
		CreatedAt: fixedTime.Unix(),
		Room: &livekitproto.Room{
			Sid:  "RM_official_signature_01",
			Name: media.RoomName(uuid.New(), uuid.New()),
		},
	}
	body, token := signedLiveKitWebhookPayload(t, apiKey, apiSecret, event)

	t.Run("valid signature reaches processor", func(t *testing.T) {
		verifier, err := media.NewLiveKitWebhookVerifier(apiKey, apiSecret)
		if err != nil {
			t.Fatalf("create official verifier: %v", err)
		}
		processor := &fakeWebhookProcessor{result: media.WebhookResult{Recorded: true}}
		handler := newP4MediaTestHandler(nil, nil, nil, verifier, processor, nil)
		request := httptest.NewRequest(http.MethodPost, liveKitWebhookPath, bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/webhook+json")
		request.Header.Set("Authorization", token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent || !processor.called ||
			processor.event.ID != event.Id || processor.event.RoomSID != event.Room.Sid {
			t.Fatalf(
				"valid official webhook did not reach processor: status=%d processor=%+v",
				response.Code, processor,
			)
		}
	})

	_, wrongToken := signedLiveKitWebhookPayload(
		t, apiKey, "different-test-secret-long-enough", event,
	)
	tamperedBody := append(append([]byte(nil), body...), ' ')
	tests := []struct {
		name  string
		body  []byte
		token string
	}{
		{name: "unsigned", body: body},
		{name: "wrong key", body: body, token: wrongToken},
		{name: "body tamper", body: tamperedBody, token: token},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			verifier, err := media.NewLiveKitWebhookVerifier(apiKey, apiSecret)
			if err != nil {
				t.Fatalf("create official verifier: %v", err)
			}
			processor := &fakeWebhookProcessor{}
			handler := newP4MediaTestHandler(nil, nil, nil, verifier, processor, nil)
			request := httptest.NewRequest(
				http.MethodPost, liveKitWebhookPath, bytes.NewReader(test.body),
			)
			request.Header.Set("Content-Type", "application/webhook+json")
			if test.token != "" {
				request.Header.Set("Authorization", test.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || processor.called {
				t.Fatalf(
					"invalid official webhook crossed mutation boundary: status=%d called=%t",
					response.Code, processor.called,
				)
			}
		})
	}
}

func signedLiveKitWebhookPayload(
	t *testing.T,
	apiKey string,
	apiSecret string,
	event *livekitproto.WebhookEvent,
) ([]byte, string) {
	t.Helper()
	body, err := protojson.Marshal(event)
	if err != nil {
		t.Fatalf("marshal webhook: %v", err)
	}
	digest := sha256.Sum256(body)
	token, err := auth.NewAccessToken(apiKey, apiSecret).
		SetSha256(base64.StdEncoding.EncodeToString(digest[:])).
		ToJWT()
	if err != nil {
		t.Fatalf("sign webhook: %v", err)
	}
	return body, token
}

func TestLegacyMediaDisabledReturnsGone(t *testing.T) {
	t.Parallel()

	tenantID, actorID, classID := uuid.New(), uuid.New(), uuid.New()
	legacy := &fakeMediaService{requestError: media.ErrLegacyMediaDisabled}
	handler := newMediaTestHandler(classIdentityService(tenantID, actorID, nil), legacy, nil)
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/classes/"+classID.String()+"/media-token", nil,
	)
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusGone {
		t.Fatalf("disabled legacy media status=%d body=%s", response.Code, response.Body.String())
	}
}

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

func newP4MediaTestHandler(
	identityService identity.ServiceAPI,
	credentialService media.InstanceCredentialServiceAPI,
	legacyService media.ServiceAPI,
	webhookVerifier media.WebhookVerifier,
	webhookProcessor media.WebhookProcessor,
	logger *slog.Logger,
) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test",
			Port:        "8080",
			WebOrigin:   "http://localhost:5173",
			Authentication: config.AuthenticationConfig{
				SessionTTL: 8 * time.Hour,
			},
		},
		logger,
		Options{
			Clock: fixedClock, Identity: identityService, Media: legacyService,
			MediaCredentials: credentialService, LiveKitWebhook: webhookVerifier,
			MediaWebhooks: webhookProcessor,
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

type fakeInstanceCredentialService struct {
	access       media.AccessContext
	spaceID      uuid.UUID
	input        media.IssueInstanceCredentialInput
	credential   media.InstanceCredential
	requestError error
	calls        int
}

func (service *fakeInstanceCredentialService) IssueInstanceCredential(
	_ context.Context,
	access media.AccessContext,
	spaceID uuid.UUID,
	input media.IssueInstanceCredentialInput,
) (media.InstanceCredential, error) {
	service.calls++
	service.access, service.spaceID, service.input = access, spaceID, input
	return service.credential, service.requestError
}

type fakeWebhookProcessor struct {
	event        media.WebhookEvent
	result       media.WebhookResult
	requestError error
	called       bool
}

func (processor *fakeWebhookProcessor) RecordWebhook(
	_ context.Context,
	event media.WebhookEvent,
) (media.WebhookResult, error) {
	processor.called = true
	processor.event = event
	return processor.result, processor.requestError
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
