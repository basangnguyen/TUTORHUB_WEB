BEGIN;

DROP FUNCTION IF EXISTS tutorhub.purge_expired_availability_polls(integer);

-- A finalized official ClassSession survives outside the poll tables. Refuse a
-- downgrade that would erase the authoritative source link and make that
-- session indistinguishable from an ordinary organizer-created session.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM tutorhub.availability_polls
        WHERE outcome_type = 'class_session'
    ) THEN
        RAISE EXCEPTION
            'cannot downgrade availability polls while finalized class-session outcomes exist'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

DROP TABLE tutorhub.study_meetings;
DROP TABLE tutorhub.availability_poll_mutation_receipts;
DROP TABLE tutorhub.availability_poll_answers;
DROP TABLE tutorhub.availability_poll_responses;
DROP TABLE tutorhub.availability_poll_capabilities;
DROP TABLE tutorhub.availability_poll_participants;

ALTER TABLE tutorhub.availability_polls
    DROP CONSTRAINT availability_polls_selected_slot_fk;

DROP TABLE tutorhub.availability_poll_slots;
DROP TABLE tutorhub.availability_polls;

DELETE FROM tutorhub.tenant_feature_overrides
WHERE feature_key = 'availability_polls';

DELETE FROM tutorhub.tenant_quota_overrides
WHERE quota_key IN (
    'active_availability_polls',
    'availability_poll_range_days',
    'availability_poll_slots',
    'availability_poll_participants',
    'availability_poll_creations_per_hour',
    'availability_poll_capability_creations_per_hour',
    'active_study_meetings',
    'study_meeting_creations_per_hour'
);

DELETE FROM tutorhub.tenant_quota_windows
WHERE quota_key IN (
    'availability_poll_creations_per_hour',
    'availability_poll_capability_creations_per_hour',
    'study_meeting_creations_per_hour'
);

ALTER TABLE tutorhub.tenant_quota_windows
    DROP CONSTRAINT tenant_quota_windows_key_valid;

ALTER TABLE tutorhub.tenant_quota_windows
    ADD CONSTRAINT tenant_quota_windows_key_valid
        CHECK (quota_key = 'invite_creations_per_hour');

ALTER TABLE tutorhub.tenant_quota_overrides
    DROP CONSTRAINT tenant_quota_overrides_key_valid,
    DROP CONSTRAINT tenant_quota_overrides_limit_valid;

ALTER TABLE tutorhub.tenant_quota_overrides
    ADD CONSTRAINT tenant_quota_overrides_key_valid CHECK (
        quota_key IN (
            'members',
            'active_classes',
            'invite_creations_per_hour'
        )
    ),
    ADD CONSTRAINT tenant_quota_overrides_limit_valid CHECK (
        (quota_key = 'members' AND limit_value BETWEEN 1 AND 10000)
        OR (quota_key = 'active_classes' AND limit_value BETWEEN 1 AND 1000)
        OR (
            quota_key = 'invite_creations_per_hour'
            AND limit_value BETWEEN 1 AND 10000
        )
    );

ALTER TABLE tutorhub.tenant_feature_overrides
    DROP CONSTRAINT tenant_feature_overrides_key_valid;

-- Keep both pre-existing Phase 3 feature keys. Migration 000018 accidentally
-- omitted in_app_notifications from the rebuilt check; restoring it here is
-- the intended pre-P3-02D catalog rather than reintroducing that omission.
ALTER TABLE tutorhub.tenant_feature_overrides
    ADD CONSTRAINT tenant_feature_overrides_key_valid CHECK (
        feature_key IN (
            'membership_invitations',
            'class_management',
            'class_invite_links',
            'class_session_scheduling',
            'class_session_recurrence',
            'in_app_notifications'
        )
    );

COMMIT;
