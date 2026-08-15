BEGIN;

ALTER TABLE tutorhub.media_space_mutation_receipts
    DROP CONSTRAINT media_space_mutation_receipts_operation_valid,
    DROP CONSTRAINT media_space_mutation_receipts_result_valid;

ALTER TABLE tutorhub.media_space_mutation_receipts
    ADD CONSTRAINT media_space_mutation_receipts_operation_valid CHECK (
        operation IN (
            'start',
            'end',
            'cancel',
            'member_invite',
            'member_revoke',
            'member_restore',
            'admission_admit',
            'admission_deny',
            'admission_cancel',
            'admission_restore',
            'lock',
            'unlock',
            'participant_promote',
            'participant_demote',
            'participant_mute',
            'participant_remove',
            'recover'
        )
    ),
    ADD CONSTRAINT media_space_mutation_receipts_result_valid CHECK (
        operation NOT IN ('start', 'recover') OR result_room_instance_id IS NOT NULL
    );

COMMIT;
