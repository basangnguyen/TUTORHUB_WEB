package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	KindDirect Kind = "direct"
	KindClass  Kind = "class"

	defaultListLimit    = 30
	maximumListLimit    = 100
	maximumCursorLength = 1024
)

var (
	ErrInvalidInput        = errors.New("invalid conversation input")
	ErrAccessDenied        = errors.New("conversation access denied")
	ErrNotFound            = errors.New("conversation not found")
	ErrUnavailable         = errors.New("conversation target unavailable")
	ErrReadOnly            = errors.New("conversation is read only")
	ErrIdempotencyConflict = errors.New("message idempotency conflict")
	ErrVersionConflict     = errors.New("message version conflict")
)

type Kind string

type AccessContext struct {
	TenantID          uuid.UUID
	ActorID           uuid.UUID
	MembershipActive  bool
	OrganizationRoles []policy.OrganizationRole
}

type Participant struct {
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
}

type ViewerAccess struct {
	CanPostMessages bool `json:"can_post_messages"`
}

type Conversation struct {
	ID                uuid.UUID     `json:"id"`
	TenantID          uuid.UUID     `json:"-"`
	Kind              Kind          `json:"kind"`
	ClassID           *uuid.UUID    `json:"class_id,omitempty"`
	ClassStatus       *string       `json:"class_status,omitempty"`
	Title             string        `json:"title"`
	Participants      []Participant `json:"participants"`
	ViewerAccess      ViewerAccess  `json:"viewer_access"`
	UnreadCount       int           `json:"unread_count"`
	UnreadCountCapped bool          `json:"unread_count_capped"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
}

type ListInput struct {
	Kind   *Kind
	Limit  int
	Cursor string
}

type Page struct {
	Items      []Conversation `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type CreateResult struct {
	Conversation Conversation
	Created      bool
}

type ServiceAPI interface {
	List(context.Context, AccessContext, ListInput) (Page, error)
	Get(context.Context, AccessContext, uuid.UUID) (Conversation, error)
	CreateDirect(context.Context, AccessContext, string) (CreateResult, error)
	CreateClass(context.Context, AccessContext, uuid.UUID) (CreateResult, error)
	ListMessages(context.Context, AccessContext, uuid.UUID, MessageListInput) (MessagePage, error)
	SendMessage(context.Context, AccessContext, uuid.UUID, SendMessageInput) (MessageMutationResult, error)
	EditMessage(context.Context, AccessContext, uuid.UUID, uuid.UUID, EditMessageInput) (Message, error)
	DeleteMessage(context.Context, AccessContext, uuid.UUID, uuid.UUID, DeleteMessageInput) (Message, error)
	MarkRead(context.Context, AccessContext, uuid.UUID, uuid.UUID) (MessageReadState, error)
}
