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
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresClassSessionLifecycleAndTenantScope(t *testing.T) {
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
	if version.Number < 14 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create session integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin session fixture: %v", err)
	}
	defer func() { _ = setup.Rollback(context.Background()) }()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "session")
	otherTenantID, otherOwnerID := seedTenantOwner(t, ctx, setup, "session-other")
	classID := uuid.New()
	otherClassID := uuid.New()
	insertClass := func(id uuid.UUID, tenantID, ownerID uuid.UUID, code string) {
		t.Helper()
		if _, insertErr := setup.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
) VALUES ($1, $2, $3, $4, 'Session integration class', 'Asia/Ho_Chi_Minh', 'active')`,
			id, tenantID, ownerID, code,
		); insertErr != nil {
			t.Fatalf("insert session class: %v", insertErr)
		}
	}
	insertClass(classID, tenantID, ownerID, "SS"+strings.ToUpper(uuid.NewString()[:8]))
	insertClass(otherClassID, otherTenantID, otherOwnerID, "SX"+strings.ToUpper(uuid.NewString()[:8]))
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit session fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID)
	defer cleanupClassIntegrationFixture(t, pool, otherTenantID, otherOwnerID)

	repository := NewPostgresRepository(pool, 30*time.Second, policy.NewEngine())
	tenantContext := mustTenantContext(t, tenantID, ownerID)
	otherContext := mustTenantContext(t, otherTenantID, otherOwnerID)
	now := time.Date(2026, 7, 23, 3, 0, 0, 0, time.UTC)
	created, err := repository.CreateSession(
		ctx, tenantContext, classID,
		CreateSessionParams{
			Title: "Algebra", Description: "Review",
			StartsAt: now, EndsAt: now.Add(time.Hour),
			Timezone: "Asia/Ho_Chi_Minh", CreatedBy: ownerID,
		},
		now,
	)
	if err != nil {
		t.Fatalf("create class session: %v", err)
	}
	if created.Status != SessionStatusScheduled || created.Version != 1 {
		t.Fatalf("unexpected created session: %+v", created)
	}
	if _, err := repository.GetSession(ctx, otherContext, classID, created.ID); !errors.Is(
		err, ErrSessionNotFound,
	) {
		t.Fatalf("cross-tenant session must be concealed, got %v", err)
	}
	updatedTitle := "Advanced algebra"
	updated, err := repository.UpdateSession(
		ctx, tenantContext, classID, created.ID,
		UpdateSessionParams{Title: &updatedTitle, ExpectedVersion: 1},
		now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("update class session: %v", err)
	}
	if updated.Title != updatedTitle || updated.Version != 2 {
		t.Fatalf("unexpected updated session: %+v", updated)
	}
	if _, err := repository.UpdateSession(
		ctx, tenantContext, classID, created.ID,
		UpdateSessionParams{Title: &updatedTitle, ExpectedVersion: 1},
		now.Add(2*time.Minute),
	); !errors.Is(err, ErrSessionVersionConflict) {
		t.Fatalf("stale session update must conflict, got %v", err)
	}
	cancelled, err := repository.CancelSession(
		ctx, tenantContext, classID, created.ID,
		CancelSessionParams{ExpectedVersion: 2},
		now.Add(3*time.Minute),
	)
	if err != nil {
		t.Fatalf("cancel class session: %v", err)
	}
	if cancelled.Status != SessionStatusCancelled || cancelled.Version != 3 {
		t.Fatalf("unexpected cancelled session: %+v", cancelled)
	}
	idempotent, err := repository.CancelSession(
		ctx, tenantContext, classID, created.ID,
		CancelSessionParams{ExpectedVersion: 1},
		now.Add(4*time.Minute),
	)
	if err != nil || idempotent.Version != cancelled.Version {
		t.Fatalf("cancel must be idempotent: session=%+v err=%v", idempotent, err)
	}
	var eventCount int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.outbox_events
         WHERE tenant_id = $1 AND aggregate_id = $2`,
		tenantID, created.ID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("count session events: %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("expected three session events, got %d", eventCount)
	}
}
