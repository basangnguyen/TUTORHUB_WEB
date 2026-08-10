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

const p404DisposableConfirmation = "I_UNDERSTAND_P4_04_DISPOSABLE_ONLY"

func TestPostgresMediaLobbyAdmissionInviteRaceAndRestoreBarrier(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_04_DISPOSABLE_CONFIRM")) != p404DisposableConfirmation {
		t.Skip("P4_04_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply P4-04 lobby migrations")
	}
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	assertP402SeparatedDatabaseRoles(t, ctx, migrationPool, runtimePool)
	fixture := seedMediaIntegrationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupMediaIntegrationFixture(t, migrationPool, fixture) })
	setMediaFeatureOverrides(
		t, ctx, migrationPool, fixture.tenantID, fixture.adminID,
		map[featurecontrol.FeatureKey]bool{
			featurecontrol.FeatureClassroomMediaRooms: true,
			featurecontrol.FeatureInstantStudyRooms:   true,
		},
	)
	setMediaQuotaOverrides(
		t, ctx, migrationPool, fixture.tenantID, fixture.adminID,
		map[featurecontrol.QuotaKey]int64{
			featurecontrol.QuotaActiveMediaSpaces:         20,
			featurecontrol.QuotaMediaSpaceStartsPerHour:   40,
			featurecontrol.QuotaMediaParticipantsPerSpace: 50,
			featurecontrol.QuotaActiveMediaParticipants:   100,
		},
	)

	now := time.Now().UTC().Truncate(time.Microsecond)
	clock := func() time.Time { return now }
	lifecycle, instances := newP402IntegrationServices(t, runtimePool, clock)
	postgresLifecycle, ok := lifecycle.repository.(*PostgresLifecycleRepository)
	if !ok {
		t.Fatal("P4-04 lifecycle repository is not PostgreSQL")
	}
	lobbyRepository, err := NewPostgresLobbyRepository(postgresLifecycle)
	if err != nil {
		t.Fatalf("create P4-04 lobby repository: %v", err)
	}
	lobby, err := NewLobbyService(lobbyRepository, LobbyServiceConfig{Clock: clock})
	if err != nil {
		t.Fatalf("create P4-04 lobby service: %v", err)
	}
	joinAttempts, err := NewJoinAttemptService(instances, clock)
	if err != nil {
		t.Fatalf("create P4-04 join-attempt service: %v", err)
	}
	ownerAccess := p402IntegrationAccess(
		fixture.tenantID, fixture.ownerID, policy.OrganizationRoleStudent, uuid.New(),
	)
	created, err := lifecycle.CreateSpace(
		ctx, ownerAccess,
		CreateSpaceInput{
			Source: CreateSourceInput{
				Kind: SourceStudyMeeting, StudyMeetingID: fixture.studyMeetingID,
			},
			IdempotencyKey: mediaIntegrationKey("p404-space"),
		},
	)
	if err != nil {
		t.Fatalf("create P4-04 StudyMeeting space: %v", err)
	}
	now = now.Add(time.Second)
	opened, err := lifecycle.StartSpace(
		ctx, ownerAccess, created.Space.ID,
		TransitionInput{
			ExpectedVersion: created.Space.Version,
			IdempotencyKey:  mediaIntegrationKey("p404-start"),
		},
	)
	if err != nil || opened.ActiveRoomInstance == nil {
		t.Fatalf("start P4-04 StudyMeeting space: space=%+v err=%v", opened, err)
	}
	roomID := opened.ActiveRoomInstance.ID
	now = now.Add(time.Second)
	active, err := instances.ActivateRoomInstance(
		ctx, ownerAccess, opened.ID, roomID,
		"RM_"+strings.ReplaceAll(uuid.NewString(), "-", ""), now,
	)
	if err != nil || active.ActiveRoomInstance == nil {
		t.Fatalf("activate P4-04 room: space=%+v err=%v", active, err)
	}
	roomVersion := active.ActiveRoomInstance.Version
	teacherAccess := p402IntegrationAccess(
		fixture.tenantID, fixture.teacherID, policy.OrganizationRoleTeacher, uuid.New(),
	)
	if _, err := joinAttempts.CreateJoinAttempt(
		ctx, teacherAccess, active.ID,
		CreateJoinAttemptInput{
			JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: roomID,
			ExpectedSpaceVersion: active.Version,
		},
	); !isConcealedMediaError(err) {
		t.Fatalf("uninvited same-tenant join error = %v, want concealed source", err)
	}
	foreignAccess := p402IntegrationAccess(
		fixture.foreignTenantID, fixture.foreignOwnerID, policy.OrganizationRoleTeacher, uuid.New(),
	)
	if _, err := joinAttempts.CreateJoinAttempt(
		ctx, foreignAccess, active.ID,
		CreateJoinAttemptInput{
			JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: roomID,
			ExpectedSpaceVersion: active.Version,
		},
	); !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("foreign-tenant join error = %v, want concealed space-not-found", err)
	}

	studentEmail := fixture.privateEmail
	var outsiderEmail string
	var foreignEmail string
	var inactiveEmail string
	if err := migrationPool.QueryRow(
		ctx, `SELECT email FROM tutorhub.users WHERE id = $1`, fixture.outsiderID,
	).Scan(&outsiderEmail); err != nil {
		t.Fatal("resolve same-tenant P4-04 fixture email")
	}
	if err := migrationPool.QueryRow(
		ctx, `SELECT email FROM tutorhub.users WHERE id = $1`, fixture.foreignOwnerID,
	).Scan(&foreignEmail); err != nil {
		t.Fatal("resolve foreign P4-04 fixture email")
	}
	if err := migrationPool.QueryRow(
		ctx, `SELECT email FROM tutorhub.users WHERE id = $1`, fixture.coTeacherID,
	).Scan(&inactiveEmail); err != nil {
		t.Fatal("resolve inactive P4-04 fixture email")
	}
	invite := func(email, key string) LobbyMember {
		t.Helper()
		member, err := lobby.InviteMember(
			ctx, ownerAccess, active.ID,
			InviteLobbyMemberInput{
				Email: email, ExpectedSpaceVersion: active.Version,
				IdempotencyKey: mediaIntegrationKey(key),
			},
		)
		if err != nil {
			t.Fatalf("invite P4-04 member: %v", err)
		}
		return member
	}
	studentMember := invite(studentEmail, "p404-invite-student")
	outsiderMember := invite(outsiderEmail, "p404-invite-outsider")
	_ = invite(inactiveEmail, "p404-invite-inactive")
	if studentMember.UserID != fixture.studentID || outsiderMember.UserID != fixture.outsiderID {
		t.Fatal("exact same-tenant invitation resolved the wrong user")
	}
	if _, err := lobby.InviteMember(
		ctx, ownerAccess, active.ID,
		InviteLobbyMemberInput{
			Email: foreignEmail, ExpectedSpaceVersion: active.Version,
			IdempotencyKey: mediaIntegrationKey("p404-invite-foreign"),
		},
	); !errors.Is(err, ErrLobbyMemberNotFound) {
		t.Fatalf("foreign email invite error = %v, want concealed member-not-found", err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE tutorhub.memberships SET status = 'suspended', updated_at = now()
WHERE tenant_id = $1 AND user_id = $2`,
		fixture.tenantID, fixture.coTeacherID,
	); err != nil {
		t.Fatal("suspend invited P4-04 fixture member")
	}
	inactiveAccess := p402IntegrationAccess(
		fixture.tenantID, fixture.coTeacherID, policy.OrganizationRoleStudent, uuid.New(),
	)
	if _, err := joinAttempts.CreateJoinAttempt(
		ctx, inactiveAccess, active.ID,
		CreateJoinAttemptInput{
			JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: roomID,
			ExpectedSpaceVersion: active.Version,
		},
	); !errors.Is(err, ErrSpaceAccessDenied) && !isConcealedMediaError(err) {
		t.Fatalf("inactive invited member join error = %v, want denied/concealed", err)
	}

	studentAccess := p402IntegrationAccess(
		fixture.tenantID, fixture.studentID, policy.OrganizationRoleStudent, uuid.New(),
	)
	studentAttempt := createP404WaitingAttempt(
		t, ctx, joinAttempts, studentAccess, active.ID, roomID, active.Version,
	)
	if _, err := instances.PrepareCredential(
		ctx, studentAccess, active.ID, studentAttempt.JoinAttemptID, now,
	); !errors.Is(err, ErrAdmissionRequired) {
		t.Fatalf("waiting participant credential error = %v, want admission-required", err)
	}
	if _, err := lobby.Admit(
		ctx, ownerAccess, active.ID, *studentAttempt.AdmissionRequestID,
		p404AdmissionInput(
			roomID, roomVersion+1, active.Version, *studentAttempt.AdmissionVersion,
			"p404-stale-room", "",
		),
	); !errors.Is(err, ErrAdmissionVersionConflict) {
		t.Fatalf("stale room-version admit error = %v, want version conflict", err)
	}
	var staleStatus string
	var staleVersion int64
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT status, version FROM tutorhub.media_admission_requests
WHERE tenant_id = $1 AND id = $2`,
		fixture.tenantID, *studentAttempt.AdmissionRequestID,
	).Scan(&staleStatus, &staleVersion); err != nil {
		t.Fatal("inspect stale P4-04 admission")
	}
	if staleStatus != "waiting" || staleVersion != *studentAttempt.AdmissionVersion {
		t.Fatalf("stale room mutation changed admission to %s v%d", staleStatus, staleVersion)
	}

	now = now.Add(time.Second)
	raceStatus := runP404AdmissionRace(
		t, ctx, lobby, ownerAccess, active.ID, roomID, roomVersion, active.Version,
		studentAttempt,
	)
	expectedStudentReservation := 0
	if raceStatus == LobbyAdmissionAdmitted {
		expectedStudentReservation = 1
	}
	assertNoP404WaitingCapacity(
		t, ctx, migrationPool, fixture.tenantID, active.ID, roomID, fixture.studentID,
		1, expectedStudentReservation,
	)

	outsiderAccess := p402IntegrationAccess(
		fixture.tenantID, fixture.outsiderID, policy.OrganizationRoleStudent, uuid.New(),
	)
	outsiderAttempt := createP404WaitingAttempt(
		t, ctx, joinAttempts, outsiderAccess, active.ID, roomID, active.Version,
	)
	now = now.Add(time.Second)
	denied, err := lobby.Deny(
		ctx, ownerAccess, active.ID, *outsiderAttempt.AdmissionRequestID,
		p404AdmissionInput(
			roomID, roomVersion, active.Version, *outsiderAttempt.AdmissionVersion,
			"p404-deny-outsider", "not_expected",
		),
	)
	if err != nil || denied.Status != LobbyAdmissionDenied {
		t.Fatalf("deny P4-04 outsider: admission=%+v err=%v", denied, err)
	}
	if _, err := joinAttempts.CreateJoinAttempt(
		ctx, outsiderAccess, active.ID,
		CreateJoinAttemptInput{
			JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: roomID,
			ExpectedSpaceVersion: active.Version,
		},
	); !errors.Is(err, ErrParticipantConflict) {
		t.Fatalf("unrestored removed participant error = %v, want participant conflict", err)
	}
	now = now.Add(time.Second)
	restored, err := lobby.RestoreAdmission(
		ctx, ownerAccess, active.ID, denied.ID,
		p404AdmissionInput(
			roomID, roomVersion, active.Version, denied.Version,
			"p404-restore-outsider", "",
		),
	)
	if err != nil || restored.Status != LobbyAdmissionCancelled {
		t.Fatalf("restore P4-04 outsider: admission=%+v err=%v", restored, err)
	}
	restoredAttempt := createP404WaitingAttempt(
		t, ctx, joinAttempts, outsiderAccess, active.ID, roomID, active.Version,
	)

	now = now.Add(time.Second)
	endErr, admitErr := runP404EndAdmissionRace(
		ctx, lifecycle, lobby, ownerAccess, active, roomID, roomVersion, restoredAttempt,
	)
	if endErr != nil {
		t.Fatalf("P4-04 end race did not end the room: %v", endErr)
	}
	if admitErr != nil && !errors.Is(admitErr, ErrSpaceVersionConflict) &&
		!errors.Is(admitErr, ErrRoomNotOpen) && !errors.Is(admitErr, ErrAdmissionVersionConflict) &&
		!errors.Is(admitErr, ErrAdmissionTransition) {
		t.Fatalf("P4-04 end-race admission returned unexpected error: %v", admitErr)
	}
	assertNoP404WaitingCapacity(
		t, ctx, migrationPool, fixture.tenantID, active.ID, roomID, fixture.outsiderID,
		2, 0,
	)
	assertP404EventPrivacy(
		t, ctx, migrationPool, fixture.tenantID,
		[]string{studentEmail, outsiderEmail, inactiveEmail},
	)
}

func createP404WaitingAttempt(
	t *testing.T,
	ctx context.Context,
	service *JoinAttemptService,
	access AccessContext,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	spaceVersion int64,
) JoinAttempt {
	t.Helper()
	result, err := service.CreateJoinAttempt(
		ctx, access, spaceID,
		CreateJoinAttemptInput{
			JoinAttemptID: uuid.New(), ExpectedRoomInstanceID: roomID,
			ExpectedSpaceVersion: spaceVersion,
		},
	)
	if err != nil || result.Attempt.Status != JoinAttemptWaiting ||
		result.Attempt.AdmissionRequestID == nil || result.Attempt.AdmissionVersion == nil {
		t.Fatalf("create P4-04 waiting attempt: result=%+v err=%v", result, err)
	}
	return result.Attempt
}

func p404AdmissionInput(
	roomID uuid.UUID,
	roomVersion int64,
	spaceVersion int64,
	admissionVersion int64,
	key string,
	reason string,
) AdmissionMutationInput {
	return AdmissionMutationInput{
		ExpectedSpaceVersion: spaceVersion, ExpectedRoomInstanceID: roomID,
		ExpectedRoomInstanceVersion: roomVersion,
		ExpectedAdmissionVersion:    admissionVersion,
		IdempotencyKey:              mediaIntegrationKey(key),
		ReasonCode:                  reason,
	}
}

func runP404AdmissionRace(
	t *testing.T,
	ctx context.Context,
	service *LobbyService,
	access AccessContext,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	roomVersion int64,
	spaceVersion int64,
	attempt JoinAttempt,
) LobbyAdmissionStatus {
	t.Helper()
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	errorsByOperation := make([]error, 2)
	results := make([]LobbyAdmission, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		ready <- struct{}{}
		<-start
		results[0], errorsByOperation[0] = service.Admit(
			ctx, access, spaceID, *attempt.AdmissionRequestID,
			p404AdmissionInput(
				roomID, roomVersion, spaceVersion, *attempt.AdmissionVersion,
				"p404-race-admit", "",
			),
		)
	}()
	go func() {
		defer wait.Done()
		ready <- struct{}{}
		<-start
		results[1], errorsByOperation[1] = service.Deny(
			ctx, access, spaceID, *attempt.AdmissionRequestID,
			p404AdmissionInput(
				roomID, roomVersion, spaceVersion, *attempt.AdmissionVersion,
				"p404-race-deny", "race_denied",
			),
		)
	}()
	<-ready
	<-ready
	close(start)
	wait.Wait()
	successes := 0
	winner := LobbyAdmissionStatus("")
	for index, err := range errorsByOperation {
		if err == nil {
			successes++
			winner = results[index].Status
			continue
		}
		if !errors.Is(err, ErrAdmissionVersionConflict) && !errors.Is(err, ErrAdmissionTransition) {
			t.Fatalf("unexpected admission race error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("admit/deny race successes = %d, want exactly one", successes)
	}
	if winner != LobbyAdmissionAdmitted && winner != LobbyAdmissionDenied {
		t.Fatalf("admit/deny race winner status = %q", winner)
	}
	return winner
}

func runP404EndAdmissionRace(
	ctx context.Context,
	lifecycle *LifecycleService,
	lobby *LobbyService,
	access AccessContext,
	space MediaSpace,
	roomID uuid.UUID,
	roomVersion int64,
	attempt JoinAttempt,
) (error, error) {
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	var endErr error
	var admitErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		ready <- struct{}{}
		<-start
		_, endErr = lifecycle.EndSpace(
			ctx, access, space.ID,
			TransitionInput{
				ExpectedVersion: space.Version,
				IdempotencyKey:  mediaIntegrationKey("p404-end-race"),
			},
		)
	}()
	go func() {
		defer wait.Done()
		ready <- struct{}{}
		<-start
		_, admitErr = lobby.Admit(
			ctx, access, space.ID, *attempt.AdmissionRequestID,
			p404AdmissionInput(
				roomID, roomVersion, space.Version, *attempt.AdmissionVersion,
				"p404-end-race-admit", "",
			),
		)
	}()
	<-ready
	<-ready
	close(start)
	wait.Wait()
	return endErr, admitErr
}

func assertNoP404WaitingCapacity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	userID uuid.UUID,
	expectedAdmissions int,
	expectedReserved int,
) {
	t.Helper()
	var admissions int
	var waiting int
	var reserved int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*),
    count(*) FILTER (WHERE admission.status = 'waiting'),
    count(*) FILTER (WHERE participant.capacity_reserved)
FROM tutorhub.media_admission_requests AS admission
JOIN tutorhub.media_participant_sessions AS participant
  ON participant.tenant_id = admission.tenant_id
 AND participant.admission_request_id = admission.id
WHERE admission.tenant_id = $1 AND admission.space_id = $2
  AND admission.room_instance_id = $3 AND admission.user_id = $4`,
		tenantID, spaceID, roomID, userID,
	).Scan(&admissions, &waiting, &reserved); err != nil {
		t.Fatal("inspect P4-04 terminal capacity")
	}
	if admissions != expectedAdmissions || waiting != 0 || reserved != expectedReserved {
		t.Fatalf(
			"P4-04 rows admissions=%d waiting=%d reserved=%d, want %d/0/%d",
			admissions, waiting, reserved, expectedAdmissions, expectedReserved,
		)
	}
}

func assertP404EventPrivacy(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	privateEmails []string,
) {
	t.Helper()
	assertRows := func(label string, query string) {
		t.Helper()
		rows, err := pool.Query(ctx, query, tenantID)
		if err != nil {
			t.Fatalf("read P4-04 %s privacy rows", label)
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var payload string
			if err := rows.Scan(&payload); err != nil {
				t.Fatalf("scan P4-04 %s privacy row", label)
			}
			count++
			lower := strings.ToLower(payload)
			for _, forbidden := range []string{
				`"email"`, "provider", "join_attempt", "participant_session",
				`"session_id"`, "token",
			} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("P4-04 %s payload contains private field class %q", label, forbidden)
				}
			}
			for _, email := range privateEmails {
				if strings.Contains(lower, strings.ToLower(email)) {
					t.Fatalf("P4-04 %s payload contains a raw invite lookup value", label)
				}
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterate P4-04 %s privacy rows", label)
		}
		if count == 0 {
			t.Fatalf("P4-04 %s privacy assertion found no lifecycle rows", label)
		}
	}
	assertRows(
		"outbox",
		`SELECT payload::text FROM tutorhub.outbox_events
WHERE tenant_id = $1
  AND (event_type LIKE 'media_space_member.%' OR event_type LIKE 'media_admission.%')`,
	)
	assertRows(
		"audit",
		`SELECT metadata::text FROM tutorhub.audit_events
WHERE tenant_id = $1
  AND (action LIKE 'media_space_member.%' OR action LIKE 'media_admission.%')`,
	)
}
