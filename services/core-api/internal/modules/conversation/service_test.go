package conversation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestServiceListDefaultsAndBindsCursorToTenantAndKind(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	updatedAt := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		listItems:   []Conversation{{ID: uuid.New(), Kind: KindDirect, UpdatedAt: updatedAt}},
		listHasMore: true,
	}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	access := testAccess(tenantID)
	kind := KindDirect
	page, err := service.List(context.Background(), access, ListInput{Kind: &kind})
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if repository.listInput.Limit != defaultListLimit || repository.listCursor.ID != uuid.Nil {
		t.Fatalf("unexpected normalized list input: %+v / %+v", repository.listInput, repository.listCursor)
	}
	if len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("unexpected page: %+v", page)
	}
	if _, err := service.List(context.Background(), testAccess(uuid.New()), ListInput{
		Kind: &kind, Cursor: page.NextCursor,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("foreign-tenant cursor error = %v, want invalid input", err)
	}
	classKind := KindClass
	if _, err := service.List(context.Background(), access, ListInput{
		Kind: &classKind, Cursor: page.NextCursor,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross-kind cursor error = %v, want invalid input", err)
	}
}

func TestServiceNormalizesDirectTargetAndRejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	repository := &fakeRepository{}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	access := testAccess(uuid.New())
	if _, err := service.CreateDirect(
		context.Background(), access, "  STUDENT@EXAMPLE.TEST ",
	); err != nil {
		t.Fatalf("create direct conversation: %v", err)
	}
	if repository.directEmail != "student@example.test" {
		t.Fatalf("normalized email = %q", repository.directEmail)
	}
	for _, value := range []string{"", "Display Name <student@example.test>", "not-an-email"} {
		if _, err := service.CreateDirect(context.Background(), access, value); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("email %q error = %v, want invalid input", value, err)
		}
	}
	if _, err := service.CreateClass(context.Background(), access, uuid.Nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil class error = %v, want not found", err)
	}
	invalidAccess := access
	invalidAccess.MembershipActive = false
	if _, err := service.Get(context.Background(), invalidAccess, uuid.New()); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("inactive access error = %v, want access denied", err)
	}
}

func TestServiceRejectsInvalidListBoundsAndKind(t *testing.T) {
	t.Parallel()
	service, err := NewService(&fakeRepository{})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	access := testAccess(uuid.New())
	unknown := Kind("group")
	for _, input := range []ListInput{
		{Limit: -1},
		{Limit: maximumListLimit + 1},
		{Kind: &unknown},
		{Cursor: "not-a-cursor"},
	} {
		if _, err := service.List(context.Background(), access, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("input %+v error = %v, want invalid input", input, err)
		}
	}
}

type fakeRepository struct {
	listInput   ListInput
	listCursor  listCursor
	listItems   []Conversation
	listHasMore bool
	listError   error
	directEmail string
	direct      CreateResult
	directError error
	classID     uuid.UUID
	class       CreateResult
	classError  error
	get         Conversation
	getError    error
}

func (repository *fakeRepository) List(
	_ context.Context,
	_ AccessContext,
	input ListInput,
	cursor listCursor,
) ([]Conversation, bool, error) {
	repository.listInput = input
	repository.listCursor = cursor
	return repository.listItems, repository.listHasMore, repository.listError
}

func (repository *fakeRepository) Get(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
) (Conversation, error) {
	return repository.get, repository.getError
}

func (repository *fakeRepository) CreateDirect(
	_ context.Context,
	_ AccessContext,
	email string,
) (CreateResult, error) {
	repository.directEmail = email
	return repository.direct, repository.directError
}

func (repository *fakeRepository) CreateClass(
	_ context.Context,
	_ AccessContext,
	classID uuid.UUID,
) (CreateResult, error) {
	repository.classID = classID
	return repository.class, repository.classError
}

func testAccess(tenantID uuid.UUID) AccessContext {
	return AccessContext{
		TenantID: tenantID, ActorID: uuid.New(), MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleStudent},
	}
}

var _ Repository = (*fakeRepository)(nil)
