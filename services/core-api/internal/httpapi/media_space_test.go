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
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/media"
)

func TestMediaSpaceCreateRequiresCSRFAndExpectedTenant(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	identityService := classIdentityService(tenantID, actorID, nil)
	meetingID, spaceID := uuid.New(), uuid.New()
	service := &fakeMediaLifecycleService{createResult: media.CreateSpaceResult{
		Created: true,
		Space: media.MediaSpace{
			ID: spaceID, Source: media.SourceReference{
				Kind: media.SourceStudyMeeting, StudyMeetingID: &meetingID,
			},
			Status: media.SpaceStatusScheduled, Version: 1,
			CreatedAt: fixedTime, UpdatedAt: fixedTime,
		},
	}}
	handler := newMediaSpaceTestHandler(identityService, service)
	body := `{"source":{"kind":"study_meeting","study_meeting_id":"` + meetingID.String() +
		`"},"idempotency_key":"media-create-0001"}`

	missingCSRF := httptest.NewRequest(http.MethodPost, mediaSpacesCollectionPath, strings.NewReader(body))
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || service.createCalls != 0 {
		t.Fatalf("missing CSRF must fail closed: status=%d calls=%d", missingCSRFResponse.Code, service.createCalls)
	}

	missingTenant := httptest.NewRequest(http.MethodPost, mediaSpacesCollectionPath, strings.NewReader(body))
	missingTenant.Header.Set("Content-Type", "application/json")
	addAuthenticatedMutationCookies(missingTenant)
	missingTenantResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingTenantResponse, missingTenant)
	if missingTenantResponse.Code != http.StatusBadRequest || service.createCalls != 0 {
		t.Fatalf("missing expected tenant must fail closed: status=%d calls=%d", missingTenantResponse.Code, service.createCalls)
	}

	request := httptest.NewRequest(http.MethodPost, mediaSpacesCollectionPath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create media space: status=%d body=%s", response.Code, response.Body.String())
	}
	if service.createCalls != 1 || service.createInput.Source.StudyMeetingID != meetingID ||
		service.createInput.IdempotencyKey != "media-create-0001" {
		t.Fatalf("unexpected create call: %+v", service)
	}
	assertMediaAccess(t, service.access, identityService.principal)
	if response.Header().Get("Cache-Control") != "private, no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("missing privacy headers: %v", response.Header())
	}
}

func TestMediaSpaceReadUsesAuthenticatedSessionWithoutCSRF(t *testing.T) {
	t.Parallel()

	tenantID, actorID, spaceID := uuid.New(), uuid.New(), uuid.New()
	identityService := classIdentityService(tenantID, actorID, nil)
	service := &fakeMediaLifecycleService{getResult: media.MediaSpace{
		ID: spaceID, Status: media.SpaceStatusOpen, Version: 2,
		CreatedAt: fixedTime, UpdatedAt: fixedTime,
	}}
	handler := newMediaSpaceTestHandler(identityService, service)
	request := httptest.NewRequest(
		http.MethodGet, "/api/v1/media/spaces/"+spaceID.String(), nil,
	)
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addSessionCookie(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || service.getCalls != 1 || service.spaceID != spaceID {
		t.Fatalf("read media space: status=%d service=%+v body=%s", response.Code, service, response.Body.String())
	}
}

func TestMediaSpaceQuotaRejectionIsAlwaysTooManyRequests(t *testing.T) {
	t.Parallel()

	tenantID, actorID, spaceID := uuid.New(), uuid.New(), uuid.New()
	identityService := classIdentityService(tenantID, actorID, nil)
	service := &fakeMediaLifecycleService{requestError: &featurecontrol.QuotaExceededError{
		Quota: featurecontrol.QuotaActiveMediaSpaces, Limit: 10, Used: 10,
	}}
	handler := newMediaSpaceTestHandler(identityService, service)
	request := httptest.NewRequest(
		http.MethodPost, "/api/v1/media/spaces/"+spaceID.String()+"/start",
		strings.NewReader(`{"expected_version":1,"idempotency_key":"media-start-00001"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(mediaSpaceTenantHeader, tenantID.String())
	addAuthenticatedMutationCookies(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests ||
		!strings.Contains(response.Body.String(), `"code":"quota_exceeded"`) {
		t.Fatalf("quota rejection must be stable 429: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMediaSpaceScopeMismatchStopsBeforeService(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	service := &fakeMediaLifecycleService{}
	handler := newMediaSpaceTestHandler(classIdentityService(tenantID, actorID, nil), service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/media/spaces/"+uuid.NewString(), nil)
	request.Header.Set(mediaSpaceTenantHeader, uuid.NewString())
	addSessionCookie(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || service.getCalls != 0 ||
		!strings.Contains(response.Body.String(), `"code":"media_space_scope_changed"`) {
		t.Fatalf("scope mismatch must stop before service: status=%d calls=%d body=%s", response.Code, service.getCalls, response.Body.String())
	}
}

func newMediaSpaceTestHandler(
	identityService identity.ServiceAPI,
	service media.LifecycleServiceAPI,
) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{Clock: fixedClock, Identity: identityService, MediaSpaces: service},
	)
}

type fakeMediaLifecycleService struct {
	access           media.AccessContext
	spaceID          uuid.UUID
	createCalls      int
	getCalls         int
	transitionCalls  int
	operation        string
	createInput      media.CreateSpaceInput
	transitionInput  media.TransitionInput
	createResult     media.CreateSpaceResult
	getResult        media.MediaSpace
	transitionResult media.MediaSpace
	requestError     error
}

func (service *fakeMediaLifecycleService) CreateSpace(
	_ context.Context,
	access media.AccessContext,
	input media.CreateSpaceInput,
) (media.CreateSpaceResult, error) {
	service.createCalls++
	service.access, service.createInput = access, input
	return service.createResult, service.requestError
}

func (service *fakeMediaLifecycleService) GetSpace(
	_ context.Context,
	access media.AccessContext,
	spaceID uuid.UUID,
) (media.MediaSpace, error) {
	service.getCalls++
	service.access, service.spaceID = access, spaceID
	return service.getResult, service.requestError
}

func (service *fakeMediaLifecycleService) StartSpace(
	_ context.Context,
	access media.AccessContext,
	spaceID uuid.UUID,
	input media.TransitionInput,
) (media.MediaSpace, error) {
	return service.transition(access, spaceID, input, "start")
}

func (service *fakeMediaLifecycleService) EndSpace(
	_ context.Context,
	access media.AccessContext,
	spaceID uuid.UUID,
	input media.TransitionInput,
) (media.MediaSpace, error) {
	return service.transition(access, spaceID, input, "end")
}

func (service *fakeMediaLifecycleService) CancelSpace(
	_ context.Context,
	access media.AccessContext,
	spaceID uuid.UUID,
	input media.TransitionInput,
) (media.MediaSpace, error) {
	return service.transition(access, spaceID, input, "cancel")
}

func (service *fakeMediaLifecycleService) transition(
	access media.AccessContext,
	spaceID uuid.UUID,
	input media.TransitionInput,
	operation string,
) (media.MediaSpace, error) {
	service.transitionCalls++
	service.access, service.spaceID = access, spaceID
	service.transitionInput, service.operation = input, operation
	return service.transitionResult, service.requestError
}
