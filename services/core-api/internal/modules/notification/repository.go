package notification

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type Repository interface {
	List(
		context.Context,
		tenancy.Context,
		ListInput,
		listCursor,
	) ([]Notification, bool, error)
	UnreadCount(context.Context, tenancy.Context) (UnreadCount, error)
	MarkRead(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		time.Time,
	) (Notification, error)
	MarkAllRead(context.Context, tenancy.Context, time.Time) (MarkAllResult, error)
	GetPreference(context.Context, tenancy.Context) (Preference, error)
	PutPreference(
		context.Context,
		tenancy.Context,
		PutPreferenceInput,
		time.Time,
	) (Preference, error)
}
