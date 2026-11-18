BEGIN;

ALTER TABLE tutorhub.auth_flows
    ADD COLUMN provider text NOT NULL DEFAULT 'oidc',
    ADD CONSTRAINT auth_flows_provider_check CHECK (provider IN ('oidc', 'google', 'facebook', 'microsoft', 'apple')),
    DROP CONSTRAINT auth_flows_purpose_check,
    DROP CONSTRAINT auth_flows_identity_link_binding_check,
    ADD CONSTRAINT auth_flows_purpose_check CHECK (purpose IN ('login', 'identity_link', 'reauth')),
    ADD CONSTRAINT auth_flows_identity_link_binding_check CHECK (
        (purpose = 'login' AND user_id IS NULL AND session_id IS NULL)
        OR (purpose IN ('identity_link', 'reauth') AND user_id IS NOT NULL AND session_id IS NOT NULL)
    );

CREATE INDEX identities_provider_slug_user_idx
    ON tutorhub.identities (provider_slug, user_id)
    WHERE provider_slug IS NOT NULL;

COMMIT;
