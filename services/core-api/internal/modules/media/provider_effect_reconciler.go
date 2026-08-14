package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrProviderEffectReconcileUnavailable = errors.New("media provider effect reconciliation unavailable")

// DurableProviderEffectRef is the immutable database identity of a committed
// provider effect. OriginalActorID is retained only to address the original
// receipt and preserve attribution; reconciliation never impersonates or
// reauthorizes that interactive actor.
type DurableProviderEffectRef struct {
	TenantID        uuid.UUID
	OriginalActorID uuid.UUID
	SpaceID         uuid.UUID
	IdempotencyKey  string
	Operation       string
	Attempt         int
}

type DurableProviderEffect struct {
	Ref                 DurableProviderEffectRef
	RoomName            string
	ParticipantIdentity string
}

type DurableProviderEffectRepository interface {
	ClaimNextProviderEffect(
		context.Context,
		time.Time,
		time.Duration,
	) (DurableProviderEffect, bool, error)
	CompleteProviderEffect(
		context.Context,
		DurableProviderEffectRef,
		ProviderEffectStatus,
		string,
		time.Time,
	) error
}

type DurableProviderEffectReconciler struct {
	repository          DurableProviderEffectRepository
	roomProvider        RoomProvider
	participantProvider ParticipantModerationProvider
	clock               func() time.Time
	lease               time.Duration
}

func NewDurableProviderEffectReconciler(
	repository DurableProviderEffectRepository,
	roomProvider RoomProvider,
	participantProvider ParticipantModerationProvider,
	clock func() time.Time,
) (*DurableProviderEffectReconciler, error) {
	if repository == nil || roomProvider == nil || participantProvider == nil {
		return nil, fmt.Errorf("provider effect reconciler dependencies are required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &DurableProviderEffectReconciler{
		repository: repository, roomProvider: roomProvider,
		participantProvider: participantProvider, clock: clock,
		lease: providerModerationLease,
	}, nil
}

// ReconcileOnce claims at most one durable receipt. It deliberately takes no
// AccessContext: the business mutation was already authorized and committed,
// and a moderator logout must not strand its provider-side convergence.
func (reconciler *DurableProviderEffectReconciler) ReconcileOnce(
	ctx context.Context,
) (bool, error) {
	if reconciler == nil || reconciler.repository == nil || reconciler.lease <= 0 {
		return false, ErrProviderEffectReconcileUnavailable
	}
	now := reconciler.clock().UTC()
	effect, claimed, err := reconciler.repository.ClaimNextProviderEffect(
		ctx, now, reconciler.lease,
	)
	if err != nil || !claimed {
		return claimed, err
	}

	status, errorCode := ProviderEffectApplied, ""
	if err := reconciler.apply(ctx, effect); err != nil {
		status, errorCode = classifyModerationProviderError(err)
	}
	if err := reconciler.repository.CompleteProviderEffect(
		ctx, effect.Ref, status, errorCode, reconciler.clock().UTC(),
	); err != nil {
		return true, err
	}
	return true, nil
}

// Run is a bounded polling loop suitable for multiple stateless Core API
// replicas. Database row leases make concurrent replicas single-winner.
func (reconciler *DurableProviderEffectReconciler) Run(
	ctx context.Context,
	pollInterval time.Duration,
	report func(error),
) {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_, err := reconciler.ReconcileOnce(ctx)
			if err != nil && ctx.Err() == nil && report != nil {
				report(err)
			}
			timer.Reset(pollInterval)
		}
	}
}

func (reconciler *DurableProviderEffectReconciler) apply(
	ctx context.Context,
	effect DurableProviderEffect,
) error {
	if !opaqueProviderRoomNamePattern.MatchString(effect.RoomName) {
		return ErrInvalidProviderResponse
	}
	switch ModerationOperation(effect.Ref.Operation) {
	case ModerationOperation("end"):
		return reconciler.roomProvider.DeleteRoom(ctx, effect.RoomName)
	case ModerationMute:
		if effect.ParticipantIdentity == "" {
			return ErrInvalidProviderResponse
		}
		return reconciler.participantProvider.MuteParticipantMicrophone(
			ctx, effect.RoomName, effect.ParticipantIdentity,
		)
	case ModerationPromote, ModerationDemote, ModerationRemove:
		if effect.ParticipantIdentity == "" {
			return ErrInvalidProviderResponse
		}
		return reconciler.participantProvider.RemoveParticipant(
			ctx, effect.RoomName, effect.ParticipantIdentity,
		)
	default:
		return ErrInvalidModerationRequest
	}
}
