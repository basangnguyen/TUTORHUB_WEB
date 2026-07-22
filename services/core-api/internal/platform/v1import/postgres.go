package v1import

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func acquireRun(ctx context.Context, connection *pgx.Conn, parsed ParsedFixture, totalRecords int) (runState, error) {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return runState{}, fmt.Errorf("begin V1 fixture run acquisition: %w", err)
	}
	defer transaction.Rollback(context.Background())

	var run runState
	err = transaction.QueryRow(ctx, `
SELECT id, status, checkpoint_ordinal, total_records, failure_attempts,
       COALESCE(last_error_code, '')
FROM tutorhub.legacy_import_runs
WHERE source_system = $1
  AND fixture_key = $2
  AND fixture_sha256 = $3
  AND status IN ('running', 'failed')
ORDER BY started_at DESC, id DESC
LIMIT 1
FOR UPDATE`, parsed.Fixture.SourceSystem, parsed.Fixture.FixtureKey, sourceHashBytes(parsed.SHA256)).Scan(
		&run.ID,
		&run.Status,
		&run.CheckpointOrdinal,
		&run.TotalRecords,
		&run.FailureAttempts,
		&run.LastErrorCode,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		err = transaction.QueryRow(ctx, `
INSERT INTO tutorhub.legacy_import_runs (
    source_system, fixture_key, fixture_sha256, status, total_records,
    checkpoint_ordinal, failure_attempts, started_at, updated_at
) VALUES ($1, $2, $3, 'running', $4, 0, 0, now(), now())
RETURNING id, status, checkpoint_ordinal, total_records, failure_attempts,
          COALESCE(last_error_code, '')`,
			parsed.Fixture.SourceSystem,
			parsed.Fixture.FixtureKey,
			sourceHashBytes(parsed.SHA256),
			totalRecords,
		).Scan(
			&run.ID,
			&run.Status,
			&run.CheckpointOrdinal,
			&run.TotalRecords,
			&run.FailureAttempts,
			&run.LastErrorCode,
		)
	} else if err == nil {
		if run.TotalRecords != totalRecords {
			return runState{}, errors.New("V1 fixture run total does not match fixture")
		}
		_, err = transaction.Exec(ctx, `
UPDATE tutorhub.legacy_import_runs
SET status = 'running', last_error_code = NULL, updated_at = now()
WHERE id = $1`, run.ID)
		run.Status = "running"
		run.LastErrorCode = ""
	}
	if err != nil {
		return runState{}, fmt.Errorf("acquire V1 fixture import run: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return runState{}, fmt.Errorf("commit V1 fixture run acquisition: %w", err)
	}
	return run, nil
}

func applyRecord(
	ctx context.Context,
	connection *pgx.Conn,
	sourceSystem string,
	runID uuid.UUID,
	record plannedRecord,
) error {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin V1 fixture record: %w", err)
	}
	defer transaction.Rollback(context.Background())

	var checkpoint int
	var total int
	if err := transaction.QueryRow(ctx, `
SELECT checkpoint_ordinal, total_records
FROM tutorhub.legacy_import_runs
WHERE id = $1 AND status = 'running'
FOR UPDATE`, runID).Scan(&checkpoint, &total); err != nil {
		return fmt.Errorf("lock V1 fixture run: %w", err)
	}
	if checkpoint > record.Ordinal {
		return transaction.Commit(ctx)
	}
	if checkpoint != record.Ordinal {
		return fmt.Errorf("V1 fixture checkpoint %d does not match ordinal %d", checkpoint, record.Ordinal)
	}

	outcome := outcomeSkipped
	reasonCode := record.SkipReason
	var targetID *uuid.UUID
	if record.SkipReason == "" {
		processedOutcome, processedTargetID, processErr := processRecord(ctx, transaction, sourceSystem, record)
		if processErr != nil {
			return processErr
		}
		outcome = processedOutcome
		reasonCode = ""
		targetID = &processedTargetID
	}

	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.legacy_import_run_items (
    run_id, ordinal, entity_type, external_id, outcome, reason_code,
    target_id, source_sha256, processed_at
) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, now())`,
		runID,
		record.Ordinal,
		record.EntityType,
		record.ExternalID,
		outcome,
		reasonCode,
		targetID,
		sourceHashBytes(record.SourceHash),
	); err != nil {
		return fmt.Errorf("record V1 fixture outcome: %w", err)
	}

	nextCheckpoint := record.Ordinal + 1
	if nextCheckpoint == total {
		_, err = transaction.Exec(ctx, `
UPDATE tutorhub.legacy_import_runs
SET checkpoint_ordinal = $2,
    status = 'completed',
    last_error_code = NULL,
    updated_at = now(),
    completed_at = now()
WHERE id = $1`, runID, nextCheckpoint)
	} else {
		_, err = transaction.Exec(ctx, `
UPDATE tutorhub.legacy_import_runs
SET checkpoint_ordinal = $2, updated_at = now()
WHERE id = $1`, runID, nextCheckpoint)
	}
	if err != nil {
		return fmt.Errorf("advance V1 fixture checkpoint: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit V1 fixture record: %w", err)
	}
	return nil
}

func markRunFailed(ctx context.Context, connection *pgx.Conn, runID uuid.UUID, errorCode string) error {
	_, err := connection.Exec(ctx, `
UPDATE tutorhub.legacy_import_runs
SET status = 'failed',
    failure_attempts = failure_attempts + 1,
    last_error_code = $2,
    updated_at = now()
WHERE id = $1
  AND status = 'running'
  AND checkpoint_ordinal < total_records`, runID, errorCode)
	if err != nil {
		return fmt.Errorf("mark V1 fixture run failed: %w", err)
	}
	return nil
}

func completeEmptyRun(ctx context.Context, connection *pgx.Conn, runID uuid.UUID) error {
	_, err := connection.Exec(ctx, `
UPDATE tutorhub.legacy_import_runs
SET status = 'completed', completed_at = now(), updated_at = now()
WHERE id = $1 AND status = 'running' AND total_records = 0`, runID)
	if err != nil {
		return fmt.Errorf("complete empty V1 fixture run: %w", err)
	}
	return nil
}

func processRecord(
	ctx context.Context,
	transaction pgx.Tx,
	sourceSystem string,
	record plannedRecord,
) (recordOutcome, uuid.UUID, error) {
	targetID := record.TargetID
	mappingExists := false
	var mappedTargetID uuid.UUID
	err := transaction.QueryRow(ctx, `
SELECT target_id
FROM tutorhub.legacy_import_mappings
WHERE source_system = $1 AND entity_type = $2 AND external_id = $3`,
		sourceSystem, record.EntityType, record.ExternalID,
	).Scan(&mappedTargetID)
	switch {
	case err == nil:
		mappingExists = true
		targetID = mappedTargetID
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return "", uuid.Nil, fmt.Errorf("load V1 fixture mapping: %w", err)
	}

	var outcome recordOutcome
	switch desired := record.Desired.(type) {
	case desiredUser:
		outcome, err = upsertUser(ctx, transaction, targetID, mappingExists, desired)
	case desiredTenant:
		outcome, err = upsertTenant(ctx, transaction, targetID, mappingExists, desired)
	case desiredMembership:
		outcome, err = upsertMembership(ctx, transaction, targetID, mappingExists, desired)
	case desiredClass:
		outcome, err = upsertClass(ctx, transaction, targetID, mappingExists, desired)
	default:
		err = errors.New("unsupported V1 fixture entity payload")
	}
	if err != nil {
		return "", uuid.Nil, err
	}

	if _, err := transaction.Exec(ctx, `
INSERT INTO tutorhub.legacy_import_mappings (
    source_system, entity_type, external_id, target_id, source_sha256, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, now(), now())
ON CONFLICT (source_system, entity_type, external_id) DO UPDATE SET
    source_sha256 = EXCLUDED.source_sha256,
    updated_at = now()
WHERE tutorhub.legacy_import_mappings.target_id = EXCLUDED.target_id`,
		sourceSystem,
		record.EntityType,
		record.ExternalID,
		targetID,
		sourceHashBytes(record.SourceHash),
	); err != nil {
		return "", uuid.Nil, fmt.Errorf("upsert V1 fixture mapping: %w", err)
	}
	return outcome, targetID, nil
}

func loadApplyReport(
	ctx context.Context,
	connection *pgx.Conn,
	parsed ParsedFixture,
	plan []plannedRecord,
	runID uuid.UUID,
) (Report, error) {
	report := newReport(ModeApply, parsed, plan)
	report.RunID = runID.String()

	var lastErrorCode string
	if err := connection.QueryRow(ctx, `
SELECT status, checkpoint_ordinal, total_records, failure_attempts,
       COALESCE(last_error_code, '')
FROM tutorhub.legacy_import_runs
WHERE id = $1`, runID).Scan(
		&report.Status,
		&report.CheckpointOrdinal,
		&report.TotalRecords,
		&report.FailureAttempts,
		&lastErrorCode,
	); err != nil {
		return Report{}, fmt.Errorf("load V1 fixture run report: %w", err)
	}

	rows, err := connection.Query(ctx, `
SELECT entity_type, external_id, outcome, COALESCE(reason_code, '')
FROM tutorhub.legacy_import_run_items
WHERE run_id = $1
ORDER BY ordinal`, runID)
	if err != nil {
		return Report{}, fmt.Errorf("load V1 fixture reconciliation rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entityType EntityType
		var externalID string
		var outcome recordOutcome
		var reasonCode string
		if err := rows.Scan(&entityType, &externalID, &outcome, &reasonCode); err != nil {
			return Report{}, fmt.Errorf("scan V1 fixture reconciliation row: %w", err)
		}
		counts := report.Entities[entityType]
		incrementOutcome(&counts, outcome)
		report.Entities[entityType] = counts
		if outcome == outcomeSkipped {
			report.Issues = append(report.Issues, Issue{EntityType: entityType, ExternalID: externalID, ReasonCode: reasonCode})
		}
	}
	if err := rows.Err(); err != nil {
		return Report{}, fmt.Errorf("iterate V1 fixture reconciliation rows: %w", err)
	}

	if report.Status == "failed" && report.CheckpointOrdinal < len(plan) {
		failedRecord := plan[report.CheckpointOrdinal]
		counts := report.Entities[failedRecord.EntityType]
		counts.Failed++
		report.Entities[failedRecord.EntityType] = counts
		report.Issues = append(report.Issues, Issue{
			EntityType: failedRecord.EntityType,
			ExternalID: failedRecord.ExternalID,
			ReasonCode: lastErrorCode,
		})
	}
	finalizeReportTotals(&report)
	return report, nil
}

func upsertUser(ctx context.Context, tx pgx.Tx, targetID uuid.UUID, mappingExists bool, desired desiredUser) (recordOutcome, error) {
	var current desiredUser
	err := tx.QueryRow(ctx, `
SELECT email, display_name, locale, timezone, status, created_at, updated_at
FROM tutorhub.users WHERE id = $1`, targetID).Scan(
		&current.Email, &current.DisplayName, &current.Locale, &current.Timezone,
		&current.Status, &current.CreatedAt, &current.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if mappingExists {
			return "", ErrMappedTargetMissing
		}
		var conflictingID uuid.UUID
		conflictErr := tx.QueryRow(ctx, `SELECT id FROM tutorhub.users WHERE lower(email) = lower($1)`, desired.Email).Scan(&conflictingID)
		if conflictErr == nil {
			return "", ErrNaturalKeyConflict
		}
		if !errors.Is(conflictErr, pgx.ErrNoRows) {
			return "", fmt.Errorf("check V1 user natural key: %w", conflictErr)
		}
		_, err = tx.Exec(ctx, `
INSERT INTO tutorhub.users (
    id, email, display_name, locale, timezone, status, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			targetID, desired.Email, desired.DisplayName, desired.Locale, desired.Timezone,
			desired.Status, desired.CreatedAt, desired.UpdatedAt,
		)
		if err != nil {
			return "", fmt.Errorf("insert V1 user: %w", err)
		}
		return outcomeImported, nil
	}
	if err != nil {
		return "", fmt.Errorf("load mapped V1 user: %w", err)
	}
	if !mappingExists {
		return "", ErrNaturalKeyConflict
	}
	if equalUser(current, desired) {
		return outcomeUnchanged, nil
	}
	_, err = tx.Exec(ctx, `
UPDATE tutorhub.users
SET email=$2, display_name=$3, locale=$4, timezone=$5, status=$6,
    created_at=$7, updated_at=$8
WHERE id=$1`, targetID, desired.Email, desired.DisplayName, desired.Locale,
		desired.Timezone, desired.Status, desired.CreatedAt, desired.UpdatedAt)
	if err != nil {
		return "", fmt.Errorf("update V1 user: %w", err)
	}
	return outcomeUpdated, nil
}

func upsertTenant(ctx context.Context, tx pgx.Tx, targetID uuid.UUID, mappingExists bool, desired desiredTenant) (recordOutcome, error) {
	var current desiredTenant
	err := tx.QueryRow(ctx, `
SELECT slug, name, locale, timezone, status, created_at, updated_at, archived_at
FROM tutorhub.tenants WHERE id = $1`, targetID).Scan(
		&current.Slug, &current.Name, &current.Locale, &current.Timezone,
		&current.Status, &current.CreatedAt, &current.UpdatedAt, &current.ArchivedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if mappingExists {
			return "", ErrMappedTargetMissing
		}
		var conflictingID uuid.UUID
		conflictErr := tx.QueryRow(ctx, `SELECT id FROM tutorhub.tenants WHERE slug = $1`, desired.Slug).Scan(&conflictingID)
		if conflictErr == nil {
			return "", ErrNaturalKeyConflict
		}
		if !errors.Is(conflictErr, pgx.ErrNoRows) {
			return "", fmt.Errorf("check V1 tenant natural key: %w", conflictErr)
		}
		_, err = tx.Exec(ctx, `
INSERT INTO tutorhub.tenants (
    id, slug, name, locale, timezone, status, created_at, updated_at, archived_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, targetID, desired.Slug, desired.Name,
			desired.Locale, desired.Timezone, desired.Status, desired.CreatedAt,
			desired.UpdatedAt, desired.ArchivedAt)
		if err != nil {
			return "", fmt.Errorf("insert V1 tenant: %w", err)
		}
		return outcomeImported, nil
	}
	if err != nil {
		return "", fmt.Errorf("load mapped V1 tenant: %w", err)
	}
	if !mappingExists {
		return "", ErrNaturalKeyConflict
	}
	if equalTenant(current, desired) {
		return outcomeUnchanged, nil
	}
	_, err = tx.Exec(ctx, `
UPDATE tutorhub.tenants
SET slug=$2, name=$3, locale=$4, timezone=$5, status=$6,
    created_at=$7, updated_at=$8, archived_at=$9
WHERE id=$1`, targetID, desired.Slug, desired.Name, desired.Locale, desired.Timezone,
		desired.Status, desired.CreatedAt, desired.UpdatedAt, desired.ArchivedAt)
	if err != nil {
		return "", fmt.Errorf("update V1 tenant: %w", err)
	}
	return outcomeUpdated, nil
}

func upsertMembership(ctx context.Context, tx pgx.Tx, targetID uuid.UUID, mappingExists bool, desired desiredMembership) (recordOutcome, error) {
	var current desiredMembership
	err := tx.QueryRow(ctx, `
SELECT tenant_id, user_id, role, status, joined_at, created_at, updated_at
FROM tutorhub.memberships WHERE id = $1`, targetID).Scan(
		&current.TenantID, &current.UserID, &current.Role, &current.Status,
		&current.JoinedAt, &current.CreatedAt, &current.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if mappingExists {
			return "", ErrMappedTargetMissing
		}
		var conflictingID uuid.UUID
		conflictErr := tx.QueryRow(ctx, `
SELECT id FROM tutorhub.memberships WHERE tenant_id=$1 AND user_id=$2`, desired.TenantID, desired.UserID).Scan(&conflictingID)
		if conflictErr == nil {
			return "", ErrNaturalKeyConflict
		}
		if !errors.Is(conflictErr, pgx.ErrNoRows) {
			return "", fmt.Errorf("check V1 membership natural key: %w", conflictErr)
		}
		_, err = tx.Exec(ctx, `
INSERT INTO tutorhub.memberships (
    id, tenant_id, user_id, role, status, joined_at, created_at, updated_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, targetID, desired.TenantID, desired.UserID,
			desired.Role, desired.Status, desired.JoinedAt, desired.CreatedAt, desired.UpdatedAt)
		if err != nil {
			return "", fmt.Errorf("insert V1 membership: %w", err)
		}
		return outcomeImported, nil
	}
	if err != nil {
		return "", fmt.Errorf("load mapped V1 membership: %w", err)
	}
	if !mappingExists || current.TenantID != desired.TenantID || current.UserID != desired.UserID {
		return "", ErrNaturalKeyConflict
	}
	if equalMembership(current, desired) {
		return outcomeUnchanged, nil
	}
	_, err = tx.Exec(ctx, `
UPDATE tutorhub.memberships
SET role=$2, status=$3, joined_at=$4, created_at=$5, updated_at=$6
WHERE id=$1`, targetID, desired.Role, desired.Status, desired.JoinedAt,
		desired.CreatedAt, desired.UpdatedAt)
	if err != nil {
		return "", fmt.Errorf("update V1 membership: %w", err)
	}
	return outcomeUpdated, nil
}

func upsertClass(ctx context.Context, tx pgx.Tx, targetID uuid.UUID, mappingExists bool, desired desiredClass) (recordOutcome, error) {
	var current desiredClass
	err := tx.QueryRow(ctx, `
SELECT tenant_id, owner_user_id, code, title, description, timezone, status,
       created_at, updated_at, archived_at, archived_from_status
FROM tutorhub.classes WHERE id = $1`, targetID).Scan(
		&current.TenantID, &current.OwnerUserID, &current.Code, &current.Title,
		&current.Description, &current.Timezone, &current.Status, &current.CreatedAt,
		&current.UpdatedAt, &current.ArchivedAt, &current.ArchivedFromStatus,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if mappingExists {
			return "", ErrMappedTargetMissing
		}
		var conflictingID uuid.UUID
		conflictErr := tx.QueryRow(ctx, `
SELECT id FROM tutorhub.classes WHERE tenant_id=$1 AND code=$2`, desired.TenantID, desired.Code).Scan(&conflictingID)
		if conflictErr == nil {
			return "", ErrNaturalKeyConflict
		}
		if !errors.Is(conflictErr, pgx.ErrNoRows) {
			return "", fmt.Errorf("check V1 class natural key: %w", conflictErr)
		}
		_, err = tx.Exec(ctx, `
INSERT INTO tutorhub.classes (
    id, tenant_id, owner_user_id, code, title, description, timezone, status,
    created_at, updated_at, archived_at, archived_from_status
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, targetID, desired.TenantID,
			desired.OwnerUserID, desired.Code, desired.Title, desired.Description,
			desired.Timezone, desired.Status, desired.CreatedAt, desired.UpdatedAt,
			desired.ArchivedAt, desired.ArchivedFromStatus)
		if err != nil {
			return "", fmt.Errorf("insert V1 class: %w", err)
		}
		return outcomeImported, nil
	}
	if err != nil {
		return "", fmt.Errorf("load mapped V1 class: %w", err)
	}
	if !mappingExists || current.TenantID != desired.TenantID {
		return "", ErrNaturalKeyConflict
	}
	if equalClass(current, desired) {
		return outcomeUnchanged, nil
	}
	_, err = tx.Exec(ctx, `
UPDATE tutorhub.classes
SET owner_user_id=$2, code=$3, title=$4, description=$5, timezone=$6, status=$7,
    created_at=$8, updated_at=$9, archived_at=$10, archived_from_status=$11
WHERE id=$1`, targetID, desired.OwnerUserID, desired.Code, desired.Title,
		desired.Description, desired.Timezone, desired.Status, desired.CreatedAt,
		desired.UpdatedAt, desired.ArchivedAt, desired.ArchivedFromStatus)
	if err != nil {
		return "", fmt.Errorf("update V1 class: %w", err)
	}
	return outcomeUpdated, nil
}

func equalUser(left desiredUser, right desiredUser) bool {
	return left.Email == right.Email && left.DisplayName == right.DisplayName &&
		left.Locale == right.Locale && left.Timezone == right.Timezone &&
		left.Status == right.Status && equalTime(left.CreatedAt, right.CreatedAt) &&
		equalTime(left.UpdatedAt, right.UpdatedAt)
}

func equalTenant(left desiredTenant, right desiredTenant) bool {
	return left.Slug == right.Slug && left.Name == right.Name && left.Locale == right.Locale &&
		left.Timezone == right.Timezone && left.Status == right.Status &&
		equalTime(left.CreatedAt, right.CreatedAt) && equalTime(left.UpdatedAt, right.UpdatedAt) &&
		equalOptionalTime(left.ArchivedAt, right.ArchivedAt)
}

func equalMembership(left desiredMembership, right desiredMembership) bool {
	return left.TenantID == right.TenantID && left.UserID == right.UserID &&
		left.Role == right.Role && left.Status == right.Status &&
		equalOptionalTime(left.JoinedAt, right.JoinedAt) &&
		equalTime(left.CreatedAt, right.CreatedAt) && equalTime(left.UpdatedAt, right.UpdatedAt)
}

func equalClass(left desiredClass, right desiredClass) bool {
	return left.TenantID == right.TenantID && left.OwnerUserID == right.OwnerUserID &&
		left.Code == right.Code && left.Title == right.Title &&
		left.Description == right.Description && left.Timezone == right.Timezone &&
		left.Status == right.Status && equalTime(left.CreatedAt, right.CreatedAt) &&
		equalTime(left.UpdatedAt, right.UpdatedAt) &&
		equalOptionalTime(left.ArchivedAt, right.ArchivedAt) &&
		equalOptionalString(left.ArchivedFromStatus, right.ArchivedFromStatus)
}

func equalTime(left time.Time, right time.Time) bool {
	return left.Equal(right)
}

func equalOptionalTime(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func equalOptionalString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
