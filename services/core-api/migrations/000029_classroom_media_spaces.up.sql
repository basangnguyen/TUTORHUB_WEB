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
            'file_uploads',
            'classroom_media_rooms',
            'instant_study_rooms'
        )
    );

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
            'file_upload_intents_per_hour',
            'active_media_spaces',
            'media_participants_per_space',
            'active_media_participants',
            'media_space_starts_per_hour'
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
        OR (quota_key = 'active_media_spaces' AND limit_value BETWEEN 1 AND 100)
        OR (quota_key = 'media_participants_per_space' AND limit_value BETWEEN 1 AND 50)
        OR (quota_key = 'active_media_participants' AND limit_value BETWEEN 1 AND 500)
        OR (quota_key = 'media_space_starts_per_hour' AND limit_value BETWEEN 1 AND 200)
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
            'file_upload_intents_per_hour',
            'media_space_starts_per_hour'
        )
    );

CREATE TABLE tutorhub.media_spaces (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    source_kind text NOT NULL,
    class_id uuid,
    source_class_session_id uuid,
    source_series_id uuid,
    source_occurrence_key text,
    source_study_meeting_id uuid,
    status text NOT NULL DEFAULT 'scheduled',
    version bigint NOT NULL DEFAULT 1,
    lobby_enabled boolean NOT NULL DEFAULT true,
    locked boolean NOT NULL DEFAULT false,
    create_idempotency_key text NOT NULL,
    create_request_fingerprint bytea NOT NULL,
    created_by uuid NOT NULL,
    updated_by uuid NOT NULL,
    opened_at timestamptz,
    opened_by uuid,
    ended_at timestamptz,
    ended_by uuid,
    cancelled_at timestamptz,
    cancelled_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT media_spaces_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT media_spaces_tenant_fk
        FOREIGN KEY (tenant_id)
        REFERENCES tutorhub.tenants (id)
        ON DELETE CASCADE,
    CONSTRAINT media_spaces_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id),
    CONSTRAINT media_spaces_class_session_fk
        FOREIGN KEY (tenant_id, class_id, source_class_session_id)
        REFERENCES tutorhub.class_sessions (tenant_id, class_id, id),
    CONSTRAINT media_spaces_series_fk
        FOREIGN KEY (tenant_id, class_id, source_series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id),
    CONSTRAINT media_spaces_study_meeting_fk
        FOREIGN KEY (tenant_id, source_study_meeting_id)
        REFERENCES tutorhub.study_meetings (tenant_id, id),
    CONSTRAINT media_spaces_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_spaces_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_spaces_opener_membership_fk
        FOREIGN KEY (tenant_id, opened_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_spaces_ender_membership_fk
        FOREIGN KEY (tenant_id, ended_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_spaces_canceller_membership_fk
        FOREIGN KEY (tenant_id, cancelled_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_spaces_source_kind_valid CHECK (
        source_kind IN ('class_session', 'class_session_occurrence', 'study_meeting')
    ),
    CONSTRAINT media_spaces_source_union_valid CHECK (
        (
            source_kind = 'class_session'
            AND class_id IS NOT NULL
            AND source_class_session_id IS NOT NULL
            AND source_series_id IS NULL
            AND source_occurrence_key IS NULL
            AND source_study_meeting_id IS NULL
        )
        OR (
            source_kind = 'class_session_occurrence'
            AND class_id IS NOT NULL
            AND source_class_session_id IS NULL
            AND source_series_id IS NOT NULL
            AND length(source_occurrence_key) BETWEEN 8 AND 128
            AND source_occurrence_key = btrim(source_occurrence_key)
            AND source_study_meeting_id IS NULL
        )
        OR (
            source_kind = 'study_meeting'
            AND class_id IS NULL
            AND source_class_session_id IS NULL
            AND source_series_id IS NULL
            AND source_occurrence_key IS NULL
            AND source_study_meeting_id IS NOT NULL
        )
    ),
    CONSTRAINT media_spaces_status_valid CHECK (
        status IN ('scheduled', 'open', 'ended', 'cancelled')
    ),
    CONSTRAINT media_spaces_version_positive CHECK (version > 0),
    CONSTRAINT media_spaces_create_idempotency_valid CHECK (
        length(create_idempotency_key) BETWEEN 16 AND 128
        AND create_idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(create_request_fingerprint) = 32
    ),
    CONSTRAINT media_spaces_create_idempotency_unique
        UNIQUE (tenant_id, created_by, create_idempotency_key),
    CONSTRAINT media_spaces_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT media_spaces_lifecycle_consistent CHECK (
        (
            status = 'scheduled'
            AND locked = false
            AND opened_at IS NULL
            AND opened_by IS NULL
            AND ended_at IS NULL
            AND ended_by IS NULL
            AND cancelled_at IS NULL
            AND cancelled_by IS NULL
        )
        OR (
            status = 'open'
            AND opened_at IS NOT NULL
            AND opened_by IS NOT NULL
            AND opened_at >= created_at
            AND opened_at <= updated_at
            AND ended_at IS NULL
            AND ended_by IS NULL
            AND cancelled_at IS NULL
            AND cancelled_by IS NULL
        )
        OR (
            status = 'ended'
            AND locked = false
            AND opened_at IS NOT NULL
            AND opened_by IS NOT NULL
            AND ended_at IS NOT NULL
            AND ended_by IS NOT NULL
            AND opened_at >= created_at
            AND ended_at >= opened_at
            AND ended_at <= updated_at
            AND cancelled_at IS NULL
            AND cancelled_by IS NULL
        )
        OR (
            status = 'cancelled'
            AND locked = false
            AND opened_at IS NULL
            AND opened_by IS NULL
            AND ended_at IS NULL
            AND ended_by IS NULL
            AND cancelled_at IS NOT NULL
            AND cancelled_by IS NOT NULL
            AND cancelled_at >= created_at
            AND cancelled_at <= updated_at
        )
    )
);

CREATE UNIQUE INDEX media_spaces_class_session_source_unique
    ON tutorhub.media_spaces (tenant_id, source_class_session_id)
    WHERE source_kind = 'class_session';

CREATE UNIQUE INDEX media_spaces_occurrence_source_unique
    ON tutorhub.media_spaces (tenant_id, source_series_id, source_occurrence_key)
    WHERE source_kind = 'class_session_occurrence';

CREATE UNIQUE INDEX media_spaces_study_meeting_source_unique
    ON tutorhub.media_spaces (tenant_id, source_study_meeting_id)
    WHERE source_kind = 'study_meeting';

CREATE INDEX media_spaces_tenant_status_updated_idx
    ON tutorhub.media_spaces (tenant_id, status, updated_at DESC, id DESC);

CREATE TABLE tutorhub.media_room_instances (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    attempt_number integer NOT NULL,
    status text NOT NULL DEFAULT 'provisioning',
    version bigint NOT NULL DEFAULT 1,
    provider_kind text NOT NULL DEFAULT 'livekit',
    provider_room_name text NOT NULL,
    provider_room_sid text,
    created_by uuid NOT NULL,
    updated_by uuid NOT NULL,
    activated_at timestamptz,
    closing_at timestamptz,
    ended_at timestamptz,
    failed_at timestamptz,
    failure_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT media_room_instances_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT media_room_instances_space_id_unique UNIQUE (tenant_id, space_id, id),
    CONSTRAINT media_room_instances_space_attempt_unique
        UNIQUE (tenant_id, space_id, attempt_number),
    CONSTRAINT media_room_instances_space_fk
        FOREIGN KEY (tenant_id, space_id)
        REFERENCES tutorhub.media_spaces (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_room_instances_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_room_instances_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_room_instances_attempt_positive CHECK (attempt_number > 0),
    CONSTRAINT media_room_instances_status_valid CHECK (
        status IN ('provisioning', 'active', 'closing', 'ended', 'failed')
    ),
    CONSTRAINT media_room_instances_version_positive CHECK (version > 0),
    CONSTRAINT media_room_instances_provider_kind_valid CHECK (
        provider_kind = 'livekit'
    ),
    CONSTRAINT media_room_instances_provider_name_valid CHECK (
        length(provider_room_name) BETWEEN 16 AND 128
        AND provider_room_name = btrim(provider_room_name)
        AND provider_room_name ~ '^[A-Za-z0-9_-]+$'
    ),
    CONSTRAINT media_room_instances_provider_name_unique
        UNIQUE (provider_kind, provider_room_name),
    CONSTRAINT media_room_instances_provider_sid_valid CHECK (
        provider_room_sid IS NULL
        OR (
            length(btrim(provider_room_sid)) BETWEEN 1 AND 255
            AND provider_room_sid !~ '[[:cntrl:]]'
        )
    ),
    CONSTRAINT media_room_instances_failure_code_valid CHECK (
        failure_code IS NULL
        OR (
            length(failure_code) BETWEEN 1 AND 64
            AND failure_code ~ '^[a-z][a-z0-9_]*$'
        )
    ),
    CONSTRAINT media_room_instances_updated_after_created CHECK (
        updated_at >= created_at
    ),
    CONSTRAINT media_room_instances_lifecycle_consistent CHECK (
        (
            status = 'provisioning'
            AND activated_at IS NULL
            AND closing_at IS NULL
            AND ended_at IS NULL
            AND failed_at IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'active'
            AND activated_at IS NOT NULL
            AND activated_at >= created_at
            AND activated_at <= updated_at
            AND closing_at IS NULL
            AND ended_at IS NULL
            AND failed_at IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'closing'
            AND activated_at IS NOT NULL
            AND closing_at IS NOT NULL
            AND activated_at >= created_at
            AND closing_at >= activated_at
            AND closing_at <= updated_at
            AND ended_at IS NULL
            AND failed_at IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'ended'
            AND activated_at IS NOT NULL
            AND closing_at IS NOT NULL
            AND ended_at IS NOT NULL
            AND activated_at >= created_at
            AND closing_at >= activated_at
            AND ended_at >= closing_at
            AND ended_at <= updated_at
            AND failed_at IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'failed'
            AND ended_at IS NULL
            AND failed_at IS NOT NULL
            AND failed_at >= created_at
            AND failed_at <= updated_at
            AND failure_code IS NOT NULL
            AND (activated_at IS NULL OR activated_at >= created_at)
            AND (closing_at IS NULL OR (activated_at IS NOT NULL AND closing_at >= activated_at))
        )
    )
);

CREATE UNIQUE INDEX media_room_instances_provider_sid_unique
    ON tutorhub.media_room_instances (provider_kind, provider_room_sid)
    WHERE provider_room_sid IS NOT NULL;

CREATE UNIQUE INDEX media_room_instances_one_active_unique
    ON tutorhub.media_room_instances (tenant_id, space_id)
    WHERE status IN ('provisioning', 'active', 'closing');

CREATE INDEX media_room_instances_tenant_status_updated_idx
    ON tutorhub.media_room_instances (tenant_id, status, updated_at DESC, id DESC);

CREATE TABLE tutorhub.media_space_members (
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'active',
    version bigint NOT NULL DEFAULT 1,
    invited_by uuid NOT NULL,
    revoked_at timestamptz,
    revoked_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, space_id, user_id),
    CONSTRAINT media_space_members_space_fk
        FOREIGN KEY (tenant_id, space_id)
        REFERENCES tutorhub.media_spaces (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_space_members_user_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_space_members_inviter_membership_fk
        FOREIGN KEY (tenant_id, invited_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_space_members_revoker_membership_fk
        FOREIGN KEY (tenant_id, revoked_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_space_members_status_valid CHECK (
        status IN ('active', 'revoked')
    ),
    CONSTRAINT media_space_members_version_positive CHECK (version > 0),
    CONSTRAINT media_space_members_updated_after_created CHECK (updated_at >= created_at),
    CONSTRAINT media_space_members_lifecycle_consistent CHECK (
        (
            status = 'active'
            AND revoked_at IS NULL
            AND revoked_by IS NULL
        )
        OR (
            status = 'revoked'
            AND revoked_at IS NOT NULL
            AND revoked_by IS NOT NULL
            AND revoked_at >= created_at
            AND revoked_at <= updated_at
        )
    )
);

CREATE INDEX media_space_members_user_active_idx
    ON tutorhub.media_space_members (tenant_id, user_id, space_id)
    WHERE status = 'active';

CREATE TABLE tutorhub.media_admission_requests (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    room_instance_id uuid NOT NULL,
    user_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'waiting',
    version bigint NOT NULL DEFAULT 1,
    idempotency_key text NOT NULL,
    request_fingerprint bytea NOT NULL,
    resolved_at timestamptz,
    resolved_by uuid,
    resolution_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT media_admission_requests_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT media_admission_requests_instance_id_unique
        UNIQUE (tenant_id, space_id, room_instance_id, id),
    CONSTRAINT media_admission_requests_instance_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_admission_requests_user_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_admission_requests_resolver_membership_fk
        FOREIGN KEY (tenant_id, resolved_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_admission_requests_status_valid CHECK (
        status IN ('waiting', 'admitted', 'denied', 'cancelled', 'expired')
    ),
    CONSTRAINT media_admission_requests_version_positive CHECK (version > 0),
    CONSTRAINT media_admission_requests_idempotency_valid CHECK (
        length(idempotency_key) BETWEEN 16 AND 128
        AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT media_admission_requests_idempotency_unique
        UNIQUE (tenant_id, room_instance_id, user_id, idempotency_key),
    CONSTRAINT media_admission_requests_resolution_code_valid CHECK (
        resolution_code IS NULL
        OR (
            length(resolution_code) BETWEEN 1 AND 64
            AND resolution_code ~ '^[a-z][a-z0-9_]*$'
        )
    ),
    CONSTRAINT media_admission_requests_updated_after_created CHECK (
        updated_at >= created_at
    ),
    CONSTRAINT media_admission_requests_lifecycle_consistent CHECK (
        (
            status = 'waiting'
            AND resolved_at IS NULL
            AND resolved_by IS NULL
            AND resolution_code IS NULL
        )
        OR (
            status = 'admitted'
            AND resolved_at IS NOT NULL
            AND resolved_by IS NOT NULL
            AND resolved_at >= created_at
            AND resolved_at <= updated_at
        )
        OR (
            status = 'denied'
            AND resolved_at IS NOT NULL
            AND resolved_by IS NOT NULL
            AND resolution_code IS NOT NULL
            AND resolved_at >= created_at
            AND resolved_at <= updated_at
        )
        OR (
            status IN ('cancelled', 'expired')
            AND resolved_at IS NOT NULL
            AND resolution_code IS NOT NULL
            AND resolved_at >= created_at
            AND resolved_at <= updated_at
        )
    )
);

CREATE UNIQUE INDEX media_admission_requests_one_waiting_unique
    ON tutorhub.media_admission_requests (tenant_id, room_instance_id, user_id)
    WHERE status = 'waiting';

CREATE INDEX media_admission_requests_instance_status_idx
    ON tutorhub.media_admission_requests
        (tenant_id, room_instance_id, status, created_at, id);

CREATE TABLE tutorhub.media_participant_sessions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    room_instance_id uuid NOT NULL,
    user_id uuid NOT NULL,
    admission_request_id uuid,
    join_attempt_id uuid NOT NULL,
    provider_participant_identity text NOT NULL,
    instance_role text NOT NULL DEFAULT 'attendee',
    status text NOT NULL DEFAULT 'waiting',
    capacity_reserved boolean NOT NULL DEFAULT true,
    version bigint NOT NULL DEFAULT 1,
    admitted_at timestamptz,
    joining_at timestamptz,
    connected_at timestamptz,
    reconnecting_at timestamptz,
    terminal_at timestamptz,
    removed_by uuid,
    failure_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT media_participant_sessions_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT media_participant_sessions_instance_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_participant_sessions_user_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_participant_sessions_admission_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id, admission_request_id)
        REFERENCES tutorhub.media_admission_requests (
            tenant_id,
            space_id,
            room_instance_id,
            id
        ),
    CONSTRAINT media_participant_sessions_remover_membership_fk
        FOREIGN KEY (tenant_id, removed_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_participant_sessions_join_attempt_unique
        UNIQUE (tenant_id, room_instance_id, user_id, join_attempt_id),
    CONSTRAINT media_participant_sessions_provider_identity_valid CHECK (
        length(provider_participant_identity) BETWEEN 16 AND 128
        AND provider_participant_identity = btrim(provider_participant_identity)
        AND provider_participant_identity ~ '^[A-Za-z0-9_-]+$'
    ),
    CONSTRAINT media_participant_sessions_instance_role_valid CHECK (
        instance_role IN ('host', 'co_host', 'teaching_assistant', 'attendee')
    ),
    CONSTRAINT media_participant_sessions_status_valid CHECK (
        status IN (
            'waiting',
            'admitted',
            'joining',
            'connected',
            'reconnecting',
            'left',
            'removed',
            'failed'
        )
    ),
    CONSTRAINT media_participant_sessions_version_positive CHECK (version > 0),
    CONSTRAINT media_participant_sessions_failure_code_valid CHECK (
        failure_code IS NULL
        OR (
            length(failure_code) BETWEEN 1 AND 64
            AND failure_code ~ '^[a-z][a-z0-9_]*$'
        )
    ),
    CONSTRAINT media_participant_sessions_updated_after_created CHECK (
        updated_at >= created_at
    ),
    CONSTRAINT media_participant_sessions_lifecycle_consistent CHECK (
        (
            status = 'waiting'
            AND capacity_reserved = true
            AND admitted_at IS NULL
            AND joining_at IS NULL
            AND connected_at IS NULL
            AND reconnecting_at IS NULL
            AND terminal_at IS NULL
            AND removed_by IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'admitted'
            AND capacity_reserved = true
            AND admitted_at IS NOT NULL
            AND admitted_at >= created_at
            AND admitted_at <= updated_at
            AND joining_at IS NULL
            AND connected_at IS NULL
            AND reconnecting_at IS NULL
            AND terminal_at IS NULL
            AND removed_by IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'joining'
            AND capacity_reserved = true
            AND admitted_at IS NOT NULL
            AND joining_at IS NOT NULL
            AND joining_at >= admitted_at
            AND joining_at <= updated_at
            AND connected_at IS NULL
            AND reconnecting_at IS NULL
            AND terminal_at IS NULL
            AND removed_by IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'connected'
            AND capacity_reserved = true
            AND admitted_at IS NOT NULL
            AND joining_at IS NOT NULL
            AND connected_at IS NOT NULL
            AND joining_at >= admitted_at
            AND connected_at >= joining_at
            AND connected_at <= updated_at
            AND reconnecting_at IS NULL
            AND terminal_at IS NULL
            AND removed_by IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'reconnecting'
            AND capacity_reserved = true
            AND admitted_at IS NOT NULL
            AND joining_at IS NOT NULL
            AND connected_at IS NOT NULL
            AND reconnecting_at IS NOT NULL
            AND joining_at >= admitted_at
            AND connected_at >= joining_at
            AND reconnecting_at >= connected_at
            AND reconnecting_at <= updated_at
            AND terminal_at IS NULL
            AND removed_by IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'left'
            AND capacity_reserved = false
            AND terminal_at IS NOT NULL
            AND terminal_at >= created_at
            AND terminal_at <= updated_at
            AND removed_by IS NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'removed'
            AND capacity_reserved = false
            AND terminal_at IS NOT NULL
            AND terminal_at >= created_at
            AND terminal_at <= updated_at
            AND removed_by IS NOT NULL
            AND failure_code IS NULL
        )
        OR (
            status = 'failed'
            AND capacity_reserved = false
            AND terminal_at IS NOT NULL
            AND terminal_at >= created_at
            AND terminal_at <= updated_at
            AND removed_by IS NULL
            AND failure_code IS NOT NULL
        )
    )
);

CREATE UNIQUE INDEX media_participant_sessions_provider_identity_unique
    ON tutorhub.media_participant_sessions
        (tenant_id, room_instance_id, provider_participant_identity);

CREATE UNIQUE INDEX media_participant_sessions_one_active_user_unique
    ON tutorhub.media_participant_sessions (tenant_id, room_instance_id, user_id)
    WHERE status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting');

CREATE INDEX media_participant_sessions_user_status_idx
    ON tutorhub.media_participant_sessions
        (tenant_id, user_id, status, updated_at DESC, id DESC);

CREATE TABLE tutorhub.media_space_mutation_receipts (
    tenant_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint bytea NOT NULL,
    operation text NOT NULL,
    space_id uuid NOT NULL,
    result_space_version bigint NOT NULL,
    result_room_instance_id uuid,
    actor_user_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_user_id, idempotency_key),
    CONSTRAINT media_space_mutation_receipts_space_fk
        FOREIGN KEY (tenant_id, space_id)
        REFERENCES tutorhub.media_spaces (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_space_mutation_receipts_instance_fk
        FOREIGN KEY (tenant_id, space_id, result_room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id),
    CONSTRAINT media_space_mutation_receipts_actor_membership_fk
        FOREIGN KEY (tenant_id, actor_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_space_mutation_receipts_idempotency_valid CHECK (
        length(idempotency_key) BETWEEN 16 AND 128
        AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT media_space_mutation_receipts_operation_valid CHECK (
        operation IN ('start', 'end', 'cancel')
    ),
    CONSTRAINT media_space_mutation_receipts_result_version_positive CHECK (
        result_space_version > 0
    ),
    CONSTRAINT media_space_mutation_receipts_result_valid CHECK (
        operation <> 'start' OR result_room_instance_id IS NOT NULL
    )
);

CREATE INDEX media_space_mutation_receipts_space_created_idx
    ON tutorhub.media_space_mutation_receipts (tenant_id, space_id, created_at DESC);

-- Environment-specific Core API column grants are provisioned separately.
-- P4-01 leaves admission/member/participant write privileges unprovisioned.
REVOKE ALL ON tutorhub.media_spaces FROM PUBLIC;
REVOKE ALL ON tutorhub.media_room_instances FROM PUBLIC;
REVOKE ALL ON tutorhub.media_space_members FROM PUBLIC;
REVOKE ALL ON tutorhub.media_admission_requests FROM PUBLIC;
REVOKE ALL ON tutorhub.media_participant_sessions FROM PUBLIC;
REVOKE ALL ON tutorhub.media_space_mutation_receipts FROM PUBLIC;

COMMIT;
