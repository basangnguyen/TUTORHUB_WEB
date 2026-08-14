package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PostgresProviderEffectRepository is a trusted convergence boundary. It may
// scan provider-effect receipts across tenants because it never reads business
// payloads and only leases already-authorized, committed effects. Every update
// is pinned to the claimed tenant and immutable receipt identity.
type PostgresProviderEffectRepository struct {
	lifecycle *PostgresLifecycleRepository
}

func NewPostgresProviderEffectRepository(
	lifecycle *PostgresLifecycleRepository,
) (*PostgresProviderEffectRepository, error) {
	if lifecycle == nil || lifecycle.database == nil {
		return nil, fmt.Errorf("media lifecycle repository is required")
	}
	return &PostgresProviderEffectRepository{lifecycle: lifecycle}, nil
}

func (repository *PostgresProviderEffectRepository) ClaimNextProviderEffect(
	ctx context.Context,
	now time.Time,
	lease time.Duration,
) (DurableProviderEffect, bool, error) {
	if repository == nil || repository.lifecycle == nil || lease <= 0 {
		return DurableProviderEffect{}, false, ErrProviderEffectReconcileUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return DurableProviderEffect{}, false, ErrProviderEffectReconcileUnavailable
	}
	defer rollbackLifecycle(transaction)

	var effect DurableProviderEffect
	var roomName, participantIdentity sql.NullString
	err = transaction.QueryRow(
		queryContext,
		`WITH candidate AS (
    SELECT tenant_id, actor_user_id, idempotency_key, space_id,
           operation, provider_effect_attempts
    FROM tutorhub.media_space_mutation_receipts
    WHERE provider_effect_required
      AND operation IN (
          'end', 'participant_promote', 'participant_demote',
          'participant_mute', 'participant_remove'
      )
      AND (
          provider_effect_status IN ('pending', 'retryable_failed')
          OR (
              provider_effect_status = 'applying'
              AND provider_effect_lease_until <= $1
          )
      )
    ORDER BY provider_effect_updated_at NULLS FIRST, created_at,
             tenant_id, idempotency_key
    FOR UPDATE SKIP LOCKED
    LIMIT 1
), claimed AS (
    UPDATE tutorhub.media_space_mutation_receipts AS receipt
    SET provider_effect_status = 'applying',
        provider_effect_attempts = candidate.provider_effect_attempts + 1,
        provider_effect_error_code = NULL,
        provider_effect_lease_until = $2,
        provider_effect_updated_at = $1
    FROM candidate
    WHERE receipt.tenant_id = candidate.tenant_id
      AND receipt.actor_user_id = candidate.actor_user_id
      AND receipt.idempotency_key = candidate.idempotency_key
      AND receipt.space_id = candidate.space_id
      AND receipt.operation = candidate.operation
    RETURNING receipt.tenant_id, receipt.actor_user_id,
              receipt.idempotency_key, receipt.space_id, receipt.operation,
              receipt.provider_effect_attempts, receipt.result_room_instance_id,
              receipt.target_participant_session_id
)
SELECT claimed.tenant_id, claimed.actor_user_id, claimed.space_id,
       claimed.idempotency_key, claimed.operation,
       claimed.provider_effect_attempts, room.provider_room_name,
       target.provider_participant_identity
FROM claimed
LEFT JOIN tutorhub.media_room_instances AS room
  ON room.tenant_id = claimed.tenant_id
 AND room.space_id = claimed.space_id
 AND room.id = claimed.result_room_instance_id
LEFT JOIN tutorhub.media_participant_sessions AS target
  ON target.tenant_id = claimed.tenant_id
 AND target.space_id = claimed.space_id
 AND target.room_instance_id = claimed.result_room_instance_id
 AND target.id = claimed.target_participant_session_id`,
		now.UTC(), now.UTC().Add(lease),
	).Scan(
		&effect.Ref.TenantID, &effect.Ref.OriginalActorID, &effect.Ref.SpaceID,
		&effect.Ref.IdempotencyKey, &effect.Ref.Operation, &effect.Ref.Attempt,
		&roomName, &participantIdentity,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := transaction.Commit(queryContext); err != nil {
			return DurableProviderEffect{}, false, ErrProviderEffectReconcileUnavailable
		}
		return DurableProviderEffect{}, false, nil
	}
	if err != nil {
		return DurableProviderEffect{}, false, ErrProviderEffectReconcileUnavailable
	}
	if participantIdentity.Valid {
		effect.ParticipantIdentity = participantIdentity.String
	}
	if roomName.Valid {
		effect.RoomName = roomName.String
	}
	if err := transaction.Commit(queryContext); err != nil {
		return DurableProviderEffect{}, false, ErrProviderEffectReconcileUnavailable
	}
	return effect, true, nil
}

func (repository *PostgresProviderEffectRepository) CompleteProviderEffect(
	ctx context.Context,
	ref DurableProviderEffectRef,
	status ProviderEffectStatus,
	errorCode string,
	now time.Time,
) error {
	if repository == nil || repository.lifecycle == nil ||
		ref.TenantID == uuid.Nil || ref.OriginalActorID == uuid.Nil ||
		ref.SpaceID == uuid.Nil || ref.IdempotencyKey == "" || ref.Operation == "" ||
		ref.Attempt < 1 ||
		(status != ProviderEffectApplied && status != ProviderEffectRetryableFailed &&
			status != ProviderEffectPermanentFailed) ||
		!validDurableProviderCompletion(status, errorCode) {
		return ErrProviderEffectReconcileUnavailable
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return ErrProviderEffectReconcileUnavailable
	}
	defer rollbackLifecycle(transaction)
	command, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.media_space_mutation_receipts
SET provider_effect_status = $7,
    provider_effect_error_code = NULLIF($8, ''),
    provider_effect_lease_until = NULL,
    provider_effect_updated_at = $9
WHERE tenant_id = $1 AND actor_user_id = $2 AND space_id = $3
  AND idempotency_key = $4 AND operation = $5
  AND provider_effect_required AND provider_effect_status = 'applying'
  AND provider_effect_attempts = $6`,
		ref.TenantID, ref.OriginalActorID, ref.SpaceID, ref.IdempotencyKey,
		ref.Operation, ref.Attempt, status, errorCode, now.UTC(),
	)
	if err != nil || command.RowsAffected() != 1 {
		return ErrProviderEffectReconcileUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ErrProviderEffectReconcileUnavailable
	}
	return nil
}

func validDurableProviderCompletion(status ProviderEffectStatus, errorCode string) bool {
	switch status {
	case ProviderEffectApplied:
		return errorCode == ""
	case ProviderEffectRetryableFailed:
		return errorCode == "provider_unavailable"
	case ProviderEffectPermanentFailed:
		return errorCode == "provider_invalid_response"
	default:
		return false
	}
}
