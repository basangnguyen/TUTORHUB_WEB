package edgecontext

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestVerifierTrustsOnlyFreshSignedContext(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	verifier, err := New(key, Config{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://api.test/api/v1/path?x=1", nil)
	request.RemoteAddr = "198.51.100.42:443"
	signRequest(request, key, now, "203.0.113.0/24")

	if got := verifier.ResolveRemoteAddress(request); got != "203.0.113.0" {
		t.Fatalf("unexpected trusted address: %q", got)
	}

	tampered := request.Clone(request.Context())
	tampered.URL.Path = "/api/v1/other"
	if got := verifier.ResolveRemoteAddress(tampered); got != request.RemoteAddr {
		t.Fatalf("tampered request must fall back to peer, got %q", got)
	}

	stale := request.Clone(request.Context())
	signRequest(stale, key, now.Add(-3*time.Minute), "203.0.113.0/24")
	if got := verifier.ResolveRemoteAddress(stale); got != request.RemoteAddr {
		t.Fatalf("stale request must fall back to peer, got %q", got)
	}
}

func TestVerifierMatchesCloudflareSignerGoldenVector(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	verifier, err := New(key, Config{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://api.test/api/v1/path?x=1", nil)
	request.RemoteAddr = "198.51.100.42:443"
	request.Header.Set(HeaderVersion, "v1")
	request.Header.Set(HeaderTimestamp, "1784548800")
	request.Header.Set(HeaderClientPrefix, "203.0.113.0/24")
	request.Header.Set(
		HeaderSignature,
		"M5qd8zMEjsOUEU3WfQAV-oJlUrgfeL9UoFpayvxodJo",
	)

	if got := verifier.ResolveRemoteAddress(request); got != "203.0.113.0" {
		t.Fatalf("golden Cloudflare signature was rejected, got %q", got)
	}
}

func TestVerifierAcceptsPrivacyReducedIPv6Prefix(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	key := []byte("abcdef0123456789abcdef0123456789")
	verifier, err := New(key, Config{Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://api.test/ready", nil)
	request.RemoteAddr = "192.0.2.4:443"
	signRequest(request, key, now, "2001:db8:1234:5600::/56")
	if got := verifier.ResolveRemoteAddress(request); got != "2001:db8:1234:5600::" {
		t.Fatalf("unexpected trusted IPv6 address: %q", got)
	}
}

func TestVerifierRejectsInvalidKeyAndNonReducedPrefix(t *testing.T) {
	t.Parallel()

	if _, err := New([]byte("short"), Config{}); err == nil {
		t.Fatal("short key must be rejected")
	}
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	key := []byte("0123456789abcdef0123456789abcdef")
	verifier, _ := New(key, Config{Clock: func() time.Time { return now }})
	request := httptest.NewRequest(http.MethodGet, "https://api.test/ready", nil)
	request.RemoteAddr = "192.0.2.4:443"
	signRequest(request, key, now, "203.0.113.7/24")
	if got := verifier.ResolveRemoteAddress(request); got != request.RemoteAddr {
		t.Fatalf("host address disguised as prefix must be rejected, got %q", got)
	}
}

func signRequest(request *http.Request, key []byte, timestamp time.Time, prefix string) {
	timestampValue := strconv.FormatInt(timestamp.UTC().Unix(), 10)
	request.Header.Set(HeaderVersion, Version)
	request.Header.Set(HeaderTimestamp, timestampValue)
	request.Header.Set(HeaderClientPrefix, prefix)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical(
		Version,
		timestampValue,
		request.Method,
		request.URL.RequestURI(),
		prefix,
	)))
	request.Header.Set(HeaderSignature, base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
}
