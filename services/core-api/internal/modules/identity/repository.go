package identity

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type CreateFlowParams struct {
	StateHash              []byte
	BrowserBindingHash     []byte
	NonceHash              []byte
	CodeVerifierCiphertext []byte
	ReturnTo               string
	CreatedAt              time.Time
	ExpiresAt              time.Time
}

type Repository interface {
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
}
