BEGIN;

ALTER TABLE tutorhub.tenant_feature_overrides
    DROP CONSTRAINT tenant_feature_overrides_key_valid;

ALTER TABLE tutorhub.tenant_feature_overrides
    ADD CONSTRAINT tenant_feature_overrides_key_valid CHECK (
        feature_key IN (
            'membership_invitations',
            'class_management',
            'class_invite_links',
            'class_session_scheduling'
        )
    );

CREATE TABLE tutorhub.class_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    starts_at timestamptz NOT NULL,
    ends_at timestamptz NOT NULL,
    timezone text NOT NULL,
    status text NOT NULL DEFAULT 'scheduled',
    version bigint NOT NULL DEFAULT 1,
    created_by uuid NOT NULL,
    updated_by uuid NOT NULL,
    cancelled_at timestamptz,
    cancelled_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT class_sessions_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_sessions_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_sessions_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_sessions_canceller_membership_fk
        FOREIGN KEY (tenant_id, cancelled_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_sessions_tenant_class_id_unique
        UNIQUE (tenant_id, class_id, id),
    CONSTRAINT class_sessions_title_valid
        CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    CONSTRAINT class_sessions_description_valid
        CHECK (length(description) <= 4000),
    CONSTRAINT class_sessions_time_range_valid CHECK (
        ends_at > starts_at
        AND ends_at <= starts_at + interval '24 hours'
    ),
    CONSTRAINT class_sessions_timezone_valid CHECK (
        length(btrim(timezone)) BETWEEN 1 AND 100
        AND lower(btrim(timezone)) <> 'local'
    ),
    CONSTRAINT class_sessions_status_valid
        CHECK (status IN ('scheduled', 'cancelled', 'live', 'ended')),
    CONSTRAINT class_sessions_version_positive CHECK (version > 0),
    CONSTRAINT class_sessions_updated_after_created
        CHECK (updated_at >= created_at),
    CONSTRAINT class_sessions_cancellation_consistent CHECK (
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

CREATE INDEX class_sessions_class_starts_idx
    ON tutorhub.class_sessions (tenant_id, class_id, starts_at, id);

CREATE INDEX class_sessions_class_ends_idx
    ON tutorhub.class_sessions (tenant_id, class_id, ends_at, id);

REVOKE ALL ON tutorhub.class_sessions FROM PUBLIC;

COMMIT;
