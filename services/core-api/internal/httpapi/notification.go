package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/modules/notification"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	notificationsCollectionPath = "/api/v1/notifications"
	notificationUnreadCountPath = "/api/v1/notifications/unread-count"
	notificationReadAllPath     = "/api/v1/notifications/read-all"
	notificationReadPattern     = "/api/v1/notifications/{notification_id}/read"
	notificationPreferencePath  = "/api/v1/notification-preferences"
	notificationTenantHeader    = "X-TutorHub-Expected-Tenant-ID"
	maximumNotificationBodySize = 16 * 1024
)

type notificationHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service notification.ServiceAPI
}

type notificationResponse struct {
	ID           uuid.UUID         `json:"id"`
	EffectKey    string            `json:"effect_key"`
	TemplateKey  string            `json:"template_key"`
	ResourceType string            `json:"resource_type,omitempty"`
	ResourceID   *uuid.UUID        `json:"resource_id,omitempty"`
	Context      map[string]string `json:"context"`
	OccurredAt   time.Time         `json:"occurred_at"`
	ReadAt       *time.Time        `json:"read_at"`
	CreatedAt    time.Time         `json:"created_at"`
}

type notificationPageResponse struct {
	Items      []notificationResponse `json:"items"`
	NextCursor *string                `json:"next_cursor"`
}

type updateNotificationPreferenceRequest struct {
	InAppEnabled          *bool               `json:"in_app_enabled"`
	EmailEnabled          *bool               `json:"email_enabled"`
	ReminderOffsetMinutes *int                `json:"reminder_offset_minutes"`
	QuietHoursEnabled     *bool               `json:"quiet_hours_enabled"`
	QuietHoursStart       nullableStringField `json:"quiet_hours_start"`
	QuietHoursEnd         nullableStringField `json:"quiet_hours_end"`
	QuietHoursTimezone    *string             `json:"quiet_hours_timezone"`
	ExpectedVersion       *int64              `json:"expected_version"`
}

// nullableStringField distinguishes an explicit JSON null from an omitted field.
// PUT uses full-replacement semantics, so both states must remain observable.
type nullableStringField struct {
	present bool
	value   *string
}

func (field *nullableStringField) UnmarshalJSON(data []byte) error {
	field.present = true
	if string(data) == "null" {
		field.value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	field.value = &value
	return nil
}

func newNotificationHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service notification.ServiceAPI,
) notificationHandlers {
	return notificationHandlers{logger: logger, auth: auth, service: service}
}

func notificationResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", notificationTenantHeader)
		next.ServeHTTP(w, r)
	})
}

func (handlers notificationHandlers) list(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	input, err := parseNotificationListInput(r)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	page, err := handlers.service.List(r.Context(), scope, input)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	response := notificationPageResponse{
		Items: make([]notificationResponse, 0, len(page.Items)),
	}
	for _, item := range page.Items {
		mapped, mapErr := mapNotification(item)
		if mapErr != nil {
			handlers.writeProblem(w, r, mapErr)
			return
		}
		response.Items = append(response.Items, mapped)
	}
	if page.NextCursor != "" {
		response.NextCursor = &page.NextCursor
	}
	writeJSON(handlers.logger, w, http.StatusOK, response)
}

func (handlers notificationHandlers) unreadCount(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	result, err := handlers.service.UnreadCount(r.Context(), scope)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers notificationHandlers) markRead(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	notificationID, ok := parseResourceUUID(r.PathValue("notification_id"))
	if !ok {
		handlers.writeProblem(w, r, notification.ErrNotFound)
		return
	}
	item, err := handlers.service.MarkRead(r.Context(), scope, notificationID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	response, err := mapNotification(item)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, response)
}

func (handlers notificationHandlers) markAllRead(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	result, err := handlers.service.MarkAllRead(r.Context(), scope)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers notificationHandlers) preference(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		handlers.getPreference(w, r)
		return
	}
	if r.Method == http.MethodPut {
		handlers.putPreference(w, r)
		return
	}
	w.Header().Set("Allow", "GET, HEAD, PUT")
	writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Notification preferences support GET and PUT requests.")
}

func (handlers notificationHandlers) getPreference(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	preference, err := handlers.service.GetPreference(r.Context(), scope)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, preference)
}

func (handlers notificationHandlers) putPreference(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	var request updateNotificationPreferenceRequest
	if err := decodeJSONRequest(w, r, &request, maximumNotificationBodySize); err != nil || !request.complete() {
		handlers.writeProblem(w, r, notification.ErrInvalidInput)
		return
	}
	preference, err := handlers.service.PutPreference(r.Context(), scope, notification.PutPreferenceInput{
		InAppEnabled:          *request.InAppEnabled,
		EmailEnabled:          *request.EmailEnabled,
		ReminderOffsetMinutes: *request.ReminderOffsetMinutes,
		QuietHoursEnabled:     *request.QuietHoursEnabled,
		QuietHoursStart:       request.QuietHoursStart.value,
		QuietHoursEnd:         request.QuietHoursEnd.value,
		QuietHoursTimezone:    *request.QuietHoursTimezone,
		ExpectedVersion:       *request.ExpectedVersion,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, preference)
}

func (request updateNotificationPreferenceRequest) complete() bool {
	return request.InAppEnabled != nil && request.EmailEnabled != nil &&
		request.ReminderOffsetMinutes != nil && request.QuietHoursEnabled != nil &&
		request.QuietHoursStart.present && request.QuietHoursEnd.present &&
		request.QuietHoursTimezone != nil && request.ExpectedVersion != nil
}

func parseNotificationListInput(r *http.Request) (notification.ListInput, error) {
	query := r.URL.Query()
	input := notification.ListInput{Cursor: strings.TrimSpace(query.Get("cursor"))}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return notification.ListInput{}, notification.ErrInvalidInput
		}
		input.Limit = limit
	}
	if raw := strings.TrimSpace(query.Get("unread_only")); raw != "" {
		switch raw {
		case "true":
			input.UnreadOnly = true
		case "false":
			input.UnreadOnly = false
		default:
			return notification.ListInput{}, notification.ErrInvalidInput
		}
	}
	return input, nil
}

func notificationScope(principal identity.Principal) (tenancy.Context, bool) {
	if principal.ActiveTenant == nil {
		return tenancy.Context{}, false
	}
	scope, err := tenancy.New(principal.ActiveTenant.ID, principal.User.ID)
	return scope, err == nil
}

func (handlers notificationHandlers) scope(
	w http.ResponseWriter,
	r *http.Request,
	principal identity.Principal,
) (tenancy.Context, bool) {
	scope, ok := notificationScope(principal)
	if !ok {
		handlers.writeProblem(w, r, notification.ErrAccessDenied)
		return tenancy.Context{}, false
	}
	expectedTenantID, ok := parseResourceUUID(
		strings.TrimSpace(r.Header.Get(notificationTenantHeader)),
	)
	if !ok {
		handlers.writeProblem(w, r, notification.ErrInvalidInput)
		return tenancy.Context{}, false
	}
	if expectedTenantID != scope.TenantID {
		handlers.writeProblem(w, r, notification.ErrScopeChanged)
		return tenancy.Context{}, false
	}
	return scope, true
}

func mapNotification(item notification.Notification) (notificationResponse, error) {
	if item.Kind == notification.KindSystemWorkerCanary {
		return notificationResponse{}, errors.New("system notification cannot be exposed")
	}
	contextValues := make(map[string]string)
	if len(item.Context) > 0 {
		if err := json.Unmarshal(item.Context, &contextValues); err != nil {
			return notificationResponse{}, err
		}
	}
	return notificationResponse{
		ID: item.ID, EffectKey: item.EffectKey, TemplateKey: item.TemplateKey,
		ResourceType: item.ResourceType, ResourceID: item.ResourceID, Context: contextValues,
		OccurredAt: item.OccurredAt, ReadAt: item.ReadAt, CreatedAt: item.CreatedAt,
	}, nil
}

func (handlers notificationHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.service == nil {
		writeCodedProblem(w, r, http.StatusServiceUnavailable, "notification_unavailable", "Notifications unavailable", "Notifications are not configured for this environment.")
		return false
	}
	return true
}

func (handlers notificationHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers notificationHandlers) writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status, code := http.StatusServiceUnavailable, "notification_unavailable"
	title, detail := "Notification request failed", "Notifications could not be loaded safely."
	switch {
	case errors.Is(err, notification.ErrInvalidInput):
		status, code = http.StatusBadRequest, "notification_invalid"
		title, detail = "Invalid notification request", "Review the pagination or preference values."
	case errors.Is(err, notification.ErrAccessDenied):
		status, code = http.StatusForbidden, "notification_forbidden"
		title, detail = "Notification access denied", "The active workspace cannot authorize this request."
	case errors.Is(err, notification.ErrNotFound):
		status, code = http.StatusNotFound, "notification_not_found"
		title, detail = "Notification not found", "The notification does not exist in the active user scope."
	case errors.Is(err, notification.ErrConflict):
		status, code = http.StatusConflict, "notification_preference_conflict"
		title, detail = "Notification preferences changed", "Reload the latest preferences before saving again."
	case errors.Is(err, notification.ErrScopeChanged):
		status, code = http.StatusConflict, "notification_scope_changed"
		title, detail = "Active workspace changed", "Reload the current session before retrying the notification request."
	default:
		handlers.logger.Error("notification request failed", "error", err)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
