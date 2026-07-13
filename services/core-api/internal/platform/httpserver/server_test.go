package httpserver

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
)

func TestNewAppliesServerLimits(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Port:              "9090",
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       3 * time.Second,
		WriteTimeout:      4 * time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    2048,
	}
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := New(cfg, handler)

	if server.Addr != ":9090" ||
		server.Handler == nil ||
		server.ReadHeaderTimeout != 2*time.Second ||
		server.ReadTimeout != 3*time.Second ||
		server.WriteTimeout != 4*time.Second ||
		server.IdleTimeout != 5*time.Second ||
		server.MaxHeaderBytes != 2048 {
		t.Fatalf("unexpected server configuration: %+v", server)
	}
}

func TestRunServesAndShutsDown(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() {
		runResult <- Run(ctx, server, listener, logger, time.Second)
	}()

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String())
	if err != nil {
		cancel()
		t.Fatalf("request running server: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		cancel()
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, response.StatusCode)
	}

	cancel()
	select {
	case err := <-runResult:
		if err != nil {
			t.Fatalf("run server: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not shut down before timeout")
	}
}
