package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/httpapi"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func TestPhoneStartupKeepsPaidAdaptersClosed(t *testing.T) {
	for _, sender := range []string{"zns", "sms_brandname", "twilio"} {
		cfg := config.Config{Authentication: config.AuthenticationConfig{PhoneEnabled: true, PhoneSender: sender, DeliveryBudgetApproved: true}}
		if _, err := newAuthFoundation(cfg, nil, nil); err == nil || !strings.Contains(err.Error(), "AUTH-11") {
			t.Fatalf("%s should fail at startup while its adapter is pending", sender)
		}
	}
	base := config.AuthenticationConfig{PhoneEnabled: true, PhoneSender: "devinbox", PhoneOTPDailyQuota: 2}
	if !phoneLoginAvailable(base) {
		t.Fatal("configured phone sender should count as available")
	}
	base.PhoneOTPDailyQuota = 0
	if phoneLoginAvailable(base) {
		t.Fatal("zero delivery budget should not count as a login method")
	}
	base.PhoneOTPDailyQuota = 2
	base.PhoneSender = "disabled"
	if phoneLoginAvailable(base) {
		t.Fatal("disabled phone sender should not count as a login method")
	}
}

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

func TestEmailCodeOnlyRuntimeAndDisabledPasswordStatus(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://fixture@127.0.0.1:1/auth_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, emailCode := range []bool{false, true} {
		auth := config.AuthenticationConfig{
			Enabled: true, EmailCodeEnabled: emailCode, SessionKey: bytes.Repeat([]byte{0x42}, 32),
			LegalTermsVersion: "2026-11-16-v2.0", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 24 * time.Hour,
			SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour,
		}
		runtime, err := newAuthFoundation(config.Config{Environment: "test", Authentication: auth}, pool, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := runtime.native.(identity.NativeAccountAuthAPI); !ok {
			t.Fatal("authenticated password status and email-code routes require the native account service")
		}
		runtime.close()
		provider, err := configuredIdentityProvider(context.Background(), auth)
		if err != nil || provider != nil {
			t.Fatal("email-code-only runtime attempted OIDC discovery")
		}
	}
	if _, err := newAuthFoundation(config.Config{Authentication: config.AuthenticationConfig{EmailCodeEnabled: true}}, nil, nil); err == nil {
		t.Fatal("email-code-only runtime accepted missing database/key")
	}
}

func TestDeviceManagementStartupRequiresRuntimeAndDoesNotEnableOIDC(t *testing.T) {
	if _, err := newAuthFoundation(config.Config{Authentication: config.AuthenticationConfig{DeviceManagementEnabled: true}}, nil, nil); err == nil {
		t.Fatal("device management accepted missing database/key")
	}
	pool, err := pgxpool.New(context.Background(), "postgres://fixture@127.0.0.1:1/auth_test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	auth := config.AuthenticationConfig{
		DeviceManagementEnabled: true, SessionKey: bytes.Repeat([]byte{0x42}, 32),
		LegalTermsVersion: "2026-11-16-v2.0", SessionTTL: 8 * time.Hour, SessionAbsoluteTTL: 24 * time.Hour,
		SessionPersistentAbsoluteTTL: 30 * 24 * time.Hour,
	}
	runtime, err := newAuthFoundation(config.Config{Environment: "test", Authentication: auth}, pool, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.close()
	if _, ok := runtime.native.(identity.NativeSecurityAPI); !ok {
		t.Fatal("device management requires the session security service")
	}
	if provider, err := configuredIdentityProvider(context.Background(), auth); err != nil || provider != nil {
		t.Fatal("device management must not activate OIDC discovery")
	}
}
