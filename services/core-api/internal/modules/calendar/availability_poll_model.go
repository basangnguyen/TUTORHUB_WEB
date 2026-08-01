package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	PollShareClassMembers   = "class_members"
	PollShareInvitedOnly    = "invited_only"
	PollShareAnyoneWithLink = "anyone_with_link"

	PollStatusDraft     = "draft"
	PollStatusOpen      = "open"
	PollStatusClosed    = "closed"
	PollStatusFinalized = "finalized"
	PollStatusCancelled = "cancelled"

	PollAnswerPreferred   = "preferred"
	PollAnswerAvailable   = "available"
	PollAnswerUnavailable = "unavailable"

	PollParticipantInternal = "internal_user"
	PollParticipantExternal = "external_invitee"

	PollParticipantActive  = "active"
	PollParticipantRevoked = "revoked"

	PollCapabilityInvited = "invited_participant"
	PollCapabilityPublic  = "public_link"

	PollOutcomeClassSession = "class_session"
	PollOutcomeStudyMeeting = "study_meeting"

	StudyMeetingScheduled = "scheduled"
	StudyMeetingCancelled = "cancelled"
)

var (
	ErrAvailabilityPollInvalid               = errors.New("invalid availability poll request")
	ErrAvailabilityPollAccessDenied          = errors.New("availability poll access denied")
	ErrAvailabilityPollNotFound              = errors.New("availability poll not found")
	ErrAvailabilityPollConflict              = errors.New("availability poll version or state conflict")
	ErrAvailabilityPollClosed                = errors.New("availability poll no longer accepts responses")
	ErrAvailabilityPollIdempotencyConflict   = errors.New("availability poll idempotency conflict")
	ErrAvailabilityPollCapabilityUnavailable = errors.New("availability poll capability unavailable")
	ErrAvailabilityPollCapacityExceeded      = errors.New("availability poll capacity exceeded")
	ErrAvailabilityPollUnavailable           = errors.New("availability poll service unavailable")
	ErrStudyMeetingNotFound                  = errors.New("study meeting not found")
	ErrStudyMeetingConflict                  = errors.New("study meeting conflict")
)

type AvailabilityPollSlotInput struct {
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type AvailabilityPollSlot struct {
	ID       uuid.UUID `json:"id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Ordinal  int       `json:"ordinal"`
}

type AvailabilityPollParticipantInput struct {
	Kind           string     `json:"kind"`
	InternalUserID *uuid.UUID `json:"internal_user_id"`
}

type AvailabilityPollParticipant struct {
	ID             uuid.UUID  `json:"id"`
	Kind           string     `json:"kind"`
	InternalUserID *uuid.UUID `json:"internal_user_id"`
	Status         string     `json:"status"`
	HasResponded   bool       `json:"has_responded"`
}

type AvailabilityPollAnswerInput struct {
	SlotID uuid.UUID `json:"slot_id"`
	State  string    `json:"state"`
}

type AvailabilityPollAnswer struct {
	SlotID uuid.UUID `json:"slot_id"`
	State  string    `json:"state"`
}

type AvailabilityPollResponseProjection struct {
	Version     int64                    `json:"version"`
	Answers     []AvailabilityPollAnswer `json:"answers"`
	SubmittedAt time.Time                `json:"submitted_at"`
}

type AvailabilityPollIndividualResponse struct {
	ResponseID     uuid.UUID                `json:"response_id"`
	ParticipantID  *uuid.UUID               `json:"participant_id"`
	InternalUserID *uuid.UUID               `json:"internal_user_id"`
	ActorType      string                   `json:"actor_type"`
	Version        int64                    `json:"version"`
	Answers        []AvailabilityPollAnswer `json:"answers"`
	SubmittedAt    time.Time                `json:"submitted_at"`
	createdAt      time.Time
}

type ListAvailabilityPollResponsesInput struct {
	Cursor string
	Limit  int
}

type AvailabilityPollIndividualResponsePage struct {
	Responses  []AvailabilityPollIndividualResponse `json:"responses"`
	NextCursor *string                              `json:"next_cursor"`
}

type AvailabilityPollViewerCapabilities struct {
	CanManage               bool `json:"can_manage"`
	CanRespond              bool `json:"can_respond"`
	CanShare                bool `json:"can_share"`
	CanFinalizeClassSession bool `json:"can_finalize_class_session"`
	CanFinalizeStudyMeeting bool `json:"can_finalize_study_meeting"`
	CanViewExactAggregate   bool `json:"can_view_exact_aggregate"`
	CanViewIndividual       bool `json:"can_view_individual_responses"`
}

type AvailabilityPollOutcomeReference struct {
	Type string    `json:"type"`
	ID   uuid.UUID `json:"id"`
}

type AvailabilityPoll struct {
	ID                     uuid.UUID                           `json:"id"`
	PublicID               uuid.UUID                           `json:"public_id"`
	ClassID                *uuid.UUID                          `json:"class_id"`
	OwnerUserID            uuid.UUID                           `json:"owner_user_id"`
	Title                  string                              `json:"title"`
	Description            string                              `json:"description"`
	Timezone               string                              `json:"timezone"`
	RangeStart             string                              `json:"range_start"`
	RangeEnd               string                              `json:"range_end"`
	WorkingDayStart        string                              `json:"working_day_start"`
	WorkingDayEnd          string                              `json:"working_day_end"`
	DurationMinutes        int                                 `json:"duration_minutes"`
	SlotGranularityMinutes int                                 `json:"slot_granularity_minutes"`
	DeadlineAt             time.Time                           `json:"deadline_at"`
	ShareMode              string                              `json:"share_mode"`
	Status                 string                              `json:"status"`
	Version                int64                               `json:"version"`
	Slots                  []AvailabilityPollSlot              `json:"slots"`
	Participants           []AvailabilityPollParticipant       `json:"participants"`
	MyResponse             *AvailabilityPollResponseProjection `json:"my_response"`
	ViewerCapabilities     AvailabilityPollViewerCapabilities  `json:"viewer_capabilities"`
	Outcome                *AvailabilityPollOutcomeReference   `json:"outcome"`
	CreatedAt              time.Time                           `json:"created_at"`
	UpdatedAt              time.Time                           `json:"updated_at"`
}

type AvailabilityPollRankedSlot struct {
	Slot             AvailabilityPollSlot `json:"slot"`
	Rank             int                  `json:"rank"`
	CohortSatisfied  bool                 `json:"cohort_satisfied"`
	AggregateBucket  *string              `json:"aggregate_bucket"`
	UnavailableCount *int                 `json:"unavailable_count"`
	PreferredCount   *int                 `json:"preferred_count"`
	AvailableCount   *int                 `json:"available_count"`
}

type AvailabilityPollSummary struct {
	PollID        uuid.UUID                    `json:"poll_id"`
	PollVersion   int64                        `json:"poll_version"`
	Status        string                       `json:"status"`
	ResponseCount *int                         `json:"response_count"`
	RankedSlots   []AvailabilityPollRankedSlot `json:"ranked_slots"`
}

type AvailabilityPollMutationResponse struct {
	Poll    AvailabilityPoll        `json:"poll"`
	Summary AvailabilityPollSummary `json:"summary"`
}

type CreateAvailabilityPollInput struct {
	Title                  string
	Description            string
	ClassID                *uuid.UUID
	Timezone               string
	RangeStart             string
	RangeEnd               string
	WorkingDayStart        string
	WorkingDayEnd          string
	DurationMinutes        int
	SlotGranularityMinutes int
	DeadlineAt             time.Time
	ShareMode              string
	Slots                  []AvailabilityPollSlotInput
	Participants           []AvailabilityPollParticipantInput
	IdempotencyKey         string
}

type UpdateAvailabilityPollInput struct {
	CreateAvailabilityPollInput
	ExpectedVersion int64
}

type ListAvailabilityPollsInput struct {
	Status string
	Limit  int
}

type RespondAvailabilityPollInput struct {
	ExpectedResponseVersion int64
	Answers                 []AvailabilityPollAnswerInput
	IdempotencyKey          string
}

type FinalizeAvailabilityPollInput struct {
	SlotID          uuid.UUID
	OutcomeType     string
	ClassID         *uuid.UUID
	ExpectedVersion int64
	IdempotencyKey  string
}

type CreateAvailabilityPollCapabilityInput struct {
	Scope           string
	ParticipantID   *uuid.UUID
	ExpiresAt       time.Time
	ExpectedVersion int64
}

type AvailabilityPollCapability struct {
	ID            uuid.UUID  `json:"id"`
	PollID        uuid.UUID  `json:"poll_id"`
	ParticipantID *uuid.UUID `json:"participant_id"`
	Scope         string     `json:"scope"`
	ExpiresAt     time.Time  `json:"expires_at"`
	RevokedAt     *time.Time `json:"revoked_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type AvailabilityPollCapabilitySecret struct {
	Capability AvailabilityPollCapability `json:"capability"`
	RawToken   string                     `json:"raw_token"`
	ShareURL   string                     `json:"share_url"`
}

type PublicAvailabilityPoll struct {
	PublicID    uuid.UUID                           `json:"public_id"`
	Title       string                              `json:"title"`
	Description string                              `json:"description"`
	Timezone    string                              `json:"timezone"`
	DeadlineAt  time.Time                           `json:"deadline_at"`
	Status      string                              `json:"status"`
	Slots       []AvailabilityPollSlot              `json:"slots"`
	MyResponse  *AvailabilityPollResponseProjection `json:"my_response"`
	RankedSlots []AvailabilityPollRankedSlot        `json:"ranked_slots"`
}

type PublicAvailabilityPollExchange struct {
	Poll                   PublicAvailabilityPoll `json:"poll"`
	ResponseToken          string                 `json:"response_token"`
	ResponseTokenExpiresAt time.Time              `json:"response_token_expires_at"`
}

type RespondPublicAvailabilityPollInput struct {
	PublicID                uuid.UUID
	ResponseToken           string
	ExpectedResponseVersion int64
	Answers                 []AvailabilityPollAnswerInput
	IdempotencyKey          string
}

type StudyMeeting struct {
	ID           uuid.UUID  `json:"id"`
	ClassID      *uuid.UUID `json:"class_id"`
	OwnerUserID  uuid.UUID  `json:"owner_user_id"`
	SourcePollID *uuid.UUID `json:"source_poll_id"`
	Title        string     `json:"title"`
	StartsAt     time.Time  `json:"starts_at"`
	EndsAt       time.Time  `json:"ends_at"`
	Timezone     string     `json:"timezone"`
	Status       string     `json:"status"`
	Version      int64      `json:"version"`
	CancelledAt  *time.Time `json:"cancelled_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type CreateStudyMeetingInput struct {
	ClassID        *uuid.UUID
	Title          string
	StartsAt       time.Time
	EndsAt         time.Time
	Timezone       string
	IdempotencyKey string
}

type UpdateStudyMeetingInput struct {
	ClassID         *uuid.UUID
	Title           string
	StartsAt        time.Time
	EndsAt          time.Time
	Timezone        string
	ExpectedVersion int64
}

type ListStudyMeetingsInput struct {
	From  *time.Time
	To    *time.Time
	Limit int
}

type AvailabilityPollServiceAPI interface {
	ListPolls(context.Context, tenancy.Context, ListAvailabilityPollsInput) ([]AvailabilityPoll, error)
	CreatePoll(context.Context, tenancy.Context, CreateAvailabilityPollInput) (AvailabilityPoll, error)
	GetPoll(context.Context, tenancy.Context, uuid.UUID) (AvailabilityPoll, error)
	UpdatePoll(context.Context, tenancy.Context, uuid.UUID, UpdateAvailabilityPollInput) (AvailabilityPoll, error)
	OpenPoll(context.Context, tenancy.Context, uuid.UUID, int64) (AvailabilityPoll, error)
	ClosePoll(context.Context, tenancy.Context, uuid.UUID, int64) (AvailabilityPoll, error)
	ReopenPoll(context.Context, tenancy.Context, uuid.UUID, int64, time.Time) (AvailabilityPoll, error)
	CancelPoll(context.Context, tenancy.Context, uuid.UUID, int64, string) (AvailabilityPoll, error)
	Respond(context.Context, tenancy.Context, uuid.UUID, RespondAvailabilityPollInput) (AvailabilityPollMutationResponse, error)
	ListIndividualResponses(context.Context, tenancy.Context, uuid.UUID, ListAvailabilityPollResponsesInput) (AvailabilityPollIndividualResponsePage, error)
	Summary(context.Context, tenancy.Context, uuid.UUID) (AvailabilityPollSummary, error)
	Finalize(context.Context, tenancy.Context, uuid.UUID, FinalizeAvailabilityPollInput) (AvailabilityPollMutationResponse, error)
	CreateCapability(context.Context, tenancy.Context, uuid.UUID, CreateAvailabilityPollCapabilityInput) (AvailabilityPollCapabilitySecret, error)
	RevokeCapability(context.Context, tenancy.Context, uuid.UUID, uuid.UUID, int64, string) (AvailabilityPollCapability, error)
	ResolvePublic(context.Context, uuid.UUID, string) (PublicAvailabilityPollExchange, error)
	RespondPublic(context.Context, RespondPublicAvailabilityPollInput) (PublicAvailabilityPoll, error)
	ListStudyMeetings(context.Context, tenancy.Context, ListStudyMeetingsInput) ([]StudyMeeting, error)
	CreateStudyMeeting(context.Context, tenancy.Context, CreateStudyMeetingInput) (StudyMeeting, error)
	GetStudyMeeting(context.Context, tenancy.Context, uuid.UUID) (StudyMeeting, error)
	UpdateStudyMeeting(context.Context, tenancy.Context, uuid.UUID, UpdateStudyMeetingInput) (StudyMeeting, error)
	CancelStudyMeeting(context.Context, tenancy.Context, uuid.UUID, int64, string) (StudyMeeting, error)
}

type AvailabilityPollRepository interface {
	AvailabilityPollServiceAPI
}
