BEGIN;

DROP TABLE IF EXISTS tutorhub.notification_preferences;
DROP TABLE IF EXISTS tutorhub.notifications;

DELETE FROM tutorhub.tenant_feature_overrides
WHERE feature_key = 'in_app_notifications';

ALTER TABLE tutorhub.tenant_feature_overrides
    DROP CONSTRAINT tenant_feature_overrides_key_valid;

ALTER TABLE tutorhub.tenant_feature_overrides
    ADD CONSTRAINT tenant_feature_overrides_key_valid CHECK (
        feature_key IN (
            'membership_invitations',
            'class_management',
            'class_invite_links',
            'class_session_scheduling'
        )
    );

COMMIT;
