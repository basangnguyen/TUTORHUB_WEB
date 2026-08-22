BEGIN;

-- Development rollback only: discard checkpoints that were staged but never
-- promoted before restoring the generation foreign key.
DELETE FROM tutorhub.whiteboard_document_checkpoints AS checkpoint
WHERE NOT EXISTS (
    SELECT 1
    FROM tutorhub.whiteboard_document_generations AS generation
    WHERE generation.tenant_id = checkpoint.tenant_id
      AND generation.document_id = checkpoint.document_id
      AND generation.generation = checkpoint.generation
);

ALTER TABLE tutorhub.whiteboard_document_checkpoints
    ADD CONSTRAINT whiteboard_document_checkpoints_generation_fk
        FOREIGN KEY (tenant_id, document_id, generation)
        REFERENCES tutorhub.whiteboard_document_generations (
            tenant_id, document_id, generation
        )
        ON DELETE CASCADE;

COMMIT;
