package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort               = "8080"
	defaultWebOrigin          = "http://localhost:5173"
	defaultAPIOrigin          = "http://localhost:8080"
	defaultReadHeaderTimeout  = 5 * time.Second
	defaultReadTimeout        = 15 * time.Second
	defaultWriteTimeout       = 30 * time.Second
	defaultIdleTimeout        = 60 * time.Second
	defaultShutdownTimeout    = 10 * time.Second
	defaultMaxHeaderBytes     = 1 << 20
	defaultDBMaxConnections   = 4
	defaultDBMinConnections   = 0
	defaultDBConnectTimeout   = 10 * time.Second
	defaultDBQueryTimeout     = 5 * time.Second
	defaultDBMaxLifetime      = 30 * time.Minute
	defaultDBMaxIdleTime      = 5 * time.Minute
	defaultDBHealthPeriod     = time.Minute
	defaultAuthFlowTTL        = 10 * time.Minute
	defaultSessionTTL         = 8 * time.Hour
	defaultSessionAbsoluteTTL = 24 * time.Hour
	defaultLiveKitTokenTTL    = 5 * time.Minute
)

var validEnvironments = map[string]struct{}{
	"development": {},
	"test":        {},
	"staging":     {},
	"production":  {},
}

var validLogLevels = map[string]struct{}{
	"debug": {},
	"info":  {},
	"warn":  {},
	"error": {},
}

type Config struct {
	Environment       string
	Port              string
	WebOrigin         string
	APIOrigin         string
	LogLevel          string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	Database          DatabaseConfig
	Authentication    AuthenticationConfig
	LiveKit           LiveKitConfig
}

type DatabaseConfig struct {
	PoolURL               string
	MaxConnections        int32
	MinConnections        int32
	ConnectTimeout        time.Duration
	QueryTimeout          time.Duration
	MaxConnectionLifetime time.Duration
	MaxConnectionIdleTime time.Duration
	HealthCheckPeriod     time.Duration
}

type AuthenticationConfig struct {
	Enabled            bool
	IssuerURL          string
	ClientID           string
	ClientSecret       string
	CallbackURL        string
	PostLogoutURL      string
	Scopes             []string
	SessionKey         []byte
	CookieSecure       bool
	FlowTTL            time.Duration
	SessionTTL         time.Duration
	SessionAbsoluteTTL time.Duration
}

type LiveKitConfig struct {
	Enabled   bool
	URL       string
	APIKey    string
	APISecret string
	TokenTTL  time.Duration
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func (cfg Config) Address() string {
	return net.JoinHostPort("", cfg.Port)
}

type lookupEnv func(string) (string, bool)

func load(lookup lookupEnv) (Config, error) {
	cfg := Config{
		Environment: strings.ToLower(strings.TrimSpace(valueOrDefault(lookup, "APP_ENV", "development"))),
		Port:        strings.TrimSpace(valueOrDefault(lookup, "PORT", defaultPort)),
		WebOrigin:   strings.TrimSpace(valueOrDefault(lookup, "PUBLIC_WEB_ORIGIN", defaultWebOrigin)),
		APIOrigin:   strings.TrimSpace(valueOrDefault(lookup, "PUBLIC_API_ORIGIN", defaultAPIOrigin)),
		LogLevel:    strings.ToLower(strings.TrimSpace(valueOrDefault(lookup, "LOG_LEVEL", "info"))),
	}

	var validationErrors []error
	if _, ok := validEnvironments[cfg.Environment]; !ok {
		validationErrors = append(validationErrors, fmt.Errorf(
			"APP_ENV must be one of development, test, staging, production",
		))
	}

	if err := validatePort(cfg.Port); err != nil {
		validationErrors = append(validationErrors, err)
	}

	if err := validateWebOrigin(cfg.Environment, cfg.WebOrigin); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if err := validateOrigin(cfg.Environment, "PUBLIC_API_ORIGIN", cfg.APIOrigin); err != nil {
		validationErrors = append(validationErrors, err)
	}

	if _, ok := validLogLevels[cfg.LogLevel]; !ok {
		validationErrors = append(validationErrors, fmt.Errorf(
			"LOG_LEVEL must be one of debug, info, warn, error",
		))
	}

	cfg.ReadHeaderTimeout = durationValue(
		lookup,
		"HTTP_READ_HEADER_TIMEOUT",
		defaultReadHeaderTimeout,
		&validationErrors,
	)
	cfg.ReadTimeout = durationValue(
		lookup,
		"HTTP_READ_TIMEOUT",
		defaultReadTimeout,
		&validationErrors,
	)
	cfg.WriteTimeout = durationValue(
		lookup,
		"HTTP_WRITE_TIMEOUT",
		defaultWriteTimeout,
		&validationErrors,
	)
	cfg.IdleTimeout = durationValue(
		lookup,
		"HTTP_IDLE_TIMEOUT",
		defaultIdleTimeout,
		&validationErrors,
	)
	cfg.ShutdownTimeout = durationValue(
		lookup,
		"HTTP_SHUTDOWN_TIMEOUT",
		defaultShutdownTimeout,
		&validationErrors,
	)
	cfg.MaxHeaderBytes = intValue(
		lookup,
		"HTTP_MAX_HEADER_BYTES",
		defaultMaxHeaderBytes,
		1024,
		16<<20,
		&validationErrors,
	)
	cfg.Database = databaseConfig(lookup, cfg.Environment, &validationErrors)
	cfg.Authentication = authenticationConfig(
		lookup,
		cfg.Environment,
		cfg.APIOrigin,
		cfg.WebOrigin,
		&validationErrors,
	)
	cfg.LiveKit = liveKitConfig(lookup, cfg.Environment, &validationErrors)

	if err := errors.Join(validationErrors...); err != nil {
		return Config{}, fmt.Errorf("validate configuration: %w", err)
	}

	return cfg, nil
}

func liveKitConfig(
	lookup lookupEnv,
	environment string,
	validationErrors *[]error,
) LiveKitConfig {
	liveKitURL := strings.TrimSpace(valueOrDefault(lookup, "LIVEKIT_URL", ""))
	apiKey := strings.TrimSpace(valueOrDefault(lookup, "LIVEKIT_API_KEY", ""))
	apiSecret := strings.TrimSpace(valueOrDefault(lookup, "LIVEKIT_API_SECRET", ""))
	tokenTTL := durationValue(
		lookup,
		"LIVEKIT_TOKEN_TTL",
		defaultLiveKitTokenTTL,
		validationErrors,
	)
	enabled := liveKitURL != "" || apiKey != "" || apiSecret != ""
	config := LiveKitConfig{
		Enabled:   enabled,
		URL:       liveKitURL,
		APIKey:    apiKey,
		APISecret: apiSecret,
		TokenTTL:  tokenTTL,
	}
	if !enabled {
		return config
	}

	for key, value := range map[string]string{
		"LIVEKIT_URL":        liveKitURL,
		"LIVEKIT_API_KEY":    apiKey,
		"LIVEKIT_API_SECRET": apiSecret,
	} {
		if value == "" {
			*validationErrors = append(
				*validationErrors,
				fmt.Errorf("%s is required when LiveKit is enabled", key),
			)
		}
	}
	if liveKitURL != "" {
		if err := validateLiveKitURL(environment, liveKitURL); err != nil {
			*validationErrors = append(*validationErrors, err)
		}
	}
	if tokenTTL < time.Minute || tokenTTL > 15*time.Minute {
		*validationErrors = append(
			*validationErrors,
			fmt.Errorf("LIVEKIT_TOKEN_TTL must be between 1m and 15m"),
		)
	}

	return config
}

func authenticationConfig(
	lookup lookupEnv,
	environment string,
	apiOrigin string,
	webOrigin string,
	validationErrors *[]error,
) AuthenticationConfig {
	issuerURL := strings.TrimSpace(valueOrDefault(lookup, "OIDC_ISSUER_URL", ""))
	clientID := strings.TrimSpace(valueOrDefault(lookup, "OIDC_CLIENT_ID", ""))
	clientSecret := strings.TrimSpace(valueOrDefault(lookup, "OIDC_CLIENT_SECRET", ""))
	sessionSecret := strings.TrimSpace(valueOrDefault(lookup, "SESSION_SECRET", ""))
	enabled := issuerURL != "" || clientID != "" || clientSecret != "" || sessionSecret != ""
	required := environment == "staging" || environment == "production"

	if required {
		enabled = true
	}

	callbackURL := strings.TrimSpace(valueOrDefault(
		lookup,
		"OIDC_CALLBACK_URL",
		strings.TrimRight(apiOrigin, "/")+"/api/v1/auth/callback",
	))
	postLogoutURL := strings.TrimSpace(valueOrDefault(
		lookup,
		"OIDC_POST_LOGOUT_URL",
		strings.TrimRight(webOrigin, "/")+"/signed-out",
	))
	scopes := parseScopes(valueOrDefault(lookup, "OIDC_SCOPES", "openid profile email"))
	cookieSecure := boolValue(
		lookup,
		"SESSION_COOKIE_SECURE",
		required,
		validationErrors,
	)
	flowTTL := durationValue(
		lookup,
		"AUTH_FLOW_TTL",
		defaultAuthFlowTTL,
		validationErrors,
	)
	sessionTTL := durationValue(
		lookup,
		"SESSION_TTL",
		defaultSessionTTL,
		validationErrors,
	)
	absoluteTTL := durationValue(
		lookup,
		"SESSION_ABSOLUTE_TTL",
		defaultSessionAbsoluteTTL,
		validationErrors,
	)

	config := AuthenticationConfig{
		Enabled:            enabled,
		IssuerURL:          issuerURL,
		ClientID:           clientID,
		ClientSecret:       clientSecret,
		CallbackURL:        callbackURL,
		PostLogoutURL:      postLogoutURL,
		Scopes:             scopes,
		CookieSecure:       cookieSecure,
		FlowTTL:            flowTTL,
		SessionTTL:         sessionTTL,
		SessionAbsoluteTTL: absoluteTTL,
	}

	if !enabled {
		return config
	}

	requiredValues := map[string]string{
		"OIDC_ISSUER_URL":    issuerURL,
		"OIDC_CLIENT_ID":     clientID,
		"OIDC_CLIENT_SECRET": clientSecret,
		"SESSION_SECRET":     sessionSecret,
	}
	for key, value := range requiredValues {
		if value == "" {
			*validationErrors = append(*validationErrors, fmt.Errorf("%s is required when authentication is enabled", key))
		}
	}

	if issuerURL != "" {
		if err := validateHTTPSURL(environment, "OIDC_ISSUER_URL", issuerURL); err != nil {
			*validationErrors = append(*validationErrors, err)
		}
	}
	if err := validateHTTPSURL(environment, "OIDC_CALLBACK_URL", callbackURL); err != nil {
		*validationErrors = append(*validationErrors, err)
	}
	if err := validateHTTPSURL(environment, "OIDC_POST_LOGOUT_URL", postLogoutURL); err != nil {
		*validationErrors = append(*validationErrors, err)
	}
	if !containsString(scopes, "openid") {
		*validationErrors = append(*validationErrors, fmt.Errorf("OIDC_SCOPES must include openid"))
	}
	if flowTTL > 15*time.Minute {
		*validationErrors = append(*validationErrors, fmt.Errorf("AUTH_FLOW_TTL must not exceed 15m"))
	}
	if sessionTTL > absoluteTTL {
		*validationErrors = append(*validationErrors, fmt.Errorf("SESSION_TTL must not exceed SESSION_ABSOLUTE_TTL"))
	}
	if absoluteTTL > 7*24*time.Hour {
		*validationErrors = append(*validationErrors, fmt.Errorf("SESSION_ABSOLUTE_TTL must not exceed 168h"))
	}
	if required && !cookieSecure {
		*validationErrors = append(*validationErrors, fmt.Errorf("SESSION_COOKIE_SECURE must be true in %s", environment))
	}

	if sessionSecret != "" {
		key, err := decodeSessionKey(sessionSecret)
		if err != nil {
			*validationErrors = append(*validationErrors, err)
		} else {
			config.SessionKey = key
		}
	}

	return config
}

func databaseConfig(
	lookup lookupEnv,
	environment string,
	validationErrors *[]error,
) DatabaseConfig {
	poolURL := strings.TrimSpace(valueOrDefault(lookup, "DATABASE_POOL_URL", ""))
	if poolURL == "" && (environment == "staging" || environment == "production") {
		*validationErrors = append(
			*validationErrors,
			fmt.Errorf("DATABASE_POOL_URL is required in %s", environment),
		)
	} else if poolURL != "" {
		if err := validateDatabaseURL(environment, poolURL); err != nil {
			*validationErrors = append(*validationErrors, err)
		}
	}

	maximum := int32Value(
		lookup,
		"DATABASE_MAX_CONNECTIONS",
		defaultDBMaxConnections,
		1,
		100,
		validationErrors,
	)
	minimum := int32Value(
		lookup,
		"DATABASE_MIN_CONNECTIONS",
		defaultDBMinConnections,
		0,
		100,
		validationErrors,
	)
	if minimum > maximum {
		*validationErrors = append(
			*validationErrors,
			fmt.Errorf("DATABASE_MIN_CONNECTIONS must not exceed DATABASE_MAX_CONNECTIONS"),
		)
	}

	return DatabaseConfig{
		PoolURL:        poolURL,
		MaxConnections: maximum,
		MinConnections: minimum,
		ConnectTimeout: durationValue(
			lookup,
			"DATABASE_CONNECT_TIMEOUT",
			defaultDBConnectTimeout,
			validationErrors,
		),
		QueryTimeout: durationValue(
			lookup,
			"DATABASE_QUERY_TIMEOUT",
			defaultDBQueryTimeout,
			validationErrors,
		),
		MaxConnectionLifetime: durationValue(
			lookup,
			"DATABASE_MAX_CONNECTION_LIFETIME",
			defaultDBMaxLifetime,
			validationErrors,
		),
		MaxConnectionIdleTime: durationValue(
			lookup,
			"DATABASE_MAX_CONNECTION_IDLE_TIME",
			defaultDBMaxIdleTime,
			validationErrors,
		),
		HealthCheckPeriod: durationValue(
			lookup,
			"DATABASE_HEALTH_CHECK_PERIOD",
			defaultDBHealthPeriod,
			validationErrors,
		),
	}
}

func validateDatabaseURL(environment string, value string) error {
	databaseURL, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("DATABASE_POOL_URL must be a valid PostgreSQL URL")
	}
	if databaseURL.Scheme != "postgres" && databaseURL.Scheme != "postgresql" {
		return fmt.Errorf("DATABASE_POOL_URL must use postgres or postgresql")
	}
	if databaseURL.Hostname() == "" || databaseURL.User == nil {
		return fmt.Errorf("DATABASE_POOL_URL must include host and credentials")
	}
	if databaseURL.Fragment != "" {
		return fmt.Errorf("DATABASE_POOL_URL must not include a fragment")
	}
	if environment == "staging" || environment == "production" {
		sslMode := databaseURL.Query().Get("sslmode")
		if sslMode != "require" && sslMode != "verify-full" {
			return fmt.Errorf("DATABASE_POOL_URL must require TLS in %s", environment)
		}
	}

	return nil
}

func validatePort(value string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("PORT must be a number between 1 and 65535")
	}

	return nil
}

func validateWebOrigin(environment string, value string) error {
	return validateOrigin(environment, "PUBLIC_WEB_ORIGIN", value)
}

func validateOrigin(environment string, key string, value string) error {
	origin, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL: %w", key, err)
	}

	if origin.Scheme != "http" && origin.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", key)
	}
	if origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" {
		return fmt.Errorf("%s must contain only scheme and host", key)
	}
	if origin.Path != "" && origin.Path != "/" {
		return fmt.Errorf("%s must not contain a path", key)
	}
	if (environment == "staging" || environment == "production") && origin.Scheme != "https" {
		return fmt.Errorf("%s must use https in %s", key, environment)
	}

	return nil
}

func validateHTTPSURL(environment string, key string, value string) error {
	parsedURL, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL: %w", key, err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", key)
	}
	if parsedURL.Host == "" || parsedURL.User != nil || parsedURL.Fragment != "" {
		return fmt.Errorf("%s must include a host and must not contain credentials or a fragment", key)
	}
	if (environment == "staging" || environment == "production") && parsedURL.Scheme != "https" {
		return fmt.Errorf("%s must use https in %s", key, environment)
	}

	return nil
}

func validateLiveKitURL(environment string, value string) error {
	parsedURL, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("LIVEKIT_URL must be a valid WebSocket URL")
	}
	if parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		return fmt.Errorf("LIVEKIT_URL must use ws or wss")
	}
	if parsedURL.Host == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return fmt.Errorf("LIVEKIT_URL must include only a WebSocket scheme and host")
	}
	if parsedURL.Path != "" && parsedURL.Path != "/" {
		return fmt.Errorf("LIVEKIT_URL must not contain a path")
	}
	if (environment == "staging" || environment == "production") && parsedURL.Scheme != "wss" {
		return fmt.Errorf("LIVEKIT_URL must use wss in %s", environment)
	}

	return nil
}

func durationValue(
	lookup lookupEnv,
	key string,
	fallback time.Duration,
	validationErrors *[]error,
) time.Duration {
	raw := strings.TrimSpace(valueOrDefault(lookup, key, fallback.String()))
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be a positive duration", key))
		return fallback
	}

	return value
}

func intValue(
	lookup lookupEnv,
	key string,
	fallback int,
	minimum int,
	maximum int,
	validationErrors *[]error,
) int {
	raw := strings.TrimSpace(valueOrDefault(lookup, key, strconv.Itoa(fallback)))
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		*validationErrors = append(*validationErrors, fmt.Errorf(
			"%s must be a number between %d and %d",
			key,
			minimum,
			maximum,
		))
		return fallback
	}

	return value
}

func int32Value(
	lookup lookupEnv,
	key string,
	fallback int32,
	minimum int32,
	maximum int32,
	validationErrors *[]error,
) int32 {
	raw := strings.TrimSpace(valueOrDefault(lookup, key, strconv.FormatInt(int64(fallback), 10)))
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < int64(minimum) || value > int64(maximum) {
		*validationErrors = append(*validationErrors, fmt.Errorf(
			"%s must be a number between %d and %d",
			key,
			minimum,
			maximum,
		))
		return fallback
	}

	return int32(value)
}

func boolValue(
	lookup lookupEnv,
	key string,
	fallback bool,
	validationErrors *[]error,
) bool {
	raw := strings.TrimSpace(valueOrDefault(lookup, key, strconv.FormatBool(fallback)))
	value, err := strconv.ParseBool(raw)
	if err != nil {
		*validationErrors = append(*validationErrors, fmt.Errorf("%s must be true or false", key))
		return fallback
	}

	return value
}

func parseScopes(value string) []string {
	seen := make(map[string]struct{})
	scopes := make([]string, 0)
	for _, scope := range strings.Fields(value) {
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		scopes = append(scopes, scope)
	}

	return scopes
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}

	return false
}

func decodeSessionKey(value string) ([]byte, error) {
	encodings := []*base64.Encoding{
		base64.RawURLEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.StdEncoding,
	}

	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(value)
		if err == nil {
			if len(decoded) < 32 {
				return nil, fmt.Errorf("SESSION_SECRET must decode to at least 32 bytes")
			}
			return decoded, nil
		}
	}

	return nil, fmt.Errorf("SESSION_SECRET must be valid base64 or base64url")
}

func valueOrDefault(lookup lookupEnv, key string, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}

	return fallback
}
