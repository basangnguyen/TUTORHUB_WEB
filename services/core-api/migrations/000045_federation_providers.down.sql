BEGIN;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM tutorhub.auth_flows WHERE provider <> 'oidc' OR purpose = 'reauth')
       OR EXISTS (SELECT 1 FROM tutorhub.identities WHERE provider_slug <> 'oidc' AND kind IN ('oidc', 'oauth2')) THEN
        RAISE EXCEPTION 'AUTH-05 rollback blocked: federation flows or identities still exist';
    END IF;
END $$;

DROP INDEX tutorhub.identities_provider_slug_user_idx;

ALTER TABLE tutorhub.auth_flows
    DROP CONSTRAINT auth_flows_identity_link_binding_check,
    DROP CONSTRAINT auth_flows_purpose_check,
    DROP CONSTRAINT auth_flows_provider_check,
    DROP COLUMN provider,
    ADD CONSTRAINT auth_flows_purpose_check CHECK (purpose IN ('login', 'identity_link')),
    ADD CONSTRAINT auth_flows_identity_link_binding_check CHECK (
        (purpose = 'login' AND user_id IS NULL AND session_id IS NULL)
        OR (purpose = 'identity_link' AND user_id IS NOT NULL AND session_id IS NOT NULL)
    );

COMMIT;
