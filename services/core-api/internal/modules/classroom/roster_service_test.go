package classroom

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestRosterServiceNormalizesSearchAndBindsCursorScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 19, 8, 0, 0, 0, time.UTC)
	access := enrollmentServiceAccess()
	classID := uuid.New()
	memberID := uuid.New()
	status := EnrollmentStatusActive
	repository := &enrollmentServiceRepositoryStub{listRosterResult: ListRosterResult{
		Owner:   RosterOwner{User: RosterUser{ID: uuid.New(), DisplayName: "Owner"}},
		Items:   []RosterMember{{User: RosterUser{ID: memberID}}},
		HasMore: true,
	}}
	classes := &enrollmentServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: access.TenantID, Status: ClassStatusActive,
	}}
	service := newEnrollmentServiceTestSubject(t, repository, classes, nil, now)

	page, err := service.ListRoster(context.Background(), access, classID, ListRosterInput{
		Search: "  A\u030A   USER  ", Status: &status, Limit: 25,
	})
	if err != nil {
		t.Fatalf("list roster: %v", err)
	}
	if repository.listRosterParams.Search != "å user" ||
		repository.listRosterParams.Status == nil ||
		*repository.listRosterParams.Status != EnrollmentStatusActive ||
		repository.listRosterParams.Limit != 25 || page.NextCursor == "" {
		t.Fatalf("unexpected normalized roster page: params=%+v page=%+v", repository.listRosterParams, page)
	}
	if len(classes.calls) != 1 || classes.calls[0].Action != policy.ActionEnrollmentManage {
		t.Fatalf("unexpected roster authorization: %+v", classes.calls)
	}

	_, err = service.ListRoster(context.Background(), access, classID, ListRosterInput{
		Search: "å user", Status: &status, Limit: 25, Cursor: page.NextCursor,
	})
	if err != nil || repository.listRosterParams.After == nil ||
		repository.listRosterParams.After.UserID != memberID {
		t.Fatalf("decode same-scope cursor: params=%+v error=%v", repository.listRosterParams, err)
	}
	before := repository.listRosterCalls
	otherStatus := EnrollmentStatusSuspended
	_, err = service.ListRoster(context.Background(), access, classID, ListRosterInput{
		Search: "å user", Status: &otherStatus, Limit: 25, Cursor: page.NextCursor,
	})
	if !errors.Is(err, ErrInvalidRosterCursor) || repository.listRosterCalls != before {
		t.Fatalf("cursor reused with another filter: calls=%d error=%v", repository.listRosterCalls, err)
	}
	otherAccess := access
	otherAccess.TenantID = uuid.New()
	_, err = service.ListRoster(context.Background(), otherAccess, classID, ListRosterInput{
		Search: "å user", Status: &status, Limit: 25, Cursor: page.NextCursor,
	})
	if !errors.Is(err, ErrInvalidRosterCursor) {
		t.Fatalf("cursor reused across tenants returned %v", err)
	}
	_, err = service.ListRoster(context.Background(), access, uuid.New(), ListRosterInput{
		Search: "å user", Status: &status, Limit: 25, Cursor: page.NextCursor,
	})
	if !errors.Is(err, ErrInvalidRosterCursor) {
		t.Fatalf("cursor reused across classes returned %v", err)
	}
}

func TestRosterServiceRejectsInvalidListAndRoleInputsBeforePersistence(t *testing.T) {
	t.Parallel()

	access := enrollmentServiceAccess()
	classID := uuid.New()
	now := time.Date(2026, time.July, 19, 8, 30, 0, 0, time.UTC)
	repository := &enrollmentServiceRepositoryStub{}
	classes := &enrollmentServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: access.TenantID, Status: ClassStatusActive,
	}}
	service := newEnrollmentServiceTestSubject(t, repository, classes, nil, now)

	for _, input := range []ListRosterInput{
		{Limit: -1},
		{Limit: maximumRosterLimit + 1},
		{Search: strings.Repeat("x", maximumRosterSearchLength+1)},
		{Cursor: "not-a-roster-cursor"},
	} {
		if _, err := service.ListRoster(context.Background(), access, classID, input); err == nil {
			t.Fatalf("invalid roster input was accepted: %+v", input)
		}
	}
	if repository.listRosterCalls != 0 {
		t.Fatal("invalid list input must not reach the repository")
	}

	if _, err := service.UpdateRosterRole(
		context.Background(), access, classID, uuid.Nil,
		UpdateRosterRoleInput{ClassRole: policy.ClassRoleStudent},
	); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("nil role target returned %v", err)
	}
	for _, role := range []policy.ClassRole{"", policy.ClassRoleOwner, "platform_admin"} {
		if _, err := service.UpdateRosterRole(
			context.Background(), access, classID, uuid.New(),
			UpdateRosterRoleInput{ClassRole: role},
		); !errors.Is(err, ErrInvalidEnrollmentInput) {
			t.Fatalf("invalid persisted role %q returned %v", role, err)
		}
	}
	if repository.updateRoleCalls != 0 {
		t.Fatal("invalid role updates must not reach persistence")
	}
}

func TestRosterServiceUpdatesRoleAndBlocksArchivedMutation(t *testing.T) {
	t.Parallel()

	access := enrollmentServiceAccess()
	classID := uuid.New()
	userID := uuid.New()
	now := time.Date(2026, time.July, 19, 9, 0, 0, 123, time.FixedZone("ICT", 7*60*60))
	want := EnrollmentMutationResult{Enrollment: Enrollment{
		ID: uuid.New(), TenantID: access.TenantID, ClassID: classID,
		UserID: userID, ClassRole: policy.ClassRoleTeachingAssistant,
		Status: EnrollmentStatusActive,
	}, Changed: true}
	repository := &enrollmentServiceRepositoryStub{updateRoleResult: want}
	classes := &enrollmentServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: access.TenantID, Status: ClassStatusActive,
	}}
	service := newEnrollmentServiceTestSubject(t, repository, classes, nil, now)

	got, err := service.UpdateRosterRole(
		context.Background(), access, classID, userID,
		UpdateRosterRoleInput{ClassRole: policy.ClassRoleTeachingAssistant},
	)
	if err != nil || got != want {
		t.Fatalf("update roster role: result=%+v error=%v", got, err)
	}
	if repository.updateRoleUser != userID || repository.updateRoleClass != classID ||
		repository.updateRoleParams.ClassRole != policy.ClassRoleTeachingAssistant ||
		repository.updateRoleParams.Source != "roster_single" ||
		!repository.updateRoleParams.ChangedAt.Equal(now.UTC()) ||
		repository.updateRoleParams.ChangedAt.Location() != time.UTC {
		t.Fatalf("unexpected role update propagation: %+v", repository)
	}

	classes.class.Status = ClassStatusArchived
	if _, err := service.UpdateRosterRole(
		context.Background(), access, classID, userID,
		UpdateRosterRoleInput{ClassRole: policy.ClassRoleStudent},
	); !errors.Is(err, ErrEnrollmentConflict) {
		t.Fatalf("archived role mutation returned %v", err)
	}
	if repository.updateRoleCalls != 1 {
		t.Fatal("archived class must block role persistence")
	}
}

func TestRosterServiceBulkValidationOrderingAndPartialFailures(t *testing.T) {
	t.Parallel()

	access := enrollmentServiceAccess()
	classID := uuid.New()
	now := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	classes := &enrollmentServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: access.TenantID, Status: ClassStatusActive,
	}}
	repository := &enrollmentServiceRepositoryStub{}
	service := newEnrollmentServiceTestSubject(t, repository, classes, nil, now)
	role := policy.ClassRoleTeachingAssistant
	duplicate := uuid.New()
	tooMany := make([]uuid.UUID, maximumRosterBulkOperations+1)
	for index := range tooMany {
		tooMany[index] = uuid.New()
	}
	invalidInputs := []BulkRosterInput{
		{},
		{Action: RosterBulkActionSuspend},
		{Action: RosterBulkActionSuspend, UserIDs: []uuid.UUID{uuid.Nil}},
		{Action: RosterBulkActionSuspend, UserIDs: []uuid.UUID{duplicate, duplicate}},
		{Action: RosterBulkActionSuspend, ClassRole: &role, UserIDs: []uuid.UUID{uuid.New()}},
		{Action: RosterBulkActionUpdateRole, UserIDs: []uuid.UUID{uuid.New()}},
		{Action: "promote_owner", ClassRole: &role, UserIDs: []uuid.UUID{uuid.New()}},
		{Action: RosterBulkActionRemove, UserIDs: tooMany},
	}
	for _, input := range invalidInputs {
		if _, err := service.BulkMutateRoster(
			context.Background(), access, classID, input,
		); !errors.Is(err, ErrInvalidEnrollmentInput) {
			t.Fatalf("invalid bulk input %+v returned %v", input, err)
		}
	}
	if repository.updateRoleCalls != 0 || repository.suspendCalls != 0 ||
		repository.removeCalls != 0 {
		t.Fatal("invalid bulk inputs must not reach persistence")
	}

	first, second, third := uuid.New(), uuid.New(), uuid.New()
	repository.updateRoleFunc = func(
		userID uuid.UUID,
		params UpdateRosterRoleParams,
	) (EnrollmentMutationResult, error) {
		if params.Source != "roster_bulk" || !params.ChangedAt.Equal(now) {
			t.Fatalf("unexpected bulk role params: %+v", params)
		}
		enrollment := Enrollment{
			ID: uuid.New(), TenantID: access.TenantID, ClassID: classID,
			UserID: userID, ClassRole: role, Status: EnrollmentStatusActive,
		}
		switch userID {
		case first:
			return EnrollmentMutationResult{Enrollment: enrollment, Changed: true}, nil
		case second:
			return EnrollmentMutationResult{Enrollment: enrollment}, nil
		case third:
			return EnrollmentMutationResult{}, ErrEnrollmentConflict
		default:
			return EnrollmentMutationResult{}, errors.New("unexpected user")
		}
	}
	result, err := service.BulkMutateRoster(
		context.Background(), access, classID,
		BulkRosterInput{
			Action: RosterBulkActionUpdateRole, ClassRole: &role,
			UserIDs: []uuid.UUID{first, second, third},
		},
	)
	if err != nil {
		t.Fatalf("bulk role mutation: %v", err)
	}
	if len(result.Items) != 3 || result.Items[0].UserID != first ||
		result.Items[1].UserID != second || result.Items[2].UserID != third ||
		!result.Items[0].Changed || result.Items[1].Changed ||
		result.Items[2].Failure == nil ||
		result.Items[2].Failure.Code != RosterBulkFailureConflict ||
		result.SucceededCount != 2 || result.UnchangedCount != 1 ||
		result.FailedCount != 1 {
		t.Fatalf("unexpected ordered bulk result: %+v", result)
	}

	infrastructureError := errors.New("database unavailable")
	repository.updateRoleCalls = 0
	repository.updateRoleFunc = func(
		userID uuid.UUID,
		_ UpdateRosterRoleParams,
	) (EnrollmentMutationResult, error) {
		if userID == first {
			return EnrollmentMutationResult{Enrollment: Enrollment{UserID: userID}, Changed: true}, nil
		}
		return EnrollmentMutationResult{}, infrastructureError
	}
	_, err = service.BulkMutateRoster(
		context.Background(), access, classID,
		BulkRosterInput{
			Action: RosterBulkActionUpdateRole, ClassRole: &role,
			UserIDs: []uuid.UUID{first, second, third},
		},
	)
	if !errors.Is(err, infrastructureError) || repository.updateRoleCalls != 2 {
		t.Fatalf("infrastructure failure did not stop the batch: calls=%d error=%v", repository.updateRoleCalls, err)
	}
}
