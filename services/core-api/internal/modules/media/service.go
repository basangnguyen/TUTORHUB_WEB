package media

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	maximumErrorCodeLength = 64
)

var safeEventCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
var safeWebhookEventIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

var validClientEventStages = map[string]struct{}{
	"token":        {},
	"connect":      {},
	"connected":    {},
	"media":        {},
	"reconnecting": {},
	"reconnected":  {},
	"disconnected": {},
	"leave":        {},
}

var validClientEventOutcomes = map[string]struct{}{
	"started":   {},
	"succeeded": {},
	"failed":    {},
}

type TokenIssuer interface {
	Issue(TokenGrant) (string, error)
}

type EventSink interface {
	RecordClientEvent(context.Context, ClientEvent)
}

type WebhookReceiptRepository interface {
	RecordWebhookReceipt(context.Context, WebhookReceipt) (bool, error)
}

type ClassReader interface {
	Get(context.Context, classroom.AccessContext, uuid.UUID) (classroom.Class, error)
}

type ServiceAPI interface {
	IssueJoinCredential(context.Context, AccessContext, uuid.UUID) (JoinCredential, error)
	RecordClientEvent(context.Context, AccessContext, uuid.UUID, ClientEventInput) error
	RecordWebhook(context.Context, WebhookEvent) (WebhookResult, error)
}

type ServiceConfig struct {
	ServerURL string
	TokenTTL  time.Duration
	Clock     func() time.Time
	NewID     func() uuid.UUID
}

type Service struct {
	classes           ClassReader
	authorizer        policy.Authorizer
	issuer            TokenIssuer
	events            EventSink
	webhookRepository WebhookReceiptRepository
	serverURL         string
	tokenTTL          time.Duration
	clock             func() time.Time
	newID             func() uuid.UUID
}

func NewService(
	classes ClassReader,
	authorizer policy.Authorizer,
	issuer TokenIssuer,
	events EventSink,
	webhookRepository WebhookReceiptRepository,
	config ServiceConfig,
) (*Service, error) {
	if classes == nil || authorizer == nil {
		return nil, fmt.Errorf("classroom service and policy authorizer are required")
	}
	if issuer == nil {
		return nil, fmt.Errorf("LiveKit token issuer is required")
	}
	if strings.TrimSpace(config.ServerURL) == "" {
		return nil, fmt.Errorf("LiveKit server URL is required")
	}
	if config.TokenTTL < time.Minute || config.TokenTTL > 15*time.Minute {
		return nil, fmt.Errorf("LiveKit token TTL must be between 1m and 15m")
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.NewID == nil {
		config.NewID = uuid.New
	}

	return &Service{
		classes:           classes,
		authorizer:        authorizer,
		issuer:            issuer,
		events:            events,
		webhookRepository: webhookRepository,
		serverURL:         strings.TrimRight(config.ServerURL, "/"),
		tokenTTL:          config.TokenTTL,
		clock:             config.Clock,
		newID:             config.NewID,
	}, nil
}

func (service *Service) IssueJoinCredential(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) (JoinCredential, error) {
	class, err := service.authorizedClass(ctx, access, classID)
	if err != nil {
		return JoinCredential{}, err
	}
	if class.Status != classroom.ClassStatusActive {
		return JoinCredential{}, ErrClassUnavailable
	}

	roomName := RoomName(access.TenantID, classID)
	participantIdentity := ParticipantIdentity(access.ActorID, access.SessionID)
	canPublish := service.authorize(
		access,
		policy.ActionMediaPublish,
		classID,
		policy.ResourceState(class.Status),
	).Allowed
	grant := TokenGrant{
		RoomName:            roomName,
		ParticipantIdentity: participantIdentity,
		ParticipantName:     strings.TrimSpace(access.DisplayName),
		Role:                strings.TrimSpace(access.Role),
		CanPublish:          canPublish,
		CanPublishData:      false,
		CanSubscribe:        true,
		ValidFor:            service.tokenTTL,
	}
	token, err := service.issuer.Issue(grant)
	if err != nil {
		return JoinCredential{}, fmt.Errorf("issue LiveKit token: %w", err)
	}
	now := service.clock().UTC()

	return JoinCredential{
		AccessToken:         token,
		ServerURL:           service.serverURL,
		RoomName:            roomName,
		ParticipantIdentity: participantIdentity,
		ParticipantName:     grant.ParticipantName,
		AttemptID:           service.newID(),
		CanPublish:          canPublish,
		ExpiresAt:           now.Add(service.tokenTTL),
	}, nil
}

func (service *Service) RecordClientEvent(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	input ClientEventInput,
) error {
	if err := validateClientEvent(input); err != nil {
		return err
	}
	if _, err := service.authorizedClass(ctx, access, classID); err != nil {
		return err
	}
	if service.events != nil {
		service.events.RecordClientEvent(ctx, ClientEvent{
			TenantID:   access.TenantID,
			ClassID:    classID,
			ActorID:    access.ActorID,
			AttemptID:  input.AttemptID,
			Stage:      input.Stage,
			Outcome:    input.Outcome,
			ErrorCode:  input.ErrorCode,
			DurationMS: input.DurationMS,
			RecordedAt: service.clock().UTC(),
		})
	}

	return nil
}

func (service *Service) RecordWebhook(
	ctx context.Context,
	event WebhookEvent,
) (WebhookResult, error) {
	if service.webhookRepository == nil {
		return WebhookResult{}, ErrUnavailable
	}
	event.ID = strings.TrimSpace(event.ID)
	if !safeWebhookEventIDPattern.MatchString(event.ID) ||
		strings.TrimSpace(event.EventType) == "" || event.OccurredAt.IsZero() {
		return WebhookResult{}, ErrInvalidWebhook
	}
	tenantID, classID, ok := ParseRoomName(event.RoomName)
	if !ok {
		return WebhookResult{Ignored: true}, nil
	}
	recorded, err := service.webhookRepository.RecordWebhookReceipt(ctx, WebhookReceipt{
		EventID:             event.ID,
		EventType:           strings.TrimSpace(event.EventType),
		TenantID:            tenantID,
		ClassID:             classID,
		RoomName:            event.RoomName,
		ParticipantIdentity: strings.TrimSpace(event.ParticipantIdentity),
		OccurredAt:          event.OccurredAt.UTC(),
		ReceivedAt:          service.clock().UTC(),
	})
	if err != nil {
		return WebhookResult{}, fmt.Errorf("record LiveKit webhook: %w", err)
	}

	return WebhookResult{Recorded: recorded, Duplicate: !recorded}, nil
}

func (service *Service) authorizedClass(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
) (classroom.Class, error) {
	if access.SessionID == uuid.Nil || classID == uuid.Nil {
		return classroom.Class{}, ErrAccessDenied
	}
	preflight := service.authorize(
		access, policy.ActionSessionJoin, classID, policy.ResourceStateUnknown,
	)
	if !preflight.Allowed {
		if preflight.ConcealResource {
			return classroom.Class{}, classroom.ErrClassNotFound
		}
		return classroom.Class{}, ErrAccessDenied
	}
	class, err := service.classes.Get(ctx, classroom.AccessContext{
		TenantID: access.TenantID, ActorID: access.ActorID,
		MembershipActive:  access.MembershipActive,
		OrganizationRoles: append([]policy.OrganizationRole(nil), access.OrganizationRoles...),
		ClassRoles:        append([]policy.ClassRole(nil), access.ClassRoles...),
	}, classID)
	if err != nil {
		switch {
		case errors.Is(err, classroom.ErrClassNotFound):
			return classroom.Class{}, classroom.ErrClassNotFound
		case errors.Is(err, classroom.ErrClassAccessDenied):
			return classroom.Class{}, ErrAccessDenied
		default:
			return classroom.Class{}, fmt.Errorf("authorize media class: %w", err)
		}
	}
	decision := service.authorize(
		access, policy.ActionSessionJoin, classID, policy.ResourceState(class.Status),
	)
	if !decision.Allowed {
		if decision.Reason == policy.DenialResourceState {
			return classroom.Class{}, ErrClassUnavailable
		}
		if decision.ConcealResource {
			return classroom.Class{}, classroom.ErrClassNotFound
		}
		return classroom.Class{}, ErrAccessDenied
	}

	return class, nil
}

func (service *Service) authorize(
	access AccessContext,
	action policy.Action,
	classID uuid.UUID,
	state policy.ResourceState,
) policy.Decision {
	return service.authorizer.Authorize(policy.Input{
		Subject: policy.Subject{
			ActorID: access.ActorID, ActiveTenantID: access.TenantID,
			MembershipActive:  access.MembershipActive,
			OrganizationRoles: append([]policy.OrganizationRole(nil), access.OrganizationRoles...),
			ClassRoles:        append([]policy.ClassRole(nil), access.ClassRoles...),
		},
		Action:   action,
		Resource: policy.Resource{TenantID: access.TenantID, ClassID: classID, State: state},
	})
}

func validateClientEvent(input ClientEventInput) error {
	if input.AttemptID == uuid.Nil {
		return fmt.Errorf("%w: attempt ID is required", ErrInvalidRequest)
	}
	if _, ok := validClientEventStages[input.Stage]; !ok {
		return fmt.Errorf("%w: unsupported telemetry stage", ErrInvalidRequest)
	}
	if _, ok := validClientEventOutcomes[input.Outcome]; !ok {
		return fmt.Errorf("%w: unsupported telemetry outcome", ErrInvalidRequest)
	}
	if input.DurationMS < 0 || input.DurationMS > int64((10*time.Minute)/time.Millisecond) {
		return fmt.Errorf("%w: telemetry duration is outside the accepted range", ErrInvalidRequest)
	}
	if input.ErrorCode != "" &&
		(len(input.ErrorCode) > maximumErrorCodeLength || !safeEventCodePattern.MatchString(input.ErrorCode)) {
		return fmt.Errorf("%w: telemetry error code is invalid", ErrInvalidRequest)
	}

	return nil
}

func RoomName(tenantID uuid.UUID, classID uuid.UUID) string {
	return "th_" + tenantID.String() + "_" + classID.String()
}

func ParseRoomName(roomName string) (uuid.UUID, uuid.UUID, bool) {
	parts := strings.Split(roomName, "_")
	if len(parts) != 3 || parts[0] != "th" {
		return uuid.Nil, uuid.Nil, false
	}
	tenantID, tenantErr := uuid.Parse(parts[1])
	classID, classErr := uuid.Parse(parts[2])
	if tenantErr != nil || classErr != nil || tenantID == uuid.Nil || classID == uuid.Nil {
		return uuid.Nil, uuid.Nil, false
	}

	return tenantID, classID, true
}

func ParticipantIdentity(actorID uuid.UUID, sessionID uuid.UUID) string {
	return "u_" + actorID.String() + "_s_" + sessionID.String()
}
