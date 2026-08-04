package conversation

import (
	"time"

	"github.com/google/uuid"
)

const (
	MessageStateActive  MessageState = "active"
	MessageStateDeleted MessageState = "deleted"

	defaultMessageListLimit    = 50
	maximumMessageListLimit    = 100
	maximumMessageCursorLength = 1024
	maximumMessageCharacters   = 4000
	maximumMessageBytes        = 16 * 1024
	maximumUnreadCount         = 100
	actorMessageRateLimit      = 60
)

type MessageState string

type MessageAuthor struct {
	UserID      uuid.UUID `json:"user_id"`
	DisplayName string    `json:"display_name"`
}

type Message struct {
	ID              uuid.UUID     `json:"id"`
	ConversationID  uuid.UUID     `json:"conversation_id"`
	Sequence        int64         `json:"sequence"`
	ClientMessageID uuid.UUID     `json:"client_message_id"`
	Author          MessageAuthor `json:"author"`
	Content         *string       `json:"content,omitempty"`
	State           MessageState  `json:"state"`
	Version         int64         `json:"version"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	EditedAt        *time.Time    `json:"edited_at,omitempty"`
	DeletedAt       *time.Time    `json:"deleted_at,omitempty"`
}

type MessageListInput struct {
	Limit  int
	Cursor string
}

type MessageReadState struct {
	LastReadMessageID uuid.UUID `json:"last_read_message_id"`
	LastReadSequence  int64     `json:"last_read_sequence"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type MessagePage struct {
	Items             []Message         `json:"items"`
	NextCursor        string            `json:"next_cursor,omitempty"`
	UnreadCount       int               `json:"unread_count"`
	UnreadCountCapped bool              `json:"unread_count_capped"`
	ReadState         *MessageReadState `json:"read_state"`
}

type SendMessageInput struct {
	ClientMessageID uuid.UUID
	Content         string
}

type EditMessageInput struct {
	Content         string
	ExpectedVersion int64
}

type DeleteMessageInput struct {
	ExpectedVersion int64
}

type MessageMutationResult struct {
	Message Message
	Created bool
}

type messageRepositoryPage struct {
	Items             []Message
	HasMore           bool
	UnreadCount       int
	UnreadCountCapped bool
	ReadState         *MessageReadState
}
