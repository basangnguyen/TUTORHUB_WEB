package conversation

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	List(context.Context, AccessContext, ListInput, listCursor) ([]Conversation, bool, error)
	Get(context.Context, AccessContext, uuid.UUID) (Conversation, error)
	CreateDirect(context.Context, AccessContext, string) (CreateResult, error)
	CreateClass(context.Context, AccessContext, uuid.UUID) (CreateResult, error)
	ListMessages(context.Context, AccessContext, uuid.UUID, MessageListInput, messageCursor) (messageRepositoryPage, error)
	SendMessage(context.Context, AccessContext, uuid.UUID, SendMessageInput) (MessageMutationResult, error)
	EditMessage(context.Context, AccessContext, uuid.UUID, uuid.UUID, EditMessageInput) (Message, error)
	DeleteMessage(context.Context, AccessContext, uuid.UUID, uuid.UUID, DeleteMessageInput) (Message, error)
	MarkRead(context.Context, AccessContext, uuid.UUID, uuid.UUID) (MessageReadState, error)
}
