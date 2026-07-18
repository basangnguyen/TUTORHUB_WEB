package identity

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type CreateFlowParams struct {
	StateHash              []byte
	BrowserBindingHash     []byte
	NonceHash              []byte
	CodeVerifierCiphertext []byte
	ReturnTo               string
	Purpose                string
	UserID                 uuid.UUID
	SessionID              uuid.UUID
	CreatedAt              time.Time
	ExpiresAt              time.Time
}

type SessionRotation struct {
	TokenHash              []byte
	CSRFHash               []byte
	ExpectedContextVersion int64
	RotatedAt              time.Time
}

type TenantMutationResult struct {
	Principal Principal
	ExpiresAt time.Time
}

type TenantArchiveMutationResult struct {
	Principal Principal
	ExpiresAt time.Time
}

type Repository interface {
	MembershipInvitationRepository
	CreateFlow(ctx context.Context, params CreateFlowParams) error
	ConsumeFlow(
		ctx context.Context,
		stateHash []byte,
		browserBindingHash []byte,
		consumedAt time.Time,
	) (StoredFlow, error)
	CreateAuthenticatedSession(
		ctx context.Context,
		claims ProviderClaims,
		metadata SessionMetadata,
	) (Principal, error)
	GetSession(
		ctx context.Context,
		tokenHash []byte,
		now time.Time,
		idleTTL time.Duration,
	) (SessionRecord, error)
	RotateCSRF(ctx context.Context, sessionID uuid.UUID, csrfHash []byte, now time.Time) error
	RevokeSession(ctx context.Context, tokenHash []byte, now time.Time, reason string) error
	CreateTenant(
		ctx context.Context,
		sessionID uuid.UUID,
		userID uuid.UUID,
		authorizedSourceTenantID uuid.UUID,
		input CreateTenantInput,
		rotation SessionRotation,
	) (TenantMutationResult, error)
	SwitchActiveTenant(
		ctx context.Context,
		sessionID uuid.UUID,
		userID uuid.UUID,
		tenantID uuid.UUID,
		rotation SessionRotation,
	) (TenantMutationResult, error)
	ListTenants(ctx context.Context, userID uuid.UUID) ([]Tenant, error)
	GetTenant(ctx context.Context, tenantContext tenancy.Context) (Tenant, error)
	UpdateTenant(
		ctx context.Context,
		tenantContext tenancy.Context,
		input UpdateTenantInput,
		updatedAt time.Time,
	) (Tenant, error)
	ArchiveTenant(
		ctx context.Context,
		tenantContext tenancy.Context,
		sessionID uuid.UUID,
		expectedVersion int64,
		rotation SessionRotation,
	) (TenantArchiveMutationResult, error)
	GetProfile(ctx context.Context, userID uuid.UUID) (User, error)
	UpdateProfile(
		ctx context.Context,
		sessionID uuid.UUID,
		userID uuid.UUID,
		patch ProfilePatch,
		updatedAt time.Time,
	) (User, error)
	ListIdentities(ctx context.Context, userID uuid.UUID) ([]ExternalIdentity, error)
	LinkIdentity(
		ctx context.Context,
		userID uuid.UUID,
		sessionID uuid.UUID,
		claims ProviderClaims,
		linkedAt time.Time,
	) (ExternalIdentity, error)
	UnlinkIdentity(
		ctx context.Context,
		userID uuid.UUID,
		sessionID uuid.UUID,
		identityID uuid.UUID,
		unlinkedAt time.Time,
	) error
}
