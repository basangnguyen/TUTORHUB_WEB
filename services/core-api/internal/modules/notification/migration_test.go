package notification

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationMigrationKeepsProjectionAndPreferenceContracts(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "..", "migrations", "000016_notifications.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read notification migration: %v", err)
	}
	sql := string(contents)
	required := []string{
		"CREATE TABLE tutorhub.notifications",
		"CREATE TABLE tutorhub.notification_preferences",
		"notifications_source_recipient_effect_unique",
		"source_outbox_event_id, recipient_user_id, effect_key",
		"kind <> 'system.worker_canary'",
		"notification_preferences_quiet_hours_valid",
		`@.type() != "string"`,
		"'in_app_notifications'",
		"REVOKE ALL ON tutorhub.notifications FROM PUBLIC",
		"REVOKE ALL ON tutorhub.notification_preferences FROM PUBLIC",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("migration is missing %q", fragment)
		}
	}
}
