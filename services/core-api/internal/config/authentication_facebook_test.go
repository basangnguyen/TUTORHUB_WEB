package config

import (
	"strings"
	"testing"
)

func TestFacebookProviderConfiguration(t *testing.T) {
	cfg, err := load(mapLookup(nil))
	if err != nil || cfg.Authentication.ProviderFacebookEnabled || cfg.Authentication.FacebookIssuerURL != defaultFacebookIssuer {
		t.Fatal("unsafe Facebook defaults")
	}
	values := map[string]string{"APP_ENV": "test", "AUTH_PROVIDER_FACEBOOK_ENABLED": "true", "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false",
		"PUBLIC_API_ORIGIN": "http://localhost:8080", "DATABASE_POOL_URL": "postgres://fixture:fixture@localhost/fixture?sslmode=disable",
		"SESSION_SECRET": validSessionSecret()}
	if _, err := load(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "FACEBOOK_CLIENT_ID") || !strings.Contains(err.Error(), "FACEBOOK_CLIENT_SECRET") {
		t.Fatal("Facebook startup must require both credentials")
	}
	values["FACEBOOK_CLIENT_ID"], values["FACEBOOK_CLIENT_SECRET"] = "fixture", "fixture"
	cfg, err = load(mapLookup(values))
	if err != nil || !cfg.Authentication.Enabled || cfg.Authentication.OIDCEnabled || cfg.Authentication.FacebookCallbackURL != "http://localhost:8080/api/v1/auth/oauth/facebook/callback" {
		t.Fatal("Facebook-only startup or callback is invalid")
	}
	delete(values, "DATABASE_POOL_URL")
	delete(values, "SESSION_SECRET")
	if _, err := load(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "DATABASE_POOL_URL") || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatal("Facebook-only startup skipped session prerequisites")
	}
}

func TestFacebookIssuerOverride(t *testing.T) {
	for _, tc := range []struct {
		env, issuer string
		valid       bool
	}{
		{"production", defaultFacebookIssuer, true}, {"production", "https://fake.example", false},
		{"staging", "https://fake.example", false}, {"development", "http://localhost:9090", true},
		{"test", "http://127.0.0.1:9090", true}, {"test", "https://user:private@issuer.example", false},
		{"test", "https://issuer.example/#private", false}, {"test", "https://issuer.example/?x=private", false},
		{"test", "https://issuer.example/%private", false},
	} {
		var failures []error
		cfg := AuthenticationConfig{ProviderFacebookEnabled: true}
		configureFacebookProvider(&cfg, mapLookup(map[string]string{"FACEBOOK_CLIENT_ID": "fixture", "FACEBOOK_CLIENT_SECRET": "fixture", "FACEBOOK_ISSUER_URL": tc.issuer}), tc.env, "https://api.example.test", &failures)
		if (len(failures) == 0) != tc.valid {
			t.Fatalf("issuer validation failed for %s", tc.env)
		}
		for _, err := range failures {
			if strings.Contains(err.Error(), "private") {
				t.Fatal("invalid configuration value disclosed")
			}
		}
	}
}
