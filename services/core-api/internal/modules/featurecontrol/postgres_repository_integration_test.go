//go:build integration

package featurecontrol

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresRepositoryCapabilitiesOverridesAndAuthoritativeAuthorization(t *testing.T) {
	migrationURL := requireFeatureControlEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireFeatureControlEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	applyFeatureControlMigrations(t, ctx, migrationURL)
	pool := openFeatureControlPool(t, ctx, poolURL)
	defer pool.Close()

	outer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin feature control integration transaction: %v", err)
	}
	defer func() { _ = outer.Rollback(context.Background()) }()
	fixture := insertFeatureControlFixture(t, ctx, outer, "repository")
	catalog := NewDefaultCatalog()
	repository, err := NewPostgresRepository(outer, 10*time.Second, policy.NewEngine(), catalog)
	if err != nil {
		t.Fatalf("create PostgreSQL feature control repository: %v", err)
	}
	now := time.Date(2026, 7, 20, 10, 15, 0, 0, time.UTC)
	service, err := NewService(repository, catalog, func() time.Time { return now })
	if err != nil {
		t.Fatalf("create feature control service: %v", err)
	}
	adminContext := tenancy.Context{TenantID: fixture.tenantID, ActorID: fixture.adminID}

	defaults, err := service.GetCapabilities(ctx, adminContext)
	if err != nil {
		t.Fatalf("read default capabilities: %v", err)
	}
	if defaults.Version != 0 || !defaults.AllowedAction.ManageControls {
		t.Fatalf("unexpected default control projection: %+v", defaults)
	}
	assertFeatureCapability(
		t,
		defaults,
		FeatureMembershipInvitations,
		true,
		ValueSourceCatalogDefault,
	)
	assertQuotaCapability(t, defaults, QuotaMembers, 100, 1, ValueSourceCatalogDefault)

	requestContext, _ := requestmeta.New(
		ctx,
		"feature-control-update",
		"192.0.2.14:443",
		"TutorHub integration browser",
		now,
	)
	requestmeta.SetPrincipal(requestContext, fixture.adminID, fixture.tenantID)
	updated, err := service.PutOverrides(requestContext, adminContext, PutOverridesInput{
		ExpectedVersion: 0,
		FeatureOverrides: []FeatureOverride{
			{Key: FeatureClassManagement, Enabled: false},
		},
		QuotaOverrides: []QuotaOverride{
			{Key: QuotaMembers, Limit: 2},
			{Key: QuotaActiveClasses, Limit: 1},
			{Key: QuotaInviteCreationsPerHour, Limit: 1},
		},
	})
	if err != nil {
		t.Fatalf("replace feature control aggregate: %v", err)
	}
	if updated.Version != 1 {
		t.Fatalf("updated version = %d, want 1", updated.Version)
	}
	assertFeatureCapability(
		t,
		updated,
		FeatureClassManagement,
		false,
		ValueSourceTenantOverride,
	)
	assertQuotaCapability(t, updated, QuotaMembers, 2, 1, ValueSourceTenantOverride)
	if _, err := service.PutOverrides(ctx, adminContext, PutOverridesInput{
		ExpectedVersion: 0,
	}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale aggregate error = %v, want version conflict", err)
	}

	studentID := insertFeatureControlUser(t, ctx, outer, "student")
	if _, err := outer.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, $3, 'active', $4)`,
		fixture.tenantID,
		studentID,
		policy.OrganizationRoleStudent,
		now,
	); err != nil {
		t.Fatalf("insert feature control student membership: %v", err)
	}
	if _, err := service.PutOverrides(
		ctx,
		tenancy.Context{TenantID: fixture.tenantID, ActorID: studentID},
		PutOverridesInput{ExpectedVersion: 1},
	); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("student override error = %v, want access denied", err)
	}
	other := insertFeatureControlFixture(t, ctx, outer, "other")
	if _, err := service.GetCapabilities(
		ctx,
		tenancy.Context{TenantID: other.tenantID, ActorID: fixture.adminID},
	); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("cross-tenant capability error = %v, want access denied", err)
	}

	var outboxCount, auditCount int
	if err := outer.QueryRow(
		ctx,
		`SELECT
    (SELECT count(*) FROM tutorhub.outbox_events
     WHERE tenant_id = $1 AND event_type = 'tenant.feature_controls.updated'),
    (SELECT count(*) FROM tutorhub.audit_events
     WHERE tenant_id = $1 AND action = 'tenant.feature_control.update'
       AND resource_id = $1 AND outcome = 'succeeded')`,
		fixture.tenantID,
	).Scan(&outboxCount, &auditCount); err != nil {
		t.Fatalf("read feature control facts: %v", err)
	}
	if outboxCount != 1 || auditCount != 1 {
		t.Fatalf("transactional facts = outbox:%d audit:%d, want 1/1", outboxCount, auditCount)
	}

	enforcement, err := outer.Begin(ctx)
	if err != nil {
		t.Fatalf("begin feature enforcement savepoint: %v", err)
	}
	if err := repository.RequireFeature(
		ctx,
		enforcement,
		fixture.tenantID,
		FeatureClassManagement,
	); !errors.Is(err, ErrFeatureDisabled) {
		t.Fatalf("disabled feature enforcement error = %v", err)
	}
	_ = enforcement.Rollback(ctx)
}

func TestPostgresEnforcerSerializesConcurrentCapacityAndRateMutations(t *testing.T) {
	migrationURL := requireFeatureControlEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireFeatureControlEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	applyFeatureControlMigrations(t, ctx, migrationURL)
	pool := openFeatureControlPool(t, ctx, poolURL)
	defer pool.Close()

	fixture := insertFeatureControlFixture(t, ctx, pool, "concurrency")
	defer func() { cleanupCommittedFeatureControlFixture(t, pool, fixture) }()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tutorhub.tenant_feature_control_revisions (
    tenant_id, version, updated_by, updated_at
) VALUES ($1, 1, $2, $3)`,
		fixture.tenantID,
		fixture.adminID,
		fixture.now,
	); err != nil {
		t.Fatalf("seed feature control revision: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tutorhub.tenant_quota_overrides (
    tenant_id, quota_key, limit_value, updated_by, created_at, updated_at
) VALUES
    ($1, $4, 2, $2, $3, $3),
    ($1, $5, 1, $2, $3, $3),
    ($1, $6, 1, $2, $3, $3)`,
		fixture.tenantID,
		fixture.adminID,
		fixture.now,
		QuotaMembers,
		QuotaActiveClasses,
		QuotaInviteCreationsPerHour,
	); err != nil {
		t.Fatalf("seed feature control quota overrides: %v", err)
	}
	repository, err := NewPostgresRepository(
		pool,
		15*time.Second,
		policy.NewEngine(),
		NewDefaultCatalog(),
	)
	if err != nil {
		t.Fatalf("create concurrent feature control repository: %v", err)
	}

	candidateA := insertFeatureControlUser(t, ctx, pool, "candidate-a")
	candidateB := insertFeatureControlUser(t, ctx, pool, "candidate-b")
	fixture.userIDs = append(fixture.userIDs, candidateA, candidateB)
	memberErrors := runConcurrentFeatureControlMutations(
		t,
		ctx,
		func(index int) error {
			transaction, beginErr := pool.Begin(ctx)
			if beginErr != nil {
				return beginErr
			}
			defer func() { _ = transaction.Rollback(context.Background()) }()
			if guardErr := repository.RequireMemberCapacity(
				ctx,
				transaction,
				fixture.tenantID,
			); guardErr != nil {
				return guardErr
			}
			candidate := candidateA
			if index == 1 {
				candidate = candidateB
			}
			if _, execErr := transaction.Exec(
				ctx,
				`INSERT INTO tutorhub.memberships (
    tenant_id, user_id, role, status, joined_at
) VALUES ($1, $2, $3, 'active', $4)`,
				fixture.tenantID,
				candidate,
				policy.OrganizationRoleStudent,
				fixture.now,
			); execErr != nil {
				return execErr
			}
			return transaction.Commit(ctx)
		},
	)
	assertOneSuccessOneQuotaFailure(t, memberErrors, QuotaMembers)
	var activeMembers int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.memberships WHERE tenant_id = $1 AND status = 'active'`,
		fixture.tenantID,
	).Scan(&activeMembers); err != nil {
		t.Fatalf("count active members: %v", err)
	}
	if activeMembers != 2 {
		t.Fatalf("active members = %d, want 2", activeMembers)
	}

	classA := uuid.New()
	classB := uuid.New()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
) VALUES
    ($1, $3, $4, 'FC-A', 'Feature control class A', 'Asia/Ho_Chi_Minh', 'draft'),
    ($2, $3, $4, 'FC-B', 'Feature control class B', 'Asia/Ho_Chi_Minh', 'draft')`,
		classA,
		classB,
		fixture.tenantID,
		fixture.adminID,
	); err != nil {
		t.Fatalf("seed draft classes: %v", err)
	}
	classErrors := runConcurrentFeatureControlMutations(
		t,
		ctx,
		func(index int) error {
			transaction, beginErr := pool.Begin(ctx)
			if beginErr != nil {
				return beginErr
			}
			defer func() { _ = transaction.Rollback(context.Background()) }()
			if guardErr := repository.RequireActiveClassCapacity(
				ctx,
				transaction,
				fixture.tenantID,
			); guardErr != nil {
				return guardErr
			}
			classID := classA
			if index == 1 {
				classID = classB
			}
			if _, execErr := transaction.Exec(
				ctx,
				`UPDATE tutorhub.classes
SET status = 'active', updated_at = $3
WHERE tenant_id = $1 AND id = $2`,
				fixture.tenantID,
				classID,
				fixture.now,
			); execErr != nil {
				return execErr
			}
			return transaction.Commit(ctx)
		},
	)
	assertOneSuccessOneQuotaFailure(t, classErrors, QuotaActiveClasses)

	rolledBack, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rolled-back rate transaction: %v", err)
	}
	if _, err := repository.ConsumeInviteCreation(
		ctx,
		rolledBack,
		fixture.tenantID,
		fixture.now,
	); err != nil {
		t.Fatalf("consume rolled-back invitation quota: %v", err)
	}
	if err := rolledBack.Rollback(ctx); err != nil {
		t.Fatalf("rollback invitation quota: %v", err)
	}
	rateErrors := runConcurrentFeatureControlMutations(
		t,
		ctx,
		func(_ int) error {
			transaction, beginErr := pool.Begin(ctx)
			if beginErr != nil {
				return beginErr
			}
			defer func() { _ = transaction.Rollback(context.Background()) }()
			result, consumeErr := repository.ConsumeInviteCreation(
				ctx,
				transaction,
				fixture.tenantID,
				fixture.now,
			)
			if consumeErr != nil {
				var quotaFailure *QuotaExceededError
				if errors.As(consumeErr, &quotaFailure) &&
					(quotaFailure.ResetAt.IsZero() || quotaFailure.RetryAfter <= 0) {
					return fmt.Errorf("quota failure omitted reset metadata: %w", consumeErr)
				}
				return consumeErr
			}
			if result.Used != 1 || result.Remaining != 0 || result.RetryAfter <= 0 {
				return fmt.Errorf("unexpected invitation quota result: %+v", result)
			}
			return transaction.Commit(ctx)
		},
	)
	assertOneSuccessOneQuotaFailure(t, rateErrors, QuotaInviteCreationsPerHour)
}

type featureControlIntegrationFixture struct {
	tenantID uuid.UUID
	adminID  uuid.UUID
	userIDs  []uuid.UUID
	now      time.Time
}

type featureControlExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func insertFeatureControlFixture(
	t *testing.T,
	ctx context.Context,
	database featureControlExecer,
	suffix string,
) featureControlIntegrationFixture {
	t.Helper()
	unique := strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))[:12]
	adminID := insertFeatureControlUser(t, ctx, database, suffix+"-admin-"+unique)
	tenantID := uuid.New()
	now := time.Date(2026, 7, 20, 10, 15, 0, 0, time.UTC)
	if _, err := database.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name, locale, timezone, status)
VALUES ($1, $2, $3, 'vi', 'Asia/Ho_Chi_Minh', 'active')`,
		tenantID,
		"feature-control-"+unique,
		"Feature Control "+suffix,
	); err != nil {
		t.Fatalf("insert feature control tenant: %v", err)
	}
	if _, err := database.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, $3, 'active', $4)`,
		tenantID,
		adminID,
		policy.OrganizationRoleAdmin,
		now,
	); err != nil {
		t.Fatalf("insert feature control admin membership: %v", err)
	}
	return featureControlIntegrationFixture{
		tenantID: tenantID, adminID: adminID, userIDs: []uuid.UUID{adminID}, now: now,
	}
}

func insertFeatureControlUser(
	t *testing.T,
	ctx context.Context,
	database featureControlExecer,
	suffix string,
) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	unique := strings.ToLower(strings.ReplaceAll(uuid.NewString(), "-", ""))[:12]
	if _, err := database.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name, locale, timezone, status)
VALUES ($1, $2, $3, 'vi', 'Asia/Ho_Chi_Minh', 'active')`,
		userID,
		fmt.Sprintf("feature-control-%s-%s@example.test", suffix, unique),
		"Feature Control "+suffix,
	); err != nil {
		t.Fatalf("insert feature control user: %v", err)
	}
	return userID
}

func runConcurrentFeatureControlMutations(
	t *testing.T,
	ctx context.Context,
	mutation func(int) error,
) []error {
	t.Helper()
	start := make(chan struct{})
	errorsByMutation := make([]error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for index := range 2 {
		go func(index int) {
			defer wait.Done()
			select {
			case <-start:
				errorsByMutation[index] = mutation(index)
			case <-ctx.Done():
				errorsByMutation[index] = ctx.Err()
			}
		}(index)
	}
	close(start)
	wait.Wait()
	return errorsByMutation
}

func assertOneSuccessOneQuotaFailure(t *testing.T, results []error, quota QuotaKey) {
	t.Helper()
	successes := 0
	quotaFailures := 0
	for _, result := range results {
		if result == nil {
			successes++
			continue
		}
		var failure *QuotaExceededError
		if errors.As(result, &failure) && failure.Quota == quota {
			quotaFailures++
			continue
		}
		t.Fatalf("unexpected concurrent mutation error: %v", result)
	}
	if successes != 1 || quotaFailures != 1 {
		t.Fatalf("concurrent results = success:%d quota:%d, want 1/1", successes, quotaFailures)
	}
}

func assertFeatureCapability(
	t *testing.T,
	capabilities Capabilities,
	key FeatureKey,
	enabled bool,
	source ValueSource,
) {
	t.Helper()
	for _, capability := range capabilities.Features {
		if capability.Key == key {
			if capability.Enabled != enabled || capability.Source != source {
				t.Fatalf("feature %q = %+v", key, capability)
			}
			return
		}
	}
	t.Fatalf("feature capability %q is missing", key)
}

func assertQuotaCapability(
	t *testing.T,
	capabilities Capabilities,
	key QuotaKey,
	limit int64,
	used int64,
	source ValueSource,
) {
	t.Helper()
	for _, capability := range capabilities.Quotas {
		if capability.Key == key {
			if capability.Limit != limit || capability.Used != used || capability.Source != source {
				t.Fatalf("quota %q = %+v", key, capability)
			}
			return
		}
	}
	t.Fatalf("quota capability %q is missing", key)
}

func cleanupCommittedFeatureControlFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture featureControlIntegrationFixture,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.tenants WHERE id = $1`, fixture.tenantID); err != nil {
		t.Errorf("delete feature control integration tenant: %v", err)
	}
	if len(fixture.userIDs) > 0 {
		if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.users WHERE id = ANY($1)`, fixture.userIDs); err != nil {
			t.Errorf("delete feature control integration users: %v", err)
		}
	}
}

func applyFeatureControlMigrations(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()
	if err := migrationrunner.Up(ctx, databaseURL); err != nil {
		t.Fatalf("apply feature control migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, databaseURL)
	if err != nil {
		t.Fatalf("read feature control migration version: %v", err)
	}
	if version.Number < 12 || version.Dirty {
		t.Fatalf("unexpected feature control migration version: %+v", version)
	}
}

func openFeatureControlPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create feature control integration pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping feature control integration database: %v", err)
	}
	return pool
}

func requireFeatureControlEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for integration tests", key)
	}
	return value
}
