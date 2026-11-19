package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type federationMemoryRepository struct {
	memoryRepository
	login                                 FederationLoginParams
	loginCalls, confirmCalls, reauthCalls int
	pending                               bool
	registrationPending                   bool
	intent                                StoredLinkIntent
	intentHash                            []byte
	intentConsumed                        bool
	confirm                               LinkIntentConfirmParams
	reauth                                FederationReauthParams
}

func (repo *federationMemoryRepository) ConsumeFlow(ctx context.Context, state, binding []byte, now time.Time) (StoredFlow, error) {
	return repo.ConsumeProviderFlow(ctx, "oidc", state, binding, now)
}
func (repo *federationMemoryRepository) ConsumeProviderFlow(ctx context.Context, slug string, state, binding []byte, now time.Time) (StoredFlow, error) {
	if repo.flow.ProviderSlug != slug {
		return StoredFlow{}, ErrInvalidAuthFlow
	}
	flow, err := repo.memoryRepository.ConsumeFlow(ctx, state, binding, now)
	flow.ProviderSlug = repo.flow.ProviderSlug
	flow.CreatedAt = repo.flow.CreatedAt
	return flow, err
}
func (repo *federationMemoryRepository) CompleteFederatedLogin(_ context.Context, input FederationLoginParams) (FederationLoginRecord, error) {
	repo.login = input
	repo.loginCalls++
	if repo.registrationPending {
		return FederationLoginRecord{RegistrationPending: true}, nil
	}
	if repo.pending {
		repo.intent = StoredLinkIntent{ID: uuid.New(), UserID: repo.principal.User.ID, PayloadCiphertext: input.LinkPayloadCiphertext, CreatedAt: input.Metadata.CreatedAt, ExpiresAt: input.LinkExpiresAt}
		repo.intentHash = input.LinkBindingHash
		return FederationLoginRecord{LinkPending: true}, nil
	}
	return FederationLoginRecord{Principal: repo.principal}, nil
}

type microsoftFederationStub struct {
	*fakeProvider
	policy MicrosoftProvider
}

func (p *microsoftFederationStub) matchesMicrosoftIssuer(issuer string) bool {
	return p.policy.matchesMicrosoftIssuer(issuer)
}

func microsoftTestRegistry(t *testing.T, provider Provider, others ...ProviderRegistration) *ProviderRegistry {
	t.Helper()
	entries := append(others, ProviderRegistration{Slug: "microsoft", Issuer: MicrosoftAuthorityURL, Provider: provider})
	registry, err := NewProviderRegistry(entries...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestMicrosoftFederationBindsProviderTenantAndEmailTrust(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"trusted", "pending OTP", "malformed email", "email collision", "trusted settings link", "unverified settings link", "malformed email settings link", "different callback tenant", "wrong state", "wrong nonce", "noncanonical subject"} {
		t.Run(scenario, func(t *testing.T) {
			svc, repo, _, _, now := federationServiceFixture(t)
			p := &microsoftFederationStub{fakeProvider: &fakeProvider{}, policy: MicrosoftProvider{tenantMode: "common"}}
			svc.registry = microsoftTestRegistry(t, p)
			if _, err := svc.BeginOAuth(ctx, "microsoft", "/app", "reauth", repo.principal); !errors.Is(err, ErrFederationReauthRequired) {
				t.Fatal("Microsoft accepted federated reauth")
			}
			var start LoginStart
			var err error
			if strings.HasSuffix(scenario, "settings link") {
				start, err = svc.BeginProviderIdentityLink(ctx, repo.principal, "microsoft")
			} else {
				start, err = svc.BeginOAuth(ctx, "microsoft", "/app", "login", Principal{})
			}
			if err != nil {
				t.Fatal(err)
			}
			p.claims = validClaims(now, p.nonce)
			p.claims.ProviderOrgID, p.claims.ProviderStableID = uuid.NewString(), uuid.NewString()
			p.claims.Issuer = microsoftIssuer(p.claims.ProviderOrgID)
			in := CallbackInput{Code: "code", State: p.state, BrowserBinding: start.BrowserBinding, Issuer: p.claims.Issuer}
			var want error
			switch scenario {
			case "pending OTP":
				p.claims.EmailVerified = false
				repo.registrationPending = true
			case "malformed email":
				p.claims.Email = "invalid claim"
				repo.registrationPending = true
			case "email collision":
				repo.pending = true
			case "unverified settings link":
				p.claims.EmailVerified, want = false, ErrVerifiedEmailRequired
			case "malformed email settings link":
				p.claims.Email, want = "invalid claim", ErrVerifiedEmailRequired
			case "different callback tenant":
				in.Issuer, want = microsoftIssuer(uuid.NewString()), ErrProviderExchange
			case "wrong state":
				in.State, want = "other", ErrInvalidAuthFlow
			case "wrong nonce":
				p.claims.Nonce, want = "other", ErrProviderExchange
			case "noncanonical subject":
				p.claims.Subject, want = " subject ", ErrProviderExchange
			}
			result, err := svc.CompleteOAuth(ctx, "microsoft", in)
			if !errors.Is(err, want) || (want != nil && repo.loginCalls != 0) {
				t.Fatalf("callback proof = %v; want %v", err, want)
			}
			if want != nil {
				return
			}
			raw, err := svc.crypto.Decrypt(repo.login.LinkPayloadCiphertext)
			var payload FederationIdentityPayload
			if err != nil || json.Unmarshal([]byte(raw), &payload) != nil || payload.ProviderSlug != "microsoft" || payload.Issuer != p.claims.Issuer || payload.ProviderOrgID != p.claims.ProviderOrgID || payload.ProviderStableID != p.claims.ProviderStableID || payload.EmailVerified != (p.claims.EmailVerified && scenario != "malformed email") {
				t.Fatal("provider identity provenance lost in encrypted transaction")
			}
			if repo.registrationPending {
				if !result.RegistrationPending || len(result.RegistrationBinding) != 43 || result.LinkPending || result.SessionToken != "" {
					t.Fatal("invalid registration result")
				}
			} else if repo.pending {
				repo.intent.TargetEmail = p.claims.Email
				view, err := svc.GetLinkIntent(ctx, result.LinkBinding)
				if err != nil || !result.LinkPending || result.SessionToken != "" || view.Provider != "microsoft" || view.MaskedEmail != "s***@example.com" {
					t.Fatal("invalid owner-bound intent", err)
				}
				if _, err := svc.ConfirmLinkIntent(ctx, repo.principal, result.LinkBinding); err != nil || repo.confirm.Payload.ProviderStableID != p.claims.ProviderStableID {
					t.Fatal("link confirmation lost provider metadata", err)
				}
			} else if result.SessionToken == "" || (scenario == "trusted settings link" && (!result.IdentityLinked || repo.login.Flow.UserID != repo.principal.User.ID || repo.login.Flow.SessionID != repo.principal.SessionID)) {
				t.Fatal("trusted callback lost its login or settings session binding")
			}
		})
	}
}

func TestMicrosoftFlowIsolationAcrossCallbackRoutes(t *testing.T) {
	ctx := context.Background()
	for _, origin := range []string{"microsoft", "oidc", "google", "facebook", "legacy"} {
		t.Run(origin, func(t *testing.T) {
			svc, repo, oidc, google, _ := federationServiceFixture(t)
			fb := &fakeProvider{}
			ms := &microsoftFederationStub{fakeProvider: &fakeProvider{}, policy: MicrosoftProvider{tenantMode: "common"}}
			svc.registry = microsoftTestRegistry(t, ms, ProviderRegistration{Slug: "oidc", Issuer: "https://identity.example", Provider: oidc}, ProviderRegistration{Slug: "google", Issuer: GoogleIssuerURL, Provider: google}, ProviderRegistration{Slug: "facebook", Issuer: FacebookIssuerURL, Provider: fb})
			providers := map[string]*fakeProvider{"oidc": oidc, "legacy": oidc, "google": google, "facebook": fb, "microsoft": ms.fakeProvider}
			var start LoginStart
			var err error
			if origin == "legacy" {
				start, err = svc.BeginLogin(ctx, "/app")
			} else {
				start, err = svc.BeginOAuth(ctx, origin, "/app", "login", Principal{})
			}
			if err != nil {
				t.Fatal(err)
			}
			in := CallbackInput{Code: "code", State: providers[origin].state, BrowserBinding: start.BrowserBinding}
			for dest, p := range providers {
				if (origin == "microsoft") == (dest == "microsoft") {
					continue
				}
				if dest == "legacy" {
					_, err = svc.CompleteLogin(ctx, in)
				} else {
					_, err = svc.CompleteOAuth(ctx, dest, in)
				}
				if !errors.Is(err, ErrInvalidAuthFlow) || p.code != "" || repo.loginCalls != 0 || repo.consumed {
					t.Fatalf("%s flow reached %s exchange", origin, dest)
				}
			}
		})
	}
}
func (repo *federationMemoryRepository) ReadLinkIntent(_ context.Context, binding []byte, now time.Time) (StoredLinkIntent, error) {
	if repo.intentConsumed || !bytes.Equal(binding, repo.intentHash) || !repo.intent.ExpiresAt.After(now) {
		return StoredLinkIntent{}, ErrLinkIntentUnavailable
	}
	return repo.intent, nil
}
func (repo *federationMemoryRepository) ConfirmLinkIntent(_ context.Context, input LinkIntentConfirmParams) (Principal, error) {
	repo.confirmCalls++
	repo.confirm = input
	repo.intentConsumed = true
	return repo.principal, nil
}
func (repo *federationMemoryRepository) CancelLinkIntent(_ context.Context, binding []byte, now time.Time) error {
	if _, err := repo.ReadLinkIntent(context.Background(), binding, now); err != nil {
		return err
	}
	repo.intentConsumed = true
	return nil
}
func (repo *federationMemoryRepository) ReauthenticateFederated(_ context.Context, input FederationReauthParams) (Principal, error) {
	repo.reauthCalls++
	repo.reauth = input
	return repo.principal, nil
}

func federationServiceFixture(t *testing.T) (*Service, *federationMemoryRepository, *fakeProvider, *fakeProvider, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	repo := &federationMemoryRepository{memoryRepository: memoryRepository{principal: Principal{User: User{ID: uuid.New()}, SessionID: uuid.New(), ContextVersion: 7, AssuranceLevel: 1, AuthenticatedAt: now, CookieExpiresAt: now.Add(30 * 24 * time.Hour)}}}
	legacy, google := &fakeProvider{}, &fakeProvider{}
	service := newTestService(t, repo, legacy, now)
	registry, err := NewProviderRegistry(ProviderRegistration{Slug: "oidc", Issuer: "https://identity.example", Provider: legacy}, ProviderRegistration{Slug: "google", Issuer: GoogleIssuerURL, Provider: google})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConfigureFederation(registry, FederationConfig{SignupEnabled: true, LegalTermsVersion: "terms-v1"}); err != nil {
		t.Fatal(err)
	}
	return service, repo, legacy, google, now
}

func federationCallback(provider *fakeProvider, start LoginStart, now time.Time) CallbackInput {
	provider.claims = validClaims(now, provider.nonce)
	provider.claims.Issuer = GoogleIssuerURL
	provider.claims.ProviderOrgID = "school.example"
	return CallbackInput{State: provider.state, BrowserBinding: start.BrowserBinding, Code: "code", UserAgent: "Chrome/130 Windows", RemoteAddress: "203.0.113.9:1234"}
}

func TestFederationRegistryAndStartGuards(t *testing.T) {
	service, repo, _, _, now := federationServiceFixture(t)
	for _, slug := range []string{"unknown", "facebook", "GOOGLE"} {
		if _, err := service.BeginOAuth(context.Background(), slug, "/app", "login", Principal{}); !errors.Is(err, ErrFederationProviderUnavailable) {
			t.Fatal("unconfigured provider accepted")
		}
	}
	if _, err := service.BeginOAuth(context.Background(), "google", "https://evil.example", "login", Principal{}); err == nil {
		t.Fatal("external redirect accepted")
	}
	if _, err := service.BeginOAuth(context.Background(), "google", "/app", "reauth", Principal{}); !errors.Is(err, ErrSessionNotFound) {
		t.Fatal("anonymous reauth accepted")
	}
	start, err := service.BeginOAuth(context.Background(), "google", "/app", "reauth", repo.principal)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(start.AuthorizationURL)
	if parsed.Query().Get("prompt") != "login" || parsed.Query().Get("max_age") != "0" || repo.flow.UserID != repo.principal.User.ID || repo.flow.SessionID != repo.principal.SessionID || repo.flow.ProviderSlug != "google" {
		t.Fatal("reauth not bound or prompt missing")
	}
	stale := repo.principal
	stale.AuthenticatedAt = now.Add(-11 * time.Minute)
	if _, err := service.BeginProviderIdentityLink(context.Background(), stale, "google"); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatal("settings link omitted recent auth")
	}
	if _, err := service.BeginProviderIdentityLink(context.Background(), repo.principal, "google"); err != nil || repo.flow.Purpose != "identity_link" {
		t.Fatal("settings flow unavailable")
	}
	empty, err := NewProviderRegistry()
	if err != nil || service.ConfigureFederation(empty, FederationConfig{LegalTermsVersion: "test"}) != nil {
		t.Fatal("native-only startup failed")
	}
	if _, err := NewProviderRegistry(ProviderRegistration{Slug: "google", Issuer: GoogleIssuerURL}); err == nil {
		t.Fatal("nil provider accepted")
	}
}

func TestFederationFlowRejectsMixUpBeforeExchange(t *testing.T) {
	for _, destination := range []string{"legacy", "oidc"} {
		t.Run(destination, func(t *testing.T) {
			service, repo, legacy, google, now := federationServiceFixture(t)
			start, err := service.BeginOAuth(context.Background(), "google", "/app", "login", Principal{})
			if err != nil {
				t.Fatal(err)
			}
			input := federationCallback(google, start, now)
			if destination == "legacy" {
				_, err = service.CompleteLogin(context.Background(), input)
			} else {
				_, err = service.CompleteOAuth(context.Background(), "oidc", input)
			}
			if !errors.Is(err, ErrInvalidAuthFlow) || legacy.code != "" || google.code != "" || repo.loginCalls != 0 {
				t.Fatal("mixed-up callback exchanged or authenticated")
			}
		})
	}
	service, _, legacy, google, now := federationServiceFixture(t)
	start, err := service.BeginLogin(context.Background(), "/app")
	if err != nil {
		t.Fatal(err)
	}
	input := federationCallback(legacy, start, now)
	if _, err := service.CompleteOAuth(context.Background(), "google", input); !errors.Is(err, ErrInvalidAuthFlow) || google.code != "" {
		t.Fatal("OIDC flow accepted by Google callback")
	}
}

func TestFederationCallbackProofAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*CallbackInput, *fakeProvider)
		want   error
	}{
		{"wrong state", func(i *CallbackInput, _ *fakeProvider) { i.State = "wrong" }, ErrInvalidAuthFlow},
		{"wrong binding", func(i *CallbackInput, _ *fakeProvider) { i.BrowserBinding = "wrong" }, ErrInvalidAuthFlow},
		{"callback issuer", func(i *CallbackInput, _ *fakeProvider) { i.Issuer = "https://evil.example" }, ErrInvalidAuthFlow},
		{"nonce", func(_ *CallbackInput, p *fakeProvider) { p.claims.Nonce = "wrong" }, ErrProviderExchange},
		{"claim issuer", func(_ *CallbackInput, p *fakeProvider) { p.claims.Issuer = "https://evil.example" }, ErrProviderExchange},
		{"unverified email", func(_ *CallbackInput, p *fakeProvider) { p.claims.EmailVerified = false }, ErrVerifiedEmailRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, repo, _, google, now := federationServiceFixture(t)
			start, _ := service.BeginOAuth(context.Background(), "google", "/app", "login", Principal{})
			input := federationCallback(google, start, now)
			tc.mutate(&input, google)
			if _, err := service.CompleteOAuth(context.Background(), "google", input); !errors.Is(err, tc.want) || repo.loginCalls != 0 {
				t.Fatalf("proof error %v", err)
			}
		})
	}
	service, repo, _, google, now := federationServiceFixture(t)
	start, _ := service.BeginOAuth(context.Background(), "google", "/app", "login", Principal{})
	input := federationCallback(google, start, now)
	result, err := service.CompleteOAuth(context.Background(), "google", input)
	if err != nil || result.SessionToken == "" || result.CSRFToken == "" || repo.login.Claims.ProviderSlug != "google" || repo.login.Metadata.DeviceLabel != "Chrome · Windows" || repo.login.LegalTermsVersion != "terms-v1" || PKCEChallenge(google.verifier) != google.challenge {
		t.Fatal("successful callback metadata mismatch")
	}
	if _, err := service.CompleteOAuth(context.Background(), "google", input); !errors.Is(err, ErrInvalidAuthFlow) {
		t.Fatal("flow replayed")
	}
}

func TestFederationLinkIntentIsSeparateEncryptedAndOwnerBound(t *testing.T) {
	service, repo, _, google, now := federationServiceFixture(t)
	repo.pending = true
	start, _ := service.BeginOAuth(context.Background(), "google", "/app/classes", "login", Principal{})
	input := federationCallback(google, start, now)
	result, err := service.CompleteOAuth(context.Background(), "google", input)
	if err != nil || !result.LinkPending || result.SessionToken != "" || result.CSRFToken != "" || len(result.LinkBinding) != 43 || bytes.Contains(repo.intent.PayloadCiphertext, []byte(google.claims.Email)) {
		t.Fatal("link collision created session or exposed payload")
	}
	if LinkTransactionCookieName(true) == AuthTransactionCookieName(true) || LinkTransactionCookieName(true) != "__Host-tutorhub_link_tx" {
		t.Fatal("link intent shares OTP cookie")
	}
	view, err := service.GetLinkIntent(context.Background(), result.LinkBinding)
	if err != nil || view.MaskedEmail == google.claims.Email || view.Provider != "google" || view.ExpiresInSeconds != 900 {
		t.Fatal("intent preview leaked or lost binding")
	}
	wrong := repo.principal
	wrong.User.ID = uuid.New()
	if _, err := service.ConfirmLinkIntent(context.Background(), wrong, result.LinkBinding); !errors.Is(err, ErrLinkIntentMismatch) || repo.confirmCalls != 0 {
		t.Fatal("foreign owner confirmed")
	}
	confirmed, err := service.ConfirmLinkIntent(context.Background(), repo.principal, result.LinkBinding)
	if err != nil || confirmed.SessionToken == "" || !confirmed.CookieExpiresAt.Equal(repo.principal.CookieExpiresAt) || confirmed.ReturnTo != "/app/classes" || repo.confirm.Rotation.ExpectedContextVersion != 7 || repo.confirm.Payload.ProviderOrgID != "school.example" {
		t.Fatal("confirmation lost metadata or lifetime")
	}
	if _, err := service.ConfirmLinkIntent(context.Background(), repo.principal, result.LinkBinding); !errors.Is(err, ErrLinkIntentUnavailable) {
		t.Fatal("confirmed intent replayed")
	}
}

func TestFederationIntentCancelExpiryAndProviderKillSwitch(t *testing.T) {
	for _, scenario := range []string{"cancel", "expired", "disabled"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, _, google, now := federationServiceFixture(t)
			repo.pending = true
			start, _ := service.BeginOAuth(context.Background(), "google", "/app", "login", Principal{})
			result, err := service.CompleteOAuth(context.Background(), "google", federationCallback(google, start, now))
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "cancel":
				if service.CancelLinkIntent(context.Background(), result.LinkBinding) != nil {
					t.Fatal("cancel failed")
				}
			case "expired":
				repo.intent.ExpiresAt = now
			case "disabled":
				service.registry, _ = NewProviderRegistry()
			}
			if _, err := service.GetLinkIntent(context.Background(), result.LinkBinding); !errors.Is(err, ErrLinkIntentUnavailable) {
				t.Fatal("terminal or disabled intent remained usable")
			}
			if _, err := service.ConfirmLinkIntent(context.Background(), repo.principal, result.LinkBinding); !errors.Is(err, ErrLinkIntentUnavailable) || repo.confirmCalls != 0 {
				t.Fatal("terminal intent confirmed")
			}
		})
	}
}

func TestFederationReauthRequiresActualFreshProviderAuthTime(t *testing.T) {
	for _, scenario := range []string{"missing", "stale", "future", "valid"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, _, google, now := federationServiceFixture(t)
			start, _ := service.BeginOAuth(context.Background(), "google", "/app/settings", "reauth", repo.principal)
			input := federationCallback(google, start, now)
			switch scenario {
			case "missing":
				google.claims.AuthTime = time.Time{}
			case "stale":
				google.claims.AuthTime = now.Add(-time.Minute)
			case "future":
				google.claims.AuthTime = now.Add(2 * time.Minute)
			}
			result, err := service.CompleteOAuth(context.Background(), "google", input)
			if scenario != "valid" {
				if !errors.Is(err, ErrFederationReauthRequired) || repo.reauthCalls != 0 {
					t.Fatal("unproven provider authentication accepted")
				}
				return
			}
			if err != nil || repo.reauthCalls != 1 || !result.ExpiresAt.Equal(repo.principal.CookieExpiresAt) || result.SessionToken == "" || repo.reauth.Flow.UserID != repo.principal.User.ID {
				t.Fatal("reauth lost session binding or persistent lifetime")
			}
		})
	}
}

func TestFederationPayloadsAndResultsRedactLogging(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	for _, value := range []any{OAuthResult{LinkBinding: "secret-link", LoginResult: LoginResult{SessionToken: "secret-session"}}, LinkIntentResult{SessionToken: "secret-session", CSRFToken: "secret-csrf"}, FederationIdentityPayload{Email: "private@example.test", Subject: "private-subject"}} {
		logger.Info("safe", "value", value)
		fmt.Fprint(&output, value)
	}
	if strings.Contains(output.String(), "secret-") || strings.Contains(output.String(), "private@") {
		t.Fatal("sensitive federation data logged")
	}
	service, repo, _, google, now := federationServiceFixture(t)
	repo.pending = true
	start, _ := service.BeginOAuth(context.Background(), "google", "/app", "login", Principal{})
	result, _ := service.CompleteOAuth(context.Background(), "google", federationCallback(google, start, now))
	raw, _ := service.crypto.Decrypt(repo.intent.PayloadCiphertext)
	var payload FederationIdentityPayload
	if json.Unmarshal([]byte(raw), &payload) != nil || payload.ProviderSlug != "google" || payload.ReturnTo != "/app" || result.LinkBinding == "" {
		t.Fatal("encrypted intent payload lost claims")
	}
}
