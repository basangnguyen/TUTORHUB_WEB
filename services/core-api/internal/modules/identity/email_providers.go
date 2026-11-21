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

var resendChallengeActions = map[string]struct{ vi, en string }{
	"signup_email":      {"xác minh địa chỉ email đăng ký", "verify your signup email"},
	"login_code":        {"đăng nhập", "sign in"},
	"password_recovery": {"đặt lại mật khẩu", "reset your password"},
	"link_identity":     {"liên kết phương thức đăng nhập", "link a sign-in method"},
	"reauth":            {"xác nhận lại danh tính", "confirm your identity again"},
	"mfa":               {"xác minh bước hai", "complete two-step verification"},
}

func (s *ResendEmailSender) SendChallenge(ctx context.Context, msg ChallengeMessage) (DeliveryReceipt, error) {
	action, ok := resendChallengeActions[msg.Purpose]
	if !ok || ctx.Err() != nil || msg.ChallengeID == uuid.Nil || msg.Channel != DeliveryEmail ||
		!validOTP(msg.Code) || !msg.ExpiresAt.After(time.Now()) || !config.ValidBareEmailAddress(msg.Destination) {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	expiry := msg.ExpiresAt.UTC().Format("2006-01-02 15:04:05 UTC")
	subjectVI, subjectEN := "Mã TutorHub để "+action.vi, "TutorHub code to "+action.en
	warningVI := "Mã có hiệu lực đến " + expiry + ". Không chia sẻ mã này với bất kỳ ai."
	warningEN := "This code expires at " + expiry + ". Do not share it with anyone."
	subject, warning := subjectEN, warningEN
	switch {
	case strings.HasPrefix(strings.ToLower(msg.Locale), "vi"):
		subject, warning = subjectVI, warningVI
	case msg.Locale == "":
		subject, warning = subjectVI+" / "+subjectEN, warningVI+"\n\n"+warningEN
	}
	text := subject + "\n\n" + msg.Code + "\n\n" + warning
	return s.send(ctx, msg.Destination, "auth-challenge:"+msg.ChallengeID.String(), subject, text)
}

// Notices have no locale in the delivery contract, so both languages are included.
var resendNotices = map[string]struct{ subject, text string }{
	"account_exists": {
		"Tài khoản TutorHub đã tồn tại / Existing TutorHub account",
		"Địa chỉ email này đã có tài khoản TutorHub. Hãy dùng phương thức đăng nhập hiện có.\n\nThis email already has a TutorHub account. Use your existing sign-in method.",
	},
	"password_changed": {
		"Mật khẩu TutorHub đã được đổi / TutorHub password changed",
		"Mật khẩu TutorHub của bạn đã được đổi. Nếu bạn không thực hiện thay đổi này, hãy khôi phục tài khoản và liên hệ hỗ trợ.\n\nYour TutorHub password has changed. If you did not make this change, recover your account and contact support.",
	},
	"phone_released": {
		"Số điện thoại đã được gỡ / TutorHub phone sign-in removed",
		"Số điện thoại đăng nhập đã được gỡ khỏi tài khoản TutorHub của bạn. Dùng phương thức đăng nhập khác để kiểm tra cài đặt bảo mật.\n\nA phone number was removed from your TutorHub account. Use another sign-in method to review your security settings.",
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
	return s.send(ctx, notice.Destination, key, message.subject, message.text)
}

func (s *ResendEmailSender) send(ctx context.Context, destination, key, subject, text string) (DeliveryReceipt, error) {
	from := mail.Address{Name: s.cfg.FromName, Address: s.cfg.FromEmail}
	payload := resendPayload{
		From: from.String(), To: []string{destination}, Subject: subject, Text: text,
		HTML: "<p>" + strings.ReplaceAll(html.EscapeString(text), "\n\n", "</p><p>") + "</p>",
	}
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
