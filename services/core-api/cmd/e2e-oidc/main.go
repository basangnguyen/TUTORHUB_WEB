package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/e2eoidc"
)

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With(
		"service", "tutorhub-e2e-oidc",
	)
	config := e2eoidc.Config{
		Environment:   strings.TrimSpace(os.Getenv("APP_ENV")),
		ListenAddress: valueOrDefault("E2E_OIDC_ADDRESS", "127.0.0.1:9091"),
		IssuerURL:     valueOrDefault("OIDC_ISSUER_URL", "http://127.0.0.1:9091"),
		ClientID:      strings.TrimSpace(os.Getenv("OIDC_CLIENT_ID")),
		ClientSecret:  strings.TrimSpace(os.Getenv("OIDC_CLIENT_SECRET")),
		RedirectURL: valueOrDefault(
			"OIDC_CALLBACK_URL",
			"http://127.0.0.1:8080/api/v1/auth/callback",
		),
		PostLogoutURL: valueOrDefault(
			"OIDC_POST_LOGOUT_URL",
			"http://127.0.0.1:5173/signed-out",
		),
		// Only the AUTH-05 fake Google, AUTH-06 fake Facebook and AUTH-07 fake
		// Microsoft instances enable per-scenario accounts.
		AllowCustomAccounts: strings.TrimSpace(os.Getenv("E2E_OIDC_CUSTOM_ACCOUNTS")) == "true",
		Flavor:              strings.TrimSpace(os.Getenv("E2E_OIDC_FLAVOR")),
		Accounts:            defaultAccounts(strings.TrimSpace(os.Getenv("E2E_OIDC_FLAVOR"))),
	}
	provider, err := e2eoidc.New(config)
	if err != nil {
		logger.Error("invalid fake OIDC configuration", "error", err)
		return 1
	}

	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		logger.Error("listen", "address", config.ListenAddress, "error", err)
		return 1
	}
	server := &http.Server{
		Handler:           provider.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	logger.Info("fake OIDC ready", "issuer", config.IssuerURL)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("fake OIDC stopped with error", "error", err)
		return 1
	}
	return 0
}

// defaultAccounts returns the fixed chooser accounts. Facebook subjects are
// numeric app-scoped IDs, as TutorHub's Facebook adapter requires; Microsoft
// accounts carry a tenant, an object ID and the xms_edov claim.
func defaultAccounts(flavor string) []e2eoidc.Account {
	if flavor == e2eoidc.FlavorMicrosoft {
		domainVerified := true
		tenantID := e2eoidc.MicrosoftDefaultWorkTenantID
		objectID := "0b7f1d2e-3c4a-4b5d-8e6f-7a8b9c0d1e2f"
		return []e2eoidc.Account{{
			ID:             "microsoft-teacher",
			Subject:        e2eoidc.MicrosoftTestSubject(tenantID, objectID),
			Email:          "teacher.microsoft.e2e@tutorhub.local",
			DisplayName:    "E2E Microsoft Teacher",
			Locale:         "en",
			TenantID:       tenantID,
			ObjectID:       objectID,
			DomainVerified: &domainVerified,
		}}
	}
	if flavor == e2eoidc.FlavorFacebook {
		return []e2eoidc.Account{{
			ID:          "facebook-teacher",
			Subject:     "100000000000001",
			Email:       "teacher.facebook.e2e@tutorhub.local",
			DisplayName: "E2E Facebook Teacher",
			Locale:      "en",
		}}
	}
	return []e2eoidc.Account{
		{
			ID:          "admin",
			Subject:     "e2e-admin",
			Email:       "admin.e2e@tutorhub.local",
			DisplayName: "E2E Administrator",
			Locale:      "en",
		},
		{
			ID:          "student",
			Subject:     "e2e-student",
			Email:       "student.e2e@tutorhub.local",
			DisplayName: "E2E Student",
			Locale:      "en",
		},
		{
			ID:          "teacher",
			Subject:     "e2e-teacher",
			Email:       "teacher.e2e@tutorhub.local",
			DisplayName: "E2E Teacher",
			Locale:      "en",
		},
	}
}

func valueOrDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
