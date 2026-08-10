package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

type JoinAttemptStatus string

const (
	JoinAttemptWaiting             JoinAttemptStatus = "waiting"
	JoinAttemptAdmitted            JoinAttemptStatus = "admitted"
	JoinAttemptJoining             JoinAttemptStatus = "joining"
	JoinAttemptDenied              JoinAttemptStatus = "denied"
	JoinAttemptCancelled           JoinAttemptStatus = "cancelled"
	JoinAttemptMeetingEnded        JoinAttemptStatus = "meeting_ended"
	JoinAttemptTimeout             JoinAttemptStatus = "timeout"
	JoinAttemptProviderUnavailable JoinAttemptStatus = "provider_unavailable"
)

var (
	ErrInvalidJoinAttempt = errors.New("invalid media join attempt")
	ErrJoinAttemptStale   = errors.New("media join attempt authority is stale")
)

type CreateJoinAttemptInput struct {
	JoinAttemptID          uuid.UUID
	ExpectedRoomInstanceID uuid.UUID
	ExpectedSpaceVersion   int64
}

type JoinAttempt struct {
	ParticipantSessionID       uuid.UUID         `json:"participant_session_id"`
	RoomInstanceID             uuid.UUID         `json:"room_instance_id"`
	AdmissionRequestID         *uuid.UUID        `json:"admission_request_id,omitempty"`
	AdmissionVersion           *int64            `json:"admission_version,omitempty"`
	JoinAttemptID              uuid.UUID         `json:"join_attempt_id"`
	Status                     JoinAttemptStatus `json:"status"`
	Version                    int64             `json:"version"`
	InstanceRole               InstanceRole      `json:"instance_role"`
	CanPublishCameraMicrophone bool              `json:"can_publish_camera_microphone"`
	CanShareScreen             bool              `json:"can_share_screen"`
	CanSubscribe               bool              `json:"can_subscribe"`
	CreatedAt                  time.Time         `json:"created_at"`
	UpdatedAt                  time.Time         `json:"updated_at"`
	ExpiresAt                  *time.Time        `json:"expires_at,omitempty"`
}

type CreateJoinAttemptResult struct {
	Attempt JoinAttempt
	Created bool
}

type JoinAttemptRepository interface {
	CreateOrReuseJoinAttempt(
		context.Context,
		AccessContext,
		uuid.UUID,
		CreateJoinAttemptInput,
		time.Time,
	) (CreateJoinAttemptResult, error)
}

type JoinAttemptServiceAPI interface {
	CreateJoinAttempt(
		context.Context,
		AccessContext,
		uuid.UUID,
		CreateJoinAttemptInput,
	) (CreateJoinAttemptResult, error)
}

type JoinAttemptService struct {
	repository JoinAttemptRepository
	clock      func() time.Time
}

func NewJoinAttemptService(
	repository JoinAttemptRepository,
	clock func() time.Time,
) (*JoinAttemptService, error) {
	if repository == nil {
		return nil, fmt.Errorf("media join-attempt repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &JoinAttemptService{repository: repository, clock: clock}, nil
}

func (service *JoinAttemptService) CreateJoinAttempt(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input CreateJoinAttemptInput,
) (CreateJoinAttemptResult, error) {
	if service == nil || service.repository == nil {
		return CreateJoinAttemptResult{}, ErrLifecycleUnavailable
	}
	if spaceID == uuid.Nil || input.JoinAttemptID == uuid.Nil ||
		input.ExpectedRoomInstanceID == uuid.Nil || input.ExpectedSpaceVersion < 1 ||
		access.TenantID == uuid.Nil || access.ActorID == uuid.Nil || access.SessionID == uuid.Nil {
		return CreateJoinAttemptResult{}, ErrInvalidJoinAttempt
	}
	result, err := service.repository.CreateOrReuseJoinAttempt(
		ctx, access, spaceID, input, service.clock().UTC(),
	)
	if err != nil {
		return CreateJoinAttemptResult{}, normalizeJoinAttemptError(err)
	}
	if !validJoinAttemptResult(result, input) {
		return CreateJoinAttemptResult{}, ErrLifecycleUnavailable
	}
	return result, nil
}

func validJoinAttemptResult(
	result CreateJoinAttemptResult,
	input CreateJoinAttemptInput,
) bool {
	attempt := result.Attempt
	if attempt.ParticipantSessionID == uuid.Nil ||
		attempt.RoomInstanceID != input.ExpectedRoomInstanceID ||
		attempt.JoinAttemptID != input.JoinAttemptID || attempt.Version < 1 ||
		!validInstanceRole(attempt.InstanceRole) || attempt.CreatedAt.IsZero() ||
		attempt.UpdatedAt.Before(attempt.CreatedAt) {
		return false
	}
	switch attempt.Status {
	case JoinAttemptWaiting:
		return attempt.AdmissionRequestID != nil && *attempt.AdmissionRequestID != uuid.Nil &&
			attempt.AdmissionVersion != nil && *attempt.AdmissionVersion > 0 &&
			attempt.ExpiresAt != nil && !attempt.ExpiresAt.IsZero()
	case JoinAttemptAdmitted, JoinAttemptJoining:
		return true
	case JoinAttemptDenied, JoinAttemptCancelled, JoinAttemptMeetingEnded,
		JoinAttemptTimeout, JoinAttemptProviderUnavailable:
		return attempt.AdmissionRequestID != nil && *attempt.AdmissionRequestID != uuid.Nil &&
			attempt.AdmissionVersion != nil && *attempt.AdmissionVersion > 0
	default:
		return false
	}
}

func normalizeJoinAttemptError(err error) error {
	if err == nil {
		return nil
	}
	known := []error{
		ErrInvalidJoinAttempt, ErrJoinAttemptStale, ErrRoomNotOpen, ErrRoomLocked,
		ErrParticipantConflict, ErrSpaceAccessDenied, ErrSpaceNotFound,
		ErrSourceUnavailable, ErrLifecycleUnavailable,
		featurecontrol.ErrInvalidControl, featurecontrol.ErrAccessDenied,
		featurecontrol.ErrTenantNotFound, featurecontrol.ErrFeatureDisabled,
		featurecontrol.ErrQuotaExceeded, featurecontrol.ErrUnavailable,
	}
	for _, candidate := range known {
		if errors.Is(err, candidate) {
			return err
		}
	}
	return ErrLifecycleUnavailable
}
