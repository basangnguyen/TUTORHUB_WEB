BEGIN;

ALTER TABLE tutorhub.identities
    DROP CONSTRAINT identities_status_check,
    ADD CONSTRAINT identities_status_check CHECK (status IN ('active', 'unlinked', 'erased')),
    ADD CONSTRAINT identities_facebook_erasure_check CHECK (
        (status <> 'erased' AND (provider_slug IS DISTINCT FROM 'facebook' OR subject NOT LIKE 'erased:%'))
        OR (status = 'erased' AND provider_slug IS NOT DISTINCT FROM 'facebook' AND subject LIKE 'erased:%'
            AND email_at_provider IS NULL AND NOT email_verified AND provider_org_id IS NULL AND provider_stable_id IS NULL)
    );

ALTER TABLE tutorhub.auth_transactions
    DROP CONSTRAINT auth_transactions_kind_valid,
    ADD CONSTRAINT auth_transactions_kind_valid CHECK (kind IN (
        'signup_email', 'login', 'recovery', 'phone', 'mfa', 'link_intent', 'reauth', 'passkey', 'federated_registration'
    )),
    ADD COLUMN provider_subject_digest bytea,
    ADD CONSTRAINT auth_transactions_provider_digest_valid CHECK (
        provider_subject_digest IS NULL OR (
           kind IN ('federated_registration','link_intent') AND state='pending' AND octet_length(provider_subject_digest)=32
        )
    ),
    ADD CONSTRAINT auth_transactions_registration_digest_required CHECK (
        kind <> 'federated_registration' OR state <> 'pending' OR provider_subject_digest IS NOT NULL
    );

CREATE INDEX auth_transactions_provider_subject_idx ON tutorhub.auth_transactions (provider_subject_digest)
    WHERE provider_subject_digest IS NOT NULL;

-- The runtime can close a transaction but cannot replace its subject digest.
CREATE FUNCTION tutorhub.clear_finished_federation_payload() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog, tutorhub AS $$
BEGIN
    NEW.provider_subject_digest := NULL;
    NEW.payload_ciphertext := NULL;
    RETURN NEW;
END;
$$;
REVOKE ALL ON FUNCTION tutorhub.clear_finished_federation_payload() FROM PUBLIC;
CREATE TRIGGER clear_finished_federation_payload
BEFORE UPDATE OF state ON tutorhub.auth_transactions
FOR EACH ROW WHEN (OLD.provider_subject_digest IS NOT NULL AND NEW.state <> 'pending')
EXECUTE FUNCTION tutorhub.clear_finished_federation_payload();

CREATE TABLE tutorhub.external_data_deletion_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider text NOT NULL CHECK (provider = 'facebook'),
    idempotency_key bytea NOT NULL UNIQUE CHECK (octet_length(idempotency_key) = 32),
    confirmation_code text NOT NULL UNIQUE CHECK (confirmation_code ~ '^[0-9a-f]{32}$'),
    status text NOT NULL CHECK (status IN ('received', 'completed')),
    created_at timestamptz NOT NULL,
    completed_at timestamptz,
    CHECK ((status = 'received' AND completed_at IS NULL) OR
           (status = 'completed' AND completed_at IS NOT NULL AND completed_at >= created_at))
);
CREATE INDEX external_data_deletion_requests_retention_idx ON tutorhub.external_data_deletion_requests (created_at);
-- An authenticated deauthorization payload must only be applied once, even if
-- its retry arrives after the user deliberately relinks the same subject.
CREATE UNIQUE INDEX outbox_facebook_deauthorization_event_idx ON tutorhub.outbox_events (aggregate_id)
    WHERE event_type='identity.provider_deauthorized';
COMMENT ON TABLE tutorhub.external_data_deletion_requests IS 'Retention: one year. Maintenance uses purge_expired_deletion_receipts. No provider identity or raw signed request.';
REVOKE ALL ON tutorhub.external_data_deletion_requests FROM PUBLIC;

-- Invoker rights: only an owner/maintenance connection may purge expired rows.
CREATE FUNCTION tutorhub.purge_expired_deletion_receipts() RETURNS bigint
LANGUAGE sql SET search_path = pg_catalog, tutorhub AS $$
    WITH expired AS (
        SELECT id FROM tutorhub.external_data_deletion_requests
        WHERE created_at <= current_timestamp - interval '1 year'
        ORDER BY created_at, id LIMIT 500 FOR UPDATE SKIP LOCKED
    ), removed AS (
        DELETE FROM tutorhub.external_data_deletion_requests r USING expired e
        WHERE r.id=e.id RETURNING 1
    ) SELECT count(*) FROM removed;
$$;
REVOKE ALL ON FUNCTION tutorhub.purge_expired_deletion_receipts() FROM PUBLIC;

CREATE FUNCTION tutorhub.erase_provider_identity(p_provider_slug text, p_issuer text, p_subject text,
    p_subject_digest bytea, p_erased_at timestamptz) RETURNS integer
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, tutorhub AS $$
DECLARE
    target_id uuid;
    erased_count integer := 0;
BEGIN
    IF p_provider_slug IS DISTINCT FROM 'facebook' OR p_subject IS NULL OR p_subject = ''
       OR p_subject LIKE 'erased:%' OR p_issuer IS NULL OR p_issuer = '' OR p_erased_at IS NULL
       OR p_subject_digest IS NULL OR octet_length(p_subject_digest) <> 32 THEN
        RAISE EXCEPTION 'invalid provider erasure request' USING ERRCODE = '22023';
    END IF;
    -- Same subject lock as federation login, pending completion and link confirmation.
    PERFORM pg_advisory_xact_lock(hashtextextended(
        format('issuer:%s:%s:subject:%s', octet_length(p_issuer), lower(p_issuer), p_subject), 0));
    -- Account settings and recovery lock the user before sessions or identities.
    PERFORM u.id FROM tutorhub.users u JOIN tutorhub.identities i ON i.user_id=u.id
      WHERE i.provider_slug='facebook' AND i.provider=p_issuer AND i.subject=p_subject
        AND i.kind IN ('oidc','oauth2') AND i.status IN ('active','unlinked') FOR UPDATE OF u;
    SELECT id INTO target_id FROM tutorhub.identities
      WHERE provider_slug = 'facebook' AND provider = p_issuer AND subject = p_subject
        AND kind IN ('oidc','oauth2') AND status IN ('active','unlinked') FOR UPDATE;
    IF FOUND THEN
        UPDATE tutorhub.sessions SET revoked_at = p_erased_at, revoked_reason = 'provider_data_deleted'
          WHERE identity_id = target_id AND revoked_at IS NULL;
        UPDATE tutorhub.identities SET subject = 'erased:' || gen_random_uuid()::text,
            email_at_provider = NULL, email_verified = false, provider_org_id = NULL, provider_stable_id = NULL,
            status = 'erased', unlinked_at = p_erased_at, updated_at = p_erased_at WHERE id = target_id;
        erased_count := 1;
    END IF;
    UPDATE tutorhub.auth_transactions SET state = 'cancelled', payload_ciphertext = NULL, provider_subject_digest = NULL
      WHERE state = 'pending' AND kind IN ('federated_registration','link_intent') AND provider_subject_digest = p_subject_digest;
    RETURN erased_count;
END;
$$;
REVOKE ALL ON FUNCTION tutorhub.erase_provider_identity(text,text,text,bytea,timestamptz) FROM PUBLIC;

-- A deployment may grant EXECUTE to named roles through ALTER DEFAULT PRIVILEGES.
-- Start these new functions owner-only, including in those environments.
DO $$
DECLARE inherited_grant record;
BEGIN
    FOR inherited_grant IN
        SELECT DISTINCT p.oid::regprocedure AS proc, r.rolname AS grantee
        FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a
        JOIN pg_roles r ON r.oid=a.grantee
        WHERE p.oid IN ('tutorhub.erase_provider_identity(text,text,text,bytea,timestamptz)'::regprocedure,
                        'tutorhub.clear_finished_federation_payload()'::regprocedure,
                        'tutorhub.purge_expired_deletion_receipts()'::regprocedure)
          AND a.grantee <> p.proowner
    LOOP
        EXECUTE format('REVOKE ALL ON FUNCTION %s FROM %I', inherited_grant.proc, inherited_grant.grantee);
    END LOOP;
END;
$$;

COMMIT;
