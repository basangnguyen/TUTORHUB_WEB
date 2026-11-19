package identity

import (
	"fmt"
	"net/url"
	"strings"
)

type ProviderRegistration struct {
	Slug, Issuer string
	Provider     Provider
}

// Registry entries are immutable startup configuration; callback input never
// chooses a remote issuer or token endpoint.
type ProviderRegistry struct {
	providers map[string]ProviderRegistration
}

func NewProviderRegistry(entries ...ProviderRegistration) (*ProviderRegistry, error) {
	result := &ProviderRegistry{providers: make(map[string]ProviderRegistration, len(entries))}
	for _, entry := range entries {
		switch entry.Slug {
		case "oidc", "google", "facebook", "microsoft", "apple":
		default:
			return nil, fmt.Errorf("unsupported federation provider")
		}
		if entry.Provider == nil || entry.Issuer == "" {
			return nil, fmt.Errorf("incomplete federation provider")
		}
		if _, exists := result.providers[entry.Slug]; exists {
			return nil, fmt.Errorf("duplicate federation provider")
		}
		parsed, err := url.Parse(entry.Issuer)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return nil, fmt.Errorf("invalid federation issuer")
		}
		entry.Issuer = strings.TrimRight(entry.Issuer, "/")
		if entry.Slug == "microsoft" {
			if _, ok := entry.Provider.(microsoftIssuerPolicy); !ok || entry.Issuer != MicrosoftAuthorityURL {
				return nil, fmt.Errorf("Microsoft provider requires a tenant issuer policy")
			}
		}
		for _, other := range result.providers {
			if microsoftIssuerOverlap(entry, other) || microsoftIssuerOverlap(other, entry) {
				return nil, fmt.Errorf("Microsoft tenant issuers cannot be shared with another provider")
			}
			if (entry.Slug == "facebook" || other.Slug == "facebook") && other.Issuer == entry.Issuer {
				return nil, fmt.Errorf("Facebook issuer cannot be shared with another provider")
			}
		}
		result.providers[entry.Slug] = entry
	}
	return result, nil
}

func (registry *ProviderRegistry) lookup(slug string) (ProviderRegistration, error) {
	if registry == nil {
		return ProviderRegistration{}, ErrFederationProviderUnavailable
	}
	entry, ok := registry.providers[slug]
	if !ok {
		return ProviderRegistration{}, ErrFederationProviderUnavailable
	}
	return entry, nil
}

type microsoftIssuerPolicy interface {
	matchesMicrosoftIssuer(string) bool
}

func microsoftIssuerOverlap(entry, other ProviderRegistration) bool {
	return entry.Slug == "microsoft" && (other.Issuer == MicrosoftAuthorityURL || microsoftIssuerTenant(other.Issuer) != "")
}

func (entry ProviderRegistration) matchesIssuer(issuer string) bool {
	if entry.Slug == "microsoft" {
		policy, ok := entry.Provider.(microsoftIssuerPolicy)
		return ok && policy.matchesMicrosoftIssuer(issuer)
	}
	if entry.Slug == "google" && entry.Issuer == GoogleIssuerURL {
		return issuer == GoogleIssuerURL || issuer == "accounts.google.com"
	}
	return issuer == entry.Issuer
}

// CloneWithCallbackURL shares discovery/JWKS without changing the legacy route.
func (provider *OIDCProvider) CloneWithCallbackURL(callback string) *OIDCProvider {
	copy := *provider
	copy.oauthConfig.RedirectURL = callback
	return &copy
}
