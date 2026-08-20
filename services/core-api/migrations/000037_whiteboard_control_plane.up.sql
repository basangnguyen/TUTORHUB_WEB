BEGIN;

-- P5-COLLAB-02 stores only whiteboard control-plane metadata. Canonical Yjs
-- document state, causal operations, awareness and undo history remain owned
-- by the collaboration data plane and must never be persisted in these tables.
CREATE TABLE tutorhub.whiteboard_documents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    media_space_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'created',
    version bigint NOT NULL DEFAULT 1,
    current_generation bigint NOT NULL DEFAULT 1,
    revoke_generation bigint NOT NULL DEFAULT 1,
    create_idempotency_key text NOT NULL,
    create_request_fingerprint bytea NOT NULL,
    created_by uuid NOT NULL,
    updated_by uuid NOT NULL,
    opened_at timestamptz,
    opened_by uuid,
    suspended_at timestamptz,
    suspended_by uuid,
    resumed_at timestamptz,
    resumed_by uuid,
    closed_at timestamptz,
    closed_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT whiteboard_documents_tenant_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT whiteboard_documents_source_unique UNIQUE (tenant_id, media_space_id),
    CONSTRAINT whiteboard_documents_tenant_fk
        FOREIGN KEY (tenant_id)
        REFERENCES tutorhub.tenants (id)
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_documents_media_space_fk
        FOREIGN KEY (tenant_id, media_space_id)
        REFERENCES tutorhub.media_spaces (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_documents_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_documents_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_documents_opener_membership_fk
        FOREIGN KEY (tenant_id, opened_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_documents_suspender_membership_fk
        FOREIGN KEY (tenant_id, suspended_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_documents_resumer_membership_fk
        FOREIGN KEY (tenant_id, resumed_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_documents_closer_membership_fk
        FOREIGN KEY (tenant_id, closed_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_documents_status_valid CHECK (
        status IN ('created', 'open', 'suspended', 'closed')
    ),
    CONSTRAINT whiteboard_documents_version_positive CHECK (version > 0),
    CONSTRAINT whiteboard_documents_generation_positive CHECK (
        current_generation > 0 AND revoke_generation > 0
    ),
    CONSTRAINT whiteboard_documents_create_idempotency_valid CHECK (
        length(create_idempotency_key) BETWEEN 16 AND 128
        AND create_idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(create_request_fingerprint) = 32
    ),
    CONSTRAINT whiteboard_documents_create_idempotency_unique
        UNIQUE (tenant_id, created_by, create_idempotency_key),
    CONSTRAINT whiteboard_documents_updated_after_created CHECK (
        updated_at >= created_at
    ),
    CONSTRAINT whiteboard_documents_lifecycle_consistent CHECK (
        (
            status = 'created'
            AND opened_at IS NULL AND opened_by IS NULL
            AND suspended_at IS NULL AND suspended_by IS NULL
            AND resumed_at IS NULL AND resumed_by IS NULL
            AND closed_at IS NULL AND closed_by IS NULL
        )
        OR (
            status = 'open'
            AND opened_at IS NOT NULL AND opened_by IS NOT NULL
            AND opened_at >= created_at AND opened_at <= updated_at
            AND closed_at IS NULL AND closed_by IS NULL
            AND (
                (
                    suspended_at IS NULL AND suspended_by IS NULL
                    AND resumed_at IS NULL AND resumed_by IS NULL
                )
                OR (
                    suspended_at IS NOT NULL AND suspended_by IS NOT NULL
                    AND resumed_at IS NOT NULL AND resumed_by IS NOT NULL
                    AND suspended_at >= opened_at
                    AND resumed_at >= suspended_at
                    AND resumed_at <= updated_at
                )
            )
        )
        OR (
            status = 'suspended'
            AND opened_at IS NOT NULL AND opened_by IS NOT NULL
            AND suspended_at IS NOT NULL AND suspended_by IS NOT NULL
            AND opened_at >= created_at
            AND suspended_at >= opened_at
            AND suspended_at <= updated_at
            AND closed_at IS NULL AND closed_by IS NULL
            AND (
                (resumed_at IS NULL AND resumed_by IS NULL)
                OR (
                    resumed_at IS NOT NULL AND resumed_by IS NOT NULL
                    AND resumed_at >= opened_at
                    AND suspended_at >= resumed_at
                )
            )
        )
        OR (
            status = 'closed'
            AND closed_at IS NOT NULL AND closed_by IS NOT NULL
            AND closed_at >= created_at AND closed_at <= updated_at
            AND (
                (opened_at IS NULL AND opened_by IS NULL)
                OR (
                    opened_at IS NOT NULL AND opened_by IS NOT NULL
                    AND opened_at >= created_at AND closed_at >= opened_at
                )
            )
            AND (
                (suspended_at IS NULL AND suspended_by IS NULL)
                OR (
                    suspended_at IS NOT NULL AND suspended_by IS NOT NULL
                    AND opened_at IS NOT NULL AND suspended_at >= opened_at
                    AND suspended_at <= closed_at
                )
            )
            AND (
                (resumed_at IS NULL AND resumed_by IS NULL)
                OR (
                    resumed_at IS NOT NULL AND resumed_by IS NOT NULL
                    AND suspended_at IS NOT NULL
                    AND resumed_at >= suspended_at AND resumed_at <= closed_at
                )
            )
        )
    )
);

CREATE INDEX whiteboard_documents_tenant_status_updated_idx
    ON tutorhub.whiteboard_documents (tenant_id, status, updated_at DESC, id DESC);

-- One row is one immutable canonical generation descriptor. The document row
-- below points at exactly one descriptor through a deferred composite FK.
CREATE TABLE tutorhub.whiteboard_document_generations (
    tenant_id uuid NOT NULL,
    document_id uuid NOT NULL,
    generation bigint NOT NULL,
    authority_kind text NOT NULL DEFAULT 'yjs',
    authority_version text NOT NULL DEFAULT '13.6.27',
    provider_kind text NOT NULL DEFAULT 'hocuspocus',
    provider_version text NOT NULL DEFAULT '4.6.0',
    provider_document_name text NOT NULL,
    reason text NOT NULL,
    restored_from_snapshot_id uuid,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, document_id, generation),
    CONSTRAINT whiteboard_generations_document_fk
        FOREIGN KEY (tenant_id, document_id)
        REFERENCES tutorhub.whiteboard_documents (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_generations_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_generations_generation_positive CHECK (generation > 0),
    CONSTRAINT whiteboard_generations_authority_exact CHECK (
        authority_kind = 'yjs' AND authority_version = '13.6.27'
    ),
    CONSTRAINT whiteboard_generations_provider_exact CHECK (
        provider_kind = 'hocuspocus' AND provider_version = '4.6.0'
    ),
    CONSTRAINT whiteboard_generations_provider_name_valid CHECK (
        length(provider_document_name) BETWEEN 25 AND 128
        AND provider_document_name ~ '^wb_[A-Za-z0-9_-]{22,125}$'
    ),
    CONSTRAINT whiteboard_generations_provider_name_unique
        UNIQUE (provider_kind, provider_document_name),
    CONSTRAINT whiteboard_generations_reason_valid CHECK (
        reason IN ('initial', 'restore', 'import', 'provider_migration')
    ),
    CONSTRAINT whiteboard_generations_restore_shape_valid CHECK (
        (reason = 'initial' AND generation = 1 AND restored_from_snapshot_id IS NULL)
        OR (reason = 'restore' AND generation > 1 AND restored_from_snapshot_id IS NOT NULL)
        OR (
            reason IN ('import', 'provider_migration')
            AND generation > 1
            AND restored_from_snapshot_id IS NULL
        )
    )
);

ALTER TABLE tutorhub.whiteboard_documents
    ADD CONSTRAINT whiteboard_documents_current_generation_fk
        FOREIGN KEY (tenant_id, id, current_generation)
        REFERENCES tutorhub.whiteboard_document_generations (
            tenant_id, document_id, generation
        )
        DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE tutorhub.whiteboard_capability_policies (
    tenant_id uuid NOT NULL,
    document_id uuid NOT NULL,
    audience text NOT NULL,
    capability text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_by uuid NOT NULL,
    updated_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, document_id, audience),
    CONSTRAINT whiteboard_capability_policies_document_fk
        FOREIGN KEY (tenant_id, document_id)
        REFERENCES tutorhub.whiteboard_documents (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_capability_policies_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_capability_policies_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_capability_policies_audience_valid CHECK (
        audience IN (
            'organization_admin', 'host', 'co_host',
            'teaching_assistant', 'attendee'
        )
    ),
    CONSTRAINT whiteboard_capability_policies_capability_valid CHECK (
        capability IN ('view', 'edit', 'present')
    ),
    CONSTRAINT whiteboard_capability_policies_version_positive CHECK (version > 0),
    CONSTRAINT whiteboard_capability_policies_updated_after_created CHECK (
        updated_at >= created_at
    )
);

CREATE TABLE tutorhub.whiteboard_snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    document_id uuid NOT NULL,
    generation bigint NOT NULL,
    snapshot_kind text NOT NULL,
    format_version text NOT NULL,
    engine_version text NOT NULL DEFAULT '0.18.1',
    authority_version text NOT NULL DEFAULT '13.6.27',
    schema_version integer NOT NULL,
    causal_watermark_sha256 bytea NOT NULL,
    content_sha256 bytea NOT NULL,
    size_bytes bigint NOT NULL,
    object_key text NOT NULL,
    object_version_id text NOT NULL,
    verification_key_id text NOT NULL,
    provenance_kind text NOT NULL,
    created_by uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    retention_policy text NOT NULL DEFAULT 'private_alpha_14d',
    retention_until timestamptz NOT NULL DEFAULT (now() + interval '14 days'),
    CONSTRAINT whiteboard_snapshots_tenant_document_id_unique
        UNIQUE (tenant_id, document_id, id),
    CONSTRAINT whiteboard_snapshots_generation_fk
        FOREIGN KEY (tenant_id, document_id, generation)
        REFERENCES tutorhub.whiteboard_document_generations (
            tenant_id, document_id, generation
        )
        ON DELETE RESTRICT,
    CONSTRAINT whiteboard_snapshots_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_snapshots_kind_valid CHECK (
        snapshot_kind IN ('checkpoint', 'manual', 'pre_restore', 'import')
    ),
    CONSTRAINT whiteboard_snapshots_versions_valid CHECK (
        length(format_version) BETWEEN 1 AND 32
        AND engine_version = '0.18.1'
        AND authority_version = '13.6.27'
        AND schema_version BETWEEN 1 AND 1000
    ),
    CONSTRAINT whiteboard_snapshots_hashes_valid CHECK (
        octet_length(causal_watermark_sha256) = 32
        AND octet_length(content_sha256) = 32
    ),
    CONSTRAINT whiteboard_snapshots_size_valid CHECK (
        size_bytes BETWEEN 1 AND 67108864
    ),
    CONSTRAINT whiteboard_snapshots_object_key_valid CHECK (
        object_key ~ '^wb/[a-f0-9]{2}/[a-f0-9]{64}$'
    ),
    CONSTRAINT whiteboard_snapshots_object_key_unique UNIQUE (object_key),
    CONSTRAINT whiteboard_snapshots_object_version_valid CHECK (
        length(btrim(object_version_id)) BETWEEN 1 AND 255
        AND object_version_id !~ '[[:cntrl:]]'
    ),
    CONSTRAINT whiteboard_snapshots_verification_key_valid CHECK (
        length(verification_key_id) BETWEEN 8 AND 128
        AND verification_key_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
    ),
    CONSTRAINT whiteboard_snapshots_provenance_valid CHECK (
        (provenance_kind = 'user' AND created_by IS NOT NULL)
        OR (provenance_kind = 'service' AND created_by IS NULL)
    ),
    CONSTRAINT whiteboard_snapshots_retention_exact CHECK (
        retention_policy = 'private_alpha_14d'
        AND retention_until = created_at + interval '14 days'
    )
);

ALTER TABLE tutorhub.whiteboard_document_generations
    ADD CONSTRAINT whiteboard_generations_restore_snapshot_fk
        FOREIGN KEY (tenant_id, document_id, restored_from_snapshot_id)
        REFERENCES tutorhub.whiteboard_snapshots (tenant_id, document_id, id)
        DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX whiteboard_snapshots_document_created_idx
    ON tutorhub.whiteboard_snapshots (
        tenant_id, document_id, created_at DESC, id DESC
    );

CREATE INDEX whiteboard_snapshots_retention_idx
    ON tutorhub.whiteboard_snapshots (retention_until, id);

-- These rows are bounded business-command idempotency receipts, not a live
-- whiteboard operation log and not a source for reconstructing Yjs history.
CREATE TABLE tutorhub.whiteboard_document_mutation_receipts (
    tenant_id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint bytea NOT NULL,
    operation text NOT NULL,
    document_id uuid NOT NULL,
    result_document_version bigint NOT NULL,
    result_generation bigint NOT NULL,
    result_revoke_generation bigint NOT NULL,
    result_status text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, actor_user_id, idempotency_key),
    CONSTRAINT whiteboard_mutation_receipts_document_fk
        FOREIGN KEY (tenant_id, document_id)
        REFERENCES tutorhub.whiteboard_documents (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT whiteboard_mutation_receipts_generation_fk
        FOREIGN KEY (tenant_id, document_id, result_generation)
        REFERENCES tutorhub.whiteboard_document_generations (
            tenant_id, document_id, generation
        ),
    CONSTRAINT whiteboard_mutation_receipts_actor_membership_fk
        FOREIGN KEY (tenant_id, actor_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT whiteboard_mutation_receipts_idempotency_valid CHECK (
        length(idempotency_key) BETWEEN 16 AND 128
        AND idempotency_key ~ '^[A-Za-z0-9][A-Za-z0-9._:-]*$'
        AND octet_length(request_fingerprint) = 32
    ),
    CONSTRAINT whiteboard_mutation_receipts_operation_valid CHECK (
        operation IN (
            'create', 'open', 'suspend', 'resume', 'close',
            'capability_set', 'restore'
        )
    ),
    CONSTRAINT whiteboard_mutation_receipts_result_positive CHECK (
        result_document_version > 0
        AND result_generation > 0
        AND result_revoke_generation > 0
    ),
    CONSTRAINT whiteboard_mutation_receipts_status_valid CHECK (
        result_status IN ('created', 'open', 'suspended', 'closed')
    )
);

CREATE INDEX whiteboard_mutation_receipts_document_created_idx
    ON tutorhub.whiteboard_document_mutation_receipts (
        tenant_id, document_id, created_at DESC, actor_user_id, idempotency_key
    );

-- Environment-specific runtime and maintenance grants are provisioned only
-- after the migration through the reviewed disposable acceptance harness.
REVOKE ALL ON tutorhub.whiteboard_documents FROM PUBLIC;
REVOKE ALL ON tutorhub.whiteboard_document_generations FROM PUBLIC;
REVOKE ALL ON tutorhub.whiteboard_capability_policies FROM PUBLIC;
REVOKE ALL ON tutorhub.whiteboard_snapshots FROM PUBLIC;
REVOKE ALL ON tutorhub.whiteboard_document_mutation_receipts FROM PUBLIC;

COMMIT;
