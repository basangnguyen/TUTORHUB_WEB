BEGIN;

ALTER TABLE tutorhub.auth_challenge_deliveries
    DROP CONSTRAINT auth_challenge_deliveries_challenge_id_fkey,
    ALTER COLUMN challenge_id DROP NOT NULL,
    ADD CONSTRAINT auth_challenge_deliveries_challenge_id_fkey
        FOREIGN KEY (challenge_id) REFERENCES tutorhub.auth_challenges(id) ON DELETE SET NULL;

CREATE FUNCTION tutorhub.require_delivery_challenge() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, tutorhub AS $$
BEGIN
    IF NEW.challenge_id IS NULL THEN
        RAISE EXCEPTION 'a new delivery requires a challenge' USING ERRCODE = '23502';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER require_delivery_challenge BEFORE INSERT ON tutorhub.auth_challenge_deliveries
    FOR EACH ROW EXECUTE FUNCTION tutorhub.require_delivery_challenge();

-- Schema checks in 000048/49 tie each status to exactly one non-null retirement timestamp.
CREATE INDEX user_phone_numbers_retention_idx
    ON tutorhub.user_phone_numbers (COALESCE(released_at, removed_at), id)
    WHERE status IN ('released', 'removed');
CREATE INDEX mfa_recovery_codes_retention_idx
    ON tutorhub.mfa_recovery_codes (COALESCE(consumed_at, invalidated_at), id)
    WHERE COALESCE(consumed_at, invalidated_at) IS NOT NULL;

CREATE FUNCTION tutorhub.purge_expired_auth_artifacts(batch_size integer DEFAULT 500) RETURNS jsonb
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$
DECLARE
    as_of CONSTANT timestamptz := pg_catalog.clock_timestamp();
    n_auth_transactions integer;
    n_auth_challenge_deliveries integer;
    n_user_phone_numbers integer;
    n_mfa_recovery_codes integer;
    n_auth_flows integer;
    n_external_data_deletion_requests integer;
BEGIN
    IF batch_size IS NULL OR batch_size < 1 OR batch_size > 5000 THEN
        RAISE EXCEPTION 'batch_size must be between 1 and 5000' USING ERRCODE = '22023';
    END IF;

    WITH expired AS (
        SELECT id FROM tutorhub.auth_transactions
        WHERE expires_at <= as_of - interval '24 hours'
        ORDER BY expires_at, id LIMIT batch_size FOR UPDATE SKIP LOCKED
    )
    DELETE FROM tutorhub.auth_transactions AS artifact USING expired
    WHERE artifact.id = expired.id;
    GET DIAGNOSTICS n_auth_transactions = ROW_COUNT;

    WITH expired AS (
        SELECT id FROM tutorhub.auth_challenge_deliveries
        WHERE created_at <= as_of - interval '30 days'
        ORDER BY created_at, id LIMIT batch_size FOR UPDATE SKIP LOCKED
    )
    DELETE FROM tutorhub.auth_challenge_deliveries AS artifact USING expired
    WHERE artifact.id = expired.id;
    GET DIAGNOSTICS n_auth_challenge_deliveries = ROW_COUNT;

    WITH expired AS (
        SELECT id FROM tutorhub.user_phone_numbers
        WHERE status IN ('released', 'removed') AND COALESCE(released_at, removed_at) <= as_of - interval '90 days'
        ORDER BY COALESCE(released_at, removed_at), id LIMIT batch_size FOR UPDATE SKIP LOCKED
    )
    DELETE FROM tutorhub.user_phone_numbers AS artifact USING expired
    WHERE artifact.id = expired.id;
    GET DIAGNOSTICS n_user_phone_numbers = ROW_COUNT;

    WITH expired AS (
        SELECT id FROM tutorhub.mfa_recovery_codes
        WHERE COALESCE(consumed_at, invalidated_at) <= as_of - interval '30 days'
        ORDER BY COALESCE(consumed_at, invalidated_at), id LIMIT batch_size FOR UPDATE SKIP LOCKED
    )
    DELETE FROM tutorhub.mfa_recovery_codes AS artifact USING expired
    WHERE artifact.id = expired.id;
    GET DIAGNOSTICS n_mfa_recovery_codes = ROW_COUNT;

    WITH expired AS (
        SELECT state_hash FROM tutorhub.auth_flows
        WHERE expires_at <= as_of - interval '24 hours'
        ORDER BY expires_at, state_hash LIMIT batch_size FOR UPDATE SKIP LOCKED
    )
    DELETE FROM tutorhub.auth_flows AS artifact USING expired
    WHERE artifact.state_hash = expired.state_hash;
    GET DIAGNOSTICS n_auth_flows = ROW_COUNT;

    WITH expired AS (
        SELECT id FROM tutorhub.external_data_deletion_requests
        WHERE created_at <= as_of - interval '1 year'
        ORDER BY created_at, id LIMIT batch_size FOR UPDATE SKIP LOCKED
    )
    DELETE FROM tutorhub.external_data_deletion_requests AS artifact USING expired
    WHERE artifact.id = expired.id;
    GET DIAGNOSTICS n_external_data_deletion_requests = ROW_COUNT;

    RETURN pg_catalog.jsonb_build_object(
        'auth_transactions', n_auth_transactions,
        'auth_challenge_deliveries', n_auth_challenge_deliveries,
        'user_phone_numbers', n_user_phone_numbers,
        'mfa_recovery_codes', n_mfa_recovery_codes,
        'auth_flows', n_auth_flows,
        'external_data_deletion_requests', n_external_data_deletion_requests
    );
END;
$$;

-- The dedicated login has no table DML. Keep the historical, fixed-size receipt
-- entry point usable under that same boundary. Strip old grants before elevation.
REVOKE ALL ON FUNCTION tutorhub.require_delivery_challenge(),
    tutorhub.purge_expired_auth_artifacts(integer), tutorhub.purge_expired_deletion_receipts() FROM PUBLIC;
DO $$
DECLARE
    grant_row record;
BEGIN
    FOR grant_row IN
        SELECT DISTINCT p.oid::regprocedure AS proc, r.rolname
        FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a JOIN pg_roles r ON r.oid = a.grantee
        WHERE p.oid IN ('tutorhub.require_delivery_challenge()'::regprocedure,
                        'tutorhub.purge_expired_auth_artifacts(integer)'::regprocedure,
                        'tutorhub.purge_expired_deletion_receipts()'::regprocedure)
          AND a.grantee <> p.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I CASCADE', grant_row.proc, grant_row.rolname);
    END LOOP;
END;
$$;
ALTER FUNCTION tutorhub.purge_expired_deletion_receipts() SECURITY DEFINER;
ALTER FUNCTION tutorhub.purge_expired_deletion_receipts() SET search_path = pg_catalog, pg_temp;

-- PostgreSQL has no per-role DENY for PUBLIC EXECUTE. These historical CHECK
-- predicates and triggers otherwise give the maintenance role more than two
-- functions. Keep the predicate grants used by existing table/column writers.
DO $$
DECLARE
    helper record;
    writer record;
BEGIN
    FOR helper IN
        SELECT p.oid, p.oid::regprocedure AS proc, p.proowner, c.relname
        FROM (VALUES
            ('tutorhub.audit_metadata_is_redacted(jsonb)'::regprocedure, 'audit_events'),
            ('tutorhub.sso_org_ids_valid(text,text[])'::regprocedure, 'auth_sso_connections'),
            ('tutorhub.sso_school_domain_valid(text)'::regprocedure, 'auth_sso_connections'),
            ('tutorhub.sso_school_domain_valid(text)'::regprocedure, 'auth_sso_domains')
        ) AS c(proc, relname) JOIN pg_proc p ON p.oid = c.proc
        WHERE EXISTS (SELECT FROM aclexplode(COALESCE(p.proacl, acldefault('f',p.proowner))) a
                      WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
    LOOP
        FOR writer IN
            SELECT DISTINCT r.rolname
            FROM pg_class t JOIN pg_namespace n ON n.oid=t.relnamespace
            CROSS JOIN LATERAL (
                SELECT a.grantee FROM aclexplode(t.relacl) a WHERE a.privilege_type IN ('INSERT','UPDATE')
                UNION SELECT a.grantee FROM pg_attribute col CROSS JOIN LATERAL aclexplode(col.attacl) a
                      WHERE col.attrelid=t.oid AND a.privilege_type IN ('INSERT','UPDATE')
                UNION SELECT t.relowner
            ) AS w JOIN pg_roles r ON r.oid=w.grantee
            WHERE n.nspname='tutorhub' AND t.relname=helper.relname AND r.oid<>helper.proowner
        LOOP
            EXECUTE format('GRANT EXECUTE ON FUNCTION %s TO %I', helper.proc, writer.rolname);
        END LOOP;
    END LOOP;
END;
$$;
REVOKE EXECUTE ON FUNCTION
    tutorhub.audit_metadata_is_redacted(jsonb), tutorhub.sso_org_ids_valid(text,text[]),
    tutorhub.sso_school_domain_valid(text), tutorhub.reject_audit_event_mutation(),
    tutorhub.enforce_working_schedule_weekly_interval(), tutorhub.enforce_working_schedule_exception(),
    tutorhub.enforce_working_schedule_exception_interval(), tutorhub.normalize_class_session_participation_defaults(),
    tutorhub.enforce_class_session_attendee_limit(), tutorhub.reject_calendar_invitation_snapshot_mutation()
    FROM PUBLIC;

COMMIT;
