//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresRepositoryOIDCSessionLifecycle(t *testing.T) {
	migrationURL := requireIdentityEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireIdentityEnvironment(t, "DATABASE_POOL_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number < 4 || version.Dirty {
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
	defer func() { _ = transaction.Rollback(context.Background()) }()

	userID := uuid.New()
	tenantID := uuid.New()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	email := "identity-" + unique + "@example.test"
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name) VALUES ($1, $2, 'Pending profile')`,
		userID,
		email,
	); err != nil {
		t.Fatalf("insert identity user: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name) VALUES ($1, $2, 'Identity Tenant')`,
		tenantID,
		"identity-"+unique,
	); err != nil {
		t.Fatalf("insert identity tenant: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, 'teacher', 'active', now())`,
		tenantID,
		userID,
	); err != nil {
		t.Fatalf("insert identity membership: %v", err)
	}

	now := time.Date(2026, time.July, 13, 15, 0, 0, 0, time.UTC)
	crypto, err := NewCrypto(bytes.Repeat([]byte{0x37}, 32))
	if err != nil {
		t.Fatalf("create identity crypto: %v", err)
	}
	provider := &fakeProvider{}
	service, err := NewService(
		NewPostgresRepository(transaction, 10*time.Second, policy.NewEngine()),
		provider,
		crypto,
		ServiceConfig{
			FlowTTL:            10 * time.Minute,
			SessionTTL:         8 * time.Hour,
			SessionAbsoluteTTL: 24 * time.Hour,
		},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("create identity service: %v", err)
	}

	start, err := service.BeginLogin(ctx, "/app/classrooms")
	if err != nil {
		t.Fatalf("begin OIDC flow: %v", err)
	}
	provider.claims = ProviderClaims{
		Issuer:        "https://identity.integration.example",
		Subject:       "subject-" + unique,
		Email:         email,
		EmailVerified: true,
		DisplayName:   "Integration Teacher",
		Locale:        "vi",
		Nonce:         provider.nonce,
		AuthTime:      now.Add(-time.Minute),
	}
	login, err := service.CompleteLogin(ctx, CallbackInput{
		State:          provider.state,
		BrowserBinding: start.BrowserBinding,
		Code:           "integration-code",
		UserAgent:      "TutorHub integration test",
		RemoteAddress:  "198.51.100.42:55000",
	})
	if err != nil {
		t.Fatalf("complete OIDC flow: %v", err)
	}
	principal, err := service.Authenticate(ctx, login.SessionToken)
	if err != nil {
		t.Fatalf("authenticate persisted session: %v", err)
	}
	if principal.User.ID != userID || principal.ActiveTenant == nil ||
		principal.ActiveTenant.ID != tenantID || principal.ActiveTenant.Role != "teacher" {
		t.Fatalf("unexpected persisted principal: %+v", principal)
	}
	if !containsPermission(principal.Permissions, "session.start") {
		t.Fatalf("teacher permissions were not resolved: %v", principal.Permissions)
	}

	var (
		emailVerified bool
		sessionHash   []byte
		flowConsumed  bool
	)
	if err := transaction.QueryRow(
		ctx,
		`SELECT email_verified FROM tutorhub.identities WHERE provider = $1 AND subject = $2`,
		provider.claims.Issuer,
		provider.claims.Subject,
	).Scan(&emailVerified); err != nil {
		t.Fatalf("read persisted identity: %v", err)
	}
	if !emailVerified {
		t.Fatal("verified email claim was not persisted")
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT token_hash FROM tutorhub.sessions WHERE id = $1`,
		principal.SessionID,
	).Scan(&sessionHash); err != nil {
		t.Fatalf("read persisted session hash: %v", err)
	}
	if !bytes.Equal(sessionHash, crypto.Digest(sessionPurpose, login.SessionToken)) {
		t.Fatal("database must contain the keyed session hash, not the opaque token")
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT consumed_at IS NOT NULL FROM tutorhub.auth_flows WHERE state_hash = $1`,
		crypto.Digest(statePurpose, provider.state),
	).Scan(&flowConsumed); err != nil {
		t.Fatalf("read consumed authentication flow: %v", err)
	}
	if !flowConsumed {
		t.Fatal("OIDC flow must be consumed atomically")
	}
	if _, err := service.CompleteLogin(ctx, CallbackInput{
		State:          provider.state,
		BrowserBinding: start.BrowserBinding,
		Code:           "replay",
	}); !errors.Is(err, ErrInvalidAuthFlow) {
		t.Fatalf("replayed flow must fail, got %v", err)
	}

	rotated, err := service.RotateCSRF(ctx, login.SessionToken)
	if err != nil {
		t.Fatalf("rotate persisted CSRF token: %v", err)
	}
	if _, err := service.ValidateCSRF(ctx, login.SessionToken, rotated.Token); err != nil {
		t.Fatalf("validate persisted CSRF token: %v", err)
	}
	if _, err := service.Logout(ctx, login.SessionToken); err != nil {
		t.Fatalf("revoke persisted session: %v", err)
	}
	if _, err := service.Authenticate(ctx, login.SessionToken); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("revoked session must not authenticate, got %v", err)
	}
}

func TestPostgresRepositoryTenantOnboardingAndSwitchIsolation(t *testing.T) {
	migrationURL := requireIdentityEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireIdentityEnvironment(t, "DATABASE_POOL_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create integration pool: %v", err)
	}
	defer pool.Close()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin integration transaction: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()

	userID := uuid.New()
	sessionID := uuid.New()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	expiresAt := now.Add(8 * time.Hour)
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name)
VALUES ($1, $2, 'Workspace owner')`,
		userID,
		"workspace-"+unique+"@example.test",
	); err != nil {
		t.Fatalf("insert workspace owner: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.sessions (
    id,
    user_id,
    token_hash,
    csrf_token_hash,
    created_at,
    last_seen_at,
    expires_at,
    absolute_expires_at,
    auth_time
)
VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $5)`,
		sessionID,
		userID,
		bytes.Repeat([]byte{0x10}, 32),
		bytes.Repeat([]byte{0x11}, 32),
		now.Add(-time.Minute),
		expiresAt,
		now.Add(24*time.Hour),
	); err != nil {
		t.Fatalf("insert onboarding session: %v", err)
	}

	repository := NewPostgresRepository(transaction, 10*time.Second, policy.NewEngine())
	creationRotation := SessionRotation{
		TokenHash: bytes.Repeat([]byte{0x20}, 32),
		CSRFHash:  bytes.Repeat([]byte{0x21}, 32),
		RotatedAt: now,
	}
	created, err := repository.CreateTenant(
		ctx,
		sessionID,
		userID,
		CreateTenantInput{Name: "Integration Workspace", Slug: "workspace-" + unique},
		creationRotation,
	)
	if err != nil {
		t.Fatalf("create first tenant: %v", err)
	}
	if created.Principal.ActiveTenant == nil ||
		created.Principal.ActiveTenant.Role != "org_admin" ||
		len(created.Principal.Memberships) != 1 ||
		!created.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("unexpected onboarding principal: %+v", created)
	}

	var (
		activeTenantID uuid.UUID
		persistedToken []byte
		ownerRole      string
		eventCount     int
	)
	if err := transaction.QueryRow(
		ctx,
		`SELECT active_tenant_id, token_hash
FROM tutorhub.sessions
WHERE id = $1`,
		sessionID,
	).Scan(&activeTenantID, &persistedToken); err != nil {
		t.Fatalf("read rotated onboarding session: %v", err)
	}
	if activeTenantID != created.Principal.ActiveTenant.ID ||
		!bytes.Equal(persistedToken, creationRotation.TokenHash) {
		t.Fatal("tenant creation must activate the tenant and replace the session token hash")
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT role FROM tutorhub.memberships WHERE tenant_id = $1 AND user_id = $2`,
		activeTenantID,
		userID,
	).Scan(&ownerRole); err != nil || ownerRole != "org_admin" {
		t.Fatalf("read owner membership: role=%q error=%v", ownerRole, err)
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'tenant.created'`,
		activeTenantID,
	).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("read tenant outbox event: count=%d error=%v", eventCount, err)
	}

	if _, err := repository.CreateTenant(
		ctx,
		sessionID,
		userID,
		CreateTenantInput{Name: "Unexpected Workspace", Slug: "duplicate-" + unique},
		SessionRotation{
			TokenHash: bytes.Repeat([]byte{0x22}, 32),
			CSRFHash:  bytes.Repeat([]byte{0x23}, 32),
			RotatedAt: now.Add(time.Second),
		},
	); !errors.Is(err, ErrTenantCreationDenied) {
		t.Fatalf("second onboarding tenant must be denied, got %v", err)
	}

	secondTenantID := uuid.New()
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name) VALUES ($1, $2, 'Second Workspace')`,
		secondTenantID,
		"second-"+unique,
	); err != nil {
		t.Fatalf("insert second tenant: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, 'teacher', 'active', $3)`,
		secondTenantID,
		userID,
		now,
	); err != nil {
		t.Fatalf("insert second tenant membership: %v", err)
	}
	switchRotation := SessionRotation{
		TokenHash: bytes.Repeat([]byte{0x30}, 32),
		CSRFHash:  bytes.Repeat([]byte{0x31}, 32),
		RotatedAt: now.Add(2 * time.Second),
	}
	switched, err := repository.SwitchActiveTenant(
		ctx,
		sessionID,
		userID,
		secondTenantID,
		switchRotation,
	)
	if err != nil {
		t.Fatalf("switch active tenant: %v", err)
	}
	if switched.Principal.ActiveTenant == nil ||
		switched.Principal.ActiveTenant.ID != secondTenantID ||
		switched.Principal.ActiveTenant.Role != "teacher" ||
		!containsPermission(switched.Principal.Permissions, "class.create") {
		t.Fatalf("unexpected switched principal: %+v", switched.Principal)
	}

	foreignTenantID := uuid.New()
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name) VALUES ($1, $2, 'Foreign Workspace')`,
		foreignTenantID,
		"foreign-"+unique,
	); err != nil {
		t.Fatalf("insert foreign tenant: %v", err)
	}
	if _, err := repository.SwitchActiveTenant(
		ctx,
		sessionID,
		userID,
		foreignTenantID,
		SessionRotation{
			TokenHash: bytes.Repeat([]byte{0x40}, 32),
			CSRFHash:  bytes.Repeat([]byte{0x41}, 32),
			RotatedAt: now.Add(3 * time.Second),
		},
	); !errors.Is(err, ErrTenantAccessDenied) {
		t.Fatalf("cross-tenant switch must be denied, got %v", err)
	}
}

func containsPermission(permissions []string, expected string) bool {
	for _, permission := range permissions {
		if permission == expected {
			return true
		}
	}
	return false
}

func requireIdentityEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for integration tests", key)
	}
	return value
}
