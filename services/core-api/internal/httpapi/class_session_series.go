package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
)

const (
	classSessionSeriesCollectionPattern = "/api/v1/classes/{class_id}/session-series"
	classSessionSeriesResourcePattern   = "/api/v1/classes/{class_id}/session-series/{series_id}"
	classSessionSeriesPreviewPattern    = "/api/v1/classes/{class_id}/session-series/{series_id}/preview"
	classSessionSeriesMutationPattern   = "/api/v1/classes/{class_id}/session-series/{series_id}/occurrence"
	classSessionSeriesCancelPattern     = "/api/v1/classes/{class_id}/session-series/{series_id}/occurrence/cancel"
)

type recurrenceEndRequest struct {
	Type  recurrence.EndType `json:"type"`
	Date  string             `json:"date"`
	Count int                `json:"count"`
}

type recurrenceRuleRequest struct {
	Frequency recurrence.Frequency `json:"frequency"`
	Interval  int                  `json:"interval"`
	Weekdays  []recurrence.Weekday `json:"weekdays"`
	MonthDays []int                `json:"month_days"`
	Months    []int                `json:"months"`
	Ends      recurrenceEndRequest `json:"ends"`
}

type recurrenceEndResponse struct {
	Type  recurrence.EndType `json:"type"`
	Date  string             `json:"date,omitempty"`
	Count int                `json:"count,omitempty"`
}

type recurrenceRuleResponse struct {
	Frequency recurrence.Frequency  `json:"frequency"`
	Interval  int                   `json:"interval"`
	Weekdays  []recurrence.Weekday  `json:"weekdays,omitempty"`
	MonthDays []int                 `json:"month_days,omitempty"`
	Months    []int                 `json:"months,omitempty"`
	Ends      recurrenceEndResponse `json:"ends"`
}

func (request recurrenceRuleRequest) domain() recurrence.Rule {
	return recurrence.Rule{
		Frequency: request.Frequency,
		Interval:  request.Interval,
		Weekdays:  request.Weekdays,
		MonthDays: request.MonthDays,
		Months:    request.Months,
		End: recurrence.End{
			Type: request.Ends.Type, Date: request.Ends.Date, Count: request.Ends.Count,
		},
	}
}

type createClassSessionSeriesRequest struct {
	Title         string                   `json:"title"`
	Description   string                   `json:"description"`
	StartsAt      string                   `json:"starts_at"`
	EndsAt        string                   `json:"ends_at"`
	Timezone      string                   `json:"timezone"`
	Rule          recurrenceRuleRequest    `json:"rule"`
	OverlapPolicy recurrence.OverlapPolicy `json:"overlap_policy"`
}

type classSessionOccurrenceMutationRequest struct {
	Scope                    recurrence.EditScope             `json:"scope"`
	OccurrenceKey            string                           `json:"occurrence_key"`
	ExpectedVersion          int64                            `json:"expected_version"`
	IdempotencyKey           string                           `json:"idempotency_key"`
	FollowingExceptionPolicy recurrence.FutureExceptionPolicy `json:"following_exception_policy"`
	Title                    *string                          `json:"title"`
	Description              *string                          `json:"description"`
	StartsAt                 *string                          `json:"starts_at"`
	EndsAt                   *string                          `json:"ends_at"`
	Timezone                 *string                          `json:"timezone"`
	Rule                     *recurrenceRuleRequest           `json:"rule"`
	OverlapPolicy            *recurrence.OverlapPolicy        `json:"overlap_policy"`
	OverrideConflict         bool                             `json:"override_schedule_conflict"`
	ConflictReason           string                           `json:"schedule_conflict_reason"`
}

func (request classSessionOccurrenceMutationRequest) domain() classroom.OccurrenceMutationInput {
	var rule *recurrence.Rule
	if request.Rule != nil {
		value := request.Rule.domain()
		rule = &value
	}
	return classroom.OccurrenceMutationInput{
		Scope: request.Scope, OccurrenceKey: request.OccurrenceKey,
		ExpectedVersion: request.ExpectedVersion, IdempotencyKey: request.IdempotencyKey,
		FollowingExceptionPolicy: request.FollowingExceptionPolicy,
		Title:                    request.Title, Description: request.Description,
		StartsAt: request.StartsAt, EndsAt: request.EndsAt, Timezone: request.Timezone,
		Rule: rule, OverlapPolicy: request.OverlapPolicy,
		OverrideScheduleConflict: request.OverrideConflict,
		ScheduleConflictReason:   request.ConflictReason,
	}
}

type classSessionSeriesResponse struct {
	ID              uuid.UUID                        `json:"id"`
	ClassID         uuid.UUID                        `json:"class_id"`
	Title           string                           `json:"title"`
	Description     string                           `json:"description"`
	LocalStart      string                           `json:"local_start"`
	Timezone        string                           `json:"timezone"`
	DurationMinutes int                              `json:"duration_minutes"`
	Rule            recurrenceRuleResponse           `json:"rule"`
	OverlapPolicy   recurrence.OverlapPolicy         `json:"overlap_policy"`
	Status          classroom.SeriesStatus           `json:"status"`
	Version         int64                            `json:"version"`
	Sequence        int64                            `json:"sequence"`
	ICalUID         string                           `json:"ical_uid"`
	SplitFrom       *uuid.UUID                       `json:"split_from_series_id"`
	CreatedBy       uuid.UUID                        `json:"created_by"`
	UpdatedBy       uuid.UUID                        `json:"updated_by"`
	CancelledAt     *time.Time                       `json:"cancelled_at"`
	CancelledBy     *uuid.UUID                       `json:"cancelled_by"`
	CreatedAt       time.Time                        `json:"created_at"`
	UpdatedAt       time.Time                        `json:"updated_at"`
	ViewerAccess    classSessionViewerAccessResponse `json:"viewer_access"`
}

type seriesMutationResponse struct {
	Series classSessionSeriesResponse `json:"series"`
	Replay bool                       `json:"replay"`
}

type seriesScopePreviewResponse struct {
	Scope                   recurrence.EditScope             `json:"scope"`
	BoundaryOccurrenceKey   string                           `json:"boundary_occurrence_key"`
	BoundaryOriginalLocal   string                           `json:"boundary_original_local"`
	AffectedOccurrenceCount int                              `json:"affected_occurrence_count"`
	FutureExceptionCount    int                              `json:"future_exception_count"`
	RetainedExceptionCount  int                              `json:"retained_exception_count"`
	DiscardedExceptionCount int                              `json:"discarded_exception_count"`
	FutureExceptionPolicy   recurrence.FutureExceptionPolicy `json:"future_exception_policy"`
	Conflicts               []classScheduleConflictResponse  `json:"conflicts"`
}

type classScheduleConflictResponse struct {
	ClassID       uuid.UUID  `json:"class_id"`
	SeriesID      *uuid.UUID `json:"series_id,omitempty"`
	OccurrenceKey string     `json:"occurrence_key"`
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
}

type classSessionSeriesHandlers struct {
	auth   authHandlers
	series classroom.SessionSeriesServiceAPI
	base   classSessionHandlers
}

func classSessionSeriesResourceAuditMutation(r *http.Request) (audit.Draft, bool) {
	seriesID, ok := parseResourceUUID(r.PathValue("series_id"))
	if !ok {
		return audit.Draft{}, false
	}
	action := audit.ActionClassSessionUpdate
	switch r.Method {
	case http.MethodPatch:
		action = audit.ActionClassSessionUpdate
	case http.MethodPost:
		action = audit.ActionClassSessionCancel
	default:
		return audit.Draft{}, false
	}
	return audit.Draft{
		Action: action, ResourceType: "class_session_series", ResourceID: seriesID,
	}, true
}

func newClassSessionSeriesHandlers(
	auth authHandlers,
	series classroom.SessionSeriesServiceAPI,
	base classSessionHandlers,
) classSessionSeriesHandlers {
	return classSessionSeriesHandlers{auth: auth, series: series, base: base}
}

func (handlers classSessionSeriesHandlers) collection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Recurring session collections accept POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.base.csrfPrincipal(w, r)
	if !ok {
		return
	}
	classID, ok := parseResourceUUID(r.PathValue("class_id"))
	if !ok {
		handlers.base.writeProblem(w, r, classroom.ErrClassNotFound)
		return
	}
	var request createClassSessionSeriesRequest
	if err := decodeJSONRequest(w, r, &request, maximumClassSessionRequestSize); err != nil {
		handlers.base.writeProblem(w, r, classroom.ErrInvalidSessionInput)
		return
	}
	series, err := handlers.series.CreateSeries(
		r.Context(), classAccess(principal), classID,
		classroom.CreateSeriesInput{
			Title: request.Title, Description: request.Description,
			StartsAt: request.StartsAt, EndsAt: request.EndsAt,
			Timezone: request.Timezone, Rule: request.Rule.domain(),
			OverlapPolicy: request.OverlapPolicy,
		},
	)
	if err != nil {
		handlers.base.writeProblem(w, r, err)
		return
	}
	w.Header().Set("Location", classSessionsPathPrefix+classID.String()+"/session-series/"+series.ID.String())
	handlers.writeJSON(w, http.StatusCreated, newClassSessionSeriesResponse(series))
}

func (handlers classSessionSeriesHandlers) resource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Recurring session details support GET requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.auth.authenticatedPrincipal(w, r)
	if !ok {
		return
	}
	classID, seriesID, ok := handlers.pathIDs(w, r)
	if !ok {
		return
	}
	series, err := handlers.series.GetSeries(r.Context(), classAccess(principal), classID, seriesID)
	if err != nil {
		handlers.base.writeProblem(w, r, err)
		return
	}
	handlers.writeJSON(w, http.StatusOK, newClassSessionSeriesResponse(series))
}

func (handlers classSessionSeriesHandlers) preview(w http.ResponseWriter, r *http.Request) {
	handlers.mutate(w, r, "preview")
}

func (handlers classSessionSeriesHandlers) update(w http.ResponseWriter, r *http.Request) {
	handlers.mutate(w, r, "update")
}

func (handlers classSessionSeriesHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	handlers.mutate(w, r, "cancel")
}

func (handlers classSessionSeriesHandlers) mutate(w http.ResponseWriter, r *http.Request, operation string) {
	method := http.MethodPost
	if operation == "update" {
		method = http.MethodPatch
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "The recurring occurrence mutation uses an explicit method.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	principal, ok := handlers.base.csrfPrincipal(w, r)
	if !ok {
		return
	}
	classID, seriesID, ok := handlers.pathIDs(w, r)
	if !ok {
		return
	}
	var request classSessionOccurrenceMutationRequest
	if err := decodeJSONRequest(w, r, &request, maximumClassSessionRequestSize); err != nil {
		handlers.base.writeProblem(w, r, classroom.ErrInvalidSessionInput)
		return
	}
	input := request.domain()
	if operation == "preview" {
		preview, err := handlers.series.PreviewSeriesMutation(
			r.Context(), classAccess(principal), classID, seriesID, input,
		)
		if err != nil {
			handlers.base.writeProblem(w, r, err)
			return
		}
		conflicts := make([]classScheduleConflictResponse, 0, len(preview.Conflicts))
		for _, conflict := range preview.Conflicts {
			conflicts = append(conflicts, classScheduleConflictResponse{
				ClassID: conflict.ClassID, SeriesID: conflict.SeriesID,
				OccurrenceKey: conflict.OccurrenceKey,
				StartsAt:      conflict.StartsAt.UTC(), EndsAt: conflict.EndsAt.UTC(),
			})
		}
		handlers.writeJSON(w, http.StatusOK, seriesScopePreviewResponse{
			Scope: preview.Scope, BoundaryOccurrenceKey: preview.BoundaryOccurrenceKey,
			BoundaryOriginalLocal:   preview.BoundaryOriginalLocal,
			AffectedOccurrenceCount: preview.AffectedOccurrenceCount,
			FutureExceptionCount:    preview.FutureExceptionCount,
			RetainedExceptionCount:  preview.RetainedExceptionCount,
			DiscardedExceptionCount: preview.DiscardedExceptionCount,
			FutureExceptionPolicy:   preview.FutureExceptionPolicy,
			Conflicts:               conflicts,
		})
		return
	}
	var result classroom.SeriesMutationResult
	var err error
	if operation == "cancel" {
		result, err = handlers.series.CancelSeriesOccurrence(
			r.Context(), classAccess(principal), classID, seriesID, input,
		)
	} else {
		result, err = handlers.series.UpdateSeriesOccurrence(
			r.Context(), classAccess(principal), classID, seriesID, input,
		)
	}
	if err != nil {
		handlers.base.writeProblem(w, r, err)
		return
	}
	handlers.writeJSON(w, http.StatusOK, seriesMutationResponse{
		Series: newClassSessionSeriesResponse(result.Series), Replay: result.Replay,
	})
}

func (handlers classSessionSeriesHandlers) pathIDs(
	w http.ResponseWriter,
	r *http.Request,
) (uuid.UUID, uuid.UUID, bool) {
	classID, classOK := parseResourceUUID(r.PathValue("class_id"))
	seriesID, seriesOK := parseResourceUUID(r.PathValue("series_id"))
	if classOK && seriesOK {
		return classID, seriesID, true
	}
	handlers.base.writeProblem(w, r, classroom.ErrSeriesNotFound)
	return uuid.Nil, uuid.Nil, false
}

func (handlers classSessionSeriesHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) {
		return false
	}
	if handlers.series != nil {
		return true
	}
	writeProblem(w, r, http.StatusServiceUnavailable, "Recurring sessions unavailable", "Recurring session storage is not configured for this environment.")
	return false
}

func (handlers classSessionSeriesHandlers) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Cookie")
	writeJSON(handlers.base.logger, w, status, value)
}

func newClassSessionSeriesResponse(series classroom.ClassSessionSeries) classSessionSeriesResponse {
	return classSessionSeriesResponse{
		ID: series.ID, ClassID: series.ClassID, Title: series.Title,
		Description: series.Description, LocalStart: series.LocalStart,
		Timezone: series.Timezone, DurationMinutes: series.DurationMinutes,
		Rule: newRecurrenceRuleResponse(series.Rule), OverlapPolicy: series.OverlapPolicy,
		Status: series.Status, Version: series.Version, Sequence: series.Sequence,
		ICalUID: series.ICalUID, SplitFrom: series.SplitFrom,
		CreatedBy: series.CreatedBy, UpdatedBy: series.UpdatedBy,
		CancelledAt: series.CancelledAt, CancelledBy: series.CancelledBy,
		CreatedAt: series.CreatedAt.UTC(), UpdatedAt: series.UpdatedAt.UTC(),
		ViewerAccess: classSessionViewerAccessResponse{
			CanUpdate: series.ViewerAccess.CanUpdate,
			CanCancel: series.ViewerAccess.CanCancel,
		},
	}
}

func newRecurrenceRuleResponse(rule recurrence.Rule) recurrenceRuleResponse {
	return recurrenceRuleResponse{
		Frequency: rule.Frequency,
		Interval:  rule.Interval,
		Weekdays:  append([]recurrence.Weekday(nil), rule.Weekdays...),
		MonthDays: append([]int(nil), rule.MonthDays...),
		Months:    append([]int(nil), rule.Months...),
		Ends: recurrenceEndResponse{
			Type:  rule.End.Type,
			Date:  rule.End.Date,
			Count: rule.End.Count,
		},
	}
}
