BEGIN;

ALTER TABLE tutorhub.classes
    ADD COLUMN timezone text,
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD COLUMN archived_from_status text;

UPDATE tutorhub.classes AS class
SET timezone = tenant.timezone
FROM tutorhub.tenants AS tenant
WHERE tenant.id = class.tenant_id;

UPDATE tutorhub.classes
SET archived_from_status = 'active'
WHERE status = 'archived';

ALTER TABLE tutorhub.classes
    ALTER COLUMN timezone SET NOT NULL,
    DROP CONSTRAINT classes_archive_state_valid,
    ADD CONSTRAINT classes_code_format_valid
        CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{2,31}$'),
    ADD CONSTRAINT classes_description_length_valid
        CHECK (length(description) <= 4000),
    ADD CONSTRAINT classes_timezone_valid
        CHECK (
            timezone = btrim(timezone)
            AND length(timezone) BETWEEN 1 AND 100
            AND lower(timezone) <> 'local'
        ),
    ADD CONSTRAINT classes_version_positive CHECK (version > 0),
    ADD CONSTRAINT classes_archived_from_status_valid
        CHECK (
            (status = 'archived'
                AND archived_at IS NOT NULL
                AND archived_from_status IN ('draft', 'active'))
            OR
            (status <> 'archived'
                AND archived_at IS NULL
                AND archived_from_status IS NULL)
        );

DROP INDEX tutorhub.classes_tenant_status_created_idx;

CREATE INDEX classes_tenant_status_created_id_idx
    ON tutorhub.classes (tenant_id, status, created_at DESC, id DESC);

CREATE INDEX classes_tenant_created_id_idx
    ON tutorhub.classes (tenant_id, created_at DESC, id DESC);

COMMIT;
