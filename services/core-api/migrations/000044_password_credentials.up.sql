BEGIN;

ALTER TABLE tutorhub.users ADD COLUMN email_verified_at timestamptz;
UPDATE tutorhub.users AS u
SET email_verified_at = (
    SELECT MIN(i.created_at) FROM tutorhub.identities AS i
    WHERE i.user_id = u.id AND i.status = 'active' AND i.email_verified
        AND lower(i.email_at_provider) = lower(u.email)
);

CREATE TABLE tutorhub.user_password_credentials (
    user_id uuid PRIMARY KEY REFERENCES tutorhub.users(id) ON DELETE CASCADE,
    password_hash text,
    hash_version smallint NOT NULL DEFAULT 1,
    status text NOT NULL DEFAULT 'active',
    failed_attempts integer NOT NULL DEFAULT 0,
    last_failed_at timestamptz,
    locked_until timestamptz,
    compromised_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT user_password_credentials_status_valid CHECK (status IN ('active', 'removed')),
    CONSTRAINT user_password_credentials_hash_present CHECK (
        (status = 'active' AND password_hash IS NOT NULL AND password_hash LIKE '$argon2id$v=19$%')
        OR (status = 'removed' AND password_hash IS NULL)
    ),
    CONSTRAINT user_password_credentials_attempts_valid CHECK (failed_attempts >= 0),
    CONSTRAINT user_password_credentials_version_valid CHECK (hash_version >= 1),
    CONSTRAINT user_password_credentials_timestamps_valid CHECK (updated_at >= created_at)
);

CREATE TABLE tutorhub.user_legal_acceptances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES tutorhub.users(id) ON DELETE CASCADE,
    document_kind text NOT NULL,
    version text NOT NULL,
    accepted_at timestamptz NOT NULL,
    ip_bucket_hash bytea,
    CONSTRAINT user_legal_acceptances_kind_valid CHECK (document_kind IN ('user_agreement', 'privacy_policy')),
    CONSTRAINT user_legal_acceptances_version_valid CHECK (length(btrim(version)) BETWEEN 1 AND 64),
    CONSTRAINT user_legal_acceptances_ip_hash_length CHECK (ip_bucket_hash IS NULL OR octet_length(ip_bucket_hash) = 32),
    UNIQUE (user_id, document_kind, version)
);

REVOKE ALL ON TABLE tutorhub.user_password_credentials, tutorhub.user_legal_acceptances FROM PUBLIC;

COMMIT;
