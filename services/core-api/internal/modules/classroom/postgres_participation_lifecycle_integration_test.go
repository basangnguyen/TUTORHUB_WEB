//go:build integration

package classroom

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type cancellationLifecycleFixture struct {
	TenantID        uuid.UUID
	ClassID         uuid.UUID
	Source          ParticipationSourceRef
	ICalUID         string
	InitialSequence int64
	AttendeeID      uuid.UUID
	CapabilityID    uuid.UUID
}

func TestPostgresFollowingCancellationSnapshotsAndReplay(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	version, err := migrationrunner.CurrentVersion(ctx, migrationURL)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version.Number < 21 || version.Dirty {
		t.Fatalf("unexpected migration version: %+v", version)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create following cancellation integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin following cancellation fixture: %v", err)
	}
	defer func() { _ = setup.Rollback(context.Background()) }()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "following-cancellation")
	classID := uuid.New()
	if _, err := setup.Exec(
		ctx,
		`INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES ($1, $2, $3, $4, 'Following cancellation integration class',
        'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		"FC"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert following cancellation class: %v", err)
	}
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit following cancellation fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID)

	protector := newCancellationLifecycleProtector(t)
	repository := NewPostgresRepository(
		pool,
		30*time.Second,
		policy.NewEngine(),
	).WithCalendarProtectedData(protector)
	tenantContext := mustTenantContext(t, tenantID, ownerID)
	startsAt := time.Date(2026, 10, 5, 2, 0, 0, 0, time.UTC)
	location := mustLocation("Asia/Ho_Chi_Minh")
	createParams, _, err := normalizeCreateSeriesInput(
		ctx,
		CreateSeriesInput{
			Title:       "Following cancellation",
			Description: "Keep the prefix and cancel the suffix",
			StartsAt:    startsAt.In(location).Format(time.RFC3339),
			EndsAt:      startsAt.Add(time.Hour).In(location).Format(time.RFC3339),
			Timezone:    "Asia/Ho_Chi_Minh",
			Rule: recurrence.Rule{
				Frequency: recurrence.FrequencyDaily,
				Interval:  1,
				End: recurrence.End{
					Type:  recurrence.EndAfterCount,
					Count: 3,
				},
			},
			OverlapPolicy: recurrence.OverlapReject,
		},
		ownerID,
	)
	if err != nil {
		t.Fatalf("normalize following cancellation series: %v", err)
	}
	series, err := repository.CreateSeries(
		ctx,
		tenantContext,
		classID,
		createParams,
		startsAt.Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("create following cancellation series: %v", err)
	}
	occurrences, err := expandCompleteSeries(ctx, definitionFromSeries(series))
	if err != nil || len(occurrences) != 3 {
		t.Fatalf("expand following cancellation series: count=%d err=%v", len(occurrences), err)
	}

	masterFixture := seedCancellationLifecycleFixture(
		t,
		ctx,
		pool,
		protector,
		tenantID,
		classID,
		ownerID,
		SeriesParticipationSource(series.ID),
		series.Version,
		series.Sequence,
		startsAt.Add(-30*time.Minute),
	)
	overrideFixture := seedCancellationLifecycleFixture(
		t,
		ctx,
		pool,
		protector,
		tenantID,
		classID,
		ownerID,
		OccurrenceParticipationSource(series.ID, occurrences[1].Key),
		series.Version,
		series.Sequence,
		startsAt.Add(-30*time.Minute),
	)
	params, err := normalizeSeriesMutationInput(
		OccurrenceMutationInput{
			Scope:           recurrence.ScopeFollowing,
			OccurrenceKey:   occurrences[1].Key,
			ExpectedVersion: series.Version,
			IdempotencyKey:  "series-following-cancel-0001",
		},
		ownerID,
		"cancel",
	)
	if err != nil {
		t.Fatalf("normalize following cancellation: %v", err)
	}
	cancelledAt := startsAt.Add(-15 * time.Minute)
	cancelled, err := repository.CancelSeriesOccurrence(
		ctx,
		tenantContext,
		classID,
		series.ID,
		params,
		cancelledAt,
	)
	if err != nil {
		t.Fatalf("cancel following occurrences: %v", err)
	}
	if cancelled.Replay ||
		cancelled.Series.Status != SeriesStatusScheduled ||
		cancelled.Series.Version != series.Version+1 ||
		cancelled.Series.Sequence != series.Sequence+1 ||
		cancelled.Series.Rule.End.Type != recurrence.EndAfterCount ||
		cancelled.Series.Rule.End.Count != 1 {
		t.Fatalf("unexpected following cancellation result: %+v", cancelled)
	}
	assertOpenCancellationLifecycle(t, ctx, pool, masterFixture)
	assertInvitationLifecycleSnapshot(
		t,
		ctx,
		pool,
		masterFixture,
		"updated",
		"series_following_cancelled",
		cancelled.Series.Sequence,
	)
	assertCancellationLifecycle(
		t,
		ctx,
		pool,
		overrideFixture,
		"series_following_cancelled",
		2,
		cancelled.Series.Sequence,
	)

	replayed, err := repository.CancelSeriesOccurrence(
		ctx,
		tenantContext,
		classID,
		series.ID,
		params,
		cancelledAt.Add(time.Minute),
	)
	if err != nil || !replayed.Replay ||
		replayed.Series.Version != cancelled.Series.Version ||
		replayed.Series.Sequence != cancelled.Series.Sequence {
		t.Fatalf("replay following cancellation: result=%+v err=%v", replayed, err)
	}
	assertInvitationLifecycleSnapshot(
		t,
		ctx,
		pool,
		masterFixture,
		"updated",
		"series_following_cancelled",
		cancelled.Series.Sequence,
	)
	assertCancellationLifecycle(
		t,
		ctx,
		pool,
		overrideFixture,
		"series_following_cancelled",
		2,
		cancelled.Series.Sequence,
	)
}

func newCancellationLifecycleProtector(t *testing.T) *protecteddata.Protector {
	t.Helper()
	protector, err := protecteddata.New(protecteddata.Config{
		Key:        bytes.Repeat([]byte{0x7d}, 32),
		KeyVersion: 11,
	})
	if err != nil {
		t.Fatalf("create cancellation lifecycle protector: %v", err)
	}
	return protector
}

func seedCancellationLifecycleFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	protector *protecteddata.Protector,
	tenantID uuid.UUID,
	classID uuid.UUID,
	ownerID uuid.UUID,
	source ParticipationSourceRef,
	sourceVersion int64,
	sequence int64,
	createdAt time.Time,
) cancellationLifecycleFixture {
	t.Helper()
	if err := source.Validate(); err != nil {
		t.Fatalf("validate cancellation lifecycle source: %v", err)
	}
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	externalRecipientID := uuid.New()
	email := "calendar-cancel-" + uuid.NewString() + "@example.test"
	displayName := "Calendar cancellation recipient"
	recipientFingerprint, err := protector.DeliveryAddressFingerprint(
		tenantID.String(),
		[]byte(email),
	)
	if err != nil {
		t.Fatalf("fingerprint cancellation external recipient: %v", err)
	}
	sealedExternalAddress, err := protector.Seal(protecteddata.Context{
		TenantID: tenantID.String(),
		Purpose:  protecteddata.PurposeInvitationRecipientAddress,
		RecordID: externalRecipientID.String(),
	}, []byte(email))
	if err != nil {
		t.Fatalf("protect cancellation external address: %v", err)
	}
	sealedExternalName, err := protector.Seal(protecteddata.Context{
		TenantID: tenantID.String(),
		Purpose:  protecteddata.PurposeInvitationRecipientDisplayName,
		RecordID: externalRecipientID.String(),
	}, []byte(displayName))
	if err != nil {
		t.Fatalf("protect cancellation external display name: %v", err)
	}
	tokenDigest := sha256.Sum256([]byte(uuid.NewString()))

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_external_recipients (
    id, tenant_id, delivery_address_fingerprint, delivery_address_ciphertext,
    display_name_ciphertext, crypto_key_version, created_by, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
		externalRecipientID,
		tenantID,
		recipientFingerprint[:],
		sealedExternalAddress.Ciphertext,
		sealedExternalName.Ciphertext,
		sealedExternalAddress.KeyVersion,
		ownerID,
		createdAt.UTC(),
	); err != nil {
		t.Fatalf("insert cancellation external recipient: %v", err)
	}

	var attendeeID uuid.UUID
	if err := pool.QueryRow(
		ctx,
		`INSERT INTO tutorhub.class_session_attendees (
    tenant_id, class_id, session_id, series_id, occurrence_key,
    external_recipient_id, participation_role, business_role,
    audience_source, response_requested, status, rsvp_state, rsvp_source,
    created_by, updated_by, created_at, updated_at
)
VALUES (
    $1, $2, $3, $4, $5,
    $6, 'required', 'external_guest',
    'manual', true, 'active', 'needs_action', 'none',
    $7, $7, $8, $8
)
RETURNING id`,
		tenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
		externalRecipientID,
		ownerID,
		createdAt.UTC(),
	).Scan(&attendeeID); err != nil {
		t.Fatalf("insert cancellation attendee: %v", err)
	}
	switch source.Kind {
	case ParticipationSourceSession:
		if _, err := pool.Exec(
			ctx,
			`UPDATE tutorhub.class_sessions
SET audience_revision = GREATEST(audience_revision, 1),
    response_requested = true
WHERE tenant_id = $1 AND class_id = $2 AND id = $3`,
			tenantID,
			classID,
			source.SessionID,
		); err != nil {
			t.Fatalf("publish cancellation session audience: %v", err)
		}
	case ParticipationSourceSeries:
		if _, err := pool.Exec(
			ctx,
			`UPDATE tutorhub.class_session_series
SET audience_revision = GREATEST(audience_revision, 1),
    response_requested = true
WHERE tenant_id = $1 AND class_id = $2 AND id = $3`,
			tenantID,
			classID,
			source.SeriesID,
		); err != nil {
			t.Fatalf("publish cancellation series audience: %v", err)
		}
	}

	aggregateID := source.SessionID
	if aggregateID == uuid.Nil {
		aggregateID = source.SeriesID
	}
	revisionID := uuid.New()
	canonicalPayload := []byte(`{"response_requested":true}`)
	sealedPayload, err := protector.Seal(protecteddata.Context{
		TenantID: tenantID.String(),
		Purpose:  protecteddata.PurposeInvitationCanonicalPayload,
		RecordID: revisionID.String(),
	}, canonicalPayload)
	if err != nil {
		t.Fatalf("protect cancellation invitation payload: %v", err)
	}
	payloadDigest := sha256.Sum256(canonicalPayload)
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_invitation_revisions (
    id, tenant_id, class_id, session_id, series_id, occurrence_key,
    source_version, audience_revision, ical_uid, ical_sequence,
    method, lifecycle, organizer_user_id, actor_type, created_by,
    reason_code, timezone_data_version, canonical_payload_ciphertext,
    canonical_payload_sha256, crypto_key_version, created_at
)
VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, 1, $8, $9,
    NULL, 'published', $10, 'user', $10,
    'lifecycle_test', 'integration-test', $11,
    $12, $13, $14
)`,
		revisionID,
		tenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
		sourceVersion,
		"urn:uuid:"+aggregateID.String(),
		sequence,
		ownerID,
		sealedPayload.Ciphertext,
		payloadDigest[:],
		sealedPayload.KeyVersion,
		createdAt.UTC(),
	); err != nil {
		t.Fatalf("insert cancellation invitation revision: %v", err)
	}

	invitationRecipientID := uuid.New()
	sealedSnapshotAddress, err := protector.Seal(protecteddata.Context{
		TenantID: tenantID.String(),
		Purpose:  protecteddata.PurposeInvitationRecipientAddress,
		RecordID: invitationRecipientID.String(),
	}, []byte(email))
	if err != nil {
		t.Fatalf("protect cancellation invitation recipient address: %v", err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_invitation_recipients (
    id, tenant_id, class_id, invitation_revision_id, attendee_id,
    recipient_kind, participation_role, business_role, audience_source,
    response_requested, locale, viewer_timezone, rsvp_state, rsvp_source,
    delivery_address_fingerprint, delivery_address_ciphertext,
    crypto_key_version, created_at
)
VALUES (
    $1, $2, $3, $4, $5,
    'external', 'required', 'external_guest', 'manual',
    true, 'vi-VN', 'Asia/Ho_Chi_Minh', 'needs_action', 'none',
    $6, $7, $8, $9
)`,
		invitationRecipientID,
		tenantID,
		classID,
		revisionID,
		attendeeID,
		recipientFingerprint[:],
		sealedSnapshotAddress.Ciphertext,
		sealedSnapshotAddress.KeyVersion,
		createdAt.UTC(),
	); err != nil {
		t.Fatalf("insert cancellation invitation recipient: %v", err)
	}

	var capabilityID uuid.UUID
	if err := pool.QueryRow(
		ctx,
		`INSERT INTO tutorhub.calendar_rsvp_capabilities (
    tenant_id, invitation_revision_id, invitation_recipient_id,
    purpose, token_version, token_digest, issued_at, expires_at
)
VALUES ($1, $2, $3, 'respond', 1, $4, $5, $6)
RETURNING id`,
		tenantID,
		revisionID,
		invitationRecipientID,
		tokenDigest[:],
		createdAt.UTC(),
		createdAt.UTC().Add(24*time.Hour),
	).Scan(&capabilityID); err != nil {
		t.Fatalf("insert cancellation RSVP capability: %v", err)
	}
	return cancellationLifecycleFixture{
		TenantID:        tenantID,
		ClassID:         classID,
		Source:          source,
		ICalUID:         "urn:uuid:" + aggregateID.String(),
		InitialSequence: sequence,
		AttendeeID:      attendeeID,
		CapabilityID:    capabilityID,
	}
}

func assertCancellationLifecycle(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture cancellationLifecycleFixture,
	expectedReason string,
	expectedAttendeeVersion int64,
	expectedSequence int64,
) {
	t.Helper()
	var (
		responseClosed bool
		version        int64
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT response_closed_at IS NOT NULL, version
FROM tutorhub.class_session_attendees
WHERE id = $1`,
		fixture.AttendeeID,
	).Scan(&responseClosed, &version); err != nil {
		t.Fatalf("read cancelled attendee lifecycle: %v", err)
	}
	if !responseClosed || version != expectedAttendeeVersion {
		t.Fatalf(
			"unexpected cancelled attendee lifecycle: closed=%t version=%d",
			responseClosed,
			version,
		)
	}

	var (
		capabilityRevoked bool
		revocationReason  string
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT revoked_at IS NOT NULL, revoked_reason
FROM tutorhub.calendar_rsvp_capabilities
WHERE id = $1`,
		fixture.CapabilityID,
	).Scan(&capabilityRevoked, &revocationReason); err != nil {
		t.Fatalf("read cancelled capability lifecycle: %v", err)
	}
	if !capabilityRevoked || revocationReason != expectedReason {
		t.Fatalf(
			"unexpected cancelled capability lifecycle: revoked=%t reason=%q",
			capabilityRevoked,
			revocationReason,
		)
	}
	assertInvitationLifecycleSnapshot(
		t,
		ctx,
		pool,
		fixture,
		"cancelled",
		expectedReason,
		expectedSequence,
	)
}

func assertOpenCancellationLifecycle(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture cancellationLifecycleFixture,
) {
	t.Helper()
	var (
		responseClosed    bool
		capabilityRevoked bool
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT attendee.response_closed_at IS NOT NULL,
       capability.revoked_at IS NOT NULL
FROM tutorhub.class_session_attendees AS attendee
JOIN tutorhub.calendar_rsvp_capabilities AS capability
  ON capability.id = $2
WHERE attendee.id = $1`,
		fixture.AttendeeID,
		fixture.CapabilityID,
	).Scan(&responseClosed, &capabilityRevoked); err != nil {
		t.Fatalf("read retained prefix lifecycle: %v", err)
	}
	if responseClosed || capabilityRevoked {
		t.Fatalf(
			"following cancellation closed retained master: response_closed=%t capability_revoked=%t",
			responseClosed,
			capabilityRevoked,
		)
	}
}

func assertInvitationLifecycleSnapshot(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture cancellationLifecycleFixture,
	expectedLifecycle string,
	expectedReason string,
	expectedSequence int64,
) {
	t.Helper()
	sessionID, seriesID, occurrenceKey := participationScopeValues(fixture.Source)
	var (
		revisionCount  int
		recipientCount int
		icalUID        string
		minSequence    int64
		maxSequence    int64
		allMethodNull  bool
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT count(DISTINCT revision.id),
       COALESCE(min(revision.ical_uid), ''),
       COALESCE(min(revision.ical_sequence), -1),
       COALESCE(max(revision.ical_sequence), -1),
       COALESCE(bool_and(revision.method IS NULL), false),
       count(recipient.id)
FROM tutorhub.calendar_invitation_revisions AS revision
LEFT JOIN tutorhub.calendar_invitation_recipients AS recipient
  ON recipient.tenant_id = revision.tenant_id
 AND recipient.class_id = revision.class_id
 AND recipient.invitation_revision_id = revision.id
WHERE revision.tenant_id = $1
  AND revision.class_id = $2
  AND revision.session_id IS NOT DISTINCT FROM $3::uuid
  AND revision.series_id IS NOT DISTINCT FROM $4::uuid
  AND revision.occurrence_key IS NOT DISTINCT FROM $5::text
  AND revision.lifecycle = $6
  AND revision.reason_code = $7`,
		fixture.TenantID,
		fixture.ClassID,
		sessionID,
		seriesID,
		occurrenceKey,
		expectedLifecycle,
		expectedReason,
	).Scan(
		&revisionCount,
		&icalUID,
		&minSequence,
		&maxSequence,
		&allMethodNull,
		&recipientCount,
	); err != nil {
		t.Fatalf("read cancellation invitation snapshot: %v", err)
	}
	if revisionCount != 1 || recipientCount != 1 ||
		icalUID != fixture.ICalUID ||
		minSequence != expectedSequence ||
		maxSequence != expectedSequence ||
		expectedSequence <= fixture.InitialSequence ||
		!allMethodNull {
		t.Fatalf(
			"unexpected cancellation snapshot: revisions=%d recipients=%d uid=%q sequence=%d..%d method_null=%t",
			revisionCount,
			recipientCount,
			icalUID,
			minSequence,
			maxSequence,
			allMethodNull,
		)
	}
}
