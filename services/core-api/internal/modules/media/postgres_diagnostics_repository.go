package media

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

type PostgresDiagnosticRepository struct {
	lifecycle *PostgresLifecycleRepository
}

func NewPostgresDiagnosticRepository(lifecycle *PostgresLifecycleRepository) (*PostgresDiagnosticRepository, error) {
	if lifecycle == nil || lifecycle.database == nil {
		return nil, fmt.Errorf("media lifecycle repository is required")
	}
	return &PostgresDiagnosticRepository{lifecycle: lifecycle}, nil
}

func (repository *PostgresDiagnosticRepository) RecordDiagnostic(
	ctx context.Context,
	access AccessContext,
	spaceID uuid.UUID,
	input RecordDiagnosticInput,
	recordedAt time.Time,
) error {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	command, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return ErrDiagnosticUnavailable
	}
	defer rollbackLifecycle(command)
	result, err := command.Exec(queryContext, `INSERT INTO tutorhub.media_join_diagnostics (
    id, tenant_id, space_id, room_instance_id, participant_session_id,
    join_attempt_id, stage, outcome, error_code, network_quality, media_path,
    duration_ms, recorded_at, retention_until
)
SELECT $1, participant.tenant_id, participant.space_id, participant.room_instance_id,
       participant.id, participant.join_attempt_id, $7, $8, $9, $10, $11, $12,
       $13, $13::timestamptz + interval '30 days'
FROM tutorhub.media_participant_sessions AS participant
JOIN tutorhub.memberships AS membership
  ON membership.tenant_id = participant.tenant_id
 AND membership.user_id = participant.user_id
JOIN tutorhub.tenants AS tenant ON tenant.id = participant.tenant_id
WHERE participant.tenant_id = $2
  AND participant.space_id = $3
  AND participant.room_instance_id = $4
  AND participant.user_id = $5
  AND participant.join_attempt_id = $6
  AND membership.status = 'active'
  AND tenant.status = 'active'
ON CONFLICT (tenant_id, id) DO NOTHING`,
		input.EventID, access.TenantID, spaceID, input.RoomInstanceID, access.ActorID,
		input.JoinAttemptID, input.Stage, input.Outcome, input.ErrorCode,
		input.NetworkQuality, input.MediaPath, input.DurationMS, recordedAt,
	)
	if err != nil {
		return ErrDiagnosticUnavailable
	}
	if result.RowsAffected() == 0 {
		var exists bool
		if err := command.QueryRow(queryContext, `SELECT EXISTS (
    SELECT 1 FROM tutorhub.media_join_diagnostics WHERE tenant_id = $1 AND id = $2
)`, access.TenantID, input.EventID).Scan(&exists); err != nil {
			return ErrDiagnosticUnavailable
		}
		if !exists {
			return ErrSpaceNotFound
		}
	}
	if err := command.Commit(queryContext); err != nil {
		return ErrDiagnosticUnavailable
	}
	return nil
}

func (repository *PostgresDiagnosticRepository) ExportDiagnostics(
	ctx context.Context,
	access AccessContext,
	filter DiagnosticExportFilter,
) (DiagnosticExport, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.lifecycle.queryTimeout)
	defer cancel()
	transaction, err := repository.lifecycle.database.Begin(queryContext)
	if err != nil {
		return DiagnosticExport{}, ErrDiagnosticUnavailable
	}
	defer rollbackLifecycle(transaction)
	var role, membershipStatus, tenantStatus string
	if err := transaction.QueryRow(queryContext, `SELECT membership.role, membership.status, tenant.status
FROM tutorhub.memberships AS membership
JOIN tutorhub.tenants AS tenant ON tenant.id = membership.tenant_id
WHERE membership.tenant_id = $1 AND membership.user_id = $2
FOR SHARE OF membership, tenant`, access.TenantID, access.ActorID).Scan(
		&role, &membershipStatus, &tenantStatus,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return DiagnosticExport{}, ErrDiagnosticForbidden
		}
		return DiagnosticExport{}, ErrDiagnosticUnavailable
	}
	if policy.OrganizationRole(role) != policy.OrganizationRoleAdmin ||
		membershipStatus != "active" || tenantStatus != "active" {
		return DiagnosticExport{}, ErrDiagnosticForbidden
	}

	export := DiagnosticExport{From: filter.From, To: filter.To, Items: []DiagnosticExportItem{}}
	var p95 *float64
	if err := transaction.QueryRow(queryContext, `SELECT
    count(DISTINCT join_attempt_id) FILTER (WHERE stage = 'join_attempt')::integer,
    count(DISTINCT join_attempt_id) FILTER (
        WHERE stage = 'media' AND outcome = 'succeeded'
    )::integer,
    percentile_cont(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (
        WHERE stage = 'media' AND outcome = 'succeeded'
    ),
    count(*) FILTER (WHERE stage = 'reconnected' AND outcome = 'succeeded')::integer,
    count(*) FILTER (WHERE stage = 'reconnected' AND outcome = 'failed')::integer
FROM tutorhub.media_join_diagnostics
WHERE tenant_id = $1 AND recorded_at >= $2 AND recorded_at < $3`,
		access.TenantID, filter.From, filter.To,
	).Scan(
		&export.Metrics.JoinAttempts, &export.Metrics.SuccessfulJoins, &p95,
		&export.Metrics.ReconnectSucceeded, &export.Metrics.ReconnectFailed,
	); err != nil {
		return DiagnosticExport{}, ErrDiagnosticUnavailable
	}
	if export.Metrics.JoinAttempts > 0 {
		export.Metrics.JoinSuccessRate = float64(export.Metrics.SuccessfulJoins) /
			float64(export.Metrics.JoinAttempts)
	}
	if p95 != nil {
		export.Metrics.P95TimeToMediaMS = int(math.Round(*p95))
	}

	rows, err := transaction.Query(queryContext, `SELECT
    id, participant_session_id, stage, outcome, error_code,
    network_quality, media_path, duration_ms, recorded_at
FROM tutorhub.media_join_diagnostics
WHERE tenant_id = $1 AND recorded_at >= $2 AND recorded_at < $3
ORDER BY recorded_at DESC, id DESC
LIMIT $4`, access.TenantID, filter.From, filter.To, filter.Limit+1)
	if err != nil {
		return DiagnosticExport{}, ErrDiagnosticUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var item DiagnosticExportItem
		var participantSessionID uuid.UUID
		if err := rows.Scan(
			&item.EventID, &participantSessionID, &item.Stage, &item.Outcome,
			&item.ErrorCode, &item.NetworkQuality, &item.MediaPath,
			&item.DurationMS, &item.RecordedAt,
		); err != nil {
			return DiagnosticExport{}, ErrDiagnosticUnavailable
		}
		item.SessionRef = diagnosticSessionRef(access.TenantID, participantSessionID)
		export.Items = append(export.Items, item)
	}
	if rows.Err() != nil {
		return DiagnosticExport{}, ErrDiagnosticUnavailable
	}
	if len(export.Items) > filter.Limit {
		export.Items = export.Items[:filter.Limit]
		export.Truncated = true
	}
	if err := transaction.Commit(queryContext); err != nil {
		return DiagnosticExport{}, ErrDiagnosticUnavailable
	}
	return export, nil
}
