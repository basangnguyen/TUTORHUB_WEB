package audit

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
)

type Action string

const (
	ActionTenantCreate               Action = "tenant.create"
	ActionTenantUpdate               Action = "tenant.update"
	ActionTenantArchive              Action = "tenant.archive"
	ActionTenantSwitch               Action = "tenant.switch"
	ActionTenantFeatureControlUpdate Action = "tenant.feature_control.update"
	ActionMembershipInvitationCreate Action = "membership.invitation.create"
	ActionMembershipInvitationRevoke Action = "membership.invitation.revoke"
	ActionMembershipInvitationAccept Action = "membership.invitation.accept"
	ActionMembershipInvitationExpire Action = "membership.invitation.expire"
	ActionClassCreate                Action = "class.create"
	ActionClassUpdate                Action = "class.update"
	ActionClassArchive               Action = "class.archive"
	ActionClassRestore               Action = "class.restore"
	ActionClassTransferOwnership     Action = "class.transfer_ownership"
	ActionClassEnrollmentEnroll      Action = "class.enrollment.enroll"
	ActionClassEnrollmentSuspend     Action = "class.enrollment.suspend"
	ActionClassEnrollmentRemove      Action = "class.enrollment.remove"
	ActionClassEnrollmentJoin        Action = "class.enrollment.join"
	ActionClassEnrollmentLeave       Action = "class.enrollment.leave"
	ActionClassEnrollmentUpdateRole  Action = "class.enrollment.update_role"
	ActionClassRosterBulk            Action = "class.roster.bulk"
	ActionClassInviteCodeCreate      Action = "class.invite_code.create"
	ActionClassInviteCodeRevoke      Action = "class.invite_code.revoke"
	ActionClassInviteCodeExpire      Action = "class.invite_code.expire"
	ActionClassInviteCodeExhaust     Action = "class.invite_code.exhaust"
	ActionClassSessionCreate         Action = "class.session.create"
	ActionClassSessionUpdate         Action = "class.session.update"
	ActionClassSessionCancel         Action = "class.session.cancel"
)

type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeDenied    Outcome = "denied"
	OutcomeFailed    Outcome = "failed"
)

type ActorType string

const (
	ActorTypeUser   ActorType = "user"
	ActorTypeSystem ActorType = "system"
)

type Metadata map[string]string

const MetadataKeyTargetUserID = "target_user_id"

type Draft struct {
	TenantID     uuid.UUID
	ActorID      uuid.UUID
	Action       Action
	ResourceType string
	ResourceID   uuid.UUID
	Outcome      Outcome
	Metadata     Metadata
	OccurredAt   time.Time
}

type DomainEvent struct {
	TenantID      uuid.UUID
	ActorID       uuid.UUID
	EventType     string
	AggregateType string
	AggregateID   uuid.UUID
	Metadata      Metadata
	OccurredAt    time.Time
}

type Actor struct {
	Type        ActorType  `json:"type"`
	UserID      *uuid.UUID `json:"user_id"`
	DisplayName *string    `json:"display_name"`
}

type Resource struct {
	Type string     `json:"type"`
	ID   *uuid.UUID `json:"id"`
}

type Event struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	Actor      Actor     `json:"actor"`
	Action     Action    `json:"action"`
	Resource   Resource  `json:"resource"`
	Outcome    Outcome   `json:"outcome"`
	RequestID  string    `json:"request_id"`
	Metadata   Metadata  `json:"metadata"`
	OccurredAt time.Time `json:"occurred_at"`
}

type Filter struct {
	OccurredFrom *time.Time
	OccurredTo   *time.Time
	Action       Action
	ResourceType string
	ResourceID   uuid.UUID
	Outcome      Outcome
	Limit        int
	Cursor       string
}

type Page struct {
	Items      []Event `json:"items"`
	NextCursor string  `json:"next_cursor,omitempty"`
}

type ExportSink interface {
	WriteAuditPage(context.Context, Page) error
}

type Exporter interface {
	Export(context.Context, uuid.UUID, Filter, ExportSink) error
}

type RetentionPolicy interface {
	Cutoff(uuid.UUID, time.Time) (time.Time, bool)
}

type DisabledRetentionPolicy struct{}

func (DisabledRetentionPolicy) Cutoff(uuid.UUID, time.Time) (time.Time, bool) {
	return time.Time{}, false
}

var (
	ErrInvalidFilter = errors.New("invalid audit filter")
	ErrAccessDenied  = errors.New("audit access denied")
	ErrNotFound      = errors.New("audit tenant not found")
)

var metadataKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var resourceTypePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
var forbiddenMetadataKeyPattern = regexp.MustCompile(
	`token|secret|password|cookie|session|email|name|description|payload|request_body|sql|error|stack|hash`,
)

var actionCatalog = map[Action]struct{}{
	ActionTenantCreate: {}, ActionTenantUpdate: {}, ActionTenantArchive: {}, ActionTenantSwitch: {},
	ActionTenantFeatureControlUpdate: {},
	ActionMembershipInvitationCreate: {}, ActionMembershipInvitationRevoke: {},
	ActionMembershipInvitationAccept: {}, ActionMembershipInvitationExpire: {},
	ActionClassCreate: {}, ActionClassUpdate: {}, ActionClassArchive: {}, ActionClassRestore: {},
	ActionClassTransferOwnership: {}, ActionClassEnrollmentEnroll: {},
	ActionClassEnrollmentSuspend: {}, ActionClassEnrollmentRemove: {},
	ActionClassEnrollmentJoin: {}, ActionClassEnrollmentLeave: {},
	ActionClassEnrollmentUpdateRole: {}, ActionClassInviteCodeCreate: {},
	ActionClassRosterBulk:       {},
	ActionClassInviteCodeRevoke: {}, ActionClassInviteCodeExpire: {},
	ActionClassInviteCodeExhaust: {},
	ActionClassSessionCreate:     {},
	ActionClassSessionUpdate:     {},
	ActionClassSessionCancel:     {},
}

var domainEventActions = map[string]Action{
	"tenant.created":                  ActionTenantCreate,
	"tenant.updated":                  ActionTenantUpdate,
	"tenant.archived":                 ActionTenantArchive,
	"tenant.switched":                 ActionTenantSwitch,
	"tenant.feature_controls.updated": ActionTenantFeatureControlUpdate,
	"membership.invitation.created":   ActionMembershipInvitationCreate,
	"membership.invitation.revoked":   ActionMembershipInvitationRevoke,
	"membership.invitation.accepted":  ActionMembershipInvitationAccept,
	"membership.invitation.expired":   ActionMembershipInvitationExpire,
	"class.created":                   ActionClassCreate,
	"class.updated":                   ActionClassUpdate,
	"class.archived":                  ActionClassArchive,
	"class.restored":                  ActionClassRestore,
	"class.ownership_transferred":     ActionClassTransferOwnership,
	"class.enrollment.created":        ActionClassEnrollmentEnroll,
	"class.enrollment.reactivated":    ActionClassEnrollmentEnroll,
	"class.enrollment.suspended":      ActionClassEnrollmentSuspend,
	"class.enrollment.removed":        ActionClassEnrollmentRemove,
	"class.enrollment.joined":         ActionClassEnrollmentJoin,
	"class.enrollment.rejoined":       ActionClassEnrollmentJoin,
	"class.enrollment.left":           ActionClassEnrollmentLeave,
	"class.enrollment.role_changed":   ActionClassEnrollmentUpdateRole,
	"class.invite_code.created":       ActionClassInviteCodeCreate,
	"class.invite_code.revoked":       ActionClassInviteCodeRevoke,
	"class.invite_code.expired":       ActionClassInviteCodeExpire,
	"class.invite_code.exhausted":     ActionClassInviteCodeExhaust,
	"class_session.scheduled.v1":      ActionClassSessionCreate,
	"class_session.rescheduled.v1":    ActionClassSessionUpdate,
	"class_session.cancelled.v1":      ActionClassSessionCancel,
}

func ActionForDomainEvent(eventType string) (Action, bool) {
	action, ok := domainEventActions[eventType]
	return action, ok
}

func Actions() []Action {
	actions := make([]Action, 0, len(actionCatalog))
	for action := range actionCatalog {
		actions = append(actions, action)
	}
	sort.Slice(actions, func(left, right int) bool { return actions[left] < actions[right] })
	return actions
}

func validateDraft(draft Draft) error {
	if draft.TenantID == uuid.Nil || len(draft.ResourceType) > 80 ||
		!resourceTypePattern.MatchString(draft.ResourceType) {
		return ErrInvalidFilter
	}
	if _, ok := actionCatalog[draft.Action]; !ok {
		return ErrInvalidFilter
	}
	switch draft.Outcome {
	case OutcomeSucceeded, OutcomeDenied, OutcomeFailed:
	default:
		return ErrInvalidFilter
	}
	return validateMetadata(draft.Metadata)
}

func validateMetadata(metadata Metadata) error {
	if len(metadata) > 32 {
		return ErrInvalidFilter
	}
	for key, value := range metadata {
		if !metadataKeyPattern.MatchString(key) || forbiddenMetadataKeyPattern.MatchString(key) {
			return ErrInvalidFilter
		}
		if len(value) > 256 {
			return ErrInvalidFilter
		}
	}
	return nil
}

func copyMetadata(metadata Metadata) Metadata {
	if len(metadata) == 0 {
		return Metadata{}
	}
	copy := make(Metadata, len(metadata))
	for key, value := range metadata {
		copy[key] = value
	}
	return copy
}
