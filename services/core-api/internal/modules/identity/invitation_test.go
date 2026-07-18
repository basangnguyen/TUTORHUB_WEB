package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestServiceCreatesOpaqueMembershipInvitation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 8, 0, 0, 0, time.UTC)
	principal, tenantID := invitationAdminPrincipal(now)
	repository := &invitationMemoryRepository{
		memoryRepository: &memoryRepository{principal: principal},
	}
	service := newTestService(t, repository, &fakeProvider{}, now)

	result, err := service.CreateMembershipInvitation(
		context.Background(),
		principal,
		tenantID,
		CreateMembershipInvitationInput{
			Email:        "  Student@Example.COM  ",
			IntendedRole: " student ",
		},
	)
	if err != nil {
		t.Fatalf("create membership invitation: %v", err)
	}

	if repository.createCalls != 1 {
		t.Fatalf("expected one repository creation, got %d", repository.createCalls)
	}
	if repository.createdContext.TenantID != tenantID ||
		repository.createdContext.ActorID != principal.User.ID {
		t.Fatalf("unexpected tenant context: %+v", repository.createdContext)
	}
	if repository.createdParams.Email != "student@example.com" ||
		repository.createdParams.IntendedRole != "student" {
		t.Fatalf("invitation input was not normalized: %+v", repository.createdParams)
	}
	if repository.createdParams.CreatedAt != now ||
		repository.createdParams.ExpiresAt != now.Add(defaultMembershipInvitationTTL) {
		t.Fatalf("unexpected invitation lifetime: %+v", repository.createdParams)
	}
	if len(repository.createdParams.TokenHash) != 32 {
		t.Fatalf("expected a SHA-256 HMAC, got %d bytes", len(repository.createdParams.TokenHash))
	}
	if !bytes.Equal(
		repository.createdParams.TokenHash,
		service.crypto.Digest(membershipInvitationTokenPurpose, result.Token),
	) {
		t.Fatal("repository did not receive the purpose-bound token HMAC")
	}
	if bytes.Equal(repository.createdParams.TokenHash, []byte(result.Token)) ||
		bytes.Contains(repository.createdParams.TokenHash, []byte(result.Token)) {
		t.Fatal("repository must never receive the raw invitation token as persistence data")
	}
	if _, err := normalizeMembershipInvitationToken(result.Token); err != nil {
		t.Fatalf("service returned a malformed invitation token: %v", err)
	}
	if result.Invitation.Email != "student@example.com" ||
		result.Invitation.IntendedRole != "student" ||
		result.Invitation.Status != MembershipInvitationPending {
		t.Fatalf("unexpected created invitation: %+v", result.Invitation)
	}
}

func TestServiceRejectsInvalidMembershipInvitationInputBeforePersistence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 8, 15, 0, 0, time.UTC)
	principal, tenantID := invitationAdminPrincipal(now)
	tests := []struct {
		name  string
		input CreateMembershipInvitationInput
	}{
		{
			name: "display name is not one normalized address",
			input: CreateMembershipInvitationInput{
				Email:        "Student <student@example.com>",
				IntendedRole: "student",
			},
		},
		{
			name: "multiple addresses",
			input: CreateMembershipInvitationInput{
				Email:        "first@example.com, second@example.com",
				IntendedRole: "student",
			},
		},
		{
			name: "organization administrator cannot be granted by this flow",
			input: CreateMembershipInvitationInput{
				Email:        "admin@example.com",
				IntendedRole: "org_admin",
			},
		},
		{
			name: "unknown role",
			input: CreateMembershipInvitationInput{
				Email:        "student@example.com",
				IntendedRole: "owner",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repository := &invitationMemoryRepository{
				memoryRepository: &memoryRepository{principal: principal},
			}
			service := newTestService(t, repository, &fakeProvider{}, now)
			_, err := service.CreateMembershipInvitation(
				context.Background(),
				principal,
				tenantID,
				test.input,
			)
			if !errors.Is(err, ErrInvalidMembershipInvitation) {
				t.Fatalf("expected invalid invitation input, got %v", err)
			}
			if repository.createCalls != 0 {
				t.Fatal("invalid invitation must not reach persistence")
			}
		})
	}
}

func TestServicePreviewsInvitationWithoutExposingEmailOrToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 8, 30, 0, 0, time.UTC)
	rawToken := membershipInvitationTestToken(0x21)
	repository := &invitationMemoryRepository{
		memoryRepository: &memoryRepository{},
		previewResult: StoredMembershipInvitationPreview{
			Invitation: MembershipInvitation{
				Email:        "alice@example.com",
				IntendedRole: "teacher",
				Status:       MembershipInvitationPending,
				ExpiresAt:    now.Add(time.Hour),
			},
			TenantName: "Example Academy",
		},
	}
	service := newTestService(t, repository, &fakeProvider{}, now)

	preview, err := service.PreviewMembershipInvitation(
		context.Background(),
		"  "+rawToken+"  ",
	)
	if err != nil {
		t.Fatalf("preview membership invitation: %v", err)
	}
	if preview.TenantName != "Example Academy" ||
		preview.MaskedEmail != "a***@example.com" ||
		preview.IntendedRole != "teacher" ||
		preview.Status != MembershipInvitationPending {
		t.Fatalf("unexpected invitation preview: %+v", preview)
	}
	if repository.previewCalls != 1 ||
		!bytes.Equal(
			repository.previewHash,
			service.crypto.Digest(membershipInvitationTokenPurpose, rawToken),
		) {
		t.Fatal("preview persistence must receive only the purpose-bound token HMAC")
	}
	if bytes.Contains(repository.previewHash, []byte(rawToken)) {
		t.Fatal("preview persistence received raw token material")
	}
}

func TestServiceRejectsMalformedInvitationTokensBeforePersistence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 8, 45, 0, 0, time.UTC)
	principal, _ := invitationAdminPrincipal(now)
	repository := &invitationMemoryRepository{
		memoryRepository: &memoryRepository{principal: principal},
	}
	service := newTestService(t, repository, &fakeProvider{}, now)
	validEncoded := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x31}, 32))
	tests := []string{
		"",
		"wrong_" + validEncoded,
		membershipInvitationTokenPrefix +
			base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x31}, 31)),
		membershipInvitationTokenPrefix + validEncoded + "=",
		membershipInvitationTokenPrefix + validEncoded + "!",
	}

	for _, rawToken := range tests {
		if _, err := service.PreviewMembershipInvitation(
			context.Background(),
			rawToken,
		); !errors.Is(err, ErrMembershipInvitationUnavailable) {
			t.Fatalf("expected unavailable preview for %q, got %v", rawToken, err)
		}
		if _, err := service.AcceptMembershipInvitation(
			context.Background(),
			principal,
			rawToken,
		); !errors.Is(err, ErrMembershipInvitationUnavailable) {
			t.Fatalf("expected unavailable accept for %q, got %v", rawToken, err)
		}
	}
	if repository.previewCalls != 0 || repository.acceptCalls != 0 {
		t.Fatal("malformed token must not reach persistence")
	}
}

func TestServiceAcceptsInvitationWithSessionBoundDigest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 9, 0, 0, 0, time.UTC)
	principal, tenantID := invitationAdminPrincipal(now)
	rawToken := membershipInvitationTestToken(0x41)
	repository := &invitationMemoryRepository{
		memoryRepository: &memoryRepository{principal: principal},
		acceptResult: AcceptMembershipInvitationResult{
			Invitation: MembershipInvitation{
				ID:           uuid.New(),
				TenantID:     tenantID,
				Email:        principal.User.Email,
				IntendedRole: "student",
				Status:       MembershipInvitationAccepted,
			},
			Principal: principal,
		},
	}
	service := newTestService(t, repository, &fakeProvider{}, now)

	result, err := service.AcceptMembershipInvitation(
		context.Background(),
		principal,
		rawToken,
	)
	if err != nil {
		t.Fatalf("accept membership invitation: %v", err)
	}
	if result.Invitation.Status != MembershipInvitationAccepted ||
		repository.acceptCalls != 1 {
		t.Fatalf("unexpected acceptance result: %+v", result)
	}
	if repository.acceptParams.SessionID != principal.SessionID ||
		repository.acceptParams.UserID != principal.User.ID ||
		repository.acceptParams.AcceptedAt != now ||
		!bytes.Equal(
			repository.acceptParams.TokenHash,
			service.crypto.Digest(membershipInvitationTokenPurpose, rawToken),
		) {
		t.Fatalf("unexpected invitation acceptance params: %+v", repository.acceptParams)
	}

	if _, err := service.AcceptMembershipInvitation(
		context.Background(),
		Principal{},
		rawToken,
	); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected session validation before token persistence, got %v", err)
	}
	if repository.acceptCalls != 1 {
		t.Fatal("invalid principal must not reach invitation persistence")
	}
}

func TestServiceAuthorizesMembershipInvitationManagement(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 18, 9, 15, 0, 0, time.UTC)
	principal, tenantID := invitationAdminPrincipal(now)
	invitationID := uuid.New()
	repository := &invitationMemoryRepository{
		memoryRepository: &memoryRepository{principal: principal},
		listResult: []MembershipInvitation{
			{ID: invitationID, TenantID: tenantID},
		},
		revokeResult: MembershipInvitation{
			ID:       invitationID,
			TenantID: tenantID,
			Status:   MembershipInvitationRevoked,
		},
	}
	service := newTestService(t, repository, &fakeProvider{}, now)

	invitations, err := service.ListMembershipInvitations(
		context.Background(),
		principal,
		tenantID,
	)
	if err != nil || len(invitations) != 1 ||
		repository.listAt != now ||
		repository.listContext.ActorID != principal.User.ID {
		t.Fatalf("unexpected invitation list: invitations=%+v error=%v", invitations, err)
	}
	revoked, err := service.RevokeMembershipInvitation(
		context.Background(),
		principal,
		tenantID,
		invitationID,
	)
	if err != nil || revoked.Status != MembershipInvitationRevoked ||
		repository.revokedID != invitationID ||
		repository.revokedAt != now {
		t.Fatalf("unexpected invitation revoke: invitation=%+v error=%v", revoked, err)
	}

	teacher := principal
	active := *teacher.ActiveTenant
	active.Role = "teacher"
	teacher.ActiveTenant = &active
	if _, err := service.ListMembershipInvitations(
		context.Background(),
		teacher,
		tenantID,
	); !errors.Is(err, ErrTenantAccessDenied) {
		t.Fatalf("expected non-admin denial, got %v", err)
	}
	if _, err := service.CreateMembershipInvitation(
		context.Background(),
		teacher,
		tenantID,
		CreateMembershipInvitationInput{
			Email:        "blocked@example.com",
			IntendedRole: "student",
		},
	); !errors.Is(err, ErrTenantAccessDenied) {
		t.Fatalf("expected non-admin create denial, got %v", err)
	}
	if _, err := service.RevokeMembershipInvitation(
		context.Background(),
		teacher,
		tenantID,
		invitationID,
	); !errors.Is(err, ErrTenantAccessDenied) {
		t.Fatalf("expected non-admin revoke denial, got %v", err)
	}

	otherTenantID := uuid.New()
	if _, err := service.ListMembershipInvitations(
		context.Background(),
		principal,
		otherTenantID,
	); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("expected concealed cross-tenant denial, got %v", err)
	}
	if _, err := service.CreateMembershipInvitation(
		context.Background(),
		principal,
		otherTenantID,
		CreateMembershipInvitationInput{
			Email:        "concealed@example.com",
			IntendedRole: "guest",
		},
	); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("expected concealed cross-tenant create denial, got %v", err)
	}
	if _, err := service.RevokeMembershipInvitation(
		context.Background(),
		principal,
		otherTenantID,
		invitationID,
	); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("expected concealed cross-tenant revoke denial, got %v", err)
	}
	if _, err := service.RevokeMembershipInvitation(
		context.Background(),
		principal,
		tenantID,
		uuid.Nil,
	); !errors.Is(err, ErrMembershipInvitationUnavailable) {
		t.Fatalf("expected missing invitation concealment, got %v", err)
	}
	if repository.listCalls != 1 || repository.createCalls != 0 || repository.revokeCalls != 1 {
		t.Fatal("denied invitation management must not reach persistence")
	}
}

func TestNewServiceValidatesMembershipInvitationTTL(t *testing.T) {
	t.Parallel()

	crypto, err := NewCrypto(bytes.Repeat([]byte{0x55}, 32))
	if err != nil {
		t.Fatalf("create crypto: %v", err)
	}
	for _, ttl := range []time.Duration{
		minimumMembershipInvitationTTL - time.Second,
		maximumMembershipInvitationTTL + time.Second,
	} {
		_, err := NewService(
			&memoryRepository{},
			&fakeProvider{},
			crypto,
			policy.NewEngine(),
			ServiceConfig{
				FlowTTL:                 time.Minute,
				SessionTTL:              time.Hour,
				SessionAbsoluteTTL:      2 * time.Hour,
				MembershipInvitationTTL: ttl,
			},
			time.Now,
		)
		if err == nil {
			t.Fatalf("expected invalid invitation TTL %s", ttl)
		}
	}
}

func TestMaskMembershipInvitationEmail(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"a@example.com":     "*@example.com",
		"alice@example.com": "a***@example.com",
		"invalid":           "***",
	}
	for email, expected := range tests {
		if actual := maskMembershipInvitationEmail(email); actual != expected {
			t.Fatalf("mask %q: got %q want %q", email, actual, expected)
		}
	}
}

func membershipInvitationTestToken(fill byte) string {
	return membershipInvitationTokenPrefix +
		base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{fill}, randomTokenBytes))
}

func invitationAdminPrincipal(now time.Time) (Principal, uuid.UUID) {
	tenantID := uuid.New()
	userID := uuid.New()
	tenant := Tenant{
		ID:       tenantID,
		Name:     "Invitation Tenant",
		Status:   "active",
		Role:     "org_admin",
		IsActive: true,
	}
	return Principal{
		SessionID:       uuid.New(),
		ContextVersion:  1,
		AuthenticatedAt: now.Add(-time.Minute),
		User: User{
			ID:    userID,
			Email: "admin@example.com",
		},
		ActiveTenant: &tenant,
		Memberships:  []Tenant{tenant},
		Permissions: []string{
			string(policy.PermissionTenantManageMembers),
		},
	}, tenantID
}

type invitationMemoryRepository struct {
	*memoryRepository

	listCalls      int
	listContext    tenancy.Context
	listAt         time.Time
	listResult     []MembershipInvitation
	listErr        error
	createCalls    int
	createdContext tenancy.Context
	createdParams  CreateMembershipInvitationParams
	createResult   MembershipInvitation
	createErr      error
	revokeCalls    int
	revokedContext tenancy.Context
	revokedID      uuid.UUID
	revokedAt      time.Time
	revokeResult   MembershipInvitation
	revokeErr      error
	previewCalls   int
	previewHash    []byte
	previewAt      time.Time
	previewResult  StoredMembershipInvitationPreview
	previewErr     error
	acceptCalls    int
	acceptParams   AcceptMembershipInvitationParams
	acceptResult   AcceptMembershipInvitationResult
	acceptErr      error
}

func (repository *invitationMemoryRepository) ListMembershipInvitations(
	_ context.Context,
	tenantContext tenancy.Context,
	now time.Time,
) ([]MembershipInvitation, error) {
	repository.listCalls++
	repository.listContext = tenantContext
	repository.listAt = now
	return append([]MembershipInvitation(nil), repository.listResult...), repository.listErr
}

func (repository *invitationMemoryRepository) CreateMembershipInvitation(
	_ context.Context,
	tenantContext tenancy.Context,
	params CreateMembershipInvitationParams,
) (MembershipInvitation, error) {
	repository.createCalls++
	repository.createdContext = tenantContext
	repository.createdParams = params
	repository.createdParams.TokenHash = append([]byte(nil), params.TokenHash...)
	if repository.createErr != nil {
		return MembershipInvitation{}, repository.createErr
	}
	if repository.createResult.ID != uuid.Nil {
		return repository.createResult, nil
	}
	return MembershipInvitation{
		ID:           uuid.New(),
		TenantID:     tenantContext.TenantID,
		Email:        params.Email,
		IntendedRole: params.IntendedRole,
		Status:       MembershipInvitationPending,
		ExpiresAt:    params.ExpiresAt,
		CreatedAt:    params.CreatedAt,
		UpdatedAt:    params.CreatedAt,
		InvitedBy:    tenantContext.ActorID,
	}, nil
}

func (repository *invitationMemoryRepository) RevokeMembershipInvitation(
	_ context.Context,
	tenantContext tenancy.Context,
	invitationID uuid.UUID,
	now time.Time,
) (MembershipInvitation, error) {
	repository.revokeCalls++
	repository.revokedContext = tenantContext
	repository.revokedID = invitationID
	repository.revokedAt = now
	return repository.revokeResult, repository.revokeErr
}

func (repository *invitationMemoryRepository) PreviewMembershipInvitation(
	_ context.Context,
	tokenHash []byte,
	now time.Time,
) (StoredMembershipInvitationPreview, error) {
	repository.previewCalls++
	repository.previewHash = append([]byte(nil), tokenHash...)
	repository.previewAt = now
	return repository.previewResult, repository.previewErr
}

func (repository *invitationMemoryRepository) AcceptMembershipInvitation(
	_ context.Context,
	params AcceptMembershipInvitationParams,
) (AcceptMembershipInvitationResult, error) {
	repository.acceptCalls++
	repository.acceptParams = params
	repository.acceptParams.TokenHash = append([]byte(nil), params.TokenHash...)
	return repository.acceptResult, repository.acceptErr
}

// These no-op invitation methods keep the pre-existing all-purpose identity
// repository fake conformant after Repository gained the P2-03 interface.
func (*memoryRepository) ListMembershipInvitations(
	context.Context,
	tenancy.Context,
	time.Time,
) ([]MembershipInvitation, error) {
	return []MembershipInvitation{}, nil
}

func (*memoryRepository) CreateMembershipInvitation(
	context.Context,
	tenancy.Context,
	CreateMembershipInvitationParams,
) (MembershipInvitation, error) {
	return MembershipInvitation{}, nil
}

func (*memoryRepository) RevokeMembershipInvitation(
	context.Context,
	tenancy.Context,
	uuid.UUID,
	time.Time,
) (MembershipInvitation, error) {
	return MembershipInvitation{}, nil
}

func (*memoryRepository) PreviewMembershipInvitation(
	context.Context,
	[]byte,
	time.Time,
) (StoredMembershipInvitationPreview, error) {
	return StoredMembershipInvitationPreview{}, nil
}

func (*memoryRepository) AcceptMembershipInvitation(
	context.Context,
	AcceptMembershipInvitationParams,
) (AcceptMembershipInvitationResult, error) {
	return AcceptMembershipInvitationResult{}, nil
}
