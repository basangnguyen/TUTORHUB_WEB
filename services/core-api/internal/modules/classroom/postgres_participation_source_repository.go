package classroom

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/calendar/recurrence"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type lockedTypedParticipationSource struct {
	Ref                   ParticipationSourceRef
	ID                    uuid.UUID
	ClassID               uuid.UUID
	Title                 string
	Description           string
	StartsAt              time.Time
	EndsAt                time.Time
	Timezone              string
	Status                SessionStatus
	Version               int64
	OrganizerUserID       uuid.UUID
	ShowAs                string
	Visibility            string
	GuestsCanInvite       bool
	GuestsCanModify       bool
	GuestsCanSeeGuestList bool
	ResponseRequested     bool
	AudienceRevision      int64
	ICalUID               string
	Sequence              int64
	// OccurrenceAudienceOverridden is true only after an occurrence has its
	// own immutable invitation revision. Before that point the occurrence
	// inherits the series audience and may contain sparse per-user RSVP rows.
	OccurrenceAudienceOverridden bool
	InheritedAudienceRevision    int64
	InheritedSequence            int64
}

type occurrenceParticipationState struct {
	RevisionID        uuid.UUID
	AudienceRevision  int64
	Sequence          int64
	ResponseRequested bool
}

type sourceScopedParticipationReceipt struct {
	Fingerprint   []byte
	Operation     string
	ClassID       uuid.UUID
	SessionID     uuid.NullUUID
	SeriesID      uuid.NullUUID
	OccurrenceKey *string
	ActorType     string
	ActorUserID   uuid.NullUUID
}

// GetParticipationAudience is the recurring-aware read authority. A session
// source is delegated to the battle-tested compatibility path, while series
// and occurrence identities use their explicit database scope.
func (repository *PostgresRepository) GetParticipationAudience(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
) (SessionAudience, error) {
	source, err := source.Normalized()
	if err != nil || tenantContext.Validate() != nil || classID == uuid.Nil {
		return SessionAudience{}, ErrSessionParticipationNotFound
	}
	if source.Kind == ParticipationSourceSession {
		audience, readErr := repository.GetSessionAudience(
			ctx,
			tenantContext,
			classID,
			source.SessionID,
		)
		if readErr != nil {
			return SessionAudience{}, readErr
		}
		audience.Source = source
		return audience, nil
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SessionAudience{}, fmt.Errorf("begin participation audience read: %w", err)
	}
	defer rollbackClassTransaction(transaction)
	if err := repository.requireSessionSchedulingFeature(
		queryContext,
		transaction,
		tenantContext.TenantID,
	); err != nil {
		return SessionAudience{}, err
	}
	locked, err := repository.lockTypedParticipationSource(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
		false,
	)
	if err != nil {
		return SessionAudience{}, err
	}
	audience, err := repository.readTypedParticipationAudience(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		locked,
	)
	if err != nil {
		return SessionAudience{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return SessionAudience{}, fmt.Errorf("commit participation audience read: %w", err)
	}
	return audience, nil
}

func (repository *PostgresRepository) ReplaceParticipationAudience(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
	params AudienceReplacementParams,
	replacedAt time.Time,
) (SessionAudienceMutationResult, error) {
	source, err := source.Normalized()
	if err != nil || tenantContext.Validate() != nil || classID == uuid.Nil {
		return SessionAudienceMutationResult{}, ErrSessionParticipationNotFound
	}
	params, err = repository.validateSourceAudienceParams(source, params)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if source.Kind == ParticipationSourceSession {
		result, replaceErr := repository.ReplaceSessionAudience(
			ctx,
			tenantContext,
			classID,
			source.SessionID,
			params,
			replacedAt,
		)
		result.Audience.Source = source
		return result, replaceErr
	}
	if repository.calendarProtector == nil {
		return SessionAudienceMutationResult{}, ErrSessionParticipationUnavailable
	}
	fingerprint, err := decodeParticipationFingerprint(params.Fingerprint)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SessionAudienceMutationResult{}, fmt.Errorf(
			"begin participation audience replacement: %w",
			err,
		)
	}
	defer rollbackClassTransaction(transaction)
	if err := repository.requireSessionSchedulingFeature(
		queryContext,
		transaction,
		tenantContext.TenantID,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	lockedClass, membership, err := repository.lockClassMutation(
		queryContext,
		transaction,
		tenantContext,
		classID,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext,
		membership,
		lockedClass.Class,
		policy.ActionSessionSchedule,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := lockParticipationIdempotency(
		queryContext,
		transaction,
		tenantContext.TenantID,
		params.IdempotencyKey,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	replay, err := findTypedParticipationReplay(
		queryContext,
		transaction,
		tenantContext,
		params.IdempotencyKey,
		fingerprint,
		participationOperationAudienceReplace,
		classID,
		source,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	locked, err := repository.lockTypedParticipationSource(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
		true,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if replay {
		audience, readErr := repository.readTypedParticipationAudience(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			locked,
		)
		if readErr != nil {
			return SessionAudienceMutationResult{}, readErr
		}
		if err := transaction.Commit(queryContext); err != nil {
			return SessionAudienceMutationResult{}, fmt.Errorf(
				"commit replayed participation audience replacement: %w",
				err,
			)
		}
		return SessionAudienceMutationResult{Audience: audience, Replayed: true}, nil
	}
	if locked.Status != SessionStatusScheduled {
		return SessionAudienceMutationResult{}, ErrInvalidSessionTransition
	}
	if locked.AudienceRevision != params.ExpectedAudienceRevision {
		return SessionAudienceMutationResult{}, ErrSessionAudienceVersionConflict
	}
	resolved, err := resolveInternalAudience(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		locked.OrganizerUserID,
		params.Attendees,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	current, err := lockTypedParticipationAttendees(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	effectiveCurrent := current
	if source.Kind == ParticipationSourceOccurrence &&
		!locked.OccurrenceAudienceOverridden {
		inherited, inheritedErr := lockTypedParticipationAttendees(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			SeriesParticipationSource(source.SeriesID),
		)
		if inheritedErr != nil {
			return SessionAudienceMutationResult{}, inheritedErr
		}
		effectiveCurrent = mergeInheritedParticipationAttendees(inherited, current)
	}
	if !audienceReplacementChanges(
		locked.ResponseRequested,
		params.ResponseRequested,
		effectiveCurrent,
		resolved,
	) {
		audience, readErr := repository.readTypedParticipationAudience(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			locked,
		)
		if readErr != nil {
			return SessionAudienceMutationResult{}, readErr
		}
		if err := insertTypedParticipationReceipt(
			queryContext,
			transaction,
			tenantContext,
			params.IdempotencyKey,
			fingerprint,
			participationOperationAudienceReplace,
			classID,
			source,
			uuid.Nil,
			uuid.Nil,
			locked.Version,
		); err != nil {
			return SessionAudienceMutationResult{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return SessionAudienceMutationResult{}, fmt.Errorf(
				"commit no-op participation audience replacement: %w",
				err,
			)
		}
		return SessionAudienceMutationResult{Audience: audience}, nil
	}

	changedAt := replacedAt.UTC()
	if err := applyTypedAudienceReplacement(
		queryContext,
		transaction,
		tenantContext,
		classID,
		locked,
		params,
		current,
		resolved,
		changedAt,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	newAudienceRevision := locked.AudienceRevision + 1
	newSequence := locked.Sequence
	if locked.AudienceRevision > 0 ||
		(source.Kind == ParticipationSourceOccurrence &&
			!locked.OccurrenceAudienceOverridden &&
			locked.InheritedAudienceRevision > 0) {
		newSequence++
	}
	updatedVersion, err := updateTypedParticipationSource(
		queryContext,
		transaction,
		tenantContext,
		classID,
		locked,
		params.ResponseRequested,
		newAudienceRevision,
		newSequence,
		changedAt,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	recipients, err := readTypedInvitationSnapshotRecipients(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	revisionID := uuid.New()
	for index := range recipients {
		recipients[index].ID = uuid.New()
	}
	if err := repository.insertTypedInvitationSnapshot(
		queryContext,
		transaction,
		tenantContext,
		locked,
		updatedVersion,
		newAudienceRevision,
		newSequence,
		params.ResponseRequested,
		revisionID,
		recipients,
		changedAt,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := insertTypedParticipationReceipt(
		queryContext,
		transaction,
		tenantContext,
		params.IdempotencyKey,
		fingerprint,
		participationOperationAudienceReplace,
		classID,
		source,
		uuid.Nil,
		revisionID,
		updatedVersion,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := insertTypedParticipationEvent(
		queryContext,
		transaction,
		tenantContext,
		classID,
		source,
		"class_session.audience_replaced.v1",
		newAudienceRevision,
		newSequence,
		len(recipients),
		"",
		changedAt,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	locked.Version = updatedVersion
	locked.AudienceRevision = newAudienceRevision
	locked.Sequence = newSequence
	locked.ResponseRequested = params.ResponseRequested
	if source.Kind == ParticipationSourceOccurrence {
		locked.OccurrenceAudienceOverridden = true
	}
	audience, err := repository.readTypedParticipationAudience(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		locked,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return SessionAudienceMutationResult{}, fmt.Errorf(
			"commit participation audience replacement: %w",
			err,
		)
	}
	return SessionAudienceMutationResult{Audience: audience}, nil
}

func (repository *PostgresRepository) RespondToParticipationSource(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
	params SelfRSVPParams,
	respondedAt time.Time,
) (SelfRSVPMutationResult, error) {
	source, err := source.Normalized()
	if err != nil || tenantContext.Validate() != nil || classID == uuid.Nil {
		return SelfRSVPMutationResult{}, ErrSessionParticipationNotFound
	}
	params, err = repository.validateSourceRSVPParams(source, params)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if source.Kind == ParticipationSourceSession {
		return repository.RespondToSession(
			ctx,
			tenantContext,
			classID,
			source.SessionID,
			params,
			respondedAt,
		)
	}
	fingerprint, err := decodeParticipationFingerprint(params.Fingerprint)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SelfRSVPMutationResult{}, fmt.Errorf(
			"begin participation RSVP response: %w",
			err,
		)
	}
	defer rollbackClassTransaction(transaction)
	if err := repository.requireSessionSchedulingFeature(
		queryContext,
		transaction,
		tenantContext.TenantID,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	lockedClass, membership, err := repository.lockClassMutation(
		queryContext,
		transaction,
		tenantContext,
		classID,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext,
		membership,
		lockedClass.Class,
		policy.ActionClassView,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := lockParticipationIdempotency(
		queryContext,
		transaction,
		tenantContext.TenantID,
		params.IdempotencyKey,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	replay, err := findTypedParticipationReplay(
		queryContext,
		transaction,
		tenantContext,
		params.IdempotencyKey,
		fingerprint,
		participationOperationRSVPRespond,
		classID,
		source,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	locked, err := repository.lockTypedParticipationSource(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source,
		true,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if replay {
		attendee, readErr := readTypedSelfAttendee(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			source,
			tenantContext.ActorID,
			false,
		)
		if readErr != nil {
			return SelfRSVPMutationResult{}, readErr
		}
		if err := transaction.Commit(queryContext); err != nil {
			return SelfRSVPMutationResult{}, fmt.Errorf(
				"commit replayed participation RSVP response: %w",
				err,
			)
		}
		result := attendee.toAudienceAttendee()
		result.IsSelf = true
		return SelfRSVPMutationResult{Attendee: result, Replayed: true}, nil
	}
	if locked.Status != SessionStatusScheduled || !locked.ResponseRequested {
		return SelfRSVPMutationResult{}, ErrSessionRSVPUnavailable
	}
	revisionSource := source
	revisionAudience := locked.AudienceRevision
	var attendee persistedSessionAttendee
	if source.Kind == ParticipationSourceOccurrence &&
		!locked.OccurrenceAudienceOverridden {
		locked, attendee, err = repository.materializeInheritedOccurrenceAudience(
			queryContext,
			transaction,
			tenantContext,
			classID,
			locked,
			respondedAt.UTC(),
		)
		revisionAudience = locked.AudienceRevision
	} else {
		attendee, err = readTypedSelfAttendee(
			queryContext,
			transaction,
			tenantContext.TenantID,
			classID,
			source,
			tenantContext.ActorID,
			true,
		)
	}
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if revisionAudience < 1 {
		return SelfRSVPMutationResult{}, ErrSessionRSVPUnavailable
	}
	if attendee.Version != params.ExpectedAttendeeVersion {
		return SelfRSVPMutationResult{}, ErrSessionAttendeeVersionConflict
	}
	if !attendee.ResponseRequested {
		return SelfRSVPMutationResult{}, ErrSessionRSVPUnavailable
	}
	if attendee.RSVPState == params.State &&
		attendee.RSVPSource == "tutorhub_authenticated" &&
		optionalTextEqual(attendee.ResponseNote, params.Note) {
		if err := insertTypedParticipationReceipt(
			queryContext,
			transaction,
			tenantContext,
			params.IdempotencyKey,
			fingerprint,
			participationOperationRSVPRespond,
			classID,
			source,
			attendee.ID,
			uuid.Nil,
			attendee.Version,
		); err != nil {
			return SelfRSVPMutationResult{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return SelfRSVPMutationResult{}, fmt.Errorf(
				"commit no-op participation RSVP response: %w",
				err,
			)
		}
		return SelfRSVPMutationResult{Attendee: attendee.toAudienceAttendee()}, nil
	}
	revisionID, sequence, err := findTypedInvitationRevision(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		revisionSource,
		revisionAudience,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	updated, err := updateTypedSelfRSVP(
		queryContext,
		transaction,
		tenantContext,
		classID,
		source,
		attendee,
		params,
		revisionID,
		sequence,
		respondedAt.UTC(),
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := insertTypedParticipationReceipt(
		queryContext,
		transaction,
		tenantContext,
		params.IdempotencyKey,
		fingerprint,
		participationOperationRSVPRespond,
		classID,
		source,
		updated.ID,
		revisionID,
		updated.Version,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := insertTypedParticipationEvent(
		queryContext,
		transaction,
		tenantContext,
		classID,
		source,
		"class_session.rsvp_responded.v1",
		locked.AudienceRevision,
		locked.Sequence,
		0,
		string(params.State),
		respondedAt.UTC(),
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return SelfRSVPMutationResult{}, fmt.Errorf(
			"commit participation RSVP response: %w",
			err,
		)
	}
	result := updated.toAudienceAttendee()
	result.IsSelf = true
	return SelfRSVPMutationResult{Attendee: result}, nil
}

func (repository *PostgresRepository) validateSourceAudienceParams(
	source ParticipationSourceRef,
	params AudienceReplacementParams,
) (AudienceReplacementParams, error) {
	normalized, err := (ReplaceAudienceInput{
		ExpectedAudienceRevision: params.ExpectedAudienceRevision,
		IdempotencyKey:           params.IdempotencyKey,
		ResponseRequested:        params.ResponseRequested,
		Attendees:                toAudienceInput(params.Attendees),
	}).normalized()
	if err != nil {
		return AudienceReplacementParams{}, err
	}
	bound, err := bindAudienceReplacementToSource(source, normalized)
	if err != nil {
		return AudienceReplacementParams{}, err
	}
	if params.Fingerprint != bound.Fingerprint {
		return AudienceReplacementParams{}, fmt.Errorf(
			"%w: source-bound request fingerprint mismatch",
			ErrInvalidParticipationInput,
		)
	}
	return bound, nil
}

func (repository *PostgresRepository) validateSourceRSVPParams(
	source ParticipationSourceRef,
	params SelfRSVPParams,
) (SelfRSVPParams, error) {
	normalized, err := (SelfRSVPInput{
		State:                   params.State,
		Note:                    params.Note,
		ExpectedAttendeeVersion: params.ExpectedAttendeeVersion,
		IdempotencyKey:          params.IdempotencyKey,
	}).normalized()
	if err != nil {
		return SelfRSVPParams{}, err
	}
	bound, err := bindSelfRSVPToSource(source, normalized)
	if err != nil {
		return SelfRSVPParams{}, err
	}
	if params.Fingerprint != bound.Fingerprint {
		return SelfRSVPParams{}, fmt.Errorf(
			"%w: source-bound request fingerprint mismatch",
			ErrInvalidParticipationInput,
		)
	}
	return bound, nil
}

func (repository *PostgresRepository) lockTypedParticipationSource(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
	forUpdate bool,
) (lockedTypedParticipationSource, error) {
	if source.Kind == ParticipationSourceSession {
		locked, err := lockParticipationSession(
			ctx,
			transaction,
			tenantID,
			classID,
			source.SessionID,
		)
		if err != nil {
			return lockedTypedParticipationSource{}, err
		}
		return lockedTypedParticipationSource{
			Ref:                   source,
			ID:                    locked.ID,
			ClassID:               locked.ClassID,
			Title:                 locked.Title,
			Description:           locked.Description,
			StartsAt:              locked.StartsAt,
			EndsAt:                locked.EndsAt,
			Timezone:              locked.Timezone,
			Status:                locked.Status,
			Version:               locked.Version,
			OrganizerUserID:       locked.OrganizerUserID,
			ShowAs:                locked.ShowAs,
			Visibility:            locked.Visibility,
			GuestsCanInvite:       locked.GuestsCanInvite,
			GuestsCanModify:       locked.GuestsCanModify,
			GuestsCanSeeGuestList: locked.GuestsCanSeeGuestList,
			ResponseRequested:     locked.ResponseRequested,
			AudienceRevision:      locked.AudienceRevision,
			ICalUID:               locked.ICalUID,
			Sequence:              locked.Sequence,
		}, nil
	}

	series, err := lockClassSessionSeries(ctx, transaction, tenantID, classID, source.SeriesID)
	if err != nil {
		if errors.Is(err, ErrSeriesNotFound) {
			return lockedTypedParticipationSource{}, ErrSessionParticipationNotFound
		}
		return lockedTypedParticipationSource{}, err
	}
	var organizerID uuid.UUID
	var showAs, visibility string
	var guestsCanInvite, guestsCanModify, guestsCanSeeGuestList bool
	var responseRequested bool
	var audienceRevision int64
	err = transaction.QueryRow(
		ctx,
		`SELECT organizer_user_id, show_as, visibility,
       guests_can_invite, guests_can_modify, guests_can_see_guest_list,
       response_requested, audience_revision
FROM tutorhub.class_session_series
WHERE tenant_id = $1 AND class_id = $2 AND id = $3`,
		tenantID,
		classID,
		source.SeriesID,
	).Scan(
		&organizerID,
		&showAs,
		&visibility,
		&guestsCanInvite,
		&guestsCanModify,
		&guestsCanSeeGuestList,
		&responseRequested,
		&audienceRevision,
	)
	if err != nil {
		return lockedTypedParticipationSource{}, fmt.Errorf(
			"read recurring participation source: %w",
			err,
		)
	}
	status := SessionStatusScheduled
	if series.Status != SeriesStatusScheduled {
		status = SessionStatusCancelled
	}
	first, err := recurrence.ResolveOccurrence(
		ctx,
		definitionFromSeries(series),
		series.LocalStart,
	)
	if err != nil {
		return lockedTypedParticipationSource{}, mapRecurrenceError(err)
	}
	locked := lockedTypedParticipationSource{
		Ref:                       source,
		ID:                        series.ID,
		ClassID:                   series.ClassID,
		Title:                     series.Title,
		Description:               series.Description,
		StartsAt:                  first.StartsAt,
		EndsAt:                    first.EndsAt,
		Timezone:                  series.Timezone,
		Status:                    status,
		Version:                   series.Version,
		OrganizerUserID:           organizerID,
		ShowAs:                    showAs,
		Visibility:                visibility,
		GuestsCanInvite:           guestsCanInvite,
		GuestsCanModify:           guestsCanModify,
		GuestsCanSeeGuestList:     guestsCanSeeGuestList,
		ResponseRequested:         responseRequested,
		AudienceRevision:          audienceRevision,
		ICalUID:                   series.ICalUID,
		Sequence:                  series.Sequence,
		InheritedAudienceRevision: audienceRevision,
		InheritedSequence:         series.Sequence,
	}
	if source.Kind != ParticipationSourceOccurrence {
		return locked, nil
	}
	if series.Status != SeriesStatusScheduled {
		return lockedTypedParticipationSource{}, ErrSessionParticipationNotFound
	}
	occurrences, err := expandCompleteSeries(ctx, definitionFromSeries(series))
	if err != nil {
		return lockedTypedParticipationSource{}, err
	}
	exceptions, err := listSeriesExceptions(ctx, transaction, tenantID, classID, series.ID)
	if err != nil {
		return lockedTypedParticipationSource{}, err
	}
	occurrences, err = applyPersistedExceptions(ctx, series, occurrences, exceptions)
	if err != nil {
		return lockedTypedParticipationSource{}, err
	}
	index := occurrenceIndex(occurrences, source.OccurrenceKey)
	if index < 0 {
		return lockedTypedParticipationSource{}, ErrSessionParticipationNotFound
	}
	occurrence := occurrences[index]
	locked.StartsAt = occurrence.StartsAt
	locked.EndsAt = occurrence.EndsAt
	for _, exception := range exceptions {
		if exception.OccurrenceKey != source.OccurrenceKey ||
			exception.Type != recurrence.ExceptionOverride {
			continue
		}
		if exception.OverrideTitle != nil {
			locked.Title = *exception.OverrideTitle
		}
		if exception.OverrideDescription != nil {
			locked.Description = *exception.OverrideDescription
		}
		if exception.OverrideTimezone != nil {
			locked.Timezone = *exception.OverrideTimezone
		}
		break
	}
	state, err := repository.readOccurrenceParticipationState(
		ctx,
		transaction,
		tenantID,
		classID,
		source,
		responseRequested,
	)
	if err != nil {
		return lockedTypedParticipationSource{}, err
	}
	locked.OccurrenceAudienceOverridden = state.RevisionID != uuid.Nil
	locked.AudienceRevision = state.AudienceRevision
	locked.ResponseRequested = state.ResponseRequested
	if locked.OccurrenceAudienceOverridden {
		locked.Sequence = state.Sequence
	}
	_ = forUpdate // lockClassSessionSeries always takes the authoritative row lock.
	return locked, nil
}

func (repository *PostgresRepository) readOccurrenceParticipationState(
	ctx context.Context,
	queryer participationQuerier,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
	inheritedResponseRequested bool,
) (occurrenceParticipationState, error) {
	var state occurrenceParticipationState
	var ciphertext []byte
	var keyVersion int16
	err := queryer.QueryRow(
		ctx,
		`SELECT id, audience_revision, ical_sequence,
       canonical_payload_ciphertext, crypto_key_version
FROM tutorhub.calendar_invitation_revisions
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NULL
  AND series_id = $3
  AND occurrence_key = $4
ORDER BY audience_revision DESC, created_at DESC, id DESC
LIMIT 1`,
		tenantID,
		classID,
		source.SeriesID,
		source.OccurrenceKey,
	).Scan(
		&state.RevisionID,
		&state.AudienceRevision,
		&state.Sequence,
		&ciphertext,
		&keyVersion,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		state.ResponseRequested = inheritedResponseRequested
		return state, nil
	}
	if err != nil {
		return occurrenceParticipationState{}, fmt.Errorf(
			"read occurrence participation state: %w",
			err,
		)
	}
	if repository.calendarProtector == nil {
		return occurrenceParticipationState{}, ErrSessionParticipationUnavailable
	}
	plaintext, err := repository.calendarProtector.Open(protecteddata.Context{
		TenantID: tenantID.String(),
		Purpose:  protecteddata.PurposeInvitationCanonicalPayload,
		RecordID: state.RevisionID.String(),
	}, protecteddata.SealedValue{
		KeyVersion: keyVersion,
		Ciphertext: ciphertext,
	})
	if err != nil {
		return occurrenceParticipationState{}, ErrSessionParticipationUnavailable
	}
	var snapshot canonicalSessionInvitationSnapshot
	if err := json.Unmarshal(plaintext, &snapshot); err != nil {
		return occurrenceParticipationState{}, ErrSessionParticipationUnavailable
	}
	state.ResponseRequested = snapshot.ResponseRequested
	return state, nil
}

func (repository *PostgresRepository) readTypedParticipationAudience(
	ctx context.Context,
	queryer participationQuerier,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source lockedTypedParticipationSource,
) (SessionAudience, error) {
	if source.Ref.Kind == ParticipationSourceOccurrence &&
		!source.OccurrenceAudienceOverridden {
		inherited := source
		inherited.Ref = SeriesParticipationSource(source.Ref.SeriesID)
		inherited.AudienceRevision = source.InheritedAudienceRevision
		inherited.Sequence = source.InheritedSequence
		inherited.OccurrenceAudienceOverridden = false
		audience, err := repository.readTypedParticipationAudience(
			ctx,
			queryer,
			tenantID,
			classID,
			inherited,
		)
		if err != nil {
			return SessionAudience{}, err
		}
		audience.Source = source.Ref
		// Zero is the occurrence-local CAS revision. The returned attendee
		// rows are inherited from the series until the first occurrence
		// audience mutation or RSVP materializes a copy-on-write snapshot.
		audience.AudienceRevision = 0
		audience.ResponseRequested = source.ResponseRequested
		audience.SourceStatus = source.Status
		return audience, nil
	}
	sessionID, seriesID, occurrenceKey := participationScopeValues(source.Ref)
	rows, err := queryer.Query(
		ctx,
		`SELECT id, internal_user_id, participation_role, business_role, rsvp_state,
       responded_at, version, response_requested, response_closed_at,
       can_see_guest_list
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND occurrence_key IS NOT DISTINCT FROM $5::text
  AND status = 'active'
  AND internal_user_id IS NOT NULL
ORDER BY internal_user_id ASC`,
		tenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
	)
	if err != nil {
		return SessionAudience{}, fmt.Errorf("read participation audience attendees: %w", err)
	}
	defer rows.Close()
	audience := SessionAudience{
		Source:            source.Ref,
		AudienceRevision:  source.AudienceRevision,
		ResponseRequested: source.ResponseRequested,
		SourceStatus:      source.Status,
		Attendees:         make([]SessionAudienceAttendee, 0),
	}
	for rows.Next() {
		var attendee SessionAudienceAttendee
		if scanErr := rows.Scan(
			&attendee.ID,
			&attendee.UserID,
			&attendee.ParticipationRole,
			&attendee.BusinessRole,
			&attendee.RSVPState,
			&attendee.RespondedAt,
			&attendee.Version,
			&attendee.ResponseRequested,
			&attendee.ResponseClosedAt,
			&attendee.CanSeeGuestList,
		); scanErr != nil {
			return SessionAudience{}, fmt.Errorf(
				"scan participation audience attendee: %w",
				scanErr,
			)
		}
		audience.Attendees = append(audience.Attendees, attendee)
	}
	if err := rows.Err(); err != nil {
		return SessionAudience{}, fmt.Errorf(
			"iterate participation audience attendees: %w",
			err,
		)
	}
	return audience, nil
}

func lockTypedParticipationAttendees(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
) ([]persistedSessionAttendee, error) {
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	rows, err := transaction.Query(
		ctx,
		`SELECT id, internal_user_id, participation_role, business_role, audience_source,
       response_requested, rsvp_state, rsvp_source, responded_at, response_note, version
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND occurrence_key IS NOT DISTINCT FROM $5::text
  AND status = 'active'
ORDER BY internal_user_id ASC NULLS LAST, id ASC
FOR UPDATE`,
		tenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
	)
	if err != nil {
		return nil, fmt.Errorf("lock active participation attendees: %w", err)
	}
	defer rows.Close()
	attendees := make([]persistedSessionAttendee, 0)
	for rows.Next() {
		var attendee persistedSessionAttendee
		var userID uuid.NullUUID
		if scanErr := rows.Scan(
			&attendee.ID,
			&userID,
			&attendee.ParticipationRole,
			&attendee.BusinessRole,
			&attendee.AudienceSource,
			&attendee.ResponseRequested,
			&attendee.RSVPState,
			&attendee.RSVPSource,
			&attendee.RespondedAt,
			&attendee.ResponseNote,
			&attendee.Version,
		); scanErr != nil {
			return nil, fmt.Errorf("scan active participation attendee: %w", scanErr)
		}
		if !userID.Valid {
			// External participation activation belongs to the capability slice;
			// never drop or overwrite it from an internal-only replacement.
			return nil, ErrSessionParticipationUnavailable
		}
		attendee.UserID = userID.UUID
		attendees = append(attendees, attendee)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active participation attendees: %w", err)
	}
	return attendees, nil
}

// mergeInheritedParticipationAttendees returns the effective occurrence
// audience before a full copy-on-write snapshot exists. Sparse exact rows are
// allowed only as a defensive recovery path and may override an inherited user;
// they can never add a user who is no longer in the series audience.
func mergeInheritedParticipationAttendees(
	inherited []persistedSessionAttendee,
	exact []persistedSessionAttendee,
) []persistedSessionAttendee {
	if len(exact) == 0 {
		return inherited
	}
	exactByUserID := make(map[uuid.UUID]persistedSessionAttendee, len(exact))
	for _, attendee := range exact {
		exactByUserID[attendee.UserID] = attendee
	}
	merged := make([]persistedSessionAttendee, 0, len(inherited))
	for _, attendee := range inherited {
		if override, ok := exactByUserID[attendee.UserID]; ok {
			merged = append(merged, override)
			continue
		}
		merged = append(merged, attendee)
	}
	return merged
}

func applyTypedAudienceReplacement(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source lockedTypedParticipationSource,
	params AudienceReplacementParams,
	current []persistedSessionAttendee,
	desired []resolvedAudienceMember,
	changedAt time.Time,
) error {
	currentByUserID := make(map[uuid.UUID]persistedSessionAttendee, len(current))
	for _, attendee := range current {
		currentByUserID[attendee.UserID] = attendee
	}
	desiredByUserID := make(map[uuid.UUID]resolvedAudienceMember, len(desired))
	for _, member := range desired {
		desiredByUserID[member.UserID] = member
	}
	for _, attendee := range current {
		member, remainsActive := desiredByUserID[attendee.UserID]
		if !remainsActive {
			if _, err := transaction.Exec(
				ctx,
				`UPDATE tutorhub.class_session_attendees
SET status = 'removed',
    response_closed_at = $5,
    removed_by = $4,
    removed_at = $5,
    removal_reason = 'audience_replaced',
    updated_by = $4,
    updated_at = $5,
    version = version + 1
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND status = 'active'`,
				tenantContext.TenantID,
				classID,
				attendee.ID,
				tenantContext.ActorID,
				changedAt,
			); err != nil {
				return mapParticipationPostgresError("remove participation attendee", err)
			}
			continue
		}
		resetRSVP := attendee.ParticipationRole != member.ParticipationRole ||
			attendee.BusinessRole != member.BusinessRole ||
			attendee.AudienceSource != member.AudienceSource ||
			attendee.ResponseRequested != params.ResponseRequested
		if !resetRSVP {
			continue
		}
		if _, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.class_session_attendees
SET participation_role = $4,
    business_role = $5,
    audience_source = $6,
    response_requested = $7,
    rsvp_state = 'needs_action',
    rsvp_source = 'none',
    responded_at = NULL,
    response_note = NULL,
    response_invitation_revision_id = NULL,
    response_sequence = NULL,
    response_closed_at = CASE WHEN $7 THEN NULL ELSE $9 END,
    updated_by = $8,
    updated_at = $9,
    version = version + 1
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND status = 'active'`,
			tenantContext.TenantID,
			classID,
			attendee.ID,
			string(member.ParticipationRole),
			member.BusinessRole,
			member.AudienceSource,
			params.ResponseRequested,
			tenantContext.ActorID,
			changedAt,
		); err != nil {
			return mapParticipationPostgresError("update participation attendee", err)
		}
	}
	sessionID, seriesID, occurrenceKey := participationScopeValues(source.Ref)
	for _, member := range desired {
		if _, alreadyActive := currentByUserID[member.UserID]; alreadyActive {
			continue
		}
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.class_session_attendees (
    tenant_id, class_id, session_id, series_id, occurrence_key,
    internal_user_id, participation_role, business_role, audience_source,
    show_as, visibility, can_invite_others, can_modify_event, can_see_guest_list,
    response_requested, status, rsvp_state, rsvp_source,
    response_closed_at, created_by, updated_by, created_at, updated_at
)
VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12, $13, $14,
    $15, 'active', 'needs_action', 'none',
    CASE WHEN $15 THEN NULL ELSE $17 END,
    $16, $16, $17, $17
)`,
			tenantContext.TenantID,
			classID,
			sessionID,
			seriesID,
			occurrenceKey,
			member.UserID,
			string(member.ParticipationRole),
			member.BusinessRole,
			member.AudienceSource,
			source.ShowAs,
			source.Visibility,
			source.GuestsCanInvite,
			source.GuestsCanModify,
			source.GuestsCanSeeGuestList,
			params.ResponseRequested,
			tenantContext.ActorID,
			changedAt,
		); err != nil {
			return mapParticipationPostgresError("insert participation attendee", err)
		}
	}
	return nil
}

func updateTypedParticipationSource(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source lockedTypedParticipationSource,
	responseRequested bool,
	audienceRevision int64,
	sequence int64,
	updatedAt time.Time,
) (int64, error) {
	var version int64
	if source.Ref.Kind == ParticipationSourceSeries {
		err := transaction.QueryRow(
			ctx,
			`UPDATE tutorhub.class_session_series
SET response_requested = $4,
    audience_revision = $5,
    sequence = $6,
    version = version + 1,
    updated_by = $7,
    updated_at = $8
WHERE tenant_id = $1
  AND class_id = $2
  AND id = $3
  AND version = $9
RETURNING version`,
			tenantContext.TenantID,
			classID,
			source.Ref.SeriesID,
			responseRequested,
			audienceRevision,
			sequence,
			tenantContext.ActorID,
			updatedAt,
			source.Version,
		).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrSessionAudienceVersionConflict
		}
		if err != nil {
			return 0, mapParticipationPostgresError(
				"update series participation source",
				err,
			)
		}
		return version, nil
	}
	err := transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.class_session_series
SET version = version + 1,
    updated_by = $4,
    updated_at = $5
WHERE tenant_id = $1
  AND class_id = $2
  AND id = $3
  AND version = $6
RETURNING version`,
		tenantContext.TenantID,
		classID,
		source.Ref.SeriesID,
		tenantContext.ActorID,
		updatedAt,
		source.Version,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrSessionAudienceVersionConflict
	}
	if err != nil {
		return 0, mapParticipationPostgresError(
			"update occurrence participation source",
			err,
		)
	}
	return version, nil
}

func readTypedInvitationSnapshotRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
) ([]invitationSnapshotRecipient, error) {
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	rows, err := transaction.Query(
		ctx,
		`SELECT attendee.id,
       attendee.internal_user_id,
       attendee.participation_role,
       attendee.business_role,
       attendee.audience_source,
       attendee.response_requested,
       attendee.can_see_guest_list,
       attendee.rsvp_state,
       attendee.rsvp_source,
       app_user.email,
       app_user.display_name,
       COALESCE(preference.locale, app_user.locale),
       COALESCE(preference.viewer_timezone, app_user.timezone)
FROM tutorhub.class_session_attendees AS attendee
JOIN tutorhub.users AS app_user
  ON app_user.id = attendee.internal_user_id
 AND app_user.status = 'active'
LEFT JOIN tutorhub.calendar_display_preferences AS preference
  ON preference.tenant_id = attendee.tenant_id
 AND preference.user_id = attendee.internal_user_id
WHERE attendee.tenant_id = $1
  AND attendee.class_id = $2
  AND attendee.session_id IS NOT DISTINCT FROM $3::uuid
  AND attendee.series_id IS NOT DISTINCT FROM $4::uuid
  AND attendee.occurrence_key IS NOT DISTINCT FROM $5::text
  AND attendee.status = 'active'
  AND attendee.internal_user_id IS NOT NULL
ORDER BY attendee.internal_user_id ASC`,
		tenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
	)
	if err != nil {
		return nil, fmt.Errorf("read typed invitation snapshot recipients: %w", err)
	}
	defer rows.Close()
	recipients := make([]invitationSnapshotRecipient, 0)
	for rows.Next() {
		var recipient invitationSnapshotRecipient
		if scanErr := rows.Scan(
			&recipient.AttendeeID,
			&recipient.UserID,
			&recipient.ParticipationRole,
			&recipient.BusinessRole,
			&recipient.AudienceSource,
			&recipient.ResponseRequested,
			&recipient.CanSeeGuestList,
			&recipient.RSVPState,
			&recipient.RSVPSource,
			&recipient.Email,
			&recipient.DisplayName,
			&recipient.Locale,
			&recipient.ViewerTimezone,
		); scanErr != nil {
			return nil, fmt.Errorf(
				"scan typed invitation snapshot recipient: %w",
				scanErr,
			)
		}
		recipient.Email = strings.ToLower(strings.TrimSpace(recipient.Email))
		recipient.DisplayName = strings.TrimSpace(recipient.DisplayName)
		recipient.Locale = normalizeInvitationLocale(recipient.Locale)
		recipient.ViewerTimezone = strings.TrimSpace(recipient.ViewerTimezone)
		if !validInvitationRecipient(recipient) {
			return nil, ErrSessionParticipationUnavailable
		}
		recipients = append(recipients, recipient)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"iterate typed invitation snapshot recipients: %w",
			err,
		)
	}
	if len(recipients) > maximumAudienceAttendees {
		return nil, fmt.Errorf(
			"%w: audience cannot exceed %d distinct attendees",
			ErrInvalidParticipationInput,
			maximumAudienceAttendees,
		)
	}
	return recipients, nil
}

func (repository *PostgresRepository) insertTypedInvitationSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	source lockedTypedParticipationSource,
	sourceVersion int64,
	audienceRevision int64,
	sequence int64,
	responseRequested bool,
	revisionID uuid.UUID,
	recipients []invitationSnapshotRecipient,
	createdAt time.Time,
) error {
	if repository.calendarProtector == nil || revisionID == uuid.Nil {
		return ErrSessionParticipationUnavailable
	}
	lifecycle := "updated"
	if source.AudienceRevision == 0 {
		lifecycle = "published"
	}
	snapshotAttendees := make([]invitationSnapshotAttendee, 0, len(recipients))
	for _, recipient := range recipients {
		snapshotAttendees = append(snapshotAttendees, invitationSnapshotAttendee{
			AttendeeID:        recipient.AttendeeID,
			RecipientID:       recipient.ID,
			UserID:            recipient.UserID,
			ParticipationRole: recipient.ParticipationRole,
			BusinessRole:      recipient.BusinessRole,
			AudienceSource:    recipient.AudienceSource,
			RSVPState:         recipient.RSVPState,
			RSVPSource:        recipient.RSVPSource,
		})
	}
	sort.Slice(snapshotAttendees, func(left int, right int) bool {
		return snapshotAttendees[left].UserID.String() <
			snapshotAttendees[right].UserID.String()
	})
	payload, err := json.Marshal(canonicalSessionInvitationSnapshot{
		SchemaVersion:     "tutorhub.calendar.invitation.v1",
		SourceType:        participationSnapshotSourceType(source.Ref),
		SourceID:          source.ID,
		OccurrenceKey:     source.Ref.OccurrenceKey,
		ClassID:           source.ClassID,
		SourceVersion:     sourceVersion,
		AudienceRevision:  audienceRevision,
		Lifecycle:         lifecycle,
		ICalUID:           source.ICalUID,
		ICalSequence:      sequence,
		OrganizerUserID:   source.OrganizerUserID,
		Title:             source.Title,
		Description:       source.Description,
		StartsAt:          source.StartsAt.UTC().Format(time.RFC3339Nano),
		EndsAt:            source.EndsAt.UTC().Format(time.RFC3339Nano),
		Timezone:          source.Timezone,
		ShowAs:            source.ShowAs,
		Visibility:        source.Visibility,
		ResponseRequested: responseRequested,
		GuestPermissions: invitationSnapshotGuestPermissions{
			CanInviteOthers: source.GuestsCanInvite,
			CanModifyEvent:  source.GuestsCanModify,
			CanSeeGuestList: source.GuestsCanSeeGuestList,
		},
		Attendees: snapshotAttendees,
	})
	if err != nil {
		return fmt.Errorf("encode typed invitation canonical snapshot: %w", err)
	}
	payloadDigest := sha256.Sum256(payload)
	sealedPayload, err := repository.calendarProtector.Seal(protecteddata.Context{
		TenantID: tenantContext.TenantID.String(),
		Purpose:  protecteddata.PurposeInvitationCanonicalPayload,
		RecordID: revisionID.String(),
	}, payload)
	if err != nil {
		return fmt.Errorf("protect typed invitation canonical snapshot: %w", err)
	}
	sessionID, seriesID, occurrenceKey := participationScopeValues(source.Ref)
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_invitation_revisions (
    id, tenant_id, class_id, session_id, series_id, occurrence_key,
    source_version, audience_revision, ical_uid, ical_sequence,
    method, lifecycle, organizer_user_id, actor_type, created_by,
    reason_code, timezone_data_version,
    canonical_payload_ciphertext, canonical_payload_sha256,
    crypto_key_version, created_at
)
VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10,
    NULL, $11, $12, 'user', $13,
    'audience_replaced', $14,
    $15, $16,
    $17, $18
)`,
		revisionID,
		tenantContext.TenantID,
		source.ClassID,
		sessionID,
		seriesID,
		occurrenceKey,
		sourceVersion,
		audienceRevision,
		source.ICalUID,
		sequence,
		lifecycle,
		source.OrganizerUserID,
		tenantContext.ActorID,
		calendarTimezoneDataVersion,
		sealedPayload.Ciphertext,
		payloadDigest[:],
		sealedPayload.KeyVersion,
		createdAt,
	); err != nil {
		return mapParticipationPostgresError(
			"insert typed invitation canonical snapshot",
			err,
		)
	}
	for _, recipient := range recipients {
		sealedAddress, err := repository.calendarProtector.Seal(protecteddata.Context{
			TenantID: tenantContext.TenantID.String(),
			Purpose:  protecteddata.PurposeInvitationRecipientAddress,
			RecordID: recipient.ID.String(),
		}, []byte(recipient.Email))
		if err != nil {
			return fmt.Errorf("protect typed invitation recipient address: %w", err)
		}
		sealedName, err := repository.calendarProtector.Seal(protecteddata.Context{
			TenantID: tenantContext.TenantID.String(),
			Purpose:  protecteddata.PurposeInvitationRecipientDisplayName,
			RecordID: recipient.ID.String(),
		}, []byte(recipient.DisplayName))
		if err != nil {
			return fmt.Errorf("protect typed invitation recipient display name: %w", err)
		}
		addressFingerprint, err := repository.calendarProtector.DeliveryAddressFingerprint(
			tenantContext.TenantID.String(),
			[]byte(recipient.Email),
		)
		if err != nil {
			return fmt.Errorf("fingerprint typed invitation recipient address: %w", err)
		}
		if sealedAddress.KeyVersion != sealedPayload.KeyVersion ||
			sealedName.KeyVersion != sealedPayload.KeyVersion {
			return ErrSessionParticipationUnavailable
		}
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.calendar_invitation_recipients (
    id, tenant_id, class_id, invitation_revision_id, attendee_id,
    recipient_kind, participation_role, business_role, audience_source,
    response_requested, can_see_guest_list, locale, viewer_timezone,
    rsvp_state, rsvp_source,
    delivery_address_fingerprint, delivery_address_ciphertext,
    display_name_ciphertext, crypto_key_version, created_at
)
VALUES (
    $1, $2, $3, $4, $5,
    'internal', $6, $7, $8,
    $9, $10, $11, $12,
    $13, $14,
    $15, $16,
    $17, $18, $19
)`,
			recipient.ID,
			tenantContext.TenantID,
			source.ClassID,
			revisionID,
			recipient.AttendeeID,
			string(recipient.ParticipationRole),
			recipient.BusinessRole,
			recipient.AudienceSource,
			recipient.ResponseRequested,
			recipient.CanSeeGuestList,
			recipient.Locale,
			recipient.ViewerTimezone,
			string(recipient.RSVPState),
			recipient.RSVPSource,
			addressFingerprint[:],
			sealedAddress.Ciphertext,
			sealedName.Ciphertext,
			sealedAddress.KeyVersion,
			createdAt,
		); err != nil {
			return mapParticipationPostgresError(
				"insert typed invitation recipient snapshot",
				err,
			)
		}
	}
	return nil
}

// materializeInheritedOccurrenceAudience freezes the effective series
// audience for one occurrence before recording its first occurrence-specific
// RSVP. This copy-on-write boundary prevents the response from mutating the
// series while keeping the original invitation/RSVP state for every attendee.
// The neutral snapshot is not itself an email delivery effect; P3-05A still
// requires an explicit outbox effect before any provider call.
func (repository *PostgresRepository) materializeInheritedOccurrenceAudience(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source lockedTypedParticipationSource,
	materializedAt time.Time,
) (lockedTypedParticipationSource, persistedSessionAttendee, error) {
	if source.Ref.Kind != ParticipationSourceOccurrence ||
		source.OccurrenceAudienceOverridden ||
		source.InheritedAudienceRevision < 1 {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{},
			ErrSessionRSVPUnavailable
	}
	inheritedSource := SeriesParticipationSource(source.Ref.SeriesID)
	inherited, err := lockTypedParticipationAttendees(
		ctx,
		transaction,
		tenantContext.TenantID,
		classID,
		inheritedSource,
	)
	if err != nil {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{}, err
	}
	actorInvited := false
	for _, attendee := range inherited {
		if attendee.UserID == tenantContext.ActorID &&
			attendee.ResponseRequested {
			actorInvited = true
			break
		}
	}
	if !actorInvited {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{},
			ErrSessionRSVPUnavailable
	}
	exact, err := lockTypedParticipationAttendees(
		ctx,
		transaction,
		tenantContext.TenantID,
		classID,
		source.Ref,
	)
	if err != nil {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{}, err
	}
	if len(exact) != 0 {
		// Exact rows without an immutable occurrence snapshot would make the
		// inheritance boundary ambiguous; fail closed instead of guessing.
		return lockedTypedParticipationSource{}, persistedSessionAttendee{},
			ErrSessionParticipationUnavailable
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.class_session_attendees (
    tenant_id, class_id, session_id, series_id, occurrence_key,
    internal_user_id, external_recipient_id,
    participation_role, business_role, audience_source,
    show_as, visibility,
    can_invite_others, can_modify_event, can_see_guest_list,
    response_requested, status, rsvp_state, rsvp_source,
    responded_at, response_note, response_invitation_revision_id,
    response_sequence, response_closed_at, version,
    created_by, updated_by, created_at, updated_at
)
SELECT attendee.tenant_id, attendee.class_id, NULL, attendee.series_id, $4,
       attendee.internal_user_id, attendee.external_recipient_id,
       attendee.participation_role, attendee.business_role, attendee.audience_source,
       attendee.show_as, attendee.visibility,
       attendee.can_invite_others, attendee.can_modify_event,
       attendee.can_see_guest_list,
       attendee.response_requested, attendee.status,
       attendee.rsvp_state, attendee.rsvp_source,
       attendee.responded_at, attendee.response_note,
       attendee.response_invitation_revision_id,
       attendee.response_sequence, attendee.response_closed_at,
       attendee.version,
       attendee.created_by, attendee.updated_by,
       attendee.created_at, GREATEST(attendee.updated_at, $5)
FROM tutorhub.class_session_attendees AS attendee
WHERE attendee.tenant_id = $1
  AND attendee.class_id = $2
  AND attendee.session_id IS NULL
  AND attendee.series_id = $3
  AND attendee.occurrence_key IS NULL
  AND attendee.status = 'active'`,
		tenantContext.TenantID,
		classID,
		source.Ref.SeriesID,
		source.Ref.OccurrenceKey,
		materializedAt,
	); err != nil {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{},
			mapParticipationPostgresError(
				"materialize inherited occurrence audience",
				err,
			)
	}
	recipients, err := readTypedInvitationSnapshotRecipients(
		ctx,
		transaction,
		tenantContext.TenantID,
		classID,
		source.Ref,
	)
	if err != nil {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{}, err
	}
	if len(recipients) != len(inherited) {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{},
			ErrSessionParticipationUnavailable
	}
	revisionID := uuid.New()
	for index := range recipients {
		recipients[index].ID = uuid.New()
	}
	if err := repository.insertTypedInvitationSnapshot(
		ctx,
		transaction,
		tenantContext,
		source,
		source.Version,
		1,
		source.Sequence,
		source.ResponseRequested,
		revisionID,
		recipients,
		materializedAt,
	); err != nil {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{}, err
	}
	source.OccurrenceAudienceOverridden = true
	source.AudienceRevision = 1
	attendee, err := readTypedSelfAttendee(
		ctx,
		transaction,
		tenantContext.TenantID,
		classID,
		source.Ref,
		tenantContext.ActorID,
		true,
	)
	if err != nil {
		return lockedTypedParticipationSource{}, persistedSessionAttendee{}, err
	}
	return source, attendee, nil
}

func readTypedSelfAttendee(
	ctx context.Context,
	queryer participationQuerier,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
	userID uuid.UUID,
	forUpdate bool,
) (persistedSessionAttendee, error) {
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	query := `SELECT id, internal_user_id, participation_role, business_role, audience_source,
       response_requested, rsvp_state, rsvp_source, responded_at, response_note, version
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND occurrence_key IS NOT DISTINCT FROM $5::text
  AND internal_user_id = $6
  AND status = 'active'
  AND response_closed_at IS NULL`
	if forUpdate {
		query += "\nFOR UPDATE"
	}
	attendee, err := scanPersistedSessionAttendee(queryer.QueryRow(
		ctx,
		query,
		tenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
		userID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedSessionAttendee{}, ErrSessionRSVPUnavailable
	}
	if err != nil {
		return persistedSessionAttendee{}, fmt.Errorf(
			"read current typed RSVP attendee: %w",
			err,
		)
	}
	return attendee, nil
}

func findTypedInvitationRevision(
	ctx context.Context,
	queryer participationQuerier,
	tenantID uuid.UUID,
	classID uuid.UUID,
	source ParticipationSourceRef,
	audienceRevision int64,
) (uuid.UUID, int64, error) {
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	var revisionID uuid.UUID
	var sequence int64
	err := queryer.QueryRow(
		ctx,
		`SELECT id, ical_sequence
FROM tutorhub.calendar_invitation_revisions
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND occurrence_key IS NOT DISTINCT FROM $5::text
  AND audience_revision = $6
ORDER BY created_at DESC, id DESC
LIMIT 1`,
		tenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
		audienceRevision,
	).Scan(&revisionID, &sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, ErrSessionRSVPUnavailable
	}
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("read current typed invitation revision: %w", err)
	}
	return revisionID, sequence, nil
}

func updateTypedSelfRSVP(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
	attendee persistedSessionAttendee,
	params SelfRSVPParams,
	invitationRevisionID uuid.UUID,
	invitationSequence int64,
	respondedAt time.Time,
) (persistedSessionAttendee, error) {
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	updated, err := scanPersistedSessionAttendee(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.class_session_attendees
SET rsvp_state = $7,
    rsvp_source = 'tutorhub_authenticated',
    responded_at = $8,
    response_note = NULLIF($9, ''),
    response_invitation_revision_id = $10,
    response_sequence = $11,
    updated_by = $6,
    updated_at = $8,
    version = version + 1
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id IS NOT DISTINCT FROM $3::uuid
  AND series_id IS NOT DISTINCT FROM $4::uuid
  AND occurrence_key IS NOT DISTINCT FROM $5::text
  AND internal_user_id = $6
  AND id = $12
  AND status = 'active'
  AND response_requested = true
  AND response_closed_at IS NULL
  AND version = $13
RETURNING id, internal_user_id, participation_role, business_role, audience_source,
          response_requested, rsvp_state, rsvp_source, responded_at, response_note, version`,
		tenantContext.TenantID,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
		tenantContext.ActorID,
		string(params.State),
		respondedAt,
		params.Note,
		invitationRevisionID,
		invitationSequence,
		attendee.ID,
		params.ExpectedAttendeeVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedSessionAttendee{}, ErrSessionAttendeeVersionConflict
	}
	if err != nil {
		return persistedSessionAttendee{}, mapParticipationPostgresError(
			"update current typed RSVP",
			err,
		)
	}
	return updated, nil
}

func findTypedParticipationReplay(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	idempotencyKey string,
	fingerprint []byte,
	operation string,
	classID uuid.UUID,
	source ParticipationSourceRef,
) (bool, error) {
	var receipt sourceScopedParticipationReceipt
	var occurrenceKey *string
	err := transaction.QueryRow(
		ctx,
		`SELECT request_fingerprint, operation, class_id, session_id, series_id,
       occurrence_key, actor_type, actor_user_id
FROM tutorhub.calendar_participation_mutation_receipts
WHERE tenant_id = $1 AND idempotency_key = $2`,
		tenantContext.TenantID,
		idempotencyKey,
	).Scan(
		&receipt.Fingerprint,
		&receipt.Operation,
		&receipt.ClassID,
		&receipt.SessionID,
		&receipt.SeriesID,
		&occurrenceKey,
		&receipt.ActorType,
		&receipt.ActorUserID,
	)
	receipt.OccurrenceKey = occurrenceKey
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read typed participation idempotency receipt: %w", err)
	}
	if !receipt.ActorUserID.Valid ||
		!bytes.Equal(receipt.Fingerprint, fingerprint) ||
		receipt.Operation != operation ||
		receipt.ClassID != classID ||
		receipt.ActorType != "tutorhub_authenticated" ||
		receipt.ActorUserID.UUID != tenantContext.ActorID ||
		!participationReceiptMatchesSource(receipt, source) {
		return false, ErrSessionParticipationIdempotencyConflict
	}
	return true, nil
}

func insertTypedParticipationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	idempotencyKey string,
	fingerprint []byte,
	operation string,
	classID uuid.UUID,
	source ParticipationSourceRef,
	resultAttendeeID uuid.UUID,
	resultInvitationRevisionID uuid.UUID,
	resultVersion int64,
) error {
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_participation_mutation_receipts (
    tenant_id, idempotency_key, request_fingerprint, operation,
    class_id, session_id, series_id, occurrence_key,
    actor_type, actor_user_id,
    result_attendee_id, result_invitation_revision_id, result_version
)
VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8,
    'tutorhub_authenticated', $9,
    $10, $11, $12
)`,
		tenantContext.TenantID,
		idempotencyKey,
		fingerprint,
		operation,
		classID,
		sessionID,
		seriesID,
		occurrenceKey,
		tenantContext.ActorID,
		nullableParticipationUUID(resultAttendeeID),
		nullableParticipationUUID(resultInvitationRevisionID),
		resultVersion,
	)
	if err != nil {
		return mapParticipationPostgresError(
			"insert typed participation receipt",
			err,
		)
	}
	return nil
}

func insertTypedParticipationEvent(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	source ParticipationSourceRef,
	eventType string,
	audienceRevision int64,
	sequence int64,
	attendeeCount int,
	rsvpState string,
	occurredAt time.Time,
) error {
	aggregateID := source.SessionID
	aggregateType := "class_session"
	if source.Kind != ParticipationSourceSession {
		aggregateID = source.SeriesID
		aggregateType = "class_session_series"
	}
	sessionID, seriesID, occurrenceKey := participationScopeValues(source)
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, $2, $3, $4,
    jsonb_build_object(
        'source_kind', $5::text,
        'session_id', $6::uuid,
        'series_id', $7::uuid,
        'occurrence_key', $8::text,
        'class_id', $9::uuid,
        'actor_user_id', $10::uuid,
        'audience_revision', $11::bigint,
        'sequence', $12::bigint,
        'attendee_count', $13::integer,
        'rsvp_state', NULLIF($14::text, '')
    ),
    $15, $15
)`,
		tenantContext.TenantID,
		aggregateType,
		aggregateID,
		eventType,
		string(source.Kind),
		sessionID,
		seriesID,
		occurrenceKey,
		classID,
		tenantContext.ActorID,
		audienceRevision,
		sequence,
		attendeeCount,
		rsvpState,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert typed %s outbox event: %w", eventType, err)
	}
	metadata := audit.Metadata{
		"class_id":          classID.String(),
		"source_kind":       string(source.Kind),
		"audience_revision": fmt.Sprintf("%d", audienceRevision),
		"sequence":          fmt.Sprintf("%d", sequence),
		"attendee_count":    fmt.Sprintf("%d", attendeeCount),
	}
	if source.OccurrenceKey != "" {
		metadata["occurrence_key"] = source.OccurrenceKey
	}
	if rsvpState != "" {
		metadata["rsvp_state"] = rsvpState
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID:      tenantContext.TenantID,
		ActorID:       tenantContext.ActorID,
		EventType:     eventType,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Metadata:      metadata,
		OccurredAt:    occurredAt,
	}); err != nil {
		return fmt.Errorf("insert typed %s audit event: %w", eventType, err)
	}
	return nil
}

func participationScopeValues(source ParticipationSourceRef) (any, any, any) {
	switch source.Kind {
	case ParticipationSourceSession:
		return source.SessionID, nil, nil
	case ParticipationSourceSeries:
		return nil, source.SeriesID, nil
	case ParticipationSourceOccurrence:
		return nil, source.SeriesID, source.OccurrenceKey
	default:
		return nil, nil, nil
	}
}

func participationReceiptMatchesSource(
	receipt sourceScopedParticipationReceipt,
	source ParticipationSourceRef,
) bool {
	switch source.Kind {
	case ParticipationSourceSession:
		return receipt.SessionID.Valid &&
			receipt.SessionID.UUID == source.SessionID &&
			!receipt.SeriesID.Valid &&
			receipt.OccurrenceKey == nil
	case ParticipationSourceSeries:
		return !receipt.SessionID.Valid &&
			receipt.SeriesID.Valid &&
			receipt.SeriesID.UUID == source.SeriesID &&
			receipt.OccurrenceKey == nil
	case ParticipationSourceOccurrence:
		return !receipt.SessionID.Valid &&
			receipt.SeriesID.Valid &&
			receipt.SeriesID.UUID == source.SeriesID &&
			receipt.OccurrenceKey != nil &&
			*receipt.OccurrenceKey == source.OccurrenceKey
	default:
		return false
	}
}

func participationSnapshotSourceType(source ParticipationSourceRef) string {
	switch source.Kind {
	case ParticipationSourceSession:
		return "class_session"
	case ParticipationSourceSeries:
		return "class_session_series"
	case ParticipationSourceOccurrence:
		return "class_session_occurrence"
	default:
		return "unknown"
	}
}
