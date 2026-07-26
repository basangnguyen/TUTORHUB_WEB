BEGIN;

DROP INDEX IF EXISTS tutorhub.class_sessions_series_occurrence_idx;

ALTER TABLE tutorhub.class_sessions
    DROP COLUMN IF EXISTS original_overlap_offset_seconds,
    DROP COLUMN IF EXISTS original_timezone,
    DROP COLUMN IF EXISTS original_local_start,
    DROP COLUMN IF EXISTS occurrence_key,
    DROP COLUMN IF EXISTS series_id;

DROP TABLE tutorhub.class_session_exceptions;
DROP TABLE tutorhub.class_session_series;

DELETE FROM tutorhub.tenant_feature_overrides
WHERE feature_key = 'class_session_recurrence';

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
