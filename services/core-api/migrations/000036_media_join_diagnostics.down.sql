BEGIN;

REVOKE ALL ON FUNCTION tutorhub.purge_expired_media_join_diagnostics(integer) FROM PUBLIC;
DROP FUNCTION tutorhub.purge_expired_media_join_diagnostics(integer);
DROP TABLE tutorhub.media_join_diagnostics;

COMMIT;
