package classroom

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

// ExternalRSVPServiceAPI keeps authenticated issuance separate from public
// token exchange. Public methods accept no tenant, class, attendee or recipient
// identifier: the repository derives all authority from the capability digest.
type ExternalRSVPServiceAPI interface {
	IssueCapability(
		context.Context,
		AccessContext,
		uuid.UUID,
		ExternalRSVPCapabilityIssue,
	) (ExternalRSVPCapabilityToken, error)
	ResolveCapability(context.Context, string) (ExternalRSVPProjection, error)
	RespondWithCapability(
		context.Context,
		ExternalRSVPResponseInput,
	) (ExternalRSVPMutationResult, error)
}

type ExternalRSVPService struct {
	repository      ExternalRSVPCapabilityRepository
	classAuthorizer ClassActionAuthorizer
	clock           func() time.Time
}

func NewExternalRSVPService(
	repository ExternalRSVPCapabilityRepository,
	classAuthorizer ClassActionAuthorizer,
	clock func() time.Time,
) (*ExternalRSVPService, error) {
	if repository == nil || classAuthorizer == nil {
		return nil, fmt.Errorf("external RSVP repository and class authorizer are required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &ExternalRSVPService{
		repository:      repository,
		classAuthorizer: classAuthorizer,
		clock:           clock,
	}, nil
}

func (service *ExternalRSVPService) IssueCapability(
	ctx context.Context,
	access AccessContext,
	classID uuid.UUID,
	issue ExternalRSVPCapabilityIssue,
) (ExternalRSVPCapabilityToken, error) {
	if classID == uuid.Nil {
		return ExternalRSVPCapabilityToken{}, ErrExternalRSVPCapabilityUnavailable
	}
	if _, err := service.classAuthorizer.AuthorizeClass(
		ctx,
		access,
		classID,
		policy.ActionSessionSchedule,
	); err != nil {
		return ExternalRSVPCapabilityToken{}, err
	}
	tenantContext, err := tenancy.New(access.TenantID, access.ActorID)
	if err != nil {
		return ExternalRSVPCapabilityToken{}, ErrSessionParticipationAccessDenied
	}
	issue.IssuedAt = service.clock().UTC()
	return service.repository.IssueExternalRSVPCapability(
		ctx,
		tenantContext,
		classID,
		issue,
	)
}

func (service *ExternalRSVPService) ResolveCapability(
	ctx context.Context,
	rawToken string,
) (ExternalRSVPProjection, error) {
	return service.repository.ResolveExternalRSVPCapability(
		ctx,
		rawToken,
		service.clock().UTC(),
	)
}

func (service *ExternalRSVPService) RespondWithCapability(
	ctx context.Context,
	input ExternalRSVPResponseInput,
) (ExternalRSVPMutationResult, error) {
	params, err := input.normalized()
	if err != nil {
		return ExternalRSVPMutationResult{}, err
	}
	return service.repository.RespondWithExternalRSVPCapability(
		ctx,
		params,
		service.clock().UTC(),
	)
}

