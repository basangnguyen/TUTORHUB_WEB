//go:build integration

package classroom

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

func TestResolveInternalAudienceIncludesImplicitClassOwner(t *testing.T) {
	migrationURL := requireEnvironment(t, "DATABASE_MIGRATION_URL")
	poolURL := requireEnvironment(t, "DATABASE_POOL_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if err := migrationrunner.Up(ctx, migrationURL); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := pgxpool.New(ctx, poolURL)
	if err != nil {
		t.Fatalf("create implicit owner audience integration pool: %v", err)
	}
	defer pool.Close()

	transaction, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin implicit owner audience fixture: %v", err)
	}
	defer func() { _ = transaction.Rollback(context.Background()) }()

	tenantID, ownerID := seedTenantOwner(t, ctx, transaction, "participation-owner-audience")
	organizerID := seedTenantMember(
		t, ctx, transaction, tenantID, "participation-owner-audience-organizer", "teacher",
	)
	classID := uuid.New()
	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES ($1, $2, $3, $4, 'Implicit owner audience integration class',
        'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		"PO"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert implicit owner audience class: %v", err)
	}

	resolved, err := resolveInternalAudience(
		ctx,
		transaction,
		tenantID,
		classID,
		organizerID,
		[]InternalAudienceAttendee{{
			UserID:            ownerID,
			ParticipationRole: ParticipationRoleRequired,
		}},
	)
	if err != nil {
		t.Fatalf("resolve implicit class owner audience: %v", err)
	}
	if len(resolved) != 1 || resolved[0].UserID != ownerID ||
		resolved[0].ParticipationRole != ParticipationRoleRequired ||
		resolved[0].BusinessRole != string(policy.OrganizationRoleTeacher) ||
		resolved[0].AudienceSource != "roster" {
		t.Fatalf("unexpected implicit class owner audience: %+v", resolved)
	}
}

func TestPostgresSessionParticipationSnapshotsIdempotencyAndRSVPCAS(t *testing.T) {
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
		t.Fatalf("create participation integration pool: %v", err)
	}
	defer pool.Close()

	setup, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin participation fixture: %v", err)
	}
	defer func() { _ = setup.Rollback(context.Background()) }()
	tenantID, ownerID := seedTenantOwner(t, ctx, setup, "participation")
	studentID := seedTenantMember(t, ctx, setup, tenantID, "participation-student", "student")
	classID := uuid.New()
	sessionID := uuid.New()
	if _, err := setup.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, timezone, status
)
VALUES ($1, $2, $3, $4, 'Participation integration class',
        'Asia/Ho_Chi_Minh', 'active')`,
		classID,
		tenantID,
		ownerID,
		"PA"+strings.ToUpper(uuid.NewString()[:8]),
	); err != nil {
		t.Fatalf("insert participation class: %v", err)
	}
	if _, err := setup.Exec(ctx, `
INSERT INTO tutorhub.class_enrollments (
    tenant_id, class_id, user_id, class_role, status, enrolled_by, joined_at
)
VALUES ($1, $2, $3, 'student', 'active', $4, now())`,
		tenantID,
		classID,
		studentID,
		ownerID,
	); err != nil {
		t.Fatalf("insert participation enrollment: %v", err)
	}
	startsAt := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Microsecond)
	if _, err := setup.Exec(ctx, `
INSERT INTO tutorhub.class_sessions (
    id, tenant_id, class_id, title, description,
    starts_at, ends_at, timezone, status,
    created_by, updated_by, organizer_user_id, ical_uid
)
VALUES (
    $1, $2, $3, 'Participation repository integration',
    'Encrypted invitation snapshot fixture',
    $4, $5, 'Asia/Ho_Chi_Minh', 'scheduled',
    $6, $6, $6, $7
)`,
		sessionID,
		tenantID,
		classID,
		startsAt,
		startsAt.Add(time.Hour),
		ownerID,
		"urn:uuid:"+sessionID.String(),
	); err != nil {
		t.Fatalf("insert participation session: %v", err)
	}
	if err := setup.Commit(ctx); err != nil {
		t.Fatalf("commit participation fixture: %v", err)
	}
	defer cleanupClassIntegrationFixture(t, pool, tenantID, ownerID, studentID)

	protector, err := protecteddata.New(protecteddata.Config{
		Key:        bytes.Repeat([]byte{0x6d}, 32),
		KeyVersion: 7,
	})
	if err != nil {
		t.Fatalf("create participation test protector: %v", err)
	}
	repository := NewPostgresRepository(
		pool,
		30*time.Second,
		policy.NewEngine(),
	).WithCalendarProtectedData(protector)
	ownerContext := mustTenantContext(t, tenantID, ownerID)
	studentContext := mustTenantContext(t, tenantID, studentID)
	replacedAt := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)

	replaceParams, err := (ReplaceAudienceInput{
		ExpectedAudienceRevision: 0,
		IdempotencyKey:           "participation-audience-0001",
		ResponseRequested:        true,
		Attendees: []InternalAudienceAttendeeInput{
			{UserID: ownerID, ParticipationRole: ParticipationRoleRequired},
			{UserID: studentID, ParticipationRole: ParticipationRoleRequired},
		},
	}).normalized()
	if err != nil {
		t.Fatalf("normalize participation audience: %v", err)
	}
	replaced, err := repository.ReplaceSessionAudience(
		ctx,
		ownerContext,
		classID,
		sessionID,
		replaceParams,
		replacedAt,
	)
	if err != nil {
		t.Fatalf("replace participation audience: %v", err)
	}
	if replaced.Replayed || replaced.Audience.AudienceRevision != 1 ||
		!replaced.Audience.ResponseRequested || len(replaced.Audience.Attendees) != 2 {
		t.Fatalf("unexpected participation audience replacement: %+v", replaced)
	}

	studentAttendee := findIntegrationAudienceAttendee(t, replaced.Audience, studentID)
	if studentAttendee.Version != 1 ||
		studentAttendee.ParticipationRole != ParticipationRoleRequired ||
		studentAttendee.BusinessRole != "student" ||
		studentAttendee.RSVPState != RSVPStateNeedsAction {
		t.Fatalf("unexpected created student attendee: %+v", studentAttendee)
	}
	assertParticipationMutationCount(
		t, ctx, pool, tenantID, sessionID,
		"calendar_participation_mutation_receipts", "operation", "audience_replace", 1,
	)
	assertParticipationMutationCount(
		t, ctx, pool, tenantID, sessionID,
		"outbox_events", "event_type", "class_session.audience_replaced.v1", 1,
	)
	assertParticipationAuditCount(
		t, ctx, pool, tenantID, sessionID, "class.session.audience.replace", 1,
	)
	assertEncryptedInvitationSnapshot(
		t, ctx, pool, protector, tenantID, classID, sessionID, 2,
	)
	assertAudienceReadWaitsForSourceMutation(
		t, ctx, pool, repository, ownerContext, tenantID, classID, sessionID,
	)

	replayed, err := repository.ReplaceSessionAudience(
		ctx,
		ownerContext,
		classID,
		sessionID,
		replaceParams,
		replacedAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf("replay participation audience replacement: %v", err)
	}
	if !replayed.Replayed || replayed.Audience.AudienceRevision != 1 ||
		len(replayed.Audience.Attendees) != 2 {
		t.Fatalf("unexpected audience replay: %+v", replayed)
	}
	assertParticipationSnapshotCounts(t, ctx, pool, tenantID, sessionID, 1, 2)
	assertParticipationMutationCount(
		t, ctx, pool, tenantID, sessionID,
		"outbox_events", "event_type", "class_session.audience_replaced.v1", 1,
	)
	assertParticipationAuditCount(
		t, ctx, pool, tenantID, sessionID, "class.session.audience.replace", 1,
	)

	conflictingParams, err := (ReplaceAudienceInput{
		ExpectedAudienceRevision: 1,
		IdempotencyKey:           replaceParams.IdempotencyKey,
		ResponseRequested:        true,
		Attendees: []InternalAudienceAttendeeInput{
			{UserID: ownerID, ParticipationRole: ParticipationRoleRequired},
		},
	}).normalized()
	if err != nil {
		t.Fatalf("normalize conflicting audience replacement: %v", err)
	}
	if _, err := repository.ReplaceSessionAudience(
		ctx,
		ownerContext,
		classID,
		sessionID,
		conflictingParams,
		replacedAt.Add(2*time.Minute),
	); !errors.Is(err, ErrSessionParticipationIdempotencyConflict) {
		t.Fatalf("same key with another audience must conflict, got %v", err)
	}
	assertParticipationSnapshotCounts(t, ctx, pool, tenantID, sessionID, 1, 2)

	rsvpParams, err := (SelfRSVPInput{
		State:                   RSVPStateAccepted,
		Note:                    "Confirmed",
		ExpectedAttendeeVersion: studentAttendee.Version,
		IdempotencyKey:          "participation-rsvp-000001",
	}).normalized()
	if err != nil {
		t.Fatalf("normalize student RSVP: %v", err)
	}
	rsvp, err := repository.RespondToSession(
		ctx,
		studentContext,
		classID,
		sessionID,
		rsvpParams,
		replacedAt.Add(3*time.Minute),
	)
	if err != nil {
		t.Fatalf("record student RSVP: %v", err)
	}
	if rsvp.Replayed || rsvp.Attendee.ID != studentAttendee.ID ||
		rsvp.Attendee.RSVPState != RSVPStateAccepted ||
		rsvp.Attendee.Version != studentAttendee.Version+1 {
		t.Fatalf("unexpected student RSVP result: %+v", rsvp)
	}
	var (
		persistedRSVPState  string
		persistedRSVPSource string
		responseRevisionID  uuid.UUID
		responseSequence    int64
		persistedVersion    int64
	)
	if err := pool.QueryRow(ctx, `
SELECT rsvp_state, rsvp_source, response_invitation_revision_id,
       response_sequence, version
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1 AND class_id = $2 AND id = $3`,
		tenantID,
		classID,
		studentAttendee.ID,
	).Scan(
		&persistedRSVPState,
		&persistedRSVPSource,
		&responseRevisionID,
		&responseSequence,
		&persistedVersion,
	); err != nil {
		t.Fatalf("read persisted student RSVP: %v", err)
	}
	if persistedRSVPState != string(RSVPStateAccepted) ||
		persistedRSVPSource != "tutorhub_authenticated" ||
		responseRevisionID == uuid.Nil || responseSequence != 0 ||
		persistedVersion != 2 {
		t.Fatalf(
			"unexpected persisted RSVP state=%s source=%s revision=%s sequence=%d version=%d",
			persistedRSVPState,
			persistedRSVPSource,
			responseRevisionID,
			responseSequence,
			persistedVersion,
		)
	}
	assertParticipationMutationCount(
		t, ctx, pool, tenantID, sessionID,
		"calendar_participation_mutation_receipts", "operation", "rsvp_respond", 1,
	)
	assertParticipationMutationCount(
		t, ctx, pool, tenantID, sessionID,
		"outbox_events", "event_type", "class_session.rsvp_responded.v1", 1,
	)
	assertParticipationAuditCount(
		t, ctx, pool, tenantID, sessionID, "class.session.rsvp.respond", 1,
	)

	staleParams, err := (SelfRSVPInput{
		State:                   RSVPStateDeclined,
		ExpectedAttendeeVersion: studentAttendee.Version,
		IdempotencyKey:          "participation-rsvp-stale-0001",
	}).normalized()
	if err != nil {
		t.Fatalf("normalize stale student RSVP: %v", err)
	}
	if _, err := repository.RespondToSession(
		ctx,
		studentContext,
		classID,
		sessionID,
		staleParams,
		replacedAt.Add(4*time.Minute),
	); !errors.Is(err, ErrSessionAttendeeVersionConflict) {
		t.Fatalf("stale RSVP must fail optimistic CAS, got %v", err)
	}
	assertParticipationMutationCount(
		t, ctx, pool, tenantID, sessionID,
		"calendar_participation_mutation_receipts", "operation", "rsvp_respond", 1,
	)
	assertParticipationMutationCount(
		t, ctx, pool, tenantID, sessionID,
		"outbox_events", "event_type", "class_session.rsvp_responded.v1", 1,
	)

	disableResponsesParams, err := (ReplaceAudienceInput{
		ExpectedAudienceRevision: 1,
		IdempotencyKey:           "participation-audience-0002",
		ResponseRequested:        false,
		Attendees: []InternalAudienceAttendeeInput{
			{UserID: ownerID, ParticipationRole: ParticipationRoleRequired},
			{UserID: studentID, ParticipationRole: ParticipationRoleRequired},
		},
	}).normalized()
	if err != nil {
		t.Fatalf("normalize response-policy audience replacement: %v", err)
	}
	responsesDisabled, err := repository.ReplaceSessionAudience(
		ctx,
		ownerContext,
		classID,
		sessionID,
		disableResponsesParams,
		replacedAt.Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf("disable participation responses: %v", err)
	}
	if responsesDisabled.Audience.AudienceRevision != 2 ||
		responsesDisabled.Audience.ResponseRequested {
		t.Fatalf("unexpected disabled response audience: %+v", responsesDisabled.Audience)
	}
	var (
		resetState            bool
		resetSource           bool
		clearedRespondedAt    bool
		clearedNote           bool
		clearedRevision       bool
		clearedSequence       bool
		responseClosed        bool
		responsePolicyOff     bool
		responsePolicyVersion int64
	)
	if err := pool.QueryRow(ctx, `
SELECT rsvp_state = 'needs_action',
       rsvp_source = 'none',
       responded_at IS NULL,
       response_note IS NULL,
       response_invitation_revision_id IS NULL,
       response_sequence IS NULL,
       response_closed_at IS NOT NULL,
       response_requested = false,
       version
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1 AND class_id = $2 AND id = $3`,
		tenantID,
		classID,
		studentAttendee.ID,
	).Scan(
		&resetState,
		&resetSource,
		&clearedRespondedAt,
		&clearedNote,
		&clearedRevision,
		&clearedSequence,
		&responseClosed,
		&responsePolicyOff,
		&responsePolicyVersion,
	); err != nil {
		t.Fatalf("read disabled RSVP policy state: %v", err)
	}
	if !resetState || !resetSource || !clearedRespondedAt || !clearedNote ||
		!clearedRevision || !clearedSequence || !responseClosed ||
		!responsePolicyOff || responsePolicyVersion != 3 {
		t.Fatalf(
			"response policy reset incomplete state=%t source=%t responded=%t note=%t revision=%t sequence=%t closed=%t disabled=%t version=%d",
			resetState,
			resetSource,
			clearedRespondedAt,
			clearedNote,
			clearedRevision,
			clearedSequence,
			responseClosed,
			responsePolicyOff,
			responsePolicyVersion,
		)
	}
}

func findIntegrationAudienceAttendee(
	t *testing.T,
	audience SessionAudience,
	userID uuid.UUID,
) SessionAudienceAttendee {
	t.Helper()
	for _, attendee := range audience.Attendees {
		if attendee.UserID == userID {
			return attendee
		}
	}
	t.Fatalf("audience attendee %s was not created", userID)
	return SessionAudienceAttendee{}
}

func assertAudienceReadWaitsForSourceMutation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *PostgresRepository,
	tenantContext tenancy.Context,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
) {
	t.Helper()

	lockTransaction, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin audience source lock: %v", err)
	}
	defer func() { _ = lockTransaction.Rollback(context.Background()) }()
	var lockedID uuid.UUID
	if err := lockTransaction.QueryRow(ctx, `
SELECT id
FROM tutorhub.class_sessions
WHERE tenant_id = $1 AND class_id = $2 AND id = $3
FOR UPDATE`,
		tenantID,
		classID,
		sessionID,
	).Scan(&lockedID); err != nil {
		t.Fatalf("lock audience source: %v", err)
	}

	type audienceReadResult struct {
		audience SessionAudience
		err      error
	}
	resultChannel := make(chan audienceReadResult, 1)
	readContext, cancelRead := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRead()
	go func() {
		audience, readErr := repository.GetSessionAudience(
			readContext,
			tenantContext,
			classID,
			sessionID,
		)
		resultChannel <- audienceReadResult{audience: audience, err: readErr}
	}()

	select {
	case result := <-resultChannel:
		t.Fatalf(
			"audience read crossed an in-flight source mutation instead of waiting: audience=%+v err=%v",
			result.audience,
			result.err,
		)
	case <-time.After(150 * time.Millisecond):
	}

	if err := lockTransaction.Rollback(ctx); err != nil {
		t.Fatalf("release audience source lock: %v", err)
	}
	select {
	case result := <-resultChannel:
		if result.err != nil {
			t.Fatalf("read audience after source lock release: %v", result.err)
		}
		if result.audience.AudienceRevision != 1 || len(result.audience.Attendees) != 2 {
			t.Fatalf("unexpected coherent audience after source lock release: %+v", result.audience)
		}
	case <-readContext.Done():
		t.Fatalf("audience read did not resume after source lock release: %v", readContext.Err())
	}
}

func assertEncryptedInvitationSnapshot(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	protector *protecteddata.Protector,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
	expectedRecipients int,
) {
	t.Helper()
	var (
		revisionID uuid.UUID
		method     pgtype.Text
		lifecycle  string
		revision   int64
		sequence   int64
		ciphertext []byte
		digest     []byte
		keyVersion int16
	)
	if err := pool.QueryRow(ctx, `
SELECT id, method, lifecycle, audience_revision, ical_sequence,
       canonical_payload_ciphertext, canonical_payload_sha256,
       crypto_key_version
FROM tutorhub.calendar_invitation_revisions
WHERE tenant_id = $1 AND class_id = $2 AND session_id = $3`,
		tenantID,
		classID,
		sessionID,
	).Scan(
		&revisionID,
		&method,
		&lifecycle,
		&revision,
		&sequence,
		&ciphertext,
		&digest,
		&keyVersion,
	); err != nil {
		t.Fatalf("read invitation revision snapshot: %v", err)
	}
	if method.Valid || lifecycle != "published" || revision != 1 ||
		sequence != 0 || keyVersion != protector.KeyVersion() {
		t.Fatalf(
			"unexpected neutral revision method=%+v lifecycle=%s revision=%d sequence=%d key=%d",
			method,
			lifecycle,
			revision,
			sequence,
			keyVersion,
		)
	}
	plaintext, err := protector.Open(protecteddata.Context{
		TenantID: tenantID.String(),
		Purpose:  protecteddata.PurposeInvitationCanonicalPayload,
		RecordID: revisionID.String(),
	}, protecteddata.SealedValue{
		KeyVersion: keyVersion,
		Ciphertext: ciphertext,
	})
	if err != nil {
		t.Fatalf("decrypt invitation canonical snapshot: %v", err)
	}
	calculatedDigest := sha256.Sum256(plaintext)
	if !bytes.Equal(digest, calculatedDigest[:]) ||
		!bytes.Contains(plaintext, []byte(`"schema_version":"tutorhub.calendar.invitation.v1"`)) ||
		!bytes.Contains(plaintext, []byte(sessionID.String())) ||
		bytes.Contains(ciphertext, []byte("Participation repository integration")) {
		t.Fatal("canonical invitation snapshot was not encrypted or integrity-bound as expected")
	}

	rows, err := pool.Query(ctx, `
SELECT recipient.id, attendee.internal_user_id,
       recipient.delivery_address_fingerprint,
       recipient.delivery_address_ciphertext,
       recipient.display_name_ciphertext,
       recipient.crypto_key_version,
       app_user.email,
       app_user.display_name
FROM tutorhub.calendar_invitation_recipients AS recipient
JOIN tutorhub.class_session_attendees AS attendee
  ON attendee.tenant_id = recipient.tenant_id
 AND attendee.class_id = recipient.class_id
 AND attendee.id = recipient.attendee_id
JOIN tutorhub.users AS app_user
  ON app_user.id = attendee.internal_user_id
WHERE recipient.tenant_id = $1
  AND recipient.class_id = $2
  AND recipient.invitation_revision_id = $3
ORDER BY attendee.internal_user_id`,
		tenantID,
		classID,
		revisionID,
	)
	if err != nil {
		t.Fatalf("read encrypted invitation recipients: %v", err)
	}
	defer rows.Close()
	recipientCount := 0
	for rows.Next() {
		var (
			recipientID        uuid.UUID
			userID             uuid.UUID
			addressFingerprint []byte
			addressCiphertext  []byte
			nameCiphertext     []byte
			recipientKey       int16
			email              string
			displayName        string
		)
		if err := rows.Scan(
			&recipientID,
			&userID,
			&addressFingerprint,
			&addressCiphertext,
			&nameCiphertext,
			&recipientKey,
			&email,
			&displayName,
		); err != nil {
			t.Fatalf("scan encrypted invitation recipient: %v", err)
		}
		recipientCount++
		address, err := protector.Open(protecteddata.Context{
			TenantID: tenantID.String(),
			Purpose:  protecteddata.PurposeInvitationRecipientAddress,
			RecordID: recipientID.String(),
		}, protecteddata.SealedValue{
			KeyVersion: recipientKey,
			Ciphertext: addressCiphertext,
		})
		if err != nil {
			t.Fatalf("decrypt recipient address for %s: %v", userID, err)
		}
		name, err := protector.Open(protecteddata.Context{
			TenantID: tenantID.String(),
			Purpose:  protecteddata.PurposeInvitationRecipientDisplayName,
			RecordID: recipientID.String(),
		}, protecteddata.SealedValue{
			KeyVersion: recipientKey,
			Ciphertext: nameCiphertext,
		})
		if err != nil {
			t.Fatalf("decrypt recipient display name for %s: %v", userID, err)
		}
		expectedFingerprint, err := protector.DeliveryAddressFingerprint(
			tenantID.String(),
			[]byte(strings.ToLower(strings.TrimSpace(email))),
		)
		if err != nil {
			t.Fatalf("fingerprint recipient address for %s: %v", userID, err)
		}
		if string(address) != strings.ToLower(strings.TrimSpace(email)) ||
			string(name) != strings.TrimSpace(displayName) ||
			!bytes.Equal(addressFingerprint, expectedFingerprint[:]) ||
			bytes.Contains(addressCiphertext, []byte(email)) ||
			bytes.Contains(nameCiphertext, []byte(displayName)) {
			t.Fatalf("recipient %s snapshot is not protected consistently", userID)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate encrypted invitation recipients: %v", err)
	}
	if recipientCount != expectedRecipients {
		t.Fatalf("invitation recipient count=%d, want %d", recipientCount, expectedRecipients)
	}
}

func assertParticipationSnapshotCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	sessionID uuid.UUID,
	expectedRevisions int,
	expectedRecipients int,
) {
	t.Helper()
	var revisions, recipients int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM tutorhub.calendar_invitation_revisions
WHERE tenant_id = $1 AND session_id = $2`,
		tenantID,
		sessionID,
	).Scan(&revisions); err != nil {
		t.Fatalf("count invitation revisions: %v", err)
	}
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM tutorhub.calendar_invitation_recipients AS recipient
JOIN tutorhub.calendar_invitation_revisions AS revision
  ON revision.tenant_id = recipient.tenant_id
 AND revision.id = recipient.invitation_revision_id
WHERE revision.tenant_id = $1 AND revision.session_id = $2`,
		tenantID,
		sessionID,
	).Scan(&recipients); err != nil {
		t.Fatalf("count invitation recipients: %v", err)
	}
	if revisions != expectedRevisions || recipients != expectedRecipients {
		t.Fatalf(
			"snapshot counts revisions=%d recipients=%d, want %d/%d",
			revisions,
			recipients,
			expectedRevisions,
			expectedRecipients,
		)
	}
}

func assertParticipationMutationCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	sessionID uuid.UUID,
	table string,
	discriminatorColumn string,
	discriminator string,
	expected int,
) {
	t.Helper()
	var query string
	switch {
	case table == "calendar_participation_mutation_receipts" &&
		discriminatorColumn == "operation":
		query = `
SELECT count(*)
FROM tutorhub.calendar_participation_mutation_receipts
WHERE tenant_id = $1 AND session_id = $2 AND operation = $3`
	case table == "outbox_events" && discriminatorColumn == "event_type":
		query = `
SELECT count(*)
FROM tutorhub.outbox_events
WHERE tenant_id = $1 AND aggregate_id = $2 AND event_type = $3`
	default:
		t.Fatalf("unsupported participation count target %s.%s", table, discriminatorColumn)
	}
	var count int
	if err := pool.QueryRow(ctx, query, tenantID, sessionID, discriminator).Scan(&count); err != nil {
		t.Fatalf("count %s %s rows: %v", table, discriminator, err)
	}
	if count != expected {
		t.Fatalf("%s %s count=%d, want %d", table, discriminator, count, expected)
	}
}

func assertParticipationAuditCount(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID uuid.UUID,
	sessionID uuid.UUID,
	action string,
	expected int,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM tutorhub.audit_events
WHERE tenant_id = $1
  AND resource_type = 'class_session'
  AND resource_id = $2
  AND action = $3`,
		tenantID,
		sessionID,
		action,
	).Scan(&count); err != nil {
		t.Fatalf("count participation audit action %s: %v", action, err)
	}
	if count != expected {
		t.Fatalf("participation audit action %s count=%d, want %d", action, count, expected)
	}
}
