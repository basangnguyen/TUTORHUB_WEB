package calendar

import (
	"context"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type Repository interface {
	ListItems(
		context.Context,
		tenancy.Context,
		listParams,
	) ([]Item, bool, error)
	GetPreference(context.Context, tenancy.Context) (DisplayPreference, error)
	UpdatePreference(
		context.Context,
		tenancy.Context,
		UpdatePreferenceInput,
		time.Time,
	) (DisplayPreference, error)
}
