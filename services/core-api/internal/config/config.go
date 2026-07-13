package config

import (
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
	defaultPort              = "8080"
	defaultWebOrigin         = "http://localhost:5173"
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultWriteTimeout      = 30 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultShutdownTimeout   = 10 * time.Second
	defaultMaxHeaderBytes    = 1 << 20
	defaultDBMaxConnections  = 4
	defaultDBMinConnections  = 0
	defaultDBConnectTimeout  = 10 * time.Second
	defaultDBQueryTimeout    = 5 * time.Second
	defaultDBMaxLifetime     = 30 * time.Minute
	defaultDBMaxIdleTime     = 5 * time.Minute
	defaultDBHealthPeriod    = time.Minute
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
	LogLevel          string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	MaxHeaderBytes    int
	Database          DatabaseConfig
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

	if err := errors.Join(validationErrors...); err != nil {
		return Config{}, fmt.Errorf("validate configuration: %w", err)
	}

	return cfg, nil
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

	maximum := intValue(
		lookup,
		"DATABASE_MAX_CONNECTIONS",
		defaultDBMaxConnections,
		1,
		100,
		validationErrors,
	)
	minimum := intValue(
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
		MaxConnections: int32(maximum),
		MinConnections: int32(minimum),
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
	origin, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("PUBLIC_WEB_ORIGIN must be a valid URL: %w", err)
	}

	if origin.Scheme != "http" && origin.Scheme != "https" {
		return fmt.Errorf("PUBLIC_WEB_ORIGIN must use http or https")
	}
	if origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" {
		return fmt.Errorf("PUBLIC_WEB_ORIGIN must contain only scheme and host")
	}
	if origin.Path != "" && origin.Path != "/" {
		return fmt.Errorf("PUBLIC_WEB_ORIGIN must not contain a path")
	}
	if (environment == "staging" || environment == "production") && origin.Scheme != "https" {
		return fmt.Errorf("PUBLIC_WEB_ORIGIN must use https in %s", environment)
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

func valueOrDefault(lookup lookupEnv, key string, fallback string) string {
	if value, ok := lookup(key); ok {
		return value
	}

	return fallback
}
