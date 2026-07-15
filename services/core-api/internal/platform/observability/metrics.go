package observability

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

type HTTPMetrics interface {
	RequestStarted()
	RequestCompleted(status int, duration time.Duration)
	PanicRecovered()
}

type Metrics struct {
	startedAt        time.Time
	requestsTotal    atomic.Int64
	requestsInFlight atomic.Int64
	durationNanos    atomic.Int64
	panicsTotal      atomic.Int64
	responses        [6]atomic.Int64
}

type MetricsSnapshot struct {
	Uptime           time.Duration
	RequestsTotal    int64
	RequestsInFlight int64
	Duration         time.Duration
	PanicsTotal      int64
	Responses        [6]int64
}

func NewMetrics() *Metrics {
	return &Metrics{startedAt: time.Now()}
}

func (metrics *Metrics) RequestStarted() {
	metrics.requestsTotal.Add(1)
	metrics.requestsInFlight.Add(1)
}

func (metrics *Metrics) RequestCompleted(status int, duration time.Duration) {
	metrics.requestsInFlight.Add(-1)
	metrics.durationNanos.Add(duration.Nanoseconds())
	metrics.responses[statusClassIndex(status)].Add(1)
}

func (metrics *Metrics) PanicRecovered() {
	metrics.panicsTotal.Add(1)
}

func (metrics *Metrics) Snapshot() MetricsSnapshot {
	snapshot := MetricsSnapshot{
		Uptime:           time.Since(metrics.startedAt),
		RequestsTotal:    metrics.requestsTotal.Load(),
		RequestsInFlight: metrics.requestsInFlight.Load(),
		Duration:         time.Duration(metrics.durationNanos.Load()),
		PanicsTotal:      metrics.panicsTotal.Load(),
	}
	for index := range metrics.responses {
		snapshot.Responses[index] = metrics.responses[index].Load()
	}

	return snapshot
}

func (metrics *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		snapshot := metrics.Snapshot()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)

		writeMetric(w, "# HELP tutorhub_process_uptime_seconds Process uptime in seconds.\n")
		writeMetric(w, "# TYPE tutorhub_process_uptime_seconds gauge\n")
		writeMetric(w, "tutorhub_process_uptime_seconds "+formatFloat(snapshot.Uptime.Seconds())+"\n")
		writeMetric(w, "# HELP tutorhub_http_requests_total HTTP requests accepted by the service.\n")
		writeMetric(w, "# TYPE tutorhub_http_requests_total counter\n")
		writeMetric(w, "tutorhub_http_requests_total "+strconv.FormatInt(snapshot.RequestsTotal, 10)+"\n")
		writeMetric(w, "# HELP tutorhub_http_requests_in_flight HTTP requests currently executing.\n")
		writeMetric(w, "# TYPE tutorhub_http_requests_in_flight gauge\n")
		writeMetric(w, "tutorhub_http_requests_in_flight "+strconv.FormatInt(snapshot.RequestsInFlight, 10)+"\n")
		writeMetric(w, "# HELP tutorhub_http_request_duration_seconds_sum Total request duration in seconds.\n")
		writeMetric(w, "# TYPE tutorhub_http_request_duration_seconds_sum counter\n")
		writeMetric(w, "tutorhub_http_request_duration_seconds_sum "+formatFloat(snapshot.Duration.Seconds())+"\n")
		writeMetric(w, "# HELP tutorhub_http_panics_total Recovered HTTP handler panics.\n")
		writeMetric(w, "# TYPE tutorhub_http_panics_total counter\n")
		writeMetric(w, "tutorhub_http_panics_total "+strconv.FormatInt(snapshot.PanicsTotal, 10)+"\n")
		writeMetric(w, "# HELP tutorhub_http_responses_total HTTP responses grouped by status class.\n")
		writeMetric(w, "# TYPE tutorhub_http_responses_total counter\n")
		for index, label := range []string{"1xx", "2xx", "3xx", "4xx", "5xx", "other"} {
			writeMetric(
				w,
				fmt.Sprintf(
					"tutorhub_http_responses_total{status_class=%q} %d\n",
					label,
					snapshot.Responses[index],
				),
			)
		}
	})
}

func statusClassIndex(status int) int {
	if status < 100 || status >= 600 {
		return 5
	}

	return status/100 - 1
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func writeMetric(writer io.Writer, value string) {
	_, _ = io.WriteString(writer, value)
}
