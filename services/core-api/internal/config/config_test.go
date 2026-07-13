package config

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapLookup(nil))
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}

	if cfg.Environment != "development" {
		t.Fatalf("expected development environment, got %q", cfg.Environment)
	}
	if cfg.Address() != ":8080" {
		t.Fatalf("expected :8080 address, got %q", cfg.Address())
	}
	if cfg.WebOrigin != defaultWebOrigin {
		t.Fatalf("expected default web origin, got %q", cfg.WebOrigin)
	}
	if cfg.APIOrigin != defaultAPIOrigin {
		t.Fatalf("expected default API origin, got %q", cfg.APIOrigin)
	}
	if cfg.ReadHeaderTimeout != defaultReadHeaderTimeout ||
		cfg.ReadTimeout != defaultReadTimeout ||
		cfg.WriteTimeout != defaultWriteTimeout ||
		cfg.IdleTimeout != defaultIdleTimeout ||
		cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Fatalf("unexpected timeout defaults: %+v", cfg)
	}
	if cfg.MaxHeaderBytes != defaultMaxHeaderBytes {
		t.Fatalf("expected max header bytes %d, got %d", defaultMaxHeaderBytes, cfg.MaxHeaderBytes)
	}
	if cfg.Database.PoolURL != "" ||
		cfg.Database.MaxConnections != defaultDBMaxConnections ||
		cfg.Database.MinConnections != defaultDBMinConnections ||
		cfg.Database.QueryTimeout != defaultDBQueryTimeout {
		t.Fatalf("unexpected database defaults: %+v", cfg.Database)
	}
	if cfg.Authentication.Enabled {
		t.Fatal("authentication must remain disabled when no OIDC values are configured locally")
	}
}

func TestLoadCustomValues(t *testing.T) {
	t.Parallel()

	cfg, err := load(mapLookup(map[string]string{
		"APP_ENV":                  " staging ",
		"PORT":                     "9090",
		"PUBLIC_WEB_ORIGIN":        "https://staging.tutorhub.example",
		"PUBLIC_API_ORIGIN":        "https://api.staging.tutorhub.example",
		"LOG_LEVEL":                "DEBUG",
		"HTTP_READ_TIMEOUT":        "20s",
		"HTTP_SHUTDOWN_TIMEOUT":    "25s",
		"HTTP_MAX_HEADER_BYTES":    "2097152",
		"HTTP_WRITE_TIMEOUT":       "45s",
		"HTTP_IDLE_TIMEOUT":        "2m",
		"HTTP_READ_HEADER_TIMEOUT": "7s",
		"DATABASE_POOL_URL":        "postgresql://app:secret@db.example/tutorhub?sslmode=require",
		"DATABASE_MAX_CONNECTIONS": "8",
		"DATABASE_MIN_CONNECTIONS": "2",
		"DATABASE_QUERY_TIMEOUT":   "9s",
		"OIDC_ISSUER_URL":          "https://login.staging.tutorhub.example",
		"OIDC_CLIENT_ID":           "tutorhub-staging",
		"OIDC_CLIENT_SECRET":       "not-a-real-secret",
		"SESSION_SECRET":           validSessionSecret(),
		"SESSION_COOKIE_SECURE":    "true",
		"SESSION_TTL":              "6h",
		"SESSION_ABSOLUTE_TTL":     "24h",
	}))
	if err != nil {
		t.Fatalf("load custom values: %v", err)
	}

	if cfg.Environment != "staging" || cfg.Port != "9090" || cfg.LogLevel != "debug" {
		t.Fatalf("unexpected custom configuration: %+v", cfg)
	}
	if cfg.ReadTimeout != 20*time.Second ||
		cfg.ShutdownTimeout != 25*time.Second ||
		cfg.WriteTimeout != 45*time.Second ||
		cfg.IdleTimeout != 2*time.Minute ||
		cfg.ReadHeaderTimeout != 7*time.Second {
		t.Fatalf("unexpected custom timeouts: %+v", cfg)
	}
	if cfg.MaxHeaderBytes != 2<<20 {
		t.Fatalf("expected 2 MiB max header bytes, got %d", cfg.MaxHeaderBytes)
	}
	if cfg.Database.MaxConnections != 8 ||
		cfg.Database.MinConnections != 2 ||
		cfg.Database.QueryTimeout != 9*time.Second {
		t.Fatalf("unexpected custom database config: %+v", cfg.Database)
	}
	if !cfg.Authentication.Enabled ||
		cfg.Authentication.ClientID != "tutorhub-staging" ||
		cfg.Authentication.SessionTTL != 6*time.Hour ||
		!cfg.Authentication.CookieSecure ||
		len(cfg.Authentication.SessionKey) != 32 {
		t.Fatalf("unexpected authentication config")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{
		"APP_ENV":                  "unknown",
		"PORT":                     "70000",
		"PUBLIC_WEB_ORIGIN":        "ftp://example.com/path",
		"LOG_LEVEL":                "verbose",
		"HTTP_READ_TIMEOUT":        "0s",
		"HTTP_MAX_HEADER_BYTES":    "1",
		"DATABASE_POOL_URL":        "https://not-postgres.example",
		"DATABASE_MIN_CONNECTIONS": "10",
		"DATABASE_MAX_CONNECTIONS": "2",
	}))
	if err == nil {
		t.Fatal("expected validation error")
	}

	message := err.Error()
	for _, expected := range []string{
		"APP_ENV",
		"PORT",
		"PUBLIC_WEB_ORIGIN",
		"LOG_LEVEL",
		"HTTP_READ_TIMEOUT",
		"HTTP_MAX_HEADER_BYTES",
		"DATABASE_POOL_URL",
		"DATABASE_MIN_CONNECTIONS",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected error to mention %s, got %q", expected, message)
		}
	}
}

func TestLoadRequiresDatabaseOutsideLocalEnvironments(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{
		"APP_ENV":           "staging",
		"PUBLIC_WEB_ORIGIN": "https://staging.tutorhub.example",
	}))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_POOL_URL is required") {
		t.Fatalf("expected database requirement error, got %v", err)
	}
}

func TestLoadRequiresAuthenticationOutsideLocalEnvironments(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{
		"APP_ENV":           "staging",
		"PUBLIC_WEB_ORIGIN": "https://staging.tutorhub.example",
		"PUBLIC_API_ORIGIN": "https://api.staging.tutorhub.example",
		"DATABASE_POOL_URL": "postgresql://app:secret@db.example/tutorhub?sslmode=require",
	}))
	if err == nil || !strings.Contains(err.Error(), "OIDC_ISSUER_URL is required") {
		t.Fatalf("expected authentication requirement error, got %v", err)
	}
}

func TestLoadRejectsPartialOrWeakAuthenticationConfiguration(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{
		"OIDC_ISSUER_URL":       "http://localhost:9090",
		"OIDC_CLIENT_ID":        "local-client",
		"SESSION_SECRET":        base64.RawURLEncoding.EncodeToString([]byte("too-short")),
		"SESSION_TTL":           "48h",
		"SESSION_ABSOLUTE_TTL":  "24h",
		"AUTH_FLOW_TTL":         "20m",
		"SESSION_COOKIE_SECURE": "not-a-boolean",
	}))
	if err == nil {
		t.Fatal("expected authentication validation error")
	}

	message := err.Error()
	for _, expected := range []string{
		"OIDC_CLIENT_SECRET",
		"SESSION_SECRET",
		"SESSION_TTL",
		"AUTH_FLOW_TTL",
		"SESSION_COOKIE_SECURE",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected auth error to mention %s, got %q", expected, message)
		}
	}
}

func TestLoadRequiresHTTPSOutsideLocalEnvironments(t *testing.T) {
	t.Parallel()

	_, err := load(mapLookup(map[string]string{
		"APP_ENV":           "production",
		"PUBLIC_WEB_ORIGIN": "http://tutorhub.example",
	}))
	if err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Fatalf("expected HTTPS validation error, got %v", err)
	}
}

func mapLookup(values map[string]string) lookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func validSessionSecret() string {
	return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
}
