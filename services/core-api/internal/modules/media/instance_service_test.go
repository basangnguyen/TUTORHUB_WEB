package media

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestInstanceCredentialServiceIssuesExactP4GrantWithoutProviderIdentifiers(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actorID := uuid.New()
	sessionID := uuid.New()
	spaceID := uuid.New()
	roomInstanceID := uuid.New()
	participantSessionID := uuid.New()
	joinAttemptID := uuid.New()
	const (
		providerRoomName            = "r_0123456789abcdef0123456789abcdef"
		providerParticipantIdentity = "p_0123456789abcdef0123456789abcdef"
	)
	repository := &fakeInstanceCredentialRepository{grant: ParticipantCredentialGrant{
		ParticipantSessionID:        participantSessionID,
		RoomInstanceID:              roomInstanceID,
		JoinAttemptID:               joinAttemptID,
		ProviderRoomName:            providerRoomName,
		ProviderParticipantIdentity: providerParticipantIdentity,
		ParticipantName:             "  Teaching assistant  ",
		InstanceRole:                InstanceRoleTeachingAssistant,
		CanPublishCameraMicrophone:  true,
		CanShareScreen:              false,
		CanSubscribe:                true,
	}}
	issuer := &fakeInstanceTokenIssuer{token: "signed-instance-token"}
	service := newTestInstanceCredentialService(t, repository, issuer)
	access := AccessContext{
		TenantID: tenantID, ActorID: actorID, SessionID: sessionID,
		Role:             "organization_admin",
		MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{
			policy.OrganizationRoleAdmin,
		},
		ClassRoles: []policy.ClassRole{policy.ClassRoleOwner},
	}

	credential, err := service.IssueInstanceCredential(
		context.Background(), access, spaceID,
		IssueInstanceCredentialInput{JoinAttemptID: joinAttemptID},
	)
	if err != nil {
		t.Fatalf("issue instance credential: %v", err)
	}
	if repository.calls != 1 || repository.access.TenantID != tenantID ||
		repository.access.ActorID != actorID || repository.access.SessionID != sessionID ||
		repository.spaceID != spaceID || repository.joinAttemptID != joinAttemptID ||
		!repository.now.Equal(mediaTestTime) {
		t.Fatalf("unexpected credential repository call: %+v", repository)
	}
	if issuer.calls != 1 {
		t.Fatalf("expected one issuer call, got %d", issuer.calls)
	}
	grant := issuer.grant
	if grant.RoomName != providerRoomName ||
		grant.ParticipantIdentity != providerParticipantIdentity ||
		grant.ParticipantName != "Teaching assistant" ||
		grant.Role != string(InstanceRoleTeachingAssistant) ||
		grant.OrganizationRole != "" || grant.ClassRole != "" ||
		grant.CanPublish || !grant.CanPublishCameraMicrophone || grant.CanShareScreen ||
		grant.CanPublishData || !grant.CanSubscribe || grant.ValidFor != 5*time.Minute {
		t.Fatalf("unexpected P4 token grant: %+v", grant)
	}
	if credential.AccessToken != "signed-instance-token" ||
		credential.ServerURL != "wss://test.livekit.cloud" ||
		credential.ParticipantSessionID != participantSessionID ||
		credential.RoomInstanceID != roomInstanceID ||
		credential.JoinAttemptID != joinAttemptID ||
		credential.InstanceRole != InstanceRoleTeachingAssistant ||
		!credential.CanPublishCameraMicrophone || credential.CanShareScreen ||
		!credential.CanSubscribe || !credential.ExpiresAt.Equal(mediaTestTime.Add(5*time.Minute)) {
		t.Fatalf("unexpected instance credential: %+v", credential)
	}

	payload, err := json.Marshal(credential)
	if err != nil {
		t.Fatalf("marshal credential response: %v", err)
	}
	serialized := string(payload)
	for _, forbidden := range []string{
		providerRoomName,
		providerParticipantIdentity,
		`"room_name"`,
		`"participant_identity"`,
		`"provider_room_sid"`,
	} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("credential response exposed provider binding %q: %s", forbidden, serialized)
		}
	}
}

func TestInstanceCredentialServiceRejectsInvalidRequestBeforeRepositoryOrIssuer(t *testing.T) {
	t.Parallel()

	repository := &fakeInstanceCredentialRepository{}
	issuer := &fakeInstanceTokenIssuer{token: "must-not-be-issued"}
	service := newTestInstanceCredentialService(t, repository, issuer)
	_, err := service.IssueInstanceCredential(
		context.Background(),
		AccessContext{TenantID: uuid.New(), ActorID: uuid.New(), SessionID: uuid.New()},
		uuid.Nil,
		IssueInstanceCredentialInput{JoinAttemptID: uuid.New()},
	)
	if !errors.Is(err, ErrInvalidCredentialRequest) {
		t.Fatalf("expected invalid credential request, got %v", err)
	}
	if repository.calls != 0 || issuer.calls != 0 {
		t.Fatalf("invalid request reached repository or issuer: repository=%d issuer=%d", repository.calls, issuer.calls)
	}
}

func TestInstanceCredentialServiceRejectsInvalidRepositoryGrantBeforeIssuer(t *testing.T) {
	t.Parallel()

	joinAttemptID := uuid.New()
	repository := &fakeInstanceCredentialRepository{grant: ParticipantCredentialGrant{
		ParticipantSessionID:        uuid.New(),
		RoomInstanceID:              uuid.New(),
		JoinAttemptID:               uuid.New(),
		ProviderRoomName:            "r_0123456789abcdef0123456789abcdef",
		ProviderParticipantIdentity: "p_0123456789abcdef0123456789abcdef",
		InstanceRole:                InstanceRoleAttendee,
	}}
	issuer := &fakeInstanceTokenIssuer{token: "must-not-be-issued"}
	service := newTestInstanceCredentialService(t, repository, issuer)

	_, err := service.IssueInstanceCredential(
		context.Background(), validInstanceCredentialAccess(), uuid.New(),
		IssueInstanceCredentialInput{JoinAttemptID: joinAttemptID},
	)
	if !errors.Is(err, ErrLifecycleUnavailable) {
		t.Fatalf("expected invalid repository grant to fail closed, got %v", err)
	}
	if repository.calls != 1 || issuer.calls != 0 {
		t.Fatalf("invalid grant reached issuer: repository=%d issuer=%d", repository.calls, issuer.calls)
	}
}

func TestInstanceCredentialServiceNormalizesIssuerFailureAsProviderUnavailable(t *testing.T) {
	t.Parallel()

	joinAttemptID := uuid.New()
	const sensitiveDetail = "provider-sensitive-signing-detail"
	repository := &fakeInstanceCredentialRepository{grant: ParticipantCredentialGrant{
		ParticipantSessionID:        uuid.New(),
		RoomInstanceID:              uuid.New(),
		JoinAttemptID:               joinAttemptID,
		ProviderRoomName:            "r_0123456789abcdef0123456789abcdef",
		ProviderParticipantIdentity: "p_0123456789abcdef0123456789abcdef",
		InstanceRole:                InstanceRoleHost,
		CanPublishCameraMicrophone:  true,
		CanShareScreen:              true,
		CanSubscribe:                true,
	}}
	issuer := &fakeInstanceTokenIssuer{err: errors.New(sensitiveDetail)}
	service := newTestInstanceCredentialService(t, repository, issuer)

	credential, err := service.IssueInstanceCredential(
		context.Background(), validInstanceCredentialAccess(), uuid.New(),
		IssueInstanceCredentialInput{JoinAttemptID: joinAttemptID},
	)
	if !errors.Is(err, ErrMediaProviderUnavailable) || strings.Contains(err.Error(), sensitiveDetail) {
		t.Fatalf("issuer failure was not safely normalized: %v", err)
	}
	if credential != (InstanceCredential{}) || repository.calls != 1 || issuer.calls != 1 {
		t.Fatalf("unexpected issuer failure result: credential=%+v repository=%d issuer=%d", credential, repository.calls, issuer.calls)
	}
}

func TestProviderWebhookServiceRedactsRepositoryFailure(t *testing.T) {
	t.Parallel()

	const sensitiveDetail = "database-sensitive-provider-binding"
	repository := &fakeProviderWebhookRepository{err: errors.New(sensitiveDetail)}
	service, err := NewProviderWebhookService(
		repository,
		nil,
		func() time.Time { return mediaTestTime },
	)
	if err != nil {
		t.Fatalf("create provider webhook service: %v", err)
	}
	result, err := service.RecordWebhook(context.Background(), WebhookEvent{
		ID:         "evt_redacted_repository_failure",
		EventType:  "room_started",
		RoomName:   "r_0123456789abcdef0123456789abcdef",
		RoomSID:    "RM_opaque",
		OccurredAt: mediaTestTime,
	})
	if !errors.Is(err, ErrLifecycleUnavailable) || strings.Contains(err.Error(), sensitiveDetail) {
		t.Fatalf("repository failure was not safely normalized: %v", err)
	}
	if result != (WebhookResult{}) || repository.calls != 1 {
		t.Fatalf("unexpected webhook failure result: result=%+v calls=%d", result, repository.calls)
	}
}

func newTestInstanceCredentialService(
	t *testing.T,
	repository InstanceCredentialRepository,
	issuer TokenIssuer,
) *InstanceCredentialService {
	t.Helper()
	service, err := NewInstanceCredentialService(repository, issuer, ServiceConfig{
		ServerURL: "  wss://test.livekit.cloud/  ",
		TokenTTL:  5 * time.Minute,
		Clock:     func() time.Time { return mediaTestTime },
	})
	if err != nil {
		t.Fatalf("create instance credential service: %v", err)
	}
	return service
}

func validInstanceCredentialAccess() AccessContext {
	return AccessContext{
		TenantID: uuid.New(), ActorID: uuid.New(), SessionID: uuid.New(),
		MembershipActive: true,
	}
}

type fakeInstanceCredentialRepository struct {
	grant         ParticipantCredentialGrant
	err           error
	calls         int
	access        AccessContext
	spaceID       uuid.UUID
	joinAttemptID uuid.UUID
	now           time.Time
}

func (repository *fakeInstanceCredentialRepository) PrepareCredential(
	_ context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	joinAttemptID uuid.UUID,
	now time.Time,
) (ParticipantCredentialGrant, error) {
	repository.calls++
	repository.access = access
	repository.spaceID = spaceID
	repository.joinAttemptID = joinAttemptID
	repository.now = now
	return repository.grant, repository.err
}

type fakeInstanceTokenIssuer struct {
	token string
	err   error
	calls int
	grant TokenGrant
}

func (issuer *fakeInstanceTokenIssuer) Issue(grant TokenGrant) (string, error) {
	issuer.calls++
	issuer.grant = grant
	return issuer.token, issuer.err
}

type fakeProviderWebhookRepository struct {
	result  WebhookResult
	handled bool
	err     error
	calls   int
}

func (repository *fakeProviderWebhookRepository) RecordProviderWebhook(
	_ context.Context,
	_ WebhookEvent,
	_ time.Time,
) (WebhookResult, bool, error) {
	repository.calls++
	return repository.result, repository.handled, repository.err
}

func (repository *fakeProviderWebhookRepository) AllowLegacyMedia(
	context.Context,
	uuid.UUID,
) (bool, error) {
	return false, nil
}
