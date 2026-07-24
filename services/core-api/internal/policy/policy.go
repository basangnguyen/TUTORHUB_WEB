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
	PermissionTenantView           Permission = "tenant.view"
	PermissionTenantManage         Permission = "tenant.manage"
	PermissionTenantManageMembers  Permission = "tenant.manage_members"
	PermissionTenantManageFeatures Permission = "tenant.manage_features"
	PermissionClassCreate          Permission = "class.create"
	PermissionClassUpdate          Permission = "class.update"
	PermissionClassArchive         Permission = "class.archive"
	PermissionClassTransferOwner   Permission = "class.transfer_ownership"
	PermissionClassView            Permission = "class.view"
	PermissionEnrollmentManage     Permission = "enrollment.manage"
	PermissionEnrollmentLeave      Permission = "enrollment.leave"
	PermissionSessionSchedule      Permission = "session.schedule"
	PermissionSessionStart         Permission = "session.start"
	PermissionSessionEnd           Permission = "session.end"
	PermissionSessionJoin          Permission = "session.join"
	PermissionParticipantAdmit     Permission = "participant.admit"
	PermissionParticipantRemove    Permission = "participant.remove"
	PermissionMediaPublish         Permission = "media.publish"
	PermissionChatSend             Permission = "chat.send"
	PermissionAuditView            Permission = "audit.view"
)

type Action string

const (
	ActionTenantView             Action = Action(PermissionTenantView)
	ActionTenantManage           Action = Action(PermissionTenantManage)
	ActionTenantManageMembers    Action = Action(PermissionTenantManageMembers)
	ActionTenantManageFeatures   Action = Action(PermissionTenantManageFeatures)
	ActionClassCreate            Action = Action(PermissionClassCreate)
	ActionClassUpdate            Action = Action(PermissionClassUpdate)
	ActionClassArchive           Action = Action(PermissionClassArchive)
	ActionClassTransferOwnership Action = Action(PermissionClassTransferOwner)
	ActionClassView              Action = Action(PermissionClassView)
	ActionEnrollmentManage       Action = Action(PermissionEnrollmentManage)
	ActionEnrollmentLeave        Action = Action(PermissionEnrollmentLeave)
	ActionSessionSchedule        Action = Action(PermissionSessionSchedule)
	ActionSessionStart           Action = Action(PermissionSessionStart)
	ActionSessionEnd             Action = Action(PermissionSessionEnd)
	ActionSessionJoin            Action = Action(PermissionSessionJoin)
	ActionParticipantAdmit       Action = Action(PermissionParticipantAdmit)
	ActionParticipantRemove      Action = Action(PermissionParticipantRemove)
	ActionMediaPublish           Action = Action(PermissionMediaPublish)
	ActionChatSend               Action = Action(PermissionChatSend)
	ActionAuditView              Action = Action(PermissionAuditView)
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

// RosterMutationAction identifies a target-aware class enrollment mutation. Class
// lifecycle and enrollment state transitions remain domain concerns; this type
// only answers whether the actor may exercise management authority over the
// target role.
type RosterMutationAction string

const (
	RosterMutationUpdateRole RosterMutationAction = "update_role"
	RosterMutationSuspend    RosterMutationAction = "suspend"
	RosterMutationRemove     RosterMutationAction = "remove"
)

type RosterMutationDenialReason string

const (
	RosterMutationDenialNone               RosterMutationDenialReason = ""
	RosterMutationDenialManagementRequired RosterMutationDenialReason = "management_required"
	RosterMutationDenialInvalidInput       RosterMutationDenialReason = "invalid_input"
	RosterMutationDenialSelf               RosterMutationDenialReason = "self_mutation"
	RosterMutationDenialProtectedOwner     RosterMutationDenialReason = "protected_owner"
	RosterMutationDenialUnknownRole        RosterMutationDenialReason = "unknown_role"
	RosterMutationDenialHierarchy          RosterMutationDenialReason = "hierarchy"
)

type RosterMutationInput struct {
	ManagementGranted       bool
	Action                  RosterMutationAction
	ActorID                 uuid.UUID
	TargetUserID            uuid.UUID
	ActorOrganizationRoles  []OrganizationRole
	ActorClassRoles         []ClassRole
	TargetOrganizationRoles []OrganizationRole
	TargetClassRole         ClassRole
	TargetIsOwner           bool
	DesiredClassRole        ClassRole
}

type RosterMutationDecision struct {
	Allowed bool
	Reason  RosterMutationDenialReason
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
	PermissionTenantManageFeatures,
	PermissionClassCreate,
	PermissionClassUpdate,
	PermissionClassArchive,
	PermissionClassTransferOwner,
	PermissionClassView,
	PermissionEnrollmentManage,
	PermissionEnrollmentLeave,
	PermissionSessionSchedule,
	PermissionSessionStart,
	PermissionSessionEnd,
	PermissionSessionJoin,
	PermissionParticipantAdmit,
	PermissionParticipantRemove,
	PermissionMediaPublish,
	PermissionChatSend,
	PermissionAuditView,
}

var organizationPermissions = map[OrganizationRole][]Permission{
	OrganizationRoleAdmin: append([]Permission(nil), permissionOrder...),
	OrganizationRoleTeacher: {
		PermissionTenantView,
		PermissionClassCreate,
		PermissionClassUpdate,
		PermissionClassView,
		PermissionEnrollmentManage,
		PermissionSessionSchedule,
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
	},
	OrganizationRoleGuest: {
		PermissionTenantView,
	},
}

var classPermissions = map[ClassRole][]Permission{
	ClassRoleOwner: {
		PermissionClassUpdate,
		PermissionClassArchive,
		PermissionClassTransferOwner,
		PermissionClassView,
		PermissionEnrollmentManage,
		PermissionEnrollmentLeave,
		PermissionSessionSchedule,
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
		PermissionEnrollmentLeave,
		PermissionSessionSchedule,
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
		PermissionEnrollmentLeave,
		PermissionSessionJoin,
		PermissionParticipantAdmit,
		PermissionMediaPublish,
		PermissionChatSend,
	},
	ClassRoleStudent: {
		PermissionClassView,
		PermissionEnrollmentLeave,
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

// CanMutateRoster applies the class roster hierarchy after the caller
// has obtained enrollment.manage through the regular Authorizer. Authority is
// strictly hierarchical: an actor cannot mutate a peer or grant a role at or
// above the actor's own level. Owner changes remain exclusive to the dedicated
// ownership-transfer boundary.
func CanMutateRoster(input RosterMutationInput) RosterMutationDecision {
	if !input.ManagementGranted {
		return RosterMutationDecision{Reason: RosterMutationDenialManagementRequired}
	}
	if !validRosterMutationAction(input.Action) || input.ActorID == uuid.Nil ||
		input.TargetUserID == uuid.Nil {
		return RosterMutationDecision{Reason: RosterMutationDenialInvalidInput}
	}
	if input.ActorID == input.TargetUserID {
		return RosterMutationDecision{Reason: RosterMutationDenialSelf}
	}
	if input.TargetIsOwner || input.TargetClassRole == ClassRoleOwner {
		return RosterMutationDecision{Reason: RosterMutationDenialProtectedOwner}
	}

	actorLevel, actorRolesValid := rosterAuthorityLevel(
		input.ActorOrganizationRoles,
		input.ActorClassRoles,
		true,
	)
	targetLevel, targetRolesValid := rosterAuthorityLevel(
		input.TargetOrganizationRoles,
		[]ClassRole{input.TargetClassRole},
		true,
	)
	if !actorRolesValid || !targetRolesValid {
		return RosterMutationDecision{Reason: RosterMutationDenialUnknownRole}
	}
	if actorLevel <= targetLevel {
		return RosterMutationDecision{Reason: RosterMutationDenialHierarchy}
	}

	switch input.Action {
	case RosterMutationUpdateRole:
		desiredLevel, valid := persistedClassRoleLevel(input.DesiredClassRole)
		if !valid {
			return RosterMutationDecision{Reason: RosterMutationDenialUnknownRole}
		}
		if actorLevel <= desiredLevel {
			return RosterMutationDecision{Reason: RosterMutationDenialHierarchy}
		}
	case RosterMutationSuspend, RosterMutationRemove:
		if input.DesiredClassRole != "" {
			return RosterMutationDecision{Reason: RosterMutationDenialInvalidInput}
		}
	}

	return RosterMutationDecision{Allowed: true, Reason: RosterMutationDenialNone}
}

func validRosterMutationAction(action RosterMutationAction) bool {
	switch action {
	case RosterMutationUpdateRole, RosterMutationSuspend, RosterMutationRemove:
		return true
	default:
		return false
	}
}

func rosterAuthorityLevel(
	organizationRoles []OrganizationRole,
	classRoles []ClassRole,
	requireOrganizationRole bool,
) (int, bool) {
	if requireOrganizationRole && len(organizationRoles) == 0 {
		return 0, false
	}
	level := 0
	for _, role := range organizationRoles {
		roleLevel, valid := organizationRosterLevel(role)
		if !valid {
			return 0, false
		}
		if roleLevel > level {
			level = roleLevel
		}
	}
	for _, role := range classRoles {
		roleLevel, valid := classRosterLevel(role)
		if !valid {
			return 0, false
		}
		if roleLevel > level {
			level = roleLevel
		}
	}
	return level, true
}

func organizationRosterLevel(role OrganizationRole) (int, bool) {
	switch role {
	case OrganizationRoleAdmin:
		return 4, true
	case OrganizationRoleTeacher:
		return 2, true
	case OrganizationRoleStudent, OrganizationRoleGuest:
		return 0, true
	default:
		return 0, false
	}
}

func classRosterLevel(role ClassRole) (int, bool) {
	switch role {
	case ClassRoleOwner:
		return 3, true
	case ClassRoleCoTeacher:
		return 2, true
	case ClassRoleTeachingAssistant:
		return 1, true
	case ClassRoleStudent:
		return 0, true
	default:
		return 0, false
	}
}

func persistedClassRoleLevel(role ClassRole) (int, bool) {
	switch role {
	case ClassRoleCoTeacher, ClassRoleTeachingAssistant, ClassRoleStudent:
		return classRosterLevel(role)
	default:
		return 0, false
	}
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
		ActionTenantManageFeatures,
		ActionClassCreate, ActionClassView, ActionAuditView:
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
		case ActionEnrollmentManage, ActionEnrollmentLeave:
			return false
		default:
			return actionRequiresClass(action)
		}
	default:
		return actionRequiresClass(action)
	}
}
