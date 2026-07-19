BEGIN;

CREATE FUNCTION tutorhub.audit_metadata_is_redacted(value jsonb)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
PARALLEL SAFE
AS $$
    SELECT
        jsonb_typeof(value) = 'object'
        AND octet_length(value::text) <= 8192
        AND NOT EXISTS (
            SELECT 1
            FROM jsonb_each(value) AS item(key, item_value)
            WHERE
                item.key !~ '^[a-z][a-z0-9_]{0,63}$'
                OR item.key ~ '(token|secret|password|cookie|session|email|name|description|payload|request_body|sql|error|stack|hash)'
                OR jsonb_typeof(item.item_value) <> 'string'
                OR length(item.item_value #>> '{}') > 256
        )
$$;

CREATE TABLE tutorhub.audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE RESTRICT,
    actor_type text NOT NULL,
    actor_user_id uuid
        REFERENCES tutorhub.users (id) ON DELETE RESTRICT,
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id uuid,
    outcome text NOT NULL,
    request_id text NOT NULL,
    request_instance_id uuid NOT NULL,
    source_ip_prefix inet,
    user_agent_hash bytea,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT audit_events_actor_type_valid
        CHECK (actor_type IN ('user', 'system')),
    CONSTRAINT audit_events_actor_consistent CHECK (
        (actor_type = 'user' AND actor_user_id IS NOT NULL)
        OR (actor_type = 'system' AND actor_user_id IS NULL)
    ),
    CONSTRAINT audit_events_action_valid CHECK (
        length(action) BETWEEN 3 AND 100
        AND action ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'
    ),
    CONSTRAINT audit_events_resource_type_valid CHECK (
        length(resource_type) BETWEEN 1 AND 80
        AND resource_type ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'
    ),
    CONSTRAINT audit_events_outcome_valid
        CHECK (outcome IN ('succeeded', 'denied', 'failed')),
    CONSTRAINT audit_events_request_id_valid CHECK (
        length(request_id) BETWEEN 1 AND 128
        AND request_id ~ '^[A-Za-z0-9._-]+$'
    ),
    CONSTRAINT audit_events_user_agent_hash_valid CHECK (
        user_agent_hash IS NULL OR octet_length(user_agent_hash) = 32
    ),
    CONSTRAINT audit_events_source_ip_prefix_valid CHECK (
        source_ip_prefix IS NULL
        OR (
            (
                (family(source_ip_prefix) = 4 AND masklen(source_ip_prefix) = 24)
                OR (family(source_ip_prefix) = 6 AND masklen(source_ip_prefix) = 56)
            )
            AND source_ip_prefix = network(source_ip_prefix)::inet
        )
    ),
    CONSTRAINT audit_events_metadata_redacted
        CHECK (tutorhub.audit_metadata_is_redacted(metadata))
);

CREATE INDEX audit_events_tenant_time_idx
    ON tutorhub.audit_events (tenant_id, occurred_at DESC, id DESC);

CREATE INDEX audit_events_tenant_action_time_idx
    ON tutorhub.audit_events (tenant_id, action, occurred_at DESC, id DESC);

CREATE INDEX audit_events_tenant_resource_time_idx
    ON tutorhub.audit_events
        (tenant_id, resource_type, resource_id, occurred_at DESC, id DESC);

CREATE INDEX audit_events_request_instance_idx
    ON tutorhub.audit_events (request_instance_id, occurred_at DESC, id DESC);

CREATE FUNCTION tutorhub.reject_audit_event_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER audit_events_immutable_rows
    BEFORE UPDATE OR DELETE ON tutorhub.audit_events
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.reject_audit_event_mutation();

CREATE TRIGGER audit_events_immutable_truncate
    BEFORE TRUNCATE ON tutorhub.audit_events
    FOR EACH STATEMENT
    EXECUTE FUNCTION tutorhub.reject_audit_event_mutation();

ALTER TABLE tutorhub.audit_events
    ENABLE ALWAYS TRIGGER audit_events_immutable_rows;
ALTER TABLE tutorhub.audit_events
    ENABLE ALWAYS TRIGGER audit_events_immutable_truncate;

REVOKE UPDATE, DELETE, TRUNCATE, TRIGGER
    ON tutorhub.audit_events FROM PUBLIC;

COMMIT;
