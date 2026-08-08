BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM tutorhub.content_files
        WHERE status IN ('uploaded', 'processing', 'ready', 'rejected')
          AND stored_checksum_sha256 IS NULL
    ) THEN
        RAISE EXCEPTION
            'cannot downgrade version-bound finalize while files without stored SHA-256 exist';
    END IF;
END
$$;

ALTER TABLE tutorhub.content_files
    DROP CONSTRAINT content_files_storage_proof_consistent;

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
            status IN ('uploaded', 'processing', 'ready', 'rejected')
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
    );

COMMIT;
