BEGIN;

ALTER TABLE tutorhub.media_space_mutation_receipts
    DROP CONSTRAINT media_space_mutation_receipts_operation_valid;

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
            'admission_restore'
        )
    );

ALTER TABLE tutorhub.media_participant_sessions
    ADD COLUMN rejoin_restored_at timestamptz,
    ADD COLUMN rejoin_restored_by uuid;

ALTER TABLE tutorhub.media_participant_sessions
    ADD CONSTRAINT media_participant_sessions_rejoin_restorer_membership_fk
        FOREIGN KEY (tenant_id, rejoin_restored_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    ADD CONSTRAINT media_participant_sessions_rejoin_restore_consistent CHECK (
        (
            rejoin_restored_at IS NULL
            AND rejoin_restored_by IS NULL
        )
        OR (
            status = 'removed'
            AND terminal_at IS NOT NULL
            AND rejoin_restored_at IS NOT NULL
            AND rejoin_restored_by IS NOT NULL
            AND rejoin_restored_at >= terminal_at
            AND rejoin_restored_at <= updated_at
        )
    );

CREATE INDEX media_participant_sessions_unrestored_removed_idx
    ON tutorhub.media_participant_sessions (
        tenant_id,
        room_instance_id,
        user_id,
        terminal_at DESC,
        id DESC
    )
    WHERE status = 'removed' AND rejoin_restored_at IS NULL;

COMMIT;
