package content

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
	"github.com/tutorhub-v2/core-api/internal/policy"
)

const defaultQueryTimeout = 10 * time.Second

type transactionDatabase interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     transactionDatabase
	queryTimeout time.Duration
	authorizer   policy.Authorizer
	controls     featurecontrol.Enforcer
}

func NewPostgresRepository(
	database transactionDatabase,
	queryTimeout time.Duration,
	authorizer policy.Authorizer,
	controls featurecontrol.Enforcer,
) (*PostgresRepository, error) {
	if database == nil || authorizer == nil || controls == nil {
		return nil, fmt.Errorf("content database, authorizer, and feature controls are required")
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresRepository{
		database: database, queryTimeout: queryTimeout, authorizer: authorizer, controls: controls,
	}, nil
}

func (repository *PostgresRepository) CreateIntent(
	ctx context.Context,
	access AccessContext,
	command CreateCommand,
) (CreateIntentResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return CreateIntentResult{}, fmt.Errorf("begin upload intent creation: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return CreateIntentResult{}, err
	}
	existing, err := loadFileByClientRequest(
		queryContext, transaction, access.TenantID, access.ActorID, command.ClientRequestID, true,
	)
	if err == nil {
		return repository.replayIntent(queryContext, transaction, access, command, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CreateIntentResult{}, fmt.Errorf("query upload intent idempotency: %w", err)
	}
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureFileUploads,
	); err != nil {
		return CreateIntentResult{}, err
	}
	// RequireFeature serializes tenant mutations. Recheck after acquiring its advisory
	// lock so two concurrent requests with the same idempotency key cannot race into
	// the unique constraint.
	existing, err = loadFileByClientRequest(
		queryContext, transaction, access.TenantID, access.ActorID, command.ClientRequestID, true,
	)
	if err == nil {
		return repository.replayIntent(queryContext, transaction, access, command, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CreateIntentResult{}, fmt.Errorf("recheck upload intent idempotency: %w", err)
	}
	if err := repository.authorizeClass(
		queryContext, transaction, access, command.ClassID, policy.ActionFileUpload,
	); err != nil {
		return CreateIntentResult{}, err
	}
	if err := repository.controls.RequireQuotaAtMost(
		queryContext, transaction, access.TenantID,
		featurecontrol.QuotaSingleFileBytes, command.ExpectedSizeBytes,
	); err != nil {
		return CreateIntentResult{}, err
	}
	if err := expirePendingFiles(
		queryContext, transaction, access.TenantID, command.CreatedAt,
	); err != nil {
		return CreateIntentResult{}, err
	}
	usage, err := lockFileUsage(queryContext, transaction, access.TenantID, command.CreatedAt)
	if err != nil {
		return CreateIntentResult{}, err
	}
	if usage.FileCount == int64(^uint64(0)>>1) ||
		usage.ReservedBytes > int64(^uint64(0)>>1)-usage.CommittedBytes ||
		usage.ReservedBytes+usage.CommittedBytes > int64(^uint64(0)>>1)-command.ExpectedSizeBytes {
		return CreateIntentResult{}, ErrQuotaExceeded
	}
	if err := repository.controls.RequireQuotaAtMost(
		queryContext, transaction, access.TenantID,
		featurecontrol.QuotaFilesPerTenant, usage.FileCount+1,
	); err != nil {
		return CreateIntentResult{}, err
	}
	if err := repository.controls.RequireQuotaAtMost(
		queryContext, transaction, access.TenantID,
		featurecontrol.QuotaFileBytesPerTenant,
		usage.ReservedBytes+usage.CommittedBytes+command.ExpectedSizeBytes,
	); err != nil {
		return CreateIntentResult{}, err
	}
	if _, err := repository.controls.ConsumeRateQuota(
		queryContext, transaction, access.TenantID,
		featurecontrol.QuotaFileUploadIntentsPerHour, command.CreatedAt,
	); err != nil {
		return CreateIntentResult{}, err
	}
	row, err := scanFileRow(transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.content_files (
    id, tenant_id, class_id, creator_user_id, client_request_id,
    request_fingerprint, object_key, display_name, declared_media_type,
    expected_size_bytes, expected_checksum_sha256, upload_expires_at,
    created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $13)
RETURNING `+fileReturning,
		command.ID, access.TenantID, command.ClassID, access.ActorID,
		command.ClientRequestID, command.RequestFingerprint, command.ObjectKey,
		command.DisplayName, command.DeclaredMediaType, command.ExpectedSizeBytes,
		command.ChecksumSHA256, command.UploadExpiresAt, command.CreatedAt,
	))
	if err != nil {
		return CreateIntentResult{}, fmt.Errorf("insert upload intent: %w", err)
	}
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.tenant_file_usage
SET file_count = file_count + 1,
    reserved_bytes = reserved_bytes + $2,
    updated_at = $3
WHERE tenant_id = $1`,
		access.TenantID, command.ExpectedSizeBytes, command.CreatedAt,
	); err != nil {
		return CreateIntentResult{}, fmt.Errorf("reserve upload intent quota: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return CreateIntentResult{}, fmt.Errorf("commit upload intent creation: %w", err)
	}
	return CreateIntentResult{File: projectFile(row), Created: true}, nil
}

func (repository *PostgresRepository) replayIntent(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	command CreateCommand,
	existing fileRow,
) (CreateIntentResult, error) {
	if err := repository.authorizeClass(
		ctx, transaction, access, existing.ClassID, policy.ActionFileUpload,
	); err != nil {
		return CreateIntentResult{}, err
	}
	if existing.DeletedAt.Valid && existing.DeletionReason.String == "intent_expired" {
		return CreateIntentResult{}, ErrIntentExpired
	}
	if !bytes.Equal(existing.RequestFingerprint, command.RequestFingerprint) {
		return CreateIntentResult{}, ErrIdempotencyConflict
	}
	if existing.Status == StatusPending && !command.CreatedAt.Before(existing.UploadExpiresAt) {
		if err := expireFile(ctx, transaction, existing, command.CreatedAt); err != nil {
			return CreateIntentResult{}, err
		}
		if err := transaction.Commit(ctx); err != nil {
			return CreateIntentResult{}, fmt.Errorf("commit expired upload intent: %w", err)
		}
		return CreateIntentResult{}, ErrIntentExpired
	}
	if existing.DeletedAt.Valid {
		return CreateIntentResult{}, ErrNotFound
	}
	if err := transaction.Commit(ctx); err != nil {
		return CreateIntentResult{}, fmt.Errorf("commit upload intent replay: %w", err)
	}
	return CreateIntentResult{File: projectFile(existing), Created: false}, nil
}

func (repository *PostgresRepository) Get(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
) (File, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return File{}, fmt.Errorf("begin file metadata query: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return File{}, err
	}
	row, err := loadFile(queryContext, transaction, access.TenantID, fileID, false)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.DeletedAt.Valid) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, fmt.Errorf("query file metadata: %w", err)
	}
	if err := repository.authorizeClass(
		queryContext, transaction, access, row.ClassID, policy.ActionFileView,
	); err != nil {
		return File{}, err
	}
	if row.Status != StatusReady && row.CreatorUserID != access.ActorID {
		if err := repository.authorizeClassAsActive(
			queryContext, transaction, access, row.ClassID, policy.ActionFileUpload,
		); err != nil {
			return File{}, ErrNotFound
		}
	}
	if err := transaction.Commit(queryContext); err != nil {
		return File{}, fmt.Errorf("commit file metadata query: %w", err)
	}
	return projectFile(row), nil
}

func (repository *PostgresRepository) PrepareUpload(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	expectedVersion int64,
	now time.Time,
) (UploadTarget, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return UploadTarget{}, fmt.Errorf("begin file upload capability preparation: %w", err)
	}
	defer rollback(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureFileUploads,
	); err != nil {
		return UploadTarget{}, err
	}
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return UploadTarget{}, err
	}
	row, err := loadFile(queryContext, transaction, access.TenantID, fileID, true)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.DeletedAt.Valid) {
		return UploadTarget{}, ErrNotFound
	}
	if err != nil {
		return UploadTarget{}, fmt.Errorf("lock file upload capability target: %w", err)
	}
	if row.CreatorUserID != access.ActorID {
		return UploadTarget{}, ErrNotFound
	}
	if err := repository.authorizeClass(
		queryContext, transaction, access, row.ClassID, policy.ActionFileUpload,
	); err != nil {
		return UploadTarget{}, err
	}
	if row.Status != StatusPending || row.Version != expectedVersion {
		return UploadTarget{}, ErrVersionConflict
	}
	if !now.Before(row.UploadExpiresAt) {
		if err := expireFile(queryContext, transaction, row, now); err != nil {
			return UploadTarget{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return UploadTarget{}, fmt.Errorf("commit expired file upload capability: %w", err)
		}
		return UploadTarget{}, ErrIntentExpired
	}
	if err := transaction.Commit(queryContext); err != nil {
		return UploadTarget{}, fmt.Errorf("commit file upload capability preparation: %w", err)
	}
	return UploadTarget{File: projectFile(row), ObjectKey: row.ObjectKey}, nil
}

func (repository *PostgresRepository) PrepareDownload(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
) (DownloadTarget, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return DownloadTarget{}, fmt.Errorf("begin file download capability preparation: %w", err)
	}
	defer rollback(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureFileUploads,
	); err != nil {
		return DownloadTarget{}, err
	}
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return DownloadTarget{}, err
	}
	row, err := loadFile(queryContext, transaction, access.TenantID, fileID, false)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.DeletedAt.Valid) {
		return DownloadTarget{}, ErrNotFound
	}
	if err != nil {
		return DownloadTarget{}, fmt.Errorf("query file download capability target: %w", err)
	}
	if err := repository.authorizeClass(
		queryContext, transaction, access, row.ClassID, policy.ActionFileView,
	); err != nil {
		return DownloadTarget{}, err
	}
	if row.Status != StatusReady {
		if row.CreatorUserID == access.ActorID {
			return DownloadTarget{}, ErrNotReady
		}
		if err := repository.authorizeClassAsActive(
			queryContext, transaction, access, row.ClassID, policy.ActionFileUpload,
		); err != nil {
			return DownloadTarget{}, ErrNotFound
		}
		return DownloadTarget{}, ErrNotReady
	}
	if !row.StorageVersionID.Valid || strings.TrimSpace(row.StorageVersionID.String) == "" ||
		!row.StoredMediaType.Valid || strings.TrimSpace(row.StoredMediaType.String) == "" {
		return DownloadTarget{}, ErrStorageUnavailable
	}
	if err := transaction.Commit(queryContext); err != nil {
		return DownloadTarget{}, fmt.Errorf("commit file download capability preparation: %w", err)
	}
	return DownloadTarget{
		File: projectFile(row), ObjectKey: row.ObjectKey,
		VersionID:       strings.TrimSpace(row.StorageVersionID.String),
		StoredMediaType: strings.TrimSpace(row.StoredMediaType.String),
	}, nil
}

func (repository *PostgresRepository) PrepareFinalize(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	expectedVersion int64,
	now time.Time,
) (FinalizeTarget, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return FinalizeTarget{}, fmt.Errorf("begin file finalize preparation: %w", err)
	}
	defer rollback(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureFileUploads,
	); err != nil {
		return FinalizeTarget{}, err
	}
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return FinalizeTarget{}, err
	}
	row, err := loadFile(queryContext, transaction, access.TenantID, fileID, true)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.DeletedAt.Valid) {
		return FinalizeTarget{}, ErrNotFound
	}
	if err != nil {
		return FinalizeTarget{}, fmt.Errorf("lock file finalize target: %w", err)
	}
	if row.CreatorUserID != access.ActorID {
		return FinalizeTarget{}, ErrNotFound
	}
	if err := repository.authorizeClass(
		queryContext, transaction, access, row.ClassID, policy.ActionFileUpload,
	); err != nil {
		return FinalizeTarget{}, err
	}
	if row.Status == StatusUploaded {
		if expectedVersion != row.Version && expectedVersion+1 != row.Version {
			return FinalizeTarget{}, ErrVersionConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return FinalizeTarget{}, fmt.Errorf("commit uploaded file replay: %w", err)
		}
		return finalizeTarget(row), nil
	}
	if row.Status != StatusPending || expectedVersion != row.Version {
		return FinalizeTarget{}, ErrVersionConflict
	}
	if !now.Before(row.UploadExpiresAt) {
		if err := expireFile(queryContext, transaction, row, now); err != nil {
			return FinalizeTarget{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return FinalizeTarget{}, fmt.Errorf("commit expired file finalize: %w", err)
		}
		return FinalizeTarget{}, ErrIntentExpired
	}
	if err := transaction.Commit(queryContext); err != nil {
		return FinalizeTarget{}, fmt.Errorf("commit file finalize preparation: %w", err)
	}
	return finalizeTarget(row), nil
}

func (repository *PostgresRepository) CommitFinalize(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	proof FinalizeProof,
) (File, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return File{}, fmt.Errorf("begin file finalize commit: %w", err)
	}
	defer rollback(transaction)
	if err := repository.controls.RequireFeature(
		queryContext, transaction, access.TenantID, featurecontrol.FeatureFileUploads,
	); err != nil {
		return File{}, err
	}
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return File{}, err
	}
	row, err := loadFile(queryContext, transaction, access.TenantID, fileID, true)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.DeletedAt.Valid) {
		return File{}, ErrNotFound
	}
	if err != nil {
		return File{}, fmt.Errorf("lock file finalize commit: %w", err)
	}
	if row.CreatorUserID != access.ActorID {
		return File{}, ErrNotFound
	}
	if err := repository.authorizeClass(
		queryContext, transaction, access, row.ClassID, policy.ActionFileUpload,
	); err != nil {
		return File{}, err
	}
	if row.Status == StatusUploaded {
		if (proof.ExpectedVersion != row.Version && proof.ExpectedVersion+1 != row.Version) ||
			!storedProofMatches(row, proof) {
			return File{}, ErrVersionConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return File{}, fmt.Errorf("commit concurrent file finalize replay: %w", err)
		}
		return projectFile(row), nil
	}
	if row.Status != StatusPending || row.Version != proof.ExpectedVersion {
		return File{}, ErrVersionConflict
	}
	if !proof.FinalizedAt.Before(row.UploadExpiresAt) {
		if err := expireFile(queryContext, transaction, row, proof.FinalizedAt); err != nil {
			return File{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return File{}, fmt.Errorf("commit expired file finalize proof: %w", err)
		}
		return File{}, ErrIntentExpired
	}
	if proof.ContentLength != row.ExpectedSizeBytes || proof.ContentType != row.DeclaredMediaType ||
		proof.ETag == "" || proof.VersionID == "" {
		return File{}, ErrStorageMismatch
	}
	commandTag, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.tenant_file_usage
SET reserved_bytes = reserved_bytes - $2,
    committed_bytes = committed_bytes + $2,
    updated_at = $3
WHERE tenant_id = $1 AND reserved_bytes >= $2`,
		access.TenantID, row.ExpectedSizeBytes, proof.FinalizedAt,
	)
	if err != nil {
		return File{}, fmt.Errorf("commit finalized file quota: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return File{}, fmt.Errorf("commit finalized file quota: usage invariant failed")
	}
	updated, err := scanFileRow(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.content_files
SET status = 'uploaded',
    version = version + 1,
    stored_size_bytes = $3,
    stored_media_type = $4,
    storage_etag = $5,
    storage_version_id = $6,
    uploaded_at = $7,
    updated_at = $7
WHERE tenant_id = $1 AND id = $2 AND status = 'pending' AND version = $8
RETURNING `+fileReturning,
		access.TenantID, fileID, proof.ContentLength, proof.ContentType,
		proof.ETag, proof.VersionID, proof.FinalizedAt,
		proof.ExpectedVersion,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, ErrVersionConflict
	}
	if err != nil {
		return File{}, fmt.Errorf("persist finalized file proof: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return File{}, fmt.Errorf("commit file finalize: %w", err)
	}
	return projectFile(updated), nil
}

type fileUsage struct {
	FileCount      int64
	ReservedBytes  int64
	CommittedBytes int64
}

func lockFileUsage(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	now time.Time,
) (fileUsage, error) {
	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.tenant_file_usage (tenant_id, updated_at)
VALUES ($1, $2)
ON CONFLICT (tenant_id) DO NOTHING`,
		tenantID, now,
	); err != nil {
		return fileUsage{}, fmt.Errorf("initialize tenant file usage: %w", err)
	}
	var usage fileUsage
	if err := transaction.QueryRow(
		ctx,
		`SELECT file_count, reserved_bytes, committed_bytes
FROM tutorhub.tenant_file_usage
WHERE tenant_id = $1
FOR UPDATE`,
		tenantID,
	).Scan(&usage.FileCount, &usage.ReservedBytes, &usage.CommittedBytes); err != nil {
		return fileUsage{}, fmt.Errorf("lock tenant file usage: %w", err)
	}
	return usage, nil
}

func expirePendingFiles(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID uuid.UUID,
	now time.Time,
) error {
	var count, bytesReleased int64
	if err := transaction.QueryRow(
		ctx,
		`WITH expired AS (
    UPDATE tutorhub.content_files
    SET deleted_at = $2,
        deletion_reason = 'intent_expired',
        version = version + 1,
        updated_at = $2
    WHERE tenant_id = $1
      AND status = 'pending'
      AND deleted_at IS NULL
      AND upload_expires_at <= $2
    RETURNING expected_size_bytes
)
SELECT count(*), COALESCE(sum(expected_size_bytes), 0) FROM expired`,
		tenantID, now,
	).Scan(&count, &bytesReleased); err != nil {
		return fmt.Errorf("expire pending file intents: %w", err)
	}
	if count == 0 {
		return nil
	}
	commandTag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.tenant_file_usage
SET file_count = file_count - $2,
    reserved_bytes = reserved_bytes - $3,
    updated_at = $4
WHERE tenant_id = $1
  AND file_count >= $2
  AND reserved_bytes >= $3`,
		tenantID, count, bytesReleased, now,
	)
	if err != nil {
		return fmt.Errorf("release expired file quota: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("release expired file quota: usage invariant failed")
	}
	return nil
}

func expireFile(
	ctx context.Context,
	transaction pgx.Tx,
	row fileRow,
	now time.Time,
) error {
	commandTag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.content_files
SET deleted_at = $3,
    deletion_reason = 'intent_expired',
    version = version + 1,
    updated_at = $3
WHERE tenant_id = $1 AND id = $2
  AND status = 'pending' AND deleted_at IS NULL`,
		row.TenantID, row.ID, now,
	)
	if err != nil {
		return fmt.Errorf("expire file upload intent: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return ErrVersionConflict
	}
	commandTag, err = transaction.Exec(
		ctx,
		`UPDATE tutorhub.tenant_file_usage
SET file_count = file_count - 1,
    reserved_bytes = reserved_bytes - $2,
    updated_at = $3
WHERE tenant_id = $1 AND file_count >= 1 AND reserved_bytes >= $2`,
		row.TenantID, row.ExpectedSizeBytes, now,
	)
	if err != nil {
		return fmt.Errorf("release expired upload intent quota: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("release expired upload intent quota: usage invariant failed")
	}
	return nil
}

type lockedClass struct {
	OwnerUserID uuid.UUID
	Status      policy.ResourceState
}

func (repository *PostgresRepository) authorizeClass(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) error {
	class, classRoles, err := lockClassAccess(ctx, transaction, access, classID)
	if err != nil {
		return err
	}
	return repository.authorizeClassResource(access, classID, class.Status, classRoles, action)
}

func (repository *PostgresRepository) authorizeClassAsActive(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	classID uuid.UUID,
	action policy.Action,
) error {
	_, classRoles, err := lockClassAccess(ctx, transaction, access, classID)
	if err != nil {
		return err
	}
	return repository.authorizeClassResource(
		access, classID, policy.ResourceStateActive, classRoles, action,
	)
}

func (repository *PostgresRepository) authorizeClassResource(
	access AccessContext,
	classID uuid.UUID,
	state policy.ResourceState,
	classRoles []policy.ClassRole,
	action policy.Action,
) error {
	subject := policy.Subject{
		ActorID: access.ActorID, ActiveTenantID: access.TenantID,
		MembershipActive:  access.MembershipActive,
		OrganizationRoles: append([]policy.OrganizationRole(nil), access.OrganizationRoles...),
		ClassRoles:        append([]policy.ClassRole(nil), classRoles...),
	}
	decision := repository.authorizer.Authorize(policy.Input{
		Subject: subject, Action: action,
		Resource: policy.Resource{TenantID: access.TenantID, ClassID: classID, State: state},
	})
	if decision.Allowed {
		return nil
	}
	if decision.Reason == policy.DenialResourceState && action == policy.ActionFileUpload {
		return ErrReadOnly
	}
	return ErrNotFound
}

func lockClassAccess(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	classID uuid.UUID,
) (lockedClass, []policy.ClassRole, error) {
	var class lockedClass
	if err := transaction.QueryRow(
		ctx,
		`SELECT owner_user_id, status
FROM tutorhub.classes
WHERE tenant_id = $1 AND id = $2
FOR SHARE`,
		access.TenantID, classID,
	).Scan(&class.OwnerUserID, &class.Status); errors.Is(err, pgx.ErrNoRows) {
		return lockedClass{}, nil, ErrNotFound
	} else if err != nil {
		return lockedClass{}, nil, fmt.Errorf("lock file class: %w", err)
	}
	roles := make([]policy.ClassRole, 0, 2)
	if class.OwnerUserID == access.ActorID {
		roles = append(roles, policy.ClassRoleOwner)
	}
	var role policy.ClassRole
	var status string
	err := transaction.QueryRow(
		ctx,
		`SELECT class_role, status
FROM tutorhub.class_enrollments
WHERE tenant_id = $1 AND class_id = $2 AND user_id = $3
FOR SHARE`,
		access.TenantID, classID, access.ActorID,
	).Scan(&role, &status)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return lockedClass{}, nil, fmt.Errorf("lock file class enrollment: %w", err)
	}
	if err == nil && status == "active" {
		roles = append(roles, role)
	}
	return class, roles, nil
}

func (repository *PostgresRepository) requireActiveScope(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
) (AccessContext, error) {
	if access.TenantID == uuid.Nil || access.ActorID == uuid.Nil {
		return AccessContext{}, ErrAccessDenied
	}
	var role policy.OrganizationRole
	if err := transaction.QueryRow(
		ctx,
		`SELECT member.role
FROM tutorhub.tenants AS tenant
JOIN tutorhub.memberships AS member ON member.tenant_id = tenant.id
JOIN tutorhub.users AS tutor ON tutor.id = member.user_id
WHERE tenant.id = $1 AND member.user_id = $2
  AND tenant.status = 'active'
  AND member.status = 'active'
  AND tutor.status = 'active'`,
		access.TenantID, access.ActorID,
	).Scan(&role); errors.Is(err, pgx.ErrNoRows) {
		return AccessContext{}, ErrAccessDenied
	} else if err != nil {
		return AccessContext{}, fmt.Errorf("authorize file scope: %w", err)
	}
	access.MembershipActive = true
	access.OrganizationRoles = []policy.OrganizationRole{role}
	return access, nil
}

type fileRow struct {
	ID                     uuid.UUID
	TenantID               uuid.UUID
	ClassID                uuid.UUID
	CreatorUserID          uuid.UUID
	ClientRequestID        uuid.UUID
	RequestFingerprint     []byte
	ObjectKey              string
	DisplayName            string
	DeclaredMediaType      string
	ExpectedSizeBytes      int64
	ExpectedChecksumSHA256 []byte
	Status                 Status
	Version                int64
	UploadExpiresAt        time.Time
	StoredSizeBytes        sql.NullInt64
	StoredMediaType        sql.NullString
	StoredChecksumSHA256   []byte
	StorageETag            sql.NullString
	StorageVersionID       sql.NullString
	UploadedAt             sql.NullTime
	CreatedAt              time.Time
	UpdatedAt              time.Time
	DeletedAt              sql.NullTime
	DeletionReason         sql.NullString
}

const fileReturning = `id, tenant_id, class_id, creator_user_id, client_request_id,
request_fingerprint, object_key, display_name, declared_media_type,
expected_size_bytes, expected_checksum_sha256, status, version, upload_expires_at,
stored_size_bytes, stored_media_type, stored_checksum_sha256, storage_etag,
storage_version_id, uploaded_at, created_at, updated_at, deleted_at, deletion_reason`

func loadFile(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, fileID uuid.UUID,
	lock bool,
) (fileRow, error) {
	query := `SELECT ` + fileReturning + `
FROM tutorhub.content_files
WHERE tenant_id = $1 AND id = $2`
	if lock {
		query += " FOR UPDATE"
	}
	return scanFileRow(transaction.QueryRow(ctx, query, tenantID, fileID))
}

func loadFileByClientRequest(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, actorID, clientRequestID uuid.UUID,
	lock bool,
) (fileRow, error) {
	query := `SELECT ` + fileReturning + `
FROM tutorhub.content_files
WHERE tenant_id = $1 AND creator_user_id = $2 AND client_request_id = $3`
	if lock {
		query += " FOR UPDATE"
	}
	return scanFileRow(transaction.QueryRow(ctx, query, tenantID, actorID, clientRequestID))
}

type rowScanner interface {
	Scan(...any) error
}

func scanFileRow(row rowScanner) (fileRow, error) {
	var item fileRow
	err := row.Scan(
		&item.ID, &item.TenantID, &item.ClassID, &item.CreatorUserID,
		&item.ClientRequestID, &item.RequestFingerprint, &item.ObjectKey,
		&item.DisplayName, &item.DeclaredMediaType, &item.ExpectedSizeBytes,
		&item.ExpectedChecksumSHA256, &item.Status, &item.Version,
		&item.UploadExpiresAt, &item.StoredSizeBytes, &item.StoredMediaType,
		&item.StoredChecksumSHA256, &item.StorageETag, &item.StorageVersionID,
		&item.UploadedAt, &item.CreatedAt, &item.UpdatedAt, &item.DeletedAt,
		&item.DeletionReason,
	)
	return item, err
}

func projectFile(row fileRow) File {
	item := File{
		ID: row.ID, ClassID: row.ClassID, CreatorUserID: row.CreatorUserID,
		DisplayName: row.DisplayName, DeclaredMediaType: row.DeclaredMediaType,
		ExpectedSizeBytes:      row.ExpectedSizeBytes,
		ExpectedChecksumSHA256: hex.EncodeToString(row.ExpectedChecksumSHA256),
		Status:                 row.Status, Version: row.Version, UploadExpiresAt: row.UploadExpiresAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.UploadedAt.Valid {
		uploadedAt := row.UploadedAt.Time
		item.UploadedAt = &uploadedAt
	}
	return item
}

func finalizeTarget(row fileRow) FinalizeTarget {
	target := FinalizeTarget{
		File: projectFile(row), ObjectKey: row.ObjectKey,
	}
	if row.StorageVersionID.Valid {
		target.StorageVersionID = strings.TrimSpace(row.StorageVersionID.String)
	}
	return target
}

func storedProofMatches(row fileRow, proof FinalizeProof) bool {
	return row.StoredSizeBytes.Valid && row.StoredSizeBytes.Int64 == proof.ContentLength &&
		row.StoredMediaType.Valid && row.StoredMediaType.String == proof.ContentType &&
		len(row.StoredChecksumSHA256) == 0 &&
		row.StorageETag.Valid && row.StorageETag.String == proof.ETag &&
		row.StorageVersionID.Valid && row.StorageVersionID.String == proof.VersionID
}

func rollback(transaction pgx.Tx) {
	_ = transaction.Rollback(context.Background())
}
