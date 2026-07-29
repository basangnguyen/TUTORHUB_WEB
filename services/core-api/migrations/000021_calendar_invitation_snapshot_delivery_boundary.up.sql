BEGIN;

-- 000020 stores an encrypted immutable business snapshot for each invitation
-- revision. That snapshot remains the input to later deterministic rendering.
-- It must not, however, claim one delivery METHOD: a single audience mutation
-- can add recipients (future REQUEST effects) and remove recipients (future
-- CANCEL effects) in the same revision. P3-05A owns the recipient-specific
-- delivery effect, rendered MIME and selected REQUEST/CANCEL method.
ALTER TABLE tutorhub.calendar_invitation_revisions
    DROP CONSTRAINT calendar_invitation_revisions_method_valid;

ALTER TABLE tutorhub.calendar_invitation_revisions
    ALTER COLUMN method DROP NOT NULL;

-- NOT VALID retains any experimental pre-boundary REQUEST/CANCEL value without
-- rewriting immutable history. PostgreSQL enforces the check for every row
-- created after this migration, so new business snapshots stay method-neutral.
ALTER TABLE tutorhub.calendar_invitation_revisions
    ADD CONSTRAINT calendar_invitation_revisions_delivery_method_deferred
        CHECK (method IS NULL) NOT VALID;

-- AES-GCM's v1 envelope is 29 bytes before plaintext
-- (format byte + 12-byte nonce + 16-byte authentication tag). TutorHub permits
-- one-character display names, so the original 32-byte lower bound rejected an
-- otherwise valid protected value. Delivery addresses remain at least 32 bytes
-- because their normalized plaintext is at least three bytes.
ALTER TABLE tutorhub.calendar_external_recipients
    DROP CONSTRAINT calendar_external_recipients_ciphertext_valid;

ALTER TABLE tutorhub.calendar_external_recipients
    ADD CONSTRAINT calendar_external_recipients_ciphertext_valid CHECK (
        octet_length(delivery_address_ciphertext) BETWEEN 32 AND 4096
        AND (
            display_name_ciphertext IS NULL
            OR octet_length(display_name_ciphertext) BETWEEN 29 AND 2048
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
            OR octet_length(display_name_ciphertext) BETWEEN 29 AND 2048
        )
        AND crypto_key_version > 0
    );

COMMENT ON TABLE tutorhub.calendar_invitation_revisions IS
    'Append-only encrypted business/audience snapshot. It contains no per-recipient delivery effect, provider attempt, delivery state, rendered MIME bytes, or delivery method; P3-05A owns delivery records.';

COMMENT ON TABLE tutorhub.calendar_invitation_recipients IS
    'Append-only recipient snapshot for one invitation revision. It is not a delivery ledger and its RSVP fields are historical snapshot values, not the live RSVP authority.';

COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.canonical_payload_ciphertext IS
    'Encrypted immutable canonical business snapshot and renderer input. It is not a rendered MIME or per-recipient delivery payload.';

COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.canonical_payload_sha256 IS
    'SHA-256 integrity value for the immutable canonical business snapshot, not a rendered MIME hash.';

COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.crypto_key_version IS
    'Encryption key version for the immutable canonical business snapshot.';

COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.method IS
    'Deprecated revision-level delivery field. Must be NULL for post-000021 snapshots; P3-05A derives REQUEST or CANCEL per recipient from immutable audience diff and source lifecycle.';

COMMENT ON COLUMN tutorhub.calendar_invitation_revisions.lifecycle IS
    'Immutable source lifecycle: published, updated, or cancelled. It remains business truth and is not a per-recipient delivery method.';

COMMENT ON COLUMN tutorhub.calendar_invitation_recipients.rsvp_state IS
    'Historical RSVP state at snapshot creation only. The active attendee row is the PostgreSQL RSVP source of truth.';

COMMENT ON COLUMN tutorhub.calendar_invitation_recipients.rsvp_source IS
    'Historical RSVP source at snapshot creation only. The active attendee row is the PostgreSQL RSVP source of truth.';

COMMIT;
