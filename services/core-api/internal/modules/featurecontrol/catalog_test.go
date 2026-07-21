package featurecontrol

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCatalogDefaultsAndStableOrder(t *testing.T) {
	t.Parallel()

	catalog := NewDefaultCatalog()
	features := catalog.Features()
	if got, want := featureKeys(features), []FeatureKey{
		FeatureClassInviteLinks,
		FeatureClassManagement,
		FeatureMembershipInvitations,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("feature catalog order = %v, want %v", got, want)
	}
	for _, definition := range features {
		if !definition.DefaultEnabled {
			t.Fatalf("feature %q must preserve the existing enabled behavior", definition.Key)
		}
	}
	quotas := catalog.Quotas()
	if got, want := quotaKeys(quotas), []QuotaKey{
		QuotaActiveClasses,
		QuotaInviteCreationsPerHour,
		QuotaMembers,
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("quota catalog order = %v, want %v", got, want)
	}
	want := map[QuotaKey]QuotaDefinition{
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
	for _, definition := range quotas {
		if definition != want[definition.Key] {
			t.Fatalf("quota definition = %+v, want %+v", definition, want[definition.Key])
		}
	}
}

func TestCatalogPrecedenceCannotBypassDeploymentGuardrails(t *testing.T) {
	t.Parallel()

	catalog, err := NewCatalog(Guardrails{
		ForcedOffFeatures: map[FeatureKey]bool{FeatureClassManagement: true},
		QuotaCeilings:     map[QuotaKey]int64{QuotaMembers: 50},
	})
	if err != nil {
		t.Fatalf("create guarded catalog: %v", err)
	}
	enabled := true
	feature, err := catalog.EvaluateFeature(FeatureClassManagement, &enabled)
	if err != nil {
		t.Fatalf("evaluate guarded feature: %v", err)
	}
	if feature.Enabled || feature.Source != ValueSourceDeploymentGuardrail {
		t.Fatalf("tenant override bypassed feature guardrail: %+v", feature)
	}
	overrideAboveCeiling := int64(80)
	quota, err := catalog.EvaluateQuota(QuotaMembers, &overrideAboveCeiling)
	if err != nil {
		t.Fatalf("evaluate guarded quota: %v", err)
	}
	if quota.Limit != 50 || quota.Source != ValueSourceDeploymentGuardrail {
		t.Fatalf("tenant override bypassed quota ceiling: %+v", quota)
	}
	overrideBelowCeiling := int64(40)
	quota, err = catalog.EvaluateQuota(QuotaMembers, &overrideBelowCeiling)
	if err != nil {
		t.Fatalf("evaluate tenant quota: %v", err)
	}
	if quota.Limit != 40 || quota.Source != ValueSourceTenantOverride {
		t.Fatalf("lower tenant override should remain effective: %+v", quota)
	}
}

func TestCatalogRejectsUnknownAndInvalidGuardrails(t *testing.T) {
	t.Parallel()

	tests := []Guardrails{
		{ForcedOffFeatures: map[FeatureKey]bool{"unknown": true}},
		{ForcedOffFeatures: map[FeatureKey]bool{FeatureClassManagement: false}},
		{QuotaCeilings: map[QuotaKey]int64{"unknown": 1}},
		{QuotaCeilings: map[QuotaKey]int64{QuotaActiveClasses: 0}},
		{QuotaCeilings: map[QuotaKey]int64{QuotaActiveClasses: 1001}},
	}
	for index, guardrails := range tests {
		if _, err := NewCatalog(guardrails); !errors.Is(err, ErrInvalidControl) {
			t.Fatalf("guardrail case %d error = %v, want invalid control", index, err)
		}
	}
	catalog := NewDefaultCatalog()
	if _, err := catalog.EvaluateFeature("unknown", nil); !errors.Is(err, ErrInvalidControl) {
		t.Fatalf("unknown feature error = %v", err)
	}
	if _, err := catalog.EvaluateQuota("unknown", nil); !errors.Is(err, ErrInvalidControl) {
		t.Fatalf("unknown quota error = %v", err)
	}
}

func TestNormalizeOverridesValidatesAggregateAndSortsCopy(t *testing.T) {
	t.Parallel()

	input := PutOverridesInput{
		ExpectedVersion: 4,
		FeatureOverrides: []FeatureOverride{
			{Key: FeatureMembershipInvitations, Enabled: false},
			{Key: FeatureClassInviteLinks, Enabled: true},
		},
		QuotaOverrides: []QuotaOverride{
			{Key: QuotaMembers, Limit: 200},
			{Key: QuotaActiveClasses, Limit: 30},
		},
	}
	normalized, err := normalizeOverrides(NewDefaultCatalog(), input)
	if err != nil {
		t.Fatalf("normalize overrides: %v", err)
	}
	if normalized.FeatureOverrides[0].Key != FeatureClassInviteLinks ||
		normalized.QuotaOverrides[0].Key != QuotaActiveClasses {
		t.Fatalf("overrides are not canonical: %+v", normalized)
	}
	if input.FeatureOverrides[0].Key != FeatureMembershipInvitations ||
		input.QuotaOverrides[0].Key != QuotaMembers {
		t.Fatal("normalization mutated caller-owned slices")
	}
	invalid := []PutOverridesInput{
		{ExpectedVersion: -1},
		{FeatureOverrides: []FeatureOverride{{Key: "unknown"}}},
		{FeatureOverrides: []FeatureOverride{
			{Key: FeatureClassManagement}, {Key: FeatureClassManagement},
		}},
		{QuotaOverrides: []QuotaOverride{{Key: "unknown", Limit: 1}}},
		{QuotaOverrides: []QuotaOverride{{Key: QuotaMembers, Limit: 0}}},
		{QuotaOverrides: []QuotaOverride{
			{Key: QuotaMembers, Limit: 1}, {Key: QuotaMembers, Limit: 2},
		}},
	}
	for index, candidate := range invalid {
		if _, err := normalizeOverrides(NewDefaultCatalog(), candidate); !errors.Is(
			err,
			ErrInvalidControl,
		) {
			t.Fatalf("invalid aggregate case %d error = %v", index, err)
		}
	}
}

func TestTypedFailuresExposeBoundedDetails(t *testing.T) {
	t.Parallel()

	featureFailure := &FeatureDisabledError{Feature: FeatureClassInviteLinks}
	if !errors.Is(featureFailure, ErrFeatureDisabled) {
		t.Fatal("feature failure must unwrap to ErrFeatureDisabled")
	}
	resetAt := time.Date(2026, 7, 20, 10, 0, 0, 0, time.UTC)
	quotaFailure := &QuotaExceededError{
		Quota: QuotaInviteCreationsPerHour, Limit: 60, Used: 60,
		ResetAt: resetAt, RetryAfter: 30 * time.Second,
	}
	if !errors.Is(quotaFailure, ErrQuotaExceeded) || quotaFailure.ResetAt != resetAt {
		t.Fatalf("unexpected quota failure: %+v", quotaFailure)
	}
	versionFailure := &VersionConflictError{Expected: 2, Current: 3}
	if !errors.Is(versionFailure, ErrVersionConflict) {
		t.Fatal("version failure must unwrap to ErrVersionConflict")
	}
}

func TestTenantControlLockKeyIsStableAndTenantScoped(t *testing.T) {
	t.Parallel()

	tenantA := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tenantB := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	if tenantControlLockKey(tenantA) != tenantControlLockKey(tenantA) {
		t.Fatal("tenant advisory lock key is not deterministic")
	}
	if tenantControlLockKey(tenantA) == tenantControlLockKey(tenantB) {
		t.Fatal("different tenants unexpectedly share an advisory lock key")
	}
}

func featureKeys(definitions []FeatureDefinition) []FeatureKey {
	keys := make([]FeatureKey, 0, len(definitions))
	for _, definition := range definitions {
		keys = append(keys, definition.Key)
	}
	return keys
}

func quotaKeys(definitions []QuotaDefinition) []QuotaKey {
	keys := make([]QuotaKey, 0, len(definitions))
	for _, definition := range definitions {
		keys = append(keys, definition.Key)
	}
	return keys
}
