BEGIN;

ALTER TABLE tutorhub.tenant_feature_overrides
    DROP CONSTRAINT tenant_feature_overrides_key_valid;

ALTER TABLE tutorhub.tenant_feature_overrides
    ADD CONSTRAINT tenant_feature_overrides_key_valid CHECK (
        feature_key IN (
            'membership_invitations',
            'class_management',
            'class_invite_links',
            'class_session_scheduling',
            'class_session_recurrence',
            'in_app_notifications',
            'availability_polls',
            'conversations',
            'file_uploads'
        )
    );

-- P3-08 adds bounded class-file storage and upload-intent rate controls while
-- preserving every quota key introduced by earlier phases.
ALTER TABLE tutorhub.tenant_quota_overrides
    DROP CONSTRAINT tenant_quota_overrides_key_valid,
    DROP CONSTRAINT tenant_quota_overrides_limit_valid;

ALTER TABLE tutorhub.tenant_quota_overrides
    ADD CONSTRAINT tenant_quota_overrides_key_valid CHECK (
        quota_key IN (
            'members',
            'active_classes',
            'invite_creations_per_hour',
            'active_availability_polls',
            'availability_poll_range_days',
            'availability_poll_slots',
            'availability_poll_participants',
            'availability_poll_creations_per_hour',
            'availability_poll_capability_creations_per_hour',
            'active_study_meetings',
            'study_meeting_creations_per_hour',
            'messages_per_tenant',
            'message_sends_per_hour',
            'files_per_tenant',
            'file_bytes_per_tenant',
            'single_file_bytes',
            'file_upload_intents_per_hour'
        )
    ),
    ADD CONSTRAINT tenant_quota_overrides_limit_valid CHECK (
        (quota_key = 'members' AND limit_value BETWEEN 1 AND 10000)
        OR (quota_key = 'active_classes' AND limit_value BETWEEN 1 AND 1000)
        OR (quota_key = 'invite_creations_per_hour' AND limit_value BETWEEN 1 AND 10000)
        OR (quota_key = 'active_availability_polls' AND limit_value BETWEEN 1 AND 200)
        OR (quota_key = 'availability_poll_range_days' AND limit_value BETWEEN 1 AND 90)
        OR (quota_key = 'availability_poll_slots' AND limit_value BETWEEN 1 AND 1000)
        OR (quota_key = 'availability_poll_participants' AND limit_value BETWEEN 1 AND 500)
        OR (quota_key = 'availability_poll_creations_per_hour' AND limit_value BETWEEN 1 AND 200)
        OR (
            quota_key = 'availability_poll_capability_creations_per_hour'
            AND limit_value BETWEEN 1 AND 1000
        )
        OR (quota_key = 'active_study_meetings' AND limit_value BETWEEN 1 AND 200)
        OR (quota_key = 'study_meeting_creations_per_hour' AND limit_value BETWEEN 1 AND 200)
        OR (quota_key = 'messages_per_tenant' AND limit_value BETWEEN 1 AND 10000000)
        OR (quota_key = 'message_sends_per_hour' AND limit_value BETWEEN 1 AND 100000)
        OR (quota_key = 'files_per_tenant' AND limit_value BETWEEN 1 AND 1000000)
        OR (quota_key = 'file_bytes_per_tenant' AND limit_value BETWEEN 1 AND 10995116277760)
        OR (quota_key = 'single_file_bytes' AND limit_value BETWEEN 1 AND 5368709120)
        OR (quota_key = 'file_upload_intents_per_hour' AND limit_value BETWEEN 1 AND 100000)
    );

ALTER TABLE tutorhub.tenant_quota_windows
    DROP CONSTRAINT tenant_quota_windows_key_valid;

ALTER TABLE tutorhub.tenant_quota_windows
    ADD CONSTRAINT tenant_quota_windows_key_valid CHECK (
        quota_key IN (
            'invite_creations_per_hour',
            'availability_poll_creations_per_hour',
            'availability_poll_capability_creations_per_hour',
            'study_meeting_creations_per_hour',
            'message_sends_per_hour',
            'file_upload_intents_per_hour'
        )
    );

CREATE TABLE tutorhub.content_files (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    creator_user_id uuid NOT NULL,
    client_request_id uuid NOT NULL,
    request_fingerprint bytea NOT NULL,
    object_key text NOT NULL,
    display_name text NOT NULL,
    declared_media_type text NOT NULL,
    expected_size_bytes bigint NOT NULL,
    expected_checksum_sha256 bytea NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    version bigint NOT NULL DEFAULT 1,
    upload_expires_at timestamptz NOT NULL,
    stored_size_bytes bigint,
    stored_media_type text,
    stored_checksum_sha256 bytea,
    storage_etag text,
    storage_version_id text,
    uploaded_at timestamptz,
    processing_at timestamptz,
    ready_at timestamptz,
    rejected_at timestamptz,
    deleted_at timestamptz,
    deletion_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT content_files_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT content_files_tenant_fk
        FOREIGN KEY (tenant_id)
        REFERENCES tutorhub.tenants (id)
        ON DELETE CASCADE,
    CONSTRAINT content_files_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id),
    CONSTRAINT content_files_creator_membership_fk
        FOREIGN KEY (tenant_id, creator_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT content_files_creator_idempotency_unique
        UNIQUE (tenant_id, creator_user_id, client_request_id),
    CONSTRAINT content_files_object_key_unique UNIQUE (object_key),
    CONSTRAINT content_files_request_fingerprint_valid CHECK (
        octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT content_files_object_key_valid CHECK (
        object_key = 'tenants/' || tenant_id::text || '/files/' || id::text || '/original'
    ),
    CONSTRAINT content_files_display_name_valid CHECK (
        char_length(display_name) BETWEEN 1 AND 255
        AND octet_length(display_name) <= 1024
        AND display_name = btrim(display_name)
        AND display_name !~ '[[:cntrl:]]'
        AND position(E'\r' IN display_name) = 0
        AND position(E'\n' IN display_name) = 0
    ),
    CONSTRAINT content_files_declared_media_type_valid CHECK (
        char_length(declared_media_type) BETWEEN 3 AND 127
        AND octet_length(declared_media_type) <= 127
        AND declared_media_type = lower(btrim(declared_media_type))
        AND declared_media_type ~ '^[a-z0-9][a-z0-9!#$&^_.+-]*/[a-z0-9][a-z0-9!#$&^_.+-]*$'
    ),
    CONSTRAINT content_files_expected_size_valid CHECK (
        expected_size_bytes BETWEEN 1 AND 5368709120
    ),
    CONSTRAINT content_files_expected_checksum_valid CHECK (
        octet_length(expected_checksum_sha256) = 32
    ),
    CONSTRAINT content_files_status_valid CHECK (
        status IN ('pending', 'uploaded', 'processing', 'ready', 'rejected')
    ),
    CONSTRAINT content_files_version_positive CHECK (version > 0),
    CONSTRAINT content_files_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT content_files_expiry_after_created CHECK (upload_expires_at > created_at),
    CONSTRAINT content_files_storage_proof_consistent CHECK (
        (
            status = 'pending'
            AND stored_size_bytes IS NULL
            AND stored_media_type IS NULL
            AND stored_checksum_sha256 IS NULL
            AND storage_etag IS NULL
            AND storage_version_id IS NULL
            AND uploaded_at IS NULL
        )
        OR (
            status IN ('uploaded', 'processing', 'ready', 'rejected')
            AND stored_size_bytes = expected_size_bytes
            AND stored_media_type = declared_media_type
            AND stored_checksum_sha256 = expected_checksum_sha256
            AND octet_length(stored_checksum_sha256) = 32
            AND length(btrim(storage_etag)) BETWEEN 1 AND 512
            AND length(btrim(storage_version_id)) BETWEEN 1 AND 512
            AND uploaded_at IS NOT NULL
            AND uploaded_at >= created_at
            AND uploaded_at <= updated_at
        )
    ),
    CONSTRAINT content_files_processing_lifecycle_consistent CHECK (
        (status IN ('pending', 'uploaded') AND processing_at IS NULL)
        OR (
            status IN ('processing', 'ready', 'rejected')
            AND processing_at IS NOT NULL
            AND processing_at >= uploaded_at
            AND processing_at <= updated_at
        )
    ),
    CONSTRAINT content_files_terminal_lifecycle_consistent CHECK (
        (
            status = 'ready'
            AND ready_at IS NOT NULL
            AND rejected_at IS NULL
            AND ready_at >= processing_at
            AND ready_at <= updated_at
        )
        OR (
            status = 'rejected'
            AND rejected_at IS NOT NULL
            AND ready_at IS NULL
            AND rejected_at >= processing_at
            AND rejected_at <= updated_at
        )
        OR (
            status IN ('pending', 'uploaded', 'processing')
            AND ready_at IS NULL
            AND rejected_at IS NULL
        )
    ),
    CONSTRAINT content_files_deletion_consistent CHECK (
        (deleted_at IS NULL AND deletion_reason IS NULL)
        OR (
            deleted_at IS NOT NULL
            AND deleted_at >= created_at
            AND deleted_at <= updated_at
            AND length(btrim(deletion_reason)) BETWEEN 1 AND 64
        )
    )
);

CREATE INDEX content_files_class_status_created_idx
    ON tutorhub.content_files (tenant_id, class_id, status, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;

CREATE INDEX content_files_creator_rate_idx
    ON tutorhub.content_files (tenant_id, creator_user_id, created_at DESC);

CREATE INDEX content_files_pending_expiry_idx
    ON tutorhub.content_files (upload_expires_at, tenant_id, id)
    WHERE status = 'pending' AND deleted_at IS NULL;

CREATE TABLE tutorhub.tenant_file_usage (
    tenant_id uuid PRIMARY KEY,
    file_count bigint NOT NULL DEFAULT 0,
    reserved_bytes bigint NOT NULL DEFAULT 0,
    committed_bytes bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_file_usage_tenant_fk
        FOREIGN KEY (tenant_id)
        REFERENCES tutorhub.tenants (id)
        ON DELETE CASCADE,
    CONSTRAINT tenant_file_usage_counts_valid CHECK (
        file_count >= 0 AND reserved_bytes >= 0 AND committed_bytes >= 0
    )
);

-- Environment-specific Core API column grants are provisioned separately.
-- Keep private filenames, object keys and checksums denied to PUBLIC by default.
REVOKE ALL ON tutorhub.content_files FROM PUBLIC;
REVOKE ALL ON tutorhub.tenant_file_usage FROM PUBLIC;

COMMIT;
