package config

import (
	"fmt"
	"net/url"
	"strings"
)

const defaultMicrosoftAuthority = "https://login.microsoftonline.com"

func configureMicrosoftProvider(cfg *AuthenticationConfig, lookup lookupEnv, environment, apiOrigin string, validationErrors *[]error) {
	cfg.MicrosoftClientID = strings.TrimSpace(valueOrDefault(lookup, "MICROSOFT_CLIENT_ID", ""))
	cfg.MicrosoftClientSecret = strings.TrimSpace(valueOrDefault(lookup, "MICROSOFT_CLIENT_SECRET", ""))
	cfg.MicrosoftCallbackURL = strings.TrimRight(apiOrigin, "/") + "/api/v1/auth/oauth/microsoft/callback"

	cfg.MicrosoftTenantMode = strings.TrimSpace(valueOrDefault(lookup, "MICROSOFT_TENANT_MODE", ""))
	switch cfg.MicrosoftTenantMode {
	case "":
		cfg.MicrosoftTenantMode = "common"
	case "common", "organizations", "consumers":
	default:
		*validationErrors = append(*validationErrors, fmt.Errorf("MICROSOFT_TENANT_MODE must be common, organizations or consumers"))
	}
	cfg.MicrosoftAllowedTenantIDs = microsoftTenantIDs(lookup, "MICROSOFT_ALLOWED_TENANT_IDS", validationErrors)
	cfg.MicrosoftBlockedTenantIDs = microsoftTenantIDs(lookup, "MICROSOFT_BLOCKED_TENANT_IDS", validationErrors)

	authority := strings.TrimSpace(valueOrDefault(lookup, "MICROSOFT_AUTHORITY_URL", ""))
	if authority == "" {
		authority = defaultMicrosoftAuthority
	}
	cfg.MicrosoftAuthorityURL = strings.TrimSuffix(authority, "/")
	if cfg.MicrosoftAuthorityURL != defaultMicrosoftAuthority {
		if environment != "development" && environment != "test" {
			*validationErrors = append(*validationErrors, fmt.Errorf("MICROSOFT_AUTHORITY_URL overrides require development or test"))
		} else {
			u, err := url.Parse(cfg.MicrosoftAuthorityURL)
			if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || cfg.MicrosoftAuthorityURL != u.Scheme+"://"+u.Host {
				*validationErrors = append(*validationErrors, fmt.Errorf("MICROSOFT_AUTHORITY_URL must be an HTTP(S) origin with only scheme and host"))
			}
		}
	}

	if !cfg.ProviderMicrosoftEnabled {
		return
	}
	if cfg.MicrosoftClientID == "" {
		*validationErrors = append(*validationErrors, fmt.Errorf("MICROSOFT_CLIENT_ID is required when Microsoft authentication is enabled"))
	}
	if cfg.MicrosoftClientSecret == "" {
		*validationErrors = append(*validationErrors, fmt.Errorf("MICROSOFT_CLIENT_SECRET is required when Microsoft authentication is enabled"))
	}
	if validateHTTPSURL(environment, "MICROSOFT_CALLBACK_URL", cfg.MicrosoftCallbackURL) != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("Microsoft authentication requires a valid PUBLIC_API_ORIGIN for its callback"))
	}
}

func microsoftTenantIDs(lookup lookupEnv, key string, validationErrors *[]error) []string {
	var ids []string
	for _, id := range uuidListValue(lookup, key, validationErrors) {
		ids = append(ids, id.String())
	}
	return ids
}
