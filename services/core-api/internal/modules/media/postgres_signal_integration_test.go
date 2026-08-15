//go:build integration

package media

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const p406DisposableConfirmation = "I_UNDERSTAND_P4_06_DISPOSABLE_ONLY"

type p406SignalFixture struct {
	spaceID       uuid.UUID
	roomID        uuid.UUID
	access        map[string]AccessContext
	keys          map[string]uuid.UUID
	participantID map[string]uuid.UUID
}

func TestPostgresMediaParticipantSignalsLifecycleAndConcurrency(t *testing.T) {
	if strings.TrimSpace(os.Getenv("P4_06_DISPOSABLE_CONFIRM")) != p406DisposableConfirmation {
		t.Skip("P4_06_DISPOSABLE_CONFIRM is not set to the disposable-only confirmation")
	}
	migrationURL := requireMediaIntegrationEnvironment(t, "DATABASE_MIGRATION_URL")
	runtimeURL := requireMediaIntegrationEnvironment(t, "DATABASE_POOL_URL")
	maintenanceURL := requireMediaIntegrationEnvironment(t, "DATABASE_POLL_MAINTENANCE_URL")
	requireP406SignalFixtureDatabaseURLBoundary(t, migrationURL, runtimeURL, maintenanceURL)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatal("inspect participant-signal migration ledger")
	}
	if version.Dirty ||
		(version.Number != 31 && version.Number != 32 && version.Number != 33 && version.Number != 34 && version.Number != 35) {
		t.Fatal("P4-06 disposable integration requires a clean ledger at version 31, 32, 33, or 34")
	}
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatal("apply participant-signal migrations")
	}
	version, err = migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatal("inspect participant-signal migration ledger after forward migration")
	}
	if version.Number != 35 || version.Dirty {
		t.Fatal("P4-06 disposable integration requires latest ledger 35 false after forward migration")
	}
	migrationPool := openMediaIntegrationPool(t, ctx, migrationURL)
	t.Cleanup(migrationPool.Close)
	runtimePool := openMediaIntegrationPool(t, ctx, runtimeURL)
	t.Cleanup(runtimePool.Close)
	maintenancePool := openMediaIntegrationPool(t, ctx, maintenanceURL)
	t.Cleanup(maintenancePool.Close)
	assertP402SeparatedDatabaseRoles(t, ctx, migrationPool, runtimePool)

	baseFixture := seedMediaIntegrationFixture(t, ctx, migrationPool)
	t.Cleanup(func() { cleanupMediaIntegrationFixture(t, migrationPool, baseFixture) })
	setMediaFeatureOverrides(t, ctx, migrationPool, baseFixture.tenantID, baseFixture.adminID,
		map[featurecontrol.FeatureKey]bool{featurecontrol.FeatureClassroomMediaRooms: true})
	fixture := seedP406SignalFixture(t, ctx, migrationPool, baseFixture)
	t.Cleanup(func() { cleanupP406SignalRateLimits(t, migrationPool, fixture) })
	service := newP406SignalService(t, runtimePool)

	initial := getP406Snapshot(t, ctx, service, fixture, fixture.access["owner"])
	if len(initial.Participants) != 3 || initial.SelfParticipantKey != fixture.keys["owner"] ||
		!initial.ViewerOperations.CanModerateHands {
		t.Fatalf("owner roster projection is incomplete: %+v", initial)
	}
	for index, name := range []string{"owner", "student", "co_teacher"} {
		participant := initial.Participants[index]
		if participant.ParticipantKey != fixture.keys[name] ||
			participant.RosterSequence != int64(index+1) ||
			participant.ParticipantKey == fixture.access[name].ActorID {
			t.Fatalf("authoritative roster item %d is not opaque and ordered: %+v", index, participant)
		}
	}
	studentView := getP406Snapshot(t, ctx, service, fixture, fixture.access["student"])
	if studentView.SelfParticipantKey != fixture.keys["student"] ||
		studentView.ViewerOperations.CanModerateHands {
		t.Fatal("attendee self key or moderator capability is incorrect")
	}
	foreign := p402IntegrationAccess(baseFixture.foreignTenantID, baseFixture.foreignOwnerID,
		policy.OrganizationRoleTeacher, uuid.New())
	if _, err := service.GetParticipantSnapshot(ctx, foreign, fixture.spaceID,
		p406SnapshotInput(initial, fixture)); !errors.Is(err, ErrMediaSignalNotFound) {
		t.Fatalf("foreign-tenant roster error = %v, want concealed not found", err)
	}
	if _, err := service.GetParticipantSnapshot(ctx,
		p402IntegrationAccess(baseFixture.tenantID, baseFixture.outsiderID,
			policy.OrganizationRoleStudent, uuid.New()), fixture.spaceID,
		p406SnapshotInput(initial, fixture)); !errors.Is(err, ErrMediaSignalNotFound) {
		t.Fatalf("same-tenant unauthorized roster error = %v, want concealed not found", err)
	}
	assertP406ParticipantKeyConstraint(t, ctx, migrationPool, baseFixture, fixture)

	studentRaiseKey := mediaIntegrationKey("p406-student-raise")
	studentRaised := sendP406SignalConcurrently(t, ctx, service, fixture,
		fixture.access["student"], initial, studentRaiseKey,
		MediaSignalHandRaise, uuid.Nil, "")
	replayed := sendP406Signal(t, ctx, service, fixture, fixture.access["student"], initial,
		studentRaiseKey, MediaSignalHandRaise, uuid.Nil, "")
	if replayed.ProjectionVersion != studentRaised.ProjectionVersion ||
		replayed.LastSignalSequence != studentRaised.LastSignalSequence {
		t.Fatal("same-key hand raise did not replay the original projection")
	}
	assertP406ReceiptReplayBoundary(
		t, ctx, migrationPool, maintenancePool, service, fixture, initial,
		studentRaised, studentRaiseKey,
	)
	if _, err := service.SendSignal(ctx, fixture.access["student"], fixture.spaceID,
		p406SignalInput(fixture, initial, mediaIntegrationKey("p406-stale-hand"),
			MediaSignalHandLower, uuid.Nil, "")); !errors.Is(err, ErrMediaSignalVersionConflict) {
		t.Fatalf("stale projection error = %v, want version conflict", err)
	}
	coRaised := sendP406Signal(t, ctx, service, fixture, fixture.access["co_teacher"], studentRaised,
		mediaIntegrationKey("p406-co-raise"), MediaSignalHandRaise, uuid.Nil, "")
	if len(coRaised.RaisedHands) != 2 ||
		coRaised.RaisedHands[0].ParticipantKey != fixture.keys["student"] ||
		coRaised.RaisedHands[1].ParticipantKey != fixture.keys["co_teacher"] {
		t.Fatalf("raised-hand FIFO is incorrect: %+v", coRaised.RaisedHands)
	}
	studentLowered := sendP406Signal(t, ctx, service, fixture, fixture.access["student"], coRaised,
		mediaIntegrationKey("p406-student-lower"), MediaSignalHandLower, uuid.Nil, "")
	studentReraised := sendP406Signal(t, ctx, service, fixture, fixture.access["student"], studentLowered,
		mediaIntegrationKey("p406-student-reraise"), MediaSignalHandRaise, uuid.Nil, "")
	if len(studentReraised.RaisedHands) != 2 ||
		studentReraised.RaisedHands[0].ParticipantKey != fixture.keys["co_teacher"] ||
		studentReraised.RaisedHands[1].ParticipantKey != fixture.keys["student"] {
		t.Fatalf("re-raised hand did not move to FIFO tail: %+v", studentReraised.RaisedHands)
	}
	if _, err := service.SendSignal(ctx, fixture.access["student"], fixture.spaceID,
		p406SignalInput(fixture, studentReraised, mediaIntegrationKey("p406-student-lower-one-denied"),
			MediaSignalHandLowerOne, fixture.keys["co_teacher"], "")); !errors.Is(err, ErrSpaceAccessDenied) {
		t.Fatalf("attendee lower-one error = %v, want access denied", err)
	}
	ownerLoweredOne := sendP406Signal(t, ctx, service, fixture, fixture.access["owner"], studentReraised,
		mediaIntegrationKey("p406-owner-lower-one"), MediaSignalHandLowerOne,
		fixture.keys["student"], "")
	if len(ownerLoweredOne.RaisedHands) != 1 ||
		ownerLoweredOne.RaisedHands[0].ParticipantKey != fixture.keys["co_teacher"] {
		t.Fatalf("moderator lower-one result is incorrect: %+v", ownerLoweredOne.RaisedHands)
	}
	studentAgain := sendP406Signal(t, ctx, service, fixture, fixture.access["student"], ownerLoweredOne,
		mediaIntegrationKey("p406-student-raise-again"), MediaSignalHandRaise, uuid.Nil, "")
	ownerLoweredAll := sendP406Signal(t, ctx, service, fixture, fixture.access["owner"], studentAgain,
		mediaIntegrationKey("p406-owner-lower-all"), MediaSignalHandLowerAll, uuid.Nil, "")
	if len(ownerLoweredAll.RaisedHands) != 0 {
		t.Fatalf("moderator lower-all left active hands: %+v", ownerLoweredAll.RaisedHands)
	}

	if _, err := service.SendSignal(ctx, fixture.access["student"], fixture.spaceID,
		SendMediaSignalInput{
			ExpectedSpaceVersion: 2, ExpectedRoomInstanceID: fixture.roomID,
			ExpectedRoomInstanceVersion: 2, ExpectedProjectionVersion: ownerLoweredAll.ProjectionVersion,
			IdempotencyKey: mediaIntegrationKey("p406-invalid-reaction"), Kind: MediaSignalReaction,
			Reaction: MediaReaction("fire"),
		}); !errors.Is(err, ErrInvalidMediaSignalRequest) {
		t.Fatalf("non-allowlisted reaction error = %v, want invalid request", err)
	}
	var invalidReactionRows int
	if err := migrationPool.QueryRow(ctx, `SELECT count(*) FROM tutorhub.media_reaction_events
WHERE tenant_id=$1 AND room_instance_id=$2 AND reaction NOT IN
('thumbs_up','clap','heart','celebrate','laugh','surprised')`, baseFixture.tenantID,
		fixture.roomID).Scan(&invalidReactionRows); err != nil || invalidReactionRows != 0 {
		t.Fatalf("unknown reaction persistence = rows:%d error:%v", invalidReactionRows, err)
	}
	clapOne := sendP406Signal(t, ctx, service, fixture, fixture.access["student"], ownerLoweredAll,
		mediaIntegrationKey("p406-clap-one"), MediaSignalReaction, uuid.Nil, MediaReactionClap)
	clapTwo := sendP406Signal(t, ctx, service, fixture, fixture.access["student"], clapOne,
		mediaIntegrationKey("p406-clap-two"), MediaSignalReaction, uuid.Nil, MediaReactionClap)
	assertP406ReactionTTLAndGrouping(t, ctx, migrationPool, service, fixture, clapTwo)
	rateBase := getP406Snapshot(t, ctx, service, fixture, fixture.access["co_teacher"])
	secondService := newP406SignalService(t, runtimePool)
	burstNow := p406DatabaseTimeWithWindowHeadroom(t, ctx, migrationPool, 5*time.Second, 4*time.Second)
	for offset := 0; offset < 3; offset++ {
		p406SetRateLimitCount(
			t, ctx, migrationPool, mediaReactionBurstActorPurpose,
			p406ActorRateBucket(fixture, fixture.access["co_teacher"]), 5*time.Second,
			burstNow.Add(time.Duration(offset)*5*time.Second), 3,
		)
	}
	_, rateErr := secondService.SendSignal(ctx, fixture.access["co_teacher"], fixture.spaceID,
		p406SignalInput(fixture, rateBase, mediaIntegrationKey("p406-rate-denied"),
			MediaSignalReaction, uuid.Nil, MediaReactionHeart))
	var limited *MediaSignalRateLimitError
	if !errors.As(rateErr, &limited) || limited.RetryAfter < time.Second {
		t.Fatalf("fourth burst reaction error = %v, want bounded rate limit", rateErr)
	}
	rateBase = assertP406ReactionCrossInstanceRateMatrix(
		t, ctx, migrationPool, service, secondService, fixture, rateBase,
	)

	waited := assertP406AcceptedTimeAfterRoomWait(t, ctx, migrationPool, service, fixture, rateBase)
	assertP406SnapshotSignalConcurrency(t, ctx, service, fixture, waited)
	assertP406ParticipantAndRoomTerminalHandCleanup(
		t, ctx, migrationPool, runtimePool, fixture,
	)
}

func assertP406ReceiptReplayBoundary(
	t *testing.T,
	ctx context.Context,
	migrationPool *pgxpool.Pool,
	maintenancePool *pgxpool.Pool,
	service *MediaSignalService,
	fixture p406SignalFixture,
	original MediaParticipantSnapshot,
	accepted MediaParticipantSnapshot,
	key string,
) {
	t.Helper()
	var receiptCount int
	var exactRetention bool
	if err := migrationPool.QueryRow(ctx, `SELECT
    count(*),
    COALESCE(bool_and(retention_until = created_at + interval '24 hours'), false)
FROM tutorhub.media_signal_mutation_receipts
WHERE tenant_id = $1 AND room_instance_id = $2
  AND actor_user_id = $3 AND idempotency_key = $4`,
		fixture.access["student"].TenantID, fixture.roomID,
		fixture.access["student"].ActorID, key,
	).Scan(&receiptCount, &exactRetention); err != nil {
		t.Fatalf("inspect exact P4-06 receipt retention: %v", err)
	}
	if receiptCount != 1 || !exactRetention {
		t.Fatalf("same-key receipt retention = count:%d exact:%t, want 1/true",
			receiptCount, exactRetention)
	}

	if _, err := migrationPool.Exec(ctx, `WITH anchor AS (
    SELECT clock_timestamp() AS current_time
)
UPDATE tutorhub.media_signal_mutation_receipts AS receipt
SET created_at = anchor.current_time - interval '23 hours',
    retention_until = anchor.current_time + interval '1 hour'
FROM anchor
WHERE receipt.tenant_id = $1 AND receipt.room_instance_id = $2
  AND receipt.actor_user_id = $3 AND receipt.idempotency_key = $4`,
		fixture.access["student"].TenantID, fixture.roomID,
		fixture.access["student"].ActorID, key,
	); err != nil {
		t.Fatalf("move P4-06 receipt inside replay retention: %v", err)
	}
	withinRetention := sendP406Signal(
		t, ctx, service, fixture, fixture.access["student"], original, key,
		MediaSignalHandRaise, uuid.Nil, "",
	)
	if withinRetention.ProjectionVersion != accepted.ProjectionVersion ||
		withinRetention.LastSignalSequence != accepted.LastSignalSequence {
		t.Fatal("same-key signal inside 24-hour retention did not replay")
	}

	if _, err := migrationPool.Exec(ctx, `WITH anchor AS (
    SELECT clock_timestamp() AS current_time
)
UPDATE tutorhub.media_signal_mutation_receipts AS receipt
SET created_at = anchor.current_time - interval '24 hours',
    retention_until = anchor.current_time
FROM anchor
WHERE receipt.tenant_id = $1 AND receipt.room_instance_id = $2
  AND receipt.actor_user_id = $3 AND receipt.idempotency_key = $4`,
		fixture.access["student"].TenantID, fixture.roomID,
		fixture.access["student"].ActorID, key,
	); err != nil {
		t.Fatalf("expire P4-06 receipt at exact retention boundary: %v", err)
	}
	if deleted := callP406Purge(
		t, ctx, maintenancePool, "purge_expired_media_signal_receipts", 1,
	); deleted != 1 {
		t.Fatalf("expired P4-06 replay receipt purge = %d, want 1", deleted)
	}
	if err := migrationPool.QueryRow(ctx, `SELECT count(*)
FROM tutorhub.media_signal_mutation_receipts
WHERE tenant_id = $1 AND room_instance_id = $2
  AND actor_user_id = $3 AND idempotency_key = $4`,
		fixture.access["student"].TenantID, fixture.roomID,
		fixture.access["student"].ActorID, key,
	).Scan(&receiptCount); err != nil {
		t.Fatalf("inspect purged P4-06 replay receipt: %v", err)
	}
	if receiptCount != 0 {
		t.Fatal("expired P4-06 replay receipt remained after bounded purge")
	}
	if _, err := service.SendSignal(
		ctx, fixture.access["student"], fixture.spaceID,
		p406SignalInput(fixture, original, key, MediaSignalHandRaise, uuid.Nil, ""),
	); !errors.Is(err, ErrMediaSignalVersionConflict) {
		t.Fatalf("post-retention same-key signal error = %v, want version conflict", err)
	}
}

func assertP406ReactionCrossInstanceRateMatrix(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	first *MediaSignalService,
	second *MediaSignalService,
	fixture p406SignalFixture,
	snapshot MediaParticipantSnapshot,
) MediaParticipantSnapshot {
	t.Helper()
	coTeacher := fixture.access["co_teacher"]
	minuteNow := p406DatabaseTimeWithWindowHeadroom(t, ctx, pool, time.Minute, 4*time.Second)
	p406SetRateLimitCount(t, ctx, pool, mediaReactionBurstActorPurpose,
		p406ActorRateBucket(fixture, coTeacher), 5*time.Second, minuteNow, 0)
	p406SetRateLimitCount(t, ctx, pool, mediaReactionMinuteActorPurpose,
		p406ActorRateBucket(fixture, coTeacher), time.Minute, minuteNow, 19)
	p406SetRateLimitCount(t, ctx, pool, mediaReactionRoomPurpose,
		p406RoomRateBucket(fixture), 5*time.Second, minuteNow, 0)

	minuteAccepted := sendP406Signal(
		t, ctx, first, fixture, coTeacher, snapshot,
		mediaIntegrationKey("p406-minute-twentieth"), MediaSignalReaction,
		uuid.Nil, MediaReactionSurprised,
	)
	p406AssertRateLimitCount(t, ctx, pool, mediaReactionMinuteActorPurpose,
		p406ActorRateBucket(fixture, coTeacher), minuteNow.Truncate(time.Minute), 20)
	p406SetRateLimitCount(t, ctx, pool, mediaReactionBurstActorPurpose,
		p406ActorRateBucket(fixture, coTeacher), 5*time.Second, minuteNow, 0)
	_, minuteErr := second.SendSignal(
		ctx, coTeacher, fixture.spaceID,
		p406SignalInput(
			fixture, minuteAccepted, mediaIntegrationKey("p406-minute-twenty-first"),
			MediaSignalReaction, uuid.Nil, MediaReactionSurprised,
		),
	)
	var minuteLimited *MediaSignalRateLimitError
	if !errors.As(minuteErr, &minuteLimited) || minuteLimited.RetryAfter < time.Second {
		t.Fatalf("cross-instance actor 20/60 reaction error = %v, want bounded rate limit", minuteErr)
	}

	roomNow := p406DatabaseTimeWithWindowHeadroom(t, ctx, pool, 5*time.Second, 4*time.Second)
	student := fixture.access["student"]
	for offset := 0; offset < 3; offset++ {
		p406SetRateLimitCount(
			t, ctx, pool, mediaReactionRoomPurpose, p406RoomRateBucket(fixture),
			5*time.Second, roomNow.Add(time.Duration(offset)*5*time.Second), 100,
		)
	}
	_, roomErr := second.SendSignal(
		ctx, student, fixture.spaceID,
		p406SignalInput(
			fixture, minuteAccepted, mediaIntegrationKey("p406-room-hundred-first"),
			MediaSignalReaction, uuid.Nil, MediaReactionCelebrate,
		),
	)
	var roomLimited *MediaSignalRateLimitError
	if !errors.As(roomErr, &roomLimited) || roomLimited.RetryAfter < time.Second {
		t.Fatalf("cross-instance room 100/5 reaction error = %v, want bounded rate limit", roomErr)
	}

	cleanupP406SignalRateLimits(t, pool, fixture)
	return minuteAccepted
}

func p406ActorRateBucket(fixture p406SignalFixture, access AccessContext) string {
	return access.TenantID.String() + "\x00" + fixture.roomID.String() + "\x00" + access.ActorID.String()
}

func p406RoomRateBucket(fixture p406SignalFixture) string {
	return fixture.access["owner"].TenantID.String() + "\x00" + fixture.roomID.String()
}

func p406RateBucketHash(purpose string, bucket string) []byte {
	digest := sha256.Sum256([]byte(purpose + "\x00" + bucket))
	return digest[:]
}

func p406DatabaseTimeWithWindowHeadroom(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	window time.Duration,
	minimum time.Duration,
) time.Time {
	t.Helper()
	for {
		var now time.Time
		if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatalf("read P4-06 database rate-limit time: %v", err)
		}
		remaining := now.Truncate(window).Add(window).Sub(now)
		if remaining >= minimum {
			return now
		}
		select {
		case <-ctx.Done():
			t.Fatal("wait for stable P4-06 rate-limit window")
		case <-time.After(remaining + 100*time.Millisecond):
		}
	}
}

func p406SetRateLimitCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	purpose string,
	bucket string,
	window time.Duration,
	acceptedAt time.Time,
	used int64,
) {
	t.Helper()
	windowStart := acceptedAt.Truncate(window)
	if _, err := pool.Exec(ctx, `INSERT INTO tutorhub.rate_limit_windows (
    purpose, bucket_hash, window_started_at, window_ends_at, used_count, updated_at
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (purpose, bucket_hash, window_started_at)
DO UPDATE SET window_ends_at = EXCLUDED.window_ends_at,
              used_count = EXCLUDED.used_count,
              updated_at = EXCLUDED.updated_at`,
		purpose, p406RateBucketHash(purpose, bucket), windowStart, windowStart.Add(window),
		used, acceptedAt,
	); err != nil {
		t.Fatalf("set P4-06 rate-limit evidence row: %v", err)
	}
}

func p406AssertRateLimitCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	purpose string,
	bucket string,
	windowStart time.Time,
	want int64,
) {
	t.Helper()
	var used int64
	if err := pool.QueryRow(ctx, `SELECT used_count
FROM tutorhub.rate_limit_windows
WHERE purpose = $1 AND bucket_hash = $2 AND window_started_at = $3`,
		purpose, p406RateBucketHash(purpose, bucket), windowStart,
	).Scan(&used); err != nil {
		t.Fatalf("inspect P4-06 rate-limit evidence row: %v", err)
	}
	if used != want {
		t.Fatalf("P4-06 rate-limit count = %d, want %d", used, want)
	}
}

func cleanupP406SignalRateLimits(
	t *testing.T,
	pool *pgxpool.Pool,
	fixture p406SignalFixture,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type target struct {
		purpose string
		bucket  string
	}
	targets := []target{{mediaReactionRoomPurpose, p406RoomRateBucket(fixture)}}
	for _, access := range fixture.access {
		bucket := p406ActorRateBucket(fixture, access)
		targets = append(targets,
			target{mediaReactionBurstActorPurpose, bucket},
			target{mediaReactionMinuteActorPurpose, bucket},
			target{mediaHandActorRatePurpose, bucket},
			target{mediaHandLowerAllRatePurpose, bucket},
		)
	}
	targets = append(targets, target{mediaHandRoomRatePurpose, p406RoomRateBucket(fixture)})
	for _, target := range targets {
		if _, err := pool.Exec(ctx, `DELETE FROM tutorhub.rate_limit_windows
WHERE purpose = $1 AND bucket_hash = $2`,
			target.purpose, p406RateBucketHash(target.purpose, target.bucket),
		); err != nil {
			t.Errorf("delete P4-06 rate-limit evidence: %v", err)
		}
	}
}

func assertP406ParticipantAndRoomTerminalHandCleanup(
	t *testing.T,
	ctx context.Context,
	migrationPool *pgxpool.Pool,
	runtimePool *pgxpool.Pool,
	fixture p406SignalFixture,
) {
	t.Helper()
	seedHand := func(name string, sequence int64) {
		t.Helper()
		if _, err := migrationPool.Exec(ctx, `INSERT INTO tutorhub.media_participant_hand_states (
    tenant_id, space_id, room_instance_id, participant_session_id,
    signal_sequence, raised_at
) VALUES ($1, $2, $3, $4, $5, clock_timestamp())
ON CONFLICT (tenant_id, room_instance_id, participant_session_id)
DO UPDATE SET is_raised = true,
              signal_sequence = EXCLUDED.signal_sequence,
              raised_at = EXCLUDED.raised_at`,
			fixture.access[name].TenantID, fixture.spaceID, fixture.roomID,
			fixture.participantID[name], sequence,
		); err != nil {
			t.Fatalf("seed P4-06 %s hand cleanup state: %v", name, err)
		}
	}
	assertParticipant := func(name string, wantStatus string, wantRaised bool) {
		t.Helper()
		var status string
		var raised bool
		if err := migrationPool.QueryRow(ctx, `SELECT participant.status,
       COALESCE(hand.is_raised, false)
FROM tutorhub.media_participant_sessions AS participant
LEFT JOIN tutorhub.media_participant_hand_states AS hand
  ON hand.tenant_id = participant.tenant_id
 AND hand.room_instance_id = participant.room_instance_id
 AND hand.participant_session_id = participant.id
WHERE participant.tenant_id = $1 AND participant.room_instance_id = $2
  AND participant.id = $3`, fixture.access[name].TenantID, fixture.roomID,
			fixture.participantID[name]).Scan(&status, &raised); err != nil {
			t.Fatalf("inspect P4-06 %s hand cleanup state: %v", name, err)
		}
		if status != wantStatus || raised != wantRaised {
			t.Fatalf("P4-06 %s cleanup = status:%s raised:%t, want %s/%t",
				name, status, raised, wantStatus, wantRaised)
		}
	}

	seedHand("student", 1001)
	leftTx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin P4-06 participant-left cleanup: %v", err)
	}
	defer func() { _ = leftTx.Rollback(context.Background()) }()
	leftStatus, leftMutation := classifyParticipantWebhook(
		providerRoomBinding{
			TenantID: fixture.access["student"].TenantID, SpaceID: fixture.spaceID,
			RoomInstanceID: fixture.roomID, Status: RoomInstanceActive,
		},
		&participantRow{
			ID: fixture.participantID["student"], Status: "connected", Version: 3,
			CreatedAt: time.Now().UTC().Add(-time.Minute), UpdatedAt: time.Now().UTC().Add(-time.Minute),
		},
		WebhookEvent{EventType: "participant_left", ParticipantIdentity: "opaque"},
		time.Now().UTC(),
	)
	if leftStatus != "applied" || leftMutation == nil {
		t.Fatal("P4-06 participant-left cleanup was not classified as applied")
	}
	if err := leftMutation(ctx, leftTx); err != nil {
		t.Fatalf("apply P4-06 participant-left cleanup: %v", err)
	}
	if err := leftTx.Commit(ctx); err != nil {
		t.Fatalf("commit P4-06 participant-left cleanup: %v", err)
	}
	assertParticipant("student", "left", false)

	seedHand("co_teacher", 1002)
	removedTx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin P4-06 participant-removed cleanup: %v", err)
	}
	defer func() { _ = removedTx.Rollback(context.Background()) }()
	if err := removeLobbyMemberParticipants(
		ctx, removedTx,
		tenancy.Context{
			TenantID: fixture.access["co_teacher"].TenantID,
			ActorID:  fixture.access["owner"].ActorID,
		},
		fixture.spaceID, fixture.roomID, fixture.access["co_teacher"].ActorID,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("apply P4-06 participant-removed cleanup: %v", err)
	}
	if err := removedTx.Commit(ctx); err != nil {
		t.Fatalf("commit P4-06 participant-removed cleanup: %v", err)
	}
	assertParticipant("co_teacher", "removed", false)

	seedHand("owner", 1003)
	terminalTx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin P4-06 room-terminal cleanup: %v", err)
	}
	defer func() { _ = terminalTx.Rollback(context.Background()) }()
	if err := terminateRoomParticipants(
		ctx, terminalTx, fixture.access["owner"].TenantID,
		fixture.spaceID, fixture.roomID, time.Now().UTC(),
	); err != nil {
		t.Fatalf("apply P4-06 room-terminal cleanup: %v", err)
	}
	if err := terminalTx.Commit(ctx); err != nil {
		t.Fatalf("commit P4-06 room-terminal cleanup: %v", err)
	}
	assertParticipant("owner", "left", false)
	var activeHands, activeParticipants int
	if err := migrationPool.QueryRow(ctx, `SELECT
    (SELECT count(*) FROM tutorhub.media_participant_hand_states
      WHERE tenant_id = $1 AND room_instance_id = $2 AND is_raised),
    (SELECT count(*) FROM tutorhub.media_participant_sessions
      WHERE tenant_id = $1 AND room_instance_id = $2
        AND status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting'))`,
		fixture.access["owner"].TenantID, fixture.roomID,
	).Scan(&activeHands, &activeParticipants); err != nil {
		t.Fatalf("inspect P4-06 room-terminal cleanup: %v", err)
	}
	if activeHands != 0 || activeParticipants != 0 {
		t.Fatalf("P4-06 room-terminal cleanup retained hands/participants = %d/%d",
			activeHands, activeParticipants)
	}
}

func seedP406SignalFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	base mediaIntegrationFixture) p406SignalFixture {
	t.Helper()
	fixture := p406SignalFixture{
		spaceID: uuid.New(), roomID: uuid.New(), access: map[string]AccessContext{},
		keys: map[string]uuid.UUID{"owner": uuid.New(), "student": uuid.New(), "co_teacher": uuid.New()},
		participantID: map[string]uuid.UUID{
			"owner": uuid.New(), "student": uuid.New(), "co_teacher": uuid.New(),
		},
	}
	fixture.access["owner"] = p402IntegrationAccess(base.tenantID, base.ownerID,
		policy.OrganizationRoleStudent, uuid.New())
	fixture.access["student"] = p402IntegrationAccess(base.tenantID, base.studentID,
		policy.OrganizationRoleStudent, uuid.New())
	fixture.access["co_teacher"] = p402IntegrationAccess(base.tenantID, base.coTeacherID,
		policy.OrganizationRoleStudent, uuid.New())
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `UPDATE tutorhub.class_sessions
SET status='live', version=version+1, updated_by=$3, updated_at=$4
WHERE tenant_id=$1 AND id=$2`, base.tenantID, base.sessionIDs["owner"], base.ownerID, now); err != nil {
		t.Fatalf("activate P4-06 source session: %v", err)
	}
	_, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_spaces (
    id, tenant_id, source_kind, class_id, source_class_session_id, status, version,
    lobby_enabled, locked, create_idempotency_key, create_request_fingerprint,
    created_by, updated_by, opened_at, opened_by, created_at, updated_at
) VALUES ($1,$2,'class_session',$3,$4,'open',2,false,false,$5,$6,$7,$7,$8,$7,$8,$8)`,
		fixture.spaceID, base.tenantID, base.classID, base.sessionIDs["owner"],
		mediaIntegrationKey("p406-space"), make([]byte, 32), base.ownerID, now)
	if err != nil {
		t.Fatalf("insert P4-06 signal space: %v", err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO tutorhub.media_room_instances (
    id, tenant_id, space_id, attempt_number, status, version, provider_room_name,
    provider_room_sid, created_by, updated_by, activated_at, created_at, updated_at,
    projection_version, last_signal_sequence, next_roster_sequence
) VALUES ($1,$2,$3,1,'active',2,$4,$5,$6,$6,$7,$7,$7,1,0,3)`,
		fixture.roomID, base.tenantID, fixture.spaceID,
		"p406room_"+strings.ReplaceAll(uuid.NewString(), "-", ""),
		"RM_"+strings.ReplaceAll(uuid.NewString(), "-", ""), base.ownerID, now)
	if err != nil {
		t.Fatalf("insert P4-06 active room: %v", err)
	}
	participants := []struct {
		name string
		user uuid.UUID
		role InstanceRole
	}{{"owner", base.ownerID, InstanceRoleHost}, {"student", base.studentID, InstanceRoleAttendee},
		{"co_teacher", base.coTeacherID, InstanceRoleCoHost}}
	for index, participant := range participants {
		_, err = pool.Exec(ctx, `INSERT INTO tutorhub.media_participant_sessions (
    id, tenant_id, space_id, room_instance_id, user_id, join_attempt_id,
    provider_participant_identity, instance_role, status, capacity_reserved, version,
    admitted_at, joining_at, connected_at, participant_key, roster_sequence,
    created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'connected',true,3,$9,$9,$9,$10,$11,$9,$9)`,
			fixture.participantID[participant.name], base.tenantID, fixture.spaceID, fixture.roomID, participant.user,
			uuid.New(), "p_"+strings.ReplaceAll(uuid.NewString(), "-", ""), participant.role,
			now, fixture.keys[participant.name], index+1)
		if err != nil {
			t.Fatalf("insert P4-06 %s participant: %v", participant.name, err)
		}
	}
	return fixture
}

func newP406SignalService(t *testing.T, pool *pgxpool.Pool) *MediaSignalService {
	t.Helper()
	lifecycle, _ := newMediaIntegrationServices(t, pool)
	repository, ok := lifecycle.repository.(*PostgresLifecycleRepository)
	if !ok {
		t.Fatal("media lifecycle repository is not PostgreSQL")
	}
	repository.queryTimeout = 45 * time.Second
	signals, err := NewPostgresMediaSignalRepository(repository, uuid.New)
	if err != nil {
		t.Fatalf("create P4-06 PostgreSQL signal repository: %v", err)
	}
	service, err := NewMediaSignalService(signals)
	if err != nil {
		t.Fatalf("create P4-06 signal service: %v", err)
	}
	return service
}

func getP406Snapshot(t *testing.T, ctx context.Context, service *MediaSignalService,
	fixture p406SignalFixture, access AccessContext) MediaParticipantSnapshot {
	t.Helper()
	snapshot, err := service.GetParticipantSnapshot(ctx, access, fixture.spaceID,
		GetMediaParticipantSnapshotInput{ExpectedSpaceVersion: 2,
			ExpectedRoomInstanceID: fixture.roomID, ExpectedRoomInstanceVersion: 2})
	if err != nil {
		t.Fatalf("get P4-06 participant snapshot: %v", err)
	}
	return snapshot
}

func p406SnapshotInput(snapshot MediaParticipantSnapshot,
	fixture p406SignalFixture) GetMediaParticipantSnapshotInput {
	return GetMediaParticipantSnapshotInput{ExpectedSpaceVersion: 2,
		ExpectedRoomInstanceID: fixture.roomID, ExpectedRoomInstanceVersion: 2}
}

func p406SignalInput(fixture p406SignalFixture, snapshot MediaParticipantSnapshot, key string,
	kind MediaSignalKind, target uuid.UUID, reaction MediaReaction) SendMediaSignalInput {
	return SendMediaSignalInput{ExpectedSpaceVersion: 2, ExpectedRoomInstanceID: fixture.roomID,
		ExpectedRoomInstanceVersion: 2, ExpectedProjectionVersion: snapshot.ProjectionVersion,
		IdempotencyKey: key, Kind: kind, TargetParticipantKey: target, Reaction: reaction}
}

func sendP406Signal(t *testing.T, ctx context.Context, service *MediaSignalService,
	fixture p406SignalFixture, access AccessContext, snapshot MediaParticipantSnapshot, key string,
	kind MediaSignalKind, target uuid.UUID, reaction MediaReaction) MediaParticipantSnapshot {
	t.Helper()
	result, err := service.SendSignal(ctx, access, fixture.spaceID,
		p406SignalInput(fixture, snapshot, key, kind, target, reaction))
	if err != nil {
		t.Fatalf("send P4-06 %s signal: %v", kind, err)
	}
	return result
}

func sendP406SignalConcurrently(t *testing.T, ctx context.Context, service *MediaSignalService,
	fixture p406SignalFixture, access AccessContext, snapshot MediaParticipantSnapshot, key string,
	kind MediaSignalKind, target uuid.UUID, reaction MediaReaction) MediaParticipantSnapshot {
	t.Helper()
	type outcome struct {
		snapshot MediaParticipantSnapshot
		err      error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			value, err := service.SendSignal(ctx, access, fixture.spaceID,
				p406SignalInput(fixture, snapshot, key, kind, target, reaction))
			results <- outcome{snapshot: value, err: err}
		}()
	}
	close(start)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent same-key %s signal errors = %v / %v", kind, first.err, second.err)
	}
	if first.snapshot.ProjectionVersion != second.snapshot.ProjectionVersion ||
		first.snapshot.LastSignalSequence != second.snapshot.LastSignalSequence {
		t.Fatalf("concurrent same-key %s signal diverged", kind)
	}
	return first.snapshot
}

func assertP406ParticipantKeyConstraint(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	base mediaIntegrationFixture, fixture p406SignalFixture) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `INSERT INTO tutorhub.media_participant_sessions (
    id, tenant_id, space_id, room_instance_id, user_id, join_attempt_id,
    provider_participant_identity, instance_role, status, capacity_reserved, version,
    admitted_at, joining_at, connected_at, participant_key, roster_sequence, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,'attendee','connected',true,3,$8,$8,$8,$9,99,$8,$8)`,
		uuid.New(), base.tenantID, fixture.spaceID, fixture.roomID, base.outsiderID, uuid.New(),
		"p_"+strings.ReplaceAll(uuid.NewString(), "-", ""), now, fixture.keys["student"])
	if err == nil {
		t.Fatal("duplicate room participant_key unexpectedly inserted")
	}
}

func assertP406ReactionTTLAndGrouping(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	service *MediaSignalService, fixture p406SignalFixture, snapshot MediaParticipantSnapshot) {
	t.Helper()
	var invalidTTL int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tutorhub.media_reaction_events
WHERE tenant_id=$1 AND room_instance_id=$2 AND reaction='clap'
  AND expires_at <> accepted_at + interval '10 seconds'`, fixture.access["student"].TenantID,
		fixture.roomID).Scan(&invalidTTL); err != nil || invalidTTL != 0 {
		t.Fatalf("reaction TTL exactness = invalid:%d error:%v", invalidTTL, err)
	}
	_, err := pool.Exec(ctx, `WITH ordered AS (
    SELECT id, row_number() OVER (ORDER BY signal_sequence) AS position
    FROM tutorhub.media_reaction_events
    WHERE tenant_id=$1 AND room_instance_id=$2 AND reaction='clap'
), anchor AS (SELECT clock_timestamp() AS accepted_at)
UPDATE tutorhub.media_reaction_events AS reaction
SET accepted_at=anchor.accepted_at + (ordered.position-1) * interval '100 milliseconds',
    expires_at=anchor.accepted_at + (ordered.position-1) * interval '100 milliseconds' + interval '10 seconds'
FROM ordered, anchor WHERE reaction.id=ordered.id`, fixture.access["student"].TenantID, fixture.roomID)
	if err != nil {
		t.Fatalf("normalize reaction grouping times: %v", err)
	}
	grouped := getP406Snapshot(t, ctx, service, fixture, fixture.access["student"])
	found := false
	for _, cluster := range grouped.ReactionClusters {
		if cluster.Reaction == MediaReactionClap && cluster.Count == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("two bounded clap events were not grouped: %+v", grouped.ReactionClusters)
	}
	_, err = pool.Exec(ctx, `WITH anchor AS (SELECT clock_timestamp() AS current_time)
UPDATE tutorhub.media_reaction_events
SET accepted_at=anchor.current_time-interval '20 seconds',
    expires_at=anchor.current_time-interval '10 seconds'
FROM anchor
WHERE tenant_id=$1 AND room_instance_id=$2 AND reaction='clap'`,
		fixture.access["student"].TenantID, fixture.roomID)
	if err != nil {
		t.Fatalf("expire reaction fixtures: %v", err)
	}
	expired := getP406Snapshot(t, ctx, service, fixture, fixture.access["student"])
	for _, cluster := range expired.ReactionClusters {
		if cluster.Reaction == MediaReactionClap {
			t.Fatalf("expired clap remained visible: %+v", cluster)
		}
	}
	_ = snapshot
}

func assertP406AcceptedTimeAfterRoomWait(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	service *MediaSignalService, fixture p406SignalFixture,
	snapshot MediaParticipantSnapshot) MediaParticipantSnapshot {
	t.Helper()
	blocker, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin P4-06 room blocker: %v", err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(ctx, `SELECT id FROM tutorhub.media_room_instances
WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, fixture.access["owner"].TenantID, fixture.roomID); err != nil {
		t.Fatalf("lock P4-06 room: %v", err)
	}
	type outcome struct {
		snapshot MediaParticipantSnapshot
		err      error
	}
	result := make(chan outcome, 1)
	key := mediaIntegrationKey("p406-database-time-after-wait")
	go func() {
		value, sendErr := service.SendSignal(ctx, fixture.access["owner"], fixture.spaceID,
			p406SignalInput(fixture, snapshot, key, MediaSignalHandRaise, uuid.Nil, ""))
		result <- outcome{snapshot: value, err: sendErr}
	}()
	time.Sleep(100 * time.Millisecond)
	var releasedAt time.Time
	if err := blocker.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&releasedAt); err != nil {
		t.Fatalf("read blocker release database time: %v", err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release P4-06 room blocker: %v", err)
	}
	completed := <-result
	if completed.err != nil {
		t.Fatalf("signal after room-lock wait: %v", completed.err)
	}
	var acceptedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM tutorhub.media_signal_mutation_receipts
WHERE tenant_id=$1 AND room_instance_id=$2 AND actor_user_id=$3 AND idempotency_key=$4`,
		fixture.access["owner"].TenantID, fixture.roomID, fixture.access["owner"].ActorID,
		key).Scan(&acceptedAt); err != nil {
		t.Fatalf("read waited signal accepted time: %v", err)
	}
	if acceptedAt.Before(releasedAt) {
		t.Fatalf("signal accepted_at preceded released DB lock: accepted=%s released=%s",
			acceptedAt.UTC().Format(time.RFC3339Nano), releasedAt.UTC().Format(time.RFC3339Nano))
	}
	return completed.snapshot
}

func assertP406SnapshotSignalConcurrency(t *testing.T, ctx context.Context,
	service *MediaSignalService, fixture p406SignalFixture, snapshot MediaParticipantSnapshot) {
	t.Helper()
	const readers = 6
	start := make(chan struct{})
	errorsSeen := make(chan error, readers+1)
	var wait sync.WaitGroup
	for index := 0; index < readers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := service.GetParticipantSnapshot(ctx, fixture.access["student"], fixture.spaceID,
				p406SnapshotInput(snapshot, fixture))
			errorsSeen <- err
		}()
	}
	wait.Add(1)
	go func() {
		defer wait.Done()
		<-start
		_, err := service.SendSignal(ctx, fixture.access["owner"], fixture.spaceID,
			p406SignalInput(fixture, snapshot, mediaIntegrationKey("p406-lock-concurrency"),
				MediaSignalReaction, uuid.Nil, MediaReactionCelebrate))
		errorsSeen <- err
	}()
	close(start)
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("shared snapshot/signal lock concurrency failed: %v", err)
		}
	}
}
