//go:build integration

package media

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const p402DisposableConfirmation = "I_UNDERSTAND_P4_02_DISPOSABLE_ONLY"

func TestPostgresMediaRoomInstanceCredentialAndSignedWebhookBinding(t *testing.T) {
	gateStartedAt := time.Now()
	logStage := func(stage string) {
		t.Logf("stage=%s elapsed_ms=%d", stage, time.Since(gateStartedAt).Milliseconds())
	}
	if strings.TrimSpace(os.Getenv("P4_02_DISPOSABLE_CONFIRM")) != p402DisposableConfirmation {
		t.Skip("P4_02_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply room-instance binding migrations")
	}
	logStage("migrated")
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	assertP402SeparatedDatabaseRoles(t, ctx, migrationPool, runtimePool)
	fixture := seedMediaIntegrationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupMediaIntegrationFixture(t, migrationPool, fixture) })

	setMediaFeatureOverrides(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.adminID,
		map[featurecontrol.FeatureKey]bool{
			featurecontrol.FeatureClassroomMediaRooms: true,
			featurecontrol.FeatureInstantStudyRooms:   true,
		},
	)
	setMediaQuotaOverrides(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.adminID,
		map[featurecontrol.QuotaKey]int64{
			featurecontrol.QuotaActiveMediaSpaces:         20,
			featurecontrol.QuotaMediaSpaceStartsPerHour:   40,
			featurecontrol.QuotaMediaParticipantsPerSpace: 50,
			featurecontrol.QuotaActiveMediaParticipants:   100,
		},
	)
	logStage("fixture_ready")

	// Keep a deterministic sub-second component so the real PostgreSQL path
	// proves that Unix-second LiveKit timestamps are clamped, not rejected.
	now := time.Now().UTC().Truncate(time.Second).Add(500 * time.Millisecond)
	clock := func() time.Time { return now }
	lifecycle, instances := newP402IntegrationServices(t, runtimePool, clock)
	provider := &p402IntegrationRoomProvider{
		sid:                 "RM_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		ensureBarrierTarget: 2,
		ensureBarrier:       make(chan struct{}),
	}
	providerLifecycle, err := NewProviderLifecycleService(
		lifecycle,
		instances,
		provider,
		clock,
	)
	if err != nil {
		t.Fatalf("create provider lifecycle service: %v", err)
	}

	ownerAccess := p402IntegrationAccess(
		fixture.tenantID,
		fixture.ownerID,
		policy.OrganizationRoleStudent,
		uuid.New(),
	)
	created, err := providerLifecycle.CreateSpace(
		ctx,
		ownerAccess,
		officialMediaCreateInput(
			fixture.sessionIDs["owner"],
			mediaIntegrationKey("p402-binding-create"),
		),
	)
	if err != nil {
		t.Fatalf("create P4-02 media space: %v", err)
	}
	now = now.Add(time.Second)
	startInput := TransitionInput{
		ExpectedVersion: created.Space.Version,
		IdempotencyKey:  mediaIntegrationKey("p402-binding-start"),
	}
	// Two full runtime transactions plus the provider barrier can exceed a short
	// local timeout on a cold remote disposable compute. Keep this below the
	// enclosing five-minute gate while allowing both integration repository
	// budgets to complete and prove convergence rather than scheduler speed.
	startContext, cancelStart := context.WithTimeout(ctx, 2*time.Minute)
	startOutcomes := runConcurrentP402Starts(
		startContext,
		providerLifecycle,
		ownerAccess,
		created.Space.ID,
		startInput,
	)
	cancelStart()
	for _, outcome := range startOutcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent start and provider reconcile: %v", outcome.err)
		}
	}
	opened := startOutcomes[0].space
	if opened.Status != SpaceStatusOpen || opened.ActiveRoomInstance == nil ||
		opened.ActiveRoomInstance.Status != RoomInstanceActive ||
		opened.ActiveRoomInstance.ProviderRoomSID != provider.sid ||
		!opaqueProviderRoomNamePattern.MatchString(
			opened.ActiveRoomInstance.ProviderRoomName,
		) {
		t.Fatal("activated room binding did not match the persisted provider result")
	}
	providerRoomName := opened.ActiveRoomInstance.ProviderRoomName
	roomInstanceID := opened.ActiveRoomInstance.ID
	for _, outcome := range startOutcomes[1:] {
		if outcome.space.ActiveRoomInstance == nil ||
			outcome.space.ActiveRoomInstance.ID != roomInstanceID ||
			outcome.space.ActiveRoomInstance.ProviderRoomSID != provider.sid {
			t.Fatal("concurrent provider reconcile created divergent room bindings")
		}
	}
	if provider.ensureCount() != 2 {
		t.Fatalf("concurrent provider ensure count = %d, want 2 idempotent calls", provider.ensureCount())
	}
	logStage("concurrent_start")

	replayed, err := providerLifecycle.StartSpace(ctx, ownerAccess, opened.ID, startInput)
	if err != nil {
		t.Fatalf("replay bound P4-02 start: %v", err)
	}
	if replayed.ActiveRoomInstance == nil ||
		replayed.ActiveRoomInstance.ID != roomInstanceID ||
		replayed.ActiveRoomInstance.ProviderRoomSID != provider.sid ||
		provider.ensureCount() != 2 {
		t.Fatal("start replay changed the canonical provider binding")
	}
	if _, err := instances.ActivateRoomInstance(
		ctx,
		ownerAccess,
		opened.ID,
		roomInstanceID,
		provider.sid,
		now,
	); err != nil {
		t.Fatalf("idempotent exact room activation: %v", err)
	}
	if _, err := instances.ActivateRoomInstance(
		ctx,
		ownerAccess,
		opened.ID,
		roomInstanceID,
		"RM_"+strings.ReplaceAll(uuid.NewString(), "-", ""),
		now,
	); !errors.Is(err, ErrSpaceTransition) {
		t.Fatalf("conflicting provider SID activation error = %v, want transition conflict", err)
	}
	foreignAccess := p402IntegrationAccess(
		fixture.foreignTenantID,
		fixture.foreignOwnerID,
		policy.OrganizationRoleTeacher,
		uuid.New(),
	)
	if _, err := instances.ActivateRoomInstance(
		ctx,
		foreignAccess,
		opened.ID,
		roomInstanceID,
		provider.sid,
		now,
	); !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("foreign activation error = %v, want concealed room", err)
	}
	outsiderAccess := p402IntegrationAccess(
		fixture.tenantID,
		fixture.outsiderID,
		policy.OrganizationRoleStudent,
		uuid.New(),
	)
	if _, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		outsiderAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          uuid.New(),
			ExpectedRoomInstanceID: uuid.New(),
			ExpectedSpaceVersion:   opened.Version + 1,
		},
		now,
	); !isConcealedMediaError(err) {
		t.Fatalf("same-tenant inaccessible stale join error = %v, want concealed source", err)
	}
	logStage("activation_checks")

	now = now.Add(time.Second)
	ownerAttemptID := uuid.New()
	ownerAttempt, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		ownerAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          ownerAttemptID,
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	)
	if err != nil || !ownerAttempt.Created ||
		ownerAttempt.Attempt.Status != JoinAttemptAdmitted ||
		ownerAttempt.Attempt.InstanceRole != InstanceRoleHost {
		t.Fatalf("create admitted owner join attempt: result=%+v error=%v", ownerAttempt, err)
	}
	ownerAttemptRetry, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		ownerAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          ownerAttemptID,
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	)
	if err != nil || ownerAttemptRetry.Created ||
		ownerAttemptRetry.Attempt.ParticipantSessionID !=
			ownerAttempt.Attempt.ParticipantSessionID {
		t.Fatalf("retry admitted owner join attempt: result=%+v error=%v", ownerAttemptRetry, err)
	}
	ownerGrant, err := instances.PrepareCredential(
		ctx,
		ownerAccess,
		opened.ID,
		ownerAttemptID,
		now,
	)
	if err != nil {
		t.Fatalf("prepare owner room credential: %v", err)
	}
	ownerRetry, err := instances.PrepareCredential(
		ctx,
		ownerAccess,
		opened.ID,
		ownerAttemptID,
		now,
	)
	if err != nil {
		t.Fatalf("retry owner room credential: %v", err)
	}
	assertP402CredentialReplay(t, ownerGrant, ownerRetry)
	if ownerGrant.InstanceRole != InstanceRoleHost ||
		!ownerGrant.CanPublishCameraMicrophone || !ownerGrant.CanShareScreen ||
		!ownerGrant.CanSubscribe || ownerGrant.ProviderRoomName != providerRoomName ||
		strings.Contains(ownerGrant.ProviderParticipantIdentity, fixture.ownerID.String()) ||
		strings.Contains(ownerGrant.ProviderParticipantIdentity, fixture.tenantID.String()) {
		t.Fatal("owner credential authority or opaque identity is invalid")
	}

	issuer := &p402RecordingTokenIssuer{token: "signed-p402-integration-token"}
	credentialService, err := NewInstanceCredentialService(
		instances,
		issuer,
		ServiceConfig{
			ServerURL: "wss://media.integration.test",
			TokenTTL:  5 * time.Minute,
			Clock:     clock,
		},
	)
	if err != nil {
		t.Fatalf("create instance credential service: %v", err)
	}
	credential, err := credentialService.IssueInstanceCredential(
		ctx,
		ownerAccess,
		opened.ID,
		IssueInstanceCredentialInput{JoinAttemptID: ownerAttemptID},
	)
	if err != nil {
		t.Fatalf("issue signed room-instance credential: %v", err)
	}
	issuedGrant := issuer.lastGrant(t)
	if credential.AccessToken != issuer.token || credential.RoomInstanceID != roomInstanceID ||
		credential.ParticipantSessionID != ownerGrant.ParticipantSessionID ||
		credential.ExpiresAt != now.Add(5*time.Minute) ||
		issuedGrant.RoomName != providerRoomName ||
		issuedGrant.ParticipantIdentity != ownerGrant.ProviderParticipantIdentity ||
		issuedGrant.Role != string(InstanceRoleHost) ||
		!issuedGrant.CanPublishCameraMicrophone || !issuedGrant.CanShareScreen ||
		issuedGrant.CanPublishData || !issuedGrant.CanSubscribe ||
		issuedGrant.ValidFor != 5*time.Minute {
		t.Fatal("signed credential did not preserve the exact bounded token grant")
	}

	studentAccess := p402IntegrationAccess(
		fixture.tenantID,
		fixture.studentID,
		policy.OrganizationRoleStudent,
		uuid.New(),
	)
	studentAttemptID := uuid.New()
	studentWaiting, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		studentAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          studentAttemptID,
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	)
	if err != nil || !studentWaiting.Created ||
		studentWaiting.Attempt.Status != JoinAttemptWaiting ||
		studentWaiting.Attempt.AdmissionRequestID == nil {
		t.Fatalf("create waiting attendee join attempt: result=%+v error=%v", studentWaiting, err)
	}
	studentWaitingRetry, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		studentAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          studentAttemptID,
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	)
	if err != nil || studentWaitingRetry.Created ||
		studentWaitingRetry.Attempt.ParticipantSessionID !=
			studentWaiting.Attempt.ParticipantSessionID ||
		studentWaitingRetry.Attempt.AdmissionRequestID == nil ||
		*studentWaitingRetry.Attempt.AdmissionRequestID !=
			*studentWaiting.Attempt.AdmissionRequestID {
		t.Fatalf("retry waiting attendee join attempt: result=%+v error=%v", studentWaitingRetry, err)
	}
	if _, err := instances.PrepareCredential(
		ctx,
		studentAccess,
		opened.ID,
		studentAttemptID,
		now,
	); !errors.Is(err, ErrAdmissionRequired) {
		t.Fatalf(
			"default lobby credential error class = %s, want admission_required",
			p402IntegrationErrorClass(err),
		)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`DELETE FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		studentWaiting.Attempt.ParticipantSessionID,
	); err != nil {
		t.Fatalf("remove waiting participant fixture before lobby-off retry: %v", err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`DELETE FROM tutorhub.media_admission_requests
WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		*studentWaiting.Attempt.AdmissionRequestID,
	); err != nil {
		t.Fatalf("remove waiting admission fixture before lobby-off retry: %v", err)
	}
	lobbyUpdate, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.media_spaces
SET lobby_enabled = false, updated_at = $3
WHERE tenant_id = $1 AND id = $2 AND lobby_enabled = true`,
		fixture.tenantID,
		opened.ID,
		now,
	)
	if err != nil || lobbyUpdate.RowsAffected() != 1 {
		t.Fatal("prepare non-lobby credential concurrency fixture")
	}
	studentAttemptID = uuid.New()
	studentAdmitted, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		studentAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          studentAttemptID,
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	)
	if err != nil || !studentAdmitted.Created ||
		studentAdmitted.Attempt.Status != JoinAttemptAdmitted {
		t.Fatalf("create admitted lobby-off attendee attempt: result=%+v error=%v", studentAdmitted, err)
	}
	studentOutcomes := runConcurrentP402Credentials(
		ctx,
		instances,
		studentAccess,
		opened.ID,
		studentAttemptID,
		now,
	)
	if studentOutcomes[0].err != nil || studentOutcomes[1].err != nil {
		t.Fatalf(
			"concurrent credential error classes = %s/%s",
			p402IntegrationErrorClass(studentOutcomes[0].err),
			p402IntegrationErrorClass(studentOutcomes[1].err),
		)
	}
	assertP402CredentialReplay(t, studentOutcomes[0].grant, studentOutcomes[1].grant)
	logStage("credential_concurrency")
	studentGrant := studentOutcomes[0].grant
	if studentGrant.InstanceRole != InstanceRoleAttendee ||
		!studentGrant.CanPublishCameraMicrophone || studentGrant.CanShareScreen ||
		!studentGrant.CanSubscribe {
		t.Fatal("student credential authority was not least privilege")
	}
	// Keep the subsequent Unix-second webhook probe off an exact second so the
	// PostgreSQL precision-clamp assertion remains meaningful.
	now = now.Add(125 * time.Millisecond)
	roleChangedAt := now
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.class_enrollments
SET class_role = 'teaching_assistant', updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		fixture.tenantID,
		fixture.classID,
		fixture.studentID,
		roleChangedAt,
	); err != nil {
		t.Fatalf("change participant source role: %v", err)
	}
	studentRoleRefresh, err := instances.PrepareCredential(
		ctx,
		studentAccess,
		opened.ID,
		studentAttemptID,
		now,
	)
	if err != nil {
		t.Fatalf("refresh participant source authority: %v", err)
	}
	assertP402CredentialReplay(t, studentGrant, studentRoleRefresh)
	if studentRoleRefresh.InstanceRole != InstanceRoleTeachingAssistant ||
		!studentRoleRefresh.CanPublishCameraMicrophone ||
		!studentRoleRefresh.CanShareScreen {
		t.Fatal("participant role refresh did not apply current authority")
	}
	assertP402ParticipantRole(
		t,
		ctx,
		migrationPool,
		studentGrant.ParticipantSessionID,
		InstanceRoleTeachingAssistant,
	)
	now = now.Add(250 * time.Millisecond)
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.class_enrollments
SET class_role = 'student', updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		fixture.tenantID,
		fixture.classID,
		fixture.studentID,
		now,
	); err != nil {
		t.Fatalf("downgrade participant source role: %v", err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.media_spaces
SET lobby_enabled = true, updated_at = $3
WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		opened.ID,
		now,
	); err != nil {
		t.Fatalf("restore lobby before current-role credential check: %v", err)
	}
	if _, err := instances.PrepareCredential(
		ctx,
		studentAccess,
		opened.ID,
		studentAttemptID,
		now,
	); !errors.Is(err, ErrAdmissionRequired) {
		t.Fatalf("downgraded lobby-bypass credential error = %v, want admission required", err)
	}
	if _, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		studentAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          uuid.New(),
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	); !errors.Is(err, ErrParticipantConflict) {
		t.Fatalf("second active join attempt error = %v, want participant conflict", err)
	}
	if _, err := instances.PrepareCredential(
		ctx,
		foreignAccess,
		opened.ID,
		uuid.New(),
		now,
	); !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("foreign credential error = %v, want concealed room", err)
	}

	teacherAccess := p402IntegrationAccess(
		fixture.tenantID,
		fixture.teacherID,
		policy.OrganizationRoleTeacher,
		uuid.New(),
	)
	teacherAttemptID := uuid.New()
	teacherAttempt, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		teacherAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          teacherAttemptID,
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	)
	if err != nil || !teacherAttempt.Created ||
		teacherAttempt.Attempt.Status != JoinAttemptAdmitted {
		t.Fatalf("create admitted teacher join attempt: result=%+v error=%v", teacherAttempt, err)
	}
	var teacherGrant ParticipantCredentialGrant
	for request := 0; request < int(mediaCredentialRateLimit); request++ {
		teacherGrant, err = instances.PrepareCredential(
			ctx,
			teacherAccess,
			opened.ID,
			teacherAttemptID,
			now,
		)
		if err != nil {
			t.Fatalf("credential request %d before rate limit: %v", request+1, err)
		}
	}
	if teacherGrant.InstanceRole != InstanceRoleCoHost ||
		!teacherGrant.CanPublishCameraMicrophone || !teacherGrant.CanShareScreen {
		t.Fatal("teacher credential authority was not the expected co-host grant")
	}
	if _, err := instances.PrepareCredential(
		ctx,
		teacherAccess,
		opened.ID,
		teacherAttemptID,
		now,
	); err == nil {
		t.Fatal("credential request above rate limit unexpectedly succeeded")
	} else {
		var rateLimit *CredentialRateLimitError
		if !errors.As(err, &rateLimit) || rateLimit.RetryAfter <= 0 ||
			rateLimit.RetryAfter > mediaCredentialRateWindow {
			t.Fatalf("rate-limit error = %v, want bounded retry metadata", err)
		}
	}

	setMediaQuotaOverrides(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.adminID,
		map[featurecontrol.QuotaKey]int64{
			featurecontrol.QuotaMediaParticipantsPerSpace: 3,
			featurecontrol.QuotaActiveMediaParticipants:   3,
		},
	)
	adminAccess := p402IntegrationAccess(
		fixture.tenantID,
		fixture.adminID,
		policy.OrganizationRoleAdmin,
		uuid.New(),
	)
	if _, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		adminAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          uuid.New(),
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	); !errors.Is(err, featurecontrol.ErrQuotaExceeded) {
		t.Fatalf("participant quota error = %v, want quota exceeded", err)
	}
	assertP402ParticipantCount(t, ctx, migrationPool, opened.ID, fixture.adminID, 0)
	setMediaQuotaOverrides(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.adminID,
		map[featurecontrol.QuotaKey]int64{
			featurecontrol.QuotaMediaParticipantsPerSpace: 50,
			featurecontrol.QuotaActiveMediaParticipants:   100,
		},
	)

	coTeacherAccess := p402IntegrationAccess(
		fixture.tenantID,
		fixture.coTeacherID,
		policy.OrganizationRoleStudent,
		uuid.New(),
	)
	coTeacherAttemptID := uuid.New()
	coTeacherAttempt, err := instances.CreateOrReuseJoinAttempt(
		ctx,
		coTeacherAccess,
		opened.ID,
		CreateJoinAttemptInput{
			JoinAttemptID:          coTeacherAttemptID,
			ExpectedRoomInstanceID: roomInstanceID,
			ExpectedSpaceVersion:   opened.Version,
		},
		now,
	)
	if err != nil || !coTeacherAttempt.Created ||
		coTeacherAttempt.Attempt.Status != JoinAttemptAdmitted {
		t.Fatalf("create admitted co-teacher join attempt: result=%+v error=%v", coTeacherAttempt, err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.media_spaces
SET locked = true, updated_at = $3
WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		opened.ID,
		now,
	); err != nil {
		t.Fatalf("lock integration media space: %v", err)
	}
	if _, err := instances.PrepareCredential(
		ctx,
		outsiderAccess,
		opened.ID,
		uuid.New(),
		now,
	); !isConcealedMediaError(err) {
		t.Fatalf("same-tenant inaccessible locked credential error = %v, want concealed source", err)
	}
	if _, err := instances.PrepareCredential(
		ctx,
		coTeacherAccess,
		opened.ID,
		coTeacherAttemptID,
		now,
	); !errors.Is(err, ErrRoomLocked) {
		t.Fatalf("locked-room admitted-attempt credential error = %v, want room locked", err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.media_spaces
SET locked = false, updated_at = $3
WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		opened.ID,
		now,
	); err != nil {
		t.Fatalf("unlock integration media space: %v", err)
	}

	webhookReceivedAt := now.Add(2 * time.Second)
	legacy := &p402LegacyService{}
	webhooks, err := NewProviderWebhookService(
		instances,
		legacy,
		func() time.Time { return webhookReceivedAt },
	)
	if err != nil {
		t.Fatalf("create canonical provider webhook service: %v", err)
	}
	teacherStateAt := p402ParticipantCreatedAt(
		t,
		ctx,
		migrationPool,
		teacherGrant.ParticipantSessionID,
	)
	teacherUnixSecondEvent := WebhookEvent{
		ID:                  p402EventID("participant-joined-unix-second"),
		EventType:           "participant_joined",
		RoomName:            providerRoomName,
		RoomSID:             provider.sid,
		ParticipantIdentity: teacherGrant.ProviderParticipantIdentity,
		ParticipantSID:      "PA_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		OccurredAt:          teacherStateAt.Truncate(time.Second),
	}
	result, err := webhooks.RecordWebhook(ctx, teacherUnixSecondEvent)
	if err != nil || !result.Recorded || result.Ignored || result.Duplicate {
		t.Fatalf("record Unix-second participant webhook: result=%+v error=%v", result, err)
	}
	assertP402ParticipantTransitionTime(
		t,
		ctx,
		migrationPool,
		teacherGrant.ParticipantSessionID,
		teacherStateAt,
		teacherUnixSecondEvent.OccurredAt,
	)
	assertP402Receipt(t, ctx, migrationPool, teacherUnixSecondEvent.ID, "applied", 1)
	webhookReceivedAt = webhookReceivedAt.Add(time.Second)
	joinedAt := now.Add(time.Second)
	joinedEvent := WebhookEvent{
		ID:                  p402EventID("participant-joined"),
		EventType:           "participant_joined",
		RoomName:            providerRoomName,
		RoomSID:             provider.sid,
		ParticipantIdentity: studentGrant.ProviderParticipantIdentity,
		ParticipantSID:      "PA_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		OccurredAt:          joinedAt,
	}
	webhookOutcomes := runConcurrentP402Webhooks(
		ctx,
		webhooks,
		[]WebhookEvent{joinedEvent, joinedEvent},
	)
	recordedWebhooks := 0
	duplicateWebhooks := 0
	for _, outcome := range webhookOutcomes {
		if outcome.err != nil || outcome.result.Ignored {
			t.Fatalf(
				"concurrent participant joined webhook: result=%+v error=%v",
				outcome.result,
				outcome.err,
			)
		}
		if outcome.result.Recorded {
			recordedWebhooks++
		}
		if outcome.result.Duplicate {
			duplicateWebhooks++
		}
	}
	if recordedWebhooks != 1 || duplicateWebhooks != 1 {
		t.Fatalf(
			"concurrent webhook outcomes recorded=%d duplicate=%d, want 1/1",
			recordedWebhooks,
			duplicateWebhooks,
		)
	}
	assertP402ParticipantState(
		t,
		ctx,
		migrationPool,
		studentGrant.ParticipantSessionID,
		"connected",
		true,
	)
	assertP402Receipt(t, ctx, migrationPool, joinedEvent.ID, "applied", 1)
	logStage("webhook_concurrency")

	staleLeft := WebhookEvent{
		ID:                  p402EventID("participant-left-stale"),
		EventType:           "participant_left",
		RoomName:            providerRoomName,
		RoomSID:             provider.sid,
		ParticipantIdentity: studentGrant.ProviderParticipantIdentity,
		ParticipantSID:      joinedEvent.ParticipantSID,
		OccurredAt:          joinedAt.Add(-time.Second),
	}
	webhookReceivedAt = webhookReceivedAt.Add(time.Second)
	result, err = webhooks.RecordWebhook(ctx, staleLeft)
	if err != nil || !result.Recorded || !result.Ignored {
		t.Fatalf("record stale participant webhook: result=%+v error=%v", result, err)
	}
	assertP402Receipt(t, ctx, migrationPool, staleLeft.ID, "ignored_stale", 1)
	assertP402ParticipantState(
		t,
		ctx,
		migrationPool,
		studentGrant.ParticipantSessionID,
		"connected",
		true,
	)

	mismatchRoomName := "r_" + strings.Repeat("f", 32)
	mismatchEvent := WebhookEvent{
		ID:         p402EventID("binding-mismatch"),
		EventType:  "track_published",
		RoomName:   mismatchRoomName,
		RoomSID:    provider.sid,
		OccurredAt: now.Add(2 * time.Second),
	}
	webhookReceivedAt = webhookReceivedAt.Add(time.Second)
	result, err = webhooks.RecordWebhook(ctx, mismatchEvent)
	if err != nil || !result.Recorded || !result.Ignored {
		t.Fatalf("record mismatched provider binding: result=%+v error=%v", result, err)
	}
	assertP402Receipt(t, ctx, migrationPool, mismatchEvent.ID, "ignored_mismatch", 1)

	privateUnknownIdentity := "private-user-" + uuid.NewString() + "@example.test"
	unknownParticipant := WebhookEvent{
		ID:                  p402EventID("unknown-participant"),
		EventType:           "participant_joined",
		RoomName:            providerRoomName,
		RoomSID:             provider.sid,
		ParticipantIdentity: privateUnknownIdentity,
		ParticipantSID:      "PA_private_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		OccurredAt:          now.Add(3 * time.Second),
	}
	webhookReceivedAt = webhookReceivedAt.Add(time.Second)
	result, err = webhooks.RecordWebhook(ctx, unknownParticipant)
	if err != nil || !result.Recorded || !result.Ignored {
		t.Fatalf("record unknown participant webhook: result=%+v error=%v", result, err)
	}
	assertP402Receipt(
		t,
		ctx,
		migrationPool,
		unknownParticipant.ID,
		"ignored_unknown_participant",
		1,
	)

	leftEvent := WebhookEvent{
		ID:                  p402EventID("participant-left"),
		EventType:           "participant_left",
		RoomName:            providerRoomName,
		RoomSID:             provider.sid,
		ParticipantIdentity: studentGrant.ProviderParticipantIdentity,
		ParticipantSID:      joinedEvent.ParticipantSID,
		OccurredAt:          now.Add(4 * time.Second),
	}
	webhookReceivedAt = now.Add(5 * time.Second)
	result, err = webhooks.RecordWebhook(ctx, leftEvent)
	if err != nil || !result.Recorded || result.Ignored {
		t.Fatalf("record participant left webhook: result=%+v error=%v", result, err)
	}
	assertP402ParticipantState(
		t,
		ctx,
		migrationPool,
		studentGrant.ParticipantSessionID,
		"left",
		false,
	)

	now = now.Add(6 * time.Second)
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.class_enrollments
SET status = 'removed', suspended_at = NULL, left_at = NULL,
    removed_at = $4, updated_at = $4
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3`,
		fixture.tenantID,
		fixture.classID,
		fixture.studentID,
		now,
	); err != nil {
		t.Fatalf("revoke previously joined participant source authority: %v", err)
	}
	if _, err := instances.PrepareCredential(
		ctx,
		studentAccess,
		opened.ID,
		uuid.New(),
		now,
	); !isConcealedMediaError(err) {
		t.Fatalf("revoked source credential error = %v, want concealed denial", err)
	}
	assertP402ParticipantCount(t, ctx, migrationPool, opened.ID, fixture.studentID, 1)

	unknownRoomEvent := WebhookEvent{
		ID:         p402EventID("unknown-room-and-sid"),
		EventType:  "room_started",
		RoomName:   "r_" + strings.Repeat("e", 32),
		RoomSID:    "RM_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		OccurredAt: now,
	}
	webhookReceivedAt = now.Add(time.Second)
	result, err = webhooks.RecordWebhook(ctx, unknownRoomEvent)
	if err != nil || !result.Ignored || result.Recorded || result.Duplicate {
		t.Fatalf("unknown room and SID webhook result=%+v error=%v", result, err)
	}
	assertP402Receipt(t, ctx, migrationPool, unknownRoomEvent.ID, "", 0)

	legacyAllowed, err := instances.AllowLegacyMedia(ctx, fixture.tenantID)
	if err != nil || legacyAllowed {
		t.Fatalf("legacy media gate = allowed:%t error:%v, want disabled", legacyAllowed, err)
	}
	legacyEvent := WebhookEvent{
		ID:         p402EventID("legacy-disabled"),
		EventType:  "room_started",
		RoomName:   RoomName(fixture.tenantID, fixture.classID),
		RoomSID:    "RM_legacy_not_bound",
		OccurredAt: now,
	}
	webhookReceivedAt = now.Add(time.Second)
	result, err = webhooks.RecordWebhook(ctx, legacyEvent)
	if err != nil || !result.Ignored || result.Recorded || result.Duplicate {
		t.Fatalf("disabled legacy webhook result=%+v error=%v", result, err)
	}
	if legacy.webhookCount() != 0 {
		t.Fatalf("legacy webhook calls = %d, want 0", legacy.webhookCount())
	}
	assertP402LegacyWriteCount(t, ctx, migrationPool, fixture.tenantID, 0)

	ownerJoinEvent := WebhookEvent{
		ID:                  p402EventID("owner-concurrent-join"),
		EventType:           "participant_joined",
		RoomName:            providerRoomName,
		RoomSID:             provider.sid,
		ParticipantIdentity: ownerGrant.ProviderParticipantIdentity,
		ParticipantSID:      "PA_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		OccurredAt:          now.Add(time.Second),
	}
	ownerLeftEvent := WebhookEvent{
		ID:                  p402EventID("owner-concurrent-left"),
		EventType:           "participant_left",
		RoomName:            providerRoomName,
		RoomSID:             provider.sid,
		ParticipantIdentity: ownerGrant.ProviderParticipantIdentity,
		ParticipantSID:      ownerJoinEvent.ParticipantSID,
		OccurredAt:          now.Add(2 * time.Second),
	}
	webhookReceivedAt = now.Add(3 * time.Second)
	ownerOutcomes := runConcurrentP402Webhooks(
		ctx,
		webhooks,
		[]WebhookEvent{ownerJoinEvent, ownerLeftEvent},
	)
	for _, outcome := range ownerOutcomes {
		if outcome.err != nil || !outcome.result.Recorded || outcome.result.Duplicate {
			t.Fatalf(
				"concurrent connect/leave outcome: result=%+v error=%v",
				outcome.result,
				outcome.err,
			)
		}
	}
	ownerJoinDisposition := "applied"
	if ownerOutcomes[0].result.Ignored {
		ownerJoinDisposition = "ignored_stale"
	}
	assertP402Receipt(t, ctx, migrationPool, ownerJoinEvent.ID, ownerJoinDisposition, 1)
	assertP402Receipt(t, ctx, migrationPool, ownerLeftEvent.ID, "applied", 1)
	assertP402ParticipantState(
		t,
		ctx,
		migrationPool,
		ownerGrant.ParticipantSessionID,
		"left",
		false,
	)

	finishedEvent := WebhookEvent{
		ID:         p402EventID("room-finished"),
		EventType:  "room_finished",
		RoomName:   providerRoomName,
		RoomSID:    provider.sid,
		OccurredAt: now.Add(time.Second),
	}
	webhookReceivedAt = now.Add(2 * time.Second)
	result, err = webhooks.RecordWebhook(ctx, finishedEvent)
	if err != nil || !result.Recorded || result.Ignored {
		t.Fatalf("record room finished webhook: result=%+v error=%v", result, err)
	}
	assertP402RoomBinding(
		t,
		ctx,
		migrationPool,
		roomInstanceID,
		RoomInstanceClosing,
		provider.sid,
	)
	assertP402ParticipantState(
		t,
		ctx,
		migrationPool,
		ownerGrant.ParticipantSessionID,
		"left",
		false,
	)
	assertP402ParticipantState(
		t,
		ctx,
		migrationPool,
		teacherGrant.ParticipantSessionID,
		"left",
		false,
	)
	assertP402RoomCapacityReleased(t, ctx, migrationPool, roomInstanceID)
	terminalRejoin := WebhookEvent{
		ID:                  p402EventID("participant-rejoin-terminal-room"),
		EventType:           "participant_joined",
		RoomName:            providerRoomName,
		RoomSID:             provider.sid,
		ParticipantIdentity: ownerGrant.ProviderParticipantIdentity,
		ParticipantSID:      "PA_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		OccurredAt:          finishedEvent.OccurredAt.Add(time.Second),
	}
	webhookReceivedAt = webhookReceivedAt.Add(time.Second)
	result, err = webhooks.RecordWebhook(ctx, terminalRejoin)
	if err != nil || !result.Recorded || !result.Ignored || result.Duplicate {
		t.Fatalf("record terminal participant rejoin: result=%+v error=%v", result, err)
	}
	assertP402Receipt(t, ctx, migrationPool, terminalRejoin.ID, "ignored_terminal", 1)
	assertP402ParticipantState(
		t,
		ctx,
		migrationPool,
		ownerGrant.ParticipantSessionID,
		"left",
		false,
	)

	now = now.Add(3 * time.Second)
	logStage("before_end")
	ended, err := providerLifecycle.EndSpace(
		ctx,
		ownerAccess,
		opened.ID,
		TransitionInput{
			ExpectedVersion: opened.Version,
			IdempotencyKey:  mediaIntegrationKey("p402-binding-end"),
		},
	)
	if err != nil || ended.Status != SpaceStatusEnded {
		t.Fatalf(
			"end bound provider room error class = %s operation = %s",
			p402IntegrationErrorClass(err),
			p402IntegrationErrorOperation(err),
		)
	}
	logStage("ended")
	if provider.deleteCount(providerRoomName) != 1 {
		t.Fatalf("provider delete count = %d, want 1", provider.deleteCount(providerRoomName))
	}
	if _, err := instances.PrepareCredential(
		ctx,
		ownerAccess,
		opened.ID,
		uuid.New(),
		now,
	); !errors.Is(err, ErrRoomNotOpen) {
		t.Fatalf("ended-room credential error = %v, want room not open", err)
	}

	now = now.Add(time.Second)
	failedCreated, err := lifecycle.CreateSpace(
		ctx,
		ownerAccess,
		officialMediaCreateInput(
			fixture.sessionIDs["teacher"],
			mediaIntegrationKey("p402-failed-create"),
		),
	)
	if err != nil {
		t.Fatalf("create failed-intent media space: %v", err)
	}
	now = now.Add(time.Second)
	failedOpened, err := lifecycle.StartSpace(
		ctx,
		ownerAccess,
		failedCreated.Space.ID,
		TransitionInput{
			ExpectedVersion: failedCreated.Space.Version,
			IdempotencyKey:  mediaIntegrationKey("p402-failed-start"),
		},
	)
	if err != nil || failedOpened.ActiveRoomInstance == nil ||
		failedOpened.ActiveRoomInstance.Status != RoomInstanceProvisioning {
		t.Fatalf("open failed-intent provisioning room: %v", err)
	}
	failedRoom := failedOpened.ActiveRoomInstance
	if _, err := instances.PrepareCredential(
		ctx,
		ownerAccess,
		failedOpened.ID,
		uuid.New(),
		now,
	); !errors.Is(err, ErrRoomNotOpen) {
		t.Fatalf("provisioning credential error = %v, want room not open", err)
	}
	failedEvent := WebhookEvent{
		ID:         p402EventID("provisioning-room-finished"),
		EventType:  "room_finished",
		RoomName:   failedRoom.ProviderRoomName,
		RoomSID:    "RM_" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		OccurredAt: failedRoom.CreatedAt.Truncate(time.Second),
	}
	webhookReceivedAt = failedRoom.CreatedAt.Add(2 * time.Second)
	result, err = webhooks.RecordWebhook(ctx, failedEvent)
	if err != nil || !result.Recorded || result.Ignored || result.Duplicate {
		t.Fatalf("fail provisioning room through webhook: result=%+v error=%v", result, err)
	}
	assertP402FailedRoom(
		t,
		ctx,
		migrationPool,
		failedRoom.ID,
		failedRoom.CreatedAt,
	)
	if _, err := instances.PrepareCredential(
		ctx,
		ownerAccess,
		failedOpened.ID,
		uuid.New(),
		now,
	); !errors.Is(err, ErrRoomNotOpen) {
		t.Fatalf("failed-room credential error = %v, want room not open", err)
	}
	now = failedRoom.CreatedAt.Add(3 * time.Second)
	failedEnded, err := providerLifecycle.EndSpace(
		ctx,
		ownerAccess,
		failedOpened.ID,
		TransitionInput{
			ExpectedVersion: failedOpened.Version,
			IdempotencyKey:  mediaIntegrationKey("p402-failed-end"),
		},
	)
	if err != nil || failedEnded.Status != SpaceStatusEnded {
		t.Fatalf("owner end failed room intent: %v", err)
	}
	if provider.deleteCount(failedRoom.ProviderRoomName) != 1 {
		t.Fatal("owner end did not clean up the failed provider room intent")
	}
	assertP402FailedRoom(
		t,
		ctx,
		migrationPool,
		failedRoom.ID,
		failedRoom.CreatedAt,
	)
	mismatchedParticipantReceiptID := p402EventID("composite-participant-fk")
	if _, err := migrationPool.Exec(
		ctx,
		`INSERT INTO tutorhub.media_provider_webhook_receipts (
    provider_kind, event_id, tenant_id, space_id, room_instance_id,
    participant_session_id, event_type, disposition, occurred_at,
    received_at, retention_until
) VALUES ('livekit', $1, $2, $3, $4, $5, 'participant_joined',
          'applied', $6, $6, $7)`,
		mismatchedParticipantReceiptID,
		fixture.tenantID,
		failedOpened.ID,
		failedRoom.ID,
		ownerGrant.ParticipantSessionID,
		now,
		now.Add(24*time.Hour),
	); err == nil {
		t.Fatal("composite participant receipt foreign key accepted a different room binding")
	}
	assertP402Receipt(t, ctx, migrationPool, mismatchedParticipantReceiptID, "", 0)

	assertP402WebhookPrivacy(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		providerRoomName,
		provider.sid,
		studentGrant.ProviderParticipantIdentity,
		privateUnknownIdentity,
		unknownParticipant.ParticipantSID,
	)

	now = now.Add(time.Second)
	cascadeCreated, err := lifecycle.CreateSpace(
		ctx,
		ownerAccess,
		officialMediaCreateInput(
			fixture.archiveSessionID,
			mediaIntegrationKey("p402-cascade-create"),
		),
	)
	if err != nil {
		t.Fatalf("create webhook cascade space: %v", err)
	}
	now = now.Add(time.Second)
	cascadeOpened, err := lifecycle.StartSpace(
		ctx,
		ownerAccess,
		cascadeCreated.Space.ID,
		TransitionInput{
			ExpectedVersion: cascadeCreated.Space.Version,
			IdempotencyKey:  mediaIntegrationKey("p402-cascade-start"),
		},
	)
	if err != nil || cascadeOpened.ActiveRoomInstance == nil ||
		cascadeOpened.ActiveRoomInstance.Status != RoomInstanceProvisioning {
		t.Fatalf("create provisioning cascade room: %v", err)
	}
	cascadeRoom := cascadeOpened.ActiveRoomInstance
	cascadeSID := "RM_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	cascadeStartedEvent := WebhookEvent{
		ID:         p402EventID("cascade-room-started"),
		EventType:  "room_started",
		RoomName:   cascadeRoom.ProviderRoomName,
		RoomSID:    cascadeSID,
		OccurredAt: now.Add(time.Second),
	}
	webhookReceivedAt = now.Add(2 * time.Second)
	result, err = webhooks.RecordWebhook(ctx, cascadeStartedEvent)
	if err != nil || !result.Recorded || result.Ignored {
		t.Fatalf("activate room through signed webhook: result=%+v error=%v", result, err)
	}
	assertP402Receipt(t, ctx, migrationPool, cascadeStartedEvent.ID, "applied", 1)
	assertP402RoomBinding(
		t,
		ctx,
		migrationPool,
		cascadeRoom.ID,
		RoomInstanceActive,
		cascadeSID,
	)
	now = now.Add(3 * time.Second)
	if _, err := providerLifecycle.EndSpace(
		ctx,
		ownerAccess,
		cascadeOpened.ID,
		TransitionInput{
			ExpectedVersion: cascadeOpened.Version,
			IdempotencyKey:  mediaIntegrationKey("p402-cascade-end"),
		},
	); err != nil {
		t.Fatalf("end webhook cascade room: %v", err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`DELETE FROM tutorhub.media_spaces WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID,
		cascadeOpened.ID,
	); err != nil {
		t.Fatalf("delete isolated cascade test space: %v", err)
	}
	assertP402Receipt(t, ctx, migrationPool, cascadeStartedEvent.ID, "", 0)
	var cascadeRooms int
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.media_room_instances
WHERE tenant_id = $1 AND space_id = $2`,
		fixture.tenantID,
		cascadeOpened.ID,
	).Scan(&cascadeRooms); err != nil {
		t.Fatalf("count cascaded room instances: %v", err)
	}
	if cascadeRooms != 0 {
		t.Fatalf("cascaded room instances = %d, want 0", cascadeRooms)
	}
}

func newP402IntegrationServices(
	t *testing.T,
	pool *pgxpool.Pool,
	clock func() time.Time,
) (*LifecycleService, *PostgresInstanceRepository) {
	t.Helper()
	base, _ := newMediaIntegrationServices(t, pool)
	repository, ok := base.repository.(*PostgresLifecycleRepository)
	if !ok {
		t.Fatal("media integration lifecycle repository is not PostgreSQL")
	}
	// A contender starts its timeout before the first transaction releases the
	// exact space/room locks. Remote disposable latency therefore needs a larger
	// harness-only budget than local PostgreSQL; production defaults are unchanged.
	repository.queryTimeout = 60 * time.Second
	lifecycle, err := NewLifecycleService(
		repository,
		LifecycleServiceConfig{Clock: clock, NewID: uuid.New},
	)
	if err != nil {
		t.Fatalf("create deterministic media lifecycle service: %v", err)
	}
	instances, err := NewPostgresInstanceRepository(repository, uuid.New)
	if err != nil {
		t.Fatalf("create room-instance PostgreSQL repository: %v", err)
	}
	return lifecycle, instances
}

func assertP402SeparatedDatabaseRoles(
	t *testing.T,
	ctx context.Context,
	migrationPool *pgxpool.Pool,
	runtimePool *pgxpool.Pool,
) {
	t.Helper()
	var migrationRole string
	var runtimeRole string
	if err := migrationPool.QueryRow(ctx, `SELECT current_user`).Scan(&migrationRole); err != nil {
		t.Fatal("verify P4-02 migration database role")
	}
	if err := runtimePool.QueryRow(ctx, `SELECT current_user`).Scan(&runtimeRole); err != nil {
		t.Fatal("verify P4-02 runtime database role")
	}
	if migrationRole == runtimeRole {
		t.Fatal("P4-02 integration requires separate migration-owner and runtime database roles")
	}
}

func p402IntegrationAccess(
	tenantID uuid.UUID,
	actorID uuid.UUID,
	role policy.OrganizationRole,
	sessionID uuid.UUID,
) AccessContext {
	access := mediaIntegrationAccess(tenantID, actorID, role)
	access.SessionID = sessionID
	access.DisplayName = "P4-02 integration actor"
	return access
}

type p402CredentialOutcome struct {
	grant ParticipantCredentialGrant
	err   error
}

type p402StartOutcome struct {
	space MediaSpace
	err   error
}

type p402WebhookOutcome struct {
	result WebhookResult
	err    error
}

func runConcurrentP402Starts(
	ctx context.Context,
	service *ProviderLifecycleService,
	access AccessContext,
	spaceID uuid.UUID,
	input TransitionInput,
) []p402StartOutcome {
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	outcomes := make([]p402StartOutcome, 2)
	var wait sync.WaitGroup
	wait.Add(len(outcomes))
	for index := range outcomes {
		go func(index int) {
			defer wait.Done()
			ready <- struct{}{}
			<-start
			outcomes[index].space, outcomes[index].err = service.StartSpace(
				ctx,
				access,
				spaceID,
				input,
			)
		}(index)
	}
	for range outcomes {
		<-ready
	}
	close(start)
	wait.Wait()
	return outcomes
}

func runConcurrentP402Webhooks(
	ctx context.Context,
	service *ProviderWebhookService,
	events []WebhookEvent,
) []p402WebhookOutcome {
	ready := make(chan struct{}, len(events))
	start := make(chan struct{})
	outcomes := make([]p402WebhookOutcome, len(events))
	var wait sync.WaitGroup
	wait.Add(len(outcomes))
	for index := range outcomes {
		go func(index int) {
			defer wait.Done()
			ready <- struct{}{}
			<-start
			outcomes[index].result, outcomes[index].err = service.RecordWebhook(
				ctx,
				events[index],
			)
		}(index)
	}
	for range outcomes {
		<-ready
	}
	close(start)
	wait.Wait()
	return outcomes
}

func runConcurrentP402Credentials(
	ctx context.Context,
	repository *PostgresInstanceRepository,
	access AccessContext,
	spaceID uuid.UUID,
	joinAttemptID uuid.UUID,
	now time.Time,
) []p402CredentialOutcome {
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	outcomes := make([]p402CredentialOutcome, 2)
	var wait sync.WaitGroup
	wait.Add(len(outcomes))
	for index := range outcomes {
		go func(index int) {
			defer wait.Done()
			ready <- struct{}{}
			<-start
			outcomes[index].grant, outcomes[index].err = repository.PrepareCredential(
				ctx,
				access,
				spaceID,
				joinAttemptID,
				now,
			)
		}(index)
	}
	for range outcomes {
		<-ready
	}
	close(start)
	wait.Wait()
	return outcomes
}

func p402IntegrationErrorClass(err error) string {
	if err == nil {
		return "none"
	}
	var rateLimit *CredentialRateLimitError
	switch {
	case errors.As(err, &rateLimit):
		return "rate_limit"
	case errors.Is(err, ErrLifecycleUnavailable):
		return "lifecycle_unavailable"
	case errors.Is(err, ErrRoomNotOpen):
		return "room_not_open"
	case errors.Is(err, ErrRoomLocked):
		return "room_locked"
	case errors.Is(err, ErrAdmissionRequired):
		return "admission_required"
	case errors.Is(err, ErrParticipantConflict):
		return "participant_conflict"
	case errors.Is(err, ErrSpaceAccessDenied):
		return "access_denied"
	case errors.Is(err, ErrSpaceNotFound):
		return "space_not_found"
	case errors.Is(err, ErrSourceUnavailable):
		return "source_unavailable"
	case errors.Is(err, ErrSpaceVersionConflict):
		return "space_version_conflict"
	case errors.Is(err, ErrSpaceTransition):
		return "space_transition"
	case errors.Is(err, featurecontrol.ErrFeatureDisabled):
		return "feature_disabled"
	case errors.Is(err, featurecontrol.ErrQuotaExceeded):
		return "quota_exceeded"
	default:
		return "unknown"
	}
}

func p402IntegrationErrorOperation(err error) string {
	if err == nil {
		return "none"
	}
	message := err.Error()
	operations := []struct {
		prefix string
		code   string
	}{
		{"begin media space transition:", "begin_transition"},
		{"authorize media membership:", "authorize_membership"},
		{"load media transition receipt:", "load_transition_receipt"},
		{"lock media space:", "lock_space"},
		{"lock active room instance:", "lock_room"},
		{"terminate media room participants:", "terminate_participants"},
		{"end media space:", "end_space"},
		{"append media space event:", "append_event"},
		{"commit media space transition:", "commit_transition"},
		{"begin provider room lookup:", "begin_room_lookup"},
		{"load provider room space:", "load_room_space"},
		{"load provider room name:", "load_room_name"},
		{"commit provider room lookup:", "commit_room_lookup"},
	}
	for _, operation := range operations {
		if strings.HasPrefix(message, operation.prefix) {
			return operation.code
		}
	}
	return "unknown"
}

func assertP402CredentialReplay(
	t *testing.T,
	first ParticipantCredentialGrant,
	second ParticipantCredentialGrant,
) {
	t.Helper()
	if first.ParticipantSessionID == uuid.Nil ||
		first.ParticipantSessionID != second.ParticipantSessionID ||
		first.RoomInstanceID != second.RoomInstanceID ||
		first.JoinAttemptID != second.JoinAttemptID ||
		first.ProviderRoomName != second.ProviderRoomName ||
		first.ProviderParticipantIdentity != second.ProviderParticipantIdentity {
		t.Fatal("credential retry changed the canonical participant binding")
	}
}

func assertP402ParticipantCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	spaceID uuid.UUID,
	userID uuid.UUID,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.media_participant_sessions
WHERE space_id = $1 AND user_id = $2`,
		spaceID,
		userID,
	).Scan(&count); err != nil {
		t.Fatalf("count P4-02 participant sessions: %v", err)
	}
	if count != want {
		t.Fatalf("P4-02 participant sessions = %d, want %d", count, want)
	}
}

func assertP402RoomCapacityReleased(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roomInstanceID uuid.UUID,
) {
	t.Helper()
	var reserved int
	var active int
	if err := pool.QueryRow(
		ctx,
		`SELECT
    count(*) FILTER (WHERE capacity_reserved),
    count(*) FILTER (
        WHERE status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')
    )
FROM tutorhub.media_participant_sessions
WHERE room_instance_id = $1`,
		roomInstanceID,
	).Scan(&reserved, &active); err != nil {
		t.Fatal("inspect P4-02 room participant capacity")
	}
	if reserved != 0 || active != 0 {
		t.Fatal("room_finished did not release all participant capacity")
	}
}

func assertP402ParticipantState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	participantSessionID uuid.UUID,
	wantStatus string,
	wantReserved bool,
) {
	t.Helper()
	var status string
	var capacityReserved bool
	if err := pool.QueryRow(
		ctx,
		`SELECT status, capacity_reserved
FROM tutorhub.media_participant_sessions
WHERE id = $1`,
		participantSessionID,
	).Scan(&status, &capacityReserved); err != nil {
		t.Fatalf("read P4-02 participant state: %v", err)
	}
	if status != wantStatus || capacityReserved != wantReserved {
		t.Fatalf(
			"P4-02 participant state = %s/%t, want %s/%t",
			status,
			capacityReserved,
			wantStatus,
			wantReserved,
		)
	}
}

func p402ParticipantCreatedAt(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	participantSessionID uuid.UUID,
) time.Time {
	t.Helper()
	var createdAt time.Time
	if err := pool.QueryRow(
		ctx,
		`SELECT created_at
FROM tutorhub.media_participant_sessions
WHERE id = $1`,
		participantSessionID,
	).Scan(&createdAt); err != nil {
		t.Fatal("read P4-02 participant transition baseline")
	}
	return createdAt.UTC()
}

func assertP402ParticipantTransitionTime(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	participantSessionID uuid.UUID,
	wantClampedAt time.Time,
	providerOccurredAt time.Time,
) {
	t.Helper()
	var connectedAt time.Time
	var updatedAt time.Time
	if err := pool.QueryRow(
		ctx,
		`SELECT connected_at, updated_at
FROM tutorhub.media_participant_sessions
WHERE id = $1`,
		participantSessionID,
	).Scan(&connectedAt, &updatedAt); err != nil {
		t.Fatal("read P4-02 participant transition timestamp")
	}
	providerSecond := providerOccurredAt.Nanosecond() == 0
	connectedMatches := connectedAt.UTC().Equal(wantClampedAt.UTC())
	updatedMatches := updatedAt.UTC().Equal(wantClampedAt.UTC())
	connectedAfterProvider := connectedAt.After(providerOccurredAt)
	if !providerSecond || !connectedMatches || !updatedMatches || !connectedAfterProvider {
		t.Fatalf(
			"Unix-second provider timestamp clamp failed: provider_second=%t connected_matches=%t updated_matches=%t connected_after_provider=%t",
			providerSecond,
			connectedMatches,
			updatedMatches,
			connectedAfterProvider,
		)
	}
}

func assertP402ParticipantRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	participantSessionID uuid.UUID,
	wantRole InstanceRole,
) {
	t.Helper()
	var role InstanceRole
	if err := pool.QueryRow(
		ctx,
		`SELECT instance_role
FROM tutorhub.media_participant_sessions
WHERE id = $1`,
		participantSessionID,
	).Scan(&role); err != nil {
		t.Fatalf("read P4-02 participant role: %v", err)
	}
	if role != wantRole {
		t.Fatalf("P4-02 participant role = %s, want %s", role, wantRole)
	}
}

func assertP402Receipt(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	eventID string,
	wantDisposition string,
	wantCount int,
) {
	t.Helper()
	var count int
	var disposition string
	err := pool.QueryRow(
		ctx,
		`SELECT count(*), COALESCE(max(disposition), '')
FROM tutorhub.media_provider_webhook_receipts
WHERE provider_kind = 'livekit' AND event_id = $1`,
		eventID,
	).Scan(&count, &disposition)
	if err != nil {
		t.Fatalf("inspect P4-02 provider receipt: %v", err)
	}
	if count != wantCount || disposition != wantDisposition {
		t.Fatalf(
			"P4-02 provider receipt = count:%d disposition:%q, want %d/%q",
			count,
			disposition,
			wantCount,
			wantDisposition,
		)
	}
}

func assertP402RoomBinding(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roomInstanceID uuid.UUID,
	wantStatus RoomInstanceStatus,
	wantSID string,
) {
	t.Helper()
	var status RoomInstanceStatus
	var sid string
	if err := pool.QueryRow(
		ctx,
		`SELECT status, provider_room_sid
FROM tutorhub.media_room_instances
WHERE id = $1`,
		roomInstanceID,
	).Scan(&status, &sid); err != nil {
		t.Fatalf("read P4-02 room binding: %v", err)
	}
	if status != wantStatus || sid != wantSID {
		t.Fatalf("P4-02 room binding status = %s, want %s; SID match=%t", status, wantStatus, sid == wantSID)
	}
}

func assertP402FailedRoom(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roomInstanceID uuid.UUID,
	wantFailedAt time.Time,
) {
	t.Helper()
	var status RoomInstanceStatus
	var failureCode string
	var failedAt time.Time
	var providerSID string
	if err := pool.QueryRow(
		ctx,
		`SELECT status, COALESCE(failure_code, ''), failed_at,
       COALESCE(provider_room_sid, '')
FROM tutorhub.media_room_instances
WHERE id = $1`,
		roomInstanceID,
	).Scan(&status, &failureCode, &failedAt, &providerSID); err != nil {
		t.Fatal("read P4-02 failed room intent")
	}
	if status != RoomInstanceFailed || failureCode != "provider_room_finished" ||
		providerSID != "" || !failedAt.UTC().Equal(wantFailedAt.UTC()) {
		t.Fatal("provisioning room failure binding or timestamp is invalid")
	}
}

func assertP402LegacyWriteCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	want int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.livekit_webhook_events WHERE tenant_id = $1`,
		tenantID,
	).Scan(&count); err != nil {
		t.Fatalf("count legacy provider webhook writes: %v", err)
	}
	if count != want {
		t.Fatalf("legacy provider webhook writes = %d, want %d", count, want)
	}
}

func assertP402WebhookPrivacy(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	privateValues ...string,
) {
	t.Helper()
	var receiptText string
	var invalidRetention int
	if err := pool.QueryRow(
		ctx,
		`SELECT
    COALESCE(string_agg(to_jsonb(receipt)::text, ' '), ''),
    count(*) FILTER (
        WHERE retention_until <= received_at
           OR retention_until > received_at + interval '30 days'
    )
FROM tutorhub.media_provider_webhook_receipts AS receipt
WHERE tenant_id = $1`,
		tenantID,
	).Scan(&receiptText, &invalidRetention); err != nil {
		t.Fatalf("inspect P4-02 webhook receipt privacy: %v", err)
	}
	if invalidRetention != 0 {
		t.Fatalf("provider receipts with invalid retention = %d", invalidRetention)
	}
	for _, privateValue := range privateValues {
		if privateValue != "" && strings.Contains(receiptText, privateValue) {
			t.Fatalf("provider receipt leaked private provider value")
		}
	}
	for _, forbiddenField := range []string{
		"room_name",
		"provider_room_name",
		"room_sid",
		"provider_room_sid",
		"participant_identity",
		"provider_participant_identity",
		"participant_sid",
		"raw_payload",
		"access_token",
	} {
		if strings.Contains(receiptText, forbiddenField) {
			t.Fatalf("provider receipt retained forbidden field %q", forbiddenField)
		}
	}
}

func p402EventID(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
}

type p402IntegrationRoomProvider struct {
	mu                  sync.Mutex
	sid                 string
	ensured             []string
	deleted             []string
	ensureBarrierTarget int
	ensureBarrier       chan struct{}
	ensureBarrierOnce   sync.Once
}

func (provider *p402IntegrationRoomProvider) EnsureRoom(
	ctx context.Context,
	roomName string,
) (ProviderRoom, error) {
	provider.mu.Lock()
	provider.ensured = append(provider.ensured, roomName)
	ensureCount := len(provider.ensured)
	barrierTarget := provider.ensureBarrierTarget
	barrier := provider.ensureBarrier
	provider.mu.Unlock()
	if barrier != nil && barrierTarget > 0 {
		if ensureCount >= barrierTarget {
			provider.ensureBarrierOnce.Do(func() { close(barrier) })
		}
		select {
		case <-barrier:
		case <-ctx.Done():
			return ProviderRoom{}, ctx.Err()
		}
	}
	return ProviderRoom{SID: provider.sid}, nil
}

func (provider *p402IntegrationRoomProvider) DeleteRoom(
	_ context.Context,
	roomName string,
) error {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.deleted = append(provider.deleted, roomName)
	return nil
}

func (provider *p402IntegrationRoomProvider) ensureCount() int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	return len(provider.ensured)
}

func (provider *p402IntegrationRoomProvider) deleteCount(roomName string) int {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	count := 0
	for _, deleted := range provider.deleted {
		if deleted == roomName {
			count++
		}
	}
	return count
}

type p402RecordingTokenIssuer struct {
	mu     sync.Mutex
	token  string
	grants []TokenGrant
}

func (issuer *p402RecordingTokenIssuer) Issue(grant TokenGrant) (string, error) {
	issuer.mu.Lock()
	defer issuer.mu.Unlock()
	issuer.grants = append(issuer.grants, grant)
	return issuer.token, nil
}

func (issuer *p402RecordingTokenIssuer) lastGrant(t *testing.T) TokenGrant {
	t.Helper()
	issuer.mu.Lock()
	defer issuer.mu.Unlock()
	if len(issuer.grants) != 1 {
		t.Fatalf("issued token grants = %d, want 1", len(issuer.grants))
	}
	return issuer.grants[0]
}

type p402LegacyService struct {
	mu           sync.Mutex
	webhookCalls int
}

func (*p402LegacyService) IssueJoinCredential(
	context.Context,
	AccessContext,
	uuid.UUID,
) (JoinCredential, error) {
	return JoinCredential{}, ErrUnavailable
}

func (*p402LegacyService) RecordClientEvent(
	context.Context,
	AccessContext,
	uuid.UUID,
	ClientEventInput,
) error {
	return ErrUnavailable
}

func (service *p402LegacyService) RecordWebhook(
	context.Context,
	WebhookEvent,
) (WebhookResult, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	service.webhookCalls++
	return WebhookResult{Recorded: true}, nil
}

func (service *p402LegacyService) webhookCount() int {
	service.mu.Lock()
	defer service.mu.Unlock()
	return service.webhookCalls
}
