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
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const p409DisposableConfirmation = "I_UNDERSTAND_P4_09_DISPOSABLE_ONLY"

func TestPostgresMediaRecoveryForwardMigration(t *testing.T) {
	requireP409DisposableConfirmation(t)
	runP409ForwardMigration(t)
}

func runP409ForwardMigration(t *testing.T) {
	t.Helper()
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406SignalFixtureDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Dirty || (version.Number != 34 && version.Number != 35) {
		t.Fatal("P4-09 forward migration requires a clean disposable ledger at 34 or 35")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply P4-09 forward migration")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("rerun P4-09 forward migration idempotently")
	}
	version, err = migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Number != 35 || version.Dirty {
		t.Fatal("P4-09 forward migration must finish at ledger 35 false")
	}
	t.Log("P4_09_FORWARD_MIGRATION PASS ledger=35 dirty=false idempotent=true")
}

func TestPostgresMediaRecoveryConcurrencyAuthorityAndLockBarrier(t *testing.T) {
	requireP409DisposableConfirmation(t)
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406SignalFixtureDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil || version.Number != 35 || version.Dirty {
		t.Fatal("P4-09 recovery integration requires ledger 35 false")
	}
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	assertP402SeparatedDatabaseRoles(t, ctx, migrationPool, runtimePool)

	base := seedMediaIntegrationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupMediaIntegrationFixture(t, migrationPool, base) })
	setMediaFeatureOverrides(
		t, ctx, migrationPool, base.tenantID, base.adminID,
		map[featurecontrol.FeatureKey]bool{
			featurecontrol.FeatureClassroomMediaRooms: true,
		},
	)
	fixture := seedP407OfficialFixture(t, ctx, migrationPool, base)
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := migrationPool.Exec(ctx, `UPDATE tutorhub.media_room_instances
SET status='failed', version=version+1, failed_at=$4,
    failure_code='provider_room_finished', updated_at=$4
WHERE tenant_id=$1 AND space_id=$2 AND id=$3 AND status='active'`,
		base.tenantID, fixture.spaceID, fixture.roomID, now); err != nil {
		t.Fatalf("fail P4-09 source room: %v", err)
	}
	if _, err := migrationPool.Exec(ctx, `UPDATE tutorhub.media_participant_sessions
SET status='failed', version=version+1, capacity_reserved=false,
    terminal_at=$4, reconnecting_at=NULL, failure_code='provider_room_finished', updated_at=$4
WHERE tenant_id=$1 AND space_id=$2 AND room_instance_id=$3
  AND status IN ('waiting','admitted','joining','connected','reconnecting')`,
		base.tenantID, fixture.spaceID, fixture.roomID, now); err != nil {
		t.Fatalf("fail P4-09 source participants: %v", err)
	}

	var expectedSpaceVersion, expectedRoomVersion int64
	if err := migrationPool.QueryRow(ctx, `SELECT space.version, room.version
FROM tutorhub.media_spaces AS space
JOIN tutorhub.media_room_instances AS room
  ON room.tenant_id=space.tenant_id AND room.space_id=space.id
WHERE space.tenant_id=$1 AND space.id=$2 AND room.id=$3`,
		base.tenantID, fixture.spaceID, fixture.roomID,
	).Scan(&expectedSpaceVersion, &expectedRoomVersion); err != nil {
		t.Fatalf("read P4-09 expected versions: %v", err)
	}
	service, _ := newMediaIntegrationServices(t, runtimePool)
	inputs := []RecoverSpaceInput{
		{
			ExpectedSpaceVersion:        expectedSpaceVersion,
			ExpectedRoomInstanceID:      fixture.roomID,
			ExpectedRoomInstanceVersion: expectedRoomVersion,
			IdempotencyKey:              "p409-recover-race-a",
		},
		{
			ExpectedSpaceVersion:        expectedSpaceVersion,
			ExpectedRoomInstanceID:      fixture.roomID,
			ExpectedRoomInstanceVersion: expectedRoomVersion,
			IdempotencyKey:              "p409-recover-race-b",
		},
	}
	type recoveryResult struct {
		index int
		space MediaSpace
		err   error
	}
	results := make(chan recoveryResult, len(inputs))
	start := make(chan struct{})
	var wait sync.WaitGroup
	for index, input := range inputs {
		wait.Add(1)
		go func(index int, input RecoverSpaceInput) {
			defer wait.Done()
			<-start
			space, recoverErr := service.RecoverSpace(
				ctx, fixture.access["owner"], fixture.spaceID, input,
			)
			results <- recoveryResult{index: index, space: space, err: recoverErr}
		}(index, input)
	}
	close(start)
	wait.Wait()
	close(results)

	successes, conflicts, winner := 0, 0, -1
	var successorID uuid.UUID
	for result := range results {
		if result.err == nil {
			successes++
			winner = result.index
			if result.space.ActiveRoomInstance == nil ||
				result.space.ActiveRoomInstance.Status != RoomInstanceProvisioning {
				t.Fatalf("recovery winner did not project provisioning successor: %+v", result.space)
			}
			successorID = result.space.ActiveRoomInstance.ID
			continue
		}
		if errors.Is(result.err, ErrSpaceVersionConflict) {
			conflicts++
			continue
		}
		t.Fatalf("unexpected concurrent recovery error: %v", result.err)
	}
	if successes != 1 || conflicts != 1 || winner < 0 || successorID == uuid.Nil {
		t.Fatalf("recovery race = successes:%d conflicts:%d winner:%d", successes, conflicts, winner)
	}

	replayed, err := service.RecoverSpace(
		ctx, fixture.access["owner"], fixture.spaceID, inputs[winner],
	)
	if err != nil || replayed.ActiveRoomInstance == nil ||
		replayed.ActiveRoomInstance.ID != successorID {
		t.Fatalf("recovery replay failed to converge: space=%+v err=%v", replayed, err)
	}
	foreign := mediaIntegrationAccess(
		base.foreignTenantID, base.foreignOwnerID, policy.OrganizationRoleTeacher,
	)
	if _, err := service.RecoverSpace(
		ctx, foreign, fixture.spaceID, inputs[winner],
	); !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("foreign recovery error=%v, want concealed not found", err)
	}

	var roomCount, activeCount, maxAttempt, receiptCount, eventCount int
	if err := migrationPool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_room_instances
     WHERE tenant_id=$1 AND space_id=$2),
    (SELECT count(*) FROM tutorhub.media_room_instances
     WHERE tenant_id=$1 AND space_id=$2 AND status IN ('provisioning','active','closing')),
    (SELECT max(attempt_number) FROM tutorhub.media_room_instances
     WHERE tenant_id=$1 AND space_id=$2),
    (SELECT count(*) FROM tutorhub.media_space_mutation_receipts
     WHERE tenant_id=$1 AND space_id=$2 AND operation='recover'),
    (SELECT count(*) FROM tutorhub.outbox_events
     WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='media_space.recovered.v1')`,
		base.tenantID, fixture.spaceID,
	).Scan(&roomCount, &activeCount, &maxAttempt, &receiptCount, &eventCount); err != nil {
		t.Fatalf("read P4-09 recovery result: %v", err)
	}
	if roomCount != 2 || activeCount != 1 || maxAttempt != 2 ||
		receiptCount != 1 || eventCount != 1 {
		t.Fatalf(
			"recovery durable state rooms=%d active=%d attempt=%d receipts=%d events=%d",
			roomCount, activeCount, maxAttempt, receiptCount, eventCount,
		)
	}

	lockedAt := now.Add(time.Second)
	if _, err := migrationPool.Exec(ctx, `UPDATE tutorhub.media_room_instances
SET status='failed', version=version+1, failed_at=$4,
    failure_code='provider_room_finished', updated_at=$4
WHERE tenant_id=$1 AND space_id=$2 AND id=$3 AND status='provisioning'`,
		base.tenantID, fixture.spaceID, successorID, lockedAt); err != nil {
		t.Fatalf("fail successor for lock-wins gate: %v", err)
	}
	var lockedSpaceVersion, lockedRoomVersion int64
	if err := migrationPool.QueryRow(ctx, `UPDATE tutorhub.media_spaces
SET locked=true, version=version+1, updated_by=$3, updated_at=$4
WHERE tenant_id=$1 AND id=$2 AND status='open'
RETURNING version`, base.tenantID, fixture.spaceID, base.ownerID, lockedAt,
	).Scan(&lockedSpaceVersion); err != nil {
		t.Fatalf("lock recovery space: %v", err)
	}
	if err := migrationPool.QueryRow(ctx, `SELECT version
FROM tutorhub.media_room_instances
WHERE tenant_id=$1 AND space_id=$2 AND id=$3`,
		base.tenantID, fixture.spaceID, successorID,
	).Scan(&lockedRoomVersion); err != nil {
		t.Fatalf("read locked recovery room version: %v", err)
	}
	if _, err := service.RecoverSpace(
		ctx, fixture.access["owner"], fixture.spaceID, RecoverSpaceInput{
			ExpectedSpaceVersion:        lockedSpaceVersion,
			ExpectedRoomInstanceID:      successorID,
			ExpectedRoomInstanceVersion: lockedRoomVersion,
			IdempotencyKey:              "p409-recover-locked",
		},
	); !errors.Is(err, ErrSpaceTransition) {
		t.Fatalf("locked recovery error=%v, want transition conflict", err)
	}

	t.Log("P4_09_RECOVERY PASS exactly_one=true replay=true tenant_conceal=true lock_wins=true")
}

func requireP409DisposableConfirmation(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_09_DISPOSABLE_CONFIRM")) != p409DisposableConfirmation {
		t.Skip("P4_09_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
}
