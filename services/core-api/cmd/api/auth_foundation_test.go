package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/httpapi"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

type deliveryBudgetLimiter struct {
	action httpapi.InvitationRateLimitAction
	bucket string
	result httpapi.InvitationRateLimitDecision
}

func (limiter *deliveryBudgetLimiter) Allow(_ context.Context, action httpapi.InvitationRateLimitAction, bucket string, _ time.Time) httpapi.InvitationRateLimitDecision {
	limiter.action, limiter.bucket = action, bucket
	return limiter.result
}

func TestAuthDeliveryBudgetIsGlobalAndFailsClosed(t *testing.T) {
	for _, channel := range []identity.DeliveryChannel{identity.DeliveryEmail, identity.DeliverySMS} {
		limiter := &deliveryBudgetLimiter{result: httpapi.InvitationRateLimitDecision{Allowed: true}}
		budget := authDeliveryBudget{limiter: limiter}
		if !budget.AllowDelivery(context.Background(), channel) || limiter.bucket != "global" {
			t.Fatal("delivery quota must use a global bucket")
		}
		want := httpapi.AuthRateLimitDeliveryDailyPhone
		if channel == identity.DeliveryEmail {
			want = httpapi.AuthRateLimitDeliveryDailyEmail
		}
		if limiter.action != want {
			t.Fatal("delivery quota used the wrong channel")
		}
		limiter.result = httpapi.InvitationRateLimitDecision{Allowed: true, Err: errors.New("unavailable")}
		if budget.AllowDelivery(context.Background(), channel) || (authDeliveryBudget{}).AllowDelivery(context.Background(), channel) {
			t.Fatal("quota storage failure must prevent delivery")
		}
	}
}

func TestAuthFoundationDisabledNeedsNoDatabase(t *testing.T) {
	runtime, err := newAuthFoundation(config.Config{}, nil, nil)
	if err != nil || runtime.inbox != nil || runtime.dispatcher != nil {
		t.Fatal("disabled foundation must preserve database-free startup")
	}
	runtime.close()
	for _, sender := range []string{"devinbox", "smtp", "ses"} {
		cfg := config.Config{Environment: "test", Authentication: config.AuthenticationConfig{EmailSender: sender}}
		if _, err := newAuthFoundation(cfg, nil, nil); err == nil {
			t.Fatal("configured delivery must fail closed without prerequisites")
		}
	}
}

func TestNativeStartupDoesNotDiscoverOIDC(t *testing.T) {
	provider, err := configuredIdentityProvider(context.Background(), config.AuthenticationConfig{
		Enabled: true, SignupEnabled: true, PasswordEnabled: true, OIDCEnabled: false,
		IssuerURL: "this-ignored-value-must-never-be-discovered",
	})
	if err != nil || provider != nil {
		t.Fatal("native-only startup attempted OIDC initialization")
	}
	if _, err := newAuthFoundation(config.Config{Authentication: config.AuthenticationConfig{SignupEnabled: true}}, nil, nil); err == nil {
		t.Fatal("native authentication must reject missing runtime prerequisites")
	}
}
