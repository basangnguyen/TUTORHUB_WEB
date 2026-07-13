BEGIN;

CREATE TABLE tutorhub.classes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    owner_user_id uuid NOT NULL,
    code text NOT NULL,
    title text NOT NULL,
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'draft',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT classes_owner_membership_fk
        FOREIGN KEY (tenant_id, owner_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT classes_code_normalized CHECK (code = upper(btrim(code))),
    CONSTRAINT classes_code_not_empty CHECK (length(code) BETWEEN 3 AND 32),
    CONSTRAINT classes_title_not_empty CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    CONSTRAINT classes_status_valid CHECK (status IN ('draft', 'active', 'archived')),
    CONSTRAINT classes_archive_state_valid CHECK (
        (status = 'archived' AND archived_at IS NOT NULL)
        OR (status <> 'archived' AND archived_at IS NULL)
    ),
    CONSTRAINT classes_tenant_code_unique UNIQUE (tenant_id, code),
    CONSTRAINT classes_tenant_id_id_unique UNIQUE (tenant_id, id)
);

CREATE INDEX classes_tenant_status_created_idx
    ON tutorhub.classes (tenant_id, status, created_at DESC);
CREATE INDEX classes_owner_idx
    ON tutorhub.classes (tenant_id, owner_user_id, status);

COMMIT;
