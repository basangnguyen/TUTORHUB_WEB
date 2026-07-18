package policy

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
)

func TestOrganizationPermissionMatrix(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	tests := []struct {
		name string
		role OrganizationRole
		want []Permission
	}{
		{
			name: "organization admin",
			role: OrganizationRoleAdmin,
			want: append([]Permission(nil), permissionOrder...),
		},
		{
			name: "teacher",
			role: OrganizationRoleTeacher,
			want: []Permission{
				PermissionTenantView, PermissionClassCreate, PermissionClassUpdate, PermissionClassView,
				PermissionEnrollmentManage, PermissionSessionStart, PermissionSessionEnd,
				PermissionSessionJoin, PermissionParticipantAdmit,
				PermissionParticipantRemove, PermissionMediaPublish, PermissionChatSend,
			},
		},
		{
			name: "student",
			role: OrganizationRoleStudent,
			want: []Permission{
				PermissionTenantView, PermissionClassView, PermissionSessionJoin, PermissionMediaPublish,
				PermissionChatSend,
			},
		},
		{
			name: "guest",
			role: OrganizationRoleGuest,
			want: []Permission{PermissionTenantView, PermissionSessionJoin, PermissionChatSend},
		},
	}

	engine := NewEngine()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := engine.EffectivePermissions(Subject{
				ActorID: actorID, ActiveTenantID: tenantID, MembershipActive: true,
				OrganizationRoles: []OrganizationRole{test.role},
			})
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("unexpected permissions for %s: got=%v want=%v", test.role, got, test.want)
			}
		})
	}
}

func TestClassPermissionMatrix(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	tests := []struct {
		name string
		role ClassRole
		want []Permission
	}{
		{
			name: "owner",
			role: ClassRoleOwner,
			want: []Permission{
				PermissionTenantView, PermissionClassUpdate, PermissionClassView, PermissionEnrollmentManage,
				PermissionSessionStart, PermissionSessionEnd, PermissionSessionJoin,
				PermissionParticipantAdmit, PermissionParticipantRemove,
				PermissionMediaPublish, PermissionChatSend,
			},
		},
		{
			name: "co-teacher",
			role: ClassRoleCoTeacher,
			want: []Permission{
				PermissionTenantView, PermissionClassUpdate, PermissionClassView, PermissionEnrollmentManage,
				PermissionSessionStart, PermissionSessionEnd, PermissionSessionJoin,
				PermissionParticipantAdmit, PermissionParticipantRemove,
				PermissionMediaPublish, PermissionChatSend,
			},
		},
		{
			name: "teaching assistant",
			role: ClassRoleTeachingAssistant,
			want: []Permission{
				PermissionTenantView, PermissionClassView, PermissionSessionJoin, PermissionParticipantAdmit,
				PermissionMediaPublish, PermissionChatSend,
			},
		},
		{
			name: "student",
			role: ClassRoleStudent,
			want: []Permission{
				PermissionTenantView, PermissionClassView, PermissionSessionJoin, PermissionMediaPublish,
				PermissionChatSend,
			},
		},
	}

	engine := NewEngine()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := engine.EffectivePermissions(Subject{
				ActorID: actorID, ActiveTenantID: tenantID, MembershipActive: true,
				OrganizationRoles: []OrganizationRole{OrganizationRoleGuest},
				ClassRoles:        []ClassRole{test.role},
			})
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("unexpected permissions for %s: got=%v want=%v", test.role, got, test.want)
			}
		})
	}
}

func TestEffectivePermissionsUnionsMultipleRolesDeterministically(t *testing.T) {
	t.Parallel()

	subject := validTestSubject()
	subject.OrganizationRoles = []OrganizationRole{OrganizationRoleStudent, OrganizationRoleGuest}
	subject.ClassRoles = []ClassRole{ClassRoleTeachingAssistant, ClassRoleStudent}

	want := []Permission{
		PermissionTenantView,
		PermissionClassView,
		PermissionSessionJoin,
		PermissionParticipantAdmit,
		PermissionMediaPublish,
		PermissionChatSend,
	}
	if got := NewEngine().EffectivePermissions(subject); !reflect.DeepEqual(got, want) {
		t.Fatalf("effective permission union is not deterministic: got=%v want=%v", got, want)
	}
}

func TestAuthorizeDeniesInvalidAndCrossScopeRequests(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	classID := uuid.New()
	valid := Input{
		Subject: validTestSubject(),
		Action:  ActionClassView,
		Resource: Resource{
			TenantID: validTestSubject().ActiveTenantID,
			ClassID:  classID,
			State:    ResourceStateActive,
		},
	}
	tests := []struct {
		name    string
		mutate  func(*Input)
		reason  DenialReason
		conceal bool
	}{
		{
			name:   "missing actor",
			mutate: func(input *Input) { input.Subject.ActorID = uuid.Nil },
			reason: DenialInvalidSubject,
		},
		{
			name:   "missing active tenant",
			mutate: func(input *Input) { input.Subject.ActiveTenantID = uuid.Nil },
			reason: DenialInvalidSubject,
		},
		{
			name:   "inactive membership",
			mutate: func(input *Input) { input.Subject.MembershipActive = false },
			reason: DenialInactiveMembership,
		},
		{
			name:    "missing resource tenant",
			mutate:  func(input *Input) { input.Resource.TenantID = uuid.Nil },
			reason:  DenialResourceScope,
			conceal: true,
		},
		{
			name:    "cross-tenant resource",
			mutate:  func(input *Input) { input.Resource.TenantID = uuid.New() },
			reason:  DenialResourceScope,
			conceal: true,
		},
		{
			name: "missing class scope",
			mutate: func(input *Input) {
				input.Action = ActionSessionJoin
				input.Resource.ClassID = uuid.Nil
			},
			reason:  DenialResourceScope,
			conceal: true,
		},
		{
			name:   "unknown action",
			mutate: func(input *Input) { input.Action = Action("unknown") },
			reason: DenialPermission,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := valid
			test.mutate(&input)
			decision := engine.Authorize(input)
			if decision.Allowed || decision.Reason != test.reason ||
				decision.ConcealResource != test.conceal {
				t.Fatalf("unexpected decision: %+v", decision)
			}
		})
	}
}

func TestAuthorizeUsesPermissionAndResourceState(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	subject := validTestSubject()
	subject.OrganizationRoles = []OrganizationRole{OrganizationRoleStudent}
	resource := Resource{
		TenantID: subject.ActiveTenantID,
		ClassID:  uuid.New(),
		State:    ResourceStateActive,
	}

	if decision := engine.Authorize(Input{
		Subject: subject, Action: ActionSessionJoin, Resource: resource,
	}); !decision.Allowed {
		t.Fatalf("student session join should be allowed: %+v", decision)
	}
	if decision := engine.Authorize(Input{
		Subject: subject, Action: ActionParticipantRemove, Resource: resource,
	}); decision.Allowed || decision.Reason != DenialPermission {
		t.Fatalf("student participant removal must be denied: %+v", decision)
	}

	resource.State = ResourceStateArchived
	if decision := engine.Authorize(Input{
		Subject: subject, Action: ActionSessionJoin, Resource: resource,
	}); decision.Allowed || decision.Reason != DenialResourceState {
		t.Fatalf("archived class join must be denied: %+v", decision)
	}
	if decision := engine.Authorize(Input{
		Subject: subject, Action: ActionClassView, Resource: resource,
	}); !decision.Allowed {
		t.Fatalf("archived class detail should remain visible: %+v", decision)
	}
}

func TestOnlyOrganizationAdminCanManageTenantMembers(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	engine := NewEngine()
	for _, test := range []struct {
		role    OrganizationRole
		allowed bool
	}{
		{role: OrganizationRoleAdmin, allowed: true},
		{role: OrganizationRoleTeacher},
		{role: OrganizationRoleStudent},
		{role: OrganizationRoleGuest},
	} {
		decision := engine.Authorize(Input{
			Subject: Subject{
				ActorID:          actorID,
				ActiveTenantID:   tenantID,
				MembershipActive: true,
				OrganizationRoles: []OrganizationRole{
					test.role,
				},
			},
			Action: ActionTenantManageMembers,
			Resource: Resource{
				TenantID: tenantID,
				State:    ResourceStateActive,
			},
		})
		if decision.Allowed != test.allowed {
			t.Fatalf("unexpected member-management decision for %s: %+v", test.role, decision)
		}
	}
}

func validTestSubject() Subject {
	return Subject{
		ActorID:           uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		ActiveTenantID:    uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		MembershipActive:  true,
		OrganizationRoles: []OrganizationRole{OrganizationRoleStudent},
	}
}
