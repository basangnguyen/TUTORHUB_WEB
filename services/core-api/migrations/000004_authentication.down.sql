BEGIN;

DROP TABLE IF EXISTS tutorhub.auth_flows;

DROP INDEX IF EXISTS tutorhub.sessions_identity_idx;

ALTER TABLE tutorhub.sessions
    DROP CONSTRAINT IF EXISTS sessions_revocation_consistent,
    DROP CONSTRAINT IF EXISTS sessions_absolute_after_creation,
    DROP CONSTRAINT IF EXISTS sessions_idle_before_absolute,
    DROP COLUMN IF EXISTS revoked_reason,
    DROP COLUMN IF EXISTS auth_time,
    DROP COLUMN IF EXISTS absolute_expires_at,
    DROP COLUMN IF EXISTS identity_id;

ALTER TABLE tutorhub.identities
    DROP COLUMN IF EXISTS last_authenticated_at,
    DROP COLUMN IF EXISTS email_verified;

COMMIT;
