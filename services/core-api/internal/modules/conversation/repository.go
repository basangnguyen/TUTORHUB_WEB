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
}
