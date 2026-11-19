package config

import "testing"

func TestE2EIPRateLimitMultiplierIsTestOnly(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		environment, value string
		want               int
		wantErr            bool
	}{
		{"production", "", 1, false},
		{"test", "", 1, false},
		{"test", "10", 10, false},
		{"test", "100", 100, false},
		{"test", "0", 1, true},
		{"test", "101", 1, true},
		{"development", "10", 10, true},
		{"staging", "10", 10, true},
		{"production", "10", 10, true},
		{"production", "1", 1, false},
	} {
		values := map[string]string{}
		if test.value != "" {
			values["AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER"] = test.value
		}
		var failures []error
		var cfg AuthenticationConfig
		configureAuthFoundation(&cfg, mapLookup(values), test.environment, &failures)
		if (len(failures) > 0) != test.wantErr {
			t.Errorf("%s=%q errors=%v", test.environment, test.value, failures)
		}
		if !test.wantErr && cfg.E2EIPRateLimitMultiplier != test.want {
			t.Errorf("%s=%q multiplier=%d", test.environment, test.value, cfg.E2EIPRateLimitMultiplier)
		}
	}
}
