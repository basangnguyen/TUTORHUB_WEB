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
				PermissionEnrollmentManage, PermissionSessionSchedule, PermissionSessionStart, PermissionSessionEnd,
				PermissionSessionJoin, PermissionParticipantAdmit,
				PermissionParticipantRemove, PermissionMediaPublish, PermissionChatSend,
			},
		},
		{
			name: "student",
			role: OrganizationRoleStudent,
			want: []Permission{PermissionTenantView},
		},
		{
			name: "guest",
			role: OrganizationRoleGuest,
			want: []Permission{PermissionTenantView},
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
				PermissionTenantView, PermissionClassUpdate, PermissionClassArchive,
				PermissionClassTransferOwner, PermissionClassView, PermissionEnrollmentManage,
				PermissionEnrollmentLeave, PermissionSessionSchedule,
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
				PermissionEnrollmentLeave, PermissionSessionSchedule,
				PermissionSessionStart, PermissionSessionEnd, PermissionSessionJoin,
				PermissionParticipantAdmit, PermissionParticipantRemove,
				PermissionMediaPublish, PermissionChatSend,
			},
		},
		{
			name: "teaching assistant",
			role: ClassRoleTeachingAssistant,
			want: []Permission{
				PermissionTenantView, PermissionClassView, PermissionEnrollmentLeave,
				PermissionSessionJoin, PermissionParticipantAdmit,
				PermissionMediaPublish, PermissionChatSend,
			},
		},
		{
			name: "student",
			role: ClassRoleStudent,
			want: []Permission{
				PermissionTenantView, PermissionClassView, PermissionEnrollmentLeave,
				PermissionSessionJoin, PermissionMediaPublish,
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
		PermissionEnrollmentLeave,
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
	}); decision.Allowed || decision.Reason != DenialPermission {
		t.Fatalf("unenrolled student session join must be denied: %+v", decision)
	}
	subject.ClassRoles = []ClassRole{ClassRoleStudent}
	if decision := engine.Authorize(Input{
		Subject: subject, Action: ActionSessionJoin, Resource: resource,
	}); !decision.Allowed {
		t.Fatalf("enrolled student session join should be allowed: %+v", decision)
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
	if decision := engine.Authorize(Input{
		Subject: subject, Action: ActionEnrollmentLeave, Resource: resource,
	}); !decision.Allowed {
		t.Fatalf("active enrollment role should be allowed to leave an archived class: %+v", decision)
	}
}

func TestClassLifecycleAndOwnershipPermissions(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	tenantID := uuid.New()
	classID := uuid.New()
	actorID := uuid.New()
	resource := Resource{
		TenantID: tenantID,
		ClassID:  classID,
		State:    ResourceStateActive,
	}
	tests := []struct {
		name             string
		organizationRole OrganizationRole
		classRoles       []ClassRole
		action           Action
		allowed          bool
	}{
		{
			name:             "organization admin archives",
			organizationRole: OrganizationRoleAdmin,
			action:           ActionClassArchive,
			allowed:          true,
		},
		{
			name:             "organization admin transfers ownership",
			organizationRole: OrganizationRoleAdmin,
			action:           ActionClassTransferOwnership,
			allowed:          true,
		},
		{
			name:             "owner archives",
			organizationRole: OrganizationRoleStudent,
			classRoles:       []ClassRole{ClassRoleOwner},
			action:           ActionClassArchive,
			allowed:          true,
		},
		{
			name:             "owner transfers ownership",
			organizationRole: OrganizationRoleStudent,
			classRoles:       []ClassRole{ClassRoleOwner},
			action:           ActionClassTransferOwnership,
			allowed:          true,
		},
		{
			name:             "teacher cannot archive another class",
			organizationRole: OrganizationRoleTeacher,
			action:           ActionClassArchive,
		},
		{
			name:             "co-teacher cannot transfer ownership",
			organizationRole: OrganizationRoleStudent,
			classRoles:       []ClassRole{ClassRoleCoTeacher},
			action:           ActionClassTransferOwnership,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision := engine.Authorize(Input{
				Subject: Subject{
					ActorID:           actorID,
					ActiveTenantID:    tenantID,
					MembershipActive:  true,
					OrganizationRoles: []OrganizationRole{test.organizationRole},
					ClassRoles:        test.classRoles,
				},
				Action:   test.action,
				Resource: resource,
			})
			if decision.Allowed != test.allowed {
				t.Fatalf("unexpected lifecycle decision: %+v", decision)
			}
		})
	}
}

func TestClassResourceStateRequiresActiveClassForRoomActions(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	subject := validTestSubject()
	subject.OrganizationRoles = []OrganizationRole{OrganizationRoleAdmin}
	resource := Resource{
		TenantID: subject.ActiveTenantID,
		ClassID:  uuid.New(),
	}

	for _, state := range []ResourceState{ResourceStateDraft, ResourceStateArchived} {
		resource.State = state
		decision := engine.Authorize(Input{
			Subject: subject, Action: ActionSessionJoin, Resource: resource,
		})
		if decision.Allowed || decision.Reason != DenialResourceState {
			t.Fatalf("room join must be blocked for %s class: %+v", state, decision)
		}
	}

	resource.State = ResourceStateArchived
	for _, action := range []Action{
		ActionClassArchive,
		ActionClassTransferOwnership,
		ActionEnrollmentManage,
	} {
		decision := engine.Authorize(Input{
			Subject: subject, Action: action, Resource: resource,
		})
		if !decision.Allowed {
			t.Fatalf("%s must remain available for archived class: %+v", action, decision)
		}
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

func TestOnlyOrganizationAdminCanManageTenantFeatures(t *testing.T) {
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
				ActorID:           actorID,
				ActiveTenantID:    tenantID,
				MembershipActive:  true,
				OrganizationRoles: []OrganizationRole{test.role},
			},
			Action: ActionTenantManageFeatures,
			Resource: Resource{
				TenantID: tenantID,
				State:    ResourceStateActive,
			},
		})
		if decision.Allowed != test.allowed {
			t.Fatalf("unexpected feature-management decision for %s: %+v", test.role, decision)
		}
	}
}

func TestOnlyOrganizationAdminCanViewTenantAudit(t *testing.T) {
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
			Action: ActionAuditView,
			Resource: Resource{
				TenantID: tenantID,
				State:    ResourceStateActive,
			},
		})
		if decision.Allowed != test.allowed {
			t.Fatalf("unexpected audit decision for %s: %+v", test.role, decision)
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
