//go:build integration

package securitysuite

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/classroom"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	securityQueryTimeout   = 15 * time.Second
	securitySessionPurpose = "session-token"
	securityCSRFPurpose    = "csrf-token"
)

type securityActor struct {
	ID           uuid.UUID
	Email        string
	TenantID     uuid.UUID
	SessionToken string
	CSRFToken    string
	Principal    identity.Principal
	Access       classroom.AccessContext
}

type securityFixture struct {
	tx     pgx.Tx
	pool   *pgxpool.Pool
	ctx    context.Context
	cancel context.CancelFunc
	now    time.Time

	tenantA uuid.UUID
	tenantB uuid.UUID
	actors  map[string]*securityActor

	classA  classroom.Class
	classB  classroom.Class
	classB2 classroom.Class
	codeB   classroom.ClassInviteCode
	tokenB  string

	policy          *policy.Engine
	crypto          *identity.Crypto
	classService    *classroom.Service
	enrollment      *classroom.EnrollmentService
	identityService *identity.Service
}

type securityOIDCProvider struct{}

func (securityOIDCProvider) AuthorizationURL(state string, nonce string, codeChallenge string) string {
	values := url.Values{}
	values.Set("state", state)
	values.Set("nonce", nonce)
	values.Set("code_challenge", codeChallenge)
	return "https://security-fixture.invalid/authorize?" + values.Encode()
}

func (securityOIDCProvider) ExchangeAndVerify(context.Context, string, string) (identity.ProviderClaims, error) {
	return identity.ProviderClaims{}, errors.New("security fixture does not exchange OIDC codes")
}

func (securityOIDCProvider) EndSessionURL() string { return "https://security-fixture.invalid/logout" }

func newSecurityFixture(t *testing.T) *securityFixture {
	t.Helper()
	migrationURL := securityEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := securityEnvironment(t, "DATABASE_POOL_URL")

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		cancel()
		t.Fatalf("apply integration migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		cancel()
		t.Fatalf("create integration pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		cancel()
		t.Fatalf("ping integration pool: %v", err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		pool.Close()
		cancel()
		t.Fatalf("begin security fixture transaction: %v", err)
	}

	f := &securityFixture{
		tx: tx, pool: pool, ctx: ctx, cancel: cancel,
		// Keep mutation timestamps slightly ahead of database defaults created
		// during fixture setup without coupling this suite to a calendar date.
		now:    time.Now().UTC().Truncate(time.Microsecond).Add(30 * time.Second),
		actors: make(map[string]*securityActor),
	}
	t.Cleanup(f.close)
	f.policy = policy.NewEngine()
	f.crypto, err = identity.NewCrypto(bytes.Repeat([]byte{0x37}, 32))
	if err != nil {
		t.Fatalf("create fixture crypto: %v", err)
	}
	identityRepository := identity.NewPostgresRepository(f.tx, securityQueryTimeout, f.policy)
	f.identityService, err = identity.NewService(
		identityRepository,
		securityOIDCProvider{},
		f.crypto,
		f.policy,
		identity.ServiceConfig{
			FlowTTL:                 10 * time.Minute,
			SessionTTL:              8 * time.Hour,
			SessionAbsoluteTTL:      24 * time.Hour,
			RecentAuthTTL:           10 * time.Minute,
			MembershipInvitationTTL: 7 * 24 * time.Hour,
		},
		func() time.Time { return f.now },
	)
	if err != nil {
		t.Fatalf("create identity service: %v", err)
	}
	classRepository := classroom.NewPostgresRepository(f.tx, securityQueryTimeout, f.policy)
	f.classService, err = classroom.NewService(
		classRepository,
		f.policy,
		classroom.ServiceConfig{RecentAuthTTL: 10 * time.Minute, Clock: func() time.Time { return f.now }},
	)
	if err != nil {
		t.Fatalf("create classroom service: %v", err)
	}
	f.enrollment, err = classroom.NewEnrollmentService(
		classRepository,
		f.classService,
		f.policy,
		f.crypto,
		func() time.Time { return f.now },
	)
	if err != nil {
		t.Fatalf("create enrollment service: %v", err)
	}

	f.seedUsersAndTenants(t)
	f.seedSessions(t)
	f.authenticateActors(t)
	f.seedClassesAndRoster(t)
	return f
}

func (f *securityFixture) close() {
	if f.tx != nil {
		_ = f.tx.Rollback(context.Background())
	}
	if f.pool != nil {
		f.pool.Close()
	}
	if f.cancel != nil {
		f.cancel()
	}
}

func securityEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		if strings.TrimSpace(os.Getenv("CI")) != "" {
			t.Fatalf("%s is required for security integration tests", key)
		}
		t.Skipf("%s is not configured; skipping PostgreSQL security integration test", key)
	}
	return value
}

func (f *securityFixture) seedUsersAndTenants(t *testing.T) {
	t.Helper()
	f.tenantA = uuid.New()
	f.tenantB = uuid.New()
	f.insertTenant(t, f.tenantA, "security-a")
	f.insertTenant(t, f.tenantB, "security-b")

	f.addActor(t, "admin", f.tenantA, "org_admin")
	f.addActor(t, "teacher", f.tenantA, "teacher")
	f.addActor(t, "owner", f.tenantA, "teacher")
	f.addActor(t, "co_teacher", f.tenantA, "student")
	f.addActor(t, "ta", f.tenantA, "student")
	f.addActor(t, "student", f.tenantA, "student")
	f.addActor(t, "guest", f.tenantA, "guest")
	f.addActor(t, "switcher", f.tenantA, "student")
	f.addActor(t, "no_active", uuid.Nil, "")
	f.addActor(t, "owner_b", f.tenantB, "teacher")
	f.addActor(t, "foreign_student_b", f.tenantB, "student")
	// The switcher is deliberately a member of both workspaces; its session
	// starts in tenant A and is rotated to tenant B in the switch test.
	f.insertMembership(t, f.tenantB, f.actors["switcher"].ID, "student")
}

func (f *securityFixture) insertTenant(t *testing.T, id uuid.UUID, suffix string) {
	t.Helper()
	_, err := f.tx.Exec(f.ctx, `
INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, $3)`, id, suffix+"-"+strings.ReplaceAll(uuid.NewString()[:8], "-", ""), "Security "+suffix)
	if err != nil {
		t.Fatalf("insert tenant %s: %v", suffix, err)
	}
}

func (f *securityFixture) addActor(
	t *testing.T,
	name string,
	activeTenant uuid.UUID,
	role string,
) {
	t.Helper()
	id := uuid.New()
	email := "security-" + name + "-" + strings.ReplaceAll(uuid.NewString(), "-", "") + "@example.test"
	_, err := f.tx.Exec(f.ctx, `
INSERT INTO tutorhub.users (id, email, display_name)
VALUES ($1, $2, $3)`, id, email, "Security "+name)
	if err != nil {
		t.Fatalf("insert actor %s: %v", name, err)
	}
	if activeTenant != uuid.Nil {
		f.insertMembership(t, activeTenant, id, role)
	}
	f.actors[name] = &securityActor{ID: id, Email: email, TenantID: activeTenant}
}

func (f *securityFixture) insertMembership(t *testing.T, tenantID, userID uuid.UUID, role string) {
	t.Helper()
	_, err := f.tx.Exec(f.ctx, `
INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, $3, 'active', $4)`, tenantID, userID, role, f.now)
	if err != nil {
		t.Fatalf("insert membership %s/%s: %v", tenantID, userID, err)
	}
}

func (f *securityFixture) seedSessions(t *testing.T) {
	t.Helper()
	for name, actor := range f.actors {
		raw, csrf := f.insertSession(t, actor.ID, actor.TenantID, name)
		actor.SessionToken, actor.CSRFToken = raw, csrf
	}
}

func (f *securityFixture) insertSession(
	t *testing.T,
	userID, tenantID uuid.UUID,
	suffix string,
) (string, string) {
	t.Helper()
	raw := "security-session-" + suffix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	csrf := "security-csrf-" + suffix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	createdAt := f.now.Add(-time.Minute)
	_, err := f.tx.Exec(f.ctx, `
INSERT INTO tutorhub.sessions (
    id, user_id, active_tenant_id, token_hash, csrf_token_hash,
    created_at, last_seen_at, expires_at, absolute_expires_at,
    auth_time, context_version
)
VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8, $6, 1)`,
		uuid.New(), userID, nullableUUID(tenantID),
		f.crypto.Digest(securitySessionPurpose, raw),
		f.crypto.Digest(securityCSRFPurpose, csrf),
		createdAt, f.now.Add(8*time.Hour), f.now.Add(24*time.Hour),
	)
	if err != nil {
		t.Fatalf("insert session for %s: %v", suffix, err)
	}
	return raw, csrf
}

func nullableUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

func (f *securityFixture) authenticateActors(t *testing.T) {
	t.Helper()
	for name, actor := range f.actors {
		principal, err := f.identityService.Authenticate(f.ctx, actor.SessionToken)
		if err != nil {
			t.Fatalf("authenticate actor %s: %v", name, err)
		}
		actor.Principal = principal
		actor.Access = securityAccess(principal)
	}
}

func securityAccess(principal identity.Principal) classroom.AccessContext {
	access := classroom.AccessContext{ActorID: principal.User.ID, AuthenticatedAt: principal.AuthenticatedAt}
	if principal.ActiveTenant == nil {
		return access
	}
	access.TenantID = principal.ActiveTenant.ID
	access.MembershipActive = principal.ActiveTenant.IsActive
	access.OrganizationRoles = []policy.OrganizationRole{policy.OrganizationRole(principal.ActiveTenant.Role)}
	return access
}

func (f *securityFixture) seedClassesAndRoster(t *testing.T) {
	t.Helper()
	ownerA := f.actors["owner"]
	ownerB := f.actors["owner_b"]
	var err error
	f.classA, err = f.classService.Create(f.ctx, ownerA.Access, classroom.CreateClassInput{
		Code: "SEC-A-" + strings.ToUpper(uuid.NewString()[:8]), Title: "Security class A",
		Description: "Tenant A fixture", Timezone: stringPointer("UTC"),
	})
	if err != nil {
		t.Fatalf("create class A: %v", err)
	}
	active := classroom.ClassStatusActive
	f.classA, err = f.classService.Update(f.ctx, ownerA.Access, f.classA.ID, classroom.UpdateClassInput{
		Status: &active, ExpectedVersion: f.classA.Version,
	})
	if err != nil {
		t.Fatalf("activate class A: %v", err)
	}
	f.classB, err = f.classService.Create(f.ctx, ownerB.Access, classroom.CreateClassInput{
		Code: "SEC-B-" + strings.ToUpper(uuid.NewString()[:8]), Title: "Security class B",
		Description: "Tenant B fixture", Timezone: stringPointer("UTC"),
	})
	if err != nil {
		t.Fatalf("create class B: %v", err)
	}
	f.classB, err = f.classService.Update(f.ctx, ownerB.Access, f.classB.ID, classroom.UpdateClassInput{
		Status: &active, ExpectedVersion: f.classB.Version,
	})
	if err != nil {
		t.Fatalf("activate class B: %v", err)
	}
	f.classB2, err = f.classService.Create(f.ctx, ownerB.Access, classroom.CreateClassInput{
		Code: "SEC-B2-" + strings.ToUpper(uuid.NewString()[:8]), Title: "Security class B second page",
		Description: "Tenant B cursor fixture", Timezone: stringPointer("UTC"),
	})
	if err != nil {
		t.Fatalf("create second class B: %v", err)
	}
	f.classB2, err = f.classService.Update(f.ctx, ownerB.Access, f.classB2.ID, classroom.UpdateClassInput{
		Status: &active, ExpectedVersion: f.classB2.Version,
	})
	if err != nil {
		t.Fatalf("activate second class B: %v", err)
	}

	for _, name := range []string{"co_teacher", "ta", "student", "switcher"} {
		if _, err := f.enrollment.DirectEnroll(f.ctx, ownerA.Access, f.classA.ID, classroom.DirectEnrollmentInput{
			MemberEmail: f.actors[name].Email,
		}); err != nil {
			t.Fatalf("enroll %s in class A: %v", name, err)
		}
	}
	for name, role := range map[string]policy.ClassRole{
		"co_teacher": policy.ClassRoleCoTeacher,
		"ta":         policy.ClassRoleTeachingAssistant,
		"student":    policy.ClassRoleStudent,
		"switcher":   policy.ClassRoleStudent,
	} {
		if _, err := f.enrollment.UpdateRosterRole(f.ctx, ownerA.Access, f.classA.ID, f.actors[name].ID, classroom.UpdateRosterRoleInput{ClassRole: role}); err != nil {
			t.Fatalf("set role %s in class A: %v", name, err)
		}
	}
	if _, err := f.enrollment.DirectEnroll(f.ctx, ownerB.Access, f.classB.ID, classroom.DirectEnrollmentInput{
		MemberEmail: f.actors["foreign_student_b"].Email,
	}); err != nil {
		t.Fatalf("enroll foreign student in class B: %v", err)
	}
	if _, err := f.enrollment.DirectEnroll(f.ctx, ownerB.Access, f.classB.ID, classroom.DirectEnrollmentInput{
		MemberEmail: f.actors["switcher"].Email,
	}); err != nil {
		t.Fatalf("enroll workspace switcher in class B: %v", err)
	}
	invite, err := f.enrollment.CreateInviteCode(f.ctx, ownerB.Access, f.classB.ID, classroom.CreateClassInviteCodeInput{
		ExpiresInSeconds: 3600, UsageLimit: 5,
	})
	if err != nil {
		t.Fatalf("create class B invite code: %v", err)
	}
	f.codeB = invite.InviteCode
	f.tokenB = invite.Token
}

func stringPointer(value string) *string { return &value }

func requireSecurityError(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s unexpectedly succeeded", label)
	}
}

func TestSecurityActorResourceMatrix(t *testing.T) {
	f := newSecurityFixture(t)
	cases := []struct {
		name        string
		allowed     bool
		canUpdate   bool
		canArchive  bool
		canTransfer bool
		canManage   bool
		canJoin     bool
		canPublish  bool
		canLeave    bool
	}{
		{name: "anonymous"},
		{name: "no_active"},
		{name: "guest"},
		{name: "student", allowed: true, canJoin: true, canPublish: true, canLeave: true},
		{name: "ta", allowed: true, canJoin: true, canPublish: true, canLeave: true},
		{name: "teacher", allowed: true, canUpdate: true, canManage: true, canJoin: true, canPublish: true},
		{name: "co_teacher", allowed: true, canUpdate: true, canManage: true, canJoin: true, canPublish: true, canLeave: true},
		{name: "owner", allowed: true, canUpdate: true, canArchive: true, canTransfer: true, canManage: true, canJoin: true, canPublish: true},
		{name: "admin", allowed: true, canUpdate: true, canArchive: true, canTransfer: true, canManage: true, canJoin: true, canPublish: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var access classroom.AccessContext
			if tc.name == "anonymous" {
				access = classroom.AccessContext{}
			} else {
				access = f.actors[tc.name].Access
			}
			class, err := f.classService.Get(f.ctx, access, f.classA.ID)
			if !tc.allowed {
				requireSecurityError(t, "class access for "+tc.name, err)
				return
			}
			if err != nil {
				t.Fatalf("class access for %s: %v", tc.name, err)
			}
			got := class.ViewerAccess
			if got.CanUpdateClass != tc.canUpdate || got.CanArchiveClass != tc.canArchive ||
				got.CanTransferOwnership != tc.canTransfer || got.CanManageEnrollments != tc.canManage ||
				got.CanJoinRoom != tc.canJoin || got.CanPublishMedia != tc.canPublish || got.CanLeave != tc.canLeave {
				t.Fatalf("unexpected access projection for %s: %+v", tc.name, got)
			}
		})
	}
}

type securityClassSnapshot struct {
	Owner   uuid.UUID
	Title   string
	Status  classroom.ClassStatus
	Version int64
}

type securityInviteSnapshot struct {
	Status     classroom.ClassInviteCodeStatus
	UsageCount int
	RevokedAt  *time.Time
}

type securityEnrollmentSnapshot struct {
	Status    classroom.EnrollmentStatus
	Role      policy.ClassRole
	UpdatedAt time.Time
}

func (f *securityFixture) classSnapshot(t *testing.T, id uuid.UUID) securityClassSnapshot {
	t.Helper()
	var snapshot securityClassSnapshot
	if err := f.tx.QueryRow(f.ctx, `SELECT owner_user_id, title, status, version FROM tutorhub.classes WHERE id = $1`, id).Scan(
		&snapshot.Owner, &snapshot.Title, &snapshot.Status, &snapshot.Version,
	); err != nil {
		t.Fatalf("snapshot class %s: %v", id, err)
	}
	return snapshot
}

func (f *securityFixture) inviteSnapshot(t *testing.T, id uuid.UUID) securityInviteSnapshot {
	t.Helper()
	var snapshot securityInviteSnapshot
	var status string
	if err := f.tx.QueryRow(f.ctx, `SELECT status, usage_count, revoked_at FROM tutorhub.class_invite_codes WHERE id = $1`, id).Scan(
		&status, &snapshot.UsageCount, &snapshot.RevokedAt,
	); err != nil {
		t.Fatalf("snapshot invite %s: %v", id, err)
	}
	snapshot.Status = classroom.ClassInviteCodeStatus(status)
	return snapshot
}

func (f *securityFixture) enrollmentSnapshot(t *testing.T, classID, userID uuid.UUID) securityEnrollmentSnapshot {
	t.Helper()
	var snapshot securityEnrollmentSnapshot
	var status, role string
	if err := f.tx.QueryRow(f.ctx, `
SELECT status, class_role, updated_at
FROM tutorhub.class_enrollments
WHERE class_id = $1 AND user_id = $2`, classID, userID).Scan(&status, &role, &snapshot.UpdatedAt); err != nil {
		t.Fatalf("snapshot enrollment %s/%s: %v", classID, userID, err)
	}
	snapshot.Status = classroom.EnrollmentStatus(status)
	snapshot.Role = policy.ClassRole(role)
	return snapshot
}

func (f *securityFixture) rowCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := f.tx.QueryRow(f.ctx, query, args...).Scan(&count); err != nil {
		t.Fatalf("count fixture rows: %v", err)
	}
	return count
}

func TestSecurityForeignResourceIDsDoNotMutate(t *testing.T) {
	f := newSecurityFixture(t)
	ownerA := f.actors["owner"].Access
	foreign := f.actors["foreign_student_b"]
	classABefore := f.classSnapshot(t, f.classA.ID)
	classBBefore := f.classSnapshot(t, f.classB.ID)
	inviteBefore := f.inviteSnapshot(t, f.codeB.ID)
	enrollmentBefore := f.enrollmentSnapshot(t, f.classB.ID, foreign.ID)
	classACountBefore := f.rowCount(t, `SELECT count(*) FROM tutorhub.classes WHERE tenant_id = $1`, f.tenantA)
	classBCountBefore := f.rowCount(t, `SELECT count(*) FROM tutorhub.classes WHERE tenant_id = $1`, f.tenantB)
	inviteCountBefore := f.rowCount(t, `SELECT count(*) FROM tutorhub.class_invite_codes WHERE class_id = $1`, f.classB.ID)
	enrollmentCountBefore := f.rowCount(t, `SELECT count(*) FROM tutorhub.class_enrollments WHERE class_id = $1`, f.classB.ID)

	foreignPage, err := f.classService.List(f.ctx, f.actors["owner_b"].Access, classroom.ListClassesInput{Limit: 1})
	if err != nil {
		t.Fatalf("create tenant B pagination cursor: %v", err)
	}
	if foreignPage.NextCursor == "" {
		t.Fatal("tenant B fixture did not produce a pagination cursor")
	}
	if _, err := f.classService.List(f.ctx, ownerA, classroom.ListClassesInput{
		Limit: 1, Cursor: foreignPage.NextCursor,
	}); !errors.Is(err, classroom.ErrInvalidClassCursor) {
		t.Fatalf("tenant B cursor replay in tenant A should fail, got %v", err)
	}

	if _, err := f.classService.Get(f.ctx, ownerA, f.classB.ID); !errors.Is(err, classroom.ErrClassNotFound) {
		t.Fatalf("foreign class get should conceal resource, got %v", err)
	}
	if _, err := f.classService.Update(f.ctx, ownerA, f.classB.ID, classroom.UpdateClassInput{
		Title: stringPointer("must not write"), Status: classStatusPointer(classroom.ClassStatusActive), ExpectedVersion: classBBefore.Version,
	}); err == nil {
		t.Fatal("foreign class update unexpectedly succeeded")
	}
	if _, err := f.classService.Archive(f.ctx, ownerA, f.classB.ID, classBBefore.Version); err == nil {
		t.Fatal("foreign class archive unexpectedly succeeded")
	}
	if _, err := f.classService.Restore(f.ctx, ownerA, f.classB.ID, classBBefore.Version); err == nil {
		t.Fatal("foreign class restore unexpectedly succeeded")
	}
	if _, err := f.classService.TransferOwnership(f.ctx, ownerA, f.classB.ID, classroom.TransferClassOwnershipInput{
		NewOwnerUserID: f.actors["owner_b"].ID, ExpectedVersion: classBBefore.Version,
	}); err == nil {
		t.Fatal("foreign class ownership transfer unexpectedly succeeded")
	}

	if _, err := f.enrollment.DirectEnroll(f.ctx, ownerA, f.classB.ID, classroom.DirectEnrollmentInput{MemberEmail: foreign.Email}); err == nil {
		t.Fatal("foreign class direct enrollment unexpectedly succeeded")
	}
	if _, err := f.enrollment.SuspendEnrollment(f.ctx, ownerA, f.classB.ID, foreign.ID); err == nil {
		t.Fatal("foreign class suspend unexpectedly succeeded")
	}
	if _, err := f.enrollment.RemoveEnrollment(f.ctx, ownerA, f.classB.ID, foreign.ID); err == nil {
		t.Fatal("foreign class remove unexpectedly succeeded")
	}
	if _, err := f.enrollment.ListRoster(f.ctx, ownerA, f.classB.ID, classroom.ListRosterInput{Limit: 25}); err == nil {
		t.Fatal("foreign class roster list unexpectedly succeeded")
	}
	if _, err := f.enrollment.UpdateRosterRole(f.ctx, ownerA, f.classB.ID, foreign.ID, classroom.UpdateRosterRoleInput{ClassRole: policy.ClassRoleStudent}); err == nil {
		t.Fatal("foreign class role update unexpectedly succeeded")
	}
	if result, err := f.enrollment.BulkMutateRoster(f.ctx, ownerA, f.classB.ID, classroom.BulkRosterInput{
		Action: classroom.RosterBulkActionUpdateRole, ClassRole: classRolePointer(policy.ClassRoleStudent), UserIDs: []uuid.UUID{foreign.ID},
	}); err == nil && result.FailedCount == 0 {
		t.Fatal("foreign class bulk roster unexpectedly succeeded")
	}
	if _, err := f.enrollment.ListInviteCodes(f.ctx, ownerA, f.classB.ID); err == nil {
		t.Fatal("foreign class invite list unexpectedly succeeded")
	}
	if _, err := f.enrollment.RevokeInviteCode(f.ctx, ownerA, f.classB.ID, f.codeB.ID); err == nil {
		t.Fatal("foreign class invite revoke unexpectedly succeeded")
	}
	if _, err := f.enrollment.LeaveClass(f.ctx, ownerA, f.classB.ID); err == nil {
		t.Fatal("foreign class leave unexpectedly succeeded")
	}
	if _, err := f.enrollment.JoinByInviteCode(f.ctx, ownerA, f.tokenB); err == nil {
		t.Fatal("invalid/foreign invite join unexpectedly succeeded")
	}

	// The class is in tenant A, but the target user and invite code are from B.
	if _, err := f.enrollment.SuspendEnrollment(f.ctx, ownerA, f.classA.ID, foreign.ID); err == nil {
		t.Fatal("foreign user suspend unexpectedly succeeded")
	}
	if _, err := f.enrollment.RemoveEnrollment(f.ctx, ownerA, f.classA.ID, foreign.ID); err == nil {
		t.Fatal("foreign user remove unexpectedly succeeded")
	}
	if _, err := f.enrollment.UpdateRosterRole(f.ctx, ownerA, f.classA.ID, foreign.ID, classroom.UpdateRosterRoleInput{ClassRole: policy.ClassRoleStudent}); err == nil {
		t.Fatal("foreign user role update unexpectedly succeeded")
	}
	if result, err := f.enrollment.BulkMutateRoster(f.ctx, ownerA, f.classA.ID, classroom.BulkRosterInput{
		Action: classroom.RosterBulkActionUpdateRole, ClassRole: classRolePointer(policy.ClassRoleStudent), UserIDs: []uuid.UUID{foreign.ID},
	}); err == nil && result.FailedCount == 0 {
		t.Fatal("foreign user bulk role update unexpectedly succeeded")
	}
	if _, err := f.enrollment.RevokeInviteCode(f.ctx, ownerA, f.classA.ID, f.codeB.ID); err == nil {
		t.Fatal("foreign invite code revoke unexpectedly succeeded")
	}

	if got := f.classSnapshot(t, f.classA.ID); got != classABefore {
		t.Fatalf("class A mutated during foreign-ID checks: before=%+v after=%+v", classABefore, got)
	}
	if got := f.classSnapshot(t, f.classB.ID); got != classBBefore {
		t.Fatalf("class B mutated during foreign-ID checks: before=%+v after=%+v", classBBefore, got)
	}
	if got := f.inviteSnapshot(t, f.codeB.ID); !securityInviteSnapshotsEqual(got, inviteBefore) {
		t.Fatalf("invite mutated during foreign-ID checks: before=%+v after=%+v", inviteBefore, got)
	}
	if got := f.enrollmentSnapshot(t, f.classB.ID, foreign.ID); !securityEnrollmentSnapshotsEqual(got, enrollmentBefore) {
		t.Fatalf("enrollment mutated during foreign-ID checks: before=%+v after=%+v", enrollmentBefore, got)
	}
	if got := f.rowCount(t, `SELECT count(*) FROM tutorhub.classes WHERE tenant_id = $1`, f.tenantA); got != classACountBefore {
		t.Fatalf("tenant A class count changed: before=%d after=%d", classACountBefore, got)
	}
	if got := f.rowCount(t, `SELECT count(*) FROM tutorhub.classes WHERE tenant_id = $1`, f.tenantB); got != classBCountBefore {
		t.Fatalf("tenant B class count changed: before=%d after=%d", classBCountBefore, got)
	}
	if got := f.rowCount(t, `SELECT count(*) FROM tutorhub.class_invite_codes WHERE class_id = $1`, f.classB.ID); got != inviteCountBefore {
		t.Fatalf("tenant B invite count changed: before=%d after=%d", inviteCountBefore, got)
	}
	if got := f.rowCount(t, `SELECT count(*) FROM tutorhub.class_enrollments WHERE class_id = $1`, f.classB.ID); got != enrollmentCountBefore {
		t.Fatalf("tenant B enrollment count changed: before=%d after=%d", enrollmentCountBefore, got)
	}
}

func securityInviteSnapshotsEqual(left, right securityInviteSnapshot) bool {
	if left.Status != right.Status || left.UsageCount != right.UsageCount {
		return false
	}
	if left.RevokedAt == nil || right.RevokedAt == nil {
		return left.RevokedAt == nil && right.RevokedAt == nil
	}
	return left.RevokedAt.Equal(*right.RevokedAt)
}

func securityEnrollmentSnapshotsEqual(left, right securityEnrollmentSnapshot) bool {
	return left.Status == right.Status && left.Role == right.Role && left.UpdatedAt.Equal(right.UpdatedAt)
}

func classStatusPointer(value classroom.ClassStatus) *classroom.ClassStatus { return &value }

func classRolePointer(value policy.ClassRole) *policy.ClassRole { return &value }

func TestSecurityStaleMembershipRevokeReloads(t *testing.T) {
	f := newSecurityFixture(t)
	admin := f.actors["admin"]
	if _, err := f.classService.Get(f.ctx, admin.Access, f.classA.ID); err != nil {
		t.Fatalf("admin should initially access class A: %v", err)
	}
	if _, err := f.tx.Exec(f.ctx, `
UPDATE tutorhub.memberships
SET status = 'removed', updated_at = $3
WHERE tenant_id = $1 AND user_id = $2`, f.tenantA, admin.ID, f.now); err != nil {
		t.Fatalf("revoke stale membership: %v", err)
	}
	fresh, err := f.identityService.Authenticate(f.ctx, admin.SessionToken)
	if err != nil {
		t.Fatalf("reload principal after membership revoke: %v", err)
	}
	if fresh.ActiveTenant != nil || len(fresh.Permissions) != 0 {
		t.Fatalf("revoked membership remained active in principal: %+v", fresh)
	}
	access := securityAccess(fresh)
	if _, err := f.classService.Get(f.ctx, access, f.classA.ID); err == nil {
		t.Fatal("stale principal accessed class after membership revoke")
	}
	if _, err := f.enrollment.ListRoster(f.ctx, access, f.classA.ID, classroom.ListRosterInput{Limit: 25}); err == nil {
		t.Fatal("stale principal listed roster after membership revoke")
	}
}

func TestSecurityWorkspaceSwitchRotatesSession(t *testing.T) {
	f := newSecurityFixture(t)
	switcher := f.actors["switcher"]
	oldPrincipal := switcher.Principal
	if _, err := f.classService.Get(f.ctx, switcher.Access, f.classA.ID); err != nil {
		t.Fatalf("switcher should initially access class A: %v", err)
	}
	rotated, err := f.identityService.SwitchActiveTenant(f.ctx, oldPrincipal, f.tenantB)
	if err != nil {
		t.Fatalf("switch active tenant: %v", err)
	}
	if rotated.SessionToken == switcher.SessionToken || rotated.CSRFToken == switcher.CSRFToken {
		t.Fatal("workspace switch did not rotate session credentials")
	}
	if rotated.Principal.ActiveTenant == nil || rotated.Principal.ActiveTenant.ID != f.tenantB {
		t.Fatalf("rotated principal has wrong active tenant: %+v", rotated.Principal.ActiveTenant)
	}
	if rotated.Principal.ContextVersion <= oldPrincipal.ContextVersion {
		t.Fatalf("workspace switch did not advance context version: old=%d new=%d", oldPrincipal.ContextVersion, rotated.Principal.ContextVersion)
	}
	if _, err := f.identityService.Authenticate(f.ctx, switcher.SessionToken); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("old session token should fail after switch, got %v", err)
	}
	if _, err := f.identityService.ValidateCSRF(f.ctx, switcher.SessionToken, switcher.CSRFToken); !errors.Is(err, identity.ErrSessionNotFound) {
		t.Fatalf("old CSRF/session should fail after switch, got %v", err)
	}
	newAccess := securityAccess(rotated.Principal)
	if _, err := f.classService.Get(f.ctx, newAccess, f.classA.ID); !errors.Is(err, classroom.ErrClassNotFound) {
		t.Fatalf("new tenant context accessed exact class from old tenant, got %v", err)
	}
	if _, err := f.classService.Get(f.ctx, newAccess, f.classB.ID); err != nil {
		t.Fatalf("new tenant context should access tenant B class: %v", err)
	}
}
