package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestP409RecoveryMigrationIsForwardExactAndRollbackGuarded(t *testing.T) {
	t.Parallel()

	up := readP409File(t, "..", "..", "..", "migrations", "000035_media_room_recovery.up.sql")
	down := readP409File(t, "..", "..", "..", "migrations", "000035_media_room_recovery.down.sql")
	for _, required := range []string{
		"DROP CONSTRAINT media_space_mutation_receipts_operation_valid",
		"'recover'",
		"operation NOT IN ('start', 'recover') OR result_room_instance_id IS NOT NULL",
		"COMMIT;",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("000035 up missing reviewed invariant %q", required)
		}
	}
	for _, forbidden := range []string{"GRANT ", "CREATE ROLE", "ALTER ROLE", "provider_room_sid"} {
		if strings.Contains(strings.ToUpper(up), strings.ToUpper(forbidden)) {
			t.Fatalf("000035 up unexpectedly expands privilege/provider scope with %q", forbidden)
		}
	}
	if !strings.Contains(down, "WHERE operation = 'recover'") ||
		!strings.Contains(down, "RAISE EXCEPTION") {
		t.Fatal("000035 down must fail closed while recovery receipts exist")
	}
}

func TestP409RepositoryLocksExactFailedAttemptBeforeSuccessorInsert(t *testing.T) {
	t.Parallel()

	source := readP409File(t, "postgres_lifecycle_repository.go")
	recovery := p409Section(
		t,
		source,
		"func (repository *PostgresLifecycleRepository) recoverSpace(",
		"func (repository *PostgresLifecycleRepository) reauthorizeRecoveryReplay(",
	)
	ordered := []string{
		"row.Status != SpaceStatusOpen || row.Locked",
		"RequireFeature(",
		"authorizeSource(",
		"loadLatestRoom(",
		"previous.ID != command.ExpectedRoomInstanceID",
		"previous.Status != RoomInstanceFailed",
		"INSERT INTO tutorhub.media_room_instances",
		"attempt_number",
		"UPDATE tutorhub.media_spaces",
		"media_space.recovered.v1",
	}
	position := -1
	for _, fragment := range ordered {
		next := strings.Index(recovery[position+1:], fragment)
		if next < 0 {
			t.Fatalf("recovery repository missing ordered barrier %q", fragment)
		}
		position += next + 1
	}
	if !strings.Contains(source, "WHERE tenant_id = $1 AND space_id = $2") ||
		!strings.Contains(source, "ORDER BY attempt_number DESC") ||
		!strings.Contains(source, "FOR UPDATE") {
		t.Fatal("latest recovery instance lookup must stay tenant-scoped and lockable")
	}
}

func TestP409ProviderFinishFailsParticipantsWithoutEndingBusinessSpace(t *testing.T) {
	t.Parallel()

	instanceSource := readP409File(t, "postgres_instance_repository.go")
	finished := p409Section(
		t,
		instanceSource,
		`case "room_finished":`,
		`case "participant_joined", "participant_left":`,
	)
	if strings.Count(finished, "SET status = 'failed'") != 2 ||
		strings.Count(finished, `"provider_room_finished"`) != 2 ||
		strings.Count(finished, "return failRoomParticipants(") != 2 {
		t.Fatal("both active and provisioning provider-finish paths must fail exact instance participants")
	}
	lifecycle := readP409File(t, "postgres_lifecycle_repository.go")
	failure := p409Section(t, lifecycle, "func failRoomParticipants(", "func insertTransitionReceipt(")
	for _, required := range []string{
		"status = 'failed'",
		"capacity_reserved = false",
		"failure_code = $5",
		"status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')",
		"advanceMediaRosterProjection(",
	} {
		if !strings.Contains(failure, required) {
			t.Fatalf("participant failure cascade missing %q", required)
		}
	}
}

func readP409File(t *testing.T, parts ...string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func p409Section(t *testing.T, source, start, end string) string {
	t.Helper()
	startAt := strings.Index(source, start)
	endAt := strings.Index(source, end)
	if startAt < 0 || endAt <= startAt {
		t.Fatalf("cannot isolate reviewed source section %q -> %q", start, end)
	}
	return source[startAt:endAt]
}
