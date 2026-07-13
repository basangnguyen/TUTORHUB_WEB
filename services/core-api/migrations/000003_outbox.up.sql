BEGIN;

CREATE TABLE tutorhub.outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    available_at timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0,
    published_at timestamptz,
    last_error text,
    CONSTRAINT outbox_aggregate_type_not_empty CHECK (length(btrim(aggregate_type)) BETWEEN 1 AND 100),
    CONSTRAINT outbox_event_type_not_empty CHECK (length(btrim(event_type)) BETWEEN 1 AND 200),
    CONSTRAINT outbox_payload_object CHECK (jsonb_typeof(payload) = 'object'),
    CONSTRAINT outbox_attempts_non_negative CHECK (attempts >= 0)
);

CREATE INDEX outbox_pending_idx
    ON tutorhub.outbox_events (available_at, occurred_at)
    WHERE published_at IS NULL;
CREATE INDEX outbox_tenant_aggregate_idx
    ON tutorhub.outbox_events (tenant_id, aggregate_type, aggregate_id, occurred_at DESC);

COMMIT;
