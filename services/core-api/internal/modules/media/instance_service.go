package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	defaultInstanceCredentialTTL = 5 * time.Minute
)

type InstanceRole string

const (
	InstanceRoleHost              InstanceRole = "host"
	InstanceRoleCoHost            InstanceRole = "co_host"
	InstanceRoleTeachingAssistant InstanceRole = InstanceRole(policy.ClassRoleTeachingAssistant)
	InstanceRoleAttendee          InstanceRole = "attendee"
)

var (
	ErrInvalidCredentialRequest = errors.New("invalid media credential request")
	ErrRoomNotOpen              = errors.New("media room is not open")
	ErrRoomLocked               = errors.New("media room is locked")
	ErrAdmissionRequired        = errors.New("media room admission is required")
	ErrParticipantConflict      = errors.New("media participant session conflict")
	ErrLegacyMediaDisabled      = errors.New("legacy classroom media is disabled")
)

type CredentialRateLimitError struct {
	RetryAfter time.Duration
}

func (err *CredentialRateLimitError) Error() string {
	return "media credential rate limit exceeded"
}

type IssueInstanceCredentialInput struct {
	JoinAttemptID uuid.UUID
}

type InstanceCredential struct {
	AccessToken                string       `json:"access_token"`
	ServerURL                  string       `json:"server_url"`
	ParticipantSessionID       uuid.UUID    `json:"participant_session_id"`
	RoomInstanceID             uuid.UUID    `json:"room_instance_id"`
	JoinAttemptID              uuid.UUID    `json:"join_attempt_id"`
	InstanceRole               InstanceRole `json:"instance_role"`
	CanPublishCameraMicrophone bool         `json:"can_publish_camera_microphone"`
	CanShareScreen             bool         `json:"can_share_screen"`
	CanSubscribe               bool         `json:"can_subscribe"`
	ExpiresAt                  time.Time    `json:"expires_at"`
}

type ParticipantCredentialGrant struct {
	ParticipantSessionID        uuid.UUID
	ParticipantKey              uuid.UUID
	RoomInstanceID              uuid.UUID
	JoinAttemptID               uuid.UUID
	ProviderRoomName            string
	ProviderParticipantIdentity string
	ParticipantName             string
	InstanceRole                InstanceRole
	CanPublishCameraMicrophone  bool
	CanShareScreen              bool
	CanSubscribe                bool
}

type InstanceCredentialRepository interface {
	PrepareCredential(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		time.Time,
	) (ParticipantCredentialGrant, error)
}

type InstanceCredentialServiceAPI interface {
	IssueInstanceCredential(
		context.Context,
		AccessContext,
		uuid.UUID,
		IssueInstanceCredentialInput,
	) (InstanceCredential, error)
}

type InstanceCredentialService struct {
	repository InstanceCredentialRepository
	issuer     TokenIssuer
	serverURL  string
	tokenTTL   time.Duration
	clock      func() time.Time
}

func NewInstanceCredentialService(
	repository InstanceCredentialRepository,
	issuer TokenIssuer,
	config ServiceConfig,
) (*InstanceCredentialService, error) {
	if repository == nil || issuer == nil {
		return nil, fmt.Errorf("media credential repository and token issuer are required")
	}
	serverURL := strings.TrimRight(strings.TrimSpace(config.ServerURL), "/")
	if serverURL == "" {
		return nil, fmt.Errorf("LiveKit server URL is required")
	}
	if config.TokenTTL == 0 {
		config.TokenTTL = defaultInstanceCredentialTTL
	}
	if config.TokenTTL < time.Minute || config.TokenTTL > 15*time.Minute {
		return nil, fmt.Errorf("LiveKit token TTL must be between 1m and 15m")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &InstanceCredentialService{
		repository: repository,
		issuer:     issuer,
		serverURL:  serverURL,
		tokenTTL:   config.TokenTTL,
		clock:      config.Clock,
	}, nil
}

func (service *InstanceCredentialService) IssueInstanceCredential(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input IssueInstanceCredentialInput,
) (InstanceCredential, error) {
	if service == nil || service.repository == nil || service.issuer == nil {
		return InstanceCredential{}, ErrLifecycleUnavailable
	}
	if spaceID == uuid.Nil || input.JoinAttemptID == uuid.Nil || access.TenantID == uuid.Nil ||
		access.ActorID == uuid.Nil || access.SessionID == uuid.Nil {
		return InstanceCredential{}, ErrInvalidCredentialRequest
	}
	now := service.clock().UTC()
	grant, err := service.repository.PrepareCredential(
		ctx, access, spaceID, input.JoinAttemptID, now,
	)
	if err != nil {
		return InstanceCredential{}, normalizeInstanceCredentialError(err)
	}
	if grant.ParticipantSessionID == uuid.Nil || grant.RoomInstanceID == uuid.Nil ||
		grant.ParticipantKey == uuid.Nil ||
		grant.JoinAttemptID != input.JoinAttemptID ||
		strings.TrimSpace(grant.ProviderRoomName) == "" ||
		strings.TrimSpace(grant.ProviderParticipantIdentity) == "" ||
		!validInstanceRole(grant.InstanceRole) {
		return InstanceCredential{}, ErrLifecycleUnavailable
	}
	token, err := service.issuer.Issue(TokenGrant{
		RoomName:                   grant.ProviderRoomName,
		ParticipantIdentity:        grant.ProviderParticipantIdentity,
		ParticipantName:            strings.TrimSpace(grant.ParticipantName),
		ParticipantKey:             grant.ParticipantKey,
		Role:                       string(grant.InstanceRole),
		CanPublishCameraMicrophone: grant.CanPublishCameraMicrophone,
		CanShareScreen:             grant.CanShareScreen,
		CanPublishData:             false,
		CanSubscribe:               grant.CanSubscribe,
		ValidFor:                   service.tokenTTL,
	})
	if err != nil {
		return InstanceCredential{}, fmt.Errorf("%w: issue instance credential", ErrMediaProviderUnavailable)
	}
	return InstanceCredential{
		AccessToken:                token,
		ServerURL:                  service.serverURL,
		ParticipantSessionID:       grant.ParticipantSessionID,
		RoomInstanceID:             grant.RoomInstanceID,
		JoinAttemptID:              grant.JoinAttemptID,
		InstanceRole:               grant.InstanceRole,
		CanPublishCameraMicrophone: grant.CanPublishCameraMicrophone,
		CanShareScreen:             grant.CanShareScreen,
		CanSubscribe:               grant.CanSubscribe,
		ExpiresAt:                  now.Add(service.tokenTTL),
	}, nil
}

func validInstanceRole(role InstanceRole) bool {
	switch role {
	case InstanceRoleHost, InstanceRoleCoHost, InstanceRoleTeachingAssistant, InstanceRoleAttendee:
		return true
	default:
		return false
	}
}

func normalizeInstanceCredentialError(err error) error {
	if err == nil {
		return nil
	}
	known := []error{
		ErrInvalidCredentialRequest, ErrRoomNotOpen, ErrRoomLocked,
		ErrAdmissionRequired, ErrParticipantConflict, ErrSpaceAccessDenied,
		ErrSpaceNotFound, ErrSourceUnavailable, ErrLifecycleUnavailable,
		ErrMediaProviderUnavailable,
		featurecontrol.ErrInvalidControl, featurecontrol.ErrAccessDenied,
		featurecontrol.ErrTenantNotFound, featurecontrol.ErrFeatureDisabled,
		featurecontrol.ErrQuotaExceeded, featurecontrol.ErrUnavailable,
	}
	for _, candidate := range known {
		if errors.Is(err, candidate) {
			return err
		}
	}
	var rateLimit *CredentialRateLimitError
	if errors.As(err, &rateLimit) {
		return err
	}
	return ErrLifecycleUnavailable
}

type ProviderWebhookRepository interface {
	RecordProviderWebhook(context.Context, WebhookEvent, time.Time) (WebhookResult, bool, error)
	AllowLegacyMedia(context.Context, uuid.UUID) (bool, error)
}

type WebhookProcessor interface {
	RecordWebhook(context.Context, WebhookEvent) (WebhookResult, error)
}

type ProviderWebhookService struct {
	repository ProviderWebhookRepository
	legacy     ServiceAPI
	clock      func() time.Time
}

func NewProviderWebhookService(
	repository ProviderWebhookRepository,
	legacy ServiceAPI,
	clock func() time.Time,
) (*ProviderWebhookService, error) {
	if repository == nil {
		return nil, fmt.Errorf("provider webhook repository is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &ProviderWebhookService{repository: repository, legacy: legacy, clock: clock}, nil
}

func (service *ProviderWebhookService) RecordWebhook(
	ctx context.Context,
	event WebhookEvent,
) (WebhookResult, error) {
	if service == nil || service.repository == nil {
		return WebhookResult{}, ErrUnavailable
	}
	if err := validateProviderWebhookEvent(event); err != nil {
		return WebhookResult{}, err
	}
	result, handled, err := service.repository.RecordProviderWebhook(
		ctx, event, service.clock().UTC(),
	)
	if err != nil {
		// Repository/provider details can include driver text or opaque binding
		// identifiers. The webhook boundary exposes only the typed availability
		// failure and never forwards the raw cause to HTTP logging.
		return WebhookResult{}, ErrLifecycleUnavailable
	}
	if handled {
		return result, nil
	}
	if service.legacy == nil {
		return WebhookResult{Ignored: true}, nil
	}
	tenantID, _, legacyRoom := ParseRoomName(event.RoomName)
	if !legacyRoom {
		return WebhookResult{Ignored: true}, nil
	}
	allowed, err := service.repository.AllowLegacyMedia(ctx, tenantID)
	if err != nil {
		return WebhookResult{}, ErrUnavailable
	}
	if !allowed {
		return WebhookResult{Ignored: true}, nil
	}
	return service.legacy.RecordWebhook(ctx, event)
}

func validateProviderWebhookEvent(event WebhookEvent) error {
	eventType := strings.TrimSpace(event.EventType)
	roomName, roomSID := strings.TrimSpace(event.RoomName), strings.TrimSpace(event.RoomSID)
	participantIdentity := strings.TrimSpace(event.ParticipantIdentity)
	participantSID := strings.TrimSpace(event.ParticipantSID)
	if !safeWebhookEventIDPattern.MatchString(strings.TrimSpace(event.ID)) ||
		!validProviderWebhookEventType(eventType) || event.OccurredAt.IsZero() ||
		(roomName == "" && roomSID == "") ||
		(roomName != "" && !validProviderIdentifier(roomName, 128)) ||
		(roomSID != "" && !validProviderIdentifier(roomSID, 255)) ||
		(participantIdentity != "" && !validProviderIdentifier(participantIdentity, 128)) ||
		(participantSID != "" && !validProviderIdentifier(participantSID, 255)) {
		return ErrInvalidWebhook
	}
	return nil
}

func validProviderWebhookEventType(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

type LegacyAuthorityGate interface {
	AllowLegacyMedia(context.Context, uuid.UUID) (bool, error)
}

type LegacyCompatibleService struct {
	legacy ServiceAPI
	gate   LegacyAuthorityGate
}

func NewLegacyCompatibleService(legacy ServiceAPI, gate LegacyAuthorityGate) (*LegacyCompatibleService, error) {
	if legacy == nil || gate == nil {
		return nil, fmt.Errorf("legacy media service and authority gate are required")
	}
	return &LegacyCompatibleService{legacy: legacy, gate: gate}, nil
}

func (service *LegacyCompatibleService) IssueJoinCredential(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) (JoinCredential, error) {
	allowed, err := service.allow(ctx, access.TenantID)
	if err != nil {
		return JoinCredential{}, err
	}
	if !allowed {
		return JoinCredential{}, ErrLegacyMediaDisabled
	}
	return service.legacy.IssueJoinCredential(ctx, access, classID)
}

func (service *LegacyCompatibleService) RecordClientEvent(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input ClientEventInput,
) error {
	allowed, err := service.allow(ctx, access.TenantID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrLegacyMediaDisabled
	}
	return service.legacy.RecordClientEvent(ctx, access, classID, input)
}

func (service *LegacyCompatibleService) RecordWebhook(
	ctx context.Context,
	event WebhookEvent,
) (WebhookResult, error) {
	tenantID, _, ok := ParseRoomName(event.RoomName)
	if !ok {
		return WebhookResult{Ignored: true}, nil
	}
	allowed, err := service.allow(ctx, tenantID)
	if err != nil {
		return WebhookResult{}, err
	}
	if !allowed {
		return WebhookResult{Ignored: true}, nil
	}
	return service.legacy.RecordWebhook(ctx, event)
}

func (service *LegacyCompatibleService) allow(ctx context.Context, tenantID uuid.UUID) (bool, error) {
	if service == nil || service.legacy == nil || service.gate == nil || tenantID == uuid.Nil {
		return false, ErrUnavailable
	}
	allowed, err := service.gate.AllowLegacyMedia(ctx, tenantID)
	if err != nil {
		return false, ErrUnavailable
	}
	return allowed, nil
}
