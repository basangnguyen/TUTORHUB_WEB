package identity

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type passwordBreachTransport func(*http.Request) (*http.Response, error)

func (transport passwordBreachTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestHIBPOnlySendsPaddedPrefixAndDetectsNonzeroSuffix(t *testing.T) {
	const password = "violet marble canoe 983!"
	digest := sha1.Sum([]byte(password))
	encoded := strings.ToUpper(hex.EncodeToString(digest[:]))
	for _, count := range []int{0, 19} {
		var calls atomic.Int32
		checker := newPasswordBreachChecker("hibp", slog.New(slog.NewJSONHandler(io.Discard, nil)))
		checker.client.Transport = passwordBreachTransport(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			if r.Method != http.MethodGet || r.URL.Path != "/range/"+encoded[:5] || r.URL.RawQuery != "" || r.Header.Get("Add-Padding") != "true" || r.Header.Get("User-Agent") == "" || r.ContentLength != 0 {
				t.Error("HIBP request sent data outside the padded range protocol")
			}
			if r.Body != nil {
				t.Error("HIBP request contains a body")
			}
			body := fmt.Sprintf("%s:0\r\n%s:%d\r\n", strings.Repeat("A", 35), encoded[5:], count)
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})
		err := checker.check(context.Background(), password, "learner@example.test", "Learner")
		if (count > 0) != errors.Is(err, ErrPasswordPolicy) || calls.Load() != 1 || checker.failures.Load() != 0 {
			t.Fatalf("HIBP classification count=%d calls=%d failures=%d err=%v", count, calls.Load(), checker.failures.Load(), err)
		}
	}
}

func TestHIBPFailureIsBoundedAndLogsNoSensitiveData(t *testing.T) {
	const password = "violet marble canoe 983!"
	digest := sha1.Sum([]byte(password))
	encoded := strings.ToUpper(hex.EncodeToString(digest[:]))
	for _, fixture := range []struct {
		name    string
		status  int
		body    string
		failure bool
	}{
		{"provider_error", http.StatusServiceUnavailable, "private@example.test", false},
		{"oversize", http.StatusOK, strings.Repeat("A", hibpMaxResponseBytes+1), false},
		{"invalid_row", http.StatusOK, "private@example.test:" + password, false},
		{"empty", http.StatusOK, "", false},
		{"transport", 0, "", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var logs bytes.Buffer
			checker := newPasswordBreachChecker("hibp", slog.New(slog.NewJSONHandler(&logs, nil)))
			checker.client.Transport = passwordBreachTransport(func(*http.Request) (*http.Response, error) {
				if fixture.failure {
					return nil, errors.New(password + " private@example.test " + encoded)
				}
				return &http.Response{StatusCode: fixture.status, Body: io.NopCloser(strings.NewReader(fixture.body)), Header: make(http.Header)}, nil
			})
			if err := checker.check(context.Background(), password, "private@example.test", "Learner"); err != nil {
				t.Fatal("unavailable HIBP did not fail open")
			}
			if checker.failures.Load() != 1 || !strings.Contains(logs.String(), "fail_open") {
				t.Fatal("safe HIBP failure metric or log missing")
			}
			for _, sensitive := range []string{password, "private@example.test", encoded, encoded[:5], encoded[5:]} {
				if strings.Contains(logs.String(), sensitive) {
					t.Fatal("HIBP failure log leaked password, identity or digest")
				}
			}
		})
	}
}

func TestHIBPRedirectAndTimeoutFailOpen(t *testing.T) {
	var redirected atomic.Int32
	checker := newPasswordBreachChecker("hibp", slog.New(slog.NewJSONHandler(io.Discard, nil)))
	checker.client.Transport = passwordBreachTransport(func(r *http.Request) (*http.Response, error) {
		if strings.HasPrefix(r.URL.Path, "/range/") {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://blocked.example/leak"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		redirected.Add(1)
		return nil, errors.New("redirect followed")
	})
	if err := checker.check(context.Background(), "violet marble canoe 983!", "learner@example.test", "Learner"); err != nil || checker.failures.Load() != 1 || redirected.Load() != 0 {
		t.Fatal("HIBP followed redirect or failed closed")
	}
	checker.client.Transport = passwordBreachTransport(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	started := time.Now()
	if err := checker.check(context.Background(), "violet marble canoe 983!", "learner@example.test", "Learner"); err != nil {
		t.Fatal("HIBP timeout did not fail open")
	}
	if elapsed := time.Since(started); elapsed < 1400*time.Millisecond || elapsed > 2500*time.Millisecond {
		t.Fatalf("HIBP deadline not bounded to 1.5s: %s", elapsed)
	}
	if checker.failures.Load() != 2 {
		t.Fatal("HIBP timeout metric missing")
	}
}

func TestPasswordBreachOfflineAndOffNeverUseNetwork(t *testing.T) {
	for _, mode := range []string{"offline", "off"} {
		checker := newPasswordBreachChecker(mode, slog.New(slog.NewJSONHandler(io.Discard, nil)))
		checker.client.Transport = passwordBreachTransport(func(*http.Request) (*http.Response, error) {
			t.Error("offline password mode made a network request")
			return nil, errors.New("forbidden")
		})
		err := checker.check(context.Background(), "password", "learner@example.test", "Learner")
		if (mode == "offline") != errors.Is(err, ErrPasswordPolicy) {
			t.Fatal("offline/off policy switch incorrect")
		}
		if err := checker.check(context.Background(), "violet marble canoe 983!", "learner@example.test", "Learner"); err != nil {
			t.Fatal("safe passphrase rejected offline")
		}
	}
}

func TestHIBPRejectsMalformedAndOversizedRows(t *testing.T) {
	suffix := strings.Repeat("A", 35)
	for _, body := range []string{"", suffix + ":-1", suffix + ":18446744073709551616", suffix + ":1:2", strings.Repeat("A", 34) + ":1", strings.Repeat("X", 35) + ":1", strings.Repeat("A", 129)} {
		if _, valid := parseHIBPRange([]byte(body), suffix); valid {
			t.Fatal("invalid HIBP range response accepted")
		}
	}
}
