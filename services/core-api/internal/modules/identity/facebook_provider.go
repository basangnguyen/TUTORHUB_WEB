package identity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const FacebookIssuerURL = "https://www.facebook.com"
const facebookGraphVersion = "v27.0"

type FacebookProvider struct {
	*OIDCProvider
}

func NewFacebookProvider(ctx context.Context, cfg OIDCProviderConfig) (*FacebookProvider, error) {
	issuer := strings.TrimRight(cfg.IssuerURL, "/")
	if issuer == "" {
		issuer = FacebookIssuerURL
	}
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" || strings.TrimSpace(cfg.CallbackURL) == "" {
		return nil, ErrProviderExchange
	}
	graphOrigin := issuer
	if issuer == FacebookIssuerURL {
		graphOrigin = "https://graph.facebook.com"
	}
	if cfg.HTTPTimeout <= 0 {
		cfg.HTTPTimeout = 10 * time.Second
	}
	client := &http.Client{Timeout: cfg.HTTPTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	// Meta's discovery metadata need not advertise the manual code flow.
	// Use fixed endpoints, including the JWKS URI, from startup configuration.
	keys := oidc.NewRemoteKeySet(oidc.ClientContext(ctx, client), issuer+"/.well-known/oauth/openid/jwks/")
	return &FacebookProvider{&OIDCProvider{
		issuer: issuer, httpClient: client,
		oauthConfig: oauth2.Config{ClientID: cfg.ClientID, RedirectURL: cfg.CallbackURL, Scopes: []string{"openid", "email"},
			Endpoint: oauth2.Endpoint{AuthURL: issuer + "/" + facebookGraphVersion + "/dialog/oauth",
				TokenURL: graphOrigin + "/" + facebookGraphVersion + "/oauth/access_token"}},
		verifier: oidc.NewVerifier(issuer, keys, &oidc.Config{ClientID: cfg.ClientID, SupportedSigningAlgs: []string{"RS256"}}),
	}}, nil
}

func (provider *FacebookProvider) ExchangeAndVerify(ctx context.Context, code, verifier string) (ProviderClaims, error) {
	if code == "" || verifier == "" {
		return ProviderClaims{}, ErrProviderExchange
	}
	// Meta's manual OIDC flow exchanges a code by GET using the PKCE verifier.
	// The App Secret is needed for Meta callbacks and never goes into this URL.
	query := url.Values{"client_id": {provider.oauthConfig.ClientID}, "redirect_uri": {provider.oauthConfig.RedirectURL},
		"code": {code}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.oauthConfig.Endpoint.TokenURL+"?"+query.Encode(), nil)
	if err != nil {
		return ProviderClaims{}, ErrProviderExchange
	}
	req.Header.Set("Accept", "application/json")
	resp, err := provider.httpClient.Do(req)
	if err != nil {
		return ProviderClaims{}, ErrProviderExchange
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ProviderClaims{}, ErrProviderExchange
	}
	const limit = 64 << 10
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	var token struct {
		IDToken string `json:"id_token"`
	}
	if err != nil || len(body) > limit || json.Unmarshal(body, &token) != nil || token.IDToken == "" {
		return ProviderClaims{}, ErrProviderExchange
	}
	return provider.verifiedSocialClaims(ctx, token.IDToken, ProviderRegistration{Slug: "facebook", Issuer: provider.issuer}, time.Now())
}
