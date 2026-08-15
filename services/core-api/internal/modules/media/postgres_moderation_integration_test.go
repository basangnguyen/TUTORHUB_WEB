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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const p407DisposableConfirmation = "I_UNDERSTAND_P4_07_DISPOSABLE_ONLY"

type p407ModerationFixture struct {
	spaceID       uuid.UUID
	roomID        uuid.UUID
	access        map[string]AccessContext
	keys          map[string]uuid.UUID
	participantID map[string]uuid.UUID
	joinAttemptID map[string]uuid.UUID
}

type p407ModerationProvider struct {
	mu          sync.Mutex
	muteCalls   int
	removeCalls int
	deleteCalls int
}

func (provider *p407ModerationProvider) EnsureRoom(context.Context, string) (ProviderRoom, error) {
	return ProviderRoom{}, ErrInvalidRequest
}

func (provider *p407ModerationProvider) DeleteRoom(_ context.Context, roomName string) error {
	if !opaqueProviderRoomNamePattern.MatchString(roomName) {
		return ErrInvalidRequest
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.deleteCalls++
	return nil
}

func (provider *p407ModerationProvider) MuteParticipantMicrophone(
	_ context.Context,
	roomName string,
	participantIdentity string,
) error {
	if !opaqueProviderRoomNamePattern.MatchString(roomName) || participantIdentity == "" {
		return ErrInvalidRequest
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.muteCalls++
	return nil
}

func (provider *p407ModerationProvider) RemoveParticipant(
	_ context.Context,
	roomName string,
	participantIdentity string,
) error {
	if !opaqueProviderRoomNamePattern.MatchString(roomName) || participantIdentity == "" {
		return ErrInvalidRequest
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	provider.removeCalls++
	return nil
}

func TestPostgresMediaModerationForwardMigration(t *testing.T) {
	requireP407DisposableConfirmation(t)
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406SignalFixtureDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Dirty ||
		(version.Number != 32 && version.Number != 33 && version.Number != 34 && version.Number != 35) {
		t.Fatal("P4-07 forward migration requires a clean disposable ledger at 32, 33, or 34")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply P4-07 forward migration")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("rerun P4-07 forward migration idempotently")
	}
	version, err = migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Number != 35 || version.Dirty {
		t.Fatal("P4-07 forward migration must finish at ledger 35 false")
	}
}

func TestPostgresMediaModerationAuthorityConcurrencyAndProviderReceipts(t *testing.T) {
	requireP407DisposableConfirmation(t)
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406SignalFixtureDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Number != 35 || version.Dirty {
		t.Fatal("P4-07 moderation integration requires ledger 35 false")
	}
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	assertP402SeparatedDatabaseRoles(t, ctx, migrationPool, runtimePool)

	base := seedMediaIntegrationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupMediaIntegrationFixture(t, migrationPool, base) })
	setMediaFeatureOverrides(t, ctx, migrationPool, base.tenantID, base.adminID,
		map[featurecontrol.FeatureKey]bool{featurecontrol.FeatureClassroomMediaRooms: true})
	setMediaQuotaOverrides(t, ctx, migrationPool, base.tenantID, base.adminID,
		map[featurecontrol.QuotaKey]int64{
			featurecontrol.QuotaMediaParticipantsPerSpace: 50,
			featurecontrol.QuotaActiveMediaParticipants:   100,
		})
	fixture := seedP407OfficialFixture(t, ctx, migrationPool, base)
	service, instances, provider := newP407ModerationServices(t, runtimePool)
	lifecycle, err := NewLifecycleService(instances.lifecycle)
	if err != nil {
		t.Fatalf("create P4-07 lifecycle race service: %v", err)
	}
	lobbyRepository, err := NewPostgresLobbyRepository(instances.lifecycle)
	if err != nil {
		t.Fatalf("create P4-07 lobby race repository: %v", err)
	}
	lobby, err := NewLobbyService(lobbyRepository, LobbyServiceConfig{})
	if err != nil {
		t.Fatalf("create P4-07 lobby race service: %v", err)
	}

	state := readP407State(t, ctx, migrationPool, fixture)
	foreign := p402IntegrationAccess(base.foreignTenantID, base.foreignOwnerID,
		policy.OrganizationRoleTeacher, uuid.New())
	if _, err := service.MuteParticipant(ctx, foreign, fixture.spaceID,
		fixture.keys["student"], p407ParticipantInput(state, "p407-foreign-mute", "")); !errors.Is(err, ErrModerationNotFound) {
		t.Fatalf("foreign moderation error = %v, want concealed not found", err)
	}
	if _, err := service.SetLocked(ctx, fixture.access["student"], fixture.spaceID,
		p407LockInput(state, "p407-attendee-lock", true)); !errors.Is(err, ErrModerationForbidden) {
		t.Fatalf("attendee lock error = %v, want forbidden", err)
	}
	if _, err := service.RemoveParticipant(ctx, fixture.access["ta"], fixture.spaceID,
		fixture.keys["student"], p407ParticipantInput(state, "p407-ta-remove-0001", "")); !errors.Is(err, ErrModerationForbidden) {
		t.Fatalf("TA remove error = %v, want forbidden", err)
	}

	muteRequestID := "p407-ta-mute-student-0001"
	muteInput := p407ParticipantInput(state, muteRequestID, "")
	muted, err := service.MuteParticipant(ctx, fixture.access["ta"], fixture.spaceID,
		fixture.keys["student"], muteInput)
	if err != nil || muted.ProviderEffectStatus != ProviderEffectApplied {
		t.Fatalf("TA mute result=%+v error=%v", muted, err)
	}
	replayed, err := service.MuteParticipant(ctx, fixture.access["ta"], fixture.spaceID,
		fixture.keys["student"], muteInput)
	if err != nil || replayed.ProviderEffectStatus != ProviderEffectApplied {
		t.Fatalf("TA mute replay result=%+v error=%v", replayed, err)
	}
	if _, err := service.MuteParticipant(ctx, fixture.access["ta"], fixture.spaceID,
		fixture.keys["student"], p407ParticipantInput(state, muteRequestID, "changed_reason")); !errors.Is(err, ErrModerationIdempotency) {
		t.Fatalf("conflicting mute replay error = %v, want idempotency conflict", err)
	}
	provider.mu.Lock()
	if provider.muteCalls != 1 {
		t.Fatalf("idempotent mute provider calls = %d, want 1", provider.muteCalls)
	}
	provider.mu.Unlock()
	assertP407ModerationRateLimit(
		t, ctx, migrationPool, service, provider, fixture,
	)

	promoteInput := ChangeParticipantRoleInput{Expected: readP407State(t, ctx, migrationPool, fixture),
		IdempotencyKey: "p407-host-promote-0001", DesiredRole: InstanceRoleCoHost}
	promoted, err := service.ChangeParticipantRole(ctx, fixture.access["owner"], fixture.spaceID,
		fixture.keys["student"], promoteInput)
	if err != nil || promoted.TargetInstanceRole == nil ||
		*promoted.TargetInstanceRole != InstanceRoleCoHost ||
		promoted.ProviderEffectStatus != ProviderEffectApplied {
		t.Fatalf("host promote result=%+v error=%v", promoted, err)
	}
	grant, err := instances.PrepareCredential(ctx, fixture.access["student"], fixture.spaceID,
		fixture.joinAttemptID["student"], time.Now().UTC())
	if err != nil || grant.InstanceRole != InstanceRoleCoHost || !grant.CanShareScreen {
		t.Fatalf("promoted credential grant=%+v error=%v", grant, err)
	}
	assertP407RoleCredentialRace(t, ctx, migrationPool, service, instances, fixture)

	assertP407LockJoinRace(t, ctx, migrationPool, service, instances, fixture)
	state = readP407State(t, ctx, migrationPool, fixture)
	if _, err := service.SetLocked(ctx, fixture.access["owner"], fixture.spaceID,
		p407LockInput(state, "p407-unlock-before-admit-race", false)); err != nil {
		t.Fatalf("unlock before P4-07 lock/admit race: %v", err)
	}
	assertP407LockAdmissionRace(t, ctx, migrationPool, service, instances, lobby, fixture)

	assertP407RemoveRejoinRace(t, ctx, migrationPool, service, instances, fixture)

	state = readP407State(t, ctx, migrationPool, fixture)
	if _, err := service.RemoveParticipant(ctx, fixture.access["admin"], fixture.spaceID,
		fixture.keys["owner"], p407ParticipantInput(state, "p407-admin-remove-host-0001", "safety_policy")); !errors.Is(err, ErrModerationForbidden) {
		t.Fatalf("safety admin host target error = %v, want forbidden", err)
	}
	if _, err := service.RemoveParticipant(ctx, fixture.access["admin"], fixture.spaceID,
		fixture.keys["ta"], p407ParticipantInput(state, "p407-admin-remove-ta-no-reason", "")); !errors.Is(err, ErrModerationForbidden) {
		t.Fatalf("safety admin no-reason error = %v, want forbidden", err)
	}
	safetyKey := "p407-admin-remove-ta-0001"
	if _, err := service.RemoveParticipant(ctx, fixture.access["admin"], fixture.spaceID,
		fixture.keys["ta"], p407ParticipantInput(state, safetyKey, "safety_policy")); err != nil {
		t.Fatalf("official safety-admin remove: %v", err)
	}
	assertP407ReceiptAndAudit(t, ctx, migrationPool, base.tenantID, muteRequestID, safetyKey)
	assertP407StudyMeetingSafetyRemove(t, ctx, migrationPool, base, service)
	assertP407EndCredentialRace(t, ctx, migrationPool, lifecycle, instances, fixture)
	assertP407DurableProviderReconcileAfterActorDeactivation(
		t, ctx, migrationPool, instances, provider, base, safetyKey,
	)
}

func assertP407ModerationRateLimit(
	t *testing.T,
	ctx context.Context,
	ownerPool *pgxpool.Pool,
	service *ModerationService,
	provider *p407ModerationProvider,
	fixture p407ModerationFixture,
) {
	t.Helper()
	policy, ok := moderationRatePolicyForOperation(ModerationMute)
	if !ok {
		t.Fatal("P4-07 mute rate policy is unavailable")
	}
	bucketHash := moderationRateBucketHash(
		policy, fixture.access["ta"].TenantID, fixture.roomID, fixture.access["ta"].ActorID,
	)
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := ownerPool.Exec(cleanupContext, `DELETE FROM tutorhub.rate_limit_windows
WHERE purpose = $1 AND bucket_hash = $2`, policy.purpose, bucketHash[:]); err != nil {
			t.Errorf("delete P4-07 moderation rate evidence: %v", err)
		}
	})

	var used int64
	if err := ownerPool.QueryRow(ctx, `SELECT used_count
FROM tutorhub.rate_limit_windows
WHERE purpose = $1 AND bucket_hash = $2
ORDER BY window_started_at DESC
LIMIT 1`, policy.purpose, bucketHash[:]).Scan(&used); err != nil || used != 1 {
		t.Fatalf("idempotent replay moderation rate count = %d error=%v, want 1", used, err)
	}
	var databaseTime time.Time
	if err := ownerPool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&databaseTime); err != nil {
		t.Fatalf("read P4-07 database time: %v", err)
	}
	windowStart := databaseTime.UTC().Truncate(moderationRateWindow)
	for _, start := range []time.Time{windowStart, windowStart.Add(moderationRateWindow)} {
		if _, err := ownerPool.Exec(ctx, `INSERT INTO tutorhub.rate_limit_windows (
    purpose, bucket_hash, window_started_at, window_ends_at, used_count, updated_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (purpose, bucket_hash, window_started_at)
DO UPDATE SET window_ends_at = EXCLUDED.window_ends_at,
              used_count = EXCLUDED.used_count,
              updated_at = EXCLUDED.updated_at`, policy.purpose, bucketHash[:], start,
			start.Add(moderationRateWindow), policy.limit, start); err != nil {
			t.Fatalf("saturate P4-07 moderation rate window: %v", err)
		}
	}

	state := readP407State(t, ctx, ownerPool, fixture)
	_, err := service.MuteParticipant(
		ctx, fixture.access["ta"], fixture.spaceID, fixture.keys["student"],
		p407ParticipantInput(state, "p407-ta-mute-rate-0002", ""),
	)
	var limited *ModerationRateLimitError
	if !errors.As(err, &limited) || limited.RetryAfter < time.Second ||
		limited.RetryAfter > moderationRateWindow {
		t.Fatalf("moderation rate error = %v, typed=%+v", err, limited)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.muteCalls != 1 {
		t.Fatalf("rate-limited moderation reached provider: calls=%d, want 1", provider.muteCalls)
	}
}

func requireP407DisposableConfirmation(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_07_DISPOSABLE_CONFIRM")) != p407DisposableConfirmation {
		t.Skip("P4_07_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
}

func newP407ModerationServices(
	t *testing.T,
	pool *pgxpool.Pool,
) (*ModerationService, *PostgresInstanceRepository, *p407ModerationProvider) {
	t.Helper()
	base, _ := newMediaIntegrationServices(t, pool)
	lifecycle, ok := base.repository.(*PostgresLifecycleRepository)
	if !ok {
		t.Fatal("P4-07 lifecycle repository is not PostgreSQL")
	}
	lifecycle.queryTimeout = 60 * time.Second
	repository, err := NewPostgresModerationRepository(lifecycle)
	if err != nil {
		t.Fatalf("create P4-07 moderation repository: %v", err)
	}
	provider := &p407ModerationProvider{}
	service, err := NewModerationService(repository, provider, time.Now)
	if err != nil {
		t.Fatalf("create P4-07 moderation service: %v", err)
	}
	instances, err := NewPostgresInstanceRepository(lifecycle, uuid.New)
	if err != nil {
		t.Fatalf("create P4-07 instance repository: %v", err)
	}
	return service, instances, provider
}

func seedP407OfficialFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	base mediaIntegrationFixture,
) p407ModerationFixture {
	t.Helper()
	fixture := p407ModerationFixture{
		spaceID: uuid.New(), roomID: uuid.New(), access: map[string]AccessContext{},
		keys: map[string]uuid.UUID{}, participantID: map[string]uuid.UUID{},
		joinAttemptID: map[string]uuid.UUID{},
	}
	users := map[string]uuid.UUID{"owner": base.ownerID, "student": base.studentID,
		"co_teacher": base.coTeacherID, "ta": base.outsiderID}
	roles := map[string]InstanceRole{"owner": InstanceRoleHost, "student": InstanceRoleAttendee,
		"co_teacher": InstanceRoleCoHost, "ta": InstanceRoleTeachingAssistant}
	fixture.access["owner"] = p402IntegrationAccess(base.tenantID, base.ownerID,
		policy.OrganizationRoleStudent, uuid.New())
	fixture.access["student"] = p402IntegrationAccess(base.tenantID, base.studentID,
		policy.OrganizationRoleStudent, uuid.New())
	fixture.access["co_teacher"] = p402IntegrationAccess(base.tenantID, base.coTeacherID,
		policy.OrganizationRoleStudent, uuid.New())
	fixture.access["ta"] = p402IntegrationAccess(base.tenantID, base.outsiderID,
		policy.OrganizationRoleStudent, uuid.New())
	fixture.access["admin"] = p402IntegrationAccess(base.tenantID, base.adminID,
		policy.OrganizationRoleAdmin, uuid.New())
	fixture.access["teacher"] = p402IntegrationAccess(base.tenantID, base.teacherID,
		policy.OrganizationRoleTeacher, uuid.New())
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
) VALUES ($1,$2,$3,'teaching_assistant','active',$4,now())`, base.tenantID, base.classID,
		base.outsiderID, base.ownerID); err != nil {
		t.Fatalf("insert P4-07 TA enrollment: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tutorhub.class_sessions
SET status='live', version=version+1, updated_by=$3, updated_at=$4
WHERE tenant_id=$1 AND id=$2`, base.tenantID, base.sessionIDs["owner"], base.ownerID, now); err != nil {
		t.Fatalf("activate P4-07 official source: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_spaces (
    id, tenant_id, source_kind, class_id, source_class_session_id, status, version,
    lobby_enabled, locked, create_idempotency_key, create_request_fingerprint,
    created_by, updated_by, opened_at, opened_by, created_at, updated_at
) VALUES ($1,$2,'class_session',$3,$4,'open',2,false,false,$5,$6,$7,$7,$8,$7,$8,$8)`,
		fixture.spaceID, base.tenantID, base.classID, base.sessionIDs["owner"],
		mediaIntegrationKey("p407-space"), make([]byte, 32), base.ownerID, now); err != nil {
		t.Fatalf("insert P4-07 media space: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_room_instances (
    id, tenant_id, space_id, attempt_number, status, version, provider_room_name,
    provider_room_sid, created_by, updated_by, activated_at, created_at, updated_at,
    projection_version, last_signal_sequence, next_roster_sequence
) VALUES ($1,$2,$3,1,'active',2,$4,$5,$6,$6,$7,$7,$7,1,0,5)`, fixture.roomID,
		base.tenantID, fixture.spaceID, opaqueProviderRoomName(fixture.roomID),
		"RM_"+strings.ReplaceAll(uuid.NewString(), "-", ""), base.ownerID, now); err != nil {
		t.Fatalf("insert P4-07 active room: %v", err)
	}
	order := []string{"owner", "student", "co_teacher", "ta"}
	for index, name := range order {
		fixture.keys[name], fixture.participantID[name], fixture.joinAttemptID[name] =
			uuid.New(), uuid.New(), uuid.New()
		participantStatus := "connected"
		var connectedAt any = now
		// These two sessions remain in the credential-issuance state so P4-07 can
		// exercise role/credential and end/credential races without inventing a
		// second participant for the same user.
		if name == "student" || name == "co_teacher" {
			participantStatus = "joining"
			connectedAt = nil
		}
		if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_participant_sessions (
    id, tenant_id, space_id, room_instance_id, user_id, join_attempt_id,
    provider_participant_identity, instance_role, status, capacity_reserved, version,
    admitted_at, joining_at, connected_at, participant_key, roster_sequence,
    created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,true,3,$10,$10,$11,$12,$13,$10,$10)`,
			fixture.participantID[name], base.tenantID, fixture.spaceID, fixture.roomID,
			users[name], fixture.joinAttemptID[name], "p_"+strings.ReplaceAll(uuid.NewString(), "-", ""),
			roles[name], participantStatus, now, connectedAt, fixture.keys[name], index+1); err != nil {
			t.Fatalf("insert P4-07 %s participant: %v", name, err)
		}
	}
	return fixture
}

func readP407State(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture p407ModerationFixture,
) ModerationExpectedVersions {
	t.Helper()
	state := ModerationExpectedVersions{RoomInstanceID: fixture.roomID}
	if err := pool.QueryRow(ctx, `SELECT space.version, room.version, room.projection_version
FROM tutorhub.media_spaces AS space
JOIN tutorhub.media_room_instances AS room
  ON room.tenant_id=space.tenant_id AND room.space_id=space.id
WHERE space.tenant_id=$1 AND space.id=$2 AND room.id=$3`, fixture.access["owner"].TenantID,
		fixture.spaceID, fixture.roomID).Scan(&state.SpaceVersion, &state.RoomVersion,
		&state.ProjectionVersion); err != nil {
		t.Fatalf("read P4-07 expected versions: %v", err)
	}
	return state
}

func p407ParticipantInput(
	expected ModerationExpectedVersions,
	key string,
	reason string,
) ModerateParticipantInput {
	return ModerateParticipantInput{Expected: expected, IdempotencyKey: key, ReasonCode: reason}
}

func p407LockInput(
	expected ModerationExpectedVersions,
	key string,
	locked bool,
) LockMediaSpaceInput {
	return LockMediaSpaceInput{Expected: expected, IdempotencyKey: key, Locked: locked}
}

func assertP407LockJoinRace(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *ModerationService,
	instances *PostgresInstanceRepository,
	fixture p407ModerationFixture,
) {
	t.Helper()
	state := readP407State(t, ctx, pool, fixture)
	var wait sync.WaitGroup
	wait.Add(2)
	var lockErr, joinErr error
	go func() {
		defer wait.Done()
		_, lockErr = service.SetLocked(ctx, fixture.access["owner"], fixture.spaceID,
			p407LockInput(state, "p407-lock-join-race-0001", true))
	}()
	go func() {
		defer wait.Done()
		_, joinErr = instances.CreateOrReuseJoinAttempt(ctx, fixture.access["teacher"], fixture.spaceID,
			CreateJoinAttemptInput{JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: fixture.roomID,
				ExpectedSpaceVersion: state.SpaceVersion}, time.Now().UTC())
	}()
	wait.Wait()
	if lockErr != nil && !errors.Is(lockErr, ErrModerationConflict) {
		t.Fatalf("lock/join race lock error = %v", lockErr)
	}
	if joinErr != nil && !errors.Is(joinErr, ErrRoomLocked) && !errors.Is(joinErr, ErrJoinAttemptStale) {
		t.Fatalf("lock/join race join error = %v", joinErr)
	}
	var locked bool
	if err := pool.QueryRow(ctx, `SELECT locked FROM tutorhub.media_spaces
WHERE tenant_id=$1 AND id=$2`, fixture.access["owner"].TenantID, fixture.spaceID).Scan(&locked); err != nil {
		t.Fatalf("inspect P4-07 lock/join race: %v", err)
	}
	if !locked {
		state = readP407State(t, ctx, pool, fixture)
		if _, err := service.SetLocked(ctx, fixture.access["owner"], fixture.spaceID,
			p407LockInput(state, "p407-lock-after-race-0001", true)); err != nil {
			t.Fatalf("converge room lock after join won race: %v", err)
		}
	}
	state = readP407State(t, ctx, pool, fixture)
	if _, err := instances.CreateOrReuseJoinAttempt(ctx, fixture.access["admin"], fixture.spaceID,
		CreateJoinAttemptInput{JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: fixture.roomID,
			ExpectedSpaceVersion: state.SpaceVersion}, time.Now().UTC()); !errors.Is(err, ErrRoomLocked) {
		t.Fatalf("new join after lock error = %v, want room locked", err)
	}
}

func assertP407LockAdmissionRace(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *ModerationService,
	instances *PostgresInstanceRepository,
	lobby *LobbyService,
	fixture p407ModerationFixture,
) {
	t.Helper()
	if tag, err := pool.Exec(ctx, `UPDATE tutorhub.media_spaces
SET lobby_enabled = true
WHERE tenant_id = $1 AND id = $2 AND status = 'open'`,
		fixture.access["owner"].TenantID, fixture.spaceID); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("enable P4-07 lobby race fixture: rows=%d error=%v", tag.RowsAffected(), err)
	}
	waiting, err := instances.CreateOrReuseJoinAttempt(
		ctx, fixture.access["admin"], fixture.spaceID,
		CreateJoinAttemptInput{JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: fixture.roomID,
			ExpectedSpaceVersion: readP407State(t, ctx, pool, fixture).SpaceVersion},
		time.Now().UTC(),
	)
	if err != nil || waiting.Attempt.Status != JoinAttemptWaiting ||
		waiting.Attempt.AdmissionRequestID == nil || waiting.Attempt.AdmissionVersion == nil {
		t.Fatalf("create P4-07 waiting admission: result=%+v error=%v", waiting, err)
	}
	state := readP407State(t, ctx, pool, fixture)
	barrier := beginP407RowBarrier(t, ctx, pool, `SELECT id FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, fixture.access["owner"].TenantID, fixture.spaceID)

	type lockOutcome struct {
		result ModerationResult
		err    error
	}
	lockDone := make(chan lockOutcome, 1)
	go func() {
		result, lockErr := service.SetLocked(
			ctx, fixture.access["owner"], fixture.spaceID,
			p407LockInput(state, "p407-lock-admit-race-0001", true),
		)
		lockDone <- lockOutcome{result: result, err: lockErr}
	}()
	waitForP407BlockedQueries(t, ctx, pool, "FROM tutorhub.media_spaces", 1, barrier)

	admitDone := make(chan error, 1)
	go func() {
		_, admitErr := lobby.Admit(
			ctx, fixture.access["owner"], fixture.spaceID,
			*waiting.Attempt.AdmissionRequestID,
			AdmissionMutationInput{
				ExpectedSpaceVersion: state.SpaceVersion, ExpectedRoomInstanceID: fixture.roomID,
				ExpectedRoomInstanceVersion: state.RoomVersion,
				ExpectedAdmissionVersion:    *waiting.Attempt.AdmissionVersion,
				IdempotencyKey:              mediaIntegrationKey("p407-lock-admit-loser"),
			},
		)
		admitDone <- admitErr
	}()
	waitForP407BlockedQueries(t, ctx, pool, "pg_advisory_xact_lock", 2, barrier)
	releaseP407RowBarrier(t, ctx, barrier)

	locked := <-lockDone
	admitErr := <-admitDone
	if locked.err != nil || locked.result.Locked == nil || !*locked.result.Locked {
		t.Fatalf("P4-07 lock/admit lock outcome=%+v error=%v", locked.result, locked.err)
	}
	if !errors.Is(admitErr, ErrRoomLocked) && !errors.Is(admitErr, ErrSpaceVersionConflict) {
		t.Fatalf("P4-07 admission after queued lock error=%v, want fail-closed lock or version conflict", admitErr)
	}
	var admissionStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM tutorhub.media_admission_requests
WHERE tenant_id = $1 AND id = $2`, fixture.access["owner"].TenantID,
		*waiting.Attempt.AdmissionRequestID).Scan(&admissionStatus); err != nil {
		t.Fatalf("inspect P4-07 lock/admit loser: %v", err)
	}
	if admissionStatus != string(LobbyAdmissionWaiting) {
		t.Fatalf("P4-07 locked admission status=%q, want waiting", admissionStatus)
	}
	state = readP407State(t, ctx, pool, fixture)
	if _, err := service.SetLocked(ctx, fixture.access["owner"], fixture.spaceID,
		p407LockInput(state, "p407-unlock-after-admit-race", false)); err != nil {
		t.Fatalf("unlock after P4-07 lock/admit race: %v", err)
	}
	if tag, err := pool.Exec(ctx, `UPDATE tutorhub.media_spaces
SET lobby_enabled = false
WHERE tenant_id = $1 AND id = $2 AND status = 'open'`,
		fixture.access["owner"].TenantID, fixture.spaceID); err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("disable P4-07 lobby race fixture: rows=%d error=%v", tag.RowsAffected(), err)
	}
}

func assertP407RoleCredentialRace(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *ModerationService,
	instances *PostgresInstanceRepository,
	fixture p407ModerationFixture,
) {
	t.Helper()
	state := readP407State(t, ctx, pool, fixture)
	barrier := beginP407RowBarrier(t, ctx, pool, `SELECT id
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3 AND id = $4
FOR UPDATE`, fixture.access["owner"].TenantID, fixture.spaceID, fixture.roomID,
		fixture.participantID["student"])

	type roleOutcome struct {
		result ModerationResult
		err    error
	}
	roleDone := make(chan roleOutcome, 1)
	go func() {
		result, roleErr := service.ChangeParticipantRole(
			ctx, fixture.access["owner"], fixture.spaceID, fixture.keys["student"],
			ChangeParticipantRoleInput{Expected: state,
				IdempotencyKey: "p407-role-credential-race", DesiredRole: InstanceRoleAttendee},
		)
		roleDone <- roleOutcome{result: result, err: roleErr}
	}()
	waitForP407BlockedQueries(t, ctx, pool, "participant_key = $4", 1, barrier)

	type credentialOutcome struct {
		grant ParticipantCredentialGrant
		err   error
	}
	credentialDone := make(chan credentialOutcome, 1)
	go func() {
		grant, credentialErr := instances.PrepareCredential(
			ctx, fixture.access["student"], fixture.spaceID,
			fixture.joinAttemptID["student"], time.Now().UTC(),
		)
		credentialDone <- credentialOutcome{grant: grant, err: credentialErr}
	}()
	waitForP407BlockedQueries(t, ctx, pool, "pg_advisory_xact_lock", 2, barrier)
	releaseP407RowBarrier(t, ctx, barrier)

	role := <-roleDone
	credential := <-credentialDone
	if role.err != nil || role.result.TargetInstanceRole == nil ||
		*role.result.TargetInstanceRole != InstanceRoleAttendee ||
		role.result.ProviderEffectStatus != ProviderEffectApplied {
		t.Fatalf("P4-07 role/credential demotion outcome=%+v error=%v", role.result, role.err)
	}
	if credential.err != nil || credential.grant.InstanceRole != InstanceRoleAttendee ||
		credential.grant.CanShareScreen {
		t.Fatalf("P4-07 credential after queued demotion=%+v error=%v", credential.grant, credential.err)
	}
}

func assertP407RemoveRejoinRace(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	service *ModerationService,
	instances *PostgresInstanceRepository,
	fixture p407ModerationFixture,
) {
	t.Helper()
	state := readP407State(t, ctx, pool, fixture)
	barrier := beginP407RowBarrier(t, ctx, pool, `SELECT id
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3 AND id = $4
FOR UPDATE`, fixture.access["owner"].TenantID, fixture.spaceID, fixture.roomID,
		fixture.participantID["student"])

	type removeOutcome struct {
		result ModerationResult
		err    error
	}
	removeDone := make(chan removeOutcome, 1)
	go func() {
		result, removeErr := service.RemoveParticipant(
			ctx, fixture.access["co_teacher"], fixture.spaceID, fixture.keys["student"],
			p407ParticipantInput(state, "p407-remove-rejoin-race", ""),
		)
		removeDone <- removeOutcome{result: result, err: removeErr}
	}()
	waitForP407BlockedQueries(t, ctx, pool, "participant_key = $4", 1, barrier)

	rejoinDone := make(chan error, 1)
	go func() {
		_, rejoinErr := instances.CreateOrReuseJoinAttempt(
			ctx, fixture.access["student"], fixture.spaceID,
			CreateJoinAttemptInput{JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: fixture.roomID,
				ExpectedSpaceVersion: state.SpaceVersion}, time.Now().UTC(),
		)
		rejoinDone <- rejoinErr
	}()
	waitForP407BlockedQueries(t, ctx, pool, "pg_advisory_xact_lock", 2, barrier)
	releaseP407RowBarrier(t, ctx, barrier)

	removed := <-removeDone
	rejoinErr := <-rejoinDone
	if removed.err != nil || removed.result.ProviderEffectStatus != ProviderEffectApplied {
		t.Fatalf("P4-07 remove/rejoin removal outcome=%+v error=%v", removed.result, removed.err)
	}
	if !errors.Is(rejoinErr, ErrParticipantConflict) {
		t.Fatalf("P4-07 rejoin after queued remove error=%v, want restore barrier", rejoinErr)
	}
}

func assertP407EndCredentialRace(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	lifecycle *LifecycleService,
	instances *PostgresInstanceRepository,
	fixture p407ModerationFixture,
) {
	t.Helper()
	state := readP407State(t, ctx, pool, fixture)
	barrier := beginP407RowBarrier(t, ctx, pool, `SELECT id FROM tutorhub.media_spaces
WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, fixture.access["owner"].TenantID, fixture.spaceID)

	type endOutcome struct {
		space MediaSpace
		err   error
	}
	endDone := make(chan endOutcome, 1)
	go func() {
		space, endErr := lifecycle.EndSpace(
			ctx, fixture.access["owner"], fixture.spaceID,
			TransitionInput{ExpectedVersion: state.SpaceVersion,
				IdempotencyKey: mediaIntegrationKey("p407-end-credential-race")},
		)
		endDone <- endOutcome{space: space, err: endErr}
	}()
	waitForP407BlockedQueries(t, ctx, pool, "FROM tutorhub.media_spaces", 1, barrier)

	credentialDone := make(chan error, 1)
	go func() {
		_, credentialErr := instances.PrepareCredential(
			ctx, fixture.access["co_teacher"], fixture.spaceID,
			fixture.joinAttemptID["co_teacher"], time.Now().UTC(),
		)
		credentialDone <- credentialErr
	}()
	waitForP407BlockedQueries(t, ctx, pool, "pg_advisory_xact_lock", 2, barrier)
	releaseP407RowBarrier(t, ctx, barrier)

	ended := <-endDone
	credentialErr := <-credentialDone
	if ended.err != nil || ended.space.Status != SpaceStatusEnded {
		t.Fatalf("P4-07 end/credential end outcome=%+v error=%v", ended.space, ended.err)
	}
	if !errors.Is(credentialErr, ErrRoomNotOpen) {
		t.Fatalf("P4-07 credential after queued end error=%v, want room not open", credentialErr)
	}
}

func beginP407RowBarrier(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	query string,
	arguments ...any,
) pgx.Tx {
	t.Helper()
	transaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin P4-07 row barrier: %v", err)
	}
	if _, err := transaction.Exec(ctx, query, arguments...); err != nil {
		_ = transaction.Rollback(ctx)
		t.Fatalf("acquire P4-07 row barrier: %v", err)
	}
	return transaction
}

func waitForP407BlockedQueries(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	queryFragment string,
	minimum int,
	barrier pgx.Tx,
) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		var matching int
		err := pool.QueryRow(ctx, `SELECT
    count(*) FILTER (WHERE wait_event_type = 'Lock'),
    count(*) FILTER (
        WHERE wait_event_type = 'Lock' AND position($1 in query) > 0
    )
FROM pg_stat_activity
WHERE datname = current_database() AND pid <> pg_backend_pid()`, queryFragment).Scan(&waiting, &matching)
		if err == nil && waiting >= minimum && (matching > 0 || waiting >= minimum) {
			return
		}
		select {
		case <-ctx.Done():
			_ = barrier.Rollback(context.Background())
			t.Fatalf("wait for P4-07 blocked query %q: %v", queryFragment, ctx.Err())
		case <-deadline.C:
			_ = barrier.Rollback(context.Background())
			t.Fatalf("P4-07 query barrier %q did not observe %d lock waiters", queryFragment, minimum)
		case <-ticker.C:
		}
	}
}

func releaseP407RowBarrier(t *testing.T, ctx context.Context, transaction pgx.Tx) {
	t.Helper()
	if err := transaction.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Fatalf("release P4-07 row barrier: %v", err)
	}
}

func assertP407ReceiptAndAudit(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	muteRequestID string,
	safetyKey string,
) {
	t.Helper()
	for _, key := range []string{muteRequestID, safetyKey} {
		var status string
		var attempts int
		if err := pool.QueryRow(ctx, `SELECT provider_effect_status, provider_effect_attempts
FROM tutorhub.media_space_mutation_receipts
WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, key).Scan(&status, &attempts); err != nil {
			t.Fatalf("read P4-07 provider receipt: %v", err)
		}
		if status != "applied" || attempts != 1 {
			t.Fatalf("P4-07 provider receipt %s = %s/%d, want applied/1", key, status, attempts)
		}
	}
	var muteOutbox, muteAudit, safetyOutbox, safetyAudit int
	if err := pool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.outbox_events WHERE tenant_id=$1
      AND event_type='media_participant.muted.v1'),
    (SELECT count(*) FROM tutorhub.audit_events WHERE tenant_id=$1
      AND action='media_participant.mute'),
    (SELECT count(*) FROM tutorhub.outbox_events WHERE tenant_id=$1
      AND event_type='media_participant.removed.v1'
      AND payload->>'safety_action'='true' AND payload->>'reason_code'='safety_policy'),
    (SELECT count(*) FROM tutorhub.audit_events WHERE tenant_id=$1
      AND action='media_participant.remove'
      AND metadata->>'safety_action'='true' AND metadata->>'reason_code'='safety_policy')`, tenantID).
		Scan(&muteOutbox, &muteAudit, &safetyOutbox, &safetyAudit); err != nil {
		t.Fatalf("read P4-07 audit/outbox evidence: %v", err)
	}
	if muteOutbox != 1 || muteAudit != 1 || safetyOutbox < 1 || safetyAudit < 1 {
		t.Fatalf("P4-07 exact-once/audit counts = mute %d/%d safety %d/%d",
			muteOutbox, muteAudit, safetyOutbox, safetyAudit)
	}
}

func assertP407StudyMeetingSafetyRemove(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	base mediaIntegrationFixture,
	service *ModerationService,
) {
	t.Helper()
	spaceID, roomID, participantID, participantKey := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_spaces (
    id, tenant_id, source_kind, source_study_meeting_id, status, version,
    lobby_enabled, locked, create_idempotency_key, create_request_fingerprint,
    created_by, updated_by, opened_at, opened_by, created_at, updated_at
) VALUES ($1,$2,'study_meeting',$3,'open',2,false,false,$4,$5,$6,$6,$7,$6,$7,$7)`,
		spaceID, base.tenantID, base.studyMeetingID, mediaIntegrationKey("p407-meeting-space"),
		make([]byte, 32), base.ownerID, now); err != nil {
		t.Fatalf("insert P4-07 StudyMeeting space: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_room_instances (
    id, tenant_id, space_id, attempt_number, status, version, provider_room_name,
    provider_room_sid, created_by, updated_by, activated_at, created_at, updated_at,
    projection_version, last_signal_sequence, next_roster_sequence
) VALUES ($1,$2,$3,1,'active',2,$4,$5,$6,$6,$7,$7,$7,1,0,2)`, roomID,
		base.tenantID, spaceID, opaqueProviderRoomName(roomID),
		"RM_"+strings.ReplaceAll(uuid.NewString(), "-", ""), base.ownerID, now); err != nil {
		t.Fatalf("insert P4-07 StudyMeeting room: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_space_members (
    tenant_id, space_id, user_id, status, version, invited_by, created_at, updated_at
) VALUES ($1,$2,$3,'active',1,$4,$5,$5)`, base.tenantID, spaceID, base.studentID,
		base.ownerID, now); err != nil {
		t.Fatalf("insert P4-07 StudyMeeting member: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_participant_sessions (
    id, tenant_id, space_id, room_instance_id, user_id, join_attempt_id,
    provider_participant_identity, instance_role, status, capacity_reserved, version,
    admitted_at, joining_at, connected_at, participant_key, roster_sequence,
    created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,'attendee','connected',true,3,$8,$8,$8,$9,1,$8,$8)`,
		participantID, base.tenantID, spaceID, roomID, base.studentID, uuid.New(),
		"p_"+strings.ReplaceAll(uuid.NewString(), "-", ""), now, participantKey); err != nil {
		t.Fatalf("insert P4-07 StudyMeeting target: %v", err)
	}
	admin := p402IntegrationAccess(base.tenantID, base.adminID, policy.OrganizationRoleAdmin, uuid.New())
	requestID := "p407-meeting-safety-remove-0001"
	result, err := service.RemoveParticipant(ctx, admin, spaceID, participantKey,
		ModerateParticipantInput{Expected: ModerationExpectedVersions{RoomInstanceID: roomID,
			SpaceVersion: 2, RoomVersion: 2, ProjectionVersion: 1},
			IdempotencyKey: requestID, ReasonCode: "safety_policy"})
	if err != nil || result.ProviderEffectStatus != ProviderEffectApplied {
		t.Fatalf("StudyMeeting safety remove result=%+v error=%v", result, err)
	}
}

func assertP407DurableProviderReconcileAfterActorDeactivation(
	t *testing.T,
	ctx context.Context,
	ownerPool *pgxpool.Pool,
	instances *PostgresInstanceRepository,
	provider *p407ModerationProvider,
	base mediaIntegrationFixture,
	idempotencyKey string,
) {
	t.Helper()
	if _, err := ownerPool.Exec(ctx, `UPDATE tutorhub.media_space_mutation_receipts
SET provider_effect_status='retryable_failed', provider_effect_error_code='provider_unavailable',
    provider_effect_lease_until=NULL, provider_effect_updated_at=now()
WHERE tenant_id=$1 AND actor_user_id=$2 AND idempotency_key=$3
  AND operation='participant_remove'`, base.tenantID, base.adminID, idempotencyKey); err != nil {
		t.Fatalf("prepare durable provider retry: %v", err)
	}
	if _, err := ownerPool.Exec(ctx, `UPDATE tutorhub.memberships
SET status='suspended', updated_at=now()
WHERE tenant_id=$1 AND user_id=$2`, base.tenantID, base.adminID); err != nil {
		t.Fatalf("deactivate original moderation actor: %v", err)
	}
	repository, err := NewPostgresProviderEffectRepository(instances.lifecycle)
	if err != nil {
		t.Fatalf("create durable provider repository: %v", err)
	}
	reconciler, err := NewDurableProviderEffectReconciler(
		repository, provider, provider, time.Now,
	)
	if err != nil {
		t.Fatalf("create durable provider reconciler: %v", err)
	}
	provider.mu.Lock()
	removeCallsBefore := provider.removeCalls
	provider.mu.Unlock()

	// The reconciler intentionally scans a global cross-tenant work queue. Keep
	// unrelated disposable receipts locked so this concurrency probe can prove a
	// single winner for its own durable effect without consuming other fixtures.
	otherEffects := beginP407RowBarrier(t, ctx, ownerPool, `SELECT tenant_id
FROM tutorhub.media_space_mutation_receipts
WHERE provider_effect_required
  AND operation IN ('end', 'participant_promote', 'participant_demote',
                    'participant_mute', 'participant_remove')
  AND (
      provider_effect_status IN ('pending', 'retryable_failed')
      OR (provider_effect_status = 'applying' AND provider_effect_lease_until <= clock_timestamp())
  )
  AND NOT (
      tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3
      AND operation = 'participant_remove'
  )
FOR UPDATE`, base.tenantID, base.adminID, idempotencyKey)
	t.Cleanup(func() { _ = otherEffects.Rollback(context.Background()) })

	start := make(chan struct{})
	results := make(chan bool, 2)
	errorsSeen := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			<-start
			claimed, reconcileErr := reconciler.ReconcileOnce(ctx)
			results <- claimed
			errorsSeen <- reconcileErr
		}()
	}
	close(start)
	wait.Wait()
	releaseP407RowBarrier(t, ctx, otherEffects)
	close(results)
	close(errorsSeen)
	claimCount := 0
	for claimed := range results {
		if claimed {
			claimCount++
		}
	}
	for reconcileErr := range errorsSeen {
		if reconcileErr != nil {
			t.Fatalf("trusted durable provider reconciliation: %v", reconcileErr)
		}
	}
	if claimCount != 1 {
		t.Fatalf("concurrent durable provider claims = %d, want 1", claimCount)
	}
	provider.mu.Lock()
	removeCallsAfter := provider.removeCalls
	provider.mu.Unlock()
	if removeCallsAfter != removeCallsBefore+1 {
		t.Fatalf("durable provider calls = %d -> %d, want exactly one retry",
			removeCallsBefore, removeCallsAfter)
	}
	var status ProviderEffectStatus
	var attempts int
	var originalActor uuid.UUID
	if err := ownerPool.QueryRow(ctx, `SELECT provider_effect_status,
       provider_effect_attempts, actor_user_id
FROM tutorhub.media_space_mutation_receipts
WHERE tenant_id=$1 AND idempotency_key=$2`, base.tenantID, idempotencyKey).
		Scan(&status, &attempts, &originalActor); err != nil {
		t.Fatalf("read durable provider retry result: %v", err)
	}
	if status != ProviderEffectApplied || attempts != 2 || originalActor != base.adminID {
		t.Fatalf("durable retry status/attempt/actor = %s/%d/%s",
			status, attempts, originalActor)
	}
}
