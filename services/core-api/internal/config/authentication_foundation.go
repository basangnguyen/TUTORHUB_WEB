package config

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// These deployment gates are separate from tenant feature controls because an
// authentication transaction has no authenticated tenant yet.
func configureAuthFoundation(cfg *AuthenticationConfig, lookup lookupEnv, environment string, validationErrors *[]error) {
	for key, target := range map[string]*bool{
		"AUTH_SIGNUP_ENABLED":             &cfg.SignupEnabled,
		"AUTH_METHOD_PASSWORD_ENABLED":    &cfg.PasswordEnabled,
		"AUTH_METHOD_EMAIL_CODE_ENABLED":  &cfg.EmailCodeEnabled,
		"AUTH_METHOD_PHONE_ENABLED":       &cfg.PhoneEnabled,
		"AUTH_PASSKEYS_ENABLED":           &cfg.PasskeysEnabled,
		"AUTH_MFA_TOTP_ENABLED":           &cfg.MFATOTPEnabled,
		"AUTH_DEVICE_MANAGEMENT_ENABLED":  &cfg.DeviceManagementEnabled,
		"AUTH_SSO_DOMAINS_ENABLED":        &cfg.SSODomainsEnabled,
		"AUTH_PROVIDER_GOOGLE_ENABLED":    &cfg.ProviderGoogleEnabled,
		"AUTH_PROVIDER_FACEBOOK_ENABLED":  &cfg.ProviderFacebookEnabled,
		"AUTH_PROVIDER_MICROSOFT_ENABLED": &cfg.ProviderMicrosoftEnabled,
		"AUTH_PROVIDER_APPLE_ENABLED":     &cfg.ProviderAppleEnabled,
	} {
		*target = boolValue(lookup, key, false, validationErrors)
	}
	cfg.ProviderOIDCDefaultEnabled = boolValue(lookup, "AUTH_PROVIDER_OIDC_DEFAULT_ENABLED", true, validationErrors)
	cfg.PasswordMinLength = intValue(lookup, "PASSWORD_MIN_LENGTH", 8, 8, 64, validationErrors)
	cfg.PasswordHashMaxConcurrency = intValue(lookup, "PASSWORD_HASH_MAX_CONCURRENCY", 2, 1, 32, validationErrors)
	cfg.PasswordBreachCheck = strings.TrimSpace(valueOrDefault(lookup, "AUTH_PASSWORD_BREACH_CHECK", "offline"))
	if !containsString([]string{"offline", "hibp", "off"}, cfg.PasswordBreachCheck) {
		*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_PASSWORD_BREACH_CHECK must be offline, hibp or off"))
	}
	cfg.LegalTermsVersion = strings.TrimSpace(valueOrDefault(lookup, "AUTH_LEGAL_TERMS_VERSION", "2026-11-16-v2.0"))
	if !utf8.ValidString(cfg.LegalTermsVersion) || utf8.RuneCountInString(cfg.LegalTermsVersion) < 1 || utf8.RuneCountInString(cfg.LegalTermsVersion) > 64 {
		*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_LEGAL_TERMS_VERSION must contain 1 to 64 characters"))
	}
	cfg.SessionPersistentAbsoluteTTL = durationValue(lookup, "SESSION_PERSISTENT_ABSOLUTE_TTL", 720*time.Hour, validationErrors)
	if cfg.SessionPersistentAbsoluteTTL > 720*time.Hour {
		*validationErrors = append(*validationErrors, fmt.Errorf("SESSION_PERSISTENT_ABSOLUTE_TTL must not exceed 720h"))
	}
	cfg.PhoneAllowedCountries = authPhoneCountries(lookup, validationErrors)
	cfg.EmailSender = strings.TrimSpace(valueOrDefault(lookup, "AUTH_EMAIL_SENDER", "disabled"))
	cfg.PhoneSender = strings.TrimSpace(valueOrDefault(lookup, "AUTH_PHONE_SENDER", "disabled"))
	cfg.SMTPAddr = strings.TrimSpace(valueOrDefault(lookup, "AUTH_SMTP_ADDR", ""))
	cfg.DeliveryBudgetApproved = boolValue(lookup, "AUTH_DELIVERY_BUDGET_APPROVED", false, validationErrors)
	cfg.EmailDailyQuota = intValue(lookup, "AUTH_EMAIL_DAILY_QUOTA", 200, 0, 1_000_000, validationErrors)
	cfg.PhoneOTPDailyQuota = intValue(lookup, "AUTH_PHONE_OTP_DAILY_QUOTA", 50, 0, 1_000_000, validationErrors)
	// Loopback E2E runs every auth spec from one IP in one database. Scaling the
	// per-IP buckets is a test-only aid; production always uses the base limits.
	cfg.E2EIPRateLimitMultiplier = intValue(lookup, "AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER", 1, 1, 100, validationErrors)

	development := environment == "development" || environment == "test"
	if cfg.E2EIPRateLimitMultiplier != 1 && environment != "test" {
		*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER requires APP_ENV=test"))
	}
	validateAuthSender("AUTH_EMAIL_SENDER", cfg.EmailSender, []string{"disabled", "devinbox", "smtp", "ses"}, development, cfg.DeliveryBudgetApproved, validationErrors)
	validateAuthSender("AUTH_PHONE_SENDER", cfg.PhoneSender, []string{"disabled", "devinbox", "zns", "sms_brandname", "twilio"}, development, cfg.DeliveryBudgetApproved, validationErrors)
	if cfg.SMTPAddr != "" {
		host, port, err := net.SplitHostPort(cfg.SMTPAddr)
		portNumber, portErr := strconv.Atoi(port)
		ip := net.ParseIP(host)
		if !development || err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 ||
			(host != "localhost" && (ip == nil || !ip.IsLoopback())) {
			*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_SMTP_ADDR must be a loopback host:port in development or test"))
		}
	} else if cfg.EmailSender == "smtp" {
		*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_SMTP_ADDR is required for the smtp sender"))
	}
}

func validateAuthSender(key, sender string, allowed []string, development, budgetApproved bool, validationErrors *[]error) {
	if !containsString(allowed, sender) {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s is not a supported sender", key))
		return
	}
	if !development && (sender == "devinbox" || sender == "smtp") {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s local senders require development or test", key))
	}
	if containsString([]string{"ses", "zns", "sms_brandname", "twilio"}, sender) && !budgetApproved {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s paid senders require AUTH_DELIVERY_BUDGET_APPROVED=true", key))
	}
}

var authCountryPattern = regexp.MustCompile(`^[A-Z]{2}$`)

func authPhoneCountries(lookup lookupEnv, validationErrors *[]error) []string {
	entries := strings.Split(valueOrDefault(lookup, "AUTH_PHONE_ALLOWED_COUNTRIES", "VN"), ",")
	countries := make([]string, 0, len(entries))
	for _, entry := range entries {
		country := strings.ToUpper(strings.TrimSpace(entry))
		if !authCountryPattern.MatchString(country) || containsString(countries, country) {
			*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_PHONE_ALLOWED_COUNTRIES must contain distinct two-letter country codes"))
			continue
		}
		countries = append(countries, country)
	}
	return countries
}
