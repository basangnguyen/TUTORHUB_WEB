BEGIN;

-- Shared staging and production use this migration forward-only. This inverse
-- exists solely for disposable/local development before any retained board is
-- created.
ALTER TABLE tutorhub.whiteboard_document_generations
    DROP CONSTRAINT whiteboard_generations_restore_snapshot_fk;

ALTER TABLE tutorhub.whiteboard_documents
    DROP CONSTRAINT whiteboard_documents_current_generation_fk;

DROP TABLE tutorhub.whiteboard_document_mutation_receipts;
DROP TABLE tutorhub.whiteboard_snapshots;
DROP TABLE tutorhub.whiteboard_capability_policies;
DROP TABLE tutorhub.whiteboard_document_generations;
DROP TABLE tutorhub.whiteboard_documents;

COMMIT;
