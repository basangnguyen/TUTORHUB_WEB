package featurecontrol

import "testing"

func TestClassSessionSchedulingIsAStableFeature(t *testing.T) {
	t.Parallel()
	catalog := NewDefaultCatalog()
	definition, ok := catalog.FeatureDefinition(FeatureClassSessionScheduling)
	if !ok || !definition.DefaultEnabled {
		t.Fatalf("class session scheduling must be enabled by default: %+v %t", definition, ok)
	}
}
