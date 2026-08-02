//go:build integration

package calendar

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

func TestAvailabilityPollCoreRuntimeExactACL(t *testing.T) {
	migrationURL := requireCalendarEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireCalendarEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := strings.TrimSpace(os.Getenv("DATABASE_POLL_MAINTENANCE_URL"))
	if maintenanceURL == "" {
		t.Skip("exact runtime ACL requires the disposable maintenance login")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply runtime ACL migrations: %v", err)
	}
	migrationPool := openCalendarPool(t, ctx, migrationURL)
	defer migrationPool.Close()
	runtimePool := openCalendarPool(t, ctx, runtimeURL)
	defer runtimePool.Close()
	maintenancePool := openCalendarPool(t, ctx, maintenanceURL)
	defer maintenancePool.Close()

	var runtimeRole, migrationRole, maintenanceRole string
	if err := runtimePool.QueryRow(ctx, "SELECT current_user").Scan(&runtimeRole); err != nil {
		t.Fatalf("read runtime identity: %v", err)
	}
	if err := migrationPool.QueryRow(ctx, "SELECT current_user").Scan(&migrationRole); err != nil {
		t.Fatalf("read migration identity: %v", err)
	}
	if err := maintenancePool.QueryRow(ctx, "SELECT current_user").Scan(&maintenanceRole); err != nil {
		t.Fatalf("read maintenance identity: %v", err)
	}

	var schemaUsage, schemaCreate, functionExecute bool
	if err := runtimePool.QueryRow(ctx, `
SELECT
    has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
    has_schema_privilege(current_user, 'tutorhub', 'CREATE'),
    has_function_privilege(
        current_user,
        'tutorhub.purge_expired_availability_polls(integer)',
        'EXECUTE'
    )`).Scan(&schemaUsage, &schemaCreate, &functionExecute); err != nil {
		t.Fatalf("inspect runtime schema/function ACL: %v", err)
	}
	if !schemaUsage || schemaCreate || functionExecute {
		t.Fatal("runtime schema/function ACL is not exact")
	}

	type relationExpectation struct {
		name            string
		selectPrivilege bool
		insertPrivilege bool
		updatePrivilege bool
		columnUpdate    bool
	}
	relations := []relationExpectation{
		{name: "tutorhub.availability_polls", selectPrivilege: true, insertPrivilege: true, updatePrivilege: true, columnUpdate: true},
		{name: "tutorhub.availability_poll_slots", selectPrivilege: true, insertPrivilege: true, updatePrivilege: true, columnUpdate: true},
		{name: "tutorhub.availability_poll_participants", selectPrivilege: true, insertPrivilege: true, updatePrivilege: true, columnUpdate: true},
		{name: "tutorhub.availability_poll_capabilities", selectPrivilege: true, insertPrivilege: true, updatePrivilege: true, columnUpdate: true},
		{name: "tutorhub.availability_poll_responses", selectPrivilege: true, insertPrivilege: true, updatePrivilege: true, columnUpdate: true},
		{name: "tutorhub.availability_poll_answers", selectPrivilege: true, insertPrivilege: true, updatePrivilege: true, columnUpdate: true},
		{name: "tutorhub.availability_poll_mutation_receipts", selectPrivilege: true, insertPrivilege: true, updatePrivilege: false, columnUpdate: false},
		{name: "tutorhub.study_meetings", selectPrivilege: true, insertPrivilege: true, updatePrivilege: true, columnUpdate: true},
	}
	for _, expected := range relations {
		var selected, inserted, updated, deleted, truncated, referenced, triggered bool
		var columnSelected, columnInserted, columnUpdated bool
		if err := runtimePool.QueryRow(ctx, `
SELECT
    has_table_privilege(current_user, $1, 'SELECT'),
    has_table_privilege(current_user, $1, 'INSERT'),
    has_table_privilege(current_user, $1, 'UPDATE'),
    has_table_privilege(current_user, $1, 'DELETE'),
    has_table_privilege(current_user, $1, 'TRUNCATE'),
    has_table_privilege(current_user, $1, 'REFERENCES'),
    has_table_privilege(current_user, $1, 'TRIGGER'),
    has_any_column_privilege(current_user, $1, 'SELECT'),
    has_any_column_privilege(current_user, $1, 'INSERT'),
    has_any_column_privilege(current_user, $1, 'UPDATE')`, expected.name).Scan(
			&selected, &inserted, &updated, &deleted, &truncated, &referenced, &triggered,
			&columnSelected, &columnInserted, &columnUpdated,
		); err != nil {
			t.Fatalf("inspect runtime ACL for %s: %v", expected.name, err)
		}
		if selected != expected.selectPrivilege || inserted != expected.insertPrivilege ||
			updated != expected.updatePrivilege || columnUpdated != expected.columnUpdate ||
			!columnSelected || !columnInserted || deleted || truncated || referenced || triggered {
			t.Fatalf("runtime ACL mismatch for %s", expected.name)
		}
	}

	var safeRole, noMigrationInheritance, noMaintenanceInheritance bool
	var notTableOwner, notFunctionOwner bool
	if err := migrationPool.QueryRow(ctx, `
SELECT
    NOT runtime.rolsuper,
    NOT pg_has_role(runtime.oid, migration.oid, 'MEMBER'),
    NOT pg_has_role(runtime.oid, maintenance.oid, 'MEMBER'),
    NOT EXISTS (
        SELECT 1
        FROM pg_class AS relation
        JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
        WHERE namespace.nspname = 'tutorhub'
          AND relation.relname = ANY($4::text[])
          AND relation.relowner = runtime.oid
    ),
    NOT EXISTS (
        SELECT 1
        FROM pg_proc AS function
        WHERE function.oid = 'tutorhub.purge_expired_availability_polls(integer)'::regprocedure
          AND function.proowner = runtime.oid
    )
FROM pg_roles AS runtime
JOIN pg_roles AS migration ON migration.rolname = $2
JOIN pg_roles AS maintenance ON maintenance.rolname = $3
WHERE runtime.rolname = $1`,
		runtimeRole,
		migrationRole,
		maintenanceRole,
		[]string{
			"availability_polls", "availability_poll_slots",
			"availability_poll_participants", "availability_poll_capabilities",
			"availability_poll_responses", "availability_poll_answers",
			"availability_poll_mutation_receipts", "study_meetings",
		},
	).Scan(
		&safeRole, &noMigrationInheritance, &noMaintenanceInheritance,
		&notTableOwner, &notFunctionOwner,
	); err != nil {
		t.Fatalf("inspect runtime role safety: %v", err)
	}
	if !safeRole || !noMigrationInheritance || !noMaintenanceInheritance ||
		!notTableOwner || !notFunctionOwner {
		t.Fatal("runtime role safety boundary is not exact")
	}

	assertPermissionDenied(t, ctx, runtimePool,
		"SELECT tutorhub.purge_expired_availability_polls(1)")
	assertPermissionDenied(t, ctx, runtimePool,
		"DELETE FROM tutorhub.availability_polls WHERE false")
	assertPermissionDenied(t, ctx, runtimePool,
		"UPDATE tutorhub.availability_poll_mutation_receipts SET result_version = result_version WHERE false")
}
