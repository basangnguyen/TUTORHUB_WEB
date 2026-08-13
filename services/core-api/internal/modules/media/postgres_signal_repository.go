package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	mediaHandActorRatePurpose       = "media.signal.hand.actor"
	mediaHandRoomRatePurpose        = "media.signal.hand.room"
	mediaHandLowerAllRatePurpose    = "media.signal.hand.lower_all"
	mediaReactionBurstActorPurpose  = "media.signal.reaction.actor_burst"
	mediaReactionMinuteActorPurpose = "media.signal.reaction.actor_minute"
	mediaReactionRoomPurpose        = "media.signal.reaction.room_burst"
)

type PostgresMediaSignalRepository struct {
	lifecycle *PostgresLifecycleRepository
	newID     func() uuid.UUID
}

func NewPostgresMediaSignalRepository(
	lifecycle *PostgresLifecycleRepository,
	newID func() uuid.UUID,
) (*PostgresMediaSignalRepository, error) {
	if lifecycle == nil || lifecycle.database == nil || lifecycle.controls == nil {
		return nil, fmt.Errorf("media lifecycle repository is required")
	}
	if newID == nil {
		newID = uuid.New
	}
	return &PostgresMediaSignalRepository{lifecycle: lifecycle, newID: newID}, nil
}

type signalRoomRow struct {
	ID                 uuid.UUID
	Status             RoomInstanceStatus
	Version            int64
	ProjectionVersion  int64
	LastSignalSequence int64
	NextRosterSequence int64
}

type signalParticipantRow struct {
	ID             uuid.UUID
	ParticipantKey uuid.UUID
	RosterSequence int64
	InstanceRole   InstanceRole
	Status         ParticipantConnectionState
}

type signalAuthority struct {
	access      AccessContext
	scope       tenancy.Context
	space       spaceRow
	room        signalRoomRow
	participant signalParticipantRow
	moderator   bool
}

func (repository *PostgresMediaSignalRepository) GetParticipantSnapshot(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input GetMediaParticipantSnapshotInput,
) (MediaParticipantSnapshot, error) {
	if repository == nil || repository.lifecycle == nil {
		return MediaParticipantSnapshot{}, ErrMediaSignalUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("begin participant snapshot", err)
	}
	defer rollbackLifecycle(transaction)

	authority, err := repository.loadSignalAuthority(
		queryContext, transaction, access, spaceID, input.ExpectedSpaceVersion,
		input.ExpectedRoomInstanceID, input.ExpectedRoomInstanceVersion, false, true,
	)
	if err != nil {
		return MediaParticipantSnapshot{}, err
	}
	databaseTime, err := loadMediaSignalDatabaseTime(queryContext, transaction)
	if err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("read participant snapshot time", err)
	}
	snapshot, err := loadMediaParticipantSnapshot(queryContext, transaction, authority, databaseTime)
	if err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("load participant snapshot", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("commit participant snapshot", err)
	}
	return snapshot, nil
}

func (repository *PostgresMediaSignalRepository) SendSignal(
	ctx context.Context,
	access AccessContext,
	command SendMediaSignalCommand,
) (MediaParticipantSnapshot, error) {
	if repository == nil || repository.lifecycle == nil {
		return MediaParticipantSnapshot{}, ErrMediaSignalUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("begin participant signal", err)
	}
	defer rollbackLifecycle(transaction)

	authority, err := repository.loadSignalAuthority(
		queryContext, transaction, access, command.SpaceID,
		command.ExpectedSpaceVersion, command.ExpectedRoomInstanceID,
		command.ExpectedRoomInstanceVersion, true, false,
	)
	if err != nil {
		return MediaParticipantSnapshot{}, err
	}
	replayFingerprint, replayed, err := loadMediaSignalReplay(
		queryContext, transaction, authority.scope.TenantID, authority.room.ID,
		authority.scope.ActorID, command.IdempotencyKey,
	)
	if err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("load participant signal replay", err)
	}
	if replayed {
		if !bytes.Equal(replayFingerprint, command.Fingerprint) {
			return MediaParticipantSnapshot{}, ErrMediaSignalIdempotency
		}
		acceptedAt, timeErr := loadMediaSignalDatabaseTime(queryContext, transaction)
		if timeErr != nil {
			return MediaParticipantSnapshot{}, signalRepositoryUnavailable("read replayed participant signal time", timeErr)
		}
		snapshot, loadErr := loadMediaParticipantSnapshot(
			queryContext, transaction, authority, acceptedAt,
		)
		if loadErr != nil {
			return MediaParticipantSnapshot{}, signalRepositoryUnavailable("load replayed participant signal", loadErr)
		}
		if commitErr := transaction.Commit(queryContext); commitErr != nil {
			return MediaParticipantSnapshot{}, signalRepositoryUnavailable("commit participant signal replay", commitErr)
		}
		return snapshot, nil
	}
	if authority.space.Version != command.ExpectedSpaceVersion ||
		authority.room.Version != command.ExpectedRoomInstanceVersion {
		return MediaParticipantSnapshot{}, ErrMediaSignalVersionConflict
	}
	if authority.room.ProjectionVersion != command.ExpectedProjectionVersion {
		return MediaParticipantSnapshot{}, ErrMediaSignalVersionConflict
	}
	acceptedAt, err := loadMediaSignalDatabaseTime(queryContext, transaction)
	if err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("read participant signal time", err)
	}
	if err := consumeMediaSignalLimits(
		queryContext, transaction, authority, command, acceptedAt,
	); err != nil {
		return MediaParticipantSnapshot{}, err
	}

	changed, err := repository.applySignal(queryContext, transaction, &authority, command)
	if err != nil {
		return MediaParticipantSnapshot{}, err
	}
	if changed {
		if err := advanceSignalRoomProjection(
			queryContext, transaction, authority.scope.TenantID, command.SpaceID,
			&authority.room,
		); err != nil {
			return MediaParticipantSnapshot{}, signalRepositoryUnavailable("advance participant signal sequence", err)
		}
		if err := repository.persistSignalChange(
			queryContext, transaction, authority, command, acceptedAt,
		); err != nil {
			return MediaParticipantSnapshot{}, err
		}
	}
	if err := insertMediaSignalReceipt(
		queryContext, transaction, authority, command, acceptedAt,
	); err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("record participant signal receipt", err)
	}
	snapshot, err := loadMediaParticipantSnapshot(
		queryContext, transaction, authority, acceptedAt,
	)
	if err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("load mutated participant snapshot", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MediaParticipantSnapshot{}, signalRepositoryUnavailable("commit participant signal", err)
	}
	return snapshot, nil
}

func loadMediaSignalDatabaseTime(
	ctx context.Context,
	transaction pgx.Tx,
) (time.Time, error) {
	var databaseTime time.Time
	if err := transaction.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&databaseTime); err != nil {
		return time.Time{}, err
	}
	return databaseTime.UTC(), nil
}

func (repository *PostgresMediaSignalRepository) loadSignalAuthority(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	spaceID uuid.UUID,
	expectedSpaceVersion int64,
	expectedRoomID uuid.UUID,
	expectedRoomVersion int64,
	lock bool,
	enforceExpectedVersions bool,
) (signalAuthority, error) {
	if lock {
		if err := repository.lifecycle.acquireTenantControlLock(
			ctx, transaction, access.TenantID,
		); err != nil {
			return signalAuthority{}, err
		}
	} else if err := featurecontrol.AcquireTenantControlReadLock(
		ctx, transaction, access.TenantID,
	); err != nil {
		return signalAuthority{}, err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(ctx, transaction, access)
	if err != nil {
		return signalAuthority{}, err
	}
	space, err := loadSpace(ctx, transaction, scope.TenantID, spaceID, lock)
	if errors.Is(err, pgx.ErrNoRows) {
		return signalAuthority{}, ErrMediaSignalNotFound
	}
	if err != nil {
		return signalAuthority{}, signalRepositoryUnavailable("load participant signal space", err)
	}
	source, err := repository.lifecycle.authorizeSource(
		ctx, transaction, access, scope, space, policy.ActionSessionJoin, false, false,
	)
	if err != nil {
		if errors.Is(err, ErrSpaceAccessDenied) || errors.Is(err, ErrSpaceNotFound) ||
			errors.Is(err, ErrSourceUnavailable) {
			return signalAuthority{}, ErrMediaSignalNotFound
		}
		return signalAuthority{}, err
	}
	if lock {
		if err := repository.lifecycle.controls.RequireFeature(
			ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
		); err != nil {
			return signalAuthority{}, err
		}
	} else if err := repository.lifecycle.controls.RequireFeatureForRead(
		ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return signalAuthority{}, err
	}
	if enforceExpectedVersions && space.Version != expectedSpaceVersion {
		return signalAuthority{}, ErrMediaSignalVersionConflict
	}
	// A room lock stops new joins/admissions but does not eject an already-active
	// participant or revoke their in-room controls (ADR-0030 section 8).
	if space.Status != SpaceStatusOpen {
		return signalAuthority{}, ErrRoomNotOpen
	}
	room, err := loadSignalRoom(
		ctx, transaction, scope.TenantID, space.ID, expectedRoomID, lock,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return signalAuthority{}, ErrMediaSignalNotFound
	}
	if err != nil {
		return signalAuthority{}, signalRepositoryUnavailable("load participant signal room", err)
	}
	if enforceExpectedVersions && room.Version != expectedRoomVersion {
		return signalAuthority{}, ErrMediaSignalVersionConflict
	}
	if room.Status != RoomInstanceActive {
		return signalAuthority{}, ErrRoomNotOpen
	}
	participant, err := loadSignalParticipantByActor(
		ctx, transaction, scope.TenantID, space.ID, room.ID, scope.ActorID, lock,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return signalAuthority{}, ErrMediaSignalNotFound
	}
	if err != nil {
		return signalAuthority{}, signalRepositoryUnavailable("load participant signal actor", err)
	}
	return signalAuthority{
		access: access, scope: scope, space: space, room: room, participant: participant,
		moderator: source.InstanceRole == InstanceRoleHost ||
			source.InstanceRole == InstanceRoleCoHost ||
			source.InstanceRole == InstanceRoleTeachingAssistant,
	}, nil
}

func loadSignalRoom(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	lock bool,
) (signalRoomRow, error) {
	query := `SELECT id, status, version, projection_version,
       last_signal_sequence, next_roster_sequence
FROM tutorhub.media_room_instances
WHERE tenant_id = $1 AND space_id = $2 AND id = $3`
	if lock {
		query += " FOR UPDATE"
	}
	var room signalRoomRow
	err := transaction.QueryRow(ctx, query, tenantID, spaceID, roomID).Scan(
		&room.ID, &room.Status, &room.Version, &room.ProjectionVersion,
		&room.LastSignalSequence, &room.NextRosterSequence,
	)
	return room, err
}

func loadSignalParticipantByActor(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	actorID uuid.UUID,
	lock bool,
) (signalParticipantRow, error) {
	query := `SELECT id, participant_key, roster_sequence, instance_role, status
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND user_id = $4 AND status IN ('joining', 'connected', 'reconnecting')`
	if lock {
		query += " FOR UPDATE"
	}
	var participant signalParticipantRow
	err := transaction.QueryRow(ctx, query, tenantID, spaceID, roomID, actorID).Scan(
		&participant.ID, &participant.ParticipantKey, &participant.RosterSequence,
		&participant.InstanceRole, &participant.Status,
	)
	return participant, err
}

func loadMediaParticipantSnapshot(
	ctx context.Context,
	transaction pgx.Tx,
	authority signalAuthority,
	databaseTime time.Time,
) (MediaParticipantSnapshot, error) {
	snapshot := MediaParticipantSnapshot{
		RoomInstanceID:     authority.room.ID,
		ProjectionVersion:  authority.room.ProjectionVersion,
		LastSignalSequence: authority.room.LastSignalSequence,
		SelfParticipantKey: authority.participant.ParticipantKey,
		ViewerOperations: MediaSignalViewerOperations{
			CanRaiseHand: true, CanSendReaction: true,
			CanModerateHands: authority.moderator,
		},
		Participants: []MediaParticipant{}, RaisedHands: []RaisedHand{},
		ReactionClusters: []ReactionCluster{}, ServerTime: databaseTime.UTC(),
	}
	rows, err := transaction.Query(
		ctx,
		`SELECT participant.participant_key, participant.roster_sequence,
       target.display_name, participant.instance_role, participant.status
FROM tutorhub.media_participant_sessions AS participant
JOIN tutorhub.users AS target ON target.id = participant.user_id
WHERE participant.tenant_id = $1 AND participant.space_id = $2
  AND participant.room_instance_id = $3
  AND participant.status IN ('joining', 'connected', 'reconnecting')
ORDER BY participant.roster_sequence, participant.participant_key
LIMIT 50`,
		authority.scope.TenantID, authority.space.ID, authority.room.ID,
	)
	if err != nil {
		return MediaParticipantSnapshot{}, err
	}
	for rows.Next() {
		var participant MediaParticipant
		if err := rows.Scan(
			&participant.ParticipantKey, &participant.RosterSequence,
			&participant.DisplayName, &participant.InstanceRole, &participant.Connection,
		); err != nil {
			rows.Close()
			return MediaParticipantSnapshot{}, err
		}
		participant.DisplayName = truncateMediaDisplayName(participant.DisplayName)
		snapshot.Participants = append(snapshot.Participants, participant)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return MediaParticipantSnapshot{}, err
	}
	rows.Close()

	handRows, err := transaction.Query(
		ctx,
		`SELECT participant.participant_key, hand.signal_sequence, hand.raised_at
FROM tutorhub.media_participant_hand_states AS hand
JOIN tutorhub.media_participant_sessions AS participant
  ON participant.tenant_id = hand.tenant_id
 AND participant.space_id = hand.space_id
 AND participant.room_instance_id = hand.room_instance_id
 AND participant.id = hand.participant_session_id
WHERE hand.tenant_id = $1 AND hand.space_id = $2 AND hand.room_instance_id = $3
  AND hand.is_raised
  AND participant.status IN ('joining', 'connected', 'reconnecting')
ORDER BY hand.signal_sequence, participant.participant_key
LIMIT 50`,
		authority.scope.TenantID, authority.space.ID, authority.room.ID,
	)
	if err != nil {
		return MediaParticipantSnapshot{}, err
	}
	for handRows.Next() {
		var hand RaisedHand
		if err := handRows.Scan(&hand.ParticipantKey, &hand.SignalSequence, &hand.RaisedAt); err != nil {
			handRows.Close()
			return MediaParticipantSnapshot{}, err
		}
		hand.RaisedAt = hand.RaisedAt.UTC()
		snapshot.RaisedHands = append(snapshot.RaisedHands, hand)
	}
	if err := handRows.Err(); err != nil {
		handRows.Close()
		return MediaParticipantSnapshot{}, err
	}
	handRows.Close()

	reactionRows, err := transaction.Query(
		ctx,
		`SELECT reaction, signal_sequence, accepted_at, expires_at
FROM tutorhub.media_reaction_events
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND expires_at > $4
ORDER BY signal_sequence
LIMIT 500`,
		authority.scope.TenantID, authority.space.ID, authority.room.ID, databaseTime.UTC(),
	)
	if err != nil {
		return MediaParticipantSnapshot{}, err
	}
	events := make([]mediaReactionEvent, 0)
	for reactionRows.Next() {
		var event mediaReactionEvent
		if err := reactionRows.Scan(
			&event.Reaction, &event.SignalSequence, &event.AcceptedAt, &event.ExpiresAt,
		); err != nil {
			reactionRows.Close()
			return MediaParticipantSnapshot{}, err
		}
		event.AcceptedAt, event.ExpiresAt = event.AcceptedAt.UTC(), event.ExpiresAt.UTC()
		events = append(events, event)
	}
	if err := reactionRows.Err(); err != nil {
		reactionRows.Close()
		return MediaParticipantSnapshot{}, err
	}
	reactionRows.Close()
	snapshot.ReactionClusters = groupMediaReactionEvents(events)
	return snapshot, nil
}

func truncateMediaDisplayName(value string) string {
	runes := []rune(value)
	if len(runes) > 200 {
		runes = runes[:200]
	}
	return string(runes)
}

func loadMediaSignalReplay(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	roomID uuid.UUID,
	actorID uuid.UUID,
	key string,
) ([]byte, bool, error) {
	var fingerprint []byte
	err := transaction.QueryRow(
		ctx,
		`SELECT request_fingerprint
FROM tutorhub.media_signal_mutation_receipts
WHERE tenant_id = $1 AND room_instance_id = $2
  AND actor_user_id = $3 AND idempotency_key = $4`,
		tenantID, roomID, actorID, key,
	).Scan(&fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return fingerprint, err == nil, err
}

func (repository *PostgresMediaSignalRepository) applySignal(
	ctx context.Context,
	transaction pgx.Tx,
	authority *signalAuthority,
	command SendMediaSignalCommand,
) (bool, error) {
	switch command.Kind {
	case MediaSignalHandRaise:
		var exists bool
		if err := transaction.QueryRow(
			ctx,
			`SELECT EXISTS (
    SELECT 1 FROM tutorhub.media_participant_hand_states
    WHERE tenant_id = $1 AND room_instance_id = $2 AND participant_session_id = $3
      AND is_raised
)`,
			authority.scope.TenantID, authority.room.ID, authority.participant.ID,
		).Scan(&exists); err != nil {
			return false, signalRepositoryUnavailable("inspect active participant hand", err)
		}
		return !exists, nil
	case MediaSignalHandLower:
		tag, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.media_participant_hand_states
SET is_raised = false
WHERE tenant_id = $1 AND room_instance_id = $2 AND participant_session_id = $3
  AND is_raised`,
			authority.scope.TenantID, authority.room.ID, authority.participant.ID,
		)
		if err != nil {
			return false, signalRepositoryUnavailable("lower own participant hand", err)
		}
		return tag.RowsAffected() > 0, nil
	case MediaSignalHandLowerOne:
		if !authority.moderator {
			return false, ErrSpaceAccessDenied
		}
		var targetID uuid.UUID
		err := transaction.QueryRow(
			ctx,
			`SELECT id
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND participant_key = $4 AND status IN ('joining', 'connected', 'reconnecting')
FOR UPDATE`,
			authority.scope.TenantID, authority.space.ID, authority.room.ID,
			command.TargetParticipantKey,
		).Scan(&targetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrMediaSignalTargetUnavailable
		}
		if err != nil {
			return false, signalRepositoryUnavailable("load participant hand target", err)
		}
		tag, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.media_participant_hand_states
SET is_raised = false
WHERE tenant_id = $1 AND room_instance_id = $2 AND participant_session_id = $3
  AND is_raised`,
			authority.scope.TenantID, authority.room.ID, targetID,
		)
		if err != nil {
			return false, signalRepositoryUnavailable("lower participant hand", err)
		}
		return tag.RowsAffected() > 0, nil
	case MediaSignalHandLowerAll:
		if !authority.moderator {
			return false, ErrSpaceAccessDenied
		}
		tag, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.media_participant_hand_states
SET is_raised = false
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND is_raised`,
			authority.scope.TenantID, authority.space.ID, authority.room.ID,
		)
		if err != nil {
			return false, signalRepositoryUnavailable("lower all participant hands", err)
		}
		return tag.RowsAffected() > 0, nil
	case MediaSignalReaction:
		return true, nil
	default:
		return false, ErrInvalidMediaSignalRequest
	}
}

func advanceSignalRoomProjection(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	room *signalRoomRow,
) error {
	return transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.media_room_instances
SET projection_version = projection_version + 1,
    last_signal_sequence = last_signal_sequence + 1
WHERE tenant_id = $1 AND space_id = $2 AND id = $3 AND status = 'active'
RETURNING projection_version, last_signal_sequence`,
		tenantID, spaceID, room.ID,
	).Scan(&room.ProjectionVersion, &room.LastSignalSequence)
}

func (repository *PostgresMediaSignalRepository) persistSignalChange(
	ctx context.Context,
	transaction pgx.Tx,
	authority signalAuthority,
	command SendMediaSignalCommand,
	acceptedAt time.Time,
) error {
	switch command.Kind {
	case MediaSignalHandRaise:
		_, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.media_participant_hand_states (
    tenant_id, space_id, room_instance_id, participant_session_id,
    signal_sequence, raised_at
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (tenant_id, room_instance_id, participant_session_id)
DO UPDATE SET is_raised = true,
              signal_sequence = EXCLUDED.signal_sequence,
              raised_at = EXCLUDED.raised_at`,
			authority.scope.TenantID, authority.space.ID, authority.room.ID,
			authority.participant.ID, authority.room.LastSignalSequence,
			acceptedAt,
		)
		if err != nil {
			return signalRepositoryUnavailable("raise participant hand", err)
		}
	case MediaSignalReaction:
		eventID := repository.newID()
		if eventID == uuid.Nil {
			return ErrMediaSignalUnavailable
		}
		_, err := transaction.Exec(
			ctx,
			`INSERT INTO tutorhub.media_reaction_events (
    id, tenant_id, space_id, room_instance_id, participant_session_id,
    reaction, signal_sequence, accepted_at, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			eventID, authority.scope.TenantID, authority.space.ID, authority.room.ID,
			authority.participant.ID, command.Reaction,
			authority.room.LastSignalSequence, acceptedAt,
			acceptedAt.Add(mediaReactionTTL),
		)
		if err != nil {
			return signalRepositoryUnavailable("record participant reaction", err)
		}
	}
	return nil
}

func insertMediaSignalReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	authority signalAuthority,
	command SendMediaSignalCommand,
	acceptedAt time.Time,
) error {
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.media_signal_mutation_receipts (
    tenant_id, space_id, room_instance_id, actor_user_id, idempotency_key,
    request_fingerprint, kind, result_projection_version,
    result_signal_sequence, created_at, retention_until
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		authority.scope.TenantID, authority.space.ID, authority.room.ID,
		authority.scope.ActorID, command.IdempotencyKey, command.Fingerprint,
		command.Kind, authority.room.ProjectionVersion,
		authority.room.LastSignalSequence, acceptedAt,
		acceptedAt.Add(mediaSignalReceiptRetention),
	)
	return err
}

type mediaSignalRateSpec struct {
	purpose string
	bucket  string
	window  time.Duration
	limit   int64
}

func consumeMediaSignalLimits(
	ctx context.Context,
	transaction pgx.Tx,
	authority signalAuthority,
	command SendMediaSignalCommand,
	acceptedAt time.Time,
) error {
	tenant := authority.scope.TenantID.String()
	room := authority.room.ID.String()
	actor := authority.scope.ActorID.String()
	var specs []mediaSignalRateSpec
	switch command.Kind {
	case MediaSignalReaction:
		specs = []mediaSignalRateSpec{
			{mediaReactionBurstActorPurpose, tenant + "\x00" + room + "\x00" + actor, 5 * time.Second, 3},
			{mediaReactionMinuteActorPurpose, tenant + "\x00" + room + "\x00" + actor, time.Minute, 20},
			{mediaReactionRoomPurpose, tenant + "\x00" + room, 5 * time.Second, 100},
		}
	case MediaSignalHandLowerAll:
		specs = []mediaSignalRateSpec{
			{mediaHandLowerAllRatePurpose, tenant + "\x00" + room + "\x00" + actor, time.Minute, 6},
		}
	case MediaSignalHandRaise, MediaSignalHandLower, MediaSignalHandLowerOne:
		specs = []mediaSignalRateSpec{
			{mediaHandActorRatePurpose, tenant + "\x00" + room + "\x00" + actor, time.Minute, 6},
			{mediaHandRoomRatePurpose, tenant + "\x00" + room, time.Minute, 120},
		}
	default:
		return ErrInvalidMediaSignalRequest
	}
	for _, spec := range specs {
		if err := consumeMediaSignalRateLimit(
			ctx, transaction, spec, acceptedAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func consumeMediaSignalRateLimit(
	ctx context.Context,
	transaction pgx.Tx,
	spec mediaSignalRateSpec,
	acceptedAt time.Time,
) error {
	windowStartedAt := acceptedAt.Truncate(spec.window)
	windowEndsAt := windowStartedAt.Add(spec.window)
	bucketHash := sha256.Sum256([]byte(spec.purpose + "\x00" + spec.bucket))
	var used int64
	err := transaction.QueryRow(
		ctx,
		`INSERT INTO tutorhub.rate_limit_windows (
    purpose, bucket_hash, window_started_at, window_ends_at, used_count, updated_at
)
VALUES ($1, $2, $3, $4, 1, $5)
ON CONFLICT (purpose, bucket_hash, window_started_at)
DO UPDATE SET used_count = tutorhub.rate_limit_windows.used_count + 1,
              updated_at = EXCLUDED.updated_at
WHERE tutorhub.rate_limit_windows.used_count < $6
RETURNING used_count`,
		spec.purpose, bucketHash[:], windowStartedAt, windowEndsAt, acceptedAt, spec.limit,
	).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		retryAfter := windowEndsAt.Sub(acceptedAt)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return &MediaSignalRateLimitError{RetryAfter: retryAfter}
	}
	if err != nil {
		return signalRepositoryUnavailable("consume participant signal rate limit", err)
	}
	return nil
}

func signalRepositoryUnavailable(operation string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrMediaSignalUnavailable, operation, err)
}
