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
            'in_app_notifications'
        )
    );

CREATE TABLE tutorhub.notifications (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    recipient_user_id uuid NOT NULL,
    source_outbox_event_id uuid NOT NULL,
    effect_key text NOT NULL,
    kind text NOT NULL,
    template_key text NOT NULL,
    resource_type text,
    resource_id uuid,
    context jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,
    read_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notifications_recipient_membership_fk
        FOREIGN KEY (tenant_id, recipient_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id)
        ON DELETE CASCADE,
    CONSTRAINT notifications_source_recipient_effect_unique
        UNIQUE (source_outbox_event_id, recipient_user_id, effect_key),
    CONSTRAINT notifications_effect_key_valid CHECK (
        effect_key = btrim(effect_key)
        AND length(effect_key) BETWEEN 1 AND 100
        AND effect_key ~ '^[a-z][a-z0-9_.-]{0,99}$'
    ),
    CONSTRAINT notifications_kind_valid CHECK (
        kind = btrim(kind)
        AND length(kind) BETWEEN 3 AND 100
        AND kind ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'
    ),
    CONSTRAINT notifications_template_key_valid CHECK (
        template_key = btrim(template_key)
        AND length(template_key) BETWEEN 3 AND 100
        AND template_key ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'
    ),
    CONSTRAINT notifications_resource_valid CHECK (
        (resource_type IS NULL AND resource_id IS NULL)
        OR (
            resource_type IS NOT NULL
            AND resource_id IS NOT NULL
            AND resource_type = btrim(resource_type)
            AND length(resource_type) BETWEEN 1 AND 64
            AND resource_type ~ '^[a-z][a-z0-9_]{0,63}$'
        )
    ),
    CONSTRAINT notifications_context_object CHECK (
        jsonb_typeof(context) = 'object'
        AND octet_length(context::text) <= 4096
        AND NOT jsonb_path_exists(
            context,
            '$.* ? (@.type() != "string")'
        )
    ),
    CONSTRAINT notifications_read_after_creation CHECK (
        read_at IS NULL OR read_at >= created_at
    )
);

CREATE INDEX notifications_recipient_feed_idx
    ON tutorhub.notifications (
        tenant_id,
        recipient_user_id,
        created_at DESC,
        id DESC
    )
    WHERE kind <> 'system.worker_canary';

CREATE INDEX notifications_recipient_unread_idx
    ON tutorhub.notifications (
        tenant_id,
        recipient_user_id,
        created_at DESC,
        id DESC
    )
    WHERE read_at IS NULL
      AND kind <> 'system.worker_canary';

CREATE TABLE tutorhub.notification_preferences (
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    in_app_enabled boolean NOT NULL DEFAULT true,
    email_enabled boolean NOT NULL DEFAULT false,
    reminder_offset_minutes integer NOT NULL DEFAULT 15,
    quiet_hours_enabled boolean NOT NULL DEFAULT false,
    quiet_hours_start text,
    quiet_hours_end text,
    quiet_hours_timezone text NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id),
    CONSTRAINT notification_preferences_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id)
        ON DELETE CASCADE,
    CONSTRAINT notification_preferences_reminder_offset_valid CHECK (
        reminder_offset_minutes BETWEEN 0 AND 40320
    ),
    CONSTRAINT notification_preferences_quiet_hours_valid CHECK (
        (
            NOT quiet_hours_enabled
            AND quiet_hours_start IS NULL
            AND quiet_hours_end IS NULL
        )
        OR (
            quiet_hours_enabled
            AND quiet_hours_start ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'
            AND quiet_hours_end ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'
            AND quiet_hours_start <> quiet_hours_end
        )
    ),
    CONSTRAINT notification_preferences_timezone_valid CHECK (
        quiet_hours_timezone = btrim(quiet_hours_timezone)
        AND length(quiet_hours_timezone) BETWEEN 1 AND 100
        AND lower(quiet_hours_timezone) <> 'local'
    ),
    CONSTRAINT notification_preferences_version_positive CHECK (version > 0),
    CONSTRAINT notification_preferences_updated_after_created CHECK (
        updated_at >= created_at
    )
);

REVOKE ALL ON tutorhub.notifications FROM PUBLIC;
REVOKE ALL ON tutorhub.notification_preferences FROM PUBLIC;

COMMIT;
