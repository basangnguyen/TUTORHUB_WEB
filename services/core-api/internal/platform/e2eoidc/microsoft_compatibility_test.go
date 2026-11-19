package e2eoidc_test

import (
	"context"
	"crypto/rsa"
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

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/e2eoidc"
)

const (
	microsoftClientID     = "tutorhub-e2e-microsoft"
	microsoftClientSecret = "ephemeral-microsoft-client-secret"
	microsoftRedirectURL  = "http://127.0.0.1:8080/api/v1/auth/oauth/microsoft/callback"
	microsoftVerifier     = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	microsoftOtherTenant  = "c0a8e9d1-2b3f-4a5c-8d7e-9f0a1b2c3d4e"
	microsoftExplicitOID  = "7d4e2c1a-9b8f-4e6d-a5c3-1f2e3d4c5b6a"
)

type runningMicrosoft struct {
	authority string
	client    *http.Client
}

func microsoftConfig(listenAddress string, authority string) e2eoidc.Config {
	verified := true
	return e2eoidc.Config{
		Environment:   "test",
		ListenAddress: listenAddress,
		IssuerURL:     authority,
		ClientID:      microsoftClientID,
		ClientSecret:  microsoftClientSecret,
		RedirectURL:   microsoftRedirectURL,
		PostLogoutURL: "http://127.0.0.1:5173/signed-out",
		Accounts: []e2eoidc.Account{{
			ID: "microsoft-teacher", Subject: e2eoidc.MicrosoftTestSubject(e2eoidc.MicrosoftDefaultWorkTenantID, microsoftExplicitOID),
			Email: "teacher.microsoft.e2e@tutorhub.local", DisplayName: "E2E Microsoft Teacher", Locale: "en",
			TenantID: e2eoidc.MicrosoftDefaultWorkTenantID, ObjectID: microsoftExplicitOID, DomainVerified: &verified,
		}},
		AllowCustomAccounts: true,
		Flavor:              e2eoidc.FlavorMicrosoft,
	}
}

func startFakeMicrosoft(t *testing.T) runningMicrosoft {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	authority := "http://" + listener.Addr().String()
	fake, err := e2eoidc.New(microsoftConfig(listener.Addr().String(), authority))
	if err != nil {
		listener.Close()
		t.Fatalf("create fake Microsoft: %v", err)
	}
	server := &http.Server{Handler: fake.Handler(), ReadHeaderTimeout: time.Second}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})
	return runningMicrosoft{
		authority: authority,
		client: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

func newMicrosoftAdapter(t *testing.T, running runningMicrosoft, mode string) *identity.MicrosoftProvider {
	t.Helper()
	adapter, err := identity.NewMicrosoftProvider(context.Background(), identity.MicrosoftProviderConfig{
		OIDCProviderConfig: identity.OIDCProviderConfig{
			IssuerURL: running.authority, ClientID: microsoftClientID, ClientSecret: microsoftClientSecret,
			CallbackURL: microsoftRedirectURL, HTTPTimeout: 2 * time.Second,
		},
		TenantMode: mode,
	})
	if err != nil {
		t.Fatalf("configure the Microsoft adapter (%s): %v", mode, err)
	}
	return adapter
}

// chooseMicrosoftAccount opens the authorize URL, submits the custom-account
// form and returns the fake's redirect (callback URL or error status).
func chooseMicrosoftAccount(t *testing.T, running runningMicrosoft, authorizeURL string, fields url.Values) *http.Response {
	t.Helper()
	chooser, err := running.client.Get(authorizeURL)
	if err != nil {
		t.Fatalf("open chooser: %v", err)
	}
	body, _ := io.ReadAll(chooser.Body)
	chooser.Body.Close()
	requestID := regexp.MustCompile(`name="request_id" type="hidden" value="([^"]+)"`).FindSubmatch(body)
	if chooser.StatusCode != http.StatusOK || len(requestID) != 2 {
		t.Fatalf("unexpected chooser: status=%d", chooser.StatusCode)
	}
	for _, field := range []string{`name="custom_tenant_id"`, `name="custom_object_id"`, `name="custom_xms_edov"`} {
		if !strings.Contains(string(body), field) {
			t.Fatalf("the Microsoft chooser must offer %s", field)
		}
	}
	if strings.Contains(string(body), `name="custom_email_unverified"`) {
		t.Fatal("the Microsoft chooser expresses trust through xms_edov, not email_verified")
	}
	form := url.Values{"request_id": {string(requestID[1])}, "account": {e2eoidc.CustomAccountID}}
	for key, values := range fields {
		form[key] = values
	}
	request, _ := http.NewRequest(http.MethodPost, running.authority+"/authorize/complete", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatalf("complete authorization: %v", err)
	}
	response.Body.Close()
	return response
}

func microsoftCallbackCode(t *testing.T, response *http.Response) string {
	t.Helper()
	callback, err := url.Parse(response.Header.Get("Location"))
	if response.StatusCode != http.StatusSeeOther || err != nil ||
		callback.Query().Get("state") != "state-value" || callback.Query().Get("code") == "" {
		t.Fatalf("invalid callback redirect: %d %q", response.StatusCode, response.Header.Get("Location"))
	}
	if callback.Query().Has("iss") {
		t.Fatal("like Entra ID, the fake must not add an RFC 9207 iss parameter")
	}
	return callback.Query().Get("code")
}

func decodeJWTPart(t *testing.T, token string, index int) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("malformed JWT")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[index])
	if err != nil {
		t.Fatalf("decode JWT part: %v", err)
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("parse JWT part: %v", err)
	}
	return value
}

// AUTH-07B: the fourth fake instance stands in for Microsoft in Playwright.
// Pin that TutorHub's real Microsoft adapter (per-mode v2.0 discovery, JWKS on
// the authority origin with per-key issuers, canonical tenant issuer, tid/oid,
// xms_edov) accepts it for work, school and personal accounts.
func TestMicrosoftFlavorIsCompatibleWithTutorHubMicrosoftAdapter(t *testing.T) {
	running := startFakeMicrosoft(t)
	adapter := newMicrosoftAdapter(t, running, "common")
	authorizeURL := adapter.AuthorizationURL("state-value", "nonce-value", identity.PKCEChallenge(microsoftVerifier))
	if !strings.HasPrefix(authorizeURL, running.authority+"/common/oauth2/v2.0/authorize?") {
		t.Fatalf("adapter must use the v2.0 authorize path of its mode, got %q", authorizeURL)
	}

	signIn := func(t *testing.T, fields url.Values) identity.ProviderClaims {
		t.Helper()
		code := microsoftCallbackCode(t, chooseMicrosoftAccount(t, running, authorizeURL, fields))
		claims, err := adapter.ExchangeAndVerify(context.Background(), code, microsoftVerifier)
		if err != nil {
			t.Fatalf("exchange through the Microsoft adapter: %v", err)
		}
		return claims
	}

	const email = "auth07.case@example.test"
	work := signIn(t, url.Values{
		"custom_email": {email}, "custom_tenant_id": {e2eoidc.MicrosoftDefaultWorkTenantID}, "custom_xms_edov": {"true"},
	})
	workObject := e2eoidc.MicrosoftTestObjectID(e2eoidc.MicrosoftDefaultWorkTenantID, email)
	if work.ProviderSlug != "microsoft" ||
		work.Issuer != "https://login.microsoftonline.com/"+e2eoidc.MicrosoftDefaultWorkTenantID+"/v2.0" ||
		work.Subject != e2eoidc.MicrosoftTestSubject(e2eoidc.MicrosoftDefaultWorkTenantID, workObject) ||
		work.ProviderOrgID != e2eoidc.MicrosoftDefaultWorkTenantID || work.ProviderStableID != workObject ||
		work.Email != email || !work.EmailVerified || work.Nonce != "nonce-value" {
		t.Fatalf("unexpected work account claims: %+v", work)
	}

	personal := signIn(t, url.Values{"custom_email": {email}, "custom_tenant_id": {e2eoidc.MicrosoftPersonalTenantID}})
	if personal.Issuer != e2eoidc.MicrosoftIssuer(e2eoidc.MicrosoftPersonalTenantID) ||
		personal.ProviderOrgID != e2eoidc.MicrosoftPersonalTenantID || personal.Email != email || personal.EmailVerified {
		t.Fatalf("a personal account without xms_edov must not have a trusted email: %+v", personal)
	}
	if personal.Subject == work.Subject || personal.ProviderStableID == work.ProviderStableID {
		t.Fatal("the same email in two tenants must be two different Microsoft identities")
	}

	other := signIn(t, url.Values{"custom_email": {email}, "custom_tenant_id": {microsoftOtherTenant}, "custom_xms_edov": {"true"}})
	if other.Issuer != e2eoidc.MicrosoftIssuer(microsoftOtherTenant) || other.Subject == work.Subject || !other.EmailVerified {
		t.Fatalf("a second work tenant must be a separate trusted identity: %+v", other)
	}

	denied := signIn(t, url.Values{"custom_email": {email}, "custom_tenant_id": {e2eoidc.MicrosoftDefaultWorkTenantID}, "custom_xms_edov": {"false"}})
	if denied.Subject != work.Subject || denied.EmailVerified {
		t.Fatalf("xms_edov=false keeps the identity and drops the trust: %+v", denied)
	}

	withheld := signIn(t, url.Values{
		"custom_tenant_id": {e2eoidc.MicrosoftDefaultWorkTenantID}, "custom_object_id": {microsoftExplicitOID}, "custom_xms_edov": {"true"},
	})
	if withheld.Email != "" || withheld.EmailVerified || withheld.ProviderStableID != microsoftExplicitOID ||
		withheld.Subject != e2eoidc.MicrosoftTestSubject(e2eoidc.MicrosoftDefaultWorkTenantID, microsoftExplicitOID) {
		t.Fatalf("an account without an email must have no trusted email: %+v", withheld)
	}
}

func TestMicrosoftFlavorHonoursTenantModes(t *testing.T) {
	running := startFakeMicrosoft(t)
	exchange := func(t *testing.T, mode string, tenantID string) error {
		t.Helper()
		adapter := newMicrosoftAdapter(t, running, mode)
		authorizeURL := adapter.AuthorizationURL("state-value", "nonce-value", identity.PKCEChallenge(microsoftVerifier))
		if !strings.HasPrefix(authorizeURL, running.authority+"/"+mode+"/oauth2/v2.0/authorize?") {
			t.Fatalf("unexpected %s authorize URL %q", mode, authorizeURL)
		}
		code := microsoftCallbackCode(t, chooseMicrosoftAccount(t, running, authorizeURL, url.Values{
			"custom_email": {"mode@example.test"}, "custom_tenant_id": {tenantID}, "custom_xms_edov": {"true"},
		}))
		_, err := adapter.ExchangeAndVerify(context.Background(), code, microsoftVerifier)
		return err
	}
	// The consumers metadata names the concrete personal-account issuer.
	if err := exchange(t, "consumers", e2eoidc.MicrosoftPersonalTenantID); err != nil {
		t.Fatalf("consumers must accept a personal account: %v", err)
	}
	if err := exchange(t, "consumers", e2eoidc.MicrosoftDefaultWorkTenantID); err == nil {
		t.Fatal("consumers must reject a work account")
	}
	if err := exchange(t, "organizations", e2eoidc.MicrosoftDefaultWorkTenantID); err != nil {
		t.Fatalf("organizations must accept a work account: %v", err)
	}
	if err := exchange(t, "organizations", e2eoidc.MicrosoftPersonalTenantID); err == nil {
		t.Fatal("organizations must reject a personal account")
	}
}

func TestMicrosoftFlavorServesEntraShapedMetadataKeysAndTokens(t *testing.T) {
	running := startFakeMicrosoft(t)
	getJSON := func(t *testing.T, path string) map[string]any {
		t.Helper()
		response, err := running.client.Get(running.authority + path)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		defer response.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return body
	}

	metadata := getJSON(t, "/common/v2.0/.well-known/openid-configuration")
	if metadata["issuer"] != "https://login.microsoftonline.com/{tenantid}/v2.0" ||
		metadata["jwks_uri"] != running.authority+"/common/discovery/v2.0/keys" ||
		metadata["token_endpoint"] != running.authority+"/common/oauth2/v2.0/token" {
		t.Fatalf("unexpected common metadata: %v", metadata)
	}
	if consumers := getJSON(t, "/consumers/v2.0/.well-known/openid-configuration"); consumers["issuer"] != e2eoidc.MicrosoftIssuer(e2eoidc.MicrosoftPersonalTenantID) {
		t.Fatalf("unexpected consumers issuer: %v", consumers["issuer"])
	}

	keys, ok := getJSON(t, "/common/discovery/v2.0/keys")["keys"].([]any)
	if !ok || len(keys) != 1 {
		t.Fatalf("expected one signing key, got %v", keys)
	}
	key := keys[0].(map[string]any)
	if key["use"] != "sig" || key["kid"] == "" || key["issuer"] != "https://login.microsoftonline.com/{tenantid}/v2.0" {
		t.Fatalf("the key must be a signing key restricted to tenant issuers: %v", key)
	}
	if _, hasAlg := key["alg"]; hasAlg {
		t.Fatal("Entra signing keys carry no alg member")
	}
	modulus, _ := base64.RawURLEncoding.DecodeString(key["n"].(string))
	if (&rsa.PublicKey{N: new(big.Int).SetBytes(modulus)}).N.BitLen() < 2048 {
		t.Fatal("the signing key must be at least 2048 bits")
	}

	adapter := newMicrosoftAdapter(t, running, "common")
	authorizeURL := adapter.AuthorizationURL("state-value", "nonce-value", identity.PKCEChallenge(microsoftVerifier))
	redeem := func(secret string, code string) (int, map[string]any) {
		form := url.Values{
			"grant_type": {"authorization_code"}, "client_id": {microsoftClientID}, "client_secret": {secret},
			"code": {code}, "redirect_uri": {microsoftRedirectURL}, "code_verifier": {microsoftVerifier},
		}
		response, err := running.client.PostForm(running.authority+"/common/oauth2/v2.0/token", form)
		if err != nil {
			t.Fatalf("token request: %v", err)
		}
		defer response.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		return response.StatusCode, body
	}
	fields := url.Values{"custom_email": {"tokens@example.test"}, "custom_tenant_id": {microsoftOtherTenant}}
	code := microsoftCallbackCode(t, chooseMicrosoftAccount(t, running, authorizeURL, fields))
	if status, _ := redeem("wrong-secret-value", code); status != http.StatusUnauthorized {
		t.Fatalf("a wrong client secret must be rejected, got %d", status)
	}
	code = microsoftCallbackCode(t, chooseMicrosoftAccount(t, running, authorizeURL, fields))
	status, body := redeem(microsoftClientSecret, code)
	if status != http.StatusOK || body["id_token"] == nil {
		t.Fatalf("valid exchange failed: %d", status)
	}
	if again, _ := redeem(microsoftClientSecret, code); again != http.StatusBadRequest {
		t.Fatalf("a code must be single-use, got %d", again)
	}
	idToken := body["id_token"].(string)
	if header := decodeJWTPart(t, idToken, 0); header["alg"] != "RS256" || header["kid"] != key["kid"] {
		t.Fatalf("the ID token must be RS256 with the published kid: %v", header)
	}
	claims := decodeJWTPart(t, idToken, 1)
	if claims["iss"] != e2eoidc.MicrosoftIssuer(microsoftOtherTenant) || claims["tid"] != microsoftOtherTenant ||
		claims["aud"] != microsoftClientID || claims["ver"] != "2.0" || claims["nbf"] == nil ||
		claims["oid"] != e2eoidc.MicrosoftTestObjectID(microsoftOtherTenant, "tokens@example.test") {
		t.Fatalf("unexpected Microsoft ID token claims: %v", claims)
	}
	for _, absent := range []string{"email_verified", "xms_edov", "auth_time"} {
		if _, present := claims[absent]; present {
			t.Fatalf("the ID token must not carry %s here", absent)
		}
	}
}

func TestMicrosoftCustomAccountsRejectInvalidTenantObjectAndClaims(t *testing.T) {
	running := startFakeMicrosoft(t)
	adapter := newMicrosoftAdapter(t, running, "common")
	authorizeURL := adapter.AuthorizationURL("state-value", "nonce-value", identity.PKCEChallenge(microsoftVerifier))
	for name, fields := range map[string]url.Values{
		"tenant alias":          {"custom_email": {"a@example.test"}, "custom_tenant_id": {"organizations"}},
		"nil tenant":            {"custom_email": {"a@example.test"}, "custom_tenant_id": {"00000000-0000-0000-0000-000000000000"}},
		"braced tenant":         {"custom_email": {"a@example.test"}, "custom_tenant_id": {"{" + microsoftOtherTenant + "}"}},
		"object ID not a GUID":  {"custom_email": {"a@example.test"}, "custom_tenant_id": {microsoftOtherTenant}, "custom_object_id": {"unknown"}},
		"no email or object ID": {"custom_tenant_id": {microsoftOtherTenant}},
		"xms_edov not boolean":  {"custom_email": {"a@example.test"}, "custom_tenant_id": {microsoftOtherTenant}, "custom_xms_edov": {"yes"}},
		"display-name email":    {"custom_email": {"A <a@example.test>"}, "custom_tenant_id": {microsoftOtherTenant}},
	} {
		t.Run(name, func(t *testing.T) {
			if response := chooseMicrosoftAccount(t, running, authorizeURL, fields); response.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", response.StatusCode)
			}
		})
	}
}

func TestMicrosoftTestIdentifiersAreDeterministicAndTenantScoped(t *testing.T) {
	first := e2eoidc.MicrosoftTestObjectID(microsoftOtherTenant, "a@example.test")
	if first != e2eoidc.MicrosoftTestObjectID(microsoftOtherTenant, "a@example.test") ||
		first == e2eoidc.MicrosoftTestObjectID(e2eoidc.MicrosoftPersonalTenantID, "a@example.test") {
		t.Fatal("object IDs must be deterministic per tenant and email")
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(first) {
		t.Fatalf("object ID %q must be a canonical lowercase UUID", first)
	}
	subject := e2eoidc.MicrosoftTestSubject(microsoftOtherTenant, first)
	if len(subject) != 43 || strings.ContainsAny(subject, " +/=") ||
		subject == e2eoidc.MicrosoftTestSubject(e2eoidc.MicrosoftPersonalTenantID, first) {
		t.Fatalf("subject %q must be a tenant-scoped 43-character base64url value", subject)
	}
}

func TestMicrosoftFlavorConfigurationIsValidated(t *testing.T) {
	base := microsoftConfig("127.0.0.1:9094", "http://127.0.0.1:9094")
	if _, err := e2eoidc.New(base); err != nil {
		t.Fatalf("a valid Microsoft configuration was rejected: %v", err)
	}
	for name, mutate := range map[string]func(*e2eoidc.Account){
		"missing tenant":     func(account *e2eoidc.Account) { account.TenantID = "" },
		"tenant alias":       func(account *e2eoidc.Account) { account.TenantID = "common" },
		"invalid object ID":  func(account *e2eoidc.Account) { account.ObjectID = "unknown" },
		"subject whitespace": func(account *e2eoidc.Account) { account.Subject = "sub value" },
	} {
		t.Run(name, func(t *testing.T) {
			config := microsoftConfig("127.0.0.1:9094", "http://127.0.0.1:9094")
			config.Accounts = append([]e2eoidc.Account(nil), config.Accounts...)
			mutate(&config.Accounts[0])
			if _, err := e2eoidc.New(config); err == nil {
				t.Fatal("an invalid Microsoft test account must be rejected")
			}
		})
	}
}

func TestOtherFlavorsDoNotServeMicrosoftEndpoints(t *testing.T) {
	for _, flavor := range []string{"", e2eoidc.FlavorFacebook} {
		subject := "e2e-teacher"
		if flavor == e2eoidc.FlavorFacebook {
			subject = "100000000000001"
		}
		provider, err := e2eoidc.New(e2eoidc.Config{
			Environment: "test", ListenAddress: "127.0.0.1:9091", IssuerURL: "http://127.0.0.1:9091",
			ClientID: "tutorhub-e2e", ClientSecret: "ephemeral-oidc-client-secret",
			RedirectURL: "http://127.0.0.1:8080/api/v1/auth/callback", PostLogoutURL: "http://127.0.0.1:5173/signed-out",
			Accounts: []e2eoidc.Account{{ID: "teacher", Subject: subject, Email: "t@example.test", DisplayName: "T"}},
			Flavor:   flavor,
		})
		if err != nil {
			t.Fatalf("create provider: %v", err)
		}
		server := &http.Server{Handler: provider.Handler(), ReadHeaderTimeout: time.Second}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		go func() { _ = server.Serve(listener) }()
		for _, path := range []string{"/common/v2.0/.well-known/openid-configuration", "/common/oauth2/v2.0/authorize", "/common/discovery/v2.0/keys"} {
			response, err := http.Get("http://" + listener.Addr().String() + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNotFound {
				t.Fatalf("flavor %q: GET %s = %d, want 404 outside the Microsoft flavor", flavor, path, response.StatusCode)
			}
		}
		_ = server.Close()
	}
}
