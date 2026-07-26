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
