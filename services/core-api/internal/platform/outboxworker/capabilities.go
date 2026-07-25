package outboxworker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrUnsafeDatabaseCapabilities = errors.New(
		"outbox worker database capabilities do not match the least-privilege contract",
	)
	ErrDatabaseCapabilityProbeFailed = errors.New(
		"outbox worker database capability probe failed",
	)
)

// workerUpdateColumns is the complete set of outbox columns the runtime worker
// may mutate. Keep this list aligned with the claim, ack, retry, dead-letter and
// exhausted-event SQL in store.go. The startup probe deliberately fails if a
// future migration adds a column-level UPDATE grant outside this list.
var workerUpdateColumns = []string{
	"attempts",
	"available_at",
	"dead_lettered_at",
	"last_error",
	"lease_owner",
	"lease_token",
	"leased_at",
	"leased_until",
	"published_at",
}

type capabilityDatabase interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// VerifyDatabaseCapabilities fails closed unless the session authenticated
// directly as a dedicated login role with exactly the capabilities required by
// the leased outbox worker. SET ROLE is deliberately rejected: a privileged
// session could otherwise pass the current_user checks and later RESET ROLE.
//
// The probe does not read or return current_user. Database errors are
// intentionally collapsed into a safe sentinel so credentials, URLs and role
// names cannot escape through startup errors or logs.
func VerifyDatabaseCapabilities(
	ctx context.Context,
	database capabilityDatabase,
	queryTimeout time.Duration,
) error {
	if database == nil {
		return fmt.Errorf("%w: database is required", ErrDatabaseCapabilityProbeFailed)
	}
	if queryTimeout <= 0 {
		return fmt.Errorf("%w: query timeout must be positive", ErrDatabaseCapabilityProbeFailed)
	}

	queryContext, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	var capabilitiesSafe bool
	if err := database.QueryRow(
		queryContext,
		workerCapabilityProbeSQL,
		workerUpdateColumns,
	).Scan(&capabilitiesSafe); err != nil {
		return ErrDatabaseCapabilityProbeFailed
	}
	if !capabilitiesSafe {
		return ErrUnsafeDatabaseCapabilities
	}
	return nil
}

const workerCapabilityProbeSQL = `
WITH principal AS (
    SELECT oid,
           rolcanlogin,
           rolsuper,
           rolcreaterole,
           rolcreatedb,
           rolreplication,
           rolbypassrls
    FROM pg_catalog.pg_roles
    WHERE rolname = current_user
),
role_contract AS (
    SELECT session_user = current_user
           AND principal.rolcanlogin
           AND NOT principal.rolsuper
           AND NOT principal.rolcreaterole
           AND NOT principal.rolcreatedb
           AND NOT principal.rolreplication
           AND NOT principal.rolbypassrls
           AND NOT EXISTS (
               SELECT 1
               FROM pg_catalog.pg_roles AS granted_role
               WHERE granted_role.oid <> principal.oid
                 AND pg_has_role(principal.oid, granted_role.oid, 'MEMBER')
           ) AS capabilities_safe
    FROM principal
),
resource_contract AS (
    SELECT database_definition.datdba <> principal.oid
           AND schema_definition.nspowner <> principal.oid
           AND table_definition.relowner <> principal.oid AS capabilities_safe
    FROM principal
    JOIN pg_catalog.pg_database AS database_definition
      ON database_definition.datname = current_database()
    JOIN pg_catalog.pg_namespace AS schema_definition
      ON schema_definition.nspname = 'tutorhub'
    JOIN pg_catalog.pg_class AS table_definition
      ON table_definition.relnamespace = schema_definition.oid
     AND table_definition.relname = 'outbox_events'
     AND table_definition.relkind IN ('r', 'p')
),
column_capabilities AS (
    SELECT column_name,
           has_column_privilege(
               current_user,
               'tutorhub.outbox_events',
               column_name,
               'UPDATE'
           ) AS can_update,
           has_column_privilege(
               current_user,
               'tutorhub.outbox_events',
               column_name,
               'INSERT'
           ) AS can_insert
    FROM information_schema.columns
    WHERE table_schema = 'tutorhub'
      AND table_name = 'outbox_events'
),
column_contract AS (
    SELECT count(*) FILTER (
               WHERE column_name = ANY($1::text[])
           ) = cardinality($1::text[])
           AND COALESCE(
               bool_and(
                   can_update = (column_name = ANY($1::text[]))
               ),
               false
           )
           AND COALESCE(bool_and(NOT can_insert), false) AS capabilities_safe
    FROM column_capabilities
),
other_relation_contract AS (
    SELECT NOT EXISTS (
        SELECT 1
        FROM pg_catalog.pg_class AS relation_definition
        JOIN pg_catalog.pg_namespace AS schema_definition
          ON schema_definition.oid = relation_definition.relnamespace
        WHERE schema_definition.nspname = 'tutorhub'
          AND relation_definition.relname <> 'outbox_events'
          AND relation_definition.relkind IN ('r', 'p', 'v', 'm', 'f')
          AND (
              has_table_privilege(current_user, relation_definition.oid, 'SELECT')
              OR has_table_privilege(current_user, relation_definition.oid, 'INSERT')
              OR has_table_privilege(current_user, relation_definition.oid, 'UPDATE')
              OR has_table_privilege(current_user, relation_definition.oid, 'DELETE')
              OR has_table_privilege(current_user, relation_definition.oid, 'TRUNCATE')
              OR has_table_privilege(current_user, relation_definition.oid, 'REFERENCES')
              OR has_table_privilege(current_user, relation_definition.oid, 'TRIGGER')
              OR has_any_column_privilege(current_user, relation_definition.oid, 'SELECT')
              OR has_any_column_privilege(current_user, relation_definition.oid, 'INSERT')
              OR has_any_column_privilege(current_user, relation_definition.oid, 'UPDATE')
              OR has_any_column_privilege(current_user, relation_definition.oid, 'REFERENCES')
          )
    ) AS capabilities_safe
)
SELECT has_schema_privilege(current_user, 'tutorhub', 'USAGE')
       AND NOT has_schema_privilege(current_user, 'tutorhub', 'CREATE')
       AND NOT has_schema_privilege(current_user, 'public', 'CREATE')
       AND NOT has_database_privilege(
           current_user,
           current_database(),
           'CREATE'
       )
       AND has_table_privilege(
           current_user,
           'tutorhub.outbox_events',
           'SELECT'
       )
       AND NOT has_table_privilege(
           current_user,
           'tutorhub.outbox_events',
           'INSERT'
       )
       AND NOT has_table_privilege(
           current_user,
           'tutorhub.outbox_events',
           'DELETE'
       )
       AND NOT has_table_privilege(
           current_user,
           'tutorhub.outbox_events',
           'TRUNCATE'
       )
       AND NOT has_table_privilege(
           current_user,
           'tutorhub.outbox_events',
           'REFERENCES'
       )
       AND NOT has_any_column_privilege(
           current_user,
           'tutorhub.outbox_events',
           'REFERENCES'
       )
       AND NOT has_table_privilege(
           current_user,
           'tutorhub.outbox_events',
           'TRIGGER'
       )
       AND role_contract.capabilities_safe
       AND resource_contract.capabilities_safe
       AND column_contract.capabilities_safe
       AND other_relation_contract.capabilities_safe
FROM role_contract
CROSS JOIN resource_contract
CROSS JOIN column_contract
CROSS JOIN other_relation_contract`
