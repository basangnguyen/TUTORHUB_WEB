BEGIN;

-- A development rollback removes P4-08 room history before restoring the
-- two-kind shape. Production/staging acceptance remains forward-only.
DELETE FROM tutorhub.conversations
WHERE kind = 'room';

DROP INDEX tutorhub.conversations_media_space_unique;

ALTER TABLE tutorhub.conversations
    DROP CONSTRAINT conversations_shape_valid,
    DROP CONSTRAINT conversations_media_space_fk,
    DROP COLUMN media_space_id;

ALTER TABLE tutorhub.conversations
    ADD CONSTRAINT conversations_shape_valid CHECK (
        (
            kind = 'direct'
            AND class_id IS NULL
            AND direct_user_low_id IS NOT NULL
            AND direct_user_high_id IS NOT NULL
            AND direct_user_low_id < direct_user_high_id
            AND created_by_user_id IN (direct_user_low_id, direct_user_high_id)
        )
        OR (
            kind = 'class'
            AND class_id IS NOT NULL
            AND direct_user_low_id IS NULL
            AND direct_user_high_id IS NULL
        )
    );

COMMIT;
