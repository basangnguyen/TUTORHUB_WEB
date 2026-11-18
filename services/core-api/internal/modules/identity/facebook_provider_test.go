package identity

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"
)

const facebookFixtureCallback = "http://localhost/api/v1/auth/oauth/facebook/callback"

func TestFacebookIssuerCannotBeRegisteredWithAnotherTrustPolicy(t *testing.T) {
	for _, slug := range []string{"google", "oidc"} {
		facebook := ProviderRegistration{"facebook", "https://issuer.example/", &fakeProvider{}}
		other := ProviderRegistration{slug, "https://issuer.example", &fakeProvider{}}
		for _, entries := range [][]ProviderRegistration{{facebook, other}, {other, facebook}} {
			if _, err := NewProviderRegistry(entries...); err == nil {
				t.Fatal("Facebook issuer accepted under a different trust policy")
			}
		}
	}
}

func newFakeFacebookIssuer(t *testing.T) (*fakeSignedIssuer, *FacebookProvider) {
	t.Helper()
	f := newFakeSignedIssuer(t, "facebook")
	provider, err := NewFacebookProvider(context.Background(), OIDCProviderConfig{IssuerURL: f.server.URL, ClientID: "facebook-client",
		ClientSecret: "meta-fixture-secret", CallbackURL: facebookFixtureCallback})
	if err != nil {
		t.Fatal(err)
	}
	return f, provider
}

func TestFacebookProviderUsesVersionedCodeFlowAndUntrustedEmail(t *testing.T) {
	f, provider := newFakeFacebookIssuer(t)
	location, err := url.Parse(provider.AuthorizationURL("state", "nonce", "challenge"))
	if err != nil {
		t.Fatal(err)
	}
	q := location.Query()
	if location.Path != "/v27.0/dialog/oauth" || q.Get("scope") != "openid email" || q.Get("response_type") != "code" ||
		q.Get("state") != "state" || q.Get("nonce") != "nonce" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") != "challenge" {
		t.Fatal("invalid Meta authorization request")
	}
	for _, email := range []string{"student@example.test", ""} {
		f.mu.Lock()
		if email == "" {
			delete(f.claims, "email")
		}
		f.claims["sub"] = "12345"
		f.mu.Unlock()
		claims, err := provider.ExchangeAndVerify(context.Background(), "valid-code", "valid-verifier")
		if err != nil || claims.ProviderSlug != "facebook" || claims.Issuer != f.server.URL || claims.Subject != "12345" ||
			claims.EmailVerified || claims.Email != email || claims.ProviderOrgID != "" || claims.Nonce != "valid-nonce" {
			t.Fatal("invalid Facebook identity or email trust")
		}
	}
}

func TestFacebookProviderRejectsInvalidProof(t *testing.T) {
	f, provider := newFakeFacebookIssuer(t)
	for _, tc := range []struct {
		name, key string
		value     any
	}{
		{"audience", "aud", "different-app"}, {"multiple audiences", "aud", []string{"facebook-client", "other"}},
		{"issuer", "iss", "https://www.facebook.com.evil.example"}, {"missing nonce", "nonce", ""},
		{"missing subject", "sub", ""}, {"invalid ASID", "sub", "not-an-asid"}, {"expired", "exp", time.Now().Add(-time.Minute).Unix()},
		{"future issue", "iat", time.Now().Add(time.Hour).Unix()}, {"missing issue", "iat", 0},
		{"signature", "", nil}, {"PKCE", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.reset()
			f.mu.Lock()
			if tc.key != "" {
				f.claims[tc.key] = tc.value
			}
			f.corruptSignature = tc.name == "signature"
			f.mu.Unlock()
			verifier := "valid-verifier"
			if tc.name == "PKCE" {
				verifier = "wrong"
			}
			if _, err := provider.ExchangeAndVerify(context.Background(), "valid-code", verifier); !errors.Is(err, ErrProviderExchange) {
				t.Fatal("invalid Facebook proof accepted")
			}
		})
	}
}

func TestFacebookProviderRejectsMixupStateAndNonce(t *testing.T) {
	f, facebook := newFakeFacebookIssuer(t)
	for _, tc := range []struct{ start, callback, invalid string }{
		{"facebook", "google", ""}, {"facebook", "oidc", ""}, {"google", "facebook", ""}, {"oidc", "facebook", ""},
		{"facebook", "facebook", "state"}, {"facebook", "facebook", "nonce"},
	} {
		t.Run(tc.start+"-"+tc.callback+tc.invalid, func(t *testing.T) {
			service, repo, legacy, google, _ := federationServiceFixture(t)
			registry, err := NewProviderRegistry(ProviderRegistration{"oidc", "https://identity.example", legacy},
				ProviderRegistration{"google", GoogleIssuerURL, google}, ProviderRegistration{"facebook", f.server.URL, facebook})
			if err != nil || service.ConfigureFederation(registry, FederationConfig{SignupEnabled: true, LegalTermsVersion: "terms-v1"}) != nil {
				t.Fatal("cannot configure fixture")
			}
			start, err := service.BeginOAuth(context.Background(), tc.start, "/app", "login", Principal{})
			if err != nil {
				t.Fatal(err)
			}
			location, _ := url.Parse(start.AuthorizationURL)
			q := location.Query()
			state := q.Get("state")
			switch tc.start {
			case "google":
				state = google.state
			case "oidc":
				state = legacy.state
			}
			if tc.invalid == "state" {
				state = "wrong-state"
			}
			f.mu.Lock()
			before := f.tokenCalls
			f.challenge = q.Get("code_challenge")
			f.claims["nonce"] = "different-nonce"
			f.mu.Unlock()
			_, err = service.CompleteOAuth(context.Background(), tc.callback, CallbackInput{State: state, Code: "valid-code", BrowserBinding: start.BrowserBinding})
			want, calls := ErrInvalidAuthFlow, before
			if tc.invalid == "nonce" {
				want, calls = ErrProviderExchange, before+1
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if !errors.Is(err, want) || repo.loginCalls != 0 || f.tokenCalls != calls || legacy.code != "" || google.code != "" {
				t.Fatal("unexpected provider exchange or login")
			}
		})
	}
}
