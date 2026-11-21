package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/nyaruka/phonenumbers"
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
		"AUTH_REQUIRE_ADMIN_MFA":          &cfg.RequireAdminMFA,
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
	cfg.PhoneFallbackSender = strings.TrimSpace(valueOrDefault(lookup, "AUTH_PHONE_FALLBACK_SENDER", "disabled"))
	if cfg.PhoneFallbackSender == "" {
		cfg.PhoneFallbackSender = "disabled"
	}
	cfg.SMTPAddr = strings.TrimSpace(valueOrDefault(lookup, "AUTH_SMTP_ADDR", ""))
	approval := valueOrDefault(lookup, "AUTH_DELIVERY_BUDGET_APPROVED", "false")
	cfg.DeliveryBudgetApproved = approval == "true"
	if approval != "true" && approval != "false" {
		*validationErrors = append(*validationErrors, errors.New("AUTH_DELIVERY_BUDGET_APPROVED must be true or false"))
	}
	cfg.EmailDailyQuota = intValue(lookup, "AUTH_EMAIL_DAILY_QUOTA", 200, 0, 1_000_000, validationErrors)
	cfg.PhoneOTPDailyQuota = intValue(lookup, "AUTH_PHONE_OTP_DAILY_QUOTA", 50, 0, 1_000_000, validationErrors)
	cfg.PhoneCountryHourlyQuota = intValue(lookup, "AUTH_PHONE_COUNTRY_HOURLY_QUOTA", 100, 1, 1_000_000, validationErrors)
	cfg.PhoneBreakerMinSends = intValue(lookup, "AUTH_PHONE_BREAKER_MIN_SENDS", 50, 1, 100_000, validationErrors)
	cfg.PhoneBreakerMinConversionPct = intValue(lookup, "AUTH_PHONE_BREAKER_MIN_CONVERSION_PCT", 30, 0, 100, validationErrors)
	cfg.PhoneStaleAfter = durationValue(lookup, "AUTH_PHONE_STALE_AFTER", 1440*time.Hour, validationErrors)
	cfg.PhoneReclaimAfter = durationValue(lookup, "AUTH_PHONE_RECLAIM_AFTER", 2160*time.Hour, validationErrors)
	if cfg.PhoneStaleAfter < time.Hour {
		*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_PHONE_STALE_AFTER must be at least 1h"))
	}
	if cfg.PhoneReclaimAfter < cfg.PhoneStaleAfter {
		*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_PHONE_RECLAIM_AFTER must be at least AUTH_PHONE_STALE_AFTER"))
	}
	// Loopback E2E runs every auth spec from one IP in one database. Scaling the
	// per-IP buckets is a test-only aid; production always uses the base limits.
	cfg.E2EIPRateLimitMultiplier = intValue(lookup, "AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER", 1, 1, 100, validationErrors)

	development := environment == "development" || environment == "test"
	if cfg.E2EIPRateLimitMultiplier != 1 && environment != "test" {
		*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_E2E_IP_RATE_LIMIT_MULTIPLIER requires APP_ENV=test"))
	}
	// AUTH-12B: offline DNS TXT verification for loopback E2E only. Every
	// other environment must resolve school domains through public DNS.
	cfg.E2EDNSUDPAddr = strings.TrimSpace(valueOrDefault(lookup, "AUTH_E2E_DNS_UDP_ADDR", ""))
	if cfg.E2EDNSUDPAddr != "" {
		if environment != "test" {
			*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_E2E_DNS_UDP_ADDR requires APP_ENV=test"))
		} else if !validE2EDNSUDPAddr(cfg.E2EDNSUDPAddr) {
			*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_E2E_DNS_UDP_ADDR must be 127.0.0.1:<port>"))
		}
	}
	validateAuthSender("AUTH_EMAIL_SENDER", cfg.EmailSender, []string{"disabled", "devinbox", "smtp", "ses"}, development, cfg.DeliveryBudgetApproved, validationErrors)
	if cfg.PhoneSender == "" {
		*validationErrors = append(*validationErrors, errors.New("AUTH_PHONE_SENDER is not a supported sender"))
	}
	if err := cfg.ValidatePhoneRouting(environment); err != nil {
		*validationErrors = append(*validationErrors, err)
	}
	configurePhoneProviders(cfg, lookup, environment, validationErrors)
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

// validE2EDNSUDPAddr accepts exactly the IPv4 loopback address with an
// explicit, canonical non-zero port (no host names, IPv6 or wildcard binds).
func validE2EDNSUDPAddr(value string) bool {
	addr, err := netip.ParseAddrPort(value)
	return err == nil && addr.Addr() == netip.AddrFrom4([4]byte{127, 0, 0, 1}) &&
		addr.Port() != 0 && addr.String() == value
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
		if !authCountryPattern.MatchString(country) || phonenumbers.GetCountryCodeForRegion(country) == 0 || containsString(countries, country) {
			*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_PHONE_ALLOWED_COUNTRIES must contain distinct supported two-letter country codes"))
			continue
		}
		countries = append(countries, country)
	}
	return countries
}

// ValidatePhoneRouting also protects startup when the caller constructs Config directly.
func (cfg AuthenticationConfig) ValidatePhoneRouting(environment string) error {
	primary, fallback := cfg.PhoneSender, cfg.PhoneFallbackSender
	if primary == "" {
		primary = "disabled"
	}
	if fallback == "" {
		fallback = "disabled"
	}
	local := environment == "development" || environment == "test"
	var failures []error
	validateAuthSender("AUTH_PHONE_SENDER", primary, []string{"disabled", "devinbox", "zns", "sms_brandname", "twilio"}, local, cfg.DeliveryBudgetApproved, &failures)
	validateAuthSender("AUTH_PHONE_FALLBACK_SENDER", fallback, []string{"disabled", "sms_brandname", "twilio"}, local, cfg.DeliveryBudgetApproved, &failures)
	if fallback != "disabled" && (primary == fallback || !containsString([]string{"zns", "sms_brandname", "twilio"}, primary)) {
		failures = append(failures, errors.New("AUTH_PHONE_FALLBACK_SENDER requires a distinct paid AUTH_PHONE_SENDER"))
	}
	if primary != "disabled" {
		if len(cfg.PhoneAllowedCountries) == 0 {
			failures = append(failures, errors.New("AUTH_PHONE_ALLOWED_COUNTRIES requires at least one country"))
		}
		for _, country := range cfg.PhoneAllowedCountries {
			code := phonenumbers.GetCountryCodeForRegion(country)
			if code == 0 || (code != 84 && primary != "devinbox" && primary != "twilio" && fallback != "twilio") {
				failures = append(failures, errors.New("AUTH_PHONE_ALLOWED_COUNTRIES includes a country without a phone delivery route"))
				break
			}
		}
	}
	return errors.Join(failures...)
}

func configurePhoneProviders(cfg *AuthenticationConfig, lookup lookupEnv, environment string, failures *[]error) {
	get := func(key string) string { return valueOrDefault(lookup, key, "") }
	for _, sender := range []string{cfg.PhoneSender, cfg.PhoneFallbackSender} {
		var err error
		switch sender {
		case "zns":
			cfg.ZNS, err = (ZNSConfig{
				APIURL: get("AUTH_ZNS_API_URL"), AccessToken: get("AUTH_ZNS_ACCESS_TOKEN"),
				TemplateID: get("AUTH_ZNS_TEMPLATE_ID"), OTPParam: get("AUTH_ZNS_OTP_PARAM"),
			}).Normalize(environment)
		case "sms_brandname":
			cfg.SMSBrandname, err = (SMSBrandnameConfig{
				APIURL: get("AUTH_SMS_BRANDNAME_API_URL"), APIToken: get("AUTH_SMS_BRANDNAME_API_TOKEN"),
				Sender: get("AUTH_SMS_BRANDNAME_SENDER"), Template: get("AUTH_SMS_BRANDNAME_TEMPLATE"),
			}).Normalize(environment)
		case "twilio":
			cfg.Twilio, err = (TwilioConfig{
				APIBaseURL: get("AUTH_TWILIO_API_BASE_URL"), AccountSID: get("AUTH_TWILIO_ACCOUNT_SID"),
				AuthToken: get("AUTH_TWILIO_AUTH_TOKEN"), From: get("AUTH_TWILIO_FROM"), Template: get("AUTH_TWILIO_TEMPLATE"),
			}).Normalize(environment)
		}
		if err != nil {
			*failures = append(*failures, err)
		}
	}
}

var (
	otpParamPattern      = regexp.MustCompile(`^[a-zA-Z0-9_]{1,32}$`)
	smsSenderPattern     = regexp.MustCompile(`^[a-zA-Z0-9 ._-]{1,11}$`)
	twilioAccountPattern = regexp.MustCompile(`^AC[a-zA-Z0-9]{32}$`)
	twilioFromPattern    = regexp.MustCompile(`^(\+[1-9][0-9]{6,14}|MG[a-zA-Z0-9]{32})$`)
	providerDNSLabel     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

func (c ZNSConfig) Normalize(environment string) (ZNSConfig, error) {
	if c.APIURL == "" {
		c.APIURL = "https://business.openapi.zalo.me/message/template"
	}
	if c.OTPParam == "" {
		c.OTPParam = "otp"
	}
	switch {
	case !validPhoneProviderURL(c.APIURL, environment):
		return ZNSConfig{}, errors.New("AUTH_ZNS_API_URL is not a permitted provider endpoint")
	case !validPhoneToken(c.AccessToken):
		return ZNSConfig{}, errors.New("AUTH_ZNS_ACCESS_TOKEN must contain a valid token")
	case c.TemplateID == "" || !utf8.ValidString(c.TemplateID) || utf8.RuneCountInString(c.TemplateID) > 64 ||
		strings.IndexFunc(c.TemplateID, func(r rune) bool { return !unicode.IsPrint(r) || unicode.IsSpace(r) }) >= 0:
		return ZNSConfig{}, errors.New("AUTH_ZNS_TEMPLATE_ID must contain 1 to 64 printable non-space characters")
	case !otpParamPattern.MatchString(c.OTPParam):
		return ZNSConfig{}, errors.New("AUTH_ZNS_OTP_PARAM must be a valid parameter name")
	}
	return c, nil
}

func (c SMSBrandnameConfig) Normalize(environment string) (SMSBrandnameConfig, error) {
	if c.Template == "" {
		c.Template = "Ma xac minh TutorHub cua ban la {{code}}. Hieu luc 5 phut."
	}
	switch {
	case !validPhoneProviderURL(c.APIURL, environment):
		return SMSBrandnameConfig{}, errors.New("AUTH_SMS_BRANDNAME_API_URL is required and must be a permitted provider endpoint")
	case !validPhoneToken(c.APIToken):
		return SMSBrandnameConfig{}, errors.New("AUTH_SMS_BRANDNAME_API_TOKEN must contain a valid token")
	case strings.TrimSpace(c.Sender) == "" || !smsSenderPattern.MatchString(c.Sender):
		return SMSBrandnameConfig{}, errors.New("AUTH_SMS_BRANDNAME_SENDER must be a valid 1 to 11 character brandname")
	case !validSMSTemplateText(c.Template) || strings.Count(c.Template, "{{code}}") != 1 || strings.IndexFunc(c.Template, unicode.IsControl) >= 0:
		return SMSBrandnameConfig{}, errors.New("AUTH_SMS_BRANDNAME_TEMPLATE must be printable, at most 320 characters and contain {{code}} once")
	}
	return c, nil
}

func (c TwilioConfig) Normalize(environment string) (TwilioConfig, error) {
	if c.APIBaseURL == "" {
		c.APIBaseURL = "https://api.twilio.com"
	}
	if c.Template == "" {
		c.Template = "Your TutorHub verification code is {{code}}. Valid for 5 minutes."
	}
	switch {
	case !validPhoneProviderURL(c.APIBaseURL, environment):
		return TwilioConfig{}, errors.New("AUTH_TWILIO_API_BASE_URL is not a permitted provider endpoint")
	case !twilioAccountPattern.MatchString(c.AccountSID):
		return TwilioConfig{}, errors.New("AUTH_TWILIO_ACCOUNT_SID must be a valid account SID")
	case !validPhoneToken(c.AuthToken):
		return TwilioConfig{}, errors.New("AUTH_TWILIO_AUTH_TOKEN must contain a valid token")
	case !twilioFromPattern.MatchString(c.From):
		return TwilioConfig{}, errors.New("AUTH_TWILIO_FROM must be an E.164 number or messaging service SID")
	case !validSMSTemplateText(c.Template) || !strings.Contains(c.Template, "{{code}}"):
		return TwilioConfig{}, errors.New("AUTH_TWILIO_TEMPLATE must be valid UTF-8, at most 320 characters and contain {{code}}")
	}
	c.APIBaseURL = strings.TrimRight(c.APIBaseURL, "/")
	return c, nil
}

func validSMSTemplateText(template string) bool {
	return utf8.ValidString(template) && utf8.RuneCountInString(template) <= 320
}

func validPhoneToken(token string) bool {
	return token != "" && strings.IndexFunc(token, func(r rune) bool { return r < '!' || r > '~' }) < 0
}

func validPhoneProviderURL(raw, environment string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Hostname() == "" || strings.ContainsAny(raw, "?#") || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return false
		}
	}
	host := strings.ToLower(u.Hostname())
	if u.Scheme == "http" {
		local := environment == "development" || environment == "test"
		return local && u.Port() != "" && (host == "127.0.0.1" || host == "localhost") && !strings.HasPrefix(u.Host, "[")
	}
	host = strings.TrimSuffix(host, ".")
	if u.Scheme != "https" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		return PublicPhoneProviderIP(ip)
	}
	labels := strings.Split(host, ".")
	// A DNS top-level label starts with a letter. In particular, reject
	// ambiguous numeric host spellings before the platform resolver sees them.
	tld := labels[len(labels)-1]
	if strings.HasPrefix(u.Host, "[") || len(host) > 253 || len(labels) < 2 || tld == "" || tld[0] < 'a' || tld[0] > 'z' {
		return false
	}
	for _, label := range labels {
		if !providerDNSLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// IsGlobalUnicast includes several non-public IPv4 and IPv6 ranges. Deny
// special-use networks as well as IPv6 transition ranges that encapsulate IPv4.
var nonPublicProviderRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("3fff::/20"),
}
var providerIPv6Global = netip.MustParsePrefix("2000::/3")

// PublicPhoneProviderIP is shared by configuration validation and the dialer
// so DNS answers are held to the same standard as literal IP endpoints.
func PublicPhoneProviderIP(ip netip.Addr) bool {
	if ip.Zone() != "" {
		return false
	}
	ip = ip.Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || (ip.Is6() && !providerIPv6Global.Contains(ip)) {
		return false
	}
	for _, reserved := range nonPublicProviderRanges {
		if reserved.Contains(ip) {
			return false
		}
	}
	return true
}
