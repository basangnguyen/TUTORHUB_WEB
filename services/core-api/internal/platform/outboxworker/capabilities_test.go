package outboxworker

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestVerifyDatabaseCapabilitiesAcceptsExactLeastPrivilegeContract(t *testing.T) {
	t.Parallel()

	database := &capabilityDatabaseStub{
		row: capabilityRowStub{capabilitiesSafe: true},
	}
	if err := VerifyDatabaseCapabilities(
		context.Background(),
		database,
		time.Second,
		CapabilityContract{},
	); err != nil {
		t.Fatalf("verify exact capabilities: %v", err)
	}

	if database.queryCalls != 1 {
		t.Fatalf("query calls = %d, want 1", database.queryCalls)
	}
	if !strings.Contains(database.query, "current_user") {
		t.Fatal("capability probe must inspect the connected principal")
	}
	for _, required := range []string{
		"session_user = current_user",
		"principal.rolcanlogin",
		"NOT principal.rolsuper",
		"NOT principal.rolcreaterole",
		"NOT principal.rolcreatedb",
		"NOT principal.rolreplication",
		"NOT principal.rolbypassrls",
		"pg_has_role(principal.oid, granted_role.oid, 'MEMBER')",
		"database_definition.datdba <> principal.oid",
		"schema_definition.nspowner <> principal.oid",
		"table_definition.relowner <> principal.oid",
		"relation_definition.relname <> 'outbox_events'",
		"relation_definition.relname <> 'notifications'",
		"'tutorhub.notifications'",
		"notification_column_contract.capabilities_safe",
		"notification_relation_contract.capabilities_safe",
		"relation_definition.relkind IN ('r', 'p', 'v', 'm', 'f')",
		"NOT has_schema_privilege(current_user, 'public', 'CREATE')",
		"has_any_column_privilege",
		"'REFERENCES'",
		"'TRIGGER'",
	} {
		if !strings.Contains(database.query, required) {
			t.Fatalf("capability probe is missing fail-closed check %q", required)
		}
	}
	if strings.Contains(database.query, "tutorhub_runtime") {
		t.Fatal("capability probe must not assume a database role name")
	}
	if len(database.args) != 3 {
		t.Fatalf("query arguments = %d, want 3", len(database.args))
	}
	columns, ok := database.args[0].([]string)
	if !ok {
		t.Fatalf("update-column argument type = %T, want []string", database.args[0])
	}
	if !reflect.DeepEqual(columns, workerUpdateColumns) {
		t.Fatalf("update columns = %v, want %v", columns, workerUpdateColumns)
	}
	canaryEnabled, ok := database.args[1].(bool)
	if !ok || canaryEnabled {
		t.Fatalf("canary gate argument = %#v, want false", database.args[1])
	}
	notificationColumns, ok := database.args[2].([]string)
	if !ok {
		t.Fatalf("notification-column argument type = %T, want []string", database.args[2])
	}
	if !reflect.DeepEqual(notificationColumns, notificationCanaryInsertColumns) {
		t.Fatalf(
			"notification insert columns = %v, want %v",
			notificationColumns,
			notificationCanaryInsertColumns,
		)
	}
	if database.deadlineRemaining <= 0 || database.deadlineRemaining > time.Second {
		t.Fatalf("query deadline remaining = %s", database.deadlineRemaining)
	}
}

func TestVerifyDatabaseCapabilitiesRejectsUnsafeContract(t *testing.T) {
	t.Parallel()

	err := VerifyDatabaseCapabilities(
		context.Background(),
		&capabilityDatabaseStub{
			row: capabilityRowStub{capabilitiesSafe: false},
		},
		time.Second,
		CapabilityContract{},
	)
	if !errors.Is(err, ErrUnsafeDatabaseCapabilities) {
		t.Fatalf("error = %v, want unsafe-capabilities sentinel", err)
	}
}

func TestVerifyDatabaseCapabilitiesRedactsProbeFailure(t *testing.T) {
	t.Parallel()

	const sensitiveError = "postgresql://worker:secret@example.invalid/db role=private_worker"
	err := VerifyDatabaseCapabilities(
		context.Background(),
		&capabilityDatabaseStub{
			row: capabilityRowStub{err: errors.New(sensitiveError)},
		},
		time.Second,
		CapabilityContract{},
	)
	if !errors.Is(err, ErrDatabaseCapabilityProbeFailed) {
		t.Fatalf("error = %v, want probe-failed sentinel", err)
	}
	if strings.Contains(err.Error(), "secret") ||
		strings.Contains(err.Error(), "private_worker") ||
		strings.Contains(err.Error(), "postgresql://") {
		t.Fatalf("probe error leaked sensitive database details: %q", err)
	}
}

func TestVerifyDatabaseCapabilitiesValidatesDependencies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		database capabilityDatabase
		timeout  time.Duration
	}{
		{name: "missing database", database: nil, timeout: time.Second},
		{
			name:     "invalid timeout",
			database: &capabilityDatabaseStub{},
			timeout:  0,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := VerifyDatabaseCapabilities(
				context.Background(),
				test.database,
				test.timeout,
				CapabilityContract{},
			)
			if !errors.Is(err, ErrDatabaseCapabilityProbeFailed) {
				t.Fatalf("error = %v, want probe-failed sentinel", err)
			}
		})
	}
}

func TestVerifyDatabaseCapabilitiesPassesCanaryContractToProbe(t *testing.T) {
	t.Parallel()

	database := &capabilityDatabaseStub{
		row: capabilityRowStub{capabilitiesSafe: true},
	}
	if err := VerifyDatabaseCapabilities(
		context.Background(),
		database,
		time.Second,
		CapabilityContract{EnableInAppNotificationCanary: true},
	); err != nil {
		t.Fatalf("verify canary capabilities: %v", err)
	}

	enabled, ok := database.args[1].(bool)
	if !ok || !enabled {
		t.Fatalf("canary gate argument = %#v, want true", database.args[1])
	}
}

func TestVerifyDatabaseCapabilitiesRejectsAmbiguousContracts(t *testing.T) {
	t.Parallel()

	database := &capabilityDatabaseStub{
		row: capabilityRowStub{capabilitiesSafe: true},
	}
	err := VerifyDatabaseCapabilities(
		context.Background(),
		database,
		time.Second,
		CapabilityContract{},
		CapabilityContract{EnableInAppNotificationCanary: true},
	)
	if !errors.Is(err, ErrDatabaseCapabilityProbeFailed) {
		t.Fatalf("error = %v, want probe-failed sentinel", err)
	}
	if database.queryCalls != 0 {
		t.Fatalf("query calls = %d, want 0", database.queryCalls)
	}
}

type capabilityDatabaseStub struct {
	row               pgx.Row
	queryCalls        int
	query             string
	args              []any
	deadlineRemaining time.Duration
}

func (database *capabilityDatabaseStub) QueryRow(
	ctx context.Context,
	query string,
	args ...any,
) pgx.Row {
	database.queryCalls++
	database.query = query
	database.args = append([]any(nil), args...)
	if deadline, ok := ctx.Deadline(); ok {
		database.deadlineRemaining = time.Until(deadline)
	}
	return database.row
}

type capabilityRowStub struct {
	capabilitiesSafe bool
	err              error
}

func (row capabilityRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	if len(destinations) != 1 {
		return errors.New("unexpected capability destination count")
	}
	destination, ok := destinations[0].(*bool)
	if !ok {
		return errors.New("unexpected capability destination type")
	}
	*destination = row.capabilitiesSafe
	return nil
}
