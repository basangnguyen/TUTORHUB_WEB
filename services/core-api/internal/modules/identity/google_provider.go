package identity

import (
	"context"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const GoogleIssuerURL = "https://accounts.google.com"

// Google uses verified ID-token claims directly; no UserInfo dependency or
// persisted access/refresh token is necessary.
type GoogleProvider struct {
	*OIDCProvider
	issuer string
	now    func() time.Time
}

func NewGoogleProvider(ctx context.Context, cfg OIDCProviderConfig) (*GoogleProvider, error) {
	if cfg.IssuerURL == "" {
		cfg.IssuerURL = GoogleIssuerURL
	}
	cfg.Scopes = []string{"openid", "email", "profile"}
	base, err := NewOIDCProvider(ctx, cfg)
	if err != nil {
		return nil, ErrProviderExchange
	}
	issuer := strings.TrimRight(cfg.IssuerURL, "/")
	// Only the default Google issuer allows its documented second spelling.
	base.verifier = base.discovery.Verifier(&oidc.Config{ClientID: cfg.ClientID, SkipIssuerCheck: issuer == GoogleIssuerURL, SupportedSigningAlgs: []string{"RS256"}})
	return &GoogleProvider{OIDCProvider: base, issuer: issuer, now: time.Now}, nil
}

func (provider *GoogleProvider) ExchangeAndVerify(ctx context.Context, code, codeVerifier string) (ProviderClaims, error) {
	exchangeContext := context.WithValue(ctx, oauth2.HTTPClient, provider.httpClient)
	token, err := provider.oauthConfig.Exchange(exchangeContext, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return ProviderClaims{}, ErrProviderExchange
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return ProviderClaims{}, ErrProviderExchange
	}
	return provider.verifiedSocialClaims(exchangeContext, raw, ProviderRegistration{Slug: "google", Issuer: provider.issuer}, provider.now())
}

// verifiedSocialClaims is shared by providers that return identity claims in
// the ID token. Only Google supplies a usable email-verification claim.
func (provider *OIDCProvider) verifiedSocialClaims(ctx context.Context, raw string, entry ProviderRegistration, now time.Time) (ProviderClaims, error) {
	verified, err := provider.verifier.Verify(ctx, raw)
	if err != nil {
		return ProviderClaims{}, ErrProviderExchange
	}
	if !entry.matchesIssuer(verified.Issuer) {
		return ProviderClaims{}, ErrProviderExchange
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		Locale        string `json:"locale"`
		Nonce         string `json:"nonce"`
		HostedDomain  string `json:"hd"`
		IssuedAt      int64  `json:"iat"`
		AuthTime      int64  `json:"auth_time"`
	}
	if verified.Claims(&claims) != nil {
		return ProviderClaims{}, ErrProviderExchange
	}
	issuedAt := time.Unix(claims.IssuedAt, 0)
	if claims.IssuedAt <= 0 || issuedAt.After(now.Add(time.Minute)) || issuedAt.After(verified.Expiry) || verified.Subject == "" || claims.Nonce == "" {
		return ProviderClaims{}, ErrProviderExchange
	}
	var authTime time.Time
	if claims.AuthTime > 0 {
		authTime = time.Unix(claims.AuthTime, 0).UTC()
	}
	result := ProviderClaims{ProviderSlug: entry.Slug, Issuer: entry.Issuer, Subject: verified.Subject,
		Email: claims.Email, DisplayName: claims.Name, Locale: claims.Locale, Nonce: claims.Nonce, AuthTime: authTime}
	if entry.Slug == "google" {
		result.ProviderOrgID = claims.HostedDomain
		result.EmailVerified = claims.EmailVerified
	}
	if entry.Slug == "facebook" && (!validFacebookASID(verified.Subject) || len(verified.Audience) != 1 || verified.Audience[0] != provider.oauthConfig.ClientID) {
		return ProviderClaims{}, ErrProviderExchange
	}
	return result, nil
}
