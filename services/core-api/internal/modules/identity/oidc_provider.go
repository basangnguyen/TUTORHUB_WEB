package identity

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type OIDCProviderConfig struct {
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	CallbackURL   string
	PostLogoutURL string
	Scopes        []string
	HTTPTimeout   time.Duration
}

type OIDCProvider struct {
	oauthConfig   oauth2.Config
	verifier      *oidc.IDTokenVerifier
	httpClient    *http.Client
	endSessionURL string
}

func NewOIDCProvider(ctx context.Context, cfg OIDCProviderConfig) (*OIDCProvider, error) {
	if strings.TrimSpace(cfg.IssuerURL) == "" ||
		strings.TrimSpace(cfg.ClientID) == "" ||
		strings.TrimSpace(cfg.ClientSecret) == "" ||
		strings.TrimSpace(cfg.CallbackURL) == "" {
		return nil, fmt.Errorf("OIDC provider configuration is incomplete")
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}

	httpClient := &http.Client{Timeout: cfg.HTTPTimeout}
	discoveryContext := oidc.ClientContext(ctx, httpClient)
	provider, err := oidc.NewProvider(discoveryContext, strings.TrimRight(cfg.IssuerURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}

	var metadata struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, fmt.Errorf("read OIDC provider metadata: %w", err)
	}

	return &OIDCProvider{
		oauthConfig: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.CallbackURL,
			Scopes:       append([]string(nil), cfg.Scopes...),
		},
		verifier: provider.Verifier(&oidc.Config{
			ClientID: cfg.ClientID,
		}),
		httpClient: httpClient,
		endSessionURL: buildEndSessionURL(
			metadata.EndSessionEndpoint,
			cfg.ClientID,
			cfg.PostLogoutURL,
		),
	}, nil
}

func (provider *OIDCProvider) AuthorizationURL(
	state string,
	nonce string,
	codeChallenge string,
) string {
	return provider.oauthConfig.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func (provider *OIDCProvider) ExchangeAndVerify(
	ctx context.Context,
	code string,
	codeVerifier string,
) (ProviderClaims, error) {
	exchangeContext := context.WithValue(ctx, oauth2.HTTPClient, provider.httpClient)
	token, err := provider.oauthConfig.Exchange(
		exchangeContext,
		code,
		oauth2.VerifierOption(codeVerifier),
	)
	if err != nil {
		return ProviderClaims{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return ProviderClaims{}, fmt.Errorf("OIDC token response is missing id_token")
	}

	idToken, err := provider.verifier.Verify(exchangeContext, rawIDToken)
	if err != nil {
		return ProviderClaims{}, fmt.Errorf("verify ID token: %w", err)
	}
	var claims struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Locale            string `json:"locale"`
		Nonce             string `json:"nonce"`
		AuthTime          int64  `json:"auth_time"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return ProviderClaims{}, fmt.Errorf("decode verified ID token claims: %w", err)
	}
	displayName := strings.TrimSpace(claims.Name)
	if displayName == "" {
		displayName = strings.TrimSpace(claims.PreferredUsername)
	}
	var authTime time.Time
	if claims.AuthTime > 0 {
		authTime = time.Unix(claims.AuthTime, 0).UTC()
	}

	return ProviderClaims{
		Issuer:        idToken.Issuer,
		Subject:       idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		DisplayName:   displayName,
		Locale:        claims.Locale,
		Nonce:         claims.Nonce,
		AuthTime:      authTime,
	}, nil
}

func (provider *OIDCProvider) EndSessionURL() string {
	return provider.endSessionURL
}

func buildEndSessionURL(endpoint string, clientID string, postLogoutURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	query := parsed.Query()
	query.Set("client_id", clientID)
	if postLogoutURL != "" {
		query.Set("post_logout_redirect_uri", postLogoutURL)
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
