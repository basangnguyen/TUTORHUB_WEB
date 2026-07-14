package classroom

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	permissionClassCreate = "class.create"
	permissionClassView   = "class.view"
)

type AccessContext struct {
	TenantID    uuid.UUID
	ActorID     uuid.UUID
	Permissions []string
}

type CreateClassInput struct {
	Code        string
	Title       string
	Description string
}

type ServiceAPI interface {
	Create(context.Context, AccessContext, CreateClassInput) (Class, error)
	Get(context.Context, AccessContext, uuid.UUID) (Class, error)
	List(context.Context, AccessContext, int) ([]Class, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("classroom repository is required")
	}

	return &Service{repository: repository}, nil
}

func (service *Service) Create(
	ctx context.Context,
	access AccessContext,
	input CreateClassInput,
) (Class, error) {
	tenantContext, err := access.authorize(permissionClassCreate)
	if err != nil {
		return Class{}, err
	}

	return service.repository.Create(ctx, tenantContext, CreateClassParams{
		OwnerUserID: access.ActorID,
		Code:        input.Code,
		Title:       input.Title,
		Description: input.Description,
	})
}

func (service *Service) Get(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) (Class, error) {
	tenantContext, err := access.authorize(permissionClassView)
	if err != nil {
		return Class{}, err
	}

	return service.repository.Get(ctx, tenantContext, classID)
}

func (service *Service) List(
	ctx context.Context,
	access AccessContext,
	limit int,
) ([]Class, error) {
	tenantContext, err := access.authorize(permissionClassView)
	if err != nil {
		return nil, err
	}
	if limit < 0 || limit > maximumListLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidListLimit, maximumListLimit)
	}

	return service.repository.List(ctx, tenantContext, limit)
}

func (access AccessContext) authorize(permission string) (tenancy.Context, error) {
	if !access.hasPermission(permission) {
		return tenancy.Context{}, ErrClassAccessDenied
	}

	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return tenancy.Context{}, ErrClassAccessDenied
	}

	return tenantContext, nil
}

func (access AccessContext) hasPermission(permission string) bool {
	for _, candidate := range access.Permissions {
		if candidate == permission {
			return true
		}
	}

	return false
}
