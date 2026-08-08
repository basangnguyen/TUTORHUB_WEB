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
const fileExpectedVersionUnknown int64 = 0

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
	return CreateIntentResult{File: projectFile(row, access, true), Created: true}, nil
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
	return CreateIntentResult{File: projectFile(existing, access, true), Created: false}, nil
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
	class, classRoles, err := lockClassAccess(queryContext, transaction, access, row.ClassID)
	if err != nil {
		return File{}, err
	}
	if err := repository.authorizeClassResource(
		access, row.ClassID, class.Status, classRoles, policy.ActionFileView,
	); err != nil {
		return File{}, err
	}
	canManageTransfer := repository.authorizeClassResource(
		access, row.ClassID, policy.ResourceStateActive, classRoles, policy.ActionFileUpload,
	) == nil
	canUpload := repository.authorizeClassResource(
		access, row.ClassID, class.Status, classRoles, policy.ActionFileUpload,
	) == nil
	if row.Status != StatusReady && row.CreatorUserID != access.ActorID && !canManageTransfer {
		return File{}, ErrNotFound
	}
	if err := transaction.Commit(queryContext); err != nil {
		return File{}, fmt.Errorf("commit file metadata query: %w", err)
	}
	return projectFile(row, access, canUpload), nil
}

func (repository *PostgresRepository) List(
	ctx context.Context,
	access AccessContext,
	params ListFilesParams,
) (ListFilesResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	if params.ClassID == uuid.Nil || params.Limit < 1 || params.Limit > maximumFileListLimit ||
		(params.After != nil && (params.After.ID == uuid.Nil || params.After.CreatedAt.IsZero())) {
		return ListFilesResult{}, ErrInvalidInput
	}
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return ListFilesResult{}, fmt.Errorf("begin class file list: %w", err)
	}
	defer rollback(transaction)
	access, err = repository.requireActiveScope(queryContext, transaction, access)
	if err != nil {
		return ListFilesResult{}, err
	}
	class, classRoles, err := lockClassAccess(queryContext, transaction, access, params.ClassID)
	if err != nil {
		return ListFilesResult{}, err
	}
	if err := repository.authorizeClassResource(
		access, params.ClassID, class.Status, classRoles, policy.ActionFileView,
	); err != nil {
		return ListFilesResult{}, err
	}
	canManageTransfer := repository.authorizeClassResource(
		access, params.ClassID, policy.ResourceStateActive, classRoles, policy.ActionFileUpload,
	) == nil
	canUpload := repository.authorizeClassResource(
		access, params.ClassID, class.Status, classRoles, policy.ActionFileUpload,
	) == nil
	query := `SELECT ` + fileReturning + `
FROM tutorhub.content_files
WHERE tenant_id = $1 AND class_id = $2 AND deleted_at IS NULL
  AND ($3::boolean OR status = 'ready' OR creator_user_id = $4)`
	arguments := []any{access.TenantID, params.ClassID, canManageTransfer, access.ActorID}
	if params.After != nil {
		query += " AND (created_at, id) < ($5, $6)"
		arguments = append(arguments, params.After.CreatedAt, params.After.ID)
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d", len(arguments)+1)
	arguments = append(arguments, params.Limit+1)
	rows, err := transaction.Query(queryContext, query, arguments...)
	if err != nil {
		return ListFilesResult{}, fmt.Errorf("query class files: %w", err)
	}
	defer rows.Close()
	items := make([]File, 0, params.Limit)
	for rows.Next() {
		row, err := scanFileRow(rows)
		if err != nil {
			return ListFilesResult{}, fmt.Errorf("scan class file: %w", err)
		}
		if len(items) < params.Limit {
			items = append(items, projectFile(row, access, canUpload))
		}
	}
	if err := rows.Err(); err != nil {
		return ListFilesResult{}, fmt.Errorf("iterate class files: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return ListFilesResult{}, fmt.Errorf("commit class file list: %w", err)
	}
	return ListFilesResult{
		Items: items, HasMore: rows.CommandTag().RowsAffected() > int64(params.Limit),
		CanUpload: canUpload,
	}, nil
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
	var multipartExists bool
	if err := transaction.QueryRow(
		queryContext,
		`SELECT EXISTS (
    SELECT 1
    FROM tutorhub.content_file_multipart_uploads
    WHERE tenant_id = $1 AND file_id = $2 AND status IN ('active', 'completing')
)`,
		access.TenantID, fileID,
	).Scan(&multipartExists); err != nil {
		return UploadTarget{}, fmt.Errorf("check active multipart upload: %w", err)
	}
	if multipartExists {
		return UploadTarget{}, ErrMultipartConflict
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
	return UploadTarget{File: projectFile(row, access, true), ObjectKey: row.ObjectKey}, nil
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
		File: projectFile(row, access, true), ObjectKey: row.ObjectKey,
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
		return projectFile(row, access, true), nil
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
	return projectFile(updated, access, true), nil
}

func (repository *PostgresRepository) PrepareMultipartCreate(
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
		return UploadTarget{}, fmt.Errorf("begin multipart creation preparation: %w", err)
	}
	defer rollback(transaction)
	access, row, err := repository.lockOwnedPendingFile(
		queryContext, transaction, access, fileID, expectedVersion, now,
	)
	if err != nil {
		return UploadTarget{}, err
	}
	if !now.Before(row.UploadExpiresAt) {
		return UploadTarget{}, ErrIntentExpired
	}
	var exists bool
	if err := transaction.QueryRow(
		queryContext,
		`SELECT EXISTS (
    SELECT 1 FROM tutorhub.content_file_multipart_uploads
    WHERE tenant_id = $1 AND file_id = $2 AND status IN ('active', 'completing')
)`, access.TenantID, fileID,
	).Scan(&exists); err != nil {
		return UploadTarget{}, fmt.Errorf("check multipart ownership: %w", err)
	}
	if exists {
		return UploadTarget{}, ErrMultipartConflict
	}
	if err := transaction.Commit(queryContext); err != nil {
		return UploadTarget{}, fmt.Errorf("commit multipart creation preparation: %w", err)
	}
	return UploadTarget{File: projectFile(row, access, true), ObjectKey: row.ObjectKey}, nil
}

func (repository *PostgresRepository) CreateMultipart(
	ctx context.Context,
	access AccessContext,
	command MultipartCreateCommand,
) (MultipartUpload, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("begin multipart ownership creation: %w", err)
	}
	defer rollback(transaction)
	access, row, err := repository.lockOwnedPendingFile(
		queryContext, transaction, access, command.FileID, command.ExpectedVersion, command.CreatedAt,
	)
	if err != nil {
		return MultipartUpload{}, err
	}
	if !command.CreatedAt.Before(row.UploadExpiresAt) {
		return MultipartUpload{}, ErrIntentExpired
	}
	if command.ID == uuid.Nil || command.ProviderUploadID == "" ||
		command.ExpiresAt != row.UploadExpiresAt || !command.CreatedAt.Before(command.ExpiresAt) {
		return MultipartUpload{}, ErrInvalidInput
	}
	var exists bool
	if err := transaction.QueryRow(
		queryContext,
		`SELECT EXISTS (
    SELECT 1 FROM tutorhub.content_file_multipart_uploads
    WHERE tenant_id = $1 AND file_id = $2 AND status IN ('active', 'completing')
)`, access.TenantID, command.FileID,
	).Scan(&exists); err != nil {
		return MultipartUpload{}, fmt.Errorf("recheck multipart ownership: %w", err)
	}
	if exists {
		return MultipartUpload{}, ErrMultipartConflict
	}
	multipart, err := scanMultipartUploadRow(transaction.QueryRow(
		queryContext,
		`INSERT INTO tutorhub.content_file_multipart_uploads (
    id, tenant_id, file_id, creator_user_id, provider_upload_id,
    expected_file_version, expires_at, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
RETURNING `+multipartUploadReturning,
		command.ID, access.TenantID, command.FileID, access.ActorID,
		command.ProviderUploadID, command.ExpectedVersion, command.ExpiresAt, command.CreatedAt,
	))
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("persist multipart ownership: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MultipartUpload{}, fmt.Errorf("commit multipart ownership: %w", err)
	}
	return projectMultipartUpload(multipart), nil
}

func (repository *PostgresRepository) PrepareMultipartPart(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
	expectedVersion int64,
	partNumber int32,
	contentLength int64,
	now time.Time,
) (MultipartTarget, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("begin multipart part preparation: %w", err)
	}
	defer rollback(transaction)
	access, file, err := repository.lockOwnedPendingFile(
		queryContext, transaction, access, fileID, expectedVersion, now,
	)
	if err != nil {
		return MultipartTarget{}, err
	}
	upload, err := loadMultipartUpload(
		queryContext, transaction, access.TenantID, fileID, multipartID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartTarget{}, ErrNotFound
	}
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("lock multipart part owner: %w", err)
	}
	if upload.CreatorUserID != access.ActorID || upload.ExpectedFileVersion != expectedVersion {
		return MultipartTarget{}, ErrNotFound
	}
	if upload.Status != MultipartStatusActive {
		return MultipartTarget{}, ErrMultipartConflict
	}
	if !now.Before(upload.ExpiresAt) {
		if err := expireMultipartUpload(queryContext, transaction, upload, now); err != nil {
			return MultipartTarget{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return MultipartTarget{}, fmt.Errorf("commit expired multipart upload: %w", err)
		}
		return MultipartTarget{}, ErrMultipartExpired
	}
	if _, err := transaction.Exec(
		queryContext,
		`INSERT INTO tutorhub.content_file_multipart_parts (
    tenant_id, multipart_upload_id, part_number, content_length_bytes, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $5)
ON CONFLICT (tenant_id, multipart_upload_id, part_number) DO NOTHING`,
		access.TenantID, multipartID, partNumber, contentLength, now,
	); err != nil {
		return MultipartTarget{}, fmt.Errorf("persist multipart part manifest: %w", err)
	}
	var storedLength int64
	if err := transaction.QueryRow(
		queryContext,
		`SELECT content_length_bytes
FROM tutorhub.content_file_multipart_parts
WHERE tenant_id = $1 AND multipart_upload_id = $2 AND part_number = $3`,
		access.TenantID, multipartID, partNumber,
	).Scan(&storedLength); err != nil {
		return MultipartTarget{}, fmt.Errorf("lock multipart part manifest: %w", err)
	}
	if storedLength != contentLength {
		return MultipartTarget{}, ErrMultipartConflict
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MultipartTarget{}, fmt.Errorf("commit multipart part preparation: %w", err)
	}
	return multipartTarget(upload, file), nil
}

func (repository *PostgresRepository) PrepareMultipartComplete(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
	expectedVersion int64,
	completedParts []MultipartCompletedPart,
	now time.Time,
) (MultipartTarget, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("begin multipart completion preparation: %w", err)
	}
	defer rollback(transaction)
	access, file, err := repository.lockOwnedPendingFile(
		queryContext, transaction, access, fileID, expectedVersion, now,
	)
	if err != nil {
		return MultipartTarget{}, err
	}
	upload, err := loadMultipartUpload(
		queryContext, transaction, access.TenantID, fileID, multipartID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartTarget{}, ErrNotFound
	}
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("lock multipart completion owner: %w", err)
	}
	if upload.CreatorUserID != access.ActorID || upload.ExpectedFileVersion != expectedVersion {
		return MultipartTarget{}, ErrNotFound
	}
	if upload.Status == MultipartStatusCompleted {
		if err := transaction.Commit(queryContext); err != nil {
			return MultipartTarget{}, fmt.Errorf("commit multipart completion replay: %w", err)
		}
		return multipartTarget(upload, file), nil
	}
	if upload.Status != MultipartStatusActive {
		return MultipartTarget{}, ErrMultipartConflict
	}
	if !now.Before(upload.ExpiresAt) {
		if err := expireMultipartUpload(queryContext, transaction, upload, now); err != nil {
			return MultipartTarget{}, err
		}
		if err := transaction.Commit(queryContext); err != nil {
			return MultipartTarget{}, fmt.Errorf("commit expired multipart completion: %w", err)
		}
		return MultipartTarget{}, ErrMultipartExpired
	}
	issued, err := loadMultipartParts(queryContext, transaction, access.TenantID, multipartID)
	if err != nil {
		return MultipartTarget{}, err
	}
	if len(issued) != len(completedParts) {
		return MultipartTarget{}, ErrMultipartConflict
	}
	for index := range issued {
		if issued[index].PartNumber != completedParts[index].PartNumber {
			return MultipartTarget{}, ErrMultipartConflict
		}
	}
	commandTag, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.content_file_multipart_uploads
SET status = 'completing', updated_at = $4
WHERE tenant_id = $1 AND file_id = $2 AND id = $3 AND status = 'active'`,
		access.TenantID, fileID, multipartID, now,
	)
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("claim multipart completion: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return MultipartTarget{}, ErrMultipartConflict
	}
	upload.Status = MultipartStatusCompleting
	upload.UpdatedAt = now
	if err := transaction.Commit(queryContext); err != nil {
		return MultipartTarget{}, fmt.Errorf("commit multipart completion preparation: %w", err)
	}
	target := multipartTarget(upload, file)
	target.IssuedParts = issued
	return target, nil
}

func (repository *PostgresRepository) ReleaseMultipartComplete(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
) error {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return fmt.Errorf("begin multipart completion release: %w", err)
	}
	defer rollback(transaction)
	if _, err := transaction.Exec(
		queryContext,
		`UPDATE tutorhub.content_file_multipart_uploads
SET status = 'active', updated_at = now()
WHERE tenant_id = $1 AND file_id = $2 AND id = $3
  AND creator_user_id = $4 AND status = 'completing'`,
		access.TenantID, fileID, multipartID, access.ActorID,
	); err != nil {
		return fmt.Errorf("release multipart completion: %w", err)
	}
	return transaction.Commit(queryContext)
}

func (repository *PostgresRepository) CommitMultipartComplete(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
	proof MultipartCompleteProof,
) (MultipartTarget, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("begin multipart completion commit: %w", err)
	}
	defer rollback(transaction)
	access, file, err := repository.lockOwnedPendingFile(
		queryContext, transaction, access, fileID, fileExpectedVersionUnknown, proof.CompletedAt,
	)
	if err != nil {
		return MultipartTarget{}, err
	}
	upload, err := loadMultipartUpload(
		queryContext, transaction, access.TenantID, fileID, multipartID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartTarget{}, ErrNotFound
	}
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("lock multipart completion commit: %w", err)
	}
	if upload.CreatorUserID != access.ActorID {
		return MultipartTarget{}, ErrNotFound
	}
	if upload.Status == MultipartStatusCompleted {
		if upload.CompletedVersionID.String != proof.VersionID || upload.CompletedETag.String != proof.ETag {
			return MultipartTarget{}, ErrMultipartConflict
		}
		if err := transaction.Commit(queryContext); err != nil {
			return MultipartTarget{}, fmt.Errorf("commit multipart proof replay: %w", err)
		}
		return multipartTarget(upload, file), nil
	}
	if upload.Status != MultipartStatusCompleting || upload.ExpectedFileVersion != file.Version ||
		proof.VersionID == "" || proof.ETag == "" || !proof.CompletedAt.Before(upload.ExpiresAt) {
		return MultipartTarget{}, ErrMultipartConflict
	}
	updated, err := scanMultipartUploadRow(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.content_file_multipart_uploads
SET status = 'completed', completed_storage_version_id = $4,
    completed_etag = $5, completed_at = $6, updated_at = $6
WHERE tenant_id = $1 AND file_id = $2 AND id = $3 AND status = 'completing'
RETURNING `+multipartUploadReturning,
		access.TenantID, fileID, multipartID, proof.VersionID, proof.ETag, proof.CompletedAt,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartTarget{}, ErrMultipartConflict
	}
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("persist multipart completion proof: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MultipartTarget{}, fmt.Errorf("commit multipart completion proof: %w", err)
	}
	return multipartTarget(updated, file), nil
}

func (repository *PostgresRepository) PrepareMultipartAbort(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
	expectedVersion int64,
	now time.Time,
) (MultipartTarget, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("begin multipart abort preparation: %w", err)
	}
	defer rollback(transaction)
	access, file, err := repository.lockOwnedPendingFile(
		queryContext, transaction, access, fileID, expectedVersion, now,
	)
	if err != nil {
		return MultipartTarget{}, err
	}
	upload, err := loadMultipartUpload(
		queryContext, transaction, access.TenantID, fileID, multipartID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartTarget{}, ErrNotFound
	}
	if err != nil {
		return MultipartTarget{}, fmt.Errorf("lock multipart abort owner: %w", err)
	}
	if upload.CreatorUserID != access.ActorID || upload.ExpectedFileVersion != expectedVersion {
		return MultipartTarget{}, ErrNotFound
	}
	if upload.Status == MultipartStatusCompleted || upload.Status == MultipartStatusCompleting {
		return MultipartTarget{}, ErrMultipartConflict
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MultipartTarget{}, fmt.Errorf("commit multipart abort preparation: %w", err)
	}
	return multipartTarget(upload, file), nil
}

func (repository *PostgresRepository) CommitMultipartAbort(
	ctx context.Context,
	access AccessContext,
	fileID uuid.UUID,
	multipartID uuid.UUID,
	status MultipartStatus,
	now time.Time,
) (MultipartUpload, error) {
	if status != MultipartStatusAborted && status != MultipartStatusExpired {
		return MultipartUpload{}, ErrInvalidInput
	}
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	transaction, err := repository.database.Begin(queryContext)
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("begin multipart abort commit: %w", err)
	}
	defer rollback(transaction)
	upload, err := loadMultipartUpload(
		queryContext, transaction, access.TenantID, fileID, multipartID, true,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartUpload{}, ErrNotFound
	}
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("lock multipart abort commit: %w", err)
	}
	if upload.CreatorUserID != access.ActorID {
		return MultipartUpload{}, ErrNotFound
	}
	if upload.Status == MultipartStatusAborted || upload.Status == MultipartStatusExpired {
		if err := transaction.Commit(queryContext); err != nil {
			return MultipartUpload{}, fmt.Errorf("commit multipart abort replay: %w", err)
		}
		return projectMultipartUpload(upload), nil
	}
	updated, err := scanMultipartUploadRow(transaction.QueryRow(
		queryContext,
		`UPDATE tutorhub.content_file_multipart_uploads
SET status = $4, aborted_at = $5, updated_at = $5
WHERE tenant_id = $1 AND file_id = $2 AND id = $3 AND status = 'active'
RETURNING `+multipartUploadReturning,
		access.TenantID, fileID, multipartID, status, now,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return MultipartUpload{}, ErrMultipartConflict
	}
	if err != nil {
		return MultipartUpload{}, fmt.Errorf("persist multipart abort: %w", err)
	}
	if err := transaction.Commit(queryContext); err != nil {
		return MultipartUpload{}, fmt.Errorf("commit multipart abort: %w", err)
	}
	return projectMultipartUpload(updated), nil
}

func (repository *PostgresRepository) lockOwnedPendingFile(
	ctx context.Context,
	transaction pgx.Tx,
	access AccessContext,
	fileID uuid.UUID,
	expectedVersion int64,
	_ time.Time,
) (AccessContext, fileRow, error) {
	if err := repository.controls.RequireFeature(
		ctx, transaction, access.TenantID, featurecontrol.FeatureFileUploads,
	); err != nil {
		return AccessContext{}, fileRow{}, err
	}
	access, err := repository.requireActiveScope(ctx, transaction, access)
	if err != nil {
		return AccessContext{}, fileRow{}, err
	}
	row, err := loadFile(ctx, transaction, access.TenantID, fileID, true)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.DeletedAt.Valid) {
		return AccessContext{}, fileRow{}, ErrNotFound
	}
	if err != nil {
		return AccessContext{}, fileRow{}, fmt.Errorf("lock multipart file: %w", err)
	}
	if row.CreatorUserID != access.ActorID {
		return AccessContext{}, fileRow{}, ErrNotFound
	}
	if err := repository.authorizeClass(
		ctx, transaction, access, row.ClassID, policy.ActionFileUpload,
	); err != nil {
		return AccessContext{}, fileRow{}, err
	}
	if row.Status != StatusPending ||
		(expectedVersion != fileExpectedVersionUnknown && row.Version != expectedVersion) {
		return AccessContext{}, fileRow{}, ErrVersionConflict
	}
	return access, row, nil
}

type multipartUploadRow struct {
	ID                  uuid.UUID
	TenantID            uuid.UUID
	FileID              uuid.UUID
	CreatorUserID       uuid.UUID
	ProviderUploadID    string
	ExpectedFileVersion int64
	Status              MultipartStatus
	ExpiresAt           time.Time
	CompletedVersionID  sql.NullString
	CompletedETag       sql.NullString
	CreatedAt           time.Time
	UpdatedAt           time.Time
	CompletedAt         sql.NullTime
	AbortedAt           sql.NullTime
}

const multipartUploadReturning = `id, tenant_id, file_id, creator_user_id,
provider_upload_id, expected_file_version, status, expires_at,
completed_storage_version_id, completed_etag, created_at, updated_at,
completed_at, aborted_at`

func loadMultipartUpload(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, fileID, multipartID uuid.UUID,
	lock bool,
) (multipartUploadRow, error) {
	query := `SELECT ` + multipartUploadReturning + `
FROM tutorhub.content_file_multipart_uploads
WHERE tenant_id = $1 AND file_id = $2 AND id = $3`
	if lock {
		query += " FOR UPDATE"
	}
	return scanMultipartUploadRow(transaction.QueryRow(ctx, query, tenantID, fileID, multipartID))
}

func scanMultipartUploadRow(row rowScanner) (multipartUploadRow, error) {
	var upload multipartUploadRow
	err := row.Scan(
		&upload.ID, &upload.TenantID, &upload.FileID, &upload.CreatorUserID,
		&upload.ProviderUploadID, &upload.ExpectedFileVersion, &upload.Status,
		&upload.ExpiresAt, &upload.CompletedVersionID, &upload.CompletedETag,
		&upload.CreatedAt, &upload.UpdatedAt, &upload.CompletedAt, &upload.AbortedAt,
	)
	return upload, err
}

func projectMultipartUpload(row multipartUploadRow) MultipartUpload {
	return MultipartUpload{
		ID: row.ID, FileID: row.FileID, Status: row.Status, ExpiresAt: row.ExpiresAt,
	}
}

func multipartTarget(upload multipartUploadRow, file fileRow) MultipartTarget {
	target := MultipartTarget{
		Upload: projectMultipartUpload(upload), File: projectFile(file, AccessContext{}, false),
		ObjectKey: file.ObjectKey, ProviderUploadID: upload.ProviderUploadID,
	}
	if upload.CompletedVersionID.Valid {
		target.CompletedVersionID = strings.TrimSpace(upload.CompletedVersionID.String)
	}
	if upload.CompletedETag.Valid {
		target.CompletedETag = strings.TrimSpace(upload.CompletedETag.String)
	}
	return target
}

func loadMultipartParts(
	ctx context.Context,
	transaction pgx.Tx,
	tenantID, multipartID uuid.UUID,
) ([]MultipartIssuedPart, error) {
	rows, err := transaction.Query(
		ctx,
		`SELECT part_number, content_length_bytes
FROM tutorhub.content_file_multipart_parts
WHERE tenant_id = $1 AND multipart_upload_id = $2
ORDER BY part_number`, tenantID, multipartID,
	)
	if err != nil {
		return nil, fmt.Errorf("lock multipart part manifest: %w", err)
	}
	defer rows.Close()
	parts := make([]MultipartIssuedPart, 0)
	for rows.Next() {
		var part MultipartIssuedPart
		if err := rows.Scan(&part.PartNumber, &part.ContentLengthBytes); err != nil {
			return nil, fmt.Errorf("scan multipart part manifest: %w", err)
		}
		parts = append(parts, part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read multipart part manifest: %w", err)
	}
	return parts, nil
}

func expireMultipartUpload(
	ctx context.Context,
	transaction pgx.Tx,
	upload multipartUploadRow,
	now time.Time,
) error {
	commandTag, err := transaction.Exec(
		ctx,
		`UPDATE tutorhub.content_file_multipart_uploads
SET status = 'expired', aborted_at = $4, updated_at = $4
WHERE tenant_id = $1 AND file_id = $2 AND id = $3 AND status = 'active'`,
		upload.TenantID, upload.FileID, upload.ID, now,
	)
	if err != nil {
		return fmt.Errorf("expire multipart upload: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return ErrMultipartConflict
	}
	return nil
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

func projectFile(row fileRow, access AccessContext, canUpload bool) File {
	item := File{
		ID: row.ID, ClassID: row.ClassID, CreatorUserID: row.CreatorUserID,
		DisplayName: row.DisplayName, DeclaredMediaType: row.DeclaredMediaType,
		ExpectedSizeBytes:      row.ExpectedSizeBytes,
		ExpectedChecksumSHA256: hex.EncodeToString(row.ExpectedChecksumSHA256),
		Status:                 row.Status, Version: row.Version, UploadExpiresAt: row.UploadExpiresAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		ViewerAccess: FileViewerAccess{
			CanDownload: row.Status == StatusReady,
			CanRetryUpload: canUpload && row.CreatorUserID == access.ActorID &&
				row.Status == StatusPending && time.Now().UTC().Before(row.UploadExpiresAt),
		},
	}
	if row.UploadedAt.Valid {
		uploadedAt := row.UploadedAt.Time
		item.UploadedAt = &uploadedAt
	}
	return item
}

func finalizeTarget(row fileRow) FinalizeTarget {
	target := FinalizeTarget{
		File: projectFile(row, AccessContext{}, false), ObjectKey: row.ObjectKey,
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
