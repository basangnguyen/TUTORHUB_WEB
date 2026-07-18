package classroom

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var (
	ErrClassNotFound                = errors.New("class not found")
	ErrDuplicateClassCode           = errors.New("class code already exists in tenant")
	ErrClassOwnerUnavailable        = errors.New("class owner is unavailable")
	ErrOwnerMembershipNeeded        = ErrClassOwnerUnavailable
	ErrInvalidClassInput            = errors.New("invalid class input")
	ErrInvalidListLimit             = errors.New("invalid class list limit")
	ErrInvalidClassCursor           = errors.New("invalid class list cursor")
	ErrClassAccessDenied            = errors.New("classroom access denied")
	ErrClassVersionConflict         = errors.New("class version is stale")
	ErrInvalidClassTransition       = errors.New("invalid class state transition")
	ErrRecentAuthenticationRequired = errors.New("recent authentication is required")
)

type Repository interface {
	Create(context.Context, tenancy.Context, CreateClassParams) (Class, error)
	Get(context.Context, tenancy.Context, uuid.UUID) (Class, error)
	List(context.Context, tenancy.Context, ListClassesParams) (ListClassesResult, error)
	// Mutation implementations must reload and lock the tenant, class, and current
	// memberships, then re-authorize, apply the version precondition, mutate, and
	// write the outbox event in one transaction. Service preflight is not a
	// substitute for this authoritative check.
	Update(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		UpdateClassParams,
		time.Time,
	) (Class, error)
	Archive(context.Context, tenancy.Context, uuid.UUID, int64, time.Time) (Class, error)
	Restore(context.Context, tenancy.Context, uuid.UUID, int64, time.Time) (Class, error)
	TransferOwnership(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		TransferClassOwnershipParams,
		time.Time,
	) (Class, error)
}
