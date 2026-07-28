BEGIN;

CREATE TABLE tutorhub.calendar_working_schedules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    scope text NOT NULL,
    owner_user_id uuid,
    timezone text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_by uuid NOT NULL,
    updated_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_working_schedules_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT calendar_working_schedules_owner_membership_fk
        FOREIGN KEY (tenant_id, owner_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT calendar_working_schedules_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT calendar_working_schedules_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT calendar_working_schedules_scope_valid CHECK (
        (scope = 'tenant_default' AND owner_user_id IS NULL)
        OR (scope = 'user_override' AND owner_user_id IS NOT NULL)
    ),
    CONSTRAINT calendar_working_schedules_timezone_valid CHECK (
        timezone = btrim(timezone)
        AND length(timezone) BETWEEN 1 AND 100
        AND lower(timezone) <> 'local'
    ),
    CONSTRAINT calendar_working_schedules_version_positive CHECK (version > 0),
    CONSTRAINT calendar_working_schedules_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX calendar_working_schedules_tenant_default_idx
    ON tutorhub.calendar_working_schedules (tenant_id)
    WHERE scope = 'tenant_default';

CREATE UNIQUE INDEX calendar_working_schedules_user_override_idx
    ON tutorhub.calendar_working_schedules (tenant_id, owner_user_id)
    WHERE scope = 'user_override';

CREATE TABLE tutorhub.calendar_working_schedule_intervals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    schedule_id uuid NOT NULL,
    weekday smallint NOT NULL,
    start_minute smallint NOT NULL,
    end_minute smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_working_schedule_intervals_schedule_fk
        FOREIGN KEY (tenant_id, schedule_id)
        REFERENCES tutorhub.calendar_working_schedules (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_working_schedule_intervals_weekday_valid
        CHECK (weekday BETWEEN 1 AND 7),
    CONSTRAINT calendar_working_schedule_intervals_range_valid CHECK (
        start_minute BETWEEN 0 AND 1439
        AND end_minute BETWEEN 1 AND 1440
        AND start_minute < end_minute
    ),
    CONSTRAINT calendar_working_schedule_intervals_unique_start
        UNIQUE (tenant_id, schedule_id, weekday, start_minute)
);

CREATE INDEX calendar_working_schedule_intervals_lookup_idx
    ON tutorhub.calendar_working_schedule_intervals
        (tenant_id, schedule_id, weekday, start_minute, end_minute);

CREATE TABLE tutorhub.calendar_working_schedule_exceptions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    schedule_id uuid NOT NULL,
    exception_date date NOT NULL,
    exception_type text NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_working_schedule_exceptions_tenant_id_unique
        UNIQUE (tenant_id, schedule_id, id),
    CONSTRAINT calendar_working_schedule_exceptions_schedule_fk
        FOREIGN KEY (tenant_id, schedule_id)
        REFERENCES tutorhub.calendar_working_schedules (tenant_id, id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_working_schedule_exceptions_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT calendar_working_schedule_exceptions_type_valid
        CHECK (exception_type IN ('holiday', 'out_of_office', 'special_hours')),
    CONSTRAINT calendar_working_schedule_exceptions_unique_date
        UNIQUE (tenant_id, schedule_id, exception_date)
);

CREATE INDEX calendar_working_schedule_exceptions_date_idx
    ON tutorhub.calendar_working_schedule_exceptions
        (tenant_id, schedule_id, exception_date);

CREATE TABLE tutorhub.calendar_working_schedule_exception_intervals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    schedule_id uuid NOT NULL,
    exception_id uuid NOT NULL,
    start_minute smallint NOT NULL,
    end_minute smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_schedule_exception_intervals_exception_fk
        FOREIGN KEY (tenant_id, schedule_id, exception_id)
        REFERENCES tutorhub.calendar_working_schedule_exceptions
            (tenant_id, schedule_id, id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_schedule_exception_intervals_range_valid CHECK (
        start_minute BETWEEN 0 AND 1439
        AND end_minute BETWEEN 1 AND 1440
        AND start_minute < end_minute
    ),
    CONSTRAINT calendar_schedule_exception_intervals_unique_start
        UNIQUE (tenant_id, schedule_id, exception_id, start_minute)
);

CREATE FUNCTION tutorhub.enforce_working_schedule_weekly_interval()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    existing_count integer;
BEGIN
    PERFORM 1
    FROM tutorhub.calendar_working_schedules
    WHERE tenant_id = NEW.tenant_id AND id = NEW.schedule_id
    FOR UPDATE;

    IF EXISTS (
        SELECT 1
        FROM tutorhub.calendar_working_schedule_intervals AS interval_row
        WHERE interval_row.tenant_id = NEW.tenant_id
          AND interval_row.schedule_id = NEW.schedule_id
          AND interval_row.weekday = NEW.weekday
          AND interval_row.id <> NEW.id
          AND interval_row.start_minute < NEW.end_minute
          AND interval_row.end_minute > NEW.start_minute
    ) THEN
        RAISE EXCEPTION 'working schedule intervals overlap'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'calendar_working_schedule_intervals_no_overlap';
    END IF;

    SELECT count(*)
    INTO existing_count
    FROM tutorhub.calendar_working_schedule_intervals AS interval_row
    WHERE interval_row.tenant_id = NEW.tenant_id
      AND interval_row.schedule_id = NEW.schedule_id
      AND interval_row.weekday = NEW.weekday
      AND interval_row.id <> NEW.id;

    IF existing_count >= 8 THEN
        RAISE EXCEPTION 'working schedule weekday interval limit exceeded'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'calendar_working_schedule_intervals_weekday_limit';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER calendar_working_schedule_intervals_guard
    BEFORE INSERT OR UPDATE
    ON tutorhub.calendar_working_schedule_intervals
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.enforce_working_schedule_weekly_interval();

CREATE FUNCTION tutorhub.enforce_working_schedule_exception()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    existing_count integer;
BEGIN
    PERFORM 1
    FROM tutorhub.calendar_working_schedules
    WHERE tenant_id = NEW.tenant_id AND id = NEW.schedule_id
    FOR UPDATE;

    SELECT count(*)
    INTO existing_count
    FROM tutorhub.calendar_working_schedule_exceptions AS exception_row
    WHERE exception_row.tenant_id = NEW.tenant_id
      AND exception_row.schedule_id = NEW.schedule_id
      AND exception_row.id <> NEW.id;

    IF existing_count >= 366 THEN
        RAISE EXCEPTION 'working schedule exception limit exceeded'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'calendar_working_schedule_exceptions_limit';
    END IF;

    IF TG_OP = 'UPDATE'
       AND NEW.exception_type <> 'special_hours'
       AND EXISTS (
           SELECT 1
           FROM tutorhub.calendar_working_schedule_exception_intervals
           WHERE tenant_id = OLD.tenant_id
             AND schedule_id = OLD.schedule_id
             AND exception_id = OLD.id
       ) THEN
        RAISE EXCEPTION 'non-working exception cannot retain special-hour intervals'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'calendar_working_schedule_exception_type_consistent';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER calendar_working_schedule_exceptions_guard
    BEFORE INSERT OR UPDATE
    ON tutorhub.calendar_working_schedule_exceptions
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.enforce_working_schedule_exception();

CREATE FUNCTION tutorhub.enforce_working_schedule_exception_interval()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    parent_type text;
    existing_count integer;
BEGIN
    SELECT exception_type
    INTO parent_type
    FROM tutorhub.calendar_working_schedule_exceptions
    WHERE tenant_id = NEW.tenant_id
      AND schedule_id = NEW.schedule_id
      AND id = NEW.exception_id
    FOR UPDATE;

    IF FOUND AND parent_type <> 'special_hours' THEN
        RAISE EXCEPTION 'only special-hours exceptions can contain intervals'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'calendar_schedule_exception_intervals_type_valid';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM tutorhub.calendar_working_schedule_exception_intervals AS interval_row
        WHERE interval_row.tenant_id = NEW.tenant_id
          AND interval_row.schedule_id = NEW.schedule_id
          AND interval_row.exception_id = NEW.exception_id
          AND interval_row.id <> NEW.id
          AND interval_row.start_minute < NEW.end_minute
          AND interval_row.end_minute > NEW.start_minute
    ) THEN
        RAISE EXCEPTION 'working schedule exception intervals overlap'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'calendar_schedule_exception_intervals_no_overlap';
    END IF;

    SELECT count(*)
    INTO existing_count
    FROM tutorhub.calendar_working_schedule_exception_intervals AS interval_row
    WHERE interval_row.tenant_id = NEW.tenant_id
      AND interval_row.schedule_id = NEW.schedule_id
      AND interval_row.exception_id = NEW.exception_id
      AND interval_row.id <> NEW.id;

    IF existing_count >= 8 THEN
        RAISE EXCEPTION 'special-hours interval limit exceeded'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'calendar_schedule_exception_intervals_limit';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER calendar_working_schedule_exception_intervals_guard
    BEFORE INSERT OR UPDATE
    ON tutorhub.calendar_working_schedule_exception_intervals
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.enforce_working_schedule_exception_interval();

ALTER TABLE tutorhub.class_sessions
    ADD COLUMN organizer_user_id uuid,
    ADD COLUMN show_as text NOT NULL DEFAULT 'busy',
    ADD COLUMN visibility text NOT NULL DEFAULT 'class',
    ADD COLUMN guests_can_invite boolean NOT NULL DEFAULT false,
    ADD COLUMN guests_can_modify boolean NOT NULL DEFAULT false,
    ADD COLUMN guests_can_see_guest_list boolean NOT NULL DEFAULT false,
    ADD COLUMN response_requested boolean NOT NULL DEFAULT false,
    ADD COLUMN audience_revision bigint NOT NULL DEFAULT 0,
    ADD COLUMN ical_uid text,
    ADD COLUMN sequence bigint NOT NULL DEFAULT 0;

UPDATE tutorhub.class_sessions
SET organizer_user_id = created_by,
    ical_uid = 'urn:uuid:' || id::text
WHERE organizer_user_id IS NULL OR ical_uid IS NULL;

ALTER TABLE tutorhub.class_sessions
    ALTER COLUMN organizer_user_id SET NOT NULL,
    ALTER COLUMN ical_uid SET NOT NULL,
    ALTER COLUMN ical_uid SET DEFAULT ('urn:uuid:' || gen_random_uuid()::text),
    ADD CONSTRAINT class_sessions_organizer_membership_fk
        FOREIGN KEY (tenant_id, organizer_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    ADD CONSTRAINT class_sessions_show_as_valid
        CHECK (show_as IN ('free', 'tentative', 'busy', 'out_of_office')),
    ADD CONSTRAINT class_sessions_visibility_valid
        CHECK (visibility IN ('class', 'private')),
    ADD CONSTRAINT class_sessions_audience_revision_nonnegative
        CHECK (audience_revision >= 0),
    ADD CONSTRAINT class_sessions_sequence_nonnegative CHECK (sequence >= 0),
    ADD CONSTRAINT class_sessions_ical_uid_valid CHECK (
        ical_uid ~ '^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    ),
    ADD CONSTRAINT class_sessions_tenant_ical_uid_unique
        UNIQUE (tenant_id, ical_uid);

ALTER TABLE tutorhub.class_session_series
    ADD COLUMN organizer_user_id uuid,
    ADD COLUMN show_as text NOT NULL DEFAULT 'busy',
    ADD COLUMN visibility text NOT NULL DEFAULT 'class',
    ADD COLUMN guests_can_invite boolean NOT NULL DEFAULT false,
    ADD COLUMN guests_can_modify boolean NOT NULL DEFAULT false,
    ADD COLUMN guests_can_see_guest_list boolean NOT NULL DEFAULT false,
    ADD COLUMN response_requested boolean NOT NULL DEFAULT false,
    ADD COLUMN audience_revision bigint NOT NULL DEFAULT 0;

UPDATE tutorhub.class_session_series
SET organizer_user_id = created_by,
    ical_uid = 'urn:uuid:' || id::text
WHERE organizer_user_id IS NULL
   OR ical_uid !~ '^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$';

ALTER TABLE tutorhub.class_session_series
    ALTER COLUMN organizer_user_id SET NOT NULL,
    DROP CONSTRAINT class_session_series_ical_uid_valid,
    ADD CONSTRAINT class_session_series_organizer_membership_fk
        FOREIGN KEY (tenant_id, organizer_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    ADD CONSTRAINT class_session_series_show_as_valid
        CHECK (show_as IN ('free', 'tentative', 'busy', 'out_of_office')),
    ADD CONSTRAINT class_session_series_visibility_valid
        CHECK (visibility IN ('class', 'private')),
    ADD CONSTRAINT class_session_series_audience_revision_nonnegative
        CHECK (audience_revision >= 0),
    ADD CONSTRAINT class_session_series_ical_uid_valid CHECK (
        ical_uid ~ '^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    );

CREATE FUNCTION tutorhub.normalize_class_session_participation_defaults()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.organizer_user_id IS NULL THEN
        NEW.organizer_user_id := NEW.created_by;
    END IF;
    IF NEW.ical_uid IS NULL
       OR NEW.ical_uid = NEW.id::text || '@calendar.tutorhub' THEN
        NEW.ical_uid := 'urn:uuid:' || NEW.id::text;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER class_sessions_participation_defaults
    BEFORE INSERT OR UPDATE OF organizer_user_id, ical_uid
    ON tutorhub.class_sessions
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.normalize_class_session_participation_defaults();

CREATE TRIGGER class_session_series_participation_defaults
    BEFORE INSERT OR UPDATE OF organizer_user_id, ical_uid
    ON tutorhub.class_session_series
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.normalize_class_session_participation_defaults();

CREATE TABLE tutorhub.calendar_external_recipients (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL
        REFERENCES tutorhub.tenants (id) ON DELETE CASCADE,
    delivery_address_fingerprint bytea NOT NULL,
    delivery_address_ciphertext bytea NOT NULL,
    display_name_ciphertext bytea,
    crypto_key_version smallint NOT NULL,
    status text NOT NULL DEFAULT 'active',
    created_by uuid NOT NULL,
    redacted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_external_recipients_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT calendar_external_recipients_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT calendar_external_recipients_fingerprint_valid
        CHECK (octet_length(delivery_address_fingerprint) = 32),
    CONSTRAINT calendar_external_recipients_ciphertext_valid CHECK (
        octet_length(delivery_address_ciphertext) BETWEEN 32 AND 4096
        AND (
            display_name_ciphertext IS NULL
            OR octet_length(display_name_ciphertext) BETWEEN 32 AND 2048
        )
    ),
    CONSTRAINT calendar_external_recipients_key_version_positive
        CHECK (crypto_key_version > 0),
    CONSTRAINT calendar_external_recipients_status_valid
        CHECK (status IN ('active', 'redacted')),
    CONSTRAINT calendar_external_recipients_redaction_consistent CHECK (
        (status = 'active' AND redacted_at IS NULL)
        OR (status = 'redacted' AND redacted_at IS NOT NULL)
    ),
    CONSTRAINT calendar_external_recipients_updated_after_created
        CHECK (updated_at >= created_at)
);

CREATE UNIQUE INDEX calendar_external_recipients_fingerprint_idx
    ON tutorhub.calendar_external_recipients
        (tenant_id, delivery_address_fingerprint)
    WHERE status = 'active';

CREATE TABLE tutorhub.class_session_attendees (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    session_id uuid,
    series_id uuid,
    occurrence_key text,
    internal_user_id uuid,
    external_recipient_id uuid,
    participation_role text NOT NULL,
    business_role text NOT NULL,
    audience_source text NOT NULL,
    show_as text NOT NULL DEFAULT 'busy',
    visibility text NOT NULL DEFAULT 'class',
    can_invite_others boolean NOT NULL DEFAULT false,
    can_modify_event boolean NOT NULL DEFAULT false,
    can_see_guest_list boolean NOT NULL DEFAULT false,
    response_requested boolean NOT NULL DEFAULT true,
    status text NOT NULL DEFAULT 'active',
    rsvp_state text NOT NULL DEFAULT 'needs_action',
    rsvp_source text NOT NULL DEFAULT 'none',
    responded_at timestamptz,
    response_note text,
    response_invitation_revision_id uuid,
    response_sequence bigint,
    response_closed_at timestamptz,
    version bigint NOT NULL DEFAULT 1,
    created_by uuid NOT NULL,
    updated_by uuid,
    removed_by uuid,
    removed_at timestamptz,
    removal_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT class_session_attendees_tenant_class_id_unique
        UNIQUE (tenant_id, class_id, id),
    CONSTRAINT class_session_attendees_session_fk
        FOREIGN KEY (tenant_id, class_id, session_id)
        REFERENCES tutorhub.class_sessions (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_session_attendees_series_fk
        FOREIGN KEY (tenant_id, class_id, series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT class_session_attendees_internal_membership_fk
        FOREIGN KEY (tenant_id, internal_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_attendees_external_recipient_fk
        FOREIGN KEY (tenant_id, external_recipient_id)
        REFERENCES tutorhub.calendar_external_recipients (tenant_id, id),
    CONSTRAINT class_session_attendees_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_attendees_updater_membership_fk
        FOREIGN KEY (tenant_id, updated_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_attendees_remover_membership_fk
        FOREIGN KEY (tenant_id, removed_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT class_session_attendees_source_scope_valid CHECK (
        (
            session_id IS NOT NULL
            AND series_id IS NULL
            AND occurrence_key IS NULL
        )
        OR (
            session_id IS NULL
            AND series_id IS NOT NULL
            AND (
                occurrence_key IS NULL
                OR length(occurrence_key) BETWEEN 8 AND 128
            )
        )
    ),
    CONSTRAINT class_session_attendees_identity_scope_valid CHECK (
        (internal_user_id IS NOT NULL AND external_recipient_id IS NULL)
        OR (internal_user_id IS NULL AND external_recipient_id IS NOT NULL)
    ),
    CONSTRAINT class_session_attendees_participation_role_valid
        CHECK (participation_role IN ('required', 'optional')),
    CONSTRAINT class_session_attendees_business_role_valid CHECK (
        business_role IN (
            'organizer', 'teacher', 'co_teacher',
            'teaching_assistant', 'student', 'external_guest'
        )
        AND (
            (external_recipient_id IS NOT NULL
             AND business_role = 'external_guest'
             AND audience_source = 'manual')
            OR (internal_user_id IS NOT NULL AND business_role <> 'external_guest')
        )
    ),
    CONSTRAINT class_session_attendees_audience_source_valid
        CHECK (audience_source IN ('roster', 'manual')),
    CONSTRAINT class_session_attendees_show_as_valid
        CHECK (show_as IN ('free', 'tentative', 'busy', 'out_of_office')),
    CONSTRAINT class_session_attendees_visibility_valid
        CHECK (visibility IN ('class', 'private')),
    CONSTRAINT class_session_attendees_status_valid
        CHECK (status IN ('active', 'removed')),
    CONSTRAINT class_session_attendees_rsvp_state_valid CHECK (
        rsvp_state IN ('needs_action', 'accepted', 'tentative', 'declined')
    ),
    CONSTRAINT class_session_attendees_rsvp_source_valid CHECK (
        rsvp_source IN (
            'none', 'tutorhub_authenticated',
            'tutorhub_external_capability', 'organizer_override'
        )
    ),
    CONSTRAINT class_session_attendees_rsvp_identity_valid CHECK (
        (internal_user_id IS NULL OR rsvp_source <> 'tutorhub_external_capability')
        AND (external_recipient_id IS NULL OR rsvp_source <> 'tutorhub_authenticated')
    ),
    CONSTRAINT class_session_attendees_rsvp_state_consistent CHECK (
        (
            rsvp_state = 'needs_action'
            AND rsvp_source = 'none'
            AND responded_at IS NULL
            AND response_note IS NULL
            AND response_invitation_revision_id IS NULL
            AND response_sequence IS NULL
        )
        OR (
            rsvp_state <> 'needs_action'
            AND rsvp_source <> 'none'
            AND responded_at IS NOT NULL
            AND response_invitation_revision_id IS NOT NULL
            AND response_sequence IS NOT NULL
            AND response_sequence >= 0
        )
    ),
    CONSTRAINT class_session_attendees_response_note_valid
        CHECK (response_note IS NULL OR length(response_note) <= 500),
    CONSTRAINT class_session_attendees_lifecycle_consistent CHECK (
        (
            status = 'active'
            AND removed_at IS NULL
            AND removed_by IS NULL
            AND removal_reason IS NULL
        )
        OR (
            status = 'removed'
            AND removed_at IS NOT NULL
            AND response_closed_at IS NOT NULL
            AND removal_reason = btrim(removal_reason)
            AND length(removal_reason) BETWEEN 3 AND 100
            AND removal_reason ~ '^[a-z][a-z0-9._-]{2,99}$'
        )
    ),
    CONSTRAINT class_session_attendees_version_positive CHECK (version > 0),
    CONSTRAINT class_session_attendees_updated_after_created
        CHECK (updated_at >= created_at),
    CONSTRAINT class_session_attendees_response_after_creation
        CHECK (responded_at IS NULL OR responded_at >= created_at),
    CONSTRAINT class_session_attendees_closed_after_creation
        CHECK (response_closed_at IS NULL OR response_closed_at >= created_at),
    CONSTRAINT class_session_attendees_removed_after_creation
        CHECK (removed_at IS NULL OR removed_at >= created_at)
);

CREATE UNIQUE INDEX class_session_attendees_active_identity_idx
    ON tutorhub.class_session_attendees (
        tenant_id,
        class_id,
        session_id,
        series_id,
        occurrence_key,
        internal_user_id,
        external_recipient_id
    ) NULLS NOT DISTINCT
    WHERE status = 'active';

CREATE INDEX class_session_attendees_internal_free_busy_idx
    ON tutorhub.class_session_attendees
        (tenant_id, internal_user_id, status, session_id, series_id)
    WHERE internal_user_id IS NOT NULL;

CREATE INDEX class_session_attendees_external_class_active_idx
    ON tutorhub.class_session_attendees
        (tenant_id, class_id, external_recipient_id)
    WHERE status = 'active' AND external_recipient_id IS NOT NULL;

CREATE INDEX class_session_attendees_source_idx
    ON tutorhub.class_session_attendees
        (tenant_id, class_id, session_id, series_id, occurrence_key, status);

CREATE FUNCTION tutorhub.enforce_class_session_attendee_limit()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    existing_count integer;
BEGIN
    IF NEW.status <> 'active' THEN
        RETURN NEW;
    END IF;

    IF NEW.session_id IS NOT NULL THEN
        PERFORM 1
        FROM tutorhub.class_sessions
        WHERE tenant_id = NEW.tenant_id
          AND class_id = NEW.class_id
          AND id = NEW.session_id
        FOR UPDATE;
    ELSE
        PERFORM 1
        FROM tutorhub.class_session_series
        WHERE tenant_id = NEW.tenant_id
          AND class_id = NEW.class_id
          AND id = NEW.series_id
        FOR UPDATE;
    END IF;

    SELECT count(*)
    INTO existing_count
    FROM tutorhub.class_session_attendees AS attendee
    WHERE attendee.tenant_id = NEW.tenant_id
      AND attendee.class_id = NEW.class_id
      AND attendee.session_id IS NOT DISTINCT FROM NEW.session_id
      AND attendee.series_id IS NOT DISTINCT FROM NEW.series_id
      AND attendee.occurrence_key IS NOT DISTINCT FROM NEW.occurrence_key
      AND attendee.status = 'active'
      AND attendee.id <> NEW.id;

    IF existing_count >= 128 THEN
        RAISE EXCEPTION 'class session attendee limit exceeded'
            USING ERRCODE = '23514',
                  CONSTRAINT = 'class_session_attendees_audience_limit';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER class_session_attendees_audience_limit_guard
    BEFORE INSERT OR UPDATE OF
        tenant_id, class_id, session_id, series_id, occurrence_key, status
    ON tutorhub.class_session_attendees
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.enforce_class_session_attendee_limit();

CREATE TABLE tutorhub.calendar_invitation_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    session_id uuid,
    series_id uuid,
    occurrence_key text,
    source_version bigint NOT NULL,
    audience_revision bigint NOT NULL,
    ical_uid text NOT NULL,
    ical_sequence bigint NOT NULL,
    method text NOT NULL,
    lifecycle text NOT NULL,
    organizer_user_id uuid NOT NULL,
    actor_type text NOT NULL,
    created_by uuid,
    reason_code text NOT NULL,
    timezone_data_version text NOT NULL,
    canonical_payload_ciphertext bytea NOT NULL,
    canonical_payload_sha256 bytea NOT NULL,
    crypto_key_version smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_invitation_revisions_tenant_class_id_unique
        UNIQUE (tenant_id, class_id, id),
    CONSTRAINT calendar_invitation_revisions_session_fk
        FOREIGN KEY (tenant_id, class_id, session_id)
        REFERENCES tutorhub.class_sessions (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_invitation_revisions_series_fk
        FOREIGN KEY (tenant_id, class_id, series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_invitation_revisions_organizer_membership_fk
        FOREIGN KEY (tenant_id, organizer_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT calendar_invitation_revisions_creator_membership_fk
        FOREIGN KEY (tenant_id, created_by)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT calendar_invitation_revisions_source_scope_valid CHECK (
        (
            session_id IS NOT NULL
            AND series_id IS NULL
            AND occurrence_key IS NULL
        )
        OR (
            session_id IS NULL
            AND series_id IS NOT NULL
            AND (
                occurrence_key IS NULL
                OR length(occurrence_key) BETWEEN 8 AND 128
            )
        )
    ),
    CONSTRAINT calendar_invitation_revisions_versions_valid CHECK (
        source_version > 0
        AND audience_revision > 0
        AND ical_sequence >= 0
    ),
    CONSTRAINT calendar_invitation_revisions_ical_uid_valid CHECK (
        ical_uid ~ '^urn:uuid:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    ),
    CONSTRAINT calendar_invitation_revisions_method_valid
        CHECK (method IN ('REQUEST', 'CANCEL')),
    CONSTRAINT calendar_invitation_revisions_lifecycle_valid
        CHECK (lifecycle IN ('published', 'updated', 'cancelled')),
    CONSTRAINT calendar_invitation_revisions_actor_valid CHECK (
        (actor_type = 'user' AND created_by IS NOT NULL)
        OR (actor_type = 'system' AND created_by IS NULL)
    ),
    CONSTRAINT calendar_invitation_revisions_reason_valid CHECK (
        reason_code = btrim(reason_code)
        AND length(reason_code) BETWEEN 3 AND 100
        AND reason_code ~ '^[a-z][a-z0-9._-]{2,99}$'
    ),
    CONSTRAINT calendar_invitation_revisions_tzdata_valid CHECK (
        timezone_data_version = btrim(timezone_data_version)
        AND length(timezone_data_version) BETWEEN 1 AND 64
    ),
    CONSTRAINT calendar_invitation_revisions_payload_valid CHECK (
        octet_length(canonical_payload_ciphertext) BETWEEN 32 AND 262144
        AND octet_length(canonical_payload_sha256) = 32
        AND crypto_key_version > 0
    )
);

CREATE UNIQUE INDEX calendar_invitation_revisions_source_sequence_idx
    ON tutorhub.calendar_invitation_revisions (
        tenant_id, class_id, session_id, series_id, occurrence_key, ical_sequence
    ) NULLS NOT DISTINCT;

CREATE TABLE tutorhub.calendar_invitation_recipients (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    class_id uuid NOT NULL,
    invitation_revision_id uuid NOT NULL,
    attendee_id uuid NOT NULL,
    recipient_kind text NOT NULL,
    participation_role text NOT NULL,
    business_role text NOT NULL,
    audience_source text NOT NULL,
    response_requested boolean NOT NULL,
    can_see_guest_list boolean NOT NULL DEFAULT false,
    locale text NOT NULL,
    viewer_timezone text NOT NULL,
    rsvp_state text NOT NULL,
    rsvp_source text NOT NULL,
    delivery_address_fingerprint bytea NOT NULL,
    delivery_address_ciphertext bytea NOT NULL,
    display_name_ciphertext bytea,
    crypto_key_version smallint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_invitation_recipients_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT calendar_invitation_recipients_revision_id_unique
        UNIQUE (tenant_id, invitation_revision_id, id),
    CONSTRAINT calendar_invitation_recipients_revision_fk
        FOREIGN KEY (tenant_id, class_id, invitation_revision_id)
        REFERENCES tutorhub.calendar_invitation_revisions (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_invitation_recipients_attendee_fk
        FOREIGN KEY (tenant_id, class_id, attendee_id)
        REFERENCES tutorhub.class_session_attendees (tenant_id, class_id, id),
    CONSTRAINT calendar_invitation_recipients_kind_valid
        CHECK (recipient_kind IN ('internal', 'external')),
    CONSTRAINT calendar_invitation_recipients_participation_role_valid
        CHECK (participation_role IN ('required', 'optional')),
    CONSTRAINT calendar_invitation_recipients_business_role_valid CHECK (
        business_role IN (
            'organizer', 'teacher', 'co_teacher',
            'teaching_assistant', 'student', 'external_guest'
        )
        AND (
            (recipient_kind = 'external' AND business_role = 'external_guest')
            OR (recipient_kind = 'internal' AND business_role <> 'external_guest')
        )
    ),
    CONSTRAINT calendar_invitation_recipients_audience_source_valid
        CHECK (audience_source IN ('roster', 'manual')),
    CONSTRAINT calendar_invitation_recipients_locale_valid
        CHECK (locale IN ('vi-VN', 'en-US')),
    CONSTRAINT calendar_invitation_recipients_timezone_valid CHECK (
        viewer_timezone = btrim(viewer_timezone)
        AND length(viewer_timezone) BETWEEN 1 AND 100
        AND lower(viewer_timezone) <> 'local'
    ),
    CONSTRAINT calendar_invitation_recipients_rsvp_valid CHECK (
        rsvp_state IN ('needs_action', 'accepted', 'tentative', 'declined')
        AND rsvp_source IN (
            'none', 'tutorhub_authenticated',
            'tutorhub_external_capability', 'organizer_override'
        )
        AND (
            (rsvp_state = 'needs_action' AND rsvp_source = 'none')
            OR (rsvp_state <> 'needs_action' AND rsvp_source <> 'none')
        )
    ),
    CONSTRAINT calendar_invitation_recipients_identity_protected CHECK (
        octet_length(delivery_address_fingerprint) = 32
        AND octet_length(delivery_address_ciphertext) BETWEEN 32 AND 4096
        AND (
            display_name_ciphertext IS NULL
            OR octet_length(display_name_ciphertext) BETWEEN 32 AND 2048
        )
        AND crypto_key_version > 0
    ),
    CONSTRAINT calendar_invitation_recipients_revision_attendee_unique
        UNIQUE (invitation_revision_id, attendee_id),
    CONSTRAINT calendar_invitation_recipients_revision_address_unique
        UNIQUE (invitation_revision_id, delivery_address_fingerprint)
);

ALTER TABLE tutorhub.class_session_attendees
    ADD CONSTRAINT class_session_attendees_response_revision_fk
        FOREIGN KEY (tenant_id, class_id, response_invitation_revision_id)
        REFERENCES tutorhub.calendar_invitation_revisions (tenant_id, class_id, id);

CREATE FUNCTION tutorhub.reject_calendar_invitation_snapshot_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'calendar invitation snapshots are append-only'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER calendar_invitation_revisions_immutable_rows
    BEFORE UPDATE OR DELETE ON tutorhub.calendar_invitation_revisions
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.reject_calendar_invitation_snapshot_mutation();

CREATE TRIGGER calendar_invitation_revisions_immutable_truncate
    BEFORE TRUNCATE ON tutorhub.calendar_invitation_revisions
    FOR EACH STATEMENT
    EXECUTE FUNCTION tutorhub.reject_calendar_invitation_snapshot_mutation();

CREATE TRIGGER calendar_invitation_recipients_immutable_rows
    BEFORE UPDATE OR DELETE ON tutorhub.calendar_invitation_recipients
    FOR EACH ROW
    EXECUTE FUNCTION tutorhub.reject_calendar_invitation_snapshot_mutation();

CREATE TRIGGER calendar_invitation_recipients_immutable_truncate
    BEFORE TRUNCATE ON tutorhub.calendar_invitation_recipients
    FOR EACH STATEMENT
    EXECUTE FUNCTION tutorhub.reject_calendar_invitation_snapshot_mutation();

ALTER TABLE tutorhub.calendar_invitation_revisions
    ENABLE ALWAYS TRIGGER calendar_invitation_revisions_immutable_rows;
ALTER TABLE tutorhub.calendar_invitation_revisions
    ENABLE ALWAYS TRIGGER calendar_invitation_revisions_immutable_truncate;
ALTER TABLE tutorhub.calendar_invitation_recipients
    ENABLE ALWAYS TRIGGER calendar_invitation_recipients_immutable_rows;
ALTER TABLE tutorhub.calendar_invitation_recipients
    ENABLE ALWAYS TRIGGER calendar_invitation_recipients_immutable_truncate;

CREATE TABLE tutorhub.calendar_rsvp_capabilities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    invitation_revision_id uuid NOT NULL,
    invitation_recipient_id uuid NOT NULL,
    purpose text NOT NULL,
    token_version smallint NOT NULL,
    token_digest bytea NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoked_reason text,
    last_used_at timestamptz,
    use_count integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT calendar_rsvp_capabilities_tenant_id_unique
        UNIQUE (tenant_id, id),
    CONSTRAINT calendar_rsvp_capabilities_recipient_revision_fk
        FOREIGN KEY (tenant_id, invitation_revision_id, invitation_recipient_id)
        REFERENCES tutorhub.calendar_invitation_recipients
            (tenant_id, invitation_revision_id, id),
    CONSTRAINT calendar_rsvp_capabilities_purpose_valid
        CHECK (purpose IN ('resolve', 'respond')),
    CONSTRAINT calendar_rsvp_capabilities_digest_valid CHECK (
        token_version > 0 AND octet_length(token_digest) = 32
    ),
    CONSTRAINT calendar_rsvp_capabilities_expiry_valid CHECK (
        expires_at > issued_at
        AND expires_at <= issued_at + interval '90 days'
    ),
    CONSTRAINT calendar_rsvp_capabilities_revocation_consistent CHECK (
        (revoked_at IS NULL AND revoked_reason IS NULL)
        OR (
            revoked_at IS NOT NULL
            AND revoked_at >= issued_at
            AND revoked_reason = btrim(revoked_reason)
            AND length(revoked_reason) BETWEEN 3 AND 100
            AND revoked_reason ~ '^[a-z][a-z0-9._-]{2,99}$'
        )
    ),
    CONSTRAINT calendar_rsvp_capabilities_use_valid CHECK (
        use_count BETWEEN 0 AND 1000000
        AND (last_used_at IS NULL OR last_used_at >= issued_at)
    )
);

CREATE UNIQUE INDEX calendar_rsvp_capabilities_digest_idx
    ON tutorhub.calendar_rsvp_capabilities (token_version, token_digest);

CREATE UNIQUE INDEX calendar_rsvp_capabilities_active_scope_idx
    ON tutorhub.calendar_rsvp_capabilities
        (tenant_id, invitation_recipient_id, purpose)
    WHERE revoked_at IS NULL;

CREATE INDEX calendar_rsvp_capabilities_expiry_idx
    ON tutorhub.calendar_rsvp_capabilities (expires_at, tenant_id)
    WHERE revoked_at IS NULL;

CREATE TABLE tutorhub.calendar_participation_mutation_receipts (
    tenant_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_fingerprint bytea NOT NULL,
    operation text NOT NULL,
    class_id uuid NOT NULL,
    session_id uuid,
    series_id uuid,
    occurrence_key text,
    actor_type text NOT NULL,
    actor_user_id uuid,
    capability_id uuid,
    result_attendee_id uuid,
    result_invitation_revision_id uuid,
    result_version bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, idempotency_key),
    CONSTRAINT calendar_participation_receipts_session_fk
        FOREIGN KEY (tenant_id, class_id, session_id)
        REFERENCES tutorhub.class_sessions (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_participation_receipts_series_fk
        FOREIGN KEY (tenant_id, class_id, series_id)
        REFERENCES tutorhub.class_session_series (tenant_id, class_id, id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_participation_receipts_actor_membership_fk
        FOREIGN KEY (tenant_id, actor_user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id),
    CONSTRAINT calendar_participation_receipts_capability_fk
        FOREIGN KEY (tenant_id, capability_id)
        REFERENCES tutorhub.calendar_rsvp_capabilities (tenant_id, id),
    CONSTRAINT calendar_participation_receipts_attendee_fk
        FOREIGN KEY (tenant_id, class_id, result_attendee_id)
        REFERENCES tutorhub.class_session_attendees (tenant_id, class_id, id),
    CONSTRAINT calendar_participation_receipts_revision_fk
        FOREIGN KEY (tenant_id, class_id, result_invitation_revision_id)
        REFERENCES tutorhub.calendar_invitation_revisions (tenant_id, class_id, id),
    CONSTRAINT calendar_participation_receipts_key_valid
        CHECK (length(idempotency_key) BETWEEN 16 AND 128),
    CONSTRAINT calendar_participation_receipts_fingerprint_valid
        CHECK (octet_length(request_fingerprint) = 32),
    CONSTRAINT calendar_participation_receipts_operation_valid CHECK (
        operation IN ('audience_replace', 'rsvp_respond', 'organizer_transfer')
    ),
    CONSTRAINT calendar_participation_receipts_source_scope_valid CHECK (
        (
            session_id IS NOT NULL
            AND series_id IS NULL
            AND occurrence_key IS NULL
        )
        OR (
            session_id IS NULL
            AND series_id IS NOT NULL
            AND (
                occurrence_key IS NULL
                OR length(occurrence_key) BETWEEN 8 AND 128
            )
        )
    ),
    CONSTRAINT calendar_participation_receipts_actor_valid CHECK (
        (
            actor_type = 'tutorhub_authenticated'
            AND actor_user_id IS NOT NULL
            AND capability_id IS NULL
        )
        OR (
            actor_type = 'tutorhub_external_capability'
            AND actor_user_id IS NULL
            AND capability_id IS NOT NULL
        )
    ),
    CONSTRAINT calendar_participation_receipts_result_version_positive
        CHECK (result_version > 0)
);

CREATE INDEX calendar_participation_receipts_created_idx
    ON tutorhub.calendar_participation_mutation_receipts
        (tenant_id, created_at);

REVOKE ALL ON tutorhub.calendar_working_schedules FROM PUBLIC;
REVOKE ALL ON tutorhub.calendar_working_schedule_intervals FROM PUBLIC;
REVOKE ALL ON tutorhub.calendar_working_schedule_exceptions FROM PUBLIC;
REVOKE ALL ON tutorhub.calendar_working_schedule_exception_intervals FROM PUBLIC;
REVOKE ALL ON tutorhub.calendar_external_recipients FROM PUBLIC;
REVOKE ALL ON tutorhub.class_session_attendees FROM PUBLIC;
REVOKE ALL ON tutorhub.calendar_invitation_revisions FROM PUBLIC;
REVOKE ALL ON tutorhub.calendar_invitation_recipients FROM PUBLIC;
REVOKE ALL ON tutorhub.calendar_rsvp_capabilities FROM PUBLIC;
REVOKE ALL ON tutorhub.calendar_participation_mutation_receipts FROM PUBLIC;

COMMIT;
