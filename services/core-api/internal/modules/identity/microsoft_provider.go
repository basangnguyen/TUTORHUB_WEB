package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

const (
	MicrosoftAuthorityURL   = "https://login.microsoftonline.com"
	microsoftPersonalTenant = "9188040d-6c67-4c5b-b112-36a304b66dad"
	microsoftIssuerTemplate = MicrosoftAuthorityURL + "/{tenantid}/v2.0"
)

type MicrosoftProviderConfig struct {
	OIDCProviderConfig
	TenantMode                         string
	AllowedTenantIDs, BlockedTenantIDs []string
}

type MicrosoftProvider struct {
	*OIDCProvider
	tenantMode                         string
	allowedTenantIDs, blockedTenantIDs []string
}

func microsoftIssuer(tid string) string {
	return MicrosoftAuthorityURL + "/" + tid + "/v2.0"
}

func microsoftGUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func microsoftIssuerTenant(issuer string) string {
	tid := strings.TrimSuffix(strings.TrimPrefix(issuer, MicrosoftAuthorityURL+"/"), "/v2.0")
	if !microsoftGUID(tid) || issuer != microsoftIssuer(tid) {
		return ""
	}
	return tid
}

func (provider *MicrosoftProvider) matchesMicrosoftIssuer(issuer string) bool {
	tid := microsoftIssuerTenant(issuer)
	if tid == "" || slices.Contains(provider.blockedTenantIDs, tid) ||
		(len(provider.allowedTenantIDs) > 0 && !slices.Contains(provider.allowedTenantIDs, tid)) {
		return false
	}
	switch provider.tenantMode {
	case "common":
		return true
	case "consumers":
		return tid == microsoftPersonalTenant
	case "organizations":
		return tid != microsoftPersonalTenant
	default:
		return false
	}
}

func NewMicrosoftProvider(ctx context.Context, cfg MicrosoftProviderConfig) (*MicrosoftProvider, error) {
	authority := strings.TrimRight(cfg.IssuerURL, "/")
	if authority == "" {
		authority = MicrosoftAuthorityURL
	}
	mode := cfg.TenantMode
	if mode == "" {
		mode = "common"
	}
	if (mode != "common" && mode != "organizations" && mode != "consumers") ||
		strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" || strings.TrimSpace(cfg.CallbackURL) == "" {
		return nil, ErrProviderExchange
	}
	for _, list := range [][]string{cfg.AllowedTenantIDs, cfg.BlockedTenantIDs} {
		for _, id := range list {
			if !microsoftGUID(id) {
				return nil, ErrProviderExchange
			}
		}
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	client := &http.Client{Timeout: cfg.HTTPTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	base := authority + "/" + mode
	var metadata struct {
		Issuer string `json:"issuer"`
		JWKS   string `json:"jwks_uri"`
	}
	if err := microsoftFetchJSON(ctx, client, base+"/v2.0/.well-known/openid-configuration", &metadata); err != nil {
		return nil, err
	}
	// The consumers alias can resolve to the concrete MSA tenant.
	if metadata.Issuer != microsoftIssuerTemplate && !(mode == "consumers" && metadata.Issuer == microsoftIssuer(microsoftPersonalTenant)) {
		return nil, ErrProviderExchange
	}
	keysURL, err := url.Parse(metadata.JWKS)
	if err != nil || keysURL.Scheme+"://"+keysURL.Host != authority || keysURL.User != nil ||
		keysURL.Path == "" || keysURL.RawQuery != "" || keysURL.ForceQuery || keysURL.Fragment != "" {
		return nil, ErrProviderExchange
	}
	keys := &microsoftKeySet{client: client, url: metadata.JWKS}
	return &MicrosoftProvider{
		tenantMode: mode, allowedTenantIDs: slices.Clone(cfg.AllowedTenantIDs), blockedTenantIDs: slices.Clone(cfg.BlockedTenantIDs),
		OIDCProvider: &OIDCProvider{
			issuer: MicrosoftAuthorityURL, httpClient: client,
			oauthConfig: oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.CallbackURL,
				Scopes: []string{"openid", "email", "profile"}, Endpoint: oauth2.Endpoint{
					AuthURL: base + "/oauth2/v2.0/authorize", TokenURL: base + "/oauth2/v2.0/token", AuthStyle: oauth2.AuthStyleInParams,
				}},
			// The key set checks the signing key's issuer. ExchangeAndVerify checks the signed tenant and issuer.
			verifier: oidc.NewVerifier(MicrosoftAuthorityURL, keys, &oidc.Config{ClientID: cfg.ClientID, SkipIssuerCheck: true, SupportedSigningAlgs: []string{"RS256"}}),
		},
	}, nil
}

func microsoftFetchJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ErrProviderExchange
	}
	resp, err := client.Do(req)
	if err != nil {
		return ErrProviderExchange
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrProviderExchange
	}
	const limit = 1 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(body) > limit || json.Unmarshal(body, target) != nil {
		return ErrProviderExchange
	}
	return nil
}

func (provider *MicrosoftProvider) ExchangeAndVerify(ctx context.Context, code, verifier string) (ProviderClaims, error) {
	if code == "" || verifier == "" {
		return ProviderClaims{}, ErrProviderExchange
	}
	ctx = oidc.ClientContext(ctx, provider.httpClient)
	token, err := provider.oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return ProviderClaims{}, ErrProviderExchange
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || raw == "" {
		return ProviderClaims{}, ErrProviderExchange
	}
	id, err := provider.verifier.Verify(ctx, raw)
	if err != nil || len(id.Audience) != 1 || !provider.matchesMicrosoftIssuer(id.Issuer) ||
		id.Nonce == "" || id.Subject == "" || id.Subject != strings.TrimSpace(id.Subject) {
		return ProviderClaims{}, ErrProviderExchange
	}
	var claims struct {
		Tenant         string          `json:"tid"`
		Object         string          `json:"oid"`
		Email          string          `json:"email"`
		DomainVerified json.RawMessage `json:"xms_edov"`
		Name           string          `json:"name"`
		Locale         string          `json:"locale"`
		NotBefore      int64           `json:"nbf"`
	}
	now := time.Now()
	// nbf uses the same one-minute clock-skew allowance as iat.
	if id.Claims(&claims) != nil || id.Issuer != microsoftIssuer(claims.Tenant) ||
		(claims.Object != "" && !microsoftGUID(claims.Object)) || id.IssuedAt.Unix() <= 0 ||
		id.IssuedAt.After(now.Add(time.Minute)) || !id.IssuedAt.Before(id.Expiry) || claims.NotBefore > now.Add(time.Minute).Unix() {
		return ProviderClaims{}, ErrProviderExchange
	}
	return ProviderClaims{ProviderSlug: "microsoft", Issuer: id.Issuer, Subject: id.Subject, Nonce: id.Nonce,
		ProviderOrgID: claims.Tenant, ProviderStableID: claims.Object, DisplayName: claims.Name, Locale: claims.Locale,
		Email: claims.Email, EmailVerified: strings.TrimSpace(claims.Email) != "" && bytes.Equal(claims.DomainVerified, []byte("true")),
	}, nil
}
