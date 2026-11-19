package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

const (
	msFixtureTenant   = "63e18f87-75f3-4ab4-a048-d07d605b98b3"
	msFixtureObject   = "4444fe2b-dd2d-4386-979a-9ceeb4309aae"
	msFixtureCallback = "http://localhost/api/v1/auth/oauth/microsoft/callback"
)

func TestMicrosoftSignedAuthority(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name, mode, tenant, keyIssuer string
		allowed, blocked              []string
		change                        map[string]any
		valid, trusted                bool
	}{
		{name: "work account", valid: true, trusted: true},
		{name: "consumer", mode: "consumers", tenant: microsoftPersonalTenant, keyIssuer: microsoftIssuer(microsoftPersonalTenant), valid: true, change: map[string]any{"xms_edov": nil}},
		{name: "MSA on common", tenant: microsoftPersonalTenant, valid: true, trusted: true},
		{name: "work tenant allowlisted", mode: "organizations", allowed: []string{msFixtureTenant}, valid: true, trusted: true},
		{name: "work rejected by consumers", mode: "consumers"},
		{name: "MSA rejected by organizations", mode: "organizations", tenant: microsoftPersonalTenant},
		{name: "deny overrides allow", allowed: []string{msFixtureTenant}, blocked: []string{msFixtureTenant}},
		{name: "outside allowlist", allowed: []string{microsoftPersonalTenant}},
		{name: "scoped key cannot sign for a different tenant", keyIssuer: microsoftIssuer(microsoftPersonalTenant)},
		{name: "scoped organization key", keyIssuer: microsoftIssuer(msFixtureTenant), valid: true, trusted: true},
		{name: "key missing issuer", keyIssuer: "missing"},
		{name: "wrong issuer", change: map[string]any{"iss": microsoftIssuer(microsoftPersonalTenant)}},
		{name: "issuer trailing slash", change: map[string]any{"iss": microsoftIssuer(msFixtureTenant) + "/"}},
		{name: "invalid tid", change: map[string]any{"tid": "organizations"}},
		{name: "missing tid", change: map[string]any{"tid": nil}},
		{name: "invalid oid", change: map[string]any{"oid": "unknown"}},
		{name: "oid optional", change: map[string]any{"oid": nil}, valid: true, trusted: true},
		{name: "audience", change: map[string]any{"aud": "unrelated-client"}},
		{name: "ambiguous audience", change: map[string]any{"aud": []string{"fixture-client", "another"}}},
		{name: "missing iat", change: map[string]any{"iat": nil}},
		{name: "future iat", change: map[string]any{"iat": time.Now().Add(10 * time.Minute).Unix()}},
		{name: "expired", change: map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}},
		{name: "not valid yet", change: map[string]any{"nbf": time.Now().Add(4 * time.Minute).Unix()}},
		{name: "wrong nonce", change: map[string]any{"nonce": "other-flow"}},
		{name: "existing account email", valid: true, trusted: true},
		{name: "missing nonce", change: map[string]any{"nonce": nil}},
		{name: "missing sub", change: map[string]any{"sub": nil}},
		{name: "invalid signature"},
		{name: "wrong PKCE"},
		{name: "email absent", change: map[string]any{"email": nil}, valid: true},
		{name: "claim absent", change: map[string]any{"xms_edov": nil}, valid: true},
		{name: "claim false", change: map[string]any{"xms_edov": false}, valid: true},
		{name: "string is not proof", change: map[string]any{"xms_edov": "true"}, valid: true},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mode, tid, keyIssuer := tc.mode, tc.tenant, tc.keyIssuer
			if mode == "" {
				mode = "common"
			}
			if tid == "" {
				tid = msFixtureTenant
			}
			if keyIssuer == "" {
				keyIssuer = microsoftIssuerTemplate
			}
			mux := http.NewServeMux()
			server := httptest.NewServer(mux)
			t.Cleanup(server.Close)
			mux.HandleFunc("GET /"+mode+"/v2.0/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
				issuer := microsoftIssuerTemplate
				if mode == "consumers" {
					issuer = microsoftIssuer(microsoftPersonalTenant)
				}
				writeTestJSON(t, w, map[string]any{"issuer": issuer, "jwks_uri": server.URL + "/common/discovery/v2.0/keys"})
			})
			published := microsoftTestKey(t, key, keyIssuer)
			mux.HandleFunc("GET /common/discovery/v2.0/keys", func(w http.ResponseWriter, _ *http.Request) {
				writeTestJSON(t, w, map[string]any{"keys": []any{published}})
			})
			ctx := context.Background()
			provider, err := NewMicrosoftProvider(ctx, MicrosoftProviderConfig{
				OIDCProviderConfig: OIDCProviderConfig{IssuerURL: server.URL, ClientID: "fixture-client", ClientSecret: "fixture-secret", CallbackURL: msFixtureCallback},
				TenantMode:         mode, AllowedTenantIDs: tc.allowed, BlockedTenantIDs: tc.blocked,
			})
			if err != nil {
				t.Fatal(err)
			}
			service, repo, _, _, _ := federationServiceFixture(t)
			service.registry = microsoftTestRegistry(t, provider)
			repo.registrationPending = tc.valid && !tc.trusted
			repo.pending = tc.name == "existing account email"
			start, err := service.BeginOAuth(ctx, "microsoft", "/app", "login", Principal{})
			if err != nil {
				t.Fatal(err)
			}
			location, err := url.Parse(start.AuthorizationURL)
			if err != nil {
				t.Fatal(err)
			}
			query := location.Query()
			if location.Path != "/"+mode+"/oauth2/v2.0/authorize" || query.Get("response_type") != "code" ||
				query.Get("scope") != "openid email profile" || query.Get("redirect_uri") != msFixtureCallback ||
				query.Get("state") == "" || query.Get("nonce") == "" || query.Get("code_challenge_method") != "S256" ||
				query.Get("code_challenge") == "" || query.Has("client_secret") {
				t.Fatal("wrong authorization request")
			}
			now := time.Now()
			claims := map[string]any{
				"iss": microsoftIssuer(tid), "tid": tid, "oid": msFixtureObject,
				"sub": "pairwise-id", "aud": "fixture-client", "nonce": query.Get("nonce"),
				"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(time.Hour).Unix(),
				"email": "student@example.test", "preferred_username": "other@example.test", "xms_edov": true,
			}
			for name, value := range tc.change {
				if value == nil {
					delete(claims, name)
				} else {
					claims[name] = value
				}
			}
			token := microsoftTestToken(t, key, claims)
			if tc.name == "invalid signature" {
				token = token[:strings.LastIndexByte(token, '.')+1] + "AAAA"
			}
			challenge := query.Get("code_challenge")
			if tc.name == "wrong PKCE" {
				challenge = PKCEChallenge("different verifier")
			}
			mux.HandleFunc("POST /"+mode+"/oauth2/v2.0/token", func(w http.ResponseWriter, r *http.Request) {
				if r.ParseForm() != nil || r.PostForm.Get("client_id") != "fixture-client" || r.PostForm.Get("client_secret") != "fixture-secret" ||
					r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != msFixtureCallback ||
					r.PostForm.Get("code") != "fixture-code" || PKCEChallenge(r.PostForm.Get("code_verifier")) != challenge {
					http.Error(w, "invalid grant", http.StatusBadRequest)
					return
				}
				writeTestJSON(t, w, map[string]any{"access_token": "test-only", "token_type": "Bearer", "id_token": token})
			})
			outcome, err := service.CompleteOAuth(ctx, "microsoft", CallbackInput{State: query.Get("state"), Code: "fixture-code", BrowserBinding: start.BrowserBinding})
			if !tc.valid {
				if !errors.Is(err, ErrProviderExchange) || repo.loginCalls != 0 {
					t.Fatal("invalid evidence reached account resolution", err)
				}
				return
			}
			email, _ := claims["email"].(string)
			object, _ := claims["oid"].(string)
			result := repo.login.Claims
			if err != nil || repo.loginCalls != 1 || result.ProviderSlug != "microsoft" || result.Issuer != microsoftIssuer(tid) ||
				result.ProviderOrgID != tid || result.ProviderStableID != object || result.Subject != "pairwise-id" ||
				result.Nonce != query.Get("nonce") || result.EmailVerified != tc.trusted || result.Email != email {
				t.Fatal("incorrect identity projection", err)
			}
			if outcome.RegistrationPending != repo.registrationPending || outcome.LinkPending != repo.pending ||
				(outcome.SessionToken != "") != (!repo.pending && !repo.registrationPending) {
				t.Fatal("callback returned an inconsistent session or pending transaction")
			}
		})
	}
}

func microsoftTestToken(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "fixture-key"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func microsoftTestKey(t *testing.T, key *rsa.PrivateKey, issuer string) map[string]any {
	t.Helper()
	raw, err := json.Marshal(jose.JSONWebKey{Key: &key.PublicKey, KeyID: "fixture-key", Use: "sig"})
	if err != nil {
		t.Fatal(err)
	}
	var published map[string]any
	if err := json.Unmarshal(raw, &published); err != nil {
		t.Fatal(err)
	}
	if issuer != "missing" {
		published["issuer"] = issuer
	}
	return published
}

func TestMicrosoftRegistryRejectsSharedTenantIssuers(t *testing.T) {
	ms := ProviderRegistration{"microsoft", MicrosoftAuthorityURL, &MicrosoftProvider{tenantMode: "common"}}
	for _, slug := range []string{"oidc", "google", "facebook"} {
		other := ProviderRegistration{slug, microsoftIssuer(msFixtureTenant), &fakeProvider{}}
		for _, entries := range [][]ProviderRegistration{{ms, other}, {other, ms}} {
			if _, err := NewProviderRegistry(entries...); err == nil {
				t.Fatal("overlapping identity namespaces")
			}
		}
	}
}
