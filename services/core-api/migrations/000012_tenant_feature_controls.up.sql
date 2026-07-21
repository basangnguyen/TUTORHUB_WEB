BEGIN;

CREATE TABLE tutorhub.tenant_feature_control_revisions (
    tenant_id uuid PRIMARY KEY
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    version bigint NOT NULL DEFAULT 0,
    updated_by uuid NOT NULL
        REFERENCES tutorhub.users (id) ON DELETE RESTRICT,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenant_feature_control_revisions_version_non_negative
        CHECK (version >= 0)
);

CREATE TABLE tutorhub.tenant_feature_overrides (
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    feature_key text NOT NULL,
    enabled boolean NOT NULL,
    updated_by uuid NOT NULL
        REFERENCES tutorhub.users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, feature_key),
    CONSTRAINT tenant_feature_overrides_key_valid CHECK (
        feature_key IN (
            'membership_invitations',
            'class_management',
            'class_invite_links'
        )
    ),
    CONSTRAINT tenant_feature_overrides_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE TABLE tutorhub.tenant_quota_overrides (
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    quota_key text NOT NULL,
    limit_value bigint NOT NULL,
    updated_by uuid NOT NULL
        REFERENCES tutorhub.users (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, quota_key),
    CONSTRAINT tenant_quota_overrides_key_valid CHECK (
        quota_key IN (
            'members',
            'active_classes',
            'invite_creations_per_hour'
        )
    ),
    CONSTRAINT tenant_quota_overrides_limit_valid CHECK (
        (quota_key = 'members' AND limit_value BETWEEN 1 AND 10000)
        OR (quota_key = 'active_classes' AND limit_value BETWEEN 1 AND 1000)
        OR (
            quota_key = 'invite_creations_per_hour'
            AND limit_value BETWEEN 1 AND 10000
        )
    ),
    CONSTRAINT tenant_quota_overrides_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE TABLE tutorhub.tenant_quota_windows (
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    quota_key text NOT NULL,
    window_started_at timestamptz NOT NULL,
    window_ends_at timestamptz NOT NULL,
    used_count bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, quota_key, window_started_at),
    CONSTRAINT tenant_quota_windows_key_valid
        CHECK (quota_key = 'invite_creations_per_hour'),
    CONSTRAINT tenant_quota_windows_hourly CHECK (
        window_ends_at = window_started_at + interval '1 hour'
    ),
    CONSTRAINT tenant_quota_windows_used_non_negative
        CHECK (used_count >= 0),
    CONSTRAINT tenant_quota_windows_updated_in_window
        CHECK (updated_at >= window_started_at)
);

CREATE INDEX tenant_quota_windows_expiry_idx
    ON tutorhub.tenant_quota_windows (window_ends_at, tenant_id);

CREATE TABLE tutorhub.rate_limit_windows (
    purpose text NOT NULL,
    bucket_hash bytea NOT NULL,
    window_started_at timestamptz NOT NULL,
    window_ends_at timestamptz NOT NULL,
    used_count bigint NOT NULL DEFAULT 0,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (purpose, bucket_hash, window_started_at),
    CONSTRAINT rate_limit_windows_purpose_valid CHECK (
        length(purpose) BETWEEN 3 AND 80
        AND purpose ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'
    ),
    CONSTRAINT rate_limit_windows_bucket_hash_valid
        CHECK (octet_length(bucket_hash) = 32),
    CONSTRAINT rate_limit_windows_window_valid
        CHECK (window_ends_at > window_started_at),
    CONSTRAINT rate_limit_windows_used_non_negative
        CHECK (used_count >= 0),
    CONSTRAINT rate_limit_windows_updated_in_window
        CHECK (updated_at >= window_started_at)
);

CREATE INDEX rate_limit_windows_expiry_idx
    ON tutorhub.rate_limit_windows (window_ends_at, purpose);

REVOKE ALL ON tutorhub.tenant_feature_control_revisions FROM PUBLIC;
REVOKE ALL ON tutorhub.tenant_feature_overrides FROM PUBLIC;
REVOKE ALL ON tutorhub.tenant_quota_overrides FROM PUBLIC;
REVOKE ALL ON tutorhub.tenant_quota_windows FROM PUBLIC;
REVOKE ALL ON tutorhub.rate_limit_windows FROM PUBLIC;

COMMIT;
