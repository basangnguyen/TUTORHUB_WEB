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
	ActionTenantCreate                Action = "tenant.create"
	ActionTenantUpdate                Action = "tenant.update"
	ActionTenantArchive               Action = "tenant.archive"
	ActionTenantSwitch                Action = "tenant.switch"
	ActionTenantFeatureControlUpdate  Action = "tenant.feature_control.update"
	ActionMembershipInvitationCreate  Action = "membership.invitation.create"
	ActionMembershipInvitationRevoke  Action = "membership.invitation.revoke"
	ActionMembershipInvitationAccept  Action = "membership.invitation.accept"
	ActionMembershipInvitationExpire  Action = "membership.invitation.expire"
	ActionClassCreate                 Action = "class.create"
	ActionClassUpdate                 Action = "class.update"
	ActionClassArchive                Action = "class.archive"
	ActionClassRestore                Action = "class.restore"
	ActionClassTransferOwnership      Action = "class.transfer_ownership"
	ActionClassEnrollmentEnroll       Action = "class.enrollment.enroll"
	ActionClassEnrollmentSuspend      Action = "class.enrollment.suspend"
	ActionClassEnrollmentRemove       Action = "class.enrollment.remove"
	ActionClassEnrollmentJoin         Action = "class.enrollment.join"
	ActionClassEnrollmentLeave        Action = "class.enrollment.leave"
	ActionClassEnrollmentUpdateRole   Action = "class.enrollment.update_role"
	ActionClassRosterBulk             Action = "class.roster.bulk"
	ActionClassInviteCodeCreate       Action = "class.invite_code.create"
	ActionClassInviteCodeRevoke       Action = "class.invite_code.revoke"
	ActionClassInviteCodeExpire       Action = "class.invite_code.expire"
	ActionClassInviteCodeExhaust      Action = "class.invite_code.exhaust"
	ActionClassSessionCreate          Action = "class.session.create"
	ActionClassSessionUpdate          Action = "class.session.update"
	ActionClassSessionCancel          Action = "class.session.cancel"
	ActionClassSessionAudienceReplace Action = "class.session.audience.replace"
	ActionClassSessionRSVPRespond     Action = "class.session.rsvp.respond"
	ActionConversationCreate          Action = "conversation.create"
	ActionAvailabilityPollCreate      Action = "availability.poll.create"
	ActionAvailabilityPollUpdate      Action = "availability.poll.update"
	ActionAvailabilityPollOpen        Action = "availability.poll.open"
	ActionAvailabilityPollClose       Action = "availability.poll.close"
	ActionAvailabilityPollReopen      Action = "availability.poll.reopen"
	ActionAvailabilityPollCancel      Action = "availability.poll.cancel"
	ActionAvailabilityPollRespond     Action = "availability.poll.respond"
	ActionAvailabilityPollShare       Action = "availability.poll.share"
	ActionAvailabilityPollRevoke      Action = "availability.poll.revoke"
	ActionAvailabilityPollFinalize    Action = "availability.poll.finalize"
	ActionStudyMeetingCreate          Action = "study_meeting.create"
	ActionStudyMeetingUpdate          Action = "study_meeting.update"
	ActionStudyMeetingCancel          Action = "study_meeting.cancel"
	ActionMediaSpaceCreate            Action = "media_space.create"
	ActionMediaSpaceStart             Action = "media_space.start"
	ActionMediaSpaceEnd               Action = "media_space.end"
	ActionMediaSpaceCancel            Action = "media_space.cancel"
	ActionMediaSpaceRecover           Action = "media_space.recover"
	ActionMediaSpaceMemberInvite      Action = "media_space_member.invite"
	ActionMediaSpaceMemberRevoke      Action = "media_space_member.revoke"
	ActionMediaSpaceMemberRestore     Action = "media_space_member.restore"
	ActionMediaAdmissionAdmit         Action = "media_admission.admit"
	ActionMediaAdmissionDeny          Action = "media_admission.deny"
	ActionMediaAdmissionCancel        Action = "media_admission.cancel"
	ActionMediaAdmissionRestore       Action = "media_admission.restore"
	ActionMediaAdmissionExpire        Action = "media_admission.expire"
	ActionMediaSpaceLock              Action = "media_space.lock"
	ActionMediaSpaceUnlock            Action = "media_space.unlock"
	ActionMediaParticipantPromote     Action = "media_participant.promote"
	ActionMediaParticipantDemote      Action = "media_participant.demote"
	ActionMediaParticipantMute        Action = "media_participant.mute"
	ActionMediaParticipantRemove      Action = "media_participant.remove"
	ActionMediaDiagnosticsExport      Action = "media_diagnostics.export"
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
	`token|secret|password|cookie|session|email|name|description|payload|request_body|sql|error|stack|hash|provider|join_attempt`,
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
	ActionClassInviteCodeExhaust:      {},
	ActionClassSessionCreate:          {},
	ActionClassSessionUpdate:          {},
	ActionClassSessionCancel:          {},
	ActionClassSessionAudienceReplace: {},
	ActionClassSessionRSVPRespond:     {},
	ActionConversationCreate:          {},
	ActionAvailabilityPollCreate:      {},
	ActionAvailabilityPollUpdate:      {},
	ActionAvailabilityPollOpen:        {},
	ActionAvailabilityPollClose:       {},
	ActionAvailabilityPollReopen:      {},
	ActionAvailabilityPollCancel:      {},
	ActionAvailabilityPollRespond:     {},
	ActionAvailabilityPollShare:       {},
	ActionAvailabilityPollRevoke:      {},
	ActionAvailabilityPollFinalize:    {},
	ActionStudyMeetingCreate:          {},
	ActionStudyMeetingUpdate:          {},
	ActionStudyMeetingCancel:          {},
	ActionMediaSpaceCreate:            {},
	ActionMediaSpaceStart:             {},
	ActionMediaSpaceEnd:               {},
	ActionMediaSpaceCancel:            {},
	ActionMediaSpaceRecover:           {},
	ActionMediaSpaceMemberInvite:      {},
	ActionMediaSpaceMemberRevoke:      {},
	ActionMediaSpaceMemberRestore:     {},
	ActionMediaAdmissionAdmit:         {},
	ActionMediaAdmissionDeny:          {},
	ActionMediaAdmissionCancel:        {},
	ActionMediaAdmissionRestore:       {},
	ActionMediaAdmissionExpire:        {},
	ActionMediaSpaceLock:              {},
	ActionMediaSpaceUnlock:            {},
	ActionMediaParticipantPromote:     {},
	ActionMediaParticipantDemote:      {},
	ActionMediaParticipantMute:        {},
	ActionMediaParticipantRemove:      {},
	ActionMediaDiagnosticsExport:      {},
}

var domainEventActions = map[string]Action{
	"tenant.created":                              ActionTenantCreate,
	"tenant.updated":                              ActionTenantUpdate,
	"tenant.archived":                             ActionTenantArchive,
	"tenant.switched":                             ActionTenantSwitch,
	"tenant.feature_controls.updated":             ActionTenantFeatureControlUpdate,
	"membership.invitation.created":               ActionMembershipInvitationCreate,
	"membership.invitation.revoked":               ActionMembershipInvitationRevoke,
	"membership.invitation.accepted":              ActionMembershipInvitationAccept,
	"membership.invitation.expired":               ActionMembershipInvitationExpire,
	"class.created":                               ActionClassCreate,
	"class.updated":                               ActionClassUpdate,
	"class.archived":                              ActionClassArchive,
	"class.restored":                              ActionClassRestore,
	"class.ownership_transferred":                 ActionClassTransferOwnership,
	"class.enrollment.created":                    ActionClassEnrollmentEnroll,
	"class.enrollment.reactivated":                ActionClassEnrollmentEnroll,
	"class.enrollment.suspended":                  ActionClassEnrollmentSuspend,
	"class.enrollment.removed":                    ActionClassEnrollmentRemove,
	"class.enrollment.joined":                     ActionClassEnrollmentJoin,
	"class.enrollment.rejoined":                   ActionClassEnrollmentJoin,
	"class.enrollment.left":                       ActionClassEnrollmentLeave,
	"class.enrollment.role_changed":               ActionClassEnrollmentUpdateRole,
	"class.invite_code.created":                   ActionClassInviteCodeCreate,
	"class.invite_code.revoked":                   ActionClassInviteCodeRevoke,
	"class.invite_code.expired":                   ActionClassInviteCodeExpire,
	"class.invite_code.exhausted":                 ActionClassInviteCodeExhaust,
	"class_session.scheduled.v1":                  ActionClassSessionCreate,
	"class_session.rescheduled.v1":                ActionClassSessionUpdate,
	"class_session.cancelled.v1":                  ActionClassSessionCancel,
	"class_session_series.scheduled.v1":           ActionClassSessionCreate,
	"class_session_series.updated.v1":             ActionClassSessionUpdate,
	"class_session_series.split.v1":               ActionClassSessionUpdate,
	"class_session_series.cancelled.v1":           ActionClassSessionCancel,
	"class_session_series.following_cancelled.v1": ActionClassSessionCancel,
	"class_session_occurrence.updated.v1":         ActionClassSessionUpdate,
	"class_session_occurrence.cancelled.v1":       ActionClassSessionCancel,
	"class_session.audience_replaced.v1":          ActionClassSessionAudienceReplace,
	"class_session.rsvp_responded.v1":             ActionClassSessionRSVPRespond,
	"conversation.created.v1":                     ActionConversationCreate,
	"availability_poll.created.v1":                ActionAvailabilityPollCreate,
	"availability_poll.updated.v1":                ActionAvailabilityPollUpdate,
	"availability_poll.opened.v1":                 ActionAvailabilityPollOpen,
	"availability_poll.reopened.v1":               ActionAvailabilityPollReopen,
	"availability_poll.response_recorded.v1":      ActionAvailabilityPollRespond,
	"availability_poll.closed.v1":                 ActionAvailabilityPollClose,
	"availability_poll.cancelled.v1":              ActionAvailabilityPollCancel,
	"availability_poll.capability_issued.v1":      ActionAvailabilityPollShare,
	"availability_poll.capability_revoked.v1":     ActionAvailabilityPollRevoke,
	"availability_poll.finalized.v1":              ActionAvailabilityPollFinalize,
	"study_meeting.scheduled.v1":                  ActionStudyMeetingCreate,
	"study_meeting.rescheduled.v1":                ActionStudyMeetingUpdate,
	"study_meeting.cancelled.v1":                  ActionStudyMeetingCancel,
	"media_space.created.v1":                      ActionMediaSpaceCreate,
	"media_space.started.v1":                      ActionMediaSpaceStart,
	"media_space.ended.v1":                        ActionMediaSpaceEnd,
	"media_space.cancelled.v1":                    ActionMediaSpaceCancel,
	"media_space.recovered.v1":                    ActionMediaSpaceRecover,
	"media_space_member.invited.v1":               ActionMediaSpaceMemberInvite,
	"media_space_member.revoked.v1":               ActionMediaSpaceMemberRevoke,
	"media_space_member.restored.v1":              ActionMediaSpaceMemberRestore,
	"media_admission.admitted.v1":                 ActionMediaAdmissionAdmit,
	"media_admission.denied.v1":                   ActionMediaAdmissionDeny,
	"media_admission.cancelled.v1":                ActionMediaAdmissionCancel,
	"media_admission.restored.v1":                 ActionMediaAdmissionRestore,
	"media_admission.expired.v1":                  ActionMediaAdmissionExpire,
	"media_space.locked.v1":                       ActionMediaSpaceLock,
	"media_space.unlocked.v1":                     ActionMediaSpaceUnlock,
	"media_participant.promoted.v1":               ActionMediaParticipantPromote,
	"media_participant.demoted.v1":                ActionMediaParticipantDemote,
	"media_participant.muted.v1":                  ActionMediaParticipantMute,
	"media_participant.removed.v1":                ActionMediaParticipantRemove,
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
