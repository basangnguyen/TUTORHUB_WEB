package httpapi

import (
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/devinbox"
)

type authProvidersResponse struct {
	SignupEnabled         bool     `json:"signup_enabled"`
	PasswordEnabled       bool     `json:"password_enabled"`
	EmailCodeEnabled      bool     `json:"email_code_enabled"`
	PhoneEnabled          bool     `json:"phone_enabled"`
	PasskeysEnabled       bool     `json:"passkeys_enabled"`
	MFATOTPEnabled        bool     `json:"mfa_totp_enabled"`
	OIDCDefaultEnabled    bool     `json:"oidc_default_enabled"`
	SSODomainsEnabled     bool     `json:"sso_domains_enabled"`
	OAuthProviders        []string `json:"oauth_providers"`
	PasswordMinLength     int      `json:"password_min_length"`
	PhoneAllowedCountries []string `json:"phone_allowed_countries"`
}

type authIdentifierRequest struct {
	Identifier string `json:"identifier"`
}

type authIdentifierResponse struct {
	Kind string `json:"kind"`
	SSO  struct {
		Required bool `json:"required"`
	} `json:"sso"`
}

// preAuthGuard applies the browser boundary before any pre-session mutation.
// Every wrapped handler must use decodeJSONRequest for its endpoint-specific schema.
func preAuthGuard(webOrigin string, next http.Handler) http.Handler {
	return authFoundationResponseHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if webOrigin == "" || r.Header.Get("Origin") != webOrigin || len(r.Header.Values("Origin")) != 1 {
			writeProblem(w, r, http.StatusForbidden, "Forbidden", "The request origin is not allowed.")
			return
		}
		if values := r.Header.Values("Sec-Fetch-Site"); len(values) > 0 && (len(values) != 1 || values[0] != "same-origin") {
			writeProblem(w, r, http.StatusForbidden, "Forbidden", "The request must originate from this site.")
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" || len(r.Header.Values("Content-Type")) != 1 {
			writeProblem(w, r, http.StatusUnsupportedMediaType, "Unsupported media type", "Content-Type must be application/json.")
			return
		}
		next.ServeHTTP(w, r)
	}))
}

func authFoundationResponseHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func authProvidersHandler(cfg config.AuthenticationConfig, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providers := make([]string, 0, 4)
		for _, provider := range []struct {
			slug    string
			enabled bool
		}{{"google", cfg.ProviderGoogleEnabled}, {"facebook", cfg.ProviderFacebookEnabled}, {"microsoft", cfg.ProviderMicrosoftEnabled}, {"apple", cfg.ProviderAppleEnabled}} {
			if provider.enabled {
				providers = append(providers, provider.slug)
			}
		}
		countries := append([]string{}, cfg.PhoneAllowedCountries...)
		writeJSON(logger, w, http.StatusOK, authProvidersResponse{
			SignupEnabled: cfg.SignupEnabled, PasswordEnabled: cfg.PasswordEnabled,
			EmailCodeEnabled: cfg.EmailCodeEnabled, PhoneEnabled: cfg.PhoneEnabled,
			PasskeysEnabled: cfg.PasskeysEnabled, MFATOTPEnabled: cfg.MFATOTPEnabled,
			OIDCDefaultEnabled: cfg.ProviderOIDCDefaultEnabled && cfg.Enabled,
			SSODomainsEnabled:  cfg.SSODomainsEnabled, OAuthProviders: providers,
			PasswordMinLength: cfg.PasswordMinLength, PhoneAllowedCountries: countries,
		})
	})
}

func authIdentifierHandler(logger *slog.Logger, limiter InvitationRateLimiter, clock func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		prefix := identity.IPPrefix(r.RemoteAddr)
		if prefix == "" {
			prefix = "unknown"
		}
		decision := limiter.Allow(r.Context(), AuthRateLimitDiscoveryIP, prefix, clock())
		if decision.Err != nil {
			writeProblem(w, r, http.StatusServiceUnavailable, "Service unavailable", "Authentication discovery is temporarily unavailable.")
			return
		}
		if !decision.Allowed {
			w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter))
			writeProblem(w, r, http.StatusTooManyRequests, "Too many requests", "Please wait before trying again.")
			return
		}
		var input authIdentifierRequest
		if err := decodeJSONRequest(w, r, &input, 2048); err != nil || len(input.Identifier) == 0 || len(input.Identifier) > 320 {
			writeProblem(w, r, http.StatusBadRequest, "Invalid request", "Provide one identifier of at most 320 bytes.")
			return
		}
		// Deliberately no account lookup, echo or hint: syntax classification reveals
		// neither account existence nor an individual's enrolled login methods.
		writeJSON(logger, w, http.StatusOK, authIdentifierResponse{Kind: classifyAuthIdentifier(input.Identifier)})
	})
}

func classifyAuthIdentifier(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 254 && !strings.ContainsAny(value, "\r\n") {
		address, err := mail.ParseAddress(value)
		if err == nil && address.Address == value && address.Name == "" && strings.Contains(value, "@") {
			return "email"
		}
	}
	// AUTH-01 is syntax-only. Country-aware E.164 normalization belongs to AUTH-10.
	digits := 0
	for index, character := range value {
		if character >= '0' && character <= '9' {
			digits++
			continue
		}
		if character == '+' && index == 0 || character == ' ' || character == '-' || character == '(' || character == ')' {
			continue
		}
		return "invalid"
	}
	if digits >= 8 && digits <= 15 {
		return "phone"
	}
	return "invalid"
}

func devAuthInboxHandler(cfg config.Config, logger *slog.Logger, inbox *devinbox.Inbox) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _ := r.Context().Value(directPeerContextKey{}).(string)
		host, _, err := net.SplitHostPort(peer)
		if err != nil {
			host = peer
		}
		if (cfg.Environment != "development" && cfg.Environment != "test") || inbox == nil ||
			(cfg.Authentication.EmailSender != "devinbox" && cfg.Authentication.PhoneSender != "devinbox") ||
			(host != "127.0.0.1" && host != "::1") {
			notFoundHandler().ServeHTTP(w, r)
			return
		}
		requireMethod(http.MethodGet, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(logger, w, http.StatusOK, struct {
				Items []devinbox.Message `json:"items"`
			}{Items: inbox.List()})
		})).ServeHTTP(w, r)
	})
}
