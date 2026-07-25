package notification

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

const (
	KindSystemWorkerCanary = "system.worker_canary"
	defaultListLimit       = 25
	maximumListLimit       = 100
	maximumCursorLength    = 1024
	maximumUnreadCount     = 1000
	defaultReminderOffset  = 15
	defaultQuietTimezone   = "Asia/Ho_Chi_Minh"
)

var (
	ErrInvalidInput = errors.New("invalid notification input")
	ErrAccessDenied = errors.New("notification access denied")
	ErrNotFound     = errors.New("notification not found")
	ErrConflict     = errors.New("notification preference version conflict")
	ErrScopeChanged = errors.New("notification active tenant changed")
)

type Notification struct {
	ID                  uuid.UUID       `json:"id"`
	TenantID            uuid.UUID       `json:"-"`
	RecipientUserID     uuid.UUID       `json:"-"`
	SourceOutboxEventID uuid.UUID       `json:"-"`
	EffectKey           string          `json:"-"`
	Kind                string          `json:"kind"`
	TemplateKey         string          `json:"template_key"`
	ResourceType        string          `json:"resource_type,omitempty"`
	ResourceID          *uuid.UUID      `json:"resource_id,omitempty"`
	Context             json.RawMessage `json:"context"`
	OccurredAt          time.Time       `json:"occurred_at"`
	ReadAt              *time.Time      `json:"read_at"`
	CreatedAt           time.Time       `json:"created_at"`
}

type ListInput struct {
	Limit      int
	Cursor     string
	UnreadOnly bool
}

type Page struct {
	Items      []Notification `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

type UnreadCount struct {
	Count    int  `json:"count"`
	IsCapped bool `json:"is_capped"`
}

type MarkAllResult struct {
	UpdatedCount int64 `json:"updated_count"`
}

type Preference struct {
	TenantID              uuid.UUID `json:"-"`
	UserID                uuid.UUID `json:"-"`
	InAppEnabled          bool      `json:"in_app_enabled"`
	EmailEnabled          bool      `json:"email_enabled"`
	ReminderOffsetMinutes int       `json:"reminder_offset_minutes"`
	QuietHoursEnabled     bool      `json:"quiet_hours_enabled"`
	QuietHoursStart       *string   `json:"quiet_hours_start"`
	QuietHoursEnd         *string   `json:"quiet_hours_end"`
	QuietHoursTimezone    string    `json:"quiet_hours_timezone"`
	Version               int64     `json:"version"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type PutPreferenceInput struct {
	InAppEnabled          bool
	EmailEnabled          bool
	ReminderOffsetMinutes int
	QuietHoursEnabled     bool
	QuietHoursStart       *string
	QuietHoursEnd         *string
	QuietHoursTimezone    string
	ExpectedVersion       int64
}

type CanaryProjection struct {
	SourceOutboxEventID uuid.UUID
	TenantID            uuid.UUID
	RecipientUserID     uuid.UUID
	OccurredAt          time.Time
}

type CanaryProjector interface {
	ProjectCanary(ctx context.Context, projection CanaryProjection) error
}

type ServiceAPI interface {
	List(ctx context.Context, scope tenancy.Context, input ListInput) (Page, error)
	UnreadCount(ctx context.Context, scope tenancy.Context) (UnreadCount, error)
	MarkRead(ctx context.Context, scope tenancy.Context, notificationID uuid.UUID) (Notification, error)
	MarkAllRead(ctx context.Context, scope tenancy.Context) (MarkAllResult, error)
	GetPreference(ctx context.Context, scope tenancy.Context) (Preference, error)
	PutPreference(
		ctx context.Context,
		scope tenancy.Context,
		input PutPreferenceInput,
	) (Preference, error)
}

func defaultPreference(scope tenancy.Context, timezone string) Preference {
	if timezone == "" {
		timezone = defaultQuietTimezone
	}
	return Preference{
		TenantID:              scope.TenantID,
		UserID:                scope.ActorID,
		InAppEnabled:          true,
		EmailEnabled:          false,
		ReminderOffsetMinutes: defaultReminderOffset,
		QuietHoursEnabled:     false,
		QuietHoursTimezone:    timezone,
		Version:               0,
	}
}
