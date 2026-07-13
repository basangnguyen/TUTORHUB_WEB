package config

import (
	"fmt"
	"os"
)

const defaultPort = "8080"

type Config struct {
	Environment string
	Port        string
	WebOrigin   string
}

func Load() (Config, error) {
	cfg := Config{
		Environment: valueOrDefault("APP_ENV", "development"),
		Port:        valueOrDefault("PORT", defaultPort),
		WebOrigin:   valueOrDefault("PUBLIC_WEB_ORIGIN", "http://localhost:5173"),
	}

	if cfg.Port == "" {
		return Config{}, fmt.Errorf("PORT must not be empty")
	}

	return cfg, nil
}

func valueOrDefault(key string, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
