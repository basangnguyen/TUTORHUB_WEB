BEGIN;

-- Never restore the proven-vulnerable invoker contract. An emergency
-- downgrade fails closed by disabling the maintenance entry point; applying
-- 000023 again recreates it without changing Availability Poll business data.
REVOKE ALL ON FUNCTION tutorhub.purge_expired_availability_polls(integer) FROM PUBLIC;
DROP FUNCTION IF EXISTS tutorhub.purge_expired_availability_polls(integer);

COMMIT;
