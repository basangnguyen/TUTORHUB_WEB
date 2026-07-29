BEGIN;

ALTER TABLE tutorhub.calendar_invitation_revisions
    DROP CONSTRAINT calendar_invitation_revisions_delivery_method_deferred;

-- A downgrade is intentionally blocked after method-neutral snapshots exist.
-- Inventing one REQUEST/CANCEL value for a mixed audience diff would corrupt
-- immutable business history.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM tutorhub.calendar_invitation_revisions
        WHERE method IS NULL
    ) THEN
        RAISE EXCEPTION
            'cannot restore the pre-000021 revision delivery-method contract while neutral invitation snapshots exist'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

-- Restoring the historical 32-byte lower bound would invalidate protected
-- one- or two-byte display names. Refuse that destructive downgrade instead
-- of deleting or rewriting encrypted identity data.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM tutorhub.calendar_external_recipients
        WHERE display_name_ciphertext IS NOT NULL
          AND octet_length(display_name_ciphertext) < 32
    ) OR EXISTS (
        SELECT 1
        FROM tutorhub.calendar_invitation_recipients
        WHERE display_name_ciphertext IS NOT NULL
          AND octet_length(display_name_ciphertext) < 32
    ) THEN
        RAISE EXCEPTION
            'cannot restore the pre-000021 protected display-name length contract while short encrypted names exist'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE tutorhub.calendar_external_recipients
    DROP CONSTRAINT calendar_external_recipients_ciphertext_valid;

ALTER TABLE tutorhub.calendar_external_recipients
    ADD CONSTRAINT calendar_external_recipients_ciphertext_valid CHECK (
        octet_length(delivery_address_ciphertext) BETWEEN 32 AND 4096
        AND (
            display_name_ciphertext IS NULL
            OR octet_length(display_name_ciphertext) BETWEEN 32 AND 2048
        )
    );

ALTER TABLE tutorhub.calendar_invitation_recipients
    DROP CONSTRAINT calendar_invitation_recipients_identity_protected;

ALTER TABLE tutorhub.calendar_invitation_recipients
    ADD CONSTRAINT calendar_invitation_recipients_identity_protected CHECK (
        octet_length(delivery_address_fingerprint) = 32
        AND octet_length(delivery_address_ciphertext) BETWEEN 32 AND 4096
        AND (
            display_name_ciphertext IS NULL
            OR octet_length(display_name_ciphertext) BETWEEN 32 AND 2048
        )
        AND crypto_key_version > 0
    );

ALTER TABLE tutorhub.calendar_invitation_revisions
    ALTER COLUMN method SET NOT NULL;

ALTER TABLE tutorhub.calendar_invitation_revisions
    ADD CONSTRAINT calendar_invitation_revisions_method_valid
        CHECK (method IN ('REQUEST', 'CANCEL'));

COMMENT ON TABLE tutorhub.calendar_invitation_revisions IS NULL;
COMMENT ON TABLE tutorhub.calendar_invitation_recipients IS NULL;
COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.canonical_payload_ciphertext IS NULL;
COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.canonical_payload_sha256 IS NULL;
COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.crypto_key_version IS NULL;
COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.method IS NULL;
COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.lifecycle IS NULL;
COMMENT ON COLUMN tutorhub.calendar_invitation_recipients.rsvp_state IS NULL;
COMMENT ON COLUMN tutorhub.calendar_invitation_recipients.rsvp_source IS NULL;

COMMIT;
