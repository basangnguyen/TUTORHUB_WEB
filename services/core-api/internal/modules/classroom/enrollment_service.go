package classroom

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	minimumClassInviteCodeTTL = 15 * time.Minute
	maximumClassInviteCodeTTL = 30 * 24 * time.Hour
	maximumInviteCodeUses     = 1000
	maximumMemberEmailLength  = 320
)

type DirectEnrollmentInput struct {
	MemberEmail string
}

type CreateClassInviteCodeInput struct {
	ExpiresInSeconds int
	UsageLimit       int
}

type CreateClassInviteCodeResult struct {
	InviteCode ClassInviteCode
	Token      string
}

type EnrollmentServiceAPI interface {
	DirectEnroll(
		context.Context,
		AccessContext,
		uuid.UUID,
		DirectEnrollmentInput,
	) (EnrollmentMutationResult, error)
	SuspendEnrollment(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
	) (EnrollmentMutationResult, error)
	RemoveEnrollment(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
	) (EnrollmentMutationResult, error)
	CreateInviteCode(
		context.Context,
		AccessContext,
		uuid.UUID,
		CreateClassInviteCodeInput,
	) (CreateClassInviteCodeResult, error)
	ListInviteCodes(
		context.Context,
		AccessContext,
		uuid.UUID,
	) ([]ClassInviteCode, error)
	RevokeInviteCode(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
	) (ClassInviteCode, error)
	JoinByInviteCode(
		context.Context,
		AccessContext,
		string,
	) (JoinClassInvitationResult, error)
	LeaveClass(
		context.Context,
		AccessContext,
		uuid.UUID,
	) (EnrollmentMutationResult, error)
}

type EnrollmentService struct {
	repository EnrollmentRepository
	classes    ClassActionAuthorizer
	authorizer policy.Authorizer
	tokenCodec ClassInviteCodeTokenCodec
	clock      func() time.Time
}

func NewEnrollmentService(
	repository EnrollmentRepository,
	classes ClassActionAuthorizer,
	authorizer policy.Authorizer,
	tokenCodec ClassInviteCodeTokenCodec,
	clock func() time.Time,
) (*EnrollmentService, error) {
	if repository == nil || classes == nil || authorizer == nil || tokenCodec == nil {
		return nil, fmt.Errorf(
			"class enrollment repository, class authorizer, policy authorizer, and token codec are required",
		)
	}
	if clock == nil {
		clock = time.Now
	}
	return &EnrollmentService{
		repository: repository,
		classes:    classes,
		authorizer: authorizer,
		tokenCodec: tokenCodec,
		clock:      clock,
	}, nil
}

func (service *EnrollmentService) DirectEnroll(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input DirectEnrollmentInput,
) (EnrollmentMutationResult, error) {
	memberEmail, err := normalizeDirectEnrollmentEmail(input.MemberEmail)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	class, err := service.authorizeManagedClass(ctx, access, classID)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	if class.Status != ClassStatusActive {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	return service.repository.DirectEnroll(
		ctx,
		tenantContextFromAccess(access),
		classID,
		DirectEnrollmentParams{
			MemberEmail: memberEmail,
			ChangedAt:   service.clock().UTC(),
		},
	)
}

func (service *EnrollmentService) SuspendEnrollment(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	userID uuid.UUID,
) (EnrollmentMutationResult, error) {
	if userID == uuid.Nil {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	class, err := service.authorizeManagedClass(ctx, access, classID)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	if class.Status != ClassStatusActive {
		return EnrollmentMutationResult{}, ErrEnrollmentConflict
	}
	return service.repository.SuspendEnrollment(
		ctx,
		tenantContextFromAccess(access),
		classID,
		userID,
		service.clock().UTC(),
	)
}

func (service *EnrollmentService) RemoveEnrollment(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	userID uuid.UUID,
) (EnrollmentMutationResult, error) {
	if userID == uuid.Nil {
		return EnrollmentMutationResult{}, ErrEnrollmentNotFound
	}
	if _, err := service.authorizeManagedClass(ctx, access, classID); err != nil {
		return EnrollmentMutationResult{}, err
	}
	return service.repository.RemoveEnrollment(
		ctx,
		tenantContextFromAccess(access),
		classID,
		userID,
		service.clock().UTC(),
	)
}

func (service *EnrollmentService) CreateInviteCode(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input CreateClassInviteCodeInput,
) (CreateClassInviteCodeResult, error) {
	if input.ExpiresInSeconds < int(minimumClassInviteCodeTTL/time.Second) ||
		input.ExpiresInSeconds > int(maximumClassInviteCodeTTL/time.Second) ||
		input.UsageLimit < 1 ||
		input.UsageLimit > maximumInviteCodeUses {
		return CreateClassInviteCodeResult{}, ErrInvalidEnrollmentInput
	}
	ttl := time.Duration(input.ExpiresInSeconds) * time.Second
	class, err := service.authorizeManagedClass(ctx, access, classID)
	if err != nil {
		return CreateClassInviteCodeResult{}, err
	}
	if class.Status != ClassStatusActive {
		return CreateClassInviteCodeResult{}, ErrClassInviteCodeConflict
	}
	token, tokenHash, err := generateClassInviteCodeToken(service.tokenCodec)
	if err != nil {
		return CreateClassInviteCodeResult{}, fmt.Errorf(
			"generate class invitation token: %w",
			err,
		)
	}
	now := service.clock().UTC()
	inviteCode, err := service.repository.CreateInviteCode(
		ctx,
		tenantContextFromAccess(access),
		classID,
		CreateInviteCodeParams{
			CodeHash:   tokenHash,
			ExpiresAt:  now.Add(ttl),
			UsageLimit: input.UsageLimit,
			CreatedAt:  now,
		},
	)
	if err != nil {
		return CreateClassInviteCodeResult{}, err
	}
	return CreateClassInviteCodeResult{InviteCode: inviteCode, Token: token}, nil
}

func (service *EnrollmentService) ListInviteCodes(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) ([]ClassInviteCode, error) {
	if _, err := service.authorizeManagedClass(ctx, access, classID); err != nil {
		return nil, err
	}
	return service.repository.ListInviteCodes(
		ctx,
		tenantContextFromAccess(access),
		classID,
		service.clock().UTC(),
	)
}

func (service *EnrollmentService) RevokeInviteCode(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	codeID uuid.UUID,
) (ClassInviteCode, error) {
	if codeID == uuid.Nil {
		return ClassInviteCode{}, ErrClassInviteCodeUnavailable
	}
	if _, err := service.authorizeManagedClass(ctx, access, classID); err != nil {
		return ClassInviteCode{}, err
	}
	return service.repository.RevokeInviteCode(
		ctx,
		tenantContextFromAccess(access),
		classID,
		codeID,
		service.clock().UTC(),
	)
}

func (service *EnrollmentService) JoinByInviteCode(
	ctx context.Context,
	access AccessContext,
	rawToken string,
) (JoinClassInvitationResult, error) {
	tenantContext, err := service.authorizeTenantView(access)
	if err != nil {
		return JoinClassInvitationResult{}, err
	}
	tokenHash, err := digestClassInviteCodeToken(service.tokenCodec, rawToken)
	if err != nil {
		return JoinClassInvitationResult{}, ErrClassInviteCodeUnavailable
	}
	result, err := service.repository.JoinByInviteCode(
		ctx,
		tenantContext,
		tokenHash,
		service.clock().UTC(),
	)
	if err != nil {
		return JoinClassInvitationResult{}, err
	}
	class, err := service.classes.AuthorizeClass(
		ctx,
		access,
		result.Class.ID,
		policy.ActionClassView,
	)
	if err != nil {
		return JoinClassInvitationResult{}, err
	}
	result.Class = class
	return result, nil
}

func (service *EnrollmentService) LeaveClass(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) (EnrollmentMutationResult, error) {
	if classID == uuid.Nil {
		return EnrollmentMutationResult{}, ErrClassNotFound
	}
	tenantContext, err := service.authorizeTenantView(access)
	if err != nil {
		return EnrollmentMutationResult{}, err
	}
	return service.repository.LeaveClass(
		ctx,
		tenantContext,
		classID,
		service.clock().UTC(),
	)
}

func (service *EnrollmentService) authorizeManagedClass(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) (Class, error) {
	if classID == uuid.Nil {
		return Class{}, ErrClassNotFound
	}
	return service.classes.AuthorizeClass(
		ctx,
		access,
		classID,
		policy.ActionEnrollmentManage,
	)
}

func (service *EnrollmentService) authorizeTenantView(
	access AccessContext,
) (tenancy.Context, error) {
	decision := service.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID:          access.ActorID,
			ActiveTenantID:   access.TenantID,
			MembershipActive: access.MembershipActive,
			OrganizationRoles: append(
				[]policy.OrganizationRole(nil),
				access.OrganizationRoles...,
			),
		},
		Action: policy.ActionTenantView,
		Resource: policy.Resource{
			TenantID: access.TenantID,
			State:    policy.ResourceStateActive,
		},
	})
	if !decision.Allowed {
		return tenancy.Context{}, ErrEnrollmentAccessDenied
	}
	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return tenancy.Context{}, ErrEnrollmentAccessDenied
	}
	return tenantContext, nil
}

func tenantContextFromAccess(access AccessContext) tenancy.Context {
	tenantContext, _ := tenancy.New(access.TenantID, access.ActorID)
	return tenantContext
}

func normalizeDirectEnrollmentEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil ||
		address.Address != value ||
		len(value) < 3 ||
		len(value) > maximumMemberEmailLength {
		return "", ErrInvalidEnrollmentInput
	}
	return value, nil
}
