BEGIN;

-- Serialize with new writes so the preflight cannot race a newly created proof.
LOCK TABLE tutorhub.identities, tutorhub.sessions, tutorhub.auth_transactions,
    tutorhub.auth_challenges, tutorhub.auth_challenge_deliveries IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tutorhub.identities WHERE kind <> 'oidc'
        OR (provider_slug IS NOT NULL AND provider_slug <> 'oidc')
        OR provider_org_id IS NOT NULL OR provider_stable_id IS NOT NULL) THEN
        RAISE EXCEPTION 'AUTH-01 rollback blocked: retained non-legacy identity data';
    END IF;
    IF EXISTS (SELECT 1 FROM tutorhub.sessions WHERE auth_methods <> ARRAY['fed']::text[]
        OR assurance_level <> 1 OR reauthenticated_at IS NOT NULL OR device_label IS NOT NULL OR persistent) THEN
        RAISE EXCEPTION 'AUTH-01 rollback blocked: retained authentication assurance or device data';
    END IF;
    IF EXISTS (SELECT 1 FROM tutorhub.auth_transactions WHERE expires_at > CURRENT_TIMESTAMP
        OR state = 'completed' OR completed_at IS NOT NULL OR payload_ciphertext IS NOT NULL
        OR user_id IS NOT NULL OR session_id IS NOT NULL)
        OR EXISTS (SELECT 1 FROM tutorhub.auth_challenges WHERE deliverable OR consumed_at IS NOT NULL)
        OR EXISTS (SELECT 1 FROM tutorhub.auth_challenge_deliveries) THEN
        RAISE EXCEPTION 'AUTH-01 rollback blocked: active transactions or retained authentication/delivery data; use a reviewed forward migration';
    END IF;
END $$;

DROP TABLE tutorhub.auth_challenge_deliveries;
DROP TABLE tutorhub.auth_challenges;
DROP TABLE tutorhub.auth_transactions;
ALTER TABLE tutorhub.sessions
    DROP COLUMN persistent, DROP COLUMN device_label, DROP COLUMN reauthenticated_at,
    DROP COLUMN assurance_level, DROP COLUMN auth_methods;
ALTER TABLE tutorhub.identities
    DROP COLUMN provider_stable_id, DROP COLUMN provider_org_id, DROP COLUMN provider_slug, DROP COLUMN kind;

COMMIT;
