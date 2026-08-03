BEGIN;

DROP TABLE tutorhub.conversation_members;
DROP TABLE tutorhub.conversations;

DELETE FROM tutorhub.tenant_feature_overrides
WHERE feature_key = 'conversations';

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
            'availability_polls'
        )
    );

COMMIT;
