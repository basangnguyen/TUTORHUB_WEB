BEGIN;

CREATE TABLE tutorhub.media_join_diagnostics (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    room_instance_id uuid NOT NULL,
    participant_session_id uuid NOT NULL,
    join_attempt_id uuid NOT NULL,
    stage text NOT NULL,
    outcome text NOT NULL,
    error_code text NOT NULL DEFAULT '',
    network_quality text NOT NULL,
    media_path text NOT NULL,
    duration_ms integer NOT NULL,
    recorded_at timestamptz NOT NULL,
    retention_until timestamptz NOT NULL,
    CONSTRAINT media_join_diagnostics_tenant_event_unique UNIQUE (tenant_id, id),
    CONSTRAINT media_join_diagnostics_instance_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_join_diagnostics_participant_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id, participant_session_id)
        REFERENCES tutorhub.media_participant_sessions (
            tenant_id, space_id, room_instance_id, id
        )
        ON DELETE CASCADE,
    CONSTRAINT media_join_diagnostics_stage_valid CHECK (
        stage IN (
            'join_attempt', 'credential', 'connect', 'media',
            'reconnecting', 'reconnected', 'disconnected', 'leave'
        )
    ),
    CONSTRAINT media_join_diagnostics_outcome_valid CHECK (
        outcome IN ('started', 'succeeded', 'failed', 'cancelled')
    ),
    CONSTRAINT media_join_diagnostics_error_code_valid CHECK (
        error_code IN (
            '', 'participant_removed', 'room_ended', 'duplicate_identity',
            'client_leave', 'transport_disconnected', 'provider_error'
        )
    ),
    CONSTRAINT media_join_diagnostics_network_quality_valid CHECK (
        network_quality IN ('unknown', 'good', 'degraded', 'poor', 'offline')
    ),
    CONSTRAINT media_join_diagnostics_media_path_valid CHECK (
        media_path IN ('unknown', 'audio_video', 'audio_only', 'listen_only')
    ),
    CONSTRAINT media_join_diagnostics_duration_valid CHECK (
        duration_ms BETWEEN 0 AND 600000
    ),
    CONSTRAINT media_join_diagnostics_retention_exact CHECK (
        retention_until = recorded_at + interval '30 days'
    )
);

CREATE INDEX media_join_diagnostics_retention_idx
    ON tutorhub.media_join_diagnostics (retention_until, id);

CREATE INDEX media_join_diagnostics_tenant_time_idx
    ON tutorhub.media_join_diagnostics (tenant_id, recorded_at DESC, id DESC);

CREATE INDEX media_join_diagnostics_metrics_idx
    ON tutorhub.media_join_diagnostics (
        tenant_id, recorded_at, stage, outcome, join_attempt_id
    );

CREATE FUNCTION tutorhub.purge_expired_media_join_diagnostics(batch_size integer DEFAULT 100)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    deleted_count integer;
BEGIN
    IF batch_size IS NULL OR batch_size < 1 OR batch_size > 1000 THEN
        RAISE EXCEPTION 'batch_size must be between 1 and 1000'
            USING ERRCODE = '22023';
    END IF;

    WITH candidates AS (
        SELECT diagnostic.id
        FROM tutorhub.media_join_diagnostics AS diagnostic
        WHERE diagnostic.retention_until <= pg_catalog.clock_timestamp()
        ORDER BY diagnostic.retention_until, diagnostic.id
        FOR UPDATE OF diagnostic SKIP LOCKED
        LIMIT batch_size
    ), deleted AS (
        DELETE FROM tutorhub.media_join_diagnostics AS diagnostic
        USING candidates
        WHERE diagnostic.id = candidates.id
        RETURNING 1
    )
    SELECT count(*)::integer INTO deleted_count FROM deleted;

    RETURN deleted_count;
END;
$$;

REVOKE ALL ON tutorhub.media_join_diagnostics FROM PUBLIC;
REVOKE ALL ON FUNCTION tutorhub.purge_expired_media_join_diagnostics(integer) FROM PUBLIC;

COMMIT;
