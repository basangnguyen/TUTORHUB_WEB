BEGIN;

REVOKE ALL ON FUNCTION tutorhub.purge_expired_media_signal_receipts(integer) FROM PUBLIC;
DROP FUNCTION tutorhub.purge_expired_media_signal_receipts(integer);
REVOKE ALL ON FUNCTION tutorhub.purge_expired_media_reactions(integer) FROM PUBLIC;
DROP FUNCTION tutorhub.purge_expired_media_reactions(integer);

DROP TABLE tutorhub.media_signal_mutation_receipts;
DROP TABLE tutorhub.media_reaction_events;
DROP TABLE tutorhub.media_participant_hand_states;

DROP INDEX tutorhub.media_participant_sessions_roster_projection_idx;
DROP INDEX tutorhub.media_participant_sessions_room_roster_sequence_unique;
DROP INDEX tutorhub.media_participant_sessions_room_key_unique;

ALTER TABLE tutorhub.media_participant_sessions
    DROP CONSTRAINT media_participant_sessions_roster_sequence_positive,
    DROP COLUMN roster_sequence,
    DROP COLUMN participant_key;

ALTER TABLE tutorhub.media_room_instances
    DROP CONSTRAINT media_room_instances_roster_sequence_non_negative,
    DROP CONSTRAINT media_room_instances_signal_sequence_non_negative,
    DROP CONSTRAINT media_room_instances_projection_version_positive,
    DROP COLUMN next_roster_sequence,
    DROP COLUMN last_signal_sequence,
    DROP COLUMN projection_version;

COMMIT;
