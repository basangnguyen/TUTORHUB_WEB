package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type facebookHTTPFixture struct {
	fakeIdentityService
	calls int
}

const receiptFixtureCode = "3510abefd09876543210cdfe98760123"

func (s *facebookHTTPFixture) FacebookDelete(_ context.Context, signed, secret, issuer string) (string, error) {
	s.calls++
	if signed != "private-event" || secret != "test-signing-key" {
		return "", identity.ErrFacebookSignedRequest
	}
	return receiptFixtureCode, nil
}
func (s *facebookHTTPFixture) FacebookDeauthorize(ctx context.Context, signed, secret, issuer string) error {
	_, err := s.FacebookDelete(ctx, signed, secret, issuer)
	return err
}
func (s *facebookHTTPFixture) FacebookDeletionStatus(_ context.Context, code string) (string, error) {
	if code != receiptFixtureCode {
		return "", identity.ErrDeletionRequestNotFound
	}
	return "completed", nil
}

func TestFacebookServerCallbackHTTP(t *testing.T) {
	cfg := authFoundationConfig()
	cfg.Authentication.ProviderFacebookEnabled = true
	cfg.Authentication.FacebookClientID = "fixture"
	cfg.Authentication.FacebookClientSecret = "test-signing-key"
	service := &facebookHTTPFixture{}
	var logs bytes.Buffer
	handler := NewHandlerWithOptions(cfg, slog.New(slog.NewTextHandler(&logs, nil)), Options{Identity: service, Clock: func() time.Time { return fixedTime }})
	const prefix = "/api/v1/auth/oauth/facebook/"
	post := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, prefix+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"data-deletion", "deauthorize"} {
		w := post(path, "signed_request=private-event")
		if path == "deauthorize" {
			if w.Code != 204 {
				t.Fatalf("deauthorize status %d", w.Code)
			}
		} else {
			var receipt facebookDeletionResponse
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.ConfirmationCode != receiptFixtureCode || receipt.URL != strings.TrimRight(cfg.WebOrigin, "/")+"/legal/data-deletion?code="+receiptFixtureCode {
				t.Fatal("invalid Meta receipt")
			}
		}
		for _, form := range []string{"", "signed_request=private-wrong", "signed_request=a&signed_request=b", "signed_request=private-event&extra=1", "signed_request=" + strings.Repeat("z", 65536)} {
			w := post(path, form)
			if w.Code != 400 || !strings.Contains(w.Body.String(), "auth_invalid_signed_request") || strings.Contains(w.Body.String(), "private-wrong") {
				t.Fatalf("invalid signed form: status %d", w.Code)
			}
		}
	}
	for _, c := range []string{receiptFixtureCode, "missing"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", prefix+"data-deletion/"+url.PathEscape(c), nil))
		if c == "missing" && w.Code != 404 {
			t.Fatal("unknown receipt exposed")
		}
		if c == receiptFixtureCode && (w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"status":"completed"}`) {
			t.Fatalf("status response %d %s", w.Code, w.Body.String())
		}
	}
	for _, s := range []string{"private-event", "private-wrong", receiptFixtureCode, "test-signing-key"} {
		if strings.Contains(logs.String(), s) {
			t.Fatal("callback or receipt in log")
		}
	}
}

func TestFacebookPrivacyCallbacksWhileLoginDisabled(t *testing.T) {
	cfg := authFoundationConfig()
	cfg.Authentication.ProviderFacebookEnabled = false
	cfg.Authentication.FacebookClientID = ""
	for _, secret := range []string{"", "test-signing-key"} {
		cfg.Authentication.FacebookClientSecret = secret
		handler := NewHandlerWithOptions(cfg, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), Options{Identity: &facebookHTTPFixture{}})
		const path = "/api/v1/auth/oauth/facebook/data-deletion"
		status := httptest.NewRecorder()
		handler.ServeHTTP(status, httptest.NewRequest("GET", path+"/"+receiptFixtureCode, nil))
		if status.Code != http.StatusOK {
			t.Fatalf("disabled login hid existing deletion receipt: %d", status.Code)
		}
		form := httptest.NewRequest("POST", path, strings.NewReader("signed_request=private-event"))
		form.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, form)
		if secret == "" && response.Code != http.StatusNotFound || secret != "" && response.Code != http.StatusOK {
			t.Fatalf("signed callback with disabled login: %d", response.Code)
		}
	}
}
