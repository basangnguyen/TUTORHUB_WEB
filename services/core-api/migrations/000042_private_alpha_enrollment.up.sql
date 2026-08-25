CREATE TABLE tutorhub.tenant_private_alpha_enrollments (
    tenant_id uuid NOT NULL REFERENCES tutorhub.tenants(id) ON DELETE CASCADE,
    program text NOT NULL,
    status text NOT NULL,
    revision bigint NOT NULL,
    notice_version text NOT NULL,
    accepted_at timestamptz,
    withdrawn_at timestamptz,
    updated_by uuid NOT NULL REFERENCES tutorhub.users(id),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, program),
    CONSTRAINT tenant_private_alpha_program_check CHECK (program = 'classroom_whiteboards'),
    CONSTRAINT tenant_private_alpha_status_check CHECK (status IN ('active', 'withdrawn')),
    CONSTRAINT tenant_private_alpha_revision_check CHECK (revision > 0),
    CONSTRAINT tenant_private_alpha_notice_check CHECK (notice_version = 'p5-collab-19-v1'),
    CONSTRAINT tenant_private_alpha_timestamps_check CHECK (
        (status = 'active' AND accepted_at IS NOT NULL AND withdrawn_at IS NULL)
        OR status = 'withdrawn'
    )
);

REVOKE ALL ON TABLE tutorhub.tenant_private_alpha_enrollments FROM PUBLIC;
