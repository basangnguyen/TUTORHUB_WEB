BEGIN;

-- Development rollback only. Terminal restore commands whose retained source
-- was already purged cannot satisfy the earlier shape and are removed before
-- restoring that constraint.
DELETE FROM tutorhub.whiteboard_artifact_commands
WHERE command_kind = 'restore'
  AND source_snapshot_id IS NULL;

ALTER TABLE tutorhub.whiteboard_artifact_commands
    DROP CONSTRAINT whiteboard_artifact_commands_restore_shape;

ALTER TABLE tutorhub.whiteboard_artifact_commands
    ADD CONSTRAINT whiteboard_artifact_commands_restore_shape CHECK (
        (
            command_kind = 'restore'
            AND source_snapshot_id IS NOT NULL
            AND target_generation IS NOT NULL
            AND target_provider_document_name IS NOT NULL
        ) OR (
            command_kind <> 'restore'
            AND source_snapshot_id IS NULL
            AND target_generation IS NULL
            AND target_provider_document_name IS NULL
        )
    );

COMMIT;
