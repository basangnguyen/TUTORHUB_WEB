package config

import (
	"slices"
	"testing"
)

func TestPasskeyRPConfiguration(t *testing.T) {
	foundation := map[string]string{"AUTH_PASSKEYS_ENABLED": "true", "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false",
		"SESSION_SECRET": validSessionSecret(), "DATABASE_POOL_URL": "postgres://fixture:fixture@localhost/fixture?sslmode=disable"}
	cfg, err := load(mapLookup(foundation))
	if err != nil || !cfg.Authentication.Enabled || !cfg.Authentication.PasskeysAvailable() ||
		cfg.Authentication.WebAuthnRPID != "localhost" || cfg.Authentication.WebAuthnRPName != "TutorHub" ||
		!slices.Equal(cfg.Authentication.WebAuthnRPOrigins, []string{defaultWebOrigin}) {
		t.Fatalf("unexpected passkey-only default configuration: %v", err)
	}
	for _, tt := range []struct {
		env, id, origins string
		valid            bool
	}{
		{"production", "example.org", "https://class.example.org:443/,https://staff.example.org:8443", true},
		{"test", "localhost", "http://localhost:5173", true},
		{"development", "localhost", "http://localhost:5173", true},
		{"production", "localhost", "http://localhost:5173", false},
		{"staging", "example.org", "http://example.org", false},
		{"development", "example.org", "http://example.org", false},
		{"production", "example.org", "https://example.org.evil.com", false},
		{"production", "example.org", "https://badexample.org", false},
		{"production", "example.org", "https://*.example.org", false},
		{"production", "co.uk", "https://school.co.uk", false},
		{"production", "github.io", "https://school.github.io", false},
		{"production", "example.org", "https://one.example.org,https://test.invalid", false},
		{"production", "example.org", "https://user@example.org", false},
		{"production", "example.org", "https://example.org/foo", false},
		{"production", "example.org", "https://example.org?", false},
		{"production", "example.org", "https://example.org#", false},
		{"production", "example.org", "https://example.org:65536", false},
		{"production", "127.0.0.1", "https://127.0.0.1", false},
		{"production", "https://example.org", "https://example.org", false},
	} {
		t.Run(tt.env+"-"+tt.origins, func(t *testing.T) {
			c := AuthenticationConfig{PasskeysEnabled: true}
			var errors []error
			configurePasskeys(&c, mapLookup(map[string]string{"WEBAUTHN_RP_ID": tt.id, "WEBAUTHN_RP_ORIGINS": tt.origins}), tt.env, "https://example.org", &errors)
			if (len(errors) == 0) != tt.valid || c.PasskeysAvailable() != tt.valid {
				t.Fatalf("want valid=%v, availability=%v errors=%v", tt.valid, c.PasskeysAvailable(), errors)
			}
		})
	}
}
