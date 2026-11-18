package config

import (
	"fmt"
	"strings"
)

const defaultGoogleIssuer = "https://accounts.google.com"

func configureGoogleProvider(cfg *AuthenticationConfig, lookup lookupEnv, environment, apiOrigin string, validationErrors *[]error) {
	cfg.GoogleClientID = strings.TrimSpace(valueOrDefault(lookup, "GOOGLE_CLIENT_ID", ""))
	cfg.GoogleClientSecret = strings.TrimSpace(valueOrDefault(lookup, "GOOGLE_CLIENT_SECRET", ""))
	cfg.GoogleIssuerURL = strings.TrimSpace(valueOrDefault(lookup, "GOOGLE_ISSUER_URL", ""))
	if cfg.GoogleIssuerURL == "" {
		cfg.GoogleIssuerURL = defaultGoogleIssuer
	}
	cfg.GoogleCallbackURL = strings.TrimRight(apiOrigin, "/") + "/api/v1/auth/oauth/google/callback"
	if cfg.GoogleIssuerURL != defaultGoogleIssuer && environment != "development" && environment != "test" {
		*validationErrors = append(*validationErrors, fmt.Errorf("GOOGLE_ISSUER_URL overrides require development or test"))
	}
	if !cfg.ProviderGoogleEnabled {
		return
	}
	if cfg.GoogleClientID == "" {
		*validationErrors = append(*validationErrors, fmt.Errorf("GOOGLE_CLIENT_ID is required when Google authentication is enabled"))
	}
	if cfg.GoogleClientSecret == "" {
		*validationErrors = append(*validationErrors, fmt.Errorf("GOOGLE_CLIENT_SECRET is required when Google authentication is enabled"))
	}
	for key, value := range map[string]string{"GOOGLE_ISSUER_URL": cfg.GoogleIssuerURL, "GOOGLE_CALLBACK_URL": cfg.GoogleCallbackURL} {
		if err := validateHTTPSURL(environment, key, value); err != nil {
			*validationErrors = append(*validationErrors, err)
		}
	}
}
