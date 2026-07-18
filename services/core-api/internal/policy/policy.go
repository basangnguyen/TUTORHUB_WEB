package policy

import "github.com/google/uuid"

type OrganizationRole string

const (
	OrganizationRoleAdmin   OrganizationRole = "org_admin"
	OrganizationRoleTeacher OrganizationRole = "teacher"
	OrganizationRoleStudent OrganizationRole = "student"
	OrganizationRoleGuest   OrganizationRole = "guest"
)

type ClassRole string

const (
	ClassRoleOwner             ClassRole = "owner"
	ClassRoleCoTeacher         ClassRole = "co_teacher"
	ClassRoleTeachingAssistant ClassRole = "teaching_assistant"
	ClassRoleStudent           ClassRole = "student"
)

type Permission string

const (
	PermissionTenantView          Permission = "tenant.view"
	PermissionTenantManage        Permission = "tenant.manage"
	PermissionTenantManageMembers Permission = "tenant.manage_members"
	PermissionClassCreate         Permission = "class.create"
	PermissionClassUpdate         Permission = "class.update"
	PermissionClassArchive        Permission = "class.archive"
	PermissionClassTransferOwner  Permission = "class.transfer_ownership"
	PermissionClassView           Permission = "class.view"
	PermissionEnrollmentManage    Permission = "enrollment.manage"
	PermissionSessionStart        Permission = "session.start"
	PermissionSessionEnd          Permission = "session.end"
	PermissionSessionJoin         Permission = "session.join"
	PermissionParticipantAdmit    Permission = "participant.admit"
	PermissionParticipantRemove   Permission = "participant.remove"
	PermissionMediaPublish        Permission = "media.publish"
	PermissionChatSend            Permission = "chat.send"
)

type Action string

const (
	ActionTenantView             Action = Action(PermissionTenantView)
	ActionTenantManage           Action = Action(PermissionTenantManage)
	ActionTenantManageMembers    Action = Action(PermissionTenantManageMembers)
	ActionClassCreate            Action = Action(PermissionClassCreate)
	ActionClassUpdate            Action = Action(PermissionClassUpdate)
	ActionClassArchive           Action = Action(PermissionClassArchive)
	ActionClassTransferOwnership Action = Action(PermissionClassTransferOwner)
	ActionClassView              Action = Action(PermissionClassView)
	ActionEnrollmentManage       Action = Action(PermissionEnrollmentManage)
	ActionSessionStart           Action = Action(PermissionSessionStart)
	ActionSessionEnd             Action = Action(PermissionSessionEnd)
	ActionSessionJoin            Action = Action(PermissionSessionJoin)
	ActionParticipantAdmit       Action = Action(PermissionParticipantAdmit)
	ActionParticipantRemove      Action = Action(PermissionParticipantRemove)
	ActionMediaPublish           Action = Action(PermissionMediaPublish)
	ActionChatSend               Action = Action(PermissionChatSend)
)

type ResourceState string

const (
	ResourceStateUnknown  ResourceState = "unknown"
	ResourceStateDraft    ResourceState = "draft"
	ResourceStateActive   ResourceState = "active"
	ResourceStateArchived ResourceState = "archived"
)

type Subject struct {
	ActorID           uuid.UUID
	ActiveTenantID    uuid.UUID
	MembershipActive  bool
	OrganizationRoles []OrganizationRole
	ClassRoles        []ClassRole
}

type Resource struct {
	TenantID uuid.UUID
	ClassID  uuid.UUID
	State    ResourceState
}

type Input struct {
	Subject  Subject
	Action   Action
	Resource Resource
}

type DenialReason string

const (
	DenialNone               DenialReason = ""
	DenialInvalidSubject     DenialReason = "invalid_subject"
	DenialInactiveMembership DenialReason = "inactive_membership"
	DenialResourceScope      DenialReason = "resource_scope"
	DenialPermission         DenialReason = "permission"
	DenialResourceState      DenialReason = "resource_state"
)

type Decision struct {
	Allowed         bool
	Reason          DenialReason
	ConcealResource bool
}

type Authorizer interface {
	EffectivePermissions(Subject) []Permission
	Authorize(Input) Decision
}

type Engine struct{}

func NewEngine() *Engine {
	return &Engine{}
}

var permissionOrder = []Permission{
	PermissionTenantView,
	PermissionTenantManage,
	PermissionTenantManageMembers,
	PermissionClassCreate,
	PermissionClassUpdate,
	PermissionClassArchive,
	PermissionClassTransferOwner,
	PermissionClassView,
	PermissionEnrollmentManage,
	PermissionSessionStart,
	PermissionSessionEnd,
	PermissionSessionJoin,
	PermissionParticipantAdmit,
	PermissionParticipantRemove,
	PermissionMediaPublish,
	PermissionChatSend,
}

var organizationPermissions = map[OrganizationRole][]Permission{
	OrganizationRoleAdmin: append([]Permission(nil), permissionOrder...),
	OrganizationRoleTeacher: {
		PermissionTenantView,
		PermissionClassCreate,
		PermissionClassUpdate,
		PermissionClassView,
		PermissionEnrollmentManage,
		PermissionSessionStart,
		PermissionSessionEnd,
		PermissionSessionJoin,
		PermissionParticipantAdmit,
		PermissionParticipantRemove,
		PermissionMediaPublish,
		PermissionChatSend,
	},
	OrganizationRoleStudent: {
		PermissionTenantView,
		PermissionClassView,
		PermissionSessionJoin,
		PermissionMediaPublish,
		PermissionChatSend,
	},
	OrganizationRoleGuest: {
		PermissionTenantView,
		PermissionSessionJoin,
		PermissionChatSend,
	},
}

var classPermissions = map[ClassRole][]Permission{
	ClassRoleOwner: {
		PermissionClassUpdate,
		PermissionClassArchive,
		PermissionClassTransferOwner,
		PermissionClassView,
		PermissionEnrollmentManage,
		PermissionSessionStart,
		PermissionSessionEnd,
		PermissionSessionJoin,
		PermissionParticipantAdmit,
		PermissionParticipantRemove,
		PermissionMediaPublish,
		PermissionChatSend,
	},
	ClassRoleCoTeacher: {
		PermissionClassUpdate,
		PermissionClassView,
		PermissionEnrollmentManage,
		PermissionSessionStart,
		PermissionSessionEnd,
		PermissionSessionJoin,
		PermissionParticipantAdmit,
		PermissionParticipantRemove,
		PermissionMediaPublish,
		PermissionChatSend,
	},
	ClassRoleTeachingAssistant: {
		PermissionClassView,
		PermissionSessionJoin,
		PermissionParticipantAdmit,
		PermissionMediaPublish,
		PermissionChatSend,
	},
	ClassRoleStudent: {
		PermissionClassView,
		PermissionSessionJoin,
		PermissionMediaPublish,
		PermissionChatSend,
	},
}

func (engine *Engine) EffectivePermissions(subject Subject) []Permission {
	if engine == nil || !validSubject(subject) || !subject.MembershipActive {
		return []Permission{}
	}

	granted := make(map[Permission]struct{}, len(permissionOrder))
	for _, role := range subject.OrganizationRoles {
		for _, permission := range organizationPermissions[role] {
			granted[permission] = struct{}{}
		}
	}
	for _, role := range subject.ClassRoles {
		for _, permission := range classPermissions[role] {
			granted[permission] = struct{}{}
		}
	}

	permissions := make([]Permission, 0, len(granted))
	for _, permission := range permissionOrder {
		if _, ok := granted[permission]; ok {
			permissions = append(permissions, permission)
		}
	}

	return permissions
}

func (engine *Engine) Authorize(input Input) Decision {
	if engine == nil || !validSubject(input.Subject) {
		return Decision{Reason: DenialInvalidSubject}
	}
	if !input.Subject.MembershipActive {
		return Decision{Reason: DenialInactiveMembership}
	}
	if input.Resource.TenantID == uuid.Nil ||
		input.Resource.TenantID != input.Subject.ActiveTenantID {
		return Decision{Reason: DenialResourceScope, ConcealResource: true}
	}
	if !validAction(input.Action) {
		return Decision{Reason: DenialPermission}
	}
	if actionRequiresClass(input.Action) && input.Resource.ClassID == uuid.Nil {
		return Decision{Reason: DenialResourceScope, ConcealResource: true}
	}
	required := Permission(input.Action)
	hasPermission := false
	for _, permission := range engine.EffectivePermissions(input.Subject) {
		if permission == required {
			hasPermission = true
			break
		}
	}
	if !hasPermission {
		return Decision{Reason: DenialPermission}
	}
	if actionBlockedForResourceState(input.Action, input.Resource.State) {
		return Decision{Reason: DenialResourceState}
	}

	return Decision{Allowed: true, Reason: DenialNone}
}

func PermissionStrings(permissions []Permission) []string {
	values := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		values = append(values, string(permission))
	}
	return values
}

func validSubject(subject Subject) bool {
	return subject.ActorID != uuid.Nil && subject.ActiveTenantID != uuid.Nil &&
		len(subject.OrganizationRoles) > 0
}

func validAction(action Action) bool {
	for _, permission := range permissionOrder {
		if action == Action(permission) {
			return true
		}
	}
	return false
}

func actionRequiresClass(action Action) bool {
	switch action {
	case ActionTenantView, ActionTenantManage, ActionTenantManageMembers,
		ActionClassCreate, ActionClassView:
		return false
	default:
		return true
	}
}

func actionBlockedForResourceState(action Action, state ResourceState) bool {
	switch state {
	case ResourceStateUnknown, ResourceStateActive:
		return false
	case ResourceStateDraft:
		switch action {
		case ActionClassView, ActionClassUpdate, ActionClassArchive,
			ActionClassTransferOwnership:
			return false
		default:
			return actionRequiresClass(action)
		}
	case ResourceStateArchived:
		switch action {
		case ActionClassView, ActionClassArchive, ActionClassTransferOwnership:
			return false
		default:
			return actionRequiresClass(action)
		}
	default:
		return actionRequiresClass(action)
	}
}
