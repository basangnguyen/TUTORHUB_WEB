BEGIN;

ALTER TABLE tutorhub.tenants
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN archived_at timestamptz;

UPDATE tutorhub.tenants
SET archived_at = updated_at
WHERE status = 'archived' AND archived_at IS NULL;

ALTER TABLE tutorhub.tenants
    ADD CONSTRAINT tenants_version_positive CHECK (version > 0),
    ADD CONSTRAINT tenants_archive_consistent CHECK (
        (status = 'archived' AND archived_at IS NOT NULL)
        OR (status <> 'archived' AND archived_at IS NULL)
    );

ALTER TABLE tutorhub.sessions
    ADD COLUMN context_version bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT sessions_context_version_positive CHECK (context_version > 0);

CREATE INDEX memberships_user_active_tenant_idx
    ON tutorhub.memberships (user_id, tenant_id)
    WHERE status = 'active';

CREATE INDEX sessions_active_tenant_idx
    ON tutorhub.sessions (active_tenant_id)
    WHERE revoked_at IS NULL AND active_tenant_id IS NOT NULL;

COMMIT;
