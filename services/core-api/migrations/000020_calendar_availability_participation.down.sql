BEGIN;

DROP TABLE tutorhub.calendar_participation_mutation_receipts;
DROP TABLE tutorhub.calendar_rsvp_capabilities;

DROP TRIGGER calendar_invitation_recipients_immutable_truncate
    ON tutorhub.calendar_invitation_recipients;
DROP TRIGGER calendar_invitation_recipients_immutable_rows
    ON tutorhub.calendar_invitation_recipients;
DROP TRIGGER calendar_invitation_revisions_immutable_truncate
    ON tutorhub.calendar_invitation_revisions;
DROP TRIGGER calendar_invitation_revisions_immutable_rows
    ON tutorhub.calendar_invitation_revisions;
DROP FUNCTION tutorhub.reject_calendar_invitation_snapshot_mutation();

ALTER TABLE tutorhub.class_session_attendees
    DROP CONSTRAINT class_session_attendees_response_revision_fk;

DROP TABLE tutorhub.calendar_invitation_recipients;
DROP TABLE tutorhub.calendar_invitation_revisions;

DROP TRIGGER class_session_attendees_audience_limit_guard
    ON tutorhub.class_session_attendees;
DROP FUNCTION tutorhub.enforce_class_session_attendee_limit();
DROP TABLE tutorhub.class_session_attendees;
DROP TABLE tutorhub.calendar_external_recipients;

DROP TRIGGER class_session_series_participation_defaults
    ON tutorhub.class_session_series;
DROP TRIGGER class_sessions_participation_defaults
    ON tutorhub.class_sessions;
DROP FUNCTION tutorhub.normalize_class_session_participation_defaults();

ALTER TABLE tutorhub.class_session_series
    DROP CONSTRAINT class_session_series_ical_uid_valid,
    DROP CONSTRAINT class_session_series_audience_revision_nonnegative,
    DROP CONSTRAINT class_session_series_visibility_valid,
    DROP CONSTRAINT class_session_series_show_as_valid,
    DROP CONSTRAINT class_session_series_organizer_membership_fk,
    DROP COLUMN audience_revision,
    DROP COLUMN response_requested,
    DROP COLUMN guests_can_see_guest_list,
    DROP COLUMN guests_can_modify,
    DROP COLUMN guests_can_invite,
    DROP COLUMN visibility,
    DROP COLUMN show_as,
    DROP COLUMN organizer_user_id,
    ADD CONSTRAINT class_session_series_ical_uid_valid
        CHECK (length(ical_uid) BETWEEN 16 AND 255);

ALTER TABLE tutorhub.class_sessions
    DROP CONSTRAINT class_sessions_tenant_ical_uid_unique,
    DROP CONSTRAINT class_sessions_ical_uid_valid,
    DROP CONSTRAINT class_sessions_sequence_nonnegative,
    DROP CONSTRAINT class_sessions_audience_revision_nonnegative,
    DROP CONSTRAINT class_sessions_visibility_valid,
    DROP CONSTRAINT class_sessions_show_as_valid,
    DROP CONSTRAINT class_sessions_organizer_membership_fk,
    DROP COLUMN sequence,
    DROP COLUMN ical_uid,
    DROP COLUMN audience_revision,
    DROP COLUMN response_requested,
    DROP COLUMN guests_can_see_guest_list,
    DROP COLUMN guests_can_modify,
    DROP COLUMN guests_can_invite,
    DROP COLUMN visibility,
    DROP COLUMN show_as,
    DROP COLUMN organizer_user_id;

DROP TRIGGER calendar_working_schedule_exception_intervals_guard
    ON tutorhub.calendar_working_schedule_exception_intervals;
DROP FUNCTION tutorhub.enforce_working_schedule_exception_interval();
DROP TRIGGER calendar_working_schedule_exceptions_guard
    ON tutorhub.calendar_working_schedule_exceptions;
DROP FUNCTION tutorhub.enforce_working_schedule_exception();
DROP TRIGGER calendar_working_schedule_intervals_guard
    ON tutorhub.calendar_working_schedule_intervals;
DROP FUNCTION tutorhub.enforce_working_schedule_weekly_interval();

DROP TABLE tutorhub.calendar_working_schedule_exception_intervals;
DROP TABLE tutorhub.calendar_working_schedule_exceptions;
DROP TABLE tutorhub.calendar_working_schedule_intervals;
DROP TABLE tutorhub.calendar_working_schedules;

COMMIT;
