BEGIN;

-- A completed restore command may outlive the retained source artifact. The
-- source foreign key intentionally uses ON DELETE SET NULL, so terminal
-- commands must accept that redaction while active commands still require an
-- exact source snapshot.
ALTER TABLE tutorhub.whiteboard_artifact_commands
    DROP CONSTRAINT whiteboard_artifact_commands_restore_shape;

ALTER TABLE tutorhub.whiteboard_artifact_commands
    ADD CONSTRAINT whiteboard_artifact_commands_restore_shape CHECK (
        (
            command_kind = 'restore'
            AND target_generation IS NOT NULL
            AND target_provider_document_name IS NOT NULL
            AND (
                source_snapshot_id IS NOT NULL
                OR status IN ('succeeded', 'failed', 'quarantined')
            )
        ) OR (
            command_kind <> 'restore'
            AND source_snapshot_id IS NULL
            AND target_generation IS NULL
            AND target_provider_document_name IS NULL
        )
    );

COMMIT;
