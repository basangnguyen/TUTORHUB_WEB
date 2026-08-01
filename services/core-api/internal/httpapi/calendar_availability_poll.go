package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	availabilityPollCollectionPath             = "/api/v1/calendar/availability-polls"
	availabilityPollResourcePattern            = "/api/v1/calendar/availability-polls/{poll_id}"
	availabilityPollOpenPattern                = "/api/v1/calendar/availability-polls/{poll_id}/open"
	availabilityPollClosePattern               = "/api/v1/calendar/availability-polls/{poll_id}/close"
	availabilityPollReopenPattern              = "/api/v1/calendar/availability-polls/{poll_id}/reopen"
	availabilityPollCancelPattern              = "/api/v1/calendar/availability-polls/{poll_id}/cancel"
	availabilityPollResponsePattern            = "/api/v1/calendar/availability-polls/{poll_id}/responses/me"
	availabilityPollIndividualResponsesPattern = "/api/v1/calendar/availability-polls/{poll_id}/responses"
	availabilityPollSummaryPattern             = "/api/v1/calendar/availability-polls/{poll_id}/summary"
	availabilityPollFinalizePattern            = "/api/v1/calendar/availability-polls/{poll_id}/finalize"
	availabilityPollCapabilitiesPattern        = "/api/v1/calendar/availability-polls/{poll_id}/capabilities"
	availabilityPollCapabilityPattern          = "/api/v1/calendar/availability-polls/{poll_id}/capabilities/{capability_id}/revoke"
	availabilityPollResolvePath                = "/api/v1/calendar/availability-polls/resolve"
	availabilityPollPublicRespondPath          = "/api/v1/calendar/availability-polls/respond"
	studyMeetingCollectionPath                 = "/api/v1/calendar/study-meetings"
	studyMeetingResourcePattern                = "/api/v1/calendar/study-meetings/{meeting_id}"
	studyMeetingCancelPattern                  = "/api/v1/calendar/study-meetings/{meeting_id}/cancel"
	maximumAvailabilityPollBodySize            = 2 * 1024 * 1024
	maximumAvailabilityPollCommandBodySize     = 128 * 1024
)

type availabilityPollHandlers struct {
	logger    *slog.Logger
	auth      authHandlers
	service   calendar.AvailabilityPollServiceAPI
	limiter   InvitationRateLimiter
	clock     func() time.Time
	webOrigin string
}

type createAvailabilityPollRequest struct {
	Title                  *string                                      `json:"title"`
	Description            *string                                      `json:"description"`
	ClassID                *uuid.UUID                                   `json:"class_id"`
	Timezone               *string                                      `json:"timezone"`
	RangeStart             *string                                      `json:"range_start"`
	RangeEnd               *string                                      `json:"range_end"`
	WorkingDayStart        *string                                      `json:"working_day_start"`
	WorkingDayEnd          *string                                      `json:"working_day_end"`
	DurationMinutes        *int                                         `json:"duration_minutes"`
	SlotGranularityMinutes *int                                         `json:"slot_granularity_minutes"`
	DeadlineAt             *time.Time                                   `json:"deadline_at"`
	ShareMode              *string                                      `json:"share_mode"`
	Slots                  *[]calendar.AvailabilityPollSlotInput        `json:"slots"`
	Participants           *[]calendar.AvailabilityPollParticipantInput `json:"participants"`
	IdempotencyKey         *string                                      `json:"idempotency_key"`
}

func (request createAvailabilityPollRequest) complete(requireIdempotency bool) bool {
	complete := request.Title != nil && request.Description != nil &&
		request.Timezone != nil && request.RangeStart != nil && request.RangeEnd != nil &&
		request.WorkingDayStart != nil && request.WorkingDayEnd != nil &&
		request.DurationMinutes != nil && request.SlotGranularityMinutes != nil &&
		request.DeadlineAt != nil && request.ShareMode != nil && request.Slots != nil &&
		request.Participants != nil
	return complete && (!requireIdempotency || request.IdempotencyKey != nil)
}

func (request createAvailabilityPollRequest) input() calendar.CreateAvailabilityPollInput {
	idempotencyKey := ""
	if request.IdempotencyKey != nil {
		idempotencyKey = *request.IdempotencyKey
	}
	return calendar.CreateAvailabilityPollInput{
		Title: *request.Title, Description: *request.Description, ClassID: request.ClassID,
		Timezone: *request.Timezone, RangeStart: *request.RangeStart, RangeEnd: *request.RangeEnd,
		WorkingDayStart: *request.WorkingDayStart, WorkingDayEnd: *request.WorkingDayEnd,
		DurationMinutes:        *request.DurationMinutes,
		SlotGranularityMinutes: *request.SlotGranularityMinutes,
		DeadlineAt:             *request.DeadlineAt, ShareMode: *request.ShareMode,
		Slots: *request.Slots, Participants: *request.Participants,
		IdempotencyKey: idempotencyKey,
	}
}

type updateAvailabilityPollRequest struct {
	createAvailabilityPollRequest
	ExpectedVersion *int64 `json:"expected_version"`
}

type availabilityPollVersionRequest struct {
	ExpectedVersion *int64 `json:"expected_version"`
}

type reopenAvailabilityPollRequest struct {
	ExpectedVersion *int64     `json:"expected_version"`
	DeadlineAt      *time.Time `json:"deadline_at"`
}

type availabilityPollReasonRequest struct {
	ExpectedVersion *int64  `json:"expected_version"`
	Reason          *string `json:"reason"`
}

type respondAvailabilityPollRequest struct {
	ExpectedResponseVersion *int64                                  `json:"expected_response_version"`
	Answers                 *[]calendar.AvailabilityPollAnswerInput `json:"answers"`
	IdempotencyKey          *string                                 `json:"idempotency_key"`
}

type finalizeAvailabilityPollRequest struct {
	SlotID          *uuid.UUID `json:"slot_id"`
	OutcomeType     *string    `json:"outcome_type"`
	ClassID         *uuid.UUID `json:"class_id"`
	ExpectedVersion *int64     `json:"expected_version"`
	IdempotencyKey  *string    `json:"idempotency_key"`
}

type createAvailabilityPollCapabilityRequest struct {
	Scope           *string    `json:"scope"`
	ParticipantID   *uuid.UUID `json:"participant_id"`
	ExpiresAt       *time.Time `json:"expires_at"`
	ExpectedVersion *int64     `json:"expected_version"`
}

func newAvailabilityPollHandlers(
	logger *slog.Logger,
	auth authHandlers,
	service calendar.AvailabilityPollServiceAPI,
	limiter InvitationRateLimiter,
	clock func() time.Time,
	webOrigin string,
) availabilityPollHandlers {
	return availabilityPollHandlers{
		logger: logger, auth: auth, service: service, limiter: limiter,
		clock: clock, webOrigin: canonicalPublicOrigin(webOrigin),
	}
}

func availabilityPollResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func publicAvailabilityPollResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		w.Header().Set(
			"Content-Security-Policy",
			"default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'",
		)
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func (handlers availabilityPollHandlers) collection(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		handlers.listPolls(w, r)
	case http.MethodPost:
		handlers.createPoll(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Availability polls support GET and POST requests.")
	}
}

func (handlers availabilityPollHandlers) listPolls(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := handlers.authenticatedScope(w, r, false)
	if !ok {
		return
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
			return
		}
		limit = parsed
	}
	polls, err := handlers.service.ListPolls(r.Context(), scope, calendar.ListAvailabilityPollsInput{
		Status: r.URL.Query().Get("status"), Limit: limit,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	if polls == nil {
		polls = []calendar.AvailabilityPoll{}
	}
	writeJSON(handlers.logger, w, http.StatusOK, map[string]any{"polls": polls})
}

func (handlers availabilityPollHandlers) createPoll(w http.ResponseWriter, r *http.Request) {
	_, scope, ok := handlers.authenticatedScope(w, r, true)
	if !ok {
		return
	}
	var request createAvailabilityPollRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollBodySize); err != nil ||
		!request.complete(true) {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	poll, err := handlers.service.CreatePoll(r.Context(), scope, request.input())
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusCreated, poll)
}

func (handlers availabilityPollHandlers) resource(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	pollID, ok := parseResourceUUID(r.PathValue("poll_id"))
	if !ok {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		_, scope, ok := handlers.authenticatedScope(w, r, false)
		if !ok {
			return
		}
		poll, err := handlers.service.GetPoll(r.Context(), scope, pollID)
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, poll)
	case http.MethodPatch:
		_, scope, ok := handlers.authenticatedScope(w, r, true)
		if !ok {
			return
		}
		var request updateAvailabilityPollRequest
		if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollBodySize); err != nil ||
			!request.createAvailabilityPollRequest.complete(false) || request.ExpectedVersion == nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
			return
		}
		poll, err := handlers.service.UpdatePoll(r.Context(), scope, pollID, calendar.UpdateAvailabilityPollInput{
			CreateAvailabilityPollInput: request.input(), ExpectedVersion: *request.ExpectedVersion,
		})
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, poll)
	default:
		w.Header().Set("Allow", "GET, HEAD, PATCH")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Availability poll resources support GET and PATCH requests.")
	}
}

func (handlers availabilityPollHandlers) open(w http.ResponseWriter, r *http.Request) {
	handlers.versionCommand(w, r, handlers.service.OpenPoll)
}

func (handlers availabilityPollHandlers) close(w http.ResponseWriter, r *http.Request) {
	handlers.versionCommand(w, r, handlers.service.ClosePoll)
}

func (handlers availabilityPollHandlers) versionCommand(
	w http.ResponseWriter,
	r *http.Request,
	command func(context.Context, tenancy.Context, uuid.UUID, int64) (calendar.AvailabilityPoll, error),
) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "This availability poll command supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, scope, ok := handlers.pollMutationScope(w, r)
	if !ok {
		return
	}
	var request availabilityPollVersionRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
		request.ExpectedVersion == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	poll, err := command(r.Context(), scope, pollID, *request.ExpectedVersion)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, poll)
}

func (handlers availabilityPollHandlers) reopen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Poll reopen supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, scope, ok := handlers.pollMutationScope(w, r)
	if !ok {
		return
	}
	var request reopenAvailabilityPollRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
		request.ExpectedVersion == nil || request.DeadlineAt == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	poll, err := handlers.service.ReopenPoll(
		r.Context(), scope, pollID, *request.ExpectedVersion, *request.DeadlineAt,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, poll)
}

func (handlers availabilityPollHandlers) cancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Poll cancellation supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, scope, ok := handlers.pollMutationScope(w, r)
	if !ok {
		return
	}
	var request availabilityPollReasonRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
		request.ExpectedVersion == nil || request.Reason == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	poll, err := handlers.service.CancelPoll(
		r.Context(), scope, pollID, *request.ExpectedVersion, *request.Reason,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, poll)
}

func (handlers availabilityPollHandlers) respond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Poll responses support PUT requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, scope, ok := handlers.pollMutationScope(w, r)
	if !ok {
		return
	}
	var request respondAvailabilityPollRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollBodySize); err != nil ||
		request.ExpectedResponseVersion == nil || request.Answers == nil ||
		request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	result, err := handlers.service.Respond(r.Context(), scope, pollID, calendar.RespondAvailabilityPollInput{
		ExpectedResponseVersion: *request.ExpectedResponseVersion,
		Answers:                 *request.Answers, IdempotencyKey: *request.IdempotencyKey,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers availabilityPollHandlers) individualResponses(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Individual poll responses support GET requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, ok := parseResourceUUID(r.PathValue("poll_id"))
	if !ok {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollNotFound)
		return
	}
	_, scope, ok := handlers.authenticatedScope(w, r, false)
	if !ok {
		return
	}
	limit := 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
			return
		}
		limit = parsed
	}
	page, err := handlers.service.ListIndividualResponses(
		r.Context(), scope, pollID, calendar.ListAvailabilityPollResponsesInput{
			Cursor: strings.TrimSpace(r.URL.Query().Get("cursor")),
			Limit:  limit,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	if page.Responses == nil {
		page.Responses = []calendar.AvailabilityPollIndividualResponse{}
	}
	writeJSON(handlers.logger, w, http.StatusOK, page)
}

func (handlers availabilityPollHandlers) summary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Poll summary supports GET requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, ok := parseResourceUUID(r.PathValue("poll_id"))
	if !ok {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollNotFound)
		return
	}
	_, scope, ok := handlers.authenticatedScope(w, r, false)
	if !ok {
		return
	}
	result, err := handlers.service.Summary(r.Context(), scope, pollID)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers availabilityPollHandlers) finalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Poll finalize supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, scope, ok := handlers.pollMutationScope(w, r)
	if !ok {
		return
	}
	var request finalizeAvailabilityPollRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
		request.SlotID == nil || request.OutcomeType == nil ||
		request.ExpectedVersion == nil || request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	result, err := handlers.service.Finalize(r.Context(), scope, pollID, calendar.FinalizeAvailabilityPollInput{
		SlotID: *request.SlotID, OutcomeType: *request.OutcomeType, ClassID: request.ClassID,
		ExpectedVersion: *request.ExpectedVersion, IdempotencyKey: *request.IdempotencyKey,
	})
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers availabilityPollHandlers) capabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Poll capability creation supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, scope, ok := handlers.pollMutationScope(w, r)
	if !ok {
		return
	}
	var request createAvailabilityPollCapabilityRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
		request.Scope == nil || request.ExpiresAt == nil || request.ExpectedVersion == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	result, err := handlers.service.CreateCapability(
		r.Context(), scope, pollID, calendar.CreateAvailabilityPollCapabilityInput{
			Scope: *request.Scope, ParticipantID: request.ParticipantID,
			ExpiresAt: *request.ExpiresAt, ExpectedVersion: *request.ExpectedVersion,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusCreated, result)
}

func (handlers availabilityPollHandlers) revokeCapability(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Poll capability revocation supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	pollID, scope, ok := handlers.pollMutationScope(w, r)
	if !ok {
		return
	}
	capabilityID, ok := parseResourceUUID(r.PathValue("capability_id"))
	if !ok {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollNotFound)
		return
	}
	var request availabilityPollReasonRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
		request.ExpectedVersion == nil || request.Reason == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	result, err := handlers.service.RevokeCapability(
		r.Context(), scope, pollID, capabilityID, *request.ExpectedVersion, *request.Reason,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

type resolvePublicAvailabilityPollRequest struct {
	PublicID *uuid.UUID `json:"public_id"`
	Token    *string    `json:"token"`
}

type respondPublicAvailabilityPollRequest struct {
	PublicID                *uuid.UUID                              `json:"public_id"`
	ResponseToken           *string                                 `json:"response_token"`
	ExpectedResponseVersion *int64                                  `json:"expected_response_version"`
	Answers                 *[]calendar.AvailabilityPollAnswerInput `json:"answers"`
	IdempotencyKey          *string                                 `json:"idempotency_key"`
}

func (handlers availabilityPollHandlers) resolvePublic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Public poll resolution supports POST requests.")
		return
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollUnavailable)
		return
	}
	if !handlers.validPublicOrigin(r) {
		writeCodedProblem(w, r, http.StatusForbidden, "availability_poll_origin_forbidden", "Poll access denied", "Open the poll from the TutorHub availability page.")
		return
	}
	var request resolvePublicAvailabilityPollRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
		request.PublicID == nil || request.Token == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollCapabilityUnavailable)
		return
	}
	if !handlers.allowPublicPoll(
		w, r, *request.PublicID, *request.Token,
		InvitationRateLimitAvailabilityPollResolveIP,
		InvitationRateLimitAvailabilityPollResolveTokenDigest,
		InvitationRateLimitAvailabilityPollResolvePublicID,
	) {
		return
	}
	result, err := handlers.service.ResolvePublic(r.Context(), *request.PublicID, *request.Token)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, result)
}

func (handlers availabilityPollHandlers) respondPublic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Public poll responses support POST requests.")
		return
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollUnavailable)
		return
	}
	if !handlers.validPublicOrigin(r) {
		writeCodedProblem(w, r, http.StatusForbidden, "availability_poll_origin_forbidden", "Poll response denied", "Open the poll from the TutorHub availability page.")
		return
	}
	var request respondPublicAvailabilityPollRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollBodySize); err != nil ||
		request.PublicID == nil || request.ResponseToken == nil ||
		request.ExpectedResponseVersion == nil || request.Answers == nil ||
		request.IdempotencyKey == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollCapabilityUnavailable)
		return
	}
	if !handlers.allowPublicPoll(
		w, r, *request.PublicID, *request.ResponseToken,
		InvitationRateLimitAvailabilityPollRespondIP,
		InvitationRateLimitAvailabilityPollRespondTokenDigest,
		InvitationRateLimitAvailabilityPollRespondPublicID,
	) {
		return
	}
	result, err := handlers.service.RespondPublic(
		r.Context(), calendar.RespondPublicAvailabilityPollInput{
			PublicID: *request.PublicID, ResponseToken: *request.ResponseToken,
			ExpectedResponseVersion: *request.ExpectedResponseVersion,
			Answers:                 *request.Answers, IdempotencyKey: *request.IdempotencyKey,
		},
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, map[string]any{"poll": result})
}

func (handlers availabilityPollHandlers) validPublicOrigin(r *http.Request) bool {
	return handlers.webOrigin != "" &&
		canonicalPublicOrigin(r.Header.Get("Origin")) == handlers.webOrigin
}

func (handlers availabilityPollHandlers) allowPublicPoll(
	w http.ResponseWriter,
	r *http.Request,
	publicID uuid.UUID,
	rawToken string,
	ipAction InvitationRateLimitAction,
	tokenAction InvitationRateLimitAction,
	publicIDAction InvitationRateLimitAction,
) bool {
	clientPrefix := identity.IPPrefix(r.RemoteAddr)
	if clientPrefix == "" {
		clientPrefix = "unknown"
	}
	tokenDigest := sha256.Sum256([]byte(rawToken))
	now := handlers.clock().UTC()
	checks := []struct {
		action InvitationRateLimitAction
		bucket string
	}{
		{ipAction, "ip:" + clientPrefix},
		{tokenAction, "token:" + hex.EncodeToString(tokenDigest[:])},
		{publicIDAction, "public_id:" + publicID.String()},
	}
	for _, check := range checks {
		decision := handlers.limiter.Allow(r.Context(), check.action, check.bucket, now)
		if decision.Err != nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollUnavailable)
			return false
		}
		if !decision.Allowed {
			w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
			writeCodedProblem(w, r, http.StatusTooManyRequests, "availability_poll_rate_limited", "Too many poll requests", "Wait before trying this poll again.")
			return false
		}
	}
	return true
}

type createStudyMeetingRequest struct {
	ClassID        *uuid.UUID `json:"class_id"`
	Title          *string    `json:"title"`
	StartsAt       *time.Time `json:"starts_at"`
	EndsAt         *time.Time `json:"ends_at"`
	Timezone       *string    `json:"timezone"`
	IdempotencyKey *string    `json:"idempotency_key"`
}

type updateStudyMeetingRequest struct {
	ClassID         *uuid.UUID `json:"class_id"`
	Title           *string    `json:"title"`
	StartsAt        *time.Time `json:"starts_at"`
	EndsAt          *time.Time `json:"ends_at"`
	Timezone        *string    `json:"timezone"`
	ExpectedVersion *int64     `json:"expected_version"`
}

func (handlers availabilityPollHandlers) studyMeetingCollection(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		_, scope, ok := handlers.authenticatedScope(w, r, false)
		if !ok {
			return
		}
		input, ok := handlers.studyMeetingListInput(w, r)
		if !ok {
			return
		}
		meetings, err := handlers.service.ListStudyMeetings(r.Context(), scope, input)
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		if meetings == nil {
			meetings = []calendar.StudyMeeting{}
		}
		writeJSON(handlers.logger, w, http.StatusOK, map[string]any{"meetings": meetings})
	case http.MethodPost:
		_, scope, ok := handlers.authenticatedScope(w, r, true)
		if !ok {
			return
		}
		var request createStudyMeetingRequest
		if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
			request.Title == nil || request.StartsAt == nil || request.EndsAt == nil ||
			request.Timezone == nil || request.IdempotencyKey == nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
			return
		}
		meeting, err := handlers.service.CreateStudyMeeting(r.Context(), scope, calendar.CreateStudyMeetingInput{
			ClassID: request.ClassID, Title: *request.Title, StartsAt: *request.StartsAt,
			EndsAt: *request.EndsAt, Timezone: *request.Timezone,
			IdempotencyKey: *request.IdempotencyKey,
		})
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusCreated, meeting)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Study meetings support GET and POST requests.")
	}
}

func (handlers availabilityPollHandlers) studyMeetingResource(w http.ResponseWriter, r *http.Request) {
	if !handlers.available(w, r) {
		return
	}
	meetingID, ok := parseResourceUUID(r.PathValue("meeting_id"))
	if !ok {
		handlers.writeProblem(w, r, calendar.ErrStudyMeetingNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		_, scope, ok := handlers.authenticatedScope(w, r, false)
		if !ok {
			return
		}
		meeting, err := handlers.service.GetStudyMeeting(r.Context(), scope, meetingID)
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, meeting)
	case http.MethodPatch:
		_, scope, ok := handlers.authenticatedScope(w, r, true)
		if !ok {
			return
		}
		var request updateStudyMeetingRequest
		if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
			request.Title == nil || request.StartsAt == nil || request.EndsAt == nil ||
			request.Timezone == nil || request.ExpectedVersion == nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
			return
		}
		meeting, err := handlers.service.UpdateStudyMeeting(r.Context(), scope, meetingID, calendar.UpdateStudyMeetingInput{
			ClassID: request.ClassID, Title: *request.Title, StartsAt: *request.StartsAt,
			EndsAt: *request.EndsAt, Timezone: *request.Timezone,
			ExpectedVersion: *request.ExpectedVersion,
		})
		if err != nil {
			handlers.writeProblem(w, r, err)
			return
		}
		writeJSON(handlers.logger, w, http.StatusOK, meeting)
	default:
		w.Header().Set("Allow", "GET, HEAD, PATCH")
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Study meeting resources support GET and PATCH requests.")
	}
}

func (handlers availabilityPollHandlers) cancelStudyMeeting(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeProblem(w, r, http.StatusMethodNotAllowed, "Method not allowed", "Study meeting cancellation supports POST requests.")
		return
	}
	if !handlers.available(w, r) {
		return
	}
	meetingID, ok := parseResourceUUID(r.PathValue("meeting_id"))
	if !ok {
		handlers.writeProblem(w, r, calendar.ErrStudyMeetingNotFound)
		return
	}
	_, scope, ok := handlers.authenticatedScope(w, r, true)
	if !ok {
		return
	}
	var request availabilityPollReasonRequest
	if err := decodeJSONRequest(w, r, &request, maximumAvailabilityPollCommandBodySize); err != nil ||
		request.ExpectedVersion == nil || request.Reason == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
		return
	}
	meeting, err := handlers.service.CancelStudyMeeting(
		r.Context(), scope, meetingID, *request.ExpectedVersion, *request.Reason,
	)
	if err != nil {
		handlers.writeProblem(w, r, err)
		return
	}
	writeJSON(handlers.logger, w, http.StatusOK, meeting)
}

func (handlers availabilityPollHandlers) authenticatedScope(
	w http.ResponseWriter,
	r *http.Request,
	csrf bool,
) (identity.Principal, tenancy.Context, bool) {
	if !handlers.auth.available(w, r) {
		return identity.Principal{}, tenancy.Context{}, false
	}
	var principal identity.Principal
	var ok bool
	if csrf {
		sessionToken, sessionOK := handlers.auth.sessionToken(w, r)
		if !sessionOK {
			return identity.Principal{}, tenancy.Context{}, false
		}
		principal, ok = handlers.auth.csrfPrincipal(w, r, sessionToken)
	} else {
		principal, ok = handlers.auth.authenticatedPrincipal(w, r)
	}
	if !ok || principal.ActiveTenant == nil {
		writeProblem(w, r, http.StatusForbidden, "Workspace access denied", "The active workspace is unavailable.")
		return identity.Principal{}, tenancy.Context{}, false
	}
	scope, err := tenancy.New(principal.ActiveTenant.ID, principal.User.ID)
	if err != nil {
		writeProblem(w, r, http.StatusForbidden, "Workspace access denied", "The active workspace is unavailable.")
		return identity.Principal{}, tenancy.Context{}, false
	}
	expectedTenantID, valid := parseResourceUUID(strings.TrimSpace(r.Header.Get(calendarTenantHeader)))
	if !valid {
		writeProblem(w, r, http.StatusBadRequest, "Invalid tenant scope", "The active workspace assertion is required.")
		return identity.Principal{}, tenancy.Context{}, false
	}
	if expectedTenantID != scope.TenantID {
		writeCodedProblem(w, r, http.StatusConflict, "calendar_scope_changed", "Active workspace changed", "Reload the current workspace before retrying.")
		return identity.Principal{}, tenancy.Context{}, false
	}
	return principal, scope, true
}

func (handlers availabilityPollHandlers) pollMutationScope(
	w http.ResponseWriter,
	r *http.Request,
) (uuid.UUID, tenancy.Context, bool) {
	pollID, ok := parseResourceUUID(r.PathValue("poll_id"))
	if !ok {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollNotFound)
		return uuid.Nil, tenancy.Context{}, false
	}
	_, scope, ok := handlers.authenticatedScope(w, r, true)
	return pollID, scope, ok
}

func (handlers availabilityPollHandlers) studyMeetingListInput(
	w http.ResponseWriter,
	r *http.Request,
) (calendar.ListStudyMeetingsInput, bool) {
	input := calendar.ListStudyMeetingsInput{}
	if raw := strings.TrimSpace(r.URL.Query().Get("from")); raw != "" {
		value, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
			return calendar.ListStudyMeetingsInput{}, false
		}
		input.From = &value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("to")); raw != "" {
		value, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
			return calendar.ListStudyMeetingsInput{}, false
		}
		input.To = &value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			handlers.writeProblem(w, r, calendar.ErrAvailabilityPollInvalid)
			return calendar.ListStudyMeetingsInput{}, false
		}
		input.Limit = value
	}
	return input, true
}

func (handlers availabilityPollHandlers) available(w http.ResponseWriter, r *http.Request) bool {
	if !handlers.auth.available(w, r) && r.URL.Path != availabilityPollResolvePath &&
		r.URL.Path != availabilityPollPublicRespondPath {
		return false
	}
	if handlers.service == nil {
		handlers.writeProblem(w, r, calendar.ErrAvailabilityPollUnavailable)
		return false
	}
	return true
}

func (handlers availabilityPollHandlers) writeProblem(
	w http.ResponseWriter,
	r *http.Request,
	err error,
) {
	if writeFeatureControlEnforcementProblem(w, r, err) {
		return
	}
	status := http.StatusInternalServerError
	code := "availability_poll_failed"
	title := "Availability poll request failed"
	detail := "The availability poll request could not be completed safely."
	switch {
	case errors.Is(err, calendar.ErrAvailabilityPollInvalid):
		status, code = http.StatusBadRequest, "availability_poll_request_invalid"
		title, detail = "Invalid availability poll request", "Review the bounded poll dates, slots, participants, and optimistic version."
	case errors.Is(err, calendar.ErrAvailabilityPollAccessDenied):
		status, code = http.StatusForbidden, "availability_poll_forbidden"
		title, detail = "Availability poll access denied", "The active workspace cannot authorize this operation."
	case errors.Is(err, calendar.ErrAvailabilityPollNotFound),
		errors.Is(err, calendar.ErrAvailabilityPollCapabilityUnavailable),
		errors.Is(err, calendar.ErrStudyMeetingNotFound):
		status, code = http.StatusNotFound, "availability_poll_not_found"
		title, detail = "Availability poll unavailable", "The requested resource is unavailable in the active workspace."
	case errors.Is(err, calendar.ErrAvailabilityPollClosed):
		status, code = http.StatusGone, "availability_poll_closed"
		title, detail = "Availability poll is closed", "This poll no longer accepts responses."
	case errors.Is(err, calendar.ErrAvailabilityPollConflict),
		errors.Is(err, calendar.ErrAvailabilityPollIdempotencyConflict),
		errors.Is(err, calendar.ErrStudyMeetingConflict),
		errors.Is(err, calendar.ErrClassSessionOutcomeConflict):
		status, code = http.StatusConflict, "availability_poll_conflict"
		title, detail = "Availability poll changed", "Reload the latest resource before retrying."
	case errors.Is(err, calendar.ErrAvailabilityPollCapacityExceeded):
		status, code = http.StatusConflict, "availability_poll_capacity_exceeded"
		title, detail = "Availability poll capacity reached", "Archive or rotate expired poll access before creating another link."
	case errors.Is(err, calendar.ErrAvailabilityPollUnavailable), errors.Is(err, context.DeadlineExceeded):
		status, code = http.StatusServiceUnavailable, "availability_poll_unavailable"
		title, detail = "Availability poll service unavailable", "Try again later."
	default:
		handlers.logger.Error(
			"availability poll request failed",
			"request_id", RequestIDFromContext(r.Context()),
			"path", logsafe.String(r.URL.Path),
			"error", logsafe.Error(err),
		)
	}
	writeCodedProblem(w, r, status, code, title, detail)
}
