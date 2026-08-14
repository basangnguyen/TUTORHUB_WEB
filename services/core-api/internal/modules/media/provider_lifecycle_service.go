package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type RoomBindingRepository interface {
	ActivateRoomInstance(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		string,
		time.Time,
	) (MediaSpace, error)
	ProviderRoomName(context.Context, AccessContext, uuid.UUID) (string, error)
	ClaimEndProviderEffect(
		context.Context,
		AccessContext,
		uuid.UUID,
		string,
		time.Time,
		time.Duration,
	) (EndRoomProviderEffect, ProviderEffectStatus, bool, error)
	CompleteEndProviderEffect(
		context.Context,
		AccessContext,
		uuid.UUID,
		string,
		int,
		ProviderEffectStatus,
		string,
		time.Time,
	) (ProviderEffectStatus, error)
}

type EndRoomProviderEffect struct {
	RoomName string
	Attempt  int
}

// MediaProviderConvergenceError means the TutorHub lifecycle transition was
// committed, while the provider-side effect still needs durable convergence.
// It intentionally exposes no provider identifier or raw provider error.
type MediaProviderConvergenceError struct {
	Space                MediaSpace
	ProviderEffectStatus ProviderEffectStatus
}

func (err *MediaProviderConvergenceError) Error() string {
	return ErrMediaProviderUnavailable.Error()
}

func (err *MediaProviderConvergenceError) Unwrap() error {
	return ErrMediaProviderUnavailable
}

// ProviderLifecycleService keeps provider calls outside PostgreSQL transactions.
// A start retry always reuses the persisted opaque room name and converges on
// the same RoomInstance binding.
type ProviderLifecycleService struct {
	base     LifecycleServiceAPI
	bindings RoomBindingRepository
	provider RoomProvider
	clock    func() time.Time
}

func NewProviderLifecycleService(
	base LifecycleServiceAPI,
	bindings RoomBindingRepository,
	provider RoomProvider,
	clock func() time.Time,
) (*ProviderLifecycleService, error) {
	if base == nil || bindings == nil || provider == nil {
		return nil, fmt.Errorf("media lifecycle, binding repository, and room provider are required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &ProviderLifecycleService{
		base: base, bindings: bindings, provider: provider, clock: clock,
	}, nil
}

func (service *ProviderLifecycleService) CreateSpace(
	ctx context.Context,
	access AccessContext,
	input CreateSpaceInput,
) (CreateSpaceResult, error) {
	return service.base.CreateSpace(ctx, access, input)
}

func (service *ProviderLifecycleService) GetSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
) (MediaSpace, error) {
	return service.base.GetSpace(ctx, access, spaceID)
}

func (service *ProviderLifecycleService) StartSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input TransitionInput,
) (MediaSpace, error) {
	space, err := service.base.StartSpace(ctx, access, spaceID, input)
	if err != nil {
		return MediaSpace{}, err
	}
	instance := space.ActiveRoomInstance
	if instance == nil {
		providerRoomName, lookupErr := service.bindings.ProviderRoomName(ctx, access, spaceID)
		if lookupErr != nil {
			return MediaSpace{}, ErrSpaceTransition
		}
		if deleteErr := service.provider.DeleteRoom(ctx, providerRoomName); deleteErr != nil {
			return MediaSpace{}, ErrMediaProviderUnavailable
		}
		return MediaSpace{}, ErrSpaceTransition
	}
	if instance.Status == RoomInstanceActive && instance.ProviderRoomSID != "" {
		return space, nil
	}
	if instance.Status != RoomInstanceProvisioning || instance.ProviderRoomName == "" {
		return MediaSpace{}, ErrSpaceTransition
	}
	providerRoom, err := service.provider.EnsureRoom(ctx, instance.ProviderRoomName)
	if err != nil || providerRoom.SID == "" {
		return MediaSpace{}, ErrMediaProviderUnavailable
	}
	activated, err := service.bindings.ActivateRoomInstance(
		ctx, access, space.ID, instance.ID, providerRoom.SID, service.clock().UTC(),
	)
	if err != nil {
		if errors.Is(err, errRoomActivationTerminal) {
			if deleteErr := service.provider.DeleteRoom(
				ctx, instance.ProviderRoomName,
			); deleteErr != nil {
				return MediaSpace{}, ErrMediaProviderUnavailable
			}
			return MediaSpace{}, ErrSpaceTransition
		}
		return MediaSpace{}, err
	}
	return activated, nil
}

func (service *ProviderLifecycleService) EndSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input TransitionInput,
) (MediaSpace, error) {
	space, err := service.base.EndSpace(ctx, access, spaceID, input)
	if err != nil {
		return MediaSpace{}, err
	}
	effect, status, claimed, err := service.bindings.ClaimEndProviderEffect(
		ctx, access, spaceID, input.IdempotencyKey, service.clock().UTC(),
		providerModerationLease,
	)
	if err != nil {
		return space, providerConvergenceError(space, status)
	}
	if !claimed {
		if status == ProviderEffectApplied {
			return space, nil
		}
		return space, providerConvergenceError(space, status)
	}
	completedStatus, errorCode := ProviderEffectApplied, ""
	if err := service.provider.DeleteRoom(ctx, effect.RoomName); err != nil {
		completedStatus, errorCode = classifyModerationProviderError(err)
	}
	status, err = service.bindings.CompleteEndProviderEffect(
		ctx, access, spaceID, input.IdempotencyKey, effect.Attempt,
		completedStatus, errorCode,
		service.clock().UTC(),
	)
	if err != nil {
		return space, providerConvergenceError(space, ProviderEffectPending)
	}
	if status != ProviderEffectApplied {
		return space, providerConvergenceError(space, status)
	}
	return space, nil
}

func providerConvergenceError(
	space MediaSpace,
	status ProviderEffectStatus,
) *MediaProviderConvergenceError {
	status = publicProviderEffectStatus(status)
	if status == ProviderEffectNone || status == "" {
		status = ProviderEffectPending
	}
	return &MediaProviderConvergenceError{
		Space: space, ProviderEffectStatus: status,
	}
}

func (service *ProviderLifecycleService) CancelSpace(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input TransitionInput,
) (MediaSpace, error) {
	return service.base.CancelSpace(ctx, access, spaceID, input)
}
