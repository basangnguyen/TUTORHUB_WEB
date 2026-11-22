package identity

import (
	"context"
	"encoding/json"
	"html"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
)

// ResendEmailSender shares the paid phone senders' isolated HTTP transport and
// per-request response limits. The embedded transport also redacts the sender.
type ResendEmailSender struct {
	phoneProviderHTTP
	cfg config.ResendConfig
}

func NewResendEmailSender(environment string, cfg config.ResendConfig) (*ResendEmailSender, error) {
	cfg, err := cfg.Normalize(environment)
	if err != nil {
		return nil, err
	}
	return &ResendEmailSender{newPhoneProviderHTTP(cfg.APIURL), cfg}, nil
}

type resendPayload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html"`
}

func (resendPayload) String() string         { return "[redacted]" }
func (p resendPayload) GoString() string     { return p.String() }
func (p resendPayload) LogValue() slog.Value { return slog.StringValue(p.String()) }

// resendChallengeCopy holds the purpose wording. None of it may contain digits:
// the six-digit code must be the only number in the subject, text and HTML so
// Gmail's "Code requested / Copy code" extractor has exactly one candidate.
type resendChallengeCopy struct{ vi, en, subjectEN string }

var resendChallengeActions = map[string]resendChallengeCopy{
	"signup_email":      {"xác minh địa chỉ email đăng ký", "verify your signup email", "Your temporary TutorHub verification code"},
	"login_code":        {"đăng nhập", "sign in", "Your temporary TutorHub login code"},
	"password_recovery": {"đặt lại mật khẩu", "reset your password", "Your temporary TutorHub password reset code"},
	"link_identity":     {"liên kết phương thức đăng nhập", "link a sign-in method", "Your temporary TutorHub verification code"},
	"reauth":            {"xác nhận lại danh tính", "confirm your identity again", "Your temporary TutorHub security code"},
	"mfa":               {"xác minh bước hai", "complete two-step verification", "Your temporary TutorHub verification code"},
}

const (
	resendFooterVI = "Mã xác minh tạm thời này sẽ sớm hết hạn. Không chia sẻ mã này với bất kỳ ai. Nếu bạn không thực hiện yêu cầu này, vui lòng bỏ qua email này."
	resendFooterEN = "This temporary verification code expires shortly. Do not share it with anyone. If you did not request this code, you can safely ignore this email."
)

func (s *ResendEmailSender) SendChallenge(ctx context.Context, msg ChallengeMessage) (DeliveryReceipt, error) {
	action, ok := resendChallengeActions[msg.Purpose]
	if !ok || ctx.Err() != nil || msg.ChallengeID == uuid.Nil || msg.Channel != DeliveryEmail ||
		!validOTP(msg.Code) || !msg.ExpiresAt.After(time.Now()) || !config.ValidBareEmailAddress(msg.Destination) {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	subject, text, body := resendChallengeContent(msg, action)
	return s.send(ctx, msg.Destination, "auth-challenge:"+msg.ChallengeID.String(), subject, text, body)
}

// resendChallengeContent renders a challenge deterministically from the message:
// it never reads the clock or prints the expiry, and the subject never carries
// the code.
func resendChallengeContent(msg ChallengeMessage, action resendChallengeCopy) (subject, text, body string) {
	leadVI := "Nhập mã xác minh tạm thời này để " + action.vi + ":"
	leadEN := "Enter this temporary verification code to " + action.en + ":"
	lang := "en"
	var leads, footers []string
	switch {
	case strings.HasPrefix(strings.ToLower(msg.Locale), "vi"):
		lang, subject = "vi", "Mã xác minh tạm thời TutorHub để "+action.vi
		leads, footers = []string{leadVI}, []string{resendFooterVI}
	case msg.Locale == "":
		// Signed-out flows have no locale, so both languages are sent.
		lang, subject = "vi", "Mã xác minh TutorHub để "+action.vi+" — Your temporary TutorHub verification code"
		leads, footers = []string{leadVI, leadEN}, []string{resendFooterVI, resendFooterEN}
	default:
		subject = action.subjectEN + " to " + action.en
		leads, footers = []string{leadEN}, []string{resendFooterEN}
	}
	text = "TutorHub\n" + strings.Join(leads, "\n") + "\n\n" + msg.Code + "\n\n" + strings.Join(footers, "\n\n")

	blocks := make([]string, 0, 5)
	if len(leads) == 1 {
		blocks = append(blocks, resendParagraph(resendLeadCSS+"margin: 0 0 18px 0;", leads[0]))
	} else {
		blocks = append(blocks, resendParagraph(resendLeadCSS+"margin: 0;", leads[0]), resendParagraph(resendSubLeadCSS, leads[1]))
	}
	blocks = append(blocks, resendCodeBox(msg.Code))
	for i, footer := range footers {
		margin := "margin: 0;"
		if i < len(footers)-1 {
			margin = "margin: 0 0 8px 0;"
		}
		blocks = append(blocks, resendParagraph(resendFooterCSS+margin, footer))
	}
	return subject, text, resendHTML(lang, blocks...)
}

// Notices have no locale in the delivery contract, so both languages are included.
var resendNotices = map[string]struct{ subject, vi, en string }{
	"account_exists": {
		"Tài khoản TutorHub đã tồn tại / Existing TutorHub account",
		"Địa chỉ email này đã có tài khoản TutorHub. Hãy dùng phương thức đăng nhập hiện có.",
		"This email already has a TutorHub account. Use your existing sign-in method.",
	},
	"password_changed": {
		"Mật khẩu TutorHub đã được đổi / TutorHub password changed",
		"Mật khẩu TutorHub của bạn đã được đổi. Nếu bạn không thực hiện thay đổi này, hãy khôi phục tài khoản và liên hệ hỗ trợ.",
		"Your TutorHub password has changed. If you did not make this change, recover your account and contact support.",
	},
	"phone_released": {
		"Số điện thoại đã được gỡ / TutorHub phone sign-in removed",
		"Số điện thoại đăng nhập đã được gỡ khỏi tài khoản TutorHub của bạn. Dùng phương thức đăng nhập khác để kiểm tra cài đặt bảo mật.",
		"A phone number was removed from your TutorHub account. Use another sign-in method to review your security settings.",
	},
}

func (s *ResendEmailSender) SendNotice(ctx context.Context, notice AccountNotice) (DeliveryReceipt, error) {
	message, ok := resendNotices[notice.Kind]
	if !ok || ctx.Err() != nil || !notice.ExpiresAt.After(time.Now()) || !config.ValidBareEmailAddress(notice.Destination) {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	id := notice.ChallengeID
	if id == uuid.Nil {
		id = uuid.New()
	}
	key := "auth-notice:" + notice.Kind + ":" + id.String()
	body := resendHTML("vi",
		resendParagraph(resendLeadCSS+"margin: 0 0 16px 0;", message.vi),
		resendParagraph(resendLeadCSS+"margin: 0;", message.en))
	return s.send(ctx, notice.Destination, key, message.subject, message.vi+"\n\n"+message.en, body)
}

// Email HTML uses inline CSS only (no <style>, remote images, links, scripts or
// url() references) so Gmail, Apple Mail and Outlook render it the same way.
const (
	resendFontCSS    = "font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif;"
	resendLeadCSS    = "font-size: 15px; line-height: 1.6; color: #374151; "
	resendSubLeadCSS = "font-size: 13px; line-height: 1.6; color: #6b7280; margin: 4px 0 18px 0;"
	resendFooterCSS  = "font-size: 13px; line-height: 1.6; color: #6b7280; "
	resendCodeCSS    = "font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace; " +
		"font-size: 30px; font-weight: 600; letter-spacing: 4px; line-height: 1.2; color: #111827; " +
		"user-select: all; -webkit-user-select: all;"
)

// resendHTML wraps already-escaped blocks in the shared TutorHub shell. lang is
// a server-chosen constant, never caller input.
func resendHTML(lang string, blocks ...string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html lang="` + lang + `"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1"></head>` +
		`<body style="margin: 0; padding: 0; background-color: #ffffff;">` +
		`<div style="max-width: 460px; margin: 0 auto; padding: 40px 24px; ` + resendFontCSS + ` color: #111827;">` +
		`<div style="font-size: 28px; font-weight: 700; line-height: 1.2; letter-spacing: -0.02em; color: #111827; margin: 0 0 24px 0;">TutorHub</div>`)
	for _, block := range blocks {
		b.WriteString(block)
	}
	b.WriteString(`</div></body></html>`)
	return b.String()
}

func resendParagraph(css, text string) string {
	return `<p style="` + css + `">` + html.EscapeString(text) + `</p>`
}

// resendCodeBox isolates the code in its own element inside a grey rounded
// box. A one-cell table keeps the background in Outlook's Word renderer.
func resendCodeBox(code string) string {
	return `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" ` +
		`style="width: 100%; border-collapse: separate; margin: 0 0 22px 0;"><tr>` +
		`<td bgcolor="#f3f4f6" style="background-color: #f3f4f6; border-radius: 12px; padding: 20px 24px;">` +
		`<div style="` + resendCodeCSS + `">` + html.EscapeString(code) + `</div></td></tr></table>`
}

func (s *ResendEmailSender) send(ctx context.Context, destination, key, subject, text, htmlBody string) (DeliveryReceipt, error) {
	from := mail.Address{Name: s.cfg.FromName, Address: s.cfg.FromEmail}
	payload := resendPayload{From: from.String(), To: []string{destination}, Subject: subject, Text: text, HTML: htmlBody}
	req, err := phoneJSONRequest(ctx, s.endpoint, payload)
	if err != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	req.Header.Set("Idempotency-Key", key)
	body, err := s.response(req)
	if err != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	var fields map[string]json.RawMessage
	var id string
	if json.Unmarshal(body, &fields) != nil || json.Unmarshal(fields["id"], &id) != nil ||
		len(id) == 0 || len(id) > 128 || strings.IndexFunc(id, func(r rune) bool { return r < '!' || r > '~' }) >= 0 {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	return DeliveryReceipt{Channel: DeliveryEmail, Provider: "resend", ProviderMessageRef: id, AcceptedAt: time.Now().UTC()}, nil
}
