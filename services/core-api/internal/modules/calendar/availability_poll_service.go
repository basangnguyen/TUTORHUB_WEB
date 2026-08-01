package calendar

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	maximumPollTitleRunes             = 200
	maximumPollDescriptionRunes       = 4000
	maximumPollSlots                  = 1000
	maximumPollParticipants           = 500
	maximumPollResponseSessions       = 500
	maximumPollResponseSessionHistory = maximumPollParticipants * 2
	maximumPollAccessCapabilities     = maximumPollParticipants * 2
	maximumPollSlotHistory            = maximumPollSlots * 10
	maximumPollParticipantHistory     = maximumPollParticipants * 10
	maximumPollResponseVersion        = 100
	maximumPollRangeDays              = 90
	maximumPollCapabilityTTL          = 30 * 24 * time.Hour
	maximumStudyMeetingDuration       = 24 * time.Hour
)

var pollIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$`)

type AvailabilityPollService struct {
	repository AvailabilityPollRepository
	clock      func() time.Time
}

func NewAvailabilityPollService(
	repository AvailabilityPollRepository,
	clock func() time.Time,
) (*AvailabilityPollService, error) {
	if repository == nil {
		return nil, fmt.Errorf("availability poll repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &AvailabilityPollService{repository: repository, clock: clock}, nil
}

func (service *AvailabilityPollService) ListPolls(
	ctx context.Context,
	scope tenancy.Context,
	input ListAvailabilityPollsInput,
) ([]AvailabilityPoll, error) {
	if err := scope.Validate(); err != nil {
		return nil, ErrAvailabilityPollAccessDenied
	}
	input.Status = strings.TrimSpace(input.Status)
	if input.Status != "" && !validPollStatus(input.Status) {
		return nil, ErrAvailabilityPollInvalid
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 100 {
		return nil, ErrAvailabilityPollInvalid
	}
	return service.repository.ListPolls(ctx, scope, input)
}

func (service *AvailabilityPollService) CreatePoll(
	ctx context.Context,
	scope tenancy.Context,
	input CreateAvailabilityPollInput,
) (AvailabilityPoll, error) {
	if err := scope.Validate(); err != nil {
		return AvailabilityPoll{}, ErrAvailabilityPollAccessDenied
	}
	normalized, err := normalizeAvailabilityPollInput(input, service.clock().UTC(), true)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	return service.repository.CreatePoll(ctx, scope, normalized)
}

func (service *AvailabilityPollService) GetPoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
) (AvailabilityPoll, error) {
	if err := validatePollScopeID(scope, pollID); err != nil {
		return AvailabilityPoll{}, err
	}
	return service.repository.GetPoll(ctx, scope, pollID)
}

func (service *AvailabilityPollService) UpdatePoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input UpdateAvailabilityPollInput,
) (AvailabilityPoll, error) {
	if err := validatePollScopeID(scope, pollID); err != nil || input.ExpectedVersion < 1 {
		return AvailabilityPoll{}, ErrAvailabilityPollInvalid
	}
	normalized, err := normalizeAvailabilityPollInput(
		input.CreateAvailabilityPollInput,
		service.clock().UTC(),
		false,
	)
	if err != nil {
		return AvailabilityPoll{}, err
	}
	input.CreateAvailabilityPollInput = normalized
	return service.repository.UpdatePoll(ctx, scope, pollID, input)
}

func (service *AvailabilityPollService) OpenPoll(
	ctx context.Context, scope tenancy.Context, pollID uuid.UUID, expectedVersion int64,
) (AvailabilityPoll, error) {
	if err := validatePollVersion(scope, pollID, expectedVersion); err != nil {
		return AvailabilityPoll{}, err
	}
	return service.repository.OpenPoll(ctx, scope, pollID, expectedVersion)
}

func (service *AvailabilityPollService) ClosePoll(
	ctx context.Context, scope tenancy.Context, pollID uuid.UUID, expectedVersion int64,
) (AvailabilityPoll, error) {
	if err := validatePollVersion(scope, pollID, expectedVersion); err != nil {
		return AvailabilityPoll{}, err
	}
	return service.repository.ClosePoll(ctx, scope, pollID, expectedVersion)
}

func (service *AvailabilityPollService) ReopenPoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	expectedVersion int64,
	deadlineAt time.Time,
) (AvailabilityPoll, error) {
	if err := validatePollVersion(scope, pollID, expectedVersion); err != nil ||
		deadlineAt.IsZero() || !deadlineAt.After(service.clock().UTC()) {
		return AvailabilityPoll{}, ErrAvailabilityPollInvalid
	}
	return service.repository.ReopenPoll(ctx, scope, pollID, expectedVersion, deadlineAt.UTC())
}

func (service *AvailabilityPollService) CancelPoll(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	expectedVersion int64,
	reason string,
) (AvailabilityPoll, error) {
	if err := validatePollVersion(scope, pollID, expectedVersion); err != nil ||
		!validMutationReason(reason) {
		return AvailabilityPoll{}, ErrAvailabilityPollInvalid
	}
	return service.repository.CancelPoll(
		ctx, scope, pollID, expectedVersion, strings.TrimSpace(reason),
	)
}

func (service *AvailabilityPollService) Respond(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input RespondAvailabilityPollInput,
) (AvailabilityPollMutationResponse, error) {
	if err := validatePollScopeID(scope, pollID); err != nil ||
		input.ExpectedResponseVersion < 0 ||
		input.ExpectedResponseVersion > maximumPollResponseVersion ||
		!pollIdempotencyPattern.MatchString(input.IdempotencyKey) {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollInvalid
	}
	answers, err := normalizePollAnswers(input.Answers)
	if err != nil {
		return AvailabilityPollMutationResponse{}, err
	}
	input.Answers = answers
	return service.repository.Respond(ctx, scope, pollID, input)
}

func (service *AvailabilityPollService) ListIndividualResponses(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input ListAvailabilityPollResponsesInput,
) (AvailabilityPollIndividualResponsePage, error) {
	input.Cursor = strings.TrimSpace(input.Cursor)
	if err := validatePollScopeID(scope, pollID); err != nil ||
		len(input.Cursor) > maximumPollResponseCursorLength ||
		input.Limit < 0 || input.Limit > maximumIndividualResponseListLimit {
		return AvailabilityPollIndividualResponsePage{}, ErrAvailabilityPollInvalid
	}
	return service.repository.ListIndividualResponses(ctx, scope, pollID, input)
}

func (service *AvailabilityPollService) Summary(
	ctx context.Context, scope tenancy.Context, pollID uuid.UUID,
) (AvailabilityPollSummary, error) {
	if err := validatePollScopeID(scope, pollID); err != nil {
		return AvailabilityPollSummary{}, err
	}
	return service.repository.Summary(ctx, scope, pollID)
}

func (service *AvailabilityPollService) Finalize(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input FinalizeAvailabilityPollInput,
) (AvailabilityPollMutationResponse, error) {
	if err := validatePollVersion(scope, pollID, input.ExpectedVersion); err != nil ||
		input.SlotID == uuid.Nil || !pollIdempotencyPattern.MatchString(input.IdempotencyKey) {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollInvalid
	}
	if input.OutcomeType != PollOutcomeClassSession && input.OutcomeType != PollOutcomeStudyMeeting {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollInvalid
	}
	if input.OutcomeType == PollOutcomeClassSession && input.ClassID == nil {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollInvalid
	}
	if input.OutcomeType == PollOutcomeStudyMeeting && input.ClassID != nil {
		return AvailabilityPollMutationResponse{}, ErrAvailabilityPollInvalid
	}
	return service.repository.Finalize(ctx, scope, pollID, input)
}

func (service *AvailabilityPollService) CreateCapability(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	input CreateAvailabilityPollCapabilityInput,
) (AvailabilityPollCapabilitySecret, error) {
	now := service.clock().UTC()
	if err := validatePollVersion(scope, pollID, input.ExpectedVersion); err != nil ||
		input.ExpiresAt.IsZero() || !input.ExpiresAt.After(now) ||
		input.ExpiresAt.After(now.Add(maximumPollCapabilityTTL)) {
		return AvailabilityPollCapabilitySecret{}, ErrAvailabilityPollInvalid
	}
	if (input.Scope == PollCapabilityInvited && input.ParticipantID == nil) ||
		(input.Scope == PollCapabilityPublic && input.ParticipantID != nil) {
		return AvailabilityPollCapabilitySecret{}, ErrAvailabilityPollInvalid
	}
	return service.repository.CreateCapability(ctx, scope, pollID, input)
}

func (service *AvailabilityPollService) RevokeCapability(
	ctx context.Context,
	scope tenancy.Context,
	pollID uuid.UUID,
	capabilityID uuid.UUID,
	expectedVersion int64,
	reason string,
) (AvailabilityPollCapability, error) {
	if err := validatePollVersion(scope, pollID, expectedVersion); err != nil ||
		capabilityID == uuid.Nil || !validMutationReason(reason) {
		return AvailabilityPollCapability{}, ErrAvailabilityPollInvalid
	}
	return service.repository.RevokeCapability(
		ctx, scope, pollID, capabilityID, expectedVersion, strings.TrimSpace(reason),
	)
}

func (service *AvailabilityPollService) ResolvePublic(
	ctx context.Context,
	publicID uuid.UUID,
	rawToken string,
) (PublicAvailabilityPollExchange, error) {
	if publicID == uuid.Nil || rawToken == "" || rawToken != strings.TrimSpace(rawToken) {
		return PublicAvailabilityPollExchange{}, ErrAvailabilityPollCapabilityUnavailable
	}
	return service.repository.ResolvePublic(ctx, publicID, rawToken)
}

func (service *AvailabilityPollService) RespondPublic(
	ctx context.Context,
	input RespondPublicAvailabilityPollInput,
) (PublicAvailabilityPoll, error) {
	if input.PublicID == uuid.Nil || input.ResponseToken == "" ||
		input.ResponseToken != strings.TrimSpace(input.ResponseToken) ||
		input.ExpectedResponseVersion < 0 ||
		input.ExpectedResponseVersion > maximumPollResponseVersion ||
		!pollIdempotencyPattern.MatchString(input.IdempotencyKey) {
		return PublicAvailabilityPoll{}, ErrAvailabilityPollCapabilityUnavailable
	}
	answers, err := normalizePollAnswers(input.Answers)
	if err != nil {
		return PublicAvailabilityPoll{}, err
	}
	input.Answers = answers
	return service.repository.RespondPublic(ctx, input)
}

func (service *AvailabilityPollService) ListStudyMeetings(
	ctx context.Context,
	scope tenancy.Context,
	input ListStudyMeetingsInput,
) ([]StudyMeeting, error) {
	if err := scope.Validate(); err != nil {
		return nil, ErrAvailabilityPollAccessDenied
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Limit < 1 || input.Limit > 100 ||
		(input.From != nil && input.To != nil && !input.To.After(*input.From)) {
		return nil, ErrAvailabilityPollInvalid
	}
	return service.repository.ListStudyMeetings(ctx, scope, input)
}

func (service *AvailabilityPollService) CreateStudyMeeting(
	ctx context.Context,
	scope tenancy.Context,
	input CreateStudyMeetingInput,
) (StudyMeeting, error) {
	normalized, err := normalizeStudyMeetingInput(scope, input)
	if err != nil {
		return StudyMeeting{}, err
	}
	return service.repository.CreateStudyMeeting(ctx, scope, normalized)
}

func (service *AvailabilityPollService) GetStudyMeeting(
	ctx context.Context, scope tenancy.Context, meetingID uuid.UUID,
) (StudyMeeting, error) {
	if err := validatePollScopeID(scope, meetingID); err != nil {
		return StudyMeeting{}, ErrStudyMeetingNotFound
	}
	return service.repository.GetStudyMeeting(ctx, scope, meetingID)
}

func (service *AvailabilityPollService) UpdateStudyMeeting(
	ctx context.Context,
	scope tenancy.Context,
	meetingID uuid.UUID,
	input UpdateStudyMeetingInput,
) (StudyMeeting, error) {
	if meetingID == uuid.Nil || input.ExpectedVersion < 1 {
		return StudyMeeting{}, ErrAvailabilityPollInvalid
	}
	normalized, err := normalizeStudyMeetingInput(scope, CreateStudyMeetingInput{
		ClassID: input.ClassID, Title: input.Title, StartsAt: input.StartsAt,
		EndsAt: input.EndsAt, Timezone: input.Timezone, IdempotencyKey: "update000",
	})
	if err != nil {
		return StudyMeeting{}, err
	}
	input.ClassID, input.Title, input.StartsAt, input.EndsAt, input.Timezone =
		normalized.ClassID, normalized.Title, normalized.StartsAt, normalized.EndsAt, normalized.Timezone
	return service.repository.UpdateStudyMeeting(ctx, scope, meetingID, input)
}

func (service *AvailabilityPollService) CancelStudyMeeting(
	ctx context.Context,
	scope tenancy.Context,
	meetingID uuid.UUID,
	expectedVersion int64,
	reason string,
) (StudyMeeting, error) {
	if err := validatePollVersion(scope, meetingID, expectedVersion); err != nil ||
		!validMutationReason(reason) {
		return StudyMeeting{}, ErrAvailabilityPollInvalid
	}
	return service.repository.CancelStudyMeeting(
		ctx, scope, meetingID, expectedVersion, strings.TrimSpace(reason),
	)
}

func normalizeAvailabilityPollInput(
	input CreateAvailabilityPollInput,
	now time.Time,
	requireIdempotency bool,
) (CreateAvailabilityPollInput, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.Timezone = strings.TrimSpace(input.Timezone)
	input.RangeStart = strings.TrimSpace(input.RangeStart)
	input.RangeEnd = strings.TrimSpace(input.RangeEnd)
	input.WorkingDayStart = strings.TrimSpace(input.WorkingDayStart)
	input.WorkingDayEnd = strings.TrimSpace(input.WorkingDayEnd)
	input.ShareMode = strings.TrimSpace(input.ShareMode)
	if !utf8.ValidString(input.Title) || utf8.RuneCountInString(input.Title) < 1 ||
		utf8.RuneCountInString(input.Title) > maximumPollTitleRunes ||
		!utf8.ValidString(input.Description) ||
		utf8.RuneCountInString(input.Description) > maximumPollDescriptionRunes ||
		len(input.Timezone) < 1 || len(input.Timezone) > 100 ||
		strings.EqualFold(input.Timezone, "local") {
		return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
	}
	location, err := time.LoadLocation(input.Timezone)
	if err != nil {
		return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
	}
	rangeStart, err := time.Parse("2006-01-02", input.RangeStart)
	if err != nil {
		return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
	}
	rangeEnd, err := time.Parse("2006-01-02", input.RangeEnd)
	if err != nil || rangeEnd.Before(rangeStart) ||
		int(rangeEnd.Sub(rangeStart)/(24*time.Hour))+1 > maximumPollRangeDays {
		return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
	}
	workingStart, normalizedStart, err := parsePollCivilTime(input.WorkingDayStart)
	if err != nil {
		return CreateAvailabilityPollInput{}, err
	}
	workingEnd, normalizedEnd, err := parsePollCivilTime(input.WorkingDayEnd)
	if err != nil || workingEnd <= workingStart {
		return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
	}
	input.WorkingDayStart, input.WorkingDayEnd = normalizedStart, normalizedEnd
	if input.DurationMinutes < 15 || input.DurationMinutes > 480 ||
		(input.SlotGranularityMinutes != 15 && input.SlotGranularityMinutes != 30 &&
			input.SlotGranularityMinutes != 60) ||
		input.DeadlineAt.IsZero() || !input.DeadlineAt.After(now) ||
		!validPollShareMode(input.ShareMode) ||
		(input.ShareMode == PollShareClassMembers && input.ClassID == nil) ||
		(input.ClassID != nil && *input.ClassID == uuid.Nil) ||
		len(input.Slots) < 1 || len(input.Slots) > maximumPollSlots ||
		len(input.Participants) > maximumPollParticipants ||
		(requireIdempotency && !pollIdempotencyPattern.MatchString(input.IdempotencyKey)) {
		return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
	}
	seenSlots := make(map[string]struct{}, len(input.Slots))
	for index := range input.Slots {
		slot := &input.Slots[index]
		slot.StartsAt, slot.EndsAt = slot.StartsAt.UTC(), slot.EndsAt.UTC()
		if slot.StartsAt.IsZero() || slot.EndsAt.IsZero() ||
			slot.EndsAt.Sub(slot.StartsAt) != time.Duration(input.DurationMinutes)*time.Minute {
			return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
		}
		localStart, localEnd := slot.StartsAt.In(location), slot.EndsAt.In(location)
		localDate := localStart.Format("2006-01-02")
		if localDate < input.RangeStart || localDate > input.RangeEnd ||
			localEnd.Format("2006-01-02") != localDate || localStart.Second() != 0 ||
			localStart.Nanosecond() != 0 || localStart.Minute()%input.SlotGranularityMinutes != 0 ||
			localStart.Hour()*60+localStart.Minute() < workingStart ||
			localEnd.Hour()*60+localEnd.Minute() > workingEnd {
			return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
		}
		key := slot.StartsAt.Format(time.RFC3339Nano) + "/" + slot.EndsAt.Format(time.RFC3339Nano)
		if _, duplicate := seenSlots[key]; duplicate {
			return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
		}
		seenSlots[key] = struct{}{}
	}
	sort.Slice(input.Slots, func(left, right int) bool {
		return input.Slots[left].StartsAt.Before(input.Slots[right].StartsAt)
	})
	seenUsers := make(map[uuid.UUID]struct{}, len(input.Participants))
	for _, participant := range input.Participants {
		switch participant.Kind {
		case PollParticipantInternal:
			if participant.InternalUserID == nil || *participant.InternalUserID == uuid.Nil {
				return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
			}
			if _, duplicate := seenUsers[*participant.InternalUserID]; duplicate {
				return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
			}
			seenUsers[*participant.InternalUserID] = struct{}{}
		case PollParticipantExternal:
			if participant.InternalUserID != nil {
				return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
			}
		default:
			return CreateAvailabilityPollInput{}, ErrAvailabilityPollInvalid
		}
	}
	input.DeadlineAt = input.DeadlineAt.UTC()
	return input, nil
}

func normalizePollAnswers(
	answers []AvailabilityPollAnswerInput,
) ([]AvailabilityPollAnswerInput, error) {
	if len(answers) < 1 || len(answers) > maximumPollSlots {
		return nil, ErrAvailabilityPollInvalid
	}
	seen := make(map[uuid.UUID]struct{}, len(answers))
	for _, answer := range answers {
		if answer.SlotID == uuid.Nil ||
			(answer.State != PollAnswerPreferred && answer.State != PollAnswerAvailable &&
				answer.State != PollAnswerUnavailable) {
			return nil, ErrAvailabilityPollInvalid
		}
		if _, duplicate := seen[answer.SlotID]; duplicate {
			return nil, ErrAvailabilityPollInvalid
		}
		seen[answer.SlotID] = struct{}{}
	}
	sort.Slice(answers, func(left, right int) bool {
		return answers[left].SlotID.String() < answers[right].SlotID.String()
	})
	return answers, nil
}

func normalizeStudyMeetingInput(
	scope tenancy.Context,
	input CreateStudyMeetingInput,
) (CreateStudyMeetingInput, error) {
	if err := scope.Validate(); err != nil {
		return CreateStudyMeetingInput{}, ErrAvailabilityPollAccessDenied
	}
	input.Title = strings.TrimSpace(input.Title)
	input.Timezone = strings.TrimSpace(input.Timezone)
	input.StartsAt, input.EndsAt = input.StartsAt.UTC(), input.EndsAt.UTC()
	if utf8.RuneCountInString(input.Title) < 1 ||
		utf8.RuneCountInString(input.Title) > maximumPollTitleRunes ||
		input.StartsAt.IsZero() || input.EndsAt.IsZero() ||
		!input.EndsAt.After(input.StartsAt) ||
		input.EndsAt.Sub(input.StartsAt) > maximumStudyMeetingDuration ||
		len(input.Timezone) < 1 || len(input.Timezone) > 100 ||
		strings.EqualFold(input.Timezone, "local") ||
		!pollIdempotencyPattern.MatchString(input.IdempotencyKey) ||
		(input.ClassID != nil && *input.ClassID == uuid.Nil) {
		return CreateStudyMeetingInput{}, ErrAvailabilityPollInvalid
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return CreateStudyMeetingInput{}, ErrAvailabilityPollInvalid
	}
	return input, nil
}

func parsePollCivilTime(value string) (int, string, error) {
	for _, layout := range []string{"15:04:05", "15:04"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.Hour()*60 + parsed.Minute(), parsed.Format("15:04:05"), nil
		}
	}
	return 0, "", ErrAvailabilityPollInvalid
}

func validatePollScopeID(scope tenancy.Context, id uuid.UUID) error {
	if err := scope.Validate(); err != nil {
		return ErrAvailabilityPollAccessDenied
	}
	if id == uuid.Nil {
		return ErrAvailabilityPollNotFound
	}
	return nil
}

func validatePollVersion(scope tenancy.Context, id uuid.UUID, expectedVersion int64) error {
	if err := validatePollScopeID(scope, id); err != nil {
		return err
	}
	if expectedVersion < 1 {
		return ErrAvailabilityPollInvalid
	}
	return nil
}

func validMutationReason(reason string) bool {
	trimmed := strings.TrimSpace(reason)
	return utf8.ValidString(trimmed) && utf8.RuneCountInString(trimmed) >= 1 &&
		utf8.RuneCountInString(trimmed) <= 256 && len(trimmed) <= 256
}

func validPollStatus(status string) bool {
	switch status {
	case PollStatusDraft, PollStatusOpen, PollStatusClosed, PollStatusFinalized, PollStatusCancelled:
		return true
	default:
		return false
	}
}

func validPollShareMode(mode string) bool {
	return mode == PollShareClassMembers || mode == PollShareInvitedOnly ||
		mode == PollShareAnyoneWithLink
}

var _ AvailabilityPollServiceAPI = (*AvailabilityPollService)(nil)
