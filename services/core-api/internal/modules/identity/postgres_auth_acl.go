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
	"auth_sso_connections":            {"status", "allowed_org_ids", "enforce_sso", "auto_join_role", "updated_at"},
	"auth_sso_domains":                {"status", "verification_token_hash", "verified_at", "updated_at"},
	"user_phone_numbers":              {"e164_ciphertext", "status", "release_reason", "last_used_at", "released_at", "removed_at", "updated_at"},
	"auth_transactions":               {"state", "completed_at", "payload_ciphertext"},
	"auth_challenges":                 {"attempts", "consumed_at", "invalidated_at"},
	"auth_challenge_deliveries":       {"status", "provider_message_ref", "error_code", "delivered_at"},
	"user_password_credentials":       {"password_hash", "hash_version", "status", "failed_attempts", "last_failed_at", "locked_until", "compromised_at", "updated_at"},
	"user_legal_acceptances":          {},
	"external_data_deletion_requests": {"status", "completed_at"},
	"mfa_totp_factors":                {"status", "last_used_step", "confirmed_at", "disabled_at", "updated_at"},
	"mfa_recovery_codes":              {"consumed_at", "invalidated_at"},
	"webauthn_credentials":            {"sign_count", "clone_warning", "backup_eligible", "backup_state", "transports", "label", "status", "last_used_at", "revoked_at", "updated_at"},
}

// ProvisionAuthFoundationRuntimeACL is an owner-only provisioning operation.
// It is deliberately never called by API startup. The caller must supply the
// environment's runtime role, using a separate privileged database connection.
// The dedicated runtime role must have no role memberships, including NOINHERIT
// memberships: SET ROLE could otherwise bypass the effective ACL checks below.
// Existing read/insert grants remain; federation updates are restricted to
// mutable fields. Expired flow cleanup belongs to the maintenance login.
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
		[]string{"auth_transactions", "auth_challenges", "auth_challenge_deliveries", "user_password_credentials", "user_legal_acceptances", "external_data_deletion_requests", "webauthn_credentials", "mfa_totp_factors", "mfa_recovery_codes", "user_phone_numbers", "auth_sso_connections", "auth_sso_domains", "users", "sessions", "identities", "auth_flows"}).Scan(&safe)
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
	// Preserve the established users ACL, including deployments with column grants.
	if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT SELECT (email_verified_at,webauthn_user_handle), INSERT (email_verified_at,webauthn_user_handle), UPDATE (email_verified_at,webauthn_user_handle) ON tutorhub.users TO %s", role)); err != nil {
		return errors.New("grant user authentication columns")
	}
	var readable, insertable, writable bool
	if err := tx.QueryRow(ctx, `SELECT bool_and(has_column_privilege($1,'tutorhub.users',c,'SELECT')),
        bool_and(has_column_privilege($1,'tutorhub.users',c,'INSERT')),
        bool_and(has_column_privilege($1,'tutorhub.users',c,'UPDATE'))
        FROM unnest(ARRAY['email_verified_at','webauthn_user_handle']) AS cols(c)`, runtimeRole).Scan(&readable, &insertable, &writable); err != nil || !readable || !insertable || !writable {
		return errors.New("user authentication column privileges are unavailable")
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
        AND has_column_privilege($1,'tutorhub.sessions','persistent','UPDATE')
        AND NOT has_column_privilege($1,'tutorhub.sessions','assurance_level','UPDATE')`, runtimeRole).Scan(&sessionDelete, &sessionMetadataOK); err != nil || sessionDelete || !sessionMetadataOK {
		return errors.New("session runtime privileges are invalid")
	}
	if err := provisionFederationColumnACL(ctx, tx, runtimeRole); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "REVOKE DELETE ON tutorhub.auth_flows FROM PUBLIC, "+role); err != nil {
		return errors.New("revoke direct authentication flow cleanup")
	}
	// These immutable CHECK predicates are needed for SSO and audit writes.
	if _, err := tx.Exec(ctx, "GRANT EXECUTE ON FUNCTION tutorhub.sso_org_ids_valid(text,text[]), tutorhub.sso_school_domain_valid(text), tutorhub.audit_metadata_is_redacted(jsonb) TO "+role); err != nil {
		return errors.New("grant authentication constraint predicates")
	}
	if _, err := tx.Exec(ctx, "REVOKE ALL ON FUNCTION "+eraseProviderProcedure+" FROM PUBLIC, "+role); err != nil {
		return errors.New("revoke broad provider erasure grants")
	}
	if _, err := tx.Exec(ctx, "GRANT EXECUTE ON FUNCTION "+eraseProviderProcedure+" TO "+role); err != nil {
		return errors.New("grant narrowly scoped provider erasure")
	}
	var erasureACL bool
	if err := tx.QueryRow(ctx, `SELECT p.prosecdef AND p.proowner=(SELECT relowner FROM pg_class WHERE oid='tutorhub.identities'::regclass)
	    AND p.proconfig=ARRAY['search_path=pg_catalog, tutorhub']
	    AND has_function_privilege($1,p.oid,'EXECUTE') AND NOT has_function_privilege($1,p.oid,'EXECUTE WITH GRANT OPTION')
	    AND NOT EXISTS (SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) acl
	                    WHERE acl.grantee <> p.proowner AND acl.grantee <> (SELECT oid FROM pg_roles WHERE rolname=$1))
	    AND NOT has_function_privilege($1,'tutorhub.clear_finished_federation_payload()','EXECUTE')
	    AND NOT has_function_privilege($1,'tutorhub.purge_expired_deletion_receipts()','EXECUTE')
	    AND NOT has_function_privilege($1,'tutorhub.purge_expired_auth_artifacts(integer)','EXECUTE')
	    AND NOT has_function_privilege($1,'tutorhub.require_delivery_challenge()','EXECUTE')
	    AND NOT has_table_privilege($1,'tutorhub.auth_flows','DELETE')
	    AND NOT has_table_privilege($1,'tutorhub.users','DELETE')
	    AND NOT has_table_privilege($1,'tutorhub.identities','DELETE')
	    AND NOT has_column_privilege($1,'tutorhub.identities','subject','UPDATE')
	    FROM pg_proc p WHERE p.oid=$2::regprocedure`, runtimeRole, eraseProviderProcedure).Scan(&erasureACL); err != nil || !erasureACL {
		return errors.New("provider erasure ACL or owner invalid")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("commit authentication ACL provisioning")
	}
	return nil
}

const eraseProviderProcedure = "tutorhub.erase_provider_identity(text,text,text,bytea,timestamptz)"

// ProvisionAuthMaintenanceACL runs on the migration owner's connection. It
// replaces the dedicated role's schema privileges in a single transaction.
func ProvisionAuthMaintenanceACL(ctx context.Context, database Database, maintenanceRole string) error {
	if database == nil || strings.TrimSpace(maintenanceRole) == "" {
		return errors.New("authentication maintenance requires a database and role")
	}
	tx, err := database.Begin(ctx)
	if err != nil {
		return errors.New("begin authentication maintenance provisioning")
	}
	defer rollback(tx)

	var safe bool
	if err := tx.QueryRow(ctx, `SELECT
		r.rolname <> current_user AND NOT (r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls)
		AND NOT EXISTS (SELECT FROM pg_auth_members WHERE member=r.oid)
		AND NOT EXISTS (SELECT FROM pg_database WHERE datname=current_database() AND datdba=r.oid)
		AND NOT EXISTS (SELECT FROM pg_namespace WHERE nspname='tutorhub' AND nspowner=r.oid)
		AND NOT EXISTS (SELECT FROM pg_class WHERE relnamespace='tutorhub'::regnamespace AND relowner=r.oid)
		AND NOT EXISTS (SELECT FROM pg_proc WHERE pronamespace='tutorhub'::regnamespace AND proowner=r.oid)
		AND current_user=pg_get_userbyid((SELECT proowner FROM pg_proc
		    WHERE oid='tutorhub.purge_expired_auth_artifacts(integer)'::regprocedure))
		FROM pg_roles r WHERE r.rolname=$1`, maintenanceRole).Scan(&safe); err != nil || !safe {
		return errors.New("authentication maintenance role safety preflight failed")
	}
	role := pgx.Identifier{maintenanceRole}.Sanitize()
	for _, statement := range []string{
		"REVOKE ALL ON SCHEMA tutorhub FROM " + role,
		"REVOKE ALL ON ALL TABLES IN SCHEMA tutorhub FROM " + role,
		"REVOKE ALL ON ALL SEQUENCES IN SCHEMA tutorhub FROM " + role,
		"REVOKE ALL ON ALL ROUTINES IN SCHEMA tutorhub FROM " + role,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return errors.New("revoke prior authentication maintenance grants")
		}
	}
	// Include column-only grants, even on tables outside the identity module.
	rows, err := tx.Query(ctx, `SELECT format('REVOKE ALL (%s) ON TABLE %I.%I FROM %I',
		string_agg(quote_ident(a.attname), ', ' ORDER BY a.attnum), 'tutorhub', c.relname, $1::text)
		FROM pg_class c JOIN pg_attribute a ON a.attrelid=c.oid
		WHERE c.relnamespace='tutorhub'::regnamespace AND a.attnum>0 AND NOT a.attisdropped
		  AND EXISTS (SELECT FROM aclexplode(a.attacl) acl JOIN pg_roles r ON r.oid=acl.grantee WHERE r.rolname=$1)
		GROUP BY c.relname`, maintenanceRole)
	if err != nil {
		return errors.New("inspect prior maintenance column grants")
	}
	statements, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return errors.New("read prior maintenance column grants")
	}
	statements = append(statements, "GRANT USAGE ON SCHEMA tutorhub TO "+role,
		"GRANT EXECUTE ON FUNCTION tutorhub.purge_expired_auth_artifacts(integer), tutorhub.purge_expired_deletion_receipts() TO "+role)
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return errors.New("set exact authentication maintenance grants")
		}
	}
	var exact bool
	if err := tx.QueryRow(ctx, `SELECT
		has_schema_privilege(r.oid, 'tutorhub', 'USAGE')
		AND NOT has_schema_privilege(r.oid, 'tutorhub', 'CREATE,USAGE WITH GRANT OPTION')
		AND NOT EXISTS (
			SELECT FROM pg_class c WHERE c.relnamespace='tutorhub'::regnamespace
			AND c.relkind IN ('r','p','v','m','f') AND (
				has_table_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER,MAINTAIN')
				OR has_any_column_privilege(r.oid,c.oid,'SELECT,INSERT,UPDATE,REFERENCES')
			)
		)
		AND NOT EXISTS (SELECT FROM pg_class c WHERE c.relnamespace='tutorhub'::regnamespace
			AND CASE WHEN c.relkind='S' THEN has_sequence_privilege(r.oid,c.oid,'USAGE,SELECT,UPDATE') ELSE false END)
		AND NOT EXISTS (
			SELECT FROM pg_proc p WHERE p.pronamespace='tutorhub'::regnamespace AND (
				has_function_privilege(r.oid,p.oid,'EXECUTE') <> (p.oid IN (
					'tutorhub.purge_expired_auth_artifacts(integer)'::regprocedure,
					'tutorhub.purge_expired_deletion_receipts()'::regprocedure))
				OR has_function_privilege(r.oid,p.oid,'EXECUTE WITH GRANT OPTION')
			)
		)
		AND NOT EXISTS (
			SELECT FROM pg_proc p WHERE p.oid IN ('tutorhub.purge_expired_auth_artifacts(integer)'::regprocedure,
				'tutorhub.purge_expired_deletion_receipts()'::regprocedure)
			AND (NOT p.prosecdef OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
				OR EXISTS (SELECT FROM pg_class c WHERE c.relnamespace='tutorhub'::regnamespace
					AND c.relname IN ('auth_transactions','auth_challenges','auth_challenge_deliveries',
						'user_phone_numbers','mfa_recovery_codes','auth_flows','external_data_deletion_requests')
					AND c.relowner<>p.proowner))
		)
		FROM pg_roles r WHERE r.rolname=$1`, maintenanceRole).Scan(&exact); err != nil || !exact {
		return errors.New("authentication maintenance privileges or definer ownership are not exact")
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.New("commit authentication maintenance provisioning")
	}
	return nil
}

func provisionFederationColumnACL(ctx context.Context, tx pgx.Tx, runtimeRole string) error {
	role := pgx.Identifier{runtimeRole}.Sanitize()
	for _, entry := range []struct {
		table               string
		foundation, updates []string
	}{
		{"auth_flows", []string{"provider"}, []string{"consumed_at"}},
		{"identities", []string{"kind", "provider_slug", "provider_org_id", "provider_stable_id"},
			[]string{"status", "unlinked_at", "email_at_provider", "email_verified", "last_authenticated_at", "updated_at"}},
	} {
		columns, err := authRelationColumns(ctx, tx, entry.table)
		if err != nil {
			return err
		}
		relation := pgx.Identifier{"tutorhub", entry.table}.Sanitize()
		// A prior table-level UPDATE would implicitly permit changing provider or
		// identity ownership. Remove both table and column UPDATE before granting
		// the exact mutable set; preserve established reads/inserts and cleanup.
		for _, grantee := range []string{role, "PUBLIC"} {
			if _, err := tx.Exec(ctx, fmt.Sprintf("REVOKE UPDATE ON TABLE %s FROM %s", relation, grantee)); err != nil {
				return errors.New("revoke broad federation updates")
			}
			if _, err := tx.Exec(ctx, fmt.Sprintf("REVOKE UPDATE (%s) ON TABLE %s FROM %s", authQuotedColumns(columns), relation, grantee)); err != nil {
				return errors.New("revoke federation column updates")
			}
		}
		foundation := authQuotedColumns(entry.foundation)
		if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT SELECT (%[1]s), INSERT (%[1]s) ON TABLE %[2]s TO %[3]s", foundation, relation, role)); err != nil {
			return errors.New("grant federation metadata read and insert")
		}
		if _, err := tx.Exec(ctx, fmt.Sprintf("GRANT UPDATE (%s) ON TABLE %s TO %s", authQuotedColumns(entry.updates), relation, role)); err != nil {
			return errors.New("grant mutable federation state updates")
		}
		for _, column := range columns {
			var read, insert, update, grantOption bool
			if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1,$2,$3,'SELECT'),
                has_column_privilege($1,$2,$3,'INSERT'),has_column_privilege($1,$2,$3,'UPDATE'),
                has_column_privilege($1,$2,$3,'UPDATE WITH GRANT OPTION')`, runtimeRole, "tutorhub."+entry.table, column).
				Scan(&read, &insert, &update, &grantOption); err != nil || grantOption || update != slices.Contains(entry.updates, column) ||
				(slices.Contains(entry.foundation, column) && (!read || !insert)) {
				return errors.New("federation runtime column privileges are not exact")
			}
		}
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
