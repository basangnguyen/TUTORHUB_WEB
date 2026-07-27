package calendar

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	defaultListLimit      = 100
	maximumListLimit      = 500
	maximumQueryRange     = 366 * 24 * time.Hour
	maximumCursorLength   = 2048
	maximumSearchLength   = 200
	maximumClassIDFilters = 100
	cursorPrefix          = "thcal1_"
	SourceClassSession    = "class_session"
)

var (
	ErrInvalidInput  = errors.New("invalid calendar input")
	ErrInvalidRange  = errors.New("invalid calendar range")
	ErrInvalidCursor = errors.New("invalid calendar cursor")
	ErrAccessDenied  = errors.New("calendar access denied")
	ErrConflict      = errors.New("calendar preference version conflict")
	ErrScopeChanged  = errors.New("calendar active tenant changed")
)

type ViewerCapabilities struct {
	CanView       bool `json:"can_view"`
	CanEdit       bool `json:"can_edit"`
	CanCancel     bool `json:"can_cancel"`
	CanReschedule bool `json:"can_reschedule"`
}

type Item struct {
	ID                 string             `json:"id"`
	SourceType         string             `json:"source_type"`
	SourceID           uuid.UUID          `json:"source_id"`
	SeriesID           *uuid.UUID         `json:"series_id,omitempty"`
	OccurrenceKey      string             `json:"occurrence_key"`
	Title              string             `json:"title"`
	StartsAt           time.Time          `json:"starts_at"`
	EndsAt             time.Time          `json:"ends_at"`
	AllDay             bool               `json:"all_day"`
	DisplayTimezone    string             `json:"display_timezone"`
	ClassID            uuid.UUID          `json:"class_id"`
	ClassTitle         string             `json:"class_title"`
	Status             string             `json:"status"`
	ColorToken         string             `json:"color_token"`
	ViewerCapabilities ViewerCapabilities `json:"viewer_capabilities"`
	Version            int64              `json:"version"`
}

type ListInput struct {
	From           string
	To             string
	Types          []string
	ClassIDs       []string
	Statuses       []string
	Search         string
	Cursor         string
	Limit          int
	ViewerTimezone string
}

type Page struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type DisplayPreference struct {
	ViewerTimezone    string    `json:"viewer_timezone"`
	Locale            string    `json:"locale"`
	TimeFormat        string    `json:"time_format"`
	WeekStart         string    `json:"week_start"`
	DefaultView       string    `json:"default_view"`
	Density           string    `json:"density"`
	TimeScaleMinutes  int       `json:"time_scale_minutes"`
	SecondaryTimezone *string   `json:"secondary_timezone"`
	Version           int64     `json:"version"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type UpdatePreferenceInput struct {
	ViewerTimezone    string
	Locale            string
	TimeFormat        string
	WeekStart         string
	DefaultView       string
	Density           string
	TimeScaleMinutes  int
	SecondaryTimezone *string
	ExpectedVersion   int64
}

type ServiceAPI interface {
	ListItems(context.Context, tenancy.Context, ListInput) (Page, error)
	GetPreference(context.Context, tenancy.Context) (DisplayPreference, error)
	UpdatePreference(
		context.Context,
		tenancy.Context,
		UpdatePreferenceInput,
	) (DisplayPreference, error)
}

type listCursor struct {
	StartsAt      time.Time
	SourceType    string
	OccurrenceKey string
}

type listParams struct {
	From           time.Time
	To             time.Time
	Types          []string
	ClassIDs       []uuid.UUID
	Statuses       []string
	Search         string
	Limit          int
	ViewerTimezone string
	After          listCursor
}

func defaultPreference(
	viewerTimezone string,
	userLocale string,
	updatedAt time.Time,
) DisplayPreference {
	locale := "vi-VN"
	if userLocale == "en" || userLocale == "en-US" {
		locale = "en-US"
	}
	return DisplayPreference{
		ViewerTimezone:   viewerTimezone,
		Locale:           locale,
		TimeFormat:       "24h",
		WeekStart:        "monday",
		DefaultView:      "week",
		Density:          "comfortable",
		TimeScaleMinutes: 30,
		Version:          0,
		UpdatedAt:        updatedAt.UTC(),
	}
}
