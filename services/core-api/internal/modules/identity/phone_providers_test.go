package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
)

const (
	testPhoneDestination = "+84912345678"
	testPhoneCode        = "029813"
	testProviderToken    = "fixture-provider-token"
)

var testPhoneProviderNames = []string{"zns", "sms_brandname", "twilio"}

func phoneTestConfig(name, endpoint string) any {
	switch name {
	case "zns":
		return config.ZNSConfig{APIURL: endpoint, AccessToken: testProviderToken, TemplateID: "verification", OTPParam: "pin"}
	case "sms_brandname":
		return config.SMSBrandnameConfig{APIURL: endpoint, APIToken: testProviderToken, Sender: "TutorHub", Template: "PIN {{code}}"}
	default:
		return config.TwilioConfig{APIBaseURL: endpoint, AuthToken: testProviderToken, AccountSID: "AC" + strings.Repeat("a", 32),
			From: "+12025550123", Template: "PIN {{code}}"}
	}
}

func newTestHTTPPhoneSender(name, env, endpoint string) (PhoneChallengeSender, error) {
	switch cfg := phoneTestConfig(name, endpoint).(type) {
	case config.ZNSConfig:
		return NewZaloZNSSender(env, cfg)
	case config.SMSBrandnameConfig:
		return NewSMSBrandnameSender(env, cfg)
	case config.TwilioConfig:
		return NewTwilioSMSSender(env, cfg, []string{"VN", "SG"})
	}
	panic("unknown phone config")
}

func phoneTestServer(t *testing.T, name string, handler http.HandlerFunc) PhoneChallengeSender {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	s, err := newTestHTTPPhoneSender(name, "test", server.URL)
	if err != nil {
		t.Fatalf("cannot construct local %s sender: %v", name, err)
	}
	return s
}

func phoneTestMessage(s PhoneChallengeSender) ChallengeMessage {
	return ChallengeMessage{ChallengeID: uuid.New(), Code: testPhoneCode, Destination: testPhoneDestination,
		Channel: s.Channel(), ExpiresAt: time.Now().Add(time.Minute)}
}

func TestPhoneProvidersWireContract(t *testing.T) {
	for _, name := range testPhoneProviderNames {
		t.Run(name, func(t *testing.T) {
			id := uuid.New().String()
			s := phoneTestServer(t, name, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Error("expected POST")
				}
				if name == "twilio" {
					account, token, ok := r.BasicAuth()
					if !ok || account != "AC"+strings.Repeat("a", 32) || token != testProviderToken ||
						r.URL.Path != "/2010-04-01/Accounts/"+account+"/Messages.json" ||
						r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.ParseForm() != nil ||
						len(r.PostForm) != 3 || r.PostForm.Get("From") != "+12025550123" ||
						r.PostForm.Get("To") != testPhoneDestination || r.PostForm.Get("Body") != "PIN "+testPhoneCode {
						t.Error("invalid Twilio request")
					}
					_, _ = io.WriteString(w, `{"sid":"  mapped-reference "}`)
					return
				}
				var fields map[string]json.RawMessage
				if r.Header.Get("Content-Type") != "application/json" || json.NewDecoder(r.Body).Decode(&fields) != nil || len(fields) != 4 {
					t.Error("invalid JSON request")
				}
				if name == "zns" {
					var data map[string]string
					if json.Unmarshal(fields["template_data"], &data) != nil || len(data) != 1 || data["pin"] != testPhoneCode ||
						r.Header.Get("access_token") != testProviderToken || phoneReference(fields["phone"]) != testPhoneDestination[1:] ||
						phoneReference(fields["tracking_id"]) != id || phoneReference(fields["template_id"]) != "verification" {
						t.Error("invalid ZNS request")
					}
					_, _ = io.WriteString(w, `{"error":0,"data":{"msg_id":"  mapped-reference "}}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer "+testProviderToken ||
					phoneReference(fields["to"]) != testPhoneDestination || phoneReference(fields["from"]) != "TutorHub" ||
					phoneReference(fields["idempotency_key"]) != id || phoneReference(fields["message"]) != "PIN "+testPhoneCode {
					t.Error("invalid Brandname request")
				}
				_, _ = io.WriteString(w, `{"message_id":"  mapped-reference ", "id":"ignored","error_code":0}`)
			})
			channel := DeliverySMS
			if name == "zns" {
				channel = DeliveryZaloZNS
			}
			if s.Channel() != channel || !s.Supports(84) || s.Supports(1) || s.Supports(65) != (name == "twilio") {
				t.Fatal("wrong channel or country coverage")
			}
			msg := phoneTestMessage(s)
			msg.ChallengeID = uuid.MustParse(id)
			r, err := s.SendChallenge(t.Context(), msg)
			if err != nil || r.ProviderMessageRef != "mapped-reference" || r.Provider != name || r.Channel != channel || r.AcceptedAt.IsZero() {
				t.Fatal("expected accepted provider reference")
			}
		})
	}
}

func TestPhoneProvidersTwilioMessagingService(t *testing.T) {
	serviceSID := "MG" + strings.Repeat("a", 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ParseForm() != nil || r.PostForm.Get("MessagingServiceSid") != serviceSID ||
			r.PostForm.Has("From") || r.PostForm.Get("To") != "+6591234567" ||
			r.PostForm.Get("Body") != "PIN "+testPhoneCode+"\n"+testPhoneCode {
			t.Error("wrong messaging service request")
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	cfg := phoneTestConfig("twilio", strings.Replace(server.URL, "http:", "HTTP:", 1)+"/").(config.TwilioConfig)
	cfg.From = serviceSID
	cfg.Template = "PIN {{code}}\n{{code}}"
	s, err := NewTwilioSMSSender("test", cfg, []string{"SG"})
	if err != nil {
		t.Fatal(err)
	}
	msg := phoneTestMessage(s)
	msg.Destination = "+6591234567"
	r, err := s.SendChallenge(t.Context(), msg)
	if err != nil || uuid.Validate(r.ProviderMessageRef) != nil {
		t.Fatal("expected generated reference")
	}
}

func TestPhoneProvidersApplicationResponses(t *testing.T) {
	for _, tc := range []struct {
		name, provider, body string
		ok                   bool
		ref                  string
	}{
		{"Zalo no account", "zns", `{"error":-118,"message":"upstream-sensitive"}`, false, ""},
		{"Zalo missing field", "zns", `{}`, false, ""},
		{"Zalo null field", "zns", `{"error":null}`, false, ""},
		{"Zalo non integer", "zns", `{"error":0.0}`, false, ""},
		{"Zalo string code", "zns", `{"error":"0"}`, false, ""},
		{"Zalo malformed", "zns", `not JSON`, false, ""},
		{"Zalo without ref", "zns", `{"error":0}`, true, ""},
		{"Brandname failed", "sms_brandname", `{"status":"failed"}`, false, ""},
		{"Brandname error", "sms_brandname", `{"status":"error"}`, false, ""},
		{"Brandname numeric code", "sms_brandname", `{"error_code":-1}`, false, ""},
		{"Brandname numeric exponent", "sms_brandname", `{"error_code":1e-4000}`, false, ""},
		{"Brandname alternate ID", "sms_brandname", `{"id":212,"error_code":0.00e5}`, true, "212"},
		{"Brandname empty ack", "sms_brandname", "", true, ""},
		{"Twilio empty ack", "twilio", "", true, ""},
		{"Twilio bounded ID", "twilio", `{"sid":"` + strings.Repeat("x", 300) + `"}`, true, strings.Repeat("x", 128)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := phoneTestServer(t, tc.provider, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) })
			r, err := s.SendChallenge(t.Context(), phoneTestMessage(s))
			if !tc.ok {
				if err != ErrAuthDeliveryFailed {
					t.Fatal("expected fixed delivery error")
				}
				return
			}
			if err != nil || (tc.ref == "" && uuid.Validate(r.ProviderMessageRef) != nil) || (tc.ref != "" && r.ProviderMessageRef != tc.ref) {
				t.Fatal("unexpected provider reference")
			}
		})
	}
}

func TestPhoneProvidersHTTPGuards(t *testing.T) {
	for _, name := range testPhoneProviderNames {
		t.Run(name+"/redirect", func(t *testing.T) {
			var forwarded atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }))
			defer target.Close()
			s := phoneTestServer(t, name, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL, http.StatusFound)
			})
			if _, err := s.SendChallenge(t.Context(), phoneTestMessage(s)); err != ErrAuthDeliveryFailed || forwarded.Load() != 0 {
				t.Fatal("redirect must not forward any request")
			}
		})
		t.Run(name+"/timeout", func(t *testing.T) {
			release := make(chan struct{})
			var calls atomic.Int32
			s := phoneTestServer(t, name, func(http.ResponseWriter, *http.Request) {
				calls.Add(1)
				<-release
			})
			defer close(release)
			var client *http.Client
			switch s := s.(type) {
			case *ZaloZNSSender:
				client = s.client
			case *SMSBrandnameSender:
				client = s.client
			case *TwilioSMSSender:
				client = s.client
			}
			transport := client.Transport.(*http.Transport)
			if client.Timeout <= 0 || client.Timeout > 8*time.Second || transport.Proxy != nil || !transport.DisableKeepAlives {
				t.Fatal("unsafe HTTP client")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			if _, err := s.SendChallenge(ctx, phoneTestMessage(s)); err != ErrAuthDeliveryFailed || ctx.Err() != context.DeadlineExceeded {
				t.Fatal("request did not respect caller timeout")
			}
			// Exercise the independent client deadline with a still-live parent.
			client.Timeout = 200 * time.Millisecond
			parent, stop := context.WithTimeout(t.Context(), 3*time.Second)
			defer stop()
			if _, err := s.SendChallenge(parent, phoneTestMessage(s)); err != ErrAuthDeliveryFailed || parent.Err() != nil || calls.Load() == 0 {
				t.Fatal("client deadline did not terminate the pending request")
			}
		})
		for _, size := range []int{phoneResponseLimit, phoneResponseLimit + 1} {
			t.Run(fmt.Sprintf("%s/chunked-%d", name, size), func(t *testing.T) {
				s := phoneTestServer(t, name, func(w http.ResponseWriter, _ *http.Request) {
					w.(http.Flusher).Flush()
					const ack = `{"error":0}`
					_, _ = io.WriteString(w, ack+strings.Repeat(" ", size-len(ack)))
				})
				_, err := s.SendChallenge(t.Context(), phoneTestMessage(s))
				if (size == phoneResponseLimit && err != nil) || (size > phoneResponseLimit && err != ErrAuthDeliveryFailed) {
					t.Fatal("response limit mismatch")
				}
			})
		}
		t.Run(name+"/redaction", func(t *testing.T) {
			const privateBody = "private-provider-error"
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			original := slog.Default()
			slog.SetDefault(logger)
			defer slog.SetDefault(original)
			s := phoneTestServer(t, name, func(w http.ResponseWriter, _ *http.Request) { http.Error(w, privateBody, 503) })
			msg := phoneTestMessage(s)
			r, err := s.SendChallenge(t.Context(), msg)
			if err != ErrAuthDeliveryFailed {
				t.Fatal("upstream HTTP error was not masked")
			}
			cfg := phoneTestConfig(name, "")
			logger.Error("send", "err", err, "sender", s, "message", msg, "receipt", r, "config", cfg)
			text := logs.String() + fmt.Sprint(s, cfg, msg, r, err)
			for _, secret := range []string{testPhoneDestination, testPhoneDestination[1:], testPhoneCode, testProviderToken, privateBody} {
				if strings.Contains(text, secret) {
					t.Fatal("log or string representation disclosed private data")
				}
			}
		})
	}
}

func TestPhoneProvidersRejectBadInput(t *testing.T) {
	for _, name := range testPhoneProviderNames {
		t.Run(name, func(t *testing.T) {
			s := phoneTestServer(t, name, func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") })
			for _, change := range []func(*ChallengeMessage){
				func(m *ChallengeMessage) { m.Code = "１２３４５６" },
				func(m *ChallengeMessage) { m.Code = "12345" },
				func(m *ChallengeMessage) { m.ExpiresAt = time.Now() },
				func(m *ChallengeMessage) { m.Channel = DeliveryEmail },
				func(m *ChallengeMessage) { m.Destination = "+84 912 345 678" },
				func(m *ChallengeMessage) { m.Destination = "+842438221234" },
			} {
				m := phoneTestMessage(s)
				change(&m)
				if _, err := s.SendChallenge(t.Context(), m); err != ErrAuthDeliveryFailed {
					t.Fatal("bad message should fail")
				}
			}
			m := phoneTestMessage(s)
			m.Destination = "+12025550123"
			if _, err := s.SendChallenge(t.Context(), m); err != ErrAuthDeliveryDisabled {
				t.Fatal("unsupported calling code should be disabled")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if _, err := s.SendChallenge(ctx, phoneTestMessage(s)); err != ErrAuthDeliveryFailed {
				t.Fatal("cancelled message should fail")
			}
		})
	}
}

func TestPhoneProvidersEndpointPolicy(t *testing.T) {
	for _, name := range testPhoneProviderNames {
		for _, env := range []string{"test", "development", "staging", "production"} {
			t.Run(name+"/"+env, func(t *testing.T) {
				for _, endpoint := range []string{"http://example.org", "http://169.254.169.254", "https://10.2.3.4",
					"https://172.16.1.1", "https://192.168.1.1", "https://[fe80::1]", "https://[fc00::1]", "https://[::]",
					"https://[ff02::1]", "https://127.0.0.1", "https://localhost", "https://user@example.org",
					"https://example.org/token?k=v", "https://example.org/token#frag"} {
					if _, err := newTestHTTPPhoneSender(name, env, endpoint); err == nil {
						t.Errorf("accepted forbidden endpoint %s", endpoint)
					}
				}
				local := env == "test" || env == "development"
				for _, endpoint := range []string{"http://127.0.0.1:12345", "http://localhost:12345"} {
					if _, err := newTestHTTPPhoneSender(name, env, endpoint); (err == nil) != local {
						t.Error("wrong handling of loopback HTTP")
					}
				}
				if _, err := newTestHTTPPhoneSender(name, env, "https://provider.example.org/otp"); err != nil {
					t.Error("valid public HTTPS endpoint rejected")
				}
			})
		}
	}
}

func TestPhoneProvidersCheckedDial(t *testing.T) {
	public := netip.MustParseAddr("8.8.8.8")
	for _, private := range []string{"0.0.0.0", "127.0.0.2", "::1", "10.0.0.1", "172.31.0.1", "192.168.0.1",
		"169.254.169.254", "fe80::1", "fd00::1", "239.1.1.1", "ff02::1", "::ffff:192.168.1.2"} {
		lookup := func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{public, netip.MustParseAddr(private)}, nil
		}
		dial := func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("mixed DNS answer reached dial")
			return nil, nil
		}
		if _, err := dialPhoneEndpoint(t.Context(), "tcp", "provider.example.org:443", false, lookup, dial); err != ErrAuthDeliveryFailed {
			t.Error("unsafe DNS answer")
		}
	}
	var lookups int
	lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
		lookups++
		if host != "provider.example.org" || network != "ip" {
			t.Fatal("unexpected hostname")
		}
		return []netip.Addr{public}, nil
	}
	var dialed string
	dial := func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = address
		return nil, errors.New("mock dial failure")
	}
	_, _ = dialPhoneEndpoint(t.Context(), "tcp", "provider.example.org:443", false, lookup, dial)
	if lookups != 1 || dialed != "8.8.8.8:443" {
		t.Fatal("dial was not pinned to checked literal address")
	}
	_, _ = dialPhoneEndpoint(t.Context(), "tcp", "localhost:8080", true, lookup, dial)
	if lookups != 1 || dialed != "127.0.0.1:8080" {
		t.Fatal("loopback dial escaped local address")
	}
}
