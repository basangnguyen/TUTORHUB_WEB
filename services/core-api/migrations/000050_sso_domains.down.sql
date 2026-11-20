BEGIN;
LOCK TABLE tutorhub.auth_sso_connections, tutorhub.auth_sso_domains IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT FROM tutorhub.auth_sso_connections) OR EXISTS (SELECT FROM tutorhub.auth_sso_domains) THEN
        RAISE EXCEPTION 'AUTH-12 rollback blocked: retained school SSO configuration';
    END IF;
END;
$$;
DROP TABLE tutorhub.auth_sso_domains, tutorhub.auth_sso_connections;
DROP FUNCTION tutorhub.sso_org_ids_valid(text,text[]), tutorhub.sso_school_domain_valid(text);
COMMIT;
