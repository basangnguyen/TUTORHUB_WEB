package media

import (
	"bytes"
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

func (repository *PostgresInstanceRepository) ClaimEndProviderEffect(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	idempotencyKey string,
	now time.Time,
	lease time.Duration,
) (EndRoomProviderEffect, ProviderEffectStatus, bool, error) {
	if repository == nil || repository.lifecycle == nil || spaceID == uuid.Nil ||
		idempotencyKey == "" || lease <= 0 {
		return EndRoomProviderEffect{}, ProviderEffectPermanentFailed, false,
			ErrLifecycleUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return EndRoomProviderEffect{}, ProviderEffectRetryableFailed, false,
			repository.lifecycle.unavailable("begin end provider effect", err)
	}
	defer rollbackLifecycle(transaction)
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return EndRoomProviderEffect{}, ProviderEffectRetryableFailed, false, err
	}
	_, scope, err := repository.lifecycle.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return EndRoomProviderEffect{}, ProviderEffectPermanentFailed, false, err
	}
	var effect EndRoomProviderEffect
	var currentStatus ProviderEffectStatus
	err = transaction.QueryRow(
		queryContext,
		`SELECT room.provider_room_name, receipt.provider_effect_attempts,
       receipt.provider_effect_status
FROM tutorhub.media_space_mutation_receipts AS receipt
JOIN tutorhub.media_room_instances AS room
  ON room.tenant_id = receipt.tenant_id
 AND room.space_id = receipt.space_id
 AND room.id = receipt.result_room_instance_id
WHERE receipt.tenant_id = $1 AND receipt.actor_user_id = $2
  AND receipt.space_id = $3 AND receipt.idempotency_key = $4
  AND receipt.operation = 'end' AND receipt.provider_effect_required
FOR UPDATE OF receipt`,
		scope.TenantID, scope.ActorID, spaceID, idempotencyKey,
	).Scan(&effect.RoomName, &effect.Attempt, &currentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return EndRoomProviderEffect{}, ProviderEffectPermanentFailed, false,
			ErrSpaceTransition
	}
	if err != nil || !opaqueProviderRoomNamePattern.MatchString(effect.RoomName) {
		return EndRoomProviderEffect{}, ProviderEffectRetryableFailed, false,
			ErrLifecycleUnavailable
	}
	if currentStatus == ProviderEffectApplied || currentStatus == ProviderEffectPermanentFailed {
		return effect, currentStatus, false, nil
	}
	effect.Attempt++
	var claimed ProviderEffectStatus
	err = transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.media_space_mutation_receipts
SET provider_effect_status = 'applying', provider_effect_attempts = $5,
    provider_effect_error_code = NULL, provider_effect_lease_until = $6,
    provider_effect_updated_at = $7
WHERE tenant_id = $1 AND actor_user_id = $2 AND space_id = $3
  AND idempotency_key = $4 AND operation = 'end' AND provider_effect_required
  AND (provider_effect_status IN ('pending', 'retryable_failed')
       OR (provider_effect_status = 'applying' AND provider_effect_lease_until <= $7))
RETURNING provider_effect_status`,
		scope.TenantID, scope.ActorID, spaceID, idempotencyKey, effect.Attempt,
		now.UTC().Add(lease), now.UTC(),
	).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := transaction.Commit(queryContext); err != nil {
			return effect, ProviderEffectRetryableFailed, false, ErrLifecycleUnavailable
		}
		return effect, currentStatus, false, nil
	}
	if err != nil {
		return EndRoomProviderEffect{}, ProviderEffectRetryableFailed, false,
			ErrLifecycleUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return EndRoomProviderEffect{}, ProviderEffectRetryableFailed, false,
			ErrLifecycleUnavailable
	}
	return effect, claimed, true, nil
}

func (repository *PostgresInstanceRepository) CompleteEndProviderEffect(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	idempotencyKey string,
	attempt int,
	status ProviderEffectStatus,
	errorCode string,
	now time.Time,
) (ProviderEffectStatus, error) {
	if repository == nil || repository.lifecycle == nil || spaceID == uuid.Nil ||
		idempotencyKey == "" || attempt < 1 ||
		(status != ProviderEffectApplied && status != ProviderEffectRetryableFailed &&
			status != ProviderEffectPermanentFailed) {
		return ProviderEffectPermanentFailed, ErrLifecycleUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return ProviderEffectRetryableFailed, ErrLifecycleUnavailable
	}
	defer rollbackLifecycle(transaction)
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return ProviderEffectRetryableFailed, err
	}
	_, scope, err := repository.lifecycle.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return ProviderEffectPermanentFailed, err
	}
	var completed ProviderEffectStatus
	err = transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.media_space_mutation_receipts
SET provider_effect_status = $6, provider_effect_error_code = NULLIF($7, ''),
    provider_effect_lease_until = NULL, provider_effect_updated_at = $8
WHERE tenant_id = $1 AND actor_user_id = $2 AND space_id = $3
  AND idempotency_key = $4 AND operation = 'end' AND provider_effect_required
  AND provider_effect_status = 'applying' AND provider_effect_attempts = $5
RETURNING provider_effect_status`,
		scope.TenantID, scope.ActorID, spaceID, idempotencyKey, attempt,
		status, errorCode, now.UTC(),
	).Scan(&completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderEffectRetryableFailed, ErrSpaceVersionConflict
	}
	if err != nil {
		return ProviderEffectRetryableFailed, ErrLifecycleUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ProviderEffectRetryableFailed, ErrLifecycleUnavailable
	}
	return completed, nil
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
	source, err := repository.lifecycle.authorizeSource(
		queryContext, transaction, access, scope, space, policy.ActionSessionJoin, false, false,
	)
	if err != nil {
		return ParticipantCredentialGrant{}, concealJoinSourceError(err)
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
	source, err = resolveEffectiveCredentialSource(
		queryContext, transaction, scope.TenantID, room.ID, scope.ActorID, source,
	)
	if err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"resolve media credential role", err,
		)
	}
	now = now.UTC()
	existing, found, err := loadParticipantByAttempt(
		queryContext, transaction, scope.TenantID, room.ID, scope.ActorID, joinAttemptID,
	)
	if err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"load media participant retry", err,
		)
	}
	if !found {
		return ParticipantCredentialGrant{}, ErrParticipantConflict
	}
	if existing.Status == string(JoinAttemptWaiting) {
		return ParticipantCredentialGrant{}, ErrAdmissionRequired
	}
	if existing.Status != string(JoinAttemptAdmitted) &&
		existing.Status != string(JoinAttemptJoining) {
		return ParticipantCredentialGrant{}, ErrParticipantConflict
	}
	if space.LobbyEnabled && source.InstanceRole == InstanceRoleAttendee {
		if err := requireAdmittedAdmission(
			queryContext, transaction, scope.TenantID, space.ID, room.ID,
			scope.ActorID, existing,
		); err != nil {
			return ParticipantCredentialGrant{}, err
		}
	}
	if err := consumeMediaCredentialRateLimit(
		queryContext, transaction, scope.TenantID, scope.ActorID, access.SessionID, now,
	); err != nil {
		return ParticipantCredentialGrant{}, err
	}
	if existing.Status == string(JoinAttemptAdmitted) {
		err := transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.media_participant_sessions
SET status = 'joining', instance_role = $5, version = version + 1,
    joining_at = $6, updated_at = $6
WHERE tenant_id = $1 AND room_instance_id = $2 AND user_id = $3
  AND join_attempt_id = $4 AND status = 'admitted'
RETURNING version`,
			scope.TenantID, room.ID, scope.ActorID, joinAttemptID,
			source.InstanceRole, now,
		).Scan(&existing.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return ParticipantCredentialGrant{}, ErrParticipantConflict
		}
		if err != nil {
			return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
				"advance admitted media participant", err,
			)
		}
		existing.Status = string(JoinAttemptJoining)
		existing.InstanceRole = source.InstanceRole
		existing.JoiningAt = sql.NullTime{Time: now, Valid: true}
		existing.UpdatedAt = now
		if err := advanceMediaRosterProjection(
			queryContext, transaction, scope.TenantID, space.ID, room.ID,
		); err != nil {
			return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
				"advance media roster joining projection", err,
			)
		}
	} else if existing.InstanceRole != source.InstanceRole {
		err := transaction.QueryRow(
			queryContext,
			`UPDATE tutorhub.media_participant_sessions
SET instance_role = $5, version = version + 1, updated_at = $6
WHERE tenant_id = $1 AND room_instance_id = $2 AND user_id = $3
  AND join_attempt_id = $4 AND status = 'joining'
RETURNING version`,
			scope.TenantID, room.ID, scope.ActorID, joinAttemptID,
			source.InstanceRole, now,
		).Scan(&existing.Version)
		if errors.Is(err, pgx.ErrNoRows) {
			return ParticipantCredentialGrant{}, ErrParticipantConflict
		}
		if err != nil {
			return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
				"refresh media participant authority", err,
			)
		}
		existing.InstanceRole = source.InstanceRole
		existing.UpdatedAt = now
		if err := advanceMediaRosterProjection(
			queryContext, transaction, scope.TenantID, space.ID, room.ID,
		); err != nil {
			return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
				"advance media roster role projection", err,
			)
		}
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ParticipantCredentialGrant{}, repository.lifecycle.unavailable(
			"commit media credential", err,
		)
	}
	return participantGrant(access, room, existing, source), nil
}

func (repository *PostgresInstanceRepository) CreateOrReuseJoinAttempt(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input CreateJoinAttemptInput,
	now time.Time,
) (CreateJoinAttemptResult, error) {
	if repository == nil || repository.lifecycle == nil || spaceID == uuid.Nil ||
		input.JoinAttemptID == uuid.Nil || input.ExpectedRoomInstanceID == uuid.Nil ||
		input.ExpectedSpaceVersion < 1 || access.SessionID == uuid.Nil {
		return CreateJoinAttemptResult{}, ErrInvalidJoinAttempt
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"begin media join attempt", err,
		)
	}
	defer rollbackLifecycle(transaction)
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return CreateJoinAttemptResult{}, err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return CreateJoinAttemptResult{}, err
	}
	space, err := loadSpace(queryContext, transaction, scope.TenantID, spaceID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreateJoinAttemptResult{}, ErrSpaceNotFound
	}
	if err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"lock media join-attempt space", err,
		)
	}
	source, err := repository.lifecycle.authorizeSource(
		queryContext, transaction, access, scope, space, policy.ActionSessionJoin, false, false,
	)
	if err != nil {
		return CreateJoinAttemptResult{}, concealJoinSourceError(err)
	}
	if err := repository.lifecycle.controls.RequireFeature(
		queryContext, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return CreateJoinAttemptResult{}, err
	}
	if space.Version != input.ExpectedSpaceVersion {
		return CreateJoinAttemptResult{}, ErrJoinAttemptStale
	}
	if space.Status != SpaceStatusOpen {
		return CreateJoinAttemptResult{}, ErrRoomNotOpen
	}
	if space.Locked {
		return CreateJoinAttemptResult{}, ErrRoomLocked
	}
	if !sourceAvailableForJoin(source) {
		return CreateJoinAttemptResult{}, ErrRoomNotOpen
	}
	room, err := loadActiveRoom(queryContext, transaction, scope.TenantID, space.ID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return CreateJoinAttemptResult{}, ErrRoomNotOpen
	}
	if err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"lock media join-attempt room", err,
		)
	}
	if room.ID != input.ExpectedRoomInstanceID {
		return CreateJoinAttemptResult{}, ErrJoinAttemptStale
	}
	if room.Status != RoomInstanceActive || !room.ProviderRoomSID.Valid ||
		room.ProviderRoomName == "" {
		return CreateJoinAttemptResult{}, ErrRoomNotOpen
	}
	source, err = resolveEffectiveCredentialSource(
		queryContext, transaction, scope.TenantID, room.ID, scope.ActorID, source,
	)
	if err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"resolve media join-attempt role", err,
		)
	}
	now = now.UTC()
	fingerprint := joinAttemptRequestFingerprint(spaceID, input)
	existing, found, err := loadParticipantByAttempt(
		queryContext, transaction, scope.TenantID, room.ID, scope.ActorID, input.JoinAttemptID,
	)
	if err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"load media join-attempt retry", err,
		)
	}
	if found {
		if existing.Status != string(JoinAttemptWaiting) &&
			existing.Status != string(JoinAttemptAdmitted) &&
			existing.Status != string(JoinAttemptJoining) {
			return CreateJoinAttemptResult{}, ErrParticipantConflict
		}
		if existing.InstanceRole != source.InstanceRole {
			return CreateJoinAttemptResult{}, ErrParticipantConflict
		}
		if existing.Status == string(JoinAttemptWaiting) {
			if err := validateWaitingAdmission(
				queryContext, transaction, scope.TenantID, space.ID, room.ID,
				scope.ActorID, existing, fingerprint,
			); err != nil {
				return CreateJoinAttemptResult{}, err
			}
		}
		if err := transaction.Commit(queryContext); err != nil {
			return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
				"commit media join-attempt retry", err,
			)
		}
		return CreateJoinAttemptResult{
			Attempt: projectJoinAttempt(existing, room, source),
		}, nil
	}

	if occupied, err := hasActiveParticipant(
		queryContext, transaction, scope.TenantID, room.ID, scope.ActorID,
	); err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"check active media participant", err,
		)
	} else if occupied {
		return CreateJoinAttemptResult{}, ErrParticipantConflict
	}
	if blocked, err := hasUnrestoredRemovalBarrier(
		queryContext, transaction, scope.TenantID, space.ID, room.ID, scope.ActorID,
	); err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"check media participant restore barrier", err,
		)
	} else if blocked {
		return CreateJoinAttemptResult{}, ErrParticipantConflict
	}
	if err := acquireLifecycleTransactionLock(
		queryContext, transaction, "media-participant-capacity:"+scope.TenantID.String(),
	); err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"lock media participant capacity", err,
		)
	}
	if err := repository.requireParticipantCapacity(
		queryContext, transaction, scope.TenantID, space.ID,
	); err != nil {
		return CreateJoinAttemptResult{}, err
	}

	participant := participantRow{
		ID:               repository.newID(),
		ParticipantKey:   repository.newID(),
		JoinAttemptID:    input.JoinAttemptID,
		ProviderIdentity: opaqueParticipantIdentity(repository.newID()),
		InstanceRole:     source.InstanceRole,
		Status:           string(JoinAttemptAdmitted),
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if participant.ID == uuid.Nil || participant.ParticipantKey == uuid.Nil ||
		!validProviderIdentifier(participant.ProviderIdentity, 128) {
		return CreateJoinAttemptResult{}, ErrLifecycleUnavailable
	}
	if err := transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.media_room_instances
SET next_roster_sequence = next_roster_sequence + 1
WHERE tenant_id = $1 AND space_id = $2 AND id = $3
RETURNING next_roster_sequence`,
		scope.TenantID, space.ID, room.ID,
	).Scan(&participant.RosterSequence); err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"allocate media roster sequence", err,
		)
	}
	if space.LobbyEnabled && source.InstanceRole == InstanceRoleAttendee {
		admissionID := repository.newID()
		if admissionID == uuid.Nil {
			return CreateJoinAttemptResult{}, ErrLifecycleUnavailable
		}
		if _, err := transaction.Exec(
			queryContext,
			`INSERT INTO tutorhub.media_admission_requests (
    id, tenant_id, space_id, room_instance_id, user_id, status, version,
    idempotency_key, request_fingerprint, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, 'waiting', 1, $6, $7, $8, $8)`,
			admissionID, scope.TenantID, space.ID, room.ID, scope.ActorID,
			input.JoinAttemptID.String(), fingerprint[:], now,
		); err != nil {
			return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
				"insert media admission request", err,
			)
		}
		participant.AdmissionRequestID = uuid.NullUUID{UUID: admissionID, Valid: true}
		participant.AdmissionVersion = sql.NullInt64{Int64: 1, Valid: true}
		participant.AdmissionCreatedAt = sql.NullTime{Time: now, Valid: true}
		participant.Status = string(JoinAttemptWaiting)
	} else {
		participant.AdmittedAt = sql.NullTime{Time: now, Valid: true}
	}
	var admissionRequestID any
	if participant.AdmissionRequestID.Valid {
		admissionRequestID = participant.AdmissionRequestID.UUID
	}
	var admittedAt any
	if participant.AdmittedAt.Valid {
		admittedAt = participant.AdmittedAt.Time
	}
	if _, err := transaction.Exec(
		queryContext,
		`INSERT INTO tutorhub.media_participant_sessions (
    id, tenant_id, space_id, room_instance_id, user_id, admission_request_id,
    join_attempt_id, provider_participant_identity, instance_role, status,
    capacity_reserved, version, admitted_at, participant_key, roster_sequence,
    created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, true, 1, $11, $12, $13, $14, $14)`,
		participant.ID, scope.TenantID, space.ID, room.ID, scope.ActorID,
		admissionRequestID, participant.JoinAttemptID, participant.ProviderIdentity,
		participant.InstanceRole, participant.Status, admittedAt,
		participant.ParticipantKey, participant.RosterSequence, now,
	); err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"insert media participant join attempt", err,
		)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return CreateJoinAttemptResult{}, repository.lifecycle.unavailable(
			"commit media join attempt", err,
		)
	}
	return CreateJoinAttemptResult{
		Attempt: projectJoinAttempt(participant, room, source),
		Created: true,
	}, nil
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
	ID                 uuid.UUID
	ParticipantKey     uuid.UUID
	RosterSequence     int64
	AdmissionRequestID uuid.NullUUID
	AdmissionVersion   sql.NullInt64
	AdmissionCreatedAt sql.NullTime
	JoinAttemptID      uuid.UUID
	ProviderIdentity   string
	InstanceRole       InstanceRole
	Status             string
	Version            int64
	AdmittedAt         sql.NullTime
	JoiningAt          sql.NullTime
	CreatedAt          time.Time
	UpdatedAt          time.Time
	ConnectedAt        sql.NullTime
	ReconnectingAt     sql.NullTime
	TerminalAt         sql.NullTime
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
		`SELECT participant.id, participant.participant_key, participant.roster_sequence,
       participant.admission_request_id, participant.join_attempt_id,
       participant.provider_participant_identity, participant.instance_role,
       participant.status, participant.version, participant.admitted_at,
       participant.joining_at, participant.created_at, participant.updated_at,
       participant.connected_at, participant.reconnecting_at, participant.terminal_at,
       admission.version, admission.created_at
FROM tutorhub.media_participant_sessions AS participant
LEFT JOIN tutorhub.media_admission_requests AS admission
  ON admission.tenant_id = participant.tenant_id
 AND admission.space_id = participant.space_id
 AND admission.room_instance_id = participant.room_instance_id
 AND admission.id = participant.admission_request_id
WHERE participant.tenant_id = $1 AND participant.room_instance_id = $2
  AND participant.user_id = $3 AND participant.join_attempt_id = $4
FOR UPDATE OF participant`,
		tenantID, roomInstanceID, userID, joinAttemptID,
	).Scan(
		&participant.ID, &participant.ParticipantKey, &participant.RosterSequence,
		&participant.AdmissionRequestID, &participant.JoinAttemptID,
		&participant.ProviderIdentity, &participant.InstanceRole, &participant.Status,
		&participant.Version, &participant.AdmittedAt, &participant.JoiningAt,
		&participant.CreatedAt, &participant.UpdatedAt,
		&participant.ConnectedAt, &participant.ReconnectingAt, &participant.TerminalAt,
		&participant.AdmissionVersion, &participant.AdmissionCreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return participantRow{}, false, nil
	}
	return participant, err == nil, err
}

func validateWaitingAdmission(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomInstanceID uuid.UUID,
	userID uuid.UUID,
	participant participantRow,
	expectedFingerprint [sha256.Size]byte,
) error {
	if !participant.AdmissionRequestID.Valid {
		return ErrParticipantConflict
	}
	var status, idempotencyKey string
	var version int64
	var fingerprint []byte
	err := transaction.QueryRow(
		ctx,
		`SELECT status, version, idempotency_key, request_fingerprint
FROM tutorhub.media_admission_requests
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
	AND user_id = $4 AND id = $5`,
		tenantID, spaceID, roomInstanceID, userID, participant.AdmissionRequestID.UUID,
	).Scan(&status, &version, &idempotencyKey, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrParticipantConflict
	}
	if err != nil {
		return fmt.Errorf("validate media admission request: %w", err)
	}
	if status != string(JoinAttemptWaiting) || version < 1 ||
		idempotencyKey != participant.JoinAttemptID.String() ||
		!bytes.Equal(fingerprint, expectedFingerprint[:]) {
		return ErrParticipantConflict
	}
	return nil
}

func requireAdmittedAdmission(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomInstanceID uuid.UUID,
	userID uuid.UUID,
	participant participantRow,
) error {
	if !participant.AdmissionRequestID.Valid {
		return ErrAdmissionRequired
	}
	var status string
	err := transaction.QueryRow(
		ctx,
		`SELECT status
FROM tutorhub.media_admission_requests
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND user_id = $4 AND id = $5`,
		tenantID, spaceID, roomInstanceID, userID, participant.AdmissionRequestID.UUID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAdmissionRequired
	}
	if err != nil {
		return fmt.Errorf("validate admitted media admission request: %w", err)
	}
	if status != string(JoinAttemptAdmitted) {
		return ErrAdmissionRequired
	}
	return nil
}

func concealJoinSourceError(err error) error {
	if errors.Is(err, ErrSpaceAccessDenied) {
		return ErrSpaceNotFound
	}
	return err
}

func joinAttemptRequestFingerprint(
	spaceID uuid.UUID,
	input CreateJoinAttemptInput,
) [sha256.Size]byte {
	return sha256.Sum256([]byte(fmt.Sprintf(
		"media.join_attempt.v1\x00%s\x00%s\x00%s\x00%d",
		spaceID, input.ExpectedRoomInstanceID, input.JoinAttemptID, input.ExpectedSpaceVersion,
	)))
}

func projectJoinAttempt(
	participant participantRow,
	room roomRow,
	source authorizedSource,
) JoinAttempt {
	var admissionRequestID *uuid.UUID
	var admissionVersion *int64
	var expiresAt *time.Time
	if participant.AdmissionRequestID.Valid {
		value := participant.AdmissionRequestID.UUID
		admissionRequestID = &value
	}
	if participant.AdmissionVersion.Valid {
		value := participant.AdmissionVersion.Int64
		admissionVersion = &value
	}
	if participant.AdmissionCreatedAt.Valid {
		value := participant.AdmissionCreatedAt.Time.UTC().Add(defaultLobbyAdmissionTTL)
		expiresAt = &value
	}
	return JoinAttempt{
		ParticipantSessionID:       participant.ID,
		RoomInstanceID:             room.ID,
		AdmissionRequestID:         admissionRequestID,
		AdmissionVersion:           admissionVersion,
		JoinAttemptID:              participant.JoinAttemptID,
		Status:                     JoinAttemptStatus(participant.Status),
		Version:                    participant.Version,
		InstanceRole:               participant.InstanceRole,
		CanPublishCameraMicrophone: source.CanPublishCameraMicrophone,
		CanShareScreen:             source.CanShareScreen,
		CanSubscribe:               true,
		CreatedAt:                  participant.CreatedAt.UTC(),
		UpdatedAt:                  participant.UpdatedAt.UTC(),
		ExpiresAt:                  expiresAt,
	}
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

func hasUnrestoredRemovalBarrier(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomInstanceID uuid.UUID,
	userID uuid.UUID,
) (bool, error) {
	var blocked bool
	err := transaction.QueryRow(
		ctx,
		`SELECT EXISTS (
    SELECT 1 FROM tutorhub.media_participant_sessions
    WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
      AND user_id = $4 AND status = 'removed'
      AND rejoin_restored_at IS NULL
)`,
		tenantID, spaceID, roomInstanceID, userID,
	).Scan(&blocked)
	return blocked, err
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
	if !source.CanJoin {
		return false
	}
	switch source.Kind {
	case SourceClassSession:
		return source.Status == "live"
	case SourceClassSessionOccurrence, SourceStudyMeeting:
		return source.Status == "scheduled"
	default:
		return false
	}
}

func resolveEffectiveCredentialSource(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	roomID uuid.UUID,
	userID uuid.UUID,
	source authorizedSource,
) (authorizedSource, error) {
	role, err := effectiveRoomRole(
		ctx, transaction, tenantID, roomID, userID, source.InstanceRole, true,
	)
	if err != nil {
		return authorizedSource{}, err
	}
	source.InstanceRole = role
	if role == InstanceRoleHost || role == InstanceRoleCoHost ||
		role == InstanceRoleTeachingAssistant {
		source.CanShareScreen = true
	}
	return source, nil
}

func participantGrant(
	access AccessContext,
	room roomRow,
	participant participantRow,
	source authorizedSource,
) ParticipantCredentialGrant {
	return ParticipantCredentialGrant{
		ParticipantSessionID:        participant.ID,
		ParticipantKey:              participant.ParticipantKey,
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
SET status = 'failed', version = version + 1, failed_at = $4,
    failure_code = 'provider_room_finished', updated_at = $4
WHERE tenant_id = $1 AND space_id = $2 AND id = $3 AND status = 'active'`,
					binding.TenantID, binding.SpaceID, binding.RoomInstanceID, transitionAt,
				)
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return ErrSpaceTransition
				}
				if err := expireOutstandingLobbyAdmissions(
					ctx,
					transaction,
					binding.TenantID,
					binding.SpaceID,
					binding.RoomInstanceID,
					nil,
					"provider_unavailable",
					transitionAt,
				); err != nil {
					return err
				}
				return failRoomParticipants(
					ctx,
					transaction,
					binding.TenantID,
					binding.SpaceID,
					binding.RoomInstanceID,
					"provider_room_finished",
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
				if err := expireOutstandingLobbyAdmissions(
					ctx,
					transaction,
					binding.TenantID,
					binding.SpaceID,
					binding.RoomInstanceID,
					nil,
					"provider_unavailable",
					transitionAt,
				); err != nil {
					return err
				}
				return failRoomParticipants(
					ctx,
					transaction,
					binding.TenantID,
					binding.SpaceID,
					binding.RoomInstanceID,
					"provider_room_finished",
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
			return advanceMediaRosterProjection(
				ctx, transaction, binding.TenantID, binding.SpaceID, binding.RoomInstanceID,
			)
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
		if _, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.media_participant_hand_states
SET is_raised = false
WHERE tenant_id = $1 AND room_instance_id = $2 AND participant_session_id = $3
  AND is_raised`,
			binding.TenantID, binding.RoomInstanceID, participant.ID,
		); err != nil {
			return err
		}
		return advanceMediaRosterProjection(
			ctx, transaction, binding.TenantID, binding.SpaceID, binding.RoomInstanceID,
		)
	}
}

func advanceMediaRosterProjection(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomInstanceID uuid.UUID,
) error {
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_room_instances
SET projection_version = projection_version + 1
WHERE tenant_id = $1 AND space_id = $2 AND id = $3`,
		tenantID, spaceID, roomInstanceID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrSpaceTransition
	}
	return nil
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
