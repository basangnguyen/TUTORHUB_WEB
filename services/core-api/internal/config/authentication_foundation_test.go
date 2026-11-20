package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
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
		"AUTH_REQUIRE_ADMIN_MFA":          func(c AuthenticationConfig) bool { return c.RequireAdminMFA },
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
			"GOOGLE_CLIENT_ID": "fixture", "GOOGLE_CLIENT_SECRET": "fixture",
			"FACEBOOK_CLIENT_ID": "fixture", "FACEBOOK_CLIENT_SECRET": "fixture",
			"MICROSOFT_CLIENT_ID": "fixture", "MICROSOFT_CLIENT_SECRET": "fixture"}))
		if err != nil || !flag(changed.Authentication) {
			t.Errorf("%s explicit enable failed: %v", key, err)
		}
		if _, err := load(mapLookup(map[string]string{key: "invalid"})); err == nil {
			t.Errorf("%s accepted invalid boolean", key)
		}
	}
	if !a.ProviderOIDCDefaultEnabled || a.PasswordMinLength != 8 || a.SessionPersistentAbsoluteTTL != 720*time.Hour ||
		a.EmailSender != "disabled" || a.PhoneSender != "disabled" || a.PhoneFallbackSender != "disabled" || a.SMTPAddr != "" || a.DeliveryBudgetApproved ||
		a.EmailDailyQuota != 200 || a.PhoneOTPDailyQuota != 50 || len(a.PhoneAllowedCountries) != 1 || a.PhoneAllowedCountries[0] != "VN" {
		t.Fatal("unsafe authentication foundation defaults")
	}
	if a.PhoneCountryHourlyQuota != 100 || a.PhoneBreakerMinSends != 50 || a.PhoneBreakerMinConversionPct != 30 ||
		a.PhoneStaleAfter != 60*24*time.Hour || a.PhoneReclaimAfter != 90*24*time.Hour {
		t.Fatal("unexpected phone quota, breaker or SIM lifetime defaults")
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
				values := phoneProviderTestValues()
				values[test.key], values["AUTH_SMTP_ADDR"], values["AUTH_DELIVERY_BUDGET_APPROVED"] = test.sender, test.addr, approved
				configureAuthFoundation(&cfg, mapLookup(values), environment, &failures)
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

func phoneProviderTestValues() map[string]string {
	return map[string]string{
		"AUTH_DELIVERY_BUDGET_APPROVED": "true",
		"AUTH_ZNS_TEMPLATE_ID":          "fixture", "AUTH_ZNS_ACCESS_TOKEN": "fixture-token",
		"AUTH_SMS_BRANDNAME_API_URL": "https://gateway.example.test/otp", "AUTH_SMS_BRANDNAME_SENDER": "TutorHub",
		"AUTH_SMS_BRANDNAME_API_TOKEN": "fixture-token",
		"AUTH_TWILIO_ACCOUNT_SID":      "AC" + strings.Repeat("a", 32), "AUTH_TWILIO_AUTH_TOKEN": "fixture-token",
		"AUTH_TWILIO_FROM": "+14155550123",
	}
}

func TestPhoneRouteConfiguration(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"test", "staging", "development", "production"} {
		for _, role := range []string{"disabled", "devinbox", "zns", "sms_brandname", "twilio"} {
			for _, fallback := range []string{"disabled", "", "sms_brandname", "twilio", "zns", "devinbox", "invalid"} {
				for _, countries := range []string{"VN", "VN,SG"} {
					for _, approval := range []string{"false", "true"} {
						values := phoneProviderTestValues()
						values["AUTH_PHONE_SENDER"], values["AUTH_PHONE_FALLBACK_SENDER"] = role, fallback
						values["AUTH_PHONE_ALLOWED_COUNTRIES"], values["AUTH_DELIVERY_BUDGET_APPROVED"] = countries, approval
						var c AuthenticationConfig
						var failures []error
						configureAuthFoundation(&c, mapLookup(values), environment, &failures)
						local := environment == "test" || environment == "development"
						free := role == "disabled" || role == "devinbox"
						fallbackOff := fallback == "" || fallback == "disabled"
						pairOK := fallbackOff || (!free && role != fallback && (fallback == "twilio" || fallback == "sms_brandname"))
						coverage := free || countries == "VN" || role == "twilio" || fallback == "twilio"
						wantOK := (local || role != "devinbox") && (free || approval == "true") && pairOK && coverage
						if (len(failures) == 0) != wantOK || (fallbackOff && c.PhoneFallbackSender != "disabled") {
							t.Fatalf("%s primary=%s fallback=%q countries=%s approved=%s: %v", environment, role, fallback, countries, approval, failures)
						}
					}
				}
			}
		}
	}
}

func TestPhoneProviderConfigValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ primary, fallback, key, value string }{
		{"zns", "", "AUTH_ZNS_ACCESS_TOKEN", ""}, {"zns", "", "AUTH_ZNS_ACCESS_TOKEN", "fixture\r\ntoken"},
		{"zns", "", "AUTH_ZNS_TEMPLATE_ID", "x y"}, {"zns", "", "AUTH_ZNS_TEMPLATE_ID", strings.Repeat("x", 65)},
		{"zns", "", "AUTH_ZNS_OTP_PARAM", "x-é"}, {"zns", "", "AUTH_ZNS_OTP_PARAM", strings.Repeat("x", 33)},
		{"zns", "", "AUTH_ZNS_API_URL", "http://localhost:12345"},
		{"zns", "sms_brandname", "AUTH_SMS_BRANDNAME_API_URL", ""}, {"zns", "sms_brandname", "AUTH_SMS_BRANDNAME_API_TOKEN", ""},
		{"sms_brandname", "", "AUTH_SMS_BRANDNAME_SENDER", ""}, {"zns", "sms_brandname", "AUTH_SMS_BRANDNAME_SENDER", "Brandname123"},
		{"zns", "sms_brandname", "AUTH_SMS_BRANDNAME_TEMPLATE", "Hi"}, {"sms_brandname", "", "AUTH_SMS_BRANDNAME_TEMPLATE", "{{code}} {{code}}"},
		{"sms_brandname", "", "AUTH_SMS_BRANDNAME_TEMPLATE", "{{code}}\n"}, {"sms_brandname", "", "AUTH_SMS_BRANDNAME_TEMPLATE", "\xff{{code}}"},
		{"sms_brandname", "", "AUTH_SMS_BRANDNAME_TEMPLATE", strings.Repeat("á", 313) + "{{code}}"},
		{"zns", "twilio", "AUTH_TWILIO_ACCOUNT_SID", ""}, {"zns", "twilio", "AUTH_TWILIO_AUTH_TOKEN", ""},
		{"zns", "twilio", "AUTH_TWILIO_API_BASE_URL", "https://user:fixture@api.example.test"},
		{"twilio", "", "AUTH_TWILIO_FROM", ""}, {"twilio", "", "AUTH_TWILIO_FROM", "MG123"}, {"twilio", "", "AUTH_TWILIO_FROM", "+0123456789"},
		{"twilio", "", "AUTH_TWILIO_TEMPLATE", "Code"}, {"twilio", "", "AUTH_TWILIO_TEMPLATE", "\xff{{code}}"},
		{"twilio", "", "AUTH_DELIVERY_BUDGET_APPROVED", "1"}, {"zns", "twilio", "AUTH_DELIVERY_BUDGET_APPROVED", "TRUE"},
	} {
		values := phoneProviderTestValues()
		values["AUTH_PHONE_SENDER"], values["AUTH_PHONE_FALLBACK_SENDER"], values[tc.key] = tc.primary, tc.fallback, tc.value
		var c AuthenticationConfig
		var failures []error
		configureAuthFoundation(&c, mapLookup(values), "staging", &failures)
		if err := errors.Join(failures...); err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s did not reject invalid configuration: %v", tc.key, err)
		}
	}

	values := phoneProviderTestValues()
	values["AUTH_PHONE_SENDER"], values["AUTH_PHONE_FALLBACK_SENDER"] = "zns", "twilio"
	values["AUTH_TWILIO_API_BASE_URL"], values["AUTH_ZNS_API_URL"] = "https://api.twilio.com///", ""
	values["AUTH_TWILIO_FROM"], values["AUTH_TWILIO_TEMPLATE"] = "MG"+strings.Repeat("a", 32), strings.Repeat("á", 312)+"{{code}}"
	values["AUTH_SMS_BRANDNAME_API_TOKEN"] = "invalid unused token"
	var c AuthenticationConfig
	var failures []error
	configureAuthFoundation(&c, mapLookup(values), "production", &failures)
	if len(failures) > 0 || c.ZNS.APIURL != "https://business.openapi.zalo.me/message/template" || c.ZNS.OTPParam != "otp" ||
		c.Twilio.APIBaseURL != "https://api.twilio.com" || c.SMSBrandname.APIToken != "" {
		t.Fatalf("failed to load selected provider defaults: %v", failures)
	}
	c.Twilio.Template = "Verification code: {{code}}\n@example.test #{{code}}"
	if _, err := c.Twilio.Normalize("production"); err != nil {
		t.Fatal("valid multiline Twilio template rejected", err)
	}
}

func TestPhoneProviderEndpointRules(t *testing.T) {
	t.Parallel()
	for _, environment := range []string{"test", "development", "staging", "production"} {
		for _, raw := range []string{
			"https://localhost", "https://LOCALHOST.", "https://foo.localhost", "https://127.0.0.2", "https://[::1]",
			"https://10.0.0.1", "https://172.31.255.255", "https://192.168.1.1", "https://169.254.169.254",
			"https://0.0.0.0", "https://224.0.0.1", "https://[fe80::1]", "https://[fd00::1]", "https://[::ffff:127.0.0.1]",
			"https://[::]", "https://[ff02::1]", "https://[64:ff9b::a00:1]", "https://100.64.0.1",
			"https://127.1", "https://2130706433", "https://0x7f.0x0.0x0.0x1", "https://127.0.0.1.",
			"ftp://api.example.test", "http://api.example.test", "http://localhost", "http://[::1]:3000", "http://127.0.0.2:3000",
			"http://127.0.0.1:0", "https://api.example.test:", "https://api.example.test:65536",
			"https://api.example.test/path?x=1", "https://api.example.test/path?", "https://api.example.test/#", "https://:fixture@api.example.test",
		} {
			if validPhoneProviderURL(raw, environment) {
				t.Errorf("unsafe endpoint allowed in %s: %s", environment, raw)
			}
		}
		for _, raw := range []string{"http://127.0.0.1:8080/fixture", "http://localhost:3000"} {
			if got := validPhoneProviderURL(raw, environment); got != (environment == "test" || environment == "development") {
				t.Errorf("unexpected fixture endpoint policy in %s", environment)
			}
		}
		for _, raw := range []string{"https://api.example.test:443/v1", "https://8.8.8.8", "https://[2606:4700:4700::1111]/v1"} {
			if !validPhoneProviderURL(raw, environment) {
				t.Errorf("valid endpoint rejected in %s: %s", environment, raw)
			}
		}
	}
}

func TestPhoneProviderConfigurationIsRedacted(t *testing.T) {
	t.Parallel()
	secret := "private-fixture-token"
	c := AuthenticationConfig{ZNS: ZNSConfig{AccessToken: secret}, SMSBrandname: SMSBrandnameConfig{APIToken: secret}, Twilio: TwilioConfig{AuthToken: secret, From: "+84912345678"}}
	for _, value := range []any{c.ZNS, c.SMSBrandname, c.Twilio, c, Config{Authentication: c}} {
		var buf bytes.Buffer
		slog.New(slog.NewJSONHandler(&buf, nil)).Info("config", "value", value)
		message := fmt.Sprint(value) + fmt.Sprintf("%#v", value) + buf.String()
		if strings.Contains(message, secret) || strings.Contains(message, "84912345678") || !strings.Contains(message, "[redacted]") {
			t.Fatal("configuration reflected authentication material")
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
		{"AUTH_PHONE_COUNTRY_HOURLY_QUOTA", "0"}, {"AUTH_PHONE_COUNTRY_HOURLY_QUOTA", "1000001"},
		{"AUTH_PHONE_BREAKER_MIN_SENDS", "0"}, {"AUTH_PHONE_BREAKER_MIN_SENDS", "100001"},
		{"AUTH_PHONE_BREAKER_MIN_CONVERSION_PCT", "-1"}, {"AUTH_PHONE_BREAKER_MIN_CONVERSION_PCT", "101"},
		{"AUTH_PHONE_STALE_AFTER", "59m"}, {"AUTH_PHONE_RECLAIM_AFTER", "1439h"},
		{"AUTH_PHONE_ALLOWED_COUNTRIES", "ZZ"},
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
