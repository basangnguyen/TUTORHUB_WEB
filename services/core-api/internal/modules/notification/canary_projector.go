package notification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tutorhub-v2/core-api/internal/platform/outboxworker"
)

const (
	canaryAggregateType       = "notification_intent"
	canaryEffectKey           = "worker_canary"
	canaryTemplateKey         = KindSystemWorkerCanary
	canaryProjectionErrorCode = "notification_projection_failed"
)

var CanaryEventType = outboxworker.MustEventType("notification.in_app_canary.requested", 1)

type CanaryPayload struct {
	RecipientUserID uuid.UUID `json:"recipient_user_id"`
	Kind            string    `json:"kind"`
}

type commandDatabase interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type PostgresCanaryProjector struct {
	database     commandDatabase
	queryTimeout time.Duration
}

func NewPostgresCanaryProjector(
	database commandDatabase,
	queryTimeout time.Duration,
) (*PostgresCanaryProjector, error) {
	if database == nil {
		return nil, fmt.Errorf("notification canary database is required")
	}
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresCanaryProjector{database: database, queryTimeout: queryTimeout}, nil
}

func (projector *PostgresCanaryProjector) ProjectCanary(
	ctx context.Context,
	projection CanaryProjection,
) error {
	if projector == nil || projection.SourceOutboxEventID == uuid.Nil ||
		projection.TenantID == uuid.Nil || projection.RecipientUserID == uuid.Nil ||
		projection.OccurredAt.IsZero() {
		return ErrInvalidInput
	}
	queryContext, cancel := context.WithTimeout(ctx, projector.queryTimeout)
	defer cancel()
	_, err := projector.database.Exec(
		queryContext,
		`INSERT INTO tutorhub.notifications (
    tenant_id,
    recipient_user_id,
    source_outbox_event_id,
    effect_key,
    kind,
    template_key,
    context,
    occurred_at
)
VALUES ($1, $2, $3, $4, $5, $5, '{}'::jsonb, $6)
ON CONFLICT DO NOTHING`,
		projection.TenantID,
		projection.RecipientUserID,
		projection.SourceOutboxEventID,
		canaryEffectKey,
		canaryTemplateKey,
		projection.OccurredAt.UTC(),
	)
	if err == nil {
		return nil
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.ConstraintName {
		case "notifications_recipient_membership_fk":
			return ErrNotFound
		case "notifications_effect_key_valid",
			"notifications_kind_valid",
			"notifications_template_key_valid",
			"notifications_context_object":
			return ErrInvalidInput
		}
	}
	return fmt.Errorf("project notification canary: %w", err)
}

func RegisterCanaryHandler(
	registry *outboxworker.Registry,
	projector CanaryProjector,
) error {
	if projector == nil {
		return fmt.Errorf("notification canary projector is required")
	}
	return outboxworker.RegisterJSON(registry, outboxworker.HandlerSpec[CanaryPayload]{
		EventType:     CanaryEventType,
		HandlerName:   "in_app_notification_canary",
		AggregateType: canaryAggregateType,
		TenantMode:    outboxworker.TenantRequired,
		Validate: func(_ outboxworker.Event, payload CanaryPayload) error {
			if payload.RecipientUserID == uuid.Nil || payload.Kind != KindSystemWorkerCanary {
				return ErrInvalidInput
			}
			return nil
		},
		Handle: func(
			ctx context.Context,
			event outboxworker.Event,
			payload CanaryPayload,
		) error {
			err := projector.ProjectCanary(ctx, CanaryProjection{
				SourceOutboxEventID: event.ID,
				TenantID:            event.TenantID.UUID,
				RecipientUserID:     payload.RecipientUserID,
				OccurredAt:          event.OccurredAt,
			})
			switch {
			case err == nil:
				return nil
			case errors.Is(err, ErrInvalidInput):
				return outboxworker.Permanent(outboxworker.ErrorCodeInvalidPayload)
			case errors.Is(err, ErrNotFound), errors.Is(err, ErrAccessDenied):
				return outboxworker.Permanent(outboxworker.ErrorCodeInvalidEventContext)
			default:
				return outboxworker.Retryable(canaryProjectionErrorCode)
			}
		},
	})
}
