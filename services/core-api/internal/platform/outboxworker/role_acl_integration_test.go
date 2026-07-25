//go:build integration

package outboxworker

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
)

var integrationAPIInsertColumns = []string{
	"tenant_id",
	"aggregate_type",
	"aggregate_id",
	"event_type",
	"payload",
	"occurred_at",
	"available_at",
}

var integrationWorkerUpdateColumns = []string{
	"attempts",
	"available_at",
	"published_at",
	"last_error",
	"lease_owner",
	"lease_token",
	"leased_at",
	"leased_until",
	"dead_lettered_at",
}

func TestPostgresOutboxRuntimeRolesEnforceLeastPrivilegeSplit(t *testing.T) {
	ctx, migrationPool, _ := openLocalOutboxIntegrationStore(t)
	requireIntegrationRoleAdministration(t, ctx, migrationPool)

	apiRole := uniqueIntegrationRoleName("api")
	workerRole := uniqueIntegrationRoleName("worker")
	apiPassword := createIntegrationLoginRole(t, ctx, migrationPool, apiRole)
	workerPassword := createIntegrationLoginRole(t, ctx, migrationPool, workerRole)
	grantOutboxAPICapabilities(t, ctx, migrationPool, apiRole)
	grantOutboxWorkerCapabilities(t, ctx, migrationPool, workerRole)

	// Prove that a privileged login cannot hide behind SET ROLE. The effective
	// table ACL is exact in this transaction, but RESET ROLE would recover the
	// migration principal, so the startup probe must fail closed.
	grantIntegrationRoleMembership(t, ctx, migrationPool, workerRole)
	withIntegrationRole(t, ctx, migrationPool, workerRole, func(transaction pgx.Tx) {
		err := VerifyDatabaseCapabilities(ctx, transaction, 3*time.Second)
		if !errors.Is(err, ErrUnsafeDatabaseCapabilities) {
			t.Fatalf(
				"SET ROLE capability probe error = %v, want unsafe-capabilities sentinel",
				err,
			)
		}
	})

	apiPool := openDirectIntegrationRolePool(t, ctx, apiRole, apiPassword)
	workerPool := openDirectIntegrationRolePool(t, ctx, workerRole, workerPassword)

	assertIntegrationRoleCapabilities(t, ctx, apiPool, roleCapabilityExpectation{
		schemaUsage:     true,
		anyInsert:       true,
		anyUpdate:       false,
		tableSelect:     false,
		tableInsert:     false,
		tableUpdate:     false,
		tableDelete:     false,
		tableTruncate:   false,
		tableReferences: false,
		anyReferences:   false,
		tableTrigger:    false,
		schemaCreate:    false,
		databaseCreate:  false,
	})
	assertExactIntegrationColumnPrivilege(
		t, ctx, apiPool, "INSERT", integrationAPIInsertColumns,
	)
	assertExactIntegrationColumnPrivilege(t, ctx, apiPool, "UPDATE", nil)
	if err := VerifyDatabaseCapabilities(ctx, apiPool, 3*time.Second); !errors.Is(
		err,
		ErrUnsafeDatabaseCapabilities,
	) {
		t.Fatalf("API role probe error = %v, want unsafe-capabilities sentinel", err)
	}

	assertIntegrationRoleCapabilities(t, ctx, workerPool, roleCapabilityExpectation{
		schemaUsage:     true,
		anyInsert:       false,
		anyUpdate:       true,
		tableSelect:     true,
		tableInsert:     false,
		tableUpdate:     false,
		tableDelete:     false,
		tableTruncate:   false,
		tableReferences: false,
		anyReferences:   false,
		tableTrigger:    false,
		schemaCreate:    false,
		databaseCreate:  false,
	})
	assertExactIntegrationColumnPrivilege(t, ctx, workerPool, "INSERT", nil)
	assertExactIntegrationColumnPrivilege(
		t, ctx, workerPool, "UPDATE", integrationWorkerUpdateColumns,
	)
	if err := VerifyDatabaseCapabilities(ctx, workerPool, 3*time.Second); err != nil {
		t.Fatalf("direct worker role capability probe rejected exact ACL: %v", err)
	}

	grantTemporaryIntegrationPrivilege(
		t,
		ctx,
		migrationPool,
		`SELECT ON TABLE tutorhub.tenants`,
		workerRole,
	)
	assertUnsafeIntegrationWorkerCapabilities(t, ctx, workerPool, "business-table SELECT")
	revokeTemporaryIntegrationPrivilege(
		t,
		ctx,
		migrationPool,
		`SELECT ON TABLE tutorhub.tenants`,
		workerRole,
	)
	viewName := createTemporaryIntegrationView(t, ctx, migrationPool)
	viewPrivilege := `SELECT ON TABLE ` + pgx.Identifier{"tutorhub", viewName}.Sanitize()
	grantTemporaryIntegrationPrivilege(t, ctx, migrationPool, viewPrivilege, workerRole)
	assertUnsafeIntegrationWorkerCapabilities(t, ctx, workerPool, "view SELECT")
	revokeTemporaryIntegrationPrivilege(t, ctx, migrationPool, viewPrivilege, workerRole)
	grantTemporaryIntegrationPrivilege(t, ctx, migrationPool, `CREATE ON SCHEMA public`, workerRole)
	assertUnsafeIntegrationWorkerCapabilities(t, ctx, workerPool, "public-schema CREATE")
	revokeTemporaryIntegrationPrivilege(t, ctx, migrationPool, `CREATE ON SCHEMA public`, workerRole)
	if err := VerifyDatabaseCapabilities(ctx, workerPool, 3*time.Second); err != nil {
		t.Fatalf("worker probe did not recover after excess privilege revoke: %v", err)
	}

	// NOINHERIT keeps this membership out of the effective ACL, but the worker
	// could still SET ROLE. The probe must therefore reject any membership.
	grantRoleToRole(t, ctx, migrationPool, apiRole, workerRole)
	if err := VerifyDatabaseCapabilities(ctx, workerPool, 3*time.Second); !errors.Is(
		err,
		ErrUnsafeDatabaseCapabilities,
	) {
		t.Fatalf("member worker probe error = %v, want unsafe-capabilities sentinel", err)
	}
	revokeRoleFromRole(t, ctx, migrationPool, apiRole, workerRole)
	if err := VerifyDatabaseCapabilities(ctx, workerPool, 3*time.Second); err != nil {
		t.Fatalf("worker probe did not recover after membership revoke: %v", err)
	}

	exerciseDirectRoleOutboxStore(t, ctx, migrationPool, apiPool, workerPool)
}

func requireIntegrationRoleAdministration(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) {
	t.Helper()

	var canAdministerRoles bool
	if err := pool.QueryRow(ctx, `
SELECT rolsuper OR rolcreaterole
FROM pg_catalog.pg_roles
WHERE rolname = current_user`).Scan(&canAdministerRoles); err != nil {
		t.Fatalf("inspect integration database role administration capability: %v", err)
	}
	if !canAdministerRoles {
		t.Skip(
			"P3-03 role ACL integration requires a loopback migration principal " +
				"with SUPERUSER or CREATEROLE",
		)
	}
}

func uniqueIntegrationRoleName(kind string) string {
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
	return fmt.Sprintf("p303_outbox_%s_%s", kind, suffix)
}

func createTemporaryIntegrationView(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) string {
	t.Helper()

	viewName := uniqueIntegrationRoleName("view")
	viewIdentifier := pgx.Identifier{"tutorhub", viewName}.Sanitize()
	if _, err := pool.Exec(ctx, `CREATE VIEW `+viewIdentifier+` AS SELECT 1 AS marker`); err != nil {
		t.Fatalf("create temporary ACL probe view: %s", safeIntegrationPostgresReason(err))
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DROP VIEW IF EXISTS `+viewIdentifier); err != nil {
			t.Errorf("drop temporary ACL probe view: %s", safeIntegrationPostgresReason(err))
		}
	})
	return viewName
}

func createIntegrationLoginRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
) string {
	t.Helper()

	identifier := pgx.Identifier{roleName}.Sanitize()
	// uuid.NewString is backed by crypto/rand. Removing dashes leaves a value
	// that is safe to embed as the password literal without an escaping path.
	password := strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err := pool.Exec(ctx, `
CREATE ROLE `+identifier+`
    LOGIN
    PASSWORD '`+password+`'
    NOSUPERUSER
    NOCREATEDB
    NOCREATEROLE
    NOINHERIT
    NOREPLICATION
    NOBYPASSRLS`)
	if err != nil {
		if isIntegrationRoleAdministrationUnavailable(err) {
			t.Skipf(
				"P3-03 role ACL integration cannot create temporary login role %q: %s",
				roleName,
				safeIntegrationPostgresReason(err),
			)
		}
		t.Fatalf(
			"create temporary outbox login role %q: %s",
			roleName,
			safeIntegrationPostgresReason(err),
		)
	}
	t.Cleanup(func() {
		cleanupIntegrationRole(t, pool, roleName)
	})
	return password
}

func openDirectIntegrationRolePool(
	t *testing.T,
	ctx context.Context,
	roleName string,
	password string,
) *pgxpool.Pool {
	t.Helper()

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_MIGRATION_URL"))
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("parse loopback database configuration for direct role connection")
	}
	poolConfig.ConnConfig.User = roleName
	poolConfig.ConnConfig.Password = password
	poolConfig.ConnConfig.RuntimeParams["application_name"] = "p3-03-role-acl-integration"
	delete(poolConfig.ConnConfig.RuntimeParams, "role")
	poolConfig.MaxConns = 2
	poolConfig.MinConns = 0

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("open direct temporary role integration pool")
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Fatal("authenticate direct temporary role integration pool")
	}
	return pool
}

func grantIntegrationRoleMembership(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
) {
	t.Helper()

	identifier := pgx.Identifier{roleName}.Sanitize()
	if _, err := pool.Exec(ctx, `GRANT `+identifier+` TO CURRENT_USER`); err != nil {
		if isIntegrationRoleAdministrationUnavailable(err) {
			t.Skipf(
				"P3-03 role ACL integration cannot SET ROLE through temporary role %q: %s",
				roleName,
				safeIntegrationPostgresReason(err),
			)
		}
		t.Fatalf("grant temporary role %q to migration principal: %v", roleName, err)
	}
}

func grantRoleToRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	grantedRole string,
	memberRole string,
) {
	t.Helper()

	statement := `GRANT ` + pgx.Identifier{grantedRole}.Sanitize() +
		` TO ` + pgx.Identifier{memberRole}.Sanitize()
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("grant temporary role membership: %s", safeIntegrationPostgresReason(err))
	}
}

func revokeRoleFromRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	grantedRole string,
	memberRole string,
) {
	t.Helper()

	statement := `REVOKE ` + pgx.Identifier{grantedRole}.Sanitize() +
		` FROM ` + pgx.Identifier{memberRole}.Sanitize()
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("revoke temporary role membership: %s", safeIntegrationPostgresReason(err))
	}
}

func grantTemporaryIntegrationPrivilege(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	privilegeClause string,
	roleName string,
) {
	t.Helper()
	statement := `GRANT ` + privilegeClause + ` TO ` + pgx.Identifier{roleName}.Sanitize()
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("grant temporary excess worker privilege: %s", safeIntegrationPostgresReason(err))
	}
}

func revokeTemporaryIntegrationPrivilege(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	privilegeClause string,
	roleName string,
) {
	t.Helper()
	statement := `REVOKE ` + privilegeClause + ` FROM ` + pgx.Identifier{roleName}.Sanitize()
	if _, err := pool.Exec(ctx, statement); err != nil {
		t.Fatalf("revoke temporary excess worker privilege: %s", safeIntegrationPostgresReason(err))
	}
}

func assertUnsafeIntegrationWorkerCapabilities(
	t *testing.T,
	ctx context.Context,
	workerPool *pgxpool.Pool,
	caseName string,
) {
	t.Helper()
	if err := VerifyDatabaseCapabilities(ctx, workerPool, 3*time.Second); !errors.Is(
		err,
		ErrUnsafeDatabaseCapabilities,
	) {
		t.Fatalf("%s probe error = %v, want unsafe-capabilities sentinel", caseName, err)
	}
}

func grantOutboxAPICapabilities(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
) {
	t.Helper()

	roleIdentifier := pgx.Identifier{roleName}.Sanitize()
	if _, err := pool.Exec(
		ctx,
		`GRANT USAGE ON SCHEMA tutorhub TO `+roleIdentifier,
	); err != nil {
		t.Fatalf("grant TutorHub schema usage to temporary API role: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`GRANT INSERT (`+sanitizeIntegrationColumnList(integrationAPIInsertColumns)+`)
ON TABLE tutorhub.outbox_events
TO `+roleIdentifier,
	); err != nil {
		t.Fatalf("grant exact outbox insert columns to temporary API role: %v", err)
	}
}

func grantOutboxWorkerCapabilities(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
) {
	t.Helper()

	roleIdentifier := pgx.Identifier{roleName}.Sanitize()
	if _, err := pool.Exec(
		ctx,
		`GRANT USAGE ON SCHEMA tutorhub TO `+roleIdentifier,
	); err != nil {
		t.Fatalf("grant TutorHub schema usage to temporary worker role: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`GRANT SELECT ON TABLE tutorhub.outbox_events TO `+roleIdentifier,
	); err != nil {
		t.Fatalf("grant outbox select to temporary worker role: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`GRANT UPDATE (`+sanitizeIntegrationColumnList(integrationWorkerUpdateColumns)+`)
ON TABLE tutorhub.outbox_events
TO `+roleIdentifier,
	); err != nil {
		t.Fatalf("grant exact outbox update columns to temporary worker role: %v", err)
	}
}

func sanitizeIntegrationColumnList(columns []string) string {
	identifiers := make([]string, 0, len(columns))
	for _, column := range columns {
		identifiers = append(identifiers, pgx.Identifier{column}.Sanitize())
	}
	return strings.Join(identifiers, ", ")
}

func withIntegrationRole(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	roleName string,
	assertions func(pgx.Tx),
) {
	t.Helper()

	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection for temporary role %q: %v", roleName, err)
	}
	defer connection.Release()

	transaction, err := connection.Begin(ctx)
	if err != nil {
		t.Fatalf("begin temporary role transaction %q: %v", roleName, err)
	}
	defer func() {
		if err := transaction.Rollback(context.Background()); err != nil &&
			!errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback temporary role transaction %q: %v", roleName, err)
		}
	}()

	if _, err := transaction.Exec(
		ctx,
		`SET LOCAL ROLE `+pgx.Identifier{roleName}.Sanitize(),
	); err != nil {
		if isIntegrationRoleAdministrationUnavailable(err) {
			t.Skipf(
				"P3-03 role ACL integration cannot activate temporary role %q: %s",
				roleName,
				safeIntegrationPostgresReason(err),
			)
		}
		t.Fatalf("set temporary role %q: %v", roleName, err)
	}
	assertions(transaction)
}

type roleCapabilityExpectation struct {
	schemaUsage     bool
	schemaCreate    bool
	databaseCreate  bool
	tableSelect     bool
	tableInsert     bool
	anyInsert       bool
	tableUpdate     bool
	anyUpdate       bool
	tableDelete     bool
	tableTruncate   bool
	tableReferences bool
	anyReferences   bool
	tableTrigger    bool
}

func assertIntegrationRoleCapabilities(
	t *testing.T,
	ctx context.Context,
	database interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	want roleCapabilityExpectation,
) {
	t.Helper()

	var got roleCapabilityExpectation
	if err := database.QueryRow(ctx, `
SELECT has_schema_privilege(current_user, 'tutorhub', 'USAGE'),
       has_schema_privilege(current_user, 'tutorhub', 'CREATE'),
       has_database_privilege(current_user, current_database(), 'CREATE'),
       has_table_privilege(current_user, 'tutorhub.outbox_events', 'SELECT'),
       has_table_privilege(current_user, 'tutorhub.outbox_events', 'INSERT'),
       has_any_column_privilege(current_user, 'tutorhub.outbox_events', 'INSERT'),
       has_table_privilege(current_user, 'tutorhub.outbox_events', 'UPDATE'),
       has_any_column_privilege(current_user, 'tutorhub.outbox_events', 'UPDATE'),
       has_table_privilege(current_user, 'tutorhub.outbox_events', 'DELETE'),
       has_table_privilege(current_user, 'tutorhub.outbox_events', 'TRUNCATE'),
       has_table_privilege(current_user, 'tutorhub.outbox_events', 'REFERENCES'),
       has_any_column_privilege(current_user, 'tutorhub.outbox_events', 'REFERENCES'),
       has_table_privilege(current_user, 'tutorhub.outbox_events', 'TRIGGER')`).Scan(
		&got.schemaUsage,
		&got.schemaCreate,
		&got.databaseCreate,
		&got.tableSelect,
		&got.tableInsert,
		&got.anyInsert,
		&got.tableUpdate,
		&got.anyUpdate,
		&got.tableDelete,
		&got.tableTruncate,
		&got.tableReferences,
		&got.anyReferences,
		&got.tableTrigger,
	); err != nil {
		t.Fatalf("inspect effective outbox role capabilities: %v", err)
	}
	if got != want {
		t.Fatalf("effective outbox role capabilities = %+v, want %+v", got, want)
	}
}

func assertExactIntegrationColumnPrivilege(
	t *testing.T,
	ctx context.Context,
	database interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	privilege string,
	allowedColumns []string,
) {
	t.Helper()

	allowed := make(map[string]struct{}, len(allowedColumns))
	for _, column := range allowedColumns {
		allowed[column] = struct{}{}
	}

	rows, err := database.Query(ctx, `
SELECT attribute.attname,
       has_column_privilege(
           current_user,
           'tutorhub.outbox_events',
           attribute.attname,
           $1
       )
FROM pg_catalog.pg_attribute AS attribute
JOIN pg_catalog.pg_class AS relation
  ON relation.oid = attribute.attrelid
JOIN pg_catalog.pg_namespace AS namespace
  ON namespace.oid = relation.relnamespace
WHERE namespace.nspname = 'tutorhub'
  AND relation.relname = 'outbox_events'
  AND attribute.attnum > 0
  AND NOT attribute.attisdropped
ORDER BY attribute.attnum`, privilege)
	if err != nil {
		t.Fatalf("query exact %s column privileges: %v", privilege, err)
	}
	defer rows.Close()

	seen := make(map[string]struct{})
	for rows.Next() {
		var column string
		var granted bool
		if err := rows.Scan(&column, &granted); err != nil {
			t.Fatalf("scan exact %s column privilege: %v", privilege, err)
		}
		_, expected := allowed[column]
		if granted != expected {
			t.Fatalf(
				"%s privilege on outbox column %q = %t, want %t",
				privilege,
				column,
				granted,
				expected,
			)
		}
		seen[column] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate exact %s column privileges: %v", privilege, err)
	}
	for column := range allowed {
		if _, exists := seen[column]; !exists {
			t.Fatalf("expected outbox column %q does not exist", column)
		}
	}
}

func exerciseDirectRoleOutboxStore(
	t *testing.T,
	ctx context.Context,
	migrationPool *pgxpool.Pool,
	apiPool *pgxpool.Pool,
	workerPool *pgxpool.Pool,
) {
	t.Helper()

	apiStore, err := NewPostgresStore(apiPool, 3*time.Second)
	if err != nil {
		t.Fatalf("create API-role outbox store: %v", err)
	}
	if _, err := apiStore.Claim(ctx, ClaimRequest{
		EventTypes:    []string{uniqueIntegrationEventType("api_forbidden_claim")},
		OwnerID:       uuid.New(),
		BatchSize:     1,
		LeaseDuration: 5 * time.Second,
		MaxAttempts:   5,
	}); err == nil {
		t.Fatal("API role unexpectedly claimed or read outbox events")
	}

	workerStore, err := NewPostgresStore(workerPool, 3*time.Second)
	if err != nil {
		t.Fatalf("create worker-role outbox store: %v", err)
	}

	ackType := uniqueIntegrationEventType("acl_ack")
	ackID := insertOutboxWithDirectAPIRole(t, ctx, apiPool, migrationPool, ackType)
	ackEvent := claimDirectRoleEvent(t, ctx, workerStore, ackType, ackID)
	if err := workerStore.Ack(ctx, ackEvent.Lease); err != nil {
		t.Fatalf("ack with direct worker role: %v", err)
	}
	assertDirectRoleEventState(t, ctx, migrationPool, ackID, true, false, 0, true)

	retryType := uniqueIntegrationEventType("acl_retry")
	retryID := insertOutboxWithDirectAPIRole(t, ctx, apiPool, migrationPool, retryType)
	retryEvent := claimDirectRoleEvent(t, ctx, workerStore, retryType, retryID)
	if err := workerStore.Retry(
		ctx,
		retryEvent.Lease,
		time.Minute,
		ErrorCodeHandlerFailed,
	); err != nil {
		t.Fatalf("retry with direct worker role: %v", err)
	}
	assertDirectRoleEventState(t, ctx, migrationPool, retryID, false, false, 1, true)

	deadType := uniqueIntegrationEventType("acl_dead_letter")
	deadID := insertOutboxWithDirectAPIRole(t, ctx, apiPool, migrationPool, deadType)
	deadEvent := claimDirectRoleEvent(t, ctx, workerStore, deadType, deadID)
	if err := workerStore.DeadLetter(
		ctx,
		deadEvent.Lease,
		ErrorCodeInvalidPayload,
	); err != nil {
		t.Fatalf("dead-letter with direct worker role: %v", err)
	}
	assertDirectRoleEventState(t, ctx, migrationPool, deadID, false, true, 1, true)
}

func insertOutboxWithDirectAPIRole(
	t *testing.T,
	ctx context.Context,
	apiPool *pgxpool.Pool,
	migrationPool *pgxpool.Pool,
	eventType string,
) uuid.UUID {
	t.Helper()

	aggregateID := uuid.New()
	if _, err := apiPool.Exec(ctx, `
INSERT INTO tutorhub.outbox_events (
    tenant_id,
    aggregate_type,
    aggregate_id,
    event_type,
    payload,
    occurred_at,
    available_at
)
VALUES (NULL, 'role_acl_probe', $1, $2, '{}'::jsonb, clock_timestamp(), clock_timestamp())`,
		aggregateID,
		eventType,
	); err != nil {
		t.Fatal("insert outbox event with direct API role")
	}

	var eventID uuid.UUID
	if err := migrationPool.QueryRow(ctx, `
SELECT id
FROM tutorhub.outbox_events
WHERE aggregate_type = 'role_acl_probe'
  AND aggregate_id = $1
  AND event_type = $2`, aggregateID, eventType).Scan(&eventID); err != nil {
		t.Fatalf("resolve direct API-role outbox event: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := migrationPool.Exec(
			cleanupCtx,
			`DELETE FROM tutorhub.outbox_events WHERE id = $1`,
			eventID,
		); err != nil {
			t.Errorf("delete direct role outbox fixture: %v", err)
		}
	})
	return eventID
}

func claimDirectRoleEvent(
	t *testing.T,
	ctx context.Context,
	store *PostgresStore,
	eventType string,
	eventID uuid.UUID,
) Event {
	t.Helper()

	events, err := store.Claim(ctx, ClaimRequest{
		EventTypes:    []string{eventType},
		OwnerID:       uuid.New(),
		BatchSize:     1,
		LeaseDuration: 5 * time.Second,
		MaxAttempts:   5,
	})
	if err != nil {
		t.Fatalf("claim with direct worker role: %v", err)
	}
	if len(events) != 1 || events[0].ID != eventID {
		t.Fatalf("direct worker claim = %+v, want event %s", events, eventID)
	}
	return events[0]
}

func assertDirectRoleEventState(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	eventID uuid.UUID,
	published bool,
	deadLettered bool,
	attempts int,
	leaseCleared bool,
) {
	t.Helper()

	var gotPublished, gotDeadLettered, gotLeaseCleared bool
	var gotAttempts int
	if err := pool.QueryRow(ctx, `
SELECT published_at IS NOT NULL,
       dead_lettered_at IS NOT NULL,
       attempts,
       lease_owner IS NULL AND leased_at IS NULL AND leased_until IS NULL
FROM tutorhub.outbox_events
WHERE id = $1`, eventID).Scan(
		&gotPublished,
		&gotDeadLettered,
		&gotAttempts,
		&gotLeaseCleared,
	); err != nil {
		t.Fatalf("inspect direct-role outbox state: %v", err)
	}
	if gotPublished != published || gotDeadLettered != deadLettered ||
		gotAttempts != attempts || gotLeaseCleared != leaseCleared {
		t.Fatalf(
			"direct-role outbox state = published:%t dead:%t attempts:%d lease_cleared:%t, "+
				"want published:%t dead:%t attempts:%d lease_cleared:%t",
			gotPublished,
			gotDeadLettered,
			gotAttempts,
			gotLeaseCleared,
			published,
			deadLettered,
			attempts,
			leaseCleared,
		)
	}
}

func cleanupIntegrationRole(t *testing.T, pool *pgxpool.Pool, roleName string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	identifier := pgx.Identifier{roleName}.Sanitize()

	statements := []string{
		`DROP OWNED BY ` + identifier,
		`REVOKE ` + identifier + ` FROM CURRENT_USER`,
		`DROP ROLE ` + identifier,
	}
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Errorf("clean up temporary outbox role %q: %v", roleName, err)
			return
		}
	}
}

func isIntegrationRoleAdministrationUnavailable(err error) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}
	return postgresError.Code == "42501" || postgresError.Code == "0A000"
}

func safeIntegrationPostgresReason(err error) string {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return "role administration is unavailable"
	}
	switch postgresError.Code {
	case "42501":
		return "migration principal lacks role-administration privilege (SQLSTATE 42501)"
	case "0A000":
		return "database provider does not support temporary role administration (SQLSTATE 0A000)"
	default:
		return "role administration is unavailable"
	}
}
