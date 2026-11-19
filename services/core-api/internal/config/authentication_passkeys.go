package config

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"golang.org/x/net/publicsuffix"
)

var passkeyDNSName = regexp.MustCompile(`^[a-z0-9.-]+$`)

func configurePasskeys(cfg *AuthenticationConfig, lookup lookupEnv, environment, webOrigin string, failures *[]error) {
	if !cfg.PasskeysEnabled {
		return
	}
	defaultID := ""
	if parsed, err := url.Parse(webOrigin); err == nil {
		defaultID = parsed.Hostname()
	}
	cfg.WebAuthnRPID = strings.ToLower(strings.TrimSpace(valueOrDefault(lookup, "WEBAUTHN_RP_ID", defaultID)))
	cfg.WebAuthnRPName = strings.TrimSpace(valueOrDefault(lookup, "WEBAUTHN_RP_NAME", "TutorHub"))
	cfg.webAuthnLocalHTTP = environment == "development" || environment == "test"
	var origins []string
	for _, origin := range strings.Split(valueOrDefault(lookup, "WEBAUTHN_RP_ORIGINS", webOrigin), ",") {
		canonical, err := cfg.passkeyOrigin(strings.TrimSpace(origin))
		if err != nil {
			*failures = append(*failures, err)
			return
		}
		origins = append(origins, canonical)
	}
	cfg.WebAuthnRPOrigins = origins
	if err := cfg.validatePasskeyRP(); err != nil {
		*failures = append(*failures, err)
	}
}

func (cfg AuthenticationConfig) PasskeysAvailable() bool {
	return cfg.PasskeysEnabled && cfg.validatePasskeyRP() == nil
}

func (cfg AuthenticationConfig) validatePasskeyRP() error {
	if cfg.WebAuthnRPName == "" || !utf8.ValidString(cfg.WebAuthnRPName) || strings.IndexFunc(cfg.WebAuthnRPName, unicode.IsControl) >= 0 {
		return errors.New("WEBAUTHN_RP_NAME must be a nonempty display name")
	}
	rpID := cfg.WebAuthnRPID
	if !passkeyDNSName.MatchString(rpID) || protocol.ValidateRPID(rpID) != nil {
		return errors.New("WEBAUTHN_RP_ID must be a valid domain")
	}
	if rpID != "localhost" {
		if _, err := publicsuffix.EffectiveTLDPlusOne(rpID); err != nil {
			return errors.New("WEBAUTHN_RP_ID must not be a public suffix")
		}
	}
	if len(cfg.WebAuthnRPOrigins) == 0 {
		return errors.New("WEBAUTHN_RP_ORIGINS requires at least one browser origin")
	}
	for _, origin := range cfg.WebAuthnRPOrigins {
		canonical, err := cfg.passkeyOrigin(origin)
		if err != nil || canonical != origin {
			return errors.New("WEBAUTHN_RP_ORIGINS requires valid browser origins")
		}
	}
	return nil
}

// passkeyOrigin returns the browser's serialized scheme, host and optional port.
func (cfg AuthenticationConfig) passkeyOrigin(origin string) (string, error) {
	u, err := url.Parse(origin)
	if err != nil || u.User != nil || u.Host == "" || u.Path != "" && u.Path != "/" ||
		strings.ContainsAny(origin, "?#[]") || strings.HasSuffix(u.Host, ":") {
		return "", errors.New("WEBAUTHN_RP_ORIGINS must contain only browser origins")
	}
	host := strings.ToLower(u.Hostname())
	if !passkeyDNSName.MatchString(host) || protocol.ValidateRPID(host) != nil ||
		!(host == cfg.WebAuthnRPID || strings.HasSuffix(host, "."+cfg.WebAuthnRPID)) {
		return "", errors.New("WEBAUTHN_RP_ID must match every RP origin domain")
	}
	localHTTP := cfg.webAuthnLocalHTTP && (host == "localhost" || strings.HasSuffix(host, ".localhost"))
	if u.Scheme != "https" && !(u.Scheme == "http" && localHTTP) {
		return "", errors.New("WEBAUTHN_RP_ORIGINS requires HTTPS except localhost in development/test")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return "", errors.New("WEBAUTHN_RP_ORIGINS contains an invalid port")
		}
		if !(u.Scheme == "https" && n == 443 || u.Scheme == "http" && n == 80) {
			host += ":" + strconv.Itoa(int(n))
		}
	}
	return u.Scheme + "://" + host, nil
}
