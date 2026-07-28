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
	"github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

func TestCalendarSchedulingWorkingScheduleGetAndPut(t *testing.T) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	service := &fakeCalendarSchedulingService{
		getResult: calendar.WorkingSchedule{
			Timezone: "Asia/Ho_Chi_Minh",
			WeeklyIntervals: []calendar.WeeklyWorkingInterval{{
				Weekday: "monday",
				CivilTimeInterval: calendar.CivilTimeInterval{
					StartsAt: "08:00", EndsAt: "17:00",
				},
			}},
			Exceptions: []calendar.WorkingScheduleException{},
			Source:     "user_override", Version: 3, UpdatedAt: calendarTestTime,
		},
		putResult: calendar.WorkingSchedule{
			Timezone: "Asia/Ho_Chi_Minh",
			WeeklyIntervals: []calendar.WeeklyWorkingInterval{{
				Weekday: "tuesday",
				CivilTimeInterval: calendar.CivilTimeInterval{
					StartsAt: "09:00", EndsAt: "16:30",
				},
			}},
			Exceptions: []calendar.WorkingScheduleException{{
				Date: "2026-09-02", Kind: "holiday", Intervals: []calendar.CivilTimeInterval{},
			}},
			Source: "user_override", Version: 4, UpdatedAt: calendarTestTime.Add(time.Minute),
		},
	}
	handlers := calendarSchedulingTestHandlers(
		classIdentityService(tenantID, actorID, nil),
		service,
	)

	getRequest := httptest.NewRequest(http.MethodGet, calendarWorkingSchedulePath, nil)
	addSessionCookie(getRequest)
	getRequest.Header.Set(calendarTenantHeader, tenantID.String())
	getResponse := httptest.NewRecorder()
	handlers.workingScheduleHandler().ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get working schedule status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	assertCalendarNoStoreHeaders(t, getResponse)
	if service.getCalls != 1 || service.getScope.TenantID != tenantID ||
		service.getScope.ActorID != actorID {
		t.Fatalf("unexpected working schedule get scope: %+v", service.getScope)
	}

	body := `{"timezone":"Asia/Ho_Chi_Minh","weekly_intervals":[` +
		`{"weekday":"tuesday","starts_at":"09:00","ends_at":"16:30"}],` +
		`"exceptions":[{"date":"2026-09-02","kind":"holiday","intervals":[]}],` +
		`"expected_version":3}`
	putRequest := calendarSchedulingMutationRequest(
		http.MethodPut,
		calendarWorkingSchedulePath,
		body,
		tenantID,
	)
	putResponse := httptest.NewRecorder()
	handlers.workingScheduleHandler().ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put working schedule status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	assertCalendarNoStoreHeaders(t, putResponse)
	if service.putCalls != 1 || service.putScope.TenantID != tenantID ||
		service.putScope.ActorID != actorID || service.putInput.ExpectedVersion != 3 ||
		len(service.putInput.WeeklyIntervals) != 1 ||
		service.putInput.WeeklyIntervals[0].Weekday != "tuesday" ||
		len(service.putInput.Exceptions) != 1 ||
		service.putInput.Exceptions[0].Kind != "holiday" {
		t.Fatalf("unexpected working schedule put: scope=%+v input=%+v", service.putScope, service.putInput)
	}
	var result calendar.WorkingSchedule
	if err := json.Unmarshal(putResponse.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode working schedule: %v", err)
	}
	if result.Version != 4 || result.WeeklyIntervals == nil || result.Exceptions == nil {
		t.Fatalf("unexpected working schedule response: %+v", result)
	}
}

func TestCalendarSchedulingAvailabilityIsScopedAndPrivacyFiltered(t *testing.T) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	classID, requiredID, externalID := uuid.New(), uuid.New(), uuid.New()
	queryFrom := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC)
	emptyReason := "no_valid_civil_slots"
	service := &fakeCalendarSchedulingService{
		queryResult: calendar.AvailabilityResult{
			Timezone: "Asia/Ho_Chi_Minh",
			Participants: []calendar.ParticipantAvailability{
				{
					Participant: calendar.AvailabilityParticipantReference{
						Kind: "internal_user", ID: requiredID.String(),
					},
					Role: "required",
					Intervals: []calendar.AvailabilityStatusInterval{{
						StartsAt: queryFrom, EndsAt: queryFrom.Add(24 * time.Hour), Status: "free",
					}},
					WorkingIntervals: []calendar.AvailabilityWorkingInterval{{
						StartsAt: queryFrom.Add(time.Hour), EndsAt: queryFrom.Add(10 * time.Hour),
					}},
				},
				{
					Participant: calendar.AvailabilityParticipantReference{
						Kind: "external_guest", ID: externalID.String(),
					},
					Role: "optional",
					Intervals: []calendar.AvailabilityStatusInterval{{
						StartsAt: queryFrom, EndsAt: queryFrom.Add(24 * time.Hour), Status: "unknown",
					}},
					WorkingIntervals: []calendar.AvailabilityWorkingInterval{},
				},
			},
			Suggestions:            []calendar.SuggestedTime{},
			EmptySuggestionsReason: &emptyReason,
		},
	}
	handlers := calendarSchedulingTestHandlers(
		classIdentityService(tenantID, actorID, nil),
		service,
	)
	body := `{"class_id":"` + classID.String() + `",` +
		`"from":"2026-07-29T00:00:00Z","to":"2026-07-30T00:00:00Z",` +
		`"timezone":"Asia/Ho_Chi_Minh","duration_minutes":60,"step_minutes":30,` +
		`"max_candidates":10,"required":[{"kind":"internal_user","id":"` +
		requiredID.String() + `"}],"optional":[{"kind":"external_guest","id":"` +
		externalID.String() + `"}]}`
	request := calendarSchedulingMutationRequest(
		http.MethodPost,
		calendarAvailabilityQueryPath,
		body,
		tenantID,
	)
	response := httptest.NewRecorder()
	handlers.availabilityQueryHandler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("query availability status=%d body=%s", response.Code, response.Body.String())
	}
	assertCalendarNoStoreHeaders(t, response)
	if service.queryCalls != 1 || service.queryScope.TenantID != tenantID ||
		service.queryScope.ActorID != actorID || service.queryInput.ClassID != classID.String() ||
		service.queryInput.DurationMinutes != 60 || service.queryInput.StepMinutes != 30 ||
		len(service.queryInput.Required) != 1 || len(service.queryInput.Optional) != 1 {
		t.Fatalf("unexpected availability query: scope=%+v input=%+v", service.queryScope, service.queryInput)
	}
	var result calendar.AvailabilityResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode availability: %v", err)
	}
	if len(result.Participants) != 2 ||
		result.Participants[1].Intervals[0].Status != "unknown" ||
		result.Participants[1].WorkingIntervals == nil ||
		result.Suggestions == nil || result.EmptySuggestionsReason == nil ||
		*result.EmptySuggestionsReason != emptyReason {
		t.Fatalf("unexpected privacy-filtered availability: %+v", result)
	}
	for _, forbidden := range []string{"email", "title", "description", "location"} {
		if strings.Contains(strings.ToLower(response.Body.String()), `"`+forbidden+`"`) {
			t.Fatalf("availability response leaked %s: %s", forbidden, response.Body.String())
		}
	}
}

func TestCalendarSchedulingRequiresSessionTenantAssertionAndMutationCSRF(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	service := &fakeCalendarSchedulingService{}
	handlers := calendarSchedulingTestHandlers(
		classIdentityService(tenantID, uuid.New(), nil),
		service,
	)

	missingSession := httptest.NewRequest(http.MethodGet, calendarWorkingSchedulePath, nil)
	missingSession.Header.Set(calendarTenantHeader, tenantID.String())
	missingSessionResponse := httptest.NewRecorder()
	handlers.workingScheduleHandler().ServeHTTP(missingSessionResponse, missingSession)
	if missingSessionResponse.Code != http.StatusUnauthorized {
		t.Fatalf("missing session status=%d body=%s", missingSessionResponse.Code, missingSessionResponse.Body.String())
	}

	missingTenant := httptest.NewRequest(http.MethodGet, calendarWorkingSchedulePath, nil)
	addSessionCookie(missingTenant)
	missingTenantResponse := httptest.NewRecorder()
	handlers.workingScheduleHandler().ServeHTTP(missingTenantResponse, missingTenant)
	assertCalendarProblem(t, missingTenantResponse, http.StatusBadRequest, "calendar_scheduling_invalid")

	staleTenant := httptest.NewRequest(http.MethodGet, calendarWorkingSchedulePath, nil)
	addSessionCookie(staleTenant)
	staleTenant.Header.Set(calendarTenantHeader, uuid.NewString())
	staleTenantResponse := httptest.NewRecorder()
	handlers.workingScheduleHandler().ServeHTTP(staleTenantResponse, staleTenant)
	assertCalendarProblem(t, staleTenantResponse, http.StatusConflict, "calendar_scope_changed")

	missingCSRF := httptest.NewRequest(
		http.MethodPut,
		calendarWorkingSchedulePath,
		strings.NewReader(`{"timezone":"UTC","weekly_intervals":[],"exceptions":[],"expected_version":0}`),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(calendarTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingCSRFResponse := httptest.NewRecorder()
	handlers.workingScheduleHandler().ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", missingCSRFResponse.Code, missingCSRFResponse.Body.String())
	}
	if service.getCalls != 0 || service.putCalls != 0 || service.queryCalls != 0 {
		t.Fatalf("invalid boundaries reached scheduling service: %+v", service)
	}
}

func TestCalendarSchedulingValidationAndStableErrorMapping(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	identityService := classIdentityService(tenantID, uuid.New(), nil)

	invalidService := &fakeCalendarSchedulingService{}
	invalidHandlers := calendarSchedulingTestHandlers(identityService, invalidService)
	invalidRequest := calendarSchedulingMutationRequest(
		http.MethodPost,
		calendarAvailabilityQueryPath,
		`{"class_id":"not-a-uuid","unexpected":true}`,
		tenantID,
	)
	invalidResponse := httptest.NewRecorder()
	invalidHandlers.availabilityQueryHandler().ServeHTTP(invalidResponse, invalidRequest)
	assertCalendarProblem(t, invalidResponse, http.StatusBadRequest, "calendar_scheduling_invalid")
	if invalidService.queryCalls != 0 {
		t.Fatalf("invalid query reached service: %d", invalidService.queryCalls)
	}

	classID, participantID := uuid.New(), uuid.New()
	availabilityBody := `{"class_id":"` + classID.String() + `",` +
		`"from":"2026-07-29T00:00:00Z","to":"2026-07-30T00:00:00Z",` +
		`"timezone":"UTC","duration_minutes":60,"step_minutes":30,"max_candidates":10,` +
		`"required":[{"kind":"internal_user","id":"` + participantID.String() + `"}],` +
		`"optional":[]}`
	workingBody := `{"timezone":"UTC","weekly_intervals":[],"exceptions":[],"expected_version":1}`
	tests := []struct {
		name       string
		err        error
		endpoint   string
		wantStatus int
		wantCode   string
	}{
		{name: "forbidden", err: calendar.ErrAccessDenied, endpoint: "get", wantStatus: http.StatusForbidden, wantCode: "calendar_scheduling_forbidden"},
		{name: "not found", err: calendar.ErrSchedulingNotFound, endpoint: "query", wantStatus: http.StatusNotFound, wantCode: "calendar_scheduling_not_found"},
		{name: "stale schedule", err: calendar.ErrWorkingScheduleStale, endpoint: "put", wantStatus: http.StatusConflict, wantCode: "calendar_working_schedule_conflict"},
		{name: "candidate capacity", err: calendar.ErrAvailabilityCapacity, endpoint: "query", wantStatus: http.StatusTooManyRequests, wantCode: "calendar_availability_capacity_exceeded"},
		{name: "unavailable", err: calendar.ErrSchedulingUnavailable, endpoint: "query", wantStatus: http.StatusServiceUnavailable, wantCode: "calendar_scheduling_unavailable"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeCalendarSchedulingService{}
			switch test.endpoint {
			case "get":
				service.getError = test.err
			case "put":
				service.putError = test.err
			case "query":
				service.queryError = test.err
			}
			handlers := calendarSchedulingTestHandlers(identityService, service)
			var request *http.Request
			var handler http.Handler
			switch test.endpoint {
			case "get":
				request = httptest.NewRequest(http.MethodGet, calendarWorkingSchedulePath, nil)
				request.Header.Set(calendarTenantHeader, tenantID.String())
				addSessionCookie(request)
				handler = handlers.workingScheduleHandler()
			case "put":
				request = calendarSchedulingMutationRequest(
					http.MethodPut, calendarWorkingSchedulePath, workingBody, tenantID,
				)
				handler = handlers.workingScheduleHandler()
			default:
				request = calendarSchedulingMutationRequest(
					http.MethodPost, calendarAvailabilityQueryPath, availabilityBody, tenantID,
				)
				handler = handlers.availabilityQueryHandler()
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			assertCalendarProblem(t, response, test.wantStatus, test.wantCode)
			assertCalendarSchedulingProblemHeaders(t, response)
		})
	}
}

func TestCalendarSchedulingRejectsOversizedAvailabilityBody(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	service := &fakeCalendarSchedulingService{}
	handlers := calendarSchedulingTestHandlers(
		classIdentityService(tenantID, uuid.New(), nil),
		service,
	)
	body := `{"padding":"` + strings.Repeat("x", maximumAvailabilityQueryBodySize) + `"}`
	request := calendarSchedulingMutationRequest(
		http.MethodPost,
		calendarAvailabilityQueryPath,
		body,
		tenantID,
	)
	response := httptest.NewRecorder()
	handlers.availabilityQueryHandler().ServeHTTP(response, request)
	assertCalendarProblem(t, response, http.StatusBadRequest, "calendar_scheduling_invalid")
	if service.queryCalls != 0 {
		t.Fatalf("oversized query reached service: %d", service.queryCalls)
	}
}

type fakeCalendarSchedulingService struct {
	getResult calendar.WorkingSchedule
	getError  error
	getCalls  int
	getScope  tenancy.Context

	putResult calendar.WorkingSchedule
	putError  error
	putCalls  int
	putScope  tenancy.Context
	putInput  calendar.PutWorkingScheduleInput

	queryResult calendar.AvailabilityResult
	queryError  error
	queryCalls  int
	queryScope  tenancy.Context
	queryInput  calendar.AvailabilityQueryInput
}

func (service *fakeCalendarSchedulingService) GetWorkingSchedule(
	_ context.Context,
	scope tenancy.Context,
) (calendar.WorkingSchedule, error) {
	service.getCalls++
	service.getScope = scope
	return service.getResult, service.getError
}

func (service *fakeCalendarSchedulingService) PutWorkingSchedule(
	_ context.Context,
	scope tenancy.Context,
	input calendar.PutWorkingScheduleInput,
) (calendar.WorkingSchedule, error) {
	service.putCalls++
	service.putScope = scope
	service.putInput = input
	return service.putResult, service.putError
}

func (service *fakeCalendarSchedulingService) QueryAvailability(
	_ context.Context,
	scope tenancy.Context,
	input calendar.AvailabilityQueryInput,
) (calendar.AvailabilityResult, error) {
	service.queryCalls++
	service.queryScope = scope
	service.queryInput = input
	return service.queryResult, service.queryError
}

func calendarSchedulingTestHandlers(
	identityService identity.ServiceAPI,
	service calendar.SchedulingServiceAPI,
) calendarSchedulingHandlers {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		Environment: "test",
		Port:        "8080",
		WebOrigin:   "http://localhost:5173",
		Authentication: config.AuthenticationConfig{
			SessionTTL: 8 * time.Hour,
		},
	}
	auth := newAuthHandlers(cfg, logger, identityService, func() time.Time {
		return calendarTestTime
	})
	return newCalendarSchedulingHandlers(logger, auth, service)
}

func calendarSchedulingMutationRequest(
	method string,
	path string,
	body string,
	tenantID uuid.UUID,
) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(csrfHeader, "csrf-token")
	request.Header.Set(calendarTenantHeader, tenantID.String())
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	return request
}

func assertCalendarSchedulingProblemHeaders(
	t *testing.T,
	response *httptest.ResponseRecorder,
) {
	t.Helper()
	if got := response.Header().Get("Cache-Control"); got != "no-store" && got != "private, no-store" {
		t.Fatalf("calendar scheduling problem cache control=%q", got)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("calendar scheduling problem referrer policy=%q", got)
	}
}
