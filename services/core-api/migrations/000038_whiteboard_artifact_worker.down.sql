BEGIN;

-- Disposable/local inverse only. Shared environments use migration 000038
-- forward-only after any artifact command or B2 object exists.
DROP FUNCTION tutorhub.fail_whiteboard_snapshot_purge(uuid, uuid, text);
DROP FUNCTION tutorhub.complete_whiteboard_snapshot_purge(uuid, uuid);
DROP FUNCTION tutorhub.claim_whiteboard_snapshot_purge(text, integer, integer);
DROP FUNCTION tutorhub.enqueue_whiteboard_snapshot_purge(integer);
DROP TABLE tutorhub.whiteboard_artifact_purge_queue;

ALTER TABLE tutorhub.whiteboard_snapshots
    DROP CONSTRAINT whiteboard_snapshots_size_valid;
ALTER TABLE tutorhub.whiteboard_snapshots
    ADD CONSTRAINT whiteboard_snapshots_size_valid CHECK (
        size_bytes BETWEEN 1 AND 67108864
    );

ALTER TABLE tutorhub.whiteboard_snapshots
    DROP CONSTRAINT whiteboard_snapshots_kind_valid;
ALTER TABLE tutorhub.whiteboard_snapshots
    ADD CONSTRAINT whiteboard_snapshots_kind_valid CHECK (
        snapshot_kind IN ('checkpoint', 'manual', 'pre_restore', 'import')
    );

ALTER TABLE tutorhub.whiteboard_snapshots
    DROP CONSTRAINT whiteboard_snapshots_object_key_valid;
ALTER TABLE tutorhub.whiteboard_snapshots
    ADD CONSTRAINT whiteboard_snapshots_object_key_valid CHECK (
        object_key ~ '^wb/[a-f0-9]{2}/[a-f0-9]{64}$'
    );

DROP TABLE tutorhub.whiteboard_artifact_commands;
DROP TABLE tutorhub.whiteboard_document_checkpoints;

COMMIT;
