//go:build integration

package audit

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type auditIntegrationDatabase struct {
	transaction pgx.Tx
}

func (database auditIntegrationDatabase) Begin(ctx context.Context) (pgx.Tx, error) {
	return database.transaction.Begin(ctx)
}

func TestPostgresAuditEventsAreTenantScopedAndAppendOnly(t *testing.T) {
	migrationURL := requireAuditEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireAuditEnvironment(t, "DATABASE_POOL_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number < 11 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}

	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create audit integration pool: %v", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping audit integration database: %v", err)
	}

	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin audit integration transaction: %v", err)
	}
	defer func() {
		_ = transaction.Rollback(context.Background())
	}()

	tenantA, actorA := seedAuditTenantAdmin(t, ctx, transaction, "a")
	tenantB, actorB := seedAuditTenantAdmin(t, ctx, transaction, "b")
	tenantContextA := mustAuditTenantContext(t, tenantA, actorA)
	tenantContextB := mustAuditTenantContext(t, tenantB, actorB)

	service, err := NewService(
		auditIntegrationDatabase{transaction: transaction},
		10*time.Second,
		policy.NewEngine(),
		time.Now,
	)
	if err != nil {
		t.Fatalf("create audit service: %v", err)
	}

	requestContextA, _ := requestmeta.New(
		ctx,
		"audit-integration-a",
		"203.0.113.44:443",
		"TutorHub integration browser",
		time.Now(),
	)
	requestmeta.SetPrincipal(requestContextA, actorA, tenantA)
	eventAID := uuid.New()
	if err := service.Record(requestContextA, Draft{
		Action:       ActionTenantUpdate,
		ResourceType: "tenant",
		ResourceID:   eventAID,
		Outcome:      OutcomeSucceeded,
		Metadata:     Metadata{"effect": "updated"},
	}); err != nil {
		t.Fatalf("record tenant A audit event: %v", err)
	}

	requestContextB, _ := requestmeta.New(
		ctx,
		"audit-integration-b",
		"198.51.100.18:443",
		"TutorHub integration browser",
		time.Now(),
	)
	requestmeta.SetPrincipal(requestContextB, actorB, tenantB)
	if err := service.Record(requestContextB, Draft{
		Action:       ActionClassCreate,
		ResourceType: "class",
		ResourceID:   uuid.New(),
		Outcome:      OutcomeSucceeded,
		Metadata:     Metadata{"effect": "created"},
	}); err != nil {
		t.Fatalf("record tenant B audit event: %v", err)
	}

	pageA, err := service.List(requestContextA, tenantContextA, tenantA, Filter{Limit: 10})
	if err != nil {
		t.Fatalf("list tenant A audit events: %v", err)
	}
	if len(pageA.Items) != 1 {
		t.Fatalf("tenant A query must return exactly its own event, got %+v", pageA)
	}
	eventA := pageA.Items[0]
	if eventA.TenantID != tenantA || eventA.Action != ActionTenantUpdate ||
		eventA.Resource.ID == nil || *eventA.Resource.ID != eventAID ||
		eventA.RequestID != "audit-integration-a" {
		t.Fatalf("unexpected tenant A audit projection: %+v", eventA)
	}
	if eventA.Actor.UserID == nil || *eventA.Actor.UserID != actorA ||
		eventA.Actor.DisplayName == nil {
		t.Fatalf("audit actor projection is incomplete: %+v", eventA.Actor)
	}
	if _, err := service.List(
		requestContextA,
		tenantContextA,
		tenantB,
		Filter{Limit: 10},
	); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant audit query must be concealed, got %v", err)
	}
	if _, err := service.List(
		requestContextB,
		tenantContextB,
		tenantB,
		Filter{Limit: 10},
	); err != nil {
		t.Fatalf("tenant B must retain access to its own audit event: %v", err)
	}

	expectAuditStatementRejected(
		t,
		ctx,
		transaction,
		"55000",
		"",
		`UPDATE tutorhub.audit_events SET metadata = '{}'::jsonb WHERE id = $1`,
		eventA.ID,
	)
	expectAuditStatementRejected(
		t,
		ctx,
		transaction,
		"55000",
		"",
		`DELETE FROM tutorhub.audit_events WHERE id = $1`,
		eventA.ID,
	)
	expectAuditStatementRejected(
		t,
		ctx,
		transaction,
		"55000",
		"",
		`TRUNCATE TABLE tutorhub.audit_events`,
	)
	expectAuditStatementRejected(
		t,
		ctx,
		transaction,
		"23514",
		"audit_events_metadata_redacted",
		`INSERT INTO tutorhub.audit_events (
    tenant_id, actor_type, actor_user_id, action, resource_type, resource_id,
    outcome, request_id, request_instance_id, metadata
)
VALUES ($1, 'user', $2, 'tenant.update', 'tenant', $1,
        'succeeded', 'audit-redaction-check', $3, '{"session_id":"forbidden"}'::jsonb)`,
		tenantA,
		actorA,
		uuid.New(),
	)
	// A valid invitation target can be resolved before the authenticated actor
	// has joined that tenant. Keep actor attribution user-owned without requiring
	// a current target-tenant membership at the database boundary.
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.audit_events (
    tenant_id, actor_type, actor_user_id, action, resource_type, resource_id,
    outcome, request_id, request_instance_id, metadata
)
VALUES ($1, 'user', $2, 'membership.invitation.accept', 'membership_invitation', NULL,
        'denied', 'audit-external-invitation-actor', $3, '{}'::jsonb)`,
		tenantA,
		actorB,
		uuid.New(),
	); err != nil {
		t.Fatalf("record externally attributed invitation attempt: %v", err)
	}
	expectAuditStatementRejected(
		t,
		ctx,
		transaction,
		"23503",
		"audit_events_actor_user_id_fkey",
		`INSERT INTO tutorhub.audit_events (
    tenant_id, actor_type, actor_user_id, action, resource_type, resource_id,
    outcome, request_id, request_instance_id, metadata
)
VALUES ($1, 'user', $2, 'membership.invitation.accept', 'membership_invitation', NULL,
        'denied', 'audit-unknown-actor', $3, '{}'::jsonb)`,
		tenantA,
		uuid.New(),
		uuid.New(),
	)
	expectAuditStatementRejected(
		t,
		ctx,
		transaction,
		"23503",
		"audit_events_tenant_id_fkey",
		`DELETE FROM tutorhub.tenants WHERE id = $1`,
		tenantA,
	)

	var persistedEvents int
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.audit_events WHERE tenant_id IN ($1, $2)`,
		tenantA,
		tenantB,
	).Scan(&persistedEvents); err != nil {
		t.Fatalf("count retained audit events: %v", err)
	}
	if persistedEvents != 3 {
		t.Fatalf("append-only checks changed audit history: got %d events", persistedEvents)
	}
}

func seedAuditTenantAdmin(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	suffix string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	userID := uuid.New()
	tenantID := uuid.New()
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name)
VALUES ($1, $2, $3)`,
		userID,
		"audit-"+suffix+"-"+unique+"@example.test",
		"Audit Admin "+strings.ToUpper(suffix),
	); err != nil {
		t.Fatalf("seed audit user %s: %v", suffix, err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, $3)`,
		tenantID,
		"audit-"+suffix+"-"+unique,
		"Audit Tenant "+strings.ToUpper(suffix),
	); err != nil {
		t.Fatalf("seed audit tenant %s: %v", suffix, err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (
    tenant_id, user_id, role, status, joined_at
)
VALUES ($1, $2, 'org_admin', 'active', now())`,
		tenantID,
		userID,
	); err != nil {
		t.Fatalf("seed audit admin membership %s: %v", suffix, err)
	}
	return tenantID, userID
}

func mustAuditTenantContext(
	t *testing.T,
	tenantID uuid.UUID,
	actorID uuid.UUID,
) tenancy.Context {
	t.Helper()
	tenantContext, err := tenancy.New(tenantID, actorID)
	if err != nil {
		t.Fatalf("create audit tenant context: %v", err)
	}
	return tenantContext
}

func expectAuditStatementRejected(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	wantCode string,
	wantConstraint string,
	query string,
	arguments ...any,
) {
	t.Helper()
	savepoint, err := transaction.Begin(ctx)
	if err != nil {
		t.Fatalf("begin audit rejection savepoint: %v", err)
	}
	defer func() {
		_ = savepoint.Rollback(context.Background())
	}()
	if _, err := savepoint.Exec(ctx, query, arguments...); err != nil {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) {
			t.Fatalf("expected PostgreSQL error %s, got %v", wantCode, err)
		}
		if postgresError.Code != wantCode {
			t.Fatalf("expected PostgreSQL error %s, got %s: %v", wantCode, postgresError.Code, err)
		}
		if wantConstraint != "" && postgresError.ConstraintName != wantConstraint {
			t.Fatalf(
				"expected constraint %s, got %s: %v",
				wantConstraint,
				postgresError.ConstraintName,
				err,
			)
		}
		return
	}
	t.Fatalf("statement unexpectedly succeeded: %s", query)
}

func requireAuditEnvironment(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required for integration tests", name)
	}
	return value
}
