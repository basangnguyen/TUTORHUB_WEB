package observability

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

type rejectingFeatureControlEnforcer struct{}

func (rejectingFeatureControlEnforcer) RequireFeature(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.FeatureKey,
) error {
	return nil
}

func (rejectingFeatureControlEnforcer) RequireMemberCapacity(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
) error {
	return &featurecontrol.QuotaExceededError{Quota: featurecontrol.QuotaMembers}
}

func (rejectingFeatureControlEnforcer) RequireActiveClassCapacity(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
) error {
	return &featurecontrol.QuotaExceededError{Quota: featurecontrol.QuotaActiveClasses}
}

func (rejectingFeatureControlEnforcer) ConsumeInviteCreation(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	time.Time,
) (featurecontrol.RateLimitResult, error) {
	return featurecontrol.RateLimitResult{}, &featurecontrol.QuotaExceededError{
		Quota: featurecontrol.QuotaInviteCreationsPerHour,
	}
}

func TestObservedFeatureControlEnforcerRecordsBoundedQuotaFailures(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics()
	enforcer := ObserveFeatureControlEnforcer(rejectingFeatureControlEnforcer{}, metrics)
	_, _ = enforcer.ConsumeInviteCreation(context.Background(), nil, uuid.New(), time.Now())
	_ = enforcer.RequireMemberCapacity(context.Background(), nil, uuid.New())
	_ = enforcer.RequireActiveClassCapacity(context.Background(), nil, uuid.New())

	if got := metrics.Snapshot().QuotaRejections; got != [3]int64{1, 1, 1} {
		t.Fatalf("unexpected quota rejection counters: %v", got)
	}
}
