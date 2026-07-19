package media

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

var (
	ErrUnavailable      = errors.New("media service unavailable")
	ErrAccessDenied     = errors.New("media access denied")
	ErrInvalidRequest   = errors.New("invalid media request")
	ErrClassUnavailable = errors.New("class is unavailable for media")
	ErrUnrecognizedRoom = errors.New("unrecognized LiveKit room")
	ErrInvalidWebhook   = errors.New("invalid LiveKit webhook")
)

type AccessContext struct {
	TenantID          uuid.UUID
	ActorID           uuid.UUID
	SessionID         uuid.UUID
	DisplayName       string
	Role              string
	MembershipActive  bool
	OrganizationRoles []policy.OrganizationRole
	ClassRoles        []policy.ClassRole
}

type JoinCredential struct {
	AccessToken         string
	ServerURL           string
	RoomName            string
	ParticipantIdentity string
	ParticipantName     string
	AttemptID           uuid.UUID
	CanPublish          bool
	ExpiresAt           time.Time
}

type TokenGrant struct {
	RoomName            string
	ParticipantIdentity string
	ParticipantName     string
	Role                string
	OrganizationRole    string
	ClassRole           string
	CanPublish          bool
	CanPublishData      bool
	CanSubscribe        bool
	ValidFor            time.Duration
}

type ClientEventInput struct {
	AttemptID  uuid.UUID
	Stage      string
	Outcome    string
	ErrorCode  string
	DurationMS int64
}

type ClientEvent struct {
	TenantID   uuid.UUID
	ClassID    uuid.UUID
	ActorID    uuid.UUID
	AttemptID  uuid.UUID
	Stage      string
	Outcome    string
	ErrorCode  string
	DurationMS int64
	RecordedAt time.Time
}

type WebhookEvent struct {
	ID                  string
	EventType           string
	RoomName            string
	ParticipantIdentity string
	OccurredAt          time.Time
}

type WebhookReceipt struct {
	EventID             string
	EventType           string
	TenantID            uuid.UUID
	ClassID             uuid.UUID
	RoomName            string
	ParticipantIdentity string
	OccurredAt          time.Time
	ReceivedAt          time.Time
}

type WebhookResult struct {
	Recorded  bool
	Duplicate bool
	Ignored   bool
}
