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
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
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
	if version.Number < 7 || version.Dirty {
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
	providerIssuer := "https://identity.integration.example"
	providerSubject := "subject-" + unique
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
	// Existing users must already own the provider/subject link; verified email alone
	// must not claim an account after P2-01 collision protection.
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.identities (
    user_id,
    provider,
    subject,
    email_at_provider,
    email_verified,
    status
)
VALUES ($1, $2, $3, $4, true, 'active')`,
		userID,
		providerIssuer,
		providerSubject,
		email,
	); err != nil {
		t.Fatalf("insert linked login identity: %v", err)
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
		policy.NewEngine(),
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
		Issuer:        providerIssuer,
		Subject:       providerSubject,
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
	staleBootstrapSessionID := uuid.New()
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
		staleBootstrapSessionID,
		userID,
		bytes.Repeat([]byte{0x12}, 32),
		bytes.Repeat([]byte{0x13}, 32),
		now.Add(-time.Minute),
		expiresAt,
		now.Add(24*time.Hour),
	); err != nil {
		t.Fatalf("insert second onboarding session: %v", err)
	}

	repository := NewPostgresRepository(transaction, 10*time.Second, policy.NewEngine())
	creationRotation := SessionRotation{
		TokenHash:              bytes.Repeat([]byte{0x20}, 32),
		CSRFHash:               bytes.Repeat([]byte{0x21}, 32),
		ExpectedContextVersion: 1,
		RotatedAt:              now,
	}
	created, err := repository.CreateTenant(
		ctx,
		sessionID,
		userID,
		uuid.Nil,
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
		contextVersion int64
		ownerRole      string
		eventCount     int
	)
	if err := transaction.QueryRow(
		ctx,
		`SELECT active_tenant_id, token_hash, context_version
FROM tutorhub.sessions
WHERE id = $1`,
		sessionID,
	).Scan(&activeTenantID, &persistedToken, &contextVersion); err != nil {
		t.Fatalf("read rotated onboarding session: %v", err)
	}
	if activeTenantID != created.Principal.ActiveTenant.ID ||
		!bytes.Equal(persistedToken, creationRotation.TokenHash) ||
		contextVersion != 2 || created.Principal.ContextVersion != 2 {
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

	staleBootstrapSlug := "stale-bootstrap-" + unique
	if _, err := repository.CreateTenant(
		ctx,
		staleBootstrapSessionID,
		userID,
		uuid.Nil,
		CreateTenantInput{Name: "Stale Bootstrap Workspace", Slug: staleBootstrapSlug},
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x26}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x27}, 32),
			ExpectedContextVersion: 1,
			RotatedAt:              now.Add(500 * time.Millisecond),
		},
	); !errors.Is(err, ErrTenantCreationDenied) {
		t.Fatalf("second stale bootstrap session must be denied, got %v", err)
	}
	var staleBootstrapTenantCount int
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.tenants WHERE slug = $1`,
		staleBootstrapSlug,
	).Scan(&staleBootstrapTenantCount); err != nil || staleBootstrapTenantCount != 0 {
		t.Fatalf(
			"stale bootstrap must not persist a tenant: count=%d error=%v",
			staleBootstrapTenantCount,
			err,
		)
	}

	if _, err := repository.CreateTenant(
		ctx,
		sessionID,
		userID,
		uuid.Nil,
		CreateTenantInput{Name: "Stale Workspace", Slug: "stale-" + unique},
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x22}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x23}, 32),
			ExpectedContextVersion: 1,
			RotatedAt:              now.Add(time.Second),
		},
	); !errors.Is(err, ErrSessionContextConflict) {
		t.Fatalf("stale tenant creation must lose the context CAS, got %v", err)
	}

	secondCreated, err := repository.CreateTenant(
		ctx,
		sessionID,
		userID,
		activeTenantID,
		CreateTenantInput{Name: "Second Workspace", Slug: "second-" + unique},
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x24}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x25}, 32),
			ExpectedContextVersion: 2,
			RotatedAt:              now.Add(2 * time.Second),
		},
	)
	if err != nil {
		t.Fatalf("create second managed tenant: %v", err)
	}
	if secondCreated.Principal.ActiveTenant == nil ||
		secondCreated.Principal.ActiveTenant.Role != "org_admin" ||
		secondCreated.Principal.ContextVersion != 3 {
		t.Fatalf("unexpected second tenant principal: %+v", secondCreated.Principal)
	}
	secondTenantID := secondCreated.Principal.ActiveTenant.ID
	secondManagedContext, err := tenancy.New(secondTenantID, userID)
	if err != nil {
		t.Fatalf("create second managed tenant context: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.memberships SET role = 'teacher' WHERE tenant_id = $1 AND user_id = $2`,
		secondTenantID,
		userID,
	); err != nil {
		t.Fatalf("demote managed tenant membership: %v", err)
	}
	deniedName := "Denied stale administrator update"
	if _, err := repository.UpdateTenant(
		ctx,
		secondManagedContext,
		UpdateTenantInput{Name: &deniedName, ExpectedVersion: 1},
		now.Add(2500*time.Millisecond),
	); !errors.Is(err, ErrTenantAccessDenied) {
		t.Fatalf("repository must re-authorize the locked membership, got %v", err)
	}
	if _, err := repository.CreateTenant(
		ctx,
		sessionID,
		userID,
		secondTenantID,
		CreateTenantInput{Name: "Denied Demoted Workspace", Slug: "demoted-" + unique},
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x28}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x29}, 32),
			ExpectedContextVersion: 3,
			RotatedAt:              now.Add(2600 * time.Millisecond),
		},
	); !errors.Is(err, ErrTenantCreationDenied) {
		t.Fatalf("demoted source membership must not authorize tenant creation, got %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.memberships SET role = 'org_admin' WHERE tenant_id = $1 AND user_id = $2`,
		secondTenantID,
		userID,
	); err != nil {
		t.Fatalf("restore managed tenant membership: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.memberships SET status = 'suspended' WHERE tenant_id = $1 AND user_id = $2`,
		secondTenantID,
		userID,
	); err != nil {
		t.Fatalf("suspend managed tenant membership before create: %v", err)
	}
	if _, err := repository.CreateTenant(
		ctx,
		sessionID,
		userID,
		secondTenantID,
		CreateTenantInput{Name: "Denied Suspended Workspace", Slug: "suspended-" + unique},
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x2a}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x2b}, 32),
			ExpectedContextVersion: 3,
			RotatedAt:              now.Add(2700 * time.Millisecond),
		},
	); !errors.Is(err, ErrTenantCreationDenied) {
		t.Fatalf("suspended source membership must not authorize tenant creation, got %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.memberships SET status = 'active' WHERE tenant_id = $1 AND user_id = $2`,
		secondTenantID,
		userID,
	); err != nil {
		t.Fatalf("reactivate managed tenant membership: %v", err)
	}

	switchRotation := SessionRotation{
		TokenHash:              bytes.Repeat([]byte{0x30}, 32),
		CSRFHash:               bytes.Repeat([]byte{0x31}, 32),
		ExpectedContextVersion: 3,
		RotatedAt:              now.Add(3 * time.Second),
	}
	switched, err := repository.SwitchActiveTenant(
		ctx,
		sessionID,
		userID,
		activeTenantID,
		switchRotation,
	)
	if err != nil {
		t.Fatalf("switch active tenant: %v", err)
	}
	if switched.Principal.ActiveTenant == nil ||
		switched.Principal.ActiveTenant.ID != activeTenantID ||
		switched.Principal.ActiveTenant.Role != "org_admin" ||
		switched.Principal.ContextVersion != 4 ||
		!containsPermission(switched.Principal.Permissions, "tenant.manage") {
		t.Fatalf("unexpected switched principal: %+v", switched.Principal)
	}

	foreignTenantID := uuid.New()
	foreignSlug := "foreign-" + unique
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name) VALUES ($1, $2, 'Foreign Workspace')`,
		foreignTenantID,
		foreignSlug,
	); err != nil {
		t.Fatalf("insert foreign tenant: %v", err)
	}
	if _, err := repository.SwitchActiveTenant(
		ctx,
		sessionID,
		userID,
		foreignTenantID,
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x40}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x41}, 32),
			ExpectedContextVersion: 4,
			RotatedAt:              now.Add(4 * time.Second),
		},
	); !errors.Is(err, ErrTenantAccessDenied) {
		t.Fatalf("cross-tenant switch must be denied, got %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.memberships SET status = 'suspended' WHERE tenant_id = $1 AND user_id = $2`,
		secondTenantID,
		userID,
	); err != nil {
		t.Fatalf("suspend second tenant membership: %v", err)
	}
	if _, err := repository.SwitchActiveTenant(
		ctx,
		sessionID,
		userID,
		secondTenantID,
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x42}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x43}, 32),
			ExpectedContextVersion: 4,
			RotatedAt:              now.Add(4500 * time.Millisecond),
		},
	); !errors.Is(err, ErrTenantAccessDenied) {
		t.Fatalf("inactive membership switch must be denied, got %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.memberships SET status = 'active' WHERE tenant_id = $1 AND user_id = $2`,
		secondTenantID,
		userID,
	); err != nil {
		t.Fatalf("restore second tenant membership: %v", err)
	}

	listed, err := repository.ListTenants(ctx, userID)
	if err != nil {
		t.Fatalf("list active tenant memberships: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("tenant list must include exactly two active memberships: %+v", listed)
	}

	tenantContext, err := tenancy.New(activeTenantID, userID)
	if err != nil {
		t.Fatalf("create tenant context: %v", err)
	}
	currentTenant, err := repository.GetTenant(ctx, tenantContext)
	if err != nil || currentTenant.ID != activeTenantID || currentTenant.Version != 1 ||
		!currentTenant.IsActive {
		t.Fatalf("get active tenant: tenant=%+v error=%v", currentTenant, err)
	}
	updatedName := "Updated Integration Workspace"
	updatedSlug := "updated-" + unique
	updated, err := repository.UpdateTenant(
		ctx,
		tenantContext,
		UpdateTenantInput{
			Name:            &updatedName,
			Slug:            &updatedSlug,
			ExpectedVersion: 1,
		},
		now.Add(5*time.Second),
	)
	if err != nil {
		t.Fatalf("update active tenant: %v", err)
	}
	if updated.Name != updatedName || updated.Slug != updatedSlug || updated.Version != 2 {
		t.Fatalf("unexpected updated tenant: %+v", updated)
	}
	if _, err := repository.UpdateTenant(
		ctx,
		tenantContext,
		UpdateTenantInput{Name: &updatedName, ExpectedVersion: 1},
		now.Add(6*time.Second),
	); !errors.Is(err, ErrTenantVersionConflict) {
		t.Fatalf("stale tenant update must fail, got %v", err)
	}
	if _, err := repository.UpdateTenant(
		ctx,
		tenantContext,
		UpdateTenantInput{Slug: &foreignSlug, ExpectedVersion: 2},
		now.Add(7*time.Second),
	); !errors.Is(err, ErrTenantSlugTaken) {
		t.Fatalf("tenant slug collision must fail, got %v", err)
	}

	foreignContext, err := tenancy.New(foreignTenantID, userID)
	if err != nil {
		t.Fatalf("create foreign tenant context: %v", err)
	}
	if _, err := repository.GetTenant(ctx, foreignContext); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("cross-tenant lookup must be concealed, got %v", err)
	}
	if _, err := repository.UpdateTenant(
		ctx,
		foreignContext,
		UpdateTenantInput{Name: &updatedName, ExpectedVersion: 1},
		now.Add(8*time.Second),
	); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("cross-tenant update must be concealed, got %v", err)
	}

	otherSessionID := uuid.New()
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.sessions (
    id, user_id, active_tenant_id, token_hash, csrf_token_hash,
    created_at, last_seen_at, expires_at, absolute_expires_at, auth_time
)
VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $6)`,
		otherSessionID,
		userID,
		activeTenantID,
		bytes.Repeat([]byte{0x50}, 32),
		bytes.Repeat([]byte{0x51}, 32),
		now.Add(-time.Minute),
		expiresAt,
		now.Add(24*time.Hour),
	); err != nil {
		t.Fatalf("insert second active tenant session: %v", err)
	}

	archiveRotation := SessionRotation{
		TokenHash:              bytes.Repeat([]byte{0x60}, 32),
		CSRFHash:               bytes.Repeat([]byte{0x61}, 32),
		ExpectedContextVersion: 4,
		RotatedAt:              now.Add(9 * time.Second),
	}
	archived, err := repository.ArchiveTenant(
		ctx,
		tenantContext,
		sessionID,
		2,
		archiveRotation,
	)
	if err != nil {
		t.Fatalf("archive managed tenant: %v", err)
	}
	if archived.Principal.ActiveTenant != nil || archived.Principal.ContextVersion != 5 ||
		len(archived.Principal.Memberships) != 1 ||
		archived.Principal.Memberships[0].ID != secondTenantID {
		t.Fatalf("archive must clear active context and preserve fallback: %+v", archived.Principal)
	}

	var (
		archivedStatus       string
		archivedVersion      int64
		archivedAt           *time.Time
		currentActiveTenant  uuid.NullUUID
		currentContext       int64
		currentToken         []byte
		otherActiveTenant    uuid.NullUUID
		otherContext         int64
		persistedMemberships int
	)
	if err := transaction.QueryRow(
		ctx,
		`SELECT status, version, archived_at FROM tutorhub.tenants WHERE id = $1`,
		activeTenantID,
	).Scan(&archivedStatus, &archivedVersion, &archivedAt); err != nil {
		t.Fatalf("read archived tenant: %v", err)
	}
	if archivedStatus != "archived" || archivedVersion != 3 || archivedAt == nil {
		t.Fatalf("unexpected archived tenant state: status=%s version=%d at=%v", archivedStatus, archivedVersion, archivedAt)
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT active_tenant_id, context_version, token_hash
FROM tutorhub.sessions WHERE id = $1`,
		sessionID,
	).Scan(&currentActiveTenant, &currentContext, &currentToken); err != nil {
		t.Fatalf("read archived current session: %v", err)
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT active_tenant_id, context_version FROM tutorhub.sessions WHERE id = $1`,
		otherSessionID,
	).Scan(&otherActiveTenant, &otherContext); err != nil {
		t.Fatalf("read archived secondary session: %v", err)
	}
	if currentActiveTenant.Valid || currentContext != 5 ||
		!bytes.Equal(currentToken, archiveRotation.TokenHash) ||
		otherActiveTenant.Valid || otherContext != 2 {
		t.Fatalf(
			"archive must clear every session context: current=(%v,%d) other=(%v,%d)",
			currentActiveTenant,
			currentContext,
			otherActiveTenant,
			otherContext,
		)
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.memberships WHERE tenant_id = $1`,
		activeTenantID,
	).Scan(&persistedMemberships); err != nil || persistedMemberships != 1 {
		t.Fatalf("archive must preserve memberships: count=%d error=%v", persistedMemberships, err)
	}

	listed, err = repository.ListTenants(ctx, userID)
	if err != nil || len(listed) != 1 || listed[0].ID != secondTenantID {
		t.Fatalf("archived tenant must leave the active list: tenants=%+v error=%v", listed, err)
	}
	selectedTenantID, err := selectActiveTenant(ctx, transaction, userID)
	if err != nil || selectedTenantID != secondTenantID {
		t.Fatalf("default tenant selection must skip archived tenants: id=%s error=%v", selectedTenantID, err)
	}

	resumed, err := repository.SwitchActiveTenant(
		ctx,
		sessionID,
		userID,
		secondTenantID,
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x70}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x71}, 32),
			ExpectedContextVersion: 5,
			RotatedAt:              now.Add(10 * time.Second),
		},
	)
	if err != nil || resumed.Principal.ActiveTenant == nil ||
		resumed.Principal.ActiveTenant.ID != secondTenantID ||
		resumed.Principal.ContextVersion != 6 {
		t.Fatalf("resume fallback tenant: result=%+v error=%v", resumed, err)
	}
	secondContext, err := tenancy.New(secondTenantID, userID)
	if err != nil {
		t.Fatalf("create second tenant context: %v", err)
	}
	if _, err := repository.ArchiveTenant(
		ctx,
		secondContext,
		sessionID,
		1,
		SessionRotation{
			TokenHash:              bytes.Repeat([]byte{0x72}, 32),
			CSRFHash:               bytes.Repeat([]byte{0x73}, 32),
			ExpectedContextVersion: 6,
			RotatedAt:              now.Add(11 * time.Second),
		},
	); !errors.Is(err, ErrLastManagedTenant) {
		t.Fatalf("last managed tenant archive must fail, got %v", err)
	}

	var (
		switchActor              string
		switchFrom               string
		switchTo                 string
		switchVersion            int64
		archiveActor             string
		archiveEventVersion      int64
		containsForbiddenPayload bool
	)
	if err := transaction.QueryRow(
		ctx,
		`SELECT payload->>'actor_user_id', payload->>'from_tenant_id',
        payload->>'to_tenant_id', (payload->>'version')::bigint
FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'tenant.switched'`,
		activeTenantID,
	).Scan(&switchActor, &switchFrom, &switchTo, &switchVersion); err != nil {
		t.Fatalf("read tenant switch event: %v", err)
	}
	if switchActor != userID.String() || switchFrom != secondTenantID.String() ||
		switchTo != activeTenantID.String() || switchVersion != 1 {
		t.Fatalf("unexpected switch event: actor=%s from=%s to=%s version=%d", switchActor, switchFrom, switchTo, switchVersion)
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT payload->>'actor_user_id', (payload->>'version')::bigint,
        payload ?| ARRAY['name', 'slug', 'session_id', 'token']
FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'tenant.archived'`,
		activeTenantID,
	).Scan(&archiveActor, &archiveEventVersion, &containsForbiddenPayload); err != nil {
		t.Fatalf("read tenant archive event: %v", err)
	}
	if archiveActor != userID.String() || archiveEventVersion != 3 || containsForbiddenPayload {
		t.Fatalf("unexpected archive event: actor=%s version=%d forbidden=%v", archiveActor, archiveEventVersion, containsForbiddenPayload)
	}
	for _, eventType := range []string{"tenant.created", "tenant.updated", "tenant.archived"} {
		if err := transaction.QueryRow(
			ctx,
			`SELECT count(*) FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = $2`,
			activeTenantID,
			eventType,
		).Scan(&eventCount); err != nil || eventCount != 1 {
			t.Fatalf("expected one %s event: count=%d error=%v", eventType, eventCount, err)
		}
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.outbox_events e
    CROSS JOIN LATERAL jsonb_object_keys(e.payload) AS keys(payload_key)
    WHERE e.event_type LIKE 'tenant.%'
      AND e.tenant_id IN ($1, $2)
      AND payload_key NOT IN (
          'actor_user_id', 'from_tenant_id', 'to_tenant_id', 'version'
      )
)`,
		activeTenantID,
		secondTenantID,
	).Scan(&containsForbiddenPayload); err != nil {
		t.Fatalf("inspect tenant event payload keys: %v", err)
	}
	if containsForbiddenPayload {
		t.Fatal("tenant event payload must contain only actor/from/to/version metadata")
	}
}

func TestPostgresRepositoryProfileAndIdentityPersistence(t *testing.T) {
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
	foreignUserID := uuid.New()
	tenantID := uuid.New()
	sessionID := uuid.New()
	firstIdentityID := uuid.New()
	secondIdentityID := uuid.New()
	foreignIdentityID := uuid.New()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	now := time.Date(2026, time.July, 17, 8, 0, 0, 0, time.UTC)

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name)
VALUES
    ($1, $2, 'Profile owner'),
    ($3, $4, 'Foreign identity owner')`,
		userID,
		"profile-"+unique+"@example.test",
		foreignUserID,
		"foreign-"+unique+"@example.test",
	); err != nil {
		t.Fatalf("insert profile users: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, 'Profile Tenant')`,
		tenantID,
		"profile-"+unique,
	); err != nil {
		t.Fatalf("insert profile tenant: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, 'teacher', 'active', $3)`,
		tenantID,
		userID,
		now.Add(-time.Hour),
	); err != nil {
		t.Fatalf("insert profile membership: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.identities (
    id,
    user_id,
    provider,
    subject,
    email_at_provider,
    email_verified,
    last_authenticated_at,
    status
)
VALUES
    ($1, $2, 'https://primary.identity.test', $3, $4, true, $5, 'active'),
    ($6, $2, 'https://backup.identity.test', $7, $4, true, $5, 'active'),
    ($8, $9, 'https://foreign.identity.test', $10, $11, true, $5, 'active')`,
		firstIdentityID,
		userID,
		"primary-"+unique,
		"profile-"+unique+"@example.test",
		now.Add(-time.Minute),
		secondIdentityID,
		"backup-"+unique,
		foreignIdentityID,
		foreignUserID,
		"foreign-"+unique,
		"foreign-"+unique+"@example.test",
	); err != nil {
		t.Fatalf("insert profile identities: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.sessions (
    id,
    user_id,
    identity_id,
    active_tenant_id,
    token_hash,
    csrf_token_hash,
    created_at,
    last_seen_at,
    expires_at,
    absolute_expires_at,
    auth_time
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8, $9, $7)`,
		sessionID,
		userID,
		firstIdentityID,
		tenantID,
		bytes.Repeat([]byte{0x51}, 32),
		bytes.Repeat([]byte{0x52}, 32),
		now.Add(-time.Minute),
		now.Add(8*time.Hour),
		now.Add(24*time.Hour),
	); err != nil {
		t.Fatalf("insert profile session: %v", err)
	}

	repository := NewPostgresRepository(transaction, 10*time.Second, policy.NewEngine())
	profile, err := repository.GetProfile(ctx, userID)
	if err != nil {
		t.Fatalf("get persisted profile: %v", err)
	}
	if profile.ID != userID || profile.DisplayName != "Profile owner" || profile.Locale != "vi" {
		t.Fatalf("unexpected initial profile: %+v", profile)
	}

	displayName := "Updated Teacher"
	locale := "en"
	timezone := "Europe/London"
	avatarObjectKey := "avatars/" + userID.String() + "/profile.webp"
	updated, err := repository.UpdateProfile(
		ctx,
		sessionID,
		userID,
		ProfilePatch{
			DisplayName:     &displayName,
			Locale:          &locale,
			Timezone:        &timezone,
			AvatarObjectKey: &avatarObjectKey,
		},
		now,
	)
	if err != nil {
		t.Fatalf("update persisted profile: %v", err)
	}
	if updated.DisplayName != displayName || updated.Locale != locale ||
		updated.Timezone != timezone || updated.AvatarObjectKey != avatarObjectKey {
		t.Fatalf("unexpected updated profile: %+v", updated)
	}

	listed, err := repository.ListIdentities(ctx, userID)
	if err != nil {
		t.Fatalf("list persisted identities: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected two initial identities, got %d", len(listed))
	}

	linked, err := repository.LinkIdentity(
		ctx,
		userID,
		sessionID,
		ProviderClaims{
			Issuer:        "https://linked.identity.test",
			Subject:       "linked-" + unique,
			Email:         "linked-" + unique + "@example.test",
			EmailVerified: true,
		},
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("link persisted identity: %v", err)
	}
	if linked.ID == uuid.Nil || linked.Provider != "https://linked.identity.test" {
		t.Fatalf("unexpected linked identity: %+v", linked)
	}

	if _, err := repository.LinkIdentity(
		ctx,
		userID,
		sessionID,
		ProviderClaims{
			Issuer:  "https://foreign.identity.test",
			Subject: "foreign-" + unique,
			Email:   "collision-" + unique + "@example.test",
		},
		now.Add(2*time.Second),
	); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("foreign identity collision must fail, got %v", err)
	}

	if err := repository.UnlinkIdentity(
		ctx,
		userID,
		sessionID,
		firstIdentityID,
		now.Add(3*time.Second),
	); err != nil {
		t.Fatalf("unlink first identity: %v", err)
	}
	if err := repository.UnlinkIdentity(
		ctx,
		userID,
		sessionID,
		secondIdentityID,
		now.Add(4*time.Second),
	); err != nil {
		t.Fatalf("unlink second identity: %v", err)
	}
	if err := repository.UnlinkIdentity(
		ctx,
		userID,
		sessionID,
		linked.ID,
		now.Add(5*time.Second),
	); !errors.Is(err, ErrLastIdentity) {
		t.Fatalf("last identity unlink must fail, got %v", err)
	}

	expectedEvents := map[string]int{
		"identity.profile.updated": 1,
		"identity.linked":          1,
		"identity.unlinked":        2,
	}
	for eventType, expectedCount := range expectedEvents {
		var eventCount int
		if err := transaction.QueryRow(
			ctx,
			`SELECT count(*)
FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = $2`,
			userID,
			eventType,
		).Scan(&eventCount); err != nil {
			t.Fatalf("read %s outbox event: %v", eventType, err)
		}
		if eventCount != expectedCount {
			t.Fatalf("expected %d %s events, got %d", expectedCount, eventType, eventCount)
		}
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
