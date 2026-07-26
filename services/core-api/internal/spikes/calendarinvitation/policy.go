package calendarinvitation

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// RenderPolicy is trusted application configuration, not recipient data. It
// prevents persisted or attacker-controlled snapshots from selecting an
// arbitrary CTA origin when MIME/iCalendar bytes are rendered.
type RenderPolicy struct {
	allowedOrigins      map[string]struct{}
	timeZoneDataVersion string
}

// NewRenderPolicy creates an immutable HTTPS-origin allowlist. Each value must
// be an origin only; paths, credentials, query strings and fragments are
// rejected.
func NewRenderPolicy(
	timeZoneDataVersion string,
	origins ...string,
) (RenderPolicy, error) {
	if err := validateBoundedSingleLine(
		"time zone data version",
		timeZoneDataVersion,
		1,
		64,
	); err != nil {
		return RenderPolicy{}, err
	}
	if len(origins) == 0 {
		return RenderPolicy{}, fmt.Errorf(
			"%w: at least one trusted HTTPS origin is required",
			ErrInvalidSnapshot,
		)
	}
	allowed := make(map[string]struct{}, len(origins))
	for _, rawOrigin := range origins {
		parsed, err := url.Parse(rawOrigin)
		if err != nil ||
			parsed.Scheme != "https" ||
			parsed.Host == "" ||
			parsed.User != nil ||
			(parsed.Path != "" && parsed.Path != "/") ||
			parsed.RawQuery != "" ||
			parsed.Fragment != "" ||
			parsed.RawFragment != "" {
			return RenderPolicy{}, fmt.Errorf(
				"%w: invalid trusted HTTPS origin",
				ErrInvalidSnapshot,
			)
		}
		origin, err := canonicalHTTPSOrigin(parsed)
		if err != nil {
			return RenderPolicy{}, err
		}
		allowed[origin] = struct{}{}
	}
	return RenderPolicy{
		allowedOrigins:      allowed,
		timeZoneDataVersion: timeZoneDataVersion,
	}, nil
}

func (policy RenderPolicy) validateSnapshot(snapshot Snapshot) error {
	if policy.timeZoneDataVersion == "" ||
		snapshot.TimeZoneDataVersion != policy.timeZoneDataVersion {
		return fmt.Errorf(
			"%w: snapshot time-zone data version does not match render policy",
			ErrInvalidSnapshot,
		)
	}
	return policy.validateDeepLink(snapshot.DeepLink)
}

func (policy RenderPolicy) validateDeepLink(rawURL string) error {
	if len(policy.allowedOrigins) == 0 {
		return fmt.Errorf("%w: render policy is not configured", ErrInvalidSnapshot)
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Opaque != "" {
		return fmt.Errorf("%w: deep link must be an HTTPS URL", ErrInvalidSnapshot)
	}
	origin, err := canonicalHTTPSOrigin(parsed)
	if err != nil {
		return err
	}
	if _, allowed := policy.allowedOrigins[origin]; !allowed {
		return fmt.Errorf("%w: deep-link origin is not allowlisted", ErrInvalidSnapshot)
	}
	return nil
}

func canonicalHTTPSOrigin(parsed *url.URL) (string, error) {
	if parsed == nil || parsed.Scheme != "https" {
		return "", fmt.Errorf("%w: invalid HTTPS origin", ErrInvalidSnapshot)
	}
	hostname := strings.ToLower(parsed.Hostname())
	if hostname == "" || !isASCII(hostname) || strings.HasSuffix(hostname, ".") {
		return "", fmt.Errorf("%w: invalid HTTPS hostname", ErrInvalidSnapshot)
	}
	port := parsed.Port()
	if port == "443" {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	}
	return "https://" + host, nil
}

func isASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] > 0x7f {
			return false
		}
	}
	return true
}
