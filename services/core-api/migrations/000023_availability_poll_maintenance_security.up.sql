BEGIN;

-- Forward-only correction for the 000022 invoker function. The function body
-- remains bounded and has no dynamic SQL; the environment-specific maintenance
-- role ACL is re-provisioned separately and is not hardcoded in a migration.
CREATE OR REPLACE FUNCTION tutorhub.purge_expired_availability_polls(batch_size integer DEFAULT 100)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    candidate record;
    deleted_count integer := 0;
    row_count integer;
BEGIN
    IF batch_size IS NULL OR batch_size < 1 OR batch_size > 1000 THEN
        RAISE EXCEPTION 'batch_size must be between 1 and 1000'
            USING ERRCODE = '22023';
    END IF;

    FOR candidate IN
        SELECT poll.tenant_id, poll.id
        FROM tutorhub.availability_polls AS poll
        WHERE poll.retention_until <= pg_catalog.clock_timestamp()
        ORDER BY poll.retention_until, poll.tenant_id, poll.id
        FOR UPDATE SKIP LOCKED
        LIMIT batch_size
    LOOP
        UPDATE tutorhub.study_meetings
        SET source_poll_id = NULL
        WHERE tenant_id = candidate.tenant_id
          AND source_poll_id = candidate.id;

        DELETE FROM tutorhub.availability_polls
        WHERE tenant_id = candidate.tenant_id
          AND id = candidate.id;
        GET DIAGNOSTICS row_count = ROW_COUNT;
        deleted_count := deleted_count + row_count;
    END LOOP;

    RETURN deleted_count;
END;
$$;

-- Reassert the deny-by-default boundary in the same transaction. The
-- environment-specific maintenance EXECUTE grant is applied by provisioning;
-- direct table grants are revoked there because this function is the sole entry
-- point for the purge operation.
REVOKE ALL ON FUNCTION tutorhub.purge_expired_availability_polls(integer) FROM PUBLIC;

COMMENT ON FUNCTION tutorhub.purge_expired_availability_polls(integer) IS
    'Bounded hard-retention purge; SECURITY DEFINER owner is reviewed separately and caller receives only EXECUTE.';

COMMIT;
