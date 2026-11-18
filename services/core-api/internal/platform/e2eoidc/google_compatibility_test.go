package e2eoidc_test

import (
	"context"
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

// AUTH-05B: the second fake instance stands in for Google in Playwright. Pin
// that TutorHub's real Google adapter (ID-token claims only, custom issuer
// allowed in test) accepts it, including the custom per-scenario accounts.
func TestCustomAccountsAreCompatibleWithTutorHubGoogleAdapter(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	issuerURL := "http://" + listener.Addr().String()
	redirectURL := "http://127.0.0.1:8080/api/v1/auth/oauth/google/callback"
	fake, err := e2eoidc.New(e2eoidc.Config{
		Environment:   "test",
		ListenAddress: listener.Addr().String(),
		IssuerURL:     issuerURL,
		ClientID:      "tutorhub-e2e-google",
		ClientSecret:  "ephemeral-google-client-secret",
		RedirectURL:   redirectURL,
		PostLogoutURL: "http://127.0.0.1:5173/signed-out",
		Accounts: []e2eoidc.Account{{
			ID: "teacher", Subject: "e2e-teacher", Email: "teacher.e2e@tutorhub.local",
			DisplayName: "E2E Teacher", Locale: "en",
		}},
		AllowCustomAccounts: true,
	})
	if err != nil {
		listener.Close()
		t.Fatalf("create fake provider: %v", err)
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

	adapter, err := identity.NewGoogleProvider(context.Background(), identity.OIDCProviderConfig{
		IssuerURL:    issuerURL,
		ClientID:     "tutorhub-e2e-google",
		ClientSecret: "ephemeral-google-client-secret",
		CallbackURL:  redirectURL,
		HTTPTimeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("discover fake Google: %v", err)
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"

	signIn := func(t *testing.T, fields url.Values) identity.ProviderClaims {
		t.Helper()
		chooser, err := client.Get(adapter.AuthorizationURL("state-value", "nonce-value", identity.PKCEChallenge(verifier)))
		if err != nil {
			t.Fatalf("open chooser: %v", err)
		}
		body, _ := io.ReadAll(chooser.Body)
		chooser.Body.Close()
		requestID := regexp.MustCompile(`name="request_id" type="hidden" value="([^"]+)"`).FindSubmatch(body)
		if chooser.StatusCode != http.StatusOK || len(requestID) != 2 {
			t.Fatalf("unexpected chooser: status=%d", chooser.StatusCode)
		}
		form := url.Values{"request_id": {string(requestID[1])}, "account": {e2eoidc.CustomAccountID}}
		for key, values := range fields {
			form[key] = values
		}
		request, _ := http.NewRequest(http.MethodPost, issuerURL+"/authorize/complete", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("complete authorization: %v", err)
		}
		response.Body.Close()
		callback, err := url.Parse(response.Header.Get("Location"))
		if err != nil || callback.Query().Get("state") != "state-value" {
			t.Fatalf("invalid callback redirect: %q", response.Header.Get("Location"))
		}
		claims, err := adapter.ExchangeAndVerify(context.Background(), callback.Query().Get("code"), verifier)
		if err != nil {
			t.Fatalf("exchange through the Google adapter: %v", err)
		}
		return claims
	}

	verified := signIn(t, url.Values{"custom_email": {"auth05.case@example.test"}})
	if verified.ProviderSlug != "google" || verified.Issuer != issuerURL ||
		!strings.HasPrefix(verified.Subject, "custom-") ||
		verified.Email != "auth05.case@example.test" || !verified.EmailVerified ||
		verified.Nonce != "nonce-value" || verified.AuthTime.IsZero() {
		t.Fatalf("unexpected Google claims: %+v", verified)
	}

	unverified := signIn(t, url.Values{
		"custom_email":            {"auth05.case@example.test"},
		"custom_email_unverified": {"true"},
	})
	if unverified.Subject != verified.Subject || unverified.EmailVerified {
		t.Fatalf("unverified custom account must keep its subject: %+v", unverified)
	}
}
