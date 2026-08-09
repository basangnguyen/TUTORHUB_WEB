BEGIN;

-- A participant receipt must bind the exact tenant, space, and room instance.
-- This key also prevents a webhook from attaching an opaque participant session
-- to a different room instance in the same tenant.
ALTER TABLE tutorhub.media_participant_sessions
    ADD CONSTRAINT media_participant_sessions_instance_id_unique
        UNIQUE (tenant_id, space_id, room_instance_id, id);

CREATE TABLE tutorhub.media_provider_webhook_receipts (
    provider_kind text NOT NULL DEFAULT 'livekit',
    event_id text NOT NULL,
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    room_instance_id uuid NOT NULL,
    participant_session_id uuid,
    event_type text NOT NULL,
    disposition text NOT NULL,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    retention_until timestamptz NOT NULL,
    PRIMARY KEY (provider_kind, event_id),
    CONSTRAINT media_provider_webhook_receipts_instance_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_provider_webhook_receipts_participant_fk
        FOREIGN KEY (
            tenant_id,
            space_id,
            room_instance_id,
            participant_session_id
        )
        REFERENCES tutorhub.media_participant_sessions (
            tenant_id,
            space_id,
            room_instance_id,
            id
        ),
    CONSTRAINT media_provider_webhook_receipts_provider_valid CHECK (
        provider_kind = 'livekit'
    ),
    CONSTRAINT media_provider_webhook_receipts_event_id_valid CHECK (
        event_id ~ '^[A-Za-z0-9_-]{1,128}$'
    ),
    CONSTRAINT media_provider_webhook_receipts_event_type_valid CHECK (
        event_type ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    CONSTRAINT media_provider_webhook_receipts_disposition_valid CHECK (
        disposition IN (
            'applied',
            'ignored_stale',
            'ignored_mismatch',
            'ignored_terminal',
            'ignored_unknown_participant',
            'ignored_unsupported_event'
        )
    ),
    CONSTRAINT media_provider_webhook_receipts_retention_valid CHECK (
        retention_until > received_at
        AND retention_until <= received_at + interval '30 days'
    )
);

CREATE INDEX media_provider_webhook_receipts_retention_idx
    ON tutorhub.media_provider_webhook_receipts (
        retention_until,
        provider_kind,
        event_id
    );

CREATE INDEX media_provider_webhook_receipts_instance_time_idx
    ON tutorhub.media_provider_webhook_receipts (
        tenant_id,
        room_instance_id,
        received_at DESC,
        event_id
    );

-- Environment-specific Core API column grants are provisioned separately.
REVOKE ALL ON tutorhub.media_provider_webhook_receipts FROM PUBLIC;

COMMIT;
