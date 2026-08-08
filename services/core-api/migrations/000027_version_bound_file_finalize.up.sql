BEGIN;

ALTER TABLE tutorhub.content_files
    DROP CONSTRAINT content_files_storage_proof_consistent;

-- P3-08 could only copy the expected digest into stored proof because B2 does
-- not return or enforce it for presigned PUT. Remove that unverified claim from
-- every non-ready processing state; the P3-10 worker will establish SHA-256
-- from the exact persisted version before making a file ready.
UPDATE tutorhub.content_files
SET stored_checksum_sha256 = NULL
WHERE status IN ('uploaded', 'processing');

ALTER TABLE tutorhub.content_files
    ADD CONSTRAINT content_files_storage_proof_consistent CHECK (
        (
            status = 'pending'
            AND stored_size_bytes IS NULL
            AND stored_media_type IS NULL
            AND stored_checksum_sha256 IS NULL
            AND storage_etag IS NULL
            AND storage_version_id IS NULL
            AND uploaded_at IS NULL
        )
        OR (
            status IN ('uploaded', 'processing')
            AND stored_size_bytes = expected_size_bytes
            AND stored_media_type = declared_media_type
            AND stored_checksum_sha256 IS NULL
            AND length(btrim(storage_etag)) BETWEEN 1 AND 512
            AND length(btrim(storage_version_id)) BETWEEN 1 AND 512
            AND uploaded_at IS NOT NULL
            AND uploaded_at >= created_at
            AND uploaded_at <= updated_at
        )
        OR (
            status = 'ready'
            AND stored_size_bytes = expected_size_bytes
            AND stored_media_type = declared_media_type
            AND stored_checksum_sha256 = expected_checksum_sha256
            AND octet_length(stored_checksum_sha256) = 32
            AND length(btrim(storage_etag)) BETWEEN 1 AND 512
            AND length(btrim(storage_version_id)) BETWEEN 1 AND 512
            AND uploaded_at IS NOT NULL
            AND uploaded_at >= created_at
            AND uploaded_at <= updated_at
        )
        OR (
            status = 'rejected'
            AND stored_size_bytes = expected_size_bytes
            AND stored_media_type = declared_media_type
            AND (
                stored_checksum_sha256 IS NULL
                OR octet_length(stored_checksum_sha256) = 32
            )
            AND length(btrim(storage_etag)) BETWEEN 1 AND 512
            AND length(btrim(storage_version_id)) BETWEEN 1 AND 512
            AND uploaded_at IS NOT NULL
            AND uploaded_at >= created_at
            AND uploaded_at <= updated_at
        )
    );

COMMIT;
