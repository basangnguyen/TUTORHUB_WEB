BEGIN;

-- P4-08 extends the canonical conversation aggregate instead of creating a
-- media-specific message store. One MediaSpace owns at most one durable room
-- conversation; every message and receipt keeps using migration 000025.
ALTER TABLE tutorhub.conversations
    ADD COLUMN media_space_id uuid;

ALTER TABLE tutorhub.conversations
    ADD CONSTRAINT conversations_media_space_fk
        FOREIGN KEY (tenant_id, media_space_id)
        REFERENCES tutorhub.media_spaces (tenant_id, id)
        ON DELETE CASCADE;

ALTER TABLE tutorhub.conversations
    DROP CONSTRAINT conversations_shape_valid;

ALTER TABLE tutorhub.conversations
    ADD CONSTRAINT conversations_shape_valid CHECK (
        (
            kind = 'direct'
            AND class_id IS NULL
            AND media_space_id IS NULL
            AND direct_user_low_id IS NOT NULL
            AND direct_user_high_id IS NOT NULL
            AND direct_user_low_id < direct_user_high_id
            AND created_by_user_id IN (direct_user_low_id, direct_user_high_id)
        )
        OR (
            kind = 'class'
            AND class_id IS NOT NULL
            AND media_space_id IS NULL
            AND direct_user_low_id IS NULL
            AND direct_user_high_id IS NULL
        )
        OR (
            kind = 'room'
            AND class_id IS NULL
            AND media_space_id IS NOT NULL
            AND direct_user_low_id IS NULL
            AND direct_user_high_id IS NULL
        )
    );

CREATE UNIQUE INDEX conversations_media_space_unique
    ON tutorhub.conversations (tenant_id, media_space_id)
    WHERE kind = 'room';

-- The table was already deny-by-default; repeat the boundary so a standalone
-- forward migration review cannot mistake the new column for a PUBLIC grant.
REVOKE ALL ON tutorhub.conversations FROM PUBLIC;

COMMIT;
