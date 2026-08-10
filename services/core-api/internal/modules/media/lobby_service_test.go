package media

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestLobbyServiceNormalizesExactTenantEmailAndKeepsItInputOnly(t *testing.T) {
	t.Parallel()

	repository := &fakeLobbyRepository{member: LobbyMember{
		UserID: uuid.New(), DisplayName: "Student", Status: LobbyMemberActive,
		Version: 1, CreatedAt: mediaTestTime, UpdatedAt: mediaTestTime,
	}}
	service, err := NewLobbyService(repository, LobbyServiceConfig{
		Clock: func() time.Time { return mediaTestTime },
	})
	if err != nil {
		t.Fatalf("create lobby service: %v", err)
	}
	spaceID := uuid.New()
	syntheticMutationID := "lobby" + "-" + "invite" + "-" + "0001"
	member, err := service.InviteMember(
		context.Background(), validLobbyServiceAccess(), spaceID,
		InviteLobbyMemberInput{
			Email: "  Student@Example.COM  ", ExpectedSpaceVersion: 7,
			IdempotencyKey: syntheticMutationID,
		},
	)
	if err != nil {
		t.Fatalf("invite lobby member: %v", err)
	}
	if member.DisplayName != "Student" || repository.invite.TargetEmail != "student@example.com" ||
		repository.invite.SpaceID != spaceID || repository.invite.ExpectedSpaceVersion != 7 ||
		len(repository.invite.Fingerprint) != 32 || !repository.invite.OccurredAt.Equal(mediaTestTime) {
		t.Fatalf("unexpected member or normalized command: member=%+v command=%+v", member, repository.invite)
	}
}

func TestLobbyServiceAdmissionFingerprintBindsEveryAuthorityVersionAndReason(t *testing.T) {
	t.Parallel()

	spaceID, roomID, admissionID := uuid.New(), uuid.New(), uuid.New()
	base := AdmissionMutationCommand{
		SpaceID: spaceID, AdmissionID: admissionID, Operation: "admission_deny",
		ExpectedSpaceVersion: 3, ExpectedRoomInstanceID: roomID,
		ExpectedRoomInstanceVersion: 5, ExpectedAdmissionVersion: 7,
		ReasonCode: "not_expected", AdmissionTTL: defaultLobbyAdmissionTTL,
	}
	baseDigest := lobbyAdmissionFingerprint(base)
	mutations := []AdmissionMutationCommand{
		func() AdmissionMutationCommand { value := base; value.SpaceID = uuid.New(); return value }(),
		func() AdmissionMutationCommand { value := base; value.AdmissionID = uuid.New(); return value }(),
		func() AdmissionMutationCommand { value := base; value.ExpectedSpaceVersion++; return value }(),
		func() AdmissionMutationCommand {
			value := base
			value.ExpectedRoomInstanceID = uuid.New()
			return value
		}(),
		func() AdmissionMutationCommand { value := base; value.ExpectedRoomInstanceVersion++; return value }(),
		func() AdmissionMutationCommand { value := base; value.ExpectedAdmissionVersion++; return value }(),
		func() AdmissionMutationCommand { value := base; value.ReasonCode = "different"; return value }(),
	}
	for index, mutation := range mutations {
		if bytes.Equal(baseDigest, lobbyAdmissionFingerprint(mutation)) {
			t.Fatalf("fingerprint mutation %d did not change the digest", index)
		}
	}
}

func TestLobbyServiceDenyRequiresBoundedReasonBeforeRepository(t *testing.T) {
	t.Parallel()

	repository := &fakeLobbyRepository{}
	service, err := NewLobbyService(repository)
	if err != nil {
		t.Fatalf("create lobby service: %v", err)
	}
	_, err = service.Deny(
		context.Background(), validLobbyServiceAccess(), uuid.New(), uuid.New(),
		AdmissionMutationInput{
			ExpectedSpaceVersion: 2, ExpectedRoomInstanceID: uuid.New(),
			ExpectedRoomInstanceVersion: 3, ExpectedAdmissionVersion: 1,
			IdempotencyKey: "lobby-deny-key-0001",
		},
	)
	if !errors.Is(err, ErrInvalidLobbyRequest) || repository.admissionCalls != 0 {
		t.Fatalf("reasonless deny reached repository: err=%v calls=%d", err, repository.admissionCalls)
	}
}

func TestLobbyServiceSelfCancellationUsesJoinAttemptNotAdmissionEnumeration(t *testing.T) {
	t.Parallel()

	joinAttemptID := uuid.New()
	repository := &fakeLobbyRepository{attempt: JoinAttempt{
		ParticipantSessionID: uuid.New(), RoomInstanceID: uuid.New(),
		JoinAttemptID: joinAttemptID, Status: JoinAttemptCancelled,
		Version: 2, InstanceRole: InstanceRoleAttendee,
		CreatedAt: mediaTestTime, UpdatedAt: mediaTestTime,
	}}
	service, err := NewLobbyService(repository, LobbyServiceConfig{
		Clock: func() time.Time { return mediaTestTime },
	})
	if err != nil {
		t.Fatalf("create lobby service: %v", err)
	}
	roomID := uuid.New()
	_, err = service.CancelJoinAttempt(
		context.Background(), validLobbyServiceAccess(), uuid.New(), joinAttemptID,
		AdmissionMutationInput{
			ExpectedSpaceVersion: 4, ExpectedRoomInstanceID: roomID,
			ExpectedRoomInstanceVersion: 2, ExpectedAdmissionVersion: 1,
			IdempotencyKey: "lobby-cancel-key-001",
		},
	)
	if err != nil {
		t.Fatalf("cancel lobby join attempt: %v", err)
	}
	if repository.cancelCalls != 1 || repository.cancel.JoinAttemptID != joinAttemptID ||
		repository.cancel.AdmissionID != uuid.Nil || repository.cancel.Operation != "admission_cancel" ||
		repository.cancel.ExpectedRoomInstanceID != roomID {
		t.Fatalf("self cancellation used the wrong identifier: %+v", repository.cancel)
	}
}

func TestLobbyServiceListDefaultsLimitAndReturnsNonNilItems(t *testing.T) {
	t.Parallel()

	repository := &fakeLobbyRepository{}
	service, err := NewLobbyService(repository)
	if err != nil {
		t.Fatalf("create lobby service: %v", err)
	}
	page, err := service.ListAdmissions(
		context.Background(), validLobbyServiceAccess(), uuid.New(),
		ListLobbyAdmissionsInput{
			ExpectedSpaceVersion: 1, ExpectedRoomInstanceID: uuid.New(),
			ExpectedRoomInstanceVersion: 1,
		},
	)
	if err != nil {
		t.Fatalf("list lobby admissions: %v", err)
	}
	if repository.listInput.Limit != defaultLobbyListLimit || page.Items == nil {
		t.Fatalf("list defaults not applied: input=%+v page=%+v", repository.listInput, page)
	}
}

type fakeLobbyRepository struct {
	page           LobbyAdmissionPage
	memberPage     LobbyMemberPage
	admission      LobbyAdmission
	member         LobbyMember
	attempt        JoinAttempt
	err            error
	listInput      ListLobbyAdmissionsInput
	invite         InviteLobbyMemberCommand
	cancel         AdmissionMutationCommand
	admissionCalls int
	cancelCalls    int
}

func validLobbyServiceAccess() AccessContext {
	return AccessContext{
		TenantID: uuid.New(), ActorID: uuid.New(), SessionID: uuid.New(),
		MembershipActive:  true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleStudent},
	}
}

func (repository *fakeLobbyRepository) ListAdmissions(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
	input ListLobbyAdmissionsInput,
	_ time.Time,
	_ time.Duration,
) (LobbyAdmissionPage, error) {
	repository.listInput = input
	return repository.page, repository.err
}

func (repository *fakeLobbyRepository) GetJoinAttempt(
	context.Context,
	AccessContext,
	uuid.UUID,
	uuid.UUID,
	ListLobbyAdmissionsInput,
	time.Time,
	time.Duration,
) (JoinAttempt, error) {
	return repository.attempt, repository.err
}

func (repository *fakeLobbyRepository) MutateAdmission(
	_ context.Context,
	_ AccessContext,
	command AdmissionMutationCommand,
) (LobbyAdmission, error) {
	repository.admissionCalls++
	repository.cancel = command
	return repository.admission, repository.err
}

func (repository *fakeLobbyRepository) CancelJoinAttempt(
	_ context.Context,
	_ AccessContext,
	command AdmissionMutationCommand,
) (JoinAttempt, error) {
	repository.cancelCalls++
	repository.cancel = command
	return repository.attempt, repository.err
}

func (repository *fakeLobbyRepository) ListMembers(
	context.Context,
	AccessContext,
	uuid.UUID,
	ListLobbyMembersInput,
) (LobbyMemberPage, error) {
	return repository.memberPage, repository.err
}

func (repository *fakeLobbyRepository) InviteMember(
	_ context.Context,
	_ AccessContext,
	command InviteLobbyMemberCommand,
) (LobbyMember, error) {
	repository.invite = command
	return repository.member, repository.err
}

func (repository *fakeLobbyRepository) MutateMember(
	context.Context,
	AccessContext,
	LobbyMemberMutationCommand,
) (LobbyMember, error) {
	return repository.member, repository.err
}
