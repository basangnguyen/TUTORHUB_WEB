package classroom

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	defaultListLimit          = 50
	maximumListLimit          = 100
	maximumClassCursorLength  = 512
	classCursorPrefix         = "thcl2_"
	defaultRecentAuthTTL      = 10 * time.Minute
	recentAuthFutureTolerance = time.Minute
)

type AccessContext struct {
	TenantID          uuid.UUID
	ActorID           uuid.UUID
	AuthenticatedAt   time.Time
	MembershipActive  bool
	OrganizationRoles []policy.OrganizationRole
	ClassRoles        []policy.ClassRole
}

type CreateClassInput struct {
	Code        string
	Title       string
	Description string
	Timezone    *string
}

type UpdateClassInput struct {
	Code            *string
	Title           *string
	Description     *string
	Timezone        *string
	Status          *ClassStatus
	ExpectedVersion int64
}

type TransferClassOwnershipInput struct {
	NewOwnerUserID  uuid.UUID
	ExpectedVersion int64
}

type ListClassesInput struct {
	Status *ClassStatus
	Limit  int
	Cursor string
}

type ClassPage struct {
	Items      []Class
	NextCursor string
}

type ServiceAPI interface {
	Create(context.Context, AccessContext, CreateClassInput) (Class, error)
	Get(context.Context, AccessContext, uuid.UUID) (Class, error)
	List(context.Context, AccessContext, ListClassesInput) (ClassPage, error)
	Update(context.Context, AccessContext, uuid.UUID, UpdateClassInput) (Class, error)
	Archive(context.Context, AccessContext, uuid.UUID, int64) (Class, error)
	Restore(context.Context, AccessContext, uuid.UUID, int64) (Class, error)
	TransferOwnership(
		context.Context,
		AccessContext,
		uuid.UUID,
		TransferClassOwnershipInput,
	) (Class, error)
}

// ClassActionAuthorizer is the narrow cross-module boundary used by media and
// other class-scoped capabilities. Implementations must resolve persisted
// enrollment state instead of trusting class roles supplied by an HTTP session.
type ClassActionAuthorizer interface {
	AuthorizeClass(context.Context, AccessContext, uuid.UUID, policy.Action) (Class, error)
}

type ServiceConfig struct {
	RecentAuthTTL time.Duration
	Clock         func() time.Time
}

type Service struct {
	repository       Repository
	enrollmentLookup EnrollmentLookup
	authorizer       policy.Authorizer
	recentAuthTTL    time.Duration
	clock            func() time.Time
}

func NewService(
	repository Repository,
	authorizer policy.Authorizer,
	configurations ...ServiceConfig,
) (*Service, error) {
	if repository == nil || authorizer == nil {
		return nil, fmt.Errorf("classroom repository and policy authorizer are required")
	}
	enrollmentLookup, ok := repository.(EnrollmentLookup)
	if !ok {
		return nil, fmt.Errorf("classroom repository must resolve class enrollments")
	}
	if len(configurations) > 1 {
		return nil, fmt.Errorf("only one classroom service configuration is supported")
	}
	config := ServiceConfig{}
	if len(configurations) == 1 {
		config = configurations[0]
	}
	if config.RecentAuthTTL <= 0 {
		config.RecentAuthTTL = defaultRecentAuthTTL
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}

	return &Service{
		repository:       repository,
		enrollmentLookup: enrollmentLookup,
		authorizer:       authorizer,
		recentAuthTTL:    config.RecentAuthTTL,
		clock:            config.Clock,
	}, nil
}

func (service *Service) Create(
	ctx context.Context,
	access AccessContext,
	input CreateClassInput,
) (Class, error) {
	tenantContext, err := service.authorize(
		access,
		policy.ActionClassCreate,
		uuid.Nil,
		policy.ResourceStateUnknown,
	)
	if err != nil {
		return Class{}, err
	}

	created, err := service.repository.Create(ctx, tenantContext, CreateClassParams{
		OwnerUserID: access.ActorID,
		Code:        input.Code,
		Title:       input.Title,
		Description: input.Description,
		Timezone:    input.Timezone,
	})
	if err != nil {
		return Class{}, err
	}
	created, _ = projectClassViewerAccess(service.authorizer, access, created, nil)
	return created, nil
}

func (service *Service) Get(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) (Class, error) {
	return service.AuthorizeClass(ctx, access, classID, policy.ActionClassView)
}

func (service *Service) AuthorizeClass(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) (Class, error) {
	if classID == uuid.Nil {
		return Class{}, ErrClassNotFound
	}
	tenantContext, err := service.authorize(
		access,
		policy.ActionTenantView,
		uuid.Nil,
		policy.ResourceStateUnknown,
	)
	if err != nil {
		return Class{}, err
	}
	class, err := service.repository.Get(ctx, tenantContext, classID)
	if err != nil {
		return Class{}, err
	}
	enrollment, err := service.enrollmentLookup.FindActorEnrollment(
		ctx,
		tenantContext,
		classID,
	)
	if err != nil {
		return Class{}, fmt.Errorf("resolve class enrollment: %w", err)
	}

	class, resolvedAccess := projectClassViewerAccess(
		service.authorizer,
		access,
		class,
		enrollment,
	)
	if _, err := service.authorize(
		resolvedAccess,
		action,
		classID,
		policy.ResourceState(class.Status),
	); err != nil {
		return Class{}, err
	}
	return class, nil
}

func (service *Service) List(
	ctx context.Context,
	access AccessContext,
	input ListClassesInput,
) (ClassPage, error) {
	tenantContext, err := service.authorize(
		access,
		policy.ActionTenantView,
		uuid.Nil,
		policy.ResourceStateUnknown,
	)
	if err != nil {
		return ClassPage{}, err
	}
	params, err := normalizeListClassesInput(input, tenantContext.TenantID)
	if err != nil {
		return ClassPage{}, err
	}
	result, err := service.repository.List(ctx, tenantContext, params)
	if err != nil {
		return ClassPage{}, err
	}

	page := ClassPage{Items: result.Items}
	if result.HasMore && len(result.Items) > 0 {
		last := result.Items[len(result.Items)-1]
		page.NextCursor, err = encodeClassCursor(
			ClassCursor{CreatedAt: last.CreatedAt, ID: last.ID},
			tenantContext.TenantID,
			params.Status,
		)
		if err != nil {
			return ClassPage{}, fmt.Errorf("encode next class cursor: %w", err)
		}
	}
	return page, nil
}

func (service *Service) Update(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input UpdateClassInput,
) (Class, error) {
	tenantContext, err := service.authorizeClassMutation(
		ctx,
		access,
		classID,
		policy.ActionClassUpdate,
	)
	if err != nil {
		return Class{}, err
	}
	params, err := (UpdateClassParams{
		Code:            input.Code,
		Title:           input.Title,
		Description:     input.Description,
		Timezone:        input.Timezone,
		Status:          input.Status,
		ExpectedVersion: input.ExpectedVersion,
	}).normalized()
	if err != nil {
		return Class{}, err
	}
	updated, err := service.repository.Update(
		ctx,
		tenantContext,
		classID,
		params,
		service.clock().UTC(),
	)
	if err != nil {
		return Class{}, err
	}
	return service.projectActorClass(ctx, access, tenantContext, updated)
}

func (service *Service) Archive(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	expectedVersion int64,
) (Class, error) {
	tenantContext, err := service.authorizeClassMutation(
		ctx,
		access,
		classID,
		policy.ActionClassArchive,
	)
	if err != nil {
		return Class{}, err
	}
	if expectedVersion < 1 {
		return Class{}, fmt.Errorf("%w: expected version is required", ErrInvalidClassInput)
	}
	archived, err := service.repository.Archive(
		ctx,
		tenantContext,
		classID,
		expectedVersion,
		service.clock().UTC(),
	)
	if err != nil {
		return Class{}, err
	}
	return service.projectActorClass(ctx, access, tenantContext, archived)
}

func (service *Service) Restore(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	expectedVersion int64,
) (Class, error) {
	tenantContext, err := service.authorizeClassMutation(
		ctx,
		access,
		classID,
		policy.ActionClassArchive,
	)
	if err != nil {
		return Class{}, err
	}
	if expectedVersion < 1 {
		return Class{}, fmt.Errorf("%w: expected version is required", ErrInvalidClassInput)
	}
	restored, err := service.repository.Restore(
		ctx,
		tenantContext,
		classID,
		expectedVersion,
		service.clock().UTC(),
	)
	if err != nil {
		return Class{}, err
	}
	return service.projectActorClass(ctx, access, tenantContext, restored)
}

func (service *Service) TransferOwnership(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input TransferClassOwnershipInput,
) (Class, error) {
	tenantContext, err := service.authorizeClassMutation(
		ctx,
		access,
		classID,
		policy.ActionClassTransferOwnership,
	)
	if err != nil {
		return Class{}, err
	}
	if err := service.requireRecentAuthentication(access.AuthenticatedAt); err != nil {
		return Class{}, err
	}
	params, err := (TransferClassOwnershipParams{
		NewOwnerUserID:  input.NewOwnerUserID,
		ExpectedVersion: input.ExpectedVersion,
	}).normalized()
	if err != nil {
		return Class{}, err
	}
	transferred, err := service.repository.TransferOwnership(
		ctx,
		tenantContext,
		classID,
		params,
		service.clock().UTC(),
	)
	if err != nil {
		return Class{}, err
	}
	return service.projectActorClass(ctx, access, tenantContext, transferred)
}

func (service *Service) projectActorClass(
	ctx context.Context,
	access AccessContext,
	tenantContext tenancy.Context,
	class Class,
) (Class, error) {
	enrollment, err := service.enrollmentLookup.FindActorEnrollment(
		ctx,
		tenantContext,
		class.ID,
	)
	if err != nil {
		return Class{}, fmt.Errorf("resolve class enrollment projection: %w", err)
	}
	class, _ = projectClassViewerAccess(
		service.authorizer,
		access,
		class,
		enrollment,
	)
	return class, nil
}

func (service *Service) authorizeClassMutation(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) (tenancy.Context, error) {
	if classID == uuid.Nil {
		return tenancy.Context{}, ErrClassNotFound
	}
	if _, err := service.AuthorizeClass(ctx, access, classID, action); err != nil {
		return tenancy.Context{}, err
	}
	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return tenancy.Context{}, ErrClassAccessDenied
	}
	return tenantContext, nil
}

func projectClassViewerAccess(
	authorizer policy.Authorizer,
	access AccessContext,
	class Class,
	enrollment *Enrollment,
) (Class, AccessContext) {
	resolved := access
	resolved.ClassRoles = nil
	class.ViewerAccess = ViewerAccess{}

	if enrollment != nil {
		status := enrollment.Status
		role := enrollment.ClassRole
		class.ViewerAccess.EnrollmentStatus = &status
		class.ViewerAccess.ClassRole = &role
		if status == EnrollmentStatusActive {
			resolved.ClassRoles = append(resolved.ClassRoles, role)
		}
	}
	if class.OwnerUserID == access.ActorID {
		role := policy.ClassRoleOwner
		class.ViewerAccess.ClassRole = &role
		resolved.ClassRoles = append(resolved.ClassRoles, role)
	}

	resource := policy.Resource{
		TenantID: class.TenantID,
		ClassID:  class.ID,
		State:    policy.ResourceState(class.Status),
	}
	class.ViewerAccess.CanUpdateClass = authorizer.Authorize(policy.Input{
		Subject: viewerSubject(resolved), Action: policy.ActionClassUpdate,
		Resource: resource,
	}).Allowed
	class.ViewerAccess.CanArchiveClass = authorizer.Authorize(policy.Input{
		Subject: viewerSubject(resolved), Action: policy.ActionClassArchive,
		Resource: resource,
	}).Allowed
	class.ViewerAccess.CanTransferOwnership = authorizer.Authorize(policy.Input{
		Subject: viewerSubject(resolved), Action: policy.ActionClassTransferOwnership,
		Resource: resource,
	}).Allowed
	class.ViewerAccess.CanManageEnrollments = authorizer.Authorize(policy.Input{
		Subject: viewerSubject(resolved), Action: policy.ActionEnrollmentManage,
		Resource: resource,
	}).Allowed
	class.ViewerAccess.CanJoinRoom = authorizer.Authorize(policy.Input{
		Subject: viewerSubject(resolved), Action: policy.ActionSessionJoin,
		Resource: resource,
	}).Allowed
	class.ViewerAccess.CanPublishMedia = authorizer.Authorize(policy.Input{
		Subject: viewerSubject(resolved), Action: policy.ActionMediaPublish,
		Resource: resource,
	}).Allowed
	class.ViewerAccess.CanLeave = class.OwnerUserID != access.ActorID &&
		enrollment != nil &&
		enrollment.Status == EnrollmentStatusActive &&
		authorizer.Authorize(policy.Input{
			Subject: viewerSubject(resolved), Action: policy.ActionEnrollmentLeave,
			Resource: resource,
		}).Allowed

	return class, resolved
}

func viewerSubject(access AccessContext) policy.Subject {
	return policy.Subject{
		ActorID:           access.ActorID,
		ActiveTenantID:    access.TenantID,
		MembershipActive:  access.MembershipActive,
		OrganizationRoles: append([]policy.OrganizationRole(nil), access.OrganizationRoles...),
		ClassRoles:        append([]policy.ClassRole(nil), access.ClassRoles...),
	}
}

func (service *Service) authorize(
	access AccessContext,
	action policy.Action,
	classID uuid.UUID,
	state policy.ResourceState,
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
			TenantID: access.TenantID, ClassID: classID, State: state,
		},
	})
	if !decision.Allowed {
		if decision.ConcealResource {
			return tenancy.Context{}, ErrClassNotFound
		}
		if decision.Reason == policy.DenialResourceState {
			return tenancy.Context{}, ErrInvalidClassTransition
		}
		return tenancy.Context{}, ErrClassAccessDenied
	}

	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return tenancy.Context{}, ErrClassAccessDenied
	}

	return tenantContext, nil
}

func (service *Service) requireRecentAuthentication(authenticatedAt time.Time) error {
	now := service.clock().UTC()
	authenticatedAt = authenticatedAt.UTC()
	if authenticatedAt.IsZero() ||
		authenticatedAt.After(now.Add(recentAuthFutureTolerance)) ||
		now.Sub(authenticatedAt) > service.recentAuthTTL {
		return ErrRecentAuthenticationRequired
	}
	return nil
}

type classCursorPayload struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	ScopeHash string `json:"scope_hash"`
}

func normalizeListClassesInput(
	input ListClassesInput,
	tenantID uuid.UUID,
) (ListClassesParams, error) {
	if input.Limit == 0 {
		input.Limit = defaultListLimit
	}
	if input.Limit < 1 || input.Limit > maximumListLimit {
		return ListClassesParams{}, fmt.Errorf(
			"%w: limit must be between 1 and %d",
			ErrInvalidListLimit,
			maximumListLimit,
		)
	}

	var status *ClassStatus
	if input.Status != nil {
		value := ClassStatus(strings.ToLower(strings.TrimSpace(string(*input.Status))))
		if err := validateClassStatus(value); err != nil {
			return ListClassesParams{}, err
		}
		status = &value
	}
	after, err := decodeClassCursor(strings.TrimSpace(input.Cursor), tenantID, status)
	if err != nil {
		return ListClassesParams{}, err
	}
	return ListClassesParams{Status: status, Limit: input.Limit, After: after}, nil
}

func encodeClassCursor(
	cursor ClassCursor,
	tenantID uuid.UUID,
	status *ClassStatus,
) (string, error) {
	statusValue := ""
	if status != nil {
		statusValue = string(*status)
	}
	contents, err := json.Marshal(classCursorPayload{
		CreatedAt: cursor.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        cursor.ID.String(),
		Status:    statusValue,
		ScopeHash: classListScopeHash(tenantID, status),
	})
	if err != nil {
		return "", err
	}
	return classCursorPrefix + base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeClassCursor(
	value string,
	tenantID uuid.UUID,
	status *ClassStatus,
) (*ClassCursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > maximumClassCursorLength || !strings.HasPrefix(value, classCursorPrefix) {
		return nil, ErrInvalidClassCursor
	}
	contents, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, classCursorPrefix))
	if err != nil {
		return nil, ErrInvalidClassCursor
	}
	var payload classCursorPayload
	if err := decodeStrictCursorJSON(contents, &payload); err != nil {
		return nil, ErrInvalidClassCursor
	}
	expectedStatus := ""
	if status != nil {
		expectedStatus = string(*status)
	}
	if payload.Status != expectedStatus ||
		payload.ScopeHash != classListScopeHash(tenantID, status) {
		return nil, ErrInvalidClassCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil || createdAt.IsZero() {
		return nil, ErrInvalidClassCursor
	}
	classID, err := uuid.Parse(payload.ID)
	if err != nil || classID == uuid.Nil {
		return nil, ErrInvalidClassCursor
	}
	return &ClassCursor{CreatedAt: createdAt.UTC(), ID: classID}, nil
}

func classListScopeHash(tenantID uuid.UUID, status *ClassStatus) string {
	statusValue := ""
	if status != nil {
		statusValue = string(*status)
	}
	digest := sha256.Sum256([]byte(tenantID.String() + "\x00" + statusValue))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
