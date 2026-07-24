BEGIN;

DROP TABLE tutorhub.class_sessions;

DELETE FROM tutorhub.tenant_feature_overrides
WHERE feature_key = 'class_session_scheduling';

ALTER TABLE tutorhub.tenant_feature_overrides
    DROP CONSTRAINT tenant_feature_overrides_key_valid;

ALTER TABLE tutorhub.tenant_feature_overrides
    ADD CONSTRAINT tenant_feature_overrides_key_valid CHECK (
        feature_key IN (
            'membership_invitations',
            'class_management',
            'class_invite_links'
        )
    );

COMMIT;
