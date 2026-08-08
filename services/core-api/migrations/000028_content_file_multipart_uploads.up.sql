BEGIN;

CREATE TABLE tutorhub.content_file_multipart_uploads (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    file_id uuid NOT NULL,
    creator_user_id uuid NOT NULL,
    provider_upload_id text NOT NULL,
    expected_file_version bigint NOT NULL,
    status text NOT NULL DEFAULT 'active',
    expires_at timestamptz NOT NULL,
    completed_storage_version_id text,
    completed_etag text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    aborted_at timestamptz,
    CONSTRAINT content_file_multipart_uploads_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT content_file_multipart_uploads_file_fk
        FOREIGN KEY (tenant_id, file_id)
        REFERENCES tutorhub.content_files (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT content_file_multipart_uploads_creator_membership_fk
        FOREIGN KEY (tenant_id, creator_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT content_file_multipart_uploads_provider_unique
        UNIQUE (tenant_id, provider_upload_id),
    CONSTRAINT content_file_multipart_uploads_provider_id_valid CHECK (
        length(btrim(provider_upload_id)) BETWEEN 1 AND 2048
        AND provider_upload_id !~ '[[:cntrl:]]'
    ),
    CONSTRAINT content_file_multipart_uploads_version_positive CHECK (
        expected_file_version > 0
    ),
    CONSTRAINT content_file_multipart_uploads_status_valid CHECK (
        status IN ('active', 'completing', 'completed', 'aborted', 'expired')
    ),
    CONSTRAINT content_file_multipart_uploads_time_valid CHECK (
        expires_at > created_at AND updated_at >= created_at
    ),
    CONSTRAINT content_file_multipart_uploads_lifecycle_consistent CHECK (
        (
            status IN ('active', 'completing')
            AND completed_storage_version_id IS NULL
            AND completed_etag IS NULL
            AND completed_at IS NULL
            AND aborted_at IS NULL
        )
        OR (
            status = 'completed'
            AND length(btrim(completed_storage_version_id)) BETWEEN 1 AND 512
            AND length(btrim(completed_etag)) BETWEEN 1 AND 512
            AND completed_at IS NOT NULL
            AND completed_at >= created_at
            AND completed_at <= updated_at
            AND aborted_at IS NULL
        )
        OR (
            status IN ('aborted', 'expired')
            AND completed_storage_version_id IS NULL
            AND completed_etag IS NULL
            AND completed_at IS NULL
            AND aborted_at IS NOT NULL
            AND aborted_at >= created_at
            AND aborted_at <= updated_at
        )
    )
);

CREATE UNIQUE INDEX content_file_multipart_uploads_one_active_idx
    ON tutorhub.content_file_multipart_uploads (tenant_id, file_id)
    WHERE status IN ('active', 'completing');

CREATE INDEX content_file_multipart_uploads_expiry_idx
    ON tutorhub.content_file_multipart_uploads (expires_at, tenant_id, id)
    WHERE status IN ('active', 'completing');

CREATE TABLE tutorhub.content_file_multipart_parts (
    tenant_id uuid NOT NULL,
    multipart_upload_id uuid NOT NULL,
    part_number integer NOT NULL,
    content_length_bytes bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, multipart_upload_id, part_number),
    CONSTRAINT content_file_multipart_parts_upload_fk
        FOREIGN KEY (tenant_id, multipart_upload_id)
        REFERENCES tutorhub.content_file_multipart_uploads (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT content_file_multipart_parts_number_valid CHECK (
        part_number BETWEEN 1 AND 10000
    ),
    CONSTRAINT content_file_multipart_parts_size_valid CHECK (
        content_length_bytes BETWEEN 1 AND 5368709120
    ),
    CONSTRAINT content_file_multipart_parts_time_valid CHECK (
        updated_at >= created_at
    )
);

-- Environment-specific Core API column grants are provisioned separately.
-- Provider upload IDs and issued-part manifests are private runtime metadata.
REVOKE ALL ON tutorhub.content_file_multipart_uploads FROM PUBLIC;
REVOKE ALL ON tutorhub.content_file_multipart_parts FROM PUBLIC;

COMMIT;
