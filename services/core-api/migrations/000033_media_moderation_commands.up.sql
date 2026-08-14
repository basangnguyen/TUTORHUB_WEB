BEGIN;

-- P4-07 keeps every dynamic co-host assignment scoped to one authoritative
-- RoomInstance. The assignment never changes organization or class roles and
-- remains as bounded history after it is revoked or the room becomes terminal.
CREATE TABLE tutorhub.media_room_role_assignments (
    tenant_id uuid NOT NULL,
    space_id uuid NOT NULL,
    room_instance_id uuid NOT NULL,
    user_id uuid NOT NULL,
    assigned_role text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    version bigint NOT NULL DEFAULT 1,
    assigned_by uuid NOT NULL,
    assigned_at timestamptz NOT NULL,
    revoked_by uuid,
    revoked_at timestamptz,
    reason_code text,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, room_instance_id, user_id),
    CONSTRAINT media_room_role_assignments_instance_fk
        FOREIGN KEY (tenant_id, space_id, room_instance_id)
        REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)
        ON DELETE CASCADE,
    CONSTRAINT media_room_role_assignments_target_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_room_role_assignments_assigner_membership_fk
        FOREIGN KEY (tenant_id, assigned_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_room_role_assignments_revoker_membership_fk
        FOREIGN KEY (tenant_id, revoked_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT media_room_role_assignments_role_valid CHECK (
        assigned_role = 'co_host'
    ),
    CONSTRAINT media_room_role_assignments_status_valid CHECK (
        status IN ('active', 'revoked')
    ),
    CONSTRAINT media_room_role_assignments_version_positive CHECK (version > 0),
    CONSTRAINT media_room_role_assignments_reason_code_valid CHECK (
        reason_code IS NULL
        OR (
            length(reason_code) BETWEEN 1 AND 64
            AND reason_code ~ '^[a-z][a-z0-9_.-]*$'
        )
    ),
    CONSTRAINT media_room_role_assignments_updated_after_assigned CHECK (
        updated_at >= assigned_at
    ),
    CONSTRAINT media_room_role_assignments_lifecycle_consistent CHECK (
        (
            status = 'active'
            AND revoked_by IS NULL
            AND revoked_at IS NULL
        )
        OR (
            status = 'revoked'
            AND revoked_by IS NOT NULL
            AND revoked_at IS NOT NULL
            AND revoked_at >= assigned_at
            AND revoked_at <= updated_at
        )
    )
);

CREATE INDEX media_room_role_assignments_active_idx
    ON tutorhub.media_room_role_assignments (
        tenant_id,
        room_instance_id,
        assigned_role,
        user_id
    )
    WHERE status = 'active';

-- The existing mutation receipt remains the single idempotency namespace for
-- MediaSpace commands. P4-07 extends it with exact target/result projections
-- and a durable, leaseable provider effect. Provider identifiers and raw
-- provider errors are deliberately absent; the adapter reloads opaque bindings
-- from authoritative tables only after claiming one persisted effect.
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
            'admission_restore',
            'lock',
            'unlock',
            'participant_promote',
            'participant_demote',
            'participant_mute',
            'participant_remove'
        )
    );

ALTER TABLE tutorhub.media_space_mutation_receipts
    ADD COLUMN target_participant_session_id uuid,
    ADD COLUMN result_room_instance_version bigint,
    ADD COLUMN result_projection_version bigint,
    ADD COLUMN result_participant_version bigint,
    ADD COLUMN result_role_assignment_version bigint,
    ADD COLUMN result_instance_role text,
    ADD COLUMN result_locked boolean,
    ADD COLUMN provider_effect_required boolean NOT NULL DEFAULT false,
    ADD COLUMN provider_effect_status text NOT NULL DEFAULT 'none',
    ADD COLUMN provider_effect_attempts integer NOT NULL DEFAULT 0,
    ADD COLUMN provider_effect_error_code text,
    ADD COLUMN provider_effect_lease_until timestamptz,
    ADD COLUMN provider_effect_updated_at timestamptz;

ALTER TABLE tutorhub.media_space_mutation_receipts
    ADD CONSTRAINT media_space_mutation_receipts_target_participant_fk
        FOREIGN KEY (
            tenant_id,
            space_id,
            result_room_instance_id,
            target_participant_session_id
        )
        REFERENCES tutorhub.media_participant_sessions (
            tenant_id,
            space_id,
            room_instance_id,
            id
        ),
    ADD CONSTRAINT media_space_mutation_receipts_room_version_positive CHECK (
        result_room_instance_version IS NULL OR result_room_instance_version > 0
    ),
    ADD CONSTRAINT media_space_mutation_receipts_projection_version_positive CHECK (
        result_projection_version IS NULL OR result_projection_version > 0
    ),
    ADD CONSTRAINT media_space_mutation_receipts_participant_version_positive CHECK (
        result_participant_version IS NULL OR result_participant_version > 0
    ),
    ADD CONSTRAINT media_space_mutation_receipts_role_assignment_version_positive CHECK (
        result_role_assignment_version IS NULL OR result_role_assignment_version > 0
    ),
    ADD CONSTRAINT media_space_mutation_receipts_instance_role_valid CHECK (
        result_instance_role IS NULL
        OR result_instance_role IN ('host', 'co_host', 'teaching_assistant', 'attendee')
    ),
    ADD CONSTRAINT media_space_mutation_receipts_provider_status_valid CHECK (
        provider_effect_status IN (
            'none',
            'pending',
            'applying',
            'applied',
            'retryable_failed',
            'permanent_failed'
        )
    ),
    ADD CONSTRAINT media_space_mutation_receipts_provider_attempts_non_negative CHECK (
        provider_effect_attempts >= 0
    ),
    ADD CONSTRAINT media_space_mutation_receipts_provider_error_code_valid CHECK (
        provider_effect_error_code IS NULL
        OR (
            length(provider_effect_error_code) BETWEEN 1 AND 64
            AND provider_effect_error_code ~ '^[a-z][a-z0-9_]*$'
        )
    ),
    ADD CONSTRAINT media_space_mutation_receipts_p407_target_consistent CHECK (
        (
            operation IN (
                'participant_promote',
                'participant_demote',
                'participant_mute',
                'participant_remove'
            )
            AND target_participant_session_id IS NOT NULL
            AND result_room_instance_id IS NOT NULL
            AND result_room_instance_version IS NOT NULL
            AND result_projection_version IS NOT NULL
            AND result_participant_version IS NOT NULL
        )
        OR (
            operation NOT IN (
                'participant_promote',
                'participant_demote',
                'participant_mute',
                'participant_remove'
            )
            AND target_participant_session_id IS NULL
            AND result_participant_version IS NULL
            AND result_role_assignment_version IS NULL
            AND result_instance_role IS NULL
        )
    ),
    ADD CONSTRAINT media_space_mutation_receipts_role_result_consistent CHECK (
        (
            operation IN ('participant_promote', 'participant_demote')
            AND result_role_assignment_version IS NOT NULL
            AND result_instance_role IS NOT NULL
        )
        OR (
            operation NOT IN ('participant_promote', 'participant_demote')
            AND result_role_assignment_version IS NULL
            AND result_instance_role IS NULL
        )
    ),
    ADD CONSTRAINT media_space_mutation_receipts_lock_result_consistent CHECK (
        (operation = 'lock' AND result_locked = true)
        OR (operation = 'unlock' AND result_locked = false)
        OR (operation NOT IN ('lock', 'unlock') AND result_locked IS NULL)
    ),
    ADD CONSTRAINT media_space_mutation_receipts_provider_effect_consistent CHECK (
        (
            provider_effect_required = false
            AND provider_effect_status = 'none'
            AND provider_effect_attempts = 0
            AND provider_effect_error_code IS NULL
            AND provider_effect_lease_until IS NULL
            AND provider_effect_updated_at IS NULL
        )
        OR (
            provider_effect_required = true
            AND operation IN (
                'end',
                'participant_promote',
                'participant_demote',
                'participant_mute',
                'participant_remove'
            )
            AND provider_effect_status <> 'none'
            AND provider_effect_updated_at IS NOT NULL
            AND (
                (provider_effect_status = 'applying' AND provider_effect_lease_until IS NOT NULL)
                OR (provider_effect_status <> 'applying' AND provider_effect_lease_until IS NULL)
            )
            AND (
                (
                    provider_effect_status IN ('retryable_failed', 'permanent_failed')
                    AND provider_effect_error_code IS NOT NULL
                )
                OR (
                    provider_effect_status NOT IN ('retryable_failed', 'permanent_failed')
                    AND provider_effect_error_code IS NULL
                )
            )
        )
    ),
    ADD CONSTRAINT media_space_mutation_receipts_participant_provider_required CHECK (
        operation NOT IN (
            'participant_promote',
            'participant_demote',
            'participant_mute',
            'participant_remove'
        )
        OR provider_effect_required = true
    );

CREATE INDEX media_space_mutation_receipts_provider_effect_idx
    ON tutorhub.media_space_mutation_receipts (
        provider_effect_status,
        provider_effect_lease_until,
        created_at,
        tenant_id,
        actor_user_id,
        idempotency_key
    )
    WHERE provider_effect_required
      AND provider_effect_status IN ('pending', 'applying', 'retryable_failed');

-- Environment-specific Core API column grants are provisioned separately.
REVOKE ALL ON tutorhub.media_room_role_assignments FROM PUBLIC;

COMMIT;
