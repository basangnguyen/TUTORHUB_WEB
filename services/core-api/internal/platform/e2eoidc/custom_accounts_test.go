package e2eoidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// AUTH-05B: the fake Google instance lets each E2E scenario sign in with its
// own email. These tests pin that the form is opt-in, deterministic and
// carries the requested email_verified value.

func startCustomTestProvider(t *testing.T, allowCustom bool) runningProvider {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	issuer := "http://" + listener.Addr().String()
	redirectURL := "http://127.0.0.1:8080/api/v1/auth/oauth/google/callback"
	provider, err := New(Config{
		Environment:         "test",
		ListenAddress:       listener.Addr().String(),
		IssuerURL:           issuer,
		ClientID:            testClientID,
		ClientSecret:        testClientSecret,
		RedirectURL:         redirectURL,
		PostLogoutURL:       "http://127.0.0.1:5173/signed-out",
		Accounts:            testAccounts(),
		AllowCustomAccounts: allowCustom,
	})
	if err != nil {
		listener.Close()
		t.Fatalf("create provider: %v", err)
	}
	server := &http.Server{Handler: provider.Handler(), ReadHeaderTimeout: time.Second}
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

// openChooser starts an authorization request and returns its chooser HTML.
func openChooser(t *testing.T, running runningProvider) string {
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
	if response.StatusCode != http.StatusOK {
		t.Fatalf("chooser status %d", response.StatusCode)
	}
	return string(body)
}

func requestIDFrom(t *testing.T, chooser string) string {
	t.Helper()
	match := regexp.MustCompile(`name="request_id" type="hidden" value="([^"]+)"`).FindStringSubmatch(chooser)
	if len(match) != 2 {
		t.Fatalf("authorization request id missing: %s", chooser)
	}
	return match[1]
}

// completeCustom posts the custom-account form and returns the HTTP status
// and the authorization code (empty unless redirected).
func completeCustom(t *testing.T, running runningProvider, requestID string, fields url.Values) (int, string) {
	t.Helper()
	form := url.Values{"request_id": {requestID}, "account": {CustomAccountID}}
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
	location, _ := url.Parse(response.Header.Get("Location"))
	return response.StatusCode, location.Query().Get("code")
}

func idTokenClaims(t *testing.T, running runningProvider, code string) map[string]any {
	t.Helper()
	token := exchangeCode(t, running.client, running, code, testVerifier)
	parts := strings.Split(token.IDToken, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT segments: %d", len(parts))
	}
	var claims map[string]any
	decodeBase64JSON(t, parts[1], &claims)
	return claims
}

func TestProviderCustomAccountsAreOffByDefault(t *testing.T) {
	running := startCustomTestProvider(t, false)
	chooser := openChooser(t, running)
	if strings.Contains(chooser, "custom_email") || strings.Contains(chooser, CustomAccountID) {
		t.Fatalf("custom-account form rendered without opt-in: %s", chooser)
	}
	status, code := completeCustom(t, running, requestIDFrom(t, chooser), url.Values{
		"custom_email": {"intruder@example.test"},
	})
	if status != http.StatusBadRequest || code != "" {
		t.Fatalf("custom account accepted without opt-in: status=%d", status)
	}
}

func TestProviderIssuesDeterministicCustomAccounts(t *testing.T) {
	running := startCustomTestProvider(t, true)
	chooser := openChooser(t, running)
	if !strings.Contains(chooser, `name="custom_email"`) ||
		!strings.Contains(chooser, "Sign in with custom account") ||
		!strings.Contains(chooser, "Sign in as E2E Administrator") {
		t.Fatalf("chooser must keep fixed accounts and add the custom form: %s", chooser)
	}

	status, code := completeCustom(t, running, requestIDFrom(t, chooser), url.Values{
		"custom_email": {"  Google.New@Example.TEST "},
	})
	if status != http.StatusSeeOther || code == "" {
		t.Fatalf("custom account was not authorized: status=%d", status)
	}
	first := idTokenClaims(t, running, code)
	subject, _ := first["sub"].(string)
	if first["email"] != "google.new@example.test" || first["email_verified"] != true ||
		!regexp.MustCompile(`^custom-[0-9a-f]{40}$`).MatchString(subject) ||
		first["nonce"] != "nonce-value" || first["aud"] != testClientID {
		t.Fatalf("unexpected custom ID token claims: %#v", first)
	}

	// The same email maps to the same subject; email_verified follows the form.
	status, code = completeCustom(t, running, requestIDFrom(t, openChooser(t, running)), url.Values{
		"custom_email":            {"google.new@example.test"},
		"custom_email_unverified": {"true"},
	})
	if status != http.StatusSeeOther || code == "" {
		t.Fatalf("second custom authorization failed: status=%d", status)
	}
	token := exchangeCode(t, running.client, running, code, testVerifier)
	parts := strings.Split(token.IDToken, ".")
	var second map[string]any
	decodeBase64JSON(t, parts[1], &second)
	if second["sub"] != subject || second["email_verified"] != false {
		t.Fatalf("custom subject must be stable and unverified on request: %#v", second)
	}
	userInfoRequest, _ := http.NewRequest(http.MethodGet, running.issuer+"/userinfo", nil)
	userInfoRequest.Header.Set("Authorization", "Bearer "+token.AccessToken)
	userInfoResponse, err := running.client.Do(userInfoRequest)
	if err != nil {
		t.Fatalf("request user info: %v", err)
	}
	defer userInfoResponse.Body.Close()
	var userInfo map[string]any
	decodeJSON(t, userInfoResponse.Body, &userInfo)
	if userInfo["sub"] != subject || userInfo["email_verified"] != false ||
		userInfo["name"] != "google.new" {
		t.Fatalf("unexpected custom user info: %#v", userInfo)
	}
}

func TestProviderRejectsInvalidCustomEmails(t *testing.T) {
	running := startCustomTestProvider(t, true)
	for name, email := range map[string]string{
		"empty":        "",
		"no at":        "not-an-email",
		"display name": "Eve <eve@example.test>",
		"newline":      "eve@example.test\nbcc@example.test",
		"too long":     strings.Repeat("a", 250) + "@example.test",
	} {
		t.Run(name, func(t *testing.T) {
			status, code := completeCustom(t, running, requestIDFrom(t, openChooser(t, running)), url.Values{
				"custom_email": {email},
			})
			if status != http.StatusBadRequest || code != "" {
				t.Fatalf("invalid custom email %q accepted: status=%d", email, status)
			}
		})
	}
}

func TestProviderReservesTheCustomAccountID(t *testing.T) {
	accounts := append(testAccounts(), Account{
		ID:          CustomAccountID,
		Subject:     "e2e-custom",
		Email:       "custom.e2e@tutorhub.local",
		DisplayName: "E2E Custom",
	})
	_, err := New(Config{
		Environment:   "test",
		ListenAddress: "127.0.0.1:9092",
		IssuerURL:     "http://127.0.0.1:9092",
		ClientID:      testClientID,
		ClientSecret:  testClientSecret,
		RedirectURL:   "http://127.0.0.1:8080/api/v1/auth/oauth/google/callback",
		PostLogoutURL: "http://127.0.0.1:5173/signed-out",
		Accounts:      accounts,
	})
	if err == nil {
		t.Fatal("a configured account may not use the reserved custom account id")
	}
}
