package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestServiceCompletesOIDCFlowAndManagesSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 13, 8, 30, 0, 0, time.UTC)
	repository := &memoryRepository{}
	provider := &fakeProvider{}
	service := newTestService(t, repository, provider, now)

	start, err := service.BeginLogin(context.Background(), "/app/classes?filter=mine")
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	if start.AuthorizationURL != "https://identity.example/authorize" || start.BrowserBinding == "" {
		t.Fatalf("unexpected login start: %+v", start)
	}
	if provider.state == "" || provider.nonce == "" || provider.challenge == "" {
		t.Fatal("provider must receive state, nonce, and PKCE challenge")
	}

	provider.claims = ProviderClaims{
		Issuer:        "https://identity.example/",
		Subject:       "subject-123",
		Email:         "Student@Example.com",
		EmailVerified: true,
		DisplayName:   "",
		Locale:        "",
		Nonce:         provider.nonce,
		AuthTime:      now.Add(-time.Minute),
	}
	result, err := service.CompleteLogin(context.Background(), CallbackInput{
		State:          provider.state,
		BrowserBinding: start.BrowserBinding,
		Code:           "authorization-code",
		UserAgent:      "TutorHub test browser",
		RemoteAddress:  "203.0.113.42:51324",
	})
	if err != nil {
		t.Fatalf("complete login: %v", err)
	}
	if result.SessionToken == "" || result.CSRFToken == "" {
		t.Fatal("login must return opaque session and CSRF tokens")
	}
	if result.ReturnTo != "/app/classes?filter=mine" || !result.ExpiresAt.Equal(now.Add(8*time.Hour)) {
		t.Fatalf("unexpected login result: %+v", result)
	}
	if provider.code != "authorization-code" || provider.verifier == "" {
		t.Fatal("provider exchange must receive the authorization code and PKCE verifier")
	}
	if PKCEChallenge(provider.verifier) != provider.challenge {
		t.Fatal("stored PKCE verifier does not match the authorization challenge")
	}
	if repository.claims.Email != "student@example.com" ||
		repository.claims.DisplayName != "student" ||
		repository.claims.Locale != "vi" {
		t.Fatalf("provider claims were not normalized: %+v", repository.claims)
	}
	if repository.metadata.IPPrefix != "203.0.113.0/24" {
		t.Fatalf("unexpected IP prefix: %s", repository.metadata.IPPrefix)
	}

	principal, err := service.Authenticate(context.Background(), result.SessionToken)
	if err != nil || principal.User.Email != "student@example.com" {
		t.Fatalf("authenticate session: principal=%+v error=%v", principal, err)
	}
	if _, err := service.ValidateCSRF(
		context.Background(),
		result.SessionToken,
		"incorrect-token",
	); !errors.Is(err, ErrInvalidCSRFToken) {
		t.Fatalf("expected invalid CSRF token, got %v", err)
	}
	rotated, err := service.RotateCSRF(context.Background(), result.SessionToken)
	if err != nil {
		t.Fatalf("rotate CSRF: %v", err)
	}
	if _, err := service.ValidateCSRF(
		context.Background(),
		result.SessionToken,
		rotated.Token,
	); err != nil {
		t.Fatalf("validate rotated CSRF token: %v", err)
	}

	logoutURL, err := service.Logout(context.Background(), result.SessionToken)
	if err != nil || logoutURL != "https://identity.example/logout" {
		t.Fatalf("logout: url=%q error=%v", logoutURL, err)
	}
	if !repository.revoked {
		t.Fatal("logout must revoke the local session")
	}
	if _, err := service.CompleteLogin(context.Background(), CallbackInput{
		State:          provider.state,
		BrowserBinding: start.BrowserBinding,
		Code:           "replayed-code",
	}); !errors.Is(err, ErrInvalidAuthFlow) {
		t.Fatalf("expected replayed callback to fail, got %v", err)
	}
}

func TestServiceRejectsInvalidOIDCResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mutate     func(*fakeProvider)
		expected   error
		returnTo   string
		beginError error
	}{
		{
			name: "nonce mismatch",
			mutate: func(provider *fakeProvider) {
				provider.claims.Nonce = "different-nonce"
			},
			expected: ErrProviderExchange,
		},
		{
			name: "unverified email",
			mutate: func(provider *fakeProvider) {
				provider.claims.EmailVerified = false
			},
			expected: ErrVerifiedEmailRequired,
		},
		{
			name:       "external return target",
			returnTo:   "https://attacker.example/redirect",
			beginError: ErrInvalidReturnTo,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.July, 13, 9, 0, 0, 0, time.UTC)
			repository := &memoryRepository{}
			provider := &fakeProvider{}
			service := newTestService(t, repository, provider, now)
			start, err := service.BeginLogin(context.Background(), test.returnTo)
			if test.beginError != nil {
				if !errors.Is(err, test.beginError) {
					t.Fatalf("expected begin error %v, got %v", test.beginError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("begin login: %v", err)
			}

			provider.claims = validClaims(now, provider.nonce)
			test.mutate(provider)
			_, err = service.CompleteLogin(context.Background(), CallbackInput{
				State:          provider.state,
				BrowserBinding: start.BrowserBinding,
				Code:           "code",
			})
			if !errors.Is(err, test.expected) {
				t.Fatalf("expected %v, got %v", test.expected, err)
			}
		})
	}
}

func newTestService(
	t *testing.T,
	repository Repository,
	provider Provider,
	now time.Time,
) *Service {
	t.Helper()

	crypto, err := NewCrypto(bytes.Repeat([]byte{0x5a}, 32))
	if err != nil {
		t.Fatalf("create crypto: %v", err)
	}
	service, err := NewService(repository, provider, crypto, ServiceConfig{
		FlowTTL:            10 * time.Minute,
		SessionTTL:         8 * time.Hour,
		SessionAbsoluteTTL: 24 * time.Hour,
	}, func() time.Time { return now })
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	return service
}

func validClaims(now time.Time, nonce string) ProviderClaims {
	return ProviderClaims{
		Issuer:        "https://identity.example",
		Subject:       "subject-123",
		Email:         "student@example.com",
		EmailVerified: true,
		DisplayName:   "Student",
		Locale:        "vi",
		Nonce:         nonce,
		AuthTime:      now,
	}
}

type fakeProvider struct {
	state     string
	nonce     string
	challenge string
	code      string
	verifier  string
	claims    ProviderClaims
	err       error
}

func (provider *fakeProvider) AuthorizationURL(state, nonce, challenge string) string {
	provider.state = state
	provider.nonce = nonce
	provider.challenge = challenge
	return "https://identity.example/authorize"
}

func (provider *fakeProvider) ExchangeAndVerify(
	_ context.Context,
	code string,
	verifier string,
) (ProviderClaims, error) {
	provider.code = code
	provider.verifier = verifier
	return provider.claims, provider.err
}

func (*fakeProvider) EndSessionURL() string {
	return "https://identity.example/logout"
}

type memoryRepository struct {
	flow       CreateFlowParams
	consumed   bool
	claims     ProviderClaims
	metadata   SessionMetadata
	principal  Principal
	csrfHash   []byte
	revoked    bool
	revokeHash []byte
}

func (repository *memoryRepository) CreateFlow(
	_ context.Context,
	params CreateFlowParams,
) error {
	repository.flow = params
	return nil
}

func (repository *memoryRepository) ConsumeFlow(
	_ context.Context,
	stateHash []byte,
	browserBindingHash []byte,
	consumedAt time.Time,
) (StoredFlow, error) {
	if repository.consumed ||
		!bytes.Equal(stateHash, repository.flow.StateHash) ||
		!bytes.Equal(browserBindingHash, repository.flow.BrowserBindingHash) ||
		!consumedAt.Before(repository.flow.ExpiresAt) {
		return StoredFlow{}, ErrInvalidAuthFlow
	}
	repository.consumed = true
	return StoredFlow{
		NonceHash:              repository.flow.NonceHash,
		CodeVerifierCiphertext: repository.flow.CodeVerifierCiphertext,
		ReturnTo:               repository.flow.ReturnTo,
	}, nil
}

func (repository *memoryRepository) CreateAuthenticatedSession(
	_ context.Context,
	claims ProviderClaims,
	metadata SessionMetadata,
) (Principal, error) {
	repository.claims = claims
	repository.metadata = metadata
	repository.csrfHash = metadata.CSRFHash
	repository.principal = Principal{
		SessionID: uuid.MustParse("7f3f7634-c04d-4f42-afd5-e52a3bf673cb"),
		User: User{
			ID:          uuid.MustParse("ee9b4cdf-e1ee-4d79-aa5b-80b7c3aa7ea3"),
			Email:       claims.Email,
			DisplayName: claims.DisplayName,
			Locale:      claims.Locale,
		},
		Memberships: []Tenant{},
		Permissions: []string{},
	}
	return repository.principal, nil
}

func (repository *memoryRepository) GetSession(
	_ context.Context,
	tokenHash []byte,
	_ time.Time,
	_ time.Duration,
) (SessionRecord, error) {
	if repository.revoked || !bytes.Equal(tokenHash, repository.metadata.TokenHash) {
		return SessionRecord{}, ErrSessionNotFound
	}
	return SessionRecord{
		Principal: repository.principal,
		CSRFHash:  repository.csrfHash,
		ExpiresAt: repository.metadata.ExpiresAt,
	}, nil
}

func (repository *memoryRepository) RotateCSRF(
	_ context.Context,
	sessionID uuid.UUID,
	csrfHash []byte,
	_ time.Time,
) error {
	if sessionID != repository.principal.SessionID || repository.revoked {
		return ErrSessionNotFound
	}
	repository.csrfHash = append([]byte(nil), csrfHash...)
	return nil
}

func (repository *memoryRepository) RevokeSession(
	_ context.Context,
	tokenHash []byte,
	_ time.Time,
	reason string,
) error {
	if !bytes.Equal(tokenHash, repository.metadata.TokenHash) {
		return fmt.Errorf("unexpected session hash")
	}
	if reason != "user_logout" {
		return fmt.Errorf("unexpected revocation reason")
	}
	repository.revoked = true
	repository.revokeHash = append([]byte(nil), tokenHash...)
	return nil
}
