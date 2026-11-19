BEGIN;

ALTER TABLE tutorhub.users
    ADD COLUMN webauthn_user_handle bytea UNIQUE,
    ADD CONSTRAINT users_webauthn_handle_length CHECK (octet_length(webauthn_user_handle) = 64);

CREATE TABLE tutorhub.webauthn_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES tutorhub.users(id),
    credential_id bytea NOT NULL UNIQUE CHECK (octet_length(credential_id) BETWEEN 1 AND 1024),
    public_key bytea NOT NULL CHECK (octet_length(public_key) > 0),
    attestation_type text NOT NULL,
    aaguid bytea CHECK (octet_length(aaguid) = 16),
    sign_count bigint NOT NULL DEFAULT 0 CHECK (sign_count >= 0),
    clone_warning boolean NOT NULL DEFAULT false,
    transports text[] NOT NULL DEFAULT '{}',
    backup_eligible boolean NOT NULL DEFAULT false,
    backup_state boolean NOT NULL DEFAULT false CHECK (NOT backup_state OR backup_eligible),
    label text NOT NULL CHECK (char_length(label) BETWEEN 1 AND 80),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','revoked')),
    created_at timestamptz NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at),
    CHECK (last_used_at BETWEEN created_at AND updated_at),
    CHECK (revoked_at BETWEEN created_at AND updated_at),
    CHECK (last_used_at <= revoked_at),
    CHECK ((status = 'revoked') = (revoked_at IS NOT NULL))
);
CREATE INDEX webauthn_credentials_user_status_idx ON tutorhub.webauthn_credentials (user_id, status);
REVOKE ALL ON TABLE tutorhub.webauthn_credentials FROM PUBLIC;

COMMIT;
