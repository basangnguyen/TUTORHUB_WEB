BEGIN;
LOCK TABLE tutorhub.mfa_totp_factors, tutorhub.mfa_recovery_codes, tutorhub.auth_transactions IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT FROM tutorhub.mfa_totp_factors)
        OR EXISTS (SELECT FROM tutorhub.mfa_recovery_codes)
        OR EXISTS (SELECT FROM tutorhub.auth_transactions WHERE kind='mfa') THEN
        RAISE EXCEPTION 'AUTH-09 rollback blocked: retained MFA data';
    END IF;
END;
$$;
DROP TABLE tutorhub.mfa_recovery_codes, tutorhub.mfa_totp_factors;
COMMIT;
