BEGIN;

CREATE TABLE tutorhub.membership_invitations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    email text NOT NULL,
    intended_role text NOT NULL,
    token_hash bytea NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    expires_at timestamptz NOT NULL,
    accepted_at timestamptz,
    revoked_at timestamptz,
    invited_by uuid NOT NULL REFERENCES tutorhub.users (id),
    accepted_by uuid REFERENCES tutorhub.users (id),
    revoked_by uuid REFERENCES tutorhub.users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT membership_invitations_email_normalized CHECK (
        email = lower(btrim(email))
        AND length(email) BETWEEN 3 AND 320
    ),
    CONSTRAINT membership_invitations_role_valid CHECK (
        intended_role IN ('teacher', 'student', 'guest')
    ),
    CONSTRAINT membership_invitations_token_hash_length CHECK (
        octet_length(token_hash) = 32
    ),
    CONSTRAINT membership_invitations_token_hash_unique UNIQUE (token_hash),
    CONSTRAINT membership_invitations_status_valid CHECK (
        status IN ('pending', 'accepted', 'revoked', 'expired')
    ),
    CONSTRAINT membership_invitations_expiry_after_creation CHECK (
        expires_at > created_at
        AND expires_at <= created_at + interval '30 days'
    ),
    CONSTRAINT membership_invitations_updated_after_creation CHECK (
        updated_at >= created_at
    ),
    CONSTRAINT membership_invitations_state_consistent CHECK (
        (
            status = 'pending'
            AND accepted_at IS NULL
            AND accepted_by IS NULL
            AND revoked_at IS NULL
            AND revoked_by IS NULL
        )
        OR (
            status = 'expired'
            AND accepted_at IS NULL
            AND accepted_by IS NULL
            AND revoked_at IS NULL
            AND revoked_by IS NULL
            AND updated_at >= expires_at
        )
        OR (
            status = 'accepted'
            AND accepted_at IS NOT NULL
            AND accepted_at >= created_at
            AND accepted_at < expires_at
            AND accepted_by IS NOT NULL
            AND revoked_at IS NULL
            AND revoked_by IS NULL
            AND updated_at >= accepted_at
        )
        OR (
            status = 'revoked'
            AND accepted_at IS NULL
            AND accepted_by IS NULL
            AND revoked_at IS NOT NULL
            AND revoked_at >= created_at
            AND revoked_at < expires_at
            AND revoked_by IS NOT NULL
            AND updated_at >= revoked_at
        )
    ),
    CONSTRAINT membership_invitations_inviter_membership_fk
        FOREIGN KEY (tenant_id, invited_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT membership_invitations_acceptor_membership_fk
        FOREIGN KEY (tenant_id, accepted_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT membership_invitations_revoker_membership_fk
        FOREIGN KEY (tenant_id, revoked_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id)
);

CREATE UNIQUE INDEX membership_invitations_pending_email_unique_idx
    ON tutorhub.membership_invitations (tenant_id, email)
    WHERE status = 'pending';

CREATE INDEX membership_invitations_tenant_created_idx
    ON tutorhub.membership_invitations (tenant_id, created_at DESC, id DESC);

CREATE INDEX membership_invitations_pending_expiry_idx
    ON tutorhub.membership_invitations (expires_at, tenant_id)
    WHERE status = 'pending';

COMMIT;
