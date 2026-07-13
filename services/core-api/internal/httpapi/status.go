package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
)

const (
	serviceName    = "tutorhub-core-api"
	serviceVersion = "0.0.1"
)

type healthResponse struct {
	Status      string `json:"status"`
	Service     string `json:"service"`
	Environment string `json:"environment"`
	Timestamp   string `json:"timestamp"`
}

type apiStatusResponse struct {
	Status      string `json:"status"`
	Service     string `json:"service"`
	Version     string `json:"version"`
	Environment string `json:"environment"`
	Timestamp   string `json:"timestamp"`
}

type livenessResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

type readinessResponse struct {
	Status    string                 `json:"status"`
	Checks    []readinessCheckResult `json:"checks"`
	Timestamp string                 `json:"timestamp"`
}

type readinessCheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

func healthHandler(
	cfg config.Config,
	logger *slog.Logger,
	clock func() time.Time,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		setOperationalHeaders(w)
		writeJSON(logger, w, http.StatusOK, healthResponse{
			Status:      "ok",
			Service:     serviceName,
			Environment: cfg.Environment,
			Timestamp:   timestamp(clock),
		})
	})
}

func apiStatusHandler(
	cfg config.Config,
	logger *slog.Logger,
	clock func() time.Time,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		setOperationalHeaders(w)
		writeJSON(logger, w, http.StatusOK, apiStatusResponse{
			Status:      "ok",
			Service:     serviceName,
			Version:     serviceVersion,
			Environment: cfg.Environment,
			Timestamp:   timestamp(clock),
		})
	})
}

func livenessHandler(logger *slog.Logger, clock func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		setOperationalHeaders(w)
		writeJSON(logger, w, http.StatusOK, livenessResponse{
			Status:    "live",
			Timestamp: timestamp(clock),
		})
	})
}

func readinessHandler(
	logger *slog.Logger,
	clock func() time.Time,
	checks []ReadinessCheck,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		statusCode := http.StatusOK
		status := "ready"
		results := make([]readinessCheckResult, 0, len(checks))

		for _, check := range checks {
			result := readinessCheckResult{Name: check.Name(), Status: "ready"}
			if err := check.Check(r.Context()); err != nil {
				statusCode = http.StatusServiceUnavailable
				status = "not_ready"
				result.Status = "not_ready"
				logger.Warn(
					"readiness check failed",
					"check", check.Name(),
					"request_id", RequestIDFromContext(r.Context()),
					"error", err,
				)
			}
			results = append(results, result)
		}

		setOperationalHeaders(w)
		writeJSON(logger, w, statusCode, readinessResponse{
			Status:    status,
			Checks:    results,
			Timestamp: timestamp(clock),
		})
	})
}

func setOperationalHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func timestamp(clock func() time.Time) string {
	return clock().UTC().Format(time.RFC3339Nano)
}
