package calendar

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCalendarMigrationContainsProjectionIndexAndPreferenceGuards(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000017_calendar_read_projection.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, fragment := range []string{
		"CREATE INDEX class_sessions_calendar_starts_idx",
		"CREATE TABLE tutorhub.calendar_display_preferences",
		"calendar_display_preferences_membership_fk",
		"calendar_display_preferences_distinct_timezones",
		"time_scale_minutes IN (15, 30, 60)",
		"REVOKE ALL ON tutorhub.calendar_display_preferences FROM PUBLIC",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("calendar migration missing %q", fragment)
		}
	}
}

func TestCalendarRecurrenceMigrationContainsSeriesExceptionAndIdentityGuards(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000018_class_session_recurrence.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, fragment := range []string{
		"CREATE TABLE tutorhub.class_session_series",
		"CREATE TABLE tutorhub.class_session_exceptions",
		"class_session_series_end_valid",
		"class_session_series_weekdays_valid",
		"class_session_series_months_valid",
		"recurrence_count BETWEEN 1 AND 512",
		"class_session_exceptions_action_payload_valid",
		"class_session_exceptions_original_offset_valid",
		"ADD COLUMN occurrence_key text",
		"class_sessions_series_identity_consistent",
		"CREATE UNIQUE INDEX class_sessions_series_occurrence_idx",
		"'class_session_recurrence'",
		"REVOKE ALL ON tutorhub.class_session_series FROM PUBLIC",
		"REVOKE ALL ON tutorhub.class_session_exceptions FROM PUBLIC",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("calendar recurrence migration missing %q", fragment)
		}
	}
}

func TestCalendarRecurrenceMutationMigrationContainsIdentityAndReceiptGuards(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000019_class_session_recurrence_mutations.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, fragment := range []string{
		"ADD COLUMN ical_uid text",
		"class_session_series_tenant_ical_uid_unique",
		"CREATE TABLE tutorhub.class_session_mutation_receipts",
		"class_session_mutation_receipts_series_fk",
		"class_session_mutation_receipts_result_series_fk",
		"class_session_mutation_receipts_key_valid",
		"class_session_mutation_receipts_operation_valid",
		"operation IN ('update', 'cancel')",
		"REVOKE ALL ON tutorhub.class_session_mutation_receipts FROM PUBLIC",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("calendar recurrence mutation migration missing %q", fragment)
		}
	}
}

func TestCalendarAvailabilityParticipationMigrationContainsPrivacyAndBoundGuards(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000020_calendar_availability_participation.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, fragment := range []string{
		"CREATE TABLE tutorhub.calendar_working_schedules",
		"CREATE TABLE tutorhub.calendar_working_schedule_intervals",
		"CREATE TABLE tutorhub.calendar_working_schedule_exceptions",
		"existing_count >= 8",
		"existing_count >= 366",
		"calendar_working_schedule_intervals_no_overlap",
		"ADD COLUMN organizer_user_id uuid",
		"ADD COLUMN audience_revision bigint NOT NULL DEFAULT 0",
		"'urn:uuid:' || id::text",
		"CREATE TABLE tutorhub.calendar_external_recipients",
		"delivery_address_ciphertext bytea NOT NULL",
		"CREATE TABLE tutorhub.class_session_attendees",
		"class_session_attendees_source_scope_valid",
		"class_session_attendees_identity_scope_valid",
		"CREATE INDEX class_session_attendees_external_class_active_idx",
		"existing_count >= 128",
		"CREATE TABLE tutorhub.calendar_invitation_revisions",
		"CREATE TABLE tutorhub.calendar_invitation_recipients",
		"calendar invitation snapshots are append-only",
		"ENABLE ALWAYS TRIGGER calendar_invitation_revisions_immutable_rows",
		"ENABLE ALWAYS TRIGGER calendar_invitation_recipients_immutable_rows",
		"CREATE TABLE tutorhub.calendar_rsvp_capabilities",
		"octet_length(token_digest) = 32",
		"CREATE TABLE tutorhub.calendar_participation_mutation_receipts",
		"octet_length(request_fingerprint) = 32",
		"REVOKE ALL ON tutorhub.calendar_rsvp_capabilities FROM PUBLIC",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("calendar availability/participation migration missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"email text",
		"raw_token",
		"token text",
	} {
		if strings.Contains(strings.ToLower(sql), forbidden) {
			t.Fatalf("calendar availability/participation migration contains forbidden plaintext field %q", forbidden)
		}
	}

	downContents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000020_calendar_availability_participation.down.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	downSQL := string(downContents)
	for _, fragment := range []string{
		"DROP TABLE tutorhub.calendar_participation_mutation_receipts",
		"DROP TABLE tutorhub.calendar_rsvp_capabilities",
		"DROP TABLE tutorhub.calendar_invitation_recipients",
		"DROP TABLE tutorhub.calendar_invitation_revisions",
		"DROP TABLE tutorhub.class_session_attendees",
		"DROP TABLE tutorhub.calendar_external_recipients",
		"DROP TABLE tutorhub.calendar_working_schedules",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("calendar availability/participation down migration missing %q", fragment)
		}
	}
}

func TestCalendarInvitationSnapshotDeliveryBoundaryDefersRecipientSpecificMethod(t *testing.T) {
	t.Parallel()

	upContents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000021_calendar_invitation_snapshot_delivery_boundary.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	upSQL := string(upContents)
	for _, fragment := range []string{
		"DROP CONSTRAINT calendar_invitation_revisions_method_valid",
		"ALTER COLUMN method DROP NOT NULL",
		"calendar_invitation_revisions_delivery_method_deferred",
		"CHECK (method IS NULL) NOT VALID",
		"P3-05A derives REQUEST or CANCEL per recipient",
		"canonical_payload_ciphertext",
		"canonical_payload_sha256",
		"crypto_key_version",
		"calendar_external_recipients_ciphertext_valid",
		"calendar_invitation_recipients_identity_protected",
		"display_name_ciphertext) BETWEEN 29 AND 2048",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("calendar invitation delivery-boundary migration missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"DROP COLUMN canonical_payload_ciphertext",
		"DROP COLUMN canonical_payload_sha256",
		"DROP COLUMN crypto_key_version",
	} {
		if strings.Contains(upSQL, forbidden) {
			t.Fatalf("calendar invitation delivery-boundary migration destroys snapshot data with %q", forbidden)
		}
	}

	downContents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000021_calendar_invitation_snapshot_delivery_boundary.down.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	downSQL := string(downContents)
	for _, fragment := range []string{
		"WHERE method IS NULL",
		"RAISE EXCEPTION",
		"octet_length(display_name_ciphertext) < 32",
		"display_name_ciphertext) BETWEEN 32 AND 2048",
		"ALTER COLUMN method SET NOT NULL",
		"CHECK (method IN ('REQUEST', 'CANCEL'))",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("calendar invitation delivery-boundary down migration missing %q", fragment)
		}
	}
}

func TestAvailabilityPollMigrationContainsNormalizedTenantPrivacyAndQuotaGuards(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000022_availability_polls_study_meetings.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)

	tables := []string{
		"availability_polls",
		"availability_poll_slots",
		"availability_poll_participants",
		"availability_poll_capabilities",
		"availability_poll_responses",
		"availability_poll_answers",
		"availability_poll_mutation_receipts",
		"study_meetings",
	}
	for _, table := range tables {
		definition := regexp.MustCompile(
			`(?s)CREATE TABLE tutorhub\.` + regexp.QuoteMeta(table) + ` \((.*?)\n\);`,
		).FindStringSubmatch(sql)
		if len(definition) != 2 {
			t.Fatalf("availability-poll migration is missing normalized table %q", table)
		}
		if !strings.Contains(definition[1], "tenant_id uuid NOT NULL") {
			t.Fatalf("table %q is missing a required tenant_id", table)
		}
		if !strings.Contains(sql, "REVOKE ALL ON tutorhub."+table+" FROM PUBLIC") {
			t.Fatalf("table %q does not revoke PUBLIC access", table)
		}
	}

	for _, fragment := range []string{
		"'availability_polls'",
		"'active_availability_polls'",
		"limit_value BETWEEN 1 AND 200",
		"'availability_poll_range_days'",
		"limit_value BETWEEN 1 AND 90",
		"'availability_poll_slots'",
		"limit_value BETWEEN 1 AND 1000",
		"'availability_poll_participants'",
		"limit_value BETWEEN 1 AND 500",
		"'availability_poll_creations_per_hour'",
		"'availability_poll_capability_creations_per_hour'",
		"'active_study_meetings'",
		"'study_meeting_creations_per_hour'",
		"availability_polls_class_fk",
		"FOREIGN KEY (tenant_id, class_id)",
		"availability_poll_slots_poll_fk",
		"FOREIGN KEY (tenant_id, poll_id)",
		"retired_at timestamptz",
		"availability_poll_slots_retirement_consistent",
		"availability_poll_slots_poll_ordinal_active_unique",
		"availability_poll_slots_poll_range_active_unique",
		"WHERE retired_at IS NULL",
		"availability_polls_selected_slot_fk",
		"FOREIGN KEY (tenant_id, id, selected_slot_id)",
		"availability_poll_participants_internal_unique",
		"WHERE internal_user_id IS NOT NULL AND status = 'active'",
		"availability_poll_capabilities_parent_fk",
		"FOREIGN KEY (tenant_id, poll_id, parent_capability_id)",
		"availability_poll_answers_response_fk",
		"FOREIGN KEY (tenant_id, poll_id, response_id)",
		"availability_poll_answers_slot_fk",
		"FOREIGN KEY (tenant_id, poll_id, slot_id)",
		"availability_poll_capabilities_digest_valid",
		"octet_length(token_digest) = 32",
		"purpose = 'response_session'",
		"expires_at <= created_at + interval '30 minutes'",
		"scope IN ('invited_participant', 'public_link')",
		"state IN ('preferred', 'available', 'unavailable')",
		"cleared_at timestamptz",
		"availability_poll_answers_clearance_consistent",
		"WHERE cleared_at IS NULL",
		"availability_poll_responses_internal_unique",
		"availability_poll_responses_capability_unique",
		"availability_poll_responses_version_valid",
		"version BETWEEN 1 AND 100",
		"availability_poll_receipts_fingerprint_valid",
		"octet_length(request_fingerprint) = 32",
		"CREATE INDEX availability_polls_retention_idx\n    ON tutorhub.availability_polls (retention_until, tenant_id, id);",
		"study_meetings_source_poll_unique",
		"study_meetings_create_idempotency_unique",
		"CREATE FUNCTION tutorhub.purge_expired_availability_polls(batch_size integer DEFAULT 100)",
		"SECURITY INVOKER",
		"batch_size IS NULL OR batch_size < 1 OR batch_size > 1000",
		"WHERE poll.retention_until <= clock_timestamp()",
		"FOR UPDATE SKIP LOCKED",
		"LIMIT batch_size",
		"SET source_poll_id = NULL",
		"REVOKE ALL ON FUNCTION tutorhub.purge_expired_availability_polls(integer) FROM PUBLIC",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("availability-poll migration is missing %q", fragment)
		}
	}
	if strings.Contains(sql, "poll.status IN ('finalized', 'cancelled')") {
		t.Fatal("retention purge must not leave overdue non-terminal polls indefinitely")
	}
	if strings.Contains(sql, "availability_polls_retention_idx\n    ON tutorhub.availability_polls (retention_until, tenant_id, id)\n    WHERE") {
		t.Fatal("retention index must cover every lifecycle state purged by maintenance")
	}

	lowerSQL := strings.ToLower(sql)
	for _, forbidden := range []string{
		"raw_token",
		"token text",
		"token_ciphertext",
		"email text",
		"available_slots",
		"date_list",
		" json ",
		" jsonb ",
		"constraint availability_poll_slots_poll_ordinal_unique",
		"constraint availability_poll_slots_poll_range_unique",
		"delete from tutorhub.availability_poll_",
	} {
		if strings.Contains(lowerSQL, forbidden) {
			t.Fatalf("availability-poll migration contains forbidden persistence %q", forbidden)
		}
	}
}

func TestAvailabilityPollMaintenanceSecurityCorrectionIsForwardOnly(t *testing.T) {
	t.Parallel()

	upContents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000023_availability_poll_maintenance_security.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	upSQL := string(upContents)
	for _, fragment := range []string{
		"CREATE OR REPLACE FUNCTION tutorhub.purge_expired_availability_polls(batch_size integer DEFAULT 100)",
		"SECURITY DEFINER",
		"SET search_path = pg_catalog, pg_temp",
		"batch_size IS NULL OR batch_size < 1 OR batch_size > 1000",
		"USING ERRCODE = '22023'",
		"pg_catalog.clock_timestamp()",
		"FOR UPDATE SKIP LOCKED",
		"LIMIT batch_size",
		"SET source_poll_id = NULL",
		"DELETE FROM tutorhub.availability_polls",
		"RETURN deleted_count",
		"REVOKE ALL ON FUNCTION tutorhub.purge_expired_availability_polls(integer) FROM PUBLIC",
		"COMMENT ON FUNCTION tutorhub.purge_expired_availability_polls(integer)",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("000023 maintenance security migration is missing %q", fragment)
		}
	}
	if strings.Contains(strings.ToUpper(upSQL), "GRANT UPDATE") ||
		strings.Contains(strings.ToUpper(upSQL), "EXECUTE IMMEDIATE") {
		t.Fatal("000023 must not widen caller table privileges or add dynamic SQL")
	}
	for _, relation := range []string{
		"tutorhub.availability_polls",
		"tutorhub.study_meetings",
	} {
		if !strings.Contains(upSQL, relation) {
			t.Fatalf("000023 function body must schema-qualify %q", relation)
		}
	}

	downContents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000023_availability_poll_maintenance_security.down.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	downSQL := string(downContents)
	for _, fragment := range []string{
		"000023",
		"fails closed",
		"REVOKE ALL ON FUNCTION tutorhub.purge_expired_availability_polls(integer) FROM PUBLIC",
		"DROP FUNCTION IF EXISTS tutorhub.purge_expired_availability_polls(integer)",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("000023 guarded down migration is missing %q", fragment)
		}
	}
	if strings.Contains(downSQL, "CREATE FUNCTION") || strings.Contains(downSQL, "SECURITY INVOKER") {
		t.Fatal("000023 down must disable maintenance instead of restoring the invoker contract")
	}
}

func TestAvailabilityPollDownMigrationGuardsExternalOutcomeAndRestoresCatalog(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000022_availability_polls_study_meetings.down.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	for _, fragment := range []string{
		"DROP FUNCTION IF EXISTS tutorhub.purge_expired_availability_polls(integer)",
		"WHERE outcome_type = 'class_session'",
		"RAISE EXCEPTION",
		"USING ERRCODE = '55000'",
		"DELETE FROM tutorhub.tenant_feature_overrides",
		"DELETE FROM tutorhub.tenant_quota_overrides",
		"DELETE FROM tutorhub.tenant_quota_windows",
		"CHECK (quota_key = 'invite_creations_per_hour')",
		"'class_session_recurrence'",
		"'in_app_notifications'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("availability-poll down migration is missing %q", fragment)
		}
	}

	guardPosition := strings.Index(sql, "WHERE outcome_type = 'class_session'")
	dropPosition := strings.Index(sql, "DROP TABLE tutorhub.availability_polls")
	if guardPosition < 0 || dropPosition < 0 || guardPosition > dropPosition {
		t.Fatal("class-session provenance downgrade guard must execute before poll data is dropped")
	}
}
