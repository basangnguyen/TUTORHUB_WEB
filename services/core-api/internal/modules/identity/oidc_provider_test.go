package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

func TestOIDCProviderUsesDiscoveryPKCEAndVerifiedIDToken(t *testing.T) {
	t.Parallel()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	const keyID = "tutorhub-test-key"
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: privateKey},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID),
	)
	if err != nil {
		t.Fatalf("create signer: %v", err)
	}

	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeTestJSON(t, w, map[string]any{
				"issuer":                                issuer.URL,
				"authorization_endpoint":                issuer.URL + "/authorize",
				"token_endpoint":                        issuer.URL + "/token",
				"jwks_uri":                              issuer.URL + "/keys",
				"userinfo_endpoint":                     issuer.URL + "/userinfo",
				"end_session_endpoint":                  issuer.URL + "/logout",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/keys":
			writeTestJSON(t, w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
				Key:       &privateKey.PublicKey,
				KeyID:     keyID,
				Algorithm: string(jose.RS256),
				Use:       "sig",
			}}})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse token form: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			code := r.Form.Get("code")
			if (code != "valid-code" && code != "mismatch-code") || r.Form.Get("code_verifier") != "valid-verifier" {
				t.Errorf("unexpected token exchange form: %v", r.Form)
				http.Error(w, "invalid grant", http.StatusBadRequest)
				return
			}
			now := time.Now().UTC()
			payload, err := json.Marshal(map[string]any{
				"iss":       issuer.URL,
				"sub":       "subject-123",
				"aud":       "web-client",
				"exp":       now.Add(time.Hour).Unix(),
				"iat":       now.Unix(),
				"auth_time": now.Add(-time.Minute).Unix(),
				"nonce":     "nonce-123",
			})
			if err != nil {
				t.Errorf("marshal ID token claims: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			signed, err := signer.Sign(payload)
			if err != nil {
				t.Errorf("sign ID token: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			compact, err := signed.CompactSerialize()
			if err != nil {
				t.Errorf("serialize ID token: %v", err)
				http.Error(w, "server error", http.StatusInternalServerError)
				return
			}
			accessToken := "server-only-token"
			if code == "mismatch-code" {
				accessToken = "mismatch-token"
			}
			writeTestJSON(t, w, map[string]any{
				"access_token": accessToken,
				"token_type":   "Bearer",
				"expires_in":   3600,
				"id_token":     compact,
			})
		case "/userinfo":
			authorization := r.Header.Get("Authorization")
			if authorization != "Bearer server-only-token" && authorization != "Bearer mismatch-token" {
				t.Errorf("unexpected userinfo authorization header: %q", authorization)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			subject := "subject-123"
			if authorization == "Bearer mismatch-token" {
				subject = "different-subject"
			}
			writeTestJSON(t, w, map[string]any{
				"sub":                subject,
				"email":              "student@example.com",
				"email_verified":     true,
				"name":               "Student Nguyen",
				"preferred_username": "student",
				"locale":             "vi",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()

	provider, err := NewOIDCProvider(context.Background(), OIDCProviderConfig{
		IssuerURL:     issuer.URL,
		ClientID:      "web-client",
		ClientSecret:  "client-secret",
		CallbackURL:   "http://localhost:8080/api/v1/auth/callback",
		PostLogoutURL: "http://localhost:5173/signed-out",
		Scopes:        []string{"openid", "profile", "email"},
		HTTPTimeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatalf("create OIDC provider: %v", err)
	}
	authorizationURL, err := url.Parse(provider.AuthorizationURL(
		"state-123",
		"nonce-123",
		"challenge-123",
	))
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := authorizationURL.Query()
	if query.Get("state") != "state-123" ||
		query.Get("nonce") != "nonce-123" ||
		query.Get("code_challenge") != "challenge-123" ||
		query.Get("code_challenge_method") != "S256" ||
		query.Get("response_type") != "code" {
		t.Fatalf("authorization URL is missing OIDC/PKCE values: %s", authorizationURL)
	}

	claims, err := provider.ExchangeAndVerify(context.Background(), "valid-code", "valid-verifier")
	if err != nil {
		t.Fatalf("exchange and verify: %v", err)
	}
	if claims.Issuer != issuer.URL ||
		claims.Subject != "subject-123" ||
		claims.Email != "student@example.com" ||
		!claims.EmailVerified ||
		claims.DisplayName != "Student Nguyen" ||
		claims.Nonce != "nonce-123" {
		t.Fatalf("unexpected verified claims: %+v", claims)
	}
	if _, err := provider.ExchangeAndVerify(
		context.Background(),
		"mismatch-code",
		"valid-verifier",
	); err == nil || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("expected mismatched userinfo subject to fail, got %v", err)
	}

	logoutURL, err := url.Parse(provider.EndSessionURL())
	if err != nil {
		t.Fatalf("parse end-session URL: %v", err)
	}
	if logoutURL.Path != "/logout" ||
		logoutURL.Query().Get("client_id") != "web-client" ||
		logoutURL.Query().Get("post_logout_redirect_uri") != "http://localhost:5173/signed-out" {
		t.Fatalf("unexpected end-session URL: %s", logoutURL)
	}
}

func TestBuildEndSessionURLRejectsInvalidEndpoint(t *testing.T) {
	t.Parallel()

	if value := buildEndSessionURL("not-a-url", "client", "https://app.example"); value != "" {
		t.Fatalf("expected invalid endpoint to be rejected, got %q", value)
	}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("write test JSON: %v", err)
	}
}
