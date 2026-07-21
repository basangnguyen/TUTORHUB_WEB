package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/tutorhub-v2/core-api/internal/platform/logsafe"
	"github.com/tutorhub-v2/core-api/internal/platform/observability"
	"github.com/tutorhub-v2/core-api/internal/platform/requestmeta"
)

const requestIDHeader = "X-Request-ID"

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

type statusRecorder struct {
	http.ResponseWriter
	status       int
	bytesWritten int
	wroteHeader  bool
}

type RemoteAddressResolver interface {
	ResolveRemoteAddress(*http.Request) string
}

func (recorder *statusRecorder) WriteHeader(status int) {
	if recorder.wroteHeader {
		return
	}

	recorder.status = status
	recorder.wroteHeader = true
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *statusRecorder) Write(payload []byte) (int, error) {
	if !recorder.wroteHeader {
		recorder.WriteHeader(http.StatusOK)
	}

	written, err := recorder.ResponseWriter.Write(payload)
	recorder.bytesWritten += written
	return written, err
}

func (recorder *statusRecorder) Unwrap() http.ResponseWriter {
	return recorder.ResponseWriter
}

func (recorder *statusRecorder) HeaderWritten() bool {
	return recorder.wroteHeader
}

func RequestIDFromContext(ctx context.Context) string {
	return requestmeta.RequestID(ctx)
}

func requestIDMiddleware(next http.Handler, resolvers ...RemoteAddressResolver) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(resolvers) > 0 && resolvers[0] != nil {
			resolved := resolvers[0].ResolveRemoteAddress(r)
			if resolved != "" && resolved != r.RemoteAddr {
				r = r.Clone(r.Context())
				r.RemoteAddr = resolved
			}
		}
		requestID := r.Header.Get(requestIDHeader)
		if !validRequestID.MatchString(requestID) {
			requestID = newRequestID()
		}

		w.Header().Set(requestIDHeader, requestID)
		ctx, _ := requestmeta.New(
			r.Context(),
			requestID,
			r.RemoteAddr,
			r.UserAgent(),
			time.Now(),
		)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestLogMiddleware(
	logger *slog.Logger,
	metrics observability.HTTPMetrics,
	tracer observability.Tracer,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		metrics.RequestStarted()
		recorder := &statusRecorder{ResponseWriter: w}
		traceContext, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path)

		defer func() {
			duration := time.Since(started)
			if !recorder.wroteHeader {
				recorder.status = http.StatusOK
			}
			metrics.RequestCompleted(recorder.status, duration)

			var spanErr error
			if recorder.status >= http.StatusInternalServerError {
				spanErr = fmt.Errorf("HTTP status %d", recorder.status)
			}
			span.End(spanErr)

			attributes := []any{
				"request_id", RequestIDFromContext(traceContext),
				"method", logsafe.String(r.Method),
				"path", logsafe.String(r.URL.Path),
				"status", recorder.status,
				"bytes", recorder.bytesWritten,
				"duration_ms", duration.Milliseconds(),
			}
			switch {
			case recorder.status >= http.StatusInternalServerError:
				logger.Error("request completed", attributes...)
			case recorder.status >= http.StatusBadRequest:
				logger.Warn("request completed", attributes...)
			default:
				logger.Info("request completed", attributes...)
			}
		}()

		next.ServeHTTP(recorder, r.WithContext(traceContext))
	})
}

func recoverMiddleware(
	logger *slog.Logger,
	metrics observability.HTTPMetrics,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}

			metrics.PanicRecovered()
			logger.Error(
				"request panic recovered",
				"request_id", RequestIDFromContext(r.Context()),
				"method", logsafe.String(r.Method),
				"path", logsafe.String(r.URL.Path),
				"error", logsafe.String(fmt.Sprint(recovered)),
				"stack", logsafe.String(string(debug.Stack())),
			)

			if state, ok := w.(interface{ HeaderWritten() bool }); ok && state.HeaderWritten() {
				return
			}

			writeProblem(
				w,
				r,
				http.StatusInternalServerError,
				"Internal server error",
				"The service encountered an unexpected error.",
			)
		}()

		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UTC().UnixNano())
	}

	return hex.EncodeToString(bytes)
}
