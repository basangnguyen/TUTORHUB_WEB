package media

import (
	"os"
	"strings"
	"testing"
)

func TestP404LobbyMutationUsesReviewedRowLockOrder(t *testing.T) {
	t.Parallel()

	source := p404RepositorySource(t, "postgres_lobby_repository.go")
	moderatorLoad := p404SourceSection(
		t,
		source,
		"func (repository *PostgresLobbyRepository) loadModeratorLobby(",
		"func (repository *PostgresLobbyRepository) loadSelfLobby(",
	)
	p404RequireOrderedFragments(t, moderatorLoad, []string{
		"repository.lifecycle.acquireTenantControlLock(",
		"repository.lifecycle.requireActiveScope(",
		"space, err := loadSpace(",
		"repository.lifecycle.authorizeSource(",
		"room, err := loadLobbyRoom(",
	})

	mutation := p404SourceSection(
		t,
		source,
		"func (repository *PostgresLobbyRepository) MutateAdmission(",
		"func (repository *PostgresLobbyRepository) CancelJoinAttempt(",
	)
	p404RequireOrderedFragments(t, mutation, []string{
		"repository.loadModeratorLobby(",
		"loadLobbyMutationReceipt(",
		"loadLobbyAdmissionForMutation(",
	})

	admissionLock := p404SourceSection(
		t,
		source,
		"func loadLobbyAdmissionForMutation(",
		"func loadLobbyParticipantByAdmission(",
	)
	p404RequireOrderedFragments(t, admissionLock, []string{
		"FOR UPDATE OF admission",
		"loadLobbyParticipantByAdmission(",
	})
	participantLock := p404SourceSection(
		t,
		source,
		"func loadLobbyParticipantByAdmission(",
		"func loadLobbyAttemptByJoinID(",
	)
	if !strings.Contains(participantLock, `query += " FOR UPDATE"`) {
		t.Fatal("P4-04 admission mutation does not lock ParticipantSession after AdmissionRequest")
	}

	selfCancellation := p404SourceSection(
		t,
		source,
		"func loadLobbyAttemptByJoinID(",
		"func loadLobbyAdmissionProjection(",
	)
	if !strings.Contains(selfCancellation, `query += " FOR UPDATE OF admission, participant"`) {
		t.Fatal("P4-04 self-cancel does not preserve AdmissionRequest before ParticipantSession lock order")
	}
}

func TestP404CredentialReturnsGrantOnlyAfterDurableAdmission(t *testing.T) {
	t.Parallel()

	source := p404RepositorySource(t, "postgres_instance_repository.go")
	credential := p404SourceSection(
		t,
		source,
		"func (repository *PostgresInstanceRepository) PrepareCredential(",
		"func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
	)
	p404RequireOrderedFragments(t, credential, []string{
		"existing.Status == string(JoinAttemptWaiting)",
		"requireAdmittedAdmission(",
		"consumeMediaCredentialRateLimit(",
		"transaction.Commit(queryContext)",
		"return participantGrant(",
	})
	if strings.Contains(credential, "issuer.Issue(") || strings.Contains(credential, "LiveKitToken") {
		t.Fatal("P4-04 credential transaction performs a provider mint before durable commit")
	}
}

func TestP404DeniedParticipantRequiresExplicitRestoreBeforeNewAttempt(t *testing.T) {
	t.Parallel()

	source := p404RepositorySource(t, "postgres_instance_repository.go")
	attempt := p404SourceSection(
		t,
		source,
		"func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
		"func (repository *PostgresInstanceRepository) requireParticipantCapacity(",
	)
	p404RequireOrderedFragments(t, attempt, []string{
		"hasActiveParticipant(",
		"hasUnrestoredRemovalBarrier(",
		"media-participant-capacity:",
		"INSERT INTO tutorhub.media_admission_requests",
		"INSERT INTO tutorhub.media_participant_sessions",
	})

	barrier := p404SourceSection(
		t,
		source,
		"func hasUnrestoredRemovalBarrier(",
		"func activeParticipantStatus(",
	)
	for _, required := range []string{
		"tenant_id = $1", "space_id = $2", "room_instance_id = $3",
		"user_id = $4", "status = 'removed'", "rejoin_restored_at IS NULL",
	} {
		if !strings.Contains(barrier, required) {
			t.Fatalf("P4-04 explicit restore barrier is missing %q", required)
		}
	}
}

func TestP404InviteIsExplicitActiveSameTenantStudyMeetingOnly(t *testing.T) {
	t.Parallel()

	source := p404RepositorySource(t, "postgres_lobby_repository.go")
	invite := p404SourceSection(
		t,
		source,
		"func (repository *PostgresLobbyRepository) InviteMember(",
		"func (repository *PostgresLobbyRepository) MutateMember(",
	)
	p404RequireOrderedFragments(t, invite, []string{
		"repository.loadStudyMeetingLobby(",
		"FROM tutorhub.memberships AS member",
		"member.tenant_id = $1 AND target.email = $2",
		"member.status = 'active' AND target.status = 'active'",
		"INSERT INTO tutorhub.media_space_members",
		"appendLobbyMemberEvent(",
	})
	for _, forbidden := range []string{"target_email", "member_email"} {
		if strings.Contains(strings.ToLower(invite), forbidden) {
			t.Fatalf("P4-04 invite persists raw email through forbidden shape %q", forbidden)
		}
	}
	memberInsert := p404SourceSection(
		t,
		invite,
		"`INSERT INTO tutorhub.media_space_members",
		"); err != nil {",
	)
	if strings.Contains(memberInsert, "TargetEmail") || strings.Contains(strings.ToLower(memberInsert), "email") {
		t.Fatal("P4-04 explicit member insert persists the lookup-only email")
	}

	studyMeetingGate := p404SourceSection(
		t,
		source,
		"func (repository *PostgresLobbyRepository) loadStudyMeetingLobby(",
		"func loadLobbyRoom(",
	)
	for _, required := range []string{
		"source.Kind != SourceStudyMeeting",
		"(!source.Owner && !source.SafetyAdmin)",
		`case "invite", "member_restore":`,
		"if !source.Owner",
	} {
		if !strings.Contains(studyMeetingGate, required) {
			t.Fatalf("P4-04 same-tenant invite gate is missing %q", required)
		}
	}
}

func TestP404LateAdmissionAndRoomTerminationCannotRetainCapacity(t *testing.T) {
	t.Parallel()

	lobbySource := p404RepositorySource(t, "postgres_lobby_repository.go")
	selfPoll := p404SourceSection(
		t,
		lobbySource,
		"func (repository *PostgresLobbyRepository) GetJoinAttempt(",
		"func (repository *PostgresLobbyRepository) MutateAdmission(",
	)
	p404RequireOrderedFragments(t, selfPoll, []string{
		"repository.loadSelfLobby(",
		"input.ExpectedRoomInstanceID, input.ExpectedRoomInstanceVersion, true, true",
		"loadLobbyAttemptByJoinID(",
		"scope.ActorID, joinAttemptID, true",
		"expireLobbyAttempt(",
		"transaction.Commit(queryContext)",
	})
	selfCancellation := p404SourceSection(
		t,
		lobbySource,
		"func (repository *PostgresLobbyRepository) CancelJoinAttempt(",
		"func (repository *PostgresLobbyRepository) ListMembers(",
	)
	if !strings.Contains(
		selfCancellation,
		"command.ExpectedRoomInstanceID, command.ExpectedRoomInstanceVersion, true, false",
	) {
		t.Fatal("P4-04 self-cancel accidentally accepts the terminal read-only version relaxation")
	}
	selfLoad := p404SourceSection(
		t,
		lobbySource,
		"func (repository *PostgresLobbyRepository) loadSelfLobby(",
		"func (repository *PostgresLobbyRepository) loadStudyMeetingLobby(",
	)
	for _, required := range []string{
		"allowTerminalRead bool",
		"space.Status == SpaceStatusEnded",
		"space.Status == SpaceStatusCancelled",
		"room.Status == RoomInstanceClosing",
		"room.Status == RoomInstanceEnded",
		"room.Status == RoomInstanceFailed",
		"(!allowTerminalRead || !terminal)",
	} {
		if !strings.Contains(selfLoad, required) {
			t.Fatalf("P4-04 terminal self-poll gate is missing %q", required)
		}
	}

	mutation := p404SourceSection(
		t,
		lobbySource,
		"func (repository *PostgresLobbyRepository) MutateAdmission(",
		"func (repository *PostgresLobbyRepository) CancelJoinAttempt(",
	)
	p404RequireOrderedFragments(t, mutation, []string{
		"loadLobbyAdmissionForMutation(",
		"!command.OccurredAt.Before(admission.CreatedAt.Add(command.AdmissionTTL))",
		"switch command.Operation",
	})

	timeout := p404SourceSection(
		t,
		lobbySource,
		"func expireLobbyAttempt(",
		"func expireOutstandingLobbyAdmissions(",
	)
	p404RequireOrderedFragments(t, timeout, []string{
		"UPDATE tutorhub.media_admission_requests",
		"status = 'expired'",
		"UPDATE tutorhub.media_participant_sessions",
		"capacity_reserved = false",
		"appendExpiredLobbyAdmissionEvent(",
	})

	lifecycleSource := p404RepositorySource(t, "postgres_lifecycle_repository.go")
	endSpace := p404SourceSection(
		t,
		lifecycleSource,
		"func (repository *PostgresLifecycleRepository) endSpace(",
		"func (repository *PostgresLifecycleRepository) cancelSpace(",
	)
	p404RequireOrderedFragments(t, endSpace, []string{
		"loadRoomForTermination(",
		"expireOutstandingLobbyAdmissions(",
		"terminateRoomParticipants(",
	})

	providerSource := p404RepositorySource(t, "postgres_instance_repository.go")
	providerClassifier := p404SourceSection(
		t,
		providerSource,
		"func classifyProviderWebhook(",
		"func classifyParticipantWebhook(",
	)
	if got := strings.Count(providerClassifier, "expireOutstandingLobbyAdmissions("); got != 2 {
		t.Fatalf("P4-04 provider terminalization expiry count = %d, want 2 reviewed room-finished paths", got)
	}
	remaining := providerClassifier
	for path := 0; path < 2; path++ {
		expiry := strings.Index(remaining, "expireOutstandingLobbyAdmissions(")
		termination := strings.Index(remaining, "terminateRoomParticipants(")
		if expiry < 0 || termination < expiry {
			t.Fatalf("P4-04 provider terminalization path %d releases participants before expiring admissions", path+1)
		}
		remaining = remaining[termination+len("terminateRoomParticipants("):]
	}
}

func TestP404LobbyEventPayloadsExcludePrivateJoinData(t *testing.T) {
	t.Parallel()

	source := p404RepositorySource(t, "postgres_lobby_repository.go")
	expiredEvent := p404SourceSection(
		t,
		source,
		"func appendExpiredLobbyAdmissionEvent(",
		"func appendLobbyAdmissionEvent(",
	)
	admissionEvent := p404SourceSection(
		t,
		source,
		"func appendLobbyAdmissionEvent(",
		"func appendLobbyMemberEvent(",
	)
	memberEvent := p404SourceSection(
		t,
		source,
		"func appendLobbyMemberEvent(",
		"\n}",
	)
	for name, section := range map[string]string{
		"admission": admissionEvent,
		"expired":   expiredEvent,
		"member":    memberEvent,
	} {
		for _, required := range []string{
			"'actor_user_id'", "'target_user_id'", "'status'", "'version'",
		} {
			if !strings.Contains(section, required) {
				t.Fatalf("P4-04 %s event is missing reviewed field %q", name, required)
			}
		}
		lower := strings.ToLower(section)
		for _, forbidden := range []string{
			"email", "display_name", "provider", "participant_session", "session_id",
			"join_attempt", "token",
		} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("P4-04 %s event contains private field class %q", name, forbidden)
			}
		}
	}
}

func TestP404RestoreEventDoesNotInventAnotherAdmissionState(t *testing.T) {
	t.Parallel()

	restore := p404SourceSection(
		t,
		p404RepositorySource(t, "postgres_lobby_repository.go"),
		"func restoreLobbyAdmission(",
		"func projectLobbyAdmission(",
	)
	p404RequireOrderedFragments(t, restore, []string{
		`admission.Status != "denied"`,
		"SET status = 'cancelled'",
		"resolution_code = 'restored'",
		`return "media_admission.restored.v1", nil`,
	})
	if strings.Contains(restore, "SET status = 'restored'") {
		t.Fatal("P4-04 restore invented a database admission state outside the migration vocabulary")
	}
}

func p404RepositorySource(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func p404SourceSection(t *testing.T, source string, start string, end string) string {
	t.Helper()
	startPosition := strings.Index(source, start)
	if startPosition < 0 {
		t.Fatalf("P4-04 source is missing section start %q", start)
	}
	remainder := source[startPosition+len(start):]
	endPosition := strings.Index(remainder, end)
	if endPosition < 0 {
		t.Fatalf("P4-04 source is missing section end %q after %q", end, start)
	}
	return source[startPosition : startPosition+len(start)+endPosition]
}

func p404RequireOrderedFragments(t *testing.T, source string, fragments []string) {
	t.Helper()
	lastPosition := -1
	for _, fragment := range fragments {
		position := strings.Index(source, fragment)
		if position < 0 {
			t.Fatalf("P4-04 security boundary is missing %q", fragment)
		}
		if position <= lastPosition {
			t.Fatalf("P4-04 security boundary %q is out of order", fragment)
		}
		lastPosition = position
	}
}
