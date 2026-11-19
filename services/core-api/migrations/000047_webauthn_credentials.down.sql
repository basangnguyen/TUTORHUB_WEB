BEGIN;

LOCK TABLE tutorhub.users, tutorhub.webauthn_credentials, tutorhub.auth_transactions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tutorhub.webauthn_credentials)
        OR EXISTS (SELECT 1 FROM tutorhub.users WHERE webauthn_user_handle IS NOT NULL)
        OR EXISTS (SELECT 1 FROM tutorhub.auth_transactions WHERE kind='passkey') THEN
        RAISE EXCEPTION 'AUTH-08 rollback blocked: passkey credentials, handles or transactions remain';
    END IF;
END;
$$;

DROP TABLE tutorhub.webauthn_credentials;
ALTER TABLE tutorhub.users DROP COLUMN webauthn_user_handle;
COMMIT;
