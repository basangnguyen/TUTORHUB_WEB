BEGIN;

DROP TABLE tutorhub.rate_limit_windows;
DROP TABLE tutorhub.tenant_quota_windows;
DROP TABLE tutorhub.tenant_quota_overrides;
DROP TABLE tutorhub.tenant_feature_overrides;
DROP TABLE tutorhub.tenant_feature_control_revisions;

COMMIT;
