package identity

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type ProviderClaims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
	Locale        string
	Nonce         string
	AuthTime      time.Time
}

type Provider interface {
	AuthorizationURL(state string, nonce string, codeChallenge string) string
	ExchangeAndVerify(
		ctx context.Context,
		code string,
		codeVerifier string,
	) (ProviderClaims, error)
	EndSessionURL() string
}

type LoginStart struct {
	AuthorizationURL string
	BrowserBinding   string
	ExpiresAt        time.Time
}

type LoginResult struct {
	SessionToken   string
	CSRFToken      string
	ExpiresAt      time.Time
	ReturnTo       string
	IdentityLinked bool
}

type CallbackInput struct {
	State          string
	BrowserBinding string
	Code           string
	UserAgent      string
	RemoteAddress  string
}

type User struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	DisplayName     string    `json:"display_name"`
	Locale          string    `json:"locale"`
	Timezone        string    `json:"timezone"`
	AvatarObjectKey string    `json:"avatar_object_key,omitempty"`
}

type ProfilePatch struct {
	DisplayName     *string `json:"display_name"`
	Locale          *string `json:"locale"`
	Timezone        *string `json:"timezone"`
	AvatarObjectKey *string `json:"avatar_object_key"`
}

type ExternalIdentity struct {
	ID                  uuid.UUID `json:"id"`
	Provider            string    `json:"provider"`
	Email               string    `json:"email"`
	EmailVerified       bool      `json:"email_verified"`
	CreatedAt           time.Time `json:"created_at"`
	LastAuthenticatedAt time.Time `json:"last_authenticated_at"`
}

type Tenant struct {
	ID       uuid.UUID `json:"id"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	IsActive bool      `json:"is_active"`
}

type CreateTenantInput struct {
	Name string
	Slug string
}

type TenantSessionResult struct {
	Principal    Principal
	SessionToken string
	CSRFToken    string
	ExpiresAt    time.Time
}

type Principal struct {
	SessionID       uuid.UUID `json:"-"`
	IdentityID      uuid.UUID `json:"-"`
	AuthenticatedAt time.Time `json:"-"`
	User            User
	ActiveTenant    *Tenant
	Memberships     []Tenant
	Permissions     []string
}

type StoredFlow struct {
	NonceHash              []byte
	CodeVerifierCiphertext []byte
	ReturnTo               string
	Purpose                string
	UserID                 uuid.UUID
	SessionID              uuid.UUID
}

type SessionRecord struct {
	Principal Principal
	CSRFHash  []byte
	ExpiresAt time.Time
}

type SessionMetadata struct {
	TokenHash     []byte
	CSRFHash      []byte
	UserAgentHash []byte
	IPPrefix      string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	AbsoluteAt    time.Time
}
