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
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/notification"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var notificationTestTime = time.Date(2026, time.July, 25, 4, 30, 0, 0, time.UTC)

func TestNotificationHandlersListUsesActiveUserScopeAndStableResponse(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	notificationID := uuid.New()
	resourceID := uuid.New()
	readAt := notificationTestTime.Add(time.Minute)
	service := &fakeNotificationService{
		page: notification.Page{
			Items: []notification.Notification{{
				ID:                  notificationID,
				TenantID:            tenantID,
				RecipientUserID:     userID,
				SourceOutboxEventID: uuid.New(),
				EffectKey:           "class_session_42",
				Kind:                "class.session_scheduled",
				TemplateKey:         "class_session.scheduled",
				ResourceType:        "class_session",
				ResourceID:          &resourceID,
				Context:             json.RawMessage(`{"class_name":"Security Lab"}`),
				OccurredAt:          notificationTestTime,
				ReadAt:              &readAt,
				CreatedAt:           notificationTestTime.Add(time.Second),
			}},
			NextCursor: "next-page-token",
		},
		unreadCount: notification.UnreadCount{Count: 1000, IsCapped: true},
	}
	handler := notificationTestHandler(
		classIdentityService(tenantID, userID, nil),
		service,
	)

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/notifications?limit=17&cursor=current-page-token&unread_only=true",
		nil,
	)
	addSessionCookie(request)
	request.Header.Set(notificationTenantHeader, tenantID.String())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
	}
	assertNotificationNoStoreHeaders(t, response)
	if service.listCalls != 1 || service.listScope.TenantID != tenantID ||
		service.listScope.ActorID != userID || service.listInput.Limit != 17 ||
		service.listInput.Cursor != "current-page-token" || !service.listInput.UnreadOnly {
		t.Fatalf("unexpected list call: %+v", service)
	}

	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected items: %#v", body["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected item: %#v", items[0])
	}
	if item["id"] != notificationID.String() ||
		item["effect_key"] != "class_session_42" ||
		item["template_key"] != "class_session.scheduled" ||
		item["resource_type"] != "class_session" ||
		item["resource_id"] != resourceID.String() ||
		body["next_cursor"] != "next-page-token" {
		t.Fatalf("unexpected response body: %#v", body)
	}
	contextValues, ok := item["context"].(map[string]any)
	if !ok || contextValues["class_name"] != "Security Lab" {
		t.Fatalf("unexpected notification context: %#v", item["context"])
	}
	for _, privateField := range []string{
		"tenant_id", "recipient_user_id", "source_outbox_event_id", "kind",
	} {
		if _, exposed := item[privateField]; exposed {
			t.Fatalf("private field %q leaked in response: %#v", privateField, item)
		}
	}

	unreadRequest := httptest.NewRequest(http.MethodGet, notificationUnreadCountPath, nil)
	addSessionCookie(unreadRequest)
	unreadRequest.Header.Set(notificationTenantHeader, tenantID.String())
	unreadResponse := httptest.NewRecorder()
	handler.ServeHTTP(unreadResponse, unreadRequest)
	if unreadResponse.Code != http.StatusOK {
		t.Fatalf("unread status=%d body=%s", unreadResponse.Code, unreadResponse.Body.String())
	}
	assertNotificationNoStoreHeaders(t, unreadResponse)
	if service.unreadCalls != 1 || service.unreadScope.TenantID != tenantID ||
		service.unreadScope.ActorID != userID {
		t.Fatalf("unexpected unread call: %+v", service)
	}
	var unreadBody notification.UnreadCount
	if err := json.Unmarshal(unreadResponse.Body.Bytes(), &unreadBody); err != nil {
		t.Fatalf("decode unread response: %v", err)
	}
	if unreadBody.Count != 1000 || !unreadBody.IsCapped {
		t.Fatalf("unexpected unread response: %+v", unreadBody)
	}
}

func TestNotificationHandlersDenyMissingActiveTenant(t *testing.T) {
	t.Parallel()

	identityService := classIdentityService(uuid.New(), uuid.New(), nil)
	identityService.principal.ActiveTenant = nil
	service := &fakeNotificationService{}
	handler := notificationTestHandler(identityService, service)
	request := httptest.NewRequest(http.MethodGet, notificationsCollectionPath, nil)
	addSessionCookie(request)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	assertNotificationProblem(t, response, http.StatusForbidden, "notification_forbidden")
	if service.listCalls != 0 {
		t.Fatalf("service must not run without an active tenant: calls=%d", service.listCalls)
	}
}

func TestNotificationHandlersRejectMissingOrStaleTenantAssertion(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	service := &fakeNotificationService{}
	handler := notificationTestHandler(
		classIdentityService(tenantID, uuid.New(), nil),
		service,
	)

	missing := httptest.NewRequest(http.MethodGet, notificationsCollectionPath, nil)
	addSessionCookie(missing)
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missing)
	assertNotificationProblem(
		t, missingResponse, http.StatusBadRequest, "notification_invalid",
	)

	stale := httptest.NewRequest(http.MethodGet, notificationsCollectionPath, nil)
	addSessionCookie(stale)
	stale.Header.Set(notificationTenantHeader, uuid.NewString())
	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, stale)
	assertNotificationProblem(
		t, staleResponse, http.StatusConflict, "notification_scope_changed",
	)

	if service.listCalls != 0 {
		t.Fatalf("scope assertion failures must not reach service: calls=%d", service.listCalls)
	}
}

func TestNotificationHandlersRequireCSRFAndScopeReadMutations(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	notificationID := uuid.New()
	service := &fakeNotificationService{
		markReadResult: notification.Notification{
			ID: notificationID, TenantID: tenantID, RecipientUserID: userID,
			EffectKey: "read-effect", Kind: "class.session_scheduled",
			TemplateKey: "class_session.scheduled", Context: json.RawMessage(`{}`),
			OccurredAt: notificationTestTime, CreatedAt: notificationTestTime,
		},
		markAllResult: notification.MarkAllResult{UpdatedCount: 7},
	}
	handler := notificationTestHandler(
		classIdentityService(tenantID, userID, nil),
		service,
	)

	missingCSRF := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/notifications/"+notificationID.String()+"/read",
		nil,
	)
	addSessionCookie(missingCSRF)
	missingCSRF.Header.Set(notificationTenantHeader, tenantID.String())
	missingCSRFResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingCSRFResponse, missingCSRF)
	if missingCSRFResponse.Code != http.StatusForbidden || service.markReadCalls != 0 {
		t.Fatalf("missing CSRF: status=%d calls=%d body=%s",
			missingCSRFResponse.Code, service.markReadCalls, missingCSRFResponse.Body.String())
	}

	markReadRequest := notificationMutationRequest(
		http.MethodPost,
		"/api/v1/notifications/"+notificationID.String()+"/read",
		"",
		tenantID,
	)
	markReadResponse := httptest.NewRecorder()
	handler.ServeHTTP(markReadResponse, markReadRequest)
	if markReadResponse.Code != http.StatusOK {
		t.Fatalf("mark read status=%d body=%s", markReadResponse.Code, markReadResponse.Body.String())
	}
	assertNotificationNoStoreHeaders(t, markReadResponse)
	if service.markReadCalls != 1 || service.markReadID != notificationID ||
		service.markReadScope.TenantID != tenantID || service.markReadScope.ActorID != userID {
		t.Fatalf("unexpected mark-read call: %+v", service)
	}

	markAllRequest := notificationMutationRequest(
		http.MethodPost, notificationReadAllPath, "", tenantID,
	)
	markAllResponse := httptest.NewRecorder()
	handler.ServeHTTP(markAllResponse, markAllRequest)
	if markAllResponse.Code != http.StatusOK {
		t.Fatalf("mark-all status=%d body=%s", markAllResponse.Code, markAllResponse.Body.String())
	}
	var markAllBody notification.MarkAllResult
	if err := json.Unmarshal(markAllResponse.Body.Bytes(), &markAllBody); err != nil {
		t.Fatalf("decode mark-all response: %v", err)
	}
	if markAllBody.UpdatedCount != 7 || service.markAllCalls != 1 ||
		service.markAllScope.TenantID != tenantID || service.markAllScope.ActorID != userID {
		t.Fatalf("unexpected mark-all response/call: body=%+v service=%+v", markAllBody, service)
	}
}

func TestNotificationHandlersMapForeignOrMissingNotificationToNotFound(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	service := &fakeNotificationService{markReadError: notification.ErrNotFound}
	handler := notificationTestHandler(
		classIdentityService(tenantID, userID, nil),
		service,
	)

	request := notificationMutationRequest(
		http.MethodPost,
		"/api/v1/notifications/"+uuid.NewString()+"/read",
		"",
		tenantID,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assertNotificationProblem(t, response, http.StatusNotFound, "notification_not_found")

	invalidIDRequest := notificationMutationRequest(
		http.MethodPost,
		"/api/v1/notifications/not-a-uuid/read",
		"",
		tenantID,
	)
	invalidIDResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidIDResponse, invalidIDRequest)
	assertNotificationProblem(t, invalidIDResponse, http.StatusNotFound, "notification_not_found")
	if service.markReadCalls != 1 {
		t.Fatalf("invalid IDs must not reach the service: calls=%d", service.markReadCalls)
	}
}

func TestNotificationPreferenceGetAndFullReplacementPut(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	start := "22:30"
	end := "06:45"
	service := &fakeNotificationService{
		preference: notification.Preference{
			TenantID: tenantID, UserID: userID, InAppEnabled: true, EmailEnabled: false,
			ReminderOffsetMinutes: 15, QuietHoursEnabled: false,
			QuietHoursTimezone: "Asia/Ho_Chi_Minh", Version: 3,
			UpdatedAt: notificationTestTime,
		},
		putPreferenceResult: notification.Preference{
			TenantID: tenantID, UserID: userID, InAppEnabled: true, EmailEnabled: true,
			ReminderOffsetMinutes: 45, QuietHoursEnabled: true,
			QuietHoursStart: &start, QuietHoursEnd: &end,
			QuietHoursTimezone: "Asia/Ho_Chi_Minh", Version: 4,
			UpdatedAt: notificationTestTime.Add(time.Minute),
		},
	}
	handler := notificationTestHandler(
		classIdentityService(tenantID, userID, nil),
		service,
	)

	getRequest := httptest.NewRequest(http.MethodGet, notificationPreferencePath, nil)
	addSessionCookie(getRequest)
	getRequest.Header.Set(notificationTenantHeader, tenantID.String())
	getResponse := httptest.NewRecorder()
	handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("get preference status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	assertNotificationNoStoreHeaders(t, getResponse)
	var getBody notification.Preference
	if err := json.Unmarshal(getResponse.Body.Bytes(), &getBody); err != nil {
		t.Fatalf("decode preference response: %v", err)
	}
	if getBody.Version != 3 || getBody.QuietHoursTimezone != "Asia/Ho_Chi_Minh" ||
		service.getPreferenceScope.TenantID != tenantID ||
		service.getPreferenceScope.ActorID != userID {
		t.Fatalf("unexpected preference response/call: body=%+v service=%+v", getBody, service)
	}

	putBody := `{"in_app_enabled":true,"email_enabled":true,"reminder_offset_minutes":45,` +
		`"quiet_hours_enabled":true,"quiet_hours_start":"22:30","quiet_hours_end":"06:45",` +
		`"quiet_hours_timezone":"Asia/Ho_Chi_Minh","expected_version":3}`
	putRequest := notificationMutationRequest(
		http.MethodPut, notificationPreferencePath, putBody, tenantID,
	)
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, putRequest)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put preference status=%d body=%s", putResponse.Code, putResponse.Body.String())
	}
	assertNotificationNoStoreHeaders(t, putResponse)
	if service.putPreferenceCalls != 1 ||
		service.putPreferenceScope.TenantID != tenantID ||
		service.putPreferenceScope.ActorID != userID ||
		!service.putPreferenceInput.InAppEnabled || !service.putPreferenceInput.EmailEnabled ||
		service.putPreferenceInput.ReminderOffsetMinutes != 45 ||
		!service.putPreferenceInput.QuietHoursEnabled ||
		service.putPreferenceInput.QuietHoursStart == nil ||
		*service.putPreferenceInput.QuietHoursStart != start ||
		service.putPreferenceInput.QuietHoursEnd == nil ||
		*service.putPreferenceInput.QuietHoursEnd != end ||
		service.putPreferenceInput.QuietHoursTimezone != "Asia/Ho_Chi_Minh" ||
		service.putPreferenceInput.ExpectedVersion != 3 {
		t.Fatalf("PUT must send a full versioned replacement: %+v", service)
	}
	var putResponseBody notification.Preference
	if err := json.Unmarshal(putResponse.Body.Bytes(), &putResponseBody); err != nil {
		t.Fatalf("decode put response: %v", err)
	}
	if putResponseBody.Version != 4 || !putResponseBody.EmailEnabled {
		t.Fatalf("unexpected put response: %+v", putResponseBody)
	}
}

func TestNotificationPreferenceRequiresCSRFAndMapsConflict(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	validBody := `{"in_app_enabled":true,"email_enabled":false,"reminder_offset_minutes":15,` +
		`"quiet_hours_enabled":false,"quiet_hours_start":null,"quiet_hours_end":null,` +
		`"quiet_hours_timezone":"Asia/Ho_Chi_Minh","expected_version":2}`
	service := &fakeNotificationService{}
	handler := notificationTestHandler(
		classIdentityService(tenantID, userID, nil),
		service,
	)

	missingCSRF := httptest.NewRequest(
		http.MethodPut,
		notificationPreferencePath,
		strings.NewReader(validBody),
	)
	missingCSRF.Header.Set("Content-Type", "application/json")
	addSessionCookie(missingCSRF)
	missingCSRF.Header.Set(notificationTenantHeader, tenantID.String())
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingCSRF)
	if missingResponse.Code != http.StatusForbidden || service.putPreferenceCalls != 0 {
		t.Fatalf("missing CSRF: status=%d calls=%d body=%s",
			missingResponse.Code, service.putPreferenceCalls, missingResponse.Body.String())
	}

	service.putPreferenceError = notification.ErrConflict
	conflictRequest := notificationMutationRequest(
		http.MethodPut, notificationPreferencePath, validBody, tenantID,
	)
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	assertNotificationProblem(
		t,
		conflictResponse,
		http.StatusConflict,
		"notification_preference_conflict",
	)
}

func TestNotificationHandlersMapFeatureDisabledAndUnavailable(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	identityService := classIdentityService(tenantID, userID, nil)

	service := &fakeNotificationService{listError: &featurecontrol.FeatureDisabledError{
		Feature: featurecontrol.FeatureInAppNotifications,
	}}
	disabledHandler := notificationTestHandler(identityService, service)
	disabledRequest := httptest.NewRequest(http.MethodGet, notificationsCollectionPath, nil)
	addSessionCookie(disabledRequest)
	disabledRequest.Header.Set(notificationTenantHeader, tenantID.String())
	disabledResponse := httptest.NewRecorder()
	disabledHandler.ServeHTTP(disabledResponse, disabledRequest)
	assertNotificationProblem(t, disabledResponse, http.StatusForbidden, "feature_disabled")

	unavailableHandler := notificationTestHandler(identityService, nil)
	unavailableRequest := httptest.NewRequest(http.MethodGet, notificationsCollectionPath, nil)
	addSessionCookie(unavailableRequest)
	unavailableResponse := httptest.NewRecorder()
	unavailableHandler.ServeHTTP(unavailableResponse, unavailableRequest)
	assertNotificationProblem(
		t,
		unavailableResponse,
		http.StatusServiceUnavailable,
		"notification_unavailable",
	)
}

func TestNotificationHandlersRejectInvalidQueriesAndBodies(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	userID := uuid.New()
	service := &fakeNotificationService{}
	handler := notificationTestHandler(
		classIdentityService(tenantID, userID, nil),
		service,
	)

	for _, path := range []string{
		"/api/v1/notifications?limit=not-a-number",
		"/api/v1/notifications?unread_only=sometimes",
		"/api/v1/notifications?unread_only=1",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		addSessionCookie(request)
		request.Header.Set(notificationTenantHeader, tenantID.String())
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assertNotificationProblem(t, response, http.StatusBadRequest, "notification_invalid")
	}
	if service.listCalls != 0 {
		t.Fatalf("invalid queries must not reach service: calls=%d", service.listCalls)
	}

	for _, body := range []string{
		`{"in_app_enabled":true}`,
		`{"in_app_enabled":`,
		`{"in_app_enabled":true,"email_enabled":false,"reminder_offset_minutes":15,` +
			`"quiet_hours_enabled":false,"quiet_hours_timezone":"Asia/Ho_Chi_Minh",` +
			`"expected_version":0,"unknown":true}`,
		`{"in_app_enabled":true,"email_enabled":false,"reminder_offset_minutes":15,` +
			`"quiet_hours_enabled":false,"quiet_hours_timezone":"Asia/Ho_Chi_Minh",` +
			`"expected_version":0}`,
	} {
		request := notificationMutationRequest(
			http.MethodPut, notificationPreferencePath, body, tenantID,
		)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assertNotificationProblem(t, response, http.StatusBadRequest, "notification_invalid")
	}
	if service.putPreferenceCalls != 0 {
		t.Fatalf("invalid bodies must not reach service: calls=%d", service.putPreferenceCalls)
	}
}

type fakeNotificationService struct {
	page      notification.Page
	listError error
	listCalls int
	listScope tenancy.Context
	listInput notification.ListInput

	unreadCount notification.UnreadCount
	unreadError error
	unreadCalls int
	unreadScope tenancy.Context

	markReadResult notification.Notification
	markReadError  error
	markReadCalls  int
	markReadScope  tenancy.Context
	markReadID     uuid.UUID

	markAllResult notification.MarkAllResult
	markAllError  error
	markAllCalls  int
	markAllScope  tenancy.Context

	preference          notification.Preference
	getPreferenceError  error
	getPreferenceCalls  int
	getPreferenceScope  tenancy.Context
	putPreferenceResult notification.Preference
	putPreferenceError  error
	putPreferenceCalls  int
	putPreferenceScope  tenancy.Context
	putPreferenceInput  notification.PutPreferenceInput
}

func (service *fakeNotificationService) List(
	_ context.Context,
	scope tenancy.Context,
	input notification.ListInput,
) (notification.Page, error) {
	service.listCalls++
	service.listScope = scope
	service.listInput = input
	return service.page, service.listError
}

func (service *fakeNotificationService) UnreadCount(
	_ context.Context,
	scope tenancy.Context,
) (notification.UnreadCount, error) {
	service.unreadCalls++
	service.unreadScope = scope
	return service.unreadCount, service.unreadError
}

func (service *fakeNotificationService) MarkRead(
	_ context.Context,
	scope tenancy.Context,
	notificationID uuid.UUID,
) (notification.Notification, error) {
	service.markReadCalls++
	service.markReadScope = scope
	service.markReadID = notificationID
	return service.markReadResult, service.markReadError
}

func (service *fakeNotificationService) MarkAllRead(
	_ context.Context,
	scope tenancy.Context,
) (notification.MarkAllResult, error) {
	service.markAllCalls++
	service.markAllScope = scope
	return service.markAllResult, service.markAllError
}

func (service *fakeNotificationService) GetPreference(
	_ context.Context,
	scope tenancy.Context,
) (notification.Preference, error) {
	service.getPreferenceCalls++
	service.getPreferenceScope = scope
	return service.preference, service.getPreferenceError
}

func (service *fakeNotificationService) PutPreference(
	_ context.Context,
	scope tenancy.Context,
	input notification.PutPreferenceInput,
) (notification.Preference, error) {
	service.putPreferenceCalls++
	service.putPreferenceScope = scope
	service.putPreferenceInput = input
	return service.putPreferenceResult, service.putPreferenceError
}

func notificationTestHandler(
	identityService identity.ServiceAPI,
	service notification.ServiceAPI,
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
			Clock:         func() time.Time { return notificationTestTime },
			Identity:      identityService,
			Notifications: service,
		},
	)
}

func notificationMutationRequest(
	method string,
	path string,
	body string,
	tenantID uuid.UUID,
) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set(csrfHeader, "csrf-token")
	request.Header.Set(notificationTenantHeader, tenantID.String())
	addSessionCookie(request)
	request.AddCookie(&http.Cookie{Name: "tutorhub_csrf", Value: "csrf-token"})
	return request
}

func assertNotificationNoStoreHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		!headerContainsValue(response.Header().Values("Vary"), "Cookie") ||
		!headerContainsValue(response.Header().Values("Vary"), notificationTenantHeader) {
		t.Fatalf("unexpected notification headers: %v", response.Header())
	}
}

func headerContainsValue(values []string, expected string) bool {
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), expected) {
				return true
			}
		}
	}
	return false
}

func assertNotificationProblem(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code string,
) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d: %s", status, response.Code, response.Body.String())
	}
	assertNotificationNoStoreHeaders(t, response)
	var problem Problem
	if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != code {
		t.Fatalf("expected problem code %q, got %+v", code, problem)
	}
}

var _ notification.ServiceAPI = (*fakeNotificationService)(nil)
