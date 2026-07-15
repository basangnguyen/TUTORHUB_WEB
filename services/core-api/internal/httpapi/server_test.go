package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/platform/observability"
)

var fixedTime = time.Date(2026, time.July, 13, 4, 5, 6, 0, time.UTC)

func TestHealth(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(Options{})
	response := performRequest(handler, http.MethodGet, "/health", "")

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var payload healthResponse
	decodeJSON(t, response, &payload)
	if payload.Status != "ok" ||
		payload.Service != serviceName ||
		payload.Environment != "test" ||
		payload.Timestamp != fixedTime.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected response: %+v", payload)
	}
	if response.Header().Get(requestIDHeader) == "" {
		t.Fatal("expected X-Request-ID header")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store cache policy, got %q", response.Header().Get("Cache-Control"))
	}
}

func TestAPIStatusIsVersioned(t *testing.T) {
	t.Parallel()

	response := performRequest(newTestHandler(Options{}), http.MethodGet, "/api/v1/status", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var payload apiStatusResponse
	decodeJSON(t, response, &payload)
	if payload.Version != serviceVersion || payload.Service != serviceName {
		t.Fatalf("unexpected API status: %+v", payload)
	}
}

func TestReadiness(t *testing.T) {
	t.Parallel()

	response := performRequest(newTestHandler(Options{}), http.MethodGet, "/ready", "")
	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}

	var payload readinessResponse
	decodeJSON(t, response, &payload)
	if payload.Status != "ready" || payload.Checks == nil || len(payload.Checks) != 0 {
		t.Fatalf("unexpected readiness response: %+v", payload)
	}
}

func TestReadinessReportsUnavailableDependency(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(Options{
		Readiness: []ReadinessCheck{failingReadinessCheck{}},
	})
	response := performRequest(handler, http.MethodGet, "/ready", "req-ready")

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, response.Code)
	}

	var payload readinessResponse
	decodeJSON(t, response, &payload)
	if payload.Status != "not_ready" ||
		len(payload.Checks) != 1 ||
		payload.Checks[0].Name != "database" ||
		payload.Checks[0].Status != "not_ready" {
		t.Fatalf("unexpected readiness response: %+v", payload)
	}
}

func TestProblemResponses(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		method        string
		path          string
		expectedCode  int
		expectedTitle string
		expectedAllow string
	}{
		{
			name:          "not found",
			method:        http.MethodGet,
			path:          "/does-not-exist",
			expectedCode:  http.StatusNotFound,
			expectedTitle: "Resource not found",
		},
		{
			name:          "method not allowed",
			method:        http.MethodPost,
			path:          "/health",
			expectedCode:  http.StatusMethodNotAllowed,
			expectedTitle: "Method not allowed",
			expectedAllow: http.MethodGet,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			response := performRequest(
				newTestHandler(Options{}),
				testCase.method,
				testCase.path,
				"req-problem",
			)
			if response.Code != testCase.expectedCode {
				t.Fatalf("expected status %d, got %d", testCase.expectedCode, response.Code)
			}
			if !strings.Contains(response.Header().Get("Content-Type"), "application/problem+json") {
				t.Fatalf("unexpected content type: %q", response.Header().Get("Content-Type"))
			}
			if response.Header().Get("Allow") != testCase.expectedAllow {
				t.Fatalf("expected Allow %q, got %q", testCase.expectedAllow, response.Header().Get("Allow"))
			}

			var payload Problem
			decodeJSON(t, response, &payload)
			if payload.Status != testCase.expectedCode ||
				payload.Title != testCase.expectedTitle ||
				payload.RequestID != "req-problem" ||
				payload.Instance != testCase.path {
				t.Fatalf("unexpected problem response: %+v", payload)
			}
		})
	}
}

func TestRequestIDAcceptsSafeValueAndReplacesUnsafeValue(t *testing.T) {
	t.Parallel()

	handler := newTestHandler(Options{})
	valid := performRequest(handler, http.MethodGet, "/health", "request.safe-123")
	if valid.Header().Get(requestIDHeader) != "request.safe-123" {
		t.Fatalf("expected safe request ID to be preserved, got %q", valid.Header().Get(requestIDHeader))
	}

	invalid := performRequest(handler, http.MethodGet, "/health", "../../unsafe id")
	replacement := invalid.Header().Get(requestIDHeader)
	if replacement == "" || replacement == "../../unsafe id" || !validRequestID.MatchString(replacement) {
		t.Fatalf("expected generated safe request ID, got %q", replacement)
	}
}

func TestPanicRecoveryReturnsProblemAndRecordsMetric(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := middlewareStack(
		logger,
		metrics,
		observability.NoopTracer{},
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("sensitive implementation detail")
		}),
	)

	response := performRequest(handler, http.MethodGet, "/panic", "req-panic")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, response.Code)
	}
	if strings.Contains(response.Body.String(), "sensitive implementation detail") {
		t.Fatal("panic detail must not be returned to clients")
	}

	var payload Problem
	decodeJSON(t, response, &payload)
	if payload.RequestID != "req-panic" || payload.Status != http.StatusInternalServerError {
		t.Fatalf("unexpected panic response: %+v", payload)
	}

	snapshot := metrics.Snapshot()
	if snapshot.PanicsTotal != 1 || snapshot.Responses[4] != 1 {
		t.Fatalf("unexpected panic metrics: %+v", snapshot)
	}
}

func TestPanicRecoveryDoesNotAppendProblemAfterHeaders(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := middlewareStack(
		logger,
		metrics,
		observability.NoopTracer{},
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
			panic("after headers")
		}),
	)

	response := performRequest(handler, http.MethodGet, "/panic-after-write", "")
	if response.Code != http.StatusNoContent {
		t.Fatalf("expected original status %d, got %d", http.StatusNoContent, response.Code)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("expected no appended body, got %q", response.Body.String())
	}
	if metrics.Snapshot().PanicsTotal != 1 {
		t.Fatal("expected panic counter to increment")
	}
}

func TestMetricsEndpointExposesRequestCounters(t *testing.T) {
	t.Parallel()

	metrics := observability.NewMetrics()
	handler := newTestHandler(Options{Metrics: metrics})
	performRequest(handler, http.MethodGet, "/health", "")
	response := performRequest(handler, http.MethodGet, "/metrics", "")

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if !strings.Contains(response.Body.String(), "tutorhub_http_requests_total 2") {
		t.Fatalf("unexpected metrics response: %q", response.Body.String())
	}
}

type failingReadinessCheck struct{}

func (failingReadinessCheck) Name() string {
	return "database"
}

func (failingReadinessCheck) Check(context.Context) error {
	return errors.New("database unavailable")
}

func newTestHandler(options Options) http.Handler {
	options.Clock = func() time.Time { return fixedTime }
	return NewHandlerWithOptions(
		config.Config{Environment: "test", Port: "8080"},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		options,
	)
}

func performRequest(handler http.Handler, method string, path string, requestID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if requestID != "" {
		request.Header.Set(requestIDHeader, requestID)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, destination any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}
