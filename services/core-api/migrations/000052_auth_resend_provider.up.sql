BEGIN;

ALTER TABLE tutorhub.auth_challenge_deliveries
    DROP CONSTRAINT auth_deliveries_provider_valid,
    ADD CONSTRAINT auth_deliveries_provider_valid
        CHECK (provider IN ('disabled', 'devinbox', 'smtp', 'ses', 'resend', 'zns', 'sms_brandname', 'twilio'));

COMMIT;
