BEGIN;

LOCK TABLE tutorhub.auth_challenge_deliveries IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT FROM tutorhub.auth_challenge_deliveries WHERE provider = 'resend') THEN
        RAISE EXCEPTION 'AUTH-14 rollback blocked: retained Resend delivery receipts'
            USING ERRCODE = '55000';
    END IF;
END;
$$;

ALTER TABLE tutorhub.auth_challenge_deliveries
    DROP CONSTRAINT auth_deliveries_provider_valid,
    ADD CONSTRAINT auth_deliveries_provider_valid
        CHECK (provider IN ('disabled', 'devinbox', 'smtp', 'ses', 'zns', 'sms_brandname', 'twilio'));

COMMIT;
