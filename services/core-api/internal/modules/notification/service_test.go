package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

func TestServiceListDefaultsAndBindsCursorToActorAndFilter(t *testing.T) {
	t.Parallel()
	scope := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	createdAt := time.Date(2026, 7, 25, 9, 0, 0, 0, time.UTC)
	repository := &notificationRepositoryStub{
		listItems: []Notification{{ID: uuid.New(), CreatedAt: createdAt}},
		listMore:  true,
	}
	service, err := NewService(repository, nil)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	page, err := service.List(context.Background(), scope, ListInput{UnreadOnly: true})
	if err != nil {
		t.Fatalf("list notifications: %v", err)
	}
	if repository.listInput.Limit != defaultListLimit || page.NextCursor == "" {
		t.Fatalf("unexpected list result/input: %+v / %+v", page, repository.listInput)
	}

	repository.listMore = false
	if _, err := service.List(context.Background(), scope, ListInput{
		Limit: defaultListLimit, Cursor: page.NextCursor, UnreadOnly: false,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cursor reused across filter error = %v, want invalid input", err)
	}
	otherScope := scope
	otherScope.ActorID = uuid.New()
	if _, err := service.List(context.Background(), otherScope, ListInput{
		Limit: defaultListLimit, Cursor: page.NextCursor, UnreadOnly: true,
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cursor reused across actor error = %v, want invalid input", err)
	}
}

func TestServiceRejectsInvalidPreferenceBeforeRepository(t *testing.T) {
	t.Parallel()
	repository := &notificationRepositoryStub{}
	service, err := NewService(repository, nil)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	scope := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	tests := []PutPreferenceInput{
		{ExpectedVersion: -1, QuietHoursTimezone: "UTC"},
		{ExpectedVersion: 0, ReminderOffsetMinutes: 40321, QuietHoursTimezone: "UTC"},
		{ExpectedVersion: 0, QuietHoursTimezone: "local"},
		{ExpectedVersion: 0, QuietHoursTimezone: "Not/A_Real_Zone"},
		{
			ExpectedVersion: 0, QuietHoursTimezone: "UTC",
			QuietHoursEnabled: true, QuietHoursStart: stringPointer("08:00"),
		},
		{
			ExpectedVersion: 0, QuietHoursTimezone: "UTC",
			QuietHoursEnabled: true, QuietHoursStart: stringPointer("8:00"),
			QuietHoursEnd: stringPointer("17:00"),
		},
		{
			ExpectedVersion: 0, QuietHoursTimezone: "UTC",
			QuietHoursEnabled: true, QuietHoursStart: stringPointer("08:00"),
			QuietHoursEnd: stringPointer("08:00"),
		},
	}
	for index, input := range tests {
		if _, err := service.PutPreference(
			context.Background(), scope, input,
		); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("case %d error = %v, want invalid input", index, err)
		}
	}
	if repository.putCalls != 0 {
		t.Fatalf("invalid inputs reached repository %d times", repository.putCalls)
	}
}

func TestServiceNormalizesPreferenceAndUsesServerClock(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 25, 10, 30, 0, 0, time.FixedZone("ICT", 7*60*60))
	repository := &notificationRepositoryStub{}
	service, err := NewService(repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	scope := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	start, end := " 22:00 ", " 06:00 "
	_, err = service.PutPreference(context.Background(), scope, PutPreferenceInput{
		InAppEnabled: true, ReminderOffsetMinutes: 15,
		QuietHoursEnabled: true, QuietHoursStart: &start, QuietHoursEnd: &end,
		QuietHoursTimezone: " Asia/Ho_Chi_Minh ", ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("put preference: %v", err)
	}
	if repository.putInput.QuietHoursTimezone != "Asia/Ho_Chi_Minh" ||
		*repository.putInput.QuietHoursStart != "22:00" ||
		*repository.putInput.QuietHoursEnd != "06:00" ||
		!repository.putAt.Equal(now.UTC()) {
		t.Fatalf("preference was not normalized: %+v at %s", repository.putInput, repository.putAt)
	}
}

func TestServiceFailsClosedForInvalidScopeAndNotificationID(t *testing.T) {
	t.Parallel()
	service, err := NewService(&notificationRepositoryStub{}, nil)
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if _, err := service.UnreadCount(context.Background(), tenancy.Context{}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("invalid scope error = %v, want access denied", err)
	}
	scope := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	if _, err := service.MarkRead(context.Background(), scope, uuid.Nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil notification error = %v, want not found", err)
	}
}

func stringPointer(value string) *string { return &value }

type notificationRepositoryStub struct {
	listInput ListInput
	listItems []Notification
	listMore  bool
	listErr   error
	putInput  PutPreferenceInput
	putAt     time.Time
	putCalls  int
}

func (repository *notificationRepositoryStub) List(
	_ context.Context,
	_ tenancy.Context,
	input ListInput,
	_ listCursor,
) ([]Notification, bool, error) {
	repository.listInput = input
	return repository.listItems, repository.listMore, repository.listErr
}

func (*notificationRepositoryStub) UnreadCount(
	context.Context, tenancy.Context,
) (UnreadCount, error) {
	return UnreadCount{}, nil
}

func (*notificationRepositoryStub) MarkRead(
	context.Context, tenancy.Context, uuid.UUID, time.Time,
) (Notification, error) {
	return Notification{}, nil
}

func (*notificationRepositoryStub) MarkAllRead(
	context.Context, tenancy.Context, time.Time,
) (MarkAllResult, error) {
	return MarkAllResult{}, nil
}

func (*notificationRepositoryStub) GetPreference(
	context.Context, tenancy.Context,
) (Preference, error) {
	return Preference{}, nil
}

func (repository *notificationRepositoryStub) PutPreference(
	_ context.Context,
	_ tenancy.Context,
	input PutPreferenceInput,
	updatedAt time.Time,
) (Preference, error) {
	repository.putCalls++
	repository.putInput = input
	repository.putAt = updatedAt
	return Preference{}, nil
}
