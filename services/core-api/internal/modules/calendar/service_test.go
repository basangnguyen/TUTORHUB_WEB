package calendar

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type serviceRepositoryStub struct {
	items             []Item
	hasMore           bool
	listParams        listParams
	preference        DisplayPreference
	updatedPreference DisplayPreference
	updateInput       UpdatePreferenceInput
}

func (stub *serviceRepositoryStub) ListItems(
	_ context.Context,
	_ tenancy.Context,
	params listParams,
) ([]Item, bool, error) {
	stub.listParams = params
	return stub.items, stub.hasMore, nil
}

func (stub *serviceRepositoryStub) GetPreference(
	context.Context,
	tenancy.Context,
) (DisplayPreference, error) {
	return stub.preference, nil
}

func (stub *serviceRepositoryStub) UpdatePreference(
	_ context.Context,
	_ tenancy.Context,
	input UpdatePreferenceInput,
	_ time.Time,
) (DisplayPreference, error) {
	stub.updateInput = input
	return stub.updatedPreference, nil
}

func TestListItemsNormalizesFiltersAndBindsCursorScope(t *testing.T) {
	t.Parallel()
	tenantID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	actorID := uuid.MustParse("20000000-0000-4000-8000-000000000002")
	classA := uuid.MustParse("30000000-0000-4000-8000-000000000003")
	classB := uuid.MustParse("40000000-0000-4000-8000-000000000004")
	sessionID := uuid.MustParse("50000000-0000-4000-8000-000000000005")
	startsAt := time.Date(2026, time.August, 3, 2, 0, 0, 0, time.UTC)
	repository := &serviceRepositoryStub{
		items: []Item{{
			ID:            SourceClassSession + ":" + sessionID.String(),
			SourceType:    SourceClassSession,
			SourceID:      sessionID,
			OccurrenceKey: sessionID.String(),
			StartsAt:      startsAt,
		}},
		hasMore: true,
	}
	service, err := NewService(repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	scope := tenancy.Context{TenantID: tenantID, ActorID: actorID}
	input := ListInput{
		From:           "2026-08-01T00:00:00+07:00",
		To:             "2026-08-31T00:00:00+07:00",
		Types:          []string{" class_session ", "class_session"},
		ClassIDs:       []string{classB.String(), classA.String(), classB.String()},
		Statuses:       []string{"scheduled", "cancelled", "scheduled"},
		Search:         "  ToÁn  ",
		ViewerTimezone: "Asia/Ho_Chi_Minh",
		Limit:          250,
	}
	page, err := service.ListItems(context.Background(), scope, input)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == "" {
		t.Fatal("expected scoped next cursor")
	}
	if got := repository.listParams.ClassIDs; len(got) != 2 || got[0] != classA || got[1] != classB {
		t.Fatalf("class filters were not sorted and deduplicated: %v", got)
	}
	if got := repository.listParams.Statuses; len(got) != 2 || got[0] != "cancelled" || got[1] != "scheduled" {
		t.Fatalf("status filters were not sorted and deduplicated: %v", got)
	}
	if repository.listParams.Search != "toán" {
		t.Fatalf("search not normalized: %q", repository.listParams.Search)
	}

	input.Cursor = page.NextCursor
	if _, err := service.ListItems(context.Background(), scope, input); err != nil {
		t.Fatalf("same scoped cursor rejected: %v", err)
	}
	if repository.listParams.After.OccurrenceKey != sessionID.String() {
		t.Fatalf("cursor occurrence not decoded: %+v", repository.listParams.After)
	}
	otherScope := tenancy.Context{
		TenantID: tenantID,
		ActorID:  uuid.MustParse("60000000-0000-4000-8000-000000000006"),
	}
	if _, err := service.ListItems(context.Background(), otherScope, input); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cursor replay by another actor must fail, got %v", err)
	}
	input.ViewerTimezone = "UTC"
	if _, err := service.ListItems(context.Background(), scope, input); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cursor replay with another timezone must fail, got %v", err)
	}
}

func TestListItemsEnforcesRangeLimitPageLimitAndIANAZone(t *testing.T) {
	t.Parallel()
	repository := &serviceRepositoryStub{}
	service, _ := NewService(repository, nil)
	scope := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	base := ListInput{
		From:           "2026-01-01T00:00:00Z",
		To:             "2027-01-03T00:00:00Z",
		ViewerTimezone: "Asia/Ho_Chi_Minh",
	}
	if _, err := service.ListItems(context.Background(), scope, base); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("range beyond 366 days must fail, got %v", err)
	}
	base.To = "2026-02-01T00:00:00Z"
	base.Limit = 501
	if _, err := service.ListItems(context.Background(), scope, base); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("page above 500 must fail, got %v", err)
	}
	base.Limit = 500
	base.ViewerTimezone = "local"
	if _, err := service.ListItems(context.Background(), scope, base); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("local pseudo-zone must fail, got %v", err)
	}
}

func TestUpdatePreferenceValidatesAndDelegatesFullReplacement(t *testing.T) {
	t.Parallel()
	repository := &serviceRepositoryStub{updatedPreference: DisplayPreference{Version: 2}}
	service, _ := NewService(repository, func() time.Time {
		return time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	})
	scope := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	secondary := "UTC"
	input := UpdatePreferenceInput{
		ViewerTimezone:    "Asia/Ho_Chi_Minh",
		Locale:            "vi-VN",
		TimeFormat:        "24h",
		WeekStart:         "monday",
		DefaultView:       "work_week",
		Density:           "compact",
		TimeScaleMinutes:  15,
		SecondaryTimezone: &secondary,
		ExpectedVersion:   1,
	}
	preference, err := service.UpdatePreference(context.Background(), scope, input)
	if err != nil {
		t.Fatal(err)
	}
	if preference.Version != 2 || repository.updateInput.DefaultView != "work_week" {
		t.Fatalf("preference was not delegated: %+v %+v", preference, repository.updateInput)
	}
	input.SecondaryTimezone = &input.ViewerTimezone
	if _, err := service.UpdatePreference(context.Background(), scope, input); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate secondary timezone must fail, got %v", err)
	}
}
