package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/httpapi"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
	"github.com/tutorhub-v2/core-api/internal/platform/database"
	"github.com/tutorhub-v2/core-api/internal/platform/httpserver"
	"github.com/tutorhub-v2/core-api/internal/platform/observability"
)

func main() {
	os.Exit(run())
}

func run() int {
	bootstrapLogger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("invalid configuration", "error", err)
		return 1
	}

	logger, err := observability.NewLogger(os.Stdout, cfg.LogLevel)
	if err != nil {
		bootstrapLogger.Error("create logger", "error", err)
		return 1
	}
	logger = logger.With(
		"service", "tutorhub-core-api",
		"environment", cfg.Environment,
	)

	metrics := observability.NewMetrics()
	readiness := make([]httpapi.ReadinessCheck, 0, 1)
	var pool *pgxpool.Pool
	if cfg.Database.PoolURL == "" {
		logger.Warn("database is not configured; readiness will fail")
		readiness = append(readiness, database.UnconfiguredReadinessCheck{})
	} else {
		pool, err = database.Open(context.Background(), cfg.Database)
		if err != nil {
			logger.Error("open database pool", "error", err)
			return 1
		}
		defer pool.Close()
		readiness = append(
			readiness,
			database.NewReadinessCheck(pool, cfg.Database.QueryTimeout),
		)
	}

	var identityService identity.ServiceAPI
	if cfg.Authentication.Enabled {
		if pool == nil {
			logger.Error("authentication requires a configured database")
			return 1
		}
		crypto, err := identity.NewCrypto(cfg.Authentication.SessionKey)
		if err != nil {
			logger.Error("initialize identity cryptography", "error", err)
			return 1
		}
		discoveryContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		provider, err := identity.NewOIDCProvider(discoveryContext, identity.OIDCProviderConfig{
			IssuerURL:     cfg.Authentication.IssuerURL,
			ClientID:      cfg.Authentication.ClientID,
			ClientSecret:  cfg.Authentication.ClientSecret,
			CallbackURL:   cfg.Authentication.CallbackURL,
			PostLogoutURL: cfg.Authentication.PostLogoutURL,
			Scopes:        cfg.Authentication.Scopes,
			HTTPTimeout:   10 * time.Second,
		})
		cancel()
		if err != nil {
			logger.Error("initialize OIDC provider", "error", err)
			return 1
		}
		identityService, err = identity.NewService(
			identity.NewPostgresRepository(pool, cfg.Database.QueryTimeout),
			provider,
			crypto,
			identity.ServiceConfig{
				FlowTTL:            cfg.Authentication.FlowTTL,
				SessionTTL:         cfg.Authentication.SessionTTL,
				SessionAbsoluteTTL: cfg.Authentication.SessionAbsoluteTTL,
			},
			time.Now,
		)
		if err != nil {
			logger.Error("initialize identity service", "error", err)
			return 1
		}
		logger.Info("authentication initialized", "issuer", cfg.Authentication.IssuerURL)
	} else {
		logger.Info("authentication is disabled for this environment")
	}

	handler := httpapi.NewHandlerWithOptions(cfg, logger, httpapi.Options{
		Metrics:   metrics,
		Tracer:    observability.NoopTracer{},
		Readiness: readiness,
		Identity:  identityService,
	})
	server := httpserver.New(cfg, handler)

	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		logger.Error("listen for HTTP", "address", server.Addr, "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := httpserver.Run(ctx, server, listener, logger, cfg.ShutdownTimeout); err != nil {
		logger.Error("core API stopped with error", "error", err)
		return 1
	}

	return 0
}
