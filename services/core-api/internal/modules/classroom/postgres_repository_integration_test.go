//go:build integration

package classroom

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

func TestPostgresRepositoryTenantIsolation(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number != 3 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}

	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create integration pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping integration database: %v", err)
	}

	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin integration transaction: %v", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	tenantA, ownerA := seedTenantOwner(t, ctx, transaction, "a")
	tenantB, ownerB := seedTenantOwner(t, ctx, transaction, "b")
	contextA := mustTenantContext(t, tenantA, ownerA)
	contextB := mustTenantContext(t, tenantB, ownerB)
	repository := NewPostgresRepository(transaction, 10*time.Second)
	classCode := "SEC" + strings.ToUpper(uuid.NewString()[:8])

	created, err := repository.Create(ctx, contextA, CreateClassParams{
		OwnerUserID: ownerA,
		Code:        classCode,
		Title:       "Kiến trúc an toàn thông tin",
		Description: "Integration fixture",
	})
	if err != nil {
		t.Fatalf("create class: %v", err)
	}
	if created.TenantID != tenantA || created.Code != classCode {
		t.Fatalf("unexpected class: %+v", created)
	}

	var eventCount int
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.outbox_events WHERE aggregate_id = $1 AND event_type = 'class.created'`,
		created.ID,
	).Scan(&eventCount); err != nil {
		t.Fatalf("count class outbox event: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected one class.created event, got %d", eventCount)
	}

	loaded, err := repository.Get(ctx, contextA, created.ID)
	if err != nil || loaded.ID != created.ID {
		t.Fatalf("get class in owner tenant: class=%+v error=%v", loaded, err)
	}
	if _, err := repository.Get(ctx, contextB, created.ID); !errors.Is(err, ErrClassNotFound) {
		t.Fatalf("cross-tenant get must be hidden as not found, got %v", err)
	}

	classesA, err := repository.List(ctx, contextA, 10)
	if err != nil || len(classesA) != 1 {
		t.Fatalf("list tenant A classes: classes=%+v error=%v", classesA, err)
	}
	classesB, err := repository.List(ctx, contextB, 10)
	if err != nil || len(classesB) != 0 {
		t.Fatalf("tenant B must not see tenant A classes: classes=%+v error=%v", classesB, err)
	}

	err = runInSavepoint(t, ctx, transaction, "owner_membership", func() error {
		_, createErr := repository.Create(ctx, contextA, CreateClassParams{
			OwnerUserID: ownerB,
			Code:        "OWN" + strings.ToUpper(uuid.NewString()[:8]),
			Title:       "Cross-tenant owner",
		})
		return createErr
	})
	if !errors.Is(err, ErrOwnerMembershipNeeded) {
		t.Fatalf("expected owner membership error, got %v", err)
	}

	err = runInSavepoint(t, ctx, transaction, "duplicate_code", func() error {
		_, createErr := repository.Create(ctx, contextA, CreateClassParams{
			OwnerUserID: ownerA,
			Code:        classCode,
			Title:       "Duplicate code",
		})
		return createErr
	})
	if !errors.Is(err, ErrDuplicateClassCode) {
		t.Fatalf("expected duplicate class code error, got %v", err)
	}
}

func runInSavepoint(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	name string,
	operation func() error,
) error {
	t.Helper()

	if _, err := transaction.Exec(ctx, "SAVEPOINT "+name); err != nil {
		t.Fatalf("create savepoint %s: %v", name, err)
	}
	operationErr := operation()
	if _, err := transaction.Exec(ctx, "ROLLBACK TO SAVEPOINT "+name); err != nil {
		t.Fatalf("rollback savepoint %s: %v", name, err)
	}
	if _, err := transaction.Exec(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		t.Fatalf("release savepoint %s: %v", name, err)
	}

	return operationErr
}

func seedTenantOwner(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	suffix string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()

	userID := uuid.New()
	tenantID := uuid.New()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	email := fmt.Sprintf("integration-%s-%s@example.test", suffix, unique)
	slug := fmt.Sprintf("integration-%s-%s", suffix, unique)

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name) VALUES ($1, $2, $3)`,
		userID,
		email,
		"Integration Owner "+suffix,
	); err != nil {
		t.Fatalf("insert integration user: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name) VALUES ($1, $2, $3)`,
		tenantID,
		slug,
		"Integration Tenant "+suffix,
	); err != nil {
		t.Fatalf("insert integration tenant: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at) VALUES ($1, $2, 'teacher', 'active', now())`,
		tenantID,
		userID,
	); err != nil {
		t.Fatalf("insert integration membership: %v", err)
	}

	return tenantID, userID
}

func mustTenantContext(
	t *testing.T,
	tenantID uuid.UUID,
	actorID uuid.UUID,
) tenancy.Context {
	t.Helper()

	context, err := tenancy.New(tenantID, actorID)
	if err != nil {
		t.Fatalf("create tenant context: %v", err)
	}
	return context
}

func requireEnvironment(t *testing.T, key string) string {
	t.Helper()

	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for integration tests", key)
	}
	return value
}
