BEGIN;

-- Do not erase authoritative runtime authority or provider-reconcile evidence.
-- Operators must first demote every dynamic co-host and converge every required
-- provider effect. The guard intentionally runs before the first DROP so a
-- refused rollback leaves both the schema and migration ledger untouched.
DO $p407_rollback_guard$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM tutorhub.media_room_role_assignments
        WHERE status = 'active'
    ) THEN
        RAISE EXCEPTION
            'cannot roll back P4-07 while active room role assignments exist'
            USING ERRCODE = '55000';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM tutorhub.media_space_mutation_receipts
        WHERE provider_effect_required
          AND provider_effect_status <> 'applied'
    ) THEN
        RAISE EXCEPTION
            'cannot roll back P4-07 while provider effects are unresolved'
            USING ERRCODE = '55000';
    END IF;
END
$p407_rollback_guard$;

DROP INDEX tutorhub.media_space_mutation_receipts_provider_effect_idx;

ALTER TABLE tutorhub.media_space_mutation_receipts
    DROP CONSTRAINT media_space_mutation_receipts_participant_provider_required,
    DROP CONSTRAINT media_space_mutation_receipts_provider_effect_consistent,
    DROP CONSTRAINT media_space_mutation_receipts_lock_result_consistent,
    DROP CONSTRAINT media_space_mutation_receipts_role_result_consistent,
    DROP CONSTRAINT media_space_mutation_receipts_p407_target_consistent,
    DROP CONSTRAINT media_space_mutation_receipts_provider_error_code_valid,
    DROP CONSTRAINT media_space_mutation_receipts_provider_attempts_non_negative,
    DROP CONSTRAINT media_space_mutation_receipts_provider_status_valid,
    DROP CONSTRAINT media_space_mutation_receipts_instance_role_valid,
    DROP CONSTRAINT media_space_mutation_receipts_role_assignment_version_positive,
    DROP CONSTRAINT media_space_mutation_receipts_participant_version_positive,
    DROP CONSTRAINT media_space_mutation_receipts_projection_version_positive,
    DROP CONSTRAINT media_space_mutation_receipts_room_version_positive,
    DROP CONSTRAINT media_space_mutation_receipts_target_participant_fk,
    DROP CONSTRAINT media_space_mutation_receipts_operation_valid,
    DROP COLUMN provider_effect_updated_at,
    DROP COLUMN provider_effect_lease_until,
    DROP COLUMN provider_effect_error_code,
    DROP COLUMN provider_effect_attempts,
    DROP COLUMN provider_effect_status,
    DROP COLUMN provider_effect_required,
    DROP COLUMN result_locked,
    DROP COLUMN result_instance_role,
    DROP COLUMN result_role_assignment_version,
    DROP COLUMN result_participant_version,
    DROP COLUMN result_projection_version,
    DROP COLUMN result_room_instance_version,
    DROP COLUMN target_participant_session_id;

DROP TABLE tutorhub.media_room_role_assignments;

-- Receipts for operations introduced by 000033 cannot satisfy the restored
-- pre-000033 operation constraint. They are forward-only additions, so remove
-- only those rows before restoring the old allowlist.
DELETE FROM tutorhub.media_space_mutation_receipts
WHERE operation IN (
    'lock',
    'unlock',
    'participant_promote',
    'participant_demote',
    'participant_mute',
    'participant_remove'
);

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

COMMIT;
