BEGIN;

-- Promote the disposable Gate F checkpoint fixture into a bounded, owned
-- runtime relation. This stores only the latest crash-recovery state for each
-- generation, never an operation/history stream.
CREATE TABLE tutorhub.whiteboard_document_checkpoints (
    tenant_id uuid NOT NULL,
    document_id uuid NOT NULL,
    generation bigint NOT NULL,
    provider_document_name text NOT NULL,
    schema_version integer NOT NULL,
    provider_version text NOT NULL,
    causal_watermark bigint NOT NULL,
    yjs_state bytea NOT NULL,
    byte_length integer NOT NULL,
    checksum bytea NOT NULL,
    writer_fence bigint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, document_id, generation),
    CONSTRAINT whiteboard_document_checkpoints_generation_fk
        FOREIGN KEY (tenant_id, document_id, generation)
        REFERENCES tutorhub.whiteboard_document_generations (
            tenant_id, document_id, generation
        )
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_document_checkpoints_provider_unique
        UNIQUE (provider_document_name),
    CONSTRAINT whiteboard_document_checkpoints_provider_valid CHECK (
        provider_document_name ~ '^wb_[A-Za-z0-9_-]{22,125}$'
    ),
    CONSTRAINT whiteboard_document_checkpoints_version_valid CHECK (
        schema_version = 1
        AND provider_version = 'hocuspocus@4.6.0+yjs@13.6.27'
    ),
    CONSTRAINT whiteboard_document_checkpoints_state_valid CHECK (
        byte_length BETWEEN 1 AND 20971520
        AND octet_length(yjs_state) = byte_length
        AND octet_length(checksum) = 32
    ),
    CONSTRAINT whiteboard_document_checkpoints_fence_valid CHECK (
        generation > 0 AND causal_watermark > 0 AND writer_fence > 0
    )
);

-- P5-COLLAB-07 durable commands contain bounded metadata only. Yjs state,
-- portable scene bytes and provider operations must never be stored here.
CREATE TABLE tutorhub.whiteboard_artifact_commands (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    document_id uuid NOT NULL,
    generation bigint NOT NULL,
    command_kind text NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    idempotency_key text NOT NULL,
    request_fingerprint bytea NOT NULL,
    source_snapshot_id uuid,
    target_generation bigint,
    target_provider_document_name text,
    lease_owner text,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    result_snapshot_id uuid,
    failure_code text,
    requested_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT whiteboard_artifact_commands_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT whiteboard_artifact_commands_document_fk
        FOREIGN KEY (tenant_id, document_id)
        REFERENCES tutorhub.whiteboard_documents (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_artifact_commands_generation_fk
        FOREIGN KEY (tenant_id, document_id, generation)
        REFERENCES tutorhub.whiteboard_document_generations (
            tenant_id, document_id, generation
        )
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_artifact_commands_actor_membership_fk
        FOREIGN KEY (tenant_id, actor_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_artifact_commands_source_snapshot_fk
        FOREIGN KEY (tenant_id, document_id, source_snapshot_id)
        REFERENCES tutorhub.whiteboard_snapshots (tenant_id, document_id, id)
        ON DELETE SET NULL (source_snapshot_id),
    CONSTRAINT whiteboard_artifact_commands_result_snapshot_fk
        FOREIGN KEY (tenant_id, document_id, result_snapshot_id)
        REFERENCES tutorhub.whiteboard_snapshots (tenant_id, document_id, id)
        ON DELETE SET NULL (result_snapshot_id),
    CONSTRAINT whiteboard_artifact_commands_kind_valid CHECK (
        command_kind IN ('snapshot', 'export', 'restore', 'import_validate')
    ),
    CONSTRAINT whiteboard_artifact_commands_status_valid CHECK (
        status IN ('pending', 'processing', 'succeeded', 'failed', 'quarantined')
    ),
    CONSTRAINT whiteboard_artifact_commands_generation_positive CHECK (
        generation > 0 AND (target_generation IS NULL OR target_generation > generation)
    ),
    CONSTRAINT whiteboard_artifact_commands_idempotency_valid CHECK (
        length(idempotency_key) BETWEEN 16 AND 128
        AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT whiteboard_artifact_commands_idempotency_unique
        UNIQUE (tenant_id, actor_user_id, idempotency_key),
    CONSTRAINT whiteboard_artifact_commands_restore_shape CHECK (
        (
            command_kind = 'restore'
            AND source_snapshot_id IS NOT NULL
            AND target_generation IS NOT NULL
            AND target_provider_document_name IS NOT NULL
        ) OR (
            command_kind <> 'restore'
            AND source_snapshot_id IS NULL
            AND target_generation IS NULL
            AND target_provider_document_name IS NULL
        )
    ),
    CONSTRAINT whiteboard_artifact_commands_provider_name_valid CHECK (
        target_provider_document_name IS NULL OR (
            length(target_provider_document_name) BETWEEN 25 AND 128
            AND target_provider_document_name ~ '^wb_[A-Za-z0-9_-]{22,125}$'
        )
    ),
    CONSTRAINT whiteboard_artifact_commands_lease_shape CHECK (
        (
            status = 'processing'
            AND lease_owner IS NOT NULL
            AND lease_token IS NOT NULL
            AND lease_until IS NOT NULL
            AND started_at IS NOT NULL
        ) OR (
            status <> 'processing'
            AND lease_owner IS NULL
            AND lease_token IS NULL
            AND lease_until IS NULL
        )
    ),
    CONSTRAINT whiteboard_artifact_commands_attempts_valid CHECK (
        attempts BETWEEN 0 AND 5
    ),
    CONSTRAINT whiteboard_artifact_commands_failure_code_valid CHECK (
        failure_code IS NULL OR (
            length(failure_code) BETWEEN 3 AND 64
            AND failure_code ~ '^[a-z][a-z0-9_]*$'
        )
    ),
    CONSTRAINT whiteboard_artifact_commands_completion_shape CHECK (
        (status IN ('succeeded', 'failed', 'quarantined')) = (completed_at IS NOT NULL)
        AND (status = 'succeeded' OR result_snapshot_id IS NULL)
        AND (status IN ('failed', 'quarantined') OR failure_code IS NULL)
    ),
    CONSTRAINT whiteboard_artifact_commands_time_order CHECK (
        updated_at >= requested_at
        AND (started_at IS NULL OR started_at >= requested_at)
        AND (completed_at IS NULL OR completed_at >= requested_at)
    )
);

CREATE INDEX whiteboard_artifact_commands_claim_idx
    ON tutorhub.whiteboard_artifact_commands (
        status, available_at, lease_until, requested_at, id
    ) WHERE status IN ('pending', 'processing');

CREATE INDEX whiteboard_artifact_commands_document_idx
    ON tutorhub.whiteboard_artifact_commands (
        tenant_id, document_id, requested_at DESC, id DESC
    );

-- ADR-0034 requires a random opaque 192-bit token. Existing 256-bit prototype
-- keys remain valid so a forward migration never strands a retained artifact.
ALTER TABLE tutorhub.whiteboard_snapshots
    DROP CONSTRAINT whiteboard_snapshots_object_key_valid;
ALTER TABLE tutorhub.whiteboard_snapshots
    ADD CONSTRAINT whiteboard_snapshots_object_key_valid CHECK (
        object_key ~ '^wb/[a-f0-9]{2}/([a-f0-9]{48}|[a-f0-9]{64})$'
    );

ALTER TABLE tutorhub.whiteboard_snapshots
    DROP CONSTRAINT whiteboard_snapshots_kind_valid;
ALTER TABLE tutorhub.whiteboard_snapshots
    ADD CONSTRAINT whiteboard_snapshots_kind_valid CHECK (
        snapshot_kind IN (
            'automatic', 'checkpoint', 'manual', 'export', 'pre_restore', 'import'
        )
    );

ALTER TABLE tutorhub.whiteboard_snapshots
    DROP CONSTRAINT whiteboard_snapshots_size_valid;
ALTER TABLE tutorhub.whiteboard_snapshots
    ADD CONSTRAINT whiteboard_snapshots_size_valid CHECK (
        size_bytes BETWEEN 1 AND 33554432
    );

-- A snapshot row stays authoritative until exact B2-version deletion succeeds.
-- The maintenance role claims only bounded metadata work; it never sees board
-- content and the Core API runtime never receives DELETE on this table.
CREATE TABLE tutorhub.whiteboard_artifact_purge_queue (
    snapshot_id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    document_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    lease_owner text,
    lease_token uuid,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    available_at timestamptz NOT NULL DEFAULT now(),
    failure_code text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT whiteboard_artifact_purge_snapshot_fk
        FOREIGN KEY (tenant_id, document_id, snapshot_id)
        REFERENCES tutorhub.whiteboard_snapshots (tenant_id, document_id, id)
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_artifact_purge_status_valid CHECK (
        status IN ('pending', 'processing', 'failed')
    ),
    CONSTRAINT whiteboard_artifact_purge_lease_shape CHECK (
        (
            status = 'processing'
            AND lease_owner IS NOT NULL
            AND lease_token IS NOT NULL
            AND lease_until IS NOT NULL
        ) OR (
            status <> 'processing'
            AND lease_owner IS NULL
            AND lease_token IS NULL
            AND lease_until IS NULL
        )
    ),
    CONSTRAINT whiteboard_artifact_purge_attempts_valid CHECK (
        attempts BETWEEN 0 AND 10
    ),
    CONSTRAINT whiteboard_artifact_purge_failure_code_valid CHECK (
        failure_code IS NULL OR (
            length(failure_code) BETWEEN 3 AND 64
            AND failure_code ~ '^[a-z][a-z0-9_]*$'
        )
    )
);

CREATE INDEX whiteboard_artifact_purge_claim_idx
    ON tutorhub.whiteboard_artifact_purge_queue (
        status, available_at, lease_until, created_at, snapshot_id
    );

-- Insert only eligible, expired and unreferenced snapshots. Row locking plus
-- SKIP LOCKED permits concurrent maintenance calls without duplicate work.
CREATE FUNCTION tutorhub.enqueue_whiteboard_snapshot_purge(p_batch integer)
RETURNS integer
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, tutorhub
AS $$
DECLARE
    inserted_count integer;
BEGIN
    IF p_batch < 1 OR p_batch > 100 THEN
        RAISE EXCEPTION 'whiteboard_purge_batch_invalid' USING ERRCODE = '22023';
    END IF;

    WITH candidates AS (
        SELECT snapshot.tenant_id, snapshot.document_id, snapshot.id
        FROM tutorhub.whiteboard_snapshots AS snapshot
        WHERE snapshot.retention_until <= transaction_timestamp()
          AND NOT EXISTS (
              SELECT 1
              FROM tutorhub.whiteboard_document_generations AS generation
              WHERE generation.tenant_id = snapshot.tenant_id
                AND generation.document_id = snapshot.document_id
                AND generation.restored_from_snapshot_id = snapshot.id
          )
          AND NOT EXISTS (
              SELECT 1
              FROM tutorhub.whiteboard_artifact_commands AS command
              WHERE command.tenant_id = snapshot.tenant_id
                AND command.document_id = snapshot.document_id
                AND command.status IN ('pending', 'processing')
                AND (
                    command.source_snapshot_id = snapshot.id
                    OR command.result_snapshot_id = snapshot.id
                )
          )
        ORDER BY snapshot.retention_until, snapshot.id
        FOR UPDATE OF snapshot SKIP LOCKED
        LIMIT p_batch
    )
    INSERT INTO tutorhub.whiteboard_artifact_purge_queue (
        snapshot_id, tenant_id, document_id
    )
    SELECT id, tenant_id, document_id FROM candidates
    ON CONFLICT (snapshot_id) DO NOTHING;

    GET DIAGNOSTICS inserted_count = ROW_COUNT;
    RETURN inserted_count;
END;
$$;

CREATE FUNCTION tutorhub.claim_whiteboard_snapshot_purge(
    p_owner text, p_batch integer, p_lease_seconds integer
)
RETURNS TABLE (
    snapshot_id uuid, object_key text, object_version_id text, lease_token uuid
)
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, tutorhub
AS $$
BEGIN
    IF p_owner !~ '^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$'
       OR p_batch < 1 OR p_batch > 25
       OR p_lease_seconds < 10 OR p_lease_seconds > 120 THEN
        RAISE EXCEPTION 'whiteboard_purge_claim_invalid' USING ERRCODE = '22023';
    END IF;

    PERFORM tutorhub.enqueue_whiteboard_snapshot_purge(LEAST(100, p_batch * 2));
    RETURN QUERY
    WITH candidates AS (
        SELECT queue.snapshot_id
        FROM tutorhub.whiteboard_artifact_purge_queue AS queue
        WHERE queue.attempts < 10
          AND (
              (queue.status = 'pending' AND queue.available_at <= transaction_timestamp())
              OR (queue.status = 'processing' AND queue.lease_until <= transaction_timestamp())
          )
        ORDER BY queue.available_at, queue.created_at, queue.snapshot_id
        FOR UPDATE OF queue SKIP LOCKED
        LIMIT p_batch
    ), claimed AS (
        UPDATE tutorhub.whiteboard_artifact_purge_queue AS queue
        SET status = 'processing', lease_owner = p_owner,
            lease_token = gen_random_uuid(),
            lease_until = transaction_timestamp() + make_interval(secs => p_lease_seconds),
            attempts = queue.attempts + 1, failure_code = NULL,
            updated_at = transaction_timestamp()
        FROM candidates
        WHERE queue.snapshot_id = candidates.snapshot_id
        RETURNING queue.snapshot_id, queue.lease_token
    )
    SELECT claimed.snapshot_id, snapshot.object_key,
           snapshot.object_version_id, claimed.lease_token
    FROM claimed
    JOIN tutorhub.whiteboard_snapshots AS snapshot
      ON snapshot.id = claimed.snapshot_id;
END;
$$;

CREATE FUNCTION tutorhub.complete_whiteboard_snapshot_purge(
    p_snapshot_id uuid, p_lease_token uuid
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, tutorhub
AS $$
DECLARE deleted_count integer;
BEGIN
    DELETE FROM tutorhub.whiteboard_snapshots AS snapshot
    USING tutorhub.whiteboard_artifact_purge_queue AS queue
    WHERE snapshot.id = p_snapshot_id
      AND queue.snapshot_id = snapshot.id
      AND queue.status = 'processing'
      AND queue.lease_token = p_lease_token;
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count = 1;
END;
$$;

CREATE FUNCTION tutorhub.fail_whiteboard_snapshot_purge(
    p_snapshot_id uuid, p_lease_token uuid, p_failure_code text
)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = pg_catalog, tutorhub
AS $$
DECLARE updated_count integer;
BEGIN
    IF p_failure_code !~ '^[a-z][a-z0-9_]{2,63}$' THEN
        RAISE EXCEPTION 'whiteboard_purge_failure_invalid' USING ERRCODE = '22023';
    END IF;
    UPDATE tutorhub.whiteboard_artifact_purge_queue
    SET status = CASE WHEN attempts < 10 THEN 'pending' ELSE 'failed' END,
        available_at = CASE WHEN attempts < 10
            THEN transaction_timestamp() + make_interval(secs => LEAST(300, attempts * attempts))
            ELSE available_at END,
        failure_code = CASE WHEN attempts < 10 THEN NULL ELSE p_failure_code END,
        lease_owner = NULL, lease_token = NULL, lease_until = NULL,
        updated_at = transaction_timestamp()
    WHERE snapshot_id = p_snapshot_id
      AND status = 'processing' AND lease_token = p_lease_token;
    GET DIAGNOSTICS updated_count = ROW_COUNT;
    RETURN updated_count = 1;
END;
$$;

REVOKE ALL ON tutorhub.whiteboard_artifact_commands FROM PUBLIC;
REVOKE ALL ON tutorhub.whiteboard_artifact_purge_queue FROM PUBLIC;
REVOKE ALL ON tutorhub.whiteboard_document_checkpoints FROM PUBLIC;
REVOKE ALL ON FUNCTION tutorhub.enqueue_whiteboard_snapshot_purge(integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION tutorhub.claim_whiteboard_snapshot_purge(text, integer, integer) FROM PUBLIC;
REVOKE ALL ON FUNCTION tutorhub.complete_whiteboard_snapshot_purge(uuid, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION tutorhub.fail_whiteboard_snapshot_purge(uuid, uuid, text) FROM PUBLIC;

COMMIT;
