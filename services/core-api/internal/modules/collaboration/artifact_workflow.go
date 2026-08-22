package collaboration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

const (
	defaultArtifactRestoreWait  = 8 * time.Second
	defaultArtifactPollInterval = 100 * time.Millisecond
)

// PostgresArtifactWorkflow persists only bounded command metadata. Artifact
// bytes are owned by the collaboration worker and immutable B2 object store.
type PostgresArtifactWorkflow struct {
	database     Database
	queryTimeout time.Duration
	restoreWait  time.Duration
	pollInterval time.Duration
	clock        func() time.Time
	newID        func() uuid.UUID
	controls     featurecontrol.Enforcer
}

type PostgresArtifactWorkflowConfig struct {
	QueryTimeout time.Duration
	RestoreWait  time.Duration
	PollInterval time.Duration
	Clock        func() time.Time
	NewID        func() uuid.UUID
	Controls     featurecontrol.Enforcer
}

func NewPostgresArtifactWorkflow(
	database Database,
	config PostgresArtifactWorkflowConfig,
) (*PostgresArtifactWorkflow, error) {
	if database == nil {
		return nil, fmt.Errorf("whiteboard artifact database is required")
	}
	if config.QueryTimeout <= 0 {
		config.QueryTimeout = defaultQueryTimeout
	}
	if config.RestoreWait <= 0 {
		config.RestoreWait = defaultArtifactRestoreWait
	}
	if config.PollInterval <= 0 {
		config.PollInterval = defaultArtifactPollInterval
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.NewID == nil {
		config.NewID = uuid.New
	}
	return &PostgresArtifactWorkflow{
		database: database, queryTimeout: config.QueryTimeout,
		restoreWait: config.RestoreWait, pollInterval: config.PollInterval,
		clock: config.Clock, newID: config.NewID,
		controls: config.Controls,
	}, nil
}

func (workflow *PostgresArtifactWorkflow) RequestSnapshot(
	ctx context.Context,
	access AccessContext,
	document Document,
	input SnapshotCreateInput,
) (ArtifactCommand, error) {
	return workflow.enqueue(ctx, access, document, "snapshot", input.IdempotencyKey,
		artifactRequestFingerprint("snapshot", document.ID, input.ExpectedGeneration),
		uuid.Nil, 0, "")
}

func (workflow *PostgresArtifactWorkflow) RequestExport(
	ctx context.Context,
	access AccessContext,
	document Document,
	input ExportInput,
) (ArtifactCommand, error) {
	return workflow.enqueue(ctx, access, document, "export", input.IdempotencyKey,
		artifactRequestFingerprint("export", document.ID, input.ExpectedGeneration),
		uuid.Nil, 0, "")
}

func (workflow *PostgresArtifactWorkflow) PrepareRestore(
	ctx context.Context,
	access AccessContext,
	document Document,
	command RestoreCommand,
) (ArtifactCommand, error) {
	queued, err := workflow.enqueue(ctx, access, document, "restore", command.IdempotencyKey,
		command.Fingerprint, command.SnapshotID, command.ExpectedGeneration+1,
		command.ProviderDocumentName)
	if err != nil {
		return ArtifactCommand{}, err
	}

	waitContext, cancel := context.WithTimeout(ctx, workflow.restoreWait)
	defer cancel()
	ticker := time.NewTicker(workflow.pollInterval)
	defer ticker.Stop()
	for {
		status, statusErr := workflow.status(waitContext, access, queued.ID)
		if statusErr != nil {
			return ArtifactCommand{}, statusErr
		}
		switch status {
		case "succeeded":
			return queued, nil
		case "failed", "quarantined":
			return ArtifactCommand{}, ErrArtifactUnavailable
		}
		select {
		case <-waitContext.Done():
			return ArtifactCommand{}, ErrArtifactUnavailable
		case <-ticker.C:
		}
	}
}

func (workflow *PostgresArtifactWorkflow) enqueue(
	ctx context.Context,
	access AccessContext,
	document Document,
	kind string,
	idempotencyKey string,
	fingerprint []byte,
	sourceSnapshotID uuid.UUID,
	targetGeneration int64,
	targetProviderDocumentName string,
) (ArtifactCommand, error) {
	queryContext, cancel := context.WithTimeout(ctx, workflow.queryTimeout)
	defer cancel()
	tx, err := workflow.database.Begin(queryContext)
	if err != nil {
		return ArtifactCommand{}, ErrArtifactUnavailable
	}
	defer rollback(tx)
	if workflow.controls != nil {
		if err := workflow.controls.RequireFeature(queryContext, tx, access.TenantID, featurecontrol.FeatureClassroomWhiteboards); err != nil {
			return ArtifactCommand{}, classifyControlError(err)
		}
	}
	if _, err := tx.Exec(queryContext,
		`SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
		"whiteboard-artifact:"+access.TenantID.String()+":"+access.ActorID.String()+":"+idempotencyKey,
	); err != nil {
		return ArtifactCommand{}, ErrArtifactUnavailable
	}

	var existing ArtifactCommand
	var storedFingerprint []byte
	var storedKind string
	err = tx.QueryRow(queryContext, `SELECT id, command_kind, document_id,
		generation, request_fingerprint, requested_at,
		COALESCE(target_provider_document_name, '')
		FROM tutorhub.whiteboard_artifact_commands
		WHERE tenant_id = $1 AND actor_user_id = $2 AND idempotency_key = $3`,
		access.TenantID, access.ActorID, idempotencyKey,
	).Scan(&existing.ID, &storedKind, &existing.DocumentID, &existing.Generation,
		&storedFingerprint, &existing.RequestedAt, &existing.TargetProviderDocument)
	if err == nil {
		if storedKind != kind || existing.DocumentID != document.ID ||
			existing.Generation != document.CurrentGeneration ||
			!bytes.Equal(storedFingerprint, fingerprint) {
			return ArtifactCommand{}, ErrIdempotencyConflict
		}
		existing.Kind = storedKind
		existing.Status = ArtifactCommandAccepted
		if err := tx.Commit(queryContext); err != nil {
			return ArtifactCommand{}, ErrArtifactUnavailable
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return ArtifactCommand{}, ErrArtifactUnavailable
	}
	if workflow.controls != nil && (kind == "snapshot" || kind == "export") {
		if _, err := tx.Exec(queryContext, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, "whiteboard-storage:"+access.TenantID.String()); err != nil {
			return ArtifactCommand{}, ErrArtifactUnavailable
		}
		var used, pending int64
		if err := tx.QueryRow(queryContext, `SELECT
			COALESCE((SELECT sum(size_bytes) FROM tutorhub.whiteboard_snapshots WHERE tenant_id = $1), 0),
			COALESCE((SELECT count(*) FROM tutorhub.whiteboard_artifact_commands WHERE tenant_id = $1 AND command_kind IN ('snapshot', 'export') AND status IN ('pending', 'processing')), 0)`, access.TenantID).Scan(&used, &pending); err != nil {
			return ArtifactCommand{}, ErrArtifactUnavailable
		}
		const artifactReservationBytes int64 = maximumPortableImportBytes
		if err := workflow.controls.RequireQuotaAtMost(queryContext, tx, access.TenantID, featurecontrol.QuotaWhiteboardStorageBytesPerTenant, used+(pending+1)*artifactReservationBytes); err != nil {
			return ArtifactCommand{}, classifyControlError(err)
		}
	}

	queued := ArtifactCommand{
		ID: workflow.newID(), Kind: kind, DocumentID: document.ID,
		Generation: document.CurrentGeneration, Status: ArtifactCommandAccepted,
		RequestedAt: workflow.clock().UTC(), TargetProviderDocument: targetProviderDocumentName,
	}
	var sourceSnapshot any
	var target any
	var provider any
	if sourceSnapshotID != uuid.Nil {
		sourceSnapshot = sourceSnapshotID
		target = targetGeneration
		provider = targetProviderDocumentName
	}
	_, err = tx.Exec(queryContext, `INSERT INTO tutorhub.whiteboard_artifact_commands
		(id, tenant_id, actor_user_id, document_id, generation, command_kind,
		 idempotency_key, request_fingerprint, source_snapshot_id,
		 target_generation, target_provider_document_name, requested_at,
		 available_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12, $12)`,
		queued.ID, access.TenantID, access.ActorID, document.ID,
		document.CurrentGeneration, kind, idempotencyKey, fingerprint,
		sourceSnapshot, target, provider, queued.RequestedAt,
	)
	if err != nil {
		return ArtifactCommand{}, classifyWriteError(err)
	}
	if err := tx.Commit(queryContext); err != nil {
		return ArtifactCommand{}, ErrArtifactUnavailable
	}
	return queued, nil
}

func (workflow *PostgresArtifactWorkflow) status(
	ctx context.Context,
	access AccessContext,
	commandID uuid.UUID,
) (string, error) {
	tx, err := workflow.database.Begin(ctx)
	if err != nil {
		return "", ErrArtifactUnavailable
	}
	defer rollback(tx)
	var status string
	err = tx.QueryRow(ctx, `SELECT status
		FROM tutorhub.whiteboard_artifact_commands
		WHERE tenant_id = $1 AND actor_user_id = $2 AND id = $3`,
		access.TenantID, access.ActorID, commandID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", ErrArtifactUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return "", ErrArtifactUnavailable
	}
	return status, nil
}

func artifactRequestFingerprint(kind string, documentID uuid.UUID, generation int64) []byte {
	digest := sha256.New()
	_, _ = digest.Write([]byte(kind))
	_, _ = digest.Write(documentID[:])
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(generation))
	_, _ = digest.Write(encoded[:])
	return digest.Sum(nil)
}
