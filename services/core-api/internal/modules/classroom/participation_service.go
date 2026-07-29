package classroom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

var (
	// ErrSessionParticipationNotFound intentionally covers an unavailable source
	// or attendee. Callers must not use it to distinguish another user's
	// participation from a source the caller cannot access.
	ErrSessionParticipationNotFound            = errors.New("class session participation not found")
	ErrSessionParticipationAccessDenied        = errors.New("class session participation access denied")
	ErrSessionAudienceVersionConflict          = errors.New("class session audience revision is stale")
	ErrSessionAttendeeVersionConflict          = errors.New("class session attendee version is stale")
	ErrSessionParticipationIdempotencyConflict = errors.New("class session participation idempotency key conflicts")
	ErrSessionRSVPUnavailable                  = errors.New("class session RSVP is unavailable")
	ErrSessionParticipationUnavailable         = errors.New("class session participation is unavailable")
)

// SessionAudience is the server-side, current participation authority for one
// scheduled source. It deliberately contains no email, recipient ciphertext,
// delivery fingerprint, invitation capability, or delivery state.
type SessionAudience struct {
	Source            ParticipationSourceRef
	AudienceRevision  int64
	ResponseRequested bool
	SourceStatus      SessionStatus
	Attendees         []SessionAudienceAttendee
	ViewerAccess      SessionAudienceViewerAccess
}

type SessionAudienceViewerAccess struct {
	CanManageAttendees bool
	CanRespond         bool
	CanSeeGuestList    bool
}

// SessionAudienceAttendee is a minimum-authority projection. UserID is safe
// to return only after the service has applied the viewer projection below;
// it is never a delivery address or an external recipient identity.
type SessionAudienceAttendee struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	ParticipationRole ParticipationRole
	BusinessRole      string
	RSVPState         RSVPState
	RespondedAt       *time.Time
	Version           int64
	IsSelf            bool
	ResponseRequested bool
	ResponseClosedAt  *time.Time
	CanSeeGuestList   bool
}

type SessionAudienceMutationResult struct {
	Audience SessionAudience
	Replayed bool
}

type SelfRSVPMutationResult struct {
	Attendee SessionAudienceAttendee
	Replayed bool
}

// SessionParticipationRepository is intentionally source-scoped. The caller
// supplies no attendee target for RSVP; the repository derives it from the
// authenticated tenant context inside its transaction.
type SessionParticipationRepository interface {
	GetSessionAudience(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
	) (SessionAudience, error)
	ReplaceSessionAudience(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		AudienceReplacementParams,
		time.Time,
	) (SessionAudienceMutationResult, error)
	RespondToSession(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		uuid.UUID,
		SelfRSVPParams,
		time.Time,
	) (SelfRSVPMutationResult, error)
}

// ParticipationSourceRepository is the typed recurring-aware authority. The
// legacy session methods remain available as compatibility wrappers while
// transports migrate to this contract.
type ParticipationSourceRepository interface {
	GetParticipationAudience(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		ParticipationSourceRef,
	) (SessionAudience, error)
	ReplaceParticipationAudience(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		ParticipationSourceRef,
		AudienceReplacementParams,
		time.Time,
	) (SessionAudienceMutationResult, error)
	RespondToParticipationSource(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		ParticipationSourceRef,
		SelfRSVPParams,
		time.Time,
	) (SelfRSVPMutationResult, error)
}

type SessionParticipationServiceAPI interface {
	GetSessionAudience(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
	) (SessionAudience, error)
	ReplaceSessionAudience(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		ReplaceAudienceInput,
	) (SessionAudienceMutationResult, error)
	RespondToSession(
		context.Context,
		AccessContext,
		uuid.UUID,
		uuid.UUID,
		SelfRSVPInput,
	) (SelfRSVPMutationResult, error)
}

type ParticipationSourceServiceAPI interface {
	SessionParticipationServiceAPI
	GetAudience(
		context.Context,
		AccessContext,
		uuid.UUID,
		ParticipationSourceRef,
	) (SessionAudience, error)
	ReplaceAudience(
		context.Context,
		AccessContext,
		uuid.UUID,
		ParticipationSourceRef,
		ReplaceAudienceInput,
	) (SessionAudienceMutationResult, error)
	Respond(
		context.Context,
		AccessContext,
		uuid.UUID,
		ParticipationSourceRef,
		SelfRSVPInput,
	) (SelfRSVPMutationResult, error)
}

type SessionParticipationServiceConfig struct {
	Clock func() time.Time
}

// SessionParticipationService owns request normalization, class policy
// checks, and the privacy projection of the current attendance authority.
// The PostgreSQL repository repeats mutation authorization under row locks and
// owns the atomic snapshot/write boundary.
type SessionParticipationService struct {
	repository       SessionParticipationRepository
	sourceRepository ParticipationSourceRepository
	classAuthorizer  ClassActionAuthorizer
	clock            func() time.Time
}

func NewSessionParticipationService(
	repository SessionParticipationRepository,
	classAuthorizer ClassActionAuthorizer,
	configurations ...SessionParticipationServiceConfig,
) (*SessionParticipationService, error) {
	if repository == nil || classAuthorizer == nil {
		return nil, fmt.Errorf("session participation repository and class authorizer are required")
	}
	if len(configurations) > 1 {
		return nil, fmt.Errorf("only one session participation configuration is supported")
	}
	config := SessionParticipationServiceConfig{}
	if len(configurations) == 1 {
		config = configurations[0]
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &SessionParticipationService{
		repository: repository,
		sourceRepository: func() ParticipationSourceRepository {
			value, _ := repository.(ParticipationSourceRepository)
			return value
		}(),
		classAuthorizer: classAuthorizer,
		clock:           config.Clock,
	}, nil
}

func (service *SessionParticipationService) GetSessionAudience(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (SessionAudience, error) {
	if service.sourceRepository != nil {
		return service.GetAudience(
			ctx,
			access,
			classID,
			SessionParticipationSource(sessionID),
		)
	}
	class, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionClassView)
	if err != nil {
		return SessionAudience{}, err
	}
	if sessionID == uuid.Nil {
		return SessionAudience{}, ErrSessionParticipationNotFound
	}
	audience, err := service.repository.GetSessionAudience(
		ctx, tenantContext, classID, sessionID,
	)
	if err != nil {
		return SessionAudience{}, err
	}
	return projectSessionAudience(audience, class, access.ActorID), nil
}

func (service *SessionParticipationService) ReplaceSessionAudience(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
	input ReplaceAudienceInput,
) (SessionAudienceMutationResult, error) {
	if service.sourceRepository != nil {
		return service.ReplaceAudience(
			ctx,
			access,
			classID,
			SessionParticipationSource(sessionID),
			input,
		)
	}
	class, tenantContext, err := service.authorize(
		ctx, access, classID, policy.ActionSessionSchedule,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if sessionID == uuid.Nil {
		return SessionAudienceMutationResult{}, ErrSessionParticipationNotFound
	}
	params, err := input.normalized()
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	result, err := service.repository.ReplaceSessionAudience(
		ctx, tenantContext, classID, sessionID, params, service.clock().UTC(),
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	result.Audience = projectSessionAudience(result.Audience, class, access.ActorID)
	return result, nil
}

func (service *SessionParticipationService) RespondToSession(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	sessionID uuid.UUID,
	input SelfRSVPInput,
) (SelfRSVPMutationResult, error) {
	if service.sourceRepository != nil {
		return service.Respond(
			ctx,
			access,
			classID,
			SessionParticipationSource(sessionID),
			input,
		)
	}
	_, tenantContext, err := service.authorize(ctx, access, classID, policy.ActionClassView)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if sessionID == uuid.Nil {
		return SelfRSVPMutationResult{}, ErrSessionParticipationNotFound
	}
	params, err := input.normalized()
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	result, err := service.repository.RespondToSession(
		ctx, tenantContext, classID, sessionID, params, service.clock().UTC(),
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	result.Attendee.IsSelf = true
	return result, nil
}

func (service *SessionParticipationService) GetAudience(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	source ParticipationSourceRef,
) (SessionAudience, error) {
	class, tenantContext, err := service.authorize(
		ctx,
		access,
		classID,
		policy.ActionClassView,
	)
	if err != nil {
		return SessionAudience{}, err
	}
	source, err = source.Normalized()
	if err != nil {
		return SessionAudience{}, ErrSessionParticipationNotFound
	}
	if service.sourceRepository == nil {
		return SessionAudience{}, ErrSessionParticipationUnavailable
	}
	audience, err := service.sourceRepository.GetParticipationAudience(
		ctx,
		tenantContext,
		classID,
		source,
	)
	if err != nil {
		return SessionAudience{}, err
	}
	return projectSessionAudience(audience, class, access.ActorID), nil
}

func (service *SessionParticipationService) ReplaceAudience(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	source ParticipationSourceRef,
	input ReplaceAudienceInput,
) (SessionAudienceMutationResult, error) {
	class, tenantContext, err := service.authorize(
		ctx,
		access,
		classID,
		policy.ActionSessionSchedule,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	source, err = source.Normalized()
	if err != nil {
		return SessionAudienceMutationResult{}, ErrSessionParticipationNotFound
	}
	if service.sourceRepository == nil {
		return SessionAudienceMutationResult{}, ErrSessionParticipationUnavailable
	}
	params, err := input.normalized()
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	params, err = bindAudienceReplacementToSource(source, params)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	result, err := service.sourceRepository.ReplaceParticipationAudience(
		ctx,
		tenantContext,
		classID,
		source,
		params,
		service.clock().UTC(),
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	result.Audience = projectSessionAudience(result.Audience, class, access.ActorID)
	return result, nil
}

func (service *SessionParticipationService) Respond(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	source ParticipationSourceRef,
	input SelfRSVPInput,
) (SelfRSVPMutationResult, error) {
	_, tenantContext, err := service.authorize(
		ctx,
		access,
		classID,
		policy.ActionClassView,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	source, err = source.Normalized()
	if err != nil {
		return SelfRSVPMutationResult{}, ErrSessionParticipationNotFound
	}
	if service.sourceRepository == nil {
		return SelfRSVPMutationResult{}, ErrSessionParticipationUnavailable
	}
	params, err := input.normalized()
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	params, err = bindSelfRSVPToSource(source, params)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	result, err := service.sourceRepository.RespondToParticipationSource(
		ctx,
		tenantContext,
		classID,
		source,
		params,
		service.clock().UTC(),
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	result.Attendee.IsSelf = true
	return result, nil
}

func (service *SessionParticipationService) authorize(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) (Class, tenancy.Context, error) {
	if classID == uuid.Nil {
		return Class{}, tenancy.Context{}, ErrSessionParticipationNotFound
	}
	class, err := service.classAuthorizer.AuthorizeClass(ctx, access, classID, action)
	if err != nil {
		return Class{}, tenancy.Context{}, err
	}
	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return Class{}, tenancy.Context{}, ErrSessionParticipationAccessDenied
	}
	return class, tenantContext, nil
}

func projectSessionAudience(
	audience SessionAudience,
	class Class,
	actorID uuid.UUID,
) SessionAudience {
	canManage := class.ViewerAccess.CanScheduleSessions
	canSeeGuestList := canManage
	for _, attendee := range audience.Attendees {
		if attendee.UserID == actorID && attendee.CanSeeGuestList {
			canSeeGuestList = true
			break
		}
	}
	projected := SessionAudience{
		Source:            audience.Source,
		AudienceRevision:  audience.AudienceRevision,
		ResponseRequested: audience.ResponseRequested,
		SourceStatus:      audience.SourceStatus,
		ViewerAccess: SessionAudienceViewerAccess{
			CanManageAttendees: canManage,
			CanSeeGuestList:    canSeeGuestList,
		},
		Attendees: make([]SessionAudienceAttendee, 0, len(audience.Attendees)),
	}
	for _, attendee := range audience.Attendees {
		attendee.IsSelf = attendee.UserID == actorID
		if !canSeeGuestList && !attendee.IsSelf {
			continue
		}
		projected.Attendees = append(projected.Attendees, attendee)
		if attendee.IsSelf &&
			audience.SourceStatus == SessionStatusScheduled &&
			audience.ResponseRequested &&
			attendee.ResponseRequested &&
			attendee.ResponseClosedAt == nil {
			projected.ViewerAccess.CanRespond = true
		}
	}
	return projected
}
