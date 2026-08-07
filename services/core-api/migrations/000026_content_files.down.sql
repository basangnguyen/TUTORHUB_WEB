BEGIN;

DROP TABLE tutorhub.content_files;
DROP TABLE tutorhub.tenant_file_usage;

DELETE FROM tutorhub.tenant_quota_windows
WHERE quota_key = 'file_upload_intents_per_hour';

DELETE FROM tutorhub.tenant_quota_overrides
WHERE quota_key IN (
    'files_per_tenant',
    'file_bytes_per_tenant',
    'single_file_bytes',
    'file_upload_intents_per_hour'
);

DELETE FROM tutorhub.tenant_feature_overrides
WHERE feature_key = 'file_uploads';

ALTER TABLE tutorhub.tenant_feature_overrides
    DROP CONSTRAINT tenant_feature_overrides_key_valid;

ALTER TABLE tutorhub.tenant_feature_overrides
    ADD CONSTRAINT tenant_feature_overrides_key_valid CHECK (
        feature_key IN (
            'membership_invitations',
            'class_management',
            'class_invite_links',
            'class_session_scheduling',
            'class_session_recurrence',
            'in_app_notifications',
            'availability_polls',
            'conversations'
        )
    );

ALTER TABLE tutorhub.tenant_quota_windows
    DROP CONSTRAINT tenant_quota_windows_key_valid;

ALTER TABLE tutorhub.tenant_quota_windows
    ADD CONSTRAINT tenant_quota_windows_key_valid CHECK (
        quota_key IN (
            'invite_creations_per_hour',
            'availability_poll_creations_per_hour',
            'availability_poll_capability_creations_per_hour',
            'study_meeting_creations_per_hour',
            'message_sends_per_hour'
        )
    );

ALTER TABLE tutorhub.tenant_quota_overrides
    DROP CONSTRAINT tenant_quota_overrides_key_valid,
    DROP CONSTRAINT tenant_quota_overrides_limit_valid;

ALTER TABLE tutorhub.tenant_quota_overrides
    ADD CONSTRAINT tenant_quota_overrides_key_valid CHECK (
        quota_key IN (
            'members',
            'active_classes',
            'invite_creations_per_hour',
            'active_availability_polls',
            'availability_poll_range_days',
            'availability_poll_slots',
            'availability_poll_participants',
            'availability_poll_creations_per_hour',
            'availability_poll_capability_creations_per_hour',
            'active_study_meetings',
            'study_meeting_creations_per_hour',
            'messages_per_tenant',
            'message_sends_per_hour'
        )
    ),
    ADD CONSTRAINT tenant_quota_overrides_limit_valid CHECK (
        (quota_key = 'members' AND limit_value BETWEEN 1 AND 10000)
        OR (quota_key = 'active_classes' AND limit_value BETWEEN 1 AND 1000)
        OR (quota_key = 'invite_creations_per_hour' AND limit_value BETWEEN 1 AND 10000)
        OR (quota_key = 'active_availability_polls' AND limit_value BETWEEN 1 AND 200)
        OR (quota_key = 'availability_poll_range_days' AND limit_value BETWEEN 1 AND 90)
        OR (quota_key = 'availability_poll_slots' AND limit_value BETWEEN 1 AND 1000)
        OR (quota_key = 'availability_poll_participants' AND limit_value BETWEEN 1 AND 500)
        OR (quota_key = 'availability_poll_creations_per_hour' AND limit_value BETWEEN 1 AND 200)
        OR (
            quota_key = 'availability_poll_capability_creations_per_hour'
            AND limit_value BETWEEN 1 AND 1000
        )
        OR (quota_key = 'active_study_meetings' AND limit_value BETWEEN 1 AND 200)
        OR (quota_key = 'study_meeting_creations_per_hour' AND limit_value BETWEEN 1 AND 200)
        OR (quota_key = 'messages_per_tenant' AND limit_value BETWEEN 1 AND 10000000)
        OR (quota_key = 'message_sends_per_hour' AND limit_value BETWEEN 1 AND 100000)
    );

COMMIT;
