package classroom

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

func (repository *PostgresRepository) insertSessionCancellationSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	source lockedParticipationSession,
	sourceVersion int64,
	sequence int64,
	cancelledAt time.Time,
	reason string,
) error {
	if source.AudienceRevision < 1 {
		return nil
	}
	recipients, err := repository.readInvitationSnapshotRecipients(
		ctx,
		transaction,
		tenantContext.TenantID,
		source.ClassID,
		source.ID,
		nil,
	)
	if err != nil {
		return err
	}
	for index := range recipients {
		recipients[index].ID = uuid.New()
	}
	return repository.insertInvitationSnapshotWithLifecycle(
		ctx,
		transaction,
		tenantContext,
		source,
		sourceVersion,
		source.AudienceRevision,
		sequence,
		source.ResponseRequested,
		uuid.New(),
		recipients,
		"cancelled",
		reason,
		cancelledAt.UTC(),
	)
}

func (repository *PostgresRepository) insertTypedCancellationSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	source lockedTypedParticipationSource,
	sourceVersion int64,
	sequence int64,
	cancelledAt time.Time,
	reason string,
) error {
	return repository.insertTypedLifecycleSnapshot(
		ctx,
		transaction,
		tenantContext,
		source,
		sourceVersion,
		sequence,
		cancelledAt,
		"cancelled",
		reason,
	)
}

func (repository *PostgresRepository) insertTypedLifecycleSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	source lockedTypedParticipationSource,
	sourceVersion int64,
	sequence int64,
	createdAt time.Time,
	lifecycle string,
	reason string,
) error {
	audienceSource := source.Ref
	audienceRevision := source.AudienceRevision
	if source.Ref.Kind == ParticipationSourceOccurrence &&
		!source.OccurrenceAudienceOverridden {
		audienceSource = SeriesParticipationSource(source.Ref.SeriesID)
		audienceRevision = source.InheritedAudienceRevision
	}
	if audienceRevision < 1 {
		return nil
	}
	recipients, err := repository.readTypedInvitationSnapshotRecipients(
		ctx,
		transaction,
		tenantContext.TenantID,
		source.ClassID,
		audienceSource,
		nil,
	)
	if err != nil {
		return err
	}
	for index := range recipients {
		recipients[index].ID = uuid.New()
	}
	return repository.insertTypedInvitationSnapshotWithLifecycle(
		ctx,
		transaction,
		tenantContext,
		source,
		sourceVersion,
		audienceRevision,
		sequence,
		source.ResponseRequested,
		uuid.New(),
		recipients,
		lifecycle,
		reason,
		createdAt.UTC(),
	)
}

// cancelSeriesWithParticipationSnapshot captures the active master audience
// and each occurrence-local override before the source is cancelled. The
// series revision carries the master cancellation; occurrence revisions keep
// stable RECURRENCE-ID cancellation lineage for recipients whose audience no
// longer follows the master.
func (repository *PostgresRepository) cancelSeriesWithParticipationSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	series ClassSessionSeries,
	cancelledAt time.Time,
	reason string,
) (ClassSessionSeries, error) {
	master, err := repository.lockTypedParticipationSource(
		ctx,
		transaction,
		series.TenantID,
		series.ClassID,
		SeriesParticipationSource(series.ID),
		true,
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	occurrences, err := repository.captureActiveOccurrenceCancellationSources(
		ctx,
		transaction,
		series.TenantID,
		series.ClassID,
		series.ID,
		nil,
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	result, err := cancelClassSessionSeries(
		ctx,
		transaction,
		series,
		tenantContext.ActorID,
		cancelledAt,
		reason,
	)
	if err != nil {
		return ClassSessionSeries{}, err
	}
	if err := repository.insertTypedCancellationSnapshot(
		ctx,
		transaction,
		tenantContext,
		master,
		result.Version,
		result.Sequence,
		cancelledAt,
		reason,
	); err != nil {
		return ClassSessionSeries{}, err
	}
	for _, occurrence := range occurrences {
		if err := repository.insertTypedCancellationSnapshot(
			ctx,
			transaction,
			tenantContext,
			occurrence,
			result.Version,
			nextCancellationSequence(occurrence.Sequence, result.Sequence),
			cancelledAt,
			reason,
		); err != nil {
			return ClassSessionSeries{}, err
		}
	}
	return result, nil
}

// snapshotFollowingCancellation records the shortened active master as an
// update and emits cancellation revisions only for active occurrence-local
// overrides removed by the boundary. Inherited future occurrences are covered
// by the master revision and must not be fanned out as synthetic overrides.
func (repository *PostgresRepository) snapshotFollowingCancellation(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	master lockedTypedParticipationSource,
	occurrences []lockedTypedParticipationSource,
	result ClassSessionSeries,
	cancelledAt time.Time,
	reason string,
) error {
	if err := repository.insertTypedLifecycleSnapshot(
		ctx,
		transaction,
		tenantContext,
		master,
		result.Version,
		result.Sequence,
		cancelledAt,
		"updated",
		reason,
	); err != nil {
		return err
	}
	for _, occurrence := range occurrences {
		if err := repository.insertTypedCancellationSnapshot(
			ctx,
			transaction,
			tenantContext,
			occurrence,
			result.Version,
			nextCancellationSequence(occurrence.Sequence, result.Sequence),
			cancelledAt,
			reason,
		); err != nil {
			return err
		}
	}
	return nil
}

func nextCancellationSequence(current int64, floor int64) int64 {
	next := current + 1
	if floor > next {
		return floor
	}
	return next
}

// captureActiveOccurrenceCancellationSources freezes the exact occurrence
// sources that have an invitation history. Inherited virtual occurrences are
// cancelled by the series revision; exact occurrence overrides need their own
// RECURRENCE-ID cancellation snapshot.
func (repository *PostgresRepository) captureActiveOccurrenceCancellationSources(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	seriesID uuid.UUID,
	allowedOccurrenceKeys []string,
) ([]lockedTypedParticipationSource, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT latest.occurrence_key
FROM (
    SELECT DISTINCT ON (occurrence_key)
           occurrence_key, lifecycle
    FROM tutorhub.calendar_invitation_revisions
    WHERE tenant_id = $1
      AND class_id = $2
      AND session_id IS NULL
      AND series_id = $3
      AND occurrence_key IS NOT NULL
    ORDER BY occurrence_key, ical_sequence DESC, created_at DESC, id DESC
) AS latest
WHERE latest.lifecycle <> 'cancelled'
ORDER BY latest.occurrence_key`,
		tenantID,
		classID,
		seriesID,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"list active occurrence invitation sources for cancellation: %w",
			err,
		)
	}
	defer rows.Close()
	allowed := make(map[string]struct{}, len(allowedOccurrenceKeys))
	for _, occurrenceKey := range allowedOccurrenceKeys {
		allowed[occurrenceKey] = struct{}{}
	}
	keys := make([]string, 0)
	for rows.Next() {
		var occurrenceKey string
		if err := rows.Scan(&occurrenceKey); err != nil {
			return nil, fmt.Errorf(
				"scan occurrence invitation source for cancellation: %w",
				err,
			)
		}
		if allowedOccurrenceKeys != nil {
			if _, ok := allowed[occurrenceKey]; !ok {
				continue
			}
		}
		keys = append(keys, occurrenceKey)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate occurrence invitation sources for cancellation: %w",
			err,
		)
	}
	rows.Close()

	sources := make([]lockedTypedParticipationSource, 0, len(keys))
	for _, occurrenceKey := range keys {
		source, err := repository.lockTypedParticipationSource(
			ctx,
			transaction,
			tenantID,
			classID,
			OccurrenceParticipationSource(seriesID, occurrenceKey),
			true,
		)
		if errors.Is(err, ErrSessionParticipationNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !source.OccurrenceAudienceOverridden {
			continue
		}
		sources = append(sources, source)
	}
	return sources, nil
}

// closeParticipationForSource closes response authority and revokes public
// RSVP capabilities for a cancelled source. A one-time session and a single
// recurring occurrence have an exact source scope; a series cancellation
// intentionally covers every materialized occurrence owned by that series.
//
// The operation is idempotent. This is important for retries (and for
// cancellations that happened before the participation lifecycle hook was
// deployed): already-closed responses and already-revoked capabilities are
// left unchanged.
func closeParticipationForSource(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
	actorID uuid.UUID,
	closedAt time.Time,
	reason string,
) error {
	normalized, err := source.Normalized()
	if err != nil {
		return err
	}
	if !externalCapabilityRevocationReason.MatchString(reason) {
		return fmt.Errorf("invalid participation lifecycle reason %q", reason)
	}

	switch normalized.Kind {
	case ParticipationSourceSession, ParticipationSourceOccurrence:
		sessionID, seriesID, occurrenceKey := participationScopeValues(normalized)
		if err := closeParticipationRows(
			ctx,
			transaction,
			tenantID,
			classID,
			sessionID,
			seriesID,
			occurrenceKey,
			nil,
			actorID,
			closedAt,
		); err != nil {
			return err
		}
		return revokeParticipationCapabilitiesByScope(
			ctx,
			transaction,
			tenantID,
			classID,
			sessionID,
			seriesID,
			occurrenceKey,
			nil,
			reason,
			closedAt,
		)
	case ParticipationSourceSeries:
		// A series source is the inherited audience authority for all of its
		// occurrences. When the whole series is cancelled, closing only rows
		// with occurrence_key IS NULL would leave materialized occurrence
		// overrides response-capable, so deliberately include every key.
		_, seriesID, _ := participationScopeValues(normalized)
		if err := closeParticipationRows(
			ctx,
			transaction,
			tenantID,
			classID,
			nil,
			seriesID,
			nil,
			[]string{},
			actorID,
			closedAt,
		); err != nil {
			return err
		}
		return revokeParticipationCapabilitiesByScope(
			ctx,
			transaction,
			tenantID,
			classID,
			nil,
			seriesID,
			nil,
			[]string{},
			reason,
			closedAt,
		)
	default:
		return fmt.Errorf("unsupported participation lifecycle source %q", normalized.Kind)
	}
}

// closeParticipationForSeriesOccurrenceKeys is used by the "following"
// recurrence mutation. It closes only materialized occurrence rows after the
// boundary; inherited series rows remain available for the retained prefix.
func closeParticipationForSeriesOccurrenceKeys(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	seriesID uuid.UUID,
	occurrenceKeys []string,
	actorID uuid.UUID,
	closedAt time.Time,
	reason string,
) error {
	if seriesID == uuid.Nil || len(occurrenceKeys) == 0 {
		return nil
	}
	if !externalCapabilityRevocationReason.MatchString(reason) {
		return fmt.Errorf("invalid participation lifecycle reason %q", reason)
	}
	keys := make([]string, 0, len(occurrenceKeys))
	seen := make(map[string]struct{}, len(occurrenceKeys))
	for _, key := range occurrenceKeys {
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	if err := closeParticipationRows(
		ctx,
		transaction,
		tenantID,
		classID,
		nil,
		seriesID,
		nil,
		keys,
		actorID,
		closedAt,
	); err != nil {
		return err
	}
	return revokeParticipationCapabilitiesByScope(
		ctx,
		transaction,
		tenantID,
		classID,
		nil,
		seriesID,
		nil,
		keys,
		reason,
		closedAt,
	)
}

// closeParticipationRows updates attendee rows in the same transaction as
// the source lifecycle mutation. occurrenceKeys == nil means an exact
// occurrence_key value (including NULL); a non-empty slice means materialized
// occurrence rows selected by ANY(...), while an empty non-nil slice means
// every occurrence owned by a fully cancelled series.
func closeParticipationRows(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID any,
	seriesID any,
	occurrenceKey any,
	occurrenceKeys []string,
	actorID uuid.UUID,
	closedAt time.Time,
) error {
	query := `UPDATE tutorhub.class_session_attendees
SET response_closed_at = COALESCE(response_closed_at, $6),
    updated_by = $5,
    updated_at = GREATEST(updated_at, $6),
    version = CASE
        WHEN response_closed_at IS NULL THEN version + 1
        ELSE version
    END
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND occurrence_key IS NOT DISTINCT FROM $7::text
  AND status = 'active'
  AND response_closed_at IS NULL`
	args := []any{
		tenantID,
		classID,
		sessionID,
		seriesID,
		actorID,
		closedAt.UTC(),
		occurrenceKey,
	}
	if occurrenceKeys != nil && len(occurrenceKeys) == 0 {
		query = `UPDATE tutorhub.class_session_attendees
SET response_closed_at = COALESCE(response_closed_at, $6),
    updated_by = $5,
    updated_at = GREATEST(updated_at, $6),
    version = CASE
        WHEN response_closed_at IS NULL THEN version + 1
        ELSE version
    END
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND status = 'active'
  AND response_closed_at IS NULL`
		args = args[:6]
	} else if len(occurrenceKeys) > 0 {
		query = `UPDATE tutorhub.class_session_attendees
SET response_closed_at = COALESCE(response_closed_at, $6),
    updated_by = $5,
    updated_at = GREATEST(updated_at, $6),
    version = CASE
        WHEN response_closed_at IS NULL THEN version + 1
        ELSE version
    END
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND occurrence_key = ANY($7::text[])
  AND status = 'active'
  AND response_closed_at IS NULL`
		args[6] = occurrenceKeys
	}
	if _, err := transaction.Exec(ctx, query, args...); err != nil {
		return mapParticipationPostgresError("close cancelled participation responses", err)
	}
	return nil
}

func revokeParticipationCapabilitiesByScope(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID any,
	seriesID any,
	occurrenceKey any,
	occurrenceKeys []string,
	reason string,
	revokedAt time.Time,
) error {
	query := `UPDATE tutorhub.calendar_rsvp_capabilities AS capability
SET revoked_at = $6,
    revoked_reason = $5
FROM tutorhub.calendar_invitation_recipients AS recipient
JOIN tutorhub.calendar_invitation_revisions AS revision
  ON revision.tenant_id = recipient.tenant_id
 AND revision.class_id = recipient.class_id
 AND revision.id = recipient.invitation_revision_id
WHERE capability.tenant_id = $1
  AND capability.invitation_revision_id = recipient.invitation_revision_id
  AND capability.invitation_recipient_id = recipient.id
  AND revision.tenant_id = $1
  AND revision.class_id = $2
  AND revision.session_id IS NOT DISTINCT FROM $3::uuid
  AND revision.series_id IS NOT DISTINCT FROM $4::uuid
  AND revision.occurrence_key IS NOT DISTINCT FROM $7::text
  AND capability.revoked_at IS NULL`
	args := []any{
		tenantID,
		classID,
		sessionID,
		seriesID,
		reason,
		revokedAt.UTC(),
		occurrenceKey,
	}
	if occurrenceKeys != nil && len(occurrenceKeys) == 0 {
		query = `UPDATE tutorhub.calendar_rsvp_capabilities AS capability
SET revoked_at = $6,
    revoked_reason = $5
FROM tutorhub.calendar_invitation_recipients AS recipient
JOIN tutorhub.calendar_invitation_revisions AS revision
  ON revision.tenant_id = recipient.tenant_id
 AND revision.class_id = recipient.class_id
 AND revision.id = recipient.invitation_revision_id
WHERE capability.tenant_id = $1
  AND capability.invitation_revision_id = recipient.invitation_revision_id
  AND capability.invitation_recipient_id = recipient.id
  AND revision.tenant_id = $1
  AND revision.class_id = $2
  AND revision.session_id IS NOT DISTINCT FROM $3::uuid
  AND revision.series_id IS NOT DISTINCT FROM $4::uuid
  AND capability.revoked_at IS NULL`
		args = args[:6]
	} else if len(occurrenceKeys) > 0 {
		query = `UPDATE tutorhub.calendar_rsvp_capabilities AS capability
SET revoked_at = $6,
    revoked_reason = $5
FROM tutorhub.calendar_invitation_recipients AS recipient
JOIN tutorhub.calendar_invitation_revisions AS revision
  ON revision.tenant_id = recipient.tenant_id
 AND revision.class_id = recipient.class_id
 AND revision.id = recipient.invitation_revision_id
WHERE capability.tenant_id = $1
  AND capability.invitation_revision_id = recipient.invitation_revision_id
  AND capability.invitation_recipient_id = recipient.id
  AND revision.tenant_id = $1
  AND revision.class_id = $2
  AND revision.session_id IS NOT DISTINCT FROM $3::uuid
  AND revision.series_id IS NOT DISTINCT FROM $4::uuid
  AND revision.occurrence_key = ANY($7::text[])
  AND capability.revoked_at IS NULL`
		args[6] = occurrenceKeys
	}
	if _, err := transaction.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("revoke cancelled participation RSVP capabilities: %w", err)
	}
	return nil
}

// closeClassParticipationForArchive is the class-lifecycle half of ADR-0020.
// It deliberately does not invent a replacement organizer or a delivery
// effect. The existing class.archived outbox fact remains the durable signal;
// this transaction only closes current response authority and revokes every
// still-live public capability before the archive becomes visible.
func closeClassParticipationForArchive(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	actorID uuid.UUID,
	archivedAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.class_session_attendees
SET response_closed_at = COALESCE(response_closed_at, $4),
    updated_by = $3,
    updated_at = GREATEST(updated_at, $4),
    version = CASE
        WHEN response_closed_at IS NULL THEN version + 1
        ELSE version
    END
WHERE tenant_id = $1
  AND class_id = $2
  AND status = 'active'
  AND response_closed_at IS NULL`,
		tenantID,
		classID,
		actorID,
		archivedAt.UTC(),
	); err != nil {
		return mapClassPostgresError("close archived class participation", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.calendar_rsvp_capabilities AS capability
SET revoked_at = $3,
    revoked_reason = 'class_archived'
FROM tutorhub.calendar_invitation_recipients AS recipient
JOIN tutorhub.calendar_invitation_revisions AS revision
  ON revision.tenant_id = recipient.tenant_id
 AND revision.class_id = recipient.class_id
 AND revision.id = recipient.invitation_revision_id
WHERE capability.tenant_id = $1
  AND capability.invitation_revision_id = recipient.invitation_revision_id
  AND capability.invitation_recipient_id = recipient.id
  AND revision.tenant_id = $1
  AND revision.class_id = $2
  AND capability.revoked_at IS NULL`,
		tenantID,
		classID,
		archivedAt.UTC(),
	); err != nil {
		return fmt.Errorf("revoke archived class RSVP capabilities: %w", err)
	}
	return nil
}
