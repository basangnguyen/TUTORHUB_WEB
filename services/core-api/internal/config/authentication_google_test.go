package config

import (
	"strings"
	"testing"
)

func TestGoogleProviderDefaultsAndPrerequisites(t *testing.T) {
	cfg, err := load(mapLookup(nil))
	if err != nil || cfg.Authentication.ProviderGoogleEnabled || cfg.Authentication.GoogleIssuerURL != defaultGoogleIssuer {
		t.Fatal("Google must remain disabled with the production issuer default")
	}
	values := map[string]string{
		"APP_ENV": "test", "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false", "AUTH_PROVIDER_GOOGLE_ENABLED": "true",
		"PUBLIC_API_ORIGIN": "http://localhost:8080", "SESSION_SECRET": validSessionSecret(),
		"DATABASE_POOL_URL": "postgres://fixture:fixture@localhost/fixture?sslmode=disable",
	}
	if _, err := load(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") || !strings.Contains(err.Error(), "GOOGLE_CLIENT_SECRET") {
		t.Fatal("Google enabled without credentials must fail startup")
	}
	values["GOOGLE_CLIENT_ID"], values["GOOGLE_CLIENT_SECRET"] = "fixture-client", "fixture-secret"
	cfg, err = load(mapLookup(values))
	if err != nil || !cfg.Authentication.Enabled || cfg.Authentication.OIDCEnabled || cfg.Authentication.GoogleCallbackURL != "http://localhost:8080/api/v1/auth/oauth/google/callback" {
		t.Fatal("Google-only startup or derived callback is invalid")
	}
	delete(values, "DATABASE_POOL_URL")
	delete(values, "SESSION_SECRET")
	if _, err := load(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "DATABASE_POOL_URL") || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatal("Google requires database and session cryptography")
	}
}

func TestGoogleIssuerOverrideRestrictedToDevelopmentAndTest(t *testing.T) {
	for _, environment := range []string{"development", "test", "staging", "production"} {
		for _, issuer := range []string{"", defaultGoogleIssuer, "http://127.0.0.1:4567"} {
			var failures []error
			cfg := AuthenticationConfig{ProviderGoogleEnabled: true}
			configureGoogleProvider(&cfg, mapLookup(map[string]string{"GOOGLE_CLIENT_ID": "fixture", "GOOGLE_CLIENT_SECRET": "fixture", "GOOGLE_ISSUER_URL": issuer}), environment, "https://api.example.test", &failures)
			wantFailure := (environment == "staging" || environment == "production") && issuer != "" && issuer != defaultGoogleIssuer
			if (len(failures) > 0) != wantFailure {
				t.Fatalf("issuer override boundary failed for %s", environment)
			}
		}
	}
	for _, issuer := range []string{"https://user:secret@example.test", "https://accounts.google.com/#fragment", "file:///fixture"} {
		var failures []error
		cfg := AuthenticationConfig{ProviderGoogleEnabled: true}
		configureGoogleProvider(&cfg, mapLookup(map[string]string{"GOOGLE_CLIENT_ID": "fixture", "GOOGLE_CLIENT_SECRET": "fixture", "GOOGLE_ISSUER_URL": issuer}), "test", "https://api.example.test", &failures)
		if len(failures) == 0 {
			t.Fatal("malformed or credential-bearing issuer accepted")
		}
		for _, failure := range failures {
			if strings.Contains(failure.Error(), "user:secret") {
				t.Fatal("configuration error leaked credential")
			}
		}
	}
}
