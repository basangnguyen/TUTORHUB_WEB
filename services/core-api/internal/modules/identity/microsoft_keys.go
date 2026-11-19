package identity

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// go-oidc's generic RemoteKeySet does not retain the issuer restriction on
// individual Entra keys. Keep it beside the public key used to verify the JWS.
type microsoftSigningKey struct {
	publicKey *rsa.PublicKey
	issuer    string
}

type microsoftKeySet struct {
	client               *http.Client
	url                  string
	mu                   sync.Mutex
	keys                 map[string]microsoftSigningKey
	expires, nextRefresh time.Time
}

func (set *microsoftKeySet) VerifySignature(ctx context.Context, raw string) ([]byte, error) {
	signed, err := jose.ParseSignedCompact(raw, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil || len(signed.Signatures) != 1 {
		return nil, ErrProviderExchange
	}
	kid := signed.Signatures[0].Protected.KeyID
	if kid == "" {
		return nil, ErrProviderExchange
	}
	key, err := set.signingKey(ctx, kid)
	if err != nil {
		return nil, err
	}
	payload, err := signed.Verify(key.publicKey)
	if err != nil {
		return nil, ErrProviderExchange
	}
	var claims struct {
		Issuer string `json:"iss"`
	}
	if json.Unmarshal(payload, &claims) != nil || microsoftIssuerTenant(claims.Issuer) == "" ||
		(key.issuer != microsoftIssuerTemplate && key.issuer != claims.Issuer) {
		return nil, ErrProviderExchange
	}
	return payload, nil
}

func (set *microsoftKeySet) signingKey(ctx context.Context, kid string) (microsoftSigningKey, error) {
	set.mu.Lock()
	defer set.mu.Unlock()
	now := time.Now()
	if key, ok := set.keys[kid]; ok && now.Before(set.expires) {
		return key, nil
	}
	// Refresh on rotation or expiry. Coalesce concurrent misses and bound
	// refreshes for unknown kids and authority outages.
	if now.Before(set.nextRefresh) {
		return microsoftSigningKey{}, ErrProviderExchange
	}
	set.nextRefresh = now.Add(30 * time.Second)
	var document struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := microsoftFetchJSON(ctx, set.client, set.url, &document); err != nil {
		return microsoftSigningKey{}, err
	}
	keys := make(map[string]microsoftSigningKey, len(document.Keys))
	for _, raw := range document.Keys {
		var metadata struct {
			Issuer string `json:"issuer"`
		}
		var jwk jose.JSONWebKey
		if json.Unmarshal(raw, &metadata) != nil || json.Unmarshal(raw, &jwk) != nil ||
			jwk.Use != "sig" || (jwk.Algorithm != "" && jwk.Algorithm != "RS256") || jwk.KeyID == "" {
			continue
		}
		public, ok := jwk.Key.(*rsa.PublicKey)
		if !ok || !jwk.Valid() || public.N.BitLen() < 2048 ||
			(metadata.Issuer != microsoftIssuerTemplate && microsoftIssuerTenant(metadata.Issuer) == "") {
			continue
		}
		if _, duplicate := keys[jwk.KeyID]; duplicate {
			return microsoftSigningKey{}, ErrProviderExchange
		}
		keys[jwk.KeyID] = microsoftSigningKey{publicKey: public, issuer: metadata.Issuer}
	}
	set.keys, set.expires = keys, now.Add(time.Hour)
	key, ok := keys[kid]
	if !ok {
		return microsoftSigningKey{}, ErrProviderExchange
	}
	return key, nil
}
