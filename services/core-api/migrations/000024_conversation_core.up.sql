BEGIN;

-- P3-06 extends the server-evaluated feature catalog. A deployment guardrail
-- can still force the feature off; tenant overrides cannot bypass it.
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
            'conversations'
        )
    );

CREATE TABLE tutorhub.conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    kind text NOT NULL,
    class_id uuid,
    direct_user_low_id uuid,
    direct_user_high_id uuid,
    created_by_user_id uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT conversations_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT conversations_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id),
    CONSTRAINT conversations_direct_low_membership_fk
        FOREIGN KEY (tenant_id, direct_user_low_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT conversations_direct_high_membership_fk
        FOREIGN KEY (tenant_id, direct_user_high_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT conversations_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT conversations_shape_valid CHECK (
        (
            kind = 'direct'
            AND class_id IS NULL
            AND direct_user_low_id IS NOT NULL
            AND direct_user_high_id IS NOT NULL
            AND direct_user_low_id < direct_user_high_id
            AND created_by_user_id IN (direct_user_low_id, direct_user_high_id)
        )
        OR (
            kind = 'class'
            AND class_id IS NOT NULL
            AND direct_user_low_id IS NULL
            AND direct_user_high_id IS NULL
        )
    ),
    CONSTRAINT conversations_updated_after_created CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX conversations_direct_pair_unique
    ON tutorhub.conversations
        (tenant_id, direct_user_low_id, direct_user_high_id)
    WHERE kind = 'direct';

CREATE UNIQUE INDEX conversations_class_unique
    ON tutorhub.conversations (tenant_id, class_id)
    WHERE kind = 'class';

CREATE INDEX conversations_tenant_updated_idx
    ON tutorhub.conversations (tenant_id, updated_at DESC, id DESC);

CREATE TABLE tutorhub.conversation_members (
    tenant_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    user_id uuid NOT NULL,
    joined_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, conversation_id, user_id),
    CONSTRAINT conversation_members_conversation_fk
        FOREIGN KEY (tenant_id, conversation_id)
        REFERENCES tutorhub.conversations (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT conversation_members_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id)
);

CREATE INDEX conversation_members_user_list_idx
    ON tutorhub.conversation_members
        (tenant_id, user_id, joined_at DESC, conversation_id);

-- Environment-specific Core API grants are provisioned separately. Keep the
-- migration deny-by-default and never expose these tenant tables via PUBLIC.
REVOKE ALL ON tutorhub.conversations FROM PUBLIC;
REVOKE ALL ON tutorhub.conversation_members FROM PUBLIC;

COMMIT;
