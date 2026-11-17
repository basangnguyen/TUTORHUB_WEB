package config

import (
	"strings"
	"testing"
)

func TestNativeAuthenticationConfigurationWithoutOIDC(t *testing.T) {
	for _, environment := range []string{"development", "test", "staging", "production"} {
		values := map[string]string{
			"APP_ENV": environment, "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false",
			"AUTH_SIGNUP_ENABLED": "true", "AUTH_METHOD_PASSWORD_ENABLED": "true",
			"SESSION_SECRET": validSessionSecret(), "DATABASE_POOL_URL": "postgres://native:fixture@localhost/native_test?sslmode=disable",
			"PUBLIC_WEB_ORIGIN": "https://web.example.test", "PUBLIC_API_ORIGIN": "https://api.example.test",
		}
		// Exercise auth validation independently of unrelated staging services.
		var failures []error
		cfg := authenticationConfig(mapLookup(values), environment, values["PUBLIC_API_ORIGIN"], values["PUBLIC_WEB_ORIGIN"], &failures)
		if len(failures) != 0 || !cfg.Enabled || cfg.OIDCEnabled || len(cfg.SessionKey) != 32 {
			t.Fatalf("native-only %s config failed: %v", environment, failures)
		}
		if cfg.LegalTermsVersion != "2026-11-16-v2.0" || cfg.PasswordHashMaxConcurrency != 2 || cfg.PasswordBreachCheck != "offline" {
			t.Fatal("native password defaults differ from policy")
		}
		for _, missing := range []string{"SESSION_SECRET", "DATABASE_POOL_URL"} {
			original := values[missing]
			delete(values, missing)
			failures = nil
			_ = authenticationConfig(mapLookup(values), environment, values["PUBLIC_API_ORIGIN"], values["PUBLIC_WEB_ORIGIN"], &failures)
			if len(failures) == 0 || !strings.Contains(failures[0].Error(), missing) {
				t.Errorf("native-only config accepted missing %s", missing)
			}
			values[missing] = original
		}
	}
}

func TestNativePasswordConfigurationBounds(t *testing.T) {
	for _, entry := range []struct{ key, value string }{
		{"PASSWORD_HASH_MAX_CONCURRENCY", "0"}, {"PASSWORD_HASH_MAX_CONCURRENCY", "33"},
		{"AUTH_PASSWORD_BREACH_CHECK", "unknown"}, {"AUTH_LEGAL_TERMS_VERSION", " "},
		{"AUTH_LEGAL_TERMS_VERSION", strings.Repeat("a", 65)},
	} {
		_, err := load(mapLookup(map[string]string{entry.key: entry.value}))
		if err == nil || !strings.Contains(err.Error(), entry.key) {
			t.Fatalf("accepted invalid %s", entry.key)
		}
	}
}

func TestEmailCodeOnlyAuthenticationRequiresNativePrerequisites(t *testing.T) {
	for _, environment := range []string{"development", "test", "staging", "production"} {
		values := map[string]string{
			"AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false", "AUTH_METHOD_EMAIL_CODE_ENABLED": "true",
			"SESSION_SECRET": validSessionSecret(), "DATABASE_POOL_URL": "postgres://fixture@localhost/auth_test?sslmode=disable",
		}
		var failures []error
		cfg := authenticationConfig(mapLookup(values), environment, "https://api.example.test", "https://web.example.test", &failures)
		if len(failures) != 0 || !cfg.Enabled || cfg.OIDCEnabled || cfg.PasswordEnabled || cfg.SignupEnabled || !cfg.EmailCodeEnabled {
			t.Fatalf("email-code-only %s startup failed: %v", environment, failures)
		}
		for _, missing := range []string{"SESSION_SECRET", "DATABASE_POOL_URL"} {
			original := values[missing]
			delete(values, missing)
			failures = nil
			_ = authenticationConfig(mapLookup(values), environment, "https://api.example.test", "https://web.example.test", &failures)
			if len(failures) == 0 {
				t.Fatalf("email-code-only config accepted missing %s", missing)
			}
			values[missing] = original
		}
	}
}
