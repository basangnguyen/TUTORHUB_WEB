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
	if version.Number < 19 || version.Dirty {
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
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit recurring session fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID)
	defer cleanupClassIntegrationFixture(t, pool, otherTenantID, otherOwnerID)

	repository := NewPostgresRepository(pool, 30*time.Second, policy.NewEngine())
	tenantContext := mustTenantContext(t, tenantID, ownerID)
	otherContext := mustTenantContext(t, otherTenantID, otherOwnerID)
	startsAt := time.Date(2026, 7, 27, 12, 30, 0, 0, time.UTC)
	createParams, _, err := normalizeCreateSeriesInput(
		ctx,
		CreateSeriesInput{
			Title: "Algebra weekly", Description: "Bounded recurrence",
			StartsAt: startsAt.Format(time.RFC3339),
			EndsAt:   startsAt.Add(time.Hour).Format(time.RFC3339),
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
	if _, err := repository.GetSeries(
		ctx, otherContext, classID, created.ID,
	); !errors.Is(err, ErrSeriesNotFound) {
		t.Fatalf("cross-tenant recurring series must be concealed, got %v", err)
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
	conflictingTitle := "Different payload"
	conflictingParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope:           recurrence.ScopeThisOccurrence,
			OccurrenceKey:   occurrences[0].Key,
			ExpectedVersion: 2,
			IdempotencyKey:  "series-update-0001",
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

	followingTitle := "Algebra advanced"
	splitParams, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope:                    recurrence.ScopeFollowing,
			OccurrenceKey:            occurrences[1].Key,
			ExpectedVersion:          2,
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
		ctx, tenantContext, classID, created.ID, splitParams, startsAt.Add(3*time.Minute),
	)
	if err != nil || split.Series.SplitFrom == nil || *split.Series.SplitFrom != created.ID {
		t.Fatalf("split following series: result=%+v err=%v", split, err)
	}
	childOccurrences, err := expandCompleteSeries(ctx, definitionFromSeries(split.Series))
	if err != nil || len(childOccurrences) != 3 {
		t.Fatalf("expand split series: count=%d err=%v", len(childOccurrences), err)
	}
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
	cancelled, err := repository.CancelSeriesOccurrence(
		ctx, tenantContext, classID, split.Series.ID, cancelParams, startsAt.Add(4*time.Minute),
	)
	if err != nil || cancelled.Series.Status != SeriesStatusCancelled {
		t.Fatalf("cancel split series: result=%+v err=%v", cancelled, err)
	}
}
