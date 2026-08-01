BEGIN;

-- P3-02D-A extends the server-evaluated catalog. The deployment guardrail can
-- still force the feature off and can only lower these tenant-configurable
-- ceilings.
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
            'availability_polls'
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
            'study_meeting_creations_per_hour'
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
    );

ALTER TABLE tutorhub.tenant_quota_windows
    DROP CONSTRAINT tenant_quota_windows_key_valid;

ALTER TABLE tutorhub.tenant_quota_windows
    ADD CONSTRAINT tenant_quota_windows_key_valid CHECK (
        quota_key IN (
            'invite_creations_per_hour',
            'availability_poll_creations_per_hour',
            'availability_poll_capability_creations_per_hour',
            'study_meeting_creations_per_hour'
        )
    );

CREATE TABLE tutorhub.availability_polls (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    public_id uuid NOT NULL DEFAULT gen_random_uuid(),
    class_id uuid,
    owner_user_id uuid NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    timezone text NOT NULL,
    range_start date NOT NULL,
    range_end date NOT NULL,
    working_day_start time without time zone NOT NULL,
    working_day_end time without time zone NOT NULL,
    duration_minutes integer NOT NULL,
    slot_granularity_minutes integer NOT NULL,
    deadline_at timestamptz NOT NULL,
    share_mode text NOT NULL,
    status text NOT NULL DEFAULT 'draft',
    version bigint NOT NULL DEFAULT 1,
    selected_slot_id uuid,
    outcome_type text,
    outcome_id uuid,
    retention_until timestamptz NOT NULL,
    create_idempotency_key text NOT NULL,
    create_request_fingerprint bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT availability_polls_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT availability_polls_tenant_public_id_unique UNIQUE (tenant_id, public_id),
    CONSTRAINT availability_polls_public_id_unique UNIQUE (public_id),
    CONSTRAINT availability_polls_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id),
    CONSTRAINT availability_polls_owner_membership_fk
        FOREIGN KEY (tenant_id, owner_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT availability_polls_title_valid
        CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    CONSTRAINT availability_polls_description_valid
        CHECK (length(description) <= 4000),
    CONSTRAINT availability_polls_timezone_valid CHECK (
        length(btrim(timezone)) BETWEEN 1 AND 100
        AND lower(btrim(timezone)) <> 'local'
    ),
    CONSTRAINT availability_polls_range_valid CHECK (
        range_end >= range_start
        AND range_end <= range_start + 89
    ),
    CONSTRAINT availability_polls_working_day_valid
        CHECK (working_day_end > working_day_start),
    CONSTRAINT availability_polls_duration_valid
        CHECK (duration_minutes BETWEEN 15 AND 480),
    CONSTRAINT availability_polls_granularity_valid
        CHECK (slot_granularity_minutes IN (15, 30, 60)),
    CONSTRAINT availability_polls_share_mode_valid
        CHECK (share_mode IN ('class_members', 'invited_only', 'anyone_with_link')),
    CONSTRAINT availability_polls_class_share_valid CHECK (
        (class_id IS NULL AND share_mode <> 'class_members')
        OR class_id IS NOT NULL
    ),
    CONSTRAINT availability_polls_status_valid
        CHECK (status IN ('draft', 'open', 'closed', 'finalized', 'cancelled')),
    CONSTRAINT availability_polls_version_positive CHECK (version > 0),
    CONSTRAINT availability_polls_outcome_consistent CHECK (
        (
            status = 'finalized'
            AND selected_slot_id IS NOT NULL
            AND outcome_type IN ('class_session', 'study_meeting')
            AND outcome_id IS NOT NULL
        )
        OR (
            status <> 'finalized'
            AND selected_slot_id IS NULL
            AND outcome_type IS NULL
            AND outcome_id IS NULL
        )
    ),
    CONSTRAINT availability_polls_retention_valid CHECK (
        (
            status IN ('draft', 'open', 'closed')
            AND retention_until > deadline_at
            AND retention_until <= deadline_at + interval '180 days'
        )
        OR (
            status IN ('finalized', 'cancelled')
            AND retention_until > updated_at
            AND retention_until <= updated_at + interval '180 days'
        )
    ),
    CONSTRAINT availability_polls_idempotency_valid CHECK (
        length(create_idempotency_key) BETWEEN 8 AND 128
        AND create_idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(create_request_fingerprint) = 32
    ),
    CONSTRAINT availability_polls_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE INDEX availability_polls_owner_status_idx
    ON tutorhub.availability_polls
        (tenant_id, owner_user_id, status, updated_at DESC, id);

CREATE INDEX availability_polls_class_status_idx
    ON tutorhub.availability_polls
        (tenant_id, class_id, status, updated_at DESC, id)
    WHERE class_id IS NOT NULL;

CREATE INDEX availability_polls_deadline_idx
    ON tutorhub.availability_polls (deadline_at, tenant_id, id)
    WHERE status = 'open';

CREATE INDEX availability_polls_retention_idx
    ON tutorhub.availability_polls (retention_until, tenant_id, id);

CREATE UNIQUE INDEX availability_polls_create_idempotency_unique
    ON tutorhub.availability_polls (tenant_id, owner_user_id, create_idempotency_key);

CREATE TABLE tutorhub.availability_poll_slots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    poll_id uuid NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    ordinal integer NOT NULL,
    retired_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT availability_poll_slots_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT availability_poll_slots_tenant_poll_id_unique
        UNIQUE (tenant_id, poll_id, id),
    CONSTRAINT availability_poll_slots_poll_fk
        FOREIGN KEY (tenant_id, poll_id)
        REFERENCES tutorhub.availability_polls (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_poll_slots_range_valid CHECK (
        ends_at > starts_at
        AND ends_at <= starts_at + interval '8 hours'
    ),
    CONSTRAINT availability_poll_slots_ordinal_nonnegative CHECK (ordinal >= 0),
    CONSTRAINT availability_poll_slots_retirement_consistent CHECK (
        retired_at IS NULL OR retired_at >= created_at
    )
);

CREATE INDEX availability_poll_slots_poll_starts_idx
    ON tutorhub.availability_poll_slots (tenant_id, poll_id, starts_at, id)
    WHERE retired_at IS NULL;

CREATE UNIQUE INDEX availability_poll_slots_poll_ordinal_active_unique
    ON tutorhub.availability_poll_slots (tenant_id, poll_id, ordinal)
    WHERE retired_at IS NULL;

CREATE UNIQUE INDEX availability_poll_slots_poll_range_active_unique
    ON tutorhub.availability_poll_slots
        (tenant_id, poll_id, starts_at, ends_at)
    WHERE retired_at IS NULL;

ALTER TABLE tutorhub.availability_polls
    ADD CONSTRAINT availability_polls_selected_slot_fk
        FOREIGN KEY (tenant_id, id, selected_slot_id)
        REFERENCES tutorhub.availability_poll_slots (tenant_id, poll_id, id);

CREATE TABLE tutorhub.availability_poll_participants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    poll_id uuid NOT NULL,
    kind text NOT NULL,
    internal_user_id uuid,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT availability_poll_participants_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT availability_poll_participants_tenant_poll_id_unique
        UNIQUE (tenant_id, poll_id, id),
    CONSTRAINT availability_poll_participants_poll_fk
        FOREIGN KEY (tenant_id, poll_id)
        REFERENCES tutorhub.availability_polls (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_poll_participants_membership_fk
        FOREIGN KEY (tenant_id, internal_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT availability_poll_participants_kind_valid CHECK (
        (kind = 'internal_user' AND internal_user_id IS NOT NULL)
        OR (kind = 'external_invitee' AND internal_user_id IS NULL)
    ),
    CONSTRAINT availability_poll_participants_status_valid
        CHECK (status IN ('active', 'revoked')),
    CONSTRAINT availability_poll_participants_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX availability_poll_participants_internal_unique
    ON tutorhub.availability_poll_participants
        (tenant_id, poll_id, internal_user_id)
    WHERE internal_user_id IS NOT NULL AND status = 'active';

CREATE INDEX availability_poll_participants_poll_idx
    ON tutorhub.availability_poll_participants
        (tenant_id, poll_id, status, created_at, id);

CREATE TABLE tutorhub.availability_poll_capabilities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    poll_id uuid NOT NULL,
    parent_capability_id uuid,
    participant_id uuid,
    purpose text NOT NULL,
    scope text NOT NULL,
    token_version smallint NOT NULL,
    token_digest bytea NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoked_by uuid,
    use_count integer NOT NULL DEFAULT 0,
    last_used_at timestamptz,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT availability_poll_capabilities_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT availability_poll_capabilities_tenant_poll_id_unique
        UNIQUE (tenant_id, poll_id, id),
    CONSTRAINT availability_poll_capabilities_poll_fk
        FOREIGN KEY (tenant_id, poll_id)
        REFERENCES tutorhub.availability_polls (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_poll_capabilities_parent_fk
        FOREIGN KEY (tenant_id, poll_id, parent_capability_id)
        REFERENCES tutorhub.availability_poll_capabilities (tenant_id, poll_id, id),
    CONSTRAINT availability_poll_capabilities_participant_fk
        FOREIGN KEY (tenant_id, poll_id, participant_id)
        REFERENCES tutorhub.availability_poll_participants (tenant_id, poll_id, id),
    CONSTRAINT availability_poll_capabilities_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT availability_poll_capabilities_revoker_membership_fk
        FOREIGN KEY (tenant_id, revoked_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT availability_poll_capabilities_purpose_valid CHECK (
        (purpose = 'poll_access' AND parent_capability_id IS NULL AND created_by IS NOT NULL)
        OR (
            purpose = 'response_session'
            AND parent_capability_id IS NOT NULL
            AND created_by IS NULL
        )
    ),
    CONSTRAINT availability_poll_capabilities_scope_valid CHECK (
        scope IN ('invited_participant', 'public_link')
    ),
    CONSTRAINT availability_poll_capabilities_scope_participant_valid CHECK (
        (scope = 'invited_participant' AND participant_id IS NOT NULL)
        OR (scope = 'public_link' AND participant_id IS NULL)
    ),
    CONSTRAINT availability_poll_capabilities_digest_valid
        CHECK (token_version > 0 AND octet_length(token_digest) = 32),
    CONSTRAINT availability_poll_capabilities_expiry_valid CHECK (
        expires_at > created_at
        AND (
            (
                purpose = 'poll_access'
                AND expires_at <= created_at + interval '30 days'
            )
            OR (
                purpose = 'response_session'
                AND expires_at <= created_at + interval '30 minutes'
            )
        )
    ),
    CONSTRAINT availability_poll_capabilities_revocation_consistent CHECK (
        (revoked_at IS NULL AND revoked_by IS NULL)
        OR (revoked_at IS NOT NULL AND revoked_at >= created_at)
    ),
    CONSTRAINT availability_poll_capabilities_use_valid CHECK (
        use_count >= 0
        AND (last_used_at IS NULL OR last_used_at >= created_at)
    )
);

CREATE UNIQUE INDEX availability_poll_capabilities_digest_unique
    ON tutorhub.availability_poll_capabilities
        (tenant_id, token_version, token_digest);

CREATE INDEX availability_poll_capabilities_poll_active_idx
    ON tutorhub.availability_poll_capabilities
        (tenant_id, poll_id, purpose, expires_at, id)
    WHERE revoked_at IS NULL;

CREATE INDEX availability_poll_capabilities_expiry_idx
    ON tutorhub.availability_poll_capabilities (expires_at, tenant_id, id)
    WHERE revoked_at IS NULL;

CREATE TABLE tutorhub.availability_poll_responses (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    poll_id uuid NOT NULL,
    participant_id uuid,
    internal_user_id uuid,
    response_capability_id uuid,
    actor_type text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    submitted_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT availability_poll_responses_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT availability_poll_responses_tenant_poll_id_unique
        UNIQUE (tenant_id, poll_id, id),
    CONSTRAINT availability_poll_responses_poll_fk
        FOREIGN KEY (tenant_id, poll_id)
        REFERENCES tutorhub.availability_polls (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_poll_responses_participant_fk
        FOREIGN KEY (tenant_id, poll_id, participant_id)
        REFERENCES tutorhub.availability_poll_participants (tenant_id, poll_id, id),
    CONSTRAINT availability_poll_responses_membership_fk
        FOREIGN KEY (tenant_id, internal_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT availability_poll_responses_capability_fk
        FOREIGN KEY (tenant_id, poll_id, response_capability_id)
        REFERENCES tutorhub.availability_poll_capabilities (tenant_id, poll_id, id),
    CONSTRAINT availability_poll_responses_actor_valid CHECK (
        (
            actor_type = 'internal_member'
            AND internal_user_id IS NOT NULL
            AND response_capability_id IS NULL
        )
        OR (
            actor_type = 'external_session'
            AND internal_user_id IS NULL
            AND response_capability_id IS NOT NULL
        )
    ),
    CONSTRAINT availability_poll_responses_version_valid
        CHECK (version BETWEEN 1 AND 100),
    CONSTRAINT availability_poll_responses_updated_after_created
        CHECK (updated_at >= created_at AND submitted_at >= created_at)
);

CREATE UNIQUE INDEX availability_poll_responses_internal_unique
    ON tutorhub.availability_poll_responses
        (tenant_id, poll_id, internal_user_id)
    WHERE internal_user_id IS NOT NULL;

CREATE UNIQUE INDEX availability_poll_responses_capability_unique
    ON tutorhub.availability_poll_responses
        (tenant_id, response_capability_id)
    WHERE response_capability_id IS NOT NULL;

CREATE UNIQUE INDEX availability_poll_responses_participant_unique
    ON tutorhub.availability_poll_responses
        (tenant_id, poll_id, participant_id)
    WHERE participant_id IS NOT NULL;

CREATE INDEX availability_poll_responses_poll_idx
    ON tutorhub.availability_poll_responses
        (tenant_id, poll_id, submitted_at, id);

CREATE TABLE tutorhub.availability_poll_answers (
    tenant_id uuid NOT NULL,
    poll_id uuid NOT NULL,
    response_id uuid NOT NULL,
    slot_id uuid NOT NULL,
    state text NOT NULL,
    cleared_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, response_id, slot_id),
    CONSTRAINT availability_poll_answers_poll_fk
        FOREIGN KEY (tenant_id, poll_id)
        REFERENCES tutorhub.availability_polls (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_poll_answers_response_fk
        FOREIGN KEY (tenant_id, poll_id, response_id)
        REFERENCES tutorhub.availability_poll_responses (tenant_id, poll_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_poll_answers_slot_fk
        FOREIGN KEY (tenant_id, poll_id, slot_id)
        REFERENCES tutorhub.availability_poll_slots (tenant_id, poll_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_poll_answers_state_valid
        CHECK (state IN ('preferred', 'available', 'unavailable')),
    CONSTRAINT availability_poll_answers_clearance_consistent CHECK (
        cleared_at IS NULL OR cleared_at >= created_at
    ),
    CONSTRAINT availability_poll_answers_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE INDEX availability_poll_answers_poll_slot_idx
    ON tutorhub.availability_poll_answers
        (tenant_id, poll_id, slot_id, state, response_id)
    WHERE cleared_at IS NULL;

CREATE TABLE tutorhub.availability_poll_mutation_receipts (
    tenant_id uuid NOT NULL,
    poll_id uuid NOT NULL,
    operation text NOT NULL,
    actor_key text NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint bytea NOT NULL,
    result_version bigint NOT NULL,
    outcome_type text,
    outcome_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, poll_id, operation, actor_key, idempotency_key),
    CONSTRAINT availability_poll_receipts_poll_fk
        FOREIGN KEY (tenant_id, poll_id)
        REFERENCES tutorhub.availability_polls (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT availability_poll_receipts_operation_valid
        CHECK (operation IN ('respond', 'finalize')),
    CONSTRAINT availability_poll_receipts_actor_key_valid CHECK (
        length(actor_key) BETWEEN 38 AND 64
        AND actor_key ~ '^(user|capability):[0-9a-f-]{36}$'
    ),
    CONSTRAINT availability_poll_receipts_idempotency_valid CHECK (
        length(idempotency_key) BETWEEN 8 AND 128
        AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
    ),
    CONSTRAINT availability_poll_receipts_fingerprint_valid
        CHECK (octet_length(request_fingerprint) = 32),
    CONSTRAINT availability_poll_receipts_result_version_positive
        CHECK (result_version > 0),
    CONSTRAINT availability_poll_receipts_outcome_consistent CHECK (
        (operation = 'respond' AND outcome_type IS NULL AND outcome_id IS NULL)
        OR (
            operation = 'finalize'
            AND outcome_type IN ('class_session', 'study_meeting')
            AND outcome_id IS NOT NULL
        )
    )
);

CREATE INDEX availability_poll_receipts_created_idx
    ON tutorhub.availability_poll_mutation_receipts
        (tenant_id, created_at);

CREATE TABLE tutorhub.study_meetings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    class_id uuid,
    owner_user_id uuid NOT NULL,
    source_poll_id uuid,
    title text NOT NULL,
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    timezone text NOT NULL,
    status text NOT NULL DEFAULT 'scheduled',
    version bigint NOT NULL DEFAULT 1,
    create_idempotency_key text NOT NULL,
    create_request_fingerprint bytea NOT NULL,
    cancelled_at timestamptz,
    cancelled_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT study_meetings_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT study_meetings_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id),
    CONSTRAINT study_meetings_owner_membership_fk
        FOREIGN KEY (tenant_id, owner_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT study_meetings_source_poll_fk
        FOREIGN KEY (tenant_id, source_poll_id)
        REFERENCES tutorhub.availability_polls (tenant_id, id),
    CONSTRAINT study_meetings_canceller_membership_fk
        FOREIGN KEY (tenant_id, cancelled_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT study_meetings_title_valid
        CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    CONSTRAINT study_meetings_time_range_valid CHECK (
        ends_at > starts_at
        AND ends_at <= starts_at + interval '24 hours'
    ),
    CONSTRAINT study_meetings_timezone_valid CHECK (
        length(btrim(timezone)) BETWEEN 1 AND 100
        AND lower(btrim(timezone)) <> 'local'
    ),
    CONSTRAINT study_meetings_status_valid
        CHECK (status IN ('scheduled', 'cancelled')),
    CONSTRAINT study_meetings_version_positive CHECK (version > 0),
    CONSTRAINT study_meetings_idempotency_valid CHECK (
        length(create_idempotency_key) BETWEEN 8 AND 128
        AND create_idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(create_request_fingerprint) = 32
    ),
    CONSTRAINT study_meetings_cancellation_consistent CHECK (
        (
            status = 'cancelled'
            AND cancelled_at IS NOT NULL
            AND cancelled_by IS NOT NULL
            AND cancelled_at >= created_at
            AND updated_at >= cancelled_at
        )
        OR (
            status = 'scheduled'
            AND cancelled_at IS NULL
            AND cancelled_by IS NULL
        )
    ),
    CONSTRAINT study_meetings_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX study_meetings_source_poll_unique
    ON tutorhub.study_meetings (tenant_id, source_poll_id)
    WHERE source_poll_id IS NOT NULL;

CREATE UNIQUE INDEX study_meetings_create_idempotency_unique
    ON tutorhub.study_meetings
        (tenant_id, owner_user_id, create_idempotency_key);

CREATE INDEX study_meetings_owner_starts_idx
    ON tutorhub.study_meetings
        (tenant_id, owner_user_id, starts_at, id);

CREATE INDEX study_meetings_class_starts_idx
    ON tutorhub.study_meetings
        (tenant_id, class_id, starts_at, id)
    WHERE class_id IS NOT NULL;

-- Maintenance-role entry point only. Runtime Core API code has no DELETE path
-- and must not receive EXECUTE/DELETE. A dedicated maintenance role requires
-- schema USAGE and function EXECUTE. It also needs the SELECT columns used by
-- each predicate, DELETE on availability_polls, and SELECT (tenant_id,
-- source_poll_id) plus UPDATE (source_poll_id) on study_meetings. Deployment
-- acceptance must verify the exact grants and FK cascades with that login.
CREATE FUNCTION tutorhub.purge_expired_availability_polls(batch_size integer DEFAULT 100)
RETURNS integer
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog, tutorhub
AS $$
DECLARE
    candidate record;
    deleted_count integer := 0;
    row_count integer;
BEGIN
    IF batch_size IS NULL OR batch_size < 1 OR batch_size > 1000 THEN
        RAISE EXCEPTION 'batch_size must be between 1 and 1000'
            USING ERRCODE = '22023';
    END IF;

    FOR candidate IN
        SELECT poll.tenant_id, poll.id
        FROM tutorhub.availability_polls AS poll
        WHERE poll.retention_until <= clock_timestamp()
        ORDER BY poll.retention_until, poll.tenant_id, poll.id
        FOR UPDATE SKIP LOCKED
        LIMIT batch_size
    LOOP
        UPDATE tutorhub.study_meetings
        SET source_poll_id = NULL
        WHERE tenant_id = candidate.tenant_id
          AND source_poll_id = candidate.id;

        DELETE FROM tutorhub.availability_polls
        WHERE tenant_id = candidate.tenant_id
          AND id = candidate.id;
        GET DIAGNOSTICS row_count = ROW_COUNT;
        deleted_count := deleted_count + row_count;
    END LOOP;

    RETURN deleted_count;
END;
$$;

REVOKE ALL ON FUNCTION tutorhub.purge_expired_availability_polls(integer) FROM PUBLIC;

REVOKE ALL ON tutorhub.availability_polls FROM PUBLIC;
REVOKE ALL ON tutorhub.availability_poll_slots FROM PUBLIC;
REVOKE ALL ON tutorhub.availability_poll_participants FROM PUBLIC;
REVOKE ALL ON tutorhub.availability_poll_capabilities FROM PUBLIC;
REVOKE ALL ON tutorhub.availability_poll_responses FROM PUBLIC;
REVOKE ALL ON tutorhub.availability_poll_answers FROM PUBLIC;
REVOKE ALL ON tutorhub.availability_poll_mutation_receipts FROM PUBLIC;
REVOKE ALL ON tutorhub.study_meetings FROM PUBLIC;

COMMIT;
