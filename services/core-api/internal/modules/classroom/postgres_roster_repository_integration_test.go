//go:build integration

package classroom

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresRosterScopePaginationHierarchyAndProjection(t *testing.T) {
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
	if version.Number < 10 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create roster integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin roster fixture: %v", err)
	}
	defer func() {
		_ = setup.Rollback(context.Background())
	}()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "roster")
	coTeacherID := seedTenantMember(t, ctx, setup, tenantID, "roster-co", "student")
	firstID := seedTenantMember(t, ctx, setup, tenantID, "roster-first", "student")
	secondID := seedTenantMember(t, ctx, setup, tenantID, "roster-second", "student")
	literalID := seedTenantMember(t, ctx, setup, tenantID, "roster-literal", "student")
	suspendedID := seedTenantMember(t, ctx, setup, tenantID, "roster-suspended", "student")
	otherClassUserID := seedTenantMember(t, ctx, setup, tenantID, "roster-other-class", "student")
	otherTenantID, otherOwnerID := seedTenantOwner(t, ctx, setup, "roster-other-tenant")
	allTenantUsers := []uuid.UUID{
		ownerID, coTeacherID, firstID, secondID, literalID, suspendedID, otherClassUserID,
	}
	classID := uuid.New()
	otherClassID := uuid.New()
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := setup.Exec(
		ctx,
		`UPDATE tutorhub.users
SET display_name = CASE id
    WHEN $1 THEN 'Co Teacher'
    WHEN $2 THEN 'Same Name'
    WHEN $3 THEN 'Same Name'
    WHEN $4 THEN 'Literal %_ Member'
    WHEN $5 THEN 'Đặng An'
    ELSE display_name
END
WHERE id = ANY($6::uuid[])`,
		coTeacherID,
		firstID,
		secondID,
		literalID,
		suspendedID,
		allTenantUsers,
	); err != nil {
		t.Fatalf("set roster fixture display names: %v", err)
	}
	if _, err := setup.Exec(
		ctx,
		`INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES
    ($1, $2, $3, $4, 'Roster integration class', 'Asia/Ho_Chi_Minh', 'active'),
    ($5, $2, $3, $6, 'Other roster class', 'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		"RO"+strings.ToUpper(uuid.NewString()[:8]),
		otherClassID,
		"RX"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert roster classes: %v", err)
	}
	insertActive := func(userID uuid.UUID, role policy.ClassRole, targetClassID uuid.UUID) {
		t.Helper()
		if _, insertErr := setup.Exec(
			ctx,
			`INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by,
    joined_at, created_at, updated_at
)
VALUES ($1, $2, $3, $4, 'active', $5, $6, $6, $6)`,
			tenantID,
			targetClassID,
			userID,
			role,
			ownerID,
			now,
		); insertErr != nil {
			t.Fatalf("insert active roster enrollment: %v", insertErr)
		}
	}
	// A legacy owner enrollment proves that the implicit owner projection is
	// pinned once and excluded from enrollment pagination.
	insertActive(ownerID, policy.ClassRoleStudent, classID)
	insertActive(coTeacherID, policy.ClassRoleCoTeacher, classID)
	insertActive(firstID, policy.ClassRoleStudent, classID)
	insertActive(secondID, policy.ClassRoleStudent, classID)
	insertActive(literalID, policy.ClassRoleStudent, classID)
	insertActive(otherClassUserID, policy.ClassRoleStudent, otherClassID)
	if _, err := setup.Exec(
		ctx,
		`INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by,
    joined_at, suspended_at, created_at, updated_at
)
VALUES ($1, $2, $3, 'student', 'suspended', $4, $5, $6, $5, $6)`,
		tenantID,
		classID,
		suspendedID,
		ownerID,
		now,
		now.Add(time.Second),
	); err != nil {
		t.Fatalf("insert suspended roster enrollment: %v", err)
	}
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit roster fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, allTenantUsers...)
	defer cleanupClassIntegrationFixture(t, pool, otherTenantID, otherOwnerID)

	repository := NewPostgresRepository(pool, 30*time.Second, policy.NewEngine())
	ownerContext := mustEnrollmentTenantContext(t, tenantID, ownerID)
	firstPage, err := repository.ListRoster(ctx, ownerContext, classID, ListRosterParams{
		Limit: 2,
	})
	if err != nil {
		t.Fatalf("list first roster page: %v", err)
	}
	if firstPage.Owner.User.ID != ownerID || len(firstPage.Items) != 2 || !firstPage.HasMore {
		t.Fatalf("unexpected first roster page: %+v", firstPage)
	}
	for _, member := range firstPage.Items {
		if member.User.ID == ownerID {
			t.Fatal("implicit owner was duplicated in enrollment pagination")
		}
	}
	secondPage, err := repository.ListRoster(ctx, ownerContext, classID, ListRosterParams{
		Limit: 100, After: &RosterCursor{UserID: firstPage.Items[1].User.ID},
	})
	if err != nil {
		t.Fatalf("list second roster page: %v", err)
	}
	seen := make(map[uuid.UUID]struct{})
	for _, member := range append(firstPage.Items, secondPage.Items...) {
		if _, duplicate := seen[member.User.ID]; duplicate {
			t.Fatalf("roster pagination duplicated user %s", member.User.ID)
		}
		seen[member.User.ID] = struct{}{}
	}
	wantRosterUsers := []uuid.UUID{coTeacherID, firstID, secondID, literalID, suspendedID}
	if len(seen) != len(wantRosterUsers) {
		t.Fatalf("roster pagination lost items: seen=%v", seen)
	}
	for _, userID := range wantRosterUsers {
		if _, ok := seen[userID]; !ok {
			t.Fatalf("roster pagination lost user %s", userID)
		}
	}

	literal, err := repository.ListRoster(ctx, ownerContext, classID, ListRosterParams{
		Search: "%_", Limit: 25,
	})
	if err != nil || len(literal.Items) != 1 || literal.Items[0].User.ID != literalID {
		t.Fatalf("literal wildcard search: page=%+v error=%v", literal, err)
	}
	unicodePage, err := repository.ListRoster(ctx, ownerContext, classID, ListRosterParams{
		Search: "đặng", Limit: 25,
	})
	if err != nil || len(unicodePage.Items) != 1 || unicodePage.Items[0].User.ID != suspendedID {
		t.Fatalf("unicode roster search: page=%+v error=%v", unicodePage, err)
	}
	suspendedStatus := EnrollmentStatusSuspended
	suspendedPage, err := repository.ListRoster(ctx, ownerContext, classID, ListRosterParams{
		Status: &suspendedStatus, Limit: 25,
	})
	if err != nil || len(suspendedPage.Items) != 1 ||
		suspendedPage.Items[0].User.ID != suspendedID {
		t.Fatalf("status-filtered roster: page=%+v error=%v", suspendedPage, err)
	}

	roleChanged, err := repository.UpdateRosterRole(
		ctx,
		ownerContext,
		classID,
		firstID,
		UpdateRosterRoleParams{
			ClassRole: policy.ClassRoleTeachingAssistant,
			ChangedAt: now.Add(2 * time.Second),
			Source:    "roster_single",
		},
	)
	if err != nil || !roleChanged.Changed ||
		roleChanged.Enrollment.ClassRole != policy.ClassRoleTeachingAssistant {
		t.Fatalf("owner role update: result=%+v error=%v", roleChanged, err)
	}
	var payload json.RawMessage
	if err := pool.QueryRow(
		ctx,
		`SELECT payload FROM tutorhub.outbox_events
WHERE tenant_id = $1 AND aggregate_id = $2
  AND event_type = 'class.enrollment.role_changed'
ORDER BY occurred_at DESC LIMIT 1`,
		tenantID,
		roleChanged.Enrollment.ID,
	).Scan(&payload); err != nil {
		t.Fatalf("read role-changed outbox event: %v", err)
	}
	payloadText := string(payload)
	if !strings.Contains(payloadText, `"previous_class_role": "student"`) ||
		!strings.Contains(payloadText, `"class_role": "teaching_assistant"`) ||
		strings.Contains(payloadText, "email") || strings.Contains(payloadText, "display_name") {
		t.Fatalf("unexpected role-changed payload: %s", payloadText)
	}
	var auditMetadata json.RawMessage
	if err := pool.QueryRow(
		ctx,
		`SELECT metadata FROM tutorhub.audit_events
WHERE tenant_id = $1 AND resource_id = $2
  AND action = 'class.enrollment.update_role'
ORDER BY occurred_at DESC LIMIT 1`,
		tenantID,
		roleChanged.Enrollment.ID,
	).Scan(&auditMetadata); err != nil {
		t.Fatalf("read role-changed audit event: %v", err)
	}
	if !strings.Contains(string(auditMetadata), `"target_user_id": "`+firstID.String()+`"`) {
		t.Fatalf("role-changed audit metadata lost stable target: %s", auditMetadata)
	}

	classService, err := NewService(repository, policy.NewEngine())
	if err != nil {
		t.Fatalf("create class projection service: %v", err)
	}
	projected, err := classService.Get(ctx, AccessContext{
		TenantID: tenantID, ActorID: firstID, MembershipActive: true,
		OrganizationRoles: []policy.OrganizationRole{policy.OrganizationRoleStudent},
	}, classID)
	if err != nil || projected.ViewerAccess.ClassRole == nil ||
		*projected.ViewerAccess.ClassRole != policy.ClassRoleTeachingAssistant {
		t.Fatalf("class API did not immediately project the new role: class=%+v error=%v", projected, err)
	}

	coTeacherContext := mustEnrollmentTenantContext(t, tenantID, coTeacherID)
	_, err = repository.UpdateRosterRole(
		ctx,
		coTeacherContext,
		classID,
		firstID,
		UpdateRosterRoleParams{
			ClassRole: policy.ClassRoleCoTeacher,
			ChangedAt: now.Add(3 * time.Second),
			Source:    "roster_single",
		},
	)
	if !errors.Is(err, ErrEnrollmentConflict) {
		t.Fatalf("co-teacher elevated a peer to co-teacher: %v", err)
	}
	_, err = repository.UpdateRosterRole(
		ctx,
		ownerContext,
		classID,
		otherClassUserID,
		UpdateRosterRoleParams{
			ClassRole: policy.ClassRoleTeachingAssistant,
			ChangedAt: now.Add(3 * time.Second),
			Source:    "roster_single",
		},
	)
	if !errors.Is(err, ErrEnrollmentNotFound) {
		t.Fatalf("cross-class role mutation returned %v", err)
	}
	_, err = repository.UpdateRosterRole(
		ctx,
		mustEnrollmentTenantContext(t, otherTenantID, otherOwnerID),
		classID,
		firstID,
		UpdateRosterRoleParams{
			ClassRole: policy.ClassRoleStudent,
			ChangedAt: now.Add(3 * time.Second),
			Source:    "roster_single",
		},
	)
	if !errors.Is(err, ErrClassNotFound) && !errors.Is(err, ErrEnrollmentAccessDenied) {
		t.Fatalf("cross-tenant role mutation returned %v", err)
	}

	if _, err := pool.Exec(
		ctx,
		`UPDATE tutorhub.classes
SET status = 'archived', archived_from_status = 'active',
    archived_at = $3, updated_at = $3, version = version + 1
WHERE tenant_id = $1 AND id = $2`,
		tenantID,
		classID,
		now.Add(4*time.Second),
	); err != nil {
		t.Fatalf("archive roster class: %v", err)
	}
	archivedPage, err := repository.ListRoster(ctx, ownerContext, classID, ListRosterParams{
		Limit: 100,
	})
	if err != nil || len(archivedPage.Items) != len(wantRosterUsers) {
		t.Fatalf("read archived roster: page=%+v error=%v", archivedPage, err)
	}
	for _, member := range archivedPage.Items {
		if member.Actions.CanRemove || member.Actions.CanSuspend ||
			len(member.Actions.AssignableRoles) != 0 {
			t.Fatalf("archived roster exposed a mutation: %+v", member)
		}
	}
	_, err = repository.UpdateRosterRole(
		ctx,
		ownerContext,
		classID,
		secondID,
		UpdateRosterRoleParams{
			ClassRole: policy.ClassRoleTeachingAssistant,
			ChangedAt: now.Add(5 * time.Second),
			Source:    "roster_single",
		},
	)
	if !errors.Is(err, ErrEnrollmentConflict) {
		t.Fatalf("archived roster role mutation returned %v", err)
	}

	// Keep deterministic evidence that same-name members are ordered by UUID.
	sameNameOrder := make([]string, 0, 2)
	for _, member := range append(firstPage.Items, secondPage.Items...) {
		if member.User.ID == firstID || member.User.ID == secondID {
			sameNameOrder = append(sameNameOrder, member.User.ID.String())
		}
	}
	wantSameNameOrder := []string{firstID.String(), secondID.String()}
	sort.Strings(wantSameNameOrder)
	if len(sameNameOrder) != 2 || sameNameOrder[0] != wantSameNameOrder[0] ||
		sameNameOrder[1] != wantSameNameOrder[1] {
		t.Fatalf("same-name UUID tiebreak was unstable: got=%v want=%v", sameNameOrder, wantSameNameOrder)
	}
}
