package classroom

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type participationServiceAuthorizationCall struct {
	Access  AccessContext
	ClassID uuid.UUID
	Action  policy.Action
}

type participationServiceClassAuthorizerStub struct {
	class Class
	err   error
	calls []participationServiceAuthorizationCall
}

func (stub *participationServiceClassAuthorizerStub) AuthorizeClass(
	_ context.Context,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) (Class, error) {
	stub.calls = append(stub.calls, participationServiceAuthorizationCall{
		Access: access, ClassID: classID, Action: action,
	})
	if stub.err != nil {
		return Class{}, stub.err
	}
	return stub.class, nil
}

type participationServiceRepositoryStub struct {
	getAudience      SessionAudience
	getErr           error
	getCalls         int
	getTenantContext tenancy.Context
	getClassID       uuid.UUID
	getSessionID     uuid.UUID

	replaceResult        SessionAudienceMutationResult
	replaceErr           error
	replaceCalls         int
	replaceTenantContext tenancy.Context
	replaceClassID       uuid.UUID
	replaceSessionID     uuid.UUID
	replaceParams        AudienceReplacementParams
	replaceAt            time.Time

	respondResult        SelfRSVPMutationResult
	respondErr           error
	respondCalls         int
	respondTenantContext tenancy.Context
	respondClassID       uuid.UUID
	respondSessionID     uuid.UUID
	respondParams        SelfRSVPParams
	respondAt            time.Time
}

func (stub *participationServiceRepositoryStub) GetSessionAudience(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (SessionAudience, error) {
	stub.getCalls++
	stub.getTenantContext = tenantContext
	stub.getClassID = classID
	stub.getSessionID = sessionID
	if stub.getErr != nil {
		return SessionAudience{}, stub.getErr
	}
	return stub.getAudience, nil
}

func (stub *participationServiceRepositoryStub) ReplaceSessionAudience(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	params AudienceReplacementParams,
	at time.Time,
) (SessionAudienceMutationResult, error) {
	stub.replaceCalls++
	stub.replaceTenantContext = tenantContext
	stub.replaceClassID = classID
	stub.replaceSessionID = sessionID
	stub.replaceParams = params
	stub.replaceAt = at
	if stub.replaceErr != nil {
		return SessionAudienceMutationResult{}, stub.replaceErr
	}
	return stub.replaceResult, nil
}

func (stub *participationServiceRepositoryStub) RespondToSession(
	_ context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	params SelfRSVPParams,
	at time.Time,
) (SelfRSVPMutationResult, error) {
	stub.respondCalls++
	stub.respondTenantContext = tenantContext
	stub.respondClassID = classID
	stub.respondSessionID = sessionID
	stub.respondParams = params
	stub.respondAt = at
	if stub.respondErr != nil {
		return SelfRSVPMutationResult{}, stub.respondErr
	}
	return stub.respondResult, nil
}

func TestSessionParticipationServiceHidesOtherAttendeesForNonManager(t *testing.T) {
	t.Parallel()

	tenantID, actorID, otherID := uuid.New(), uuid.New(), uuid.New()
	classID, sessionID := uuid.New(), uuid.New()
	respondedAt := time.Date(2026, 7, 28, 4, 30, 0, 0, time.UTC)
	repository := &participationServiceRepositoryStub{getAudience: SessionAudience{
		AudienceRevision:  5,
		ResponseRequested: true,
		SourceStatus:      SessionStatusScheduled,
		Attendees: []SessionAudienceAttendee{
			{
				ID: uuid.New(), UserID: otherID, ParticipationRole: ParticipationRoleRequired,
				RSVPState: RSVPStateAccepted, Version: 3,
			},
			{
				ID: uuid.New(), UserID: actorID, ParticipationRole: ParticipationRoleOptional,
				RSVPState: RSVPStateTentative, RespondedAt: &respondedAt, Version: 4,
				ResponseRequested: true,
			},
		},
	}}
	authorizer := &participationServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: tenantID,
		ViewerAccess: ViewerAccess{CanScheduleSessions: false},
	}}
	service, err := NewSessionParticipationService(repository, authorizer)
	if err != nil {
		t.Fatalf("new participation service: %v", err)
	}

	audience, err := service.GetSessionAudience(
		context.Background(), participationServiceAccess(tenantID, actorID), classID, sessionID,
	)
	if err != nil {
		t.Fatalf("get session audience: %v", err)
	}
	if audience.AudienceRevision != 5 || !audience.ResponseRequested ||
		audience.ViewerAccess.CanManageAttendees || audience.ViewerAccess.CanSeeGuestList ||
		!audience.ViewerAccess.CanRespond {
		t.Fatalf("unexpected projected audience access: %+v", audience)
	}
	if len(audience.Attendees) != 1 {
		t.Fatalf("non-manager attendee count=%d, want only self", len(audience.Attendees))
	}
	self := audience.Attendees[0]
	if self.UserID != actorID || !self.IsSelf || self.RSVPState != RSVPStateTentative ||
		self.RespondedAt == nil || !self.RespondedAt.Equal(respondedAt) {
		t.Fatalf("unexpected non-manager self projection: %+v", self)
	}
	if repository.getCalls != 1 || repository.getTenantContext.TenantID != tenantID ||
		repository.getTenantContext.ActorID != actorID || repository.getClassID != classID ||
		repository.getSessionID != sessionID {
		t.Fatalf("unexpected audience repository call: %+v", repository)
	}
	if len(authorizer.calls) != 1 || authorizer.calls[0].Action != policy.ActionClassView ||
		authorizer.calls[0].ClassID != classID {
		t.Fatalf("unexpected authorization call: %+v", authorizer.calls)
	}
}

func TestSessionParticipationServiceHonorsGuestListAndClosedResponseProjection(t *testing.T) {
	t.Parallel()

	tenantID, actorID, otherID := uuid.New(), uuid.New(), uuid.New()
	classID, sessionID := uuid.New(), uuid.New()
	closedAt := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	repository := &participationServiceRepositoryStub{getAudience: SessionAudience{
		AudienceRevision:  3,
		ResponseRequested: true,
		SourceStatus:      SessionStatusCancelled,
		Attendees: []SessionAudienceAttendee{
			{
				ID: uuid.New(), UserID: actorID, ParticipationRole: ParticipationRoleRequired,
				RSVPState: RSVPStateAccepted, Version: 2, ResponseRequested: true,
				ResponseClosedAt: &closedAt, CanSeeGuestList: true,
			},
			{
				ID: uuid.New(), UserID: otherID, ParticipationRole: ParticipationRoleOptional,
				RSVPState: RSVPStateNeedsAction, Version: 1, ResponseRequested: true,
			},
		},
	}}
	authorizer := &participationServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: tenantID,
		ViewerAccess: ViewerAccess{CanScheduleSessions: false},
	}}
	service, err := NewSessionParticipationService(repository, authorizer)
	if err != nil {
		t.Fatalf("new participation service: %v", err)
	}

	audience, err := service.GetSessionAudience(
		context.Background(), participationServiceAccess(tenantID, actorID), classID, sessionID,
	)
	if err != nil {
		t.Fatalf("get session audience: %v", err)
	}
	if !audience.ViewerAccess.CanSeeGuestList ||
		audience.ViewerAccess.CanManageAttendees ||
		audience.ViewerAccess.CanRespond {
		t.Fatalf("unexpected guest-list/response projection: %+v", audience.ViewerAccess)
	}
	if len(audience.Attendees) != 2 {
		t.Fatalf("authorized guest-list attendee count=%d, want 2", len(audience.Attendees))
	}
}

func TestSessionParticipationServiceReplaceAudienceUsesNormalizedParamsAndFixedClock(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	classID, sessionID := uuid.New(), uuid.New()
	first := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	second := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	fixedClock := time.Date(2026, 7, 28, 13, 45, 0, 0, time.FixedZone("ICT", 7*60*60))
	repository := &participationServiceRepositoryStub{replaceResult: SessionAudienceMutationResult{
		Audience: SessionAudience{
			AudienceRevision: 4,
			Attendees: []SessionAudienceAttendee{
				{ID: uuid.New(), UserID: first, ParticipationRole: ParticipationRoleRequired, Version: 1},
				{ID: uuid.New(), UserID: second, ParticipationRole: ParticipationRoleOptional, Version: 1},
			},
		},
	}}
	authorizer := &participationServiceClassAuthorizerStub{class: Class{
		ID: classID, TenantID: tenantID,
		ViewerAccess: ViewerAccess{CanScheduleSessions: true},
	}}
	service, err := NewSessionParticipationService(
		repository,
		authorizer,
		SessionParticipationServiceConfig{Clock: func() time.Time { return fixedClock }},
	)
	if err != nil {
		t.Fatalf("new participation service: %v", err)
	}

	result, err := service.ReplaceSessionAudience(
		context.Background(), participationServiceAccess(tenantID, actorID), classID, sessionID,
		ReplaceAudienceInput{
			ExpectedAudienceRevision: 3,
			IdempotencyKey:           "  replace-audience-key-0001  ",
			ResponseRequested:        true,
			Attendees: []InternalAudienceAttendeeInput{
				{UserID: second, ParticipationRole: ParticipationRoleOptional},
				{UserID: first, ParticipationRole: ParticipationRoleRequired},
				{UserID: second, ParticipationRole: ParticipationRoleOptional},
			},
		},
	)
	if err != nil {
		t.Fatalf("replace session audience: %v", err)
	}
	if repository.replaceCalls != 1 || repository.replaceTenantContext.TenantID != tenantID ||
		repository.replaceTenantContext.ActorID != actorID || repository.replaceClassID != classID ||
		repository.replaceSessionID != sessionID || !repository.replaceAt.Equal(fixedClock.UTC()) {
		t.Fatalf("unexpected replacement repository call: %+v", repository)
	}
	params := repository.replaceParams
	if params.ExpectedAudienceRevision != 3 || params.IdempotencyKey != "replace-audience-key-0001" ||
		!params.ResponseRequested || params.Fingerprint == "" || len(params.Attendees) != 2 ||
		params.Attendees[0].UserID != first || params.Attendees[0].ParticipationRole != ParticipationRoleRequired ||
		params.Attendees[1].UserID != second || params.Attendees[1].ParticipationRole != ParticipationRoleOptional {
		t.Fatalf("replacement params were not normalized: %+v", params)
	}
	if len(authorizer.calls) != 1 || authorizer.calls[0].Action != policy.ActionSessionSchedule {
		t.Fatalf("unexpected authorization call: %+v", authorizer.calls)
	}
	if !result.Audience.ViewerAccess.CanManageAttendees ||
		!result.Audience.ViewerAccess.CanSeeGuestList || len(result.Audience.Attendees) != 2 {
		t.Fatalf("manager result was not fully projected: %+v", result)
	}
}

func TestSessionParticipationServiceRespondsOnlyForSelf(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	classID, sessionID := uuid.New(), uuid.New()
	fixedClock := time.Date(2026, 7, 28, 8, 0, 0, 0, time.UTC)
	repository := &participationServiceRepositoryStub{respondResult: SelfRSVPMutationResult{
		Attendee: SessionAudienceAttendee{
			ID: uuid.New(), UserID: actorID, ParticipationRole: ParticipationRoleRequired,
			RSVPState: RSVPStateAccepted, Version: 7,
		},
	}}
	authorizer := &participationServiceClassAuthorizerStub{class: Class{ID: classID, TenantID: tenantID}}
	service, err := NewSessionParticipationService(
		repository,
		authorizer,
		SessionParticipationServiceConfig{Clock: func() time.Time { return fixedClock }},
	)
	if err != nil {
		t.Fatalf("new participation service: %v", err)
	}

	result, err := service.RespondToSession(
		context.Background(), participationServiceAccess(tenantID, actorID), classID, sessionID,
		SelfRSVPInput{
			State: RSVPStateAccepted, Note: "  I'll attend.  ",
			ExpectedAttendeeVersion: 6, IdempotencyKey: "self-rsvp-response-key-0001",
		},
	)
	if err != nil {
		t.Fatalf("respond to session: %v", err)
	}
	if repository.respondCalls != 1 || repository.respondTenantContext.TenantID != tenantID ||
		repository.respondTenantContext.ActorID != actorID || repository.respondClassID != classID ||
		repository.respondSessionID != sessionID || !repository.respondAt.Equal(fixedClock) {
		t.Fatalf("unexpected self RSVP repository call: %+v", repository)
	}
	if repository.respondParams.State != RSVPStateAccepted || repository.respondParams.Note != "I'll attend." ||
		repository.respondParams.ExpectedAttendeeVersion != 6 ||
		repository.respondParams.IdempotencyKey != "self-rsvp-response-key-0001" ||
		repository.respondParams.Fingerprint == "" {
		t.Fatalf("unexpected normalized self RSVP params: %+v", repository.respondParams)
	}
	if result.Attendee.UserID != actorID || !result.Attendee.IsSelf {
		t.Fatalf("self RSVP must only return the actor attendee: %+v", result.Attendee)
	}
	if len(authorizer.calls) != 1 || authorizer.calls[0].Action != policy.ActionClassView {
		t.Fatalf("unexpected authorization call: %+v", authorizer.calls)
	}
}

func TestSessionParticipationServiceInvalidInputSkipsRepository(t *testing.T) {
	t.Parallel()

	tenantID, actorID := uuid.New(), uuid.New()
	classID, sessionID := uuid.New(), uuid.New()
	newService := func() (*SessionParticipationService, *participationServiceRepositoryStub) {
		repository := &participationServiceRepositoryStub{}
		service, err := NewSessionParticipationService(
			repository,
			&participationServiceClassAuthorizerStub{class: Class{ID: classID, TenantID: tenantID}},
		)
		if err != nil {
			t.Fatalf("new participation service: %v", err)
		}
		return service, repository
	}

	t.Run("replace audience", func(t *testing.T) {
		service, repository := newService()
		_, err := service.ReplaceSessionAudience(
			context.Background(), participationServiceAccess(tenantID, actorID), classID, sessionID,
			ReplaceAudienceInput{ExpectedAudienceRevision: 0, IdempotencyKey: "short"},
		)
		if !errors.Is(err, ErrInvalidParticipationInput) {
			t.Fatalf("replace error=%v, want invalid participation input", err)
		}
		if repository.replaceCalls != 0 {
			t.Fatalf("invalid audience replacement reached repository: %+v", repository)
		}
	})

	t.Run("self RSVP", func(t *testing.T) {
		service, repository := newService()
		_, err := service.RespondToSession(
			context.Background(), participationServiceAccess(tenantID, actorID), classID, sessionID,
			SelfRSVPInput{
				State: RSVPStateNeedsAction, ExpectedAttendeeVersion: 1,
				IdempotencyKey: "self-rsvp-invalid-key-0001",
			},
		)
		if !errors.Is(err, ErrInvalidParticipationInput) {
			t.Fatalf("RSVP error=%v, want invalid participation input", err)
		}
		if repository.respondCalls != 0 {
			t.Fatalf("invalid RSVP reached repository: %+v", repository)
		}
	})
}

func participationServiceAccess(tenantID uuid.UUID, actorID uuid.UUID) AccessContext {
	return AccessContext{TenantID: tenantID, ActorID: actorID, MembershipActive: true}
}
