//go:build integration

package classroom

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	calendarModule "github.com/tutorhub-v2/core-api/internal/modules/calendar"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/ownertime"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresOwnerTimeConflictConcurrentCreateCommitsOnlyOne(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number < 22 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create owner-time integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin owner-time fixture: %v", err)
	}
	defer func() { _ = setup.Rollback(context.Background()) }()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "owner-time-race")
	classID := uuid.New()
	classCode := "OT" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
	if _, err := setup.Exec(
		ctx,
		`INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
) VALUES ($1, $2, $3, $4, 'Owner-time conflict class', 'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		classCode,
	); err != nil {
		t.Fatalf("insert owner-time class: %v", err)
	}
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit owner-time fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID)

	protector, err := protecteddata.New(protecteddata.Config{
		Key: bytes.Repeat([]byte{0x6a}, 32), KeyVersion: 12,
	})
	if err != nil {
		t.Fatalf("create owner-time protector: %v", err)
	}
	// Deliberately remove feature-control's coarse tenant lock in this test so
	// the barrier proves the owner-time protocol itself, not incidental global
	// serialization performed by another module.
	controls := allowOwnerTimeConflictFeatures{}
	authorizer := policy.NewEngine()
	classroomRepository := NewPostgresRepository(
		pool, 30*time.Second, authorizer, controls,
	).WithCalendarProtectedData(protector)
	clock := time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC)
	availabilityRepository, err := calendarModule.NewPostgresAvailabilityPollRepository(
		pool,
		30*time.Second,
		authorizer,
		controls,
		protector,
		classroomRepository,
		"https://calendar.example.test",
		func() time.Time { return clock },
	)
	if err != nil {
		t.Fatalf("create availability repository: %v", err)
	}
	availabilityService, err := calendarModule.NewAvailabilityPollService(
		availabilityRepository, func() time.Time { return clock },
	)
	if err != nil {
		t.Fatalf("create availability service: %v", err)
	}
	scope := mustTenantContext(t, tenantID, ownerID)
	startsAt := clock.Add(24 * time.Hour)
	endsAt := startsAt.Add(time.Hour)

	blocker, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin owner-time blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	var blockerPID int32
	if err := blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("read blocker backend PID: %v", err)
	}
	if err := ownertime.AcquireLocks(ctx, blocker, tenantID, []uuid.UUID{ownerID}); err != nil {
		t.Fatalf("acquire blocker owner-time lock: %v", err)
	}

	type meetingOutcome struct {
		meeting calendarModule.StudyMeeting
		err     error
	}
	type sessionOutcome struct {
		session ClassSession
		err     error
	}
	meetingResults := make(chan meetingOutcome, 1)
	sessionResults := make(chan sessionOutcome, 1)
	go func() {
		meeting, createErr := availabilityService.CreateStudyMeeting(
			ctx,
			scope,
			calendarModule.CreateStudyMeetingInput{
				ClassID: &classID, Title: "Concurrent study meeting",
				StartsAt: startsAt, EndsAt: endsAt, Timezone: "Asia/Ho_Chi_Minh",
				IdempotencyKey: "owner-time-race-meeting-0001",
			},
		)
		meetingResults <- meetingOutcome{meeting: meeting, err: createErr}
	}()
	go func() {
		session, createErr := classroomRepository.CreateSession(
			ctx,
			scope,
			classID,
			CreateSessionParams{
				Title: "Concurrent class session", StartsAt: startsAt, EndsAt: endsAt,
				Timezone: "Asia/Ho_Chi_Minh", CreatedBy: ownerID,
			},
			clock,
		)
		sessionResults <- sessionOutcome{session: session, err: createErr}
	}()

	waitForOwnerTimeConflictWaiters(t, ctx, pool, blockerPID, 2)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release owner-time blocker: %v", err)
	}
	meetingResult := receiveOwnerTimeResult(t, ctx, meetingResults, "StudyMeeting")
	sessionResult := receiveOwnerTimeResult(t, ctx, sessionResults, "ClassSession")

	meetingSucceeded := meetingResult.err == nil
	sessionSucceeded := sessionResult.err == nil
	if meetingSucceeded == sessionSucceeded {
		t.Fatalf(
			"exactly one writer must commit, meeting=%+v meeting_err=%v session=%+v session_err=%v",
			meetingResult.meeting,
			meetingResult.err,
			sessionResult.session,
			sessionResult.err,
		)
	}
	if meetingSucceeded && !errors.Is(sessionResult.err, ErrSessionScheduleConflict) {
		t.Fatalf("class writer must return schedule conflict, got %v", sessionResult.err)
	}
	if sessionSucceeded && !errors.Is(meetingResult.err, calendarModule.ErrStudyMeetingConflict) {
		t.Fatalf("StudyMeeting writer must return conflict, got %v", meetingResult.err)
	}

	var meetingCount, sessionCount int
	if err := pool.QueryRow(
		ctx,
		`SELECT
    (SELECT count(*)
       FROM tutorhub.study_meetings
      WHERE tenant_id = $1 AND owner_user_id = $2 AND status = 'scheduled'
        AND starts_at < $4 AND ends_at > $3),
    (SELECT count(*)
       FROM tutorhub.class_sessions
      WHERE tenant_id = $1 AND organizer_user_id = $2
        AND status IN ('scheduled', 'live')
        AND starts_at < $4 AND ends_at > $3)`,
		tenantID,
		ownerID,
		startsAt,
		endsAt,
	).Scan(&meetingCount, &sessionCount); err != nil {
		t.Fatalf("inspect committed owner-time sources: %v", err)
	}
	if meetingCount+sessionCount != 1 {
		t.Fatalf(
			"owner-time invariant violated after race: meetings=%d sessions=%d",
			meetingCount,
			sessionCount,
		)
	}
}

func waitForOwnerTimeConflictWaiters(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	blockerPID int32,
	want int,
) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting int
		if err := pool.QueryRow(
			ctx,
			`SELECT count(*)
FROM pg_stat_activity AS activity
WHERE $1::integer = ANY(pg_blocking_pids(activity.pid))`,
			blockerPID,
		).Scan(&waiting); err != nil {
			t.Fatalf("inspect owner-time lock waiters: %v", err)
		}
		if waiting >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for owner-time lock waiters: %v", ctx.Err())
		case <-deadline.C:
			t.Fatalf("owner-time waiters did not reach %d", want)
		case <-ticker.C:
		}
	}
}

func receiveOwnerTimeResult[T any](
	t *testing.T,
	ctx context.Context,
	results <-chan T,
	label string,
) T {
	t.Helper()
	select {
	case result := <-results:
		return result
	case <-ctx.Done():
		var zero T
		t.Fatalf("receive %s result: %v", label, ctx.Err())
		return zero
	}
}

type allowOwnerTimeConflictFeatures struct{}

func (allowOwnerTimeConflictFeatures) RequireFeature(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.FeatureKey,
) error {
	return nil
}

func (allowOwnerTimeConflictFeatures) RequireMemberCapacity(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
) error {
	return nil
}

func (allowOwnerTimeConflictFeatures) RequireActiveClassCapacity(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
) error {
	return nil
}

func (allowOwnerTimeConflictFeatures) RequireQuotaAtMost(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.QuotaKey,
	int64,
) error {
	return nil
}

func (allowOwnerTimeConflictFeatures) ConsumeRateQuota(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.QuotaKey,
	time.Time,
) (featurecontrol.RateLimitResult, error) {
	return featurecontrol.RateLimitResult{}, nil
}

func (allowOwnerTimeConflictFeatures) ConsumeInviteCreation(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	time.Time,
) (featurecontrol.RateLimitResult, error) {
	return featurecontrol.RateLimitResult{}, nil
}

var _ featurecontrol.Enforcer = allowOwnerTimeConflictFeatures{}
