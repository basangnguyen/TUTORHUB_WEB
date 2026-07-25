BEGIN;

LOCK TABLE tutorhub.outbox_events IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM tutorhub.outbox_events
        WHERE lease_owner IS NOT NULL
           OR dead_lettered_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION
            'cannot roll back outbox worker schema while retained lease or dead-letter state exists';
    END IF;
END;
$$;

DROP INDEX tutorhub.outbox_dead_lettered_idx;
DROP INDEX tutorhub.outbox_pending_age_idx;
DROP INDEX tutorhub.outbox_expired_lease_claim_idx;
DROP INDEX tutorhub.outbox_ready_claim_idx;

CREATE INDEX outbox_pending_idx
    ON tutorhub.outbox_events (available_at, occurred_at)
    WHERE published_at IS NULL;

ALTER TABLE tutorhub.outbox_events
    DROP CONSTRAINT outbox_dead_letter_state_valid,
    DROP CONSTRAINT outbox_last_error_code_valid,
    DROP CONSTRAINT outbox_terminal_has_no_lease,
    DROP CONSTRAINT outbox_terminal_state_exclusive,
    DROP CONSTRAINT outbox_lease_state_valid,
    DROP CONSTRAINT outbox_lease_token_non_negative,
    DROP COLUMN dead_lettered_at,
    DROP COLUMN leased_until,
    DROP COLUMN leased_at,
    DROP COLUMN lease_token,
    DROP COLUMN lease_owner;

COMMIT;
