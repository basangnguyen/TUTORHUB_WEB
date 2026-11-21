BEGIN;

LOCK TABLE tutorhub.auth_challenges, tutorhub.auth_challenge_deliveries IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT FROM tutorhub.auth_challenge_deliveries WHERE challenge_id IS NULL) THEN
        RAISE EXCEPTION 'AUTH-13A rollback blocked: retained delivery receipts have no challenge'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP FUNCTION tutorhub.purge_expired_auth_artifacts(integer);
DROP TRIGGER require_delivery_challenge ON tutorhub.auth_challenge_deliveries;
DROP FUNCTION tutorhub.require_delivery_challenge();
DROP INDEX tutorhub.mfa_recovery_codes_retention_idx;
DROP INDEX tutorhub.user_phone_numbers_retention_idx;
ALTER TABLE tutorhub.auth_challenge_deliveries
    DROP CONSTRAINT auth_challenge_deliveries_challenge_id_fkey,
    ALTER COLUMN challenge_id SET NOT NULL,
    ADD CONSTRAINT auth_challenge_deliveries_challenge_id_fkey
        FOREIGN KEY (challenge_id) REFERENCES tutorhub.auth_challenges(id) ON DELETE CASCADE;
ALTER FUNCTION tutorhub.purge_expired_deletion_receipts() SECURITY INVOKER;
ALTER FUNCTION tutorhub.purge_expired_deletion_receipts() SET search_path = pg_catalog, tutorhub;
-- Preserve the restricted PUBLIC ACL and explicit predicate-writer grants.
-- Restoring blanket PUBLIC EXECUTE could reopen privileges denied before 51.
COMMIT;
