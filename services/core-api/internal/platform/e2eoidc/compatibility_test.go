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

func TestProviderIsCompatibleWithTutorHubOIDCAdapter(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	issuerURL := "http://" + listener.Addr().String()
	redirectURL := "http://127.0.0.1:8080/api/v1/auth/callback"
	postLogoutURL := "http://127.0.0.1:5173/signed-out"
	fake, err := e2eoidc.New(e2eoidc.Config{
		Environment:   "test",
		ListenAddress: listener.Addr().String(),
		IssuerURL:     issuerURL,
		ClientID:      "tutorhub-e2e",
		ClientSecret:  "ephemeral-test-client-secret",
		RedirectURL:   redirectURL,
		PostLogoutURL: postLogoutURL,
		Accounts: []e2eoidc.Account{{
			ID:          "teacher",
			Subject:     "e2e-teacher",
			Email:       "teacher.e2e@tutorhub.local",
			DisplayName: "E2E Teacher",
			Locale:      "en",
		}},
	})
	if err != nil {
		listener.Close()
		t.Fatalf("create fake provider: %v", err)
	}
	server := &http.Server{
		Handler:           fake.Handler(),
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

	adapter, err := identity.NewOIDCProvider(
		context.Background(),
		identity.OIDCProviderConfig{
			IssuerURL:     issuerURL,
			ClientID:      "tutorhub-e2e",
			ClientSecret:  "ephemeral-test-client-secret",
			CallbackURL:   redirectURL,
			PostLogoutURL: postLogoutURL,
			Scopes:        []string{"openid", "profile", "email"},
			HTTPTimeout:   2 * time.Second,
		},
	)
	if err != nil {
		t.Fatalf("discover fake provider: %v", err)
	}

	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~"
	authorizationURL := adapter.AuthorizationURL(
		"state-value",
		"nonce-value",
		identity.PKCEChallenge(verifier),
	)
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	chooserResponse, err := client.Get(authorizationURL)
	if err != nil {
		t.Fatalf("open account chooser: %v", err)
	}
	chooserBody, err := io.ReadAll(chooserResponse.Body)
	chooserResponse.Body.Close()
	if err != nil {
		t.Fatalf("read account chooser: %v", err)
	}
	requestID := regexp.MustCompile(
		`name="request_id" type="hidden" value="([^"]+)"`,
	).FindSubmatch(chooserBody)
	if chooserResponse.StatusCode != http.StatusOK || len(requestID) != 2 {
		t.Fatalf(
			"unexpected account chooser: status=%d body=%s",
			chooserResponse.StatusCode,
			chooserBody,
		)
	}

	form := url.Values{
		"request_id": {string(requestID[1])},
		"account":    {"teacher"},
	}
	completeRequest, _ := http.NewRequest(
		http.MethodPost,
		issuerURL+"/authorize/complete",
		strings.NewReader(form.Encode()),
	)
	completeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	completeResponse, err := client.Do(completeRequest)
	if err != nil {
		t.Fatalf("complete authorization: %v", err)
	}
	completeResponse.Body.Close()
	callback, err := url.Parse(completeResponse.Header.Get("Location"))
	if err != nil || callback.Query().Get("state") != "state-value" {
		t.Fatalf("invalid callback redirect: %q", completeResponse.Header.Get("Location"))
	}

	claims, err := adapter.ExchangeAndVerify(
		context.Background(),
		callback.Query().Get("code"),
		verifier,
	)
	if err != nil {
		t.Fatalf("exchange through TutorHub adapter: %v", err)
	}
	if claims.Issuer != issuerURL ||
		claims.Subject != "e2e-teacher" ||
		claims.Email != "teacher.e2e@tutorhub.local" ||
		claims.DisplayName != "E2E Teacher" ||
		claims.Nonce != "nonce-value" ||
		!claims.EmailVerified {
		t.Fatalf("unexpected verified claims: %+v", claims)
	}
	if !strings.HasPrefix(adapter.EndSessionURL(), issuerURL+"/logout?") {
		t.Fatalf("unexpected end-session URL: %q", adapter.EndSessionURL())
	}
}
