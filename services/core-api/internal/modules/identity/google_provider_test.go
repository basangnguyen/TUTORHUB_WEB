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
	"sync"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
)

type fakeSignedIssuer struct {
	server           *httptest.Server
	mu               sync.Mutex
	slug             string
	claims           map[string]any
	challenge, nonce string
	userinfoCalls    int
	tokenCalls       int
	corruptSignature bool
}

func newFakeSignedIssuer(t *testing.T, slug string) *fakeSignedIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyID := slug + "-fixture"
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID))
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeSignedIssuer{slug: slug, challenge: PKCEChallenge("valid-verifier"), nonce: "valid-nonce"}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(t, w, map[string]any{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys", "/.well-known/oauth/openid/jwks/":
			writeTestJSON(t, w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: keyID, Algorithm: "RS256", Use: "sig"}}})
		case "/token", "/v27.0/oauth/access_token":
			if r.ParseForm() != nil {
				http.Error(w, "invalid", 400)
				return
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			f.tokenCalls++
			if slug == "facebook" && (r.Method != http.MethodGet || r.Form.Get("client_id") != "facebook-client" ||
				r.Form.Get("redirect_uri") != facebookFixtureCallback || r.Form.Has("client_secret")) {
				http.Error(w, "invalid exchange", 400)
				return
			}
			if r.Form.Get("code") != "valid-code" || PKCEChallenge(r.Form.Get("code_verifier")) != f.challenge {
				http.Error(w, "invalid grant", 400)
				return
			}
			raw, err := json.Marshal(f.claims)
			if err != nil {
				http.Error(w, "fixture", 500)
				return
			}
			signed, err := signer.Sign(raw)
			if err != nil {
				http.Error(w, "fixture", 500)
				return
			}
			compact, err := signed.CompactSerialize()
			if err != nil {
				http.Error(w, "fixture", 500)
				return
			}
			if f.corruptSignature {
				compact = compact[:strings.LastIndexByte(compact, '.')+1] + "AAAA"
			}
			writeTestJSON(t, w, map[string]any{"access_token": "server-only", "token_type": "Bearer", "expires_in": 3600, "id_token": compact})
		case "/userinfo":
			f.mu.Lock()
			f.userinfoCalls++
			f.mu.Unlock()
			http.Error(w, "userinfo must not be needed", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	f.reset()
	return f
}

func newFakeGoogleIssuer(t *testing.T) (*fakeSignedIssuer, *GoogleProvider) {
	t.Helper()
	f := newFakeSignedIssuer(t, "google")
	provider, err := NewGoogleProvider(context.Background(), OIDCProviderConfig{IssuerURL: f.server.URL, ClientID: "google-client", ClientSecret: "test-client-secret", CallbackURL: "http://localhost/api/v1/auth/oauth/google/callback", HTTPTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal("initialize fake Google provider")
	}
	return f, provider
}

func (f *fakeSignedIssuer) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.claims = map[string]any{"iss": f.server.URL, "sub": f.slug + "-subject", "aud": f.slug + "-client", "exp": now.Add(time.Hour).Unix(), "iat": now.Unix(), "nonce": f.nonce, "auth_time": now.Unix(), "email": "student@example.test", "email_verified": true, "name": "Student", "hd": "school.example"}
	if f.slug == "facebook" {
		f.claims["sub"] = "12345"
	}
	f.corruptSignature = false
}

func TestGoogleProviderDiscoverySignedClaimsAndPKCE(t *testing.T) {
	f, provider := newFakeGoogleIssuer(t)
	location, err := url.Parse(provider.AuthorizationURL("state", "nonce", "challenge"))
	if err != nil {
		t.Fatal(err)
	}
	query := location.Query()
	if query.Get("scope") != "openid email profile" || query.Get("state") != "state" || query.Get("nonce") != "nonce" || query.Get("code_challenge_method") != "S256" || query.Get("code_challenge") != "challenge" {
		t.Fatal("Google authorization omitted required parameters")
	}
	claims, err := provider.ExchangeAndVerify(context.Background(), "valid-code", "valid-verifier")
	if err != nil || claims.Subject != "google-subject" || claims.ProviderSlug != "google" || claims.ProviderOrgID != "school.example" || claims.Nonce != "valid-nonce" || !claims.EmailVerified || claims.Issuer != f.server.URL {
		t.Fatal("signed Google claims failed validation")
	}
	f.mu.Lock()
	calls := f.userinfoCalls
	f.mu.Unlock()
	if calls != 0 {
		t.Fatal("Google unexpectedly required UserInfo")
	}
	clone := provider.OIDCProvider.CloneWithCallbackURL("http://localhost/new-callback")
	original, _ := url.Parse(provider.AuthorizationURL("s", "n", "c"))
	changed, _ := url.Parse(clone.AuthorizationURL("s", "n", "c"))
	if original.Query().Get("redirect_uri") == changed.Query().Get("redirect_uri") || changed.Query().Get("redirect_uri") != "http://localhost/new-callback" {
		t.Fatal("callback clone mutated legacy provider")
	}
}

func TestGoogleProviderRejectsInvalidSignedTokenAndVerifier(t *testing.T) {
	f, provider := newFakeGoogleIssuer(t)
	for _, scenario := range []string{"audience", "issuer", "expired", "missing issued at", "future issued at", "missing subject", "missing nonce", "PKCE", "code"} {
		t.Run(scenario, func(t *testing.T) {
			f.reset()
			f.mu.Lock()
			switch scenario {
			case "audience":
				f.claims["aud"] = "other-client"
			case "issuer":
				f.claims["iss"] = "https://evil.example"
			case "expired":
				f.claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "missing issued at":
				delete(f.claims, "iat")
			case "future issued at":
				f.claims["iat"] = time.Now().Add(10 * time.Minute).Unix()
			case "missing subject":
				delete(f.claims, "sub")
			case "missing nonce":
				delete(f.claims, "nonce")
			}
			f.mu.Unlock()
			code, verifier := "valid-code", "valid-verifier"
			if scenario == "PKCE" {
				verifier = "wrong"
			}
			if scenario == "code" {
				code = "wrong"
			}
			if _, err := provider.ExchangeAndVerify(context.Background(), code, verifier); !errors.Is(err, ErrProviderExchange) {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}

func TestGoogleDefaultIssuerAliasesCanonicalizeIdentity(t *testing.T) {
	f, provider := newFakeGoogleIssuer(t)
	// Keep all HTTP requests on the fake issuer while exercising the default
	// Google's exact issuer allowlist and signed-token verification together.
	provider.issuer = GoogleIssuerURL
	provider.verifier = oidc.NewVerifier(GoogleIssuerURL, oidc.NewRemoteKeySet(context.Background(), f.server.URL+"/keys"), &oidc.Config{ClientID: "google-client", SkipIssuerCheck: true, SupportedSigningAlgs: []string{"RS256"}})
	for _, issuer := range []string{GoogleIssuerURL, "accounts.google.com", "https://accounts.google.com.evil.example", "accounts.google.com/"} {
		t.Run(issuer, func(t *testing.T) {
			f.reset()
			f.mu.Lock()
			f.claims["iss"] = issuer
			f.mu.Unlock()
			claims, err := provider.ExchangeAndVerify(context.Background(), "valid-code", "valid-verifier")
			allowed := issuer == GoogleIssuerURL || issuer == "accounts.google.com"
			if allowed {
				if err != nil || claims.Issuer != GoogleIssuerURL {
					t.Fatal("documented issuer did not normalize")
				}
			} else if err == nil {
				t.Fatal("untrusted issuer alias accepted")
			}
		})
	}
}

func TestGoogleProviderAndServiceValidateNonceAndVerifiedEmailTogether(t *testing.T) {
	f, provider := newFakeGoogleIssuer(t)
	for _, scenario := range []string{"success", "wrong nonce", "unverified email"} {
		t.Run(scenario, func(t *testing.T) {
			service, repo, _, _, _ := federationServiceFixture(t)
			now := time.Now().UTC()
			service.clock = func() time.Time { return now }
			registry, _ := NewProviderRegistry(ProviderRegistration{Slug: "google", Issuer: f.server.URL, Provider: provider})
			if service.ConfigureFederation(registry, FederationConfig{SignupEnabled: true, LegalTermsVersion: "test"}) != nil {
				t.Fatal("configure fake Google")
			}
			start, err := service.BeginOAuth(context.Background(), "google", "/app", "login", Principal{})
			if err != nil {
				t.Fatal(err)
			}
			authorization, _ := url.Parse(start.AuthorizationURL)
			f.reset()
			f.mu.Lock()
			f.challenge = authorization.Query().Get("code_challenge")
			f.claims["nonce"] = authorization.Query().Get("nonce")
			if scenario == "wrong nonce" {
				f.claims["nonce"] = "wrong"
			}
			if scenario == "unverified email" {
				f.claims["email_verified"] = false
			}
			f.mu.Unlock()
			result, err := service.CompleteOAuth(context.Background(), "google", CallbackInput{State: authorization.Query().Get("state"), BrowserBinding: start.BrowserBinding, Code: "valid-code", Issuer: f.server.URL})
			if scenario == "success" {
				if err != nil || result.SessionToken == "" || repo.loginCalls != 1 {
					t.Fatal("valid fake Google flow failed")
				}
			} else {
				want := ErrProviderExchange
				if strings.Contains(scenario, "unverified") {
					want = ErrVerifiedEmailRequired
				}
				if !errors.Is(err, want) || repo.loginCalls != 0 {
					t.Fatal("untrusted claims reached session creation")
				}
			}
		})
	}
}
