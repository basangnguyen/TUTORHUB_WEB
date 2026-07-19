BEGIN;

DROP TABLE tutorhub.audit_events;
DROP FUNCTION tutorhub.reject_audit_event_mutation();
DROP FUNCTION tutorhub.audit_metadata_is_redacted(jsonb);

COMMIT;
