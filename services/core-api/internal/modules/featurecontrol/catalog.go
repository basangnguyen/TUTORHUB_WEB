package featurecontrol

import (
	"fmt"
	"sort"
)

type FeatureKey string

const (
	FeatureMembershipInvitations  FeatureKey = "membership_invitations"
	FeatureClassManagement        FeatureKey = "class_management"
	FeatureClassInviteLinks       FeatureKey = "class_invite_links"
	FeatureClassSessionScheduling FeatureKey = "class_session_scheduling"
)

type QuotaKey string

const (
	QuotaMembers                QuotaKey = "members"
	QuotaActiveClasses          QuotaKey = "active_classes"
	QuotaInviteCreationsPerHour QuotaKey = "invite_creations_per_hour"
)

type ValueSource string

const (
	ValueSourceCatalogDefault      ValueSource = "catalog_default"
	ValueSourceTenantOverride      ValueSource = "tenant_override"
	ValueSourceDeploymentGuardrail ValueSource = "deployment_guardrail"
)

type FeatureDefinition struct {
	Key            FeatureKey `json:"key"`
	DefaultEnabled bool       `json:"default_enabled"`
}

type QuotaDefinition struct {
	Key          QuotaKey `json:"key"`
	DefaultLimit int64    `json:"default_limit"`
	MinimumLimit int64    `json:"minimum_limit"`
	MaximumLimit int64    `json:"maximum_limit"`
}

type Guardrails struct {
	ForcedOffFeatures map[FeatureKey]bool
	QuotaCeilings     map[QuotaKey]int64
}

type Catalog struct {
	forcedOffFeatures map[FeatureKey]bool
	quotaCeilings     map[QuotaKey]int64
}

var featureDefinitions = map[FeatureKey]FeatureDefinition{
	FeatureMembershipInvitations: {
		Key: FeatureMembershipInvitations, DefaultEnabled: true,
	},
	FeatureClassManagement: {
		Key: FeatureClassManagement, DefaultEnabled: true,
	},
	FeatureClassInviteLinks: {
		Key: FeatureClassInviteLinks, DefaultEnabled: true,
	},
	FeatureClassSessionScheduling: {
		Key: FeatureClassSessionScheduling, DefaultEnabled: true,
	},
}

var quotaDefinitions = map[QuotaKey]QuotaDefinition{
	QuotaMembers: {
		Key: QuotaMembers, DefaultLimit: 100, MinimumLimit: 1, MaximumLimit: 10000,
	},
	QuotaActiveClasses: {
		Key: QuotaActiveClasses, DefaultLimit: 25, MinimumLimit: 1, MaximumLimit: 1000,
	},
	QuotaInviteCreationsPerHour: {
		Key:          QuotaInviteCreationsPerHour,
		DefaultLimit: 60,
		MinimumLimit: 1,
		MaximumLimit: 10000,
	},
}

func NewCatalog(guardrails Guardrails) (*Catalog, error) {
	catalog := &Catalog{
		forcedOffFeatures: make(map[FeatureKey]bool, len(guardrails.ForcedOffFeatures)),
		quotaCeilings:     make(map[QuotaKey]int64, len(guardrails.QuotaCeilings)),
	}
	for key, forcedOff := range guardrails.ForcedOffFeatures {
		if _, ok := featureDefinitions[key]; !ok || !forcedOff {
			return nil, fmt.Errorf("validate feature guardrail %q: %w", key, ErrInvalidControl)
		}
		catalog.forcedOffFeatures[key] = true
	}
	for key, ceiling := range guardrails.QuotaCeilings {
		definition, ok := quotaDefinitions[key]
		if !ok || ceiling < definition.MinimumLimit || ceiling > definition.MaximumLimit {
			return nil, fmt.Errorf("validate quota guardrail %q: %w", key, ErrInvalidControl)
		}
		catalog.quotaCeilings[key] = ceiling
	}
	return catalog, nil
}

func NewDefaultCatalog() *Catalog {
	catalog, err := NewCatalog(Guardrails{})
	if err != nil {
		panic(err)
	}
	return catalog
}

func (catalog *Catalog) Features() []FeatureDefinition {
	definitions := make([]FeatureDefinition, 0, len(featureDefinitions))
	for _, definition := range featureDefinitions {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].Key < definitions[right].Key
	})
	return definitions
}

func (catalog *Catalog) Quotas() []QuotaDefinition {
	definitions := make([]QuotaDefinition, 0, len(quotaDefinitions))
	for _, definition := range quotaDefinitions {
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(left, right int) bool {
		return definitions[left].Key < definitions[right].Key
	})
	return definitions
}

func (catalog *Catalog) FeatureDefinition(key FeatureKey) (FeatureDefinition, bool) {
	definition, ok := featureDefinitions[key]
	return definition, ok
}

func (catalog *Catalog) QuotaDefinition(key QuotaKey) (QuotaDefinition, bool) {
	definition, ok := quotaDefinitions[key]
	return definition, ok
}

func (catalog *Catalog) EvaluateFeature(
	key FeatureKey,
	tenantOverride *bool,
) (EffectiveFeature, error) {
	definition, ok := featureDefinitions[key]
	if !ok || catalog == nil {
		return EffectiveFeature{}, fmt.Errorf("evaluate feature %q: %w", key, ErrInvalidControl)
	}
	if catalog.forcedOffFeatures[key] {
		return EffectiveFeature{
			Key: key, Enabled: false, Source: ValueSourceDeploymentGuardrail,
		}, nil
	}
	if tenantOverride != nil {
		return EffectiveFeature{
			Key: key, Enabled: *tenantOverride, Source: ValueSourceTenantOverride,
		}, nil
	}
	return EffectiveFeature{
		Key: key, Enabled: definition.DefaultEnabled, Source: ValueSourceCatalogDefault,
	}, nil
}

func (catalog *Catalog) EvaluateQuota(
	key QuotaKey,
	tenantOverride *int64,
) (EffectiveQuota, error) {
	definition, ok := quotaDefinitions[key]
	if !ok || catalog == nil {
		return EffectiveQuota{}, fmt.Errorf("evaluate quota %q: %w", key, ErrInvalidControl)
	}
	limit := definition.DefaultLimit
	source := ValueSourceCatalogDefault
	if tenantOverride != nil {
		if err := validateQuotaLimit(definition, *tenantOverride); err != nil {
			return EffectiveQuota{}, err
		}
		limit = *tenantOverride
		source = ValueSourceTenantOverride
	}
	if ceiling, guarded := catalog.quotaCeilings[key]; guarded && ceiling < limit {
		limit = ceiling
		source = ValueSourceDeploymentGuardrail
	}
	return EffectiveQuota{Key: key, Limit: limit, Source: source}, nil
}

func validateQuotaLimit(definition QuotaDefinition, limit int64) error {
	if limit < definition.MinimumLimit || limit > definition.MaximumLimit {
		return fmt.Errorf(
			"quota %q limit must be between %d and %d: %w",
			definition.Key,
			definition.MinimumLimit,
			definition.MaximumLimit,
			ErrInvalidControl,
		)
	}
	return nil
}
