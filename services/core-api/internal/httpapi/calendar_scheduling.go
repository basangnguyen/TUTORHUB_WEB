package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	calendarWorkingSchedulePath      = "/api/v1/calendar/working-schedule"
	calendarAvailabilityQueryPath    = "/api/v1/calendar/availability/query"
	maximumWorkingScheduleBodySize   = 256 * 1024
	maximumAvailabilityQueryBodySize = 32 * 1024
)

type calendarSchedulingHandlers struct {
	logger  *slog.Logger
	auth    authHandlers
	service calendar.SchedulingServiceAPI
}

type putCalendarWorkingScheduleRequest struct {
	Timezone        *string                              `json:"timezone"`
	WeeklyIntervals *[]calendar.WeeklyWorkingInterval    `json:"weekly_intervals"`
	Exceptions      *[]calendar.WorkingScheduleException `json:"exceptions"`
	ExpectedVersion *int64                               `json:"expected_version"`
}

func (request putCalendarWorkingScheduleRequest) complete() bool {
	return request.Timezone != nil && request.WeeklyIntervals != nil &&
		request.Exceptions != nil && request.ExpectedVersion != nil
}

type calendarAvailabilityQueryRequest struct {
	ClassID         *string                                      `json:"class_id"`
	From            *string                                      `json:"from"`
	To              *string                                      `json:"to"`
	Timezone        *string                                      `json:"timezone"`
	DurationMinutes *int                                         `json:"duration_minutes"`
	StepMinutes     *int                                         `json:"step_minutes"`
	MaxCandidates   *int                                         `json:"max_candidates"`
	Required        *[]calendar.AvailabilityParticipantReference `json:"required"`
	Optional        *[]calendar.AvailabilityParticipantReference `json:"optional"`
}

func (request calendarAvailabilityQueryRequest) complete() bool {
	return request.ClassID != nil && request.From != nil && request.To != nil &&
		request.Timezone != nil && request.DurationMinutes != nil &&
		request.StepMinutes != nil &&
		request.Required != nil && request.Optional != nil
}

func newCalendarSchedulingHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service calendar.SchedulingServiceAPI,
) calendarSchedulingHandlers {
	return calendarSchedulingHandlers{logger: logger, auth: auth, service: service}
}

// workingScheduleHandler and availabilityQueryHandler include the calendar
// privacy boundary so route wiring cannot accidentally make availability data
// cacheable by a shared intermediary.
func (handlers calendarSchedulingHandlers) workingScheduleHandler() http.Handler {
	return calendarResponseHeaders(http.HandlerFunc(handlers.workingSchedule))
}

func (handlers calendarSchedulingHandlers) availabilityQueryHandler() http.Handler {
	return calendarResponseHeaders(http.HandlerFunc(handlers.availabilityQuery))
}

func (handlers calendarSchedulingHandlers) workingSchedule(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !handlers.available(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.getWorkingSchedule(w, r)
	case http.MethodPut:
		handlers.putWorkingSchedule(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, PUT")
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Calendar working schedules support GET and PUT requests.",
		)
	}
}

func (handlers calendarSchedulingHandlers) getWorkingSchedule(
	w http.ResponseWriter,
	r *http.Request,
) {
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	schedule, err := handlers.service.GetWorkingSchedule(r.Context(), scope)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	normalizeWorkingScheduleResponse(&schedule)
	writeJSON(handlers.logger, w, http.StatusOK, schedule)
}

func (handlers calendarSchedulingHandlers) putWorkingSchedule(
	w http.ResponseWriter,
	r *http.Request,
) {
	principal, ok := handlers.csrfPrincipal(w, r)
	if !ok {
		return
	}
	scope, ok := handlers.scope(w, r, principal)
	if !ok {
		return
	}
	var request putCalendarWorkingScheduleRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumWorkingScheduleBodySize,
	); err != nil || !request.complete() {
		handlers.writeProblem(w, r, calendar.ErrInvalidInput)
		return
	}
	schedule, err := handlers.service.PutWorkingSchedule(
		r.Context(),
		scope,
		calendar.PutWorkingScheduleInput{
			Timezone:        *request.Timezone,
			WeeklyIntervals: *request.WeeklyIntervals,
			Exceptions:      *request.Exceptions,
			ExpectedVersion: *request.ExpectedVersion,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	normalizeWorkingScheduleResponse(&schedule)
	writeJSON(handlers.logger, w, http.StatusOK, schedule)
}

func (handlers calendarSchedulingHandlers) availabilityQuery(
	w http.ResponseWriter,
	r *http.Request,
) {
	if !handlers.available(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(
			w,
			r,
			http.StatusMethodNotAllowed,
			"Method not allowed",
			"Calendar availability supports POST requests.",
		)
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
	var request calendarAvailabilityQueryRequest
	if err := decodeJSONRequest(
		w,
		r,
		&request,
		maximumAvailabilityQueryBodySize,
	); err != nil || !request.complete() {
		handlers.writeProblem(w, r, calendar.ErrInvalidInput)
		return
	}
	maxCandidates := 0
	if request.MaxCandidates != nil {
		maxCandidates = *request.MaxCandidates
	}
	result, err := handlers.service.QueryAvailability(
		r.Context(),
		scope,
		calendar.AvailabilityQueryInput{
			ClassID:         *request.ClassID,
			From:            *request.From,
			To:              *request.To,
			Timezone:        *request.Timezone,
			DurationMinutes: *request.DurationMinutes,
			StepMinutes:     *request.StepMinutes,
			MaxCandidates:   maxCandidates,
			Required:        *request.Required,
			Optional:        *request.Optional,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	normalizeAvailabilityResponse(&result)
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers calendarSchedulingHandlers) available(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.service != nil {
		return true
	}
	handlers.writeProblem(w, r, calendar.ErrSchedulingUnavailable)
	return false
}

func (handlers calendarSchedulingHandlers) csrfPrincipal(
	w http.ResponseWriter,
	r *http.Request,
) (identity.Principal, bool) {
	sessionToken, ok := handlers.auth.sessionToken(w, r)
	if !ok {
		return identity.Principal{}, false
	}
	return handlers.auth.csrfPrincipal(w, r, sessionToken)
}

func (handlers calendarSchedulingHandlers) scope(
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

func (handlers calendarSchedulingHandlers) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	status, code := http.StatusInternalServerError, "calendar_scheduling_failed"
	title := "Calendar scheduling failed"
	detail := "The calendar scheduling request could not be completed safely."
	switch {
	case errors.Is(err, calendar.ErrInvalidInput), errors.Is(err, calendar.ErrInvalidRange):
		status, code = http.StatusBadRequest, "calendar_scheduling_invalid"
		title = "Invalid calendar scheduling request"
		detail = "Review the bounded range, timezone, participants, intervals, and candidate settings."
	case errors.Is(err, calendar.ErrAccessDenied):
		status, code = http.StatusForbidden, "calendar_scheduling_forbidden"
		title = "Calendar scheduling access denied"
		detail = "The active workspace cannot authorize this scheduling request."
	case errors.Is(err, calendar.ErrSchedulingNotFound):
		status, code = http.StatusNotFound, "calendar_scheduling_not_found"
		title = "Calendar scheduling resource not found"
		detail = "The requested class or participant is not available in the active workspace."
	case errors.Is(err, calendar.ErrWorkingScheduleStale):
		status, code = http.StatusConflict, "calendar_working_schedule_conflict"
		title = "Working schedule changed"
		detail = "Reload the latest working schedule before saving again."
	case errors.Is(err, calendar.ErrAvailabilityCapacity):
		status, code = http.StatusTooManyRequests, "calendar_availability_capacity_exceeded"
		title = "Availability query capacity exceeded"
		detail = "Reduce the availability range or use a larger step before retrying."
	case errors.Is(err, calendar.ErrScopeChanged):
		status, code = http.StatusConflict, "calendar_scope_changed"
		title = "Active workspace changed"
		detail = "Reload the current session before retrying the calendar request."
	case errors.Is(err, calendar.ErrSchedulingUnavailable), errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusServiceUnavailable, "calendar_scheduling_unavailable"
		title = "Calendar scheduling unavailable"
		detail = "Calendar scheduling is temporarily unavailable. Try again later."
	default:
		handlers.logger.Error(
			"calendar scheduling request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}

func normalizeWorkingScheduleResponse(schedule *calendar.WorkingSchedule) {
	if schedule.WeeklyIntervals == nil {
		schedule.WeeklyIntervals = []calendar.WeeklyWorkingInterval{}
	}
	if schedule.Exceptions == nil {
		schedule.Exceptions = []calendar.WorkingScheduleException{}
	}
}

func normalizeAvailabilityResponse(result *calendar.AvailabilityResult) {
	if result.Participants == nil {
		result.Participants = []calendar.ParticipantAvailability{}
	}
	if result.Suggestions == nil {
		result.Suggestions = []calendar.SuggestedTime{}
	}
	for index := range result.Participants {
		participant := &result.Participants[index]
		if participant.Intervals == nil {
			participant.Intervals = []calendar.AvailabilityStatusInterval{}
		}
		if participant.WorkingIntervals == nil {
			participant.WorkingIntervals = []calendar.AvailabilityWorkingInterval{}
		}
	}
}
