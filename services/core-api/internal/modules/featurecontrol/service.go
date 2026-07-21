package featurecontrol

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/tenancy"
)

type ServiceAPI interface {
	GetCapabilities(context.Context, tenancy.Context) (Capabilities, error)
	PutOverrides(
		context.Context,
		tenancy.Context,
		PutOverridesInput,
	) (Capabilities, error)
}

type Service struct {
	repository Repository
	catalog    *Catalog
	clock      func() time.Time
}

func NewService(
	repository Repository,
	catalog *Catalog,
	clock func() time.Time,
) (*Service, error) {
	if repository == nil || catalog == nil {
		return nil, fmt.Errorf("feature control repository and catalog are required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{repository: repository, catalog: catalog, clock: clock}, nil
}

func (service *Service) GetCapabilities(
	ctx context.Context,
	tenantContext tenancy.Context,
) (Capabilities, error) {
	if err := tenantContext.Validate(); err != nil {
		return Capabilities{}, ErrAccessDenied
	}
	capabilities, err := service.repository.GetCapabilities(ctx, tenantContext, service.clock().UTC())
	return capabilities, NormalizeError(err)
}

func (service *Service) PutOverrides(
	ctx context.Context,
	tenantContext tenancy.Context,
	input PutOverridesInput,
) (Capabilities, error) {
	if err := tenantContext.Validate(); err != nil {
		return Capabilities{}, ErrAccessDenied
	}
	normalized, err := normalizeOverrides(service.catalog, input)
	if err != nil {
		return Capabilities{}, err
	}
	capabilities, err := service.repository.PutOverrides(
		ctx,
		tenantContext,
		normalized,
		service.clock().UTC(),
	)
	return capabilities, NormalizeError(err)
}

func normalizeOverrides(catalog *Catalog, input PutOverridesInput) (PutOverridesInput, error) {
	if catalog == nil || input.ExpectedVersion < 0 {
		return PutOverridesInput{}, ErrInvalidControl
	}
	features := make([]FeatureOverride, len(input.FeatureOverrides))
	copy(features, input.FeatureOverrides)
	seenFeatures := make(map[FeatureKey]struct{}, len(features))
	for _, override := range features {
		if _, ok := catalog.FeatureDefinition(override.Key); !ok {
			return PutOverridesInput{}, fmt.Errorf(
				"validate feature override %q: %w", override.Key, ErrInvalidControl,
			)
		}
		if _, duplicate := seenFeatures[override.Key]; duplicate {
			return PutOverridesInput{}, fmt.Errorf(
				"duplicate feature override %q: %w", override.Key, ErrInvalidControl,
			)
		}
		seenFeatures[override.Key] = struct{}{}
	}
	quotas := make([]QuotaOverride, len(input.QuotaOverrides))
	copy(quotas, input.QuotaOverrides)
	seenQuotas := make(map[QuotaKey]struct{}, len(quotas))
	for _, override := range quotas {
		definition, ok := catalog.QuotaDefinition(override.Key)
		if !ok {
			return PutOverridesInput{}, fmt.Errorf(
				"validate quota override %q: %w", override.Key, ErrInvalidControl,
			)
		}
		if _, duplicate := seenQuotas[override.Key]; duplicate {
			return PutOverridesInput{}, fmt.Errorf(
				"duplicate quota override %q: %w", override.Key, ErrInvalidControl,
			)
		}
		if err := validateQuotaLimit(definition, override.Limit); err != nil {
			return PutOverridesInput{}, err
		}
		seenQuotas[override.Key] = struct{}{}
	}
	sort.Slice(features, func(left, right int) bool { return features[left].Key < features[right].Key })
	sort.Slice(quotas, func(left, right int) bool { return quotas[left].Key < quotas[right].Key })
	return PutOverridesInput{
		ExpectedVersion: input.ExpectedVersion, FeatureOverrides: features, QuotaOverrides: quotas,
	}, nil
}
