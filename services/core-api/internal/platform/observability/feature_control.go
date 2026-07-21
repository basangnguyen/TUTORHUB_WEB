package observability

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

type observedFeatureControlEnforcer struct {
	next    featurecontrol.Enforcer
	metrics *Metrics
}

func ObserveFeatureControlEnforcer(
	next featurecontrol.Enforcer,
	metrics *Metrics,
) featurecontrol.Enforcer {
	if next == nil || metrics == nil {
		return next
	}
	return &observedFeatureControlEnforcer{next: next, metrics: metrics}
}

func (enforcer *observedFeatureControlEnforcer) RequireFeature(
	ctx context.Context,
	transaction featurecontrol.Transaction,
	tenantID uuid.UUID,
	key featurecontrol.FeatureKey,
) error {
	return enforcer.observe(enforcer.next.RequireFeature(ctx, transaction, tenantID, key))
}

func (enforcer *observedFeatureControlEnforcer) RequireMemberCapacity(
	ctx context.Context,
	transaction featurecontrol.Transaction,
	tenantID uuid.UUID,
) error {
	return enforcer.observe(enforcer.next.RequireMemberCapacity(ctx, transaction, tenantID))
}

func (enforcer *observedFeatureControlEnforcer) RequireActiveClassCapacity(
	ctx context.Context,
	transaction featurecontrol.Transaction,
	tenantID uuid.UUID,
) error {
	return enforcer.observe(enforcer.next.RequireActiveClassCapacity(ctx, transaction, tenantID))
}

func (enforcer *observedFeatureControlEnforcer) ConsumeInviteCreation(
	ctx context.Context,
	transaction featurecontrol.Transaction,
	tenantID uuid.UUID,
	now time.Time,
) (featurecontrol.RateLimitResult, error) {
	result, err := enforcer.next.ConsumeInviteCreation(ctx, transaction, tenantID, now)
	return result, enforcer.observe(err)
}

func (enforcer *observedFeatureControlEnforcer) observe(err error) error {
	var quotaError *featurecontrol.QuotaExceededError
	if errors.As(err, &quotaError) {
		enforcer.metrics.QuotaRejected(string(quotaError.Quota))
	}
	return err
}
