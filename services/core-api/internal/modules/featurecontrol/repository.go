package featurecontrol

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type Transaction interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Enforcer interface {
	RequireFeature(context.Context, Transaction, uuid.UUID, FeatureKey) error
	RequireMemberCapacity(context.Context, Transaction, uuid.UUID) error
	RequireActiveClassCapacity(context.Context, Transaction, uuid.UUID) error
	ConsumeInviteCreation(
		context.Context,
		Transaction,
		uuid.UUID,
		time.Time,
	) (RateLimitResult, error)
}

type Repository interface {
	GetCapabilities(
		context.Context,
		tenancy.Context,
		time.Time,
	) (Capabilities, error)
	PutOverrides(
		context.Context,
		tenancy.Context,
		PutOverridesInput,
		time.Time,
	) (Capabilities, error)
}
