package media

import (
	"bytes"
	"context"
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

// PostgresLobbyRepository owns the P4-04 lobby write model. It deliberately
// shares the lifecycle transaction boundary so tenant-control locking,
// membership revalidation, source authorization, and feature controls cannot
// drift from the room lifecycle implementation.
type PostgresLobbyRepository struct {
	lifecycle *PostgresLifecycleRepository
}

func NewPostgresLobbyRepository(
	lifecycle *PostgresLifecycleRepository,
) (*PostgresLobbyRepository, error) {
	if lifecycle == nil || lifecycle.database == nil || lifecycle.controls == nil {
		return nil, fmt.Errorf("media lifecycle repository is required")
	}
	return &PostgresLobbyRepository{lifecycle: lifecycle}, nil
}

type lobbyAdmissionRow struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	Status         string
	Version        int64
	DisplayName    string
	ResolutionCode sql.NullString
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type lobbyParticipantRow struct {
	ID                 uuid.UUID
	AdmissionRequestID uuid.UUID
	JoinAttemptID      uuid.UUID
	UserID             uuid.UUID
	InstanceRole       InstanceRole
	Status             string
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type lobbyMemberRow struct {
	UserID      uuid.UUID
	DisplayName string
	Status      LobbyMemberStatus
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (repository *PostgresLobbyRepository) ListAdmissions(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input ListLobbyAdmissionsInput,
	now time.Time,
	ttl time.Duration,
) (LobbyAdmissionPage, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return LobbyAdmissionPage{}, repository.lifecycle.unavailable("begin lobby admission list", err)
	}
	defer rollbackLifecycle(transaction)

	access, scope, space, room, err := repository.loadModeratorLobby(
		queryContext, transaction, access, spaceID, input.ExpectedSpaceVersion,
		input.ExpectedRoomInstanceID, input.ExpectedRoomInstanceVersion, true,
	)
	if err != nil {
		return LobbyAdmissionPage{}, err
	}
	_ = access
	_ = space
	if err := expireTimedOutLobbyAdmissions(
		queryContext, transaction, scope, spaceID, room.ID, now.UTC(), ttl,
	); err != nil {
		return LobbyAdmissionPage{}, repository.lifecycle.unavailable("expire timed-out lobby admissions", err)
	}

	rows, err := transaction.Query(
		queryContext,
		`SELECT admission.id, admission.status, admission.version,
       target.display_name, admission.created_at, admission.updated_at
FROM tutorhub.media_admission_requests AS admission
JOIN tutorhub.users AS target ON target.id = admission.user_id
WHERE admission.tenant_id = $1 AND admission.space_id = $2
  AND admission.room_instance_id = $3 AND admission.status = 'waiting'
  AND admission.created_at > $4
ORDER BY admission.created_at, admission.id
LIMIT $5`,
		scope.TenantID, spaceID, room.ID, now.UTC().Add(-ttl), input.Limit,
	)
	if err != nil {
		return LobbyAdmissionPage{}, repository.lifecycle.unavailable("list lobby admissions", err)
	}
	defer rows.Close()

	page := LobbyAdmissionPage{Items: []LobbyAdmission{}}
	for rows.Next() {
		var item LobbyAdmission
		var updatedAt time.Time
		if err := rows.Scan(
			&item.ID, &item.Status, &item.Version, &item.DisplayName,
			&item.CreatedAt, &updatedAt,
		); err != nil {
			return LobbyAdmissionPage{}, repository.lifecycle.unavailable("scan lobby admission", err)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.ExpiresAt = item.CreatedAt.Add(ttl)
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return LobbyAdmissionPage{}, repository.lifecycle.unavailable("iterate lobby admissions", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return LobbyAdmissionPage{}, repository.lifecycle.unavailable("commit lobby admission list", err)
	}
	return page, nil
}

func (repository *PostgresLobbyRepository) GetJoinAttempt(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	joinAttemptID uuid.UUID,
	input ListLobbyAdmissionsInput,
	now time.Time,
	ttl time.Duration,
) (JoinAttempt, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("begin lobby join-attempt read", err)
	}
	defer rollbackLifecycle(transaction)

	access, scope, space, room, err := repository.loadSelfLobby(
		queryContext, transaction, access, spaceID, input.ExpectedSpaceVersion,
		input.ExpectedRoomInstanceID, input.ExpectedRoomInstanceVersion, true, true,
	)
	if err != nil {
		return JoinAttempt{}, err
	}
	participant, admission, err := loadLobbyAttemptByJoinID(
		queryContext, transaction, scope.TenantID, space.ID, room.ID,
		scope.ActorID, joinAttemptID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return JoinAttempt{}, ErrAdmissionNotFound
	}
	if err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("load lobby join attempt", err)
	}
	if admission.Status == "waiting" && !now.UTC().Before(admission.CreatedAt.Add(ttl)) {
		if err := expireLobbyAttempt(
			queryContext, transaction, scope, space.ID, room.ID,
			admission, participant, "wait_timeout", now.UTC(),
		); err != nil {
			return JoinAttempt{}, repository.lifecycle.unavailable("expire lobby join attempt", err)
		}
		participant, admission, err = loadLobbyAttemptByJoinID(
			queryContext, transaction, scope.TenantID, space.ID, room.ID,
			scope.ActorID, joinAttemptID, false,
		)
		if err != nil {
			return JoinAttempt{}, repository.lifecycle.unavailable("reload expired lobby join attempt", err)
		}
	}
	projected := projectLobbyJoinAttempt(participant, admission, space, room, now.UTC(), ttl)
	if err := transaction.Commit(queryContext); err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("commit lobby join-attempt read", err)
	}
	_ = access
	return projected, nil
}

func (repository *PostgresLobbyRepository) MutateAdmission(
	ctx context.Context,
	access AccessContext,
	command AdmissionMutationCommand,
) (LobbyAdmission, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return LobbyAdmission{}, repository.lifecycle.unavailable("begin lobby admission mutation", err)
	}
	defer rollbackLifecycle(transaction)

	access, scope, space, room, err := repository.loadModeratorLobby(
		queryContext, transaction, access, command.SpaceID, command.ExpectedSpaceVersion,
		command.ExpectedRoomInstanceID, command.ExpectedRoomInstanceVersion, true,
	)
	if err != nil {
		return LobbyAdmission{}, err
	}
	if err := acquireLobbyMutationLock(
		queryContext, transaction, scope, command.IdempotencyKey,
	); err != nil {
		return LobbyAdmission{}, repository.lifecycle.unavailable("lock lobby admission mutation", err)
	}
	if found, err := loadLobbyMutationReceipt(
		queryContext, transaction, scope, command.Operation, command.SpaceID,
		command.IdempotencyKey, command.Fingerprint,
	); err != nil {
		return LobbyAdmission{}, repository.lifecycle.unavailable("load lobby admission receipt", err)
	} else if found {
		item, err := loadLobbyAdmissionProjection(
			queryContext, transaction, scope.TenantID, command.SpaceID, room.ID,
			command.AdmissionID, command.AdmissionTTL, false,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return LobbyAdmission{}, ErrAdmissionNotFound
		}
		if err != nil {
			return LobbyAdmission{}, repository.lifecycle.unavailable("load replayed lobby admission", err)
		}
		if err := transaction.Commit(queryContext); err != nil {
			return LobbyAdmission{}, repository.lifecycle.unavailable("commit lobby admission replay", err)
		}
		return item, nil
	}
	// A lock blocks only a new admit transition. Receipt replay above remains
	// idempotent, while deny/restore and bounded reads remain available.
	if command.Operation == "admission_admit" && space.Locked {
		return LobbyAdmission{}, ErrRoomLocked
	}

	admission, participant, err := loadLobbyAdmissionForMutation(
		queryContext, transaction, scope.TenantID, command.SpaceID, room.ID,
		command.AdmissionID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return LobbyAdmission{}, ErrAdmissionNotFound
	}
	if err != nil {
		return LobbyAdmission{}, repository.lifecycle.unavailable("lock lobby admission", err)
	}
	if admission.Version != command.ExpectedAdmissionVersion {
		return LobbyAdmission{}, ErrAdmissionVersionConflict
	}
	if command.OccurredAt.Before(admission.CreatedAt) {
		return LobbyAdmission{}, ErrInvalidLobbyRequest
	}
	if admission.Status == "waiting" &&
		!command.OccurredAt.Before(admission.CreatedAt.Add(command.AdmissionTTL)) {
		if err := expireLobbyAttempt(
			queryContext, transaction, scope, command.SpaceID, room.ID,
			admission, participant, "wait_timeout", command.OccurredAt,
		); err != nil {
			return LobbyAdmission{}, repository.lifecycle.unavailable("expire late lobby admission mutation", err)
		}
		if err := transaction.Commit(queryContext); err != nil {
			return LobbyAdmission{}, repository.lifecycle.unavailable("commit late lobby admission expiry", err)
		}
		return LobbyAdmission{}, ErrAdmissionTransition
	}

	var eventType string
	switch command.Operation {
	case "admission_admit":
		eventType, err = admitLobbyAdmission(
			queryContext, transaction, scope, admission, participant, command.OccurredAt,
		)
	case "admission_deny":
		eventType, err = denyLobbyAdmission(
			queryContext, transaction, scope, admission, participant,
			command.ReasonCode, command.OccurredAt,
		)
	case "admission_restore":
		eventType, err = restoreLobbyAdmission(
			queryContext, transaction, scope, admission, participant, command.OccurredAt,
		)
	default:
		err = ErrInvalidLobbyRequest
	}
	if err != nil {
		return LobbyAdmission{}, err
	}
	updated, err := loadLobbyAdmissionProjection(
		queryContext, transaction, scope.TenantID, command.SpaceID, room.ID,
		command.AdmissionID, command.AdmissionTTL, false,
	)
	if err != nil {
		return LobbyAdmission{}, repository.lifecycle.unavailable("project lobby admission", err)
	}
	if err := insertLobbyMutationReceipt(
		queryContext, transaction, scope, command.Operation, command.SpaceID,
		room.ID, command.IdempotencyKey, command.Fingerprint, space.Version,
		command.OccurredAt,
	); err != nil {
		return LobbyAdmission{}, repository.lifecycle.unavailable("record lobby admission receipt", err)
	}
	if err := appendLobbyAdmissionEvent(
		queryContext, transaction, scope, command.SpaceID, admission.UserID, updated,
		eventType, command.ReasonCode, command.OccurredAt,
	); err != nil {
		return LobbyAdmission{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return LobbyAdmission{}, repository.lifecycle.unavailable("commit lobby admission mutation", err)
	}
	_ = access
	return updated, nil
}

func (repository *PostgresLobbyRepository) CancelJoinAttempt(
	ctx context.Context,
	access AccessContext,
	command AdmissionMutationCommand,
) (JoinAttempt, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("begin lobby cancellation", err)
	}
	defer rollbackLifecycle(transaction)

	access, scope, space, room, err := repository.loadSelfLobby(
		queryContext, transaction, access, command.SpaceID, command.ExpectedSpaceVersion,
		command.ExpectedRoomInstanceID, command.ExpectedRoomInstanceVersion, true, false,
	)
	if err != nil {
		return JoinAttempt{}, err
	}
	if space.Status != SpaceStatusOpen || room.Status != RoomInstanceActive {
		return JoinAttempt{}, ErrRoomNotOpen
	}
	if err := acquireLobbyMutationLock(
		queryContext, transaction, scope, command.IdempotencyKey,
	); err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("lock lobby cancellation", err)
	}
	if found, err := loadLobbyMutationReceipt(
		queryContext, transaction, scope, command.Operation, command.SpaceID,
		command.IdempotencyKey, command.Fingerprint,
	); err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("load lobby cancellation receipt", err)
	} else if found {
		participant, admission, err := loadLobbyAttemptByJoinID(
			queryContext, transaction, scope.TenantID, space.ID, room.ID,
			scope.ActorID, command.JoinAttemptID, false,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return JoinAttempt{}, ErrAdmissionNotFound
		}
		if err != nil {
			return JoinAttempt{}, repository.lifecycle.unavailable("load replayed lobby cancellation", err)
		}
		projected := projectLobbyJoinAttempt(
			participant, admission, space, room, command.OccurredAt,
			command.AdmissionTTL,
		)
		if err := transaction.Commit(queryContext); err != nil {
			return JoinAttempt{}, repository.lifecycle.unavailable("commit lobby cancellation replay", err)
		}
		return projected, nil
	}

	participant, admission, err := loadLobbyAttemptByJoinID(
		queryContext, transaction, scope.TenantID, space.ID, room.ID,
		scope.ActorID, command.JoinAttemptID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return JoinAttempt{}, ErrAdmissionNotFound
	}
	if err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("lock lobby cancellation", err)
	}
	if admission.Version != command.ExpectedAdmissionVersion {
		return JoinAttempt{}, ErrAdmissionVersionConflict
	}
	if admission.Status != "waiting" || participant.Status != string(JoinAttemptWaiting) {
		return JoinAttempt{}, ErrAdmissionTransition
	}
	if command.OccurredAt.Before(admission.CreatedAt) {
		return JoinAttempt{}, ErrAdmissionTransition
	}
	if !command.OccurredAt.Before(admission.CreatedAt.Add(command.AdmissionTTL)) {
		if err := expireLobbyAttempt(
			queryContext, transaction, scope, command.SpaceID, room.ID,
			admission, participant, "wait_timeout", command.OccurredAt,
		); err != nil {
			return JoinAttempt{}, repository.lifecycle.unavailable("expire late lobby cancellation", err)
		}
		if err := insertLobbyMutationReceipt(
			queryContext, transaction, scope, command.Operation, command.SpaceID,
			room.ID, command.IdempotencyKey, command.Fingerprint, space.Version,
			command.OccurredAt,
		); err != nil {
			return JoinAttempt{}, repository.lifecycle.unavailable("record late lobby cancellation receipt", err)
		}
		participant, admission, err = loadLobbyAttemptByJoinID(
			queryContext, transaction, scope.TenantID, space.ID, room.ID,
			scope.ActorID, command.JoinAttemptID, false,
		)
		if err != nil {
			return JoinAttempt{}, repository.lifecycle.unavailable("project late lobby cancellation", err)
		}
		projection := projectLobbyJoinAttempt(
			participant, admission, space, room, command.OccurredAt, command.AdmissionTTL,
		)
		if err := transaction.Commit(queryContext); err != nil {
			return JoinAttempt{}, repository.lifecycle.unavailable("commit late lobby cancellation", err)
		}
		return projection, nil
	}
	tag, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.media_admission_requests
SET status = 'cancelled', version = version + 1, resolved_at = $6,
    resolved_by = $5, resolution_code = 'actor_cancelled', updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND id = $4 AND status = 'waiting'`,
		scope.TenantID, space.ID, room.ID, admission.ID, scope.ActorID, command.OccurredAt,
	)
	if err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("cancel lobby admission", err)
	}
	if tag.RowsAffected() != 1 {
		return JoinAttempt{}, ErrAdmissionTransition
	}
	tag, err = transaction.Exec(
		queryContext,
		`UPDATE tutorhub.media_participant_sessions
SET status = 'left', version = version + 1, capacity_reserved = false,
    terminal_at = $6, updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND id = $4 AND user_id = $5 AND status = 'waiting'`,
		scope.TenantID, space.ID, room.ID, participant.ID, scope.ActorID, command.OccurredAt,
	)
	if err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("release cancelled lobby participant", err)
	}
	if tag.RowsAffected() != 1 {
		return JoinAttempt{}, ErrAdmissionTransition
	}
	if err := insertLobbyMutationReceipt(
		queryContext, transaction, scope, command.Operation, command.SpaceID,
		room.ID, command.IdempotencyKey, command.Fingerprint, space.Version,
		command.OccurredAt,
	); err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("record lobby cancellation receipt", err)
	}
	updatedParticipant, updatedAdmission, err := loadLobbyAttemptByJoinID(
		queryContext, transaction, scope.TenantID, space.ID, room.ID,
		scope.ActorID, command.JoinAttemptID, false,
	)
	if err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("project cancelled lobby attempt", err)
	}
	projection := projectLobbyJoinAttempt(
		updatedParticipant, updatedAdmission, space, room, command.OccurredAt,
		command.AdmissionTTL,
	)
	admissionProjection := projectLobbyAdmission(updatedAdmission, command.AdmissionTTL)
	if err := appendLobbyAdmissionEvent(
		queryContext, transaction, scope, command.SpaceID, scope.ActorID, admissionProjection,
		"media_admission.cancelled.v1", "actor_cancelled", command.OccurredAt,
	); err != nil {
		return JoinAttempt{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return JoinAttempt{}, repository.lifecycle.unavailable("commit lobby cancellation", err)
	}
	_ = access
	return projection, nil
}

func (repository *PostgresLobbyRepository) ListMembers(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input ListLobbyMembersInput,
) (LobbyMemberPage, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return LobbyMemberPage{}, repository.lifecycle.unavailable("begin lobby member list", err)
	}
	defer rollbackLifecycle(transaction)
	_, scope, _, _, err := repository.loadStudyMeetingLobby(
		queryContext, transaction, access, spaceID, input.ExpectedSpaceVersion,
		"list", "", false,
	)
	if err != nil {
		return LobbyMemberPage{}, err
	}
	rows, err := transaction.Query(
		queryContext,
		`SELECT member.user_id, target.display_name, member.status,
       member.version, member.created_at, member.updated_at
FROM tutorhub.media_space_members AS member
JOIN tutorhub.users AS target ON target.id = member.user_id
WHERE member.tenant_id = $1 AND member.space_id = $2
ORDER BY member.created_at, member.user_id
LIMIT $3`,
		scope.TenantID, spaceID, input.Limit,
	)
	if err != nil {
		return LobbyMemberPage{}, repository.lifecycle.unavailable("list lobby members", err)
	}
	defer rows.Close()
	page := LobbyMemberPage{Items: []LobbyMember{}}
	for rows.Next() {
		var item LobbyMember
		if err := rows.Scan(
			&item.UserID, &item.DisplayName, &item.Status, &item.Version,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return LobbyMemberPage{}, repository.lifecycle.unavailable("scan lobby member", err)
		}
		item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return LobbyMemberPage{}, repository.lifecycle.unavailable("iterate lobby members", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return LobbyMemberPage{}, repository.lifecycle.unavailable("commit lobby member list", err)
	}
	return page, nil
}

func (repository *PostgresLobbyRepository) InviteMember(
	ctx context.Context,
	access AccessContext,
	command InviteLobbyMemberCommand,
) (LobbyMember, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("begin lobby member invitation", err)
	}
	defer rollbackLifecycle(transaction)
	_, scope, space, _, err := repository.loadStudyMeetingLobby(
		queryContext, transaction, access, command.SpaceID, command.ExpectedSpaceVersion,
		"invite", "", true,
	)
	if err != nil {
		return LobbyMember{}, err
	}
	if err := acquireLobbyMutationLock(
		queryContext, transaction, scope, command.IdempotencyKey,
	); err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("lock lobby member invitation", err)
	}
	if found, err := loadLobbyMutationReceipt(
		queryContext, transaction, scope, "member_invite", command.SpaceID,
		command.IdempotencyKey, command.Fingerprint,
	); err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("load lobby invitation receipt", err)
	} else if found {
		var targetID uuid.UUID
		if err := transaction.QueryRow(
			queryContext,
			`SELECT member.user_id
FROM tutorhub.media_space_members AS member
JOIN tutorhub.users AS target ON target.id = member.user_id
WHERE member.tenant_id = $1 AND member.space_id = $2 AND target.email = $3`,
			scope.TenantID, command.SpaceID, command.TargetEmail,
		).Scan(&targetID); errors.Is(err, pgx.ErrNoRows) {
			return LobbyMember{}, ErrLobbyMemberNotFound
		} else if err != nil {
			return LobbyMember{}, repository.lifecycle.unavailable("load replayed lobby invitation", err)
		}
		member, err := loadLobbyMemberProjection(
			queryContext, transaction, scope.TenantID, command.SpaceID, targetID, false,
		)
		if err != nil {
			return LobbyMember{}, repository.lifecycle.unavailable("project replayed lobby invitation", err)
		}
		if err := transaction.Commit(queryContext); err != nil {
			return LobbyMember{}, repository.lifecycle.unavailable("commit lobby invitation replay", err)
		}
		return member, nil
	}

	var targetID uuid.UUID
	if err := transaction.QueryRow(
		queryContext,
		`SELECT member.user_id
FROM tutorhub.memberships AS member
JOIN tutorhub.users AS target ON target.id = member.user_id
WHERE member.tenant_id = $1 AND target.email = $2
  AND member.status = 'active' AND target.status = 'active'`,
		scope.TenantID, command.TargetEmail,
	).Scan(&targetID); errors.Is(err, pgx.ErrNoRows) {
		return LobbyMember{}, ErrLobbyMemberNotFound
	} else if err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("resolve lobby invitation member", err)
	}
	if targetID == scope.ActorID {
		return LobbyMember{}, ErrInvalidLobbyRequest
	}
	existing, err := loadLobbyMemberProjection(
		queryContext, transaction, scope.TenantID, command.SpaceID, targetID, true,
	)
	if err == nil && existing.Status != LobbyMemberActive {
		return LobbyMember{}, ErrLobbyMemberVersionConflict
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return LobbyMember{}, repository.lifecycle.unavailable("lock existing lobby member", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		tag, insertErr := transaction.Exec(
			queryContext,
			`INSERT INTO tutorhub.media_space_members (
    tenant_id, space_id, user_id, status, version, invited_by, created_at, updated_at
)
VALUES ($1, $2, $3, 'active', 1, $4, $5, $5)`,
			scope.TenantID, command.SpaceID, targetID, scope.ActorID, command.OccurredAt,
		)
		if insertErr != nil {
			return LobbyMember{}, repository.lifecycle.unavailable("insert lobby member", insertErr)
		}
		if tag.RowsAffected() != 1 {
			return LobbyMember{}, ErrLobbyMemberVersionConflict
		}
	}
	member, err := loadLobbyMemberProjection(
		queryContext, transaction, scope.TenantID, command.SpaceID, targetID, false,
	)
	if err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("project invited lobby member", err)
	}
	if err := insertLobbyMutationReceipt(
		queryContext, transaction, scope, "member_invite", command.SpaceID,
		uuid.Nil, command.IdempotencyKey, command.Fingerprint, space.Version,
		command.OccurredAt,
	); err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("record lobby invitation receipt", err)
	}
	if err := appendLobbyMemberEvent(
		queryContext, transaction, scope, command.SpaceID, member,
		"media_space_member.invited.v1", "", command.OccurredAt,
	); err != nil {
		return LobbyMember{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("commit lobby member invitation", err)
	}
	return member, nil
}

func (repository *PostgresLobbyRepository) MutateMember(
	ctx context.Context,
	access AccessContext,
	command LobbyMemberMutationCommand,
) (LobbyMember, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("begin lobby member mutation", err)
	}
	defer rollbackLifecycle(transaction)
	_, scope, space, room, err := repository.loadStudyMeetingLobby(
		queryContext, transaction, access, command.SpaceID, command.ExpectedSpaceVersion,
		command.Operation, command.ReasonCode, true,
	)
	if err != nil {
		return LobbyMember{}, err
	}
	if command.TargetUserID == scope.ActorID {
		return LobbyMember{}, ErrInvalidLobbyRequest
	}
	if err := acquireLobbyMutationLock(
		queryContext, transaction, scope, command.IdempotencyKey,
	); err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("lock lobby member mutation", err)
	}
	if found, err := loadLobbyMutationReceipt(
		queryContext, transaction, scope, command.Operation, command.SpaceID,
		command.IdempotencyKey, command.Fingerprint,
	); err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("load lobby member receipt", err)
	} else if found {
		member, err := loadLobbyMemberProjection(
			queryContext, transaction, scope.TenantID, command.SpaceID,
			command.TargetUserID, false,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return LobbyMember{}, ErrLobbyMemberNotFound
		}
		if err != nil {
			return LobbyMember{}, repository.lifecycle.unavailable("load replayed lobby member", err)
		}
		if err := transaction.Commit(queryContext); err != nil {
			return LobbyMember{}, repository.lifecycle.unavailable("commit lobby member replay", err)
		}
		return member, nil
	}
	member, err := loadLobbyMemberProjection(
		queryContext, transaction, scope.TenantID, command.SpaceID,
		command.TargetUserID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return LobbyMember{}, ErrLobbyMemberNotFound
	}
	if err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("lock lobby member", err)
	}
	if member.Version != command.ExpectedMemberVersion {
		return LobbyMember{}, ErrLobbyMemberVersionConflict
	}

	var nextStatus LobbyMemberStatus
	var eventType string
	switch command.Operation {
	case "member_revoke":
		if member.Status != LobbyMemberActive {
			return LobbyMember{}, ErrAdmissionTransition
		}
		revokeTag, revokeErr := transaction.Exec(
			queryContext,
			`UPDATE tutorhub.media_space_members
SET status = 'revoked', version = version + 1, revoked_at = $5,
    revoked_by = $4, updated_at = $5
WHERE tenant_id = $1 AND space_id = $2 AND user_id = $3 AND status = 'active'`,
			scope.TenantID, command.SpaceID, command.TargetUserID,
			scope.ActorID, command.OccurredAt,
		)
		if revokeErr != nil {
			return LobbyMember{}, repository.lifecycle.unavailable("revoke lobby member", revokeErr)
		}
		if revokeTag.RowsAffected() != 1 {
			return LobbyMember{}, ErrLobbyMemberVersionConflict
		}
		if room.ID != uuid.Nil {
			if err := removeLobbyMemberParticipants(
				queryContext, transaction, scope, command.SpaceID, room.ID,
				command.TargetUserID, command.OccurredAt,
			); err != nil {
				return LobbyMember{}, repository.lifecycle.unavailable("remove revoked lobby member", err)
			}
		}
		nextStatus = LobbyMemberRevoked
		eventType = "media_space_member.revoked.v1"
	case "member_restore":
		if member.Status != LobbyMemberRevoked {
			return LobbyMember{}, ErrAdmissionTransition
		}
		restoreTag, restoreErr := transaction.Exec(
			queryContext,
			`UPDATE tutorhub.media_space_members
SET status = 'active', version = version + 1, revoked_at = NULL,
    revoked_by = NULL, updated_at = $4
WHERE tenant_id = $1 AND space_id = $2 AND user_id = $3 AND status = 'revoked'`,
			scope.TenantID, command.SpaceID, command.TargetUserID, command.OccurredAt,
		)
		if restoreErr != nil {
			return LobbyMember{}, repository.lifecycle.unavailable("restore lobby member", restoreErr)
		}
		if restoreTag.RowsAffected() != 1 {
			return LobbyMember{}, ErrLobbyMemberVersionConflict
		}
		if room.ID != uuid.Nil {
			if _, err := transaction.Exec(
				queryContext,
				`UPDATE tutorhub.media_participant_sessions
SET version = version + 1, rejoin_restored_at = $6,
    rejoin_restored_by = $5, updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND user_id = $4 AND status = 'removed' AND rejoin_restored_at IS NULL`,
				scope.TenantID, command.SpaceID, room.ID, command.TargetUserID,
				scope.ActorID, command.OccurredAt,
			); err != nil {
				return LobbyMember{}, repository.lifecycle.unavailable("restore lobby member rejoin", err)
			}
		}
		nextStatus = LobbyMemberActive
		eventType = "media_space_member.restored.v1"
	default:
		return LobbyMember{}, ErrInvalidLobbyRequest
	}
	updated, err := loadLobbyMemberProjection(
		queryContext, transaction, scope.TenantID, command.SpaceID,
		command.TargetUserID, false,
	)
	if err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("project mutated lobby member", err)
	}
	if updated.Status != nextStatus {
		return LobbyMember{}, ErrLobbyUnavailable
	}
	if err := insertLobbyMutationReceipt(
		queryContext, transaction, scope, command.Operation, command.SpaceID,
		room.ID, command.IdempotencyKey, command.Fingerprint, space.Version,
		command.OccurredAt,
	); err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("record lobby member receipt", err)
	}
	if err := appendLobbyMemberEvent(
		queryContext, transaction, scope, command.SpaceID, updated, eventType,
		command.ReasonCode, command.OccurredAt,
	); err != nil {
		return LobbyMember{}, err
	}
	if err := transaction.Commit(queryContext); err != nil {
		return LobbyMember{}, repository.lifecycle.unavailable("commit lobby member mutation", err)
	}
	return updated, nil
}

func (repository *PostgresLobbyRepository) loadModeratorLobby(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	spaceID uuid.UUID,
	expectedSpaceVersion int64,
	expectedRoomID uuid.UUID,
	expectedRoomVersion int64,
	lock bool,
) (AccessContext, tenancy.Context, spaceRow, roomRow, error) {
	if err := repository.lifecycle.acquireTenantControlLock(
		ctx, transaction, access.TenantID,
	); err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(ctx, transaction, access)
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	space, err := loadSpace(ctx, transaction, scope.TenantID, spaceID, lock)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceNotFound
	}
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, repository.lifecycle.unavailable("load lobby space", err)
	}
	source, err := repository.lifecycle.authorizeSource(
		ctx, transaction, access, scope, space, policy.ActionSessionJoin, false, false,
	)
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	if err := repository.lifecycle.controls.RequireFeature(
		ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	if space.Version != expectedSpaceVersion {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceVersionConflict
	}
	if space.Status != SpaceStatusOpen || !space.LobbyEnabled {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrRoomNotOpen
	}
	room, err := loadLobbyRoom(
		ctx, transaction, scope.TenantID, space.ID, expectedRoomID, lock,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrRoomNotOpen
	}
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, repository.lifecycle.unavailable("load lobby room", err)
	}
	if room.Version != expectedRoomVersion {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrAdmissionVersionConflict
	}
	if room.Status != RoomInstanceActive {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrRoomNotOpen
	}
	actorRole, err := effectiveRoomRole(
		ctx, transaction, scope.TenantID, room.ID, scope.ActorID,
		source.InstanceRole, lock,
	)
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{},
			repository.lifecycle.unavailable("resolve lobby moderator role", err)
	}
	if actorRole != InstanceRoleHost && actorRole != InstanceRoleCoHost &&
		actorRole != InstanceRoleTeachingAssistant {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceAccessDenied
	}
	return access, scope, space, room, nil
}

func (repository *PostgresLobbyRepository) loadSelfLobby(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	spaceID uuid.UUID,
	expectedSpaceVersion int64,
	expectedRoomID uuid.UUID,
	expectedRoomVersion int64,
	lock bool,
	allowTerminalRead bool,
) (AccessContext, tenancy.Context, spaceRow, roomRow, error) {
	if err := repository.lifecycle.acquireTenantControlLock(
		ctx, transaction, access.TenantID,
	); err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(ctx, transaction, access)
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	space, err := loadSpace(ctx, transaction, scope.TenantID, spaceID, lock)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceNotFound
	}
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, repository.lifecycle.unavailable("load self lobby space", err)
	}
	if _, err := repository.lifecycle.authorizeSource(
		ctx, transaction, access, scope, space, policy.ActionSessionJoin, false, false,
	); err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	if err := repository.lifecycle.controls.RequireFeature(
		ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	spaceVersionMatches := space.Version == expectedSpaceVersion
	if !spaceVersionMatches && !allowTerminalRead {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceVersionConflict
	}
	if !space.LobbyEnabled {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrRoomNotOpen
	}
	room, err := loadLobbyRoom(
		ctx, transaction, scope.TenantID, space.ID, expectedRoomID, lock,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrRoomNotOpen
	}
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, repository.lifecycle.unavailable("load self lobby room", err)
	}
	roomVersionMatches := room.Version == expectedRoomVersion
	terminal := space.Status == SpaceStatusEnded || space.Status == SpaceStatusCancelled ||
		room.Status == RoomInstanceClosing || room.Status == RoomInstanceEnded ||
		room.Status == RoomInstanceFailed
	if (!spaceVersionMatches || !roomVersionMatches) && (!allowTerminalRead || !terminal) {
		if !spaceVersionMatches {
			return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceVersionConflict
		}
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrAdmissionVersionConflict
	}
	return access, scope, space, room, nil
}

func (repository *PostgresLobbyRepository) loadStudyMeetingLobby(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	spaceID uuid.UUID,
	expectedSpaceVersion int64,
	operation string,
	reasonCode string,
	lock bool,
) (AccessContext, tenancy.Context, spaceRow, roomRow, error) {
	if err := repository.lifecycle.acquireTenantControlLock(
		ctx, transaction, access.TenantID,
	); err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	access, scope, err := repository.lifecycle.requireActiveScope(ctx, transaction, access)
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	space, err := loadSpace(ctx, transaction, scope.TenantID, spaceID, lock)
	if errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceNotFound
	}
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, repository.lifecycle.unavailable("load study-meeting lobby space", err)
	}
	source, err := repository.lifecycle.authorizeSource(
		ctx, transaction, access, scope, space, policy.ActionClassView, false, false,
	)
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	if source.Kind != SourceStudyMeeting || (!source.Owner && !source.SafetyAdmin) {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceAccessDenied
	}
	switch operation {
	case "list":
	case "invite", "member_restore":
		if !source.Owner {
			return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceAccessDenied
		}
	case "member_revoke":
		if source.SafetyAdmin && !source.Owner && reasonCode == "" {
			return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrInvalidLobbyRequest
		}
	default:
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrInvalidLobbyRequest
	}
	if err := repository.lifecycle.controls.RequireFeature(
		ctx, transaction, scope.TenantID, featurecontrol.FeatureClassroomMediaRooms,
	); err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, err
	}
	if space.Version != expectedSpaceVersion {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrSpaceVersionConflict
	}
	if space.Status != SpaceStatusScheduled && space.Status != SpaceStatusOpen {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, ErrRoomNotOpen
	}
	room, err := loadOptionalActiveLobbyRoom(
		ctx, transaction, scope.TenantID, space.ID, lock,
	)
	if err != nil {
		return AccessContext{}, tenancy.Context{}, spaceRow{}, roomRow{}, repository.lifecycle.unavailable("load study-meeting lobby room", err)
	}
	return access, scope, space, room, nil
}

func loadLobbyRoom(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	lock bool,
) (roomRow, error) {
	query := `SELECT id, status, version, provider_room_name, provider_room_sid,
       created_at, updated_at
FROM tutorhub.media_room_instances
WHERE tenant_id = $1 AND space_id = $2 AND id = $3`
	if lock {
		query += " FOR UPDATE"
	}
	var room roomRow
	err := transaction.QueryRow(ctx, query, tenantID, spaceID, roomID).Scan(
		&room.ID, &room.Status, &room.Version, &room.ProviderRoomName,
		&room.ProviderRoomSID, &room.CreatedAt, &room.UpdatedAt,
	)
	room.CreatedAt, room.UpdatedAt = room.CreatedAt.UTC(), room.UpdatedAt.UTC()
	return room, err
}

func loadOptionalActiveLobbyRoom(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	lock bool,
) (roomRow, error) {
	room, err := loadActiveRoom(ctx, transaction, tenantID, spaceID, lock)
	if errors.Is(err, pgx.ErrNoRows) {
		return roomRow{}, nil
	}
	return room, err
}

func acquireLobbyMutationLock(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	idempotencyKey string,
) error {
	return acquireLifecycleTransactionLock(
		ctx, transaction,
		"media-lobby-mutation:"+scope.TenantID.String()+":"+
			scope.ActorID.String()+":"+idempotencyKey,
	)
}

func loadLobbyMutationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	operation string,
	spaceID uuid.UUID,
	idempotencyKey string,
	fingerprint []byte,
) (bool, error) {
	var persistedFingerprint []byte
	var persistedOperation string
	var persistedSpaceID uuid.UUID
	var actorID uuid.UUID
	err := transaction.QueryRow(
		ctx,
		`SELECT request_fingerprint, operation, space_id, actor_user_id
FROM tutorhub.media_space_mutation_receipts
WHERE tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3`,
		scope.TenantID, scope.ActorID, idempotencyKey,
	).Scan(&persistedFingerprint, &persistedOperation, &persistedSpaceID, &actorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if persistedOperation != operation || persistedSpaceID != spaceID || actorID != scope.ActorID ||
		!bytes.Equal(persistedFingerprint, fingerprint) {
		return false, ErrSpaceIdempotency
	}
	return true, nil
}

func insertLobbyMutationReceipt(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	operation string,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	idempotencyKey string,
	fingerprint []byte,
	spaceVersion int64,
	now time.Time,
) error {
	_, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.media_space_mutation_receipts (
    tenant_id, idempotency_key, request_fingerprint, operation, space_id,
    result_space_version, result_room_instance_id, actor_user_id, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		scope.TenantID, idempotencyKey, fingerprint, operation, spaceID,
		spaceVersion, nullableLifecycleUUID(roomID), scope.ActorID, now.UTC(),
	)
	return err
}

func loadLobbyAdmissionForMutation(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	admissionID uuid.UUID,
) (lobbyAdmissionRow, lobbyParticipantRow, error) {
	var admission lobbyAdmissionRow
	err := transaction.QueryRow(
		ctx,
		`SELECT admission.id, admission.user_id, admission.status, admission.version,
       target.display_name, admission.resolution_code,
       admission.created_at, admission.updated_at
FROM tutorhub.media_admission_requests AS admission
JOIN tutorhub.users AS target ON target.id = admission.user_id
WHERE admission.tenant_id = $1 AND admission.space_id = $2
  AND admission.room_instance_id = $3 AND admission.id = $4
FOR UPDATE OF admission`,
		tenantID, spaceID, roomID, admissionID,
	).Scan(
		&admission.ID, &admission.UserID, &admission.Status, &admission.Version,
		&admission.DisplayName, &admission.ResolutionCode,
		&admission.CreatedAt, &admission.UpdatedAt,
	)
	if err != nil {
		return lobbyAdmissionRow{}, lobbyParticipantRow{}, err
	}
	admission.CreatedAt = admission.CreatedAt.UTC()
	admission.UpdatedAt = admission.UpdatedAt.UTC()
	participant, err := loadLobbyParticipantByAdmission(
		ctx, transaction, tenantID, spaceID, roomID, admissionID, admission.UserID, true,
	)
	return admission, participant, err
}

func loadLobbyParticipantByAdmission(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	admissionID uuid.UUID,
	userID uuid.UUID,
	lock bool,
) (lobbyParticipantRow, error) {
	query := `SELECT id, admission_request_id, join_attempt_id, user_id,
       instance_role, status, version, created_at, updated_at
FROM tutorhub.media_participant_sessions
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND admission_request_id = $4 AND user_id = $5`
	if lock {
		query += " FOR UPDATE"
	}
	var participant lobbyParticipantRow
	err := transaction.QueryRow(
		ctx, query, tenantID, spaceID, roomID, admissionID, userID,
	).Scan(
		&participant.ID, &participant.AdmissionRequestID, &participant.JoinAttemptID,
		&participant.UserID, &participant.InstanceRole, &participant.Status,
		&participant.Version, &participant.CreatedAt, &participant.UpdatedAt,
	)
	participant.CreatedAt = participant.CreatedAt.UTC()
	participant.UpdatedAt = participant.UpdatedAt.UTC()
	return participant, err
}

func loadLobbyAttemptByJoinID(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	userID uuid.UUID,
	joinAttemptID uuid.UUID,
	lock bool,
) (lobbyParticipantRow, lobbyAdmissionRow, error) {
	query := `SELECT participant.id, participant.admission_request_id,
       participant.join_attempt_id, participant.user_id, participant.instance_role,
       participant.status, participant.version, participant.created_at,
       participant.updated_at, admission.id, admission.user_id, admission.status,
       admission.version, target.display_name, admission.resolution_code,
       admission.created_at, admission.updated_at
FROM tutorhub.media_participant_sessions AS participant
JOIN tutorhub.media_admission_requests AS admission
  ON admission.tenant_id = participant.tenant_id
 AND admission.space_id = participant.space_id
 AND admission.room_instance_id = participant.room_instance_id
 AND admission.id = participant.admission_request_id
JOIN tutorhub.users AS target ON target.id = admission.user_id
WHERE participant.tenant_id = $1 AND participant.space_id = $2
  AND participant.room_instance_id = $3 AND participant.user_id = $4
  AND participant.join_attempt_id = $5`
	if lock {
		query += " FOR UPDATE OF admission, participant"
	}
	var participant lobbyParticipantRow
	var admission lobbyAdmissionRow
	err := transaction.QueryRow(
		ctx, query, tenantID, spaceID, roomID, userID, joinAttemptID,
	).Scan(
		&participant.ID, &participant.AdmissionRequestID, &participant.JoinAttemptID,
		&participant.UserID, &participant.InstanceRole, &participant.Status,
		&participant.Version, &participant.CreatedAt, &participant.UpdatedAt,
		&admission.ID, &admission.UserID, &admission.Status, &admission.Version,
		&admission.DisplayName, &admission.ResolutionCode,
		&admission.CreatedAt, &admission.UpdatedAt,
	)
	participant.CreatedAt = participant.CreatedAt.UTC()
	participant.UpdatedAt = participant.UpdatedAt.UTC()
	admission.CreatedAt = admission.CreatedAt.UTC()
	admission.UpdatedAt = admission.UpdatedAt.UTC()
	return participant, admission, err
}

func loadLobbyAdmissionProjection(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	admissionID uuid.UUID,
	ttl time.Duration,
	lock bool,
) (LobbyAdmission, error) {
	query := `SELECT admission.id, admission.status, admission.version,
       target.display_name, admission.created_at
FROM tutorhub.media_admission_requests AS admission
JOIN tutorhub.users AS target ON target.id = admission.user_id
WHERE admission.tenant_id = $1 AND admission.space_id = $2
  AND admission.room_instance_id = $3 AND admission.id = $4`
	if lock {
		query += " FOR UPDATE OF admission"
	}
	var item LobbyAdmission
	err := transaction.QueryRow(
		ctx, query, tenantID, spaceID, roomID, admissionID,
	).Scan(&item.ID, &item.Status, &item.Version, &item.DisplayName, &item.CreatedAt)
	item.CreatedAt = item.CreatedAt.UTC()
	item.ExpiresAt = item.CreatedAt.Add(ttl)
	return item, err
}

func loadLobbyMemberProjection(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	userID uuid.UUID,
	lock bool,
) (LobbyMember, error) {
	query := `SELECT member.user_id, target.display_name, member.status,
       member.version, member.created_at, member.updated_at
FROM tutorhub.media_space_members AS member
JOIN tutorhub.users AS target ON target.id = member.user_id
WHERE member.tenant_id = $1 AND member.space_id = $2 AND member.user_id = $3`
	if lock {
		query += " FOR UPDATE OF member"
	}
	var item LobbyMember
	err := transaction.QueryRow(ctx, query, tenantID, spaceID, userID).Scan(
		&item.UserID, &item.DisplayName, &item.Status, &item.Version,
		&item.CreatedAt, &item.UpdatedAt,
	)
	item.CreatedAt, item.UpdatedAt = item.CreatedAt.UTC(), item.UpdatedAt.UTC()
	return item, err
}

func admitLobbyAdmission(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	admission lobbyAdmissionRow,
	participant lobbyParticipantRow,
	now time.Time,
) (string, error) {
	if admission.Status != "waiting" || participant.Status != string(JoinAttemptWaiting) {
		return "", ErrAdmissionTransition
	}
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_admission_requests
SET status = 'admitted', version = version + 1, resolved_at = $5,
    resolved_by = $4, resolution_code = NULL, updated_at = $5
WHERE tenant_id = $1 AND id = $2 AND user_id = $3 AND status = 'waiting'`,
		scope.TenantID, admission.ID, admission.UserID, scope.ActorID, now.UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("admit lobby admission: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return "", ErrAdmissionTransition
	}
	tag, err = transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_sessions
SET status = 'admitted', version = version + 1, admitted_at = $5, updated_at = $5
WHERE tenant_id = $1 AND id = $2 AND admission_request_id = $3
  AND user_id = $4 AND status = 'waiting'`,
		scope.TenantID, participant.ID, admission.ID, admission.UserID, now.UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("admit lobby participant: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return "", ErrAdmissionTransition
	}
	return "media_admission.admitted.v1", nil
}

func denyLobbyAdmission(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	admission lobbyAdmissionRow,
	participant lobbyParticipantRow,
	reasonCode string,
	now time.Time,
) (string, error) {
	if admission.Status != "waiting" || participant.Status != string(JoinAttemptWaiting) || reasonCode == "" {
		return "", ErrAdmissionTransition
	}
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_admission_requests
SET status = 'denied', version = version + 1, resolved_at = $6,
    resolved_by = $4, resolution_code = $5, updated_at = $6
WHERE tenant_id = $1 AND id = $2 AND user_id = $3 AND status = 'waiting'`,
		scope.TenantID, admission.ID, admission.UserID, scope.ActorID, reasonCode, now.UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("deny lobby admission: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return "", ErrAdmissionTransition
	}
	tag, err = transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_sessions
SET status = 'removed', version = version + 1, capacity_reserved = false,
    terminal_at = $6, removed_by = $5, updated_at = $6
WHERE tenant_id = $1 AND id = $2 AND admission_request_id = $3
  AND user_id = $4 AND status = 'waiting'`,
		scope.TenantID, participant.ID, admission.ID, admission.UserID,
		scope.ActorID, now.UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("remove denied lobby participant: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return "", ErrAdmissionTransition
	}
	return "media_admission.denied.v1", nil
}

func restoreLobbyAdmission(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	admission lobbyAdmissionRow,
	participant lobbyParticipantRow,
	now time.Time,
) (string, error) {
	if admission.Status != "denied" || participant.Status != "removed" {
		return "", ErrAdmissionTransition
	}
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_admission_requests
SET status = 'cancelled', version = version + 1, resolved_at = $5,
    resolved_by = $4, resolution_code = 'restored', updated_at = $5
WHERE tenant_id = $1 AND id = $2 AND user_id = $3 AND status = 'denied'`,
		scope.TenantID, admission.ID, admission.UserID, scope.ActorID, now.UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("close restored lobby admission: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return "", ErrAdmissionTransition
	}
	tag, err = transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_sessions
SET version = version + 1, rejoin_restored_at = $6,
    rejoin_restored_by = $5, updated_at = $6
WHERE tenant_id = $1 AND id = $2 AND admission_request_id = $3
  AND user_id = $4 AND status = 'removed' AND rejoin_restored_at IS NULL`,
		scope.TenantID, participant.ID, admission.ID, admission.UserID,
		scope.ActorID, now.UTC(),
	)
	if err != nil {
		return "", fmt.Errorf("restore denied lobby participant: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return "", ErrAdmissionTransition
	}
	return "media_admission.restored.v1", nil
}

func projectLobbyAdmission(row lobbyAdmissionRow, ttl time.Duration) LobbyAdmission {
	status := LobbyAdmissionStatus(row.Status)
	if row.Status == "expired" {
		switch row.ResolutionCode.String {
		case "meeting_ended":
			status = LobbyAdmissionMeetingEnded
		case "provider_unavailable":
			status = LobbyAdmissionProviderUnavailable
		default:
			status = LobbyAdmissionTimeout
		}
	}
	return LobbyAdmission{
		ID: row.ID, Status: status, Version: row.Version,
		DisplayName: row.DisplayName, CreatedAt: row.CreatedAt.UTC(),
		ExpiresAt: row.CreatedAt.UTC().Add(ttl),
	}
}

func projectLobbyJoinAttempt(
	participant lobbyParticipantRow,
	admission lobbyAdmissionRow,
	space spaceRow,
	room roomRow,
	now time.Time,
	ttl time.Duration,
) JoinAttempt {
	status := JoinAttemptStatus(participant.Status)
	switch admission.Status {
	case "waiting":
		status = JoinAttemptWaiting
		if !now.Before(admission.CreatedAt.Add(ttl)) {
			status = JoinAttemptTimeout
		}
	case "admitted":
		if participant.Status == string(JoinAttemptJoining) {
			status = JoinAttemptJoining
		} else {
			status = JoinAttemptAdmitted
		}
	case "denied":
		status = JoinAttemptDenied
	case "cancelled":
		status = JoinAttemptCancelled
	case "expired":
		switch admission.ResolutionCode.String {
		case "meeting_ended":
			status = JoinAttemptMeetingEnded
		case "provider_unavailable":
			status = JoinAttemptProviderUnavailable
		default:
			status = JoinAttemptTimeout
		}
	}
	if space.Status == SpaceStatusEnded || space.Status == SpaceStatusCancelled {
		status = JoinAttemptMeetingEnded
	}
	if room.Status == RoomInstanceFailed {
		status = JoinAttemptProviderUnavailable
	} else if room.Status == RoomInstanceEnded || room.Status == RoomInstanceClosing {
		status = JoinAttemptMeetingEnded
	}
	admissionID := admission.ID
	admissionVersion := admission.Version
	expiresAt := admission.CreatedAt.UTC().Add(ttl)
	return JoinAttempt{
		ParticipantSessionID: participant.ID,
		RoomInstanceID:       room.ID,
		AdmissionRequestID:   &admissionID,
		AdmissionVersion:     &admissionVersion,
		JoinAttemptID:        participant.JoinAttemptID,
		Status:               status,
		Version:              participant.Version,
		InstanceRole:         participant.InstanceRole,
		CanSubscribe:         true,
		CreatedAt:            participant.CreatedAt.UTC(),
		UpdatedAt:            participant.UpdatedAt.UTC(),
		ExpiresAt:            &expiresAt,
	}
}

func removeLobbyMemberParticipants(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	targetUserID uuid.UUID,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_admission_requests
SET status = 'denied', version = version + 1, resolved_at = $6,
    resolved_by = $5, resolution_code = 'member_revoked', updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND user_id = $4 AND status = 'waiting'`,
		scope.TenantID, spaceID, roomID, targetUserID, scope.ActorID, now.UTC(),
	); err != nil {
		return err
	}
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_sessions
SET status = 'removed', version = version + 1, capacity_reserved = false,
    terminal_at = $6, removed_by = $5, failure_code = NULL, updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND user_id = $4
  AND status IN ('waiting', 'admitted', 'joining', 'connected', 'reconnecting')`,
		scope.TenantID, spaceID, roomID, targetUserID, scope.ActorID, now.UTC(),
	)
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_hand_states AS hand
SET is_raised = false
FROM tutorhub.media_participant_sessions AS participant
WHERE hand.tenant_id = $1 AND hand.space_id = $2 AND hand.room_instance_id = $3
  AND participant.tenant_id = hand.tenant_id
  AND participant.space_id = hand.space_id
  AND participant.room_instance_id = hand.room_instance_id
  AND participant.id = hand.participant_session_id
  AND participant.user_id = $4
  AND hand.is_raised`,
		scope.TenantID, spaceID, roomID, targetUserID,
	); err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return advanceMediaRosterProjection(
		ctx, transaction, scope.TenantID, spaceID, roomID,
	)
}

func expireTimedOutLobbyAdmissions(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	now time.Time,
	ttl time.Duration,
) error {
	rows, err := transaction.Query(
		ctx,
		`SELECT admission.id, admission.user_id, admission.status, admission.version,
       target.display_name, admission.resolution_code,
       admission.created_at, admission.updated_at
FROM tutorhub.media_admission_requests AS admission
JOIN tutorhub.users AS target ON target.id = admission.user_id
WHERE admission.tenant_id = $1 AND admission.space_id = $2
  AND admission.room_instance_id = $3 AND admission.status = 'waiting'
  AND admission.created_at <= $4
ORDER BY admission.created_at, admission.id
FOR UPDATE OF admission`,
		scope.TenantID, spaceID, roomID, now.UTC().Add(-ttl),
	)
	if err != nil {
		return err
	}
	admissions := make([]lobbyAdmissionRow, 0)
	for rows.Next() {
		var admission lobbyAdmissionRow
		if err := rows.Scan(
			&admission.ID, &admission.UserID, &admission.Status, &admission.Version,
			&admission.DisplayName, &admission.ResolutionCode,
			&admission.CreatedAt, &admission.UpdatedAt,
		); err != nil {
			rows.Close()
			return err
		}
		admission.CreatedAt = admission.CreatedAt.UTC()
		admission.UpdatedAt = admission.UpdatedAt.UTC()
		admissions = append(admissions, admission)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, admission := range admissions {
		participant, err := loadLobbyParticipantByAdmission(
			ctx, transaction, scope.TenantID, spaceID, roomID,
			admission.ID, admission.UserID, true,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAdmissionTransition
		}
		if err != nil {
			return err
		}
		if err := expireLobbyAttempt(
			ctx, transaction, scope, spaceID, roomID, admission, participant,
			"wait_timeout", now.UTC(),
		); err != nil {
			return err
		}
	}
	return nil
}

func expireLobbyAttempt(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	admission lobbyAdmissionRow,
	participant lobbyParticipantRow,
	resolutionCode string,
	now time.Time,
) error {
	if admission.Status != "waiting" || participant.Status != string(JoinAttemptWaiting) {
		return ErrAdmissionTransition
	}
	tag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_admission_requests
SET status = 'expired', version = version + 1, resolved_at = $6,
    resolved_by = NULL, resolution_code = $5, updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND id = $4 AND status = 'waiting'`,
		scope.TenantID, spaceID, roomID, admission.ID, resolutionCode, now.UTC(),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrAdmissionTransition
	}
	tag, err = transaction.Exec(
		ctx,
		`UPDATE tutorhub.media_participant_sessions
SET status = 'failed', version = version + 1, capacity_reserved = false,
    terminal_at = $6, failure_code = 'admission_timeout', updated_at = $6
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
  AND id = $4 AND user_id = $5 AND status = 'waiting'`,
		scope.TenantID, spaceID, roomID, participant.ID, admission.UserID, now.UTC(),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrAdmissionTransition
	}
	return appendExpiredLobbyAdmissionEvent(
		ctx, transaction, scope.TenantID, scope.ActorID, spaceID,
		admission.ID, admission.UserID, admission.Version+1,
		resolutionCode, now.UTC(),
	)
}

// expireOutstandingLobbyAdmissions closes every still-waiting admission before
// participant capacity is released. resolverID is nil for provider-originated
// terminalization and present for an explicit lifecycle action.
func expireOutstandingLobbyAdmissions(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	spaceID uuid.UUID,
	roomID uuid.UUID,
	resolverID *uuid.UUID,
	resolutionCode string,
	now time.Time,
) error {
	rows, err := transaction.Query(
		ctx,
		`UPDATE tutorhub.media_admission_requests
SET status = 'expired', version = version + 1, resolved_at = $5,
    resolved_by = $4, resolution_code = $6, updated_at = $5
WHERE tenant_id = $1 AND space_id = $2 AND room_instance_id = $3
	  AND status = 'waiting'
RETURNING id, user_id, version`,
		tenantID, spaceID, roomID, nullableLifecycleUUIDPointer(resolverID),
		now.UTC(), resolutionCode,
	)
	if err != nil {
		return err
	}
	type expiredAdmission struct {
		ID      uuid.UUID
		UserID  uuid.UUID
		Version int64
	}
	expired := make([]expiredAdmission, 0)
	for rows.Next() {
		var item expiredAdmission
		if err := rows.Scan(&item.ID, &item.UserID, &item.Version); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	actorID := uuid.Nil
	if resolverID != nil {
		actorID = *resolverID
	}
	for _, item := range expired {
		if err := appendExpiredLobbyAdmissionEvent(
			ctx, transaction, tenantID, actorID, spaceID,
			item.ID, item.UserID, item.Version, resolutionCode, now.UTC(),
		); err != nil {
			return err
		}
	}
	return nil
}

func appendExpiredLobbyAdmissionEvent(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	spaceID uuid.UUID,
	admissionID uuid.UUID,
	targetUserID uuid.UUID,
	version int64,
	resolutionCode string,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'media_admission', $2, 'media_admission.expired.v1',
    jsonb_strip_nulls(jsonb_build_object(
        'admission_id', $2::uuid, 'space_id', $3::uuid,
        'actor_user_id', $4::uuid, 'target_user_id', $5::uuid,
        'status', 'expired', 'version', $6::bigint,
        'reason_code', $7::text
    )),
    $8, $8
)`,
		tenantID, admissionID, spaceID, nullableLifecycleUUID(actorID),
		targetUserID, version, resolutionCode, now.UTC(),
	); err != nil {
		return fmt.Errorf("append media_admission.expired.v1 outbox event: %w", err)
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: tenantID, ActorID: actorID, EventType: "media_admission.expired.v1",
		AggregateType: "media_admission", AggregateID: admissionID,
		Metadata: audit.Metadata{
			audit.MetadataKeyTargetUserID: targetUserID.String(),
			"status":                      "expired",
			"version":                     strconv.FormatInt(version, 10),
			"reason_code":                 resolutionCode,
		},
		OccurredAt: now.UTC(),
	}); err != nil {
		return fmt.Errorf("append media_admission.expired.v1 audit event: %w", err)
	}
	return nil
}

func appendLobbyAdmissionEvent(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	spaceID uuid.UUID,
	targetUserID uuid.UUID,
	admission LobbyAdmission,
	eventType string,
	reasonCode string,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'media_admission', $2, $3,
    jsonb_strip_nulls(jsonb_build_object(
        'admission_id', $2::uuid, 'space_id', $4::uuid,
        'actor_user_id', $5::uuid, 'target_user_id', $6::uuid,
        'status', $7::text, 'version', $8::bigint,
        'reason_code', NULLIF($9::text, '')
    )),
    $10, $10
)`,
		scope.TenantID, admission.ID, eventType, spaceID,
		scope.ActorID, targetUserID, admission.Status, admission.Version,
		reasonCode, now.UTC(),
	); err != nil {
		return fmt.Errorf("append %s outbox event: %w", eventType, err)
	}
	metadata := audit.Metadata{
		audit.MetadataKeyTargetUserID: targetUserID.String(),
		"status":                      string(admission.Status),
		"version":                     strconv.FormatInt(admission.Version, 10),
	}
	if reasonCode != "" {
		metadata["reason_code"] = reasonCode
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: scope.TenantID, ActorID: scope.ActorID, EventType: eventType,
		AggregateType: "media_admission", AggregateID: admission.ID,
		Metadata: metadata, OccurredAt: now.UTC(),
	}); err != nil {
		return fmt.Errorf("append %s audit event: %w", eventType, err)
	}
	return nil
}

func appendLobbyMemberEvent(
	ctx context.Context,
	transaction pgx.Tx,
	scope tenancy.Context,
	spaceID uuid.UUID,
	member LobbyMember,
	eventType string,
	reasonCode string,
	now time.Time,
) error {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.outbox_events (
    tenant_id, aggregate_type, aggregate_id, event_type,
    payload, occurred_at, available_at
)
VALUES (
    $1, 'media_space_member', $2, $3,
    jsonb_strip_nulls(jsonb_build_object(
        'space_id', $2::uuid, 'actor_user_id', $4::uuid,
        'target_user_id', $5::uuid, 'status', $6::text,
        'version', $7::bigint, 'reason_code', NULLIF($8::text, '')
    )),
    $9, $9
)`,
		scope.TenantID, spaceID, eventType, scope.ActorID,
		member.UserID, member.Status, member.Version, reasonCode, now.UTC(),
	); err != nil {
		return fmt.Errorf("append %s outbox event: %w", eventType, err)
	}
	metadata := audit.Metadata{
		audit.MetadataKeyTargetUserID: member.UserID.String(),
		"status":                      string(member.Status),
		"version":                     strconv.FormatInt(member.Version, 10),
	}
	if reasonCode != "" {
		metadata["reason_code"] = reasonCode
	}
	if err := audit.AppendDomainEvent(ctx, transaction, audit.DomainEvent{
		TenantID: scope.TenantID, ActorID: scope.ActorID, EventType: eventType,
		AggregateType: "media_space_member", AggregateID: spaceID,
		Metadata: metadata, OccurredAt: now.UTC(),
	}); err != nil {
		return fmt.Errorf("append %s audit event: %w", eventType, err)
	}
	return nil
}
