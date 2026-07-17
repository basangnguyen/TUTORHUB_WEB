package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
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

func TestServiceCreatesAndSwitchesTenantWithSessionRotation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 14, 8, 0, 0, 0, time.UTC)
	repository := &memoryRepository{
		principal: Principal{
			SessionID: uuid.MustParse("7f3f7634-c04d-4f42-afd5-e52a3bf673cb"),
			User: User{
				ID:          uuid.MustParse("ee9b4cdf-e1ee-4d79-aa5b-80b7c3aa7ea3"),
				Email:       "owner@example.com",
				DisplayName: "Owner",
			},
			Memberships: []Tenant{},
			Permissions: []string{},
		},
		metadata: SessionMetadata{ExpiresAt: now.Add(8 * time.Hour)},
	}
	service := newTestService(t, repository, &fakeProvider{}, now)

	created, err := service.CreateTenant(
		context.Background(),
		repository.principal,
		CreateTenantInput{Name: "  Khoa   Công nghệ thông tin ", Slug: "KMA-LAB"},
	)
	if err != nil {
		t.Fatalf("create first tenant: %v", err)
	}
	if repository.createdTenant.Name != "Khoa Công nghệ thông tin" ||
		repository.createdTenant.Slug != "kma-lab" {
		t.Fatalf("tenant input was not normalized: %+v", repository.createdTenant)
	}
	if created.Principal.ActiveTenant == nil ||
		created.Principal.ActiveTenant.Role != "org_admin" ||
		!testContainsPermission(created.Principal.Permissions, "tenant.manage") {
		t.Fatalf("unexpected tenant principal: %+v", created.Principal)
	}
	if created.SessionToken == "" || created.CSRFToken == "" ||
		!created.ExpiresAt.Equal(repository.metadata.ExpiresAt) {
		t.Fatalf("tenant creation must rotate session credentials: %+v", created)
	}

	secondTenant := Tenant{
		ID:   uuid.MustParse("1dcf80d0-b7ab-4a71-98f7-f0f7cd8fef5f"),
		Slug: "second-workspace",
		Name: "Second workspace",
		Role: "teacher",
	}
	repository.principal.Memberships = append(repository.principal.Memberships, secondTenant)
	switched, err := service.SwitchActiveTenant(
		context.Background(),
		repository.principal,
		secondTenant.ID,
	)
	if err != nil {
		t.Fatalf("switch active tenant: %v", err)
	}
	if switched.Principal.ActiveTenant == nil ||
		switched.Principal.ActiveTenant.ID != secondTenant.ID ||
		switched.SessionToken == created.SessionToken ||
		switched.CSRFToken == created.CSRFToken {
		t.Fatalf("unexpected switched tenant result: %+v", switched)
	}

	if _, err := service.CreateTenant(
		context.Background(),
		repository.principal,
		CreateTenantInput{Name: "Another tenant", Slug: "another-tenant"},
	); !errors.Is(err, ErrTenantCreationDenied) {
		t.Fatalf("expected repeat tenant creation to be denied, got %v", err)
	}
	if _, err := service.CreateTenant(
		context.Background(),
		Principal{SessionID: repository.principal.SessionID, User: repository.principal.User},
		CreateTenantInput{Name: "Valid name", Slug: "invalid slug"},
	); !errors.Is(err, ErrInvalidTenant) {
		t.Fatalf("expected invalid slug to be rejected, got %v", err)
	}
}

func TestServiceNormalizesAndValidatesProfileUpdates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 17, 9, 0, 0, 0, time.UTC)
	userID := uuid.MustParse("ee9b4cdf-e1ee-4d79-aa5b-80b7c3aa7ea3")
	repository := &memoryRepository{
		principal: Principal{
			SessionID:       uuid.MustParse("7f3f7634-c04d-4f42-afd5-e52a3bf673cb"),
			AuthenticatedAt: now,
			User: User{
				ID:          userID,
				Email:       "student@example.com",
				DisplayName: "Student",
				Locale:      "vi",
				Timezone:    "Asia/Ho_Chi_Minh",
			},
		},
	}
	service := newTestService(t, repository, &fakeProvider{}, now)
	displayName := "  Nguyá»…n   BÃ¡   SÃ¡ng  "
	locale := "EN"
	timezone := "Asia/Ho_Chi_Minh"
	avatar := "avatars/" + userID.String() + "/profile.webp"

	profile, err := service.UpdateProfile(context.Background(), repository.principal, ProfilePatch{
		DisplayName:     &displayName,
		Locale:          &locale,
		Timezone:        &timezone,
		AvatarObjectKey: &avatar,
	})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if profile.DisplayName != "Nguyá»…n BÃ¡ SÃ¡ng" || profile.Locale != "en" ||
		profile.Timezone != timezone || profile.AvatarObjectKey != avatar {
		t.Fatalf("profile was not normalized: %+v", profile)
	}

	invalidAvatar := "avatars/another-user/profile.webp"
	if _, err := service.UpdateProfile(context.Background(), repository.principal, ProfilePatch{
		AvatarObjectKey: &invalidAvatar,
	}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("expected invalid avatar key, got %v", err)
	}
	invalidTimezone := "Local"
	if _, err := service.UpdateProfile(context.Background(), repository.principal, ProfilePatch{
		Timezone: &invalidTimezone,
	}); !errors.Is(err, ErrInvalidProfile) {
		t.Fatalf("expected invalid timezone, got %v", err)
	}
}

func TestServiceLinksIdentityWithoutReplacingSession(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 17, 9, 15, 0, 0, time.UTC)
	repository := &memoryRepository{
		principal: Principal{
			SessionID:       uuid.MustParse("7f3f7634-c04d-4f42-afd5-e52a3bf673cb"),
			AuthenticatedAt: now.Add(-time.Minute),
			User: User{
				ID:          uuid.MustParse("ee9b4cdf-e1ee-4d79-aa5b-80b7c3aa7ea3"),
				Email:       "student@example.com",
				DisplayName: "Student",
			},
		},
	}
	provider := &fakeProvider{}
	service := newTestService(t, repository, provider, now)

	start, err := service.BeginIdentityLink(context.Background(), repository.principal)
	if err != nil {
		t.Fatalf("begin identity link: %v", err)
	}
	if repository.flow.Purpose != flowPurposeIdentityLink ||
		repository.flow.UserID != repository.principal.User.ID ||
		repository.flow.SessionID != repository.principal.SessionID {
		t.Fatalf("unexpected identity link flow: %+v", repository.flow)
	}

	provider.claims = validClaims(now, provider.nonce)
	provider.claims.Subject = "second-subject"
	result, err := service.CompleteLogin(context.Background(), CallbackInput{
		State:          provider.state,
		BrowserBinding: start.BrowserBinding,
		Code:           "identity-link-code",
	})
	if err != nil {
		t.Fatalf("complete identity link: %v", err)
	}
	if !result.IdentityLinked || result.SessionToken != "" || result.CSRFToken != "" ||
		result.ReturnTo != identityLinkReturnTo {
		t.Fatalf("unexpected identity link result: %+v", result)
	}
	if repository.linkedClaims.Subject != "second-subject" {
		t.Fatalf("linked claims were not persisted: %+v", repository.linkedClaims)
	}
}

func TestServiceRequiresRecentAuthenticationForIdentityMutations(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 17, 9, 30, 0, 0, time.UTC)
	repository := &memoryRepository{
		principal: Principal{
			SessionID:       uuid.MustParse("7f3f7634-c04d-4f42-afd5-e52a3bf673cb"),
			AuthenticatedAt: now.Add(-11 * time.Minute),
			User: User{
				ID:    uuid.MustParse("ee9b4cdf-e1ee-4d79-aa5b-80b7c3aa7ea3"),
				Email: "student@example.com",
			},
		},
	}
	service := newTestService(t, repository, &fakeProvider{}, now)

	if _, err := service.BeginIdentityLink(
		context.Background(),
		repository.principal,
	); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("expected recent authentication requirement, got %v", err)
	}
	if err := service.UnlinkIdentity(
		context.Background(),
		repository.principal,
		uuid.MustParse("49ef082b-b06a-4f2a-9435-f64f89e9b7d5"),
	); !errors.Is(err, ErrRecentAuthenticationRequired) {
		t.Fatalf("expected recent authentication requirement, got %v", err)
	}
}

func testContainsPermission(permissions []string, expected string) bool {
	for _, permission := range permissions {
		if permission == expected {
			return true
		}
	}
	return false
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
	flow          CreateFlowParams
	consumed      bool
	claims        ProviderClaims
	metadata      SessionMetadata
	principal     Principal
	profile       User
	identities    []ExternalIdentity
	linkedClaims  ProviderClaims
	unlinkedID    uuid.UUID
	profileErr    error
	identitiesErr error
	linkErr       error
	unlinkErr     error
	csrfHash      []byte
	revoked       bool
	revokeHash    []byte
	createdTenant CreateTenantInput
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
		Purpose:                repository.flow.Purpose,
		UserID:                 repository.flow.UserID,
		SessionID:              repository.flow.SessionID,
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
		SessionID:       uuid.MustParse("7f3f7634-c04d-4f42-afd5-e52a3bf673cb"),
		IdentityID:      uuid.MustParse("6d92b682-8f2e-4551-b4d3-0a3ba2f86250"),
		AuthenticatedAt: claims.AuthTime,
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

func (repository *memoryRepository) GetProfile(
	_ context.Context,
	userID uuid.UUID,
) (User, error) {
	if repository.profileErr != nil {
		return User{}, repository.profileErr
	}
	if userID != repository.principal.User.ID {
		return User{}, ErrSessionNotFound
	}
	if repository.profile.ID == uuid.Nil {
		return repository.principal.User, nil
	}
	return repository.profile, nil
}

func (repository *memoryRepository) UpdateProfile(
	_ context.Context,
	sessionID uuid.UUID,
	userID uuid.UUID,
	patch ProfilePatch,
	_ time.Time,
) (User, error) {
	if repository.profileErr != nil {
		return User{}, repository.profileErr
	}
	if sessionID != repository.principal.SessionID || userID != repository.principal.User.ID {
		return User{}, ErrSessionNotFound
	}
	profile := repository.principal.User
	if patch.DisplayName != nil {
		profile.DisplayName = *patch.DisplayName
	}
	if patch.Locale != nil {
		profile.Locale = *patch.Locale
	}
	if patch.Timezone != nil {
		profile.Timezone = *patch.Timezone
	}
	if patch.AvatarObjectKey != nil {
		profile.AvatarObjectKey = *patch.AvatarObjectKey
	}
	repository.profile = profile
	repository.principal.User = profile
	return profile, nil
}

func (repository *memoryRepository) ListIdentities(
	_ context.Context,
	userID uuid.UUID,
) ([]ExternalIdentity, error) {
	if repository.identitiesErr != nil {
		return nil, repository.identitiesErr
	}
	if userID != repository.principal.User.ID {
		return nil, ErrSessionNotFound
	}
	return append([]ExternalIdentity(nil), repository.identities...), nil
}

func (repository *memoryRepository) LinkIdentity(
	_ context.Context,
	userID uuid.UUID,
	sessionID uuid.UUID,
	claims ProviderClaims,
	linkedAt time.Time,
) (ExternalIdentity, error) {
	if repository.linkErr != nil {
		return ExternalIdentity{}, repository.linkErr
	}
	if sessionID != repository.principal.SessionID || userID != repository.principal.User.ID {
		return ExternalIdentity{}, ErrSessionNotFound
	}
	repository.linkedClaims = claims
	linked := ExternalIdentity{
		ID:                  uuid.MustParse("49ef082b-b06a-4f2a-9435-f64f89e9b7d5"),
		Provider:            claims.Issuer,
		Email:               claims.Email,
		EmailVerified:       claims.EmailVerified,
		CreatedAt:           linkedAt,
		LastAuthenticatedAt: linkedAt,
	}
	repository.identities = append(repository.identities, linked)
	return linked, nil
}

func (repository *memoryRepository) UnlinkIdentity(
	_ context.Context,
	userID uuid.UUID,
	sessionID uuid.UUID,
	identityID uuid.UUID,
	_ time.Time,
) error {
	if repository.unlinkErr != nil {
		return repository.unlinkErr
	}
	if sessionID != repository.principal.SessionID || userID != repository.principal.User.ID {
		return ErrSessionNotFound
	}
	repository.unlinkedID = identityID
	return nil
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

func (repository *memoryRepository) CreateTenant(
	_ context.Context,
	sessionID uuid.UUID,
	userID uuid.UUID,
	input CreateTenantInput,
	rotation SessionRotation,
) (TenantMutationResult, error) {
	if sessionID != repository.principal.SessionID || userID != repository.principal.User.ID {
		return TenantMutationResult{}, ErrSessionNotFound
	}
	if len(repository.principal.Memberships) != 0 {
		return TenantMutationResult{}, ErrTenantCreationDenied
	}
	repository.createdTenant = input
	tenant := Tenant{
		ID:       uuid.MustParse("c91445df-bde0-44f2-83ed-33ec6148bb84"),
		Slug:     input.Slug,
		Name:     input.Name,
		Role:     "org_admin",
		IsActive: true,
	}
	repository.principal.ActiveTenant = &tenant
	repository.principal.Memberships = []Tenant{tenant}
	repository.principal.Permissions = permissionsForOrganizationRole(tenant.Role)
	repository.metadata.TokenHash = append([]byte(nil), rotation.TokenHash...)
	repository.csrfHash = append([]byte(nil), rotation.CSRFHash...)

	return TenantMutationResult{
		Principal: repository.principal,
		ExpiresAt: repository.metadata.ExpiresAt,
	}, nil
}

func (repository *memoryRepository) SwitchActiveTenant(
	_ context.Context,
	sessionID uuid.UUID,
	userID uuid.UUID,
	tenantID uuid.UUID,
	rotation SessionRotation,
) (TenantMutationResult, error) {
	if sessionID != repository.principal.SessionID || userID != repository.principal.User.ID {
		return TenantMutationResult{}, ErrSessionNotFound
	}
	var selected *Tenant
	for index := range repository.principal.Memberships {
		repository.principal.Memberships[index].IsActive =
			repository.principal.Memberships[index].ID == tenantID
		if repository.principal.Memberships[index].IsActive {
			membership := repository.principal.Memberships[index]
			selected = &membership
		}
	}
	if selected == nil {
		return TenantMutationResult{}, ErrTenantAccessDenied
	}
	repository.principal.ActiveTenant = selected
	repository.principal.Permissions = permissionsForOrganizationRole(selected.Role)
	repository.metadata.TokenHash = append([]byte(nil), rotation.TokenHash...)
	repository.csrfHash = append([]byte(nil), rotation.CSRFHash...)

	return TenantMutationResult{
		Principal: repository.principal,
		ExpiresAt: repository.metadata.ExpiresAt,
	}, nil
}

func permissionsForOrganizationRole(role string) []string {
	permissions := policy.NewEngine().EffectivePermissions(policy.Subject{
		ActorID:           uuid.MustParse("11111111-1111-4111-8111-111111111111"),
		ActiveTenantID:    uuid.MustParse("22222222-2222-4222-8222-222222222222"),
		MembershipActive:  true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRole(role)},
	})
	return policy.PermissionStrings(permissions)
}
