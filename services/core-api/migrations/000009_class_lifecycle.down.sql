BEGIN;

DROP INDEX IF EXISTS tutorhub.classes_tenant_created_id_idx;
DROP INDEX IF EXISTS tutorhub.classes_tenant_status_created_id_idx;

CREATE INDEX classes_tenant_status_created_idx
    ON tutorhub.classes (tenant_id, status, created_at DESC);

ALTER TABLE tutorhub.classes
    DROP CONSTRAINT IF EXISTS classes_archived_from_status_valid,
    DROP CONSTRAINT IF EXISTS classes_version_positive,
    DROP CONSTRAINT IF EXISTS classes_timezone_valid,
    DROP CONSTRAINT IF EXISTS classes_description_length_valid,
    DROP CONSTRAINT IF EXISTS classes_code_format_valid,
    ADD CONSTRAINT classes_archive_state_valid CHECK (
        (status = 'archived' AND archived_at IS NOT NULL)
        OR (status <> 'archived' AND archived_at IS NULL)
    ),
    DROP COLUMN IF EXISTS archived_from_status,
    DROP COLUMN IF EXISTS version,
    DROP COLUMN IF EXISTS timezone;

COMMIT;
