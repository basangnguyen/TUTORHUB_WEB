BEGIN;

-- Pure predicates used by CHECK constraints; neither function reads tenant data.
CREATE FUNCTION tutorhub.sso_school_domain_valid(value text) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE SET search_path = pg_catalog AS $$
    SELECT octet_length(value) <= 253
       AND value COLLATE "C" ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$'
       AND value !~ '^[0-9.]+$'
       AND value NOT IN ('gmail.com','googlemail.com','outlook.com','hotmail.com','live.com','msn.com','yahoo.com','icloud.com')
$$;

CREATE FUNCTION tutorhub.sso_org_ids_valid(kind text, ids text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE SET search_path = pg_catalog AS $$
    SELECT cardinality(ids) BETWEEN 1 AND 64 AND array_ndims(ids) = 1
       AND cardinality(ids) = (SELECT count(DISTINCT value) FROM unnest(ids) AS item(value))
       AND NOT EXISTS (
           SELECT FROM unnest(ids) AS item(value)
           WHERE value IS NULL OR NOT CASE kind
               WHEN 'google_workspace' THEN tutorhub.sso_school_domain_valid(value)
               WHEN 'entra' THEN value COLLATE "C" ~ '^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$'
                   AND value NOT IN ('00000000-0000-0000-0000-000000000000','9188040d-6c67-4c5b-b112-36a304b66dad')
               ELSE false END)
$$;

CREATE TABLE tutorhub.auth_sso_connections (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tutorhub.tenants(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('google_workspace','entra')),
    status text NOT NULL DEFAULT 'disabled' CHECK (status IN ('enabled','disabled')),
    allowed_org_ids text[] NOT NULL CHECK (tutorhub.sso_org_ids_valid(kind, allowed_org_ids)),
    enforce_sso boolean NOT NULL DEFAULT false CHECK (NOT enforce_sso OR status = 'enabled'),
    auto_join_role text CHECK (auto_join_role IS NULL OR auto_join_role = 'student'),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now() CHECK (updated_at >= created_at),
    UNIQUE (tenant_id, id)
);
CREATE TABLE tutorhub.auth_sso_domains (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tutorhub.tenants(id) ON DELETE CASCADE,
    connection_id uuid NOT NULL,
    domain text NOT NULL CHECK (tutorhub.sso_school_domain_valid(domain)),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','verified','disabled')),
    verification_token_hash bytea CHECK (octet_length(verification_token_hash) = 32),
    verified_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now() CHECK (updated_at >= created_at),
    CHECK ((status = 'pending') = (verification_token_hash IS NOT NULL)),
    CHECK ((status = 'verified') = (verified_at IS NOT NULL)),
    CHECK (verified_at BETWEEN created_at AND updated_at),
    FOREIGN KEY (tenant_id, connection_id) REFERENCES tutorhub.auth_sso_connections(tenant_id, id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX auth_sso_domains_verified_unique ON tutorhub.auth_sso_domains(domain) WHERE status = 'verified';
CREATE INDEX auth_sso_domains_connection_idx ON tutorhub.auth_sso_domains(tenant_id, connection_id);
REVOKE ALL ON tutorhub.auth_sso_connections, tutorhub.auth_sso_domains FROM PUBLIC;
COMMIT;
