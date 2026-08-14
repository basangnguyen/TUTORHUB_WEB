package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

const providerModerationLease = 30 * time.Second

type ModerationOperation string

const (
	ModerationLock    ModerationOperation = "lock"
	ModerationUnlock  ModerationOperation = "unlock"
	ModerationPromote ModerationOperation = "participant_promote"
	ModerationDemote  ModerationOperation = "participant_demote"
	ModerationMute    ModerationOperation = "participant_mute"
	ModerationRemove  ModerationOperation = "participant_remove"
)

type ProviderEffectStatus string

const (
	ProviderEffectNone            ProviderEffectStatus = "none"
	ProviderEffectPending         ProviderEffectStatus = "pending"
	ProviderEffectApplying        ProviderEffectStatus = "applying"
	ProviderEffectApplied         ProviderEffectStatus = "applied"
	ProviderEffectRetryableFailed ProviderEffectStatus = "retryable_failed"
	ProviderEffectPermanentFailed ProviderEffectStatus = "permanent_failed"
)

var (
	ErrInvalidModerationRequest  = errors.New("invalid media moderation request")
	ErrModerationNotFound        = errors.New("media moderation target not found")
	ErrModerationForbidden       = errors.New("media moderation forbidden")
	ErrModerationConflict        = errors.New("media moderation conflict")
	ErrModerationIdempotency     = errors.New("media moderation idempotency conflict")
	ErrModerationUnavailable     = errors.New("media moderation unavailable")
	moderationIdempotencyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{15,127}$`)
	moderationReasonPattern      = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
)

type ModerationRateLimitError struct {
	RetryAfter time.Duration
}

func (err *ModerationRateLimitError) Error() string {
	return "media moderation rate limit exceeded"
}

type ModerationExpectedVersions struct {
	RoomInstanceID    uuid.UUID
	SpaceVersion      int64
	RoomVersion       int64
	ProjectionVersion int64
}

type LockMediaSpaceInput struct {
	Expected       ModerationExpectedVersions
	IdempotencyKey string
	Locked         bool
	ReasonCode     string
}

type ChangeParticipantRoleInput struct {
	Expected       ModerationExpectedVersions
	IdempotencyKey string
	DesiredRole    InstanceRole
	ReasonCode     string
}

type ModerateParticipantInput struct {
	Expected       ModerationExpectedVersions
	IdempotencyKey string
	ReasonCode     string
}

type ModerationCommand struct {
	Operation      ModerationOperation
	SpaceID        uuid.UUID
	ParticipantKey uuid.UUID
	Expected       ModerationExpectedVersions
	IdempotencyKey string
	DesiredRole    InstanceRole
	ReasonCode     string
	Fingerprint    [sha256.Size]byte
	OccurredAt     time.Time
}

type ModerationResult struct {
	SpaceID                  uuid.UUID     `json:"space_id"`
	RoomInstanceID           uuid.UUID     `json:"room_instance_id"`
	SpaceVersion             int64         `json:"space_version"`
	RoomInstanceVersion      int64         `json:"room_instance_version"`
	ProjectionVersion        int64         `json:"projection_version"`
	TargetParticipantKey     *uuid.UUID    `json:"target_participant_key,omitempty"`
	TargetParticipantVersion *int64        `json:"target_participant_version,omitempty"`
	TargetInstanceRole       *InstanceRole `json:"target_instance_role,omitempty"`
	roleAssignmentVersion    int64
	Locked                   *bool                `json:"locked,omitempty"`
	ProviderEffectStatus     ProviderEffectStatus `json:"provider_effect_status"`
}

type ProviderModerationEffect struct {
	Operation           ModerationOperation
	RoomName            string
	ParticipantIdentity string
	Attempt             int
}

type ModerationRepository interface {
	ApplyModeration(context.Context, AccessContext, ModerationCommand) (ModerationResult, error)
	ClaimProviderModerationEffect(
		context.Context,
		AccessContext,
		string,
		time.Time,
		time.Duration,
	) (ProviderModerationEffect, ProviderEffectStatus, bool, error)
	CompleteProviderModerationEffect(
		context.Context,
		AccessContext,
		string,
		int,
		ProviderEffectStatus,
		string,
		time.Time,
	) (ProviderEffectStatus, error)
}

type ParticipantModerationProvider interface {
	MuteParticipantMicrophone(context.Context, string, string) error
	RemoveParticipant(context.Context, string, string) error
}

type ModerationServiceAPI interface {
	SetLocked(context.Context, AccessContext, uuid.UUID, LockMediaSpaceInput) (ModerationResult, error)
	ChangeParticipantRole(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		ChangeParticipantRoleInput,
	) (ModerationResult, error)
	MuteParticipant(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		ModerateParticipantInput,
	) (ModerationResult, error)
	RemoveParticipant(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		ModerateParticipantInput,
	) (ModerationResult, error)
}

type ModerationService struct {
	repository ModerationRepository
	provider   ParticipantModerationProvider
	clock      func() time.Time
}

func NewModerationService(
	repository ModerationRepository,
	provider ParticipantModerationProvider,
	clock func() time.Time,
) (*ModerationService, error) {
	if repository == nil || provider == nil {
		return nil, fmt.Errorf("media moderation repository and provider are required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &ModerationService{repository: repository, provider: provider, clock: clock}, nil
}

func (service *ModerationService) SetLocked(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input LockMediaSpaceInput,
) (ModerationResult, error) {
	operation := ModerationUnlock
	if input.Locked {
		operation = ModerationLock
	}
	return service.apply(ctx, access, ModerationCommand{
		Operation: operation, SpaceID: spaceID, Expected: input.Expected,
		IdempotencyKey: input.IdempotencyKey, ReasonCode: input.ReasonCode,
	})
}

func (service *ModerationService) ChangeParticipantRole(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	participantKey uuid.UUID,
	input ChangeParticipantRoleInput,
) (ModerationResult, error) {
	operation := ModerationDemote
	if input.DesiredRole == InstanceRoleCoHost {
		operation = ModerationPromote
	}
	return service.apply(ctx, access, ModerationCommand{
		Operation: operation, SpaceID: spaceID, ParticipantKey: participantKey,
		Expected: input.Expected, IdempotencyKey: input.IdempotencyKey,
		DesiredRole: input.DesiredRole, ReasonCode: input.ReasonCode,
	})
}

func (service *ModerationService) MuteParticipant(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	participantKey uuid.UUID,
	input ModerateParticipantInput,
) (ModerationResult, error) {
	return service.apply(ctx, access, ModerationCommand{
		Operation: ModerationMute, SpaceID: spaceID, ParticipantKey: participantKey,
		Expected: input.Expected, IdempotencyKey: input.IdempotencyKey,
		ReasonCode: input.ReasonCode,
	})
}

func (service *ModerationService) RemoveParticipant(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	participantKey uuid.UUID,
	input ModerateParticipantInput,
) (ModerationResult, error) {
	return service.apply(ctx, access, ModerationCommand{
		Operation: ModerationRemove, SpaceID: spaceID, ParticipantKey: participantKey,
		Expected: input.Expected, IdempotencyKey: input.IdempotencyKey,
		ReasonCode: input.ReasonCode,
	})
}

func (service *ModerationService) apply(
	ctx context.Context,
	access AccessContext,
	command ModerationCommand,
) (ModerationResult, error) {
	if service == nil || service.repository == nil || service.provider == nil {
		return ModerationResult{}, ErrModerationUnavailable
	}
	command.ReasonCode = strings.TrimSpace(command.ReasonCode)
	if !validModerationCommand(access, command) {
		return ModerationResult{}, ErrInvalidModerationRequest
	}
	command.OccurredAt = service.clock().UTC()
	command.Fingerprint = moderationFingerprint(command)
	result, err := service.repository.ApplyModeration(ctx, access, command)
	if err != nil {
		return ModerationResult{}, normalizeModerationError(err)
	}
	if result.ProviderEffectStatus == ProviderEffectNone ||
		result.ProviderEffectStatus == ProviderEffectApplied ||
		result.ProviderEffectStatus == ProviderEffectPermanentFailed {
		return result, nil
	}
	effect, status, claimed, err := service.repository.ClaimProviderModerationEffect(
		ctx, access, command.IdempotencyKey, command.OccurredAt, providerModerationLease,
	)
	if err != nil {
		return ModerationResult{}, normalizeModerationError(err)
	}
	result.ProviderEffectStatus = status
	if !claimed {
		result.ProviderEffectStatus = publicProviderEffectStatus(status)
		return result, nil
	}
	providerErr := service.applyProviderEffect(ctx, effect)
	completedStatus, errorCode := ProviderEffectApplied, ""
	if providerErr != nil {
		completedStatus, errorCode = classifyModerationProviderError(providerErr)
	}
	status, err = service.repository.CompleteProviderModerationEffect(
		ctx, access, command.IdempotencyKey, effect.Attempt,
		completedStatus, errorCode, service.clock().UTC(),
	)
	if err != nil {
		return ModerationResult{}, normalizeModerationError(err)
	}
	result.ProviderEffectStatus = status
	return result, nil
}

// ProviderEffectApplying is an internal lease state. Public clients only need
// to know that provider convergence is still pending.
func publicProviderEffectStatus(status ProviderEffectStatus) ProviderEffectStatus {
	if status == ProviderEffectApplying {
		return ProviderEffectPending
	}
	return status
}

func (service *ModerationService) applyProviderEffect(
	ctx context.Context,
	effect ProviderModerationEffect,
) error {
	switch effect.Operation {
	case ModerationMute:
		return service.provider.MuteParticipantMicrophone(
			ctx, effect.RoomName, effect.ParticipantIdentity,
		)
	case ModerationPromote, ModerationDemote, ModerationRemove:
		return service.provider.RemoveParticipant(
			ctx, effect.RoomName, effect.ParticipantIdentity,
		)
	default:
		return ErrInvalidModerationRequest
	}
}

func validModerationCommand(access AccessContext, command ModerationCommand) bool {
	if access.TenantID == uuid.Nil || access.ActorID == uuid.Nil || access.SessionID == uuid.Nil ||
		command.SpaceID == uuid.Nil || command.Expected.RoomInstanceID == uuid.Nil ||
		command.Expected.SpaceVersion < 1 || command.Expected.RoomVersion < 1 ||
		command.Expected.ProjectionVersion < 1 ||
		!moderationIdempotencyPattern.MatchString(command.IdempotencyKey) ||
		(command.ReasonCode != "" && !moderationReasonPattern.MatchString(command.ReasonCode)) {
		return false
	}
	switch command.Operation {
	case ModerationLock, ModerationUnlock:
		return command.ParticipantKey == uuid.Nil && command.DesiredRole == ""
	case ModerationPromote:
		return command.ParticipantKey != uuid.Nil && command.DesiredRole == InstanceRoleCoHost
	case ModerationDemote:
		return command.ParticipantKey != uuid.Nil && command.DesiredRole == InstanceRoleAttendee
	case ModerationMute, ModerationRemove:
		return command.ParticipantKey != uuid.Nil && command.DesiredRole == ""
	default:
		return false
	}
}

func moderationFingerprint(command ModerationCommand) [sha256.Size]byte {
	return sha256.Sum256([]byte(fmt.Sprintf(
		"media.moderation.v1\x00%s\x00%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%s\x00%s",
		command.Operation, command.SpaceID, command.ParticipantKey,
		command.Expected.RoomInstanceID, command.Expected.SpaceVersion,
		command.Expected.RoomVersion, command.Expected.ProjectionVersion,
		command.DesiredRole, command.ReasonCode,
	)))
}

func classifyModerationProviderError(err error) (ProviderEffectStatus, string) {
	if errors.Is(err, ErrInvalidRequest) || errors.Is(err, ErrInvalidProviderResponse) ||
		errors.Is(err, ErrInvalidModerationRequest) {
		return ProviderEffectPermanentFailed, "provider_invalid_response"
	}
	return ProviderEffectRetryableFailed, "provider_unavailable"
}

func normalizeModerationError(err error) error {
	if err == nil {
		return nil
	}
	var rateLimit *ModerationRateLimitError
	if errors.As(err, &rateLimit) {
		return err
	}
	for _, known := range []error{
		ErrInvalidModerationRequest, ErrModerationNotFound, ErrModerationForbidden,
		ErrModerationConflict, ErrModerationIdempotency, ErrModerationUnavailable,
		ErrRoomNotOpen, ErrSpaceNotFound, ErrSourceUnavailable,
	} {
		if errors.Is(err, known) {
			return err
		}
	}
	return ErrModerationUnavailable
}
