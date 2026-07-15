BEGIN;

CREATE TABLE tutorhub.livekit_webhook_events (
    event_id text PRIMARY KEY,
    event_type text NOT NULL,
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    room_name text NOT NULL,
    participant_identity text,
    occurred_at timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT livekit_webhook_event_type_valid
        CHECK (length(btrim(event_type)) BETWEEN 1 AND 64),
    CONSTRAINT livekit_webhook_event_id_valid
        CHECK (event_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    CONSTRAINT livekit_webhook_room_name_valid
        CHECK (length(room_name) BETWEEN 1 AND 255),
    CONSTRAINT livekit_webhook_participant_identity_valid
        CHECK (participant_identity IS NULL OR length(participant_identity) <= 255),
    CONSTRAINT livekit_webhook_class_fk
        FOREIGN KEY (tenant_id, class_id)
        REFERENCES tutorhub.classes (tenant_id, id)
        ON DELETE CASCADE
);

CREATE INDEX livekit_webhook_class_time_idx
    ON tutorhub.livekit_webhook_events (tenant_id, class_id, occurred_at DESC);

COMMIT;
