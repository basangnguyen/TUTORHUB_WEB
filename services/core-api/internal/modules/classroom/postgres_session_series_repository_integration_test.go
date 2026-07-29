//go:build integration

package classroom

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresClassSessionSeriesLifecycleConflictAndTenantScope(t *testing.T) {
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
	if version.Number < 21 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create recurring session integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin recurring session fixture: %v", err)
	}
	defer func() { _ = setup.Rollback(context.Background()) }()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "session-series")
	otherTenantID, otherOwnerID := seedTenantOwner(t, ctx, setup, "session-series-other")
	studentID := seedTenantMember(t, ctx, setup, tenantID, "session-series-student", "student")
	adminID := seedTenantMember(t, ctx, setup, tenantID, "session-series-admin", "org_admin")
	classID := uuid.New()
	if _, err := setup.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
) VALUES ($1, $2, $3, $4, 'Recurring integration class', 'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		"SR"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert recurring session class: %v", err)
	}
	if _, err := setup.Exec(ctx, `
INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
)
VALUES ($1, $2, $3, 'student', 'active', $4, now())`,
		tenantID,
		classID,
		studentID,
		ownerID,
	); err != nil {
		t.Fatalf("insert recurring session student enrollment: %v", err)
	}
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit recurring session fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID, studentID, adminID)
	defer cleanupClassIntegrationFixture(t, pool, otherTenantID, otherOwnerID)

	calendarProtector := newCancellationLifecycleProtector(t)
	repository := NewPostgresRepository(
		pool,
		30*time.Second,
		policy.NewEngine(),
	).WithCalendarProtectedData(calendarProtector)
	classService, err := NewService(repository, policy.NewEngine())
	if err != nil {
		t.Fatalf("create recurring session class service: %v", err)
	}
	seriesService, err := NewSessionSeriesService(
		repository,
		classService,
		func() time.Time { return time.Date(2026, 7, 27, 11, 30, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatalf("create recurring session service: %v", err)
	}
	tenantContext := mustTenantContext(t, tenantID, ownerID)
	otherContext := mustTenantContext(t, otherTenantID, otherOwnerID)
	studentAccess := accessForOrganizationRole(
		tenantID, studentID, policy.OrganizationRoleStudent,
	)
	adminAccess := accessForOrganizationRole(
		tenantID, adminID, policy.OrganizationRoleAdmin,
	)
	otherAccess := accessForOrganizationRole(
		otherTenantID, otherOwnerID, policy.OrganizationRoleTeacher,
	)
	location := mustLocation("Asia/Ho_Chi_Minh")
	startsAt := time.Date(2026, 7, 27, 5, 30, 0, 0, time.UTC)
	createParams, _, err := normalizeCreateSeriesInput(
		ctx,
		CreateSeriesInput{
			Title: "Algebra weekly", Description: "Bounded recurrence",
			StartsAt: startsAt.In(location).Format(time.RFC3339),
			EndsAt:   startsAt.Add(time.Hour).In(location).Format(time.RFC3339),
			Timezone: "Asia/Ho_Chi_Minh",
			Rule: recurrence.Rule{
				Frequency: recurrence.FrequencyWeekly, Interval: 1,
				Weekdays: []recurrence.Weekday{recurrence.Monday},
				End:      recurrence.End{Type: recurrence.EndAfterCount, Count: 4},
			},
			OverlapPolicy: recurrence.OverlapReject,
		},
		ownerID,
	)
	if err != nil {
		t.Fatalf("normalize recurring series: %v", err)
	}
	created, err := repository.CreateSeries(
		ctx, tenantContext, classID, createParams, startsAt.Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("create recurring series: %v", err)
	}
	if created.Version != 1 || created.Sequence != 0 || created.ICalUID == "" {
		t.Fatalf("unexpected recurring series identity: %+v", created)
	}
	studentView, err := seriesService.GetSeries(ctx, studentAccess, classID, created.ID)
	if err != nil {
		t.Fatalf("active student must read recurring series: %v", err)
	}
	if studentView.ViewerAccess.CanUpdate || studentView.ViewerAccess.CanCancel {
		t.Fatalf("student recurring viewer capabilities must be read-only: %+v", studentView.ViewerAccess)
	}
	if _, err := seriesService.CreateSeries(
		ctx,
		studentAccess,
		classID,
		CreateSeriesInput{},
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("student recurring create must be denied before input handling, got %v", err)
	}
	if _, err := repository.GetSeries(
		ctx, otherContext, classID, created.ID,
	); !errors.Is(err, ErrSeriesNotFound) {
		t.Fatalf("cross-tenant recurring series must be concealed, got %v", err)
	}
	if _, err := seriesService.GetSeries(
		ctx, otherAccess, classID, created.ID,
	); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("cross-tenant recurring service read must conceal class, got %v", err)
	}
	if _, err := repository.CreateSession(
		ctx,
		tenantContext,
		classID,
		CreateSessionParams{
			Title:    "Overlapping one-time",
			StartsAt: startsAt.Add(15 * time.Minute),
			EndsAt:   startsAt.Add(45 * time.Minute),
			Timezone: "Asia/Ho_Chi_Minh", CreatedBy: ownerID,
		},
		startsAt,
	); !errors.Is(err, ErrSessionScheduleConflict) {
		t.Fatalf("one-time session must conflict with recurring projection, got %v", err)
	}

	occurrences, err := expandCompleteSeries(ctx, definitionFromSeries(created))
	if err != nil || len(occurrences) != 4 {
		t.Fatalf("expand created series: count=%d err=%v", len(occurrences), err)
	}
	updatedTitle := "Algebra weekly - first occurrence"
	updateParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope:           recurrence.ScopeThisOccurrence,
			OccurrenceKey:   occurrences[0].Key,
			ExpectedVersion: 1,
			IdempotencyKey:  "series-update-0001",
			Title:           &updatedTitle,
		},
		ownerID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize occurrence update: %v", err)
	}
	updated, err := repository.UpdateSeriesOccurrence(
		ctx, tenantContext, classID, created.ID, updateParams, startsAt,
	)
	if err != nil || updated.Series.Version != 2 || updated.Replay {
		t.Fatalf("update recurring occurrence: result=%+v err=%v", updated, err)
	}
	replayed, err := repository.UpdateSeriesOccurrence(
		ctx, tenantContext, classID, created.ID, updateParams, startsAt.Add(time.Minute),
	)
	if err != nil || !replayed.Replay || replayed.Series.Version != 2 {
		t.Fatalf("idempotent recurring replay: result=%+v err=%v", replayed, err)
	}
	studentMutation := OccurrenceMutationInput{
		Scope: recurrence.ScopeThisOccurrence, OccurrenceKey: occurrences[0].Key,
		ExpectedVersion: 2, IdempotencyKey: "student-series-denied-0001",
		Title: &updatedTitle,
	}
	if _, err := seriesService.PreviewSeriesMutation(
		ctx, studentAccess, classID, created.ID, studentMutation,
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("student recurring preview must be denied, got %v", err)
	}
	if _, err := seriesService.UpdateSeriesOccurrence(
		ctx, studentAccess, classID, created.ID, studentMutation,
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("student recurring update must be denied, got %v", err)
	}
	if _, err := seriesService.CancelSeriesOccurrence(
		ctx, studentAccess, classID, created.ID, studentMutation,
	); !errors.Is(err, ErrClassAccessDenied) {
		t.Fatalf("student recurring cancel must be denied, got %v", err)
	}
	if _, err := seriesService.UpdateSeriesOccurrence(
		ctx, otherAccess, classID, created.ID, studentMutation,
	); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("cross-tenant recurring mutation must conceal class, got %v", err)
	}

	concurrentTitle := "Algebra concurrent replay"
	concurrentParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope: recurrence.ScopeThisOccurrence, OccurrenceKey: occurrences[1].Key,
			ExpectedVersion: 2, IdempotencyKey: "series-concurrent-replay-0001",
			Title: &concurrentTitle,
		},
		ownerID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize concurrent replay mutation: %v", err)
	}
	replayOutcomes := runConcurrentSeriesUpdates(
		ctx, repository, tenantContext, classID, created.ID, concurrentParams, startsAt,
	)
	assertConcurrentReplay(t, replayOutcomes, 3)
	conflictingTitle := "Different payload"
	conflictingParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope:           recurrence.ScopeThisOccurrence,
			OccurrenceKey:   occurrences[1].Key,
			ExpectedVersion: 3,
			IdempotencyKey:  "series-concurrent-replay-0001",
			Title:           &conflictingTitle,
		},
		ownerID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize conflicting idempotency request: %v", err)
	}
	if _, err := repository.UpdateSeriesOccurrence(
		ctx, tenantContext, classID, created.ID, conflictingParams, startsAt.Add(2*time.Minute),
	); !errors.Is(err, ErrSeriesIdempotencyConflict) {
		t.Fatalf("changed idempotency payload must conflict, got %v", err)
	}

	futureTitle := "Algebra future exception"
	futureParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope: recurrence.ScopeThisOccurrence, OccurrenceKey: occurrences[2].Key,
			ExpectedVersion: 3, IdempotencyKey: "series-future-exception-0001",
			Title: &futureTitle,
		},
		ownerID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize future exception mutation: %v", err)
	}
	futureUpdated, err := repository.UpdateSeriesOccurrence(
		ctx, tenantContext, classID, created.ID, futureParams, startsAt.Add(3*time.Minute),
	)
	if err != nil || futureUpdated.Series.Version != 4 {
		t.Fatalf("create future recurring exception: result=%+v err=%v", futureUpdated, err)
	}

	firstCompetingTitle := "Algebra competing edit A"
	secondCompetingTitle := "Algebra competing edit B"
	firstCompetingParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope: recurrence.ScopeThisOccurrence, OccurrenceKey: occurrences[3].Key,
			ExpectedVersion: 4, IdempotencyKey: "series-competing-edit-0001",
			Title: &firstCompetingTitle,
		},
		ownerID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize first competing mutation: %v", err)
	}
	secondCompetingParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope: recurrence.ScopeThisOccurrence, OccurrenceKey: occurrences[3].Key,
			ExpectedVersion: 4, IdempotencyKey: "series-competing-edit-0002",
			Title: &secondCompetingTitle,
		},
		ownerID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize second competing mutation: %v", err)
	}
	competingOutcomes := runCompetingSeriesUpdates(
		ctx,
		repository,
		tenantContext,
		classID,
		created.ID,
		firstCompetingParams,
		secondCompetingParams,
		startsAt,
	)
	assertConcurrentVersionConflict(t, competingOutcomes, 5)

	followingTitle := "Algebra advanced"
	splitParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope:                    recurrence.ScopeFollowing,
			OccurrenceKey:            occurrences[1].Key,
			ExpectedVersion:          5,
			IdempotencyKey:           "series-split-00001",
			FollowingExceptionPolicy: recurrence.ExceptionCarry,
			Title:                    &followingTitle,
		},
		ownerID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize following split: %v", err)
	}
	split, err := repository.UpdateSeriesOccurrence(
		ctx, tenantContext, classID, created.ID, splitParams, startsAt.Add(4*time.Minute),
	)
	if err != nil || split.Series.SplitFrom == nil || *split.Series.SplitFrom != created.ID {
		t.Fatalf("split following series: result=%+v err=%v", split, err)
	}
	childOccurrences, err := expandCompleteSeries(ctx, definitionFromSeries(split.Series))
	if err != nil || len(childOccurrences) != 3 {
		t.Fatalf("expand split series: count=%d err=%v", len(childOccurrences), err)
	}
	assertSplitExceptionRetention(
		t, ctx, pool, tenantID, classID, created.ID, split.Series.ID,
		occurrences[1].OriginalLocal, 4, 3,
	)
	cancelParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope:           recurrence.ScopeEntireSeries,
			OccurrenceKey:   childOccurrences[0].Key,
			ExpectedVersion: split.Series.Version,
			IdempotencyKey:  "series-cancel-0001",
		},
		ownerID,
		"cancel",
	)
	if err != nil {
		t.Fatalf("normalize series cancel: %v", err)
	}
	seriesCancellationLifecycle := seedCancellationLifecycleFixture(
		t,
		ctx,
		pool,
		calendarProtector,
		tenantID,
		classID,
		ownerID,
		SeriesParticipationSource(split.Series.ID),
		split.Series.Version,
		split.Series.Sequence,
		startsAt.Add(4*time.Minute+30*time.Second),
	)
	occurrenceCancellationLifecycle := seedCancellationLifecycleFixture(
		t,
		ctx,
		pool,
		calendarProtector,
		tenantID,
		classID,
		ownerID,
		OccurrenceParticipationSource(split.Series.ID, childOccurrences[1].Key),
		split.Series.Version,
		split.Series.Sequence,
		startsAt.Add(4*time.Minute+30*time.Second),
	)
	cancelled, err := repository.CancelSeriesOccurrence(
		ctx, tenantContext, classID, split.Series.ID, cancelParams, startsAt.Add(5*time.Minute),
	)
	if err != nil || cancelled.Series.Status != SeriesStatusCancelled {
		t.Fatalf("cancel split series: result=%+v err=%v", cancelled, err)
	}
	assertCancellationLifecycle(
		t,
		ctx,
		pool,
		seriesCancellationLifecycle,
		"series_cancelled",
		2,
		cancelled.Series.Sequence,
	)
	assertCancellationLifecycle(
		t,
		ctx,
		pool,
		occurrenceCancellationLifecycle,
		"series_cancelled",
		2,
		cancelled.Series.Sequence,
	)

	assertAdminConflictOverride(
		t, ctx, pool, repository, tenantContext, adminAccess, classID, ownerID, startsAt,
	)
	assertRecurringClassQueryPlan(t, ctx, pool, tenantID, classID)
}

type concurrentSeriesMutationOutcome struct {
	result SeriesMutationResult
	err    error
}

func runConcurrentSeriesUpdates(
	ctx context.Context,
	repository *PostgresRepository,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	seriesID uuid.UUID,
	params SeriesMutationParams,
	updatedAt time.Time,
) []concurrentSeriesMutationOutcome {
	return runConcurrentSeriesUpdatePair(
		ctx, repository, tenantContext, classID, seriesID, params, params, updatedAt,
	)
}

func runCompetingSeriesUpdates(
	ctx context.Context,
	repository *PostgresRepository,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	seriesID uuid.UUID,
	first SeriesMutationParams,
	second SeriesMutationParams,
	updatedAt time.Time,
) []concurrentSeriesMutationOutcome {
	return runConcurrentSeriesUpdatePair(
		ctx, repository, tenantContext, classID, seriesID, first, second, updatedAt,
	)
}

func runConcurrentSeriesUpdatePair(
	ctx context.Context,
	repository *PostgresRepository,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	seriesID uuid.UUID,
	first SeriesMutationParams,
	second SeriesMutationParams,
	updatedAt time.Time,
) []concurrentSeriesMutationOutcome {
	ready := make(chan struct{})
	outcomes := make(chan concurrentSeriesMutationOutcome, 2)
	for _, params := range []SeriesMutationParams{first, second} {
		params := params
		go func() {
			<-ready
			result, err := repository.UpdateSeriesOccurrence(
				ctx, tenantContext, classID, seriesID, params, updatedAt,
			)
			outcomes <- concurrentSeriesMutationOutcome{result: result, err: err}
		}()
	}
	close(ready)
	return []concurrentSeriesMutationOutcome{<-outcomes, <-outcomes}
}

func assertConcurrentReplay(
	t *testing.T,
	outcomes []concurrentSeriesMutationOutcome,
	wantVersion int64,
) {
	t.Helper()
	replays := 0
	for _, outcome := range outcomes {
		if outcome.err != nil {
			t.Fatalf("concurrent idempotent mutation failed: %v", outcome.err)
		}
		if outcome.result.Series.Version != wantVersion {
			t.Fatalf("concurrent replay version=%d, want %d", outcome.result.Series.Version, wantVersion)
		}
		if outcome.result.Replay {
			replays++
		}
	}
	if replays != 1 {
		t.Fatalf("concurrent identical mutation replay count=%d, want 1", replays)
	}
}

func assertConcurrentVersionConflict(
	t *testing.T,
	outcomes []concurrentSeriesMutationOutcome,
	wantVersion int64,
) {
	t.Helper()
	successes, conflicts := 0, 0
	for _, outcome := range outcomes {
		switch {
		case outcome.err == nil:
			successes++
			if outcome.result.Series.Version != wantVersion || outcome.result.Replay {
				t.Fatalf("unexpected competing mutation success: %+v", outcome.result)
			}
		case errors.Is(outcome.err, ErrSeriesVersionConflict):
			conflicts++
		default:
			t.Fatalf("unexpected competing mutation error: %v", outcome.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("competing mutation successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
}

func assertSplitExceptionRetention(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	classID uuid.UUID,
	parentSeriesID uuid.UUID,
	childSeriesID uuid.UUID,
	boundaryLocal string,
	wantParentCount int,
	wantChildCount int,
) {
	t.Helper()
	boundary, err := time.Parse(civilDateTimeLayout, boundaryLocal)
	if err != nil {
		t.Fatalf("parse split boundary: %v", err)
	}
	var parentCount, retainedBeforeBoundary, childCount int
	if err := pool.QueryRow(ctx, `
SELECT
    count(*)::integer,
    count(*) FILTER (WHERE original_local_start < $4)::integer
FROM tutorhub.class_session_exceptions
WHERE tenant_id = $1 AND class_id = $2 AND series_id = $3`,
		tenantID, classID, parentSeriesID, boundary,
	).Scan(&parentCount, &retainedBeforeBoundary); err != nil {
		t.Fatalf("inspect parent split exceptions: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*)::integer
FROM tutorhub.class_session_exceptions
WHERE tenant_id = $1 AND class_id = $2 AND series_id = $3
  AND original_local_start >= $4`,
		tenantID, classID, childSeriesID, boundary,
	).Scan(&childCount); err != nil {
		t.Fatalf("inspect child split exceptions: %v", err)
	}
	if parentCount != wantParentCount || retainedBeforeBoundary != 1 || childCount != wantChildCount {
		t.Fatalf(
			"split exception retention parent=%d before=%d child=%d, want %d/1/%d",
			parentCount, retainedBeforeBoundary, childCount, wantParentCount, wantChildCount,
		)
	}
}

func assertAdminConflictOverride(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *PostgresRepository,
	teacherContext tenancy.Context,
	adminAccess AccessContext,
	classID uuid.UUID,
	teacherID uuid.UUID,
	baseTime time.Time,
) {
	t.Helper()
	adminContext := mustTenantContext(t, adminAccess.TenantID, adminAccess.ActorID)
	seriesStart := time.Date(2026, 9, 1, 2, 0, 0, 0, time.UTC)
	location := mustLocation("Asia/Ho_Chi_Minh")
	params, _, err := normalizeCreateSeriesInput(
		ctx,
		CreateSeriesInput{
			Title:    "Admin conflict acceptance",
			StartsAt: seriesStart.In(location).Format(time.RFC3339),
			EndsAt:   seriesStart.Add(time.Hour).In(location).Format(time.RFC3339),
			Timezone: "Asia/Ho_Chi_Minh",
			Rule: recurrence.Rule{
				Frequency: recurrence.FrequencyDaily, Interval: 1,
				End: recurrence.End{Type: recurrence.EndAfterCount, Count: 2},
			},
			OverlapPolicy: recurrence.OverlapReject,
		},
		teacherID,
	)
	if err != nil {
		t.Fatalf("normalize admin conflict series: %v", err)
	}
	series, err := repository.CreateSeries(
		ctx, teacherContext, classID, params, baseTime.Add(6*time.Minute),
	)
	if err != nil {
		t.Fatalf("create admin conflict series: %v", err)
	}
	occurrences, err := expandCompleteSeries(ctx, definitionFromSeries(series))
	if err != nil || len(occurrences) != 2 {
		t.Fatalf("expand admin conflict series: count=%d err=%v", len(occurrences), err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO tutorhub.class_sessions (
    id, tenant_id, class_id, title, starts_at, ends_at, timezone,
    status, created_by, updated_by
)
VALUES ($1, $2, $3, 'Admin conflict fixture', $4, $5,
        'Asia/Ho_Chi_Minh', 'scheduled', $6, $6)`,
		uuid.New(), teacherContext.TenantID, classID,
		seriesStart.Add(15*time.Minute), seriesStart.Add(45*time.Minute), teacherID,
	); err != nil {
		t.Fatalf("insert admin conflict fixture: %v", err)
	}
	updatedTitle := "Admin conflict accepted"
	teacherParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope: recurrence.ScopeEntireSeries, OccurrenceKey: occurrences[0].Key,
			ExpectedVersion: 1, IdempotencyKey: "teacher-conflict-override-0001",
			Title: &updatedTitle, OverrideScheduleConflict: true,
			ScheduleConflictReason: "Teacher cannot override",
		},
		teacherID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize teacher conflict override: %v", err)
	}
	if _, err := repository.UpdateSeriesOccurrence(
		ctx, teacherContext, classID, series.ID, teacherParams, baseTime.Add(7*time.Minute),
	); !errors.Is(err, ErrConflictOverrideDenied) {
		t.Fatalf("teacher conflict override error=%v, want denied", err)
	}
	adminParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope: recurrence.ScopeEntireSeries, OccurrenceKey: occurrences[0].Key,
			ExpectedVersion: 1, IdempotencyKey: "admin-conflict-override-00001",
			Title: &updatedTitle, OverrideScheduleConflict: true,
			ScheduleConflictReason: "Organization admin approved overlap",
		},
		adminAccess.ActorID,
		"update",
	)
	if err != nil {
		t.Fatalf("normalize admin conflict override: %v", err)
	}
	overridden, err := repository.UpdateSeriesOccurrence(
		ctx, adminContext, classID, series.ID, adminParams, baseTime.Add(8*time.Minute),
	)
	if err != nil || overridden.Series.Version != 2 || overridden.Replay {
		t.Fatalf("admin conflict override result=%+v err=%v", overridden, err)
	}
}

func assertRecurringClassQueryPlan(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	classID uuid.UUID,
) {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin recurring query-plan transaction: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	if _, err := transaction.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable sequential scan for index viability check: %v", err)
	}
	rows, err := transaction.Query(ctx, `
EXPLAIN (FORMAT TEXT, COSTS OFF)
SELECT id
FROM tutorhub.class_session_series
WHERE tenant_id = $1 AND class_id = $2 AND status = 'scheduled'
ORDER BY local_start, id
LIMIT 129`, tenantID, classID)
	if err != nil {
		t.Fatalf("explain bounded recurring class query: %v", err)
	}
	defer rows.Close()
	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan recurring query plan: %v", err)
		}
		planLines = append(planLines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate recurring query plan: %v", err)
	}
	plan := strings.Join(planLines, "\n")
	if !strings.Contains(plan, "class_session_series_class_start_idx") {
		t.Fatalf("bounded recurring class query must use class/start index; plan:\n%s", plan)
	}
}
