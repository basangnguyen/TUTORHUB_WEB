BEGIN;

CREATE TABLE tutorhub.class_invite_codes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    code_hash bytea NOT NULL,
    status text NOT NULL DEFAULT 'active',
    expires_at timestamptz NOT NULL,
    usage_limit integer NOT NULL,
    usage_count integer NOT NULL DEFAULT 0,
    created_by uuid NOT NULL,
    revoked_at timestamptz,
    revoked_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT class_invite_codes_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_invite_codes_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_invite_codes_revoker_membership_fk
        FOREIGN KEY (tenant_id, revoked_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_invite_codes_hash_length
        CHECK (octet_length(code_hash) = 32),
    CONSTRAINT class_invite_codes_hash_unique UNIQUE (code_hash),
    CONSTRAINT class_invite_codes_status_valid
        CHECK (status IN ('active', 'exhausted', 'expired', 'revoked')),
    CONSTRAINT class_invite_codes_expiry_valid CHECK (
        expires_at >= created_at + interval '15 minutes'
        AND expires_at <= created_at + interval '30 days'
    ),
    CONSTRAINT class_invite_codes_usage_limit_valid
        CHECK (usage_limit BETWEEN 1 AND 1000),
    CONSTRAINT class_invite_codes_usage_count_valid
        CHECK (usage_count BETWEEN 0 AND usage_limit),
    CONSTRAINT class_invite_codes_updated_valid
        CHECK (updated_at >= created_at),
    CONSTRAINT class_invite_codes_state_consistent CHECK (
        (
            status = 'active'
            AND usage_count < usage_limit
            AND revoked_at IS NULL
            AND revoked_by IS NULL
        )
        OR (
            status = 'exhausted'
            AND usage_count = usage_limit
            AND revoked_at IS NULL
            AND revoked_by IS NULL
        )
        OR (
            status = 'expired'
            AND usage_count < usage_limit
            AND revoked_at IS NULL
            AND revoked_by IS NULL
            AND updated_at >= expires_at
        )
        OR (
            status = 'revoked'
            AND usage_count < usage_limit
            AND revoked_at IS NOT NULL
            AND revoked_by IS NOT NULL
            AND revoked_at >= created_at
            AND revoked_at < expires_at
            AND updated_at >= revoked_at
        )
    )
);

CREATE INDEX class_invite_codes_class_created_idx
    ON tutorhub.class_invite_codes
        (tenant_id, class_id, created_at DESC, id DESC);

CREATE INDEX class_invite_codes_active_expiry_idx
    ON tutorhub.class_invite_codes
        (expires_at, tenant_id, class_id)
    WHERE status = 'active';

CREATE TABLE tutorhub.class_enrollments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    user_id uuid NOT NULL,
    class_role text NOT NULL DEFAULT 'student',
    status text NOT NULL DEFAULT 'invited',
    enrolled_by uuid NOT NULL,
    joined_at timestamptz,
    suspended_at timestamptz,
    left_at timestamptz,
    removed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT class_enrollments_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_enrollments_user_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_enrollments_actor_membership_fk
        FOREIGN KEY (tenant_id, enrolled_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_enrollments_tenant_class_user_unique
        UNIQUE (tenant_id, class_id, user_id),
    CONSTRAINT class_enrollments_role_valid CHECK (
        class_role IN ('co_teacher', 'teaching_assistant', 'student')
    ),
    CONSTRAINT class_enrollments_status_valid CHECK (
        status IN ('invited', 'active', 'suspended', 'left', 'removed')
    ),
    CONSTRAINT class_enrollments_updated_valid
        CHECK (updated_at >= created_at),
    CONSTRAINT class_enrollments_state_consistent CHECK (
        (
            status = 'invited'
            AND joined_at IS NULL
            AND suspended_at IS NULL
            AND left_at IS NULL
            AND removed_at IS NULL
        )
        OR (
            status = 'active'
            AND joined_at IS NOT NULL
            AND joined_at >= created_at
            AND suspended_at IS NULL
            AND left_at IS NULL
            AND removed_at IS NULL
        )
        OR (
            status = 'suspended'
            AND joined_at IS NOT NULL
            AND suspended_at IS NOT NULL
            AND suspended_at >= joined_at
            AND updated_at >= suspended_at
            AND left_at IS NULL
            AND removed_at IS NULL
        )
        OR (
            status = 'left'
            AND joined_at IS NOT NULL
            AND left_at IS NOT NULL
            AND left_at >= joined_at
            AND updated_at >= left_at
            AND suspended_at IS NULL
            AND removed_at IS NULL
        )
        OR (
            status = 'removed'
            AND joined_at IS NOT NULL
            AND removed_at IS NOT NULL
            AND removed_at >= joined_at
            AND updated_at >= removed_at
            AND suspended_at IS NULL
            AND left_at IS NULL
        )
    )
);

CREATE INDEX class_enrollments_class_roster_idx
    ON tutorhub.class_enrollments
        (tenant_id, class_id, status, class_role, user_id);

CREATE INDEX class_enrollments_user_classes_idx
    ON tutorhub.class_enrollments
        (tenant_id, user_id, status, class_id);

COMMIT;
