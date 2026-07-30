package classroom

// This file contains the storage boundary for manually-entered external
// attendees. External addresses are tenant-deduplicated by a keyed HMAC and
// encrypted at rest. The regular audience replacement path intentionally
// keeps its roster queries internal-only; these helpers are called explicitly
// after the source and class have been locked and authorized.

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type resolvedExternalAudienceMember struct {
	ExternalRecipientID uuid.UUID
	Fingerprint         [32]byte
	Email               string
	DisplayName         string
	ParticipationRole   ParticipationRole
	Locale              string
	ViewerTimezone      string
}

type persistedExternalAttendee struct {
	ID                  uuid.UUID
	ExternalRecipientID uuid.UUID
	Fingerprint         [32]byte
	ParticipationRole   ParticipationRole
	ResponseRequested   bool
	RSVPState           RSVPState
	RSVPSource          string
	RespondedAt         *time.Time
	ResponseNote        *string
	Version             int64
	Email               string
	DisplayName         string
	Locale              string
	ViewerTimezone      string
}

// storedAudienceFingerprint avoids writing a plain SHA-256 of an email/name
// payload to mutation receipts. Existing internal-only callers retain their
// legacy digest for compatibility; any request carrying an external address
// uses a dedicated keyed HMAC instead.
func (repository *PostgresRepository) storedAudienceFingerprint(
	tenantID uuid.UUID,
	params AudienceReplacementParams,
) ([]byte, error) {
	if len(params.ExternalAttendees) == 0 {
		return decodeParticipationFingerprint(params.Fingerprint)
	}
	if repository.calendarProtector == nil || tenantID == uuid.Nil {
		return nil, ErrSessionParticipationUnavailable
	}
	digest, err := repository.calendarProtector.AudienceMutationDigest(
		[]byte(params.Fingerprint),
	)
	if err != nil {
		return nil, ErrSessionParticipationUnavailable
	}
	return digest[:], nil
}

func (repository *PostgresRepository) resolveExternalAudience(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	inputs []ExternalAudienceAttendee,
	changedAt time.Time,
) ([]resolvedExternalAudienceMember, error) {
	if len(inputs) == 0 {
		return []resolvedExternalAudienceMember{}, nil
	}
	if repository.calendarProtector == nil || tenantContext.Validate() != nil || changedAt.IsZero() {
		return nil, ErrSessionParticipationUnavailable
	}
	addresses := make([]string, 0, len(inputs))
	for _, input := range inputs {
		addresses = append(addresses, input.Email)
	}
	rows, err := transaction.Query(
		ctx,
		`SELECT lower(app_user.email)
FROM tutorhub.users AS app_user
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = $1
 AND membership.user_id = app_user.id
 AND membership.status = 'active'
WHERE app_user.status = 'active'
  AND lower(app_user.email) = ANY($2::text[])
FOR SHARE OF membership, app_user`,
		tenantContext.TenantID,
		addresses,
	)
	if err != nil {
		return nil, fmt.Errorf("check external audience internal identities: %w", err)
	}
	internalAddresses := make(map[string]struct{}, len(addresses))
	for rows.Next() {
		var address string
		if scanErr := rows.Scan(&address); scanErr != nil {
			rows.Close()
			return nil, fmt.Errorf("scan external audience internal identity: %w", scanErr)
		}
		internalAddresses[strings.ToLower(strings.TrimSpace(address))] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate external audience internal identities: %w", err)
	}
	rows.Close()

	resolved := make([]resolvedExternalAudienceMember, 0, len(inputs))
	for _, input := range inputs {
		if _, exists := internalAddresses[input.Email]; exists {
			// Do not reveal whether the address matched an active roster member.
			return nil, ErrInvalidParticipationInput
		}
		fingerprint, fingerprintErr := repository.calendarProtector.DeliveryAddressFingerprint(
			tenantContext.TenantID.String(), []byte(input.Email),
		)
		if fingerprintErr != nil {
			return nil, ErrInvalidParticipationInput
		}
		var (
			recipientID       uuid.UUID
			addressCiphertext []byte
			nameCiphertext    []byte
			keyVersion        int16
			status            string
		)
		err := transaction.QueryRow(
			ctx,
			`SELECT id, delivery_address_ciphertext, display_name_ciphertext,
       crypto_key_version, status
FROM tutorhub.calendar_external_recipients
WHERE tenant_id = $1
  AND delivery_address_fingerprint = $2
  AND status = 'active'
FOR UPDATE`,
			tenantContext.TenantID,
			fingerprint[:],
		).Scan(&recipientID, &addressCiphertext, &nameCiphertext, &keyVersion, &status)
		if err != nil && !isNoRows(err) {
			return nil, fmt.Errorf("read external audience recipient: %w", err)
		}
		if err == nil {
			if status != "active" || recipientID == uuid.Nil {
				return nil, ErrSessionParticipationUnavailable
			}
			address, openErr := repository.calendarProtector.Open(protecteddata.Context{
				TenantID: tenantContext.TenantID.String(),
				Purpose:  protecteddata.PurposeInvitationRecipientAddress,
				RecordID: recipientID.String(),
			}, protecteddata.SealedValue{KeyVersion: keyVersion, Ciphertext: addressCiphertext})
			if openErr != nil || subtle.ConstantTimeCompare(address, []byte(input.Email)) != 1 {
				return nil, ErrSessionParticipationUnavailable
			}
		} else {
			recipientID = uuid.New()
			sealedAddress, sealErr := repository.calendarProtector.Seal(protecteddata.Context{
				TenantID: tenantContext.TenantID.String(),
				Purpose:  protecteddata.PurposeInvitationRecipientAddress,
				RecordID: recipientID.String(),
			}, []byte(input.Email))
			if sealErr != nil {
				return nil, ErrSessionParticipationUnavailable
			}
			sealedName, sealErr := repository.calendarProtector.Seal(protecteddata.Context{
				TenantID: tenantContext.TenantID.String(),
				Purpose:  protecteddata.PurposeInvitationRecipientDisplayName,
				RecordID: recipientID.String(),
			}, []byte(input.DisplayName))
			if sealErr != nil {
				return nil, ErrSessionParticipationUnavailable
			}
			if _, insertErr := transaction.Exec(
				ctx,
				`INSERT INTO tutorhub.calendar_external_recipients (
    id, tenant_id, delivery_address_fingerprint, delivery_address_ciphertext,
    display_name_ciphertext, crypto_key_version, status, created_by,
    created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, 'active', $7, $8, $8)`,
				recipientID,
				tenantContext.TenantID,
				fingerprint[:],
				sealedAddress.Ciphertext,
				sealedName.Ciphertext,
				sealedAddress.KeyVersion,
				tenantContext.ActorID,
				changedAt.UTC(),
			); insertErr != nil {
				return nil, mapParticipationPostgresError("insert external audience recipient", insertErr)
			}
		}
		resolved = append(resolved, resolvedExternalAudienceMember{
			ExternalRecipientID: recipientID,
			Fingerprint:         fingerprint,
			Email:               input.Email,
			DisplayName:         input.DisplayName,
			ParticipationRole:   input.ParticipationRole,
			Locale:              input.Locale,
			ViewerTimezone:      input.ViewerTimezone,
		})
	}
	return resolved, nil
}

// isNoRows avoids importing pgx sentinel checks at every caller while keeping
// generic error mapping at the HTTP boundary.
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func (repository *PostgresRepository) lockExternalAudienceAttendees(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
) ([]persistedExternalAttendee, error) {
	return repository.queryExternalAudienceAttendees(
		ctx,
		transaction,
		tenantID,
		classID,
		source,
		true,
	)
}

func (repository *PostgresRepository) readExternalAudienceAttendees(
	ctx context.Context,
	queryer participationQuerier,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
) ([]persistedExternalAttendee, error) {
	return repository.queryExternalAudienceAttendees(
		ctx,
		queryer,
		tenantID,
		classID,
		source,
		false,
	)
}

func (repository *PostgresRepository) queryExternalAudienceAttendees(
	ctx context.Context,
	queryer participationQuerier,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
	lockRows bool,
) ([]persistedExternalAttendee, error) {
	if repository.calendarProtector == nil {
		return nil, ErrSessionParticipationUnavailable
	}
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	query := `SELECT attendee.id, attendee.external_recipient_id,
       attendee.participation_role, attendee.response_requested,
       attendee.rsvp_state, attendee.rsvp_source, attendee.responded_at,
       attendee.response_note, attendee.version,
       recipient.delivery_address_fingerprint,
       recipient.delivery_address_ciphertext, recipient.display_name_ciphertext,
       recipient.crypto_key_version,
       COALESCE(snapshot.locale, 'vi-VN'),
       COALESCE(snapshot.viewer_timezone, 'UTC')
FROM tutorhub.class_session_attendees AS attendee
JOIN tutorhub.calendar_external_recipients AS recipient
  ON recipient.tenant_id = attendee.tenant_id
 AND recipient.id = attendee.external_recipient_id
 AND recipient.status = 'active'
LEFT JOIN LATERAL (
    SELECT invitation_recipient.locale, invitation_recipient.viewer_timezone
    FROM tutorhub.calendar_invitation_recipients AS invitation_recipient
    JOIN tutorhub.calendar_invitation_revisions AS revision
      ON revision.tenant_id = invitation_recipient.tenant_id
     AND revision.class_id = invitation_recipient.class_id
     AND revision.id = invitation_recipient.invitation_revision_id
    WHERE invitation_recipient.tenant_id = attendee.tenant_id
      AND invitation_recipient.class_id = attendee.class_id
      AND invitation_recipient.attendee_id = attendee.id
      AND invitation_recipient.recipient_kind = 'external'
    ORDER BY revision.ical_sequence DESC, revision.created_at DESC
    LIMIT 1
) AS snapshot ON true
WHERE attendee.tenant_id = $1
  AND attendee.class_id = $2
  AND attendee.session_id IS NOT DISTINCT FROM $3::uuid
  AND attendee.series_id IS NOT DISTINCT FROM $4::uuid
  AND attendee.occurrence_key IS NOT DISTINCT FROM $5::text
  AND attendee.external_recipient_id IS NOT NULL
  AND attendee.status = 'active'`
	if lockRows {
		query += "\nFOR UPDATE OF attendee, recipient"
	}
	rows, err := queryer.Query(
		ctx,
		query,
		tenantID, classID, sessionID, seriesID, occurrenceKey,
	)
	if err != nil {
		return nil, fmt.Errorf("lock external participation attendees: %w", err)
	}
	defer rows.Close()
	attendees := make([]persistedExternalAttendee, 0)
	for rows.Next() {
		var (
			attendee          persistedExternalAttendee
			fingerprint       []byte
			addressCiphertext []byte
			nameCiphertext    []byte
			keyVersion        int16
		)
		if scanErr := rows.Scan(&attendee.ID, &attendee.ExternalRecipientID,
			&attendee.ParticipationRole, &attendee.ResponseRequested,
			&attendee.RSVPState, &attendee.RSVPSource, &attendee.RespondedAt,
			&attendee.ResponseNote, &attendee.Version, &fingerprint,
			&addressCiphertext, &nameCiphertext, &keyVersion,
			&attendee.Locale, &attendee.ViewerTimezone); scanErr != nil {
			return nil, fmt.Errorf("scan external participation attendee: %w", scanErr)
		}
		if len(fingerprint) != 32 || len(addressCiphertext) == 0 || attendee.ExternalRecipientID == uuid.Nil {
			return nil, ErrSessionParticipationUnavailable
		}
		copy(attendee.Fingerprint[:], fingerprint)
		address, openErr := repository.calendarProtector.Open(protecteddata.Context{
			TenantID: tenantID.String(), Purpose: protecteddata.PurposeInvitationRecipientAddress,
			RecordID: attendee.ExternalRecipientID.String(),
		}, protecteddata.SealedValue{KeyVersion: keyVersion, Ciphertext: addressCiphertext})
		if openErr != nil || len(address) < 3 || len(address) > 320 {
			return nil, ErrSessionParticipationUnavailable
		}
		name := []byte{}
		if len(nameCiphertext) > 0 {
			name, openErr = repository.calendarProtector.Open(protecteddata.Context{
				TenantID: tenantID.String(), Purpose: protecteddata.PurposeInvitationRecipientDisplayName,
				RecordID: attendee.ExternalRecipientID.String(),
			}, protecteddata.SealedValue{KeyVersion: keyVersion, Ciphertext: nameCiphertext})
			if openErr != nil {
				return nil, ErrSessionParticipationUnavailable
			}
		}
		attendee.Email = strings.ToLower(strings.TrimSpace(string(address)))
		attendee.DisplayName = strings.TrimSpace(string(name))
		attendee.Locale = normalizeInvitationLocale(attendee.Locale)
		attendee.ViewerTimezone = strings.TrimSpace(attendee.ViewerTimezone)
		if attendee.Email == "" || attendee.DisplayName == "" || !validExternalRSVPAudienceAttendee(attendee) {
			return nil, ErrSessionParticipationUnavailable
		}
		attendees = append(attendees, attendee)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external participation attendees: %w", err)
	}
	return attendees, nil
}

func validExternalRSVPAudienceAttendee(attendee persistedExternalAttendee) bool {
	return attendee.ID != uuid.Nil && attendee.ExternalRecipientID != uuid.Nil &&
		(attendee.ParticipationRole == ParticipationRoleRequired || attendee.ParticipationRole == ParticipationRoleOptional) &&
		attendee.Email != "" && attendee.DisplayName != "" && attendee.Version > 0
}

func (attendee persistedExternalAttendee) toAudienceExternalAttendee() SessionAudienceExternalAttendee {
	return SessionAudienceExternalAttendee{
		ID:                attendee.ID,
		Email:             attendee.Email,
		DisplayName:       attendee.DisplayName,
		ParticipationRole: attendee.ParticipationRole,
		Locale:            attendee.Locale,
		ViewerTimezone:    attendee.ViewerTimezone,
		RSVPState:         attendee.RSVPState,
		RespondedAt:       attendee.RespondedAt,
		Version:           attendee.Version,
	}
}

func mergeInheritedExternalAudienceAttendees(
	inherited []persistedExternalAttendee,
	exact []persistedExternalAttendee,
) []persistedExternalAttendee {
	if len(exact) == 0 {
		return inherited
	}
	exactByRecipient := make(
		map[uuid.UUID]persistedExternalAttendee,
		len(exact),
	)
	for _, attendee := range exact {
		exactByRecipient[attendee.ExternalRecipientID] = attendee
	}
	merged := make([]persistedExternalAttendee, 0, len(inherited))
	for _, attendee := range inherited {
		if override, exists := exactByRecipient[attendee.ExternalRecipientID]; exists {
			merged = append(merged, override)
			continue
		}
		merged = append(merged, attendee)
	}
	return merged
}

func externalAudienceReplacementChanges(
	currentResponseRequested bool,
	desiredResponseRequested bool,
	current []persistedExternalAttendee,
	desired []resolvedExternalAudienceMember,
) bool {
	if currentResponseRequested != desiredResponseRequested || len(current) != len(desired) {
		return true
	}
	byRecipient := make(map[uuid.UUID]persistedExternalAttendee, len(current))
	for _, attendee := range current {
		byRecipient[attendee.ExternalRecipientID] = attendee
	}
	for _, member := range desired {
		attendee, ok := byRecipient[member.ExternalRecipientID]
		if !ok || attendee.ParticipationRole != member.ParticipationRole ||
			attendee.DisplayName != member.DisplayName || attendee.Locale != member.Locale ||
			attendee.ViewerTimezone != member.ViewerTimezone {
			return true
		}
	}
	return false
}

func (repository *PostgresRepository) applyExternalAudienceReplacement(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
	showAs, visibility string,
	responseRequested bool,
	current []persistedExternalAttendee,
	desired []resolvedExternalAudienceMember,
	changedAt time.Time,
) error {
	currentByRecipient := make(map[uuid.UUID]persistedExternalAttendee, len(current))
	for _, attendee := range current {
		currentByRecipient[attendee.ExternalRecipientID] = attendee
	}
	desiredByRecipient := make(map[uuid.UUID]resolvedExternalAudienceMember, len(desired))
	for _, member := range desired {
		desiredByRecipient[member.ExternalRecipientID] = member
	}
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	for _, attendee := range current {
		member, remains := desiredByRecipient[attendee.ExternalRecipientID]
		if !remains {
			if _, err := transaction.Exec(ctx, `UPDATE tutorhub.class_session_attendees
SET status = 'removed', response_closed_at = $5, removed_by = $4,
    removed_at = $5, removal_reason = 'audience_replaced',
    updated_by = $4, updated_at = $5, version = version + 1
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND status = 'active'`,
				tenantContext.TenantID, classID, attendee.ID, tenantContext.ActorID, changedAt); err != nil {
				return mapParticipationPostgresError("remove external participation attendee", err)
			}
			if err := revokeExternalRSVPCapabilitiesForAttendee(ctx, transaction, tenantContext.TenantID, attendee.ID, "audience_replaced", changedAt); err != nil {
				return err
			}
			continue
		}
		reset := attendee.ParticipationRole != member.ParticipationRole || attendee.ResponseRequested != responseRequested
		displayNameChanged := attendee.DisplayName != member.DisplayName
		metadataChanged := displayNameChanged || attendee.Locale != member.Locale ||
			attendee.ViewerTimezone != member.ViewerTimezone
		if !reset && attendee.Email == member.Email && !metadataChanged {
			continue
		}
		if displayNameChanged {
			sealedName, sealErr := repository.calendarProtector.Seal(protecteddata.Context{
				TenantID: tenantContext.TenantID.String(),
				Purpose:  protecteddata.PurposeInvitationRecipientDisplayName,
				RecordID: attendee.ExternalRecipientID.String(),
			}, []byte(member.DisplayName))
			if sealErr != nil {
				return ErrSessionParticipationUnavailable
			}
			if _, updateErr := transaction.Exec(ctx, `UPDATE tutorhub.calendar_external_recipients
SET display_name_ciphertext = $1, crypto_key_version = $2, updated_at = $3
WHERE tenant_id = $4 AND id = $5 AND status = 'active'`,
				sealedName.Ciphertext, sealedName.KeyVersion, changedAt,
				tenantContext.TenantID, attendee.ExternalRecipientID); updateErr != nil {
				return fmt.Errorf("update external audience recipient display name: %w", updateErr)
			}
		}
		_, err := transaction.Exec(ctx, `UPDATE tutorhub.class_session_attendees
SET participation_role = $4, response_requested = $5,
    rsvp_state = CASE WHEN $6 THEN 'needs_action' ELSE rsvp_state END,
    rsvp_source = CASE WHEN $6 THEN 'none' ELSE rsvp_source END,
    responded_at = CASE WHEN $6 THEN NULL ELSE responded_at END,
    response_note = CASE WHEN $6 THEN NULL ELSE response_note END,
    response_invitation_revision_id = CASE WHEN $6 THEN NULL ELSE response_invitation_revision_id END,
    response_sequence = CASE WHEN $6 THEN NULL ELSE response_sequence END,
    response_closed_at = CASE WHEN $6 AND $5 THEN NULL WHEN $6 THEN $8 ELSE response_closed_at END,
    updated_by = $7, updated_at = $8, version = version + 1
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND status = 'active'`,
			tenantContext.TenantID, classID, attendee.ID,
			string(member.ParticipationRole), responseRequested, reset,
			tenantContext.ActorID, changedAt)
		if err != nil {
			return mapParticipationPostgresError("update external participation attendee", err)
		}
		if reset {
			if err := revokeExternalRSVPCapabilitiesForAttendee(ctx, transaction, tenantContext.TenantID, attendee.ID, "audience_changed", changedAt); err != nil {
				return err
			}
		}
	}
	for _, member := range desired {
		if _, exists := currentByRecipient[member.ExternalRecipientID]; exists {
			continue
		}
		if _, err := transaction.Exec(ctx, `INSERT INTO tutorhub.class_session_attendees (
    tenant_id, class_id, session_id, series_id, occurrence_key,
    external_recipient_id, participation_role, business_role, audience_source,
    show_as, visibility, can_invite_others, can_modify_event, can_see_guest_list,
    response_requested, response_closed_at, status, rsvp_state, rsvp_source,
    created_by, updated_by, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, 'external_guest', 'manual',
    $8, $9, false, false, false,
    $10, CASE WHEN $10 THEN NULL ELSE $12::timestamptz END,
    'active', 'needs_action', 'none', $11, $11, $12, $12)`,
			tenantContext.TenantID, classID, sessionID, seriesID, occurrenceKey,
			member.ExternalRecipientID, string(member.ParticipationRole),
			showAs, visibility, responseRequested, tenantContext.ActorID, changedAt); err != nil {
			return mapParticipationPostgresError("insert external participation attendee", err)
		}
	}
	return nil
}

func (repository *PostgresRepository) appendExternalInvitationSnapshotRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
	recipients []invitationSnapshotRecipient,
	desired []resolvedExternalAudienceMember,
) ([]invitationSnapshotRecipient, error) {
	if repository.calendarProtector == nil {
		return nil, ErrSessionParticipationUnavailable
	}
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	rows, err := transaction.Query(ctx, `SELECT attendee.id, attendee.external_recipient_id,
       attendee.participation_role, attendee.response_requested,
       attendee.rsvp_state, attendee.rsvp_source,
       recipient.delivery_address_fingerprint, recipient.delivery_address_ciphertext,
       recipient.display_name_ciphertext, recipient.crypto_key_version,
       COALESCE(snapshot.locale, 'vi-VN'),
       COALESCE(snapshot.viewer_timezone, 'UTC')
FROM tutorhub.class_session_attendees AS attendee
JOIN tutorhub.calendar_external_recipients AS recipient
  ON recipient.tenant_id = attendee.tenant_id
 AND recipient.id = attendee.external_recipient_id
 AND recipient.status = 'active'
LEFT JOIN LATERAL (
    SELECT invitation_recipient.locale, invitation_recipient.viewer_timezone
    FROM tutorhub.calendar_invitation_recipients AS invitation_recipient
    JOIN tutorhub.calendar_invitation_revisions AS revision
      ON revision.tenant_id = invitation_recipient.tenant_id
     AND revision.class_id = invitation_recipient.class_id
     AND revision.id = invitation_recipient.invitation_revision_id
    WHERE invitation_recipient.tenant_id = attendee.tenant_id
      AND invitation_recipient.class_id = attendee.class_id
      AND invitation_recipient.attendee_id = attendee.id
      AND invitation_recipient.recipient_kind = 'external'
    ORDER BY revision.ical_sequence DESC, revision.created_at DESC
    LIMIT 1
) AS snapshot ON true
WHERE attendee.tenant_id = $1 AND attendee.class_id = $2
  AND attendee.session_id IS NOT DISTINCT FROM $3::uuid
  AND attendee.series_id IS NOT DISTINCT FROM $4::uuid
  AND attendee.occurrence_key IS NOT DISTINCT FROM $5::text
  AND attendee.external_recipient_id IS NOT NULL
  AND attendee.status = 'active'
ORDER BY attendee.external_recipient_id ASC`, tenantID, classID, sessionID, seriesID, occurrenceKey)
	if err != nil {
		return nil, fmt.Errorf("read external invitation snapshot recipients: %w", err)
	}
	defer rows.Close()
	result := append([]invitationSnapshotRecipient(nil), recipients...)
	desiredByRecipient := make(map[uuid.UUID]resolvedExternalAudienceMember, len(desired))
	for _, member := range desired {
		desiredByRecipient[member.ExternalRecipientID] = member
	}
	for rows.Next() {
		var (
			r                                              invitationSnapshotRecipient
			externalID                                     uuid.UUID
			fingerprint, addressCiphertext, nameCiphertext []byte
			keyVersion                                     int16
		)
		if scanErr := rows.Scan(&r.AttendeeID, &externalID, &r.ParticipationRole,
			&r.ResponseRequested, &r.RSVPState, &r.RSVPSource,
			&fingerprint, &addressCiphertext, &nameCiphertext, &keyVersion,
			&r.Locale, &r.ViewerTimezone); scanErr != nil {
			return nil, fmt.Errorf("scan external invitation snapshot recipient: %w", scanErr)
		}
		if len(fingerprint) != 32 || externalID == uuid.Nil {
			return nil, ErrSessionParticipationUnavailable
		}
		copy(r.DeliveryAddressFingerprint[:], fingerprint)
		address, openErr := repository.calendarProtector.Open(protecteddata.Context{TenantID: tenantID.String(), Purpose: protecteddata.PurposeInvitationRecipientAddress, RecordID: externalID.String()}, protecteddata.SealedValue{KeyVersion: keyVersion, Ciphertext: addressCiphertext})
		if openErr != nil {
			return nil, ErrSessionParticipationUnavailable
		}
		name, openErr := repository.calendarProtector.Open(protecteddata.Context{TenantID: tenantID.String(), Purpose: protecteddata.PurposeInvitationRecipientDisplayName, RecordID: externalID.String()}, protecteddata.SealedValue{KeyVersion: keyVersion, Ciphertext: nameCiphertext})
		if openErr != nil {
			return nil, ErrSessionParticipationUnavailable
		}
		r.ID = r.AttendeeID
		r.UserID = uuid.Nil
		r.RecipientKind = "external"
		r.BusinessRole = "external_guest"
		r.AudienceSource = "manual"
		r.CanSeeGuestList = false
		r.Email = strings.ToLower(strings.TrimSpace(string(address)))
		r.DisplayName = strings.TrimSpace(string(name))
		r.Locale = normalizeInvitationLocale(r.Locale)
		r.ViewerTimezone = strings.TrimSpace(r.ViewerTimezone)
		if member, ok := desiredByRecipient[externalID]; ok {
			r.Locale = member.Locale
			r.ViewerTimezone = member.ViewerTimezone
		}
		if !validInvitationRecipient(r) {
			return nil, ErrSessionParticipationUnavailable
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external invitation snapshot recipients: %w", err)
	}
	return result, nil
}
