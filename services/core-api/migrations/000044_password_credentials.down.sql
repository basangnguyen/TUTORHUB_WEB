BEGIN;

LOCK TABLE tutorhub.users, tutorhub.user_password_credentials, tutorhub.user_legal_acceptances
    IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tutorhub.user_password_credentials)
        OR EXISTS (SELECT 1 FROM tutorhub.user_legal_acceptances) THEN
        RAISE EXCEPTION 'AUTH-02A rollback blocked: retained password credentials or legal acceptances; use a reviewed forward migration';
    END IF;
END $$;

DROP TABLE tutorhub.user_legal_acceptances;
DROP TABLE tutorhub.user_password_credentials;
ALTER TABLE tutorhub.users DROP COLUMN email_verified_at;

COMMIT;
