package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func configuredFederationRegistry(ctx context.Context, cfg config.Config, legacy identity.Provider) (*identity.ProviderRegistry, error) {
	entries := []identity.ProviderRegistration{}
	if cfg.Authentication.OIDCEnabled {
		provider, ok := legacy.(*identity.OIDCProvider)
		if !ok {
			return nil, errors.New("default OIDC provider is unavailable")
		}
		entries = append(entries, identity.ProviderRegistration{Slug: "oidc", Issuer: cfg.Authentication.IssuerURL,
			Provider: provider.CloneWithCallbackURL(strings.TrimRight(cfg.APIOrigin, "/") + "/api/v1/auth/oauth/oidc/callback")})
	}
	if cfg.Authentication.ProviderGoogleEnabled {
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		provider, err := identity.NewGoogleProvider(bounded, identity.OIDCProviderConfig{
			IssuerURL: cfg.Authentication.GoogleIssuerURL, ClientID: cfg.Authentication.GoogleClientID,
			ClientSecret: cfg.Authentication.GoogleClientSecret, CallbackURL: cfg.Authentication.GoogleCallbackURL,
			HTTPTimeout: 10 * time.Second,
		})
		if err != nil {
			return nil, errors.New("Google provider initialization failed")
		}
		entries = append(entries, identity.ProviderRegistration{Slug: "google", Issuer: cfg.Authentication.GoogleIssuerURL, Provider: provider})
	}
	if cfg.Authentication.ProviderFacebookEnabled {
		provider, err := identity.NewFacebookProvider(ctx, identity.OIDCProviderConfig{
			IssuerURL: cfg.Authentication.FacebookIssuerURL, ClientID: cfg.Authentication.FacebookClientID,
			ClientSecret: cfg.Authentication.FacebookClientSecret, CallbackURL: cfg.Authentication.FacebookCallbackURL,
		})
		if err != nil {
			return nil, errors.New("Facebook provider initialization failed")
		}
		entries = append(entries, identity.ProviderRegistration{Slug: "facebook", Issuer: provider.IssuerURL(), Provider: provider})
	}
	auth := cfg.Authentication
	if auth.ProviderMicrosoftEnabled {
		provider, err := identity.NewMicrosoftProvider(ctx, identity.MicrosoftProviderConfig{
			OIDCProviderConfig: identity.OIDCProviderConfig{
				IssuerURL: auth.MicrosoftAuthorityURL, ClientID: auth.MicrosoftClientID, ClientSecret: auth.MicrosoftClientSecret,
				CallbackURL: auth.MicrosoftCallbackURL, HTTPTimeout: 10 * time.Second,
			},
			TenantMode: auth.MicrosoftTenantMode, AllowedTenantIDs: auth.MicrosoftAllowedTenantIDs, BlockedTenantIDs: auth.MicrosoftBlockedTenantIDs,
		})
		if err != nil {
			return nil, errors.New("Microsoft provider initialization failed")
		}
		entries = append(entries, identity.ProviderRegistration{Slug: "microsoft", Issuer: identity.MicrosoftAuthorityURL, Provider: provider})
	}
	return identity.NewProviderRegistry(entries...)
}
