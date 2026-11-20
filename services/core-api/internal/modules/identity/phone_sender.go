package identity

import (
	"context"
	"errors"
	"slices"

	"github.com/nyaruka/phonenumbers"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

// The provider identifier is assigned by the server at startup. Receipts and
// provider-controlled error messages never supply labels for the audit table.
type PhoneRoute struct {
	Provider string
	Sender   PhoneChallengeSender
}

type PhoneSenderRouter struct {
	routes map[int][]PhoneRoute
}

func NewPhoneSenderRouter(routes map[int][]PhoneRoute) (*PhoneSenderRouter, error) {
	result := &PhoneSenderRouter{routes: make(map[int][]PhoneRoute, len(routes))}
	for code, chain := range routes {
		if code < 1 || code > 999 || len(chain) == 0 {
			return nil, ErrAuthFoundationInput
		}
		for _, route := range chain {
			if route.Sender == nil || !route.Sender.Supports(code) {
				return nil, ErrAuthFoundationInput
			}
			switch route.Sender.Channel() {
			case DeliverySMS, "whatsapp", "zalo_zns", "voice":
			default:
				return nil, ErrAuthFoundationInput
			}
			switch route.Provider {
			case "devinbox", "sms_brandname", "zns", "twilio":
			default:
				return nil, ErrAuthFoundationInput
			}
		}
		result.routes[code] = slices.Clone(chain)
	}
	return result, nil
}

func NewDevinboxPhoneRouter(inbox *devinbox.Inbox, countries []string) (*PhoneSenderRouter, error) {
	if inbox == nil || len(countries) == 0 {
		return nil, ErrAuthFoundationInput
	}
	codes := make([]int, 0, len(countries))
	for _, country := range countries {
		code := phonenumbers.GetCountryCodeForRegion(country)
		if code == 0 {
			return nil, ErrAuthFoundationInput
		}
		codes = append(codes, code)
	}
	sender := NewDevinboxSender(inbox, DeliverySMS, codes...)
	routes := make(map[int][]PhoneRoute, len(codes))
	for _, code := range codes {
		routes[code] = []PhoneRoute{{Provider: "devinbox", Sender: sender}}
	}
	return NewPhoneSenderRouter(routes)
}

// Channel denotes the logical phone budget. Each physical attempt carries
// its own actual channel, including a country-specific primary or fallback.
func (*PhoneSenderRouter) Channel() DeliveryChannel { return DeliverySMS }
func (r *PhoneSenderRouter) Supports(code int) bool { return len(r.routes[code]) > 0 }

func phoneCallingCode(destination string) int {
	number, err := phonenumbers.Parse(destination, phonenumbers.UNKNOWN_REGION)
	if err != nil {
		return 0
	}
	return int(number.GetCountryCode())
}

type phoneDeliveryAttempt struct {
	channel  DeliveryChannel
	provider string
}

var errPhoneFallbackQuota = errors.New("phone fallback delivery budget unavailable")

func (r *PhoneSenderRouter) SendChallenge(ctx context.Context, message ChallengeMessage) (DeliveryReceipt, error) {
	return r.send(ctx, message, nil)
}

func (r *PhoneSenderRouter) send(ctx context.Context, message ChallengeMessage, budget DeliveryBudget) (DeliveryReceipt, error) {
	code := phoneCallingCode(message.Destination)
	chain := r.routes[code]
	if len(chain) == 0 {
		return DeliveryReceipt{}, ErrAuthDeliveryDisabled
	}
	var attempts []phoneDeliveryAttempt
	for i, route := range chain {
		if ctx.Err() != nil {
			return DeliveryReceipt{phoneAttempts: attempts}, ErrAuthDeliveryFailed
		}
		if i > 0 && budget != nil && !budget.AllowDelivery(ctx, DeliverySMS) {
			// All previous sends failed. No unbudgeted fallback is attempted.
			return DeliveryReceipt{phoneAttempts: attempts}, errPhoneFallbackQuota
		}
		message.Channel = route.Sender.Channel()
		attempts = append(attempts, phoneDeliveryAttempt{channel: message.Channel, provider: route.Provider})
		receipt, err := route.Sender.SendChallenge(ctx, message)
		if err == nil {
			receipt.Channel = message.Channel
			receipt.Provider = route.Provider
			receipt.phoneAttempts = attempts
			return receipt, nil
		}
		if errors.Is(err, ErrAuthDeliveryDisabled) {
			return DeliveryReceipt{phoneAttempts: attempts}, ErrAuthDeliveryDisabled
		}
	}
	return DeliveryReceipt{phoneAttempts: attempts}, ErrAuthDeliveryFailed
}
