package classroom

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

func TestServiceCreateUsesAuthenticatedActorAndTenant(t *testing.T) {
	t.Parallel()

	repository := &recordingRepository{}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create classroom service: %v", err)
	}
	access := AccessContext{
		TenantID:    uuid.New(),
		ActorID:     uuid.New(),
		Permissions: []string{permissionClassCreate},
	}

	created, err := service.Create(context.Background(), access, CreateClassInput{
		Code:        " sec-101 ",
		Title:       "  An toàn thông tin  ",
		Description: "  Lớp nền tảng  ",
	})
	if err != nil {
		t.Fatalf("create class: %v", err)
	}
	if repository.tenantContext.TenantID != access.TenantID ||
		repository.tenantContext.ActorID != access.ActorID ||
		repository.createParams.OwnerUserID != access.ActorID {
		t.Fatalf("service did not use authenticated context: repository=%+v", repository)
	}
	if created.Code != "SEC-101" || created.Title != "An toàn thông tin" {
		t.Fatalf("unexpected normalized class: %+v", created)
	}
}

func TestServiceEnforcesClassPermissions(t *testing.T) {
	t.Parallel()

	service, err := NewService(&recordingRepository{})
	if err != nil {
		t.Fatalf("create classroom service: %v", err)
	}
	access := AccessContext{TenantID: uuid.New(), ActorID: uuid.New()}

	if _, err := service.List(context.Background(), access, 20); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("list without class.view must be denied, got %v", err)
	}
	if _, err := service.Get(context.Background(), access, uuid.New()); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("get without class.view must be denied, got %v", err)
	}
	if _, err := service.Create(context.Background(), access, CreateClassInput{
		Code: "SEC101", Title: "Class",
	}); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("create without class.create must be denied, got %v", err)
	}
}

func TestServiceRejectsInvalidListLimit(t *testing.T) {
	t.Parallel()

	service, err := NewService(&recordingRepository{})
	if err != nil {
		t.Fatalf("create classroom service: %v", err)
	}
	access := AccessContext{
		TenantID: uuid.New(), ActorID: uuid.New(), Permissions: []string{permissionClassView},
	}

	if _, err := service.List(context.Background(), access, 101); !errors.Is(err, ErrInvalidListLimit) {
		t.Fatalf("expected invalid limit, got %v", err)
	}
}

type recordingRepository struct {
	tenantContext tenancy.Context
	createParams  CreateClassParams
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
	return Class{
		ID:          uuid.New(),
		TenantID:    tenantContext.TenantID,
		OwnerUserID: normalized.OwnerUserID,
		Code:        normalized.Code,
		Title:       normalized.Title,
		Description: normalized.Description,
		Status:      ClassStatusDraft,
	}, nil
}

func (repository *recordingRepository) Get(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
) (Class, error) {
	repository.tenantContext = tenantContext
	return Class{ID: classID, TenantID: tenantContext.TenantID}, nil
}

func (repository *recordingRepository) List(
	_ context.Context,
	tenantContext tenancy.Context,
	_ int,
) ([]Class, error) {
	repository.tenantContext = tenantContext
	return []Class{}, nil
}
