package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var calendarTestTime = time.Date(2026, time.July, 26, 3, 30, 0, 0, time.UTC)

func TestCalendarListUsesAuthenticatedTenantAndReturnsAggregateProjection(t *testing.T) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	classID, sessionID := uuid.New(), uuid.New()
	service := &fakeCalendarService{page: calendar.Page{
		Items: []calendar.Item{{
			ID: "class_session:" + sessionID.String(), SourceType: calendar.SourceClassSession,
			SourceID: sessionID, OccurrenceKey: sessionID.String(), Title: "Security review",
			StartsAt: calendarTestTime, EndsAt: calendarTestTime.Add(time.Hour),
			DisplayTimezone: "Asia/Ho_Chi_Minh", ClassID: classID, ClassTitle: "Security Lab",
			Status: "scheduled", ColorToken: "class_session", Version: 2,
			ViewerCapabilities: calendar.ViewerCapabilities{
				CanView: true, CanEdit: true, CanCancel: true, CanReschedule: true,
			},
		}},
		NextCursor: "next-calendar-page",
	}}
	handler := calendarTestHandler(classIdentityService(tenantID, actorID, nil), service)
	request := httptest.NewRequest(
		http.MethodGet,
		calendarItemsPath+"?from=2026-07-26T00:00:00Z&to=2026-07-27T00:00:00Z"+
			"&viewer_timezone=Asia%2FHo_Chi_Minh&types=class_session"+
			"&class_ids="+classID.String()+"&statuses=scheduled&search=security&limit=17",
		nil,
	)
	addSessionCookie(request)
	request.Header.Set(calendarTenantHeader, tenantID.String())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("list calendar status=%d body=%s", response.Code, response.Body.String())
	}
	assertCalendarNoStoreHeaders(t, response)
	if service.listCalls != 1 || service.listScope.TenantID != tenantID ||
		service.listScope.ActorID != actorID || service.listInput.Limit != 17 ||
		service.listInput.ViewerTimezone != "Asia/Ho_Chi_Minh" ||
		len(service.listInput.ClassIDs) != 1 || service.listInput.ClassIDs[0] != classID.String() ||
		len(service.listInput.Statuses) != 1 || service.listInput.Statuses[0] != "scheduled" {
		t.Fatalf("unexpected calendar list call: %+v", service)
	}
	var body calendarPageResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode calendar page: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].SourceID != sessionID ||
		body.NextCursor == nil || *body.NextCursor != "next-calendar-page" ||
		!body.Items[0].ViewerCapabilities.CanReschedule {
		t.Fatalf("unexpected calendar page: %+v", body)
	}
}

func TestCalendarRejectsMissingOrStaleExpectedTenant(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	service := &fakeCalendarService{}
	handler := calendarTestHandler(classIdentityService(tenantID, uuid.New(), nil), service)
	path := calendarItemsPath +
		"?from=2026-07-26T00:00:00Z&to=2026-07-27T00:00:00Z&viewer_timezone=UTC"

	missing := httptest.NewRequest(http.MethodGet, path, nil)
	addSessionCookie(missing)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	assertCalendarProblem(t, missingResponse, http.StatusBadRequest, "calendar_invalid")

	stale := httptest.NewRequest(http.MethodGet, path, nil)
	addSessionCookie(stale)
	stale.Header.Set(calendarTenantHeader, uuid.NewString())
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	assertCalendarProblem(t, staleResponse, http.StatusConflict, "calendar_scope_changed")
	if service.listCalls != 0 {
		t.Fatalf("invalid tenant assertions reached calendar service: %d", service.listCalls)
	}
}

func TestCalendarPreferenceGetAndFullReplacementPut(t *testing.T) {
	t.Parallel()
	tenantID, actorID := uuid.New(), uuid.New()
	secondary := "UTC"
	service := &fakeCalendarService{
		preference: calendar.DisplayPreference{
			ViewerTimezone: "Asia/Ho_Chi_Minh", Locale: "vi-VN", TimeFormat: "24h",
			WeekStart: "monday", DefaultView: "week", Density: "comfortable",
			TimeScaleMinutes: 30, Version: 3, UpdatedAt: calendarTestTime,
		},
		updatePreferenceResult: calendar.DisplayPreference{
			ViewerTimezone: "Asia/Ho_Chi_Minh", Locale: "en-US", TimeFormat: "12h",
			WeekStart: "sunday", DefaultView: "work_week", Density: "compact",
			TimeScaleMinutes: 15, SecondaryTimezone: &secondary, Version: 4,
			UpdatedAt: calendarTestTime.Add(time.Minute),
		},
	}
	handler := calendarTestHandler(classIdentityService(tenantID, actorID, nil), service)

	getRequest := httptest.NewRequest(http.MethodGet, calendarPreferencePath, nil)
	addSessionCookie(getRequest)
	getRequest.Header.Set(calendarTenantHeader, tenantID.String())
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get calendar preference status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	if service.getPreferenceCalls != 1 || service.getPreferenceScope.ActorID != actorID {
		t.Fatalf("unexpected calendar preference get call: %+v", service)
	}

	body := `{"viewer_timezone":"Asia/Ho_Chi_Minh","locale":"en-US",` +
		`"time_format":"12h","week_start":"sunday","default_view":"work_week",` +
		`"density":"compact","time_scale_minutes":15,"secondary_timezone":"UTC",` +
		`"expected_version":3}`
	putRequest := calendarMutationRequest(http.MethodPut, calendarPreferencePath, body, tenantID)
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put calendar preference status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	assertCalendarNoStoreHeaders(t, putResponse)
	input := service.updatePreferenceInput
	if service.updatePreferenceCalls != 1 || service.updatePreferenceScope.TenantID != tenantID ||
		service.updatePreferenceScope.ActorID != actorID || input.Locale != "en-US" ||
		input.TimeFormat != "12h" || input.WeekStart != "sunday" ||
		input.DefaultView != "work_week" || input.Density != "compact" ||
		input.TimeScaleMinutes != 15 || input.SecondaryTimezone == nil ||
		*input.SecondaryTimezone != "UTC" || input.ExpectedVersion != 3 {
		t.Fatalf("PUT must be a scoped full replacement: %+v", service)
	}
	var result calendar.DisplayPreference
	if err := json.Unmarshal(putResponse.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode updated calendar preference: %v", err)
	}
	if result.Version != 4 || result.SecondaryTimezone == nil || *result.SecondaryTimezone != "UTC" {
		t.Fatalf("unexpected updated calendar preference: %+v", result)
	}
}

func TestCalendarPreferencePutRequiresCSRFAndMapsCASConflict(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	service := &fakeCalendarService{}
	handler := calendarTestHandler(classIdentityService(tenantID, uuid.New(), nil), service)
	body := `{"viewer_timezone":"UTC","locale":"vi-VN","time_format":"24h",` +
		`"week_start":"monday","default_view":"week","density":"comfortable",` +
		`"time_scale_minutes":30,"secondary_timezone":null,"expected_version":1}`

	missingCSRF := httptest.NewRequest(http.MethodPut, calendarPreferencePath, strings.NewReader(body))
	missingCSRF.Header.Set("Content-Type", "application/json")
	missingCSRF.Header.Set(calendarTenantHeader, tenantID.String())
	addSessionCookie(missingCSRF)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden || service.updatePreferenceCalls != 0 {
		t.Fatalf("missing CSRF status=%d calls=%d body=%s",
			missingResponse.Code, service.updatePreferenceCalls, missingResponse.Body.String())
	}

	service.updatePreferenceError = calendar.ErrConflict
	conflictRequest := calendarMutationRequest(http.MethodPut, calendarPreferencePath, body, tenantID)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	assertCalendarProblem(
		t, conflictResponse, http.StatusConflict, "calendar_preference_conflict",
	)
}

func TestParseCalendarListInputSupportsRepeatedAndCommaSeparatedFilters(t *testing.T) {
	t.Parallel()
	query := url.Values{
		"from":            {"2026-08-01T00:00:00Z"},
		"to":              {"2026-09-01T00:00:00Z"},
		"viewer_timezone": {"Asia/Ho_Chi_Minh"},
		"types":           {"class_session"},
		"class_ids": {
			"10000000-0000-4000-8000-000000000001,20000000-0000-4000-8000-000000000002",
		},
		"statuses": {"scheduled", "cancelled,ended"},
		"search":   {" toan "},
		"limit":    {"500"},
	}
	input, err := parseCalendarListInput(query)
	if err != nil {
		t.Fatal(err)
	}
	if input.Limit != 500 || input.Search != "toan" || len(input.ClassIDs) != 2 ||
		len(input.Statuses) != 3 || input.ViewerTimezone != "Asia/Ho_Chi_Minh" {
		t.Fatalf("unexpected parsed input: %+v", input)
	}
}

func TestParseCalendarListInputRequiresViewerTimezone(t *testing.T) {
	t.Parallel()
	_, err := parseCalendarListInput(url.Values{
		"from": {"2026-08-01T00:00:00Z"},
		"to":   {"2026-09-01T00:00:00Z"},
	})
	if err == nil {
		t.Fatal("expected missing viewer timezone to fail")
	}
}

func TestCalendarResponseHeadersPreventSharedCaching(t *testing.T) {
	t.Parallel()
	handler := calendarResponseHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, calendarItemsPath, nil))
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("unexpected cache control: %q", got)
	}
	vary := recorder.Header().Values("Vary")
	if len(vary) != 2 || vary[0] != "Cookie" || vary[1] != calendarTenantHeader {
		t.Fatalf("unexpected Vary headers: %v", vary)
	}
}

type fakeCalendarService struct {
	page      calendar.Page
	listError error
	listCalls int
	listScope tenancy.Context
	listInput calendar.ListInput

	preference         calendar.DisplayPreference
	getPreferenceError error
	getPreferenceCalls int
	getPreferenceScope tenancy.Context

	updatePreferenceResult calendar.DisplayPreference
	updatePreferenceError  error
	updatePreferenceCalls  int
	updatePreferenceScope  tenancy.Context
	updatePreferenceInput  calendar.UpdatePreferenceInput
}

func (service *fakeCalendarService) ListItems(
	_ context.Context,
	scope tenancy.Context,
	input calendar.ListInput,
) (calendar.Page, error) {
	service.listCalls++
	service.listScope = scope
	service.listInput = input
	return service.page, service.listError
}

func (service *fakeCalendarService) GetPreference(
	_ context.Context,
	scope tenancy.Context,
) (calendar.DisplayPreference, error) {
	service.getPreferenceCalls++
	service.getPreferenceScope = scope
	return service.preference, service.getPreferenceError
}

func (service *fakeCalendarService) UpdatePreference(
	_ context.Context,
	scope tenancy.Context,
	input calendar.UpdatePreferenceInput,
) (calendar.DisplayPreference, error) {
	service.updatePreferenceCalls++
	service.updatePreferenceScope = scope
	service.updatePreferenceInput = input
	return service.updatePreferenceResult, service.updatePreferenceError
}

func calendarTestHandler(identityService identity.ServiceAPI, service calendar.ServiceAPI) http.Handler {
	return NewHandlerWithOptions(
		config.Config{
			Environment: "test", Port: "8080", WebOrigin: "http://localhost:5173",
			Authentication: config.AuthenticationConfig{SessionTTL: 8 * time.Hour},
		},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		Options{Clock: func() time.Time { return calendarTestTime }, Identity: identityService, Calendar: service},
	)
}

func calendarMutationRequest(
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

func assertCalendarProblem(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
	wantCode string,
) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("calendar problem status=%d want=%d body=%s",
			response.Code, wantStatus, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode calendar problem: %v", err)
	}
	if body["code"] != wantCode {
		t.Fatalf("calendar problem code=%v want=%s body=%#v", body["code"], wantCode, body)
	}
}

func assertCalendarNoStoreHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("calendar cache control=%q", got)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("calendar referrer policy=%q", got)
	}
}
