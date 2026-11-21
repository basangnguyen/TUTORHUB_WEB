package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
)

const (
	resendTestRecipient = "learner@example.test"
	resendTestCode      = "025913"
	resendTestKey       = "resend-fixture-key"
	resendTestReference = "resend-fixture-reference"
)

func resendTestConfig(endpoint string) config.ResendConfig {
	return config.ResendConfig{APIURL: endpoint, APIKey: resendTestKey, FromEmail: "codes@example.test", FromName: "TutorHub, Lớp học"}
}

func resendTestServer(t *testing.T, handler http.HandlerFunc) *ResendEmailSender {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	sender, err := NewResendEmailSender("test", resendTestConfig(server.URL+"/emails"))
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

func resendTestChallenge() ChallengeMessage {
	return ChallengeMessage{ChallengeID: uuid.New(), Purpose: "signup_email", Channel: DeliveryEmail,
		Destination: resendTestRecipient, Code: resendTestCode, ExpiresAt: time.Now().Add(5 * time.Minute)}
}

func readResendRequest(t *testing.T, r *http.Request) resendPayload {
	t.Helper()
	if r.Method != http.MethodPost || r.URL.Path != "/emails" || r.Header.Get("Authorization") != "Bearer "+resendTestKey ||
		r.Header.Get("Content-Type") != "application/json" {
		t.Error("unexpected Resend request method, endpoint or headers")
	}
	var p resendPayload
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || len(p.To) != 1 || p.To[0] != resendTestRecipient ||
		p.Subject == "" || p.Text == "" || p.HTML == "" {
		t.Error("invalid Resend JSON body")
	}
	cfg := resendTestConfig("")
	from, err := mail.ParseAddress(p.From)
	if err != nil || from.Address != cfg.FromEmail || from.Name != cfg.FromName {
		t.Error("incorrect sender address or encoded display name")
	}
	if strings.Contains(p.Subject, resendTestCode) || strings.Contains(p.HTML, "<img") || strings.Contains(p.HTML, "href=") ||
		strings.Contains(p.HTML, "src=") || !strings.Contains(p.HTML, "<p>") {
		t.Error("unexpected sensitive subject or active HTML")
	}
	return p
}

func TestResendChallengeWireContract(t *testing.T) {
	for _, tc := range []struct{ purpose, locale, action, warning string }{
		{"signup_email", "vi-VN", "xác minh địa chỉ email", "Không chia sẻ"},
		{"login_code", "en-US", "sign in", "Do not share"},
		{"password_recovery", "VI", "đặt lại mật khẩu", "Không chia sẻ"},
		{"link_identity", "en", "link a sign-in method", "Do not share"},
		{"reauth", "", "confirm your identity again", "Không chia sẻ"},
		{"mfa", "vi", "xác minh bước hai", "Không chia sẻ"},
	} {
		t.Run(tc.purpose, func(t *testing.T) {
			msg := resendTestChallenge()
			msg.Purpose, msg.Locale = tc.purpose, tc.locale
			sender := resendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				p := readResendRequest(t, r)
				if r.Header.Get("Idempotency-Key") != "auth-challenge:"+msg.ChallengeID.String() ||
					!strings.Contains(p.Subject, tc.action) {
					t.Error("incorrect idempotency key or localized purpose")
				}
				for _, body := range []string{p.Text, p.HTML} {
					for _, want := range []string{resendTestCode, tc.action, tc.warning, msg.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC")} {
						if !strings.Contains(body, want) {
							t.Error("message is missing code, purpose, expiry or warning")
						}
					}
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"id":"`+resendTestReference+`"}`)
			})
			before := time.Now()
			receipt, err := sender.SendChallenge(t.Context(), msg)
			if err != nil || receipt.ProviderMessageRef != resendTestReference || receipt.Provider != "resend" || receipt.Channel != DeliveryEmail ||
				receipt.AcceptedAt.Location() != time.UTC || receipt.AcceptedAt.Before(before) || receipt.AcceptedAt.After(time.Now()) {
				t.Fatal("invalid acceptance receipt")
			}
		})
	}
}

func TestResendNoticeWireContract(t *testing.T) {
	for _, tc := range []struct{ kind, subject string }{
		{"account_exists", "Existing TutorHub account"}, {"password_changed", "password changed"}, {"phone_released", "phone sign-in removed"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			keys := make(chan string, 4)
			sender := resendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				p := readResendRequest(t, r)
				if !strings.Contains(p.Subject, tc.subject) || strings.Contains(p.Text+p.HTML, resendTestCode) {
					t.Error("invalid account notice content")
				}
				keys <- r.Header.Get("Idempotency-Key")
				_, _ = io.WriteString(w, `{"id":"accepted-notice"}`)
			})
			n := AccountNotice{Kind: tc.kind, Destination: resendTestRecipient, ExpiresAt: time.Now().Add(time.Minute)}
			var independent string
			id := uuid.New()
			for _, challengeID := range []uuid.UUID{uuid.Nil, uuid.Nil, id, id} {
				n.ChallengeID = challengeID
				r, err := sender.SendNotice(t.Context(), n)
				if err != nil || r.ProviderMessageRef != "accepted-notice" {
					t.Fatal("account notice was not accepted")
				}
				key, ok := strings.CutPrefix(<-keys, "auth-notice:"+tc.kind+":")
				if !ok || uuid.Validate(key) != nil || key == uuid.Nil.String() {
					t.Fatal("invalid notice idempotency key")
				}
				if challengeID == uuid.Nil {
					if key == independent {
						t.Fatal("independent notices reused a key")
					}
					independent = key
				} else if key != challengeID.String() {
					t.Fatal("challenge notice lost its stable key")
				}
			}
		})
	}
}

func TestResendRejectsInvalidMessagesBeforeSending(t *testing.T) {
	s := resendTestServer(t, func(http.ResponseWriter, *http.Request) { t.Error("received an invalid message") })
	for _, change := range []func(*ChallengeMessage){
		func(m *ChallengeMessage) { m.ChallengeID = uuid.Nil },
		func(m *ChallengeMessage) { m.Channel = DeliverySMS },
		func(m *ChallengeMessage) { m.Code = "<html>" },
		func(m *ChallengeMessage) { m.Purpose = "unregistered" },
		func(m *ChallengeMessage) { m.ExpiresAt = time.Now() },
		func(m *ChallengeMessage) { m.Destination = "Learner <learner@example.test>" },
		func(m *ChallengeMessage) { m.Destination += "\r\n" },
	} {
		msg := resendTestChallenge()
		change(&msg)
		if _, err := s.SendChallenge(t.Context(), msg); err != ErrAuthDeliveryFailed {
			t.Fatal("invalid challenge was not rejected")
		}
	}
	validNotice := AccountNotice{Kind: "account_exists", Destination: resendTestRecipient, ExpiresAt: time.Now().Add(time.Minute)}
	for _, change := range []func(*AccountNotice){
		func(n *AccountNotice) { n.Kind = "unknown-notice" },
		func(n *AccountNotice) { n.ExpiresAt = time.Now() },
		func(n *AccountNotice) { n.Destination += "\r\n" },
	} {
		n := validNotice
		change(&n)
		if _, err := s.SendNotice(t.Context(), n); err != ErrAuthDeliveryFailed {
			t.Fatal("invalid notice was not rejected")
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.SendChallenge(ctx, resendTestChallenge()); err != ErrAuthDeliveryFailed {
		t.Fatal("cancelled challenge was not rejected")
	}
	if _, err := s.SendNotice(ctx, validNotice); err != ErrAuthDeliveryFailed {
		t.Fatal("cancelled notice was not rejected")
	}
}

func TestResendAcceptanceRequiresValidBoundedReference(t *testing.T) {
	maxID := strings.Repeat("a", 128)
	bounded := `{"id":"ok"}`
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		chunked bool
		wantID  string
	}{
		{"4xx", 429, `{"id":"private-provider-body"}`, false, ""},
		{"5xx", 503, "private-provider-body", false, ""},
		{"empty response", 204, "", false, ""}, {"null body", 200, "null", false, ""},
		{"array", 200, `[{"id":"ok"}]`, false, ""}, {"missing", 200, "{}", false, ""},
		{"wrong casing", 200, `{"ID":"value"}`, false, ""}, {"empty id", 200, `{"id":""}`, false, ""},
		{"numeric", 200, `{"id":123}`, false, ""}, {"null id", 200, `{"id":null}`, false, ""},
		{"space", 200, `{"id":"a b"}`, false, ""}, {"control", 200, `{"id":"a\nb"}`, false, ""},
		{"non-ASCII", 200, `{"id":"thử"}`, false, ""}, {"trailing data", 200, bounded + " trailing", false, ""},
		{"long", 200, `{"id":"` + maxID + `b"}`, false, ""}, {"128 bytes", 200, `{"id":"` + maxID + `"}`, false, maxID},
		{"chunked limit", 202, bounded + strings.Repeat(" ", phoneResponseLimit-len(bounded)), true, "ok"},
		{"chunked excess", 200, bounded + strings.Repeat(" ", phoneResponseLimit+1-len(bounded)), true, ""},
		{"known excess", 200, bounded + strings.Repeat(" ", phoneResponseLimit+1-len(bounded)), false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := resendTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				if !tc.chunked {
					w.Header().Set("Content-Length", fmt.Sprint(len(tc.body)))
				}
				w.WriteHeader(tc.status)
				if tc.chunked {
					w.(http.Flusher).Flush()
				}
				_, _ = io.WriteString(w, tc.body)
			})
			r, err := s.SendChallenge(t.Context(), resendTestChallenge())
			if tc.wantID == "" {
				if err != ErrAuthDeliveryFailed || r.ProviderMessageRef != "" {
					t.Fatal("invalid response did not produce fixed error")
				}
			} else if err != nil || r.ProviderMessageRef != tc.wantID {
				t.Fatal("valid reference was not retained")
			}
		})
	}
}

type resendTestTransport func(*http.Request) (*http.Response, error)

func (f resendTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestResendTransportAndRedaction(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	s := resendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/private-response", http.StatusTemporaryRedirect)
	})
	transport := s.client.Transport.(*http.Transport)
	s.client.Transport = resendTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.GetBody != nil {
			t.Error("paid request body can be replayed")
		}
		return transport.RoundTrip(r)
	})
	msg := resendTestChallenge()
	if _, err := s.SendChallenge(t.Context(), msg); err != ErrAuthDeliveryFailed || forwarded.Load() != 0 {
		t.Fatal("redirect was followed or its error escaped")
	}
	if s.client.Timeout != 8*time.Second || !transport.DisableKeepAlives || transport.Proxy != nil {
		t.Fatal("Resend HTTP client lost a transport guard")
	}

	// Use the public-HTTPS transport's real resolver and dial policy on the
	// local hosts entry. No DNS traffic to a provider or public socket is needed.
	remote, err := NewResendEmailSender("production", resendTestConfig("https://mail.example.test/emails"))
	if err != nil {
		t.Fatal(err)
	}
	checkedDial := remote.client.Transport.(*http.Transport).DialContext
	_, port, err := net.SplitHostPort(target.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := checkedDial(t.Context(), "tcp", net.JoinHostPort("localhost", port))
	if conn != nil {
		_ = conn.Close()
	}
	if err != ErrAuthDeliveryFailed || conn != nil {
		t.Fatal("private DNS answer reached the public transport")
	}

	const privateResponse = "private-response-diagnostic"
	rejected := resendTestServer(t, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, privateResponse, 500) })
	_, deliveryErr := rejected.SendChallenge(t.Context(), msg)
	if deliveryErr != ErrAuthDeliveryFailed {
		t.Fatal("upstream diagnostic escaped")
	}
	notice := AccountNotice{Destination: resendTestRecipient}
	r := DeliveryReceipt{ProviderMessageRef: resendTestReference}
	p := resendPayload{To: []string{resendTestRecipient}, Text: resendTestCode}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
		if a.Key == slog.TimeKey {
			return slog.Attr{}
		}
		return a
	}}))
	for _, value := range []any{s, *s, s.cfg, msg, notice, r, p, deliveryJob{message: msg, notice: &notice}, deliveryErr} {
		logger.Error("delivery", "value", value)
		_, _ = fmt.Fprintf(&logs, "%v %+v %#v", value, value, value)
	}
	for _, private := range []string{resendTestRecipient, resendTestKey, resendTestCode, resendTestReference, s.cfg.FromEmail, privateResponse} {
		if strings.Contains(logs.String(), private) {
			t.Fatal("sensitive data appeared in formatted output or slog")
		}
	}
}
