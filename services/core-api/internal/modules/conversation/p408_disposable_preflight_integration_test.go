//go:build integration

package conversation

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	p408DisposableConfirmation      = "I_UNDERSTAND_P4_08_DISPOSABLE_ONLY"
	p408OwnerPreflightConfirmation  = "I_UNDERSTAND_P4_08_OWNER_PREFLIGHT_ONLY"
	p408FinalSnapshotConfirmation   = "I_UNDERSTAND_P4_08_FINAL_SNAPSHOT_READ_ONLY"
	p408SharedPreflightConfirmation = "I_UNDERSTAND_P4_08_SHARED_PREFLIGHT_READ_ONLY"
	p408SharedFinalConfirmation     = "I_UNDERSTAND_P4_08_SHARED_FINAL_SNAPSHOT_READ_ONLY"
)

func TestPostgresP408DisposableOwnerPreflight(t *testing.T) {
	requireP408ReadOnlyConfirmation(
		t,
		"P4_08_OWNER_PREFLIGHT",
		p408OwnerPreflightConfirmation,
	)
	runPostgresP408ReadOnlySnapshot(t, 33, false)
}

func TestPostgresP408DisposableFinalSnapshot(t *testing.T) {
	requireP408ReadOnlyConfirmation(
		t,
		"P4_08_FINAL_SNAPSHOT_CONFIRM",
		p408FinalSnapshotConfirmation,
	)
	runPostgresP408ReadOnlySnapshot(t, 34, true)
}

func TestPostgresP408SharedOwnerPreflight(t *testing.T) {
	requireP408ReadOnlyConfirmation(
		t,
		"P4_08_SHARED_PREFLIGHT_CONFIRM",
		p408SharedPreflightConfirmation,
	)
	runPostgresP408ReadOnlySnapshot(t, 33, false)
}

func TestPostgresP408SharedFinalSnapshot(t *testing.T) {
	requireP408ReadOnlyConfirmation(
		t,
		"P4_08_SHARED_FINAL_CONFIRM",
		p408SharedFinalConfirmation,
	)
	runPostgresP408ReadOnlySnapshot(t, 34, true)
	runPostgresP408SharedZeroSideEffectSnapshot(t)
}

func requireP408WritableIntegrationDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("P4_08_DISPOSABLE_CONFIRM")) == p408DisposableConfirmation {
		for _, name := range []string{
			"P4_08_OWNER_PREFLIGHT",
			"P4_08_FINAL_SNAPSHOT_CONFIRM",
			"P4_08_SHARED_CONFIRM",
			"P4_08_SHARED_PREFLIGHT_CONFIRM",
			"P4_08_SHARED_FINAL_CONFIRM",
		} {
			if strings.TrimSpace(os.Getenv(name)) != "" {
				t.Fatalf("P4-08 disposable write gate refuses confirmation %s", name)
			}
		}
		return
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("CI")), "true") &&
		isP408IsolatedCIDatabase(databaseURL) {
		return
	}
	t.Skip("P4_08_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
}

func isP408IsolatedCIDatabase(databaseURL string) bool {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	database := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	return (host == "localhost" || host == "127.0.0.1") && database == "tutorhub_test"
}

func requireP408ReadOnlyConfirmation(t *testing.T, activeName, expected string) {
	t.Helper()
	actual := strings.TrimSpace(os.Getenv(activeName))
	if actual == "" {
		t.Skipf("%s is not set; explicit P4-08 read-only acceptance gate was not requested", activeName)
	}
	if actual != expected {
		t.Fatalf("%s is not set to the exact P4-08 read-only confirmation", activeName)
	}
	for _, name := range []string{
		"P4_08_DISPOSABLE_CONFIRM",
		"P4_08_OWNER_PREFLIGHT",
		"P4_08_FINAL_SNAPSHOT_CONFIRM",
		"P4_08_SHARED_CONFIRM",
		"P4_08_SHARED_PREFLIGHT_CONFIRM",
		"P4_08_SHARED_FINAL_CONFIRM",
	} {
		if name != activeName && strings.TrimSpace(os.Getenv(name)) != "" {
			t.Fatalf("P4-08 read-only gate refuses confirmation %s", name)
		}
	}
}

func runPostgresP408SharedZeroSideEffectSnapshot(t *testing.T) {
	t.Helper()
	migrationURL := requireConversationEnvironment(t, "DATABASE_MIGRATION_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool := openConversationPool(t, ctx, migrationURL)
	defer pool.Close()
	transaction := beginP408ReadOnly(t, ctx, pool)
	defer func() { _ = transaction.Rollback(context.Background()) }()

	var roomConversations, enabledMediaOverrides int64
	if err := transaction.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.conversations WHERE kind = 'room'),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
     WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms') AND enabled)`,
	).Scan(&roomConversations, &enabledMediaOverrides); err != nil {
		t.Fatal("inspect P4-08 shared zero-side-effect snapshot")
	}
	if roomConversations != 0 || enabledMediaOverrides != 0 {
		t.Fatal("P4-08 shared staging contains unexpected room-chat or media-feature side effects")
	}
	t.Log("P4_08_SHARED_FINAL PASS room_conversations=0 enabled_media_overrides=0")
}

func runPostgresP408ReadOnlySnapshot(t *testing.T, expectedVersion int, postflight bool) {
	t.Helper()
	migrationURL := requireConversationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireConversationEnvironment(t, "DATABASE_POOL_URL")
	requireP408NeonURLBoundary(t, migrationURL, runtimeURL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	migrationPool := openConversationPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	runtimePool := openConversationPool(t, ctx, runtimeURL)
	defer runtimePool.Close()
	migrationTx := beginP408ReadOnly(t, ctx, migrationPool)
	defer func() { _ = migrationTx.Rollback(context.Background()) }()
	runtimeTx := beginP408ReadOnly(t, ctx, runtimePool)
	defer func() { _ = runtimeTx.Rollback(context.Background()) }()

	var migrationRole, runtimeRole, migrationDatabase, runtimeDatabase string
	if err := migrationTx.QueryRow(ctx, "SELECT current_user, current_database()").Scan(
		&migrationRole,
		&migrationDatabase,
	); err != nil {
		t.Fatal("inspect P4-08 migration identity")
	}
	if err := runtimeTx.QueryRow(ctx, "SELECT current_user, current_database()").Scan(
		&runtimeRole,
		&runtimeDatabase,
	); err != nil {
		t.Fatal("inspect P4-08 runtime identity")
	}
	if migrationRole == runtimeRole || migrationDatabase != runtimeDatabase {
		t.Fatal("P4-08 requires distinct owner/runtime roles on one database")
	}

	var version int
	var dirty bool
	if err := migrationTx.QueryRow(
		ctx,
		"SELECT version, dirty FROM public.tutorhub_schema_migrations",
	).Scan(&version, &dirty); err != nil {
		t.Fatal("inspect P4-08 migration ledger")
	}
	if version != expectedVersion || dirty {
		t.Fatal("P4-08 found an unexpected migration ledger state")
	}

	var ownerOwnsConversations, runtimeSuperuser, runtimeCreateRole, runtimeCreateDatabase bool
	if err := migrationTx.QueryRow(ctx, `
SELECT
    pg_get_userbyid(relation.relowner) = current_user,
    runtime.rolsuper,
    runtime.rolcreaterole,
    runtime.rolcreatedb
FROM pg_class AS relation
JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
JOIN pg_roles AS runtime ON runtime.rolname = $1
WHERE namespace.nspname = 'tutorhub' AND relation.relname = 'conversations'`,
		runtimeRole,
	).Scan(
		&ownerOwnsConversations,
		&runtimeSuperuser,
		&runtimeCreateRole,
		&runtimeCreateDatabase,
	); err != nil {
		t.Fatal("inspect P4-08 role boundary")
	}
	if !ownerOwnsConversations || runtimeSuperuser || runtimeCreateRole || runtimeCreateDatabase {
		t.Fatal("P4-08 owner/runtime role boundary is not safe")
	}

	if !postflight {
		t.Log("P4_08_OWNER_PREFLIGHT PASS ledger=33 dirty=false owner_runtime_boundary=true")
		return
	}

	var columnExists, selected, inserted, updated, publicPrivilege bool
	if err := runtimeTx.QueryRow(ctx, `
SELECT
    EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'tutorhub' AND table_name = 'conversations'
          AND column_name = 'media_space_id'
    ),
    has_column_privilege(current_user, 'tutorhub.conversations', 'media_space_id', 'SELECT'),
    has_column_privilege(current_user, 'tutorhub.conversations', 'media_space_id', 'INSERT'),
    has_column_privilege(current_user, 'tutorhub.conversations', 'media_space_id', 'UPDATE'),
    EXISTS (
        SELECT 1 FROM information_schema.column_privileges
        WHERE table_schema = 'tutorhub' AND table_name = 'conversations'
          AND column_name = 'media_space_id' AND grantee = 'PUBLIC'
          AND privilege_type IN ('SELECT', 'INSERT', 'UPDATE')
    )`,
	).Scan(&columnExists, &selected, &inserted, &updated, &publicPrivilege); err != nil {
		t.Fatal("inspect P4-08 room conversation ACL")
	}
	if !columnExists || !selected || !inserted || updated || publicPrivilege {
		t.Fatal("P4-08 room conversation column ACL is not exact")
	}
	dependencies := []struct {
		relation string
		columns  []string
	}{
		{"tutorhub.media_spaces", []string{
			"tenant_id", "id", "status", "source_kind", "class_id", "source_study_meeting_id",
		}},
		{"tutorhub.media_room_instances", []string{"tenant_id", "space_id", "id", "status"}},
		{"tutorhub.media_space_members", []string{"tenant_id", "space_id", "user_id", "status"}},
		{"tutorhub.media_participant_sessions", []string{
			"tenant_id", "space_id", "room_instance_id", "user_id", "status",
		}},
		{"tutorhub.study_meetings", []string{"tenant_id", "id", "owner_user_id", "title"}},
		{"tutorhub.classes", []string{"tenant_id", "id", "owner_user_id", "status", "title"}},
		{"tutorhub.class_enrollments", []string{
			"tenant_id", "class_id", "user_id", "class_role", "status",
		}},
	}
	for _, dependency := range dependencies {
		for _, column := range dependency.columns {
			var canSelect bool
			if err := runtimeTx.QueryRow(
				ctx,
				"SELECT has_column_privilege(current_user, $1, $2, 'SELECT')",
				dependency.relation,
				column,
			).Scan(&canSelect); err != nil || !canSelect {
				t.Fatalf(
					"P4-08 runtime lacks bounded dependency read on %s.%s",
					dependency.relation,
					column,
				)
			}
		}
	}

	var roomConversations, enabledMediaOverrides int64
	if err := migrationTx.QueryRow(ctx, `
SELECT
    (SELECT count(*) FROM tutorhub.conversations WHERE kind = 'room'),
    (SELECT count(*) FROM tutorhub.tenant_feature_overrides
     WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms') AND enabled)`,
	).Scan(&roomConversations, &enabledMediaOverrides); err != nil {
		t.Fatal("inspect P4-08 final snapshot")
	}
	t.Logf(
		"P4_08_FINAL_SNAPSHOT PASS ledger=34 dirty=false room_conversations=%d retained_enabled_media_overrides=%d",
		roomConversations,
		enabledMediaOverrides,
	)
}

func beginP408ReadOnly(t *testing.T, ctx context.Context, pool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}) pgx.Tx {
	t.Helper()
	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal("begin P4-08 read-only transaction")
	}
	return transaction
}

func requireP408NeonURLBoundary(t *testing.T, migrationURL, runtimeURL string) {
	t.Helper()
	owner, ownerErr := url.Parse(migrationURL)
	runtime, runtimeErr := url.Parse(runtimeURL)
	if ownerErr != nil || runtimeErr != nil {
		t.Fatal("parse P4-08 database URL boundary")
	}
	ownerHost := strings.ToLower(owner.Hostname())
	runtimeHost := strings.ToLower(runtime.Hostname())
	ownerDatabase := strings.Trim(owner.Path, "/")
	runtimeDatabase := strings.Trim(runtime.Path, "/")
	if ownerDatabase == "" || ownerDatabase != runtimeDatabase ||
		strings.Contains(ownerHost, "-pooler.") || !strings.Contains(runtimeHost, "-pooler.") ||
		strings.Replace(runtimeHost, "-pooler.", ".", 1) != ownerHost {
		t.Fatal("P4-08 requires direct owner and matching pooled runtime URLs")
	}
}
