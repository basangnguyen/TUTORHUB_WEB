BEGIN;

CREATE SCHEMA IF NOT EXISTS tutorhub;

CREATE TABLE tutorhub.users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL,
    display_name text NOT NULL,
    locale text NOT NULL DEFAULT 'vi',
    timezone text NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_normalized CHECK (email = lower(btrim(email))),
    CONSTRAINT users_email_not_empty CHECK (length(email) BETWEEN 3 AND 320),
    CONSTRAINT users_display_name_not_empty CHECK (length(btrim(display_name)) BETWEEN 1 AND 200),
    CONSTRAINT users_status_valid CHECK (status IN ('active', 'suspended', 'deleted'))
);

CREATE UNIQUE INDEX users_email_unique_idx ON tutorhub.users (lower(email));

CREATE TABLE tutorhub.identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES tutorhub.users (id) ON DELETE CASCADE,
    provider text NOT NULL,
    subject text NOT NULL,
    email_at_provider text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT identities_provider_not_empty CHECK (length(btrim(provider)) BETWEEN 1 AND 100),
    CONSTRAINT identities_subject_not_empty CHECK (length(btrim(subject)) BETWEEN 1 AND 500),
    CONSTRAINT identities_provider_subject_unique UNIQUE (provider, subject)
);

CREATE INDEX identities_user_id_idx ON tutorhub.identities (user_id);

CREATE TABLE tutorhub.tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text NOT NULL,
    name text NOT NULL,
    locale text NOT NULL DEFAULT 'vi',
    timezone text NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT tenants_slug_normalized CHECK (slug = lower(btrim(slug))),
    CONSTRAINT tenants_slug_valid CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$'),
    CONSTRAINT tenants_name_not_empty CHECK (length(btrim(name)) BETWEEN 1 AND 200),
    CONSTRAINT tenants_status_valid CHECK (status IN ('active', 'suspended', 'archived')),
    CONSTRAINT tenants_slug_unique UNIQUE (slug)
);

CREATE TABLE tutorhub.memberships (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES tutorhub.users (id) ON DELETE CASCADE,
    role text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    joined_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT memberships_role_valid CHECK (role IN ('org_admin', 'teacher', 'student', 'guest')),
    CONSTRAINT memberships_status_valid CHECK (status IN ('invited', 'active', 'suspended', 'removed')),
    CONSTRAINT memberships_tenant_user_unique UNIQUE (tenant_id, user_id)
);

CREATE INDEX memberships_user_id_idx ON tutorhub.memberships (user_id);
CREATE INDEX memberships_tenant_role_idx ON tutorhub.memberships (tenant_id, role, status);

CREATE TABLE tutorhub.sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES tutorhub.users (id) ON DELETE CASCADE,
    active_tenant_id uuid,
    token_hash bytea NOT NULL,
    csrf_token_hash bytea NOT NULL,
    user_agent_hash bytea,
    ip_prefix inet,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    CONSTRAINT sessions_active_membership_fk
        FOREIGN KEY (active_tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT sessions_expiry_after_creation CHECK (expires_at > created_at),
    CONSTRAINT sessions_token_hash_unique UNIQUE (token_hash)
);

CREATE INDEX sessions_user_active_idx
    ON tutorhub.sessions (user_id, expires_at DESC)
    WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx
    ON tutorhub.sessions (expires_at)
    WHERE revoked_at IS NULL;

COMMIT;
