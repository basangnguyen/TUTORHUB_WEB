package calendar

import (
	"os"
	"path/filepath"
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
