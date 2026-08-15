package conversation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestP408DisposableIntegrationCommandRequiresExactConfirmation(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Join("..", "..", "..", "..", "..")
	script, err := os.ReadFile(filepath.Join(
		repositoryRoot,
		"scripts",
		"require-p408-disposable-confirm.mjs",
	))
	if err != nil {
		t.Fatal(err)
	}
	packageJSON, err := os.ReadFile(filepath.Join(repositoryRoot, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"P4_08_DISPOSABLE_CONFIRM",
		"I_UNDERSTAND_P4_08_DISPOSABLE_ONLY",
		"P4_08_SHARED_CONFIRM",
	} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("P4-08 disposable confirmation script is missing %q", required)
		}
	}
	command := string(packageJSON)
	guard := strings.Index(command, "node scripts/require-p408-disposable-confirm.mjs")
	databaseGate := strings.Index(command, "TestPostgresPersistentRoomChatAuthorityIdempotencyAndConcurrencyBarrier")
	if guard < 0 || databaseGate < 0 || guard > databaseGate {
		t.Fatal("P4-08 integration command does not guard the writable database gate")
	}
}

func TestP408SharedHarnessUsesFreshReadOnlyConfirmations(t *testing.T) {
	t.Parallel()

	source := readP408RepositorySource(t, "p408_disposable_preflight_integration_test.go")
	packageJSON := readP408RepositorySource(t, "../../../../../package.json")
	for _, required := range []string{
		"P4_08_SHARED_PREFLIGHT_CONFIRM",
		"I_UNDERSTAND_P4_08_SHARED_PREFLIGHT_READ_ONLY",
		"P4_08_SHARED_FINAL_CONFIRM",
		"I_UNDERSTAND_P4_08_SHARED_FINAL_SNAPSHOT_READ_ONLY",
		"TestPostgresP408SharedOwnerPreflight",
		"TestPostgresP408SharedFinalSnapshot",
		"P4_08_SHARED_FINAL PASS room_conversations=0 enabled_media_overrides=0",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("P4-08 shared harness is missing %q", required)
		}
	}
	for _, required := range []string{
		"test:integration:conversation:p408:shared-preflight",
		"test:integration:conversation:p408:shared-postflight",
	} {
		if !strings.Contains(packageJSON, required) {
			t.Fatalf("P4-08 package scripts are missing %q", required)
		}
	}
}

func TestP408RoomConversationProjectionUsesCanonicalSourcesAndLiveParticipantState(t *testing.T) {
	t.Parallel()

	source := readP408RepositorySource(t, "postgres_repository.go")
	for _, required := range []string{
		"case KindRoom:",
		"policy.ActionClassView",
		"policy.ActionChatSend",
		`row.MediaMemberState.String == "active"`,
		`mediaSpaceStatus == "open"`,
		"row.MediaParticipantLive",
		"room_participant.status IN (",
		"'admitted', 'joining', 'connected', 'reconnecting'",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("P4-08 room projection is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"provider_participant_identity",
		"provider_room_name",
		"provider_room_sid",
	} {
		if strings.Contains(conversationSelect, forbidden) ||
			strings.Contains(conversationGetSelect, forbidden) {
			t.Fatalf("P4-08 projection exposes provider identity field %q", forbidden)
		}
	}
}

func TestP408MessageWriteLocksAuthoritativeRoomStateBeforeConversation(t *testing.T) {
	t.Parallel()

	repositorySource := readP408RepositorySource(t, "postgres_repository.go")
	messageSource := readP408RepositorySource(t, "postgres_message_repository.go")
	for _, required := range []string{
		"FeatureClassroomMediaRooms",
		"lockRoomConversationWriteAccess",
	} {
		if !strings.Contains(messageSource, required) {
			t.Fatalf("P4-08 message write boundary is missing %q", required)
		}
	}

	lockStart := strings.Index(repositorySource, "func (repository *PostgresRepository) lockRoomConversationWriteAccess")
	if lockStart < 0 {
		t.Fatal("P4-08 room write lock helper is missing")
	}
	lockSource := repositorySource[lockStart:]
	space := strings.Index(lockSource, "FROM tutorhub.media_spaces")
	instance := strings.Index(lockSource, "FROM tutorhub.media_room_instances")
	participant := strings.Index(lockSource, "FROM tutorhub.media_participant_sessions")
	if space < 0 || instance < 0 || participant < 0 || !(space < instance && instance < participant) {
		t.Fatal("P4-08 must preserve MediaSpace -> RoomInstance -> ParticipantSession lock order")
	}
	for _, required := range []string{
		"FOR SHARE",
		`space.Status != "open"`,
		"ErrReadOnly",
		"'admitted', 'joining', 'connected', 'reconnecting'",
	} {
		if !strings.Contains(lockSource, required) {
			t.Fatalf("P4-08 room write lock is missing %q", required)
		}
	}

	roomCase := strings.Index(messageSource, "case KindRoom:")
	conversationLock := strings.Index(messageSource, "FROM tutorhub.conversations\nWHERE tenant_id = $1 AND id = $2\nFOR UPDATE")
	if roomCase < 0 || conversationLock < 0 || roomCase > conversationLock {
		t.Fatal("P4-08 source authorization must precede the conversation mutation lock")
	}
}

func TestP408MessageRepositoryDoesNotAppendMessageContentToSideEffects(t *testing.T) {
	t.Parallel()

	source := readP408RepositorySource(t, "postgres_message_repository.go")
	for _, forbidden := range []string{
		"audit.Append",
		"audit.Metadata",
		"outbox.Append",
		"outbox.Metadata",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("P4-08 message content path must not emit side-effect payload via %q", forbidden)
		}
	}
}

func readP408RepositorySource(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}
