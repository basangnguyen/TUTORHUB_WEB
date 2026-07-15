package classroom

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var (
	ErrClassNotFound         = errors.New("class not found")
	ErrDuplicateClassCode    = errors.New("class code already exists in tenant")
	ErrOwnerMembershipNeeded = errors.New("class owner must be a tenant member")
	ErrInvalidClassInput     = errors.New("invalid class input")
	ErrInvalidListLimit      = errors.New("invalid class list limit")
	ErrClassAccessDenied     = errors.New("classroom access denied")
)

type Repository interface {
	Create(context.Context, tenancy.Context, CreateClassParams) (Class, error)
	Get(context.Context, tenancy.Context, uuid.UUID) (Class, error)
	List(context.Context, tenancy.Context, int) ([]Class, error)
}
