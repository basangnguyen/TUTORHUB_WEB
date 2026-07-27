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
