BEGIN;
LOCK TABLE tutorhub.user_phone_numbers, tutorhub.identities, tutorhub.auth_transactions, tutorhub.auth_challenges IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT FROM tutorhub.user_phone_numbers)
        OR EXISTS (SELECT FROM tutorhub.identities WHERE kind = 'phone')
        OR EXISTS (SELECT FROM tutorhub.auth_transactions WHERE kind = 'phone')
        OR EXISTS (SELECT FROM tutorhub.auth_challenges WHERE purpose IN ('phone_login','phone_verify')) THEN
        RAISE EXCEPTION 'AUTH-10 rollback blocked: retained phone data';
    END IF;
END;
$$;
ALTER TABLE tutorhub.auth_challenges DROP COLUMN phone_country_code;
DROP TABLE tutorhub.user_phone_numbers;
COMMIT;
