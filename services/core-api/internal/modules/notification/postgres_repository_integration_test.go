//go:build integration

package notification

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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

var notificationCanaryInsertColumns = []string{
	"tenant_id",
	"recipient_user_id",
	"source_outbox_event_id",
	"effect_key",
	"kind",
	"template_key",
	"context",
	"occurred_at",
}

func TestPostgresCanaryProjectorWorksWithExactWorkerColumnGrant(t *testing.T) {
	migrationURL := requireNotificationEnvironment(t, "DATABASE_MIGRATION_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply notification migrations: %v", err)
	}

	migrationPool := openNotificationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	requireNotificationRoleAdministration(t, ctx, migrationPool)
	roleName := "p304_notification_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	password := createNotificationLoginRole(t, ctx, migrationPool, roleName)
	grantNotificationCanaryColumns(t, ctx, migrationPool, roleName)
	workerPool := openNotificationRolePool(t, ctx, migrationURL, roleName, password)

	var schemaUsage, tableSelect, tableInsert, anyColumnInsert bool
	if err := migrationPool.QueryRow(ctx, `
SELECT has_schema_privilege($1, 'tutorhub', 'USAGE'),
       has_table_privilege($1, 'tutorhub.notifications', 'SELECT'),
       has_table_privilege($1, 'tutorhub.notifications', 'INSERT'),
       has_any_column_privilege($1, 'tutorhub.notifications', 'INSERT')`,
		roleName,
	).Scan(&schemaUsage, &tableSelect, &tableInsert, &anyColumnInsert); err != nil {
		t.Fatalf("inspect notification canary role: %v", err)
	}
	if !schemaUsage || tableSelect || tableInsert || !anyColumnInsert {
		t.Fatalf(
			"unexpected notification canary ACL: schema=%t select=%t table_insert=%t column_insert=%t",
			schemaUsage,
			tableSelect,
			tableInsert,
			anyColumnInsert,
		)
	}

	fixture := seedNotificationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupNotificationFixture(t, migrationPool, fixture) })
	projector, err := NewPostgresCanaryProjector(workerPool, 15*time.Second)
	if err != nil {
		t.Fatalf("create exact-role notification canary projector: %v", err)
	}
	projection := CanaryProjection{
		SourceOutboxEventID: uuid.New(),
		TenantID:            fixture.tenantID,
		RecipientUserID:     fixture.ownerID,
		OccurredAt:          time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC),
	}
	if err := projector.ProjectCanary(ctx, projection); err != nil {
		t.Fatalf("project canary with exact worker role: %v", err)
	}
	if err := projector.ProjectCanary(ctx, projection); err != nil {
		t.Fatalf("redeliver canary with exact worker role: %v", err)
	}

	var rowCount int
	if err := migrationPool.QueryRow(ctx, `
SELECT count(*)
FROM tutorhub.notifications
WHERE source_outbox_event_id = $1
  AND recipient_user_id = $2
  AND effect_key = 'worker_canary'`,
		projection.SourceOutboxEventID,
		projection.RecipientUserID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count exact-role canary rows: %v", err)
	}
	if rowCount != 1 {
		t.Fatalf("exact-role canary rows = %d, want 1", rowCount)
	}
}

func TestPostgresNotificationProjectionFeedReadAndPreferenceLifecycle(t *testing.T) {
	migrationURL := requireNotificationEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireNotificationEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply notification migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read notification migration version: %v", err)
	}
	if version.Number < 16 || version.Dirty {
		t.Fatalf("unexpected notification migration version: %+v", version)
	}

	migrationPool := openNotificationPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	apiPool := openNotificationPool(t, ctx, poolURL)
	defer apiPool.Close()
	fixture := seedNotificationFixture(t, ctx, migrationPool)
	defer cleanupNotificationFixture(t, migrationPool, fixture)

	controls, err := featurecontrol.NewPostgresRepository(
		apiPool,
		15*time.Second,
		policy.NewEngine(),
		featurecontrol.NewDefaultCatalog(),
	)
	if err != nil {
		t.Fatalf("create feature-control enforcer: %v", err)
	}
	repository, err := NewPostgresRepository(apiPool, 15*time.Second, controls)
	if err != nil {
		t.Fatalf("create notification repository: %v", err)
	}
	now := time.Date(2026, 7, 25, 6, 30, 0, 0, time.UTC)
	service, err := NewService(repository, func() time.Time { return now })
	if err != nil {
		t.Fatalf("create notification service: %v", err)
	}
	scope := tenancy.Context{TenantID: fixture.tenantID, ActorID: fixture.ownerID}

	projector, err := NewPostgresCanaryProjector(migrationPool, 15*time.Second)
	if err != nil {
		t.Fatalf("create notification canary projector: %v", err)
	}
	canary := CanaryProjection{
		SourceOutboxEventID: uuid.New(),
		TenantID:            fixture.tenantID,
		RecipientUserID:     fixture.ownerID,
		OccurredAt:          now.Add(-5 * time.Minute),
	}
	if err := projector.ProjectCanary(ctx, canary); err != nil {
		t.Fatalf("project notification canary: %v", err)
	}
	if err := projector.ProjectCanary(ctx, canary); err != nil {
		t.Fatalf("redeliver notification canary: %v", err)
	}
	var canaryRows int
	if err := migrationPool.QueryRow(
		ctx,
		`SELECT count(*) FROM tutorhub.notifications
WHERE source_outbox_event_id = $1 AND recipient_user_id = $2`,
		canary.SourceOutboxEventID,
		fixture.ownerID,
	).Scan(&canaryRows); err != nil {
		t.Fatalf("count deduplicated canary rows: %v", err)
	}
	if canaryRows != 1 {
		t.Fatalf("canary rows = %d, want 1", canaryRows)
	}

	firstID := insertNotificationProjection(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.ownerID,
		"class_session.updated",
		now.Add(-2*time.Minute),
	)
	secondID := insertNotificationProjection(
		t,
		ctx,
		migrationPool,
		fixture.tenantID,
		fixture.ownerID,
		"class_session.cancelled",
		now.Add(-time.Minute),
	)

	firstPage, err := service.List(ctx, scope, ListInput{Limit: 1})
	if err != nil {
		t.Fatalf("list first notification page: %v", err)
	}
	if len(firstPage.Items) != 1 || firstPage.Items[0].ID != secondID ||
		firstPage.NextCursor == "" {
		t.Fatalf("unexpected first notification page: %+v", firstPage)
	}
	secondPage, err := service.List(ctx, scope, ListInput{
		Limit: 1, Cursor: firstPage.NextCursor,
	})
	if err != nil {
		t.Fatalf("list second notification page: %v", err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].ID != firstID ||
		secondPage.NextCursor != "" {
		t.Fatalf("unexpected second notification page: %+v", secondPage)
	}

	memberPage, err := service.List(ctx, tenancy.Context{
		TenantID: fixture.tenantID,
		ActorID:  fixture.memberID,
	}, ListInput{})
	if err != nil {
		t.Fatalf("list member-scoped notification feed: %v", err)
	}
	if len(memberPage.Items) != 0 {
		t.Fatalf("member saw another recipient's notifications: %+v", memberPage.Items)
	}
	if _, err := service.MarkRead(ctx, tenancy.Context{
		TenantID: fixture.otherTenantID,
		ActorID:  fixture.otherOwnerID,
	}, secondID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign notification error = %v, want concealed not found", err)
	}

	unread, err := service.UnreadCount(ctx, scope)
	if err != nil || unread.Count != 2 || unread.IsCapped {
		t.Fatalf("unexpected unread count: %+v / %v", unread, err)
	}
	read, err := service.MarkRead(ctx, scope, secondID)
	if err != nil || read.ReadAt == nil || !read.ReadAt.Equal(now) {
		t.Fatalf("mark notification read = %+v / %v", read, err)
	}
	idempotent, err := service.MarkRead(ctx, scope, secondID)
	if err != nil || idempotent.ReadAt == nil || !idempotent.ReadAt.Equal(now) {
		t.Fatalf("repeat mark notification read = %+v / %v", idempotent, err)
	}
	marked, err := service.MarkAllRead(ctx, scope)
	if err != nil || marked.UpdatedCount != 1 {
		t.Fatalf("mark all notification read = %+v / %v", marked, err)
	}
	repeated, err := service.MarkAllRead(ctx, scope)
	if err != nil || repeated.UpdatedCount != 0 {
		t.Fatalf("repeat mark all notification read = %+v / %v", repeated, err)
	}

	preference, err := service.GetPreference(ctx, scope)
	if err != nil || preference.Version != 0 || !preference.InAppEnabled ||
		preference.EmailEnabled || preference.ReminderOffsetMinutes != 15 {
		t.Fatalf("unexpected virtual notification preference: %+v / %v", preference, err)
	}
	start, end := "22:00", "06:00"
	preference, err = service.PutPreference(ctx, scope, PutPreferenceInput{
		InAppEnabled: true, EmailEnabled: false, ReminderOffsetMinutes: 30,
		QuietHoursEnabled: true, QuietHoursStart: &start, QuietHoursEnd: &end,
		QuietHoursTimezone: "Asia/Ho_Chi_Minh", ExpectedVersion: 0,
	})
	if err != nil || preference.Version != 1 {
		t.Fatalf("insert notification preference = %+v / %v", preference, err)
	}
	if _, err := service.PutPreference(ctx, scope, PutPreferenceInput{
		InAppEnabled: true, ReminderOffsetMinutes: 15,
		QuietHoursTimezone: "Asia/Ho_Chi_Minh", ExpectedVersion: 0,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale notification preference error = %v, want conflict", err)
	}
	preference, err = service.PutPreference(ctx, scope, PutPreferenceInput{
		InAppEnabled: true, ReminderOffsetMinutes: 15,
		QuietHoursTimezone: "Asia/Ho_Chi_Minh", ExpectedVersion: 1,
	})
	if err != nil || preference.Version != 2 || preference.QuietHoursEnabled {
		t.Fatalf("replace notification preference = %+v / %v", preference, err)
	}

	if _, err := migrationPool.Exec(
		ctx,
		`INSERT INTO tutorhub.tenant_feature_overrides (
    tenant_id, feature_key, enabled, updated_by, created_at, updated_at
) VALUES ($1, $2, false, $3, $4, $4)`,
		fixture.tenantID,
		featurecontrol.FeatureInAppNotifications,
		fixture.ownerID,
		now,
	); err != nil {
		t.Fatalf("disable in-app notifications for fixture: %v", err)
	}
	if _, err := service.List(ctx, scope, ListInput{}); !errors.Is(
		err,
		featurecontrol.ErrFeatureDisabled,
	) {
		t.Fatalf("disabled notification feed error = %v, want feature disabled", err)
	}
}

type notificationFixture struct {
	tenantID      uuid.UUID
	ownerID       uuid.UUID
	memberID      uuid.UUID
	otherTenantID uuid.UUID
	otherOwnerID  uuid.UUID
	userIDs       []uuid.UUID
}

func seedNotificationFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) notificationFixture {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin notification fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()
	fixture := notificationFixture{}
	fixture.tenantID, fixture.ownerID = insertNotificationTenantOwner(
		t, ctx, transaction, "primary",
	)
	fixture.memberID = insertNotificationMember(
		t, ctx, transaction, fixture.tenantID, "member",
	)
	fixture.otherTenantID, fixture.otherOwnerID = insertNotificationTenantOwner(
		t, ctx, transaction, "foreign",
	)
	fixture.userIDs = []uuid.UUID{fixture.ownerID, fixture.memberID, fixture.otherOwnerID}
	if err := transaction.Commit(ctx); err != nil {
		t.Fatalf("commit notification fixture: %v", err)
	}
	return fixture
}

func insertNotificationTenantOwner(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	label string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	userID, tenantID := uuid.New(), uuid.New()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name, timezone)
VALUES ($1, $2, $3, 'Asia/Ho_Chi_Minh')`,
		userID,
		fmt.Sprintf("notification-%s-%s@example.test", label, unique),
		"Notification "+label,
	); err != nil {
		t.Fatalf("insert notification user: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenants (id, slug, name)
VALUES ($1, $2, $3)`,
		tenantID,
		"notification-"+unique,
		"Notification "+label,
	); err != nil {
		t.Fatalf("insert notification tenant: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (
    tenant_id, user_id, role, status, joined_at
) VALUES ($1, $2, 'teacher', 'active', now())`,
		tenantID,
		userID,
	); err != nil {
		t.Fatalf("insert notification owner membership: %v", err)
	}
	return tenantID, userID
}

func insertNotificationMember(
	t *testing.T,
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	label string,
) uuid.UUID {
	t.Helper()
	userID := uuid.New()
	unique := strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.users (id, email, display_name)
VALUES ($1, $2, $3)`,
		userID,
		fmt.Sprintf("notification-%s-%s@example.test", label, unique),
		"Notification "+label,
	); err != nil {
		t.Fatalf("insert notification member: %v", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.memberships (
    tenant_id, user_id, role, status, joined_at
) VALUES ($1, $2, 'student', 'active', now())`,
		tenantID,
		userID,
	); err != nil {
		t.Fatalf("insert notification member membership: %v", err)
	}
	return userID
}

func insertNotificationProjection(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	recipientID uuid.UUID,
	kind string,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tutorhub.notifications (
    id, tenant_id, recipient_user_id, source_outbox_event_id,
    effect_key, kind, template_key, context, occurred_at, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $6, '{"label":"fixture"}'::jsonb, $7, $7)`,
		id,
		tenantID,
		recipientID,
		uuid.New(),
		"integration_"+strings.ReplaceAll(uuid.NewString(), "-", ""),
		kind,
		createdAt,
	); err != nil {
		t.Fatalf("insert notification projection: %v", err)
	}
	return id
}

func cleanupNotificationFixture(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture notificationFixture,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := pool.Exec(
		ctx,
		`DELETE FROM tutorhub.tenants WHERE id = ANY($1::uuid[])`,
		[]uuid.UUID{fixture.tenantID, fixture.otherTenantID},
	); err != nil {
		t.Errorf("delete notification fixture tenants: %v", err)
		return
	}
	if _, err := pool.Exec(
		ctx,
		`DELETE FROM tutorhub.users WHERE id = ANY($1::uuid[])`,
		fixture.userIDs,
	); err != nil {
		t.Errorf("delete notification fixture users: %v", err)
	}
}

func openNotificationPool(
	t *testing.T,
	ctx context.Context,
	url string,
) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("create notification integration pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping notification integration database: %v", err)
	}
	return pool
}

func requireNotificationEnvironment(t *testing.T, key string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		t.Fatalf("%s is required for notification integration tests", key)
	}
	return value
}

func requireNotificationRoleAdministration(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()
	var allowed bool
	if err := pool.QueryRow(ctx, `
SELECT rolsuper OR rolcreaterole
FROM pg_catalog.pg_roles
WHERE rolname = current_user`).Scan(&allowed); err != nil {
		t.Fatalf("inspect notification role-administration capability: %v", err)
	}
	if !allowed {
		t.Skip("P3-04 exact-role projection test requires SUPERUSER or CREATEROLE")
	}
}

func createNotificationLoginRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
) string {
	t.Helper()
	identifier := pgx.Identifier{roleName}.Sanitize()
	password := strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := pool.Exec(ctx, `CREATE ROLE `+identifier+`
LOGIN PASSWORD '`+password+`'
NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS`); err != nil {
		if notificationRoleAdministrationUnavailable(err) {
			t.Skip("P3-04 exact-role projection test cannot create a temporary login role")
		}
		t.Fatalf("create temporary notification worker role: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, statement := range []string{
			`DROP OWNED BY ` + identifier,
			`DROP ROLE ` + identifier,
		} {
			if _, err := pool.Exec(cleanupContext, statement); err != nil {
				t.Errorf("clean up temporary notification worker role: %v", err)
				return
			}
		}
	})
	return password
}

func grantNotificationCanaryColumns(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
) {
	t.Helper()
	identifier := pgx.Identifier{roleName}.Sanitize()
	if _, err := pool.Exec(ctx, `GRANT USAGE ON SCHEMA tutorhub TO `+identifier); err != nil {
		t.Fatalf("grant notification worker schema usage: %v", err)
	}
	columns := make([]string, 0, len(notificationCanaryInsertColumns))
	for _, column := range notificationCanaryInsertColumns {
		columns = append(columns, pgx.Identifier{column}.Sanitize())
	}
	if _, err := pool.Exec(ctx, `GRANT INSERT (`+strings.Join(columns, ", ")+`)
ON TABLE tutorhub.notifications TO `+identifier); err != nil {
		t.Fatalf("grant exact notification canary insert columns: %v", err)
	}
}

func openNotificationRolePool(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	roleName string,
	password string,
) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse notification integration database configuration")
	}
	config.ConnConfig.User = roleName
	config.ConnConfig.Password = password
	config.ConnConfig.RuntimeParams["application_name"] = "p3-04-notification-acl-integration"
	delete(config.ConnConfig.RuntimeParams, "role")
	config.MaxConns = 2
	config.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("open exact-role notification integration pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("authenticate exact-role notification integration pool")
	}
	return pool
}

func notificationRoleAdministrationUnavailable(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		(postgresError.Code == "42501" || postgresError.Code == "0A000")
}
