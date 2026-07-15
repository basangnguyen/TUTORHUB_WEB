package identity

import (
	"context"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	statePurpose          = "oidc-state"
	browserBindingPurpose = "oidc-browser-binding"
	noncePurpose          = "oidc-nonce"
	sessionPurpose        = "session-token"
	csrfPurpose           = "csrf-token"
	userAgentPurpose      = "session-user-agent"
	tenantSlugMinimum     = 3
	tenantSlugMaximum     = 63
	tenantNameMinimum     = 2
	tenantNameMaximum     = 120
)

var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)

type ServiceConfig struct {
	FlowTTL            time.Duration
	SessionTTL         time.Duration
	SessionAbsoluteTTL time.Duration
}

type CSRFResult struct {
	Token     string
	Principal Principal
	ExpiresAt time.Time
}

type ServiceAPI interface {
	BeginLogin(context.Context, string) (LoginStart, error)
	CompleteLogin(context.Context, CallbackInput) (LoginResult, error)
	Authenticate(context.Context, string) (Principal, error)
	RotateCSRF(context.Context, string) (CSRFResult, error)
	ValidateCSRF(context.Context, string, string) (Principal, error)
	Logout(context.Context, string) (string, error)
	CreateTenant(context.Context, Principal, CreateTenantInput) (TenantSessionResult, error)
	SwitchActiveTenant(context.Context, Principal, uuid.UUID) (TenantSessionResult, error)
}

type Service struct {
	repository Repository
	provider   Provider
	crypto     *Crypto
	config     ServiceConfig
	clock      func() time.Time
}

func NewService(
	repository Repository,
	provider Provider,
	crypto *Crypto,
	config ServiceConfig,
	clock func() time.Time,
) (*Service, error) {
	if repository == nil || provider == nil || crypto == nil {
		return nil, fmt.Errorf("identity service dependencies must be configured")
	}
	if config.FlowTTL <= 0 || config.SessionTTL <= 0 || config.SessionAbsoluteTTL <= 0 {
		return nil, fmt.Errorf("identity service durations must be positive")
	}
	if config.FlowTTL > 15*time.Minute {
		return nil, fmt.Errorf("identity authentication flow must not exceed 15 minutes")
	}
	if config.SessionTTL > config.SessionAbsoluteTTL {
		return nil, fmt.Errorf("identity session TTL must not exceed absolute TTL")
	}
	if clock == nil {
		clock = time.Now
	}

	return &Service{
		repository: repository,
		provider:   provider,
		crypto:     crypto,
		config:     config,
		clock:      clock,
	}, nil
}

func (service *Service) BeginLogin(ctx context.Context, returnTo string) (LoginStart, error) {
	normalizedReturnTo, err := NormalizeReturnTo(returnTo)
	if err != nil {
		return LoginStart{}, err
	}

	state, err := service.crypto.RandomToken()
	if err != nil {
		return LoginStart{}, fmt.Errorf("generate OIDC state: %w", err)
	}
	browserBinding, err := service.crypto.RandomToken()
	if err != nil {
		return LoginStart{}, fmt.Errorf("generate browser binding: %w", err)
	}
	nonce, err := service.crypto.RandomToken()
	if err != nil {
		return LoginStart{}, fmt.Errorf("generate OIDC nonce: %w", err)
	}
	verifier, err := service.crypto.PKCEVerifier()
	if err != nil {
		return LoginStart{}, fmt.Errorf("generate PKCE verifier: %w", err)
	}
	encryptedVerifier, err := service.crypto.Encrypt(verifier)
	if err != nil {
		return LoginStart{}, fmt.Errorf("encrypt PKCE verifier: %w", err)
	}

	now := service.clock().UTC()
	if err := service.repository.CreateFlow(ctx, CreateFlowParams{
		StateHash:              service.crypto.Digest(statePurpose, state),
		BrowserBindingHash:     service.crypto.Digest(browserBindingPurpose, browserBinding),
		NonceHash:              service.crypto.Digest(noncePurpose, nonce),
		CodeVerifierCiphertext: encryptedVerifier,
		ReturnTo:               normalizedReturnTo,
		CreatedAt:              now,
		ExpiresAt:              now.Add(service.config.FlowTTL),
	}); err != nil {
		return LoginStart{}, fmt.Errorf("persist OIDC authentication flow: %w", err)
	}

	return LoginStart{
		AuthorizationURL: service.provider.AuthorizationURL(
			state,
			nonce,
			PKCEChallenge(verifier),
		),
		BrowserBinding: browserBinding,
		ExpiresAt:      now.Add(service.config.FlowTTL),
	}, nil
}

func (service *Service) CompleteLogin(
	ctx context.Context,
	input CallbackInput,
) (LoginResult, error) {
	if strings.TrimSpace(input.State) == "" ||
		strings.TrimSpace(input.BrowserBinding) == "" ||
		strings.TrimSpace(input.Code) == "" {
		return LoginResult{}, ErrInvalidAuthFlow
	}

	now := service.clock().UTC()
	flow, err := service.repository.ConsumeFlow(
		ctx,
		service.crypto.Digest(statePurpose, input.State),
		service.crypto.Digest(browserBindingPurpose, input.BrowserBinding),
		now,
	)
	if err != nil {
		return LoginResult{}, err
	}

	verifier, err := service.crypto.Decrypt(flow.CodeVerifierCiphertext)
	if err != nil {
		return LoginResult{}, fmt.Errorf("%w: invalid stored PKCE verifier", ErrInvalidAuthFlow)
	}
	claims, err := service.provider.ExchangeAndVerify(ctx, input.Code, verifier)
	if err != nil {
		return LoginResult{}, fmt.Errorf("%w: %v", ErrProviderExchange, err)
	}
	if !service.crypto.EqualDigest(flow.NonceHash, noncePurpose, claims.Nonce) {
		return LoginResult{}, fmt.Errorf("%w: nonce mismatch", ErrProviderExchange)
	}
	claims, err = normalizeProviderClaims(claims, now)
	if err != nil {
		return LoginResult{}, err
	}

	sessionToken, err := service.crypto.RandomToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate session token: %w", err)
	}
	csrfToken, err := service.crypto.RandomToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate CSRF token: %w", err)
	}
	expiresAt := now.Add(service.config.SessionTTL)
	absoluteAt := now.Add(service.config.SessionAbsoluteTTL)

	_, err = service.repository.CreateAuthenticatedSession(ctx, claims, SessionMetadata{
		TokenHash:     service.crypto.Digest(sessionPurpose, sessionToken),
		CSRFHash:      service.crypto.Digest(csrfPurpose, csrfToken),
		UserAgentHash: service.crypto.Digest(userAgentPurpose, input.UserAgent),
		IPPrefix:      IPPrefix(input.RemoteAddress),
		CreatedAt:     now,
		ExpiresAt:     expiresAt,
		AbsoluteAt:    absoluteAt,
	})
	if err != nil {
		return LoginResult{}, fmt.Errorf("create authenticated session: %w", err)
	}

	return LoginResult{
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		ExpiresAt:    expiresAt,
		ReturnTo:     flow.ReturnTo,
	}, nil
}

func (service *Service) Authenticate(ctx context.Context, sessionToken string) (Principal, error) {
	record, err := service.session(ctx, sessionToken)
	if err != nil {
		return Principal{}, err
	}
	return record.Principal, nil
}

func (service *Service) RotateCSRF(ctx context.Context, sessionToken string) (CSRFResult, error) {
	record, err := service.session(ctx, sessionToken)
	if err != nil {
		return CSRFResult{}, err
	}
	token, err := service.crypto.RandomToken()
	if err != nil {
		return CSRFResult{}, fmt.Errorf("generate CSRF token: %w", err)
	}
	if err := service.repository.RotateCSRF(
		ctx,
		record.Principal.SessionID,
		service.crypto.Digest(csrfPurpose, token),
		service.clock().UTC(),
	); err != nil {
		return CSRFResult{}, err
	}

	return CSRFResult{
		Token:     token,
		Principal: record.Principal,
		ExpiresAt: record.ExpiresAt,
	}, nil
}

func (service *Service) ValidateCSRF(
	ctx context.Context,
	sessionToken string,
	csrfToken string,
) (Principal, error) {
	if strings.TrimSpace(csrfToken) == "" {
		return Principal{}, ErrInvalidCSRFToken
	}
	record, err := service.session(ctx, sessionToken)
	if err != nil {
		return Principal{}, err
	}
	if !service.crypto.EqualDigest(record.CSRFHash, csrfPurpose, csrfToken) {
		return Principal{}, ErrInvalidCSRFToken
	}

	return record.Principal, nil
}

func (service *Service) Logout(ctx context.Context, sessionToken string) (string, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return "", ErrSessionNotFound
	}
	if err := service.repository.RevokeSession(
		ctx,
		service.crypto.Digest(sessionPurpose, sessionToken),
		service.clock().UTC(),
		"user_logout",
	); err != nil {
		return "", err
	}

	return service.provider.EndSessionURL(), nil
}

func (service *Service) CreateTenant(
	ctx context.Context,
	principal Principal,
	input CreateTenantInput,
) (TenantSessionResult, error) {
	if principal.SessionID == uuid.Nil || principal.User.ID == uuid.Nil {
		return TenantSessionResult{}, ErrSessionNotFound
	}
	if len(principal.Memberships) != 0 {
		return TenantSessionResult{}, ErrTenantCreationDenied
	}

	normalized, err := normalizeTenantInput(input)
	if err != nil {
		return TenantSessionResult{}, err
	}
	return service.rotateTenantSession(ctx, principal, normalized, uuid.Nil)
}

func (service *Service) SwitchActiveTenant(
	ctx context.Context,
	principal Principal,
	tenantID uuid.UUID,
) (TenantSessionResult, error) {
	if principal.SessionID == uuid.Nil || principal.User.ID == uuid.Nil {
		return TenantSessionResult{}, ErrSessionNotFound
	}
	if tenantID == uuid.Nil {
		return TenantSessionResult{}, ErrInvalidTenant
	}
	if principal.ActiveTenant != nil && principal.ActiveTenant.ID == tenantID {
		return TenantSessionResult{}, ErrInvalidTenant
	}

	return service.rotateTenantSession(ctx, principal, CreateTenantInput{}, tenantID)
}

func (service *Service) rotateTenantSession(
	ctx context.Context,
	principal Principal,
	createInput CreateTenantInput,
	tenantID uuid.UUID,
) (TenantSessionResult, error) {
	sessionToken, err := service.crypto.RandomToken()
	if err != nil {
		return TenantSessionResult{}, fmt.Errorf("rotate session token: %w", err)
	}
	csrfToken, err := service.crypto.RandomToken()
	if err != nil {
		return TenantSessionResult{}, fmt.Errorf("rotate CSRF token: %w", err)
	}
	rotation := SessionRotation{
		TokenHash: service.crypto.Digest(sessionPurpose, sessionToken),
		CSRFHash:  service.crypto.Digest(csrfPurpose, csrfToken),
		RotatedAt: service.clock().UTC(),
	}

	var mutation TenantMutationResult
	if tenantID == uuid.Nil {
		mutation, err = service.repository.CreateTenant(
			ctx,
			principal.SessionID,
			principal.User.ID,
			createInput,
			rotation,
		)
	} else {
		mutation, err = service.repository.SwitchActiveTenant(
			ctx,
			principal.SessionID,
			principal.User.ID,
			tenantID,
			rotation,
		)
	}
	if err != nil {
		return TenantSessionResult{}, err
	}

	return TenantSessionResult{
		Principal:    mutation.Principal,
		SessionToken: sessionToken,
		CSRFToken:    csrfToken,
		ExpiresAt:    mutation.ExpiresAt,
	}, nil
}

func (service *Service) session(
	ctx context.Context,
	sessionToken string,
) (SessionRecord, error) {
	if strings.TrimSpace(sessionToken) == "" {
		return SessionRecord{}, ErrSessionNotFound
	}
	return service.repository.GetSession(
		ctx,
		service.crypto.Digest(sessionPurpose, sessionToken),
		service.clock().UTC(),
		service.config.SessionTTL,
	)
}

func normalizeProviderClaims(claims ProviderClaims, now time.Time) (ProviderClaims, error) {
	claims.Issuer = strings.TrimRight(strings.TrimSpace(claims.Issuer), "/")
	claims.Subject = strings.TrimSpace(claims.Subject)
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	claims.DisplayName = strings.TrimSpace(claims.DisplayName)
	claims.Locale = strings.TrimSpace(claims.Locale)

	if claims.Issuer == "" || len(claims.Issuer) > 100 || claims.Subject == "" || len(claims.Subject) > 500 {
		return ProviderClaims{}, fmt.Errorf("%w: invalid issuer or subject", ErrProviderExchange)
	}
	if !claims.EmailVerified {
		return ProviderClaims{}, ErrVerifiedEmailRequired
	}
	address, err := mail.ParseAddress(claims.Email)
	if err != nil || !strings.EqualFold(address.Address, claims.Email) || len(claims.Email) > 320 {
		return ProviderClaims{}, fmt.Errorf("%w: invalid verified email", ErrProviderExchange)
	}
	if claims.DisplayName == "" {
		claims.DisplayName = strings.SplitN(claims.Email, "@", 2)[0]
	}
	claims.DisplayName = truncateRunes(claims.DisplayName, 200)
	if claims.Locale == "" {
		claims.Locale = "vi"
	}
	claims.Locale = truncateRunes(claims.Locale, 35)
	if claims.AuthTime.IsZero() || claims.AuthTime.After(now.Add(5*time.Minute)) {
		claims.AuthTime = now
	} else {
		claims.AuthTime = claims.AuthTime.UTC()
	}

	return claims, nil
}

func normalizeTenantInput(input CreateTenantInput) (CreateTenantInput, error) {
	input.Name = strings.Join(strings.Fields(input.Name), " ")
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))

	nameLength := utf8.RuneCountInString(input.Name)
	if nameLength < tenantNameMinimum || nameLength > tenantNameMaximum {
		return CreateTenantInput{}, fmt.Errorf(
			"%w: name must contain between %d and %d characters",
			ErrInvalidTenant,
			tenantNameMinimum,
			tenantNameMaximum,
		)
	}
	if len(input.Slug) < tenantSlugMinimum ||
		len(input.Slug) > tenantSlugMaximum ||
		!tenantSlugPattern.MatchString(input.Slug) {
		return CreateTenantInput{}, fmt.Errorf(
			"%w: slug must contain 3-63 lowercase letters, numbers, or hyphens",
			ErrInvalidTenant,
		)
	}

	return input, nil
}

func truncateRunes(value string, maximum int) string {
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}
