package config

import (
	"fmt"
	"net/url"
	"strings"
)

const defaultFacebookIssuer = "https://www.facebook.com"

func configureFacebookProvider(cfg *AuthenticationConfig, lookup lookupEnv, environment, apiOrigin string, validationErrors *[]error) {
	cfg.FacebookClientID = strings.TrimSpace(valueOrDefault(lookup, "FACEBOOK_CLIENT_ID", ""))
	cfg.FacebookClientSecret = strings.TrimSpace(valueOrDefault(lookup, "FACEBOOK_CLIENT_SECRET", ""))
	cfg.FacebookIssuerURL = strings.TrimRight(strings.TrimSpace(valueOrDefault(lookup, "FACEBOOK_ISSUER_URL", "")), "/")
	if cfg.FacebookIssuerURL == "" {
		cfg.FacebookIssuerURL = defaultFacebookIssuer
	}
	cfg.FacebookCallbackURL = strings.TrimRight(apiOrigin, "/") + "/api/v1/auth/oauth/facebook/callback"
	if cfg.FacebookIssuerURL != defaultFacebookIssuer && environment != "development" && environment != "test" {
		*validationErrors = append(*validationErrors, fmt.Errorf("FACEBOOK_ISSUER_URL overrides require development or test"))
	}
	if !cfg.ProviderFacebookEnabled {
		return
	}
	if cfg.FacebookClientID == "" {
		*validationErrors = append(*validationErrors, fmt.Errorf("FACEBOOK_CLIENT_ID is required when Facebook authentication is enabled"))
	}
	if cfg.FacebookClientSecret == "" {
		*validationErrors = append(*validationErrors, fmt.Errorf("FACEBOOK_CLIENT_SECRET is required when Facebook authentication is enabled"))
	}
	parsed, err := url.Parse(cfg.FacebookIssuerURL)
	if err != nil || parsed.ForceQuery || parsed.RawQuery != "" || validateHTTPSURL(environment, "FACEBOOK_ISSUER_URL", cfg.FacebookIssuerURL) != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("FACEBOOK_ISSUER_URL must be an issuer URL without credentials, query, or fragment"))
	}
	if err := validateHTTPSURL(environment, "FACEBOOK_CALLBACK_URL", cfg.FacebookCallbackURL); err != nil {
		*validationErrors = append(*validationErrors, err)
	}
}
