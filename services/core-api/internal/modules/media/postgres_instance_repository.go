package media

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const (
	mediaCredentialRatePurpose = "media.join_credential"
	mediaCredentialRateLimit   = int64(30)
	mediaCredentialRateWindow  = 10 * time.Minute
	providerReceiptRetention   = 30 * 24 * time.Hour
	providerWebhookMaxAge      = 24 * time.Hour
	providerWebhookFutureSkew  = 5 * time.Minute
)

type PostgresInstanceRepository struct {
	lifecycle *PostgresLifecycleRepository
	newID     func() uuid.UUID
}

func NewPostgresInstanceRepository(
	lifecycle *PostgresLifecycleRepository,
	newID func() uuid.UUID,
) (*PostgresInstanceRepository, error) {
	if lifecycle == nil || lifecycle.database == nil || lifecycle.controls == nil {
		return nil, fmt.Errorf("media lifecycle repository is required")
	}
	if newID == nil {
		newID = uuid.New
	}
	return &PostgresInstanceRepository{lifecycle: lifecycle, newID: newID}, nil
}

func (repository *PostgresInstanceRepository) ActivateRoomInstance(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	roomInstanceID uuid.UUID,
	providerRoomSID string,
	activatedAt time.Time,
) (MediaSpace, error) {
	if repository == nil || repository.lifecycle == nil || spaceID == uuid.Nil ||
		roomInstanceID == uuid.Nil || !validProviderIdentifier(providerRoomSID, 255) {
		return MediaSpace{}, ErrLifecycleUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return MediaSpace{}, repository.lifecycle.unavailable("begin room activation", err)
	}
	defer rollbackLifecycle(transaction)
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return MediaSpace{}, err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return MediaSpace{}, err
	}
	space, err := loadSpace(queryContext, transaction, scope.TenantID, spaceID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaSpace{}, ErrSpaceNotFound
	}
	if err != nil {
		return MediaSpace{}, repository.lifecycle.unavailable("lock room activation space", err)
	}
	if err := repository.lifecycle.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return MediaSpace{}, err
	}
	if _, err := repository.lifecycle.authorizeSource(
		queryContext, transaction, access, scope, space, policy.ActionSessionStart, true, false,
	); err != nil {
		return MediaSpace{}, err
	}
	if space.Status != SpaceStatusOpen {
		return MediaSpace{}, ErrRoomNotOpen
	}
	activatedAt = activatedAt.UTC()
	commandTag, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.media_room_instances
SET status = 'active', version = version + 1, provider_room_sid = $5,
    activated_at = $6, updated_by = $4, updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND id = $3
  AND status = 'provisioning' AND provider_room_sid IS NULL`,
		scope.TenantID, spaceID, roomInstanceID, scope.ActorID, providerRoomSID, activatedAt,
	)
	if err != nil {
		return MediaSpace{}, repository.lifecycle.unavailable("activate provider room", err)
	}
	if commandTag.RowsAffected() == 0 {
		var status RoomInstanceStatus
		var existingSID sql.NullString
		err := transaction.QueryRow(
			queryContext,
			`SELECT status, provider_room_sid
FROM tutorhub.media_room_instances
WHERE tenant_id = $1 AND space_id = $2 AND id = $3
FOR UPDATE`,
			scope.TenantID, spaceID, roomInstanceID,
		).Scan(&status, &existingSID)
		if errors.Is(err, pgx.ErrNoRows) {
			return MediaSpace{}, ErrSpaceTransition
		}
		if err != nil {
			return MediaSpace{}, repository.lifecycle.unavailable("read provider room activation", err)
		}
		if status == RoomInstanceClosing || status == RoomInstanceEnded ||
			status == RoomInstanceFailed {
			return MediaSpace{}, errRoomActivationTerminal
		}
		if status != RoomInstanceActive || !existingSID.Valid || existingSID.String != providerRoomSID {
			return MediaSpace{}, ErrSpaceTransition
		}
	}
	projected, err := repository.lifecycle.projectAuthorizedSpace(
		queryContext, transaction, access, scope, space,
	)
	if err != nil {
		return MediaSpace{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MediaSpace{}, repository.lifecycle.unavailable("commit room activation", err)
	}
	return projected, nil
}

func (repository *PostgresInstanceRepository) ProviderRoomName(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
) (string, error) {
	if repository == nil || repository.lifecycle == nil || spaceID == uuid.Nil {
		return "", ErrLifecycleUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return "", repository.lifecycle.unavailable("begin provider room lookup", err)
	}
	defer rollbackLifecycle(transaction)
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return "", err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return "", err
	}
	space, err := loadSpace(queryContext, transaction, scope.TenantID, spaceID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSpaceNotFound
	}
	if err != nil {
		return "", repository.lifecycle.unavailable("load provider room space", err)
	}
	if _, err := repository.lifecycle.authorizeSource(
		queryContext, transaction, access, scope, space, policy.ActionClassView, false, false,
	); err != nil {
		return "", err
	}
	var providerRoomName string
	err = transaction.QueryRow(
		queryContext,
		`SELECT provider_room_name
FROM tutorhub.media_room_instances
WHERE tenant_id = $1 AND space_id = $2
ORDER BY attempt_number DESC
LIMIT 1`,
		scope.TenantID, spaceID,
	).Scan(&providerRoomName)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSpaceTransition
	}
	if err != nil {
		return "", repository.lifecycle.unavailable("load provider room name", err)
	}
	if !opaqueProviderRoomNamePattern.MatchString(providerRoomName) {
		return "", ErrLifecycleUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return "", repository.lifecycle.unavailable("commit provider room lookup", err)
	}
	return providerRoomName, nil
}

func (repository *PostgresInstanceRepository) PrepareCredential(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	joinAttemptID uuid.UUID,
	now time.Time,
) (ParticipantCredentialGrant, error) {
	if repository == nil || repository.lifecycle == nil || spaceID == uuid.Nil ||
		joinAttemptID == uuid.Nil || access.SessionID == uuid.Nil {
		return ParticipantCredentialGrant{}, ErrInvalidCredentialRequest
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"begin media credential", err,
		)
	}
	defer rollbackLifecycle(transaction)
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return ParticipantCredentialGrant{}, err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return ParticipantCredentialGrant{}, err
	}
	space, err := loadSpace(queryContext, transaction, scope.TenantID, spaceID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return ParticipantCredentialGrant{}, ErrSpaceNotFound
	}
	if err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"lock media credential space", err,
		)
	}
	if err := repository.lifecycle.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return ParticipantCredentialGrant{}, err
	}
	if space.Status != SpaceStatusOpen {
		return ParticipantCredentialGrant{}, ErrRoomNotOpen
	}
	if space.Locked {
		return ParticipantCredentialGrant{}, ErrRoomLocked
	}
	source, err := repository.lifecycle.authorizeSource(
		queryContext, transaction, access, scope, space, policy.ActionSessionJoin, false, false,
	)
	if err != nil {
		return ParticipantCredentialGrant{}, err
	}
	if !sourceAvailableForJoin(source) {
		return ParticipantCredentialGrant{}, ErrRoomNotOpen
	}
	room, err := loadActiveRoom(queryContext, transaction, scope.TenantID, space.ID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return ParticipantCredentialGrant{}, ErrRoomNotOpen
	}
	if err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"lock media credential room", err,
		)
	}
	if room.Status != RoomInstanceActive || !room.ProviderRoomSID.Valid ||
		room.ProviderRoomName == "" {
		return ParticipantCredentialGrant{}, ErrRoomNotOpen
	}
	if space.LobbyEnabled && source.InstanceRole == InstanceRoleAttendee {
		return ParticipantCredentialGrant{}, ErrAdmissionRequired
	}
	now = now.UTC()
	if err := consumeMediaCredentialRateLimit(
		queryContext, transaction, scope.TenantID, scope.ActorID, access.SessionID, now,
	); err != nil {
		return ParticipantCredentialGrant{}, err
	}

	existing, found, err := loadParticipantByAttempt(
		queryContext, transaction, scope.TenantID, room.ID, scope.ActorID, joinAttemptID,
	)
	if err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"load media participant retry", err,
		)
	}
	if found {
		if !activeParticipantStatus(existing.Status) {
			return ParticipantCredentialGrant{}, ErrParticipantConflict
		}
		if existing.InstanceRole != source.InstanceRole {
			if _, err := transaction.Exec(
				queryContext,
				`UPDATE tutorhub.media_participant_sessions
SET instance_role = $5, version = version + 1, updated_at = $6
WHERE tenant_id = $1 AND room_instance_id = $2 AND user_id = $3
  AND join_attempt_id = $4
  AND status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')`,
				scope.TenantID, room.ID, scope.ActorID, joinAttemptID,
				source.InstanceRole, now,
			); err != nil {
				return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
					"refresh media participant authority", err,
				)
			}
			existing.InstanceRole = source.InstanceRole
		}
		if err := transaction.Commit(queryContext); err != nil {
			return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
				"commit media credential retry", err,
			)
		}
		return participantGrant(access, room, existing, source), nil
	}

	if occupied, err := hasActiveParticipant(
		queryContext, transaction, scope.TenantID, room.ID, scope.ActorID,
	); err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"check active media participant", err,
		)
	} else if occupied {
		return ParticipantCredentialGrant{}, ErrParticipantConflict
	}
	if err := acquireLifecycleTransactionLock(
		queryContext, transaction, "media-participant-capacity:"+scope.TenantID.String(),
	); err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"lock media participant capacity", err,
		)
	}
	if err := repository.requireParticipantCapacity(
		queryContext, transaction, scope.TenantID, space.ID,
	); err != nil {
		return ParticipantCredentialGrant{}, err
	}
	participant := participantRow{
		ID:               repository.newID(),
		JoinAttemptID:    joinAttemptID,
		ProviderIdentity: opaqueParticipantIdentity(repository.newID()),
		InstanceRole:     source.InstanceRole,
		Status:           "joining",
		CreatedAt:        now,
	}
	if participant.ID == uuid.Nil || !validProviderIdentifier(participant.ProviderIdentity, 128) {
		return ParticipantCredentialGrant{}, ErrLifecycleUnavailable
	}
	if _, err := transaction.Exec(
		queryContext,
		`INSERT INTO tutorhub.media_participant_sessions (
    id, tenant_id, space_id, room_instance_id, user_id, join_attempt_id,
    provider_participant_identity, instance_role, status, capacity_reserved,
    admitted_at, joining_at, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'joining', true, $9, $9, $9, $9)`,
		participant.ID, scope.TenantID, space.ID, room.ID, scope.ActorID,
		joinAttemptID, participant.ProviderIdentity, participant.InstanceRole, now,
	); err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"insert media participant session", err,
		)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"commit media credential", err,
		)
	}
	return participantGrant(access, room, participant, source), nil
}

func (repository *PostgresInstanceRepository) requireParticipantCapacity(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
) error {
	var tenantUsed, spaceUsed int64
	if err := transaction.QueryRow(
		ctx,
		`SELECT
    count(*),
    count(*) FILTER (WHERE space_id = $2)
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND capacity_reserved = true
  AND status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')`,
		tenantID, spaceID,
	).Scan(&tenantUsed, &spaceUsed); err != nil {
		return repository.lifecycle.unavailable("count media participant capacity", err)
	}
	if err := repository.lifecycle.controls.RequireQuotaAtMost(
		ctx, transaction, tenantID, featurecontrol.QuotaActiveMediaParticipants, tenantUsed+1,
	); err != nil {
		return err
	}
	return repository.lifecycle.controls.RequireQuotaAtMost(
		ctx, transaction, tenantID, featurecontrol.QuotaMediaParticipantsPerSpace, spaceUsed+1,
	)
}

func (repository *PostgresInstanceRepository) AllowLegacyMedia(
	ctx context.Context,
	tenantID uuid.UUID,
) (bool, error) {
	if repository == nil || repository.lifecycle == nil || tenantID == uuid.Nil {
		return false, ErrUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return false, repository.lifecycle.unavailable("begin legacy media gate", err)
	}
	defer rollbackLifecycle(transaction)
	err = repository.lifecycle.controls.RequireFeature(
		queryContext, transaction, tenantID, featurecontrol.FeatureClassroomMediaRooms,
	)
	allowed := errors.Is(err, featurecontrol.ErrFeatureDisabled)
	if err != nil && !allowed {
		return false, repository.lifecycle.unavailable("evaluate legacy media gate", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return false, repository.lifecycle.unavailable("commit legacy media gate", err)
	}
	return allowed, nil
}

func (repository *PostgresInstanceRepository) RecordProviderWebhook(
	ctx context.Context,
	event WebhookEvent,
	receivedAt time.Time,
) (WebhookResult, bool, error) {
	if repository == nil || repository.lifecycle == nil {
		return WebhookResult{}, false, ErrUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return WebhookResult{}, false, repository.lifecycle.unavailable(
			"begin provider webhook", err,
		)
	}
	defer rollbackLifecycle(transaction)
	tenantID, found, err := discoverProviderRoomTenant(queryContext, transaction, event)
	if err != nil {
		return WebhookResult{}, false, repository.lifecycle.unavailable(
			"discover provider room tenant", err,
		)
	}
	if !found {
		return WebhookResult{Ignored: true}, false, nil
	}
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, tenantID,
	); err != nil {
		return WebhookResult{}, true, err
	}
	binding, found, err := loadProviderRoomBinding(
		queryContext, transaction, tenantID, event,
	)
	if err != nil {
		return WebhookResult{}, true, repository.lifecycle.unavailable(
			"load provider room binding", err,
		)
	}
	if !found {
		return WebhookResult{Ignored: true}, true, nil
	}
	receivedAt = receivedAt.UTC()
	var participant *participantRow
	if event.EventType == "participant_joined" || event.EventType == "participant_left" {
		loaded, participantFound, loadErr := loadWebhookParticipant(
			queryContext, transaction, binding, strings.TrimSpace(event.ParticipantIdentity),
		)
		if loadErr != nil {
			return WebhookResult{}, true, repository.lifecycle.unavailable(
				"load provider webhook participant", loadErr,
			)
		}
		if participantFound {
			participant = &loaded
		}
	}
	disposition, mutation := classifyProviderWebhook(binding, participant, event, receivedAt)
	inserted, err := insertProviderWebhookReceipt(
		queryContext, transaction, binding, participant, event, disposition, receivedAt,
	)
	if err != nil {
		return WebhookResult{}, true, repository.lifecycle.unavailable(
			"insert provider webhook receipt", err,
		)
	}
	if !inserted {
		if err := transaction.Commit(queryContext); err != nil {
			return WebhookResult{}, true, repository.lifecycle.unavailable(
				"commit provider webhook replay", err,
			)
		}
		return WebhookResult{Duplicate: true}, true, nil
	}
	if mutation != nil {
		if err := mutation(queryContext, transaction); err != nil {
			return WebhookResult{}, true, repository.lifecycle.unavailable(
				"apply provider webhook", err,
			)
		}
	}
	if err := transaction.Commit(queryContext); err != nil {
		return WebhookResult{}, true, repository.lifecycle.unavailable(
			"commit provider webhook", err,
		)
	}
	return WebhookResult{
		Recorded: true,
		Ignored:  disposition != "applied",
	}, true, nil
}

type participantRow struct {
	ID               uuid.UUID
	JoinAttemptID    uuid.UUID
	ProviderIdentity string
	InstanceRole     InstanceRole
	Status           string
	CreatedAt        time.Time
	ConnectedAt      sql.NullTime
	ReconnectingAt   sql.NullTime
	TerminalAt       sql.NullTime
}

func loadParticipantByAttempt(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	roomInstanceID uuid.UUID,
	userID uuid.UUID,
	joinAttemptID uuid.UUID,
) (participantRow, bool, error) {
	var participant participantRow
	err := transaction.QueryRow(
		ctx,
		`SELECT id, join_attempt_id, provider_participant_identity,
       instance_role, status, created_at, connected_at, reconnecting_at, terminal_at
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND room_instance_id = $2 AND user_id = $3
  AND join_attempt_id = $4
FOR UPDATE`,
		tenantID, roomInstanceID, userID, joinAttemptID,
	).Scan(
		&participant.ID, &participant.JoinAttemptID, &participant.ProviderIdentity,
		&participant.InstanceRole, &participant.Status, &participant.CreatedAt,
		&participant.ConnectedAt, &participant.ReconnectingAt, &participant.TerminalAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return participantRow{}, false, nil
	}
	return participant, err == nil, err
}

func hasActiveParticipant(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	roomInstanceID uuid.UUID,
	userID uuid.UUID,
) (bool, error) {
	var found bool
	err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1 FROM tutorhub.media_participant_sessions
    WHERE tenant_id = $1 AND room_instance_id = $2 AND user_id = $3
      AND status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')
)`,
		tenantID, roomInstanceID, userID,
	).Scan(&found)
	return found, err
}

func activeParticipantStatus(status string) bool {
	switch status {
	case "waiting", "admitted", "joining", "connected", "reconnecting":
		return true
	default:
		return false
	}
}

func sourceAvailableForJoin(source authorizedSource) bool {
	switch source.Kind {
	case SourceClassSession:
		return source.Status == "live"
	case SourceClassSessionOccurrence, SourceStudyMeeting:
		return source.Status == "scheduled"
	default:
		return false
	}
}

func participantGrant(
	access AccessContext,
	room roomRow,
	participant participantRow,
	source authorizedSource,
) ParticipantCredentialGrant {
	return ParticipantCredentialGrant{
		ParticipantSessionID:        participant.ID,
		RoomInstanceID:              room.ID,
		JoinAttemptID:               participant.JoinAttemptID,
		ProviderRoomName:            room.ProviderRoomName,
		ProviderParticipantIdentity: participant.ProviderIdentity,
		ParticipantName:             strings.TrimSpace(access.DisplayName),
		InstanceRole:                participant.InstanceRole,
		CanPublishCameraMicrophone:  source.CanPublishCameraMicrophone,
		CanShareScreen:              source.CanShareScreen,
		CanSubscribe:                true,
	}
}

func opaqueParticipantIdentity(id uuid.UUID) string {
	return "p_" + strings.ReplaceAll(id.String(), "-", "")
}

func consumeMediaCredentialRateLimit(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	sessionID uuid.UUID,
	now time.Time,
) error {
	windowStartedAt := now.Truncate(mediaCredentialRateWindow)
	windowEndsAt := windowStartedAt.Add(mediaCredentialRateWindow)
	bucketHash := sha256.Sum256([]byte(
		mediaCredentialRatePurpose + "\x00" + tenantID.String() + "\x00" +
			actorID.String() + "\x00" + sessionID.String(),
	))
	var used int64
	err := transaction.QueryRow(
		ctx,
		`INSERT INTO tutorhub.rate_limit_windows (
    purpose, bucket_hash, window_started_at, window_ends_at, used_count, updated_at
)
VALUES ($1, $2, $3, $4, 1, $5)
ON CONFLICT (purpose, bucket_hash, window_started_at)
DO UPDATE SET
    used_count = tutorhub.rate_limit_windows.used_count + 1,
    updated_at = EXCLUDED.updated_at
WHERE tutorhub.rate_limit_windows.used_count < $6
RETURNING used_count`,
		mediaCredentialRatePurpose, bucketHash[:], windowStartedAt, windowEndsAt,
		now, mediaCredentialRateLimit,
	).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		retryAfter := windowEndsAt.Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return &CredentialRateLimitError{RetryAfter: retryAfter}
	}
	if err != nil {
		return fmt.Errorf("consume media credential rate limit: %w", err)
	}
	return nil
}

type providerRoomBinding struct {
	TenantID       uuid.UUID
	SpaceID        uuid.UUID
	RoomInstanceID uuid.UUID
	Status         RoomInstanceStatus
	Version        int64
	ProviderName   string
	ProviderSID    sql.NullString
	CreatedAt      time.Time
	ActivatedAt    sql.NullTime
}

func discoverProviderRoomTenant(
	ctx context.Context,
	transaction pgx.Tx,
	event WebhookEvent,
) (uuid.UUID, bool, error) {
	roomSID := strings.TrimSpace(event.RoomSID)
	roomName := strings.TrimSpace(event.RoomName)
	var tenantID uuid.UUID
	err := transaction.QueryRow(
		ctx,
		`SELECT tenant_id
FROM tutorhub.media_room_instances
WHERE provider_kind = 'livekit'
  AND (
      ($1 <> '' AND provider_room_sid = $1)
      OR ($2 <> '' AND provider_room_name = $2)
  )
ORDER BY CASE WHEN provider_room_sid = $1 THEN 0 ELSE 1 END
LIMIT 1`,
		roomSID, roomName,
	).Scan(&tenantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, nil
	}
	return tenantID, err == nil, err
}

func loadProviderRoomBinding(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	event WebhookEvent,
) (providerRoomBinding, bool, error) {
	roomSID := strings.TrimSpace(event.RoomSID)
	roomName := strings.TrimSpace(event.RoomName)
	var binding providerRoomBinding
	err := transaction.QueryRow(
		ctx,
		`SELECT tenant_id, space_id, id, status, version, provider_room_name,
       provider_room_sid, created_at, activated_at
FROM tutorhub.media_room_instances
WHERE tenant_id = $1
  AND provider_kind = 'livekit'
  AND (
      ($2 <> '' AND provider_room_sid = $2)
      OR ($3 <> '' AND provider_room_name = $3)
  )
ORDER BY CASE WHEN provider_room_sid = $2 THEN 0 ELSE 1 END
LIMIT 1
FOR UPDATE`,
		tenantID, roomSID, roomName,
	).Scan(
		&binding.TenantID, &binding.SpaceID, &binding.RoomInstanceID,
		&binding.Status, &binding.Version, &binding.ProviderName, &binding.ProviderSID,
		&binding.CreatedAt, &binding.ActivatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return providerRoomBinding{}, false, nil
	}
	return binding, err == nil, err
}

type webhookMutation func(context.Context, pgx.Tx) error

func classifyProviderWebhook(
	binding providerRoomBinding,
	participant *participantRow,
	event WebhookEvent,
	receivedAt time.Time,
) (string, webhookMutation) {
	roomSID := strings.TrimSpace(event.RoomSID)
	roomName := strings.TrimSpace(event.RoomName)
	if (roomName != "" && roomName != binding.ProviderName) ||
		(binding.ProviderSID.Valid && roomSID != "" && roomSID != binding.ProviderSID.String) {
		return "ignored_mismatch", nil
	}
	if providerEventBefore(event.OccurredAt, binding.CreatedAt) ||
		event.OccurredAt.Before(receivedAt.Add(-providerWebhookMaxAge)) ||
		event.OccurredAt.After(receivedAt.Add(providerWebhookFutureSkew)) {
		return "ignored_stale", nil
	}
	eventAt := event.OccurredAt.UTC()
	switch strings.TrimSpace(event.EventType) {
	case "room_started":
		if roomSID == "" {
			return "ignored_mismatch", nil
		}
		switch binding.Status {
		case RoomInstanceProvisioning:
			transitionAt := providerTransitionTime(eventAt, binding.CreatedAt)
			return "applied", func(ctx context.Context, transaction pgx.Tx) error {
				tag, err := transaction.Exec(
					ctx,
					`UPDATE tutorhub.media_room_instances
SET status = 'active', version = version + 1, provider_room_sid = $4,
    activated_at = $5, updated_at = $5
WHERE tenant_id = $1 AND space_id = $2 AND id = $3
  AND status = 'provisioning'
  AND (provider_room_sid IS NULL OR provider_room_sid = $4)`,
					binding.TenantID, binding.SpaceID, binding.RoomInstanceID, roomSID, transitionAt,
				)
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return ErrSpaceTransition
				}
				return nil
			}
		case RoomInstanceActive:
			return "ignored_stale", nil
		default:
			return "ignored_terminal", nil
		}
	case "room_finished":
		switch binding.Status {
		case RoomInstanceActive:
			stateAt := binding.CreatedAt
			if binding.ActivatedAt.Valid {
				stateAt = binding.ActivatedAt.Time
			}
			if providerEventBefore(eventAt, stateAt) {
				return "ignored_stale", nil
			}
			transitionAt := providerTransitionTime(eventAt, stateAt)
			return "applied", func(ctx context.Context, transaction pgx.Tx) error {
				tag, err := transaction.Exec(
					ctx,
					`UPDATE tutorhub.media_room_instances
SET status = 'closing', version = version + 1, closing_at = $4, updated_at = $4
WHERE tenant_id = $1 AND space_id = $2 AND id = $3 AND status = 'active'`,
					binding.TenantID, binding.SpaceID, binding.RoomInstanceID, transitionAt,
				)
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return ErrSpaceTransition
				}
				return terminateRoomParticipants(
					ctx,
					transaction,
					binding.TenantID,
					binding.SpaceID,
					binding.RoomInstanceID,
					transitionAt,
				)
			}
		case RoomInstanceProvisioning:
			transitionAt := providerTransitionTime(eventAt, binding.CreatedAt)
			return "applied", func(ctx context.Context, transaction pgx.Tx) error {
				tag, err := transaction.Exec(
					ctx,
					`UPDATE tutorhub.media_room_instances
SET status = 'failed', version = version + 1, failed_at = $4,
    failure_code = 'provider_room_finished', updated_at = $4
WHERE tenant_id = $1 AND space_id = $2 AND id = $3 AND status = 'provisioning'`,
					binding.TenantID, binding.SpaceID, binding.RoomInstanceID, transitionAt,
				)
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return ErrSpaceTransition
				}
				return terminateRoomParticipants(
					ctx,
					transaction,
					binding.TenantID,
					binding.SpaceID,
					binding.RoomInstanceID,
					transitionAt,
				)
			}
		case RoomInstanceClosing:
			return "ignored_stale", nil
		default:
			return "ignored_terminal", nil
		}
	case "participant_joined", "participant_left":
		return classifyParticipantWebhook(binding, participant, event, eventAt)
	default:
		return "ignored_unsupported_event", nil
	}
}

func classifyParticipantWebhook(
	binding providerRoomBinding,
	participant *participantRow,
	event WebhookEvent,
	transitionAt time.Time,
) (string, webhookMutation) {
	if binding.Status != RoomInstanceActive {
		return "ignored_terminal", nil
	}
	identity := strings.TrimSpace(event.ParticipantIdentity)
	if identity == "" || participant == nil {
		return "ignored_unknown_participant", nil
	}
	lastTransitionAt := participantLastTransition(*participant)
	if providerEventBefore(transitionAt, lastTransitionAt) {
		return "ignored_stale", nil
	}
	transitionAt = providerTransitionTime(transitionAt, lastTransitionAt)
	if event.EventType == "participant_joined" {
		switch participant.Status {
		case "joining", "reconnecting":
		case "connected":
			return "ignored_stale", nil
		case "left", "removed", "failed":
			return "ignored_terminal", nil
		default:
			return "ignored_stale", nil
		}
		return "applied", func(ctx context.Context, transaction pgx.Tx) error {
			tag, err := transaction.Exec(
				ctx,
				`UPDATE tutorhub.media_participant_sessions
SET status = 'connected', version = version + 1,
    connected_at = COALESCE(connected_at, $5), reconnecting_at = NULL,
    updated_at = $5
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND id = $4 AND status IN ('joining', 'reconnecting')`,
				binding.TenantID, binding.SpaceID, binding.RoomInstanceID,
				participant.ID, transitionAt,
			)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrSpaceTransition
			}
			return nil
		}
	}
	switch participant.Status {
	case "admitted", "joining", "connected", "reconnecting":
	case "left", "removed", "failed":
		return "ignored_terminal", nil
	default:
		return "ignored_stale", nil
	}
	return "applied", func(ctx context.Context, transaction pgx.Tx) error {
		tag, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.media_participant_sessions
SET status = 'left', version = version + 1, capacity_reserved = false,
    terminal_at = $5, reconnecting_at = NULL, updated_at = $5
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND id = $4 AND status IN ('admitted', 'joining', 'connected', 'reconnecting')`,
			binding.TenantID, binding.SpaceID, binding.RoomInstanceID,
			participant.ID, transitionAt,
		)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrSpaceTransition
		}
		return nil
	}
}

func loadWebhookParticipant(
	ctx context.Context,
	transaction pgx.Tx,
	binding providerRoomBinding,
	identity string,
) (participantRow, bool, error) {
	if identity == "" {
		return participantRow{}, false, nil
	}
	var participant participantRow
	err := transaction.QueryRow(
		ctx,
		`SELECT id, join_attempt_id, provider_participant_identity,
       instance_role, status, created_at, connected_at, reconnecting_at, terminal_at
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND provider_participant_identity = $4
FOR UPDATE`,
		binding.TenantID, binding.SpaceID, binding.RoomInstanceID, identity,
	).Scan(
		&participant.ID, &participant.JoinAttemptID, &participant.ProviderIdentity,
		&participant.InstanceRole, &participant.Status, &participant.CreatedAt,
		&participant.ConnectedAt, &participant.ReconnectingAt, &participant.TerminalAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return participantRow{}, false, nil
	}
	return participant, err == nil, err
}

func participantLastTransition(participant participantRow) time.Time {
	latest := participant.CreatedAt
	for _, candidate := range []sql.NullTime{
		participant.ConnectedAt,
		participant.ReconnectingAt,
		participant.TerminalAt,
	} {
		if candidate.Valid && candidate.Time.After(latest) {
			latest = candidate.Time
		}
	}
	return latest
}

// LiveKit WebhookEvent.created_at is expressed in Unix seconds. PostgreSQL
// lifecycle timestamps retain finer precision, so same-second events must not
// be rejected merely because the database value has microseconds. When two
// signed events share a second, the persisted lifecycle state and receipt
// serialization provide the deterministic ordering boundary.
func providerEventBefore(eventAt time.Time, stateAt time.Time) bool {
	return eventAt.UTC().Before(stateAt.UTC().Truncate(time.Second))
}

func providerTransitionTime(eventAt time.Time, stateAt time.Time) time.Time {
	eventAt, stateAt = eventAt.UTC(), stateAt.UTC()
	if eventAt.Before(stateAt) {
		return stateAt
	}
	return eventAt
}

func insertProviderWebhookReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	binding providerRoomBinding,
	participant *participantRow,
	event WebhookEvent,
	disposition string,
	receivedAt time.Time,
) (bool, error) {
	var participantID any
	if participant != nil && participant.ID != uuid.Nil {
		participantID = participant.ID
	}
	var inserted string
	err := transaction.QueryRow(
		ctx,
		`INSERT INTO tutorhub.media_provider_webhook_receipts (
    provider_kind, event_id, tenant_id, space_id, room_instance_id,
    participant_session_id, event_type, disposition, occurred_at,
    received_at, retention_until
)
VALUES ('livekit', $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (provider_kind, event_id) DO NOTHING
RETURNING event_id`,
		strings.TrimSpace(event.ID), binding.TenantID, binding.SpaceID,
		binding.RoomInstanceID, participantID, strings.TrimSpace(event.EventType),
		disposition, event.OccurredAt.UTC(), receivedAt,
		receivedAt.Add(providerReceiptRetention),
	).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func validProviderIdentifier(value string, maximum int) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
