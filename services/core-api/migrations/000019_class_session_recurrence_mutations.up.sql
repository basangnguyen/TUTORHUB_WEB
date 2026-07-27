BEGIN;

ALTER TABLE tutorhub.class_session_series
    ADD COLUMN ical_uid text;

UPDATE tutorhub.class_session_series
SET ical_uid = id::text || '@calendar.tutorhub'
WHERE ical_uid IS NULL;

ALTER TABLE tutorhub.class_session_series
    ALTER COLUMN ical_uid SET NOT NULL,
    ADD CONSTRAINT class_session_series_ical_uid_valid
        CHECK (length(ical_uid) BETWEEN 16 AND 255),
    ADD CONSTRAINT class_session_series_tenant_ical_uid_unique
        UNIQUE (tenant_id, ical_uid);

CREATE TABLE tutorhub.class_session_mutation_receipts (
    tenant_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint text NOT NULL,
    operation text NOT NULL,
    class_id uuid NOT NULL,
    series_id uuid NOT NULL,
    result_series_id uuid NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, idempotency_key),
    CONSTRAINT class_session_mutation_receipts_series_fk
        FOREIGN KEY (tenant_id, class_id, series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_session_mutation_receipts_result_series_fk
        FOREIGN KEY (tenant_id, class_id, result_series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_session_mutation_receipts_creator_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_mutation_receipts_key_valid
        CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    CONSTRAINT class_session_mutation_receipts_fingerprint_valid
        CHECK (length(request_fingerprint) = 64),
    CONSTRAINT class_session_mutation_receipts_operation_valid
        CHECK (operation IN ('update', 'cancel'))
);

CREATE INDEX class_session_mutation_receipts_created_idx
    ON tutorhub.class_session_mutation_receipts (tenant_id, created_at);

REVOKE ALL ON tutorhub.class_session_mutation_receipts FROM PUBLIC;

COMMIT;
