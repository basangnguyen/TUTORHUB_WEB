BEGIN;

DROP TABLE tutorhub.class_session_mutation_receipts;

ALTER TABLE tutorhub.class_session_series
    DROP CONSTRAINT class_session_series_tenant_ical_uid_unique,
    DROP CONSTRAINT class_session_series_ical_uid_valid,
    DROP COLUMN ical_uid;

COMMIT;
