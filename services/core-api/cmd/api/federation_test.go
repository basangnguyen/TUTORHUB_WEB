package main

import (
	"context"
	"testing"

	"github.com/tutorhub-v2/core-api/internal/config"
)

func TestFederationStartupDisabledDoesNotDiscoverProviders(t *testing.T) {
	registry, err := configuredFederationRegistry(context.Background(), config.Config{
		Authentication: config.AuthenticationConfig{GoogleIssuerURL: "invalid-ignored-issuer"},
	}, nil)
	if err != nil || registry == nil {
		t.Fatal("disabled federation must preserve native-only startup")
	}
}

func TestFederationStartupMissingProviderDependenciesFailsClosed(t *testing.T) {
	for _, auth := range []config.AuthenticationConfig{
		{OIDCEnabled: true},
		{ProviderGoogleEnabled: true, GoogleIssuerURL: "https://accounts.google.com"},
		{ProviderFacebookEnabled: true, FacebookIssuerURL: "https://www.facebook.com"},
		{ProviderMicrosoftEnabled: true, MicrosoftAuthorityURL: "https://microsoft-authority.invalid"},
	} {
		if _, err := configuredFederationRegistry(context.Background(), config.Config{Authentication: auth}, nil); err == nil {
			t.Fatal("enabled federation accepted missing provider dependencies")
		}
	}
}
