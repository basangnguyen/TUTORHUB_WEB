package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"regexp"
	"slices"
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
	if strings.Contains(p.Subject, resendTestCode) || !strings.Contains(p.HTML, "<p style=") {
		t.Error("unexpected sensitive subject or unstyled HTML")
	}
	assertResendInertHTML(t, p.HTML)
	return p
}

// The shell may only contain static markup and inline CSS: no remote images,
// links, scripts or remote CSS.
func assertResendInertHTML(t *testing.T, body string) {
	t.Helper()
	lower := strings.ToLower(body)
	for _, active := range []string{"<img", "<script", "<a", "href=", "src=", "url("} {
		if strings.Contains(lower, active) {
			t.Errorf("HTML contains active or remote content %q", active)
		}
	}
	if !strings.Contains(body, resendTestHeader) {
		t.Error("HTML is missing the bold TutorHub header")
	}
}

const resendTestHeader = `<div style="font-size: 28px; font-weight: 700; line-height: 1.2; letter-spacing: -0.02em; color: #111827; margin: 0 0 24px 0;">TutorHub</div>`

var (
	resendTestTags   = regexp.MustCompile(`<[^>]*>`)
	resendTestDigits = regexp.MustCompile(`[0-9]+`)
	// Same extractor as postgres_resend_delivery_integration_test.go.
	resendTestOTP = regexp.MustCompile(`\b[0-9]{6}\b`)
)

// resendVisibleText drops tags (with their style attributes) and decodes
// entities, leaving the text a reader or Gmail's code extractor sees.
func resendVisibleText(body string) string {
	return html.UnescapeString(resendTestTags.ReplaceAllString(body, " "))
}

// Independent copy of the AUTH-15 wording so a typo in the production table fails.
var resendTestPurposes = []struct{ purpose, vi, en, subjectEN string }{
	{"signup_email", "xác minh địa chỉ email đăng ký", "verify your signup email", "Your temporary TutorHub verification code"},
	{"login_code", "đăng nhập", "sign in", "Your temporary TutorHub login code"},
	{"password_recovery", "đặt lại mật khẩu", "reset your password", "Your temporary TutorHub password reset code"},
	{"link_identity", "liên kết phương thức đăng nhập", "link a sign-in method", "Your temporary TutorHub verification code"},
	{"reauth", "xác nhận lại danh tính", "confirm your identity again", "Your temporary TutorHub security code"},
	{"mfa", "xác minh bước hai", "complete two-step verification", "Your temporary TutorHub verification code"},
}

const (
	resendTestFooterVI = "Mã xác minh tạm thời này sẽ sớm hết hạn. Không chia sẻ mã này với bất kỳ ai. Nếu bạn không thực hiện yêu cầu này, vui lòng bỏ qua email này."
	resendTestFooterEN = "This temporary verification code expires shortly. Do not share it with anyone. If you did not request this code, you can safely ignore this email."
	resendTestCodeBox  = `<td bgcolor="#f3f4f6" style="background-color: #f3f4f6; border-radius: 12px; padding: 20px 24px;">` +
		`<div style="font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace; ` +
		`font-size: 30px; font-weight: 600; letter-spacing: 4px; line-height: 1.2; color: #111827; ` +
		`user-select: all; -webkit-user-select: all;">` + resendTestCode + `</div></td>`
)

func TestResendChallengeWireContract(t *testing.T) {
	for _, tc := range resendTestPurposes {
		leadVI := "Nhập mã xác minh tạm thời này để " + tc.vi + ":"
		leadEN := "Enter this temporary verification code to " + tc.en + ":"
		for _, locale := range []struct {
			name, value, lang string
			subject           []string
			leads, footers    []string
		}{
			{"vi-VN", "vi-VN", "vi", []string{"Mã xác minh tạm thời TutorHub để " + tc.vi},
				[]string{leadVI}, []string{resendTestFooterVI}},
			{"en-US", "en-US", "en", []string{"Your temporary TutorHub", tc.subjectEN, tc.en},
				[]string{leadEN}, []string{resendTestFooterEN}},
			{"bilingual", "", "vi", []string{"Mã xác minh TutorHub để " + tc.vi + " — Your temporary TutorHub verification code"},
				[]string{leadVI, leadEN}, []string{resendTestFooterVI, resendTestFooterEN}},
		} {
			t.Run(tc.purpose+"/"+locale.name, func(t *testing.T) {
				msg := resendTestChallenge()
				msg.Purpose, msg.Locale = tc.purpose, locale.value
				sender := resendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
					p := readResendRequest(t, r)
					if r.Header.Get("Idempotency-Key") != "auth-challenge:"+msg.ChallengeID.String() {
						t.Error("incorrect idempotency key")
					}
					for _, want := range locale.subject {
						if !strings.Contains(p.Subject, want) {
							t.Errorf("subject %q is missing %q", p.Subject, want)
						}
					}
					if resendTestDigits.MatchString(p.Subject) {
						t.Errorf("subject %q contains digits", p.Subject)
					}

					wantText := "TutorHub\n" + strings.Join(locale.leads, "\n") + "\n\n" + resendTestCode + "\n\n" + strings.Join(locale.footers, "\n\n")
					if p.Text != wantText {
						t.Errorf("text/plain = %q, want %q", p.Text, wantText)
					}
					if resendTestOTP.FindString(p.Text) != resendTestCode {
						t.Error("integration-test OTP extractor cannot find the code in text/plain")
					}

					if !strings.HasPrefix(p.HTML, `<!DOCTYPE html><html lang="`+locale.lang+`">`) ||
						!strings.Contains(p.HTML, `<body style="margin: 0; padding: 0; background-color: #ffffff;">`) ||
						!strings.Contains(p.HTML, `<div style="max-width: 460px; margin: 0 auto; padding: 40px 24px; font-family: -apple-system,`) {
						t.Error("HTML is missing the document shell or centred container")
					}
					if !strings.Contains(p.HTML, resendTestCodeBox) || strings.Count(p.HTML, resendTestCode) != 1 ||
						strings.Index(p.HTML, resendTestHeader) > strings.Index(p.HTML, resendTestCodeBox) {
						t.Error("code is not isolated once in the grey user-select box below the header")
					}
					codeAt := strings.Index(p.HTML, resendTestCodeBox)
					for _, lead := range locale.leads {
						if i := strings.Index(p.HTML, html.EscapeString(lead)+"</p>"); i < 0 || i > codeAt {
							t.Errorf("lead %q is not a paragraph above the code box", lead)
						}
					}
					for _, footer := range locale.footers {
						if i := strings.Index(p.HTML, html.EscapeString(footer)+"</p>"); i < codeAt {
							t.Errorf("footer %q is not a paragraph below the code box", footer)
						}
					}
					if len(locale.leads) == 2 && !strings.Contains(p.HTML,
						`<p style="font-size: 13px; line-height: 1.6; color: #6b7280; margin: 4px 0 18px 0;">`+html.EscapeString(leadEN)+`</p>`) {
						t.Error("bilingual English lead is not the grey sub-line")
					}

					for name, body := range map[string]string{"text": p.Text, "visible HTML": resendVisibleText(p.HTML)} {
						if got := resendTestDigits.FindAllString(body, -1); !slices.Equal(got, []string{resendTestCode}) {
							t.Errorf("%s numbers = %q, want only the code", name, got)
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
}

func TestResendChallengeLocaleRouting(t *testing.T) {
	action := resendChallengeActions["login_code"]
	for _, tc := range []struct{ locale, subject string }{
		{"vi", "Mã xác minh tạm thời TutorHub để đăng nhập"},
		{"VI", "Mã xác minh tạm thời TutorHub để đăng nhập"},
		{"vi-VN", "Mã xác minh tạm thời TutorHub để đăng nhập"},
		{"en", "Your temporary TutorHub login code to sign in"},
		{"en-US", "Your temporary TutorHub login code to sign in"},
		{"fr-FR", "Your temporary TutorHub login code to sign in"},
		{"", "Mã xác minh TutorHub để đăng nhập — Your temporary TutorHub verification code"},
	} {
		msg := resendTestChallenge()
		msg.Locale = tc.locale
		if subject, _, _ := resendChallengeContent(msg, action); subject != tc.subject {
			t.Errorf("locale %q subject = %q, want %q", tc.locale, subject, tc.subject)
		}
	}
	if len(resendChallengeActions) != len(resendTestPurposes) {
		t.Fatal("purpose table and test copy diverged")
	}
	for _, want := range resendTestPurposes {
		if got := resendChallengeActions[want.purpose]; got != (resendChallengeCopy{want.vi, want.en, want.subjectEN}) {
			t.Errorf("purpose %q wording = %+v", want.purpose, got)
		}
	}
}

// The same message must render byte-for-byte the same content, and the expiry
// must not leak into it (no clock reads, no timestamp).
func TestResendChallengeContentIsDeterministic(t *testing.T) {
	payloads := make(chan resendPayload, 3)
	sender := resendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		payloads <- readResendRequest(t, r)
		_, _ = io.WriteString(w, `{"id":"`+resendTestReference+`"}`)
	})
	msg := resendTestChallenge()
	later := msg
	later.ExpiresAt = msg.ExpiresAt.Add(37 * time.Minute)
	for _, m := range []ChallengeMessage{msg, msg, later} {
		if _, err := sender.SendChallenge(t.Context(), m); err != nil {
			t.Fatal("challenge was not accepted")
		}
	}
	first := <-payloads
	for range 2 {
		if next := <-payloads; next.Subject != first.Subject || next.Text != first.Text || next.HTML != first.HTML {
			t.Fatal("challenge content changed between identical renders or with the expiry")
		}
	}
	for _, locale := range []string{"vi", "en", ""} {
		for purpose, action := range resendChallengeActions {
			m := resendTestChallenge()
			m.Purpose, m.Locale = purpose, locale
			s1, t1, h1 := resendChallengeContent(m, action)
			s2, t2, h2 := resendChallengeContent(m, action)
			if s1 != s2 || t1 != t2 || h1 != h2 {
				t.Fatalf("non-deterministic content for %s/%q", purpose, locale)
			}
		}
	}
}

func TestResendHTMLHelpersEscapeDynamicValues(t *testing.T) {
	const hostile = `<script>alert("x")</script><a title='t'>&</a>`
	escaped := html.EscapeString(hostile)
	for _, rendered := range []string{
		resendParagraph(resendLeadCSS+"margin: 0;", hostile),
		resendCodeBox(hostile),
		resendHTML("vi", resendParagraph(resendFooterCSS+"margin: 0;", hostile)),
	} {
		if !strings.Contains(rendered, escaped) || strings.Contains(rendered, "<script") || strings.Contains(rendered, "<a ") {
			t.Fatalf("dynamic value was not escaped: %q", rendered)
		}
	}
	assertResendInertHTML(t, resendHTML("en", resendParagraph(resendLeadCSS, hostile), resendCodeBox(hostile)))
}

func TestResendNoticeWireContract(t *testing.T) {
	for _, tc := range []struct{ kind, subject, vi, en string }{
		{"account_exists", "Existing TutorHub account", "Địa chỉ email này đã có tài khoản TutorHub.", "This email already has a TutorHub account."},
		{"password_changed", "password changed", "Mật khẩu TutorHub của bạn đã được đổi.", "Your TutorHub password has changed."},
		{"phone_released", "phone sign-in removed", "Số điện thoại đăng nhập đã được gỡ", "A phone number was removed from your TutorHub account."},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			keys := make(chan string, 4)
			notice := resendNotices[tc.kind]
			sender := resendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				p := readResendRequest(t, r)
				if !strings.Contains(p.Subject, tc.subject) || strings.Contains(p.Text+p.HTML, resendTestCode) {
					t.Error("invalid account notice content")
				}
				if p.Text != notice.vi+"\n\n"+notice.en || !strings.HasPrefix(p.Text, tc.vi) || !strings.Contains(p.Text, "\n\n"+tc.en) {
					t.Error("notice text/plain lost its bilingual paragraphs")
				}
				viAt := strings.Index(p.HTML, `<p style="font-size: 15px; line-height: 1.6; color: #374151; margin: 0 0 16px 0;">`+html.EscapeString(notice.vi)+`</p>`)
				enAt := strings.Index(p.HTML, `<p style="font-size: 15px; line-height: 1.6; color: #374151; margin: 0;">`+html.EscapeString(notice.en)+`</p>`)
				if !strings.HasPrefix(p.HTML, `<!DOCTYPE html><html lang="vi">`) || viAt < strings.Index(p.HTML, resendTestHeader) || enAt < viAt ||
					strings.Contains(p.HTML, "#f3f4f6") || strings.Contains(p.HTML, "user-select") {
					t.Error("notice HTML is not the TutorHub shell with two escaped paragraphs and no code box")
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
