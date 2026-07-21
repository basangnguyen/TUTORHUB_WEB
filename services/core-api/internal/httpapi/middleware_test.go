package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type staticRemoteAddressResolver string

func (resolver staticRemoteAddressResolver) ResolveRemoteAddress(*http.Request) string {
	return string(resolver)
}

func TestRequestIDMiddlewareAppliesTrustedRemoteAddress(t *testing.T) {
	t.Parallel()

	var remoteAddress string
	handler := requestIDMiddleware(
		http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			remoteAddress = request.RemoteAddr
		}),
		staticRemoteAddressResolver("203.0.113.0"),
	)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	request.RemoteAddr = "198.51.100.42:443"
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if remoteAddress != "203.0.113.0" {
		t.Fatalf("expected trusted remote address, got %q", remoteAddress)
	}
	if request.RemoteAddr != "198.51.100.42:443" {
		t.Fatalf("middleware must not mutate the caller request, got %q", request.RemoteAddr)
	}
}
