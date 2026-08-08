package media

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestLifecycleServiceCreatesInstantSourceWithoutProviderData(t *testing.T) {
	t.Parallel()

	spaceID := uuid.New()
	meetingID := uuid.New()
	ids := []uuid.UUID{spaceID, meetingID}
	repository := &fakeLifecycleRepository{}
	service, err := NewLifecycleService(repository, LifecycleServiceConfig{
		Clock: func() time.Time { return mediaTestTime },
		NewID: func() uuid.UUID {
			id := ids[0]
			ids = ids[1:]
			return id
		},
	})
	if err != nil {
		t.Fatalf("new lifecycle service: %v", err)
	}

	_, err = service.CreateSpace(context.Background(), lifecycleAccess(), CreateSpaceInput{
		Source: CreateSourceInput{Kind: SourceInstant, Instant: InstantSourceInput{
			Title: "  Office hours  ", DurationMinutes: 45, Timezone: "Asia/Ho_Chi_Minh",
		}},
		IdempotencyKey: "instant-create-0001",
	})
	if err != nil {
		t.Fatalf("create instant media space: %v", err)
	}
	command := repository.createCommand
	if command.SpaceID != spaceID || command.InstantMeetingID != meetingID ||
		command.Source.Instant.Title != "Office hours" || command.CreatedAt != mediaTestTime ||
		len(command.Fingerprint) != 32 {
		t.Fatalf("unexpected instant command: %+v", command)
	}
}

func TestLifecycleServiceRejectsInvalidUnionBeforeRepository(t *testing.T) {
	t.Parallel()

	repository := &fakeLifecycleRepository{}
	service, err := NewLifecycleService(repository)
	if err != nil {
		t.Fatalf("new lifecycle service: %v", err)
	}
	_, err = service.CreateSpace(context.Background(), lifecycleAccess(), CreateSpaceInput{
		Source: CreateSourceInput{
			Kind: SourceClassSession, ClassSessionID: uuid.New(), StudyMeetingID: uuid.New(),
		},
		IdempotencyKey: "class-create-00001",
	})
	if !errors.Is(err, ErrInvalidSpaceRequest) || repository.createCalls != 0 {
		t.Fatalf("invalid source must be rejected before repository: err=%v calls=%d", err, repository.createCalls)
	}

	_, err = service.StartSpace(context.Background(), lifecycleAccess(), uuid.New(), TransitionInput{
		ExpectedVersion: 1, IdempotencyKey: "too-short",
	})
	if !errors.Is(err, ErrInvalidSpaceRequest) || repository.transitionCalls != 0 {
		t.Fatalf("invalid transition must be rejected before repository: err=%v calls=%d", err, repository.transitionCalls)
	}
}

func TestLifecycleServiceStartCreatesOpaqueRoomIntent(t *testing.T) {
	t.Parallel()

	instanceID := uuid.New()
	spaceID := uuid.New()
	repository := &fakeLifecycleRepository{}
	service, err := NewLifecycleService(repository, LifecycleServiceConfig{
		Clock: func() time.Time { return mediaTestTime }, NewID: func() uuid.UUID { return instanceID },
	})
	if err != nil {
		t.Fatalf("new lifecycle service: %v", err)
	}
	_, err = service.StartSpace(context.Background(), lifecycleAccess(), spaceID, TransitionInput{
		ExpectedVersion: 3, IdempotencyKey: "media-start-00001", ReasonCode: "scheduled_start",
	})
	if err != nil {
		t.Fatalf("start media space: %v", err)
	}
	command := repository.transitionCommand
	if command.Operation != "start" || command.SpaceID != spaceID ||
		command.RoomInstanceID != instanceID || command.OccurredAt != mediaTestTime ||
		len(command.Fingerprint) != 32 {
		t.Fatalf("unexpected transition command: %+v", command)
	}
	if command.ProviderRoomName != "r_"+strings.ReplaceAll(instanceID.String(), "-", "") ||
		strings.Contains(command.ProviderRoomName, spaceID.String()) {
		t.Fatalf("provider room name is not opaque: %q", command.ProviderRoomName)
	}
}

func TestLifecycleServiceNormalizesUnknownRepositoryErrors(t *testing.T) {
	t.Parallel()

	repository := &fakeLifecycleRepository{getError: errors.New("private database error")}
	service, err := NewLifecycleService(repository)
	if err != nil {
		t.Fatalf("new lifecycle service: %v", err)
	}
	_, err = service.GetSpace(context.Background(), lifecycleAccess(), uuid.New())
	if !errors.Is(err, ErrLifecycleUnavailable) || strings.Contains(err.Error(), "private database") {
		t.Fatalf("unknown repository error was not normalized: %v", err)
	}
}

func lifecycleAccess() AccessContext {
	return AccessContext{
		TenantID: uuid.New(), ActorID: uuid.New(), MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleTeacher},
	}
}

type fakeLifecycleRepository struct {
	createCalls       int
	transitionCalls   int
	createCommand     CreateSpaceCommand
	transitionCommand TransitionCommand
	createResult      CreateSpaceResult
	transitionResult  MediaSpace
	getResult         MediaSpace
	createError       error
	transitionError   error
	getError          error
}

func (repository *fakeLifecycleRepository) CreateSpace(
	_ context.Context,
	_ AccessContext,
	command CreateSpaceCommand,
) (CreateSpaceResult, error) {
	repository.createCalls++
	repository.createCommand = command
	return repository.createResult, repository.createError
}

func (repository *fakeLifecycleRepository) GetSpace(
	_ context.Context,
	_ AccessContext,
	_ uuid.UUID,
) (MediaSpace, error) {
	return repository.getResult, repository.getError
}

func (repository *fakeLifecycleRepository) TransitionSpace(
	_ context.Context,
	_ AccessContext,
	command TransitionCommand,
) (MediaSpace, error) {
	repository.transitionCalls++
	repository.transitionCommand = command
	return repository.transitionResult, repository.transitionError
}
