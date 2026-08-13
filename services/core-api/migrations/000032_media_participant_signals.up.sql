BEGIN;

-- P4-06 keeps PostgreSQL/Core API authoritative for participant ordering and
-- classroom signals. LiveKit attributes/events may correlate transport state,
-- but cannot allocate these counters or mutate these projections.
ALTER TABLE tutorhub.media_room_instances
    ADD COLUMN projection_version bigint NOT NULL DEFAULT 1,
    ADD COLUMN last_signal_sequence bigint NOT NULL DEFAULT 0,
    ADD COLUMN next_roster_sequence bigint NOT NULL DEFAULT 0;

ALTER TABLE tutorhub.media_room_instances
    ADD CONSTRAINT media_room_instances_projection_version_positive
        CHECK (projection_version > 0),
    ADD CONSTRAINT media_room_instances_signal_sequence_non_negative
        CHECK (last_signal_sequence >= 0),
    ADD CONSTRAINT media_room_instances_roster_sequence_non_negative
        CHECK (next_roster_sequence >= 0);

ALTER TABLE tutorhub.media_participant_sessions
    ADD COLUMN participant_key uuid DEFAULT gen_random_uuid(),
    ADD COLUMN roster_sequence bigint;

WITH ranked AS (
    SELECT id,
           row_number() OVER (
               PARTITION BY tenant_id, room_instance_id
               ORDER BY created_at, id
           ) AS roster_sequence
    FROM tutorhub.media_participant_sessions
)
UPDATE tutorhub.media_participant_sessions AS participant
SET roster_sequence = ranked.roster_sequence
FROM ranked
WHERE participant.id = ranked.id;

UPDATE tutorhub.media_room_instances AS room
SET next_roster_sequence = roster.maximum_sequence
FROM (
    SELECT tenant_id, room_instance_id, max(roster_sequence) AS maximum_sequence
    FROM tutorhub.media_participant_sessions
    GROUP BY tenant_id, room_instance_id
) AS roster
WHERE room.tenant_id = roster.tenant_id
  AND room.id = roster.room_instance_id;

ALTER TABLE tutorhub.media_participant_sessions
    ALTER COLUMN participant_key SET NOT NULL,
    ALTER COLUMN roster_sequence SET NOT NULL,
    ADD CONSTRAINT media_participant_sessions_roster_sequence_positive
        CHECK (roster_sequence > 0);

CREATE UNIQUE INDEX media_participant_sessions_room_key_unique
    ON tutorhub.media_participant_sessions (
        tenant_id,
        room_instance_id,
        participant_key
    );

CREATE UNIQUE INDEX media_participant_sessions_room_roster_sequence_unique
    ON tutorhub.media_participant_sessions (
        tenant_id,
        room_instance_id,
        roster_sequence
    );

CREATE INDEX media_participant_sessions_roster_projection_idx
    ON tutorhub.media_participant_sessions (
        tenant_id,
        room_instance_id,
        roster_sequence,
        participant_key
    )
    WHERE status IN ('joining', 'connected', 'reconnecting');

CREATE TABLE tutorhub.media_participant_hand_states (
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    room_instance_id uuid NOT NULL,
    participant_session_id uuid NOT NULL,
    is_raised boolean NOT NULL DEFAULT true,
    signal_sequence bigint NOT NULL,
    raised_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, room_instance_id, participant_session_id),
    CONSTRAINT media_participant_hand_states_instance_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_participant_hand_states_participant_fk
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
        )
        ON DELETE CASCADE,
    CONSTRAINT media_participant_hand_states_sequence_positive
        CHECK (signal_sequence > 0)
);

CREATE UNIQUE INDEX media_participant_hand_states_room_sequence_unique
    ON tutorhub.media_participant_hand_states (
        tenant_id,
        room_instance_id,
        signal_sequence
    )
    WHERE is_raised;

CREATE INDEX media_participant_hand_states_fifo_idx
    ON tutorhub.media_participant_hand_states (
        tenant_id,
        room_instance_id,
        signal_sequence,
        participant_session_id
    )
    WHERE is_raised;

CREATE TABLE tutorhub.media_reaction_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    room_instance_id uuid NOT NULL,
    participant_session_id uuid NOT NULL,
    reaction text NOT NULL,
    signal_sequence bigint NOT NULL,
    accepted_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    CONSTRAINT media_reaction_events_instance_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_reaction_events_participant_fk
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
        )
        ON DELETE CASCADE,
    CONSTRAINT media_reaction_events_reaction_valid CHECK (
        reaction IN (
            'thumbs_up',
            'clap',
            'heart',
            'celebrate',
            'laugh',
            'surprised'
        )
    ),
    CONSTRAINT media_reaction_events_sequence_positive
        CHECK (signal_sequence > 0),
    CONSTRAINT media_reaction_events_ttl_exact CHECK (
        expires_at = accepted_at + interval '10 seconds'
    )
);

CREATE UNIQUE INDEX media_reaction_events_room_sequence_unique
    ON tutorhub.media_reaction_events (
        tenant_id,
        room_instance_id,
        signal_sequence
    );

CREATE INDEX media_reaction_events_snapshot_idx
    ON tutorhub.media_reaction_events (
        tenant_id,
        room_instance_id,
        expires_at,
        signal_sequence
    );

CREATE TABLE tutorhub.media_signal_mutation_receipts (
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    room_instance_id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint bytea NOT NULL,
    kind text NOT NULL,
    result_projection_version bigint NOT NULL,
    result_signal_sequence bigint NOT NULL,
    created_at timestamptz NOT NULL,
    retention_until timestamptz NOT NULL,
    PRIMARY KEY (
        tenant_id,
        room_instance_id,
        actor_user_id,
        idempotency_key
    ),
    CONSTRAINT media_signal_mutation_receipts_instance_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_signal_mutation_receipts_actor_fk
        FOREIGN KEY (tenant_id, actor_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_signal_mutation_receipts_idempotency_valid CHECK (
        length(idempotency_key) BETWEEN 16 AND 128
        AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT media_signal_mutation_receipts_kind_valid CHECK (
        kind IN (
            'hand_raise',
            'hand_lower',
            'hand_lower_one',
            'hand_lower_all',
            'reaction'
        )
    ),
    CONSTRAINT media_signal_mutation_receipts_projection_version_positive
        CHECK (result_projection_version > 0),
    CONSTRAINT media_signal_mutation_receipts_sequence_non_negative
        CHECK (result_signal_sequence >= 0),
    CONSTRAINT media_signal_mutation_receipts_retention_exact CHECK (
        retention_until = created_at + interval '24 hours'
    )
);

CREATE INDEX media_signal_mutation_receipts_retention_idx
    ON tutorhub.media_signal_mutation_receipts (
        retention_until,
        tenant_id,
        room_instance_id,
        actor_user_id,
        idempotency_key
    );

-- Expired reaction rows and idempotency receipts are hard-deleted only through
-- these bounded maintenance entry points. Core API reads already filter expired
-- reactions, so request latency never depends on opportunistic table cleanup.
CREATE FUNCTION tutorhub.purge_expired_media_reactions(batch_size integer DEFAULT 100)
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
        SELECT reaction.id
        FROM tutorhub.media_reaction_events AS reaction
        WHERE reaction.expires_at <= pg_catalog.clock_timestamp()
        ORDER BY reaction.expires_at, reaction.id
        FOR UPDATE OF reaction SKIP LOCKED
        LIMIT batch_size
    ), deleted AS (
        DELETE FROM tutorhub.media_reaction_events AS reaction
        USING candidates
        WHERE reaction.id = candidates.id
        RETURNING 1
    )
    SELECT count(*)::integer INTO deleted_count FROM deleted;

    RETURN deleted_count;
END;
$$;

CREATE FUNCTION tutorhub.purge_expired_media_signal_receipts(batch_size integer DEFAULT 100)
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
        SELECT receipt.tenant_id,
               receipt.room_instance_id,
               receipt.actor_user_id,
               receipt.idempotency_key
        FROM tutorhub.media_signal_mutation_receipts AS receipt
        WHERE receipt.retention_until <= pg_catalog.clock_timestamp()
        ORDER BY receipt.retention_until,
                 receipt.tenant_id,
                 receipt.room_instance_id,
                 receipt.actor_user_id,
                 receipt.idempotency_key
        FOR UPDATE OF receipt SKIP LOCKED
        LIMIT batch_size
    ), deleted AS (
        DELETE FROM tutorhub.media_signal_mutation_receipts AS receipt
        USING candidates
        WHERE receipt.tenant_id = candidates.tenant_id
          AND receipt.room_instance_id = candidates.room_instance_id
          AND receipt.actor_user_id = candidates.actor_user_id
          AND receipt.idempotency_key = candidates.idempotency_key
        RETURNING 1
    )
    SELECT count(*)::integer INTO deleted_count FROM deleted;

    RETURN deleted_count;
END;
$$;

-- Environment-specific Core API column grants are provisioned separately.
REVOKE ALL ON tutorhub.media_participant_hand_states FROM PUBLIC;
REVOKE ALL ON tutorhub.media_reaction_events FROM PUBLIC;
REVOKE ALL ON tutorhub.media_signal_mutation_receipts FROM PUBLIC;
REVOKE ALL ON FUNCTION tutorhub.purge_expired_media_reactions(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION tutorhub.purge_expired_media_signal_receipts(integer) FROM PUBLIC;

COMMIT;
