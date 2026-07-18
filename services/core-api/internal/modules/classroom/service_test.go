package classroom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
	"github.com/tutorhub-v2/core-api/internal/policy/policytest"
)

var classroomServiceTestTime = time.Date(2026, 7, 18, 10, 0, 0, 0, time.UTC)

func TestServiceCreateUsesAuthenticatedActorAndTenant(t *testing.T) {
	t.Parallel()

	repository := &recordingRepository{}
	service := newClassroomTestService(t, repository)
	access := accessForOrganizationRole(
		uuid.New(),
		uuid.New(),
		policy.OrganizationRoleTeacher,
	)
	timezone := "Asia/Ho_Chi_Minh"

	created, err := service.Create(context.Background(), access, CreateClassInput{
		Code:        " sec-101 ",
		Title:       "  An toàn thông tin  ",
		Description: "  Lớp nền tảng  ",
		Timezone:    &timezone,
	})
	if err != nil {
		t.Fatalf("create class: %v", err)
	}
	if repository.tenantContext.TenantID != access.TenantID ||
		repository.tenantContext.ActorID != access.ActorID ||
		repository.createParams.OwnerUserID != access.ActorID {
		t.Fatalf("service did not use authenticated context: repository=%+v", repository)
	}
	if created.Code != "SEC-101" || created.Title != "An toàn thông tin" ||
		created.Timezone != timezone || created.Version != 1 {
		t.Fatalf("unexpected normalized class: %+v", created)
	}
}

func TestServiceEnforcesClassPermissions(t *testing.T) {
	t.Parallel()

	service := newClassroomTestService(t, &recordingRepository{})
	access := AccessContext{TenantID: uuid.New(), ActorID: uuid.New()}

	if _, err := service.List(
		context.Background(),
		access,
		ListClassesInput{Limit: 20},
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("list without class.view must be denied, got %v", err)
	}
	if _, err := service.Get(
		context.Background(),
		access,
		uuid.New(),
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("get without class.view must be denied, got %v", err)
	}
	if _, err := service.Create(context.Background(), access, CreateClassInput{
		Code: "SEC101", Title: "Class",
	}); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("create without class.create must be denied, got %v", err)
	}
}

func TestServiceUsesStableStatusBoundClassCursor(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	status := ClassStatusActive
	repository := &recordingRepository{listResult: ListClassesResult{
		Items: []Class{{
			ID: classID, TenantID: tenantID, Status: status,
			CreatedAt: classroomServiceTestTime,
		}},
		HasMore: true,
	}}
	service := newClassroomTestService(t, repository)
	access := accessForOrganizationRole(tenantID, actorID, policy.OrganizationRoleStudent)

	page, err := service.List(context.Background(), access, ListClassesInput{
		Status: &status,
		Limit:  25,
	})
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if page.NextCursor == "" || len(page.Items) != 1 ||
		repository.listParams.Status == nil ||
		*repository.listParams.Status != status {
		t.Fatalf("unexpected first page: page=%+v params=%+v", page, repository.listParams)
	}

	if _, err := service.List(context.Background(), access, ListClassesInput{
		Status: &status,
		Limit:  25,
		Cursor: page.NextCursor,
	}); err != nil {
		t.Fatalf("list next page: %v", err)
	}
	if repository.listParams.After == nil ||
		repository.listParams.After.ID != classID ||
		!repository.listParams.After.CreatedAt.Equal(classroomServiceTestTime) {
		t.Fatalf("cursor was not decoded: %+v", repository.listParams.After)
	}

	otherStatus := ClassStatusDraft
	if _, err := service.List(context.Background(), access, ListClassesInput{
		Status: &otherStatus,
		Limit:  25,
		Cursor: page.NextCursor,
	}); !errors.Is(err, ErrInvalidClassCursor) {
		t.Fatalf("cursor reused across filters must fail, got %v", err)
	}
	if _, err := service.List(context.Background(), access, ListClassesInput{
		Limit: 101,
	}); !errors.Is(err, ErrInvalidListLimit) {
		t.Fatalf("invalid limit must fail, got %v", err)
	}
}

func TestServiceRequiresRecentAuthenticationForOwnershipTransfer(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	targetID := uuid.New()
	repository := &recordingRepository{class: Class{
		ID:          classID,
		TenantID:    tenantID,
		OwnerUserID: actorID,
		Status:      ClassStatusActive,
		Version:     2,
	}}
	service := newClassroomTestService(t, repository)
	access := accessForOrganizationRole(tenantID, actorID, policy.OrganizationRoleTeacher)
	access.AuthenticatedAt = classroomServiceTestTime.Add(-11 * time.Minute)

	if _, err := service.TransferOwnership(
		context.Background(),
		access,
		classID,
		TransferClassOwnershipInput{NewOwnerUserID: targetID, ExpectedVersion: 2},
	); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("stale authentication must be rejected, got %v", err)
	}
	if repository.transferCalled {
		t.Fatal("stale authentication must not reach the repository")
	}

	access.AuthenticatedAt = classroomServiceTestTime.Add(-10 * time.Minute)
	if _, err := service.TransferOwnership(
		context.Background(),
		access,
		classID,
		TransferClassOwnershipInput{NewOwnerUserID: targetID, ExpectedVersion: 2},
	); err != nil {
		t.Fatalf("recent ownership transfer: %v", err)
	}
	if !repository.transferCalled ||
		repository.transferParams.NewOwnerUserID != targetID ||
		repository.transferParams.ExpectedVersion != 2 {
		t.Fatalf("unexpected transfer params: %+v", repository)
	}
}

func TestServiceForwardsVersionedLifecycleMutations(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	repository := &recordingRepository{class: Class{
		ID:          classID,
		TenantID:    tenantID,
		OwnerUserID: actorID,
		Status:      ClassStatusActive,
		Version:     3,
	}}
	service := newClassroomTestService(t, repository)
	access := accessForOrganizationRole(tenantID, actorID, policy.OrganizationRoleStudent)
	title := "Updated class"

	if _, err := service.Update(context.Background(), access, classID, UpdateClassInput{
		Title: &title, ExpectedVersion: 3,
	}); err != nil {
		t.Fatalf("update class: %v", err)
	}
	if repository.updateParams.ExpectedVersion != 3 ||
		repository.updateParams.Title == nil ||
		*repository.updateParams.Title != title {
		t.Fatalf("unexpected update params: %+v", repository.updateParams)
	}
	if _, err := service.Archive(context.Background(), access, classID, 4); err != nil {
		t.Fatalf("archive class: %v", err)
	}
	if _, err := service.Restore(context.Background(), access, classID, 5); err != nil {
		t.Fatalf("restore class: %v", err)
	}
	if repository.archiveVersion != 4 || repository.restoreVersion != 5 {
		t.Fatalf(
			"unexpected lifecycle versions: archive=%d restore=%d",
			repository.archiveVersion,
			repository.restoreVersion,
		)
	}
	if !repository.updatedAt.Equal(classroomServiceTestTime) {
		t.Fatalf("service clock was not used: %s", repository.updatedAt)
	}
}

func TestServicePreauthorizesLifecycleAndConcealsBeforeRecentAuth(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	classID := uuid.New()
	targetID := uuid.New()
	repository := &recordingRepository{class: Class{
		ID:          classID,
		TenantID:    tenantID,
		OwnerUserID: uuid.New(),
		Status:      ClassStatusActive,
		Version:     2,
	}}
	service := newClassroomTestService(t, repository)
	access := accessForOrganizationRole(tenantID, actorID, policy.OrganizationRoleTeacher)

	if _, err := service.Archive(
		context.Background(),
		access,
		classID,
		2,
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("non-owner teacher must fail service lifecycle preflight, got %v", err)
	}
	if repository.archiveVersion != 0 {
		t.Fatal("denied lifecycle mutation must not reach the repository")
	}

	repository.getErr = ErrClassNotFound
	access.AuthenticatedAt = classroomServiceTestTime.Add(-11 * time.Minute)
	if _, err := service.TransferOwnership(
		context.Background(),
		access,
		classID,
		TransferClassOwnershipInput{NewOwnerUserID: targetID, ExpectedVersion: 2},
	); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("missing class must be concealed before recent-auth response, got %v", err)
	}
	if repository.transferCalled {
		t.Fatal("concealed ownership transfer must not reach the repository")
	}
}

func newClassroomTestService(t *testing.T, repository Repository) *Service {
	t.Helper()
	service, err := NewService(
		repository,
		policy.NewEngine(),
		ServiceConfig{
			RecentAuthTTL: 10 * time.Minute,
			Clock:         func() time.Time { return classroomServiceTestTime },
		},
	)
	if err != nil {
		t.Fatalf("create classroom service: %v", err)
	}
	return service
}

type recordingRepository struct {
	tenantContext  tenancy.Context
	class          Class
	getErr         error
	createParams   CreateClassParams
	listParams     ListClassesParams
	listResult     ListClassesResult
	updateParams   UpdateClassParams
	transferParams TransferClassOwnershipParams
	archiveVersion int64
	restoreVersion int64
	updatedAt      time.Time
	transferCalled bool
}

func accessForOrganizationRole(
	tenantID uuid.UUID,
	actorID uuid.UUID,
	role policy.OrganizationRole,
) AccessContext {
	subject := policytest.ActiveOrganizationSubject(actorID, tenantID, role)
	return AccessContext{
		TenantID: subject.ActiveTenantID, ActorID: subject.ActorID,
		AuthenticatedAt:   classroomServiceTestTime,
		MembershipActive:  subject.MembershipActive,
		OrganizationRoles: subject.OrganizationRoles,
	}
}

func (repository *recordingRepository) Create(
	_ context.Context,
	tenantContext tenancy.Context,
	params CreateClassParams,
) (Class, error) {
	normalized, err := params.normalized()
	if err != nil {
		return Class{}, err
	}
	repository.tenantContext = tenantContext
	repository.createParams = normalized
	timezone := "UTC"
	if normalized.Timezone != nil {
		timezone = *normalized.Timezone
	}
	return Class{
		ID:          uuid.New(),
		TenantID:    tenantContext.TenantID,
		OwnerUserID: normalized.OwnerUserID,
		Code:        normalized.Code,
		Title:       normalized.Title,
		Description: normalized.Description,
		Timezone:    timezone,
		Status:      ClassStatusDraft,
		Version:     1,
	}, nil
}

func (repository *recordingRepository) Get(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
) (Class, error) {
	repository.tenantContext = tenantContext
	if repository.getErr != nil {
		return Class{}, repository.getErr
	}
	class := repository.class
	if class.ID == uuid.Nil {
		class.ID = classID
	}
	if class.TenantID == uuid.Nil {
		class.TenantID = tenantContext.TenantID
	}
	return class, nil
}

func (repository *recordingRepository) List(
	_ context.Context,
	tenantContext tenancy.Context,
	params ListClassesParams,
) (ListClassesResult, error) {
	repository.tenantContext = tenantContext
	repository.listParams = params
	return repository.listResult, nil
}

func (repository *recordingRepository) Update(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params UpdateClassParams,
	updatedAt time.Time,
) (Class, error) {
	repository.tenantContext = tenantContext
	repository.updateParams = params
	repository.updatedAt = updatedAt
	return Class{
		ID: classID, TenantID: tenantContext.TenantID,
		Version: params.ExpectedVersion + 1,
	}, nil
}

func (repository *recordingRepository) Archive(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	expectedVersion int64,
	updatedAt time.Time,
) (Class, error) {
	repository.tenantContext = tenantContext
	repository.archiveVersion = expectedVersion
	repository.updatedAt = updatedAt
	return Class{
		ID: classID, TenantID: tenantContext.TenantID,
		Status: ClassStatusArchived, Version: expectedVersion + 1,
	}, nil
}

func (repository *recordingRepository) Restore(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	expectedVersion int64,
	updatedAt time.Time,
) (Class, error) {
	repository.tenantContext = tenantContext
	repository.restoreVersion = expectedVersion
	repository.updatedAt = updatedAt
	return Class{
		ID: classID, TenantID: tenantContext.TenantID,
		Status: ClassStatusActive, Version: expectedVersion + 1,
	}, nil
}

func (repository *recordingRepository) TransferOwnership(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params TransferClassOwnershipParams,
	updatedAt time.Time,
) (Class, error) {
	repository.tenantContext = tenantContext
	repository.transferCalled = true
	repository.transferParams = params
	repository.updatedAt = updatedAt
	return Class{
		ID: classID, TenantID: tenantContext.TenantID,
		OwnerUserID: params.NewOwnerUserID, Version: params.ExpectedVersion + 1,
	}, nil
}
