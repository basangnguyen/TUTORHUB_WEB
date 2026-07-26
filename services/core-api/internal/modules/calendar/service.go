package calendar

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type Service struct {
	repository Repository
	clock      func() time.Time
}

func NewService(repository Repository, clock func() time.Time) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("calendar repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{repository: repository, clock: clock}, nil
}

func (service *Service) ListItems(
	ctx context.Context,
	scope tenancy.Context,
	input ListInput,
) (Page, error) {
	if service == nil {
		return Page{}, fmt.Errorf("list calendar items: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return Page{}, ErrAccessDenied
	}
	params, err := normalizeListInput(scope, input)
	if err != nil {
		return Page{}, err
	}
	items, hasMore, err := service.repository.ListItems(ctx, scope, params)
	if err != nil {
		return Page{}, err
	}
	page := Page{Items: items}
	if page.Items == nil {
		page.Items = []Item{}
	}
	if hasMore && len(page.Items) > 0 {
		page.NextCursor, err = encodeCursor(scope, params, page.Items[len(page.Items)-1])
		if err != nil {
			return Page{}, fmt.Errorf("encode calendar cursor: %w", err)
		}
	}
	return page, nil
}

func (service *Service) GetPreference(
	ctx context.Context,
	scope tenancy.Context,
) (DisplayPreference, error) {
	if service == nil {
		return DisplayPreference{}, fmt.Errorf("get calendar preference: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return DisplayPreference{}, ErrAccessDenied
	}
	return service.repository.GetPreference(ctx, scope)
}

func (service *Service) UpdatePreference(
	ctx context.Context,
	scope tenancy.Context,
	input UpdatePreferenceInput,
) (DisplayPreference, error) {
	if service == nil {
		return DisplayPreference{}, fmt.Errorf("update calendar preference: service is unavailable")
	}
	if err := scope.Validate(); err != nil {
		return DisplayPreference{}, ErrAccessDenied
	}
	input, err := normalizePreferenceInput(input)
	if err != nil {
		return DisplayPreference{}, err
	}
	return service.repository.UpdatePreference(ctx, scope, input, service.clock().UTC())
}

func normalizeListInput(scope tenancy.Context, input ListInput) (listParams, error) {
	from, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.From))
	if err != nil {
		return listParams{}, ErrInvalidRange
	}
	to, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.To))
	if err != nil {
		return listParams{}, ErrInvalidRange
	}
	from, to = from.UTC(), to.UTC()
	if from.IsZero() || to.IsZero() || !to.After(from) || to.Sub(from) > maximumQueryRange {
		return listParams{}, ErrInvalidRange
	}
	viewerTimezone, err := normalizeTimezone(input.ViewerTimezone)
	if err != nil {
		return listParams{}, err
	}
	if input.Limit == 0 {
		input.Limit = defaultListLimit
	}
	if input.Limit < 1 || input.Limit > maximumListLimit {
		return listParams{}, ErrInvalidInput
	}
	types, err := normalizeAllowedValues(input.Types, map[string]struct{}{
		SourceClassSession: {},
	})
	if err != nil {
		return listParams{}, err
	}
	if len(types) == 0 {
		types = []string{SourceClassSession}
	}
	statuses, err := normalizeAllowedValues(input.Statuses, map[string]struct{}{
		"scheduled": {}, "cancelled": {}, "live": {}, "ended": {},
	})
	if err != nil {
		return listParams{}, err
	}
	classIDs, err := normalizeClassIDs(input.ClassIDs)
	if err != nil {
		return listParams{}, err
	}
	search := strings.ToLower(strings.TrimSpace(input.Search))
	if utf8.RuneCountInString(search) > maximumSearchLength {
		return listParams{}, ErrInvalidInput
	}
	params := listParams{
		From: from, To: to, Types: types, ClassIDs: classIDs, Statuses: statuses,
		Search: search, Limit: input.Limit, ViewerTimezone: viewerTimezone,
	}
	params.After, err = decodeCursor(input.Cursor, scope, params)
	if err != nil {
		return listParams{}, err
	}
	return params, nil
}

func normalizeAllowedValues(values []string, allowed map[string]struct{}) ([]string, error) {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := allowed[value]; !ok {
			return nil, ErrInvalidInput
		}
		unique[value] = struct{}{}
	}
	result := make([]string, 0, len(unique))
	for value := range unique {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeClassIDs(values []string) ([]uuid.UUID, error) {
	if len(values) > maximumClassIDFilters {
		return nil, ErrInvalidInput
	}
	unique := make(map[uuid.UUID]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		classID, err := uuid.Parse(value)
		if err != nil || classID == uuid.Nil {
			return nil, ErrInvalidInput
		}
		unique[classID] = struct{}{}
	}
	result := make([]uuid.UUID, 0, len(unique))
	for classID := range unique {
		result = append(result, classID)
	}
	sort.Slice(result, func(left int, right int) bool {
		return result[left].String() < result[right].String()
	})
	return result, nil
}

func normalizePreferenceInput(input UpdatePreferenceInput) (UpdatePreferenceInput, error) {
	if input.ExpectedVersion < 0 {
		return UpdatePreferenceInput{}, ErrInvalidInput
	}
	viewerTimezone, err := normalizeTimezone(input.ViewerTimezone)
	if err != nil {
		return UpdatePreferenceInput{}, err
	}
	input.ViewerTimezone = viewerTimezone
	input.Locale = strings.TrimSpace(input.Locale)
	input.TimeFormat = strings.TrimSpace(input.TimeFormat)
	input.WeekStart = strings.TrimSpace(input.WeekStart)
	input.DefaultView = strings.TrimSpace(input.DefaultView)
	input.Density = strings.TrimSpace(input.Density)
	if !oneOf(input.Locale, "vi-VN", "en-US") ||
		!oneOf(input.TimeFormat, "12h", "24h") ||
		!oneOf(input.WeekStart, "monday", "sunday") ||
		!oneOf(input.DefaultView, "day", "work_week", "week", "month", "agenda") ||
		!oneOf(input.Density, "comfortable", "compact") ||
		(input.TimeScaleMinutes != 15 && input.TimeScaleMinutes != 30 && input.TimeScaleMinutes != 60) {
		return UpdatePreferenceInput{}, ErrInvalidInput
	}
	if input.SecondaryTimezone != nil {
		secondary, err := normalizeTimezone(*input.SecondaryTimezone)
		if err != nil || secondary == viewerTimezone {
			return UpdatePreferenceInput{}, ErrInvalidInput
		}
		input.SecondaryTimezone = &secondary
	}
	return input, nil
}

func normalizeTimezone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 100 || strings.EqualFold(value, "local") {
		return "", ErrInvalidInput
	}
	if _, err := time.LoadLocation(value); err != nil {
		return "", ErrInvalidInput
	}
	return value, nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
