package collaboration

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const defaultQueryTimeout = 10 * time.Second

type Database interface {
	Begin(context.Context) (pgx.Tx, error)
}

type PostgresRepository struct {
	database     Database
	queryTimeout time.Duration
	controls     featurecontrol.Enforcer
	quotas       featurecontrol.QuotaResolver
}

type PostgresRepositoryConfig struct {
	Controls featurecontrol.Enforcer
	Quotas   featurecontrol.QuotaResolver
}

func NewPostgresRepository(database Database, queryTimeout time.Duration, configs ...PostgresRepositoryConfig) (*PostgresRepository, error) {
	if database == nil {
		return nil, fmt.Errorf("whiteboard database is required")
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	var config PostgresRepositoryConfig
	if len(configs) > 0 {
		config = configs[0]
	}
	return &PostgresRepository{database: database, queryTimeout: queryTimeout, controls: config.Controls, quotas: config.Quotas}, nil
}

func (repository *PostgresRepository) Create(
	ctx context.Context,
	access AccessContext,
	command CreateCommand,
) (CreateResult, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	tx, err := repository.database.Begin(queryContext)
	if err != nil {
		return CreateResult{}, ErrUnavailable
	}
	defer rollback(tx)
	if err := repository.requireFeature(queryContext, tx, access.TenantID, false); err != nil {
		return CreateResult{}, err
	}
	if _, err := tx.Exec(queryContext,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"whiteboard-create:"+access.TenantID.String()+":"+access.ActorID.String()+":"+command.IdempotencyKey,
	); err != nil {
		return CreateResult{}, ErrUnavailable
	}

	replayed, fingerprint, found, err := loadCreateReplay(
		queryContext, tx, access.TenantID, access.ActorID, command.IdempotencyKey,
	)
	if err != nil {
		return CreateResult{}, ErrUnavailable
	}
	if found {
		if !bytes.Equal(fingerprint, command.Fingerprint) {
			return CreateResult{}, ErrIdempotencyConflict
		}
		if err := tx.Commit(queryContext); err != nil {
			return CreateResult{}, ErrUnavailable
		}
		return CreateResult{Document: replayed, Created: false}, nil
	}

	existing, err := loadDocumentBySpace(
		queryContext, tx, access.TenantID, command.MediaSpaceID, false,
	)
	if err == nil {
		if err := tx.Commit(queryContext); err != nil {
			return CreateResult{}, ErrUnavailable
		}
		return CreateResult{Document: existing, Created: false}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return CreateResult{}, ErrUnavailable
	}
	if repository.controls != nil {
		if _, err := tx.Exec(queryContext,
			`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
			"whiteboard-document-quota:"+access.TenantID.String(),
		); err != nil {
			return CreateResult{}, ErrUnavailable
		}
		var count int64
		if err := tx.QueryRow(queryContext, `SELECT count(*) FROM tutorhub.whiteboard_documents WHERE tenant_id = $1`, access.TenantID).Scan(&count); err != nil {
			return CreateResult{}, ErrUnavailable
		}
		if err := repository.controls.RequireQuotaAtMost(queryContext, tx, access.TenantID, featurecontrol.QuotaWhiteboardDocumentsPerTenant, count+1); err != nil {
			return CreateResult{}, classifyControlError(err)
		}
	}

	_, err = tx.Exec(queryContext, `INSERT INTO tutorhub.whiteboard_documents
        (id, tenant_id, media_space_id, create_idempotency_key,
         create_request_fingerprint, created_by, updated_by)
        VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		command.DocumentID, access.TenantID, command.MediaSpaceID, command.IdempotencyKey,
		command.Fingerprint, access.ActorID,
	)
	if err != nil {
		return CreateResult{}, classifyWriteError(err)
	}
	_, err = tx.Exec(queryContext, `INSERT INTO tutorhub.whiteboard_document_generations
		(tenant_id, document_id, generation, provider_document_name, reason, created_by)
		VALUES ($1, $2, 1, $3, 'initial', $4)`,
		access.TenantID, command.DocumentID, command.ProviderDocumentName,
		access.ActorID,
	)
	if err != nil {
		return CreateResult{}, classifyWriteError(err)
	}
	for audience, capability := range defaultCapabilityPolicies() {
		if _, err := tx.Exec(queryContext, `INSERT INTO tutorhub.whiteboard_capability_policies
			(tenant_id, document_id, audience, capability, created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, $5)`,
			access.TenantID, command.DocumentID, audience, capability,
			access.ActorID,
		); err != nil {
			return CreateResult{}, classifyWriteError(err)
		}
	}
	document, err := loadDocument(queryContext, tx, access.TenantID, command.DocumentID, false)
	if err != nil {
		return CreateResult{}, ErrUnavailable
	}
	if err := tx.Commit(queryContext); err != nil {
		return CreateResult{}, ErrUnavailable
	}
	return CreateResult{Document: document, Created: true}, nil
}

func (repository *PostgresRepository) Get(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
) (Document, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	tx, err := repository.database.Begin(queryContext)
	if err != nil {
		return Document{}, ErrUnavailable
	}
	defer rollback(tx)
	if err := repository.requireFeature(queryContext, tx, access.TenantID, true); err != nil {
		return Document{}, err
	}
	document, err := loadDocument(queryContext, tx, access.TenantID, documentID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, ErrUnavailable
	}
	if err := tx.Commit(queryContext); err != nil {
		return Document{}, ErrUnavailable
	}
	return document, nil
}

func (repository *PostgresRepository) GetByMediaSpace(
	ctx context.Context,
	access AccessContext,
	mediaSpaceID uuid.UUID,
) (Document, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	tx, err := repository.database.Begin(queryContext)
	if err != nil {
		return Document{}, ErrUnavailable
	}
	defer rollback(tx)
	if err := repository.requireFeature(queryContext, tx, access.TenantID, true); err != nil {
		return Document{}, err
	}
	document, err := loadDocumentBySpace(queryContext, tx, access.TenantID, mediaSpaceID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, ErrUnavailable
	}
	if err := tx.Commit(queryContext); err != nil {
		return Document{}, ErrUnavailable
	}
	return document, nil
}

func (repository *PostgresRepository) GrantAuthority(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
) (GrantAuthority, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	tx, err := repository.database.Begin(queryContext)
	if err != nil {
		return GrantAuthority{}, ErrUnavailable
	}
	defer rollback(tx)
	if err := repository.requireFeature(queryContext, tx, access.TenantID, true); err != nil {
		return GrantAuthority{}, err
	}
	const query = `SELECT document.id, document.media_space_id, document.status,
        document.version, document.current_generation, document.revoke_generation,
        document.created_at, document.updated_at, generation.provider_document_name
        FROM tutorhub.whiteboard_documents AS document
        JOIN tutorhub.whiteboard_document_generations AS generation
          ON generation.tenant_id = document.tenant_id
         AND generation.document_id = document.id
         AND generation.generation = document.current_generation
        WHERE document.tenant_id = $1 AND document.id = $2`
	var authority GrantAuthority
	err = tx.QueryRow(queryContext, query, access.TenantID, documentID).Scan(
		&authority.Document.ID, &authority.Document.MediaSpaceID,
		&authority.Document.Status, &authority.Document.Version,
		&authority.Document.CurrentGeneration, &authority.Document.RevokeGeneration,
		&authority.Document.CreatedAt, &authority.Document.UpdatedAt,
		&authority.ProviderDocumentName,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return GrantAuthority{}, ErrNotFound
	}
	if err != nil {
		return GrantAuthority{}, ErrUnavailable
	}
	authority.WriterFence = authority.Document.RevokeGeneration
	if repository.quotas != nil {
		connectionLimit, quotaErr := repository.quotas.ResolveQuota(queryContext, tx, access.TenantID, featurecontrol.QuotaWhiteboardConnectionsPerTenant)
		if quotaErr != nil {
			return GrantAuthority{}, classifyControlError(quotaErr)
		}
		operationLimit, quotaErr := repository.quotas.ResolveQuota(queryContext, tx, access.TenantID, featurecontrol.QuotaWhiteboardOperationsPerMinute)
		if quotaErr != nil {
			return GrantAuthority{}, classifyControlError(quotaErr)
		}
		storageLimit, quotaErr := repository.quotas.ResolveQuota(queryContext, tx, access.TenantID, featurecontrol.QuotaWhiteboardStorageBytesPerTenant)
		if quotaErr != nil {
			return GrantAuthority{}, classifyControlError(quotaErr)
		}
		authority.RuntimeLimits = RuntimeLimits{MaxConnectionsPerTenant: connectionLimit.Limit, MaxOperationsPerMinute: operationLimit.Limit, MaxStorageBytesPerTenant: storageLimit.Limit}
	}
	if err := tx.Commit(queryContext); err != nil {
		return GrantAuthority{}, ErrUnavailable
	}
	return authority, nil
}

func (repository *PostgresRepository) Transition(
	ctx context.Context,
	access AccessContext,
	command TransitionCommand,
) (Document, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	tx, err := repository.database.Begin(queryContext)
	if err != nil {
		return Document{}, ErrUnavailable
	}
	defer rollback(tx)
	if err := repository.requireFeature(queryContext, tx, access.TenantID, false); err != nil {
		return Document{}, err
	}
	replayed, found, err := loadMutationReplay(queryContext, tx, access, command.IdempotencyKey, command.Operation, command.Fingerprint)
	if err != nil {
		return Document{}, err
	}
	if found {
		if err := tx.Commit(queryContext); err != nil {
			return Document{}, ErrUnavailable
		}
		return replayed, nil
	}
	document, err := loadDocument(queryContext, tx, access.TenantID, command.DocumentID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, ErrUnavailable
	}
	if document.Version != command.ExpectedVersion {
		return Document{}, ErrVersionConflict
	}
	nextStatus, valid := transitionStatus(document.Status, command.Operation)
	if !valid {
		return Document{}, ErrTransitionConflict
	}
	revokeIncrement := int64(0)
	if command.Operation == "suspend" || command.Operation == "close" {
		revokeIncrement = 1
	}
	result, err := updateLifecycleDocument(queryContext, tx, access, document, command, nextStatus, revokeIncrement)
	if err != nil {
		return Document{}, err
	}
	if err := insertMutationReceipt(queryContext, tx, access, command.IdempotencyKey,
		command.Fingerprint, command.Operation, result); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(queryContext); err != nil {
		return Document{}, ErrUnavailable
	}
	return result, nil
}

func (repository *PostgresRepository) CapabilityPolicies(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
) (map[Audience]Capability, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	tx, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rollback(tx)
	if err := repository.requireFeature(queryContext, tx, access.TenantID, true); err != nil {
		return nil, err
	}
	rows, err := tx.Query(queryContext, `SELECT audience, capability
        FROM tutorhub.whiteboard_capability_policies
        WHERE tenant_id = $1 AND document_id = $2`, access.TenantID, documentID)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	policies := make(map[Audience]Capability, 5)
	for rows.Next() {
		var audience Audience
		var capability Capability
		if err := rows.Scan(&audience, &capability); err != nil {
			return nil, ErrUnavailable
		}
		policies[audience] = capability
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	if len(policies) == 0 {
		return nil, ErrNotFound
	}
	if err := tx.Commit(queryContext); err != nil {
		return nil, ErrUnavailable
	}
	return policies, nil
}

func (repository *PostgresRepository) ListSnapshots(
	ctx context.Context,
	access AccessContext,
	documentID uuid.UUID,
	cursor SnapshotPageCursor,
	limit int,
) ([]Snapshot, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	tx, err := repository.database.Begin(queryContext)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rollback(tx)
	if err := repository.requireFeature(queryContext, tx, access.TenantID, true); err != nil {
		return nil, err
	}
	query := `SELECT id, document_id, generation, snapshot_kind,
        format_version, engine_version, authority_version, schema_version,
        causal_watermark_sha256, content_sha256, size_bytes, created_at, retention_until
        FROM tutorhub.whiteboard_snapshots
		WHERE tenant_id = $1 AND document_id = $2`
	arguments := []any{access.TenantID, documentID}
	if !cursor.CreatedAt.IsZero() && cursor.ID != uuid.Nil {
		query += ` AND (created_at, id) < ($3, $4)`
		arguments = append(arguments, cursor.CreatedAt, cursor.ID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT $` + strconv.Itoa(len(arguments)+1)
	arguments = append(arguments, limit)
	rows, err := tx.Query(queryContext, query, arguments...)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer rows.Close()
	items := make([]Snapshot, 0, limit)
	for rows.Next() {
		var snapshot Snapshot
		var watermark, content []byte
		if err := rows.Scan(
			&snapshot.ID, &snapshot.DocumentID, &snapshot.Generation, &snapshot.Kind,
			&snapshot.FormatVersion, &snapshot.EngineVersion, &snapshot.AuthorityVersion,
			&snapshot.SchemaVersion, &watermark, &content, &snapshot.SizeBytes,
			&snapshot.CreatedAt, &snapshot.RetentionUntil,
		); err != nil {
			return nil, ErrUnavailable
		}
		snapshot.CausalWatermarkSHA256 = hex.EncodeToString(watermark)
		snapshot.ContentSHA256 = hex.EncodeToString(content)
		items = append(items, snapshot)
	}
	if rows.Err() != nil {
		return nil, ErrUnavailable
	}
	if err := tx.Commit(queryContext); err != nil {
		return nil, ErrUnavailable
	}
	return items, nil
}

func (repository *PostgresRepository) Restore(
	ctx context.Context,
	access AccessContext,
	command RestoreCommand,
) (Document, error) {
	queryContext, cancel := context.WithTimeout(ctx, repository.queryTimeout)
	defer cancel()
	tx, err := repository.database.Begin(queryContext)
	if err != nil {
		return Document{}, ErrUnavailable
	}
	defer rollback(tx)
	if err := repository.requireFeature(queryContext, tx, access.TenantID, false); err != nil {
		return Document{}, err
	}
	replayed, found, err := loadMutationReplay(queryContext, tx, access, command.IdempotencyKey, "restore", command.Fingerprint)
	if err != nil {
		return Document{}, err
	}
	if found {
		if err := tx.Commit(queryContext); err != nil {
			return Document{}, ErrUnavailable
		}
		return replayed, nil
	}
	document, err := loadDocument(queryContext, tx, access.TenantID, command.DocumentID, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, ErrUnavailable
	}
	if document.Version != command.ExpectedVersion || document.CurrentGeneration != command.ExpectedGeneration {
		return Document{}, ErrVersionConflict
	}
	var snapshotGeneration int64
	if err := tx.QueryRow(queryContext, `SELECT generation
        FROM tutorhub.whiteboard_snapshots
        WHERE tenant_id = $1 AND document_id = $2 AND id = $3`,
		access.TenantID, document.ID, command.SnapshotID,
	).Scan(&snapshotGeneration); errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	} else if err != nil {
		return Document{}, ErrUnavailable
	}
	nextGeneration := document.CurrentGeneration + 1
	var prepared bool
	if err := tx.QueryRow(queryContext, `SELECT EXISTS (
		SELECT 1
		FROM tutorhub.whiteboard_artifact_commands AS command
		WHERE command.id = $1
		  AND command.tenant_id = $2
		  AND command.actor_user_id = $3
		  AND command.document_id = $4
		  AND command.generation = $5
		  AND command.command_kind = 'restore'
		  AND command.status = 'succeeded'
		  AND command.source_snapshot_id = $6
		  AND command.target_generation = $7
		  AND command.target_provider_document_name = $8
		  AND EXISTS (
			SELECT 1
			FROM tutorhub.whiteboard_document_checkpoints AS checkpoint
			WHERE checkpoint.tenant_id = command.tenant_id
			  AND checkpoint.document_id = command.document_id
			  AND checkpoint.generation = command.target_generation
			  AND checkpoint.provider_document_name = command.target_provider_document_name
		  )
	)`, command.ArtifactCommandID, access.TenantID, access.ActorID, document.ID,
		command.ExpectedGeneration, command.SnapshotID, nextGeneration,
		command.ProviderDocumentName,
	).Scan(&prepared); err != nil {
		return Document{}, ErrUnavailable
	}
	if !prepared {
		return Document{}, ErrArtifactUnavailable
	}
	_, err = tx.Exec(queryContext, `INSERT INTO tutorhub.whiteboard_document_generations
        (tenant_id, document_id, generation, provider_document_name, reason,
		 restored_from_snapshot_id, created_by)
		VALUES ($1, $2, $3, $4, 'restore', $5, $6)`,
		access.TenantID, document.ID, nextGeneration, command.ProviderDocumentName,
		command.SnapshotID, access.ActorID,
	)
	if err != nil {
		return Document{}, classifyWriteError(err)
	}
	_ = snapshotGeneration
	var result Document
	err = scanDocument(tx.QueryRow(queryContext, `UPDATE tutorhub.whiteboard_documents
        SET current_generation = $3, revoke_generation = revoke_generation + 1,
			version = version + 1, updated_by = $4, updated_at = transaction_timestamp()
		WHERE tenant_id = $1 AND id = $2 AND version = $5 AND current_generation = $6
        RETURNING id, media_space_id, status, version, current_generation,
                  revoke_generation, created_at, updated_at`,
		access.TenantID, document.ID, nextGeneration, access.ActorID,
		command.ExpectedVersion, command.ExpectedGeneration,
	), &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrVersionConflict
	}
	if err != nil {
		return Document{}, ErrUnavailable
	}
	if err := insertMutationReceipt(queryContext, tx, access, command.IdempotencyKey,
		command.Fingerprint, "restore", result); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(queryContext); err != nil {
		return Document{}, ErrUnavailable
	}
	return result, nil
}

func updateLifecycleDocument(
	ctx context.Context,
	tx pgx.Tx,
	access AccessContext,
	document Document,
	command TransitionCommand,
	nextStatus DocumentStatus,
	revokeIncrement int64,
) (Document, error) {
	query := `UPDATE tutorhub.whiteboard_documents SET
        status = $3, version = version + 1, revoke_generation = revoke_generation + $4,
		updated_by = $5, updated_at = transaction_timestamp(),
		opened_at = CASE WHEN $6 = 'open' AND status = 'created' THEN transaction_timestamp() ELSE opened_at END,
		opened_by = CASE WHEN $6 = 'open' AND status = 'created' THEN $5 ELSE opened_by END,
		suspended_at = CASE WHEN $6 = 'suspend' THEN transaction_timestamp() ELSE suspended_at END,
		suspended_by = CASE WHEN $6 = 'suspend' THEN $5 ELSE suspended_by END,
		resumed_at = CASE WHEN $6 = 'resume' THEN transaction_timestamp() ELSE resumed_at END,
		resumed_by = CASE WHEN $6 = 'resume' THEN $5 ELSE resumed_by END,
		closed_at = CASE WHEN $6 = 'close' THEN transaction_timestamp() ELSE closed_at END,
		closed_by = CASE WHEN $6 = 'close' THEN $5 ELSE closed_by END
		WHERE tenant_id = $1 AND id = $2 AND version = $7
        RETURNING id, media_space_id, status, version, current_generation,
                  revoke_generation, created_at, updated_at`
	var result Document
	err := scanDocument(tx.QueryRow(ctx, query,
		access.TenantID, document.ID, nextStatus, revokeIncrement, access.ActorID,
		command.Operation, command.ExpectedVersion,
	), &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrVersionConflict
	}
	if err != nil {
		return Document{}, ErrUnavailable
	}
	return result, nil
}

func loadCreateReplay(
	ctx context.Context,
	tx pgx.Tx,
	tenantID uuid.UUID,
	actorID uuid.UUID,
	key string,
) (Document, []byte, bool, error) {
	var document Document
	var fingerprint []byte
	err := tx.QueryRow(ctx, `SELECT id, media_space_id, status, version,
        current_generation, revoke_generation, created_at, updated_at,
        create_request_fingerprint
        FROM tutorhub.whiteboard_documents
        WHERE tenant_id = $1 AND created_by = $2 AND create_idempotency_key = $3`,
		tenantID, actorID, key,
	).Scan(&document.ID, &document.MediaSpaceID, &document.Status, &document.Version,
		&document.CurrentGeneration, &document.RevokeGeneration, &document.CreatedAt,
		&document.UpdatedAt, &fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, nil, false, nil
	}
	return document, fingerprint, err == nil, err
}

func loadMutationReplay(
	ctx context.Context,
	tx pgx.Tx,
	access AccessContext,
	key string,
	operation string,
	fingerprint []byte,
) (Document, bool, error) {
	var storedFingerprint []byte
	var storedOperation string
	var document Document
	err := tx.QueryRow(ctx, `SELECT receipt.request_fingerprint, receipt.operation,
		       receipt.document_id, document.media_space_id, receipt.result_status,
		       receipt.result_document_version, receipt.result_generation,
		       receipt.result_revoke_generation, document.created_at, receipt.created_at
		FROM tutorhub.whiteboard_document_mutation_receipts AS receipt
		JOIN tutorhub.whiteboard_documents AS document
		  ON document.tenant_id = receipt.tenant_id AND document.id = receipt.document_id
		WHERE receipt.tenant_id = $1 AND receipt.actor_user_id = $2
		  AND receipt.idempotency_key = $3`,
		access.TenantID, access.ActorID, key,
	).Scan(
		&storedFingerprint, &storedOperation, &document.ID, &document.MediaSpaceID,
		&document.Status, &document.Version, &document.CurrentGeneration,
		&document.RevokeGeneration, &document.CreatedAt, &document.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, false, nil
	}
	if err != nil {
		return Document{}, false, ErrUnavailable
	}
	if storedOperation != operation || !bytes.Equal(storedFingerprint, fingerprint) {
		return Document{}, false, ErrIdempotencyConflict
	}
	return document, true, nil
}

func insertMutationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	access AccessContext,
	key string,
	fingerprint []byte,
	operation string,
	document Document,
) error {
	_, err := tx.Exec(ctx, `INSERT INTO tutorhub.whiteboard_document_mutation_receipts
        (tenant_id, actor_user_id, idempotency_key, request_fingerprint, operation,
         document_id, result_document_version, result_generation,
		 result_revoke_generation, result_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		access.TenantID, access.ActorID, key, fingerprint, operation, document.ID,
		document.Version, document.CurrentGeneration, document.RevokeGeneration,
		document.Status,
	)
	if err != nil {
		return classifyWriteError(err)
	}
	return nil
}

func loadDocument(ctx context.Context, tx pgx.Tx, tenantID, documentID uuid.UUID, lock bool) (Document, error) {
	query := `SELECT id, media_space_id, status, version, current_generation,
        revoke_generation, created_at, updated_at
        FROM tutorhub.whiteboard_documents WHERE tenant_id = $1 AND id = $2`
	if lock {
		query += ` FOR UPDATE`
	}
	var document Document
	return document, scanDocument(tx.QueryRow(ctx, query, tenantID, documentID), &document)
}

func loadDocumentBySpace(ctx context.Context, tx pgx.Tx, tenantID, spaceID uuid.UUID, lock bool) (Document, error) {
	query := `SELECT id, media_space_id, status, version, current_generation,
        revoke_generation, created_at, updated_at
        FROM tutorhub.whiteboard_documents WHERE tenant_id = $1 AND media_space_id = $2`
	if lock {
		query += ` FOR UPDATE`
	}
	var document Document
	return document, scanDocument(tx.QueryRow(ctx, query, tenantID, spaceID), &document)
}

func scanDocument(row pgx.Row, document *Document) error {
	return row.Scan(&document.ID, &document.MediaSpaceID, &document.Status, &document.Version,
		&document.CurrentGeneration, &document.RevokeGeneration,
		&document.CreatedAt, &document.UpdatedAt)
}

func transitionStatus(current DocumentStatus, operation string) (DocumentStatus, bool) {
	switch operation {
	case "open":
		return DocumentOpen, current == DocumentCreated
	case "suspend":
		return DocumentSuspended, current == DocumentOpen
	case "resume":
		return DocumentOpen, current == DocumentSuspended
	case "close":
		return DocumentClosed, current != DocumentClosed
	default:
		return "", false
	}
}

func defaultCapabilityPolicies() map[Audience]Capability {
	return map[Audience]Capability{
		AudienceOrganizationAdmin: CapabilityPresent,
		AudienceHost:              CapabilityPresent,
		AudienceCoHost:            CapabilityEdit,
		AudienceTeachingAssistant: CapabilityEdit,
		AudienceAttendee:          CapabilityView,
	}
}

func classifyWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23505":
			return ErrIdempotencyConflict
		case "23503":
			return ErrNotFound
		case "23514", "22001", "22P02":
			return ErrInvalidRequest
		}
	}
	return ErrUnavailable
}

func (repository *PostgresRepository) requireFeature(
	ctx context.Context,
	tx pgx.Tx,
	tenantID uuid.UUID,
	read bool,
) error {
	if repository.controls == nil {
		return nil
	}
	var err error
	if read {
		err = repository.controls.RequireFeatureForRead(ctx, tx, tenantID, featurecontrol.FeatureClassroomWhiteboards)
	} else {
		err = repository.controls.RequireFeature(ctx, tx, tenantID, featurecontrol.FeatureClassroomWhiteboards)
	}
	return classifyControlError(err)
}

func classifyControlError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, featurecontrol.ErrFeatureDisabled),
		errors.Is(err, featurecontrol.ErrTenantNotFound),
		errors.Is(err, featurecontrol.ErrAccessDenied):
		return ErrNotFound
	case errors.Is(err, featurecontrol.ErrQuotaExceeded):
		return ErrQuotaExceeded
	default:
		return ErrUnavailable
	}
}

func rollback(tx pgx.Tx) {
	_ = tx.Rollback(context.Background())
}
