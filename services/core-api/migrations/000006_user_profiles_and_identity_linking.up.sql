ALTER TABLE tutorhub.users
    ADD COLUMN avatar_object_key text,
    ADD COLUMN avatar_metadata jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE tutorhub.identities
    ADD COLUMN status text NOT NULL DEFAULT 'active',
    ADD COLUMN unlinked_at timestamptz,
    ADD CONSTRAINT identities_status_check CHECK (status IN ('active', 'unlinked'));

ALTER TABLE tutorhub.auth_flows
    ADD COLUMN purpose text NOT NULL DEFAULT 'login',
    ADD COLUMN user_id uuid REFERENCES tutorhub.users(id) ON DELETE CASCADE,
    ADD COLUMN session_id uuid REFERENCES tutorhub.sessions(id) ON DELETE CASCADE,
    ADD CONSTRAINT auth_flows_purpose_check CHECK (purpose IN ('login', 'identity_link')),
    ADD CONSTRAINT auth_flows_identity_link_binding_check CHECK (
        (purpose = 'login' AND user_id IS NULL AND session_id IS NULL)
        OR
        (purpose = 'identity_link' AND user_id IS NOT NULL AND session_id IS NOT NULL)
    );

CREATE INDEX auth_flows_session_purpose_idx
    ON tutorhub.auth_flows (session_id, purpose, expires_at)
    WHERE consumed_at IS NULL;

CREATE INDEX identities_user_status_idx
    ON tutorhub.identities (user_id, status, created_at);
