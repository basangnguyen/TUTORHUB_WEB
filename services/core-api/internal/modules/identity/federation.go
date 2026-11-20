package identity

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

var (
	ErrFederationProviderUnavailable = errors.New("federation provider unavailable")
	ErrFederationSignupDisabled      = errors.New("federated signup disabled")
	ErrLinkIntentMismatch            = errors.New("link intent belongs to another account")
	ErrLinkIntentUnavailable         = errors.New("link intent unavailable")
	ErrFederationAALRequired         = errors.New("account assurance required")
	ErrFederatedIdentityMismatch     = errors.New("federated identity does not match the account")
	ErrFederationReauthRequired      = errors.New("provider did not prove recent authentication")
)

type FederationConfig struct {
	SignupEnabled     bool
	LegalTermsVersion string
}

type OAuthResult struct {
	LoginResult
	RegistrationPending   bool
	RegistrationBinding   string
	RegistrationExpiresAt time.Time
	Provider              string
	LinkPending           bool
	LinkBinding           string
	LinkExpiresAt         time.Time
}

type LinkIntentView struct {
	Provider         string `json:"provider"`
	MaskedEmail      string `json:"masked_email"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type LinkIntentResult struct {
	SessionToken, CSRFToken string
	CookieExpiresAt         time.Time
	ReturnTo                string
}

type FederationAPI interface {
	BeginOAuth(context.Context, string, string, string, Principal) (LoginStart, error)
	CompleteOAuth(context.Context, string, CallbackInput) (OAuthResult, error)
	BeginProviderIdentityLink(context.Context, Principal, string) (LoginStart, error)
	GetLinkIntent(context.Context, string) (LinkIntentView, error)
	ConfirmLinkIntent(context.Context, Principal, string) (LinkIntentResult, error)
	CancelLinkIntent(context.Context, string) error
}

type FederationIdentityPayload struct {
	ProviderSlug     string `json:"provider_slug"`
	Issuer           string `json:"issuer"`
	Subject          string `json:"subject"`
	Email            string `json:"email"`
	EmailVerified    bool   `json:"email_verified"`
	DisplayName      string `json:"display_name"`
	ProviderOrgID    string `json:"provider_org_id"`
	ProviderStableID string `json:"provider_stable_id,omitempty"`
	ReturnTo         string `json:"return_to"`
}

func (p FederationIdentityPayload) claims() ProviderClaims {
	return ProviderClaims{
		ProviderSlug: p.ProviderSlug, Issuer: p.Issuer, Subject: p.Subject,
		Email: p.Email, EmailVerified: p.EmailVerified && p.ProviderSlug != "facebook",
		DisplayName: p.DisplayName, ProviderOrgID: p.ProviderOrgID, ProviderStableID: p.ProviderStableID,
	}
}

func supportsPendingRegistration(slug string) bool {
	return slug == "facebook" || slug == "microsoft"
}

type FederationLoginParams struct {
	Claims                                         ProviderClaims
	ProviderSlug                                   string
	Flow                                           StoredFlow
	Metadata                                       SessionMetadata
	SignupEnabled                                  bool
	LegalTermsVersion                              string
	LinkBindingHash, LinkPayloadCiphertext         []byte
	RegistrationBindingHash, ProviderSubjectDigest []byte
	LinkExpiresAt                                  time.Time
	SessionAbsoluteTTL                             time.Duration
}

type FederationLoginRecord struct {
	MFA                 *MFAChallenge
	Principal           Principal
	LinkPending         bool
	RegistrationPending bool
}

type StoredLinkIntent struct {
	ID, UserID           uuid.UUID
	TargetEmail          string
	PayloadCiphertext    []byte
	CreatedAt, ExpiresAt time.Time
}

type LinkIntentConfirmParams struct {
	Principal          Principal
	BindingHash        []byte
	Intent             StoredLinkIntent
	Payload            FederationIdentityPayload
	Rotation           SessionRotation
	SessionAbsoluteTTL time.Duration
}

type FederationReauthParams struct {
	Flow               StoredFlow
	Claims             ProviderClaims
	Rotation           SessionRotation
	SessionAbsoluteTTL time.Duration
}

type FederationRepository interface {
	ConsumeProviderFlow(context.Context, string, []byte, []byte, time.Time) (StoredFlow, error)
	CompleteFederatedLogin(context.Context, FederationLoginParams) (FederationLoginRecord, error)
	ReadLinkIntent(context.Context, []byte, time.Time) (StoredLinkIntent, error)
	ConfirmLinkIntent(context.Context, LinkIntentConfirmParams) (Principal, error)
	CancelLinkIntent(context.Context, []byte, time.Time) error
	ReauthenticateFederated(context.Context, FederationReauthParams) (Principal, error)
}

func LinkTransactionCookieName(secure bool) string {
	if secure {
		return "__Host-tutorhub_link_tx"
	}
	return "tutorhub_link_tx"
}

func (OAuthResult) String() string               { return "[redacted federation result]" }
func (OAuthResult) LogValue() slog.Value         { return slog.StringValue("[redacted federation result]") }
func (LinkIntentResult) String() string          { return "[redacted link result]" }
func (LinkIntentResult) LogValue() slog.Value    { return slog.StringValue("[redacted link result]") }
func (FederationIdentityPayload) String() string { return "[redacted federation identity]" }
func (FederationIdentityPayload) LogValue() slog.Value {
	return slog.StringValue("[redacted federation identity]")
}
