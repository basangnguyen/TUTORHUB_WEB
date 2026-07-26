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
            'class_session_recurrence'
        )
    );

CREATE TABLE tutorhub.class_session_series (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    local_start timestamp without time zone NOT NULL,
    timezone text NOT NULL,
    duration_minutes integer NOT NULL,
    recurrence_frequency text NOT NULL,
    recurrence_interval integer NOT NULL DEFAULT 1,
    recurrence_weekdays text[] NOT NULL DEFAULT '{}',
    recurrence_month_days smallint[] NOT NULL DEFAULT '{}',
    recurrence_months smallint[] NOT NULL DEFAULT '{}',
    recurrence_end_type text NOT NULL,
    recurrence_until_date date,
    recurrence_count integer,
    normalized_rule text NOT NULL,
    overlap_policy text NOT NULL DEFAULT 'reject',
    status text NOT NULL DEFAULT 'scheduled',
    version bigint NOT NULL DEFAULT 1,
    sequence bigint NOT NULL DEFAULT 0,
    split_from_series_id uuid,
    created_by uuid NOT NULL,
    updated_by uuid NOT NULL,
    cancelled_at timestamptz,
    cancelled_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT class_session_series_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_session_series_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_series_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_series_canceller_membership_fk
        FOREIGN KEY (tenant_id, cancelled_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_series_tenant_class_id_unique
        UNIQUE (tenant_id, class_id, id),
    CONSTRAINT class_session_series_split_source_fk
        FOREIGN KEY (tenant_id, class_id, split_from_series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id),
    CONSTRAINT class_session_series_title_valid
        CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    CONSTRAINT class_session_series_description_valid
        CHECK (length(description) <= 4000),
    CONSTRAINT class_session_series_timezone_valid CHECK (
        length(btrim(timezone)) BETWEEN 1 AND 100
        AND lower(btrim(timezone)) <> 'local'
    ),
    CONSTRAINT class_session_series_duration_valid
        CHECK (duration_minutes BETWEEN 1 AND 1440),
    CONSTRAINT class_session_series_frequency_valid
        CHECK (recurrence_frequency IN ('daily', 'weekly', 'monthly', 'yearly')),
    CONSTRAINT class_session_series_interval_valid
        CHECK (recurrence_interval BETWEEN 1 AND 366),
    CONSTRAINT class_session_series_weekdays_valid CHECK (
        cardinality(recurrence_weekdays) <= 7
        AND recurrence_weekdays <@ ARRAY['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU']::text[]
    ),
    CONSTRAINT class_session_series_months_valid CHECK (
        cardinality(recurrence_months) <= 12
        AND recurrence_months <@ ARRAY[1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]::smallint[]
    ),
    CONSTRAINT class_session_series_end_valid CHECK (
        (
            recurrence_end_type = 'after_count'
            AND recurrence_count BETWEEN 1 AND 512
            AND recurrence_until_date IS NULL
        )
        OR (
            recurrence_end_type = 'on_date'
            AND recurrence_until_date IS NOT NULL
            AND recurrence_count IS NULL
        )
    ),
    CONSTRAINT class_session_series_rule_valid
        CHECK (length(normalized_rule) BETWEEN 1 AND 512),
    CONSTRAINT class_session_series_overlap_policy_valid
        CHECK (overlap_policy IN ('reject', 'earlier', 'later')),
    CONSTRAINT class_session_series_status_valid
        CHECK (status IN ('scheduled', 'cancelled')),
    CONSTRAINT class_session_series_version_positive CHECK (version > 0),
    CONSTRAINT class_session_series_sequence_nonnegative CHECK (sequence >= 0),
    CONSTRAINT class_session_series_updated_after_created
        CHECK (updated_at >= created_at),
    CONSTRAINT class_session_series_cancellation_consistent CHECK (
        (
            status = 'cancelled'
            AND cancelled_at IS NOT NULL
            AND cancelled_by IS NOT NULL
            AND cancelled_at >= created_at
            AND updated_at >= cancelled_at
        )
        OR (
            status <> 'cancelled'
            AND cancelled_at IS NULL
            AND cancelled_by IS NULL
        )
    )
);

CREATE INDEX class_session_series_class_start_idx
    ON tutorhub.class_session_series (tenant_id, class_id, local_start, id)
    WHERE status = 'scheduled';

CREATE TABLE tutorhub.class_session_exceptions (
    series_id uuid NOT NULL,
    occurrence_key text NOT NULL,
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    exception_type text NOT NULL,
    original_local_start timestamp without time zone NOT NULL,
    original_timezone text NOT NULL,
    original_overlap_offset_seconds integer NOT NULL,
    override_local_start timestamp without time zone,
    override_timezone text,
    override_duration_minutes integer,
    override_title text,
    override_description text,
    reason text NOT NULL DEFAULT '',
    version bigint NOT NULL DEFAULT 1,
    created_by uuid NOT NULL,
    updated_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (series_id, occurrence_key),
    CONSTRAINT class_session_exceptions_series_fk
        FOREIGN KEY (tenant_id, class_id, series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_session_exceptions_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_exceptions_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_exceptions_occurrence_key_valid
        CHECK (length(occurrence_key) BETWEEN 8 AND 128),
    CONSTRAINT class_session_exceptions_type_valid
        CHECK (exception_type IN ('cancel', 'override')),
    CONSTRAINT class_session_exceptions_original_timezone_valid CHECK (
        length(btrim(original_timezone)) BETWEEN 1 AND 100
        AND lower(btrim(original_timezone)) <> 'local'
    ),
    CONSTRAINT class_session_exceptions_original_offset_valid
        CHECK (original_overlap_offset_seconds BETWEEN -86400 AND 86400),
    CONSTRAINT class_session_exceptions_override_timezone_valid CHECK (
        override_timezone IS NULL
        OR (
            length(btrim(override_timezone)) BETWEEN 1 AND 100
            AND lower(btrim(override_timezone)) <> 'local'
        )
    ),
    CONSTRAINT class_session_exceptions_override_duration_valid
        CHECK (
            override_duration_minutes IS NULL
            OR override_duration_minutes BETWEEN 1 AND 1440
        ),
    CONSTRAINT class_session_exceptions_override_title_valid
        CHECK (
            override_title IS NULL
            OR length(btrim(override_title)) BETWEEN 1 AND 200
        ),
    CONSTRAINT class_session_exceptions_override_description_valid
        CHECK (
            override_description IS NULL
            OR length(override_description) <= 4000
        ),
    CONSTRAINT class_session_exceptions_reason_valid
        CHECK (length(reason) <= 500),
    CONSTRAINT class_session_exceptions_action_payload_valid CHECK (
        (
            exception_type = 'cancel'
            AND override_local_start IS NULL
            AND override_timezone IS NULL
            AND override_duration_minutes IS NULL
            AND override_title IS NULL
            AND override_description IS NULL
        )
        OR (
            exception_type = 'override'
            AND (
                override_local_start IS NOT NULL
                OR override_timezone IS NOT NULL
                OR override_duration_minutes IS NOT NULL
                OR override_title IS NOT NULL
                OR override_description IS NOT NULL
            )
        )
    ),
    CONSTRAINT class_session_exceptions_version_positive CHECK (version > 0),
    CONSTRAINT class_session_exceptions_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE INDEX class_session_exceptions_tenant_series_idx
    ON tutorhub.class_session_exceptions (tenant_id, class_id, series_id);

ALTER TABLE tutorhub.class_sessions
    ADD COLUMN series_id uuid,
    ADD COLUMN occurrence_key text,
    ADD COLUMN original_local_start timestamp without time zone,
    ADD COLUMN original_timezone text,
    ADD COLUMN original_overlap_offset_seconds integer,
    ADD CONSTRAINT class_sessions_series_fk
        FOREIGN KEY (tenant_id, class_id, series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id),
    ADD CONSTRAINT class_sessions_series_identity_consistent CHECK (
        (
            series_id IS NULL
            AND occurrence_key IS NULL
            AND original_local_start IS NULL
            AND original_timezone IS NULL
            AND original_overlap_offset_seconds IS NULL
        )
        OR (
            series_id IS NOT NULL
            AND length(occurrence_key) BETWEEN 8 AND 128
            AND original_local_start IS NOT NULL
            AND length(btrim(original_timezone)) BETWEEN 1 AND 100
            AND lower(btrim(original_timezone)) <> 'local'
            AND original_overlap_offset_seconds BETWEEN -86400 AND 86400
        )
    );

CREATE UNIQUE INDEX class_sessions_series_occurrence_idx
    ON tutorhub.class_sessions (tenant_id, series_id, occurrence_key)
    WHERE series_id IS NOT NULL;

REVOKE ALL ON tutorhub.class_session_series FROM PUBLIC;
REVOKE ALL ON tutorhub.class_session_exceptions FROM PUBLIC;

COMMIT;
