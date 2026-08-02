//go:build integration

package calendar

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
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestPostgresCalendarProjectionTenantIsolationPaginationAndPreferenceCAS(t *testing.T) {
	migrationURL := requireCalendarEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireCalendarEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply calendar migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read calendar migration version: %v", err)
	}
	if version.Number < 17 || version.Dirty {
		t.Fatalf("unexpected calendar migration version: %+v", version)
	}

	migrationPool := openCalendarPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	runtimePool := openCalendarPool(t, ctx, poolURL)
	defer runtimePool.Close()
	fixture := seedCalendarFixture(t, ctx, migrationPool)
	defer cleanupCalendarFixture(t, migrationPool, fixture)
	repository, err := NewPostgresRepository(runtimePool, 15*time.Second, policy.NewEngine())
	if err != nil {
		t.Fatalf("create calendar repository: %v", err)
	}
	now := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	service, err := NewService(repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("create calendar service: %v", err)
	}
	listInput := ListInput{
		From: "2026-08-01T00:00:00Z", To: "2026-08-03T00:00:00Z",
		ViewerTimezone: "Asia/Ho_Chi_Minh", Limit: 1,
	}
	teacherScope := mustCalendarScope(t, fixture.tenantID, fixture.teacherID)

	firstPage, err := service.ListItems(ctx, teacherScope, listInput)
	if err != nil {
		t.Fatalf("list first calendar page: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.Items[0].SourceID != fixture.firstSessionID ||
		firstPage.NextCursor == "" || !firstPage.Items[0].ViewerCapabilities.CanEdit {
		t.Fatalf("unexpected first calendar page: %+v", firstPage)
	}
	listInput.Cursor = firstPage.NextCursor
	secondPage, err := service.ListItems(ctx, teacherScope, listInput)
	if err != nil {
		t.Fatalf("list second calendar page: %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].SourceID != fixture.secondSessionID ||
		secondPage.NextCursor != "" {
		t.Fatalf("unexpected second calendar page: %+v", secondPage)
	}

	studentScope := mustCalendarScope(t, fixture.tenantID, fixture.studentID)
	studentInput := listInput
	studentInput.Cursor = ""
	studentInput.Limit = 500
	studentPage, err := service.ListItems(ctx, studentScope, studentInput)
	if err != nil {
		t.Fatalf("list student calendar: %v", err)
	}
	if len(studentPage.Items) != 1 || studentPage.Items[0].ClassID != fixture.enrolledClassID ||
		studentPage.Items[0].ViewerCapabilities.CanEdit {
		t.Fatalf("student projection crossed class authorization: %+v", studentPage.Items)
	}

	studentInput.Cursor = firstPage.NextCursor
	if _, err := service.ListItems(ctx, studentScope, studentInput); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("cross-actor cursor error=%v, want invalid cursor", err)
	}
	foreignPage, err := service.ListItems(ctx, mustCalendarScope(
		t, fixture.otherTenantID, fixture.otherTeacherID,
	), ListInput{
		From: "2026-08-01T00:00:00Z", To: "2026-08-03T00:00:00Z",
		ViewerTimezone: "UTC", Limit: 500,
	})
	if err != nil {
		t.Fatalf("list foreign tenant's own calendar: %v", err)
	}
	if len(foreignPage.Items) != 1 ||
		foreignPage.Items[0].SourceID != fixture.foreignSessionID {
		t.Fatalf("foreign calendar leaked or omitted tenant items: %+v", foreignPage.Items)
	}
	if _, err := service.ListItems(ctx, tenancy.Context{
		TenantID: fixture.tenantID, ActorID: fixture.otherTeacherID,
	}, ListInput{
		From: "2026-08-01T00:00:00Z", To: "2026-08-03T00:00:00Z",
		ViewerTimezone: "UTC",
	}); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("cross-tenant actor error=%v, want access denied", err)
	}

	if _, err := service.ListItems(ctx, teacherScope, ListInput{
		From: "2026-01-01T00:00:00Z", To: "2027-01-03T00:00:00Z",
		ViewerTimezone: "UTC",
	}); !errors.Is(err, ErrInvalidRange) {
		t.Fatalf("unbounded calendar range error=%v, want invalid range", err)
	}

	preference, err := service.GetPreference(ctx, studentScope)
	if err != nil || preference.Version != 0 || preference.ViewerTimezone != "Asia/Ho_Chi_Minh" {
		t.Fatalf("unexpected default calendar preference: %+v / %v", preference, err)
	}
	preferenceInput := UpdatePreferenceInput{
		ViewerTimezone: "Asia/Ho_Chi_Minh", Locale: "vi-VN", TimeFormat: "24h",
		WeekStart: "monday", DefaultView: "work_week", Density: "compact",
		TimeScaleMinutes: 15, ExpectedVersion: 0,
	}
	created, err := service.UpdatePreference(ctx, studentScope, preferenceInput)
	if err != nil || created.Version != 1 || created.DefaultView != "work_week" {
		t.Fatalf("create calendar preference: %+v / %v", created, err)
	}
	if _, err := service.UpdatePreference(ctx, studentScope, preferenceInput); !errors.Is(
		err, ErrConflict,
	) {
		t.Fatalf("stale calendar preference error=%v, want conflict", err)
	}
	preferenceInput.ExpectedVersion = 1
	preferenceInput.DefaultView = "month"
	updated, err := service.UpdatePreference(ctx, studentScope, preferenceInput)
	if err != nil || updated.Version != 2 || updated.DefaultView != "month" {
		t.Fatalf("update calendar preference: %+v / %v", updated, err)
	}
}

type calendarFixture struct {
	tenantID         uuid.UUID
	teacherID        uuid.UUID
	studentID        uuid.UUID
	enrolledClassID  uuid.UUID
	otherClassID     uuid.UUID
	firstSessionID   uuid.UUID
	secondSessionID  uuid.UUID
	foreignSessionID uuid.UUID
	otherTenantID    uuid.UUID
	otherTeacherID   uuid.UUID
	userIDs          []uuid.UUID
}

func seedCalendarFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) calendarFixture {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin calendar fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	fixture := calendarFixture{
		tenantID: uuid.New(), teacherID: uuid.New(), studentID: uuid.New(),
		enrolledClassID: uuid.New(), otherClassID: uuid.New(),
		firstSessionID:   uuid.New(),
		secondSessionID:  uuid.New(),
		foreignSessionID: uuid.New(),
		otherTenantID:    uuid.New(), otherTeacherID: uuid.New(),
	}
	if fixture.firstSessionID.String() > fixture.secondSessionID.String() {
		fixture.firstSessionID, fixture.secondSessionID =
			fixture.secondSessionID, fixture.firstSessionID
	}
	fixture.userIDs = []uuid.UUID{fixture.teacherID, fixture.studentID, fixture.otherTeacherID}
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	insertUser := func(id uuid.UUID, label string) {
		t.Helper()
		if _, insertErr := transaction.Exec(ctx, `
INSERT INTO tutorhub.users (id, email, display_name, locale, timezone)
VALUES ($1, $2, $3, 'vi', 'Asia/Ho_Chi_Minh')`,
			id, fmt.Sprintf("calendar-%s-%s@example.test", label, unique), "Calendar "+label,
		); insertErr != nil {
			t.Fatalf("insert calendar user %s: %v", label, insertErr)
		}
	}
	insertUser(fixture.teacherID, "teacher")
	insertUser(fixture.studentID, "student")
	insertUser(fixture.otherTeacherID, "foreign")
	insertTenant := func(id uuid.UUID, label string) {
		t.Helper()
		if _, insertErr := transaction.Exec(ctx, `
INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, $3)`, id, "calendar-"+label+"-"+unique, "Calendar "+label); insertErr != nil {
			t.Fatalf("insert calendar tenant %s: %v", label, insertErr)
		}
	}
	insertTenant(fixture.tenantID, "primary")
	insertTenant(fixture.otherTenantID, "foreign")
	insertMembership := func(tenantID, userID uuid.UUID, role string) {
		t.Helper()
		if _, insertErr := transaction.Exec(ctx, `
INSERT INTO tutorhub.memberships (tenant_id, user_id, role, status, joined_at)
VALUES ($1, $2, $3, 'active', now())`, tenantID, userID, role); insertErr != nil {
			t.Fatalf("insert calendar membership: %v", insertErr)
		}
	}
	insertMembership(fixture.tenantID, fixture.teacherID, "teacher")
	insertMembership(fixture.tenantID, fixture.studentID, "student")
	insertMembership(fixture.otherTenantID, fixture.otherTeacherID, "teacher")
	insertClass := func(id, tenantID, ownerID uuid.UUID, code, title string) {
		t.Helper()
		if _, insertErr := transaction.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
) VALUES ($1, $2, $3, $4, $5, 'Asia/Ho_Chi_Minh', 'active')`,
			id, tenantID, ownerID, code, title,
		); insertErr != nil {
			t.Fatalf("insert calendar class %s: %v", code, insertErr)
		}
	}
	insertClass(fixture.enrolledClassID, fixture.tenantID, fixture.teacherID, "CAL101", "Calendar 101")
	insertClass(fixture.otherClassID, fixture.tenantID, fixture.teacherID, "CAL102", "Calendar 102")
	foreignClassID := uuid.New()
	insertClass(foreignClassID, fixture.otherTenantID, fixture.otherTeacherID, "CAL201", "Foreign calendar")
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
) VALUES ($1, $2, $3, 'student', 'active', $4, now())`,
		fixture.tenantID, fixture.enrolledClassID, fixture.studentID, fixture.teacherID,
	); err != nil {
		t.Fatalf("insert calendar enrollment: %v", err)
	}
	insertSession := func(id, tenantID, classID, actorID uuid.UUID, title string, startsAt time.Time) {
		t.Helper()
		if _, insertErr := transaction.Exec(ctx, `
INSERT INTO tutorhub.class_sessions (
    id, tenant_id, class_id, title, starts_at, ends_at, timezone,
    status, created_by, updated_by
) VALUES ($1, $2, $3, $4, $5, $6, 'Asia/Ho_Chi_Minh', 'scheduled', $7, $7)`,
			id, tenantID, classID, title, startsAt, startsAt.Add(time.Hour), actorID,
		); insertErr != nil {
			t.Fatalf("insert calendar session %s: %v", title, insertErr)
		}
	}
	startsAt := time.Date(2026, time.August, 1, 2, 0, 0, 0, time.UTC)
	insertSession(
		fixture.firstSessionID, fixture.tenantID, fixture.enrolledClassID,
		fixture.teacherID, "First session", startsAt,
	)
	insertSession(
		fixture.secondSessionID, fixture.tenantID, fixture.otherClassID,
		fixture.teacherID, "Second session", startsAt,
	)
	insertSession(
		fixture.foreignSessionID, fixture.otherTenantID, foreignClassID,
		fixture.otherTeacherID, "Foreign session", startsAt,
	)
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit calendar fixture: %v", err)
	}
	return fixture
}

func cleanupCalendarFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture calendarFixture,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// ClassSession keeps a creator-membership FK with RESTRICT semantics. Remove
	// the fixture sessions before deleting the tenant memberships so cleanup
	// remains deterministic on a real PostgreSQL database.
	if _, err := pool.Exec(ctx, `
DELETE FROM tutorhub.class_sessions
WHERE tenant_id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.tenantID, fixture.otherTenantID}); err != nil {
		t.Errorf("delete calendar fixture class sessions: %v", err)
		return
	}
	if _, err := pool.Exec(ctx, `
DELETE FROM tutorhub.class_enrollments
WHERE tenant_id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.tenantID, fixture.otherTenantID}); err != nil {
		t.Errorf("delete calendar fixture enrollments: %v", err)
		return
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.tenants WHERE id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.tenantID, fixture.otherTenantID}); err != nil {
		t.Errorf("delete calendar fixture tenants: %v", err)
		return
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.users WHERE id = ANY($1::uuid[])`,
		fixture.userIDs); err != nil {
		t.Errorf("delete calendar fixture users: %v", err)
	}
}

func openCalendarPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create calendar integration pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping calendar integration database: %v", err)
	}
	return pool
}

func requireCalendarEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for calendar integration tests", key)
	}
	return value
}

func mustCalendarScope(t *testing.T, tenantID, actorID uuid.UUID) tenancy.Context {
	t.Helper()
	scope, err := tenancy.New(tenantID, actorID)
	if err != nil {
		t.Fatalf("create calendar scope: %v", err)
	}
	return scope
}
