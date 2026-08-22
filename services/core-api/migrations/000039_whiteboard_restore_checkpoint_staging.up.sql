BEGIN;

-- Restore validation must finish before the control-plane generation swap.
-- The target generation therefore has one bounded checkpoint staged briefly
-- before its generation catalog row exists. The Core API still cannot read
-- the checkpoint bytes and only promotes a worker-completed exact command.
ALTER TABLE tutorhub.whiteboard_document_checkpoints
    DROP CONSTRAINT whiteboard_document_checkpoints_generation_fk;

COMMIT;
