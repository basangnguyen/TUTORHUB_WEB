package identity

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const linkBindingPurpose = "link-tx-binding"

var _ FederationAPI = (*Service)(nil)

func (service *Service) ConfigureFederation(registry *ProviderRegistry, cfg FederationConfig) error {
	if registry == nil || strings.TrimSpace(cfg.LegalTermsVersion) == "" {
		return ErrAuthFoundationInput
	}
	if _, ok := service.repository.(FederationRepository); !ok {
		return ErrAuthFoundationStorage
	}
	service.registry, service.federation = registry, cfg
	return nil
}

func (service *Service) BeginOAuth(ctx context.Context, slug, returnTo, intent string, principal Principal) (LoginStart, error) {
	if _, err := service.registry.lookup(slug); err != nil {
		return LoginStart{}, err
	}
	if intent == "" {
		intent = flowPurposeLogin
	}
	if intent != flowPurposeLogin && intent != "reauth" {
		return LoginStart{}, ErrInvalidAuthFlow
	}
	normalized, err := NormalizeReturnTo(returnTo)
	if err != nil {
		return LoginStart{}, err
	}
	if intent == "reauth" {
		if slug == "facebook" {
			return LoginStart{}, ErrFederationReauthRequired
		}
		if err := validatePrincipal(principal); err != nil {
			return LoginStart{}, err
		}
	} else {
		principal = Principal{}
	}
	return service.beginProviderFlow(ctx, slug, normalized, intent, principal)
}

func (service *Service) BeginProviderIdentityLink(ctx context.Context, principal Principal, slug string) (LoginStart, error) {
	if slug == "facebook" {
		// Facebook linking follows the owner-bound intent issued after login.
		return LoginStart{}, ErrFederationProviderUnavailable
	}
	if _, err := service.registry.lookup(slug); err != nil {
		return LoginStart{}, err
	}
	if err := service.requireRecentAuthentication(principal); err != nil {
		return LoginStart{}, err
	}
	return service.beginProviderFlow(ctx, slug, identityLinkReturnTo, flowPurposeIdentityLink, principal)
}

func (service *Service) beginProviderFlow(ctx context.Context, slug, returnTo, purpose string, principal Principal) (LoginStart, error) {
	entry, err := service.registry.lookup(slug)
	if err != nil {
		return LoginStart{}, err
	}
	state, err := service.crypto.RandomToken()
	if err != nil {
		return LoginStart{}, ErrAuthFoundationStorage
	}
	binding, err := service.crypto.RandomToken()
	if err != nil {
		return LoginStart{}, ErrAuthFoundationStorage
	}
	nonce, err := service.crypto.RandomToken()
	if err != nil {
		return LoginStart{}, ErrAuthFoundationStorage
	}
	verifier, err := service.crypto.PKCEVerifier()
	if err != nil {
		return LoginStart{}, ErrAuthFoundationStorage
	}
	ciphertext, err := service.crypto.Encrypt(verifier)
	if err != nil {
		return LoginStart{}, ErrAuthFoundationStorage
	}
	now := service.clock().UTC()
	params := CreateFlowParams{ProviderSlug: slug, StateHash: service.crypto.Digest(statePurpose, state), BrowserBindingHash: service.crypto.Digest(browserBindingPurpose, binding),
		NonceHash: service.crypto.Digest(noncePurpose, nonce), CodeVerifierCiphertext: ciphertext, ReturnTo: returnTo, Purpose: purpose,
		UserID: principal.User.ID, SessionID: principal.SessionID, CreatedAt: now, ExpiresAt: now.Add(service.config.FlowTTL)}
	if err := service.repository.CreateFlow(ctx, params); err != nil {
		return LoginStart{}, ErrAuthFoundationStorage
	}
	authorization := entry.Provider.AuthorizationURL(state, nonce, PKCEChallenge(verifier))
	if purpose == "reauth" {
		parsed, err := url.Parse(authorization)
		if err != nil {
			return LoginStart{}, ErrProviderExchange
		}
		query := parsed.Query()
		query.Set("prompt", "login")
		query.Set("max_age", "0")
		parsed.RawQuery = query.Encode()
		authorization = parsed.String()
	}
	return LoginStart{AuthorizationURL: authorization, BrowserBinding: binding, ExpiresAt: params.ExpiresAt}, nil
}

func (service *Service) CompleteOAuth(ctx context.Context, slug string, input CallbackInput) (OAuthResult, error) {
	entry, err := service.registry.lookup(slug)
	if err != nil {
		return OAuthResult{}, err
	}
	repository, ok := service.repository.(FederationRepository)
	if !ok {
		return OAuthResult{}, ErrAuthFoundationStorage
	}
	if strings.TrimSpace(input.State) == "" || strings.TrimSpace(input.BrowserBinding) == "" || strings.TrimSpace(input.Code) == "" {
		return OAuthResult{}, ErrInvalidAuthFlow
	}
	now := service.clock().UTC()
	flow, err := repository.ConsumeProviderFlow(ctx, slug, service.crypto.Digest(statePurpose, input.State), service.crypto.Digest(browserBindingPurpose, input.BrowserBinding), now)
	if err != nil {
		return OAuthResult{}, err
	}
	if flow.ProviderSlug != slug || (input.Issuer != "" && !entry.matchesIssuer(input.Issuer)) {
		return OAuthResult{}, ErrInvalidAuthFlow
	}
	verifier, err := service.crypto.Decrypt(flow.CodeVerifierCiphertext)
	if err != nil {
		return OAuthResult{}, ErrInvalidAuthFlow
	}
	claims, err := entry.Provider.ExchangeAndVerify(ctx, input.Code, verifier)
	if err != nil {
		return OAuthResult{}, ErrProviderExchange
	}
	if !entry.matchesIssuer(strings.TrimRight(claims.Issuer, "/")) || !service.crypto.EqualDigest(flow.NonceHash, noncePurpose, claims.Nonce) {
		return OAuthResult{}, ErrProviderExchange
	}
	if flow.Purpose == "reauth" {
		if slug == "facebook" {
			return OAuthResult{}, ErrFederationReauthRequired
		}
		if flow.UserID == uuid.Nil || flow.SessionID == uuid.Nil || flow.CreatedAt.IsZero() || claims.AuthTime.IsZero() || claims.AuthTime.Before(flow.CreatedAt.Truncate(time.Second)) || claims.AuthTime.After(now.Add(time.Minute)) {
			return OAuthResult{}, ErrFederationReauthRequired
		}
	}
	claims.ProviderSlug = slug
	claims, err = normalizeFederationClaims(claims, now, slug == "facebook")
	if err != nil {
		return OAuthResult{}, err
	}
	if len(claims.ProviderOrgID) > 200 {
		return OAuthResult{}, ErrProviderExchange
	}
	returnTo, err := NormalizeReturnTo(flow.ReturnTo)
	if err != nil {
		return OAuthResult{}, ErrInvalidAuthFlow
	}
	flow.ReturnTo = returnTo
	if flow.Purpose != "login" && flow.Purpose != "identity_link" && flow.Purpose != "reauth" {
		return OAuthResult{}, ErrInvalidAuthFlow
	}
	token, csrf, rotation, err := service.newFederationRotation(now)
	if err != nil {
		return OAuthResult{}, err
	}
	if flow.Purpose == "reauth" {
		principal, err := repository.ReauthenticateFederated(ctx, FederationReauthParams{Flow: flow, Claims: claims, Rotation: rotation, SessionAbsoluteTTL: service.config.SessionAbsoluteTTL})
		if err != nil {
			return OAuthResult{}, err
		}
		return OAuthResult{Provider: slug, LoginResult: LoginResult{SessionToken: token, CSRFToken: csrf, ExpiresAt: principal.CookieExpiresAt, ReturnTo: returnTo}}, nil
	}
	payload := FederationIdentityPayload{ProviderSlug: slug, Issuer: claims.Issuer, Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, DisplayName: claims.DisplayName, ProviderOrgID: claims.ProviderOrgID, ReturnTo: returnTo}
	raw, err := json.Marshal(payload)
	if err != nil {
		return OAuthResult{}, ErrAuthFoundationStorage
	}
	ciphertext, err := service.crypto.Encrypt(string(raw))
	if err != nil {
		return OAuthResult{}, ErrAuthFoundationStorage
	}
	binding, err := service.crypto.RandomToken()
	if err != nil {
		return OAuthResult{}, ErrAuthFoundationStorage
	}
	intentExpiry := now.Add(AuthTransactionTTL)
	metadata := SessionMetadata{TokenHash: rotation.TokenHash, CSRFHash: rotation.CSRFHash, UserAgentHash: service.crypto.Digest(userAgentPurpose, input.UserAgent), DeviceLabel: DeviceLabel(input.UserAgent), IPPrefix: IPPrefix(input.RemoteAddress), CreatedAt: now, ExpiresAt: now.Add(service.config.SessionTTL), AbsoluteAt: now.Add(service.config.SessionAbsoluteTTL)}
	record, err := repository.CompleteFederatedLogin(ctx, FederationLoginParams{
		Claims: claims, ProviderSlug: slug, Flow: flow, Metadata: metadata,
		SignupEnabled: service.federation.SignupEnabled, LegalTermsVersion: service.federation.LegalTermsVersion,
		LinkBindingHash: service.crypto.Digest(linkBindingPurpose, binding), LinkPayloadCiphertext: ciphertext,
		RegistrationBindingHash: service.crypto.Digest(registrationBindingPurpose, binding),
		ProviderSubjectDigest:   service.crypto.ProviderSubjectDigest(claims.Issuer, claims.Subject),
		LinkExpiresAt:           intentExpiry, SessionAbsoluteTTL: service.config.SessionAbsoluteTTL,
	})
	if err != nil {
		return OAuthResult{}, err
	}
	if record.LinkPending {
		return OAuthResult{Provider: slug, LinkPending: true, LinkBinding: binding, LinkExpiresAt: intentExpiry}, nil
	}
	if record.RegistrationPending {
		return OAuthResult{Provider: slug, RegistrationPending: true, RegistrationBinding: binding, RegistrationExpiresAt: intentExpiry}, nil
	}
	return OAuthResult{Provider: slug, LoginResult: LoginResult{SessionToken: token, CSRFToken: csrf, ExpiresAt: record.Principal.CookieExpiresAt, ReturnTo: returnTo, IdentityLinked: flow.Purpose == flowPurposeIdentityLink}}, nil
}

func (service *Service) newFederationRotation(now time.Time) (string, string, SessionRotation, error) {
	token, err := service.crypto.RandomToken()
	if err != nil {
		return "", "", SessionRotation{}, ErrAuthFoundationStorage
	}
	csrf, err := service.crypto.RandomToken()
	if err != nil {
		return "", "", SessionRotation{}, ErrAuthFoundationStorage
	}
	return token, csrf, SessionRotation{TokenHash: service.crypto.Digest(sessionPurpose, token), CSRFHash: service.crypto.Digest(csrfPurpose, csrf), RotatedAt: now}, nil
}

func (service *Service) readFederationIntent(ctx context.Context, binding string) (FederationRepository, StoredLinkIntent, FederationIdentityPayload, error) {
	repository, ok := service.repository.(FederationRepository)
	if !ok {
		return nil, StoredLinkIntent{}, FederationIdentityPayload{}, ErrAuthFoundationStorage
	}
	if len(binding) != 43 {
		return nil, StoredLinkIntent{}, FederationIdentityPayload{}, ErrLinkIntentUnavailable
	}
	intent, err := repository.ReadLinkIntent(ctx, service.crypto.Digest(linkBindingPurpose, binding), service.clock().UTC())
	if err != nil {
		return nil, intent, FederationIdentityPayload{}, err
	}
	raw, err := service.crypto.Decrypt(intent.PayloadCiphertext)
	if err != nil {
		return nil, intent, FederationIdentityPayload{}, ErrLinkIntentUnavailable
	}
	var payload FederationIdentityPayload
	if json.Unmarshal([]byte(raw), &payload) != nil || payload.Issuer == "" || payload.Subject == "" ||
		(payload.ProviderSlug != "facebook" && (!payload.EmailVerified || payload.Email == "")) {
		return nil, intent, payload, ErrLinkIntentUnavailable
	}
	entry, err := service.registry.lookup(payload.ProviderSlug)
	if err != nil {
		return nil, intent, payload, ErrLinkIntentUnavailable
	}
	if !entry.matchesIssuer(payload.Issuer) {
		return nil, intent, payload, ErrLinkIntentUnavailable
	}
	normalized, err := NormalizeReturnTo(payload.ReturnTo)
	if err != nil || normalized != payload.ReturnTo {
		return nil, intent, payload, ErrLinkIntentUnavailable
	}
	return repository, intent, payload, nil
}

func (service *Service) GetLinkIntent(ctx context.Context, binding string) (LinkIntentView, error) {
	_, intent, payload, err := service.readFederationIntent(ctx, binding)
	if err != nil {
		return LinkIntentView{}, err
	}
	seconds := int(intent.ExpiresAt.Sub(service.clock()).Seconds())
	if seconds <= 0 {
		return LinkIntentView{}, ErrLinkIntentUnavailable
	}
	email := payload.Email
	if payload.ProviderSlug == "facebook" {
		email = intent.TargetEmail
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return LinkIntentView{}, ErrLinkIntentUnavailable
	}
	local := []rune(parts[0])
	prefix := ""
	if len(local) > 1 {
		prefix = string(local[0])
	}
	return LinkIntentView{Provider: payload.ProviderSlug, MaskedEmail: prefix + "***@" + parts[1], ExpiresInSeconds: seconds}, nil
}

func (service *Service) ConfirmLinkIntent(ctx context.Context, principal Principal, binding string) (LinkIntentResult, error) {
	if err := validatePrincipal(principal); err != nil {
		return LinkIntentResult{}, err
	}
	repository, intent, payload, err := service.readFederationIntent(ctx, binding)
	if err != nil {
		return LinkIntentResult{}, err
	}
	if intent.UserID != principal.User.ID {
		return LinkIntentResult{}, ErrLinkIntentMismatch
	}
	token, csrf, rotation, err := service.newFederationRotation(service.clock().UTC())
	if err != nil {
		return LinkIntentResult{}, err
	}
	rotation.ExpectedContextVersion = principal.ContextVersion
	updated, err := repository.ConfirmLinkIntent(ctx, LinkIntentConfirmParams{Principal: principal, BindingHash: service.crypto.Digest(linkBindingPurpose, binding), Intent: intent, Payload: payload, Rotation: rotation, SessionAbsoluteTTL: service.config.SessionAbsoluteTTL})
	if err != nil {
		return LinkIntentResult{}, err
	}
	return LinkIntentResult{SessionToken: token, CSRFToken: csrf, CookieExpiresAt: updated.CookieExpiresAt, ReturnTo: payload.ReturnTo}, nil
}

func (service *Service) CancelLinkIntent(ctx context.Context, binding string) error {
	if len(binding) != 43 {
		return ErrLinkIntentUnavailable
	}
	repository, ok := service.repository.(FederationRepository)
	if !ok {
		return ErrAuthFoundationStorage
	}
	return repository.CancelLinkIntent(ctx, service.crypto.Digest(linkBindingPurpose, binding), service.clock().UTC())
}
