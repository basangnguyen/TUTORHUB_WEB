package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/audit"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

// PostgresModerationRepository persists TutorHub authority before any provider
// effect is attempted. All public target references are opaque participant keys.
type PostgresModerationRepository struct {
	lifecycle *PostgresLifecycleRepository
}

func NewPostgresModerationRepository(
	lifecycle *PostgresLifecycleRepository,
) (*PostgresModerationRepository, error) {
	if lifecycle == nil || lifecycle.database == nil || lifecycle.controls == nil {
		return nil, fmt.Errorf("media lifecycle repository is required")
	}
	return &PostgresModerationRepository{lifecycle: lifecycle}, nil
}

type moderationTargetRow struct {
	SessionID        uuid.UUID
	ParticipantKey   uuid.UUID
	UserID           uuid.UUID
	ProviderIdentity string
	InstanceRole     InstanceRole
	Status           string
	Version          int64
}

func (repository *PostgresModerationRepository) ApplyModeration(
	ctx context.Context,
	access AccessContext,
	command ModerationCommand,
) (ModerationResult, error) {
	if repository == nil || repository.lifecycle == nil {
		return ModerationResult{}, ErrModerationUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return ModerationResult{}, ErrModerationUnavailable
	}
	defer rollbackLifecycle(transaction)

	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return ModerationResult{}, err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(
		queryContext, transaction, access,
	)
	if err != nil {
		return ModerationResult{}, ErrModerationForbidden
	}
	space, err := loadSpace(queryContext, transaction, scope.TenantID, command.SpaceID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationResult{}, ErrModerationNotFound
	}
	if err != nil {
		return ModerationResult{}, ErrModerationUnavailable
	}
	source, err := repository.lifecycle.authorizeSource(
		queryContext, transaction, access, scope, space,
		policy.ActionSessionJoin, false, false,
	)
	if err != nil {
		return ModerationResult{}, concealModerationSourceError(err)
	}
	if err := repository.lifecycle.controls.RequireFeature(
		queryContext, transaction, scope.TenantID,
		featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return ModerationResult{}, err
	}

	// Replay is intentionally evaluated before current-version/terminal checks.
	// The same committed command remains replayable after it changed those values.
	replayed, found, err := loadModerationReplay(
		queryContext, transaction, scope.TenantID, scope.ActorID, command,
	)
	if err != nil {
		return ModerationResult{}, err
	}
	if found {
		if err := transaction.Commit(queryContext); err != nil {
			return ModerationResult{}, ErrModerationUnavailable
		}
		return replayed, nil
	}
	if space.Status != SpaceStatusOpen {
		return ModerationResult{}, ErrRoomNotOpen
	}
	room, err := loadSignalRoom(
		queryContext, transaction, scope.TenantID, space.ID,
		command.Expected.RoomInstanceID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationResult{}, ErrModerationNotFound
	}
	if err != nil || room.Status != RoomInstanceActive {
		return ModerationResult{}, ErrRoomNotOpen
	}
	if space.Version != command.Expected.SpaceVersion ||
		room.Version != command.Expected.RoomVersion ||
		room.ProjectionVersion != command.Expected.ProjectionVersion {
		return ModerationResult{}, ErrModerationConflict
	}
	actor, err := loadSignalParticipantByActor(
		queryContext, transaction, scope.TenantID, space.ID, room.ID,
		scope.ActorID, true,
	)
	actorFound := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ModerationResult{}, ErrModerationUnavailable
	}
	actorRole := InstanceRole("")
	if actorFound {
		actorRole, err = effectiveRoomRole(
			queryContext, transaction, scope.TenantID, room.ID, scope.ActorID,
			actor.InstanceRole, true,
		)
		if err != nil {
			return ModerationResult{}, ErrModerationUnavailable
		}
	}
	normalAuthority, safetyAction := moderationAuthority(
		command.Operation, actorRole, actorFound, source.SafetyAdmin,
	)
	if !normalAuthority && !safetyAction {
		if !actorFound {
			return ModerationResult{}, ErrModerationNotFound
		}
		return ModerationResult{}, ErrModerationForbidden
	}
	if safetyAction && command.ReasonCode == "" {
		return ModerationResult{}, ErrModerationForbidden
	}
	if err := repository.authorizeModerationOperation(
		queryContext, transaction, access, scope, space, source, actorRole,
		command.Operation, safetyAction,
	); err != nil {
		return ModerationResult{}, err
	}
	if err := consumeModerationRateLimit(
		queryContext, transaction, scope.TenantID, room.ID, scope.ActorID,
		command.Operation,
	); err != nil {
		return ModerationResult{}, err
	}

	result := ModerationResult{
		SpaceID: space.ID, RoomInstanceID: room.ID,
		SpaceVersion: space.Version, RoomInstanceVersion: room.Version,
		ProjectionVersion:    room.ProjectionVersion,
		ProviderEffectStatus: ProviderEffectNone,
	}
	var target moderationTargetRow
	if command.Operation == ModerationLock || command.Operation == ModerationUnlock {
		locked := command.Operation == ModerationLock
		if space.Locked != locked {
			if err := transaction.QueryRow(
				queryContext,
				`UPDATE tutorhub.media_spaces
SET locked = $4, version = version + 1, updated_by = $5, updated_at = $6
WHERE tenant_id = $1 AND id = $2 AND version = $3 AND status = 'open'
RETURNING version`,
				scope.TenantID, space.ID, space.Version, locked,
				scope.ActorID, command.OccurredAt,
			).Scan(&result.SpaceVersion); err != nil {
				return ModerationResult{}, ErrModerationConflict
			}
		}
		result.Locked = &locked
	} else {
		target, err = loadModerationTarget(
			queryContext, transaction, scope.TenantID, space.ID, room.ID,
			command.ParticipantKey,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ModerationResult{}, ErrModerationNotFound
		}
		if err != nil {
			return ModerationResult{}, ErrModerationUnavailable
		}
		targetRole, err := effectiveRoomRole(
			queryContext, transaction, scope.TenantID, room.ID, target.UserID,
			target.InstanceRole, true,
		)
		if err != nil {
			return ModerationResult{}, ErrModerationUnavailable
		}
		if target.UserID == scope.ActorID || targetRole == InstanceRoleHost ||
			(!safetyAction && !moderationTargetAllowed(command.Operation, actorRole, targetRole)) {
			return ModerationResult{}, ErrModerationForbidden
		}
		target.InstanceRole = targetRole
		result.TargetParticipantKey = &target.ParticipantKey
		if err := applyParticipantModeration(
			queryContext, transaction, scope.TenantID, scope.ActorID,
			command, room, target, &result,
		); err != nil {
			return ModerationResult{}, err
		}
	}

	if err := insertModerationReceipt(
		queryContext, transaction, scope.TenantID, scope.ActorID,
		command, room, target, result,
	); err != nil {
		return ModerationResult{}, ErrModerationUnavailable
	}
	if err := appendModerationEvent(
		queryContext, transaction, scope.TenantID, scope.ActorID,
		command, target, result, safetyAction,
	); err != nil {
		return ModerationResult{}, ErrModerationUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ModerationResult{}, ErrModerationUnavailable
	}
	return result, nil
}

const moderationRateWindow = time.Minute

const (
	moderationLockRatePurpose   = "media.moderation.lock"
	moderationRoleRatePurpose   = "media.moderation.role"
	moderationMuteRatePurpose   = "media.moderation.mute"
	moderationRemoveRatePurpose = "media.moderation.remove"
	moderationLockRateLimit     = int64(12)
	moderationRoleRateLimit     = int64(30)
	moderationMuteRateLimit     = int64(60)
	moderationRemoveRateLimit   = int64(30)
)

type moderationRatePolicy struct {
	purpose string
	limit   int64
}

func moderationRatePolicyForOperation(operation ModerationOperation) (moderationRatePolicy, bool) {
	switch operation {
	case ModerationLock, ModerationUnlock:
		return moderationRatePolicy{purpose: moderationLockRatePurpose, limit: moderationLockRateLimit}, true
	case ModerationPromote, ModerationDemote:
		return moderationRatePolicy{purpose: moderationRoleRatePurpose, limit: moderationRoleRateLimit}, true
	case ModerationMute:
		return moderationRatePolicy{purpose: moderationMuteRatePurpose, limit: moderationMuteRateLimit}, true
	case ModerationRemove:
		return moderationRatePolicy{purpose: moderationRemoveRatePurpose, limit: moderationRemoveRateLimit}, true
	default:
		return moderationRatePolicy{}, false
	}
}

func moderationRateBucketHash(
	policy moderationRatePolicy,
	tenantID uuid.UUID,
	roomID uuid.UUID,
	actorID uuid.UUID,
) [sha256.Size]byte {
	return sha256.Sum256([]byte(
		policy.purpose + "\x00" + tenantID.String() + "\x00" +
			roomID.String() + "\x00" + actorID.String(),
	))
}

func consumeModerationRateLimit(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	roomID uuid.UUID,
	actorID uuid.UUID,
	operation ModerationOperation,
) error {
	policy, ok := moderationRatePolicyForOperation(operation)
	if !ok {
		return ErrInvalidModerationRequest
	}
	var acceptedAt time.Time
	if err := transaction.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&acceptedAt); err != nil {
		return ErrModerationUnavailable
	}
	acceptedAt = acceptedAt.UTC()
	windowStartedAt := acceptedAt.Truncate(moderationRateWindow)
	windowEndsAt := windowStartedAt.Add(moderationRateWindow)
	bucketHash := moderationRateBucketHash(policy, tenantID, roomID, actorID)
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
		policy.purpose, bucketHash[:], windowStartedAt, windowEndsAt, acceptedAt, policy.limit,
	).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		retryAfter := windowEndsAt.Sub(acceptedAt)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return &ModerationRateLimitError{RetryAfter: retryAfter}
	}
	if err != nil {
		return ErrModerationUnavailable
	}
	return nil
}

func applyParticipantModeration(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	command ModerationCommand,
	room signalRoomRow,
	target moderationTargetRow,
	result *ModerationResult,
) error {
	result.ProviderEffectStatus = ProviderEffectPending
	switch command.Operation {
	case ModerationPromote, ModerationDemote:
		if err := mutateDynamicCoHost(
			ctx, transaction, tenantID, actorID, command, room.ID, target, result,
		); err != nil {
			return err
		}
	case ModerationMute:
		version := target.Version
		result.TargetParticipantVersion = &version
	case ModerationRemove:
		var participantVersion int64
		if err := transaction.QueryRow(
			ctx,
			`UPDATE tutorhub.media_participant_sessions
SET status = 'removed', capacity_reserved = false, version = version + 1,
    terminal_at = $6, removed_by = $5, reconnecting_at = NULL,
    failure_code = NULL, updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3 AND id = $4
  AND status IN ('joining', 'connected', 'reconnecting')
RETURNING version`,
			tenantID, command.SpaceID, room.ID, target.SessionID, actorID,
			command.OccurredAt,
		).Scan(&participantVersion); err != nil {
			return ErrModerationConflict
		}
		if _, err := transaction.Exec(
			ctx,
			`UPDATE tutorhub.media_participant_hand_states
SET is_raised = false
WHERE tenant_id = $1 AND room_instance_id = $2 AND participant_session_id = $3
  AND is_raised`,
			tenantID, room.ID, target.SessionID,
		); err != nil {
			return ErrModerationUnavailable
		}
		var roomVersion, projectionVersion int64
		if err := transaction.QueryRow(
			ctx,
			`UPDATE tutorhub.media_room_instances
SET version = version + 1, projection_version = projection_version + 1,
    updated_by = $4, updated_at = $5
WHERE tenant_id = $1 AND space_id = $2 AND id = $3 AND status = 'active'
RETURNING version, projection_version`,
			tenantID, command.SpaceID, room.ID, actorID, command.OccurredAt,
		).Scan(&roomVersion, &projectionVersion); err != nil {
			return ErrModerationConflict
		}
		result.TargetParticipantVersion = &participantVersion
		result.RoomInstanceVersion = roomVersion
		result.ProjectionVersion = projectionVersion
	default:
		return ErrInvalidModerationRequest
	}
	return nil
}

func mutateDynamicCoHost(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	command ModerationCommand,
	roomID uuid.UUID,
	target moderationTargetRow,
	result *ModerationResult,
) error {
	var assignmentVersion int64
	if command.Operation == ModerationPromote {
		err := transaction.QueryRow(
			ctx,
			`INSERT INTO tutorhub.media_room_role_assignments (
    tenant_id, space_id, room_instance_id, user_id, assigned_role, status,
    version, assigned_by, assigned_at, reason_code, updated_at
)
VALUES ($1, $2, $3, $4, 'co_host', 'active', 1, $5, $6, NULLIF($7, ''), $6)
ON CONFLICT (tenant_id, room_instance_id, user_id) DO UPDATE
SET status = 'active', version = tutorhub.media_room_role_assignments.version + 1,
    assigned_by = EXCLUDED.assigned_by, assigned_at = EXCLUDED.assigned_at,
    revoked_by = NULL, revoked_at = NULL, reason_code = EXCLUDED.reason_code,
    updated_at = EXCLUDED.updated_at
WHERE tutorhub.media_room_role_assignments.status = 'revoked'
RETURNING version`,
			tenantID, command.SpaceID, roomID, target.UserID, actorID,
			command.OccurredAt, command.ReasonCode,
		).Scan(&assignmentVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrModerationConflict
		}
		if err != nil {
			return ErrModerationUnavailable
		}
	} else {
		if err := transaction.QueryRow(
			ctx,
			`UPDATE tutorhub.media_room_role_assignments
SET status = 'revoked', version = version + 1, revoked_by = $5,
    revoked_at = $6, reason_code = NULLIF($7, ''), updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3 AND user_id = $4
  AND assigned_role = 'co_host' AND status = 'active'
RETURNING version`,
			tenantID, command.SpaceID, roomID, target.UserID, actorID,
			command.OccurredAt, command.ReasonCode,
		).Scan(&assignmentVersion); err != nil {
			return ErrModerationConflict
		}
	}
	var participantVersion, roomVersion, projectionVersion int64
	if err := transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.media_participant_sessions
SET instance_role = $5, version = version + 1, updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3 AND id = $4
  AND status IN ('joining', 'connected', 'reconnecting')
RETURNING version`,
		tenantID, command.SpaceID, roomID, target.SessionID,
		command.DesiredRole, command.OccurredAt,
	).Scan(&participantVersion); err != nil {
		return ErrModerationConflict
	}
	if err := transaction.QueryRow(
		ctx,
		`UPDATE tutorhub.media_room_instances
SET version = version + 1, projection_version = projection_version + 1,
    updated_by = $4, updated_at = $5
WHERE tenant_id = $1 AND space_id = $2 AND id = $3 AND status = 'active'
RETURNING version, projection_version`,
		tenantID, command.SpaceID, roomID, actorID, command.OccurredAt,
	).Scan(&roomVersion, &projectionVersion); err != nil {
		return ErrModerationConflict
	}
	result.TargetParticipantVersion = &participantVersion
	result.TargetInstanceRole = &command.DesiredRole
	result.RoomInstanceVersion = roomVersion
	result.ProjectionVersion = projectionVersion
	result.roleAssignmentVersion = assignmentVersion
	return nil
}

func loadModerationTarget(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, spaceID, roomID, participantKey uuid.UUID,
) (moderationTargetRow, error) {
	var target moderationTargetRow
	err := transaction.QueryRow(
		ctx,
		`SELECT id, participant_key, user_id, provider_participant_identity,
       instance_role, status, version
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND participant_key = $4
  AND status IN ('joining', 'connected', 'reconnecting')
FOR UPDATE`,
		tenantID, spaceID, roomID, participantKey,
	).Scan(
		&target.SessionID, &target.ParticipantKey, &target.UserID,
		&target.ProviderIdentity, &target.InstanceRole, &target.Status,
		&target.Version,
	)
	return target, err
}

// effectiveRoomRole is the single dynamic RoomInstance-role resolver used by
// moderation and by later credential/signal projection hardening.
func effectiveRoomRole(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, roomID, userID uuid.UUID,
	base InstanceRole,
	lock bool,
) (InstanceRole, error) {
	if base == InstanceRoleHost || base == InstanceRoleTeachingAssistant {
		return base, nil
	}
	query := `SELECT assigned_role
FROM tutorhub.media_room_role_assignments
WHERE tenant_id = $1 AND room_instance_id = $2 AND user_id = $3
  AND status = 'active'`
	if lock {
		query += " FOR UPDATE"
	}
	var assigned InstanceRole
	err := transaction.QueryRow(ctx, query, tenantID, roomID, userID).Scan(&assigned)
	if errors.Is(err, pgx.ErrNoRows) {
		return base, nil
	}
	if err != nil {
		return "", err
	}
	return assigned, nil
}

func (repository *PostgresModerationRepository) authorizeModerationOperation(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	scope tenancy.Context,
	space spaceRow,
	source authorizedSource,
	actorRole InstanceRole,
	operation ModerationOperation,
	safetyAction bool,
) error {
	// A dynamic co-host assignment is itself the RoomInstance-scoped authority;
	// it deliberately does not grant a durable class/organization permission.
	if actorRole == InstanceRoleCoHost && source.InstanceRole != actorRole && !safetyAction {
		return nil
	}
	action := policy.ActionMediaModerate
	switch operation {
	case ModerationLock, ModerationUnlock:
		action = policy.ActionMediaLock
	case ModerationRemove:
		action = policy.ActionParticipantRemove
	case ModerationPromote, ModerationDemote, ModerationMute:
		// Keep the media.moderate policy boundary explicit.
	default:
		return ErrInvalidModerationRequest
	}
	if _, err := repository.lifecycle.authorizeSource(
		ctx, transaction, access, scope, space, action, true, safetyAction,
	); err != nil {
		return concealModerationSourceError(err)
	}
	return nil
}

func moderationActorAllowed(operation ModerationOperation, role InstanceRole) bool {
	switch operation {
	case ModerationLock, ModerationUnlock, ModerationPromote, ModerationDemote:
		return role == InstanceRoleHost
	case ModerationRemove:
		return role == InstanceRoleHost || role == InstanceRoleCoHost
	case ModerationMute:
		return role == InstanceRoleHost || role == InstanceRoleCoHost ||
			role == InstanceRoleTeachingAssistant
	default:
		return false
	}
}

func moderationAuthority(
	operation ModerationOperation,
	actorRole InstanceRole,
	actorFound bool,
	safetyAdmin bool,
) (normal bool, safety bool) {
	if operation == ModerationRemove && safetyAdmin {
		return false, true
	}
	normal = actorFound && moderationActorAllowed(operation, actorRole)
	return normal, safety
}

func moderationTargetAllowed(
	operation ModerationOperation,
	actorRole, targetRole InstanceRole,
) bool {
	if targetRole == InstanceRoleHost {
		return false
	}
	if operation == ModerationPromote {
		return actorRole == InstanceRoleHost && targetRole == InstanceRoleAttendee
	}
	if operation == ModerationDemote {
		return actorRole == InstanceRoleHost && targetRole == InstanceRoleCoHost
	}
	if actorRole == InstanceRoleHost {
		return true
	}
	return targetRole == InstanceRoleAttendee
}

func loadModerationReplay(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, actorID uuid.UUID,
	command ModerationCommand,
) (ModerationResult, bool, error) {
	var fingerprint []byte
	var operation ModerationOperation
	var result ModerationResult
	var spaceID, roomID uuid.NullUUID
	var spaceVersion, roomVersion, projectionVersion sql.NullInt64
	var targetKey uuid.NullUUID
	var targetVersion sql.NullInt64
	var targetRole sql.NullString
	var locked sql.NullBool
	err := transaction.QueryRow(
		ctx,
		`SELECT receipt.request_fingerprint, receipt.operation, receipt.space_id,
       receipt.result_room_instance_id, receipt.result_space_version,
       receipt.result_room_instance_version, receipt.result_projection_version,
       target.participant_key, receipt.result_participant_version,
       receipt.result_instance_role, receipt.result_locked,
       receipt.provider_effect_status
FROM tutorhub.media_space_mutation_receipts AS receipt
LEFT JOIN tutorhub.media_participant_sessions AS target
  ON target.tenant_id = receipt.tenant_id
 AND target.space_id = receipt.space_id
 AND target.room_instance_id = receipt.result_room_instance_id
 AND target.id = receipt.target_participant_session_id
WHERE receipt.tenant_id = $1 AND receipt.actor_user_id = $2
  AND receipt.idempotency_key = $3`,
		tenantID, actorID, command.IdempotencyKey,
	).Scan(
		&fingerprint, &operation, &spaceID, &roomID,
		&spaceVersion, &roomVersion, &projectionVersion,
		&targetKey, &targetVersion, &targetRole,
		&locked, &result.ProviderEffectStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationResult{}, false, nil
	}
	if err != nil {
		return ModerationResult{}, false, ErrModerationUnavailable
	}
	if operation != command.Operation || !bytes.Equal(fingerprint, command.Fingerprint[:]) {
		return ModerationResult{}, false, ErrModerationIdempotency
	}
	if !spaceID.Valid || !roomID.Valid || !spaceVersion.Valid || !roomVersion.Valid ||
		!projectionVersion.Valid {
		return ModerationResult{}, false, ErrModerationUnavailable
	}
	result.SpaceID, result.RoomInstanceID = spaceID.UUID, roomID.UUID
	result.SpaceVersion = spaceVersion.Int64
	result.RoomInstanceVersion = roomVersion.Int64
	result.ProjectionVersion = projectionVersion.Int64
	if targetKey.Valid {
		value := targetKey.UUID
		result.TargetParticipantKey = &value
	}
	if targetVersion.Valid {
		value := targetVersion.Int64
		result.TargetParticipantVersion = &value
	}
	if targetRole.Valid {
		value := InstanceRole(targetRole.String)
		result.TargetInstanceRole = &value
	}
	if locked.Valid {
		value := locked.Bool
		result.Locked = &value
	}
	return result, true, nil
}

func insertModerationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, actorID uuid.UUID,
	command ModerationCommand,
	room signalRoomRow,
	target moderationTargetRow,
	result ModerationResult,
) error {
	providerRequired := command.Operation == ModerationPromote ||
		command.Operation == ModerationDemote || command.Operation == ModerationMute ||
		command.Operation == ModerationRemove
	status := ProviderEffectNone
	var providerUpdatedAt any
	if providerRequired {
		status = ProviderEffectPending
		providerUpdatedAt = command.OccurredAt
	}
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.media_space_mutation_receipts (
    tenant_id, idempotency_key, request_fingerprint, operation, space_id,
    result_space_version, result_room_instance_id, actor_user_id, created_at,
    target_participant_session_id, result_room_instance_version,
    result_projection_version, result_participant_version,
    result_role_assignment_version, result_instance_role, result_locked,
    provider_effect_required, provider_effect_status, provider_effect_updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
        $14, $15, $16, $17, $18, $19)`,
		tenantID, command.IdempotencyKey, command.Fingerprint[:], command.Operation,
		command.SpaceID, result.SpaceVersion, room.ID, actorID, command.OccurredAt,
		nullableLifecycleUUID(target.SessionID), result.RoomInstanceVersion,
		result.ProjectionVersion, result.TargetParticipantVersion,
		nullableModerationInt64(result.roleAssignmentVersion),
		result.TargetInstanceRole, result.Locked, providerRequired, status,
		providerUpdatedAt,
	)
	return err
}

func appendModerationEvent(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, actorID uuid.UUID,
	command ModerationCommand,
	target moderationTargetRow,
	result ModerationResult,
	safetyAction bool,
) error {
	eventType, aggregateType, aggregateID := moderationEventIdentity(command, target)
	var targetKey any
	if target.ParticipantKey != uuid.Nil {
		targetKey = target.ParticipantKey
	}
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES ($1, $2, $3, $4,
    jsonb_strip_nulls(jsonb_build_object(
        'space_id', $5::uuid, 'room_instance_id', $6::uuid,
        'actor_user_id', $7::uuid, 'target_participant_key', $8::uuid,
        'action', $9::text, 'outcome', 'committed',
        'reason_code', NULLIF($10::text, ''),
        'safety_action', CASE WHEN $11::boolean THEN true ELSE NULL END
    )), $12, $12)`,
		tenantID, aggregateType, aggregateID, eventType, command.SpaceID,
		command.Expected.RoomInstanceID, actorID, targetKey, command.Operation,
		command.ReasonCode, safetyAction, command.OccurredAt,
	); err != nil {
		return err
	}
	metadata := audit.Metadata{
		"action": string(command.Operation), "outcome": "committed",
		"space_version": strconv.FormatInt(result.SpaceVersion, 10),
	}
	if target.ParticipantKey != uuid.Nil {
		metadata["target_participant_key"] = target.ParticipantKey.String()
	}
	if command.ReasonCode != "" {
		metadata["reason_code"] = command.ReasonCode
	}
	if safetyAction {
		metadata["safety_action"] = "true"
	}
	return audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: tenantID, ActorID: actorID, EventType: eventType,
		AggregateType: aggregateType, AggregateID: aggregateID,
		Metadata: metadata, OccurredAt: command.OccurredAt,
	})
}

func moderationEventIdentity(
	command ModerationCommand,
	target moderationTargetRow,
) (string, string, uuid.UUID) {
	switch command.Operation {
	case ModerationLock:
		return "media_space.locked.v1", "media_space", command.SpaceID
	case ModerationUnlock:
		return "media_space.unlocked.v1", "media_space", command.SpaceID
	case ModerationPromote:
		return "media_participant.promoted.v1", "media_participant", target.ParticipantKey
	case ModerationDemote:
		return "media_participant.demoted.v1", "media_participant", target.ParticipantKey
	case ModerationMute:
		return "media_participant.muted.v1", "media_participant", target.ParticipantKey
	default:
		return "media_participant.removed.v1", "media_participant", target.ParticipantKey
	}
}

func (repository *PostgresModerationRepository) ClaimProviderModerationEffect(
	ctx context.Context,
	access AccessContext,
	idempotencyKey string,
	now time.Time,
	lease time.Duration,
) (ProviderModerationEffect, ProviderEffectStatus, bool, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return ProviderModerationEffect{}, ProviderEffectRetryableFailed, false, ErrModerationUnavailable
	}
	defer rollbackLifecycle(transaction)
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return ProviderModerationEffect{}, ProviderEffectRetryableFailed, false, err
	}
	_, scope, err := repository.lifecycle.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return ProviderModerationEffect{}, ProviderEffectPermanentFailed, false, ErrModerationForbidden
	}
	var effect ProviderModerationEffect
	var currentStatus ProviderEffectStatus
	err = transaction.QueryRow(
		queryContext,
		`SELECT receipt.operation, room.provider_room_name,
       target.provider_participant_identity, receipt.provider_effect_attempts,
       receipt.provider_effect_status
FROM tutorhub.media_space_mutation_receipts AS receipt
JOIN tutorhub.media_room_instances AS room
  ON room.tenant_id = receipt.tenant_id
 AND room.space_id = receipt.space_id
 AND room.id = receipt.result_room_instance_id
JOIN tutorhub.media_participant_sessions AS target
  ON target.tenant_id = receipt.tenant_id
 AND target.space_id = receipt.space_id
 AND target.room_instance_id = receipt.result_room_instance_id
 AND target.id = receipt.target_participant_session_id
WHERE receipt.tenant_id = $1 AND receipt.actor_user_id = $2
  AND receipt.idempotency_key = $3 AND receipt.provider_effect_required
FOR UPDATE OF receipt`,
		scope.TenantID, scope.ActorID, idempotencyKey,
	).Scan(
		&effect.Operation, &effect.RoomName, &effect.ParticipantIdentity,
		&effect.Attempt, &currentStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderModerationEffect{}, ProviderEffectPermanentFailed, false, ErrModerationNotFound
	}
	if err != nil {
		return ProviderModerationEffect{}, ProviderEffectRetryableFailed, false, ErrModerationUnavailable
	}
	if currentStatus == ProviderEffectApplied || currentStatus == ProviderEffectPermanentFailed {
		return effect, publicProviderEffectStatus(currentStatus), false, nil
	}
	effect.Attempt++
	var claimed ProviderEffectStatus
	err = transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.media_space_mutation_receipts
SET provider_effect_status = 'applying', provider_effect_attempts = $4,
    provider_effect_error_code = NULL, provider_effect_lease_until = $5,
    provider_effect_updated_at = $6
WHERE tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3
  AND provider_effect_required
  AND (provider_effect_status IN ('pending', 'retryable_failed')
       OR (provider_effect_status = 'applying' AND provider_effect_lease_until <= $6))
RETURNING provider_effect_status`,
		scope.TenantID, scope.ActorID, idempotencyKey, effect.Attempt,
		now.Add(lease), now,
	).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := transaction.Commit(queryContext); err != nil {
			return effect, ProviderEffectRetryableFailed, false, ErrModerationUnavailable
		}
		return effect, publicProviderEffectStatus(currentStatus), false, nil
	}
	if err != nil {
		return ProviderModerationEffect{}, ProviderEffectRetryableFailed, false, ErrModerationUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ProviderModerationEffect{}, ProviderEffectRetryableFailed, false, ErrModerationUnavailable
	}
	return effect, publicProviderEffectStatus(claimed), true, nil
}

func (repository *PostgresModerationRepository) CompleteProviderModerationEffect(
	ctx context.Context,
	access AccessContext,
	idempotencyKey string,
	attempt int,
	status ProviderEffectStatus,
	errorCode string,
	now time.Time,
) (ProviderEffectStatus, error) {
	if status != ProviderEffectApplied && status != ProviderEffectRetryableFailed &&
		status != ProviderEffectPermanentFailed {
		return ProviderEffectPermanentFailed, ErrInvalidModerationRequest
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return ProviderEffectRetryableFailed, ErrModerationUnavailable
	}
	defer rollbackLifecycle(transaction)
	if err := repository.lifecycle.acquireTenantControlLock(
		queryContext, transaction, access.TenantID,
	); err != nil {
		return ProviderEffectRetryableFailed, err
	}
	_, scope, err := repository.lifecycle.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return ProviderEffectPermanentFailed, ErrModerationForbidden
	}
	var completed ProviderEffectStatus
	err = transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.media_space_mutation_receipts
SET provider_effect_status = $5, provider_effect_error_code = NULLIF($6, ''),
    provider_effect_lease_until = NULL, provider_effect_updated_at = $7
WHERE tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3
  AND provider_effect_required AND provider_effect_status = 'applying'
  AND provider_effect_attempts = $4
RETURNING provider_effect_status`,
		scope.TenantID, scope.ActorID, idempotencyKey, attempt, status,
		errorCode, now,
	).Scan(&completed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProviderEffectRetryableFailed, ErrModerationConflict
	}
	if err != nil {
		return ProviderEffectRetryableFailed, ErrModerationUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ProviderEffectRetryableFailed, ErrModerationUnavailable
	}
	return completed, nil
}

func concealModerationSourceError(err error) error {
	if errors.Is(err, ErrSpaceAccessDenied) || errors.Is(err, ErrSpaceNotFound) ||
		errors.Is(err, ErrSourceUnavailable) {
		return ErrModerationNotFound
	}
	return err
}

func nullableModerationInt64(value int64) any {
	if value < 1 {
		return nil
	}
	return value
}
