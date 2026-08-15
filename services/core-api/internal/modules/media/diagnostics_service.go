package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	maximumDiagnosticDuration = 10 * time.Minute
	maximumDiagnosticRange    = 31 * 24 * time.Hour
	maximumDiagnosticLimit    = 1000
)

var (
	ErrInvalidDiagnosticRequest = errors.New("invalid media diagnostic request")
	ErrDiagnosticForbidden      = errors.New("media diagnostics access denied")
	ErrDiagnosticUnavailable    = errors.New("media diagnostics unavailable")
)

type DiagnosticStage string
type DiagnosticOutcome string
type DiagnosticErrorCode string
type DiagnosticNetworkQuality string
type DiagnosticMediaPath string

const (
	DiagnosticStageJoinAttempt  DiagnosticStage = "join_attempt"
	DiagnosticStageCredential   DiagnosticStage = "credential"
	DiagnosticStageConnect      DiagnosticStage = "connect"
	DiagnosticStageMedia        DiagnosticStage = "media"
	DiagnosticStageReconnecting DiagnosticStage = "reconnecting"
	DiagnosticStageReconnected  DiagnosticStage = "reconnected"
	DiagnosticStageDisconnected DiagnosticStage = "disconnected"
	DiagnosticStageLeave        DiagnosticStage = "leave"

	DiagnosticOutcomeStarted   DiagnosticOutcome = "started"
	DiagnosticOutcomeSucceeded DiagnosticOutcome = "succeeded"
	DiagnosticOutcomeFailed    DiagnosticOutcome = "failed"
	DiagnosticOutcomeCancelled DiagnosticOutcome = "cancelled"

	DiagnosticErrorNone                  DiagnosticErrorCode = ""
	DiagnosticErrorParticipantRemoved    DiagnosticErrorCode = "participant_removed"
	DiagnosticErrorRoomEnded             DiagnosticErrorCode = "room_ended"
	DiagnosticErrorDuplicateIdentity     DiagnosticErrorCode = "duplicate_identity"
	DiagnosticErrorClientLeave           DiagnosticErrorCode = "client_leave"
	DiagnosticErrorTransportDisconnected DiagnosticErrorCode = "transport_disconnected"
	DiagnosticErrorProvider              DiagnosticErrorCode = "provider_error"

	DiagnosticNetworkUnknown  DiagnosticNetworkQuality = "unknown"
	DiagnosticNetworkGood     DiagnosticNetworkQuality = "good"
	DiagnosticNetworkDegraded DiagnosticNetworkQuality = "degraded"
	DiagnosticNetworkPoor     DiagnosticNetworkQuality = "poor"
	DiagnosticNetworkOffline  DiagnosticNetworkQuality = "offline"

	DiagnosticMediaUnknown    DiagnosticMediaPath = "unknown"
	DiagnosticMediaAudioVideo DiagnosticMediaPath = "audio_video"
	DiagnosticMediaAudioOnly  DiagnosticMediaPath = "audio_only"
	DiagnosticMediaListenOnly DiagnosticMediaPath = "listen_only"
)

type RecordDiagnosticInput struct {
	EventID        uuid.UUID
	RoomInstanceID uuid.UUID
	JoinAttemptID  uuid.UUID
	Stage          DiagnosticStage
	Outcome        DiagnosticOutcome
	ErrorCode      DiagnosticErrorCode
	NetworkQuality DiagnosticNetworkQuality
	MediaPath      DiagnosticMediaPath
	DurationMS     int
}

type DiagnosticExportFilter struct {
	From  time.Time
	To    time.Time
	Limit int
}

type DiagnosticExportItem struct {
	EventID        uuid.UUID                `json:"event_id"`
	SessionRef     string                   `json:"session_ref"`
	Stage          DiagnosticStage          `json:"stage"`
	Outcome        DiagnosticOutcome        `json:"outcome"`
	ErrorCode      DiagnosticErrorCode      `json:"error_code,omitempty"`
	NetworkQuality DiagnosticNetworkQuality `json:"network_quality"`
	MediaPath      DiagnosticMediaPath      `json:"media_path"`
	DurationMS     int                      `json:"duration_ms"`
	RecordedAt     time.Time                `json:"recorded_at"`
}

type DiagnosticMetrics struct {
	JoinAttempts       int     `json:"join_attempts"`
	SuccessfulJoins    int     `json:"successful_joins"`
	JoinSuccessRate    float64 `json:"join_success_rate"`
	P95TimeToMediaMS   int     `json:"p95_time_to_media_ms"`
	ReconnectSucceeded int     `json:"reconnect_succeeded"`
	ReconnectFailed    int     `json:"reconnect_failed"`
}

type DiagnosticExport struct {
	From      time.Time              `json:"from"`
	To        time.Time              `json:"to"`
	Items     []DiagnosticExportItem `json:"items"`
	Metrics   DiagnosticMetrics      `json:"metrics"`
	Truncated bool                   `json:"truncated"`
}

type DiagnosticRepository interface {
	RecordDiagnostic(context.Context, AccessContext, uuid.UUID, RecordDiagnosticInput, time.Time) error
	ExportDiagnostics(context.Context, AccessContext, DiagnosticExportFilter) (DiagnosticExport, error)
}

type DiagnosticServiceAPI interface {
	RecordDiagnostic(context.Context, AccessContext, uuid.UUID, RecordDiagnosticInput) error
	ExportDiagnostics(context.Context, AccessContext, DiagnosticExportFilter) (DiagnosticExport, error)
}

type DiagnosticService struct {
	repository DiagnosticRepository
	clock      func() time.Time
}

func NewDiagnosticService(repository DiagnosticRepository, clock func() time.Time) (*DiagnosticService, error) {
	if repository == nil {
		return nil, fmt.Errorf("media diagnostic repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &DiagnosticService{repository: repository, clock: clock}, nil
}

func (service *DiagnosticService) RecordDiagnostic(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input RecordDiagnosticInput,
) error {
	if service == nil || service.repository == nil {
		return ErrDiagnosticUnavailable
	}
	if err := validateRecordDiagnostic(access, spaceID, input); err != nil {
		return err
	}
	if err := service.repository.RecordDiagnostic(ctx, access, spaceID, input, service.clock().UTC()); err != nil {
		return normalizeDiagnosticError(err)
	}
	return nil
}

func (service *DiagnosticService) ExportDiagnostics(
	ctx context.Context,
	access AccessContext,
	filter DiagnosticExportFilter,
) (DiagnosticExport, error) {
	if service == nil || service.repository == nil {
		return DiagnosticExport{}, ErrDiagnosticUnavailable
	}
	filter.From = filter.From.UTC()
	filter.To = filter.To.UTC()
	if access.TenantID == uuid.Nil || access.ActorID == uuid.Nil || access.SessionID == uuid.Nil ||
		filter.From.IsZero() || filter.To.IsZero() || !filter.From.Before(filter.To) ||
		filter.To.Sub(filter.From) > maximumDiagnosticRange || filter.Limit < 1 ||
		filter.Limit > maximumDiagnosticLimit {
		return DiagnosticExport{}, ErrInvalidDiagnosticRequest
	}
	export, err := service.repository.ExportDiagnostics(ctx, access, filter)
	if err != nil {
		return DiagnosticExport{}, normalizeDiagnosticError(err)
	}
	if export.Items == nil {
		export.Items = []DiagnosticExportItem{}
	}
	return export, nil
}

func validateRecordDiagnostic(access AccessContext, spaceID uuid.UUID, input RecordDiagnosticInput) error {
	if access.TenantID == uuid.Nil || access.ActorID == uuid.Nil || access.SessionID == uuid.Nil ||
		spaceID == uuid.Nil || input.EventID == uuid.Nil || input.RoomInstanceID == uuid.Nil ||
		input.JoinAttemptID == uuid.Nil || input.DurationMS < 0 ||
		time.Duration(input.DurationMS)*time.Millisecond > maximumDiagnosticDuration ||
		!validDiagnosticStage(input.Stage) || !validDiagnosticOutcome(input.Outcome) ||
		!validDiagnosticNetwork(input.NetworkQuality) || !validDiagnosticMediaPath(input.MediaPath) ||
		!validDiagnosticErrorCode(input.ErrorCode) {
		return ErrInvalidDiagnosticRequest
	}
	return nil
}

func validDiagnosticErrorCode(value DiagnosticErrorCode) bool {
	switch value {
	case DiagnosticErrorNone, DiagnosticErrorParticipantRemoved, DiagnosticErrorRoomEnded,
		DiagnosticErrorDuplicateIdentity, DiagnosticErrorClientLeave,
		DiagnosticErrorTransportDisconnected, DiagnosticErrorProvider:
		return true
	default:
		return false
	}
}

func validDiagnosticStage(value DiagnosticStage) bool {
	switch value {
	case DiagnosticStageJoinAttempt, DiagnosticStageCredential, DiagnosticStageConnect,
		DiagnosticStageMedia, DiagnosticStageReconnecting, DiagnosticStageReconnected,
		DiagnosticStageDisconnected, DiagnosticStageLeave:
		return true
	default:
		return false
	}
}

func validDiagnosticOutcome(value DiagnosticOutcome) bool {
	switch value {
	case DiagnosticOutcomeStarted, DiagnosticOutcomeSucceeded, DiagnosticOutcomeFailed,
		DiagnosticOutcomeCancelled:
		return true
	default:
		return false
	}
}

func validDiagnosticNetwork(value DiagnosticNetworkQuality) bool {
	switch value {
	case DiagnosticNetworkUnknown, DiagnosticNetworkGood, DiagnosticNetworkDegraded,
		DiagnosticNetworkPoor, DiagnosticNetworkOffline:
		return true
	default:
		return false
	}
}

func validDiagnosticMediaPath(value DiagnosticMediaPath) bool {
	switch value {
	case DiagnosticMediaUnknown, DiagnosticMediaAudioVideo, DiagnosticMediaAudioOnly,
		DiagnosticMediaListenOnly:
		return true
	default:
		return false
	}
}

func diagnosticSessionRef(tenantID uuid.UUID, participantSessionID uuid.UUID) string {
	digest := sha256.Sum256([]byte(
		"tutorhub-media-diagnostic:" + tenantID.String() + ":" + participantSessionID.String(),
	))
	return fmt.Sprintf("ps_%x", digest[:12])
}

func normalizeDiagnosticError(err error) error {
	for _, known := range []error{
		ErrInvalidDiagnosticRequest, ErrDiagnosticForbidden, ErrDiagnosticUnavailable,
		ErrSpaceNotFound, ErrSpaceAccessDenied,
	} {
		if errors.Is(err, known) {
			return err
		}
	}
	return ErrDiagnosticUnavailable
}
