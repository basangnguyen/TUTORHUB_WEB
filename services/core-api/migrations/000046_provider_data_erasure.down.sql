BEGIN;

LOCK TABLE tutorhub.external_data_deletion_requests, tutorhub.identities,
    tutorhub.sessions, tutorhub.auth_transactions, tutorhub.outbox_events IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tutorhub.identities WHERE status='erased')
        OR EXISTS (SELECT 1 FROM tutorhub.external_data_deletion_requests)
        OR EXISTS (SELECT 1 FROM tutorhub.auth_transactions WHERE kind='federated_registration') THEN
        RAISE EXCEPTION 'AUTH-06 rollback blocked: erased identities, deletion receipts or registration transactions remain';
    END IF;
END;
$$;

DROP FUNCTION tutorhub.erase_provider_identity(text,text,text,bytea,timestamptz);
DROP FUNCTION tutorhub.purge_expired_deletion_receipts();
DROP TABLE tutorhub.external_data_deletion_requests;
DROP INDEX tutorhub.outbox_facebook_deauthorization_event_idx;
DROP TRIGGER clear_finished_federation_payload ON tutorhub.auth_transactions;
DROP FUNCTION tutorhub.clear_finished_federation_payload();
ALTER TABLE tutorhub.auth_transactions DROP COLUMN provider_subject_digest, DROP CONSTRAINT auth_transactions_kind_valid,
    ADD CONSTRAINT auth_transactions_kind_valid CHECK (kind IN (
        'signup_email', 'login', 'recovery', 'phone', 'mfa', 'link_intent', 'reauth', 'passkey'
    ));
ALTER TABLE tutorhub.identities DROP CONSTRAINT identities_facebook_erasure_check, DROP CONSTRAINT identities_status_check,
    ADD CONSTRAINT identities_status_check CHECK (status IN ('active','unlinked'));

COMMIT;
