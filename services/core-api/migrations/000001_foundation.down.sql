BEGIN;

DROP TABLE IF EXISTS tutorhub.sessions;
DROP TABLE IF EXISTS tutorhub.memberships;
DROP TABLE IF EXISTS tutorhub.tenants;
DROP TABLE IF EXISTS tutorhub.identities;
DROP TABLE IF EXISTS tutorhub.users;
DROP SCHEMA IF EXISTS tutorhub;

COMMIT;
