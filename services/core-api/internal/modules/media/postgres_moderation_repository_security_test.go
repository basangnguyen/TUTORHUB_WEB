package media

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestP407ModerationRoleMatrixIsExact(t *testing.T) {
	t.Parallel()

	operations := []ModerationOperation{
		ModerationLock, ModerationUnlock, ModerationPromote,
		ModerationDemote, ModerationMute, ModerationRemove,
	}
	roles := []InstanceRole{
		InstanceRoleHost, InstanceRoleCoHost,
		InstanceRoleTeachingAssistant, InstanceRoleAttendee,
	}
	allowed := map[InstanceRole]map[ModerationOperation]bool{
		InstanceRoleHost: {
			ModerationLock: true, ModerationUnlock: true,
			ModerationPromote: true, ModerationDemote: true,
			ModerationMute: true, ModerationRemove: true,
		},
		InstanceRoleCoHost:            {ModerationMute: true, ModerationRemove: true},
		InstanceRoleTeachingAssistant: {ModerationMute: true},
		InstanceRoleAttendee:          {},
	}
	for _, role := range roles {
		for _, operation := range operations {
			if got, want := moderationActorAllowed(operation, role), allowed[role][operation]; got != want {
				t.Errorf("moderationActorAllowed(%q, %q) = %t, want %t", operation, role, got, want)
			}
		}
	}

	targetCases := []struct {
		name      string
		operation ModerationOperation
		actor     InstanceRole
		target    InstanceRole
		want      bool
	}{
		{"host promotes attendee", ModerationPromote, InstanceRoleHost, InstanceRoleAttendee, true},
		{"host cannot promote TA", ModerationPromote, InstanceRoleHost, InstanceRoleTeachingAssistant, false},
		{"host demotes dynamic co-host", ModerationDemote, InstanceRoleHost, InstanceRoleCoHost, true},
		{"host cannot demote attendee", ModerationDemote, InstanceRoleHost, InstanceRoleAttendee, false},
		{"host mutes co-host", ModerationMute, InstanceRoleHost, InstanceRoleCoHost, true},
		{"host removes TA", ModerationRemove, InstanceRoleHost, InstanceRoleTeachingAssistant, true},
		{"co-host mutes attendee", ModerationMute, InstanceRoleCoHost, InstanceRoleAttendee, true},
		{"co-host cannot mute TA", ModerationMute, InstanceRoleCoHost, InstanceRoleTeachingAssistant, false},
		{"co-host removes attendee", ModerationRemove, InstanceRoleCoHost, InstanceRoleAttendee, true},
		{"TA mutes attendee", ModerationMute, InstanceRoleTeachingAssistant, InstanceRoleAttendee, true},
		{"TA cannot remove attendee", ModerationRemove, InstanceRoleTeachingAssistant, InstanceRoleAttendee, false},
	}
	for _, test := range targetCases {
		t.Run(test.name, func(t *testing.T) {
			got := moderationActorAllowed(test.operation, test.actor) &&
				moderationTargetAllowed(test.operation, test.actor, test.target)
			if got != test.want {
				t.Fatalf("combined moderation matrix = %t, want %t", got, test.want)
			}
		})
	}
	for _, actor := range roles {
		for _, operation := range []ModerationOperation{
			ModerationPromote, ModerationDemote, ModerationMute, ModerationRemove,
		} {
			if moderationTargetAllowed(operation, actor, InstanceRoleHost) {
				t.Errorf("%q unexpectedly permits %q to target protected host", operation, actor)
			}
		}
	}
}

func TestP407SafetyAdminAuthorityIsRemoveOnlyAndDoesNotRequireRoomMembership(t *testing.T) {
	t.Parallel()

	for _, operation := range []ModerationOperation{
		ModerationLock, ModerationUnlock, ModerationPromote,
		ModerationDemote, ModerationMute,
	} {
		normal, safety := moderationAuthority(
			operation, InstanceRoleAttendee, false, true,
		)
		if normal || safety {
			t.Errorf("safety admin unexpectedly received %q without a room role", operation)
		}
	}
	normal, safety := moderationAuthority(
		ModerationRemove, InstanceRoleAttendee, false, true,
	)
	if normal || !safety {
		t.Fatalf("non-participant safety admin remove authority = normal:%t safety:%t", normal, safety)
	}
	normal, safety = moderationAuthority(
		ModerationRemove, InstanceRoleCoHost, true, true,
	)
	if normal || !safety {
		t.Fatalf("safety marker must keep remove reason-required after join = normal:%t safety:%t", normal, safety)
	}
	_, safety = moderationAuthority(
		ModerationRemove, InstanceRoleAttendee, false, false,
	)
	if safety {
		t.Fatal("ordinary non-participant unexpectedly received safety remove authority")
	}
}

func TestP407SafetyVisibilityDoesNotCreateStudyMeetingJoinAuthority(t *testing.T) {
	t.Parallel()

	if sourceAvailableForJoin(authorizedSource{
		Kind: SourceStudyMeeting, Status: "scheduled", SafetyAdmin: true,
	}) {
		t.Fatal("safety-admin visibility unexpectedly created StudyMeeting join authority")
	}
	if !sourceAvailableForJoin(authorizedSource{
		Kind: SourceStudyMeeting, Status: "scheduled", SafetyAdmin: true, CanJoin: true,
	}) {
		t.Fatal("an explicitly authorized StudyMeeting member lost join authority")
	}
	contents, err := os.ReadFile("postgres_lifecycle_repository.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	for _, required := range []string{
		"CanJoin: owner || explicitMember",
		"Owner: owner, SafetyAdmin: safetyAdmin",
		"CanShareScreen:             owner",
	} {
		if !strings.Contains(source, required) {
			t.Errorf("StudyMeeting safety boundary is missing %q", required)
		}
	}
}

func TestP407OfficialClassSafetyAdminDoesNotReplaceNormalTeachingAuthority(t *testing.T) {
	t.Parallel()

	owner := policy.ClassRoleOwner
	coTeacher := policy.ClassRoleCoTeacher
	ta := policy.ClassRoleTeachingAssistant
	student := policy.ClassRoleStudent
	admin := []policy.OrganizationRole{policy.OrganizationRoleAdmin}
	adminTeacher := []policy.OrganizationRole{
		policy.OrganizationRoleAdmin, policy.OrganizationRoleTeacher,
	}
	for _, test := range []struct {
		name string
		role *policy.ClassRole
		org  []policy.OrganizationRole
		want bool
	}{
		{"admin without enrollment", nil, admin, true},
		{"admin enrolled as student", &student, admin, true},
		{"admin enrolled as TA", &ta, admin, true},
		{"admin class owner", &owner, admin, false},
		{"admin class co-teacher", &coTeacher, admin, false},
		{"organization teacher authority", nil, adminTeacher, false},
		{"ordinary student", &student, []policy.OrganizationRole{policy.OrganizationRoleStudent}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := classSafetyAdmin(test.role, test.org); got != test.want {
				t.Fatalf("classSafetyAdmin() = %t, want %t", got, test.want)
			}
		})
	}
	if got := instanceRoleForClass(nil, admin); got != InstanceRoleAttendee {
		t.Fatalf("safety-only organization admin role = %q, want attendee", got)
	}
}

func TestP407ModerationReplayIsBeforeVersionConflictAndReadOnly(t *testing.T) {
	t.Parallel()
	source := readP407RepositorySource(t)
	apply := p407FunctionSection(t, source, "func (repository *PostgresModerationRepository) ApplyModeration(", "func applyParticipantModeration(")
	replayAt := strings.Index(apply, "loadModerationReplay(")
	versionAt := strings.Index(apply, "space.Version != command.Expected.SpaceVersion")
	if replayAt < 0 || versionAt < 0 || replayAt > versionAt {
		t.Fatal("idempotent replay must be evaluated before expected-version conflict")
	}
	replay := p407FunctionSection(t, source, "func loadModerationReplay(", "func insertModerationReceipt(")
	if strings.Contains(replay, "FOR UPDATE") {
		t.Fatal("early idempotent replay must stay read-only so it cannot invert room/participant -> receipt lock order")
	}
	for _, fragment := range []string{
		"receipt.tenant_id = $1", "receipt.actor_user_id = $2",
		"receipt.idempotency_key = $3", "operation != command.Operation",
		"bytes.Equal(fingerprint, command.Fingerprint[:])", "ErrModerationIdempotency",
		"uuid.NullUUID", "sql.NullInt64",
	} {
		if !strings.Contains(replay, fragment) {
			t.Fatalf("moderation replay is missing %q", fragment)
		}
	}
}

func TestP407ModerationRepositoryReauthorizesOperationAndSharesEffectiveRole(t *testing.T) {
	t.Parallel()
	repository := readP407RepositorySource(t)
	for _, action := range []string{
		"policy.ActionMediaLock", "policy.ActionMediaModerate", "policy.ActionParticipantRemove",
	} {
		if !strings.Contains(repository, action) {
			t.Errorf("moderation repository does not reauthorize %s", action)
		}
	}
	for _, filename := range []string{
		"postgres_moderation_repository.go",
		"postgres_signal_repository.go",
		"postgres_instance_repository.go",
	} {
		contents, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(contents), "effectiveRoomRole(") {
			t.Errorf("%s does not use the shared effective RoomInstance-role resolver", filename)
		}
	}
}

func TestP407ModerationRatePoliciesAreEndpointScopedAndBounded(t *testing.T) {
	t.Parallel()

	if moderationRateWindow != time.Minute {
		t.Fatalf("moderation rate window = %s, want 1m", moderationRateWindow)
	}
	for _, test := range []struct {
		operation ModerationOperation
		purpose   string
		limit     int64
	}{
		{ModerationLock, moderationLockRatePurpose, 12},
		{ModerationUnlock, moderationLockRatePurpose, 12},
		{ModerationPromote, moderationRoleRatePurpose, 30},
		{ModerationDemote, moderationRoleRatePurpose, 30},
		{ModerationMute, moderationMuteRatePurpose, 60},
		{ModerationRemove, moderationRemoveRatePurpose, 30},
	} {
		policy, ok := moderationRatePolicyForOperation(test.operation)
		if !ok || policy.purpose != test.purpose || policy.limit != test.limit {
			t.Errorf("rate policy for %q = %+v, %t; want purpose=%q limit=%d", test.operation, policy, ok, test.purpose, test.limit)
		}
	}
	if _, ok := moderationRatePolicyForOperation(ModerationOperation("unknown")); ok {
		t.Fatal("unknown moderation operation unexpectedly received a rate policy")
	}
}

func TestP407ModerationRateLimitIsCrossInstanceOpaqueAndAfterAuthorization(t *testing.T) {
	t.Parallel()

	policy, _ := moderationRatePolicyForOperation(ModerationLock)
	tenantID, roomID, actorID := uuid.New(), uuid.New(), uuid.New()
	base := moderationRateBucketHash(policy, tenantID, roomID, actorID)
	for name, hash := range map[string][32]byte{
		"tenant": moderationRateBucketHash(policy, uuid.New(), roomID, actorID),
		"room":   moderationRateBucketHash(policy, tenantID, uuid.New(), actorID),
		"actor":  moderationRateBucketHash(policy, tenantID, roomID, uuid.New()),
		"action": moderationRateBucketHash(
			moderationRatePolicy{purpose: moderationMuteRatePurpose, limit: moderationMuteRateLimit},
			tenantID, roomID, actorID,
		),
	} {
		if hash == base {
			t.Errorf("moderation rate bucket did not isolate %s", name)
		}
	}

	source := readP407RepositorySource(t)
	apply := p407FunctionSection(t, source, "func (repository *PostgresModerationRepository) ApplyModeration(", "const moderationRateWindow")
	replayAt := strings.Index(apply, "loadModerationReplay(")
	authorizeAt := strings.Index(apply, "repository.authorizeModerationOperation(")
	rateAt := strings.Index(apply, "consumeModerationRateLimit(")
	mutationAt := strings.Index(apply, "UPDATE tutorhub.media_spaces")
	if replayAt < 0 || authorizeAt < 0 || rateAt < 0 || mutationAt < 0 ||
		!(replayAt < authorizeAt && authorizeAt < rateAt && rateAt < mutationAt) {
		t.Fatal("moderation limiter must run after replay and exact authorization but before mutation")
	}
	rate := p407FunctionSection(t, source, "const moderationRateWindow", "func applyParticipantModeration(")
	for _, fragment := range []string{
		"SELECT clock_timestamp()", "sha256.Sum256", "tenantID.String()",
		"roomID.String()", "actorID.String()", "INSERT INTO tutorhub.rate_limit_windows",
		"ON CONFLICT (purpose, bucket_hash, window_started_at)",
		"used_count = tutorhub.rate_limit_windows.used_count + 1",
		"WHERE tutorhub.rate_limit_windows.used_count < $6", "ModerationRateLimitError",
		"return ErrModerationUnavailable",
	} {
		if !strings.Contains(rate, fragment) {
			t.Fatalf("moderation cross-instance rate gate is missing %q", fragment)
		}
	}
	if !strings.Contains(rate, "policy.purpose, bucketHash[:]") {
		t.Fatal("moderation limiter must persist only the purpose and opaque bucket hash")
	}

	limited := normalizeModerationError(&ModerationRateLimitError{RetryAfter: 5 * time.Second})
	var typed *ModerationRateLimitError
	if !errors.As(limited, &typed) || typed.RetryAfter != 5*time.Second {
		t.Fatalf("typed moderation rate error was not preserved: %v", limited)
	}
}

func TestP407ModerationRemoveAndProviderLeaseFailClosed(t *testing.T) {
	t.Parallel()
	source := readP407RepositorySource(t)
	for _, fragment := range []string{
		"tenant_id = $1 AND space_id = $2 AND room_instance_id = $3",
		"participant_key = $4",
		"status = 'removed', capacity_reserved = false",
		"terminal_at = $6, removed_by = $5",
		"reconnecting_at = NULL",
		"SET is_raised = false",
		"projection_version = projection_version + 1",
		"provider_effect_status IN ('pending', 'retryable_failed')",
		"provider_effect_status = 'applying' AND provider_effect_lease_until <= $6",
		"provider_effect_attempts = $4",
		"provider_effect_status = 'applying'",
		"RETURNING provider_effect_status",
	} {
		if !strings.Contains(source, fragment) {
			t.Errorf("moderation remove/provider CAS is missing %q", fragment)
		}
	}
	if !strings.Contains(source, "publicProviderEffectStatus(") {
		t.Error("provider lease repository must normalize internal applying to a public allowlisted status")
	}
}

func TestP407ModerationReceiptResultsMatchMigrationOperationShape(t *testing.T) {
	t.Parallel()
	source := readP407RepositorySource(t)
	apply := p407FunctionSection(t, source, "func applyParticipantModeration(", "func mutateDynamicCoHost(")
	muteAt := strings.Index(apply, "case ModerationMute:")
	removeAt := strings.Index(apply, "case ModerationRemove:")
	defaultAt := strings.Index(apply, "default:")
	if muteAt < 0 || removeAt < 0 || defaultAt < 0 || !(muteAt < removeAt && removeAt < defaultAt) {
		t.Fatal("cannot isolate mute/remove result projection")
	}
	if strings.Contains(apply[muteAt:removeAt], "TargetInstanceRole") {
		t.Error("mute must leave result_instance_role NULL under migration 000033")
	}
	if strings.Contains(apply[removeAt:defaultAt], "TargetInstanceRole") {
		t.Error("remove must leave result_instance_role NULL under migration 000033")
	}
	if !strings.Contains(apply[muteAt:removeAt], "TargetParticipantVersion") ||
		!strings.Contains(apply[removeAt:defaultAt], "TargetParticipantVersion") {
		t.Error("mute/remove must persist their target participant version")
	}
}

func TestP407ModerationAuditAndPublicContractExcludeProviderDetailsAndUnmute(t *testing.T) {
	t.Parallel()
	repository := readP407RepositorySource(t)
	auditSection := p407FunctionSection(t, repository, "func appendModerationEvent(", "func moderationEventIdentity(")
	for _, forbidden := range []string{
		"ProviderIdentity", "provider_room_name", "provider_participant_identity",
		"track_sid", "raw_error", "error_detail", "target.UserID",
	} {
		if strings.Contains(auditSection, forbidden) {
			t.Errorf("moderation audit/outbox includes forbidden detail %q", forbidden)
		}
	}
	for _, filename := range []string{"moderation_service.go", "livekit_provider.go"} {
		contents, err := os.ReadFile(filename)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"UnmuteParticipant", "RemoteUnmute", "Muted: false", "Muted:false",
		} {
			if strings.Contains(string(contents), forbidden) {
				t.Errorf("%s exposes remote-unmute construct %q", filename, forbidden)
			}
		}
	}
	for _, eventType := range []string{
		"media_space.locked.v1", "media_space.unlocked.v1",
		"media_participant.promoted.v1", "media_participant.demoted.v1",
		"media_participant.muted.v1", "media_participant.removed.v1",
	} {
		if !strings.Contains(repository, eventType) {
			t.Errorf("moderation event allowlist is missing %q", eventType)
		}
	}
	for _, required := range []string{
		"'safety_action', CASE WHEN", `metadata["safety_action"] = "true"`,
	} {
		if !strings.Contains(auditSection, required) {
			t.Errorf("moderation audit/outbox is missing safety marker %q", required)
		}
	}
}

func readP407RepositorySource(t *testing.T) string {
	t.Helper()
	contents, err := os.ReadFile("postgres_moderation_repository.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func p407FunctionSection(t *testing.T, source, start, end string) string {
	t.Helper()
	startAt := strings.Index(source, start)
	if startAt < 0 {
		t.Fatalf("missing function marker %q", start)
	}
	endAt := strings.Index(source[startAt+len(start):], end)
	if endAt < 0 {
		t.Fatalf("missing next function marker %q", end)
	}
	return source[startAt : startAt+len(start)+endAt]
}
