//go:build integration

package identity

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresMembershipInvitationLifecycle(t *testing.T) {
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
	if version.Number < 8 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
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

	now := time.Date(2026, time.July, 18, 10, 0, 0, 0, time.UTC)
	fixture := insertInvitationIntegrationFixture(t, ctx, transaction, now)
	repository := NewPostgresRepository(
		transaction,
		10*time.Second,
		policy.NewEngine(),
	)
	service := newInvitationIntegrationService(t, repository, now)

	created, err := service.CreateMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		CreateMembershipInvitationInput{
			Email:        fixture.invitee.User.Email,
			IntendedRole: "student",
		},
	)
	if err != nil {
		t.Fatalf("create persisted invitation: %v", err)
	}
	if created.Invitation.Status != MembershipInvitationPending ||
		created.Invitation.Email != fixture.invitee.User.Email {
		t.Fatalf("unexpected created invitation: %+v", created.Invitation)
	}

	var storedHash []byte
	if err := transaction.QueryRow(
		ctx,
		`SELECT token_hash
FROM tutorhub.membership_invitations
WHERE id = $1 AND tenant_id = $2`,
		created.Invitation.ID,
		fixture.tenantID,
	).Scan(&storedHash); err != nil {
		t.Fatalf("read persisted invitation token hash: %v", err)
	}
	if len(storedHash) != 32 ||
		!bytes.Equal(
			storedHash,
			service.crypto.Digest(membershipInvitationTokenPurpose, created.Token),
		) ||
		bytes.Contains(storedHash, []byte(created.Token)) {
		t.Fatal("database must contain only the purpose-bound invitation token HMAC")
	}

	preview, err := service.PreviewMembershipInvitation(ctx, created.Token)
	if err != nil {
		t.Fatalf("preview persisted invitation: %v", err)
	}
	if preview.TenantName != fixture.tenantName ||
		preview.MaskedEmail != "i***@example.test" ||
		preview.Status != MembershipInvitationPending {
		t.Fatalf("unexpected persisted invitation preview: %+v", preview)
	}

	acceptContext, _ := requestmeta.New(ctx, "accept-invitation", "", "", now)
	requestmeta.SetPrincipal(acceptContext, fixture.invitee.User.ID, uuid.Nil)
	accepted, err := service.AcceptMembershipInvitation(
		acceptContext,
		fixture.invitee,
		created.Token,
	)
	if err != nil {
		t.Fatalf("accept persisted invitation: %v", err)
	}
	if accepted.Invitation.Status != MembershipInvitationAccepted ||
		accepted.Invitation.AcceptedBy == nil ||
		*accepted.Invitation.AcceptedBy != fixture.invitee.User.ID ||
		accepted.Principal.ActiveTenant != nil {
		t.Fatalf("unexpected invitation acceptance: %+v", accepted)
	}
	if len(accepted.Principal.Memberships) != 1 ||
		accepted.Principal.Memberships[0].ID != fixture.tenantID ||
		accepted.Principal.Memberships[0].Role != "student" ||
		accepted.Principal.Memberships[0].IsActive {
		t.Fatalf("accept must add membership without switching workspace: %+v", accepted.Principal)
	}
	assertResolvedInvitationAuditTenant(t, acceptContext, fixture.tenantID)

	repeatContext, _ := requestmeta.New(ctx, "repeat-accept-invitation", "", "", now)
	requestmeta.SetPrincipal(repeatContext, fixture.invitee.User.ID, uuid.Nil)
	repeated, err := service.AcceptMembershipInvitation(
		repeatContext,
		fixture.invitee,
		created.Token,
	)
	if err != nil || repeated.Invitation.ID != created.Invitation.ID {
		t.Fatalf("repeat acceptance must be idempotent: result=%+v error=%v", repeated, err)
	}
	assertResolvedInvitationAuditTenant(t, repeatContext, fixture.tenantID)
	if _, err := service.PreviewMembershipInvitation(
		ctx,
		created.Token,
	); !errors.Is(err, ErrMembershipInvitationUnavailable) {
		t.Fatalf("accepted token must not remain previewable, got %v", err)
	}

	var membershipCount, acceptanceEventCount int
	var activeTenantID uuid.NullUUID
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.memberships
WHERE tenant_id = $1 AND user_id = $2`,
		fixture.tenantID,
		fixture.invitee.User.ID,
	).Scan(&membershipCount); err != nil {
		t.Fatalf("count accepted membership: %v", err)
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'membership.invitation.accepted'`,
		created.Invitation.ID,
	).Scan(&acceptanceEventCount); err != nil {
		t.Fatalf("count invitation acceptance events: %v", err)
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT active_tenant_id FROM tutorhub.sessions WHERE id = $1`,
		fixture.invitee.SessionID,
	).Scan(&activeTenantID); err != nil {
		t.Fatalf("read invitation acceptance session: %v", err)
	}
	if membershipCount != 1 || acceptanceEventCount != 1 || activeTenantID.Valid {
		t.Fatalf(
			"acceptance was not idempotent: memberships=%d events=%d active_tenant=%v",
			membershipCount,
			acceptanceEventCount,
			activeTenantID,
		)
	}
	assertInvitationOutboxRedacted(t, ctx, transaction, created.Invitation.ID, created.Token)
	if _, err := service.CreateMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		CreateMembershipInvitationInput{
			Email:        fixture.invitee.User.Email,
			IntendedRole: "student",
		},
	); !errors.Is(err, ErrMembershipInvitationConflict) {
		t.Fatalf("existing membership must block a new invitation, got %v", err)
	}

	mismatch, err := service.CreateMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		CreateMembershipInvitationInput{
			Email:        "mismatch-" + fixture.unique + "@example.test",
			IntendedRole: "teacher",
		},
	)
	if err != nil {
		t.Fatalf("create identity-mismatch invitation: %v", err)
	}
	mismatchContext, _ := requestmeta.New(ctx, "mismatch-accept-invitation", "", "", now)
	requestmeta.SetPrincipal(mismatchContext, fixture.invitee.User.ID, uuid.Nil)
	if _, err := service.AcceptMembershipInvitation(
		mismatchContext,
		fixture.invitee,
		mismatch.Token,
	); !errors.Is(err, ErrMembershipInvitationIdentityMismatch) {
		t.Fatalf("expected verified identity mismatch, got %v", err)
	}
	assertResolvedInvitationAuditTenant(t, mismatchContext, fixture.tenantID)

	unresolvedContext, _ := requestmeta.New(ctx, "unknown-accept-invitation", "", "", now)
	requestmeta.SetPrincipal(unresolvedContext, fixture.invitee.User.ID, uuid.Nil)
	if _, err := service.AcceptMembershipInvitation(
		unresolvedContext,
		fixture.invitee,
		membershipInvitationTestToken(0x7f),
	); !errors.Is(err, ErrMembershipInvitationUnavailable) {
		t.Fatalf("expected unknown invitation token to be unavailable, got %v", err)
	}
	if snapshot := requestmeta.SnapshotFromContext(unresolvedContext); snapshot.AuditTenantResolved || snapshot.AuditTenantID != uuid.Nil {
		t.Fatalf("unknown token must not resolve a durable audit tenant: %#v", snapshot)
	}

	revocable, err := service.CreateMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		CreateMembershipInvitationInput{
			Email:        "revoked-" + fixture.unique + "@example.test",
			IntendedRole: "guest",
		},
	)
	if err != nil {
		t.Fatalf("create revocable invitation: %v", err)
	}
	if _, err := service.CreateMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		CreateMembershipInvitationInput{
			Email:        revocable.Invitation.Email,
			IntendedRole: "guest",
		},
	); !errors.Is(err, ErrMembershipInvitationConflict) {
		t.Fatalf("duplicate pending invitation must conflict, got %v", err)
	}
	revoked, err := service.RevokeMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		revocable.Invitation.ID,
	)
	if err != nil || revoked.Status != MembershipInvitationRevoked {
		t.Fatalf("revoke invitation: invitation=%+v error=%v", revoked, err)
	}
	repeatedRevoke, err := service.RevokeMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		revocable.Invitation.ID,
	)
	if err != nil || repeatedRevoke.Status != MembershipInvitationRevoked {
		t.Fatalf("repeat revoke must be idempotent: invitation=%+v error=%v", repeatedRevoke, err)
	}
	if _, err := service.PreviewMembershipInvitation(
		ctx,
		revocable.Token,
	); !errors.Is(err, ErrMembershipInvitationUnavailable) {
		t.Fatalf("revoked token must be unavailable, got %v", err)
	}
	reinvited, err := service.CreateMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		CreateMembershipInvitationInput{
			Email:        revocable.Invitation.Email,
			IntendedRole: "guest",
		},
	)
	if err != nil || reinvited.Invitation.ID == revocable.Invitation.ID {
		t.Fatalf("revoked address must be re-invitable: invitation=%+v error=%v", reinvited, err)
	}

	expiring, err := service.CreateMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		CreateMembershipInvitationInput{
			Email:        "expired-" + fixture.unique + "@example.test",
			IntendedRole: "student",
		},
	)
	if err != nil {
		t.Fatalf("create expiring invitation: %v", err)
	}
	lateService := newInvitationIntegrationService(t, repository, now.Add(8*24*time.Hour))
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := lateService.PreviewMembershipInvitation(
			ctx,
			expiring.Token,
		); !errors.Is(err, ErrMembershipInvitationUnavailable) {
			t.Fatalf("expired token preview %d returned %v", attempt+1, err)
		}
	}
	var expiredStatus string
	var expirationEventCount int
	if err := transaction.QueryRow(
		ctx,
		`SELECT status FROM tutorhub.membership_invitations WHERE id = $1`,
		expiring.Invitation.ID,
	).Scan(&expiredStatus); err != nil {
		t.Fatalf("read expired invitation status: %v", err)
	}
	if err := transaction.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'membership.invitation.expired'`,
		expiring.Invitation.ID,
	).Scan(&expirationEventCount); err != nil {
		t.Fatalf("count invitation expiration events: %v", err)
	}
	if expiredStatus != "expired" || expirationEventCount != 1 {
		t.Fatalf(
			"unexpected expiration transition: status=%s events=%d",
			expiredStatus,
			expirationEventCount,
		)
	}
}

func assertResolvedInvitationAuditTenant(
	t *testing.T,
	ctx context.Context,
	wantTenantID uuid.UUID,
) {
	t.Helper()
	snapshot := requestmeta.SnapshotFromContext(ctx)
	if !snapshot.AuditTenantResolved || snapshot.AuditTenantID != wantTenantID {
		t.Fatalf(
			"invitation acceptance did not retain authoritative audit tenant: got=%#v want=%s",
			snapshot,
			wantTenantID,
		)
	}
}

func TestPostgresMembershipInvitationConcurrentAcceptIsIdempotent(t *testing.T) {
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

	now := time.Now().UTC().Truncate(time.Microsecond)
	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin invitation fixture transaction: %v", err)
	}
	fixture := insertInvitationIntegrationFixture(t, ctx, setup, now)
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit invitation fixture: %v", err)
	}
	defer cleanupInvitationIntegrationFixture(
		t,
		pool,
		fixture.tenantID,
		fixture.admin.User.ID,
		fixture.invitee.User.ID,
	)

	repository := NewPostgresRepository(pool, 20*time.Second, policy.NewEngine())
	service := newInvitationIntegrationService(t, repository, now)
	created, err := service.CreateMembershipInvitation(
		ctx,
		fixture.admin,
		fixture.tenantID,
		CreateMembershipInvitationInput{
			Email:        fixture.invitee.User.Email,
			IntendedRole: "student",
		},
	)
	if err != nil {
		t.Fatalf("create concurrent invitation: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var waitGroup sync.WaitGroup
	for attempt := 0; attempt < 2; attempt++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			_, acceptErr := service.AcceptMembershipInvitation(
				ctx,
				fixture.invitee,
				created.Token,
			)
			results <- acceptErr
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	for acceptErr := range results {
		if acceptErr != nil {
			t.Fatalf("concurrent acceptance must converge idempotently: %v", acceptErr)
		}
	}
	var membershipCount, acceptanceEventCount int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.memberships
WHERE tenant_id = $1 AND user_id = $2`,
		fixture.tenantID,
		fixture.invitee.User.ID,
	).Scan(&membershipCount); err != nil {
		t.Fatalf("count concurrent membership: %v", err)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*)
FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND event_type = 'membership.invitation.accepted'`,
		created.Invitation.ID,
	).Scan(&acceptanceEventCount); err != nil {
		t.Fatalf("count concurrent acceptance events: %v", err)
	}
	if membershipCount != 1 || acceptanceEventCount != 1 {
		t.Fatalf(
			"concurrent accept duplicated state: memberships=%d events=%d",
			membershipCount,
			acceptanceEventCount,
		)
	}
}

type invitationIntegrationFixture struct {
	unique     string
	tenantID   uuid.UUID
	tenantName string
	admin      Principal
	invitee    Principal
}

func insertInvitationIntegrationFixture(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	now time.Time,
) invitationIntegrationFixture {
	t.Helper()

	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	tenantID := uuid.New()
	adminID := uuid.New()
	inviteeID := uuid.New()
	inviteeIdentityID := uuid.New()
	inviteeSessionID := uuid.New()
	tenantName := "Invitation Academy " + unique[:8]
	adminEmail := "admin-" + unique + "@example.test"
	inviteeEmail := "invitee-" + unique + "@example.test"

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name)
VALUES
    ($1, $2, 'Invitation Admin'),
    ($3, $4, 'Invitation Recipient')`,
		adminID,
		adminEmail,
		inviteeID,
		inviteeEmail,
	); err != nil {
		t.Fatalf("insert invitation users: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, $3)`,
		tenantID,
		"invite-"+unique,
		tenantName,
	); err != nil {
		t.Fatalf("insert invitation tenant: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (
    tenant_id, user_id, role, status, joined_at
)
VALUES ($1, $2, 'org_admin', 'active', $3)`,
		tenantID,
		adminID,
		now.Add(-time.Hour),
	); err != nil {
		t.Fatalf("insert invitation administrator membership: %v", err)
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
VALUES ($1, $2, 'https://invitation.identity.test', $3, $4, true, $5, 'active')`,
		inviteeIdentityID,
		inviteeID,
		"subject-"+unique,
		inviteeEmail,
		now.Add(-time.Minute),
	); err != nil {
		t.Fatalf("insert invitation identity: %v", err)
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
VALUES ($1, $2, $3, NULL, $4, $5, $6, $6, $7, $8, $9)`,
		inviteeSessionID,
		inviteeID,
		inviteeIdentityID,
		[]byte(unique),
		[]byte(strings.ToUpper(unique)),
		now.Add(-time.Hour),
		now.Add(8*time.Hour),
		now.Add(24*time.Hour),
		now.Add(-time.Minute),
	); err != nil {
		t.Fatalf("insert invitation session: %v", err)
	}

	activeTenant := Tenant{
		ID:       tenantID,
		Name:     tenantName,
		Status:   "active",
		Role:     "org_admin",
		IsActive: true,
	}
	return invitationIntegrationFixture{
		unique:     unique,
		tenantID:   tenantID,
		tenantName: tenantName,
		admin: Principal{
			SessionID:       uuid.New(),
			ContextVersion:  1,
			AuthenticatedAt: now.Add(-time.Minute),
			User: User{
				ID:    adminID,
				Email: adminEmail,
			},
			ActiveTenant: &activeTenant,
			Memberships:  []Tenant{activeTenant},
		},
		invitee: Principal{
			SessionID:       inviteeSessionID,
			ContextVersion:  1,
			IdentityID:      inviteeIdentityID,
			AuthenticatedAt: now.Add(-time.Minute),
			User: User{
				ID:    inviteeID,
				Email: inviteeEmail,
			},
			Memberships: []Tenant{},
			Permissions: []string{},
		},
	}
}

func newInvitationIntegrationService(
	t *testing.T,
	repository Repository,
	now time.Time,
) *Service {
	t.Helper()

	crypto, err := NewCrypto(bytes.Repeat([]byte{0x63}, 32))
	if err != nil {
		t.Fatalf("create invitation crypto: %v", err)
	}
	service, err := NewService(
		repository,
		&fakeProvider{},
		crypto,
		policy.NewEngine(),
		ServiceConfig{
			FlowTTL:                 10 * time.Minute,
			SessionTTL:              8 * time.Hour,
			SessionAbsoluteTTL:      24 * time.Hour,
			MembershipInvitationTTL: 7 * 24 * time.Hour,
		},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("create invitation service: %v", err)
	}
	return service
}

func assertInvitationOutboxRedacted(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	invitationID uuid.UUID,
	rawToken string,
) {
	t.Helper()

	rows, err := transaction.Query(
		ctx,
		`SELECT payload::text
FROM tutorhub.outbox_events
WHERE aggregate_id = $1 AND aggregate_type = 'membership_invitation'`,
		invitationID,
	)
	if err != nil {
		t.Fatalf("read invitation outbox payloads: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan invitation outbox payload: %v", err)
		}
		count++
		lowerPayload := strings.ToLower(payload)
		if strings.Contains(payload, rawToken) ||
			strings.Contains(lowerPayload, "token") ||
			strings.Contains(lowerPayload, "email") ||
			strings.Contains(lowerPayload, "session") {
			t.Fatalf("sensitive invitation data reached outbox payload: %s", payload)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate invitation outbox payloads: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected create and accept outbox events, got %d", count)
	}
}

func cleanupInvitationIntegrationFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	userIDs ...uuid.UUID,
) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var retainedAudit bool
	if err := pool.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM tutorhub.audit_events WHERE tenant_id = $1)`,
		tenantID,
	).Scan(&retainedAudit); err != nil {
		t.Errorf("inspect retained invitation audit fixture: %v", err)
		return
	}
	if retainedAudit {
		return
	}
	if _, err := pool.Exec(
		ctx,
		`DELETE FROM tutorhub.tenants WHERE id = $1`,
		tenantID,
	); err != nil {
		t.Errorf("clean up invitation tenant: %v", err)
		return
	}
	for _, userID := range userIDs {
		if _, err := pool.Exec(
			ctx,
			`DELETE FROM tutorhub.users WHERE id = $1`,
			userID,
		); err != nil {
			t.Errorf("clean up invitation user %s: %v", userID, err)
		}
	}
}
