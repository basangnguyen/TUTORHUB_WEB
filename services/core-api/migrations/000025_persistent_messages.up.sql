BEGIN;

-- P3-07A adds bounded message storage and hourly send controls while
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
            'message_sends_per_hour'
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
            'message_sends_per_hour'
        )
    );

CREATE TABLE tutorhub.messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    author_user_id uuid NOT NULL,
    client_message_id uuid NOT NULL,
    sequence bigint NOT NULL,
    request_fingerprint bytea NOT NULL,
    content text,
    state text NOT NULL DEFAULT 'active',
    version bigint NOT NULL DEFAULT 1,
    edited_at timestamptz,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messages_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT messages_conversation_id_unique
        UNIQUE (tenant_id, conversation_id, id),
    CONSTRAINT messages_sequence_unique
        UNIQUE (tenant_id, conversation_id, sequence),
    CONSTRAINT messages_receipt_marker_unique
        UNIQUE (tenant_id, conversation_id, sequence, id),
    CONSTRAINT messages_conversation_fk
        FOREIGN KEY (tenant_id, conversation_id)
        REFERENCES tutorhub.conversations (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT messages_author_membership_fk
        FOREIGN KEY (tenant_id, author_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT messages_client_idempotency_unique
        UNIQUE (tenant_id, author_user_id, client_message_id),
    CONSTRAINT messages_sequence_positive CHECK (sequence > 0),
    CONSTRAINT messages_request_fingerprint_valid CHECK (
        octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT messages_state_valid CHECK (state IN ('active', 'deleted')),
    CONSTRAINT messages_version_positive CHECK (version > 0),
    CONSTRAINT messages_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT messages_edited_at_valid CHECK (
        edited_at IS NULL
        OR (edited_at >= created_at AND edited_at <= updated_at)
    ),
    CONSTRAINT messages_deleted_at_valid CHECK (
        deleted_at IS NULL
        OR (deleted_at >= created_at AND deleted_at <= updated_at)
    ),
    CONSTRAINT messages_lifecycle_order_valid CHECK (
        (edited_at IS NULL OR version > 1)
        AND (deleted_at IS NULL OR version > 1)
        AND (
            deleted_at IS NULL
            OR edited_at IS NULL
            OR edited_at <= deleted_at
        )
    ),
    CONSTRAINT messages_content_lifecycle_valid CHECK (
        (
            state = 'active'
            AND deleted_at IS NULL
            AND content IS NOT NULL
            AND char_length(content) BETWEEN 1 AND 4000
            AND octet_length(content) <= 16384
            AND position(E'\r' IN content) = 0
            AND content = btrim(content, E' \t\n\r')
        )
        OR (
            state = 'deleted'
            AND deleted_at IS NOT NULL
            AND content IS NULL
        )
    )
);

CREATE INDEX messages_conversation_author_idx
    ON tutorhub.messages (
        tenant_id,
        conversation_id,
        author_user_id,
        sequence DESC
    );

CREATE INDEX messages_author_rate_idx
    ON tutorhub.messages (tenant_id, author_user_id, created_at DESC);

CREATE TABLE tutorhub.tenant_message_usage (
    tenant_id uuid PRIMARY KEY,
    message_count bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_message_usage_tenant_fk
        FOREIGN KEY (tenant_id)
        REFERENCES tutorhub.tenants (id)
        ON DELETE CASCADE,
    CONSTRAINT tenant_message_usage_count_valid CHECK (message_count >= 0)
);

CREATE TABLE tutorhub.message_receipts (
    tenant_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    user_id uuid NOT NULL,
    last_read_sequence bigint NOT NULL,
    last_read_message_id uuid NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, user_id),
    CONSTRAINT message_receipts_conversation_fk
        FOREIGN KEY (tenant_id, conversation_id)
        REFERENCES tutorhub.conversations (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT message_receipts_user_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT message_receipts_last_read_message_fk
        FOREIGN KEY (
            tenant_id,
            conversation_id,
            last_read_sequence,
            last_read_message_id
        )
        REFERENCES tutorhub.messages (
            tenant_id,
            conversation_id,
            sequence,
            id
        ),
    CONSTRAINT message_receipts_last_read_sequence_positive CHECK (
        last_read_sequence > 0
    )
);

CREATE INDEX message_receipts_user_list_idx
    ON tutorhub.message_receipts (tenant_id, user_id, conversation_id);

-- Environment-specific Core API column grants are provisioned separately.
-- Keep private message content and read state denied to PUBLIC by default.
REVOKE ALL ON tutorhub.messages FROM PUBLIC;
REVOKE ALL ON tutorhub.tenant_message_usage FROM PUBLIC;
REVOKE ALL ON tutorhub.message_receipts FROM PUBLIC;

COMMIT;
