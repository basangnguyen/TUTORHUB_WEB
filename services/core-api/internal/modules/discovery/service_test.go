package discovery

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestServiceNormalizesBoundsAndEmptyPages(t *testing.T) {
	repository := &repositoryStub{}
	service, err := NewService(repository)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	access := validTestAccess()
	page, err := service.Search(context.Background(), access, "  Algebra  ", 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if repository.query != "algebra" || repository.limit != defaultSearchLimit {
		t.Fatalf("normalized search = %q/%d", repository.query, repository.limit)
	}
	if page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("search items = %#v, want empty non-nil", page.Items)
	}
	files, err := service.RecentFiles(context.Background(), access, 0)
	if err != nil {
		t.Fatalf("recent files: %v", err)
	}
	if files == nil || repository.limit != defaultRecentFileLimit {
		t.Fatalf("recent files = %#v, limit=%d", files, repository.limit)
	}
}

func TestServiceRejectsInvalidSearchAndAccess(t *testing.T) {
	service, _ := NewService(&repositoryStub{})
	for _, query := range []string{"", "x", string(make([]byte, maximumSearchRunes+1))} {
		if _, err := service.Search(context.Background(), validTestAccess(), query, 1); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("query %q error=%v, want invalid", query, err)
		}
	}
	if _, err := service.RecentFiles(context.Background(), AccessContext{}, 1); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("inactive access error=%v, want denied", err)
	}
	if _, err := service.Search(context.Background(), validTestAccess(), "valid", maximumSearchLimit+1); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("oversized limit error=%v, want invalid", err)
	}
}

func validTestAccess() AccessContext {
	return AccessContext{
		TenantID: uuid.New(), ActorID: uuid.New(), MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleStudent},
	}
}

type repositoryStub struct {
	query string
	limit int
}

func (stub *repositoryStub) RecentFiles(_ context.Context, _ AccessContext, limit int) ([]RecentFile, error) {
	stub.limit = limit
	return nil, nil
}

func (stub *repositoryStub) Search(_ context.Context, _ AccessContext, query string, limit int) ([]SearchResult, error) {
	stub.query, stub.limit = query, limit
	return nil, nil
}
