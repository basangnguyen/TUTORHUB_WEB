package media

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestClassroomMediaMigrationContainsTenantLifecycleAndConcurrencyGuards(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000029_classroom_media_spaces.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)

	for _, table := range []string{
		"media_spaces",
		"media_room_instances",
		"media_space_members",
		"media_admission_requests",
		"media_participant_sessions",
		"media_space_mutation_receipts",
	} {
		definition := regexp.MustCompile(
			`(?s)CREATE TABLE tutorhub\.` + regexp.QuoteMeta(table) + ` \((.*?)\n\);`,
		).FindStringSubmatch(sql)
		if len(definition) != 2 {
			t.Fatalf("classroom media migration is missing table %q", table)
		}
		if !strings.Contains(definition[1], "tenant_id uuid NOT NULL") {
			t.Fatalf("classroom media table %q is missing required tenant_id", table)
		}
		if !strings.Contains(sql, "REVOKE ALL ON tutorhub."+table+" FROM PUBLIC") {
			t.Fatalf("classroom media table %q does not revoke PUBLIC access", table)
		}
	}

	for _, fragment := range []string{
		"'classroom_media_rooms'",
		"'instant_study_rooms'",
		"'active_media_spaces'",
		"limit_value BETWEEN 1 AND 100",
		"'media_participants_per_space'",
		"limit_value BETWEEN 1 AND 50",
		"'active_media_participants'",
		"limit_value BETWEEN 1 AND 500",
		"'media_space_starts_per_hour'",
		"limit_value BETWEEN 1 AND 200",
		"media_spaces_source_union_valid",
		"source_kind IN ('class_session', 'class_session_occurrence', 'study_meeting')",
		"media_spaces_class_session_fk",
		"FOREIGN KEY (tenant_id, class_id, source_class_session_id)",
		"media_spaces_series_fk",
		"FOREIGN KEY (tenant_id, class_id, source_series_id)",
		"media_spaces_study_meeting_fk",
		"FOREIGN KEY (tenant_id, source_study_meeting_id)",
		"media_spaces_class_session_source_unique",
		"media_spaces_occurrence_source_unique",
		"media_spaces_study_meeting_source_unique",
		"status IN ('scheduled', 'open', 'ended', 'cancelled')",
		"media_room_instances_one_active_unique",
		"WHERE status IN ('provisioning', 'active', 'closing')",
		"status IN ('provisioning', 'active', 'closing', 'ended', 'failed')",
		"media_admission_requests_one_waiting_unique",
		"media_participant_sessions_one_active_user_unique",
		"operation IN ('start', 'end', 'cancel')",
		"PRIMARY KEY (tenant_id, actor_user_id, idempotency_key)",
		"octet_length(request_fingerprint) = 32",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("classroom media migration is missing %q", fragment)
		}
	}

	upperSQL := strings.ToUpper(sql)
	if strings.Contains(upperSQL, "GRANT ") {
		t.Fatal("classroom media migration must not hardcode an environment runtime role grant")
	}
	for _, forbidden := range []string{
		"raw_token",
		"token text",
		"api_secret",
		"sdp",
		"ice_candidate",
		"email text",
		" json ",
		" jsonb ",
	} {
		if strings.Contains(strings.ToLower(sql), forbidden) {
			t.Fatalf("classroom media migration contains forbidden persistence %q", forbidden)
		}
	}
}

func TestClassroomMediaDownMigrationDropsChildrenAndRestoresCatalog(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000029_classroom_media_spaces.down.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)

	orderedDrops := []string{
		"DROP TABLE tutorhub.media_space_mutation_receipts",
		"DROP TABLE tutorhub.media_participant_sessions",
		"DROP TABLE tutorhub.media_admission_requests",
		"DROP TABLE tutorhub.media_space_members",
		"DROP TABLE tutorhub.media_room_instances",
		"DROP TABLE tutorhub.media_spaces",
	}
	lastPosition := -1
	for _, fragment := range orderedDrops {
		position := strings.Index(sql, fragment)
		if position < 0 {
			t.Fatalf("classroom media down migration is missing %q", fragment)
		}
		if position <= lastPosition {
			t.Fatalf("classroom media down migration drops %q out of dependency order", fragment)
		}
		lastPosition = position
	}

	for _, fragment := range []string{
		"DELETE FROM tutorhub.tenant_feature_overrides",
		"WHERE feature_key IN ('classroom_media_rooms', 'instant_study_rooms')",
		"DELETE FROM tutorhub.tenant_quota_windows",
		"WHERE quota_key = 'media_space_starts_per_hour'",
		"DELETE FROM tutorhub.tenant_quota_overrides",
		"'file_uploads'",
		"'file_upload_intents_per_hour'",
		"'files_per_tenant'",
		"'message_sends_per_hour'",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("classroom media down migration is missing %q", fragment)
		}
	}

	featureRestore := sql[strings.LastIndex(sql, "ADD CONSTRAINT tenant_feature_overrides_key_valid"):]
	for _, removed := range []string{
		"'classroom_media_rooms'",
		"'instant_study_rooms'",
		"'active_media_spaces'",
		"'media_participants_per_space'",
		"'active_media_participants'",
		"'media_space_starts_per_hour'",
	} {
		if strings.Contains(featureRestore, removed) {
			t.Fatalf("restored pre-000029 feature catalog still contains %s", removed)
		}
	}
}

func TestRoomInstanceLiveKitBindingMigrationContainsExactReceiptAndPrivacyGuards(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000030_room_instance_livekit_binding.up.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	definition := regexp.MustCompile(
		`(?s)CREATE TABLE tutorhub\.media_provider_webhook_receipts \((.*?)\n\);`,
	).FindStringSubmatch(sql)
	if len(definition) != 2 {
		t.Fatal("room-instance binding migration is missing the provider webhook receipt table")
	}

	for _, fragment := range []string{
		"UNIQUE (tenant_id, space_id, room_instance_id, id)",
		"participant_session_id uuid,",
		"PRIMARY KEY (provider_kind, event_id)",
		"FOREIGN KEY (tenant_id, space_id, room_instance_id)",
		"REFERENCES tutorhub.media_room_instances (tenant_id, space_id, id)",
		"media_provider_webhook_receipts_participant_fk",
		"room_instance_id,\n            participant_session_id",
		"REFERENCES tutorhub.media_participant_sessions",
		"provider_kind = 'livekit'",
		"event_id ~ '^[A-Za-z0-9_-]{1,128}$'",
		"event_type ~ '^[a-z][a-z0-9_]{0,63}$'",
		"'applied'",
		"'ignored_stale'",
		"'ignored_mismatch'",
		"'ignored_terminal'",
		"'ignored_unknown_participant'",
		"'ignored_unsupported_event'",
		"retention_until > received_at",
		"retention_until <= received_at + interval '30 days'",
		"media_provider_webhook_receipts_retention_idx",
		"REVOKE ALL ON tutorhub.media_provider_webhook_receipts FROM PUBLIC",
		"Environment-specific Core API column grants are provisioned separately",
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("room-instance binding migration is missing %q", fragment)
		}
	}

	if strings.Contains(definition[1], "participant_session_id uuid NOT NULL") {
		t.Fatal("provider webhook receipt participant binding must remain nullable")
	}
	upperSQL := strings.ToUpper(sql)
	if strings.Contains(upperSQL, "GRANT ") {
		t.Fatal("room-instance binding migration must not hardcode an environment runtime role grant")
	}
	for _, forbidden := range []string{
		"provider_room_name",
		"room_name",
		"provider_participant_identity",
		"participant_identity",
		"raw_payload",
		"payload json",
		"payload jsonb",
		"token text",
		"api_secret",
		"sdp",
		"ice_candidate",
		"email text",
	} {
		if strings.Contains(strings.ToLower(definition[1]), forbidden) {
			t.Fatalf("provider webhook receipt persists forbidden field %q", forbidden)
		}
	}
}

func TestRoomInstanceLiveKitBindingDownMigrationDropsOnlyForwardAdditions(t *testing.T) {
	t.Parallel()

	contents, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "migrations", "000030_room_instance_livekit_binding.down.sql",
	))
	if err != nil {
		t.Fatal(err)
	}
	sql := string(contents)
	tableDrop := strings.Index(sql, "DROP TABLE tutorhub.media_provider_webhook_receipts")
	constraintDrop := strings.Index(
		sql,
		"DROP CONSTRAINT media_participant_sessions_instance_id_unique",
	)
	if tableDrop < 0 || constraintDrop < 0 || tableDrop >= constraintDrop {
		t.Fatal("room-instance binding down migration must drop the receipt before its participant key")
	}
	for _, forbidden := range []string{
		"DROP TABLE tutorhub.media_participant_sessions",
		"DROP TABLE tutorhub.media_room_instances",
		"DROP TABLE tutorhub.media_spaces",
		"DROP COLUMN",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("room-instance binding down migration removes pre-000030 state via %q", forbidden)
		}
	}
}
