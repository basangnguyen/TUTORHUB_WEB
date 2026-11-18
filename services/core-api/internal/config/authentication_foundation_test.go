package config

import (
	"strings"
	"testing"
	"time"
)

func TestAuthFoundationDefaultsAndFlags(t *testing.T) {
	t.Parallel()
	cfg, err := load(mapLookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	a := cfg.Authentication
	flags := map[string]func(AuthenticationConfig) bool{
		"AUTH_SIGNUP_ENABLED":             func(c AuthenticationConfig) bool { return c.SignupEnabled },
		"AUTH_METHOD_PASSWORD_ENABLED":    func(c AuthenticationConfig) bool { return c.PasswordEnabled },
		"AUTH_METHOD_EMAIL_CODE_ENABLED":  func(c AuthenticationConfig) bool { return c.EmailCodeEnabled },
		"AUTH_METHOD_PHONE_ENABLED":       func(c AuthenticationConfig) bool { return c.PhoneEnabled },
		"AUTH_PASSKEYS_ENABLED":           func(c AuthenticationConfig) bool { return c.PasskeysEnabled },
		"AUTH_MFA_TOTP_ENABLED":           func(c AuthenticationConfig) bool { return c.MFATOTPEnabled },
		"AUTH_DEVICE_MANAGEMENT_ENABLED":  func(c AuthenticationConfig) bool { return c.DeviceManagementEnabled },
		"AUTH_SSO_DOMAINS_ENABLED":        func(c AuthenticationConfig) bool { return c.SSODomainsEnabled },
		"AUTH_PROVIDER_GOOGLE_ENABLED":    func(c AuthenticationConfig) bool { return c.ProviderGoogleEnabled },
		"AUTH_PROVIDER_FACEBOOK_ENABLED":  func(c AuthenticationConfig) bool { return c.ProviderFacebookEnabled },
		"AUTH_PROVIDER_MICROSOFT_ENABLED": func(c AuthenticationConfig) bool { return c.ProviderMicrosoftEnabled },
		"AUTH_PROVIDER_APPLE_ENABLED":     func(c AuthenticationConfig) bool { return c.ProviderAppleEnabled },
	}
	for key, flag := range flags {
		if flag(a) {
			t.Errorf("%s must default off", key)
		}
		changed, err := load(mapLookup(map[string]string{key: "true", "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false",
			"SESSION_SECRET": validSessionSecret(), "DATABASE_POOL_URL": "postgres://fixture:fixture@localhost/fixture?sslmode=disable",
			"GOOGLE_CLIENT_ID": "fixture", "GOOGLE_CLIENT_SECRET": "fixture"}))
		if err != nil || !flag(changed.Authentication) {
			t.Errorf("%s explicit enable failed: %v", key, err)
		}
		if _, err := load(mapLookup(map[string]string{key: "invalid"})); err == nil {
			t.Errorf("%s accepted invalid boolean", key)
		}
	}
	if !a.ProviderOIDCDefaultEnabled || a.PasswordMinLength != 8 || a.SessionPersistentAbsoluteTTL != 720*time.Hour ||
		a.EmailSender != "disabled" || a.PhoneSender != "disabled" || a.SMTPAddr != "" || a.DeliveryBudgetApproved ||
		a.EmailDailyQuota != 200 || a.PhoneOTPDailyQuota != 50 || len(a.PhoneAllowedCountries) != 1 || a.PhoneAllowedCountries[0] != "VN" {
		t.Fatal("unsafe authentication foundation defaults")
	}
	cfg, err = load(mapLookup(map[string]string{"AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false"}))
	if err != nil || cfg.Authentication.ProviderOIDCDefaultEnabled {
		t.Fatal("cannot explicitly disable default OIDC")
	}
}

func TestAuthDeliveryGuardAcrossEnvironments(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"development", "test", "staging", "production"} {
		for _, test := range []struct{ key, sender, addr string }{
			{"AUTH_EMAIL_SENDER", "disabled", ""},
			{"AUTH_EMAIL_SENDER", "devinbox", ""},
			{"AUTH_EMAIL_SENDER", "smtp", "127.0.0.1:1025"},
			{"AUTH_EMAIL_SENDER", "ses", ""},
			{"AUTH_PHONE_SENDER", "disabled", ""},
			{"AUTH_PHONE_SENDER", "devinbox", ""},
			{"AUTH_PHONE_SENDER", "zns", ""},
			{"AUTH_PHONE_SENDER", "sms_brandname", ""},
			{"AUTH_PHONE_SENDER", "twilio", ""},
		} {
			for _, approved := range []string{"false", "true"} {
				var failures []error
				var cfg AuthenticationConfig
				configureAuthFoundation(&cfg, mapLookup(map[string]string{test.key: test.sender, "AUTH_SMTP_ADDR": test.addr, "AUTH_DELIVERY_BUDGET_APPROVED": approved}), environment, &failures)
				local := environment == "development" || environment == "test"
				paid := test.sender == "ses" || test.sender == "zns" || test.sender == "sms_brandname" || test.sender == "twilio"
				wantErr := (!local && (test.sender == "devinbox" || test.sender == "smtp")) || (paid && approved != "true")
				if (len(failures) > 0) != wantErr {
					t.Errorf("%s %s=%s approval=%s errors=%v", environment, test.key, test.sender, approved, failures)
				}
			}
		}
	}
}

func TestDeviceManagementRequiresSessionAndDatabaseWithoutEnablingOIDC(t *testing.T) {
	t.Parallel()
	values := map[string]string{"AUTH_DEVICE_MANAGEMENT_ENABLED": "true", "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED": "false"}
	if _, err := load(mapLookup(values)); err == nil || !strings.Contains(err.Error(), "DATABASE_POOL_URL") || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatal("device management must validate database and session prerequisites")
	}
	values["DATABASE_POOL_URL"] = "postgres://fixture:fixture@localhost/fixture?sslmode=disable"
	values["SESSION_SECRET"] = validSessionSecret()
	cfg, err := load(mapLookup(values))
	if err != nil || !cfg.Authentication.Enabled || !cfg.Authentication.DeviceManagementEnabled || cfg.Authentication.OIDCEnabled || cfg.Authentication.PasswordEnabled || cfg.Authentication.EmailCodeEnabled {
		t.Fatal("device management must support existing sessions independently of sign-in methods")
	}
}

func TestAuthFoundationValidationRunsWithoutOIDC(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ key, value string }{
		{"PASSWORD_MIN_LENGTH", "7"}, {"PASSWORD_MIN_LENGTH", "65"},
		{"SESSION_PERSISTENT_ABSOLUTE_TTL", "721h"}, {"SESSION_PERSISTENT_ABSOLUTE_TTL", "0h"},
		{"AUTH_PHONE_ALLOWED_COUNTRIES", ""}, {"AUTH_PHONE_ALLOWED_COUNTRIES", "VN,VN"}, {"AUTH_PHONE_ALLOWED_COUNTRIES", "Vietnam"},
		{"AUTH_EMAIL_SENDER", "unknown"}, {"AUTH_EMAIL_SENDER", "twilio"}, {"AUTH_PHONE_SENDER", "ses"},
		{"AUTH_EMAIL_SENDER", "ses"}, {"AUTH_PHONE_SENDER", "twilio"},
		{"AUTH_EMAIL_DAILY_QUOTA", "-1"}, {"AUTH_PHONE_OTP_DAILY_QUOTA", "1000001"},
		{"AUTH_SMTP_ADDR", "mail.example:1025"}, {"AUTH_SMTP_ADDR", "127.0.0.1:70000"},
		{"AUTH_SMTP_ADDR", "127.0.0.1"}, {"AUTH_DELIVERY_BUDGET_APPROVED", "yes"},
	} {
		_, err := load(mapLookup(map[string]string{test.key: test.value}))
		if err == nil || !strings.Contains(err.Error(), test.key) {
			t.Errorf("%s validation failed: %v", test.key, err)
		}
	}
	for _, environment := range []string{"staging", "production"} {
		_, err := load(mapLookup(map[string]string{"APP_ENV": environment, "AUTH_EMAIL_SENDER": "devinbox", "AUTH_PHONE_SENDER": "devinbox", "AUTH_SMTP_ADDR": "127.0.0.1:1025"}))
		for _, key := range []string{"AUTH_EMAIL_SENDER", "AUTH_PHONE_SENDER", "AUTH_SMTP_ADDR"} {
			if err == nil || !strings.Contains(err.Error(), key) {
				t.Errorf("%s did not fail closed for %s", environment, key)
			}
		}
	}
	for _, address := range []string{"127.0.0.1:1025", "[::1]:1025", "localhost:1025"} {
		_, err := load(mapLookup(map[string]string{"AUTH_EMAIL_SENDER": "smtp", "AUTH_SMTP_ADDR": address, "PASSWORD_MIN_LENGTH": "64", "AUTH_PHONE_ALLOWED_COUNTRIES": "vn,US"}))
		if err != nil {
			t.Errorf("valid local config rejected: %v", err)
		}
	}
}
