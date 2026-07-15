package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
)

var mediaTestTime = time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)

func TestIssueJoinCredentialUsesTenantClassAndLeastPrivilege(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	classID := uuid.New()
	actorID := uuid.New()
	sessionID := uuid.New()
	attemptID := uuid.New()
	classes := &fakeClassService{class: classroom.Class{
		ID: classID, TenantID: tenantID, Status: classroom.ClassStatusActive,
	}}
	issuer := &fakeTokenIssuer{token: "signed-token"}
	service := newTestService(t, classes, issuer, nil, nil, attemptID)

	credential, err := service.IssueJoinCredential(context.Background(), AccessContext{
		TenantID: tenantID, ActorID: actorID, SessionID: sessionID,
		DisplayName: "  Giảng viên An  ", Role: "teacher",
		Permissions: []string{"class.view", "session.join", "media.publish"},
	}, classID)
	if err != nil {
		t.Fatalf("issue join credential: %v", err)
	}

	if credential.AccessToken != "signed-token" || credential.AttemptID != attemptID {
		t.Fatalf("unexpected credential: %+v", credential)
	}
	if credential.RoomName != RoomName(tenantID, classID) ||
		credential.ParticipantIdentity != ParticipantIdentity(actorID, sessionID) {
		t.Fatalf("unexpected server-derived identifiers: %+v", credential)
	}
	if !credential.CanPublish || !issuer.grant.CanPublish || !issuer.grant.CanSubscribe ||
		issuer.grant.CanPublishData || issuer.grant.Role != "teacher" {
		t.Fatalf("unexpected least-privilege grant: %+v", issuer.grant)
	}
	if issuer.grant.ParticipantName != "Giảng viên An" ||
		credential.ExpiresAt != mediaTestTime.Add(5*time.Minute) {
		t.Fatalf("unexpected participant or expiry: %+v", credential)
	}
	if classes.access.TenantID != tenantID || classes.access.ActorID != actorID ||
		classes.classID != classID {
		t.Fatalf("class authorization did not use active tenant: %+v", classes)
	}
}

func TestIssueJoinCredentialCreatesSubscribeOnlyGrantWithoutPublishPermission(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	classID := uuid.New()
	issuer := &fakeTokenIssuer{token: "signed-token"}
	service := newTestService(t, &fakeClassService{class: classroom.Class{
		ID: classID, TenantID: tenantID, Status: classroom.ClassStatusDraft,
	}}, issuer, nil, nil, uuid.New())

	credential, err := service.IssueJoinCredential(context.Background(), AccessContext{
		TenantID: tenantID, ActorID: uuid.New(), SessionID: uuid.New(),
		DisplayName: "Học viên", Role: "student",
		Permissions: []string{"class.view", "session.join"},
	}, classID)
	if err != nil {
		t.Fatalf("issue subscribe-only credential: %v", err)
	}
	if credential.CanPublish || issuer.grant.CanPublish || !issuer.grant.CanSubscribe {
		t.Fatalf("expected subscribe-only grant: %+v", issuer.grant)
	}
}

func TestIssueJoinCredentialDeniesMissingPermissionAndArchivedClass(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	classID := uuid.New()
	classes := &fakeClassService{class: classroom.Class{
		ID: classID, TenantID: tenantID, Status: classroom.ClassStatusArchived,
	}}
	service := newTestService(t, classes, &fakeTokenIssuer{token: "token"}, nil, nil, uuid.New())

	_, err := service.IssueJoinCredential(context.Background(), AccessContext{
		TenantID: tenantID, ActorID: uuid.New(), SessionID: uuid.New(),
		Permissions: []string{"class.view"},
	}, classID)
	if !errors.Is(err, ErrAccessDenied) || classes.getCalls != 0 {
		t.Fatalf("expected deny before class lookup, err=%v calls=%d", err, classes.getCalls)
	}

	_, err = service.IssueJoinCredential(context.Background(), AccessContext{
		TenantID: tenantID, ActorID: uuid.New(), SessionID: uuid.New(),
		Permissions: []string{"class.view", "session.join"},
	}, classID)
	if !errors.Is(err, ErrClassUnavailable) {
		t.Fatalf("expected archived class denial, got %v", err)
	}
}

func TestRecordClientEventValidatesBoundedTelemetry(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	classID := uuid.New()
	sink := &fakeEventSink{}
	service := newTestService(t, &fakeClassService{class: classroom.Class{
		ID: classID, TenantID: tenantID, Status: classroom.ClassStatusActive,
	}}, &fakeTokenIssuer{token: "token"}, sink, nil, uuid.New())
	access := AccessContext{
		TenantID: tenantID, ActorID: uuid.New(), SessionID: uuid.New(),
		Permissions: []string{"class.view", "session.join"},
	}
	attemptID := uuid.New()

	err := service.RecordClientEvent(context.Background(), access, classID, ClientEventInput{
		AttemptID: attemptID, Stage: "connect", Outcome: "failed",
		ErrorCode: "signal.timeout", DurationMS: 1250,
	})
	if err != nil {
		t.Fatalf("record client event: %v", err)
	}
	if sink.event.AttemptID != attemptID || sink.event.ErrorCode != "signal.timeout" ||
		sink.event.RecordedAt != mediaTestTime {
		t.Fatalf("unexpected telemetry event: %+v", sink.event)
	}

	err = service.RecordClientEvent(context.Background(), access, classID, ClientEventInput{
		AttemptID: attemptID, Stage: "connect", Outcome: "failed",
		ErrorCode: "raw error with user content",
	})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected unbounded error text to be rejected, got %v", err)
	}
}

func TestRecordWebhookIsDurablyIdempotentAndIgnoresOtherRoomNamespaces(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	classID := uuid.New()
	repository := &fakeWebhookRepository{recorded: false}
	service := newTestService(t, &fakeClassService{}, &fakeTokenIssuer{}, nil, repository, uuid.New())
	event := WebhookEvent{
		ID: "EV_participant_joined_01", EventType: "participant_joined",
		RoomName: RoomName(tenantID, classID), ParticipantIdentity: "participant",
		OccurredAt: mediaTestTime.Add(-time.Second),
	}

	result, err := service.RecordWebhook(context.Background(), event)
	if err != nil {
		t.Fatalf("record webhook: %v", err)
	}
	if !result.Duplicate || result.Recorded || repository.receipt.TenantID != tenantID ||
		repository.receipt.ClassID != classID {
		t.Fatalf("unexpected idempotency result: result=%+v receipt=%+v", result, repository.receipt)
	}

	result, err = service.RecordWebhook(context.Background(), WebhookEvent{
		ID: "EV_room_started_01", EventType: "room_started", RoomName: "another-product-room",
		OccurredAt: mediaTestTime,
	})
	if err != nil || !result.Ignored || repository.calls != 1 {
		t.Fatalf("expected unrelated room to be ignored: result=%+v err=%v", result, err)
	}
}

func TestRoomNameRoundTrip(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	classID := uuid.New()
	parsedTenant, parsedClass, ok := ParseRoomName(RoomName(tenantID, classID))
	if !ok || parsedTenant != tenantID || parsedClass != classID {
		t.Fatalf("room name did not round-trip")
	}
	if _, _, ok := ParseRoomName("th_invalid_room"); ok {
		t.Fatal("invalid room name must be rejected")
	}
}

func newTestService(
	t *testing.T,
	classes classroom.ServiceAPI,
	issuer TokenIssuer,
	events EventSink,
	repository WebhookReceiptRepository,
	attemptID uuid.UUID,
) *Service {
	t.Helper()
	service, err := NewService(classes, issuer, events, repository, ServiceConfig{
		ServerURL: "wss://test.livekit.cloud",
		TokenTTL:  5 * time.Minute,
		Clock:     func() time.Time { return mediaTestTime },
		NewID:     func() uuid.UUID { return attemptID },
	})
	if err != nil {
		t.Fatalf("create media service: %v", err)
	}
	return service
}

type fakeTokenIssuer struct {
	token string
	err   error
	grant TokenGrant
}

func (issuer *fakeTokenIssuer) Issue(grant TokenGrant) (string, error) {
	issuer.grant = grant
	return issuer.token, issuer.err
}

type fakeClassService struct {
	class    classroom.Class
	err      error
	access   classroom.AccessContext
	classID  uuid.UUID
	getCalls int
}

func (service *fakeClassService) Create(
	context.Context,
	classroom.AccessContext,
	classroom.CreateClassInput,
) (classroom.Class, error) {
	return classroom.Class{}, errors.New("unexpected create")
}

func (service *fakeClassService) Get(
	_ context.Context,
	access classroom.AccessContext,
	classID uuid.UUID,
) (classroom.Class, error) {
	service.getCalls++
	service.access = access
	service.classID = classID
	return service.class, service.err
}

func (service *fakeClassService) List(
	context.Context,
	classroom.AccessContext,
	int,
) ([]classroom.Class, error) {
	return nil, errors.New("unexpected list")
}

type fakeEventSink struct {
	event ClientEvent
}

func (sink *fakeEventSink) RecordClientEvent(_ context.Context, event ClientEvent) {
	sink.event = event
}

type fakeWebhookRepository struct {
	recorded bool
	err      error
	receipt  WebhookReceipt
	calls    int
}

func (repository *fakeWebhookRepository) RecordWebhookReceipt(
	_ context.Context,
	receipt WebhookReceipt,
) (bool, error) {
	repository.calls++
	repository.receipt = receipt
	return repository.recorded, repository.err
}
