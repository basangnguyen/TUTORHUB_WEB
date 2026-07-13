package observability

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsRecordsHTTPActivity(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics()
	metrics.RequestStarted()
	metrics.RequestCompleted(http.StatusCreated, 25*time.Millisecond)
	metrics.PanicRecovered()

	snapshot := metrics.Snapshot()
	if snapshot.RequestsTotal != 1 || snapshot.RequestsInFlight != 0 {
		t.Fatalf("unexpected request counters: %+v", snapshot)
	}
	if snapshot.Responses[1] != 1 {
		t.Fatalf("expected one 2xx response, got %+v", snapshot.Responses)
	}
	if snapshot.Duration != 25*time.Millisecond || snapshot.PanicsTotal != 1 {
		t.Fatalf("unexpected duration or panic count: %+v", snapshot)
	}
}

func TestMetricsHandlerUsesPrometheusTextFormat(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics()
	metrics.RequestStarted()
	metrics.RequestCompleted(http.StatusNotFound, time.Millisecond)

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if !strings.Contains(response.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("unexpected content type: %q", response.Header().Get("Content-Type"))
	}
	if !strings.Contains(response.Body.String(), `status_class="4xx"} 1`) {
		t.Fatalf("expected 4xx counter in metrics output, got %q", response.Body.String())
	}
}
