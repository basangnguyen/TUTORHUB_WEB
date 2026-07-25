package notification

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type Service struct {
	repository Repository
	clock      func() time.Time
}

func NewService(repository Repository, clock func() time.Time) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("notification repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{repository: repository, clock: clock}, nil
}

func (service *Service) List(
	ctx context.Context,
	scope tenancy.Context,
	input ListInput,
) (Page, error) {
	if service == nil {
		return Page{}, fmt.Errorf("list notifications: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return Page{}, ErrAccessDenied
	}
	input.Cursor = strings.TrimSpace(input.Cursor)
	if input.Limit == 0 {
		input.Limit = defaultListLimit
	}
	if input.Limit < 1 || input.Limit > maximumListLimit {
		return Page{}, ErrInvalidInput
	}
	cursor, err := decodeCursor(scope, input)
	if err != nil {
		return Page{}, err
	}
	items, hasMore, err := service.repository.List(ctx, scope, input, cursor)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if page.Items == nil {
		page.Items = []Notification{}
	}
	if hasMore && len(items) > 0 {
		page.NextCursor, err = encodeCursor(scope, input, items[len(items)-1])
		if err != nil {
			return Page{}, err
		}
	}
	return page, nil
}

func (service *Service) UnreadCount(
	ctx context.Context,
	scope tenancy.Context,
) (UnreadCount, error) {
	if service == nil {
		return UnreadCount{}, fmt.Errorf("count unread notifications: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return UnreadCount{}, ErrAccessDenied
	}
	return service.repository.UnreadCount(ctx, scope)
}

func (service *Service) MarkRead(
	ctx context.Context,
	scope tenancy.Context,
	notificationID uuid.UUID,
) (Notification, error) {
	if service == nil {
		return Notification{}, fmt.Errorf("mark notification read: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return Notification{}, ErrAccessDenied
	}
	if notificationID == uuid.Nil {
		return Notification{}, ErrNotFound
	}
	return service.repository.MarkRead(
		ctx,
		scope,
		notificationID,
		service.clock().UTC(),
	)
}

func (service *Service) MarkAllRead(
	ctx context.Context,
	scope tenancy.Context,
) (MarkAllResult, error) {
	if service == nil {
		return MarkAllResult{}, fmt.Errorf("mark all notifications read: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return MarkAllResult{}, ErrAccessDenied
	}
	return service.repository.MarkAllRead(ctx, scope, service.clock().UTC())
}

func (service *Service) GetPreference(
	ctx context.Context,
	scope tenancy.Context,
) (Preference, error) {
	if service == nil {
		return Preference{}, fmt.Errorf("get notification preference: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return Preference{}, ErrAccessDenied
	}
	return service.repository.GetPreference(ctx, scope)
}

func (service *Service) PutPreference(
	ctx context.Context,
	scope tenancy.Context,
	input PutPreferenceInput,
) (Preference, error) {
	if service == nil {
		return Preference{}, fmt.Errorf("update notification preference: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return Preference{}, ErrAccessDenied
	}
	input, err := normalizePreferenceInput(input)
	if err != nil {
		return Preference{}, err
	}
	return service.repository.PutPreference(ctx, scope, input, service.clock().UTC())
}

func normalizePreferenceInput(input PutPreferenceInput) (PutPreferenceInput, error) {
	if input.ExpectedVersion < 0 || input.ReminderOffsetMinutes < 0 ||
		input.ReminderOffsetMinutes > 40320 {
		return PutPreferenceInput{}, ErrInvalidInput
	}
	input.QuietHoursTimezone = strings.TrimSpace(input.QuietHoursTimezone)
	if input.QuietHoursTimezone == "" || len(input.QuietHoursTimezone) > 100 ||
		strings.EqualFold(input.QuietHoursTimezone, "local") {
		return PutPreferenceInput{}, ErrInvalidInput
	}
	if _, err := time.LoadLocation(input.QuietHoursTimezone); err != nil {
		return PutPreferenceInput{}, ErrInvalidInput
	}
	if !input.QuietHoursEnabled {
		if input.QuietHoursStart != nil || input.QuietHoursEnd != nil {
			return PutPreferenceInput{}, ErrInvalidInput
		}
		return input, nil
	}
	if input.QuietHoursStart == nil || input.QuietHoursEnd == nil {
		return PutPreferenceInput{}, ErrInvalidInput
	}
	start := strings.TrimSpace(*input.QuietHoursStart)
	end := strings.TrimSpace(*input.QuietHoursEnd)
	if !validWallTime(start) || !validWallTime(end) || start == end {
		return PutPreferenceInput{}, ErrInvalidInput
	}
	input.QuietHoursStart = &start
	input.QuietHoursEnd = &end
	return input, nil
}

func validWallTime(value string) bool {
	parsed, err := time.Parse("15:04", value)
	return err == nil && parsed.Format("15:04") == value
}
