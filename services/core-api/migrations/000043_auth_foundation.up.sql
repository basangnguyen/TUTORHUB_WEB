BEGIN;

ALTER TABLE tutorhub.identities
    ADD COLUMN kind text NOT NULL DEFAULT 'oidc',
    ADD COLUMN provider_slug text,
    ADD COLUMN provider_org_id text,
    ADD COLUMN provider_stable_id text,
    ADD CONSTRAINT identities_kind_valid CHECK (kind IN ('oidc', 'oauth2', 'email', 'phone', 'passkey'));

UPDATE tutorhub.identities SET provider_slug = 'oidc' WHERE kind = 'oidc';

ALTER TABLE tutorhub.sessions
    ADD COLUMN auth_methods text[] NOT NULL DEFAULT ARRAY['fed']::text[],
    ADD COLUMN assurance_level smallint NOT NULL DEFAULT 1,
    ADD COLUMN reauthenticated_at timestamptz,
    ADD COLUMN device_label text,
    ADD COLUMN persistent boolean NOT NULL DEFAULT false,
    ADD CONSTRAINT sessions_assurance_level_valid CHECK (assurance_level IN (1, 2)),
    ADD CONSTRAINT sessions_device_label_length CHECK (device_label IS NULL OR length(device_label) BETWEEN 1 AND 80);

CREATE TABLE tutorhub.auth_transactions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    binding_hash bytea NOT NULL UNIQUE,
    kind text NOT NULL,
    state text NOT NULL DEFAULT 'pending',
    user_id uuid REFERENCES tutorhub.users(id) ON DELETE CASCADE,
    session_id uuid REFERENCES tutorhub.sessions(id) ON DELETE CASCADE,
    payload_ciphertext bytea,
    ip_bucket_hash bytea,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    completed_at timestamptz,
    CONSTRAINT auth_transactions_binding_hash_length CHECK (octet_length(binding_hash) = 32),
    CONSTRAINT auth_transactions_ip_hash_length CHECK (ip_bucket_hash IS NULL OR octet_length(ip_bucket_hash) = 32),
    CONSTRAINT auth_transactions_kind_valid CHECK (kind IN (
        'signup_email', 'login', 'recovery', 'phone', 'mfa', 'link_intent', 'reauth', 'passkey'
    )),
    CONSTRAINT auth_transactions_state_valid CHECK (state IN ('pending', 'completed', 'cancelled')),
    CONSTRAINT auth_transactions_completion_valid CHECK (
        (state = 'completed' AND completed_at IS NOT NULL AND completed_at >= created_at)
        OR (state <> 'completed' AND completed_at IS NULL)
    ),
    CONSTRAINT auth_transactions_ttl CHECK (
        expires_at > created_at AND expires_at <= created_at + interval '15 minutes'
    )
);
CREATE INDEX auth_transactions_expiry_idx ON tutorhub.auth_transactions (expires_at, id);
CREATE INDEX auth_transactions_user_idx ON tutorhub.auth_transactions (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX auth_transactions_session_idx ON tutorhub.auth_transactions (session_id) WHERE session_id IS NOT NULL;

CREATE TABLE tutorhub.auth_challenges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id uuid NOT NULL REFERENCES tutorhub.auth_transactions(id) ON DELETE CASCADE,
    purpose text NOT NULL,
    channel text NOT NULL,
    destination_hash bytea NOT NULL,
    code_hash bytea NOT NULL,
    deliverable boolean NOT NULL,
    attempts smallint NOT NULL DEFAULT 0,
    max_attempts smallint NOT NULL DEFAULT 5,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    invalidated_at timestamptz,
    CONSTRAINT auth_challenges_hash_lengths CHECK (
        octet_length(destination_hash) = 32 AND octet_length(code_hash) = 32
    ),
    CONSTRAINT auth_challenges_purpose_valid CHECK (purpose IN (
        'signup_email', 'login_code', 'password_recovery', 'phone_login', 'phone_verify', 'mfa', 'link_identity', 'reauth'
    )),
    CONSTRAINT auth_challenges_channel_valid CHECK (channel IN ('email', 'zalo_zns', 'sms', 'whatsapp', 'voice')),
    CONSTRAINT auth_challenges_attempts_bounded CHECK (max_attempts BETWEEN 1 AND 5 AND attempts BETWEEN 0 AND max_attempts),
    CONSTRAINT auth_challenges_ttl CHECK (
        expires_at > created_at AND expires_at <= created_at + interval '10 minutes'
        AND (channel = 'email' OR expires_at <= created_at + interval '5 minutes')
    ),
    CONSTRAINT auth_challenges_consumed_valid CHECK (consumed_at IS NULL OR consumed_at >= created_at),
    CONSTRAINT auth_challenges_invalidated_valid CHECK (invalidated_at IS NULL OR invalidated_at >= created_at)
);
CREATE INDEX auth_challenges_transaction_idx ON tutorhub.auth_challenges (transaction_id, created_at DESC, id);
CREATE INDEX auth_challenges_expiry_idx ON tutorhub.auth_challenges (expires_at, id);
CREATE INDEX auth_challenges_destination_idx ON tutorhub.auth_challenges (destination_hash, created_at DESC);

CREATE TABLE tutorhub.auth_challenge_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    challenge_id uuid NOT NULL REFERENCES tutorhub.auth_challenges(id) ON DELETE CASCADE,
    channel text NOT NULL,
    provider text NOT NULL,
    status text NOT NULL,
    provider_message_ref text,
    error_code text,
    created_at timestamptz NOT NULL,
    delivered_at timestamptz,
    CONSTRAINT auth_deliveries_channel_valid CHECK (channel IN ('email', 'zalo_zns', 'sms', 'whatsapp', 'voice')),
    CONSTRAINT auth_deliveries_provider_valid CHECK (provider IN ('disabled', 'devinbox', 'smtp', 'ses', 'zns', 'sms_brandname', 'twilio')),
    CONSTRAINT auth_deliveries_status_valid CHECK (status IN ('queued', 'accepted', 'failed', 'disabled', 'skipped')),
    CONSTRAINT auth_deliveries_ref_bounded CHECK (provider_message_ref IS NULL OR length(provider_message_ref) BETWEEN 1 AND 128),
    CONSTRAINT auth_deliveries_error_bounded CHECK (error_code IS NULL OR error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    CONSTRAINT auth_deliveries_timestamp_valid CHECK (delivered_at IS NULL OR delivered_at >= created_at)
);
CREATE INDEX auth_deliveries_challenge_idx ON tutorhub.auth_challenge_deliveries (challenge_id, created_at);
CREATE INDEX auth_deliveries_retention_idx ON tutorhub.auth_challenge_deliveries (created_at, id);

REVOKE ALL ON TABLE tutorhub.auth_transactions, tutorhub.auth_challenges, tutorhub.auth_challenge_deliveries FROM PUBLIC;

-- Environment-specific runtime roles are provisioned separately with exact,
-- column-scoped UPDATE grants; the API role never receives DELETE.
COMMIT;
