package discovery

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	defaultRecentFileLimit = 5
	maximumRecentFileLimit = 10
	defaultSearchLimit     = 12
	maximumSearchLimit     = 20
	minimumSearchRunes     = 2
	maximumSearchRunes     = 100
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("discovery repository is required")
	}
	return &Service{repository: repository}, nil
}

func (service *Service) RecentFiles(
	ctx context.Context,
	access AccessContext,
	limit int,
) ([]RecentFile, error) {
	if service == nil {
		return nil, fmt.Errorf("list recent files: service is unavailable")
	}
	if !validAccess(access) {
		return nil, ErrAccessDenied
	}
	if limit == 0 {
		limit = defaultRecentFileLimit
	}
	if limit < 1 || limit > maximumRecentFileLimit {
		return nil, ErrInvalidInput
	}
	items, err := service.repository.RecentFiles(ctx, access, limit)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []RecentFile{}
	}
	return items, nil
}

func (service *Service) Search(
	ctx context.Context,
	access AccessContext,
	query string,
	limit int,
) (SearchPage, error) {
	if service == nil {
		return SearchPage{}, fmt.Errorf("search resources: service is unavailable")
	}
	if !validAccess(access) {
		return SearchPage{}, ErrAccessDenied
	}
	query = strings.ToLower(strings.TrimSpace(query))
	queryRunes := utf8.RuneCountInString(query)
	if queryRunes < minimumSearchRunes || queryRunes > maximumSearchRunes {
		return SearchPage{}, ErrInvalidInput
	}
	if limit == 0 {
		limit = defaultSearchLimit
	}
	if limit < 1 || limit > maximumSearchLimit {
		return SearchPage{}, ErrInvalidInput
	}
	items, err := service.repository.Search(ctx, access, query, limit)
	if err != nil {
		return SearchPage{}, err
	}
	if items == nil {
		items = []SearchResult{}
	}
	return SearchPage{Items: items}, nil
}

func validAccess(access AccessContext) bool {
	return access.TenantID != uuid.Nil && access.ActorID != uuid.Nil &&
		access.MembershipActive && len(access.OrganizationRoles) > 0
}
