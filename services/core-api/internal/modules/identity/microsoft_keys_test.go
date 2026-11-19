package identity

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestMicrosoftKeySetRotationAndOutage(t *testing.T) {
	first, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	next, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	firstJWK := microsoftTestKey(t, first, microsoftIssuerTemplate)
	nextJWK := microsoftTestKey(t, next, microsoftIssuer(msFixtureTenant))
	nextJWK["kid"] = "rotated"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch requests.Add(1) {
		case 1:
			writeTestJSON(t, w, map[string]any{"keys": []any{firstJWK}})
		case 2:
			writeTestJSON(t, w, map[string]any{"keys": []any{nextJWK}})
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)
	set := microsoftKeySet{client: server.Client(), url: server.URL}
	ctx := t.Context()
	key, err := set.signingKey(ctx, "fixture-key")
	if err != nil || !key.publicKey.Equal(&first.PublicKey) || key.issuer != microsoftIssuerTemplate {
		t.Fatal("could not load initial signing key", err)
	}
	for _, kid := range []string{"unknown-1", "unknown-2"} {
		if _, err := set.signingKey(ctx, kid); !errors.Is(err, ErrProviderExchange) || requests.Load() != 1 {
			t.Fatalf("unknown kid triggered a fetch during the refresh cooldown: %v", err)
		}
	}
	// Advance only the cache deadlines. The HTTP response sequence models
	// rollover and outage without slowing the test or depending on wall time.
	set.nextRefresh = time.Time{}
	key, err = set.signingKey(ctx, "rotated")
	if err != nil || !key.publicKey.Equal(&next.PublicKey) || key.issuer != microsoftIssuer(msFixtureTenant) || requests.Load() != 2 {
		t.Fatal("miss after cooldown did not load the rotated tenant key", err)
	}
	if _, err := set.signingKey(ctx, "fixture-key"); !errors.Is(err, ErrProviderExchange) || requests.Load() != 2 {
		t.Fatal("retired key survived rotation", err)
	}
	set.nextRefresh = time.Time{}
	if _, err := set.signingKey(ctx, "unknown-3"); !errors.Is(err, ErrProviderExchange) || requests.Load() != 3 {
		t.Fatal("failed JWKS fetch accepted an unknown key", err)
	}
	if _, err := set.signingKey(ctx, "rotated"); err != nil || requests.Load() != 3 {
		t.Fatal("outage discarded a still-valid cached key", err)
	}
	set.expires, set.nextRefresh = time.Time{}, time.Time{}
	if _, err := set.signingKey(ctx, "rotated"); !errors.Is(err, ErrProviderExchange) || requests.Load() != 4 {
		t.Fatal("expired key was reused during an outage", err)
	}
	if _, err := set.signingKey(ctx, "rotated"); !errors.Is(err, ErrProviderExchange) || requests.Load() != 4 {
		t.Fatal("outage was not throttled after expiry", err)
	}
}

func TestMicrosoftRejectsUntrustedDiscoveryMetadata(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("followed a foreign discovery URL")
	}))
	t.Cleanup(foreign.Close)
	for _, tc := range []struct {
		name, issuer string
		foreignKeys  bool
	}{
		{"foreign issuer", foreign.URL + "/{tenantid}/v2.0", false},
		{"unrelated concrete issuer for common", microsoftIssuer(msFixtureTenant), false},
		{"foreign JWKS", microsoftIssuerTemplate, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				origin := "http://" + r.Host
				if tc.foreignKeys {
					origin = foreign.URL
				}
				writeTestJSON(t, w, map[string]any{"issuer": tc.issuer, "jwks_uri": origin + "/keys"})
			}))
			t.Cleanup(server.Close)
			_, err := NewMicrosoftProvider(t.Context(), MicrosoftProviderConfig{
				OIDCProviderConfig: OIDCProviderConfig{IssuerURL: server.URL, CallbackURL: msFixtureCallback, ClientID: "fixture", ClientSecret: "fixture"},
			})
			if !errors.Is(err, ErrProviderExchange) {
				t.Fatal("accepted discovery metadata outside the configured trust boundary", err)
			}
		})
	}
}
