package classroom

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var externalCapabilityRevocationReason = regexp.MustCompile(`^[a-z][a-z0-9._-]{2,99}$`)

type ExternalRSVPMutationResult struct {
	Projection ExternalRSVPProjection
	Replayed   bool
}

// ExternalRSVPCapabilityRepository is deliberately split from the
// authenticated participant service: resolve/respond do not receive a tenant
// or attendee identifier from an untrusted caller. Both scopes are derived
// exclusively from the keyed capability lookup inside PostgreSQL.
type ExternalRSVPCapabilityRepository interface {
	IssueExternalRSVPCapability(
		context.Context,
		tenancy.Context,
		uuid.UUID,
		ExternalRSVPCapabilityIssue,
	) (ExternalRSVPCapabilityToken, error)
	ResolveExternalRSVPCapability(
		context.Context,
		string,
		time.Time,
	) (ExternalRSVPProjection, error)
	RespondWithExternalRSVPCapability(
		context.Context,
		externalRSVPResponseParams,
		time.Time,
	) (ExternalRSVPMutationResult, error)
}

type externalCapabilityRow struct {
	CapabilityID         uuid.UUID
	TenantID             uuid.UUID
	ClassID              uuid.UUID
	RevisionID           uuid.UUID
	RecipientID          uuid.UUID
	AttendeeID           uuid.UUID
	SessionID            uuid.NullUUID
	SeriesID             uuid.NullUUID
	OccurrenceKey        *string
	ExpiresAt            time.Time
	AttendeeVersion      int64
	RSVPState            RSVPState
	ResponseRequested    bool
	ResponseClosedAt     *time.Time
	InvitationSequence   int64
	PayloadCiphertext    []byte
	PayloadCryptoVersion int16
	TokenDigest          []byte
}

func (repository *PostgresRepository) IssueExternalRSVPCapability(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	issue ExternalRSVPCapabilityIssue,
) (ExternalRSVPCapabilityToken, error) {
	if tenantContext.Validate() != nil || classID == uuid.Nil || repository.calendarProtector == nil {
		return ExternalRSVPCapabilityToken{}, ErrExternalRSVPCapabilityUnavailable
	}
	if issue.InvitationRevisionID == uuid.Nil || issue.InvitationRecipientID == uuid.Nil ||
		issue.IssuedAt.IsZero() {
		return ExternalRSVPCapabilityToken{}, ErrExternalRSVPCapabilityUnavailable
	}
	switch issue.Purpose {
	case ExternalRSVPCapabilityResolve, ExternalRSVPCapabilityRespond:
	default:
		return ExternalRSVPCapabilityToken{}, ErrExternalRSVPCapabilityUnavailable
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ExternalRSVPCapabilityToken{}, fmt.Errorf("begin external RSVP capability issue: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if err := repository.requireSessionSchedulingFeature(
		queryContext,
		transaction,
		tenantContext.TenantID,
	); err != nil {
		return ExternalRSVPCapabilityToken{}, err
	}
	row, err := repository.readExternalCapabilityIssueTarget(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		issue.InvitationRevisionID,
		issue.InvitationRecipientID,
	)
	if err != nil {
		return ExternalRSVPCapabilityToken{}, err
	}
	projection, err := repository.externalProjection(row)
	if err != nil || !projection.ResponseRequested || row.ResponseClosedAt != nil {
		return ExternalRSVPCapabilityToken{}, ErrExternalRSVPCapabilityUnavailable
	}
	issue.EventEndsAt = projection.EndsAt
	expiresAt, err := issue.validate()
	if err != nil {
		return ExternalRSVPCapabilityToken{}, err
	}
	token, err := generateExternalRSVPCapabilityToken(repository.calendarProtector, expiresAt)
	if err != nil {
		return ExternalRSVPCapabilityToken{}, err
	}

	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.calendar_rsvp_capabilities
SET revoked_at = $1,
    revoked_reason = 'capability_rotated'
WHERE tenant_id = $2
  AND invitation_recipient_id = $3
  AND purpose = $4
  AND revoked_at IS NULL`,
		issue.IssuedAt.UTC(),
		tenantContext.TenantID,
		issue.InvitationRecipientID,
		string(issue.Purpose),
	); err != nil {
		return ExternalRSVPCapabilityToken{}, fmt.Errorf("rotate external RSVP capability: %w", err)
	}
	if _, err := transaction.Exec(
		queryContext,
		`INSERT INTO tutorhub.calendar_rsvp_capabilities (
    id, tenant_id, invitation_revision_id, invitation_recipient_id,
    purpose, token_version, token_digest, issued_at, expires_at, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $8)`,
		uuid.New(),
		tenantContext.TenantID,
		issue.InvitationRevisionID,
		issue.InvitationRecipientID,
		string(issue.Purpose),
		token.Version,
		token.Digest[:],
		issue.IssuedAt.UTC(),
		token.ExpiresAt,
	); err != nil {
		return ExternalRSVPCapabilityToken{}, mapParticipationPostgresError(
			"insert external RSVP capability",
			err,
		)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ExternalRSVPCapabilityToken{}, fmt.Errorf("commit external RSVP capability issue: %w", err)
	}
	return token, nil
}

func (repository *PostgresRepository) ResolveExternalRSVPCapability(
	ctx context.Context,
	rawToken string,
	resolvedAt time.Time,
) (ExternalRSVPProjection, error) {
	version, digest, err := digestExternalRSVPCapabilityToken(repository.calendarProtector, rawToken)
	if err != nil || resolvedAt.IsZero() {
		return ExternalRSVPProjection{}, ErrExternalRSVPCapabilityUnavailable
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ExternalRSVPProjection{}, fmt.Errorf("begin external RSVP capability resolve: %w", err)
	}
	defer rollbackClassTransaction(transaction)
	row, err := repository.lockExternalCapability(
		queryContext,
		transaction,
		version,
		digest[:],
		ExternalRSVPCapabilityResolve,
		resolvedAt.UTC(),
	)
	if err != nil {
		return ExternalRSVPProjection{}, err
	}
	if err := repository.requireSessionSchedulingFeature(
		queryContext,
		transaction,
		row.TenantID,
	); err != nil {
		return ExternalRSVPProjection{}, err
	}
	projection, err := repository.externalProjection(row)
	if err != nil {
		return ExternalRSVPProjection{}, err
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.calendar_rsvp_capabilities
SET last_used_at = $1,
    use_count = use_count + 1
WHERE tenant_id = $2
  AND id = $3
  AND use_count < 1000000`,
		resolvedAt.UTC(),
		row.TenantID,
		row.CapabilityID,
	); err != nil {
		return ExternalRSVPProjection{}, fmt.Errorf("record external RSVP capability resolve: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ExternalRSVPProjection{}, fmt.Errorf("commit external RSVP capability resolve: %w", err)
	}
	return projection, nil
}

func (repository *PostgresRepository) RespondWithExternalRSVPCapability(
	ctx context.Context,
	params externalRSVPResponseParams,
	respondedAt time.Time,
) (ExternalRSVPMutationResult, error) {
	version, digest, err := digestExternalRSVPCapabilityToken(repository.calendarProtector, params.RawToken)
	if err != nil || respondedAt.IsZero() || params.Fingerprint == "" {
		return ExternalRSVPMutationResult{}, ErrExternalRSVPCapabilityUnavailable
	}
	fingerprint, err := hex.DecodeString(params.Fingerprint)
	if err != nil || len(fingerprint) != 32 {
		return ExternalRSVPMutationResult{}, ErrExternalRSVPCapabilityUnavailable
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ExternalRSVPMutationResult{}, fmt.Errorf("begin external RSVP response: %w", err)
	}
	defer rollbackClassTransaction(transaction)
	row, err := repository.lockExternalCapability(
		queryContext,
		transaction,
		version,
		digest[:],
		ExternalRSVPCapabilityRespond,
		respondedAt.UTC(),
	)
	if err != nil {
		return ExternalRSVPMutationResult{}, err
	}
	if err := repository.requireSessionSchedulingFeature(
		queryContext,
		transaction,
		row.TenantID,
	); err != nil {
		return ExternalRSVPMutationResult{}, err
	}

	replayed, err := readExternalRSVPReceipt(
		queryContext,
		transaction,
		row,
		params.IdempotencyKey,
		fingerprint,
	)
	if err != nil {
		return ExternalRSVPMutationResult{}, err
	}
	if !replayed {
		if row.AttendeeVersion != params.ExpectedAttendeeVersion {
			return ExternalRSVPMutationResult{}, ErrExternalRSVPVersionConflict
		}
		note := any(nil)
		if params.Note != "" {
			note = params.Note
		}
		commandTag, updateErr := transaction.Exec(
			queryContext,
			`UPDATE tutorhub.class_session_attendees
SET rsvp_state = $1,
    rsvp_source = 'tutorhub_external_capability',
    responded_at = $2,
    response_note = $3,
    response_invitation_revision_id = $4,
    response_sequence = $5,
    version = version + 1,
    updated_at = $2
WHERE tenant_id = $6
  AND class_id = $7
  AND id = $8
  AND external_recipient_id IS NOT NULL
  AND status = 'active'
  AND response_requested = true
  AND response_closed_at IS NULL
  AND version = $9`,
			string(params.State),
			respondedAt.UTC(),
			note,
			row.RevisionID,
			row.InvitationSequence,
			row.TenantID,
			row.ClassID,
			row.AttendeeID,
			params.ExpectedAttendeeVersion,
		)
		if updateErr != nil {
			return ExternalRSVPMutationResult{}, mapParticipationPostgresError(
				"update external RSVP attendee",
				updateErr,
			)
		}
		if commandTag.RowsAffected() != 1 {
			return ExternalRSVPMutationResult{}, ErrExternalRSVPVersionConflict
		}
		row.AttendeeVersion++
		row.RSVPState = params.State
		if err := insertExternalRSVPReceipt(
			queryContext,
			transaction,
			row,
			params.IdempotencyKey,
			fingerprint,
			respondedAt.UTC(),
		); err != nil {
			return ExternalRSVPMutationResult{}, err
		}
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.calendar_rsvp_capabilities
SET last_used_at = $1,
    use_count = use_count + 1
WHERE tenant_id = $2
  AND id = $3
  AND use_count < 1000000`,
		respondedAt.UTC(),
		row.TenantID,
		row.CapabilityID,
	); err != nil {
		return ExternalRSVPMutationResult{}, fmt.Errorf("record external RSVP capability response: %w", err)
	}
	projection, err := repository.externalProjection(row)
	if err != nil {
		return ExternalRSVPMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ExternalRSVPMutationResult{}, fmt.Errorf("commit external RSVP response: %w", err)
	}
	return ExternalRSVPMutationResult{Projection: projection, Replayed: replayed}, nil
}

func (repository *PostgresRepository) readExternalCapabilityIssueTarget(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	revisionID uuid.UUID,
	recipientID uuid.UUID,
) (externalCapabilityRow, error) {
	row, err := scanExternalCapabilityTarget(transaction.QueryRow(
		ctx,
		externalCapabilityIssueTargetQuery,
		tenantID,
		classID,
		revisionID,
		recipientID,
	))
	if err != nil {
		return externalCapabilityRow{}, err
	}
	return row, nil
}

func (repository *PostgresRepository) lockExternalCapability(
	ctx context.Context,
	transaction pgx.Tx,
	version int16,
	digest []byte,
	purpose ExternalRSVPCapabilityPurpose,
	now time.Time,
) (externalCapabilityRow, error) {
	row, err := scanExternalCapabilityTarget(transaction.QueryRow(
		ctx,
		externalCapabilityLookupQuery,
		version,
		digest,
		string(purpose),
		now,
	))
	if err != nil {
		return externalCapabilityRow{}, err
	}
	if !hmac.Equal(row.TokenDigest, digest) {
		return externalCapabilityRow{}, ErrExternalRSVPCapabilityUnavailable
	}
	return row, nil
}

func scanExternalCapabilityTarget(row pgx.Row) (externalCapabilityRow, error) {
	var target externalCapabilityRow
	if err := row.Scan(
		&target.CapabilityID,
		&target.TenantID,
		&target.ClassID,
		&target.RevisionID,
		&target.RecipientID,
		&target.AttendeeID,
		&target.SessionID,
		&target.SeriesID,
		&target.OccurrenceKey,
		&target.ExpiresAt,
		&target.AttendeeVersion,
		&target.RSVPState,
		&target.ResponseRequested,
		&target.ResponseClosedAt,
		&target.InvitationSequence,
		&target.PayloadCiphertext,
		&target.PayloadCryptoVersion,
		&target.TokenDigest,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return externalCapabilityRow{}, ErrExternalRSVPCapabilityUnavailable
		}
		return externalCapabilityRow{}, fmt.Errorf("read external RSVP capability: %w", err)
	}
	return target, nil
}

func (repository *PostgresRepository) externalProjection(
	row externalCapabilityRow,
) (ExternalRSVPProjection, error) {
	if repository.calendarProtector == nil || row.RevisionID == uuid.Nil ||
		row.AttendeeID == uuid.Nil || row.ResponseClosedAt != nil {
		return ExternalRSVPProjection{}, ErrExternalRSVPCapabilityUnavailable
	}
	plaintext, err := repository.calendarProtector.Open(protecteddata.Context{
		TenantID: row.TenantID.String(),
		Purpose:  protecteddata.PurposeInvitationCanonicalPayload,
		RecordID: row.RevisionID.String(),
	}, protecteddata.SealedValue{
		KeyVersion: row.PayloadCryptoVersion,
		Ciphertext: row.PayloadCiphertext,
	})
	if err != nil {
		return ExternalRSVPProjection{}, ErrExternalRSVPCapabilityUnavailable
	}
	var snapshot canonicalSessionInvitationSnapshot
	if err := json.Unmarshal(plaintext, &snapshot); err != nil || snapshot.Lifecycle == "cancelled" {
		return ExternalRSVPProjection{}, ErrExternalRSVPCapabilityUnavailable
	}
	startsAt, err := time.Parse(time.RFC3339Nano, snapshot.StartsAt)
	if err != nil {
		return ExternalRSVPProjection{}, ErrExternalRSVPCapabilityUnavailable
	}
	endsAt, err := time.Parse(time.RFC3339Nano, snapshot.EndsAt)
	if err != nil || !startsAt.Before(endsAt) || snapshot.Title == "" || snapshot.Timezone == "" {
		return ExternalRSVPProjection{}, ErrExternalRSVPCapabilityUnavailable
	}
	return ExternalRSVPProjection{
		Title:               snapshot.Title,
		StartsAt:            startsAt,
		EndsAt:              endsAt,
		Timezone:            snapshot.Timezone,
		RSVPState:           row.RSVPState,
		ResponseRequested:   row.ResponseRequested,
		AttendeeVersion:     row.AttendeeVersion,
		InvitationSequence:  row.InvitationSequence,
		CapabilityExpiresAt: row.ExpiresAt.UTC(),
	}, nil
}

func readExternalRSVPReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	target externalCapabilityRow,
	idempotencyKey string,
	fingerprint []byte,
) (bool, error) {
	var storedFingerprint []byte
	var operation string
	var capabilityID uuid.NullUUID
	var attendeeID uuid.NullUUID
	var resultVersion int64
	err := transaction.QueryRow(
		ctx,
		`SELECT request_fingerprint, operation, capability_id,
       result_attendee_id, result_version
FROM tutorhub.calendar_participation_mutation_receipts
WHERE tenant_id = $1
  AND idempotency_key = $2`,
		target.TenantID,
		idempotencyKey,
	).Scan(&storedFingerprint, &operation, &capabilityID, &attendeeID, &resultVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read external RSVP idempotency receipt: %w", err)
	}
	if operation != participationOperationRSVPRespond || !capabilityID.Valid ||
		capabilityID.UUID != target.CapabilityID || !attendeeID.Valid ||
		attendeeID.UUID != target.AttendeeID || resultVersion < 1 ||
		!bytes.Equal(storedFingerprint, fingerprint) {
		return false, ErrSessionParticipationIdempotencyConflict
	}
	return true, nil
}

func insertExternalRSVPReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	target externalCapabilityRow,
	idempotencyKey string,
	fingerprint []byte,
	createdAt time.Time,
) error {
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_participation_mutation_receipts (
    tenant_id, idempotency_key, request_fingerprint, operation,
    class_id, session_id, series_id, occurrence_key,
    actor_type, actor_user_id, capability_id,
    result_attendee_id, result_invitation_revision_id, result_version, created_at
)
VALUES (
    $1, $2, $3, 'rsvp_respond',
    $4, $5, $6, $7,
    'tutorhub_external_capability', NULL, $8,
    $9, $10, $11, $12
)`,
		target.TenantID,
		idempotencyKey,
		fingerprint,
		target.ClassID,
		target.SessionID,
		target.SeriesID,
		target.OccurrenceKey,
		target.CapabilityID,
		target.AttendeeID,
		target.RevisionID,
		target.AttendeeVersion,
		createdAt,
	)
	if err != nil {
		return mapParticipationPostgresError("insert external RSVP receipt", err)
	}
	return nil
}

func revokeExternalRSVPCapabilitiesForAttendee(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	attendeeID uuid.UUID,
	reason string,
	revokedAt time.Time,
) error {
	if tenantID == uuid.Nil || attendeeID == uuid.Nil || revokedAt.IsZero() ||
		!externalCapabilityRevocationReason.MatchString(reason) {
		return ErrExternalRSVPCapabilityUnavailable
	}
	_, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.calendar_rsvp_capabilities AS capability
SET revoked_at = $1,
    revoked_reason = $2
FROM tutorhub.calendar_invitation_recipients AS recipient
WHERE capability.tenant_id = $3
  AND capability.revoked_at IS NULL
  AND recipient.tenant_id = capability.tenant_id
  AND recipient.invitation_revision_id = capability.invitation_revision_id
  AND recipient.id = capability.invitation_recipient_id
  AND recipient.attendee_id = $4`,
		revokedAt.UTC(),
		reason,
		tenantID,
		attendeeID,
	)
	if err != nil {
		return fmt.Errorf("revoke external RSVP capabilities: %w", err)
	}
	return nil
}

const externalCapabilityIssueTargetQuery = `SELECT
    '00000000-0000-0000-0000-000000000000'::uuid AS capability_id,
    recipient.tenant_id,
    recipient.class_id,
    recipient.invitation_revision_id,
    recipient.id,
    recipient.attendee_id,
    attendee.session_id,
    attendee.series_id,
    attendee.occurrence_key,
    revision.created_at AS expires_at,
    attendee.version,
    attendee.rsvp_state,
    attendee.response_requested,
    attendee.response_closed_at,
    revision.ical_sequence,
    revision.canonical_payload_ciphertext,
    revision.crypto_key_version,
    decode(repeat('00', 32), 'hex') AS token_digest
FROM tutorhub.calendar_invitation_recipients AS recipient
JOIN tutorhub.calendar_invitation_revisions AS revision
  ON revision.tenant_id = recipient.tenant_id
 AND revision.class_id = recipient.class_id
 AND revision.id = recipient.invitation_revision_id
JOIN tutorhub.class_session_attendees AS attendee
  ON attendee.tenant_id = recipient.tenant_id
 AND attendee.class_id = recipient.class_id
 AND attendee.id = recipient.attendee_id
WHERE recipient.tenant_id = $1
  AND recipient.class_id = $2
  AND recipient.invitation_revision_id = $3
  AND recipient.id = $4
  AND recipient.recipient_kind = 'external'
  AND attendee.external_recipient_id IS NOT NULL
  AND attendee.status = 'active'
  AND NOT EXISTS (
      SELECT 1
      FROM tutorhub.calendar_invitation_revisions AS newer
      WHERE newer.tenant_id = revision.tenant_id
        AND newer.class_id = revision.class_id
        AND newer.session_id IS NOT DISTINCT FROM revision.session_id
        AND newer.series_id IS NOT DISTINCT FROM revision.series_id
        AND newer.occurrence_key IS NOT DISTINCT FROM revision.occurrence_key
        AND newer.ical_sequence > revision.ical_sequence
  )
FOR UPDATE OF attendee`

const externalCapabilityLookupQuery = `SELECT
    capability.id,
    capability.tenant_id,
    recipient.class_id,
    recipient.invitation_revision_id,
    recipient.id,
    recipient.attendee_id,
    attendee.session_id,
    attendee.series_id,
    attendee.occurrence_key,
    capability.expires_at,
    attendee.version,
    attendee.rsvp_state,
    attendee.response_requested,
    attendee.response_closed_at,
    revision.ical_sequence,
    revision.canonical_payload_ciphertext,
    revision.crypto_key_version,
    capability.token_digest
FROM tutorhub.calendar_rsvp_capabilities AS capability
JOIN tutorhub.calendar_invitation_recipients AS recipient
  ON recipient.tenant_id = capability.tenant_id
 AND recipient.invitation_revision_id = capability.invitation_revision_id
 AND recipient.id = capability.invitation_recipient_id
JOIN tutorhub.calendar_invitation_revisions AS revision
  ON revision.tenant_id = recipient.tenant_id
 AND revision.class_id = recipient.class_id
 AND revision.id = recipient.invitation_revision_id
JOIN tutorhub.class_session_attendees AS attendee
  ON attendee.tenant_id = recipient.tenant_id
 AND attendee.class_id = recipient.class_id
 AND attendee.id = recipient.attendee_id
WHERE capability.token_version = $1
  AND capability.token_digest = $2
  AND capability.purpose = $3
  AND capability.revoked_at IS NULL
  AND capability.expires_at > $4
  AND capability.use_count < 1000000
  AND recipient.recipient_kind = 'external'
  AND attendee.external_recipient_id IS NOT NULL
  AND attendee.status = 'active'
  AND attendee.response_closed_at IS NULL
  AND NOT EXISTS (
      SELECT 1
      FROM tutorhub.calendar_invitation_revisions AS newer
      WHERE newer.tenant_id = revision.tenant_id
        AND newer.class_id = revision.class_id
        AND newer.session_id IS NOT DISTINCT FROM revision.session_id
        AND newer.series_id IS NOT DISTINCT FROM revision.series_id
        AND newer.occurrence_key IS NOT DISTINCT FROM revision.occurrence_key
        AND newer.ical_sequence > revision.ical_sequence
  )
FOR UPDATE OF capability, attendee`
