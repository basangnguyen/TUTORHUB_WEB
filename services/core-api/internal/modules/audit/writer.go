package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
)

type Transaction interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func AppendDomainEvent(
	ctx context.Context,
	transaction Transaction,
	event DomainEvent,
) error {
	action, ok := ActionForDomainEvent(event.EventType)
	if !ok {
		return fmt.Errorf("map audit domain event %q: %w", event.EventType, ErrInvalidFilter)
	}
	return AppendTransaction(ctx, transaction, Draft{
		TenantID:     event.TenantID,
		ActorID:      event.ActorID,
		Action:       action,
		ResourceType: event.AggregateType,
		ResourceID:   event.AggregateID,
		Outcome:      OutcomeSucceeded,
		Metadata:     event.Metadata,
		OccurredAt:   event.OccurredAt,
	})
}

func AppendTransaction(
	ctx context.Context,
	transaction Transaction,
	draft Draft,
) error {
	if transaction == nil {
		return fmt.Errorf("append audit event: transaction is required")
	}
	if err := validateDraft(draft); err != nil {
		return fmt.Errorf("append audit event: %w", err)
	}

	request := requestmeta.SnapshotFromContext(ctx)
	if request.RequestInstance == uuid.Nil {
		request.RequestInstance = uuid.New()
	}
	if draft.OccurredAt.IsZero() {
		draft.OccurredAt = time.Now().UTC()
	}

	actorType := ActorTypeSystem
	var actorID any
	var sourceIPPrefix any
	var userAgentHash any
	if draft.ActorID != uuid.Nil {
		actorType = ActorTypeUser
		actorID = draft.ActorID
		if request.ActorID == draft.ActorID {
			if request.SourceIPPrefix != "" {
				sourceIPPrefix = request.SourceIPPrefix
			}
			if len(request.UserAgentHash) > 0 {
				userAgentHash = request.UserAgentHash
			}
		}
	}
	var resourceID any
	if draft.ResourceID != uuid.Nil {
		resourceID = draft.ResourceID
	}
	metadata, err := json.Marshal(copyMetadata(draft.Metadata))
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}

	if _, err := transaction.Exec(
		ctx,
		`INSERT INTO tutorhub.audit_events (
    tenant_id,
    actor_type,
    actor_user_id,
    action,
    resource_type,
    resource_id,
    outcome,
    request_id,
    request_instance_id,
    source_ip_prefix,
    user_agent_hash,
    metadata,
    occurred_at
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10::inet, $11, $12::jsonb, $13
)`,
		draft.TenantID,
		actorType,
		actorID,
		draft.Action,
		draft.ResourceType,
		resourceID,
		draft.Outcome,
		request.RequestID,
		request.RequestInstance,
		sourceIPPrefix,
		userAgentHash,
		string(metadata),
		draft.OccurredAt.UTC(),
	); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}
