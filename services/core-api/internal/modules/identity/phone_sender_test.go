package identity

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

type phoneTestSender struct {
	channel DeliveryChannel
	calls   int
	err     error
}

func (s *phoneTestSender) Channel() DeliveryChannel { return s.channel }
func (*phoneTestSender) Supports(code int) bool     { return code == 84 }
func (s *phoneTestSender) SendChallenge(_ context.Context, m ChallengeMessage) (DeliveryReceipt, error) {
	s.calls++
	if m.Channel != s.channel {
		return DeliveryReceipt{}, ErrAuthDeliveryFailed
	}
	return DeliveryReceipt{ProviderMessageRef: "opaque-fixture"}, s.err
}

func TestPhoneSenderRouterFallbackAndCountry(t *testing.T) {
	primary := &phoneTestSender{channel: "zalo_zns", err: ErrAuthDeliveryFailed}
	fallback := &phoneTestSender{channel: DeliverySMS}
	router, err := NewPhoneSenderRouter(map[int][]PhoneRoute{84: {{Provider: "zns", Sender: primary}, {Provider: "sms_brandname", Sender: fallback}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m := ChallengeMessage{Channel: DeliverySMS, Destination: "+84912345678"}
	d := &DeliveryDispatcher{phone: router}
	ch, p := d.plannedRoute(m.Channel, m.Destination)
	if ch != "zalo_zns" || p != "zns" {
		t.Fatal("wrong planned primary")
	}
	r, err := router.send(ctx, m, foundationTestBudget(true))
	if err != nil || r.Channel != DeliverySMS || r.Provider != "sms_brandname" || len(r.phoneAttempts) != 2 || primary.calls != 1 || fallback.calls != 1 {
		t.Fatal("wrong fallback receipt or attempts")
	}
	r, err = router.send(ctx, m, foundationTestBudget(false))
	if !errors.Is(err, errPhoneFallbackQuota) || len(r.phoneAttempts) != 1 || fallback.calls != 1 {
		t.Fatal("fallback bypassed its own delivery budget")
	}
	r, err = router.SendChallenge(ctx, m)
	if !errors.Is(err, errPhoneFallbackQuota) || len(r.phoneAttempts) != 1 || fallback.calls != 1 {
		t.Fatal("missing fallback budget allowed a second attempt")
	}
	primary.err = nil
	r, err = router.SendChallenge(ctx, m)
	if err != nil || r.Channel != "zalo_zns" || r.Provider != "zns" || fallback.calls != 1 {
		t.Fatal("successful primary also sent fallback")
	}
	m.Destination = "+12025550123"
	if _, err := router.SendChallenge(ctx, m); !errors.Is(err, ErrAuthDeliveryDisabled) || router.Supports(1) {
		t.Fatal("unconfigured country sent")
	}
}

func TestDevinboxPhoneRoutesAndReleaseNotice(t *testing.T) {
	inbox, err := devinbox.New("test", 5, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(inbox.Close)
	router, err := NewDevinboxPhoneRouter(inbox, []string{"VN", "US"})
	if err != nil || !router.Supports(1) || !router.Supports(84) || router.Supports(33) {
		t.Fatal("configured calling codes differ from router")
	}
	d := &DeliveryDispatcher{email: NewDevinboxSender(inbox, DeliveryEmail), budget: foundationTestBudget(true)}
	d.deliver(deliveryJob{notice: &AccountNotice{Kind: "phone_released", Destination: "owner@example.test", ExpiresAt: time.Now().Add(time.Minute)}})
	items := inbox.List()
	if len(items) != 1 || items[0].Purpose != "phone_released" || items[0].Code != "" || items[0].ChallengeID != "" || items[0].Destination != "owner@example.test" {
		t.Fatal("release notice was not delivered privately to prior owner")
	}
}

func TestDirectPhoneAdapterAuditLabels(t *testing.T) {
	for _, tc := range []struct {
		sender   PhoneChallengeSender
		channel  DeliveryChannel
		provider string
	}{
		{&ZaloZNSSender{}, DeliveryZaloZNS, "zns"},
		{&SMSBrandnameSender{}, DeliverySMS, "sms_brandname"},
		{&TwilioSMSSender{}, DeliverySMS, "twilio"},
		{&phoneTestSender{channel: DeliverySMS}, DeliverySMS, "disabled"},
	} {
		d := &DeliveryDispatcher{phone: tc.sender}
		ch, p := d.plannedRoute(DeliverySMS, "+84912345678")
		if ch != tc.channel || p != tc.provider {
			t.Fatalf("incorrect audit labels for %T", tc.sender)
		}
	}
	if _, err := NewPhoneSenderRouter(map[int][]PhoneRoute{
		84: {{Provider: "untrusted", Sender: &phoneTestSender{channel: DeliverySMS}}},
	}); err == nil {
		t.Fatal("unknown audit provider accepted")
	}
}

func TestDirectZNSReceivesPhysicalChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":0}`))
	}))
	t.Cleanup(server.Close)
	sender, err := NewZaloZNSSender("test", config.ZNSConfig{APIURL: server.URL, AccessToken: "fixture", TemplateID: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	d := &DeliveryDispatcher{phone: sender}
	receipt, err := d.sendPhoneChallenge(context.Background(), ChallengeMessage{
		Channel: DeliverySMS, Destination: "+84912345678", Code: "123456", ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil || receipt.Channel != DeliveryZaloZNS || receipt.Provider != "zns" {
		t.Fatal("direct ZNS delivery failed for a logical phone challenge")
	}
}
