BEGIN;

DROP INDEX IF EXISTS tutorhub.sessions_active_tenant_idx;
DROP INDEX IF EXISTS tutorhub.memberships_user_active_tenant_idx;

ALTER TABLE tutorhub.sessions
    DROP CONSTRAINT IF EXISTS sessions_context_version_positive,
    DROP COLUMN IF EXISTS context_version;

ALTER TABLE tutorhub.tenants
    DROP CONSTRAINT IF EXISTS tenants_archive_consistent,
    DROP CONSTRAINT IF EXISTS tenants_version_positive,
    DROP COLUMN IF EXISTS archived_at,
    DROP COLUMN IF EXISTS version;

COMMIT;
