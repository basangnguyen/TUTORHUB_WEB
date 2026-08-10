package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJoinAttemptServiceReturnsBoundedWaitingProjection(t *testing.T) {
	t.Parallel()

	spaceID, roomID, attemptID := uuid.New(), uuid.New(), uuid.New()
	admissionID := uuid.New()
	admissionVersion := int64(1)
	expiresAt := mediaTestTime.Add(defaultLobbyAdmissionTTL)
	repository := &fakeJoinAttemptRepository{result: CreateJoinAttemptResult{
		Created: true,
		Attempt: JoinAttempt{
			ParticipantSessionID: uuid.New(), RoomInstanceID: roomID,
			AdmissionRequestID: &admissionID, AdmissionVersion: &admissionVersion,
			JoinAttemptID: attemptID,
			Status:        JoinAttemptWaiting, Version: 1, InstanceRole: InstanceRoleAttendee,
			CanPublishCameraMicrophone: true, CanSubscribe: true,
			CreatedAt: mediaTestTime, UpdatedAt: mediaTestTime, ExpiresAt: &expiresAt,
		},
	}}
	service, err := NewJoinAttemptService(repository, func() time.Time { return mediaTestTime })
	if err != nil {
		t.Fatalf("create join-attempt service: %v", err)
	}
	access := validInstanceCredentialAccess()
	input := CreateJoinAttemptInput{
		JoinAttemptID: attemptID, ExpectedRoomInstanceID: roomID, ExpectedSpaceVersion: 4,
	}

	result, err := service.CreateJoinAttempt(context.Background(), access, spaceID, input)
	if err != nil {
		t.Fatalf("create join attempt: %v", err)
	}
	if !result.Created || result.Attempt.Status != JoinAttemptWaiting ||
		result.Attempt.AdmissionRequestID == nil || *result.Attempt.AdmissionRequestID != admissionID ||
		repository.calls != 1 || repository.spaceID != spaceID || repository.input != input ||
		!repository.now.Equal(mediaTestTime) {
		t.Fatalf("unexpected join-attempt result or repository call: result=%+v repository=%+v", result, repository)
	}
}

func TestJoinAttemptServiceRejectsInvalidInputBeforeRepository(t *testing.T) {
	t.Parallel()

	repository := &fakeJoinAttemptRepository{}
	service, err := NewJoinAttemptService(repository, nil)
	if err != nil {
		t.Fatalf("create join-attempt service: %v", err)
	}
	_, err = service.CreateJoinAttempt(
		context.Background(), validInstanceCredentialAccess(), uuid.New(),
		CreateJoinAttemptInput{JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: uuid.New()},
	)
	if !errors.Is(err, ErrInvalidJoinAttempt) || repository.calls != 0 {
		t.Fatalf("invalid input reached repository: err=%v calls=%d", err, repository.calls)
	}
}

func TestJoinAttemptServiceFailsClosedOnInvalidProjection(t *testing.T) {
	t.Parallel()

	roomID, attemptID := uuid.New(), uuid.New()
	repository := &fakeJoinAttemptRepository{result: CreateJoinAttemptResult{Attempt: JoinAttempt{
		ParticipantSessionID: uuid.New(), RoomInstanceID: roomID, JoinAttemptID: attemptID,
		Status: JoinAttemptWaiting, Version: 1, InstanceRole: InstanceRoleAttendee,
		CreatedAt: mediaTestTime, UpdatedAt: mediaTestTime,
	}}}
	service, err := NewJoinAttemptService(repository, nil)
	if err != nil {
		t.Fatalf("create join-attempt service: %v", err)
	}
	_, err = service.CreateJoinAttempt(
		context.Background(), validInstanceCredentialAccess(), uuid.New(),
		CreateJoinAttemptInput{
			JoinAttemptID: attemptID, ExpectedRoomInstanceID: roomID, ExpectedSpaceVersion: 1,
		},
	)
	if !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("invalid waiting projection did not fail closed: %v", err)
	}
}

func TestJoinAttemptServiceRedactsUnknownRepositoryFailure(t *testing.T) {
	t.Parallel()

	repository := &fakeJoinAttemptRepository{err: errors.New("database-sensitive-detail")}
	service, err := NewJoinAttemptService(repository, nil)
	if err != nil {
		t.Fatalf("create join-attempt service: %v", err)
	}
	_, err = service.CreateJoinAttempt(
		context.Background(), validInstanceCredentialAccess(), uuid.New(),
		CreateJoinAttemptInput{
			JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: uuid.New(), ExpectedSpaceVersion: 1,
		},
	)
	if !errors.Is(err, ErrLifecycleUnavailable) || err.Error() == "database-sensitive-detail" {
		t.Fatalf("repository failure was not normalized: %v", err)
	}
}

type fakeJoinAttemptRepository struct {
	result  CreateJoinAttemptResult
	err     error
	calls   int
	access  AccessContext
	spaceID uuid.UUID
	input   CreateJoinAttemptInput
	now     time.Time
}

func (repository *fakeJoinAttemptRepository) CreateOrReuseJoinAttempt(
	_ context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input CreateJoinAttemptInput,
	now time.Time,
) (CreateJoinAttemptResult, error) {
	repository.calls++
	repository.access, repository.spaceID, repository.input, repository.now = access, spaceID, input, now
	return repository.result, repository.err
}
