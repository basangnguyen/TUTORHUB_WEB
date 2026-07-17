DROP INDEX IF EXISTS tutorhub.identities_user_status_idx;
DROP INDEX IF EXISTS tutorhub.auth_flows_session_purpose_idx;

ALTER TABLE tutorhub.auth_flows
    DROP CONSTRAINT IF EXISTS auth_flows_identity_link_binding_check,
    DROP CONSTRAINT IF EXISTS auth_flows_purpose_check,
    DROP COLUMN IF EXISTS session_id,
    DROP COLUMN IF EXISTS user_id,
    DROP COLUMN IF EXISTS purpose;

ALTER TABLE tutorhub.identities
    DROP CONSTRAINT IF EXISTS identities_status_check,
    DROP COLUMN IF EXISTS unlinked_at,
    DROP COLUMN IF EXISTS status;

ALTER TABLE tutorhub.users
    DROP COLUMN IF EXISTS avatar_metadata,
    DROP COLUMN IF EXISTS avatar_object_key;
