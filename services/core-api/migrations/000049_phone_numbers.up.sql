BEGIN;

CREATE TABLE tutorhub.user_phone_numbers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES tutorhub.users(id),
    identity_id uuid NOT NULL UNIQUE REFERENCES tutorhub.identities(id),
    e164_lookup_hash bytea NOT NULL CHECK (octet_length(e164_lookup_hash) = 32),
    e164_ciphertext bytea CHECK (e164_ciphertext IS NULL OR octet_length(e164_ciphertext) >= 29),
    country_code text NOT NULL CHECK (country_code ~ '^[A-Z]{2}$'),
    calling_code integer NOT NULL CHECK (calling_code BETWEEN 1 AND 999),
    masked_number text NOT NULL CHECK (length(masked_number) BETWEEN 4 AND 32 AND position('*' IN masked_number) > 0),
    last4 text NOT NULL CHECK (last4 ~ '^[0-9]{2,4}$'),
    status text NOT NULL CHECK (status IN ('active', 'released', 'removed')),
    release_reason text CHECK ((status = 'released') = (release_reason IS NOT NULL)
        AND (release_reason IS NULL OR release_reason = 'reassigned')),
    verified_at timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL,
    released_at timestamptz,
    removed_at timestamptz,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK ((status = 'active') = (e164_ciphertext IS NOT NULL)),
    CHECK ((status = 'released') = (released_at IS NOT NULL)),
    CHECK ((status = 'removed') = (removed_at IS NOT NULL)),
    CHECK (verified_at >= created_at AND last_used_at >= verified_at),
    CHECK (updated_at >= GREATEST(created_at, verified_at, last_used_at)),
    CHECK (released_at BETWEEN created_at AND updated_at),
    CHECK (removed_at BETWEEN created_at AND updated_at)
);
CREATE UNIQUE INDEX user_phone_numbers_active_hash_idx ON tutorhub.user_phone_numbers (e164_lookup_hash) WHERE status = 'active';
CREATE UNIQUE INDEX user_phone_numbers_active_user_idx ON tutorhub.user_phone_numbers (user_id) WHERE status = 'active';
CREATE INDEX user_phone_numbers_release_idx ON tutorhub.user_phone_numbers (e164_lookup_hash, released_at DESC) WHERE status = 'released';
REVOKE ALL ON TABLE tutorhub.user_phone_numbers FROM PUBLIC;

-- Keep the country available to the rolling breaker after a transaction erases its encrypted payload.
ALTER TABLE tutorhub.auth_challenges ADD COLUMN phone_country_code text
    CHECK (phone_country_code IS NULL OR (purpose IN ('phone_login','phone_verify') AND phone_country_code ~ '^[A-Z]{2}$'));
CREATE INDEX auth_challenges_phone_country_idx ON tutorhub.auth_challenges (phone_country_code, created_at)
    WHERE deliverable AND phone_country_code IS NOT NULL;
CREATE INDEX auth_challenges_phone_daily_idx ON tutorhub.auth_challenges (created_at)
    WHERE deliverable AND phone_country_code IS NOT NULL;

COMMIT;
