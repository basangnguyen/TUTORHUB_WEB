BEGIN;

DROP INDEX tutorhub.media_participant_sessions_unrestored_removed_idx;

ALTER TABLE tutorhub.media_participant_sessions
    DROP CONSTRAINT media_participant_sessions_rejoin_restore_consistent,
    DROP CONSTRAINT media_participant_sessions_rejoin_restorer_membership_fk,
    DROP COLUMN rejoin_restored_by,
    DROP COLUMN rejoin_restored_at;

ALTER TABLE tutorhub.media_space_mutation_receipts
    DROP CONSTRAINT media_space_mutation_receipts_operation_valid;

ALTER TABLE tutorhub.media_space_mutation_receipts
    ADD CONSTRAINT media_space_mutation_receipts_operation_valid CHECK (
        operation IN ('start', 'end', 'cancel')
    );

COMMIT;
