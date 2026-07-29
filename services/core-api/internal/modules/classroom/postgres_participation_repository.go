package classroom

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/platform/protecteddata"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	participationOperationAudienceReplace = "audience_replace"
	participationOperationRSVPRespond     = "rsvp_respond"
	calendarTimezoneDataVersion           = "go-runtime"
)

type lockedParticipationSession struct {
	ID                    uuid.UUID
	TenantID              uuid.UUID
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
}

type persistedSessionAttendee struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	ParticipationRole ParticipationRole
	BusinessRole      string
	AudienceSource    string
	ResponseRequested bool
	RSVPState         RSVPState
	RSVPSource        string
	RespondedAt       *time.Time
	ResponseNote      *string
	Version           int64
}

type resolvedAudienceMember struct {
	UserID            uuid.UUID
	ParticipationRole ParticipationRole
	BusinessRole      string
	AudienceSource    string
}

type invitationSnapshotRecipient struct {
	ID                uuid.UUID
	AttendeeID        uuid.UUID
	UserID            uuid.UUID
	ParticipationRole ParticipationRole
	BusinessRole      string
	AudienceSource    string
	ResponseRequested bool
	CanSeeGuestList   bool
	RSVPState         RSVPState
	RSVPSource        string
	Email             string
	DisplayName       string
	Locale            string
	ViewerTimezone    string
}

type canonicalSessionInvitationSnapshot struct {
	SchemaVersion     string                             `json:"schema_version"`
	SourceType        string                             `json:"source_type"`
	SourceID          uuid.UUID                          `json:"source_id"`
	OccurrenceKey     string                             `json:"occurrence_key,omitempty"`
	ClassID           uuid.UUID                          `json:"class_id"`
	SourceVersion     int64                              `json:"source_version"`
	AudienceRevision  int64                              `json:"audience_revision"`
	Lifecycle         string                             `json:"lifecycle"`
	ICalUID           string                             `json:"ical_uid"`
	ICalSequence      int64                              `json:"ical_sequence"`
	OrganizerUserID   uuid.UUID                          `json:"organizer_user_id"`
	Title             string                             `json:"title"`
	Description       string                             `json:"description"`
	StartsAt          string                             `json:"starts_at"`
	EndsAt            string                             `json:"ends_at"`
	Timezone          string                             `json:"timezone"`
	ShowAs            string                             `json:"show_as"`
	Visibility        string                             `json:"visibility"`
	ResponseRequested bool                               `json:"response_requested"`
	GuestPermissions  invitationSnapshotGuestPermissions `json:"guest_permissions"`
	Attendees         []invitationSnapshotAttendee       `json:"attendees"`
}

type invitationSnapshotGuestPermissions struct {
	CanInviteOthers bool `json:"can_invite_others"`
	CanModifyEvent  bool `json:"can_modify_event"`
	CanSeeGuestList bool `json:"can_see_guest_list"`
}

type invitationSnapshotAttendee struct {
	AttendeeID        uuid.UUID         `json:"attendee_id"`
	RecipientID       uuid.UUID         `json:"recipient_id"`
	UserID            uuid.UUID         `json:"user_id"`
	ParticipationRole ParticipationRole `json:"participation_role"`
	BusinessRole      string            `json:"business_role"`
	AudienceSource    string            `json:"audience_source"`
	RSVPState         RSVPState         `json:"rsvp_state"`
	RSVPSource        string            `json:"rsvp_source"`
}

type participationReceipt struct {
	Fingerprint []byte
	Operation   string
	ClassID     uuid.UUID
	SessionID   uuid.UUID
	ActorType   string
	ActorUserID uuid.UUID
}

func (repository *PostgresRepository) GetSessionAudience(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (SessionAudience, error) {
	if err := tenantContext.Validate(); err != nil || classID == uuid.Nil || sessionID == uuid.Nil {
		return SessionAudience{}, ErrSessionParticipationNotFound
	}
	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SessionAudience{}, fmt.Errorf("begin session audience read: %w", err)
	}
	defer rollbackClassTransaction(transaction)
	if err := repository.requireSessionSchedulingFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return SessionAudience{}, err
	}
	audience, err := readSessionAudience(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID,
	)
	if err != nil {
		return SessionAudience{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return SessionAudience{}, fmt.Errorf("commit session audience read: %w", err)
	}
	return audience, nil
}

func (repository *PostgresRepository) ReplaceSessionAudience(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	params AudienceReplacementParams,
	replacedAt time.Time,
) (SessionAudienceMutationResult, error) {
	if err := tenantContext.Validate(); err != nil || classID == uuid.Nil || sessionID == uuid.Nil {
		return SessionAudienceMutationResult{}, ErrSessionParticipationNotFound
	}
	if repository.calendarProtector == nil {
		return SessionAudienceMutationResult{}, ErrSessionParticipationUnavailable
	}
	if _, err := (ReplaceAudienceInput{
		ExpectedAudienceRevision: params.ExpectedAudienceRevision,
		IdempotencyKey:           params.IdempotencyKey,
		ResponseRequested:        params.ResponseRequested,
		Attendees:                toAudienceInput(params.Attendees),
	}).normalized(); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	fingerprint, err := decodeParticipationFingerprint(params.Fingerprint)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SessionAudienceMutationResult{}, fmt.Errorf("begin session audience replacement: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if err := repository.requireSessionSchedulingFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	lockedClass, membership, err := repository.lockClassMutation(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext, membership, lockedClass.Class, policy.ActionSessionSchedule,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := lockParticipationIdempotency(
		queryContext, transaction, tenantContext.TenantID, params.IdempotencyKey,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if replay, err := findParticipationReplay(
		queryContext,
		transaction,
		tenantContext,
		params.IdempotencyKey,
		fingerprint,
		participationOperationAudienceReplace,
		classID,
		sessionID,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	} else if replay {
		audience, readErr := readSessionAudience(
			queryContext, transaction, tenantContext.TenantID, classID, sessionID,
		)
		if readErr != nil {
			return SessionAudienceMutationResult{}, readErr
		}
		if err := transaction.Commit(queryContext); err != nil {
			return SessionAudienceMutationResult{}, fmt.Errorf("commit replayed session audience replacement: %w", err)
		}
		return SessionAudienceMutationResult{Audience: audience, Replayed: true}, nil
	}

	source, err := lockParticipationSession(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if source.Status != SessionStatusScheduled {
		return SessionAudienceMutationResult{}, ErrInvalidSessionTransition
	}
	if source.AudienceRevision != params.ExpectedAudienceRevision {
		return SessionAudienceMutationResult{}, ErrSessionAudienceVersionConflict
	}
	resolved, err := resolveInternalAudience(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		source.OrganizerUserID,
		params.Attendees,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	current, err := lockActiveSessionAttendees(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if !audienceReplacementChanges(source.ResponseRequested, params.ResponseRequested, current, resolved) {
		audience, readErr := readSessionAudience(
			queryContext, transaction, tenantContext.TenantID, classID, sessionID,
		)
		if readErr != nil {
			return SessionAudienceMutationResult{}, readErr
		}
		if err := insertParticipationReceipt(
			queryContext, transaction, tenantContext, params.IdempotencyKey, fingerprint,
			participationOperationAudienceReplace, classID, sessionID,
			uuid.Nil, uuid.Nil, source.Version,
		); err != nil {
			return SessionAudienceMutationResult{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return SessionAudienceMutationResult{}, fmt.Errorf("commit no-op session audience replacement: %w", err)
		}
		return SessionAudienceMutationResult{Audience: audience}, nil
	}

	if err := applyAudienceReplacement(
		queryContext,
		transaction,
		tenantContext,
		classID,
		sessionID,
		source,
		params,
		current,
		resolved,
		replacedAt.UTC(),
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}

	newAudienceRevision := source.AudienceRevision + 1
	newSequence := source.Sequence
	if source.AudienceRevision > 0 {
		newSequence++
	}
	updatedSourceVersion, err := updateParticipationSessionSource(
		queryContext,
		transaction,
		tenantContext,
		classID,
		sessionID,
		source.Version,
		params.ResponseRequested,
		newAudienceRevision,
		newSequence,
		replacedAt.UTC(),
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}

	recipients, err := readInvitationSnapshotRecipients(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	revisionID := uuid.New()
	for index := range recipients {
		recipients[index].ID = uuid.New()
	}
	if err := repository.insertInvitationSnapshot(
		queryContext,
		transaction,
		tenantContext,
		source,
		updatedSourceVersion,
		newAudienceRevision,
		newSequence,
		params.ResponseRequested,
		revisionID,
		recipients,
		replacedAt.UTC(),
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := insertParticipationReceipt(
		queryContext, transaction, tenantContext, params.IdempotencyKey, fingerprint,
		participationOperationAudienceReplace, classID, sessionID,
		uuid.Nil, revisionID, updatedSourceVersion,
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := insertSessionParticipationEvent(
		queryContext,
		transaction,
		tenantContext,
		classID,
		sessionID,
		"class_session.audience_replaced.v1",
		newAudienceRevision,
		newSequence,
		len(recipients),
		"",
		replacedAt.UTC(),
	); err != nil {
		return SessionAudienceMutationResult{}, err
	}
	audience, err := readSessionAudience(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID,
	)
	if err != nil {
		return SessionAudienceMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return SessionAudienceMutationResult{}, fmt.Errorf("commit session audience replacement: %w", err)
	}
	return SessionAudienceMutationResult{Audience: audience}, nil
}

// RespondToSession records only the authenticated actor's response. It does
// not accept an attendee identifier, so a caller cannot write another
// learner's RSVP even when they know the source identifier.
func (repository *PostgresRepository) RespondToSession(
	ctx context.Context,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	params SelfRSVPParams,
	respondedAt time.Time,
) (SelfRSVPMutationResult, error) {
	if err := tenantContext.Validate(); err != nil || classID == uuid.Nil || sessionID == uuid.Nil {
		return SelfRSVPMutationResult{}, ErrSessionParticipationNotFound
	}
	if _, err := (SelfRSVPInput{
		State:                   params.State,
		Note:                    params.Note,
		ExpectedAttendeeVersion: params.ExpectedAttendeeVersion,
		IdempotencyKey:          params.IdempotencyKey,
	}).normalized(); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	fingerprint, err := decodeParticipationFingerprint(params.Fingerprint)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}

	queryContext, cancel := repository.contextWithTimeout(ctx)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return SelfRSVPMutationResult{}, fmt.Errorf("begin session RSVP response: %w", err)
	}
	defer rollbackClassTransaction(transaction)

	if err := repository.requireSessionSchedulingFeature(
		queryContext, transaction, tenantContext.TenantID,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	lockedClass, membership, err := repository.lockClassMutation(
		queryContext, transaction, tenantContext, classID,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := repository.authorizeLockedClass(
		tenantContext, membership, lockedClass.Class, policy.ActionClassView,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := lockParticipationIdempotency(
		queryContext, transaction, tenantContext.TenantID, params.IdempotencyKey,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if replay, err := findParticipationReplay(
		queryContext,
		transaction,
		tenantContext,
		params.IdempotencyKey,
		fingerprint,
		participationOperationRSVPRespond,
		classID,
		sessionID,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	} else if replay {
		attendee, readErr := readSelfSessionAttendee(
			queryContext, transaction, tenantContext.TenantID, classID, sessionID, tenantContext.ActorID,
		)
		if readErr != nil {
			return SelfRSVPMutationResult{}, readErr
		}
		if err := transaction.Commit(queryContext); err != nil {
			return SelfRSVPMutationResult{}, fmt.Errorf("commit replayed session RSVP response: %w", err)
		}
		return SelfRSVPMutationResult{Attendee: attendee, Replayed: true}, nil
	}

	source, err := lockParticipationSession(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if source.Status != SessionStatusScheduled || !source.ResponseRequested || source.AudienceRevision < 1 {
		return SelfRSVPMutationResult{}, ErrSessionRSVPUnavailable
	}
	attendee, err := lockSelfSessionAttendee(
		queryContext, transaction, tenantContext.TenantID, classID, sessionID, tenantContext.ActorID,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if attendee.Version != params.ExpectedAttendeeVersion {
		return SelfRSVPMutationResult{}, ErrSessionAttendeeVersionConflict
	}
	if attendee.ResponseRequested == false {
		return SelfRSVPMutationResult{}, ErrSessionRSVPUnavailable
	}
	if attendee.RSVPState == params.State && attendee.RSVPSource == "tutorhub_authenticated" &&
		optionalTextEqual(attendee.ResponseNote, params.Note) {
		if err := insertParticipationReceipt(
			queryContext, transaction, tenantContext, params.IdempotencyKey, fingerprint,
			participationOperationRSVPRespond, classID, sessionID,
			attendee.ID, uuid.Nil, attendee.Version,
		); err != nil {
			return SelfRSVPMutationResult{}, err
		}
		result := attendee.toAudienceAttendee()
		if err := transaction.Commit(queryContext); err != nil {
			return SelfRSVPMutationResult{}, fmt.Errorf("commit no-op session RSVP response: %w", err)
		}
		return SelfRSVPMutationResult{Attendee: result}, nil
	}

	invitationRevisionID, invitationSequence, err := findCurrentInvitationRevision(
		queryContext,
		transaction,
		tenantContext.TenantID,
		classID,
		sessionID,
		source.AudienceRevision,
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	updated, err := updateSelfSessionRSVP(
		queryContext,
		transaction,
		tenantContext,
		classID,
		sessionID,
		attendee,
		params,
		invitationRevisionID,
		invitationSequence,
		respondedAt.UTC(),
	)
	if err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := insertParticipationReceipt(
		queryContext, transaction, tenantContext, params.IdempotencyKey, fingerprint,
		participationOperationRSVPRespond, classID, sessionID,
		updated.ID, invitationRevisionID, updated.Version,
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := insertSessionParticipationEvent(
		queryContext,
		transaction,
		tenantContext,
		classID,
		sessionID,
		"class_session.rsvp_responded.v1",
		source.AudienceRevision,
		source.Sequence,
		0,
		string(params.State),
		respondedAt.UTC(),
	); err != nil {
		return SelfRSVPMutationResult{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return SelfRSVPMutationResult{}, fmt.Errorf("commit session RSVP response: %w", err)
	}
	return SelfRSVPMutationResult{Attendee: updated.toAudienceAttendee()}, nil
}

type participationQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readSessionAudience(
	ctx context.Context,
	queryer participationQuerier,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (SessionAudience, error) {
	var audience SessionAudience
	// Replacement locks this source FOR UPDATE before changing attendee rows.
	// Taking a compatible read lock here prevents READ COMMITTED from returning
	// a source revision from before that commit with attendees from after it.
	err := queryer.QueryRow(
		ctx,
		`SELECT audience_revision, response_requested, status
FROM tutorhub.class_sessions
WHERE tenant_id = $1 AND class_id = $2 AND id = $3
FOR SHARE`,
		tenantID,
		classID,
		sessionID,
	).Scan(
		&audience.AudienceRevision,
		&audience.ResponseRequested,
		&audience.SourceStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionAudience{}, ErrSessionParticipationNotFound
	}
	if err != nil {
		return SessionAudience{}, fmt.Errorf("read session audience source: %w", err)
	}
	rows, err := queryer.Query(
		ctx,
		`SELECT id, internal_user_id, participation_role, business_role, rsvp_state,
       responded_at, version, response_requested, response_closed_at,
       can_see_guest_list
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id = $3
  AND status = 'active'
  AND internal_user_id IS NOT NULL
ORDER BY internal_user_id ASC`,
		tenantID,
		classID,
		sessionID,
	)
	if err != nil {
		return SessionAudience{}, fmt.Errorf("read session audience attendees: %w", err)
	}
	defer rows.Close()
	audience.Attendees = make([]SessionAudienceAttendee, 0)
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
			return SessionAudience{}, fmt.Errorf("scan session audience attendee: %w", scanErr)
		}
		audience.Attendees = append(audience.Attendees, attendee)
	}
	if err := rows.Err(); err != nil {
		return SessionAudience{}, fmt.Errorf("iterate session audience attendees: %w", err)
	}
	return audience, nil
}

func lockParticipationSession(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (lockedParticipationSession, error) {
	var source lockedParticipationSession
	err := transaction.QueryRow(
		ctx,
		`SELECT id, tenant_id, class_id, title, description, starts_at, ends_at,
       timezone, status, version, organizer_user_id, show_as, visibility,
       guests_can_invite, guests_can_modify, guests_can_see_guest_list,
       response_requested, audience_revision, ical_uid, sequence
FROM tutorhub.class_sessions
WHERE tenant_id = $1 AND class_id = $2 AND id = $3
FOR UPDATE`,
		tenantID,
		classID,
		sessionID,
	).Scan(
		&source.ID,
		&source.TenantID,
		&source.ClassID,
		&source.Title,
		&source.Description,
		&source.StartsAt,
		&source.EndsAt,
		&source.Timezone,
		&source.Status,
		&source.Version,
		&source.OrganizerUserID,
		&source.ShowAs,
		&source.Visibility,
		&source.GuestsCanInvite,
		&source.GuestsCanModify,
		&source.GuestsCanSeeGuestList,
		&source.ResponseRequested,
		&source.AudienceRevision,
		&source.ICalUID,
		&source.Sequence,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedParticipationSession{}, ErrSessionParticipationNotFound
	}
	if err != nil {
		return lockedParticipationSession{}, fmt.Errorf("lock session participation source: %w", err)
	}
	return source, nil
}

func resolveInternalAudience(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	organizerUserID uuid.UUID,
	attendees []InternalAudienceAttendee,
) ([]resolvedAudienceMember, error) {
	if len(attendees) == 0 {
		return []resolvedAudienceMember{}, nil
	}
	userIDs := make([]uuid.UUID, 0, len(attendees))
	roles := make(map[uuid.UUID]ParticipationRole, len(attendees))
	for _, attendee := range attendees {
		userIDs = append(userIDs, attendee.UserID)
		roles[attendee.UserID] = attendee.ParticipationRole
	}
	rows, err := transaction.Query(
		ctx,
		`WITH requested AS (
    SELECT user_id
    FROM unnest($4::uuid[]) AS selected(user_id)
)
SELECT requested.user_id,
       CASE
           WHEN requested.user_id = $3::uuid THEN 'organizer'
           ELSE enrollment.class_role
       END AS business_role,
       CASE
           WHEN requested.user_id = $3::uuid THEN 'manual'
           ELSE 'roster'
       END AS audience_source
FROM requested
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = $1
 AND membership.user_id = requested.user_id
 AND membership.status = 'active'
JOIN tutorhub.users AS app_user
  ON app_user.id = requested.user_id
 AND app_user.status = 'active'
LEFT JOIN tutorhub.class_enrollments AS enrollment
  ON enrollment.tenant_id = $1
 AND enrollment.class_id = $2
 AND enrollment.user_id = requested.user_id
 AND enrollment.status = 'active'
WHERE requested.user_id = $3::uuid OR enrollment.user_id IS NOT NULL
ORDER BY requested.user_id ASC
FOR SHARE OF membership, app_user`,
		tenantID,
		classID,
		organizerUserID,
		userIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve session audience members: %w", err)
	}
	defer rows.Close()
	resolved := make([]resolvedAudienceMember, 0, len(attendees))
	for rows.Next() {
		var member resolvedAudienceMember
		if scanErr := rows.Scan(&member.UserID, &member.BusinessRole, &member.AudienceSource); scanErr != nil {
			return nil, fmt.Errorf("scan session audience member: %w", scanErr)
		}
		member.ParticipationRole = roles[member.UserID]
		if !validAudienceBusinessRole(member.BusinessRole) ||
			(member.AudienceSource != "roster" && member.AudienceSource != "manual") {
			return nil, ErrSessionParticipationUnavailable
		}
		resolved = append(resolved, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session audience members: %w", err)
	}
	if len(resolved) != len(attendees) {
		// Do not reveal which requested internal identity was absent, inactive, or
		// outside the class roster.
		return nil, ErrSessionParticipationNotFound
	}
	return resolved, nil
}

func lockActiveSessionAttendees(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
) ([]persistedSessionAttendee, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT id, internal_user_id, participation_role, business_role, audience_source,
       response_requested, rsvp_state, rsvp_source, responded_at, response_note, version
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id = $3
  AND status = 'active'
ORDER BY internal_user_id ASC NULLS LAST, id ASC
FOR UPDATE`,
		tenantID,
		classID,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("lock active session attendees: %w", err)
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
			return nil, fmt.Errorf("scan active session attendee: %w", scanErr)
		}
		// P3-02C accepts internal audience only. Refuse to mutate a source
		// containing a later external audience rather than silently discarding it.
		if !userID.Valid {
			return nil, ErrSessionParticipationUnavailable
		}
		attendee.UserID = userID.UUID
		attendees = append(attendees, attendee)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active session attendees: %w", err)
	}
	return attendees, nil
}

func audienceReplacementChanges(
	currentResponseRequested bool,
	desiredResponseRequested bool,
	current []persistedSessionAttendee,
	desired []resolvedAudienceMember,
) bool {
	if currentResponseRequested != desiredResponseRequested || len(current) != len(desired) {
		return true
	}
	byUserID := make(map[uuid.UUID]persistedSessionAttendee, len(current))
	for _, attendee := range current {
		byUserID[attendee.UserID] = attendee
	}
	for _, member := range desired {
		attendee, ok := byUserID[member.UserID]
		if !ok || attendee.ParticipationRole != member.ParticipationRole ||
			attendee.BusinessRole != member.BusinessRole || attendee.AudienceSource != member.AudienceSource {
			return true
		}
	}
	return false
}

func applyAudienceReplacement(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	source lockedParticipationSession,
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
				return mapParticipationPostgresError("remove session attendee", err)
			}
			continue
		}
		resetRSVP := attendee.ParticipationRole != member.ParticipationRole ||
			attendee.BusinessRole != member.BusinessRole ||
			attendee.AudienceSource != member.AudienceSource ||
			attendee.ResponseRequested != params.ResponseRequested
		attendeeChanged := attendee.ParticipationRole != member.ParticipationRole ||
			attendee.BusinessRole != member.BusinessRole ||
			attendee.AudienceSource != member.AudienceSource ||
			attendee.ResponseRequested != params.ResponseRequested
		if !attendeeChanged {
			continue
		}
		if _, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.class_session_attendees
SET participation_role = $4,
    business_role = $5,
    audience_source = $6,
    response_requested = $7,
    rsvp_state = CASE WHEN $8 THEN 'needs_action' ELSE rsvp_state END,
    rsvp_source = CASE WHEN $8 THEN 'none' ELSE rsvp_source END,
    responded_at = CASE WHEN $8 THEN NULL ELSE responded_at END,
    response_note = CASE WHEN $8 THEN NULL ELSE response_note END,
    response_invitation_revision_id = CASE WHEN $8 THEN NULL ELSE response_invitation_revision_id END,
    response_sequence = CASE WHEN $8 THEN NULL ELSE response_sequence END,
    response_closed_at = CASE
        WHEN $8 AND $7 THEN NULL
        WHEN $8 AND NOT $7 THEN $10
        ELSE response_closed_at
    END,
    updated_by = $9,
    updated_at = $10,
    version = version + 1
WHERE tenant_id = $1 AND class_id = $2 AND id = $3 AND status = 'active'`,
			tenantContext.TenantID,
			classID,
			attendee.ID,
			string(member.ParticipationRole),
			member.BusinessRole,
			member.AudienceSource,
			params.ResponseRequested,
			resetRSVP,
			tenantContext.ActorID,
			changedAt,
		); err != nil {
			return mapParticipationPostgresError("update session attendee", err)
		}
	}

	for _, member := range desired {
		if _, alreadyActive := currentByUserID[member.UserID]; alreadyActive {
			continue
		}
		if _, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.class_session_attendees (
    tenant_id, class_id, session_id, internal_user_id,
    participation_role, business_role, audience_source,
    show_as, visibility, can_invite_others, can_modify_event, can_see_guest_list,
    response_requested, status, rsvp_state, rsvp_source,
    created_by, updated_by, created_at, updated_at
)
VALUES (
    $1, $2, $3, $4,
    $5, $6, $7,
    $8, $9, $10, $11, $12,
    $13, 'active', 'needs_action', 'none',
    $14, $14, $15, $15
)`,
			tenantContext.TenantID,
			classID,
			sessionID,
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
			return mapParticipationPostgresError("insert session attendee", err)
		}
	}
	return nil
}

func updateParticipationSessionSource(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	expectedVersion int64,
	responseRequested bool,
	audienceRevision int64,
	sequence int64,
	updatedAt time.Time,
) (int64, error) {
	var version int64
	err := transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.class_sessions
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
		sessionID,
		responseRequested,
		audienceRevision,
		sequence,
		tenantContext.ActorID,
		updatedAt,
		expectedVersion,
	).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrSessionAudienceVersionConflict
	}
	if err != nil {
		return 0, mapParticipationPostgresError("update session participation source", err)
	}
	return version, nil
}

func toAudienceInput(
	attendees []InternalAudienceAttendee,
) []InternalAudienceAttendeeInput {
	result := make([]InternalAudienceAttendeeInput, 0, len(attendees))
	for _, attendee := range attendees {
		result = append(result, InternalAudienceAttendeeInput{
			UserID:            attendee.UserID,
			ParticipationRole: attendee.ParticipationRole,
		})
	}
	return result
}

func decodeParticipationFingerprint(value string) ([]byte, error) {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return nil, fmt.Errorf(
			"%w: invalid request fingerprint",
			ErrInvalidParticipationInput,
		)
	}
	return decoded, nil
}

func lockParticipationIdempotency(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	idempotencyKey string,
) error {
	lockKey := tenantID.String() + "\x00" + idempotencyKey
	if _, err := transaction.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`,
		lockKey,
	); err != nil {
		return fmt.Errorf("lock session participation idempotency key: %w", err)
	}
	return nil
}

func findParticipationReplay(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	idempotencyKey string,
	fingerprint []byte,
	operation string,
	classID uuid.UUID,
	sessionID uuid.UUID,
) (bool, error) {
	var receipt participationReceipt
	var receiptSessionID uuid.NullUUID
	var receiptActorUserID uuid.NullUUID
	err := transaction.QueryRow(
		ctx,
		`SELECT request_fingerprint, operation, class_id, session_id,
       actor_type, actor_user_id
FROM tutorhub.calendar_participation_mutation_receipts
WHERE tenant_id = $1 AND idempotency_key = $2`,
		tenantContext.TenantID,
		idempotencyKey,
	).Scan(
		&receipt.Fingerprint,
		&receipt.Operation,
		&receipt.ClassID,
		&receiptSessionID,
		&receipt.ActorType,
		&receiptActorUserID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read session participation idempotency receipt: %w", err)
	}
	if !receiptSessionID.Valid || !receiptActorUserID.Valid ||
		!bytes.Equal(receipt.Fingerprint, fingerprint) ||
		receipt.Operation != operation ||
		receipt.ClassID != classID ||
		receiptSessionID.UUID != sessionID ||
		receipt.ActorType != "tutorhub_authenticated" ||
		receiptActorUserID.UUID != tenantContext.ActorID {
		return false, ErrSessionParticipationIdempotencyConflict
	}
	return true, nil
}

func insertParticipationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	idempotencyKey string,
	fingerprint []byte,
	operation string,
	classID uuid.UUID,
	sessionID uuid.UUID,
	resultAttendeeID uuid.UUID,
	resultInvitationRevisionID uuid.UUID,
	resultVersion int64,
) error {
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_participation_mutation_receipts (
    tenant_id, idempotency_key, request_fingerprint, operation,
    class_id, session_id, actor_type, actor_user_id,
    result_attendee_id, result_invitation_revision_id, result_version
)
VALUES (
    $1, $2, $3, $4,
    $5, $6, 'tutorhub_authenticated', $7,
    $8, $9, $10
)`,
		tenantContext.TenantID,
		idempotencyKey,
		fingerprint,
		operation,
		classID,
		sessionID,
		tenantContext.ActorID,
		nullableParticipationUUID(resultAttendeeID),
		nullableParticipationUUID(resultInvitationRevisionID),
		resultVersion,
	)
	if err != nil {
		return mapParticipationPostgresError("insert session participation receipt", err)
	}
	return nil
}

func nullableParticipationUUID(value uuid.UUID) any {
	if value == uuid.Nil {
		return nil
	}
	return value
}

type participationRowScanner interface {
	Scan(...any) error
}

func scanPersistedSessionAttendee(
	row participationRowScanner,
) (persistedSessionAttendee, error) {
	var attendee persistedSessionAttendee
	err := row.Scan(
		&attendee.ID,
		&attendee.UserID,
		&attendee.ParticipationRole,
		&attendee.BusinessRole,
		&attendee.AudienceSource,
		&attendee.ResponseRequested,
		&attendee.RSVPState,
		&attendee.RSVPSource,
		&attendee.RespondedAt,
		&attendee.ResponseNote,
		&attendee.Version,
	)
	return attendee, err
}

func readSelfSessionAttendee(
	ctx context.Context,
	queryer participationQuerier,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
	userID uuid.UUID,
) (SessionAudienceAttendee, error) {
	attendee, err := scanPersistedSessionAttendee(queryer.QueryRow(
		ctx,
		`SELECT id, internal_user_id, participation_role, business_role, audience_source,
       response_requested, rsvp_state, rsvp_source, responded_at, response_note, version
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id = $3
  AND internal_user_id = $4
  AND status = 'active'`,
		tenantID,
		classID,
		sessionID,
		userID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionAudienceAttendee{}, ErrSessionRSVPUnavailable
	}
	if err != nil {
		return SessionAudienceAttendee{}, fmt.Errorf("read current session RSVP attendee: %w", err)
	}
	result := attendee.toAudienceAttendee()
	result.IsSelf = true
	return result, nil
}

func lockSelfSessionAttendee(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
	userID uuid.UUID,
) (persistedSessionAttendee, error) {
	attendee, err := scanPersistedSessionAttendee(transaction.QueryRow(
		ctx,
		`SELECT id, internal_user_id, participation_role, business_role, audience_source,
       response_requested, rsvp_state, rsvp_source, responded_at, response_note, version
FROM tutorhub.class_session_attendees
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id = $3
  AND internal_user_id = $4
  AND status = 'active'
  AND response_closed_at IS NULL
FOR UPDATE`,
		tenantID,
		classID,
		sessionID,
		userID,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return persistedSessionAttendee{}, ErrSessionRSVPUnavailable
	}
	if err != nil {
		return persistedSessionAttendee{}, fmt.Errorf("lock current session RSVP attendee: %w", err)
	}
	return attendee, nil
}

func findCurrentInvitationRevision(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
	audienceRevision int64,
) (uuid.UUID, int64, error) {
	var revisionID uuid.UUID
	var sequence int64
	err := transaction.QueryRow(
		ctx,
		`SELECT id, ical_sequence
FROM tutorhub.calendar_invitation_revisions
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id = $3
  AND audience_revision = $4
ORDER BY created_at DESC, id DESC
LIMIT 1`,
		tenantID,
		classID,
		sessionID,
		audienceRevision,
	).Scan(&revisionID, &sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, ErrSessionRSVPUnavailable
	}
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("read current session invitation revision: %w", err)
	}
	return revisionID, sequence, nil
}

func updateSelfSessionRSVP(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	attendee persistedSessionAttendee,
	params SelfRSVPParams,
	invitationRevisionID uuid.UUID,
	invitationSequence int64,
	respondedAt time.Time,
) (persistedSessionAttendee, error) {
	updated, err := scanPersistedSessionAttendee(transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.class_session_attendees
SET rsvp_state = $5,
    rsvp_source = 'tutorhub_authenticated',
    responded_at = $6,
    response_note = NULLIF($7, ''),
    response_invitation_revision_id = $8,
    response_sequence = $9,
    updated_by = $4,
    updated_at = $6,
    version = version + 1
WHERE tenant_id = $1
  AND class_id = $2
  AND session_id = $3
  AND id = $10
  AND internal_user_id = $4
  AND status = 'active'
  AND response_requested = true
  AND response_closed_at IS NULL
  AND version = $11
RETURNING id, internal_user_id, participation_role, business_role, audience_source,
          response_requested, rsvp_state, rsvp_source, responded_at, response_note, version`,
		tenantContext.TenantID,
		classID,
		sessionID,
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
			"update current session RSVP",
			err,
		)
	}
	return updated, nil
}

func (attendee persistedSessionAttendee) toAudienceAttendee() SessionAudienceAttendee {
	return SessionAudienceAttendee{
		ID:                attendee.ID,
		UserID:            attendee.UserID,
		ParticipationRole: attendee.ParticipationRole,
		BusinessRole:      attendee.BusinessRole,
		RSVPState:         attendee.RSVPState,
		RespondedAt:       attendee.RespondedAt,
		Version:           attendee.Version,
	}
}

func optionalTextEqual(current *string, desired string) bool {
	if current == nil {
		return desired == ""
	}
	return strings.TrimSpace(*current) == desired
}

func validAudienceBusinessRole(value string) bool {
	switch value {
	case "organizer",
		string(policy.OrganizationRoleTeacher),
		string(policy.ClassRoleCoTeacher),
		string(policy.ClassRoleTeachingAssistant),
		string(policy.OrganizationRoleStudent):
		return true
	default:
		return false
	}
}

func readInvitationSnapshotRecipients(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	classID uuid.UUID,
	sessionID uuid.UUID,
) ([]invitationSnapshotRecipient, error) {
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
  AND attendee.session_id = $3
  AND attendee.status = 'active'
  AND attendee.internal_user_id IS NOT NULL
ORDER BY attendee.internal_user_id ASC`,
		tenantID,
		classID,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("read invitation snapshot recipients: %w", err)
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
			return nil, fmt.Errorf("scan invitation snapshot recipient: %w", scanErr)
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
		return nil, fmt.Errorf("iterate invitation snapshot recipients: %w", err)
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

func normalizeInvitationLocale(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "en", "en-us":
		return "en-US"
	default:
		return "vi-VN"
	}
}

func validInvitationRecipient(recipient invitationSnapshotRecipient) bool {
	if recipient.AttendeeID == uuid.Nil || recipient.UserID == uuid.Nil ||
		len(recipient.Email) < 3 || len(recipient.Email) > 320 ||
		strings.Count(recipient.Email, "@") != 1 ||
		strings.ContainsAny(recipient.Email, " \t\r\n") ||
		recipient.DisplayName == "" ||
		recipient.ViewerTimezone == "" ||
		recipient.Locale != "vi-VN" && recipient.Locale != "en-US" ||
		recipient.ParticipationRole != ParticipationRoleRequired &&
			recipient.ParticipationRole != ParticipationRoleOptional ||
		!validAudienceBusinessRole(recipient.BusinessRole) ||
		recipient.AudienceSource != "roster" && recipient.AudienceSource != "manual" {
		return false
	}
	switch recipient.RSVPState {
	case RSVPStateNeedsAction:
		if recipient.RSVPSource != "none" {
			return false
		}
	case RSVPStateAccepted, RSVPStateTentative, RSVPStateDeclined:
		if recipient.RSVPSource == "none" {
			return false
		}
	default:
		return false
	}
	_, err := time.LoadLocation(recipient.ViewerTimezone)
	return err == nil
}

func (repository *PostgresRepository) insertInvitationSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	source lockedParticipationSession,
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
		return snapshotAttendees[left].UserID.String() < snapshotAttendees[right].UserID.String()
	})
	payload, err := json.Marshal(canonicalSessionInvitationSnapshot{
		SchemaVersion:     "tutorhub.calendar.invitation.v1",
		SourceType:        "class_session",
		SourceID:          source.ID,
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
		return fmt.Errorf("encode invitation canonical snapshot: %w", err)
	}
	payloadDigest := sha256.Sum256(payload)
	sealedPayload, err := repository.calendarProtector.Seal(protecteddata.Context{
		TenantID: tenantContext.TenantID.String(),
		Purpose:  protecteddata.PurposeInvitationCanonicalPayload,
		RecordID: revisionID.String(),
	}, payload)
	if err != nil {
		return fmt.Errorf("protect invitation canonical snapshot: %w", err)
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.calendar_invitation_revisions (
    id, tenant_id, class_id, session_id,
    source_version, audience_revision, ical_uid, ical_sequence,
    method, lifecycle, organizer_user_id, actor_type, created_by,
    reason_code, timezone_data_version,
    canonical_payload_ciphertext, canonical_payload_sha256,
    crypto_key_version, created_at
)
VALUES (
    $1, $2, $3, $4,
    $5, $6, $7, $8,
    NULL, $9, $10, 'user', $11,
    'audience_replaced', $12,
    $13, $14,
    $15, $16
)`,
		revisionID,
		tenantContext.TenantID,
		source.ClassID,
		source.ID,
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
		return mapParticipationPostgresError("insert invitation canonical snapshot", err)
	}

	for _, recipient := range recipients {
		sealedAddress, sealErr := repository.calendarProtector.Seal(protecteddata.Context{
			TenantID: tenantContext.TenantID.String(),
			Purpose:  protecteddata.PurposeInvitationRecipientAddress,
			RecordID: recipient.ID.String(),
		}, []byte(recipient.Email))
		if sealErr != nil {
			return fmt.Errorf("protect invitation recipient address: %w", sealErr)
		}
		sealedName, sealErr := repository.calendarProtector.Seal(protecteddata.Context{
			TenantID: tenantContext.TenantID.String(),
			Purpose:  protecteddata.PurposeInvitationRecipientDisplayName,
			RecordID: recipient.ID.String(),
		}, []byte(recipient.DisplayName))
		if sealErr != nil {
			return fmt.Errorf("protect invitation recipient display name: %w", sealErr)
		}
		addressFingerprint, fingerprintErr := repository.calendarProtector.DeliveryAddressFingerprint(
			tenantContext.TenantID.String(),
			[]byte(recipient.Email),
		)
		if fingerprintErr != nil {
			return fmt.Errorf("fingerprint invitation recipient address: %w", fingerprintErr)
		}
		if sealedAddress.KeyVersion != sealedPayload.KeyVersion ||
			sealedName.KeyVersion != sealedPayload.KeyVersion {
			return ErrSessionParticipationUnavailable
		}
		if _, insertErr := transaction.Exec(
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
		); insertErr != nil {
			return mapParticipationPostgresError("insert invitation recipient snapshot", insertErr)
		}
	}
	return nil
}

func insertSessionParticipationEvent(
	ctx context.Context,
	transaction pgx.Tx,
	tenantContext tenancy.Context,
	classID uuid.UUID,
	sessionID uuid.UUID,
	eventType string,
	audienceRevision int64,
	sequence int64,
	attendeeCount int,
	rsvpState string,
	occurredAt time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'class_session', $2, $3,
    jsonb_build_object(
        'session_id', $2::uuid,
        'class_id', $4::uuid,
        'actor_user_id', $5::uuid,
        'audience_revision', $6::bigint,
        'sequence', $7::bigint,
        'attendee_count', $8::integer,
        'rsvp_state', NULLIF($9::text, '')
    ),
    $10, $10
)`,
		tenantContext.TenantID,
		sessionID,
		eventType,
		classID,
		tenantContext.ActorID,
		audienceRevision,
		sequence,
		attendeeCount,
		rsvpState,
		occurredAt,
	); err != nil {
		return fmt.Errorf("insert %s outbox event: %w", eventType, err)
	}
	metadata := audit.Metadata{
		"class_id":          classID.String(),
		"audience_revision": fmt.Sprintf("%d", audienceRevision),
		"sequence":          fmt.Sprintf("%d", sequence),
		"attendee_count":    fmt.Sprintf("%d", attendeeCount),
	}
	if rsvpState != "" {
		metadata["rsvp_state"] = rsvpState
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID:      tenantContext.TenantID,
		ActorID:       tenantContext.ActorID,
		EventType:     eventType,
		AggregateType: "class_session",
		AggregateID:   sessionID,
		Metadata:      metadata,
		OccurredAt:    occurredAt,
	}); err != nil {
		return fmt.Errorf("insert %s audit event: %w", eventType, err)
	}
	return nil
}

func mapParticipationPostgresError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return fmt.Errorf("%s: %w", operation, err)
	}
	switch postgresError.ConstraintName {
	case "class_session_attendees_audience_limit":
		return fmt.Errorf(
			"%w: audience cannot exceed %d distinct attendees",
			ErrInvalidParticipationInput,
			maximumAudienceAttendees,
		)
	case "calendar_participation_mutation_receipts_pkey":
		return ErrSessionParticipationIdempotencyConflict
	case "calendar_invitation_revisions_source_sequence_idx":
		return ErrSessionAudienceVersionConflict
	case "class_session_attendees_session_fk",
		"calendar_invitation_revisions_session_fk",
		"calendar_invitation_recipients_attendee_fk",
		"calendar_participation_receipts_session_fk":
		return ErrSessionParticipationNotFound
	case "class_session_attendees_internal_membership_fk",
		"class_session_attendees_creator_membership_fk",
		"class_session_attendees_updater_membership_fk",
		"class_session_attendees_remover_membership_fk",
		"calendar_invitation_revisions_organizer_membership_fk",
		"calendar_invitation_revisions_creator_membership_fk",
		"calendar_participation_receipts_actor_membership_fk":
		return ErrSessionParticipationAccessDenied
	default:
		if strings.HasPrefix(postgresError.ConstraintName, "class_session_attendees_") {
			return fmt.Errorf("%w: %s", ErrInvalidParticipationInput, postgresError.ConstraintName)
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
}
