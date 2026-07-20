package e2eoidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	authorizationTTL = 5 * time.Minute
	accessTokenTTL   = 5 * time.Minute
)

var (
	errInvalidConfiguration = errors.New("invalid E2E OIDC configuration")
	loginPageTemplate       = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>TutorHub test sign-in</title>
</head>
<body>
  <main>
    <h1>TutorHub test identity provider</h1>
    <p>This account chooser is available only to the loopback E2E environment.</p>
    <form action="/authorize/complete" method="post">
      <input name="request_id" type="hidden" value="{{.RequestID}}">
      <fieldset>
        <legend>Choose a test account</legend>
        {{range .Accounts}}
        <button name="account" type="submit" value="{{.ID}}">
          Sign in as {{.DisplayName}} ({{.Email}})
        </button>
        {{end}}
      </fieldset>
    </form>
  </main>
</body>
</html>`))
)

// Account is a deterministic local identity exposed by the E2E account chooser.
// It must never represent a production user.
type Account struct {
	ID          string
	Subject     string
	Email       string
	DisplayName string
	Locale      string
}

// Config describes a single confidential loopback client.
type Config struct {
	Environment   string
	ListenAddress string
	IssuerURL     string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	PostLogoutURL string
	Accounts      []Account
	Clock         func() time.Time
	Random        io.Reader
}

type authorizationRequest struct {
	ClientID      string
	RedirectURL   string
	State         string
	Nonce         string
	CodeChallenge string
	Scope         string
	ExpiresAt     time.Time
}

type authorizationCode struct {
	Account       Account
	ClientID      string
	RedirectURL   string
	Nonce         string
	CodeChallenge string
	Scope         string
	ExpiresAt     time.Time
}

type accessGrant struct {
	Account   Account
	ExpiresAt time.Time
}

// Provider implements only the OIDC surface required by TutorHub's BFF. It is
// deliberately constrained to APP_ENV=test and loopback URLs.
type Provider struct {
	config         Config
	callbackOrigin string
	webOrigin      string
	privateKey     *rsa.PrivateKey
	keyID          string
	accounts       map[string]Account
	accountOrder   []Account

	mu       sync.Mutex
	pending  map[string]authorizationRequest
	codes    map[string]authorizationCode
	accesses map[string]accessGrant
}

// New validates the loopback-only boundary and creates an ephemeral signing
// key. Neither the client secret nor issued credentials are persisted.
func New(config Config) (*Provider, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	callbackURL, err := url.Parse(config.RedirectURL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse callback URL", errInvalidConfiguration)
	}
	postLogoutURL, err := url.Parse(config.PostLogoutURL)
	if err != nil {
		return nil, fmt.Errorf("%w: parse post-logout URL", errInvalidConfiguration)
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}

	privateKey, err := rsa.GenerateKey(config.Random, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate ephemeral E2E signing key: %w", err)
	}
	keyID, err := randomToken(config.Random, 12)
	if err != nil {
		return nil, fmt.Errorf("generate E2E signing key id: %w", err)
	}

	accounts := make(map[string]Account, len(config.Accounts))
	accountOrder := append([]Account(nil), config.Accounts...)
	sort.Slice(accountOrder, func(left, right int) bool {
		return accountOrder[left].ID < accountOrder[right].ID
	})
	for _, account := range accountOrder {
		accounts[account.ID] = account
	}

	return &Provider{
		config:         config,
		callbackOrigin: callbackURL.Scheme + "://" + callbackURL.Host,
		webOrigin:      postLogoutURL.Scheme + "://" + postLogoutURL.Host,
		privateKey:     privateKey,
		keyID:          keyID,
		accounts:       accounts,
		accountOrder:   accountOrder,
		pending:        make(map[string]authorizationRequest),
		codes:          make(map[string]authorizationCode),
		accesses:       make(map[string]accessGrant),
	}, nil
}

// Handler returns the fake provider's isolated HTTP surface.
func (provider *Provider) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", provider.discovery)
	mux.HandleFunc("GET /jwks", provider.jwks)
	mux.HandleFunc("GET /authorize", provider.authorize)
	mux.HandleFunc("POST /authorize/complete", provider.completeAuthorization)
	mux.HandleFunc("POST /token", provider.token)
	mux.HandleFunc("GET /userinfo", provider.userInfo)
	mux.HandleFunc("GET /logout", provider.logout)
	mux.HandleFunc("GET /healthz", provider.health)

	// Chromium enforces form-action across the complete redirect chain, so the
	// chooser must explicitly allow the validated API callback and web origins.
	return securityHeaders(mux, provider.callbackOrigin, provider.webOrigin)
}

func (provider *Provider) discovery(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"issuer":                                provider.config.IssuerURL,
		"authorization_endpoint":                provider.config.IssuerURL + "/authorize",
		"token_endpoint":                        provider.config.IssuerURL + "/token",
		"userinfo_endpoint":                     provider.config.IssuerURL + "/userinfo",
		"jwks_uri":                              provider.config.IssuerURL + "/jwks",
		"end_session_endpoint":                  provider.config.IssuerURL + "/logout",
		"response_types_supported":              []string{"code"},
		"response_modes_supported":              []string{"query"},
		"grant_types_supported":                 []string{"authorization_code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"claims_supported": []string{
			"iss", "sub", "aud", "exp", "iat", "auth_time", "nonce",
			"email", "email_verified", "name", "preferred_username", "locale",
		},
	})
}

func (provider *Provider) jwks(writer http.ResponseWriter, _ *http.Request) {
	exponent := big.NewInt(int64(provider.privateKey.PublicKey.E)).Bytes()
	writeJSON(writer, http.StatusOK, map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": provider.keyID,
			"n":   base64.RawURLEncoding.EncodeToString(provider.privateKey.PublicKey.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(exponent),
		}},
	})
}

func (provider *Provider) authorize(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	authorization, err := provider.validateAuthorizationRequest(query)
	if err != nil {
		writeOAuthError(writer, http.StatusBadRequest, "invalid_request")
		return
	}

	requestID, err := randomToken(provider.config.Random, 24)
	if err != nil {
		writeOAuthError(writer, http.StatusInternalServerError, "server_error")
		return
	}
	provider.mu.Lock()
	provider.removeExpiredLocked(provider.config.Clock().UTC())
	provider.pending[requestID] = authorization
	provider.mu.Unlock()

	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	_ = loginPageTemplate.Execute(writer, map[string]any{
		"RequestID": requestID,
		"Accounts":  provider.accountOrder,
	})
}

func (provider *Provider) completeAuthorization(writer http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		writeOAuthError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	requestID := strings.TrimSpace(request.PostForm.Get("request_id"))
	accountID := strings.TrimSpace(request.PostForm.Get("account"))
	account, accountExists := provider.accounts[accountID]

	now := provider.config.Clock().UTC()
	provider.mu.Lock()
	provider.removeExpiredLocked(now)
	authorization, requestExists := provider.pending[requestID]
	if requestExists && accountExists {
		delete(provider.pending, requestID)
	}
	provider.mu.Unlock()
	if !requestExists || !accountExists {
		writeOAuthError(writer, http.StatusBadRequest, "invalid_request")
		return
	}

	code, err := randomToken(provider.config.Random, 32)
	if err != nil {
		writeOAuthError(writer, http.StatusInternalServerError, "server_error")
		return
	}
	provider.mu.Lock()
	provider.codes[code] = authorizationCode{
		Account:       account,
		ClientID:      authorization.ClientID,
		RedirectURL:   authorization.RedirectURL,
		Nonce:         authorization.Nonce,
		CodeChallenge: authorization.CodeChallenge,
		Scope:         authorization.Scope,
		ExpiresAt:     now.Add(authorizationTTL),
	}
	provider.mu.Unlock()

	redirect, _ := url.Parse(authorization.RedirectURL)
	redirectQuery := redirect.Query()
	redirectQuery.Set("code", code)
	redirectQuery.Set("state", authorization.State)
	redirect.RawQuery = redirectQuery.Encode()
	http.Redirect(writer, request, redirect.String(), http.StatusSeeOther)
}

func (provider *Provider) token(writer http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		writeOAuthError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	if !provider.validClient(request) {
		writer.Header().Set("WWW-Authenticate", `Basic realm="TutorHub E2E OIDC"`)
		writeOAuthError(writer, http.StatusUnauthorized, "invalid_client")
		return
	}
	if request.PostForm.Get("grant_type") != "authorization_code" {
		writeOAuthError(writer, http.StatusBadRequest, "unsupported_grant_type")
		return
	}

	code := strings.TrimSpace(request.PostForm.Get("code"))
	redirectURL := strings.TrimSpace(request.PostForm.Get("redirect_uri"))
	verifier := strings.TrimSpace(request.PostForm.Get("code_verifier"))
	now := provider.config.Clock().UTC()

	provider.mu.Lock()
	provider.removeExpiredLocked(now)
	authorization, exists := provider.codes[code]
	if exists {
		delete(provider.codes, code)
	}
	valid := exists &&
		authorization.ClientID == provider.config.ClientID &&
		authorization.RedirectURL == redirectURL &&
		validPKCEVerifier(verifier, authorization.CodeChallenge)
	provider.mu.Unlock()
	if !valid {
		writeOAuthError(writer, http.StatusBadRequest, "invalid_grant")
		return
	}

	accessToken, err := randomToken(provider.config.Random, 32)
	if err != nil {
		writeOAuthError(writer, http.StatusInternalServerError, "server_error")
		return
	}
	idToken, err := provider.signIDToken(authorization, now)
	if err != nil {
		writeOAuthError(writer, http.StatusInternalServerError, "server_error")
		return
	}
	provider.mu.Lock()
	provider.accesses[accessToken] = accessGrant{
		Account:   authorization.Account,
		ExpiresAt: now.Add(accessTokenTTL),
	}
	provider.mu.Unlock()

	writeJSON(writer, http.StatusOK, map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(accessTokenTTL.Seconds()),
		"id_token":     idToken,
		"scope":        authorization.Scope,
	})
}

func (provider *Provider) userInfo(writer http.ResponseWriter, request *http.Request) {
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeOAuthError(writer, http.StatusUnauthorized, "invalid_token")
		return
	}

	now := provider.config.Clock().UTC()
	provider.mu.Lock()
	provider.removeExpiredLocked(now)
	grant, exists := provider.accesses[parts[1]]
	provider.mu.Unlock()
	if !exists {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writeOAuthError(writer, http.StatusUnauthorized, "invalid_token")
		return
	}

	writeJSON(writer, http.StatusOK, map[string]any{
		"sub":                grant.Account.Subject,
		"email":              grant.Account.Email,
		"email_verified":     true,
		"name":               grant.Account.DisplayName,
		"preferred_username": grant.Account.Email,
		"locale":             grant.Account.Locale,
	})
}

func (provider *Provider) logout(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	if query.Get("client_id") != provider.config.ClientID ||
		query.Get("post_logout_redirect_uri") != provider.config.PostLogoutURL {
		writeOAuthError(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	http.Redirect(writer, request, provider.config.PostLogoutURL, http.StatusSeeOther)
}

func (provider *Provider) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (provider *Provider) validateAuthorizationRequest(values url.Values) (authorizationRequest, error) {
	scope := strings.Fields(values.Get("scope"))
	if values.Get("client_id") != provider.config.ClientID ||
		values.Get("redirect_uri") != provider.config.RedirectURL ||
		values.Get("response_type") != "code" ||
		values.Get("state") == "" ||
		values.Get("nonce") == "" ||
		values.Get("code_challenge_method") != "S256" ||
		len(values.Get("code_challenge")) != 43 ||
		!contains(scope, "openid") {
		return authorizationRequest{}, errInvalidConfiguration
	}
	for _, requestedScope := range scope {
		if !contains([]string{"openid", "profile", "email"}, requestedScope) {
			return authorizationRequest{}, errInvalidConfiguration
		}
	}
	return authorizationRequest{
		ClientID:      values.Get("client_id"),
		RedirectURL:   values.Get("redirect_uri"),
		State:         values.Get("state"),
		Nonce:         values.Get("nonce"),
		CodeChallenge: values.Get("code_challenge"),
		Scope:         strings.Join(scope, " "),
		ExpiresAt:     provider.config.Clock().UTC().Add(authorizationTTL),
	}, nil
}

func (provider *Provider) validClient(request *http.Request) bool {
	clientID, clientSecret, basicOK := request.BasicAuth()
	if !basicOK {
		clientID = request.PostForm.Get("client_id")
		clientSecret = request.PostForm.Get("client_secret")
	}
	return subtle.ConstantTimeCompare(
		[]byte(clientID),
		[]byte(provider.config.ClientID),
	) == 1 && subtle.ConstantTimeCompare(
		[]byte(clientSecret),
		[]byte(provider.config.ClientSecret),
	) == 1
}

func (provider *Provider) signIDToken(code authorizationCode, now time.Time) (string, error) {
	header, err := encodeJSON(map[string]string{
		"alg": "RS256",
		"kid": provider.keyID,
		"typ": "JWT",
	})
	if err != nil {
		return "", err
	}
	claims, err := encodeJSON(map[string]any{
		"iss":            provider.config.IssuerURL,
		"sub":            code.Account.Subject,
		"aud":            provider.config.ClientID,
		"exp":            now.Add(accessTokenTTL).Unix(),
		"iat":            now.Unix(),
		"auth_time":      now.Unix(),
		"nonce":          code.Nonce,
		"email":          code.Account.Email,
		"email_verified": true,
	})
	if err != nil {
		return "", err
	}
	signingInput := header + "." + claims
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(
		provider.config.Random,
		provider.privateKey,
		crypto.SHA256,
		digest[:],
	)
	if err != nil {
		return "", fmt.Errorf("sign E2E ID token: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (provider *Provider) removeExpiredLocked(now time.Time) {
	for requestID, authorization := range provider.pending {
		if !authorization.ExpiresAt.After(now) {
			delete(provider.pending, requestID)
		}
	}
	for code, authorization := range provider.codes {
		if !authorization.ExpiresAt.After(now) {
			delete(provider.codes, code)
		}
	}
	for token, grant := range provider.accesses {
		if !grant.ExpiresAt.After(now) {
			delete(provider.accesses, token)
		}
	}
}

func validateConfig(config Config) error {
	if strings.TrimSpace(config.Environment) != "test" {
		return fmt.Errorf("%w: fake issuer requires APP_ENV=test", errInvalidConfiguration)
	}
	if err := validateLoopbackAddress(config.ListenAddress); err != nil {
		return fmt.Errorf("%w: %v", errInvalidConfiguration, err)
	}
	if err := validateLoopbackURL("issuer", config.IssuerURL, true); err != nil {
		return fmt.Errorf("%w: %v", errInvalidConfiguration, err)
	}
	if err := validateLoopbackURL("redirect", config.RedirectURL, false); err != nil {
		return fmt.Errorf("%w: %v", errInvalidConfiguration, err)
	}
	if err := validateLoopbackURL("post logout", config.PostLogoutURL, false); err != nil {
		return fmt.Errorf("%w: %v", errInvalidConfiguration, err)
	}
	if strings.TrimSpace(config.ClientID) == "" || len(config.ClientSecret) < 16 {
		return fmt.Errorf("%w: client id and an ephemeral client secret of at least 16 characters are required", errInvalidConfiguration)
	}
	if len(config.Accounts) == 0 {
		return fmt.Errorf("%w: at least one test account is required", errInvalidConfiguration)
	}

	ids := make(map[string]struct{}, len(config.Accounts))
	subjects := make(map[string]struct{}, len(config.Accounts))
	emails := make(map[string]struct{}, len(config.Accounts))
	for _, account := range config.Accounts {
		id := strings.TrimSpace(account.ID)
		subject := strings.TrimSpace(account.Subject)
		email := strings.ToLower(strings.TrimSpace(account.Email))
		if id == "" || subject == "" || email == "" ||
			strings.TrimSpace(account.DisplayName) == "" ||
			!strings.Contains(email, "@") {
			return fmt.Errorf("%w: every test account requires an id, subject, email, and display name", errInvalidConfiguration)
		}
		if _, exists := ids[id]; exists {
			return fmt.Errorf("%w: duplicate account id", errInvalidConfiguration)
		}
		if _, exists := subjects[subject]; exists {
			return fmt.Errorf("%w: duplicate account subject", errInvalidConfiguration)
		}
		if _, exists := emails[email]; exists {
			return fmt.Errorf("%w: duplicate account email", errInvalidConfiguration)
		}
		ids[id] = struct{}{}
		subjects[subject] = struct{}{}
		emails[email] = struct{}{}
	}
	return nil
}

func validateLoopbackAddress(address string) error {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || strings.TrimSpace(port) == "" || !isLoopbackHost(host) {
		return errors.New("listen address must name an explicit loopback host and port")
	}
	return nil
}

func validateLoopbackURL(label string, rawURL string, issuer bool) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		!isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("%s URL must be an HTTP loopback URL without credentials, query, or fragment", label)
	}
	if issuer && parsed.Path != "" && parsed.Path != "/" {
		return errors.New("issuer URL must not contain a path")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func validPKCEVerifier(verifier string, expectedChallenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	actual := base64.RawURLEncoding.EncodeToString(digest[:])
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expectedChallenge)) == 1
}

func encodeJSON(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode E2E token payload: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func randomToken(reader io.Reader, size int) (string, error) {
	value := make([]byte, size)
	if _, err := io.ReadFull(reader, value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func securityHeaders(
	next http.Handler,
	callbackOrigin string,
	webOrigin string,
) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set(
			"Content-Security-Policy",
			"default-src 'none'; form-action 'self' "+callbackOrigin+" "+webOrigin+"; frame-ancestors 'none'; base-uri 'none'",
		)
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(writer, request)
	})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeOAuthError(writer http.ResponseWriter, status int, code string) {
	writeJSON(writer, status, map[string]string{"error": code})
}
