BEGIN;

ALTER TABLE tutorhub.identities
    ADD COLUMN email_verified boolean NOT NULL DEFAULT false,
    ADD COLUMN last_authenticated_at timestamptz;

ALTER TABLE tutorhub.sessions
    ADD COLUMN identity_id uuid REFERENCES tutorhub.identities (id) ON DELETE SET NULL,
    ADD COLUMN absolute_expires_at timestamptz,
    ADD COLUMN auth_time timestamptz,
    ADD COLUMN revoked_reason text;

UPDATE tutorhub.sessions
SET
    absolute_expires_at = expires_at,
    auth_time = created_at
WHERE absolute_expires_at IS NULL OR auth_time IS NULL;

ALTER TABLE tutorhub.sessions
    ALTER COLUMN absolute_expires_at SET NOT NULL,
    ALTER COLUMN auth_time SET NOT NULL,
    ADD CONSTRAINT sessions_idle_before_absolute CHECK (expires_at <= absolute_expires_at),
    ADD CONSTRAINT sessions_absolute_after_creation CHECK (absolute_expires_at > created_at),
    ADD CONSTRAINT sessions_revocation_consistent CHECK (
        (revoked_at IS NULL AND revoked_reason IS NULL)
        OR revoked_at IS NOT NULL
    );

CREATE INDEX sessions_identity_idx
    ON tutorhub.sessions (identity_id, created_at DESC)
    WHERE identity_id IS NOT NULL;

CREATE TABLE tutorhub.auth_flows (
    state_hash bytea PRIMARY KEY,
    browser_binding_hash bytea NOT NULL,
    nonce_hash bytea NOT NULL,
    code_verifier_ciphertext bytea NOT NULL,
    return_to text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CONSTRAINT auth_flows_state_hash_length CHECK (octet_length(state_hash) = 32),
    CONSTRAINT auth_flows_browser_binding_hash_length CHECK (octet_length(browser_binding_hash) = 32),
    CONSTRAINT auth_flows_nonce_hash_length CHECK (octet_length(nonce_hash) = 32),
    CONSTRAINT auth_flows_ciphertext_not_empty CHECK (octet_length(code_verifier_ciphertext) >= 29),
    CONSTRAINT auth_flows_return_to_internal CHECK (
        return_to LIKE '/%'
        AND return_to NOT LIKE '//%'
        AND position(E'\\' IN return_to) = 0
    ),
    CONSTRAINT auth_flows_expiry_after_creation CHECK (expires_at > created_at),
    CONSTRAINT auth_flows_consumed_after_creation CHECK (
        consumed_at IS NULL OR consumed_at >= created_at
    )
);

CREATE INDEX auth_flows_expiry_idx ON tutorhub.auth_flows (expires_at);

COMMIT;
