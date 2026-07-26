package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	calendarItemsPath       = "/api/v1/calendar/items"
	calendarPreferencePath  = "/api/v1/calendar/preferences/display"
	calendarTenantHeader    = "X-TutorHub-Expected-Tenant-ID"
	maximumCalendarBodySize = 16 * 1024
)

type calendarHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service calendar.ServiceAPI
}

type calendarPageResponse struct {
	Items      []calendar.Item `json:"items"`
	NextCursor *string         `json:"next_cursor"`
}

type updateCalendarPreferenceRequest struct {
	ViewerTimezone    *string                     `json:"viewer_timezone"`
	Locale            *string                     `json:"locale"`
	TimeFormat        *string                     `json:"time_format"`
	WeekStart         *string                     `json:"week_start"`
	DefaultView       *string                     `json:"default_view"`
	Density           *string                     `json:"density"`
	TimeScaleMinutes  *int                        `json:"time_scale_minutes"`
	SecondaryTimezone calendarNullableStringField `json:"secondary_timezone"`
	ExpectedVersion   *int64                      `json:"expected_version"`
}

type calendarNullableStringField struct {
	present bool
	value   *string
}

func (field *calendarNullableStringField) UnmarshalJSON(data []byte) error {
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

func (request updateCalendarPreferenceRequest) complete() bool {
	return request.ViewerTimezone != nil && request.Locale != nil &&
		request.TimeFormat != nil && request.WeekStart != nil &&
		request.DefaultView != nil && request.Density != nil &&
		request.TimeScaleMinutes != nil && request.SecondaryTimezone.present &&
		request.ExpectedVersion != nil
}

func newCalendarHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service calendar.ServiceAPI,
) calendarHandlers {
	return calendarHandlers{logger: logger, auth: auth, service: service}
}

func calendarResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Add("Vary", "Cookie")
		w.Header().Add("Vary", calendarTenantHeader)
		next.ServeHTTP(w, r)
	})
}

func (handlers calendarHandlers) listItems(w http.ResponseWriter, r *http.Request) {
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
	input, err := parseCalendarListInput(r.URL.Query())
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	page, err := handlers.service.ListItems(r.Context(), scope, input)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	response := calendarPageResponse{Items: page.Items}
	if response.Items == nil {
		response.Items = []calendar.Item{}
	}
	if page.NextCursor != "" {
		response.NextCursor = &page.NextCursor
	}
	writeJSON(handlers.logger, w, http.StatusOK, response)
}

func (handlers calendarHandlers) preference(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.getPreference(w, r)
	case http.MethodPut:
		handlers.updatePreference(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Calendar display preferences support GET and PUT requests.",
		)
	}
}

func (handlers calendarHandlers) getPreference(w http.ResponseWriter, r *http.Request) {
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

func (handlers calendarHandlers) updatePreference(w http.ResponseWriter, r *http.Request) {
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	var request updateCalendarPreferenceRequest
	if err := decodeJSONRequest(w, r, &request, maximumCalendarBodySize); err != nil ||
		!request.complete() {
		handlers.writeProblem(w, r, calendar.ErrInvalidInput)
		return
	}
	preference, err := handlers.service.UpdatePreference(
		r.Context(),
		scope,
		calendar.UpdatePreferenceInput{
			ViewerTimezone:    *request.ViewerTimezone,
			Locale:            *request.Locale,
			TimeFormat:        *request.TimeFormat,
			WeekStart:         *request.WeekStart,
			DefaultView:       *request.DefaultView,
			Density:           *request.Density,
			TimeScaleMinutes:  *request.TimeScaleMinutes,
			SecondaryTimezone: request.SecondaryTimezone.value,
			ExpectedVersion:   *request.ExpectedVersion,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, preference)
}

func parseCalendarListInput(query url.Values) (calendar.ListInput, error) {
	input := calendar.ListInput{
		From:           strings.TrimSpace(query.Get("from")),
		To:             strings.TrimSpace(query.Get("to")),
		Types:          commaSeparatedQueryValues(query, "types"),
		ClassIDs:       commaSeparatedQueryValues(query, "class_ids"),
		Statuses:       commaSeparatedQueryValues(query, "statuses"),
		Search:         strings.TrimSpace(query.Get("search")),
		Cursor:         strings.TrimSpace(query.Get("cursor")),
		ViewerTimezone: strings.TrimSpace(query.Get("viewer_timezone")),
	}
	if input.From == "" || input.To == "" || input.ViewerTimezone == "" {
		return calendar.ListInput{}, calendar.ErrInvalidInput
	}
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return calendar.ListInput{}, calendar.ErrInvalidInput
		}
		input.Limit = limit
	}
	return input, nil
}

func commaSeparatedQueryValues(query url.Values, key string) []string {
	var result []string
	for _, value := range query[key] {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

func (handlers calendarHandlers) scope(
	w http.ResponseWriter,
	r *http.Request,
	principal identity.Principal,
) (tenancy.Context, bool) {
	if principal.ActiveTenant == nil {
		handlers.writeProblem(w, r, calendar.ErrAccessDenied)
		return tenancy.Context{}, false
	}
	scope, err := tenancy.New(principal.ActiveTenant.ID, principal.User.ID)
	if err != nil {
		handlers.writeProblem(w, r, calendar.ErrAccessDenied)
		return tenancy.Context{}, false
	}
	expectedTenantID, ok := parseResourceUUID(strings.TrimSpace(r.Header.Get(calendarTenantHeader)))
	if !ok {
		handlers.writeProblem(w, r, calendar.ErrInvalidInput)
		return tenancy.Context{}, false
	}
	if expectedTenantID != scope.TenantID {
		handlers.writeProblem(w, r, calendar.ErrScopeChanged)
		return tenancy.Context{}, false
	}
	return scope, true
}

func (handlers calendarHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.service != nil {
		return true
	}
	writeCodedProblem(
		w,
		r,
		http.StatusServiceUnavailable,
		"calendar_unavailable",
		"Calendar unavailable",
		"Calendar storage is not configured for this environment.",
	)
	return false
}

func (handlers calendarHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers calendarHandlers) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	status, code := http.StatusInternalServerError, "calendar_failed"
	title, detail := "Calendar request failed", "The calendar request could not be completed safely."
	switch {
	case errors.Is(err, calendar.ErrInvalidInput),
		errors.Is(err, calendar.ErrInvalidRange),
		errors.Is(err, calendar.ErrInvalidCursor):
		status, code = http.StatusBadRequest, "calendar_invalid"
		title, detail = "Invalid calendar request", "Review the bounded range, filters, cursor, timezone, and preference values."
	case errors.Is(err, calendar.ErrAccessDenied):
		status, code = http.StatusForbidden, "calendar_forbidden"
		title, detail = "Calendar access denied", "The active workspace cannot authorize this calendar request."
	case errors.Is(err, calendar.ErrConflict):
		status, code = http.StatusConflict, "calendar_preference_conflict"
		title, detail = "Calendar preferences changed", "Reload the latest calendar preferences before saving again."
	case errors.Is(err, calendar.ErrScopeChanged):
		status, code = http.StatusConflict, "calendar_scope_changed"
		title, detail = "Active workspace changed", "Reload the current session before retrying the calendar request."
	default:
		handlers.logger.Error(
			"calendar request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
