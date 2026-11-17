package identity

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

var authFoundationUpdates = map[string][]string{
	"auth_transactions":         {"state", "completed_at", "payload_ciphertext"},
	"auth_challenges":           {"attempts", "consumed_at", "invalidated_at"},
	"auth_challenge_deliveries": {"status", "provider_message_ref", "error_code", "delivered_at"},
	"user_password_credentials": {"password_hash", "hash_version", "status", "failed_attempts", "last_failed_at", "locked_until", "compromised_at", "updated_at"},
	"user_legal_acceptances":    {},
}

// ProvisionAuthFoundationRuntimeACL is an owner-only provisioning operation.
// It is deliberately never called by API startup. The caller must supply the
// environment's runtime role, using a separate privileged database connection.
// The dedicated runtime role must have no role memberships, including NOINHERIT
// memberships: SET ROLE could otherwise bypass the effective ACL checks below.
// Existing identities/sessions grants are left unchanged.
func ProvisionAuthFoundationRuntimeACL(ctx context.Context, database Database, runtimeRole string) error {
	if database == nil || strings.TrimSpace(runtimeRole) == "" {
		return errors.New("authentication ACL provisioning requires database and runtime role")
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return errors.New("begin authentication ACL provisioning")
	}
	defer rollback(tx)
	var safe bool
	err = tx.QueryRow(ctx, `SELECT
    NOT r.rolsuper AND NOT r.rolcreaterole AND NOT r.rolcreatedb
    AND NOT r.rolreplication AND NOT r.rolbypassrls
    AND r.rolname <> current_user
    AND NOT EXISTS (SELECT 1 FROM pg_auth_members m WHERE m.member = r.oid)
    AND NOT pg_has_role(r.oid, (SELECT oid FROM pg_roles WHERE rolname = current_user), 'MEMBER')
    AND NOT EXISTS (
        SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'tutorhub' AND c.relname = ANY($2::text[])
        AND pg_has_role(r.oid, c.relowner, 'MEMBER')
    )
FROM pg_roles r WHERE r.rolname = $1`, runtimeRole,
		[]string{"auth_transactions", "auth_challenges", "auth_challenge_deliveries", "user_password_credentials", "user_legal_acceptances", "users", "sessions", "identities"}).Scan(&safe)
	if err != nil || !safe {
		return errors.New("authentication ACL runtime role safety preflight failed")
	}
	role := pgx.Identifier{runtimeRole}.Sanitize()
	for table, updateColumns := range authFoundationUpdates {
		columns, err := authRelationColumns(ctx, tx, table)
		if err != nil {
			return err
		}
		relation := pgx.Identifier{"tutorhub", table}.Sanitize()
		all := authQuotedColumns(columns)
		for _, grantee := range []string{role, "PUBLIC"} {
			statements := []string{
				fmt.Sprintf("REVOKE ALL PRIVILEGES ON TABLE %s FROM %s", relation, grantee),
				fmt.Sprintf("REVOKE SELECT (%[1]s), INSERT (%[1]s), UPDATE (%[1]s), REFERENCES (%[1]s) ON TABLE %[2]s FROM %[3]s", all, relation, grantee),
			}
			for _, statement := range statements {
				if _, err := tx.Exec(ctx, statement); err != nil {
					return errors.New("revoke authentication artifact privileges")
				}
			}
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT SELECT, INSERT ON TABLE %s TO %s", relation, role)); err != nil {
			return errors.New("grant authentication artifact read and insert")
		}
		if len(updateColumns) > 0 {
			if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT UPDATE (%s) ON TABLE %s TO %s", authQuotedColumns(updateColumns), relation, role)); err != nil {
				return errors.New("grant authentication artifact state updates")
			}
		}
		// Detect inherited permissions too: revoking direct grants alone does not
		// establish an exact ACL when a role is a member of a broader role.
		for _, privilege := range []string{"UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER", "SELECT WITH GRANT OPTION", "INSERT WITH GRANT OPTION"} {
			var granted bool
			if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, $2, $3)`, runtimeRole, "tutorhub."+table, privilege).Scan(&granted); err != nil || granted {
				return errors.New("authentication runtime has excessive table privileges")
			}
		}
		for _, column := range columns {
			var update, grantOption, reference, selectGrant, insertGrant bool
			if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1, $2, $3, 'UPDATE'),
                has_column_privilege($1, $2, $3, 'UPDATE WITH GRANT OPTION'),
                has_column_privilege($1, $2, $3, 'REFERENCES'),
                has_column_privilege($1, $2, $3, 'SELECT WITH GRANT OPTION'),
                has_column_privilege($1, $2, $3, 'INSERT WITH GRANT OPTION')`, runtimeRole, "tutorhub."+table, column).
				Scan(&update, &grantOption, &reference, &selectGrant, &insertGrant); err != nil || grantOption || reference || selectGrant || insertGrant || update != slices.Contains(updateColumns, column) {
				return errors.New("authentication runtime column privileges are not exact")
			}
		}
	}
	// Preserve the established users ACL. Only add the new verification column
	// permissions so deployments using column grants can insert native users too.
	if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT SELECT (email_verified_at), INSERT (email_verified_at), UPDATE (email_verified_at) ON tutorhub.users TO %s", role)); err != nil {
		return errors.New("grant user email verification column")
	}
	var readable, insertable, writable bool
	if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1,'tutorhub.users','email_verified_at','SELECT'),
        has_column_privilege($1,'tutorhub.users','email_verified_at','INSERT'),
        has_column_privilege($1,'tutorhub.users','email_verified_at','UPDATE')`, runtimeRole).Scan(&readable, &insertable, &writable); err != nil || !readable || !insertable || !writable {
		return errors.New("user email verification column privileges are unavailable")
	}
	// Preserve established sessions grants while ensuring deployments using column
	// grants can persist device_label and mutate reauth/method/persistence columns.
	if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT SELECT (auth_methods, assurance_level, reauthenticated_at, device_label, persistent), INSERT (auth_methods, assurance_level, reauthenticated_at, device_label, persistent), UPDATE (auth_methods, reauthenticated_at, persistent) ON tutorhub.sessions TO %s", role)); err != nil {
		return errors.New("grant session authentication foundation columns")
	}
	var sessionDelete, sessionMetadataOK bool
	if err := tx.QueryRow(ctx, `SELECT
        has_table_privilege($1,'tutorhub.sessions','DELETE'),
        has_column_privilege($1,'tutorhub.sessions','device_label','SELECT')
        AND has_column_privilege($1,'tutorhub.sessions','device_label','INSERT')
        AND has_column_privilege($1,'tutorhub.sessions','reauthenticated_at','SELECT')
        AND has_column_privilege($1,'tutorhub.sessions','reauthenticated_at','INSERT')
        AND has_column_privilege($1,'tutorhub.sessions','reauthenticated_at','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','auth_methods','UPDATE')
        AND has_column_privilege($1,'tutorhub.sessions','persistent','UPDATE')`, runtimeRole).Scan(&sessionDelete, &sessionMetadataOK); err != nil || sessionDelete || !sessionMetadataOK {
		return errors.New("session runtime privileges are invalid")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("commit authentication ACL provisioning")
	}
	return nil
}

func authRelationColumns(ctx context.Context, tx pgx.Tx, table string) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT a.attname FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'tutorhub' AND c.relname = $1 AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`, table)
	if err != nil {
		return nil, errors.New("inspect authentication artifact columns")
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			return nil, errors.New("read authentication artifact columns")
		}
		columns = append(columns, column)
	}
	if rows.Err() != nil || len(columns) == 0 {
		return nil, errors.New("authentication artifact table is unavailable")
	}
	return columns, nil
}

func authQuotedColumns(columns []string) string {
	quoted := make([]string, len(columns))
	for index, column := range columns {
		quoted[index] = pgx.Identifier{column}.Sanitize()
	}
	return strings.Join(quoted, ", ")
}
