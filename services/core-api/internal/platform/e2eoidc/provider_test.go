package e2eoidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	testClientID     = "tutorhub-e2e"
	testClientSecret = "ephemeral-test-client-secret"
	testVerifier     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
)

func TestProviderCompletesDiscoveryPKCEAndOneTimeCodeFlow(t *testing.T) {
	running := startTestProvider(t)
	client := running.client

	discoveryResponse := get(t, client, running.issuer+"/.well-known/openid-configuration")
	defer discoveryResponse.Body.Close()
	var discovery map[string]any
	decodeJSON(t, discoveryResponse.Body, &discovery)
	if discovery["issuer"] != running.issuer ||
		discovery["authorization_endpoint"] != running.issuer+"/authorize" {
		t.Fatalf("unexpected discovery document: %#v", discovery)
	}

	challengeDigest := sha256.Sum256([]byte(testVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	authorizationURL, _ := url.Parse(running.issuer + "/authorize")
	query := authorizationURL.Query()
	query.Set("client_id", testClientID)
	query.Set("redirect_uri", running.redirectURL)
	query.Set("response_type", "code")
	query.Set("scope", "openid profile email")
	query.Set("state", "state-value")
	query.Set("nonce", "nonce-value")
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	authorizationURL.RawQuery = query.Encode()

	authorizationResponse := get(t, client, authorizationURL.String())
	body, err := io.ReadAll(authorizationResponse.Body)
	authorizationResponse.Body.Close()
	if err != nil {
		t.Fatalf("read authorization chooser: %v", err)
	}
	requestIDMatch := regexp.MustCompile(`name="request_id" type="hidden" value="([^"]+)"`).FindSubmatch(body)
	if authorizationResponse.StatusCode != http.StatusOK || len(requestIDMatch) != 2 ||
		!strings.Contains(string(body), "Sign in as E2E Administrator") {
		t.Fatalf("unexpected authorization chooser: status=%d body=%s", authorizationResponse.StatusCode, body)
	}

	form := url.Values{
		"request_id": {string(requestIDMatch[1])},
		"account":    {"admin"},
	}
	completeRequest, _ := http.NewRequest(
		http.MethodPost,
		running.issuer+"/authorize/complete",
		strings.NewReader(form.Encode()),
	)
	completeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	completeResponse, err := client.Do(completeRequest)
	if err != nil {
		t.Fatalf("complete authorization: %v", err)
	}
	completeResponse.Body.Close()
	redirect, err := url.Parse(completeResponse.Header.Get("Location"))
	if err != nil || completeResponse.StatusCode != http.StatusSeeOther ||
		redirect.Query().Get("state") != "state-value" ||
		redirect.Query().Get("code") == "" {
		t.Fatalf("unexpected authorization redirect: status=%d location=%q", completeResponse.StatusCode, completeResponse.Header.Get("Location"))
	}
	code := redirect.Query().Get("code")

	token := exchangeCode(t, client, running, code, testVerifier)
	if token.AccessToken == "" || token.IDToken == "" || token.TokenType != "Bearer" {
		t.Fatalf("incomplete token response: %#v", token)
	}
	verifyIDToken(t, client, running.issuer, token.IDToken)

	userInfoRequest, _ := http.NewRequest(http.MethodGet, running.issuer+"/userinfo", nil)
	userInfoRequest.Header.Set("Authorization", "Bearer "+token.AccessToken)
	userInfoResponse, err := client.Do(userInfoRequest)
	if err != nil {
		t.Fatalf("request user info: %v", err)
	}
	defer userInfoResponse.Body.Close()
	var claims map[string]any
	decodeJSON(t, userInfoResponse.Body, &claims)
	if userInfoResponse.StatusCode != http.StatusOK ||
		claims["sub"] != "e2e-admin" ||
		claims["email"] != "admin.e2e@tutorhub.local" {
		t.Fatalf("unexpected user info: status=%d claims=%#v", userInfoResponse.StatusCode, claims)
	}

	replayed := exchangeCodeResponse(t, client, running, code, testVerifier)
	defer replayed.Body.Close()
	if replayed.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorization code replay was accepted: %d", replayed.StatusCode)
	}
}

func TestProviderConsumesCodeAfterWrongPKCEAttempt(t *testing.T) {
	running := startTestProvider(t)
	code := authorizeAccount(t, running, "teacher")

	rejected := exchangeCodeResponse(
		t,
		running.client,
		running,
		code,
		strings.Repeat("x", 64),
	)
	rejected.Body.Close()
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong PKCE verifier was accepted: %d", rejected.StatusCode)
	}

	replayed := exchangeCodeResponse(t, running.client, running, code, testVerifier)
	defer replayed.Body.Close()
	if replayed.StatusCode != http.StatusBadRequest {
		t.Fatalf("authorization code was not consumed after rejected PKCE attempt: %d", replayed.StatusCode)
	}
}

func TestProviderRequiresTestEnvironmentAndLoopback(t *testing.T) {
	base := Config{
		Environment:   "test",
		ListenAddress: "127.0.0.1:9091",
		IssuerURL:     "http://127.0.0.1:9091",
		ClientID:      testClientID,
		ClientSecret:  testClientSecret,
		RedirectURL:   "http://127.0.0.1:8080/api/v1/auth/callback",
		PostLogoutURL: "http://127.0.0.1:5173/signed-out",
		Accounts:      testAccounts(),
	}

	for name, mutate := range map[string]func(*Config){
		"environment": func(config *Config) { config.Environment = "development" },
		"listen":      func(config *Config) { config.ListenAddress = "0.0.0.0:9091" },
		"issuer":      func(config *Config) { config.IssuerURL = "https://identity.example" },
		"redirect":    func(config *Config) { config.RedirectURL = "https://web.example/callback" },
	} {
		t.Run(name, func(t *testing.T) {
			config := base
			mutate(&config)
			if _, err := New(config); err == nil {
				t.Fatalf("unsafe %s configuration was accepted", name)
			}
		})
	}
}

type runningProvider struct {
	issuer      string
	redirectURL string
	client      *http.Client
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
}

func startTestProvider(t *testing.T) runningProvider {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	issuer := "http://" + listener.Addr().String()
	redirectURL := "http://127.0.0.1:8080/api/v1/auth/callback"
	provider, err := New(Config{
		Environment:   "test",
		ListenAddress: listener.Addr().String(),
		IssuerURL:     issuer,
		ClientID:      testClientID,
		ClientSecret:  testClientSecret,
		RedirectURL:   redirectURL,
		PostLogoutURL: "http://127.0.0.1:5173/signed-out",
		Accounts:      testAccounts(),
	})
	if err != nil {
		listener.Close()
		t.Fatalf("create provider: %v", err)
	}
	server := &http.Server{
		Handler:           provider.Handler(),
		ReadHeaderTimeout: time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	return runningProvider{
		issuer:      issuer,
		redirectURL: redirectURL,
		client: &http.Client{
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func testAccounts() []Account {
	return []Account{
		{
			ID:          "admin",
			Subject:     "e2e-admin",
			Email:       "admin.e2e@tutorhub.local",
			DisplayName: "E2E Administrator",
			Locale:      "en",
		},
		{
			ID:          "teacher",
			Subject:     "e2e-teacher",
			Email:       "teacher.e2e@tutorhub.local",
			DisplayName: "E2E Teacher",
			Locale:      "en",
		},
	}
}

func authorizeAccount(t *testing.T, running runningProvider, account string) string {
	t.Helper()
	challengeDigest := sha256.Sum256([]byte(testVerifier))
	authorizationURL, _ := url.Parse(running.issuer + "/authorize")
	query := authorizationURL.Query()
	query.Set("client_id", testClientID)
	query.Set("redirect_uri", running.redirectURL)
	query.Set("response_type", "code")
	query.Set("scope", "openid profile email")
	query.Set("state", "state-value")
	query.Set("nonce", "nonce-value")
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challengeDigest[:]))
	query.Set("code_challenge_method", "S256")
	authorizationURL.RawQuery = query.Encode()
	response := get(t, running.client, authorizationURL.String())
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	requestID := regexp.MustCompile(`name="request_id" type="hidden" value="([^"]+)"`).FindSubmatch(body)
	if len(requestID) != 2 {
		t.Fatalf("authorization request id missing: %s", body)
	}

	form := url.Values{"request_id": {string(requestID[1])}, "account": {account}}
	request, _ := http.NewRequest(
		http.MethodPost,
		running.issuer+"/authorize/complete",
		strings.NewReader(form.Encode()),
	)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatalf("complete authorization: %v", err)
	}
	response.Body.Close()
	location, _ := url.Parse(response.Header.Get("Location"))
	return location.Query().Get("code")
}

func exchangeCode(
	t *testing.T,
	client *http.Client,
	running runningProvider,
	code string,
	verifier string,
) tokenResponse {
	t.Helper()
	response := exchangeCodeResponse(t, client, running, code, verifier)
	defer response.Body.Close()
	var token tokenResponse
	decodeJSON(t, response.Body, &token)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("exchange failed: status=%d", response.StatusCode)
	}
	return token
}

func exchangeCodeResponse(
	t *testing.T,
	client *http.Client,
	running runningProvider,
	code string,
	verifier string,
) *http.Response {
	t.Helper()
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {running.redirectURL},
		"code_verifier": {verifier},
	}
	request, _ := http.NewRequest(
		http.MethodPost,
		running.issuer+"/token",
		strings.NewReader(form.Encode()),
	)
	request.SetBasicAuth(testClientID, testClientSecret)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("exchange code: %v", err)
	}
	return response
}

func verifyIDToken(t *testing.T, client *http.Client, issuer string, rawToken string) {
	t.Helper()
	parts := strings.Split(rawToken, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT segments: %d", len(parts))
	}
	var header map[string]string
	decodeBase64JSON(t, parts[0], &header)
	var claims map[string]any
	decodeBase64JSON(t, parts[1], &claims)
	if header["alg"] != "RS256" || claims["iss"] != issuer ||
		claims["sub"] != "e2e-admin" || claims["nonce"] != "nonce-value" {
		t.Fatalf("unexpected ID token: header=%#v claims=%#v", header, claims)
	}

	jwksResponse := get(t, client, issuer+"/jwks")
	defer jwksResponse.Body.Close()
	var jwks struct {
		Keys []struct {
			KeyID    string `json:"kid"`
			Modulus  string `json:"n"`
			Exponent string `json:"e"`
		} `json:"keys"`
	}
	decodeJSON(t, jwksResponse.Body, &jwks)
	if len(jwks.Keys) != 1 || jwks.Keys[0].KeyID != header["kid"] {
		t.Fatalf("unexpected JWKS: %#v", jwks)
	}
	modulus, _ := base64.RawURLEncoding.DecodeString(jwks.Keys[0].Modulus)
	exponent, _ := base64.RawURLEncoding.DecodeString(jwks.Keys[0].Exponent)
	publicKey := &rsa.PublicKey{
		N: new(big.Int).SetBytes(modulus),
		E: int(new(big.Int).SetBytes(exponent).Int64()),
	}
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("verify ID token signature: %v", err)
	}
	if _, err := x509.MarshalPKIXPublicKey(publicKey); err != nil {
		t.Fatalf("invalid public key: %v", err)
	}
}

func get(t *testing.T, client *http.Client, target string) *http.Response {
	t.Helper()
	response, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	return response
}

func decodeJSON(t *testing.T, reader io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(reader).Decode(target); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
}

func decodeBase64JSON(t *testing.T, encoded string, target any) {
	t.Helper()
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		t.Fatalf("decode token JSON: %v", err)
	}
}
