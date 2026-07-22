BEGIN;

CREATE TABLE tutorhub.legacy_import_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    source_system text NOT NULL,
    fixture_key text NOT NULL,
    fixture_sha256 bytea NOT NULL,
    status text NOT NULL DEFAULT 'running',
    total_records integer NOT NULL,
    checkpoint_ordinal integer NOT NULL DEFAULT 0,
    failure_attempts integer NOT NULL DEFAULT 0,
    last_error_code text,
    started_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    CONSTRAINT legacy_import_runs_source_valid CHECK (
        source_system ~ '^[a-z0-9][a-z0-9._-]{2,63}$'
    ),
    CONSTRAINT legacy_import_runs_fixture_key_valid CHECK (
        fixture_key ~ '^[a-z0-9][a-z0-9._-]{2,127}$'
    ),
    CONSTRAINT legacy_import_runs_hash_valid CHECK (
        octet_length(fixture_sha256) = 32
    ),
    CONSTRAINT legacy_import_runs_status_valid CHECK (
        status IN ('running', 'failed', 'completed')
    ),
    CONSTRAINT legacy_import_runs_progress_valid CHECK (
        total_records >= 0
        AND checkpoint_ordinal BETWEEN 0 AND total_records
        AND failure_attempts >= 0
    ),
    CONSTRAINT legacy_import_runs_error_code_valid CHECK (
        last_error_code IS NULL
        OR last_error_code ~ '^[a-z][a-z0-9_]{2,63}$'
    ),
    CONSTRAINT legacy_import_runs_state_consistent CHECK (
        (
            status = 'completed'
            AND checkpoint_ordinal = total_records
            AND completed_at IS NOT NULL
            AND last_error_code IS NULL
        )
        OR (
            status = 'running'
            AND completed_at IS NULL
            AND last_error_code IS NULL
        )
        OR (
            status = 'failed'
            AND checkpoint_ordinal < total_records
            AND completed_at IS NULL
            AND last_error_code IS NOT NULL
        )
    )
);

CREATE UNIQUE INDEX legacy_import_runs_active_fixture_idx
    ON tutorhub.legacy_import_runs (source_system, fixture_key, fixture_sha256)
    WHERE status IN ('running', 'failed');

CREATE INDEX legacy_import_runs_history_idx
    ON tutorhub.legacy_import_runs (source_system, fixture_key, started_at DESC, id DESC);

CREATE TABLE tutorhub.legacy_import_mappings (
    source_system text NOT NULL,
    entity_type text NOT NULL,
    external_id text NOT NULL,
    target_id uuid NOT NULL,
    source_sha256 bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source_system, entity_type, external_id),
    CONSTRAINT legacy_import_mappings_source_valid CHECK (
        source_system ~ '^[a-z0-9][a-z0-9._-]{2,63}$'
    ),
    CONSTRAINT legacy_import_mappings_entity_valid CHECK (
        entity_type IN ('user', 'tenant', 'membership', 'class')
    ),
    CONSTRAINT legacy_import_mappings_external_id_valid CHECK (
        external_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    ),
    CONSTRAINT legacy_import_mappings_hash_valid CHECK (
        octet_length(source_sha256) = 32
    ),
    CONSTRAINT legacy_import_mappings_target_unique
        UNIQUE (source_system, entity_type, target_id),
    CONSTRAINT legacy_import_mappings_updated_valid CHECK (updated_at >= created_at)
);

CREATE TABLE tutorhub.legacy_import_run_items (
    run_id uuid NOT NULL
        REFERENCES tutorhub.legacy_import_runs (id) ON DELETE CASCADE,
    ordinal integer NOT NULL,
    entity_type text NOT NULL,
    external_id text NOT NULL,
    outcome text NOT NULL,
    reason_code text,
    target_id uuid,
    source_sha256 bytea NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, ordinal),
    CONSTRAINT legacy_import_run_items_entity_valid CHECK (
        entity_type IN ('user', 'tenant', 'membership', 'class')
    ),
    CONSTRAINT legacy_import_run_items_external_id_valid CHECK (
        external_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'
    ),
    CONSTRAINT legacy_import_run_items_outcome_valid CHECK (
        outcome IN ('imported', 'updated', 'unchanged', 'skipped')
    ),
    CONSTRAINT legacy_import_run_items_reason_valid CHECK (
        reason_code IS NULL OR reason_code ~ '^[a-z][a-z0-9_]{2,63}$'
    ),
    CONSTRAINT legacy_import_run_items_hash_valid CHECK (
        octet_length(source_sha256) = 32
    ),
    CONSTRAINT legacy_import_run_items_outcome_consistent CHECK (
        (outcome = 'skipped' AND reason_code IS NOT NULL AND target_id IS NULL)
        OR (outcome <> 'skipped' AND reason_code IS NULL AND target_id IS NOT NULL)
    ),
    CONSTRAINT legacy_import_run_items_external_unique
        UNIQUE (run_id, entity_type, external_id)
);

CREATE INDEX legacy_import_run_items_reconciliation_idx
    ON tutorhub.legacy_import_run_items (run_id, entity_type, outcome);

REVOKE ALL ON tutorhub.legacy_import_runs FROM PUBLIC;
REVOKE ALL ON tutorhub.legacy_import_mappings FROM PUBLIC;
REVOKE ALL ON tutorhub.legacy_import_run_items FROM PUBLIC;

COMMIT;
