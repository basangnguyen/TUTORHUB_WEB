package classroom

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type AccessContext struct {
	TenantID          uuid.UUID
	ActorID           uuid.UUID
	MembershipActive  bool
	OrganizationRoles []policy.OrganizationRole
	ClassRoles        []policy.ClassRole
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
	authorizer policy.Authorizer
}

func NewService(repository Repository, authorizer policy.Authorizer) (*Service, error) {
	if repository == nil || authorizer == nil {
		return nil, fmt.Errorf("classroom repository and policy authorizer are required")
	}

	return &Service{repository: repository, authorizer: authorizer}, nil
}

func (service *Service) Create(
	ctx context.Context,
	access AccessContext,
	input CreateClassInput,
) (Class, error) {
	tenantContext, err := service.authorize(access, policy.ActionClassCreate, uuid.Nil)
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
	if classID == uuid.Nil {
		return Class{}, ErrClassNotFound
	}
	tenantContext, err := service.authorize(access, policy.ActionClassView, classID)
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
	tenantContext, err := service.authorize(access, policy.ActionClassView, uuid.Nil)
	if err != nil {
		return nil, err
	}
	if limit < 0 || limit > maximumListLimit {
		return nil, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidListLimit, maximumListLimit)
	}

	return service.repository.List(ctx, tenantContext, limit)
}

func (service *Service) authorize(
	access AccessContext,
	action policy.Action,
	classID uuid.UUID,
) (tenancy.Context, error) {
	decision := service.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: access.ActorID, ActiveTenantID: access.TenantID,
			MembershipActive:  access.MembershipActive,
			OrganizationRoles: append([]policy.OrganizationRole(nil), access.OrganizationRoles...),
			ClassRoles:        append([]policy.ClassRole(nil), access.ClassRoles...),
		},
		Action: action,
		Resource: policy.Resource{
			TenantID: access.TenantID, ClassID: classID, State: policy.ResourceStateUnknown,
		},
	})
	if !decision.Allowed {
		if decision.ConcealResource {
			return tenancy.Context{}, ErrClassNotFound
		}
		return tenancy.Context{}, ErrClassAccessDenied
	}

	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return tenancy.Context{}, ErrClassAccessDenied
	}

	return tenantContext, nil
}
