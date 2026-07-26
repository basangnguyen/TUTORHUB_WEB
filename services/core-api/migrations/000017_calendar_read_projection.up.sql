BEGIN;

CREATE INDEX class_sessions_calendar_starts_idx
    ON tutorhub.class_sessions (tenant_id, starts_at, id)
    INCLUDE (ends_at, class_id, status, timezone, version);

CREATE TABLE tutorhub.calendar_display_preferences (
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    viewer_timezone text NOT NULL,
    locale text NOT NULL DEFAULT 'vi-VN',
    time_format text NOT NULL DEFAULT '24h',
    week_start text NOT NULL DEFAULT 'monday',
    default_view text NOT NULL DEFAULT 'week',
    density text NOT NULL DEFAULT 'comfortable',
    time_scale_minutes integer NOT NULL DEFAULT 30,
    secondary_timezone text,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id),
    CONSTRAINT calendar_display_preferences_membership_fk
        FOREIGN KEY (tenant_id, user_id)
        REFERENCES tutorhub.memberships (tenant_id, user_id)
        ON DELETE CASCADE,
    CONSTRAINT calendar_display_preferences_viewer_timezone_valid CHECK (
        viewer_timezone = btrim(viewer_timezone)
        AND length(viewer_timezone) BETWEEN 1 AND 100
        AND lower(viewer_timezone) <> 'local'
    ),
    CONSTRAINT calendar_display_preferences_locale_valid
        CHECK (locale IN ('vi-VN', 'en-US')),
    CONSTRAINT calendar_display_preferences_time_format_valid
        CHECK (time_format IN ('12h', '24h')),
    CONSTRAINT calendar_display_preferences_week_start_valid
        CHECK (week_start IN ('monday', 'sunday')),
    CONSTRAINT calendar_display_preferences_default_view_valid
        CHECK (default_view IN ('day', 'work_week', 'week', 'month', 'agenda')),
    CONSTRAINT calendar_display_preferences_density_valid
        CHECK (density IN ('comfortable', 'compact')),
    CONSTRAINT calendar_display_preferences_time_scale_valid
        CHECK (time_scale_minutes IN (15, 30, 60)),
    CONSTRAINT calendar_display_preferences_secondary_timezone_valid CHECK (
        secondary_timezone IS NULL
        OR (
            secondary_timezone = btrim(secondary_timezone)
            AND length(secondary_timezone) BETWEEN 1 AND 100
            AND lower(secondary_timezone) <> 'local'
        )
    ),
    CONSTRAINT calendar_display_preferences_distinct_timezones CHECK (
        secondary_timezone IS NULL OR secondary_timezone <> viewer_timezone
    ),
    CONSTRAINT calendar_display_preferences_version_positive CHECK (version > 0),
    CONSTRAINT calendar_display_preferences_updated_after_created
        CHECK (updated_at >= created_at)
);

REVOKE ALL ON tutorhub.calendar_display_preferences FROM PUBLIC;

COMMIT;
