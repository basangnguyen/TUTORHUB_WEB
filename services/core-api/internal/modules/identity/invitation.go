package identity

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	membershipInvitationTokenPrefix  = "thinv1_"
	membershipInvitationTokenPurpose = "membership-invitation-token-v1"
	defaultMembershipInvitationTTL   = 168 * time.Hour
	minimumMembershipInvitationTTL   = 15 * time.Minute
	maximumMembershipInvitationTTL   = 720 * time.Hour
	maximumMembershipInvitationEmail = 320
)

type MembershipInvitationStatus string

const (
	MembershipInvitationPending  MembershipInvitationStatus = "pending"
	MembershipInvitationAccepted MembershipInvitationStatus = "accepted"
	MembershipInvitationRevoked  MembershipInvitationStatus = "revoked"
	MembershipInvitationExpired  MembershipInvitationStatus = "expired"
)

type MembershipInvitation struct {
	ID           uuid.UUID                  `json:"id"`
	TenantID     uuid.UUID                  `json:"tenant_id"`
	Email        string                     `json:"email"`
	IntendedRole string                     `json:"intended_role"`
	Status       MembershipInvitationStatus `json:"status"`
	ExpiresAt    time.Time                  `json:"expires_at"`
	AcceptedAt   *time.Time                 `json:"accepted_at"`
	RevokedAt    *time.Time                 `json:"revoked_at"`
	CreatedAt    time.Time                  `json:"created_at"`
	UpdatedAt    time.Time                  `json:"updated_at"`
	InvitedBy    uuid.UUID                  `json:"-"`
	AcceptedBy   *uuid.UUID                 `json:"-"`
	RevokedBy    *uuid.UUID                 `json:"-"`
}

type CreateMembershipInvitationInput struct {
	Email        string
	IntendedRole string
}

type CreateMembershipInvitationResult struct {
	Invitation MembershipInvitation
	Token      string
}

type MembershipInvitationPreview struct {
	TenantName   string
	MaskedEmail  string
	IntendedRole string
	Status       MembershipInvitationStatus
	ExpiresAt    time.Time
}

type AcceptMembershipInvitationResult struct {
	Invitation MembershipInvitation
	Principal  Principal
}

// MembershipInvitationNotificationAdapter is the provider-neutral boundary for
// a future mail or notification delivery implementation. P2-03 does not call a
// provider: the transactional outbox records only the redacted invitation lifecycle,
// while the raw token exists only in the create response. A later delivery design must
// preserve that boundary instead of placing the raw token in durable event payloads.
type MembershipInvitationNotificationAdapter interface {
	DeliverMembershipInvitation(
		context.Context,
		MembershipInvitation,
		string,
	) error
}

type CreateMembershipInvitationParams struct {
	Email        string
	IntendedRole string
	TokenHash    []byte
	CreatedAt    time.Time
	ExpiresAt    time.Time
}

type AcceptMembershipInvitationParams struct {
	SessionID  uuid.UUID
	UserID     uuid.UUID
	TokenHash  []byte
	AcceptedAt time.Time
}

type StoredMembershipInvitationPreview struct {
	Invitation MembershipInvitation
	TenantName string
}

type MembershipInvitationRepository interface {
	ListMembershipInvitations(
		context.Context,
		tenancy.Context,
		time.Time,
	) ([]MembershipInvitation, error)
	CreateMembershipInvitation(
		context.Context,
		tenancy.Context,
		CreateMembershipInvitationParams,
	) (MembershipInvitation, error)
	RevokeMembershipInvitation(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		time.Time,
	) (MembershipInvitation, error)
	PreviewMembershipInvitation(
		context.Context,
		[]byte,
		time.Time,
	) (StoredMembershipInvitationPreview, error)
	AcceptMembershipInvitation(
		context.Context,
		AcceptMembershipInvitationParams,
	) (AcceptMembershipInvitationResult, error)
}

type MembershipInvitationServiceAPI interface {
	ListMembershipInvitations(
		context.Context,
		Principal,
		uuid.UUID,
	) ([]MembershipInvitation, error)
	CreateMembershipInvitation(
		context.Context,
		Principal,
		uuid.UUID,
		CreateMembershipInvitationInput,
	) (CreateMembershipInvitationResult, error)
	RevokeMembershipInvitation(
		context.Context,
		Principal,
		uuid.UUID,
		uuid.UUID,
	) (MembershipInvitation, error)
	PreviewMembershipInvitation(
		context.Context,
		string,
	) (MembershipInvitationPreview, error)
	AcceptMembershipInvitation(
		context.Context,
		Principal,
		string,
	) (AcceptMembershipInvitationResult, error)
}

func (service *Service) ListMembershipInvitations(
	ctx context.Context,
	principal Principal,
	tenantID uuid.UUID,
) ([]MembershipInvitation, error) {
	tenantContext, err := service.authorizeTenant(
		principal,
		tenantID,
		policy.ActionTenantManageMembers,
	)
	if err != nil {
		return nil, err
	}

	return service.repository.ListMembershipInvitations(
		ctx,
		tenantContext,
		service.clock().UTC(),
	)
}

func (service *Service) CreateMembershipInvitation(
	ctx context.Context,
	principal Principal,
	tenantID uuid.UUID,
	input CreateMembershipInvitationInput,
) (CreateMembershipInvitationResult, error) {
	tenantContext, err := service.authorizeTenant(
		principal,
		tenantID,
		policy.ActionTenantManageMembers,
	)
	if err != nil {
		return CreateMembershipInvitationResult{}, err
	}

	normalized, err := normalizeMembershipInvitationInput(input)
	if err != nil {
		return CreateMembershipInvitationResult{}, err
	}
	randomValue, err := service.crypto.RandomToken()
	if err != nil {
		return CreateMembershipInvitationResult{}, fmt.Errorf(
			"generate membership invitation token: %w",
			err,
		)
	}
	token := membershipInvitationTokenPrefix + randomValue
	now := service.clock().UTC()
	invitation, err := service.repository.CreateMembershipInvitation(
		ctx,
		tenantContext,
		CreateMembershipInvitationParams{
			Email:        normalized.Email,
			IntendedRole: normalized.IntendedRole,
			TokenHash: service.crypto.Digest(
				membershipInvitationTokenPurpose,
				token,
			),
			CreatedAt: now,
			ExpiresAt: now.Add(service.config.MembershipInvitationTTL),
		},
	)
	if err != nil {
		return CreateMembershipInvitationResult{}, err
	}

	return CreateMembershipInvitationResult{Invitation: invitation, Token: token}, nil
}

func (service *Service) RevokeMembershipInvitation(
	ctx context.Context,
	principal Principal,
	tenantID uuid.UUID,
	invitationID uuid.UUID,
) (MembershipInvitation, error) {
	tenantContext, err := service.authorizeTenant(
		principal,
		tenantID,
		policy.ActionTenantManageMembers,
	)
	if err != nil {
		return MembershipInvitation{}, err
	}
	if invitationID == uuid.Nil {
		return MembershipInvitation{}, ErrMembershipInvitationUnavailable
	}

	return service.repository.RevokeMembershipInvitation(
		ctx,
		tenantContext,
		invitationID,
		service.clock().UTC(),
	)
}

func (service *Service) PreviewMembershipInvitation(
	ctx context.Context,
	rawToken string,
) (MembershipInvitationPreview, error) {
	token, err := normalizeMembershipInvitationToken(rawToken)
	if err != nil {
		return MembershipInvitationPreview{}, err
	}
	stored, err := service.repository.PreviewMembershipInvitation(
		ctx,
		service.crypto.Digest(membershipInvitationTokenPurpose, token),
		service.clock().UTC(),
	)
	if err != nil {
		return MembershipInvitationPreview{}, err
	}

	return MembershipInvitationPreview{
		TenantName:   stored.TenantName,
		MaskedEmail:  maskMembershipInvitationEmail(stored.Invitation.Email),
		IntendedRole: stored.Invitation.IntendedRole,
		Status:       stored.Invitation.Status,
		ExpiresAt:    stored.Invitation.ExpiresAt,
	}, nil
}

func (service *Service) AcceptMembershipInvitation(
	ctx context.Context,
	principal Principal,
	rawToken string,
) (AcceptMembershipInvitationResult, error) {
	if err := validatePrincipal(principal); err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	token, err := normalizeMembershipInvitationToken(rawToken)
	if err != nil {
		return AcceptMembershipInvitationResult{}, err
	}
	return service.repository.AcceptMembershipInvitation(
		ctx,
		AcceptMembershipInvitationParams{
			SessionID: principal.SessionID,
			UserID:    principal.User.ID,
			TokenHash: service.crypto.Digest(
				membershipInvitationTokenPurpose,
				token,
			),
			AcceptedAt: service.clock().UTC(),
		},
	)
}

func normalizeMembershipInvitationInput(
	input CreateMembershipInvitationInput,
) (CreateMembershipInvitationInput, error) {
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > maximumMembershipInvitationEmail {
		return CreateMembershipInvitationInput{}, fmt.Errorf(
			"%w: email must be one normalized address",
			ErrInvalidMembershipInvitation,
		)
	}

	role := policy.OrganizationRole(strings.TrimSpace(input.IntendedRole))
	switch role {
	case policy.OrganizationRoleTeacher,
		policy.OrganizationRoleStudent,
		policy.OrganizationRoleGuest:
		return CreateMembershipInvitationInput{
			Email:        email,
			IntendedRole: string(role),
		}, nil
	default:
		return CreateMembershipInvitationInput{}, fmt.Errorf(
			"%w: intended_role is not grantable",
			ErrInvalidMembershipInvitation,
		)
	}
}

func normalizeMembershipInvitationToken(rawToken string) (string, error) {
	token := strings.TrimSpace(rawToken)
	if !strings.HasPrefix(token, membershipInvitationTokenPrefix) {
		return "", ErrMembershipInvitationUnavailable
	}
	encoded := strings.TrimPrefix(token, membershipInvitationTokenPrefix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != randomTokenBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return "", ErrMembershipInvitationUnavailable
	}
	return token, nil
}

func maskMembershipInvitationEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "***"
	}
	local := []rune(parts[0])
	if utf8.RuneCountInString(parts[0]) == 1 {
		return "*@" + parts[1]
	}
	return string(local[0]) + "***@" + parts[1]
}
