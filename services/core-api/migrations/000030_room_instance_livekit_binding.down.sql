BEGIN;

DROP TABLE tutorhub.media_provider_webhook_receipts;

ALTER TABLE tutorhub.media_participant_sessions
    DROP CONSTRAINT media_participant_sessions_instance_id_unique;

COMMIT;
