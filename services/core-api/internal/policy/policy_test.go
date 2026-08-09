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
				PermissionConversationCreateDirect, PermissionMessageWriteDirect,
				PermissionAvailabilityPollCreate, PermissionAvailabilityPollManageOwn,
				PermissionAvailabilityPollPublishToClass, PermissionStudyMeetingScheduleOwn,
				PermissionRoomCreateInstant,
				PermissionEnrollmentManage, PermissionSessionSchedule, PermissionSessionStart, PermissionSessionEnd,
				PermissionSessionJoin, PermissionParticipantAdmit,
				PermissionParticipantRemove, PermissionMediaPublish, PermissionMediaShareScreen,
				PermissionChatSend,
				PermissionFileView, PermissionFileUpload,
			},
		},
		{
			name: "student",
			role: OrganizationRoleStudent,
			want: []Permission{
				PermissionTenantView, PermissionConversationCreateDirect, PermissionMessageWriteDirect,
				PermissionAvailabilityPollCreate,
				PermissionAvailabilityPollManageOwn, PermissionStudyMeetingScheduleOwn,
				PermissionRoomCreateInstant,
			},
		},
		{
			name: "guest",
			role: OrganizationRoleGuest,
			want: []Permission{
				PermissionTenantView, PermissionConversationCreateDirect, PermissionMessageWriteDirect,
				PermissionAvailabilityPollCreate,
				PermissionAvailabilityPollManageOwn, PermissionStudyMeetingScheduleOwn,
				PermissionRoomCreateInstant,
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
				PermissionClassTransferOwner, PermissionClassView,
				PermissionConversationCreateDirect, PermissionMessageWriteDirect,
				PermissionAvailabilityPollCreate, PermissionAvailabilityPollManageOwn,
				PermissionAvailabilityPollPublishToClass, PermissionStudyMeetingScheduleOwn,
				PermissionRoomCreateInstant,
				PermissionEnrollmentManage, PermissionEnrollmentLeave, PermissionSessionSchedule,
				PermissionSessionStart, PermissionSessionEnd, PermissionSessionJoin,
				PermissionParticipantAdmit, PermissionParticipantRemove,
				PermissionMediaPublish, PermissionMediaShareScreen, PermissionChatSend,
				PermissionFileView, PermissionFileUpload,
			},
		},
		{
			name: "co-teacher",
			role: ClassRoleCoTeacher,
			want: []Permission{
				PermissionTenantView, PermissionClassUpdate, PermissionClassView,
				PermissionConversationCreateDirect, PermissionMessageWriteDirect,
				PermissionAvailabilityPollCreate, PermissionAvailabilityPollManageOwn,
				PermissionAvailabilityPollPublishToClass, PermissionStudyMeetingScheduleOwn,
				PermissionRoomCreateInstant,
				PermissionEnrollmentManage, PermissionEnrollmentLeave, PermissionSessionSchedule,
				PermissionSessionStart, PermissionSessionEnd, PermissionSessionJoin,
				PermissionParticipantAdmit, PermissionParticipantRemove,
				PermissionMediaPublish, PermissionMediaShareScreen, PermissionChatSend,
				PermissionFileView, PermissionFileUpload,
			},
		},
		{
			name: "teaching assistant",
			role: ClassRoleTeachingAssistant,
			want: []Permission{
				PermissionTenantView, PermissionClassView,
				PermissionConversationCreateDirect, PermissionMessageWriteDirect,
				PermissionAvailabilityPollCreate, PermissionAvailabilityPollManageOwn,
				PermissionStudyMeetingScheduleOwn,
				PermissionRoomCreateInstant,
				PermissionEnrollmentLeave, PermissionSessionJoin, PermissionParticipantAdmit,
				PermissionMediaPublish, PermissionMediaShareScreen, PermissionChatSend,
				PermissionFileView,
			},
		},
		{
			name: "student",
			role: ClassRoleStudent,
			want: []Permission{
				PermissionTenantView, PermissionClassView,
				PermissionConversationCreateDirect, PermissionMessageWriteDirect,
				PermissionAvailabilityPollCreate, PermissionAvailabilityPollManageOwn,
				PermissionStudyMeetingScheduleOwn,
				PermissionRoomCreateInstant,
				PermissionEnrollmentLeave, PermissionSessionJoin, PermissionMediaPublish,
				PermissionChatSend,
				PermissionFileView,
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
		PermissionConversationCreateDirect,
		PermissionMessageWriteDirect,
		PermissionAvailabilityPollCreate,
		PermissionAvailabilityPollManageOwn,
		PermissionStudyMeetingScheduleOwn,
		PermissionRoomCreateInstant,
		PermissionEnrollmentLeave,
		PermissionSessionJoin,
		PermissionParticipantAdmit,
		PermissionMediaPublish,
		PermissionMediaShareScreen,
		PermissionChatSend,
		PermissionFileView,
	}
	if got := NewEngine().EffectivePermissions(subject); !reflect.DeepEqual(got, want) {
		t.Fatalf("effective permission union is not deterministic: got=%v want=%v", got, want)
	}
}

func TestMediaShareScreenPermissionMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		organizationRoles []OrganizationRole
		classRoles        []ClassRole
		allowed           bool
	}{
		{
			name: "organization admin", organizationRoles: []OrganizationRole{OrganizationRoleAdmin},
			allowed: true,
		},
		{
			name: "organization teacher", organizationRoles: []OrganizationRole{OrganizationRoleTeacher},
			allowed: true,
		},
		{
			name: "class owner", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRoles: []ClassRole{ClassRoleOwner}, allowed: true,
		},
		{
			name: "co-teacher", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRoles: []ClassRole{ClassRoleCoTeacher}, allowed: true,
		},
		{
			name: "teaching assistant", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRoles: []ClassRole{ClassRoleTeachingAssistant}, allowed: true,
		},
		{
			name: "organization student", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
		},
		{
			name: "organization guest", organizationRoles: []OrganizationRole{OrganizationRoleGuest},
		},
		{
			name: "class student", organizationRoles: []OrganizationRole{OrganizationRoleGuest},
			classRoles: []ClassRole{ClassRoleStudent},
		},
	}

	engine := NewEngine()
	tenantID, classID, actorID := uuid.New(), uuid.New(), uuid.New()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision := engine.Authorize(Input{
				Subject: Subject{
					ActorID: actorID, ActiveTenantID: tenantID, MembershipActive: true,
					OrganizationRoles: test.organizationRoles, ClassRoles: test.classRoles,
				},
				Action: ActionMediaShareScreen,
				Resource: Resource{
					TenantID: tenantID, ClassID: classID, State: ResourceStateActive,
				},
			})
			if decision.Allowed != test.allowed {
				t.Fatalf("media share-screen decision=%+v, want allowed=%t", decision, test.allowed)
			}
			if !test.allowed && decision.Reason != DenialPermission {
				t.Fatalf("media share-screen denial=%+v, want permission denial", decision)
			}
		})
	}
}

func TestFilePermissionsSeparateViewFromUploadAndRespectClassLifecycle(t *testing.T) {
	t.Parallel()
	tenantID := uuid.New()
	classID := uuid.New()
	actorID := uuid.New()
	engine := NewEngine()
	student := Subject{
		ActorID: actorID, ActiveTenantID: tenantID, MembershipActive: true,
		OrganizationRoles: []OrganizationRole{OrganizationRoleStudent},
		ClassRoles:        []ClassRole{ClassRoleStudent},
	}
	resource := Resource{TenantID: tenantID, ClassID: classID, State: ResourceStateActive}
	if decision := engine.Authorize(Input{
		Subject: student, Action: ActionFileView, Resource: resource,
	}); !decision.Allowed {
		t.Fatalf("active class student file view denied: %+v", decision)
	}
	if decision := engine.Authorize(Input{
		Subject: student, Action: ActionFileUpload, Resource: resource,
	}); decision.Allowed || decision.Reason != DenialPermission {
		t.Fatalf("student file upload decision=%+v, want permission denial", decision)
	}
	owner := student
	owner.ClassRoles = []ClassRole{ClassRoleOwner}
	if decision := engine.Authorize(Input{
		Subject: owner, Action: ActionFileUpload, Resource: resource,
	}); !decision.Allowed {
		t.Fatalf("active class owner file upload denied: %+v", decision)
	}
	resource.State = ResourceStateArchived
	if decision := engine.Authorize(Input{
		Subject: owner, Action: ActionFileView, Resource: resource,
	}); !decision.Allowed {
		t.Fatalf("archived class file view denied: %+v", decision)
	}
	if decision := engine.Authorize(Input{
		Subject: owner, Action: ActionFileUpload, Resource: resource,
	}); decision.Allowed || decision.Reason != DenialResourceState {
		t.Fatalf("archived class file upload decision=%+v, want state denial", decision)
	}
}

func TestDirectConversationCreationIsTenantScopedForEveryActiveMember(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	for _, role := range []OrganizationRole{
		OrganizationRoleAdmin,
		OrganizationRoleTeacher,
		OrganizationRoleStudent,
		OrganizationRoleGuest,
	} {
		subject := validTestSubject()
		subject.OrganizationRoles = []OrganizationRole{role}
		decision := engine.Authorize(Input{
			Subject: subject,
			Action:  ActionConversationCreateDirect,
			Resource: Resource{
				TenantID: subject.ActiveTenantID,
				State:    ResourceStateActive,
			},
		})
		if !decision.Allowed {
			t.Fatalf("active %s member could not create direct conversation: %+v", role, decision)
		}
	}
}

func TestAvailabilityPollOwnActionsAreTenantScopedAndPublishRequiresClass(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	subject := validTestSubject()
	subject.OrganizationRoles = []OrganizationRole{OrganizationRoleStudent}
	resource := Resource{TenantID: subject.ActiveTenantID, State: ResourceStateActive}

	for _, action := range []Action{
		ActionAvailabilityPollCreate,
		ActionAvailabilityPollManageOwn,
		ActionStudyMeetingScheduleOwn,
	} {
		decision := engine.Authorize(Input{Subject: subject, Action: action, Resource: resource})
		if !decision.Allowed {
			t.Fatalf("tenant-scoped action %s denied without class: %+v", action, decision)
		}
	}

	studentPublish := engine.Authorize(Input{
		Subject: subject, Action: ActionAvailabilityPollPublishToClass, Resource: resource,
	})
	if studentPublish.Allowed || studentPublish.Reason != DenialResourceScope ||
		!studentPublish.ConcealResource {
		t.Fatalf("publish without a class must be concealed: %+v", studentPublish)
	}

	resource.ClassID = uuid.New()
	studentPublish = engine.Authorize(Input{
		Subject: subject, Action: ActionAvailabilityPollPublishToClass, Resource: resource,
	})
	if studentPublish.Allowed || studentPublish.Reason != DenialPermission {
		t.Fatalf("student unexpectedly published to class: %+v", studentPublish)
	}

	subject.OrganizationRoles = []OrganizationRole{OrganizationRoleTeacher}
	teacherPublish := engine.Authorize(Input{
		Subject: subject, Action: ActionAvailabilityPollPublishToClass, Resource: resource,
	})
	if !teacherPublish.Allowed {
		t.Fatalf("teacher publish denied: %+v", teacherPublish)
	}
}

func TestInstantRoomCreationIsTenantScopedForEveryActiveMember(t *testing.T) {
	t.Parallel()

	engine := NewEngine()
	for _, role := range []OrganizationRole{
		OrganizationRoleAdmin,
		OrganizationRoleTeacher,
		OrganizationRoleStudent,
		OrganizationRoleGuest,
	} {
		subject := validTestSubject()
		subject.OrganizationRoles = []OrganizationRole{role}
		decision := engine.Authorize(Input{
			Subject: subject,
			Action:  ActionRoomCreateInstant,
			Resource: Resource{
				TenantID: subject.ActiveTenantID,
				State:    ResourceStateActive,
			},
		})
		if !decision.Allowed {
			t.Fatalf("active %s member could not create an instant room: %+v", role, decision)
		}
	}

	inactive := validTestSubject()
	inactive.MembershipActive = false
	decision := engine.Authorize(Input{
		Subject: inactive,
		Action:  ActionRoomCreateInstant,
		Resource: Resource{
			TenantID: inactive.ActiveTenantID,
			State:    ResourceStateActive,
		},
	})
	if decision.Allowed || decision.Reason != DenialInactiveMembership {
		t.Fatalf("inactive member received instant-room permission: %+v", decision)
	}

	crossTenant := validTestSubject()
	decision = engine.Authorize(Input{
		Subject: crossTenant,
		Action:  ActionRoomCreateInstant,
		Resource: Resource{
			TenantID: uuid.New(),
			State:    ResourceStateActive,
		},
	})
	if decision.Allowed || decision.Reason != DenialResourceScope || !decision.ConcealResource {
		t.Fatalf("cross-tenant instant-room request was not concealed: %+v", decision)
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
