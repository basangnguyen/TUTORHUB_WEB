BEGIN;

ALTER TABLE tutorhub.outbox_events
    ADD COLUMN lease_owner uuid,
    ADD COLUMN lease_token bigint NOT NULL DEFAULT 0,
    ADD COLUMN leased_at timestamptz,
    ADD COLUMN leased_until timestamptz,
    ADD COLUMN dead_lettered_at timestamptz,
    ADD CONSTRAINT outbox_lease_token_non_negative CHECK (lease_token >= 0),
    ADD CONSTRAINT outbox_lease_state_valid CHECK (
        (
            lease_owner IS NULL
            AND leased_at IS NULL
            AND leased_until IS NULL
        )
        OR (
            lease_owner IS NOT NULL
            AND lease_token > 0
            AND leased_at IS NOT NULL
            AND leased_until IS NOT NULL
            AND leased_until > leased_at
        )
    ),
    ADD CONSTRAINT outbox_terminal_state_exclusive CHECK (
        published_at IS NULL OR dead_lettered_at IS NULL
    ),
    ADD CONSTRAINT outbox_terminal_has_no_lease CHECK (
        (
            published_at IS NULL
            AND dead_lettered_at IS NULL
        )
        OR (
            lease_owner IS NULL
            AND leased_at IS NULL
            AND leased_until IS NULL
        )
    ),
    ADD CONSTRAINT outbox_dead_letter_state_valid CHECK (
        dead_lettered_at IS NULL
        OR (
            attempts > 0
            AND last_error IS NOT NULL
        )
    );

UPDATE tutorhub.outbox_events
SET last_error = 'legacy_error_redacted'
WHERE last_error IS NOT NULL
  AND NOT (
      last_error = btrim(last_error)
      AND length(last_error) BETWEEN 1 AND 100
      AND last_error ~ '^[a-z][a-z0-9._-]{0,99}$'
  );

ALTER TABLE tutorhub.outbox_events
    ADD CONSTRAINT outbox_last_error_code_valid CHECK (
        last_error IS NULL
        OR (
            last_error = btrim(last_error)
            AND length(last_error) BETWEEN 1 AND 100
            AND last_error ~ '^[a-z][a-z0-9._-]{0,99}$'
        )
    );

CREATE INDEX outbox_ready_claim_idx
    ON tutorhub.outbox_events (event_type, available_at, occurred_at, id)
    WHERE published_at IS NULL
      AND dead_lettered_at IS NULL
      AND lease_owner IS NULL;

CREATE INDEX outbox_expired_lease_claim_idx
    ON tutorhub.outbox_events (event_type, leased_until, occurred_at, id)
    WHERE published_at IS NULL
      AND dead_lettered_at IS NULL
      AND lease_owner IS NOT NULL;

CREATE INDEX outbox_pending_age_idx
    ON tutorhub.outbox_events (occurred_at, id)
    WHERE published_at IS NULL
      AND dead_lettered_at IS NULL;

CREATE INDEX outbox_dead_lettered_idx
    ON tutorhub.outbox_events (dead_lettered_at DESC, id DESC)
    WHERE dead_lettered_at IS NOT NULL;

DROP INDEX tutorhub.outbox_pending_idx;

REVOKE ALL ON tutorhub.outbox_events FROM PUBLIC;

COMMIT;
