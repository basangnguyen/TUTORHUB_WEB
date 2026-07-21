package featurecontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

var featureControlTestTime = time.Date(2026, 7, 20, 9, 30, 0, 0, time.UTC)

func TestServiceUsesAuthenticatedTenantContextAndCanonicalAggregate(t *testing.T) {
	t.Parallel()

	repository := &recordingFeatureControlRepository{}
	service, err := NewService(repository, NewDefaultCatalog(), func() time.Time {
		return featureControlTestTime
	})
	if err != nil {
		t.Fatalf("create feature control service: %v", err)
	}
	tenantContext := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	_, err = service.PutOverrides(context.Background(), tenantContext, PutOverridesInput{
		ExpectedVersion: 2,
		FeatureOverrides: []FeatureOverride{
			{Key: FeatureMembershipInvitations, Enabled: false},
			{Key: FeatureClassInviteLinks, Enabled: true},
		},
		QuotaOverrides: []QuotaOverride{
			{Key: QuotaMembers, Limit: 150},
			{Key: QuotaActiveClasses, Limit: 20},
		},
	})
	if err != nil {
		t.Fatalf("put feature controls: %v", err)
	}
	if repository.tenantContext != tenantContext || repository.now != featureControlTestTime {
		t.Fatalf("repository context mismatch: %+v", repository)
	}
	if repository.input.FeatureOverrides[0].Key != FeatureClassInviteLinks ||
		repository.input.QuotaOverrides[0].Key != QuotaActiveClasses {
		t.Fatalf("service did not canonicalize aggregate: %+v", repository.input)
	}
}

func TestServiceFailsBeforeRepositoryForInvalidContextOrOverride(t *testing.T) {
	t.Parallel()

	repository := &recordingFeatureControlRepository{}
	service, err := NewService(repository, NewDefaultCatalog(), nil)
	if err != nil {
		t.Fatalf("create feature control service: %v", err)
	}
	if _, err := service.GetCapabilities(
		context.Background(),
		tenancy.Context{},
	); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("invalid context error = %v", err)
	}
	if _, err := service.PutOverrides(
		context.Background(),
		tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()},
		PutOverridesInput{QuotaOverrides: []QuotaOverride{{Key: QuotaMembers, Limit: 10001}}},
	); !errors.Is(err, ErrInvalidControl) {
		t.Fatalf("invalid override error = %v", err)
	}
	if repository.calls != 0 {
		t.Fatalf("invalid inputs reached repository %d times", repository.calls)
	}
}

func TestServiceClassifiesUnexpectedRepositoryFailureAsUnavailable(t *testing.T) {
	t.Parallel()

	repository := &recordingFeatureControlRepository{err: errors.New("database unavailable")}
	service, err := NewService(repository, NewDefaultCatalog(), nil)
	if err != nil {
		t.Fatalf("create feature control service: %v", err)
	}
	tenantContext := tenancy.Context{TenantID: uuid.New(), ActorID: uuid.New()}
	if _, err := service.GetCapabilities(
		context.Background(),
		tenantContext,
	); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected unavailable error, got %v", err)
	}
}

type recordingFeatureControlRepository struct {
	calls         int
	tenantContext tenancy.Context
	input         PutOverridesInput
	now           time.Time
	err           error
}

func (repository *recordingFeatureControlRepository) GetCapabilities(
	_ context.Context,
	tenantContext tenancy.Context,
	now time.Time,
) (Capabilities, error) {
	repository.calls++
	repository.tenantContext = tenantContext
	repository.now = now
	return Capabilities{TenantID: tenantContext.TenantID}, repository.err
}

func (repository *recordingFeatureControlRepository) PutOverrides(
	_ context.Context,
	tenantContext tenancy.Context,
	input PutOverridesInput,
	now time.Time,
) (Capabilities, error) {
	repository.calls++
	repository.tenantContext = tenantContext
	repository.input = input
	repository.now = now
	return Capabilities{TenantID: tenantContext.TenantID}, repository.err
}
