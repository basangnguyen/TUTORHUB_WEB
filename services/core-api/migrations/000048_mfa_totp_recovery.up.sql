BEGIN;

CREATE TABLE tutorhub.mfa_totp_factors (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES tutorhub.users(id),
    secret_ciphertext bytea NOT NULL CHECK (octet_length(secret_ciphertext) >= 29),
    status text NOT NULL CHECK (status IN ('pending', 'active', 'disabled')),
    last_used_step bigint CHECK (last_used_step > 0),
    created_at timestamptz NOT NULL,
    confirmed_at timestamptz,
    disabled_at timestamptz,
    updated_at timestamptz NOT NULL,
    CHECK (updated_at >= created_at),
    CHECK (confirmed_at BETWEEN created_at AND updated_at),
    CHECK (disabled_at BETWEEN created_at AND updated_at),
    CHECK (confirmed_at <= disabled_at),
    CHECK ((status = 'disabled') = (disabled_at IS NOT NULL)),
    CHECK ((confirmed_at IS NULL) = (last_used_step IS NULL)),
    CHECK (status <> 'pending' OR confirmed_at IS NULL),
    CHECK (status <> 'active' OR confirmed_at IS NOT NULL)
);
CREATE UNIQUE INDEX mfa_totp_live_factor ON tutorhub.mfa_totp_factors(user_id, status)
    WHERE status IN ('active','pending');

CREATE TABLE tutorhub.mfa_recovery_codes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES tutorhub.users(id),
    code_hash bytea NOT NULL CHECK (octet_length(code_hash) = 32),
    created_at timestamptz NOT NULL,
    consumed_at timestamptz CHECK (consumed_at >= created_at),
    invalidated_at timestamptz CHECK (invalidated_at >= created_at),
    CHECK (consumed_at IS NULL OR invalidated_at IS NULL),
    UNIQUE (user_id, code_hash)
);
CREATE INDEX mfa_recovery_available ON tutorhub.mfa_recovery_codes(user_id)
    WHERE consumed_at IS NULL AND invalidated_at IS NULL;
REVOKE ALL ON tutorhub.mfa_totp_factors, tutorhub.mfa_recovery_codes FROM PUBLIC;
COMMIT;
