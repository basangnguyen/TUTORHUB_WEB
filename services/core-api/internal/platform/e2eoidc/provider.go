package e2eoidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	authorizationTTL = 5 * time.Minute
	accessTokenTTL   = 5 * time.Minute

	// CustomAccountID is the reserved chooser value of the optional
	// custom-account form; configured accounts may not use it.
	CustomAccountID       = "__custom__"
	customEmailMaxLength  = 254
	customSubjectPrefix   = "custom-"
	customSubjectHexBytes = 20

	// FlavorOIDC is the default standards-based fake issuer.
	FlavorOIDC = "oidc"
	// FlavorFacebook additionally serves Meta's fixed Login endpoints
	// (Graph v27.0) so TutorHub's Facebook adapter can run against it.
	FlavorFacebook = "facebook"

	facebookGraphVersion    = "v27.0"
	facebookASIDModulus     = 100_000_000_000_000_000 // 10^17
	facebookASIDDigits      = 17
	facebookDialogPath      = "/" + facebookGraphVersion + "/dialog/oauth"
	facebookAccessTokenPath = "/" + facebookGraphVersion + "/oauth/access_token"
	facebookJWKSPath        = "/.well-known/oauth/openid/jwks/"

	// FlavorMicrosoft serves the Microsoft identity platform v2.0 surface
	// (per-mode discovery, authorize, token, JWKS) so TutorHub's Microsoft
	// adapter can run against a loopback authority. ID tokens still carry the
	// canonical tenant issuer, exactly like Entra ID.
	FlavorMicrosoft = "microsoft"
	// MicrosoftPersonalTenantID is the fixed tenant of personal accounts (MSA).
	MicrosoftPersonalTenantID = "9188040d-6c67-4c5b-b112-36a304b66dad"
	// MicrosoftDefaultWorkTenantID prefills the custom-account form: a
	// fictional work or school tenant used only by the loopback fake.
	MicrosoftDefaultWorkTenantID = "5b3c1f0e-8d2a-4e61-9c7b-2a4f6d8e0c13"

	microsoftCanonicalAuthority = "https://login.microsoftonline.com"
	microsoftIssuerTemplate     = microsoftCanonicalAuthority + "/{tenantid}/v2.0"
	microsoftJWKSPath           = "/common/discovery/v2.0/keys"
)

// microsoftTenantModes are the authority segments TutorHub may be configured
// with (MICROSOFT_TENANT_MODE).
var microsoftTenantModes = []string{"common", "organizations", "consumers"}

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
    {{if .CustomAccounts}}
    <form action="/authorize/complete" method="post">
      <input name="request_id" type="hidden" value="{{.RequestID}}">
      <fieldset>
        <legend>Use a custom test account</legend>
        {{if .Microsoft}}
        <label>Test email <input name="custom_email" type="email" autocomplete="off"></label>
        <label>Tenant ID (tid) <input name="custom_tenant_id" type="text" autocomplete="off" required value="{{.DefaultTenantID}}"></label>
        <label>Object ID (oid, optional) <input name="custom_object_id" type="text" autocomplete="off"></label>
        <label>xms_edov claim <select name="custom_xms_edov">
          <option value="">Not sent</option>
          <option value="true">true</option>
          <option value="false">false</option>
        </select></label>
        <p>Personal Microsoft accounts use tenant {{.PersonalTenantID}}. Leave the email empty to omit the claim; the object ID is then required.</p>
        {{else}}
        <label>Test email <input name="custom_email" type="email" autocomplete="off" required></label>
        {{if .Facebook}}
        <label><input name="custom_email_withheld" type="checkbox" value="true"> Do not share email with the app</label>
        {{else}}
        <label><input name="custom_email_unverified" type="checkbox" value="true"> Email is not verified</label>
        {{end}}
        {{end}}
        <button name="account" type="submit" value="{{.CustomAccountID}}">Sign in with custom account</button>
      </fieldset>
    </form>
    {{end}}
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
	// EmailUnverified makes the issued claims carry email_verified=false.
	EmailUnverified bool
	// EmailWithheld omits the email claim, like a Facebook user who did not
	// share an email address with the app (Facebook and Microsoft flavors).
	EmailWithheld bool
	// TenantID is the Entra tenant (`tid`, and the tenant of the canonical
	// issuer) of a Microsoft flavor account; MicrosoftPersonalTenantID marks
	// a personal account.
	TenantID string
	// ObjectID is the optional `oid` claim of a Microsoft flavor account.
	ObjectID string
	// DomainVerified is the Microsoft `xms_edov` claim; nil omits it.
	DomainVerified *bool
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
	// AllowCustomAccounts adds a chooser form that signs in any syntactically
	// valid test email with a subject derived from it, so each E2E scenario
	// can use its own identity. Off unless explicitly enabled.
	AllowCustomAccounts bool
	// Flavor selects the endpoint surface: FlavorOIDC (default),
	// FlavorFacebook, which requires numeric app-scoped subjects (ASIDs), or
	// FlavorMicrosoft, which requires a tenant ID on every account.
	Flavor string
	Clock  func() time.Time
	Random io.Reader
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
	if strings.TrimSpace(config.Flavor) == "" {
		config.Flavor = FlavorOIDC
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
	if provider.facebook() {
		// TutorHub's Facebook adapter uses fixed Meta endpoints, not discovery.
		mux.HandleFunc("GET "+facebookDialogPath, provider.authorize)
		mux.HandleFunc("GET "+facebookAccessTokenPath, provider.facebookAccessToken)
		mux.HandleFunc("GET "+facebookJWKSPath, provider.jwks)
	}
	if provider.microsoft() {
		// TutorHub's Microsoft adapter discovers {authority}/{mode}/v2.0 and
		// requires the JWKS on the same origin as the authority.
		for _, mode := range microsoftTenantModes {
			mux.HandleFunc("GET /"+mode+"/v2.0/.well-known/openid-configuration", provider.microsoftDiscovery(mode))
			mux.HandleFunc("GET /"+mode+"/oauth2/v2.0/authorize", provider.authorize)
			mux.HandleFunc("POST /"+mode+"/oauth2/v2.0/token", provider.token)
		}
		mux.HandleFunc("GET "+microsoftJWKSPath, provider.jwks)
	}

	// Chromium enforces form-action across the complete redirect chain, so the
	// chooser must explicitly allow the validated API callback and web origins.
	return securityHeaders(mux, provider.callbackOrigin, provider.webOrigin)
}

func (provider *Provider) facebook() bool {
	return provider.config.Flavor == FlavorFacebook
}

func (provider *Provider) microsoft() bool {
	return provider.config.Flavor == FlavorMicrosoft
}

// microsoftDiscovery mirrors the v2.0 metadata of one authority mode: the
// multi-tenant issuer template (the consumers alias names the concrete
// personal-account tenant) and endpoints on the loopback authority.
func (provider *Provider) microsoftDiscovery(mode string) http.HandlerFunc {
	return func(writer http.ResponseWriter, _ *http.Request) {
		base := provider.config.IssuerURL + "/" + mode
		issuer := microsoftIssuerTemplate
		if mode == "consumers" {
			issuer = MicrosoftIssuer(MicrosoftPersonalTenantID)
		}
		writeJSON(writer, http.StatusOK, map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                base + "/oauth2/v2.0/authorize",
			"token_endpoint":                        base + "/oauth2/v2.0/token",
			"jwks_uri":                              provider.config.IssuerURL + microsoftJWKSPath,
			"response_types_supported":              []string{"code"},
			"response_modes_supported":              []string{"query"},
			"subject_types_supported":               []string{"pairwise"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"token_endpoint_auth_methods_supported": []string{"client_secret_post"},
			"code_challenge_methods_supported":      []string{"S256"},
			"scopes_supported":                      []string{"openid", "profile", "email"},
			"claims_supported": []string{
				"iss", "sub", "aud", "exp", "iat", "nbf", "nonce", "tid", "oid",
				"email", "xms_edov", "name", "preferred_username", "ver",
			},
		})
	}
}

// MicrosoftIssuer is the canonical v2.0 issuer of a tenant, which Microsoft
// ID tokens carry whatever authority URL served them.
func MicrosoftIssuer(tenantID string) string {
	return microsoftCanonicalAuthority + "/" + tenantID + "/v2.0"
}

// MicrosoftTestObjectID derives the deterministic `oid` that the Microsoft
// flavor issues for a custom account without an explicit object ID: a
// name-based (SHA-1) UUID of the tenant and email, so each tenant yields a
// different object for the same email address.
func MicrosoftTestObjectID(tenantID string, email string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("tutorhub-e2e:microsoft-oid:"+tenantID+":"+email)).String()
}

// MicrosoftTestSubject derives the pairwise `sub` of a tenant and object, in
// the 43-character base64url shape of Microsoft's app-scoped subjects.
func MicrosoftTestSubject(tenantID string, objectID string) string {
	digest := sha256.Sum256([]byte("tutorhub-e2e:microsoft-sub:" + tenantID + ":" + objectID))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func microsoftGUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
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
	key := map[string]string{
		"kty": "RSA",
		"use": "sig",
		"alg": "RS256",
		"kid": provider.keyID,
		"n":   base64.RawURLEncoding.EncodeToString(provider.privateKey.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(exponent),
	}
	if provider.microsoft() {
		// Entra keys name the issuers they may sign for and omit `alg`.
		delete(key, "alg")
		key["issuer"] = microsoftIssuerTemplate
	}
	writeJSON(writer, http.StatusOK, map[string]any{"keys": []map[string]string{key}})
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
		"RequestID":        requestID,
		"Accounts":         provider.accountOrder,
		"CustomAccounts":   provider.config.AllowCustomAccounts,
		"CustomAccountID":  CustomAccountID,
		"Facebook":         provider.facebook(),
		"Microsoft":        provider.microsoft(),
		"DefaultTenantID":  MicrosoftDefaultWorkTenantID,
		"PersonalTenantID": MicrosoftPersonalTenantID,
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
	if accountID == CustomAccountID {
		account, accountExists = provider.customAccount(request.PostForm)
	}

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

	provider.redeemCode(
		writer,
		strings.TrimSpace(request.PostForm.Get("code")),
		strings.TrimSpace(request.PostForm.Get("redirect_uri")),
		strings.TrimSpace(request.PostForm.Get("code_verifier")),
		true,
	)
}

// facebookAccessToken mirrors Meta's manual OIDC code exchange: a GET with
// client_id, redirect_uri, code and the PKCE verifier, and no client secret.
func (provider *Provider) facebookAccessToken(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	if subtle.ConstantTimeCompare(
		[]byte(query.Get("client_id")),
		[]byte(provider.config.ClientID),
	) != 1 {
		writeOAuthError(writer, http.StatusBadRequest, "invalid_client")
		return
	}
	provider.redeemCode(
		writer,
		strings.TrimSpace(query.Get("code")),
		strings.TrimSpace(query.Get("redirect_uri")),
		strings.TrimSpace(query.Get("code_verifier")),
		false,
	)
}

// redeemCode exchanges a single-use authorization code bound to the PKCE
// challenge and redirect URI for an ID token and a short-lived access token.
func (provider *Provider) redeemCode(
	writer http.ResponseWriter,
	code string,
	redirectURL string,
	verifier string,
	includeScope bool,
) {
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

	response := map[string]any{
		"access_token": accessToken,
		"token_type":   "Bearer",
		"expires_in":   int(accessTokenTTL.Seconds()),
		"id_token":     idToken,
	}
	if includeScope {
		response["scope"] = authorization.Scope
	}
	writeJSON(writer, http.StatusOK, response)
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
		"email_verified":     !grant.Account.EmailUnverified,
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
	payload := map[string]any{
		"iss":       provider.config.IssuerURL,
		"sub":       code.Account.Subject,
		"aud":       provider.config.ClientID,
		"exp":       now.Add(accessTokenTTL).Unix(),
		"iat":       now.Unix(),
		"auth_time": now.Unix(),
		"nonce":     code.Nonce,
	}
	if provider.facebook() {
		// Meta's ID token carries no email_verified claim, and no email at
		// all when the person did not share one with the app.
		if !code.Account.EmailWithheld {
			payload["email"] = code.Account.Email
		}
		payload["name"] = code.Account.DisplayName
	} else if provider.microsoft() {
		// Entra ID v2.0: canonical tenant issuer, tid/oid, optional email and
		// xms_edov, no email_verified and no auth_time. preferred_username is
		// mutable and must never be trusted by the relying party.
		delete(payload, "auth_time")
		payload["iss"] = MicrosoftIssuer(code.Account.TenantID)
		payload["tid"] = code.Account.TenantID
		payload["nbf"] = now.Unix()
		payload["ver"] = "2.0"
		payload["name"] = code.Account.DisplayName
		if code.Account.ObjectID != "" {
			payload["oid"] = code.Account.ObjectID
		}
		if !code.Account.EmailWithheld && code.Account.Email != "" {
			payload["email"] = code.Account.Email
			payload["preferred_username"] = code.Account.Email
		}
		if code.Account.DomainVerified != nil {
			payload["xms_edov"] = *code.Account.DomainVerified
		}
	} else {
		payload["email"] = code.Account.Email
		payload["email_verified"] = !code.Account.EmailUnverified
	}
	claims, err := encodeJSON(payload)
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

// customAccount builds a deterministic identity from the custom-account
// form: the same email always maps to the same subject, so a later sign-in
// with that email finds the identity created earlier.
func (provider *Provider) customAccount(form url.Values) (Account, bool) {
	if !provider.config.AllowCustomAccounts {
		return Account{}, false
	}
	if provider.microsoft() {
		return microsoftCustomAccount(form)
	}
	email := strings.ToLower(strings.TrimSpace(form.Get("custom_email")))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Name != "" || address.Address != email ||
		len(email) > customEmailMaxLength {
		return Account{}, false
	}
	digest := sha256.Sum256([]byte(email))
	localPart, _, _ := strings.Cut(email, "@")
	if provider.facebook() {
		return Account{
			ID:            CustomAccountID,
			Subject:       FacebookTestASID(email),
			Email:         email,
			DisplayName:   localPart,
			Locale:        "en",
			EmailWithheld: form.Get("custom_email_withheld") == "true",
		}, true
	}
	return Account{
		ID:              CustomAccountID,
		Subject:         customSubjectPrefix + hex.EncodeToString(digest[:customSubjectHexBytes]),
		Email:           email,
		DisplayName:     localPart,
		Locale:          "en",
		EmailUnverified: form.Get("custom_email_unverified") == "true",
	}, true
}

// microsoftCustomAccount builds a Microsoft identity from the custom form: a
// tenant (required), an optional email, an optional object ID (derived from
// tenant and email when empty) and an optional xms_edov boolean. The same
// tenant and email always map to the same subject.
func microsoftCustomAccount(form url.Values) (Account, bool) {
	email := strings.ToLower(strings.TrimSpace(form.Get("custom_email")))
	if email != "" {
		address, err := mail.ParseAddress(email)
		if err != nil || address.Name != "" || address.Address != email ||
			len(email) > customEmailMaxLength {
			return Account{}, false
		}
	}
	tenantID := strings.ToLower(strings.TrimSpace(form.Get("custom_tenant_id")))
	if !microsoftGUID(tenantID) {
		return Account{}, false
	}
	objectID := strings.ToLower(strings.TrimSpace(form.Get("custom_object_id")))
	switch {
	case objectID == "" && email == "":
		return Account{}, false
	case objectID == "":
		objectID = MicrosoftTestObjectID(tenantID, email)
	case !microsoftGUID(objectID):
		return Account{}, false
	}
	var domainVerified *bool
	switch form.Get("custom_xms_edov") {
	case "":
	case "true", "false":
		value := form.Get("custom_xms_edov") == "true"
		domainVerified = &value
	default:
		return Account{}, false
	}
	displayName, _, _ := strings.Cut(email, "@")
	if displayName == "" {
		displayName = "Microsoft test user"
	}
	return Account{
		ID:             CustomAccountID,
		Subject:        MicrosoftTestSubject(tenantID, objectID),
		Email:          email,
		DisplayName:    displayName,
		Locale:         "en",
		EmailWithheld:  email == "",
		TenantID:       tenantID,
		ObjectID:       objectID,
		DomainVerified: domainVerified,
	}, true
}

// FacebookTestASID derives the deterministic numeric app-scoped user ID that
// the Facebook flavor issues for a custom account seed: "1" followed by the
// first eight bytes of SHA-256(seed), read big-endian, modulo 10^17 and
// zero-padded to 17 digits. Playwright recomputes it to sign Meta callbacks.
func FacebookTestASID(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	value := binary.BigEndian.Uint64(digest[:8]) % facebookASIDModulus
	return fmt.Sprintf("1%0*d", facebookASIDDigits, value)
}

func isNumericSubject(subject string) bool {
	if subject == "" || len(subject) > 128 {
		return false
	}
	for _, character := range subject {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
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
	flavor := strings.TrimSpace(config.Flavor)
	if flavor != "" && flavor != FlavorOIDC && flavor != FlavorFacebook && flavor != FlavorMicrosoft {
		return fmt.Errorf("%w: unknown flavor", errInvalidConfiguration)
	}

	ids := make(map[string]struct{}, len(config.Accounts))
	subjects := make(map[string]struct{}, len(config.Accounts))
	emails := make(map[string]struct{}, len(config.Accounts))
	for _, account := range config.Accounts {
		id := strings.TrimSpace(account.ID)
		subject := strings.TrimSpace(account.Subject)
		email := strings.ToLower(strings.TrimSpace(account.Email))
		if id == CustomAccountID {
			return fmt.Errorf("%w: account id %q is reserved", errInvalidConfiguration, CustomAccountID)
		}
		if id == "" || subject == "" || email == "" ||
			strings.TrimSpace(account.DisplayName) == "" ||
			!strings.Contains(email, "@") {
			return fmt.Errorf("%w: every test account requires an id, subject, email, and display name", errInvalidConfiguration)
		}
		if flavor == FlavorFacebook && !isNumericSubject(subject) {
			return fmt.Errorf("%w: Facebook test accounts need numeric app-scoped subjects", errInvalidConfiguration)
		}
		if flavor == FlavorMicrosoft && (!microsoftGUID(account.TenantID) ||
			(account.ObjectID != "" && !microsoftGUID(account.ObjectID)) ||
			strings.ContainsAny(account.Subject, " \t\r\n")) {
			return fmt.Errorf("%w: Microsoft test accounts need a canonical tenant ID, an optional canonical object ID and a subject without whitespace", errInvalidConfiguration)
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
