package config

import (
	"slices"
	"strings"
	"testing"
)

func TestMicrosoftProviderConfiguration(t *testing.T) {
	cfg, err := load(mapLookup(nil))
	if err != nil || cfg.Authentication.ProviderMicrosoftEnabled || cfg.Authentication.MicrosoftAuthorityURL != defaultMicrosoftAuthority ||
		cfg.Authentication.MicrosoftTenantMode != "common" || len(cfg.Authentication.MicrosoftAllowedTenantIDs) != 0 || len(cfg.Authentication.MicrosoftBlockedTenantIDs) != 0 {
		t.Fatal("Microsoft must default off, with common authority and unrestricted tenant lists")
	}
	values := map[string]string{"APP_ENV": "test", "AUTH_PROVIDER_MICROSOFT_ENABLED": "true", "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false"}
	if _, err := load(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "MICROSOFT_CLIENT_ID") ||
		!strings.Contains(err.Error(), "MICROSOFT_CLIENT_SECRET") || !strings.Contains(err.Error(), "DATABASE_POOL_URL") || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatal("Microsoft-only authentication must validate credentials, database and session secret")
	}
	values["MICROSOFT_CLIENT_ID"], values["MICROSOFT_CLIENT_SECRET"] = "fixture-id", "fixture-secret"
	values["SESSION_SECRET"] = validSessionSecret()
	values["DATABASE_POOL_URL"] = "postgres://fixture:fixture@localhost/fixture?sslmode=disable"
	values["PUBLIC_API_ORIGIN"] = "http://localhost:8080"
	values["MICROSOFT_AUTHORITY_URL"] = "http://127.0.0.1:9876/"
	values["MICROSOFT_ALLOWED_TENANT_IDS"] = " 9188040D-6C67-4C5B-B112-36A304B66DAD , aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	values["MICROSOFT_BLOCKED_TENANT_IDS"] = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"
	for _, mode := range []string{"common", "organizations", "consumers"} {
		values["MICROSOFT_TENANT_MODE"] = mode
		cfg, err = load(mapLookup(values))
		auth := cfg.Authentication
		if err != nil || !auth.ProviderMicrosoftEnabled || !auth.Enabled || auth.OIDCEnabled || auth.MicrosoftTenantMode != mode ||
			auth.MicrosoftClientID != "fixture-id" || auth.MicrosoftClientSecret != "fixture-secret" ||
			auth.MicrosoftCallbackURL != "http://localhost:8080/api/v1/auth/oauth/microsoft/callback" || auth.MicrosoftAuthorityURL != "http://127.0.0.1:9876" ||
			!slices.Equal(auth.MicrosoftAllowedTenantIDs, []string{"9188040d-6c67-4c5b-b112-36a304b66dad", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}) ||
			!slices.Equal(auth.MicrosoftBlockedTenantIDs, []string{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}) {
			t.Fatalf("incorrect configuration for mode %s: %v", mode, err)
		}
	}
}

func TestMicrosoftProviderRejectsInvalidConfiguration(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"MICROSOFT_AUTHORITY_URL", "https://user:sensitive-fixture@test.invalid"},
		{"MICROSOFT_AUTHORITY_URL", "https://test.invalid:invalid-sensitive-fixture"},
		{"MICROSOFT_AUTHORITY_URL", "https://test.invalid/path"},
		{"MICROSOFT_AUTHORITY_URL", "https://test.invalid?"},
		{"MICROSOFT_AUTHORITY_URL", "https://test.invalid#"},
		{"MICROSOFT_AUTHORITY_URL", "https://test.invalid?q=sensitive-fixture"},
		{"MICROSOFT_AUTHORITY_URL", "file:///fixture"},
		{"MICROSOFT_AUTHORITY_URL", "https://:443"},
		{"MICROSOFT_TENANT_MODE", "tenant/sensitive-fixture"},
		{"MICROSOFT_ALLOWED_TENANT_IDS", "sensitive-fixture"},
		{"MICROSOFT_BLOCKED_TENANT_IDS", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee,"},
		{"MICROSOFT_BLOCKED_TENANT_IDS", "00000000-0000-0000-0000-000000000000"},
	} {
		_, err := load(mapLookup(map[string]string{"APP_ENV": "test", tc.key: tc.value}))
		if err == nil || !strings.Contains(err.Error(), tc.key) || strings.Contains(err.Error(), "sensitive-fixture") {
			t.Errorf("%s should fail startup without echoing values", tc.key)
		}
	}
	for _, env := range []string{"development", "test", "staging", "production"} {
		for _, authority := range []string{"", defaultMicrosoftAuthority, "https://fake-authority.example.test"} {
			var failures []error
			cfg := AuthenticationConfig{}
			configureMicrosoftProvider(&cfg, mapLookup(map[string]string{"MICROSOFT_AUTHORITY_URL": authority}), env, "https://api.example.test", &failures)
			allowed := authority == "" || authority == defaultMicrosoftAuthority || env == "development" || env == "test"
			if (len(failures) == 0) != allowed {
				t.Errorf("unexpected authority override result for %s", env)
			}
		}
	}
}
