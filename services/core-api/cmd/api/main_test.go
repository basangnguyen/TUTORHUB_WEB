package main

import (
	"testing"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/featurecontrol"
)

func TestFeatureControlGuardrailsOmitFeaturesThatAreNotForcedOff(t *testing.T) {
	t.Parallel()

	configuration := config.FeatureControlConfig{
		DisableClassManagement:    true,
		MaxMembers:                10_000,
		MaxActiveClasses:          1_000,
		MaxInviteCreationsPerHour: 10_000,
	}
	guardrails := featureControlGuardrails(configuration)

	if len(guardrails.ForcedOffFeatures) != 2 ||
		!guardrails.ForcedOffFeatures[featurecontrol.FeatureClassManagement] {
		t.Fatalf("unexpected forced-off feature map: %+v", guardrails.ForcedOffFeatures)
	}
	if !guardrails.ForcedOffFeatures[featurecontrol.FeatureInAppNotifications] {
		t.Fatalf("notification visibility must fail closed by default: %+v", guardrails.ForcedOffFeatures)
	}
	if _, err := featurecontrol.NewCatalog(guardrails); err != nil {
		t.Fatalf("valid runtime configuration must initialize the catalog: %v", err)
	}
}
