package media

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type SourceKind string

const (
	SourceClassSession           SourceKind = "class_session"
	SourceClassSessionOccurrence SourceKind = "class_session_occurrence"
	SourceStudyMeeting           SourceKind = "study_meeting"
	SourceInstant                SourceKind = "instant"
)

type SpaceStatus string

const (
	SpaceStatusScheduled SpaceStatus = "scheduled"
	SpaceStatusOpen      SpaceStatus = "open"
	SpaceStatusEnded     SpaceStatus = "ended"
	SpaceStatusCancelled SpaceStatus = "cancelled"
)

type RoomInstanceStatus string

const (
	RoomInstanceProvisioning RoomInstanceStatus = "provisioning"
	RoomInstanceActive       RoomInstanceStatus = "active"
	RoomInstanceClosing      RoomInstanceStatus = "closing"
	RoomInstanceEnded        RoomInstanceStatus = "ended"
	RoomInstanceFailed       RoomInstanceStatus = "failed"
)

var (
	ErrLifecycleUnavailable     = errors.New("media lifecycle unavailable")
	ErrMediaProviderUnavailable = errors.New("media provider unavailable")
	ErrInvalidSpaceRequest      = errors.New("invalid media space request")
	ErrSpaceAccessDenied        = errors.New("media space access denied")
	ErrSpaceNotFound            = errors.New("media space not found")
	ErrSourceUnavailable        = errors.New("media space source unavailable")
	ErrSpaceVersionConflict     = errors.New("media space version conflict")
	ErrSpaceIdempotency         = errors.New("media space idempotency conflict")
	ErrSpaceTransition          = errors.New("invalid media space transition")
	errRoomActivationTerminal   = errors.New("room activation reached terminal state")
)

// SourceReference is the authoritative source projection. SourceInstant is
// accepted only in CreateSourceInput and is never persisted or returned.
type SourceReference struct {
	Kind           SourceKind `json:"kind"`
	ClassSessionID *uuid.UUID `json:"class_session_id,omitempty"`
	SeriesID       *uuid.UUID `json:"series_id,omitempty"`
	OccurrenceKey  string     `json:"occurrence_key,omitempty"`
	StudyMeetingID *uuid.UUID `json:"study_meeting_id,omitempty"`
}

type CreateSourceInput struct {
	Kind           SourceKind
	ClassSessionID uuid.UUID
	SeriesID       uuid.UUID
	OccurrenceKey  string
	StudyMeetingID uuid.UUID
	Instant        InstantSourceInput
}

type InstantSourceInput struct {
	Title           string
	DurationMinutes int
	Timezone        string
}

type ViewerOperations struct {
	CanStart            bool `json:"can_start"`
	CanRecover          bool `json:"can_recover"`
	CanEnd              bool `json:"can_end"`
	CanCancel           bool `json:"can_cancel"`
	CanManageAdmissions bool `json:"can_manage_admissions"`
	CanManageInvites    bool `json:"can_manage_invites"`
}

type RoomInstance struct {
	ID               uuid.UUID          `json:"id"`
	Status           RoomInstanceStatus `json:"status"`
	Version          int64              `json:"version"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	ProviderRoomName string             `json:"-"`
	ProviderRoomSID  string             `json:"-"`
}

type MediaSpace struct {
	ID                   uuid.UUID        `json:"id"`
	Source               SourceReference  `json:"source"`
	Status               SpaceStatus      `json:"status"`
	Version              int64            `json:"version"`
	ActiveRoomInstance   *RoomInstance    `json:"active_room_instance"`
	RecoveryRoomInstance *RoomInstance    `json:"recovery_room_instance"`
	ViewerOperations     ViewerOperations `json:"viewer_operations"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
}

type CreateSpaceInput struct {
	Source         CreateSourceInput
	IdempotencyKey string
}

type CreateSpaceResult struct {
	Space   MediaSpace
	Created bool
}

type TransitionInput struct {
	ExpectedVersion int64
	IdempotencyKey  string
	ReasonCode      string
}

type RecoverSpaceInput struct {
	ExpectedSpaceVersion        int64
	ExpectedRoomInstanceID      uuid.UUID
	ExpectedRoomInstanceVersion int64
	IdempotencyKey              string
}

type CreateSpaceCommand struct {
	SpaceID          uuid.UUID
	InstantMeetingID uuid.UUID
	Source           CreateSourceInput
	IdempotencyKey   string
	Fingerprint      []byte
	CreatedAt        time.Time
}

type TransitionCommand struct {
	SpaceID                     uuid.UUID
	RoomInstanceID              uuid.UUID
	ProviderRoomName            string
	Operation                   string
	ExpectedVersion             int64
	ExpectedRoomInstanceID      uuid.UUID
	ExpectedRoomInstanceVersion int64
	IdempotencyKey              string
	ReasonCode                  string
	Fingerprint                 []byte
	OccurredAt                  time.Time
}

type LifecycleRepository interface {
	CreateSpace(context.Context, AccessContext, CreateSpaceCommand) (CreateSpaceResult, error)
	GetSpace(context.Context, AccessContext, uuid.UUID) (MediaSpace, error)
	TransitionSpace(context.Context, AccessContext, TransitionCommand) (MediaSpace, error)
}

type LifecycleServiceAPI interface {
	CreateSpace(context.Context, AccessContext, CreateSpaceInput) (CreateSpaceResult, error)
	GetSpace(context.Context, AccessContext, uuid.UUID) (MediaSpace, error)
	StartSpace(context.Context, AccessContext, uuid.UUID, TransitionInput) (MediaSpace, error)
	EndSpace(context.Context, AccessContext, uuid.UUID, TransitionInput) (MediaSpace, error)
	CancelSpace(context.Context, AccessContext, uuid.UUID, TransitionInput) (MediaSpace, error)
	RecoverSpace(context.Context, AccessContext, uuid.UUID, RecoverSpaceInput) (MediaSpace, error)
}
