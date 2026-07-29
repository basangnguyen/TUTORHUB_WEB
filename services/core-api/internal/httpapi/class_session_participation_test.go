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
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
)

func TestClassSessionParticipationRoutesExposeScopedAudienceAndMutations(
	t *testing.T,
) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	classID, sessionID, attendeeID, peerID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	respondedAt := fixedTime.Add(-time.Hour)
	audience := classroom.SessionAudience{
		AudienceRevision:  3,
		ResponseRequested: true,
		Attendees: []classroom.SessionAudienceAttendee{
			{
				ID: attendeeID, UserID: actorID,
				ParticipationRole: classroom.ParticipationRoleRequired,
				BusinessRole:      "teacher", RSVPState: classroom.RSVPStateAccepted,
				RespondedAt: &respondedAt, Version: 4, IsSelf: true,
			},
			{
				ID: uuid.New(), UserID: peerID,
				ParticipationRole: classroom.ParticipationRoleOptional,
				BusinessRole:      "student", RSVPState: classroom.RSVPStateNeedsAction,
				Version: 1,
			},
		},
		ViewerAccess: classroom.SessionAudienceViewerAccess{
			CanManageAttendees: true,
			CanRespond:         true,
		},
	}
	service := &fakeSessionParticipationService{
		audience: audience,
		replaceResult: classroom.SessionAudienceMutationResult{
			Audience: audience,
			Replayed: true,
		},
		respondResult: classroom.SelfRSVPMutationResult{
			Attendee: audience.Attendees[0],
		},
	}
	handler := sessionParticipationTestHandler(tenantID, actorID, service)

	getRequest := httptest.NewRequest(
		http.MethodGet,
		sessionParticipationPath(classID, sessionID, "attendees"),
		nil,
	)
	addSessionCookie(getRequest)
	getRequest.Header.Set(calendarTenantHeader, tenantID.String())
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get audience status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	assertSessionParticipationPrivateHeaders(t, getResponse)
	if service.getCalls != 1 || service.classID != classID || service.sessionID != sessionID {
		t.Fatalf("unexpected get service state: %+v", service)
	}
	assertClassAccess(t, service.access, tenantID, actorID)
	var getBody sessionAudienceResponse
	if err := json.Unmarshal(getResponse.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode audience response: %v", err)
	}
	if getBody.AudienceRevision != 3 || len(getBody.Attendees) != 2 ||
		getBody.Attendees[0].RespondedAt == nil ||
		!getBody.Attendees[0].RespondedAt.Equal(respondedAt) ||
		!getBody.ViewerAccess.CanManageAttendees {
		t.Fatalf("unexpected audience response: %+v", getBody)
	}
	for _, forbidden := range []string{
		"email", "display_name", "delivery_address", "ciphertext", "fingerprint",
	} {
		if strings.Contains(strings.ToLower(getResponse.Body.String()), forbidden) {
			t.Fatalf("audience response leaked %q: %s", forbidden, getResponse.Body.String())
		}
	}

	putBody := `{"expected_audience_revision":3,` +
		`"idempotency_key":"audience-route-0001","response_requested":true,` +
		`"attendees":[{"user_id":"` + actorID.String() +
		`","participation_role":"required"},{"user_id":"` + peerID.String() +
		`","participation_role":"optional"}]}`
	putResponse := performSessionParticipationMutation(
		handler,
		http.MethodPut,
		sessionParticipationPath(classID, sessionID, "attendees"),
		putBody,
		tenantID,
	)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("replace audience status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	if service.replaceCalls != 1 ||
		service.replaceInput.ExpectedAudienceRevision != 3 ||
		service.replaceInput.IdempotencyKey != "audience-route-0001" ||
		!service.replaceInput.ResponseRequested ||
		len(service.replaceInput.Attendees) != 2 ||
		service.replaceInput.Attendees[1].UserID != peerID {
		t.Fatalf("unexpected replace input: %+v", service.replaceInput)
	}
	var replaceBody replaceSessionAudienceResponse
	if err := json.Unmarshal(putResponse.Body.Bytes(), &replaceBody); err != nil {
		t.Fatalf("decode replace response: %v", err)
	}
	if !replaceBody.Replayed || replaceBody.Audience.Attendees == nil {
		t.Fatalf("unexpected replace response: %+v", replaceBody)
	}

	responseBody := `{"state":"tentative","note":"Cần xác nhận lại",` +
		`"expected_attendee_version":4,"idempotency_key":"rsvp-route-000001"}`
	postResponse := performSessionParticipationMutation(
		handler,
		http.MethodPost,
		sessionParticipationPath(classID, sessionID, "responses"),
		responseBody,
		tenantID,
	)
	if postResponse.Code != http.StatusOK {
		t.Fatalf("respond status=%d body=%s", postResponse.Code, postResponse.Body.String())
	}
	if service.respondCalls != 1 ||
		service.respondInput.State != classroom.RSVPStateTentative ||
		service.respondInput.Note != "Cần xác nhận lại" ||
		service.respondInput.ExpectedAttendeeVersion != 4 ||
		service.respondInput.IdempotencyKey != "rsvp-route-000001" {
		t.Fatalf("unexpected response input: %+v", service.respondInput)
	}
}

func TestClassSessionParticipationRequiresTenantAssertionCSRFAndCompleteMutation(
	t *testing.T,
) {
	t.Parallel()
	tenantID, actorID, classID, sessionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	service := &fakeSessionParticipationService{}
	handler := sessionParticipationTestHandler(tenantID, actorID, service)
	attendeesPath := sessionParticipationPath(classID, sessionID, "attendees")

	missingTenant := httptest.NewRequest(http.MethodGet, attendeesPath, nil)
	addSessionCookie(missingTenant)
	missingTenantResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingTenantResponse, missingTenant)
	assertCalendarProblem(
		t,
		missingTenantResponse,
		http.StatusBadRequest,
		"class_session_participation_invalid",
	)

	staleTenant := httptest.NewRequest(http.MethodGet, attendeesPath, nil)
	addSessionCookie(staleTenant)
	staleTenant.Header.Set(calendarTenantHeader, uuid.NewString())
	staleTenantResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleTenantResponse, staleTenant)
	assertCalendarProblem(
		t,
		staleTenantResponse,
		http.StatusConflict,
		"calendar_scope_changed",
	)

	missingCSRF := httptest.NewRequest(
		http.MethodPut,
		attendeesPath,
		strings.NewReader(
			`{"expected_audience_revision":0,"idempotency_key":"audience-route-0002",`+
				`"response_requested":false,"attendees":[]}`,
		),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(calendarTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf(
			"missing CSRF status=%d body=%s",
			missingCSRFResponse.Code,
			missingCSRFResponse.Body.String(),
		)
	}

	incomplete := performSessionParticipationMutation(
		handler,
		http.MethodPost,
		sessionParticipationPath(classID, sessionID, "responses"),
		`{"state":"accepted","expected_attendee_version":1}`,
		tenantID,
	)
	assertCalendarProblem(
		t,
		incomplete,
		http.StatusBadRequest,
		"class_session_participation_invalid",
	)
	if service.getCalls != 0 || service.replaceCalls != 0 || service.respondCalls != 0 {
		t.Fatalf("invalid boundaries reached participation service: %+v", service)
	}
}

func TestClassSessionParticipationMapsStableDomainErrors(t *testing.T) {
	t.Parallel()
	tenantID, actorID, classID, sessionID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{
			name: "forbidden", err: classroom.ErrSessionParticipationAccessDenied,
			wantStatus: http.StatusForbidden, wantCode: "class_session_participation_forbidden",
		},
		{
			name: "not found", err: classroom.ErrSessionParticipationNotFound,
			wantStatus: http.StatusNotFound, wantCode: "class_session_participation_not_found",
		},
		{
			name: "audience stale", err: classroom.ErrSessionAudienceVersionConflict,
			wantStatus: http.StatusConflict, wantCode: "class_session_audience_conflict",
		},
		{
			name:       "idempotency conflict",
			err:        classroom.ErrSessionParticipationIdempotencyConflict,
			wantStatus: http.StatusConflict,
			wantCode:   "class_session_participation_idempotency_conflict",
		},
		{
			name: "unavailable", err: classroom.ErrSessionParticipationUnavailable,
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "class_session_participation_unavailable",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeSessionParticipationService{requestError: test.err}
			handler := sessionParticipationTestHandler(tenantID, actorID, service)
			request := httptest.NewRequest(
				http.MethodGet,
				sessionParticipationPath(classID, sessionID, "attendees"),
				nil,
			)
			addSessionCookie(request)
			request.Header.Set(calendarTenantHeader, tenantID.String())
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertCalendarProblem(t, response, test.wantStatus, test.wantCode)
			assertSessionParticipationPrivateHeaders(t, response)
		})
	}
}

type fakeSessionParticipationService struct {
	audience      classroom.SessionAudience
	replaceResult classroom.SessionAudienceMutationResult
	respondResult classroom.SelfRSVPMutationResult
	requestError  error

	getCalls     int
	replaceCalls int
	respondCalls int
	access       classroom.AccessContext
	classID      uuid.UUID
	sessionID    uuid.UUID
	replaceInput classroom.ReplaceAudienceInput
	respondInput classroom.SelfRSVPInput
}

func (service *fakeSessionParticipationService) GetSessionAudience(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (classroom.SessionAudience, error) {
	service.getCalls++
	service.access, service.classID, service.sessionID = access, classID, sessionID
	return service.audience, service.requestError
}

func (service *fakeSessionParticipationService) ReplaceSessionAudience(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
	input classroom.ReplaceAudienceInput,
) (classroom.SessionAudienceMutationResult, error) {
	service.replaceCalls++
	service.access, service.classID, service.sessionID = access, classID, sessionID
	service.replaceInput = input
	return service.replaceResult, service.requestError
}

func (service *fakeSessionParticipationService) RespondToSession(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
	input classroom.SelfRSVPInput,
) (classroom.SelfRSVPMutationResult, error) {
	service.respondCalls++
	service.access, service.classID, service.sessionID = access, classID, sessionID
	service.respondInput = input
	return service.respondResult, service.requestError
}

func sessionParticipationTestHandler(
	tenantID uuid.UUID,
	actorID uuid.UUID,
	service classroom.SessionParticipationServiceAPI,
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
			Clock:                func() time.Time { return fixedTime },
			Identity:             classIdentityService(tenantID, actorID, nil),
			SessionParticipation: service,
		},
	)
}

func sessionParticipationPath(
	classID uuid.UUID,
	sessionID uuid.UUID,
	resource string,
) string {
	return "/api/v1/classes/" + classID.String() +
		"/sessions/" + sessionID.String() + "/" + resource
}

func performSessionParticipationMutation(
	handler http.Handler,
	method string,
	path string,
	body string,
	tenantID uuid.UUID,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	request.Header.Set(calendarTenantHeader, tenantID.String())
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertSessionParticipationPrivateHeaders(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	cacheControl := response.Header().Get("Cache-Control")
	if cacheControl != "private, no-store" && cacheControl != "no-store" {
		t.Fatalf("cache control=%q", cacheControl)
	}
	if response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("referrer policy=%q", response.Header().Get("Referrer-Policy"))
	}
	vary := response.Header().Values("Vary")
	if len(vary) != 2 || vary[0] != "Cookie" || vary[1] != calendarTenantHeader {
		t.Fatalf("vary=%v", vary)
	}
}
