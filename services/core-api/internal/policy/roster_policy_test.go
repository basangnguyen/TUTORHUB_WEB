package policy

import (
	"testing"

	"github.com/google/uuid"
)

func TestCanMutateRosterHierarchyMatrix(t *testing.T) {
	t.Parallel()

	type actorFixture struct {
		name              string
		organizationRoles []OrganizationRole
		classRoles        []ClassRole
		managementGranted bool
		allowedTargets    map[string]bool
		allowedDesired    map[ClassRole]bool
	}
	type targetFixture struct {
		name              string
		organizationRoles []OrganizationRole
		classRole         ClassRole
		owner             bool
	}

	fullDesired := map[ClassRole]bool{
		ClassRoleCoTeacher:         true,
		ClassRoleTeachingAssistant: true,
		ClassRoleStudent:           true,
	}
	lowerDesired := map[ClassRole]bool{
		ClassRoleTeachingAssistant: true,
		ClassRoleStudent:           true,
	}
	fullTargets := map[string]bool{
		"organization teacher": true,
		"co-teacher":           true,
		"teaching assistant":   true,
		"student":              true,
	}
	lowerTargets := map[string]bool{
		"teaching assistant": true,
		"student":            true,
	}

	actors := []actorFixture{
		{
			name: "organization admin", organizationRoles: []OrganizationRole{OrganizationRoleAdmin},
			managementGranted: true, allowedTargets: fullTargets, allowedDesired: fullDesired,
		},
		{
			name: "implicit owner", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRoles: []ClassRole{ClassRoleOwner}, managementGranted: true,
			allowedTargets: fullTargets, allowedDesired: fullDesired,
		},
		{
			name: "organization teacher", organizationRoles: []OrganizationRole{OrganizationRoleTeacher},
			managementGranted: true, allowedTargets: lowerTargets, allowedDesired: lowerDesired,
		},
		{
			name: "co-teacher", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRoles: []ClassRole{ClassRoleCoTeacher}, managementGranted: true,
			allowedTargets: lowerTargets, allowedDesired: lowerDesired,
		},
		{
			name: "teaching assistant", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRoles: []ClassRole{ClassRoleTeachingAssistant}, allowedTargets: map[string]bool{},
			allowedDesired: map[ClassRole]bool{},
		},
		{
			name: "student", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRoles: []ClassRole{ClassRoleStudent}, allowedTargets: map[string]bool{},
			allowedDesired: map[ClassRole]bool{},
		},
	}
	targets := []targetFixture{
		{
			name: "organization admin", organizationRoles: []OrganizationRole{OrganizationRoleAdmin},
			classRole: ClassRoleStudent,
		},
		{
			name: "implicit owner", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRole: ClassRoleStudent, owner: true,
		},
		{
			name: "organization teacher", organizationRoles: []OrganizationRole{OrganizationRoleTeacher},
			classRole: ClassRoleStudent,
		},
		{
			name: "co-teacher", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRole: ClassRoleCoTeacher,
		},
		{
			name: "teaching assistant", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRole: ClassRoleTeachingAssistant,
		},
		{
			name: "student", organizationRoles: []OrganizationRole{OrganizationRoleStudent},
			classRole: ClassRoleStudent,
		},
	}
	desiredRoles := []ClassRole{
		ClassRoleCoTeacher,
		ClassRoleTeachingAssistant,
		ClassRoleStudent,
	}
	statusMutations := []RosterMutationAction{RosterMutationSuspend, RosterMutationRemove}

	for _, actor := range actors {
		actor := actor
		for _, target := range targets {
			target := target
			for _, mutation := range statusMutations {
				mutation := mutation
				t.Run(actor.name+"/"+string(mutation)+"/"+target.name, func(t *testing.T) {
					t.Parallel()
					decision := CanMutateRoster(rosterMatrixInput(actor, target, mutation, ""))
					want := actor.managementGranted && actor.allowedTargets[target.name]
					assertRosterMutationDecision(t, decision, want)
				})
			}
			for _, desiredRole := range desiredRoles {
				desiredRole := desiredRole
				t.Run(
					actor.name+"/update/"+target.name+"/to/"+string(desiredRole),
					func(t *testing.T) {
						t.Parallel()
						decision := CanMutateRoster(rosterMatrixInput(
							actor,
							target,
							RosterMutationUpdateRole,
							desiredRole,
						))
						want := actor.managementGranted && actor.allowedTargets[target.name] &&
							actor.allowedDesired[desiredRole]
						assertRosterMutationDecision(t, decision, want)
					},
				)
			}
		}
	}
}

func TestCanMutateRosterDenialGuards(t *testing.T) {
	t.Parallel()

	valid := RosterMutationInput{
		ManagementGranted:       true,
		Action:                  RosterMutationUpdateRole,
		ActorID:                 uuid.New(),
		TargetUserID:            uuid.New(),
		ActorOrganizationRoles:  []OrganizationRole{OrganizationRoleAdmin},
		TargetOrganizationRoles: []OrganizationRole{OrganizationRoleStudent},
		TargetClassRole:         ClassRoleStudent,
		DesiredClassRole:        ClassRoleTeachingAssistant,
	}
	tests := []struct {
		name   string
		mutate func(*RosterMutationInput)
		reason RosterMutationDenialReason
	}{
		{
			name: "management permission was not granted",
			mutate: func(input *RosterMutationInput) {
				input.ManagementGranted = false
			},
			reason: RosterMutationDenialManagementRequired,
		},
		{
			name: "unknown mutation",
			mutate: func(input *RosterMutationInput) {
				input.Action = RosterMutationAction("unknown")
			},
			reason: RosterMutationDenialInvalidInput,
		},
		{
			name: "missing actor",
			mutate: func(input *RosterMutationInput) {
				input.ActorID = uuid.Nil
			},
			reason: RosterMutationDenialInvalidInput,
		},
		{
			name: "missing target",
			mutate: func(input *RosterMutationInput) {
				input.TargetUserID = uuid.Nil
			},
			reason: RosterMutationDenialInvalidInput,
		},
		{
			name: "self mutation",
			mutate: func(input *RosterMutationInput) {
				input.TargetUserID = input.ActorID
			},
			reason: RosterMutationDenialSelf,
		},
		{
			name: "implicit owner target",
			mutate: func(input *RosterMutationInput) {
				input.TargetIsOwner = true
			},
			reason: RosterMutationDenialProtectedOwner,
		},
		{
			name: "owner cannot be a persisted target role",
			mutate: func(input *RosterMutationInput) {
				input.TargetClassRole = ClassRoleOwner
			},
			reason: RosterMutationDenialProtectedOwner,
		},
		{
			name: "actor organization role is missing",
			mutate: func(input *RosterMutationInput) {
				input.ActorOrganizationRoles = nil
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "actor organization role is unknown",
			mutate: func(input *RosterMutationInput) {
				input.ActorOrganizationRoles = []OrganizationRole{OrganizationRoleAdmin, "unknown"}
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "actor class role is unknown",
			mutate: func(input *RosterMutationInput) {
				input.ActorClassRoles = []ClassRole{"unknown"}
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "target organization role is missing",
			mutate: func(input *RosterMutationInput) {
				input.TargetOrganizationRoles = nil
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "target organization role is unknown",
			mutate: func(input *RosterMutationInput) {
				input.TargetOrganizationRoles = []OrganizationRole{"unknown"}
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "target class role is unknown",
			mutate: func(input *RosterMutationInput) {
				input.TargetClassRole = "unknown"
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "desired role is missing",
			mutate: func(input *RosterMutationInput) {
				input.DesiredClassRole = ""
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "desired role is unknown",
			mutate: func(input *RosterMutationInput) {
				input.DesiredClassRole = "unknown"
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "owner cannot be assigned",
			mutate: func(input *RosterMutationInput) {
				input.DesiredClassRole = ClassRoleOwner
			},
			reason: RosterMutationDenialUnknownRole,
		},
		{
			name: "status mutation cannot carry a desired role",
			mutate: func(input *RosterMutationInput) {
				input.Action = RosterMutationSuspend
			},
			reason: RosterMutationDenialInvalidInput,
		},
		{
			name: "actor cannot mutate a peer",
			mutate: func(input *RosterMutationInput) {
				input.ActorOrganizationRoles = []OrganizationRole{OrganizationRoleTeacher}
				input.TargetClassRole = ClassRoleCoTeacher
				input.DesiredClassRole = ClassRoleTeachingAssistant
			},
			reason: RosterMutationDenialHierarchy,
		},
		{
			name: "actor cannot grant an equal role",
			mutate: func(input *RosterMutationInput) {
				input.ActorOrganizationRoles = []OrganizationRole{OrganizationRoleTeacher}
				input.TargetClassRole = ClassRoleStudent
				input.DesiredClassRole = ClassRoleCoTeacher
			},
			reason: RosterMutationDenialHierarchy,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := valid
			input.ActorOrganizationRoles = append(
				[]OrganizationRole(nil),
				valid.ActorOrganizationRoles...,
			)
			input.ActorClassRoles = append([]ClassRole(nil), valid.ActorClassRoles...)
			input.TargetOrganizationRoles = append(
				[]OrganizationRole(nil),
				valid.TargetOrganizationRoles...,
			)
			test.mutate(&input)
			decision := CanMutateRoster(input)
			if decision.Allowed || decision.Reason != test.reason {
				t.Fatalf("unexpected denial decision: %+v", decision)
			}
		})
	}
}

func TestCanMutateRosterUsesHighestValidAuthority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		actorOrg   []OrganizationRole
		actorClass []ClassRole
		targetOrg  []OrganizationRole
		targetRole ClassRole
		desired    ClassRole
		allowed    bool
	}{
		{
			name:       "implicit owner outranks organization teacher peer",
			actorOrg:   []OrganizationRole{OrganizationRoleTeacher},
			actorClass: []ClassRole{ClassRoleOwner},
			targetOrg:  []OrganizationRole{OrganizationRoleStudent},
			targetRole: ClassRoleCoTeacher,
			desired:    ClassRoleCoTeacher,
			allowed:    true,
		},
		{
			name:       "organization admin outranks lower class role",
			actorOrg:   []OrganizationRole{OrganizationRoleAdmin},
			actorClass: []ClassRole{ClassRoleTeachingAssistant},
			targetOrg:  []OrganizationRole{OrganizationRoleTeacher},
			targetRole: ClassRoleStudent,
			desired:    ClassRoleCoTeacher,
			allowed:    true,
		},
		{
			name:       "target organization role protects a lower class role",
			actorOrg:   []OrganizationRole{OrganizationRoleTeacher},
			targetOrg:  []OrganizationRole{OrganizationRoleTeacher},
			targetRole: ClassRoleStudent,
			desired:    ClassRoleStudent,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			decision := CanMutateRoster(RosterMutationInput{
				ManagementGranted:       true,
				Action:                  RosterMutationUpdateRole,
				ActorID:                 uuid.New(),
				TargetUserID:            uuid.New(),
				ActorOrganizationRoles:  test.actorOrg,
				ActorClassRoles:         test.actorClass,
				TargetOrganizationRoles: test.targetOrg,
				TargetClassRole:         test.targetRole,
				DesiredClassRole:        test.desired,
			})
			assertRosterMutationDecision(t, decision, test.allowed)
		})
	}
}

func rosterMatrixInput(
	actor struct {
		name              string
		organizationRoles []OrganizationRole
		classRoles        []ClassRole
		managementGranted bool
		allowedTargets    map[string]bool
		allowedDesired    map[ClassRole]bool
	},
	target struct {
		name              string
		organizationRoles []OrganizationRole
		classRole         ClassRole
		owner             bool
	},
	action RosterMutationAction,
	desiredRole ClassRole,
) RosterMutationInput {
	return RosterMutationInput{
		ManagementGranted:       actor.managementGranted,
		Action:                  action,
		ActorID:                 uuid.New(),
		TargetUserID:            uuid.New(),
		ActorOrganizationRoles:  actor.organizationRoles,
		ActorClassRoles:         actor.classRoles,
		TargetOrganizationRoles: target.organizationRoles,
		TargetClassRole:         target.classRole,
		TargetIsOwner:           target.owner,
		DesiredClassRole:        desiredRole,
	}
}

func assertRosterMutationDecision(
	t *testing.T,
	decision RosterMutationDecision,
	want bool,
) {
	t.Helper()
	if decision.Allowed != want {
		t.Fatalf("unexpected roster mutation decision: got=%+v want allowed=%t", decision, want)
	}
	if decision.Allowed && decision.Reason != RosterMutationDenialNone {
		t.Fatalf("allowed decision must not include a denial reason: %+v", decision)
	}
	if !decision.Allowed && decision.Reason == RosterMutationDenialNone {
		t.Fatalf("denied decision must include a reason: %+v", decision)
	}
}
