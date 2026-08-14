package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestP406SignalReplayPrecedesVersionAndRateChecks(t *testing.T) {
	t.Parallel()

	source := p406RepositorySource(t, "postgres_signal_repository.go")
	send := p406SourceSection(
		t, source,
		"func (repository *PostgresMediaSignalRepository) SendSignal(",
		"func (repository *PostgresMediaSignalRepository) loadSignalAuthority(",
	)
	p406RequireOrderedFragments(t, send, []string{
		"repository.loadSignalAuthority(",
		"command.ExpectedRoomInstanceVersion, true, false",
		"loadMediaSignalReplay(",
		"if replayed {",
		"bytes.Equal(replayFingerprint, command.Fingerprint)",
		"transaction.Commit(queryContext)",
		"authority.space.Version != command.ExpectedSpaceVersion",
		"authority.room.Version != command.ExpectedRoomInstanceVersion",
		"authority.room.ProjectionVersion != command.ExpectedProjectionVersion",
		"consumeMediaSignalLimits(",
		"repository.applySignal(",
		"advanceSignalRoomProjection(",
		"repository.persistSignalChange(",
		"insertMediaSignalReceipt(",
	})
}

func TestP406SignalAuthorityIsTenantBoundAndKeepsLockedRoomParticipantsActive(t *testing.T) {
	t.Parallel()

	source := p406RepositorySource(t, "postgres_signal_repository.go")
	authority := p406SourceSection(
		t, source,
		"func (repository *PostgresMediaSignalRepository) loadSignalAuthority(",
		"func loadSignalRoom(",
	)
	p406RequireOrderedFragments(t, authority, []string{
		"repository.lifecycle.acquireTenantControlLock(",
		"featurecontrol.AcquireTenantControlReadLock(",
		"repository.lifecycle.requireActiveScope(",
		"loadSpace(ctx, transaction, scope.TenantID, spaceID, lock)",
		"repository.lifecycle.authorizeSource(",
		"policy.ActionSessionJoin",
		"space.Status != SpaceStatusOpen",
		"loadSignalRoom(",
		"room.Status != RoomInstanceActive",
		"loadSignalParticipantByActor(",
	})
	for _, required := range []string{
		"repository.lifecycle.controls.RequireFeature(",
		"repository.lifecycle.controls.RequireFeatureForRead(",
	} {
		if !strings.Contains(authority, required) {
			t.Fatalf("P4-06 signal authority is missing lock-mode feature gate %q", required)
		}
	}
	if strings.Contains(authority, "space.Status != SpaceStatusOpen || space.Locked") {
		t.Fatal("P4-06 signal authority incorrectly ejects active participants when the room is locked")
	}
	for _, fragment := range []string{
		"effectiveRoomRole(",
		"participant.InstanceRole = actorRole",
		"actorRole: actorRole",
	} {
		if !strings.Contains(authority, fragment) {
			t.Fatalf("P4-06 effective moderator authority is missing %q", fragment)
		}
	}
}

func TestP406SignalTimeComesOnlyFromDatabaseAfterAuthorityLock(t *testing.T) {
	t.Parallel()

	repositorySource := p406RepositorySource(t, "postgres_signal_repository.go")
	serviceSource := p406RepositorySource(t, "signal_service.go")
	for _, forbidden := range []string{"command.OccurredAt", "time.Now(", "clock func() time.Time"} {
		if strings.Contains(repositorySource, forbidden) || strings.Contains(serviceSource, forbidden) {
			t.Fatalf("P4-06 signal flow retains application time authority %q", forbidden)
		}
	}

	databaseClock := p406SourceSection(
		t, repositorySource,
		"func loadMediaSignalDatabaseTime(",
		"func (repository *PostgresMediaSignalRepository) loadSignalAuthority(",
	)
	if !strings.Contains(databaseClock, "SELECT clock_timestamp()") {
		t.Fatal("P4-06 signal flow does not read authoritative PostgreSQL time")
	}

	getSnapshot := p406SourceSection(
		t, repositorySource,
		"func (repository *PostgresMediaSignalRepository) GetParticipantSnapshot(",
		"func (repository *PostgresMediaSignalRepository) SendSignal(",
	)
	p406RequireOrderedFragments(t, getSnapshot, []string{
		"repository.loadSignalAuthority(",
		"loadMediaSignalDatabaseTime(",
		"loadMediaParticipantSnapshot(",
	})

	send := p406SourceSection(
		t, repositorySource,
		"func (repository *PostgresMediaSignalRepository) SendSignal(",
		"func loadMediaSignalDatabaseTime(",
	)
	p406RequireOrderedFragments(t, send, []string{
		"repository.loadSignalAuthority(",
		"loadMediaSignalReplay(",
		"if replayed {",
		"acceptedAt, timeErr := loadMediaSignalDatabaseTime(",
	})
	versionCheck := strings.Index(send, "authority.room.ProjectionVersion != command.ExpectedProjectionVersion")
	mutationClock := strings.Index(send, "acceptedAt, err := loadMediaSignalDatabaseTime(")
	consumeLimits := strings.Index(send, "consumeMediaSignalLimits(")
	if versionCheck < 0 || mutationClock <= versionCheck || consumeLimits <= mutationClock {
		t.Fatal("P4-06 mutation DB time must be read after version checks and before rate limits")
	}
	if strings.Count(send, "loadMediaParticipantSnapshot(") < 2 ||
		strings.Count(send, "command, acceptedAt") < 3 ||
		strings.Count(send, "authority, acceptedAt") < 2 {
		t.Fatal("P4-06 signal mutation does not reuse one DB acceptedAt consistently")
	}
}

func TestP406RosterProjectionUsesOnlyServerSequenceOpaqueKeyAndSafeFields(t *testing.T) {
	t.Parallel()

	signalSource := p406RepositorySource(t, "postgres_signal_repository.go")
	snapshot := p406SourceSection(
		t, signalSource,
		"func loadMediaParticipantSnapshot(",
		"func truncateMediaDisplayName(",
	)
	for _, required := range []string{
		"participant.participant_key", "participant.roster_sequence",
		"target.display_name", "participant.instance_role", "participant.status",
		"participant.tenant_id = $1", "participant.space_id = $2",
		"participant.room_instance_id = $3",
		"ORDER BY participant.roster_sequence, participant.participant_key",
		"LIMIT 50",
		"hand.is_raised", "ORDER BY hand.signal_sequence, participant.participant_key",
		"expires_at > $4", "LIMIT 500",
	} {
		if !strings.Contains(snapshot, required) {
			t.Fatalf("P4-06 roster projection is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"target.email", "provider_participant_identity", "join_attempt_id",
		"participant.admission_request_id", "session_token", "access_token",
	} {
		if strings.Contains(strings.ToLower(snapshot), forbidden) {
			t.Fatalf("P4-06 roster projection exposes forbidden field class %q", forbidden)
		}
	}

	instanceSource := p406RepositorySource(t, "postgres_instance_repository.go")
	attempt := p406SourceSection(
		t, instanceSource,
		"func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(",
		"func (repository *PostgresInstanceRepository) requireParticipantCapacity(",
	)
	p406RequireOrderedFragments(t, attempt, []string{
		"ParticipantKey:",
		"SET next_roster_sequence = next_roster_sequence + 1",
		"RETURNING next_roster_sequence",
		"participant_key, roster_sequence",
	})
}

func TestP406SignalSQLScopesEveryMutationAndLookupByTenantAndRoom(t *testing.T) {
	t.Parallel()

	source := p406RepositorySource(t, "postgres_signal_repository.go")
	room := p406SourceSection(t, source, "func loadSignalRoom(", "func loadSignalParticipantByActor(")
	for _, required := range []string{"tenant_id = $1", "space_id = $2", "id = $3"} {
		if !strings.Contains(room, required) {
			t.Fatalf("P4-06 room SQL is missing scope predicate %q", required)
		}
	}
	advance := p406SourceSection(
		t, source,
		"func advanceSignalRoomProjection(",
		"func (repository *PostgresMediaSignalRepository) persistSignalChange(",
	)
	for _, required := range []string{"tenant_id = $1", "space_id = $2", "id = $3"} {
		if !strings.Contains(advance, required) {
			t.Fatalf("P4-06 projection advance SQL is missing scope predicate %q", required)
		}
	}
	for name, section := range map[string]string{
		"actor":   p406SourceSection(t, source, "func loadSignalParticipantByActor(", "func loadMediaParticipantSnapshot("),
		"replay":  p406SourceSection(t, source, "func loadMediaSignalReplay(", "func (repository *PostgresMediaSignalRepository) applySignal("),
		"apply":   p406SourceSection(t, source, "func (repository *PostgresMediaSignalRepository) applySignal(", "func advanceSignalRoomProjection("),
		"persist": p406SourceSection(t, source, "func (repository *PostgresMediaSignalRepository) persistSignalChange(", "func insertMediaSignalReceipt("),
		"receipt": p406SourceSection(t, source, "func insertMediaSignalReceipt(", "type mediaSignalRateSpec struct"),
	} {
		if !strings.Contains(section, "tenant_id") || !strings.Contains(section, "room_instance_id") {
			t.Fatalf("P4-06 %s SQL is not explicitly tenant/room scoped", name)
		}
	}
	apply := p406SourceSection(
		t, source,
		"func (repository *PostgresMediaSignalRepository) applySignal(",
		"func advanceSignalRoomProjection(",
	)
	if !strings.Contains(apply, "participant_key = $4") ||
		!strings.Contains(apply, "status IN ('joining', 'connected', 'reconnecting')") ||
		!strings.Contains(apply, "FOR UPDATE") {
		t.Fatal("P4-06 lower-one does not bind and lock an active opaque participant target")
	}
}

func TestP406SignalLimitsAreCrossInstanceAndExact(t *testing.T) {
	t.Parallel()

	source := p406RepositorySource(t, "postgres_signal_repository.go")
	limits := p406SourceSection(
		t, source,
		"func consumeMediaSignalLimits(",
		"func consumeMediaSignalRateLimit(",
	)
	for _, required := range []string{
		"5 * time.Second, 3", "time.Minute, 20", "5 * time.Second, 100",
		"time.Minute, 6", "time.Minute, 120",
	} {
		if !strings.Contains(limits, required) {
			t.Fatalf("P4-06 rate matrix is missing %q", required)
		}
	}
	rate := p406SourceSection(
		t, source,
		"func consumeMediaSignalRateLimit(",
		"func signalRepositoryUnavailable(",
	)
	for _, required := range []string{
		"sha256.Sum256", "INSERT INTO tutorhub.rate_limit_windows",
		"ON CONFLICT (purpose, bucket_hash, window_started_at)",
		"used_count = tutorhub.rate_limit_windows.used_count + 1",
		"WHERE tutorhub.rate_limit_windows.used_count < $6",
		"MediaSignalRateLimitError",
	} {
		if !strings.Contains(rate, required) {
			t.Fatalf("P4-06 cross-instance rate gate is missing %q", required)
		}
	}
}

func TestP406ParticipantExitAndTerminalRoomCleanSignalState(t *testing.T) {
	t.Parallel()

	instance := p406RepositorySource(t, "postgres_instance_repository.go")
	participantWebhook := p406SourceSection(
		t, instance,
		"func classifyParticipantWebhook(",
		"func advanceMediaRosterProjection(",
	)
	for _, required := range []string{
		"UPDATE tutorhub.media_participant_hand_states",
		"SET is_raised = false",
		"participant_session_id = $3",
		"advanceMediaRosterProjection(",
	} {
		if !strings.Contains(participantWebhook, required) {
			t.Fatalf("P4-06 participant exit cleanup is missing %q", required)
		}
	}

	lifecycle := p406RepositorySource(t, "postgres_lifecycle_repository.go")
	terminal := p406SourceSection(
		t, lifecycle,
		"func terminateRoomParticipants(",
		"func insertTransitionReceipt(",
	)
	p406RequireOrderedFragments(t, terminal, []string{
		"UPDATE tutorhub.media_participant_sessions",
		"UPDATE tutorhub.media_participant_hand_states",
		"SET is_raised = false",
		"advanceMediaRosterProjection(",
	})

	lobby := p406RepositorySource(t, "postgres_lobby_repository.go")
	removal := p406SourceSection(
		t, lobby,
		"func removeLobbyMemberParticipants(",
		"func expireTimedOutLobbyAdmissions(",
	)
	for _, required := range []string{
		"UPDATE tutorhub.media_participant_hand_states AS hand",
		"SET is_raised = false",
		"participant.user_id = $4",
		"advanceMediaRosterProjection(",
	} {
		if !strings.Contains(removal, required) {
			t.Fatalf("P4-06 member-removal cleanup is missing %q", required)
		}
	}
	for name, section := range map[string]string{
		"signal runtime":      p406RepositorySource(t, "postgres_signal_repository.go"),
		"participant webhook": participantWebhook,
		"terminal room":       terminal,
		"member removal":      removal,
	} {
		for _, forbidden := range []string{
			"DELETE FROM tutorhub.media_participant_hand_states",
			"DELETE FROM tutorhub.media_reaction_events",
		} {
			if strings.Contains(section, forbidden) {
				t.Fatalf("P4-06 %s contains forbidden runtime cleanup %q", name, forbidden)
			}
		}
	}
}

func TestP406SignalQueriesUseOnlyReviewedSharedDependencyColumns(t *testing.T) {
	t.Parallel()

	source := p406RepositorySource(t, "postgres_signal_repository.go")
	snapshot := p406SourceSection(t, source, "func loadMediaParticipantSnapshot(", "func truncateMediaDisplayName(")
	if !strings.Contains(snapshot, "JOIN tutorhub.users AS target ON target.id = participant.user_id") ||
		!strings.Contains(snapshot, "target.display_name") {
		t.Fatal("P4-06 users dependency must use only id/display_name for the safe roster projection")
	}
	rate := p406SourceSection(t, source, "func consumeMediaSignalRateLimit(", "func signalRepositoryUnavailable(")
	for _, forbidden := range []string{"actor_user_id", "tenant_id", "room_instance_id", "raw_bucket"} {
		if strings.Contains(strings.ToLower(rate), forbidden) {
			t.Fatalf("P4-06 rate-limit persistence contains unreviewed dependency field %q", forbidden)
		}
	}
}

func TestP406HandRaiseUsesDefaultWithinExactInsertACL(t *testing.T) {
	t.Parallel()

	source := p406RepositorySource(t, "postgres_signal_repository.go")
	persist := p406SourceSection(
		t, source,
		"func (repository *PostgresMediaSignalRepository) persistSignalChange(",
		"func insertMediaSignalReceipt(",
	)
	if !strings.Contains(persist, "participant_session_id,\n    signal_sequence, raised_at") {
		t.Fatal("P4-06 hand raise must rely on the reviewed is_raised DEFAULT true insert boundary")
	}
	if strings.Contains(persist, "participant_session_id,\n    is_raised") {
		t.Fatal("P4-06 hand raise requests is_raised outside the exact runtime INSERT column ACL")
	}
	if !strings.Contains(persist, "DO UPDATE SET is_raised = true") {
		t.Fatal("P4-06 re-raise must explicitly restore the existing hand state")
	}
}

func TestP406SharedHarnessIsFreshFailClosedAndReadOnlyOutsideProvision(t *testing.T) {
	t.Parallel()

	shared := p406RepositorySource(t, "p406_shared_staging_integration_test.go")
	for _, required := range []string{
		"TestPostgresP406SharedOwnerPreflight",
		"TestPostgresP406SharedFinalSnapshot",
		"P4_06_SHARED_CONFIRM",
		"P4_06_SHARED_OWNER_PREFLIGHT",
		"P4_06_SHARED_ACL_PROVISION_CONFIRM",
		"P4_06_SHARED_FINAL_SNAPSHOT_CONFIRM",
		"P4_04_SHARED_CONFIRM",
		"P4_06_DISPOSABLE_CONFIRM",
		"runPostgresP406ReadOnlySnapshot(t, 31, false)",
		"runPostgresP406ReadOnlySnapshot(t, 32, true)",
	} {
		if !strings.Contains(shared, required) {
			t.Fatalf("P4-06 shared harness is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"migrationrunner",
		".Exec(",
		"INSERT INTO",
		"UPDATE tutorhub",
		"DELETE FROM",
		"TRUNCATE",
		"NewLiveKitRoomProvider",
	} {
		if strings.Contains(shared, forbidden) {
			t.Fatalf("P4-06 shared read-only harness contains forbidden boundary %q", forbidden)
		}
	}

	provision := p406RepositorySource(t, "postgres_acl_provision_integration_test.go")
	for _, required := range []string{
		"TestProvisionPostgresMediaSignalsExactACLShared",
		"p406SharedACLProvisionConfirmation",
		"requireP406SharedConfirmation(",
		"P4-04 shared ACL provisioning is retired",
	} {
		if !strings.Contains(provision, required) {
			t.Fatalf("P4-06 shared ACL provisioner is missing %q", required)
		}
	}
}

func TestP406CIUsesIsolatedFixtureRoleForStatefulSignalGate(t *testing.T) {
	t.Parallel()

	repositoryRoot := filepath.Join("..", "..", "..", "..", "..")
	contents, err := os.ReadFile(filepath.Join(repositoryRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	gate := `-run '^TestPostgresMediaParticipantSignalsLifecycleAndConcurrency$'`
	gatePosition := strings.Index(source, gate)
	if gatePosition < 0 {
		t.Fatal("P4-06 CI is missing the stateful participant-signal database gate")
	}
	fixtureCommand := `DATABASE_POOL_URL="$DATABASE_MEDIA_FIXTURE_TEST_URL" go test -count=1 -tags=integration`
	fixturePosition := strings.LastIndex(source[:gatePosition], fixtureCommand)
	if fixturePosition < 0 || gatePosition-fixturePosition > 200 {
		t.Fatal("P4-06 CI stateful signal gate does not use the isolated non-superuser fixture role")
	}
}

func p406RepositorySource(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func p406SourceSection(t *testing.T, source string, start string, end string) string {
	t.Helper()
	startPosition := strings.Index(source, start)
	if startPosition < 0 {
		t.Fatalf("P4-06 source is missing section start %q", start)
	}
	remainder := source[startPosition+len(start):]
	endPosition := strings.Index(remainder, end)
	if endPosition < 0 {
		t.Fatalf("P4-06 source is missing section end %q after %q", end, start)
	}
	return source[startPosition : startPosition+len(start)+endPosition]
}

func p406RequireOrderedFragments(t *testing.T, source string, fragments []string) {
	t.Helper()
	lastPosition := -1
	for _, fragment := range fragments {
		position := strings.Index(source, fragment)
		if position < 0 {
			t.Fatalf("P4-06 security boundary is missing %q", fragment)
		}
		if position <= lastPosition {
			t.Fatalf("P4-06 security boundary %q is out of order", fragment)
		}
		lastPosition = position
	}
}
