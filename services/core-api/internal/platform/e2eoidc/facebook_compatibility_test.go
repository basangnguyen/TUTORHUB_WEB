package e2eoidc_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
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
	facebookClientID    = "tutorhub-e2e-facebook"
	facebookRedirectURL = "http://127.0.0.1:8080/api/v1/auth/oauth/facebook/callback"
	facebookVerifier    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
)

type runningFacebook struct {
	issuer string
	client *http.Client
}

func startFakeFacebook(t *testing.T) runningFacebook {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	issuerURL := "http://" + listener.Addr().String()
	fake, err := e2eoidc.New(e2eoidc.Config{
		Environment:   "test",
		ListenAddress: listener.Addr().String(),
		IssuerURL:     issuerURL,
		ClientID:      facebookClientID,
		ClientSecret:  "ephemeral-facebook-app-secret",
		RedirectURL:   facebookRedirectURL,
		PostLogoutURL: "http://127.0.0.1:5173/signed-out",
		Accounts: []e2eoidc.Account{{
			ID: "facebook-teacher", Subject: "100000000000001",
			Email: "teacher.facebook.e2e@tutorhub.local", DisplayName: "E2E Facebook Teacher", Locale: "en",
		}},
		AllowCustomAccounts: true,
		Flavor:              e2eoidc.FlavorFacebook,
	})
	if err != nil {
		listener.Close()
		t.Fatalf("create fake Facebook: %v", err)
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
	return runningFacebook{
		issuer: issuerURL,
		client: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

// chooseCustomAccount opens the dialog URL, submits the custom-account form
// and returns the callback query the fake redirected to.
func chooseCustomAccount(t *testing.T, running runningFacebook, dialogURL string, fields url.Values) url.Values {
	t.Helper()
	chooser, err := running.client.Get(dialogURL)
	if err != nil {
		t.Fatalf("open chooser: %v", err)
	}
	body, _ := io.ReadAll(chooser.Body)
	chooser.Body.Close()
	requestID := regexp.MustCompile(`name="request_id" type="hidden" value="([^"]+)"`).FindSubmatch(body)
	if chooser.StatusCode != http.StatusOK || len(requestID) != 2 {
		t.Fatalf("unexpected chooser: status=%d", chooser.StatusCode)
	}
	if !strings.Contains(string(body), `name="custom_email_withheld"`) ||
		strings.Contains(string(body), `name="custom_email_unverified"`) {
		t.Fatal("the Facebook chooser must offer withholding the email instead of an unverified flag")
	}
	form := url.Values{"request_id": {string(requestID[1])}, "account": {e2eoidc.CustomAccountID}}
	for key, values := range fields {
		form[key] = values
	}
	request, _ := http.NewRequest(http.MethodPost, running.issuer+"/authorize/complete", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatalf("complete authorization: %v", err)
	}
	response.Body.Close()
	callback, err := url.Parse(response.Header.Get("Location"))
	if err != nil || callback.Query().Get("state") != "state-value" {
		t.Fatalf("invalid callback redirect: %q", response.Header.Get("Location"))
	}
	return callback.Query()
}

// AUTH-06B: the third fake instance stands in for Facebook in Playwright. Pin
// that TutorHub's real Facebook adapter (fixed Graph v27.0 endpoints, GET code
// exchange with PKCE, numeric ASID, single audience) accepts it.
func TestFacebookFlavorIsCompatibleWithTutorHubFacebookAdapter(t *testing.T) {
	running := startFakeFacebook(t)
	adapter, err := identity.NewFacebookProvider(context.Background(), identity.OIDCProviderConfig{
		IssuerURL:    running.issuer,
		ClientID:     facebookClientID,
		ClientSecret: "ephemeral-facebook-app-secret",
		CallbackURL:  facebookRedirectURL,
		HTTPTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("configure Facebook adapter: %v", err)
	}
	dialogURL := adapter.AuthorizationURL("state-value", "nonce-value", identity.PKCEChallenge(facebookVerifier))
	if !strings.HasPrefix(dialogURL, running.issuer+"/v27.0/dialog/oauth?") {
		t.Fatalf("adapter must use Meta's dialog path, got %q", dialogURL)
	}

	signIn := func(t *testing.T, fields url.Values) identity.ProviderClaims {
		t.Helper()
		callback := chooseCustomAccount(t, running, dialogURL, fields)
		claims, err := adapter.ExchangeAndVerify(context.Background(), callback.Get("code"), facebookVerifier)
		if err != nil {
			t.Fatalf("exchange through the Facebook adapter: %v", err)
		}
		return claims
	}

	const seed = "auth06.case@example.test"
	shared := signIn(t, url.Values{"custom_email": {seed}})
	if shared.ProviderSlug != "facebook" || shared.Issuer != running.issuer ||
		shared.Subject != e2eoidc.FacebookTestASID(seed) || shared.Email != seed ||
		shared.EmailVerified || shared.Nonce != "nonce-value" {
		t.Fatalf("unexpected Facebook claims: %+v", shared)
	}

	withheld := signIn(t, url.Values{"custom_email": {seed}, "custom_email_withheld": {"true"}})
	if withheld.Subject != shared.Subject || withheld.Email != "" {
		t.Fatalf("a withheld email must keep the ASID and drop the email claim: %+v", withheld)
	}
}

func TestFacebookTestASIDIsDeterministicAndNumeric(t *testing.T) {
	// Same vectors as e2e/auth-06-facebook.spec.ts (facebookTestASID).
	for seed, want := range map[string]string{
		"auth06.case@example.test": "198523828897236998",
		"a@b.test":                 "176951057881983527",
	} {
		if got := e2eoidc.FacebookTestASID(seed); got != want {
			t.Fatalf("FacebookTestASID(%q) = %q, want %q", seed, got, want)
		}
	}
}

func TestFacebookAccessTokenRejectsWrongClientVerifierAndReplay(t *testing.T) {
	running := startFakeFacebook(t)
	adapter, err := identity.NewFacebookProvider(context.Background(), identity.OIDCProviderConfig{
		IssuerURL: running.issuer, ClientID: facebookClientID, ClientSecret: "ephemeral-facebook-app-secret",
		CallbackURL: facebookRedirectURL, HTTPTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("configure Facebook adapter: %v", err)
	}
	dialogURL := adapter.AuthorizationURL("state-value", "nonce-value", identity.PKCEChallenge(facebookVerifier))
	exchange := func(clientID, code, verifier string) (int, map[string]any) {
		query := url.Values{"client_id": {clientID}, "redirect_uri": {facebookRedirectURL}, "code": {code}, "code_verifier": {verifier}}
		response, err := running.client.Get(running.issuer + "/v27.0/oauth/access_token?" + query.Encode())
		if err != nil {
			t.Fatalf("token request: %v", err)
		}
		defer response.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(response.Body).Decode(&body)
		return response.StatusCode, body
	}

	code := chooseCustomAccount(t, running, dialogURL, url.Values{"custom_email": {"a@b.test"}}).Get("code")
	if status, _ := exchange("another-client", code, facebookVerifier); status != http.StatusBadRequest {
		t.Fatalf("wrong client must be rejected, got %d", status)
	}
	// The wrong-client attempt does not consume the code; a wrong verifier does.
	if status, _ := exchange(facebookClientID, code, strings.Repeat("x", 43)); status != http.StatusBadRequest {
		t.Fatalf("wrong verifier must be rejected, got %d", status)
	}
	if status, _ := exchange(facebookClientID, code, facebookVerifier); status != http.StatusBadRequest {
		t.Fatalf("a code must be single-use, got %d", status)
	}

	code = chooseCustomAccount(t, running, dialogURL, url.Values{"custom_email": {"a@b.test"}}).Get("code")
	status, body := exchange(facebookClientID, code, facebookVerifier)
	if status != http.StatusOK || body["id_token"] == nil {
		t.Fatalf("valid exchange failed: %d", status)
	}
	if _, hasScope := body["scope"]; hasScope {
		t.Fatal("the Facebook token response mirrors Meta and carries no scope")
	}
	parts := strings.Split(body["id_token"].(string), ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if strings.Contains(string(payload), "email_verified") {
		t.Fatal("Facebook ID tokens must not carry email_verified")
	}

	jwks, err := running.client.Get(running.issuer + "/.well-known/oauth/openid/jwks/")
	if err != nil || jwks.StatusCode != http.StatusOK {
		t.Fatalf("Meta JWKS path must be served: %v", err)
	}
	jwks.Body.Close()
}

func TestFacebookFlavorConfigurationIsValidated(t *testing.T) {
	base := e2eoidc.Config{
		Environment: "test", ListenAddress: "127.0.0.1:9093", IssuerURL: "http://127.0.0.1:9093",
		ClientID: facebookClientID, ClientSecret: "ephemeral-facebook-app-secret",
		RedirectURL: facebookRedirectURL, PostLogoutURL: "http://127.0.0.1:5173/signed-out",
		Accounts: []e2eoidc.Account{{ID: "teacher", Subject: "e2e-teacher", Email: "t@example.test", DisplayName: "T"}},
	}

	facebook := base
	facebook.Flavor = e2eoidc.FlavorFacebook
	if _, err := e2eoidc.New(facebook); err == nil {
		t.Fatal("non-numeric subjects must be rejected for the Facebook flavor")
	}
	unknown := base
	unknown.Flavor = "apple"
	if _, err := e2eoidc.New(unknown); err == nil {
		t.Fatal("unknown flavors must be rejected")
	}
}

func TestDefaultFlavorDoesNotServeMetaEndpoints(t *testing.T) {
	provider, err := e2eoidc.New(e2eoidc.Config{
		Environment: "test", ListenAddress: "127.0.0.1:9091", IssuerURL: "http://127.0.0.1:9091",
		ClientID: "tutorhub-e2e", ClientSecret: "ephemeral-oidc-client-secret",
		RedirectURL: "http://127.0.0.1:8080/api/v1/auth/callback", PostLogoutURL: "http://127.0.0.1:5173/signed-out",
		Accounts: []e2eoidc.Account{{ID: "teacher", Subject: "e2e-teacher", Email: "t@example.test", DisplayName: "T"}},
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
	t.Cleanup(func() { _ = server.Close() })
	for _, path := range []string{"/v27.0/dialog/oauth", "/v27.0/oauth/access_token", "/.well-known/oauth/openid/jwks/"} {
		response, err := http.Get("http://" + listener.Addr().String() + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s = %d, want 404 outside the Facebook flavor", path, response.StatusCode)
		}
	}
}
