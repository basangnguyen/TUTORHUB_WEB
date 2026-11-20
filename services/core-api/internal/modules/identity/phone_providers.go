package identity

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nyaruka/phonenumbers"
	"github.com/tutorhub-v2/core-api/internal/config"
)

const (
	phoneHTTPTimeout   = 8 * time.Second
	phoneResponseLimit = 16 << 10
)

// Each sender gets its own transport. In particular, proxy environment variables
// must never move endpoint DNS resolution or access to OTP payloads to a proxy.
type phoneProviderHTTP struct {
	client   *http.Client
	endpoint string
}

func (phoneProviderHTTP) String() string         { return "[redacted]" }
func (p phoneProviderHTTP) GoString() string     { return p.String() }
func (p phoneProviderHTTP) LogValue() slog.Value { return slog.StringValue(p.String()) }

// Constructors pass only endpoints that have already gone through config.Normalize.
// For these endpoints plain HTTP means an explicitly allowed local test endpoint.
func newPhoneProviderHTTP(endpoint string) phoneProviderHTTP {
	scheme, _, _ := strings.Cut(endpoint, ":")
	local := strings.EqualFold(scheme, "http")
	dialer := &net.Dialer{Timeout: phoneHTTPTimeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialPhoneEndpoint(ctx, network, address, local, net.DefaultResolver.LookupNetIP, dialer.DialContext)
		},
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    phoneHTTPTimeout,
		MaxResponseHeaderBytes: phoneResponseLimit,
		// net/http may print unsolicited bytes on an idle HTTP/1 connection to
		// the process logger. Never pool connections carrying sensitive replies.
		DisableKeepAlives: true,
	}
	return phoneProviderHTTP{
		endpoint: endpoint,
		client: &http.Client{Transport: transport, Timeout: phoneHTTPTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func dialPhoneEndpoint(ctx context.Context, network, address string, local bool,
	lookup func(context.Context, string, string) ([]netip.Addr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, phoneHTTPTimeout)
	defer cancel()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, ErrAuthDeliveryFailed
	}
	if local {
		if host != "127.0.0.1" && !strings.EqualFold(host, "localhost") {
			return nil, ErrAuthDeliveryFailed
		}
		// Pin localhost to the IPv4 loopback permitted by the local HTTP exception.
		return dial(ctx, network, net.JoinHostPort("127.0.0.1", port))
	}
	addresses, err := lookup(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrAuthDeliveryFailed
	}
	// Reject the whole DNS answer if any address is unsafe. Dial a checked
	// literal so the underlying dialer cannot perform a second hostname lookup.
	for _, ip := range addresses {
		if !config.PublicPhoneProviderIP(ip) {
			return nil, ErrAuthDeliveryFailed
		}
	}
	for _, ip := range addresses {
		conn, err := dial(ctx, network, net.JoinHostPort(ip.Unmap().String(), port))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, ErrAuthDeliveryFailed
}

func validatePhoneMessage(ctx context.Context, sender PhoneChallengeSender, countries []string, msg ChallengeMessage) error {
	if ctx.Err() != nil || !validOTP(msg.Code) || !msg.ExpiresAt.After(time.Now()) || msg.Channel != sender.Channel() {
		return ErrAuthDeliveryFailed
	}
	if !sender.Supports(phoneCallingCode(msg.Destination)) {
		return ErrAuthDeliveryDisabled
	}
	number, err := NormalizePhoneNumber(msg.Destination, "", countries)
	if err != nil || number.E164 != msg.Destination {
		return ErrAuthDeliveryFailed
	}
	return nil
}

func phoneJSONRequest(ctx context.Context, endpoint string, payload any) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, ErrAuthDeliveryFailed
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, ErrAuthDeliveryFailed
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (p phoneProviderHTTP) response(req *http.Request) ([]byte, error) {
	if p.client == nil {
		return nil, ErrAuthDeliveryFailed
	}
	// A paid request body may not be replayed by the transport.
	req.GetBody = nil
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, ErrAuthDeliveryFailed
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, phoneResponseLimit))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.ContentLength > phoneResponseLimit {
		return nil, ErrAuthDeliveryFailed
	}
	// A chunked response exactly at the limit is valid only if EOF follows.
	if len(body) == phoneResponseLimit && resp.ContentLength < 0 {
		if _, err := io.CopyN(io.Discard, resp.Body, 1); err != io.EOF {
			return nil, ErrAuthDeliveryFailed
		}
	}
	return body, nil
}

func phoneReference(field json.RawMessage) string {
	var reference string
	if json.Unmarshal(field, &reference) != nil {
		var numeric json.Number
		if json.Unmarshal(field, &numeric) == nil {
			reference = numeric.String()
		}
	}
	return strings.TrimSpace(reference)
}

func phoneAccepted(channel DeliveryChannel, provider, reference string) DeliveryReceipt {
	if reference == "" {
		reference = uuid.NewString()
	}
	runes := []rune(reference)
	return DeliveryReceipt{Channel: channel, Provider: provider, AcceptedAt: time.Now().UTC(),
		ProviderMessageRef: string(runes[:min(len(runes), 128)])}
}

type ZaloZNSSender struct {
	phoneProviderHTTP
	cfg config.ZNSConfig
}

func NewZaloZNSSender(environment string, cfg config.ZNSConfig) (*ZaloZNSSender, error) {
	cfg, err := cfg.Normalize(environment)
	if err != nil {
		return nil, err
	}
	return &ZaloZNSSender{newPhoneProviderHTTP(cfg.APIURL), cfg}, nil
}

func (*ZaloZNSSender) Channel() DeliveryChannel { return DeliveryZaloZNS }
func (*ZaloZNSSender) Supports(code int) bool   { return code == 84 }

func (s *ZaloZNSSender) SendChallenge(ctx context.Context, msg ChallengeMessage) (DeliveryReceipt, error) {
	if err := validatePhoneMessage(ctx, s, []string{"VN"}, msg); err != nil {
		return DeliveryReceipt{}, err
	}
	req, err := phoneJSONRequest(ctx, s.endpoint, map[string]any{
		"phone": strings.TrimPrefix(msg.Destination, "+"), "template_id": s.cfg.TemplateID,
		"template_data": map[string]string{s.cfg.OTPParam: msg.Code}, "tracking_id": msg.ChallengeID.String(),
	})
	if err != nil {
		return DeliveryReceipt{}, err
	}
	req.Header.Set("access_token", s.cfg.AccessToken)
	body, err := s.response(req)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	var result struct {
		Error *int            `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil || result.Error == nil || *result.Error != 0 {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	var data struct {
		MsgID json.RawMessage `json:"msg_id"`
	}
	_ = json.Unmarshal(result.Data, &data)
	return phoneAccepted(s.Channel(), "zns", phoneReference(data.MsgID)), nil
}

type SMSBrandnameSender struct {
	phoneProviderHTTP
	cfg config.SMSBrandnameConfig
}

func NewSMSBrandnameSender(environment string, cfg config.SMSBrandnameConfig) (*SMSBrandnameSender, error) {
	cfg, err := cfg.Normalize(environment)
	if err != nil {
		return nil, err
	}
	return &SMSBrandnameSender{newPhoneProviderHTTP(cfg.APIURL), cfg}, nil
}

func (*SMSBrandnameSender) Channel() DeliveryChannel { return DeliverySMS }
func (*SMSBrandnameSender) Supports(code int) bool   { return code == 84 }

func (s *SMSBrandnameSender) SendChallenge(ctx context.Context, msg ChallengeMessage) (DeliveryReceipt, error) {
	if err := validatePhoneMessage(ctx, s, []string{"VN"}, msg); err != nil {
		return DeliveryReceipt{}, err
	}
	req, err := phoneJSONRequest(ctx, s.endpoint, map[string]string{
		"to": msg.Destination, "from": s.cfg.Sender, "message": strings.Replace(s.cfg.Template, "{{code}}", msg.Code, 1),
		"idempotency_key": msg.ChallengeID.String(),
	})
	if err != nil {
		return DeliveryReceipt{}, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIToken)
	body, err := s.response(req)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) == nil {
		var status string
		_ = json.Unmarshal(fields["status"], &status)
		status = strings.ToLower(strings.TrimSpace(status))
		if status == "failed" || status == "error" || nonzeroBrandnameCode(fields["error_code"]) {
			return DeliveryReceipt{}, ErrAuthDeliveryFailed
		}
	}
	ref := phoneReference(fields["message_id"])
	if ref == "" {
		ref = phoneReference(fields["id"])
	}
	return phoneAccepted(s.Channel(), "sms_brandname", ref), nil
}

// The field has already been parsed as JSON. Checking its mantissa handles
// integer and exponential notation without overflow or rounding to zero.
func nonzeroBrandnameCode(raw json.RawMessage) bool {
	if len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return false
	}
	if exponent := bytes.IndexAny(raw, "eE"); exponent >= 0 {
		raw = raw[:exponent]
	}
	return bytes.ContainsAny(raw, "123456789")
}

type TwilioSMSSender struct {
	phoneProviderHTTP
	cfg       config.TwilioConfig
	countries []string
	codes     []int
}

func NewTwilioSMSSender(environment string, cfg config.TwilioConfig, countries []string) (*TwilioSMSSender, error) {
	cfg, err := cfg.Normalize(environment)
	if err != nil {
		return nil, err
	}
	if len(countries) == 0 {
		return nil, ErrAuthFoundationInput
	}
	codes := make([]int, 0, len(countries))
	for _, region := range countries {
		code := phonenumbers.GetCountryCodeForRegion(region)
		if code == 0 {
			return nil, ErrAuthFoundationInput
		}
		codes = append(codes, code)
	}
	endpoint := cfg.APIBaseURL + "/2010-04-01/Accounts/" + cfg.AccountSID + "/Messages.json"
	return &TwilioSMSSender{newPhoneProviderHTTP(endpoint), cfg, slices.Clone(countries), codes}, nil
}

func (*TwilioSMSSender) Channel() DeliveryChannel { return DeliverySMS }
func (s *TwilioSMSSender) Supports(code int) bool { return slices.Contains(s.codes, code) }

func (s *TwilioSMSSender) SendChallenge(ctx context.Context, msg ChallengeMessage) (DeliveryReceipt, error) {
	if err := validatePhoneMessage(ctx, s, s.countries, msg); err != nil {
		return DeliveryReceipt{}, err
	}
	values := url.Values{"To": {msg.Destination}, "Body": {strings.ReplaceAll(s.cfg.Template, "{{code}}", msg.Code)}}
	if strings.HasPrefix(s.cfg.From, "MG") {
		values.Set("MessagingServiceSid", s.cfg.From)
	} else {
		values.Set("From", s.cfg.From)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(s.cfg.AccountSID, s.cfg.AuthToken)
	body, err := s.response(req)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	var result struct {
		SID json.RawMessage `json:"sid"`
	}
	_ = json.Unmarshal(body, &result)
	return phoneAccepted(s.Channel(), "twilio", phoneReference(result.SID)), nil
}
