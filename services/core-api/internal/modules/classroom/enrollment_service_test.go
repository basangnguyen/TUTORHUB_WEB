package classroom

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type enrollmentServiceClassAuthorizationCall struct {
	Access  AccessContext
	ClassID uuid.UUID
	Action  policy.Action
}

type enrollmentServiceClassAuthorizerStub struct {
	class Class
	err   error
	calls []enrollmentServiceClassAuthorizationCall
}

func (stub *enrollmentServiceClassAuthorizerStub) AuthorizeClass(
	_ context.Context,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) (Class, error) {
	stub.calls = append(stub.calls, enrollmentServiceClassAuthorizationCall{
		Access: access, ClassID: classID, Action: action,
	})
	if stub.err != nil {
		return Class{}, stub.err
	}
	return stub.class, nil
}

type enrollmentServiceTokenCodecStub struct {
	randomValue string
	randomErr   error
	digest      []byte
	randomCalls int
	digestCalls int
	purpose     string
	value       string
}

func (stub *enrollmentServiceTokenCodecStub) RandomToken() (string, error) {
	stub.randomCalls++
	return stub.randomValue, stub.randomErr
}

func (stub *enrollmentServiceTokenCodecStub) Digest(
	purpose string,
	value string,
) []byte {
	stub.digestCalls++
	stub.purpose = purpose
	stub.value = value
	return stub.digest
}

type enrollmentServiceRepositoryStub struct {
	listRosterResult ListRosterResult
	listRosterErr    error
	listRosterCalls  int
	listRosterTenant tenancy.Context
	listRosterClass  uuid.UUID
	listRosterParams ListRosterParams

	updateRoleResult EnrollmentMutationResult
	updateRoleErr    error
	updateRoleCalls  int
	updateRoleTenant tenancy.Context
	updateRoleClass  uuid.UUID
	updateRoleUser   uuid.UUID
	updateRoleParams UpdateRosterRoleParams
	updateRoleFunc   func(uuid.UUID, UpdateRosterRoleParams) (EnrollmentMutationResult, error)

	directResult EnrollmentMutationResult
	directErr    error
	directCalls  int
	directTenant tenancy.Context
	directClass  uuid.UUID
	directParams DirectEnrollmentParams

	suspendResult EnrollmentMutationResult
	suspendErr    error
	suspendCalls  int
	suspendTenant tenancy.Context
	suspendClass  uuid.UUID
	suspendUser   uuid.UUID
	suspendAt     time.Time
	suspendFunc   func(uuid.UUID, time.Time) (EnrollmentMutationResult, error)

	removeResult EnrollmentMutationResult
	removeErr    error
	removeCalls  int
	removeTenant tenancy.Context
	removeClass  uuid.UUID
	removeUser   uuid.UUID
	removeAt     time.Time
	removeFunc   func(uuid.UUID, time.Time) (EnrollmentMutationResult, error)

	createCodeResult ClassInviteCode
	createCodeErr    error
	createCodeCalls  int
	createCodeTenant tenancy.Context
	createCodeClass  uuid.UUID
	createCodeParams CreateInviteCodeParams

	listCodeResult []ClassInviteCode
	listCodeErr    error
	listCodeCalls  int
	listCodeTenant tenancy.Context
	listCodeClass  uuid.UUID
	listCodeAt     time.Time

	revokeCodeResult ClassInviteCode
	revokeCodeErr    error
	revokeCodeCalls  int
	revokeCodeTenant tenancy.Context
	revokeCodeClass  uuid.UUID
	revokeCodeID     uuid.UUID
	revokeCodeAt     time.Time

	joinResult JoinClassInvitationResult
	joinErr    error
	joinCalls  int
	joinTenant tenancy.Context
	joinHash   []byte
	joinAt     time.Time

	leaveResult EnrollmentMutationResult
	leaveErr    error
	leaveCalls  int
	leaveTenant tenancy.Context
	leaveClass  uuid.UUID
	leaveAt     time.Time
}

func (stub *enrollmentServiceRepositoryStub) FindActorEnrollment(
	context.Context,
	tenancy.Context,
	uuid.UUID,
) (*Enrollment, error) {
	return nil, nil
}

func (stub *enrollmentServiceRepositoryStub) ListActorEnrollments(
	context.Context,
	tenancy.Context,
	[]uuid.UUID,
) ([]Enrollment, error) {
	return []Enrollment{}, nil
}

func (stub *enrollmentServiceRepositoryStub) ListRoster(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params ListRosterParams,
) (ListRosterResult, error) {
	stub.listRosterCalls++
	stub.listRosterTenant = tenantContext
	stub.listRosterClass = classID
	stub.listRosterParams = params
	return stub.listRosterResult, stub.listRosterErr
}

func (stub *enrollmentServiceRepositoryStub) UpdateRosterRole(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	userID uuid.UUID,
	params UpdateRosterRoleParams,
) (EnrollmentMutationResult, error) {
	stub.updateRoleCalls++
	stub.updateRoleTenant = tenantContext
	stub.updateRoleClass = classID
	stub.updateRoleUser = userID
	stub.updateRoleParams = params
	if stub.updateRoleFunc != nil {
		return stub.updateRoleFunc(userID, params)
	}
	return stub.updateRoleResult, stub.updateRoleErr
}

func (stub *enrollmentServiceRepositoryStub) DirectEnroll(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params DirectEnrollmentParams,
) (EnrollmentMutationResult, error) {
	stub.directCalls++
	stub.directTenant = tenantContext
	stub.directClass = classID
	stub.directParams = params
	return stub.directResult, stub.directErr
}

func (stub *enrollmentServiceRepositoryStub) SuspendEnrollment(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	userID uuid.UUID,
	changedAt time.Time,
) (EnrollmentMutationResult, error) {
	stub.suspendCalls++
	stub.suspendTenant = tenantContext
	stub.suspendClass = classID
	stub.suspendUser = userID
	stub.suspendAt = changedAt
	if stub.suspendFunc != nil {
		return stub.suspendFunc(userID, changedAt)
	}
	return stub.suspendResult, stub.suspendErr
}

func (stub *enrollmentServiceRepositoryStub) RemoveEnrollment(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	userID uuid.UUID,
	changedAt time.Time,
) (EnrollmentMutationResult, error) {
	stub.removeCalls++
	stub.removeTenant = tenantContext
	stub.removeClass = classID
	stub.removeUser = userID
	stub.removeAt = changedAt
	if stub.removeFunc != nil {
		return stub.removeFunc(userID, changedAt)
	}
	return stub.removeResult, stub.removeErr
}

func (stub *enrollmentServiceRepositoryStub) CreateInviteCode(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	params CreateInviteCodeParams,
) (ClassInviteCode, error) {
	stub.createCodeCalls++
	stub.createCodeTenant = tenantContext
	stub.createCodeClass = classID
	stub.createCodeParams = params
	stub.createCodeParams.CodeHash = append([]byte(nil), params.CodeHash...)
	return stub.createCodeResult, stub.createCodeErr
}

func (stub *enrollmentServiceRepositoryStub) ListInviteCodes(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	now time.Time,
) ([]ClassInviteCode, error) {
	stub.listCodeCalls++
	stub.listCodeTenant = tenantContext
	stub.listCodeClass = classID
	stub.listCodeAt = now
	return stub.listCodeResult, stub.listCodeErr
}

func (stub *enrollmentServiceRepositoryStub) RevokeInviteCode(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	codeID uuid.UUID,
	now time.Time,
) (ClassInviteCode, error) {
	stub.revokeCodeCalls++
	stub.revokeCodeTenant = tenantContext
	stub.revokeCodeClass = classID
	stub.revokeCodeID = codeID
	stub.revokeCodeAt = now
	return stub.revokeCodeResult, stub.revokeCodeErr
}

func (stub *enrollmentServiceRepositoryStub) JoinByInviteCode(
	_ context.Context,
	tenantContext tenancy.Context,
	codeHash []byte,
	now time.Time,
) (JoinClassInvitationResult, error) {
	stub.joinCalls++
	stub.joinTenant = tenantContext
	stub.joinHash = append([]byte(nil), codeHash...)
	stub.joinAt = now
	return stub.joinResult, stub.joinErr
}

func (stub *enrollmentServiceRepositoryStub) LeaveClass(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	now time.Time,
) (EnrollmentMutationResult, error) {
	stub.leaveCalls++
	stub.leaveTenant = tenantContext
	stub.leaveClass = classID
	stub.leaveAt = now
	return stub.leaveResult, stub.leaveErr
}

func TestEnrollmentServiceDirectEnrollValidationAuthorizationAndLifecycle(t *testing.T) {
	now := time.Date(2026, time.July, 19, 3, 15, 0, 0, time.FixedZone("ICT", 7*60*60))
	access := enrollmentServiceAccess()
	classID := uuid.New()
	targetUserID := uuid.New()
	wantResult := EnrollmentMutationResult{
		Enrollment: Enrollment{
			ID: uuid.New(), TenantID: access.TenantID, ClassID: classID,
			UserID: targetUserID, ClassRole: policy.ClassRoleStudent,
			Status: EnrollmentStatusActive,
		},
		Changed: true,
	}

	for _, email := range []string{
		"",
		"not-an-email",
		"two@example.test,other@example.test",
		strings.Repeat("a", maximumMemberEmailLength) + "@example.test",
	} {
		t.Run("invalid email "+email, func(t *testing.T) {
			repository := &enrollmentServiceRepositoryStub{}
			classes := &enrollmentServiceClassAuthorizerStub{
				class: Class{ID: classID, TenantID: access.TenantID, Status: ClassStatusActive},
			}
			service := newEnrollmentServiceTestSubject(t, repository, classes, nil, now)
			if _, err := service.DirectEnroll(
				context.Background(), access, classID,
				DirectEnrollmentInput{MemberEmail: email},
			); !errors.Is(err, ErrInvalidEnrollmentInput) {
				t.Fatalf("invalid email returned %v", err)
			}
			if repository.directCalls != 0 || len(classes.calls) != 0 {
				t.Fatal("invalid email must fail before authorization and persistence")
			}
		})
	}

	repository := &enrollmentServiceRepositoryStub{directResult: wantResult}
	classes := &enrollmentServiceClassAuthorizerStub{
		class: Class{ID: classID, TenantID: access.TenantID, Status: ClassStatusActive},
	}
	service := newEnrollmentServiceTestSubject(t, repository, classes, nil, now)
	result, err := service.DirectEnroll(
		context.Background(), access, classID,
		DirectEnrollmentInput{MemberEmail: "  Student@Example.Test  "},
	)
	if err != nil || result != wantResult {
		t.Fatalf("direct enroll: result=%+v error=%v", result, err)
	}
	if len(classes.calls) != 1 ||
		classes.calls[0].Action != policy.ActionEnrollmentManage ||
		classes.calls[0].ClassID != classID {
		t.Fatalf("unexpected class authorization: %+v", classes.calls)
	}
	if repository.directCalls != 1 ||
		repository.directTenant.TenantID != access.TenantID ||
		repository.directTenant.ActorID != access.ActorID ||
		repository.directClass != classID ||
		repository.directParams.MemberEmail != "student@example.test" ||
		!repository.directParams.ChangedAt.Equal(now.UTC()) ||
		repository.directParams.ChangedAt.Location() != time.UTC {
		t.Fatalf("unexpected direct enrollment persistence call: %+v", repository)
	}

	authorizationError := errors.New("authoritative class deny")
	deniedRepository := &enrollmentServiceRepositoryStub{}
	deniedClasses := &enrollmentServiceClassAuthorizerStub{err: authorizationError}
	deniedService := newEnrollmentServiceTestSubject(
		t, deniedRepository, deniedClasses, nil, now,
	)
	if _, err := deniedService.DirectEnroll(
		context.Background(), access, classID,
		DirectEnrollmentInput{MemberEmail: "student@example.test"},
	); !errors.Is(err, authorizationError) {
		t.Fatalf("authorization error was not propagated: %v", err)
	}
	if deniedRepository.directCalls != 0 {
		t.Fatal("authorization failure must not reach persistence")
	}

	draftRepository := &enrollmentServiceRepositoryStub{}
	draftClasses := &enrollmentServiceClassAuthorizerStub{
		class: Class{ID: classID, TenantID: access.TenantID, Status: ClassStatusDraft},
	}
	draftService := newEnrollmentServiceTestSubject(
		t, draftRepository, draftClasses, nil, now,
	)
	if _, err := draftService.DirectEnroll(
		context.Background(), access, classID,
		DirectEnrollmentInput{MemberEmail: "student@example.test"},
	); !errors.Is(err, ErrEnrollmentConflict) {
		t.Fatalf("draft class returned %v", err)
	}
	if draftRepository.directCalls != 0 {
		t.Fatal("draft class must not persist a direct enrollment")
	}
}

func TestEnrollmentServiceCreateInviteCodeValidationAndRawTokenBoundary(t *testing.T) {
	now := time.Date(2026, time.July, 19, 4, 0, 0, 987654321, time.UTC)
	access := enrollmentServiceAccess()
	classID := uuid.New()
	validClass := Class{ID: classID, TenantID: access.TenantID, Status: ClassStatusActive}

	invalidInputs := []CreateClassInviteCodeInput{
		{ExpiresInSeconds: int(minimumClassInviteCodeTTL/time.Second) - 1, UsageLimit: 1},
		{ExpiresInSeconds: int(maximumClassInviteCodeTTL/time.Second) + 1, UsageLimit: 1},
		{ExpiresInSeconds: int(^uint(0) >> 1), UsageLimit: 1},
		{ExpiresInSeconds: int(time.Hour / time.Second), UsageLimit: 0},
		{ExpiresInSeconds: int(time.Hour / time.Second), UsageLimit: maximumInviteCodeUses + 1},
	}
	for _, input := range invalidInputs {
		repository := &enrollmentServiceRepositoryStub{}
		classes := &enrollmentServiceClassAuthorizerStub{class: validClass}
		codec := enrollmentServiceCodec()
		service := newEnrollmentServiceTestSubject(t, repository, classes, codec, now)
		if _, err := service.CreateInviteCode(
			context.Background(), access, classID, input,
		); !errors.Is(err, ErrInvalidEnrollmentInput) {
			t.Fatalf("invalid invite input %+v returned %v", input, err)
		}
		if repository.createCodeCalls != 0 || len(classes.calls) != 0 ||
			codec.randomCalls != 0 || codec.digestCalls != 0 {
			t.Fatalf("invalid invite input reached dependencies: %+v", input)
		}
	}

	codec := enrollmentServiceCodec()
	rawRandom := codec.randomValue
	rawToken := classInviteCodeTokenPrefix + rawRandom
	storedDigest := append([]byte(nil), codec.digest...)
	wantCode := ClassInviteCode{
		ID: uuid.New(), TenantID: access.TenantID, ClassID: classID,
		Status: ClassInviteCodeStatusActive, ExpiresAt: now.Add(minimumClassInviteCodeTTL),
		UsageLimit: 1, CreatedBy: access.ActorID, CreatedAt: now, UpdatedAt: now,
	}
	repository := &enrollmentServiceRepositoryStub{createCodeResult: wantCode}
	classes := &enrollmentServiceClassAuthorizerStub{class: validClass}
	service := newEnrollmentServiceTestSubject(t, repository, classes, codec, now)
	result, err := service.CreateInviteCode(
		context.Background(),
		access,
		classID,
		CreateClassInviteCodeInput{
			ExpiresInSeconds: int(minimumClassInviteCodeTTL / time.Second),
			UsageLimit:       1,
		},
	)
	if err != nil || result.InviteCode != wantCode || result.Token != rawToken {
		t.Fatalf("create invite code: result=%+v error=%v", result, err)
	}
	if codec.randomCalls != 1 || codec.digestCalls != 1 ||
		codec.purpose != classInviteCodeTokenPurpose || codec.value != rawToken {
		t.Fatalf("unexpected token codec boundary: %+v", codec)
	}
	if repository.createCodeCalls != 1 ||
		repository.createCodeTenant.TenantID != access.TenantID ||
		repository.createCodeTenant.ActorID != access.ActorID ||
		repository.createCodeClass != classID ||
		repository.createCodeParams.UsageLimit != 1 ||
		!repository.createCodeParams.CreatedAt.Equal(now) ||
		!repository.createCodeParams.ExpiresAt.Equal(now.Add(minimumClassInviteCodeTTL)) ||
		!bytes.Equal(repository.createCodeParams.CodeHash, storedDigest) {
		t.Fatalf("unexpected persisted invite parameters: %+v", repository.createCodeParams)
	}
	if bytes.Contains(repository.createCodeParams.CodeHash, []byte(rawToken)) {
		t.Fatal("repository must receive only the purpose-bound hash, never the raw token")
	}

	persistenceError := errors.New("invite persistence failed")
	failingRepository := &enrollmentServiceRepositoryStub{createCodeErr: persistenceError}
	failingService := newEnrollmentServiceTestSubject(
		t,
		failingRepository,
		&enrollmentServiceClassAuthorizerStub{class: validClass},
		enrollmentServiceCodec(),
		now,
	)
	failed, err := failingService.CreateInviteCode(
		context.Background(),
		access,
		classID,
		CreateClassInviteCodeInput{
			ExpiresInSeconds: int(time.Hour / time.Second), UsageLimit: 10,
		},
	)
	if !errors.Is(err, persistenceError) || failed.Token != "" {
		t.Fatalf("failed persistence leaked token: result=%+v error=%v", failed, err)
	}

	draftCodec := enrollmentServiceCodec()
	draftRepository := &enrollmentServiceRepositoryStub{}
	draftService := newEnrollmentServiceTestSubject(
		t,
		draftRepository,
		&enrollmentServiceClassAuthorizerStub{
			class: Class{ID: classID, TenantID: access.TenantID, Status: ClassStatusDraft},
		},
		draftCodec,
		now,
	)
	if _, err := draftService.CreateInviteCode(
		context.Background(),
		access,
		classID,
		CreateClassInviteCodeInput{
			ExpiresInSeconds: int(time.Hour / time.Second), UsageLimit: 10,
		},
	); !errors.Is(err, ErrClassInviteCodeConflict) {
		t.Fatalf("draft invite code returned %v", err)
	}
	if draftRepository.createCodeCalls != 0 || draftCodec.randomCalls != 0 {
		t.Fatal("class lifecycle denial must happen before token generation")
	}
}

func TestEnrollmentServiceJoinInvalidTokenAndTenantAccess(t *testing.T) {
	now := time.Date(2026, time.July, 19, 5, 0, 0, 0, time.UTC)
	access := enrollmentServiceAccess()
	codec := enrollmentServiceCodec()
	repository := &enrollmentServiceRepositoryStub{}
	classes := &enrollmentServiceClassAuthorizerStub{}
	service := newEnrollmentServiceTestSubject(t, repository, classes, codec, now)

	for _, rawToken := range []string{
		"",
		"wrong_" + codec.randomValue,
		classInviteCodeTokenPrefix + "short",
		classInviteCodeTokenPrefix + codec.randomValue + "=",
	} {
		if _, err := service.JoinByInviteCode(
			context.Background(), access, rawToken,
		); !errors.Is(err, ErrClassInviteCodeUnavailable) {
			t.Fatalf("invalid token %q returned %v", rawToken, err)
		}
	}
	if repository.joinCalls != 0 || len(classes.calls) != 0 {
		t.Fatal("invalid tokens must not reach repository or class authorization")
	}

	classID := uuid.New()
	joinedEnrollment := Enrollment{
		ID: uuid.New(), TenantID: access.TenantID, ClassID: classID,
		UserID: access.ActorID, ClassRole: policy.ClassRoleStudent,
		Status: EnrollmentStatusActive,
	}
	repository.joinResult = JoinClassInvitationResult{
		Class:      Class{ID: classID, TenantID: access.TenantID, Status: ClassStatusActive},
		Enrollment: &joinedEnrollment,
		Joined:     true,
	}
	projectedClass := Class{
		ID: classID, TenantID: access.TenantID, Status: ClassStatusActive,
		ViewerAccess: ViewerAccess{CanJoinRoom: true, CanLeave: true},
	}
	classes.class = projectedClass
	validToken := classInviteCodeTokenPrefix + codec.randomValue
	result, err := service.JoinByInviteCode(
		context.Background(), access, "  "+validToken+"  ",
	)
	if err != nil || !result.Joined || result.Class != projectedClass ||
		result.Enrollment == nil || result.Enrollment.ID != joinedEnrollment.ID {
		t.Fatalf("join class invitation: result=%+v error=%v", result, err)
	}
	if repository.joinCalls != 1 ||
		repository.joinTenant.TenantID != access.TenantID ||
		repository.joinTenant.ActorID != access.ActorID ||
		!bytes.Equal(repository.joinHash, codec.digest) ||
		!repository.joinAt.Equal(now) {
		t.Fatalf("unexpected join persistence call: %+v", repository)
	}
	if len(classes.calls) != 1 ||
		classes.calls[0].ClassID != classID ||
		classes.calls[0].Action != policy.ActionClassView {
		t.Fatalf("unexpected post-join class authorization: %+v", classes.calls)
	}

	inactiveAccess := access
	inactiveAccess.MembershipActive = false
	joinCalls := repository.joinCalls
	if _, err := service.JoinByInviteCode(
		context.Background(), inactiveAccess, validToken,
	); !errors.Is(err, ErrEnrollmentAccessDenied) {
		t.Fatalf("inactive tenant access returned %v", err)
	}
	if repository.joinCalls != joinCalls {
		t.Fatal("inactive tenant access must fail before token persistence")
	}
}

func TestEnrollmentServiceLifecycleAndLeavePropagation(t *testing.T) {
	now := time.Date(2026, time.July, 19, 6, 0, 0, 0, time.UTC)
	access := enrollmentServiceAccess()
	classID := uuid.New()
	targetUserID := uuid.New()
	activeClass := Class{ID: classID, TenantID: access.TenantID, Status: ClassStatusActive}
	changedEnrollment := Enrollment{
		ID: uuid.New(), TenantID: access.TenantID, ClassID: classID,
		UserID: targetUserID, ClassRole: policy.ClassRoleStudent,
		Status: EnrollmentStatusSuspended,
	}
	repository := &enrollmentServiceRepositoryStub{
		suspendResult: EnrollmentMutationResult{Enrollment: changedEnrollment, Changed: true},
		leaveResult: EnrollmentMutationResult{
			Enrollment: Enrollment{
				ID: uuid.New(), TenantID: access.TenantID, ClassID: classID,
				UserID: access.ActorID, ClassRole: policy.ClassRoleStudent,
				Status: EnrollmentStatusLeft,
			},
			Changed: true,
		},
	}
	classes := &enrollmentServiceClassAuthorizerStub{class: activeClass}
	service := newEnrollmentServiceTestSubject(t, repository, classes, nil, now)

	if _, err := service.SuspendEnrollment(
		context.Background(), access, classID, uuid.Nil,
	); !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("nil suspend target returned %v", err)
	}
	if repository.suspendCalls != 0 || len(classes.calls) != 0 {
		t.Fatal("nil suspend target must fail before authorization")
	}

	suspended, err := service.SuspendEnrollment(
		context.Background(), access, classID, targetUserID,
	)
	if err != nil || !suspended.Changed ||
		suspended.Enrollment.Status != EnrollmentStatusSuspended {
		t.Fatalf("suspend enrollment: result=%+v error=%v", suspended, err)
	}
	if repository.suspendTenant.TenantID != access.TenantID ||
		repository.suspendTenant.ActorID != access.ActorID ||
		repository.suspendClass != classID ||
		repository.suspendUser != targetUserID ||
		!repository.suspendAt.Equal(now) ||
		len(classes.calls) != 1 ||
		classes.calls[0].Action != policy.ActionEnrollmentManage {
		t.Fatalf("unexpected suspend propagation: repository=%+v calls=%+v", repository, classes.calls)
	}

	classes.class.Status = ClassStatusArchived
	if _, err := service.SuspendEnrollment(
		context.Background(), access, classID, targetUserID,
	); !errors.Is(err, ErrEnrollmentConflict) {
		t.Fatalf("archived suspend returned %v", err)
	}
	if repository.suspendCalls != 1 {
		t.Fatal("archived suspend must not reach persistence")
	}
	classes.class = activeClass

	removeError := errors.New("remove transition rejected")
	repository.removeErr = removeError
	if _, err := service.RemoveEnrollment(
		context.Background(), access, classID, targetUserID,
	); !errors.Is(err, removeError) {
		t.Fatalf("remove repository error was not propagated: %v", err)
	}
	if repository.removeCalls != 1 || repository.removeUser != targetUserID ||
		!repository.removeAt.Equal(now) {
		t.Fatalf("unexpected remove propagation: %+v", repository)
	}

	if _, err := service.LeaveClass(
		context.Background(), access, uuid.Nil,
	); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("nil leave class returned %v", err)
	}
	if repository.leaveCalls != 0 {
		t.Fatal("nil leave class must not reach persistence")
	}
	left, err := service.LeaveClass(context.Background(), access, classID)
	if err != nil || !left.Changed || left.Enrollment.Status != EnrollmentStatusLeft {
		t.Fatalf("leave class: result=%+v error=%v", left, err)
	}
	if repository.leaveCalls != 1 ||
		repository.leaveTenant.TenantID != access.TenantID ||
		repository.leaveTenant.ActorID != access.ActorID ||
		repository.leaveClass != classID || !repository.leaveAt.Equal(now) {
		t.Fatalf("unexpected leave propagation: %+v", repository)
	}

	inactiveAccess := access
	inactiveAccess.MembershipActive = false
	if _, err := service.LeaveClass(
		context.Background(), inactiveAccess, classID,
	); !errors.Is(err, ErrEnrollmentAccessDenied) {
		t.Fatalf("inactive leave access returned %v", err)
	}
	if repository.leaveCalls != 1 {
		t.Fatal("inactive leave access must fail before persistence")
	}
}

func TestEnrollmentServiceInviteLifecycleMethodsPropagateErrorsAndScope(t *testing.T) {
	now := time.Date(2026, time.July, 19, 7, 0, 0, 0, time.UTC)
	access := enrollmentServiceAccess()
	classID := uuid.New()
	codeID := uuid.New()
	activeClass := Class{ID: classID, TenantID: access.TenantID, Status: ClassStatusActive}
	code := ClassInviteCode{
		ID: codeID, TenantID: access.TenantID, ClassID: classID,
		Status: ClassInviteCodeStatusActive,
	}
	repository := &enrollmentServiceRepositoryStub{
		listCodeResult:   []ClassInviteCode{code},
		revokeCodeResult: code,
	}
	classes := &enrollmentServiceClassAuthorizerStub{class: activeClass}
	service := newEnrollmentServiceTestSubject(t, repository, classes, nil, now)

	codes, err := service.ListInviteCodes(context.Background(), access, classID)
	if err != nil || len(codes) != 1 || codes[0].ID != codeID {
		t.Fatalf("list invite codes: codes=%+v error=%v", codes, err)
	}
	if repository.listCodeTenant.TenantID != access.TenantID ||
		repository.listCodeTenant.ActorID != access.ActorID ||
		repository.listCodeClass != classID || !repository.listCodeAt.Equal(now) {
		t.Fatalf("unexpected list propagation: %+v", repository)
	}

	if _, err := service.RevokeInviteCode(
		context.Background(), access, classID, uuid.Nil,
	); !errors.Is(err, ErrClassInviteCodeUnavailable) {
		t.Fatalf("nil revoke code returned %v", err)
	}
	if repository.revokeCodeCalls != 0 {
		t.Fatal("nil code id must fail before authorization")
	}
	revoked, err := service.RevokeInviteCode(
		context.Background(), access, classID, codeID,
	)
	if err != nil || revoked.ID != codeID {
		t.Fatalf("revoke invite code: code=%+v error=%v", revoked, err)
	}
	if repository.revokeCodeTenant.TenantID != access.TenantID ||
		repository.revokeCodeTenant.ActorID != access.ActorID ||
		repository.revokeCodeClass != classID ||
		repository.revokeCodeID != codeID || !repository.revokeCodeAt.Equal(now) {
		t.Fatalf("unexpected revoke propagation: %+v", repository)
	}

	authorizationError := errors.New("class manager denied")
	classes.err = authorizationError
	listCalls := repository.listCodeCalls
	if _, err := service.ListInviteCodes(
		context.Background(), access, classID,
	); !errors.Is(err, authorizationError) {
		t.Fatalf("list authorization error was not propagated: %v", err)
	}
	if repository.listCodeCalls != listCalls {
		t.Fatal("list authorization failure must not reach persistence")
	}
}

func newEnrollmentServiceTestSubject(
	t *testing.T,
	repository EnrollmentRepository,
	classes ClassActionAuthorizer,
	codec ClassInviteCodeTokenCodec,
	now time.Time,
) *EnrollmentService {
	t.Helper()
	if codec == nil {
		codec = enrollmentServiceCodec()
	}
	service, err := NewEnrollmentService(
		repository,
		classes,
		policy.NewEngine(),
		codec,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("create enrollment service: %v", err)
	}
	return service
}

func enrollmentServiceAccess() AccessContext {
	return AccessContext{
		TenantID:          uuid.New(),
		ActorID:           uuid.New(),
		MembershipActive:  true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleStudent},
	}
}

func enrollmentServiceCodec() *enrollmentServiceTokenCodecStub {
	return &enrollmentServiceTokenCodecStub{
		randomValue: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x44}, 32)),
		digest:      bytes.Repeat([]byte{0x85}, 32),
	}
}
