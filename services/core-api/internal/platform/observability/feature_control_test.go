package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

type rejectingFeatureControlEnforcer struct {
	readCalls int
	readErr   error
}

func (rejectingFeatureControlEnforcer) RequireFeature(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.FeatureKey,
) error {
	return nil
}

func (enforcer *rejectingFeatureControlEnforcer) RequireFeatureForRead(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.FeatureKey,
) error {
	enforcer.readCalls++
	return enforcer.readErr
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

func (rejectingFeatureControlEnforcer) RequireQuotaAtMost(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.QuotaKey,
	int64,
) error {
	return &featurecontrol.QuotaExceededError{Quota: featurecontrol.QuotaActiveAvailabilityPolls}
}

func (rejectingFeatureControlEnforcer) ConsumeRateQuota(
	context.Context,
	featurecontrol.Transaction,
	uuid.UUID,
	featurecontrol.QuotaKey,
	time.Time,
) (featurecontrol.RateLimitResult, error) {
	return featurecontrol.RateLimitResult{}, &featurecontrol.QuotaExceededError{
		Quota: featurecontrol.QuotaAvailabilityPollCreationsPerHour,
	}
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
	next := &rejectingFeatureControlEnforcer{}
	enforcer := ObserveFeatureControlEnforcer(next, metrics)
	_, _ = enforcer.ConsumeInviteCreation(context.Background(), nil, uuid.New(), time.Now())
	_ = enforcer.RequireMemberCapacity(context.Background(), nil, uuid.New())
	_ = enforcer.RequireActiveClassCapacity(context.Background(), nil, uuid.New())
	_ = enforcer.RequireQuotaAtMost(
		context.Background(), nil, uuid.New(), featurecontrol.QuotaActiveAvailabilityPolls, 1,
	)
	_, _ = enforcer.ConsumeRateQuota(
		context.Background(), nil, uuid.New(),
		featurecontrol.QuotaAvailabilityPollCreationsPerHour, time.Now(),
	)

	snapshot := metrics.Snapshot()
	if len(snapshot.QuotaRejections) != len(featurecontrol.QuotaKeys()) {
		t.Fatalf("quota rejection labels are not catalog bounded: %v", snapshot.QuotaRejections)
	}
	counts := make(map[string]int64, len(snapshot.QuotaRejections))
	for _, metric := range snapshot.QuotaRejections {
		counts[metric.Quota] = metric.Count
	}
	for _, key := range []featurecontrol.QuotaKey{
		featurecontrol.QuotaMembers,
		featurecontrol.QuotaActiveClasses,
		featurecontrol.QuotaInviteCreationsPerHour,
		featurecontrol.QuotaActiveAvailabilityPolls,
		featurecontrol.QuotaAvailabilityPollCreationsPerHour,
	} {
		if counts[string(key)] != 1 {
			t.Fatalf("quota %q rejection count = %d, want 1", key, counts[string(key)])
		}
	}
}

func TestObservedFeatureControlEnforcerForwardsReadFeatureCheck(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("read feature rejected")
	next := &rejectingFeatureControlEnforcer{readErr: wantErr}
	enforcer := ObserveFeatureControlEnforcer(next, NewMetrics())
	err := enforcer.RequireFeatureForRead(
		context.Background(),
		nil,
		uuid.New(),
		featurecontrol.FeatureClassroomMediaRooms,
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("read feature error = %v, want %v", err, wantErr)
	}
	if next.readCalls != 1 {
		t.Fatalf("read feature forwarding calls = %d, want 1", next.readCalls)
	}
}
