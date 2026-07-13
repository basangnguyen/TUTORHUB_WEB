package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/tutorhub-v2/core-api/internal/config"
)

func New(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              cfg.Address(),
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}
}

func Run(
	ctx context.Context,
	server *http.Server,
	listener net.Listener,
	logger *slog.Logger,
	shutdownTimeout time.Duration,
) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- normalizeServeError(server.Serve(listener))
	}()

	logger.Info("core API started", "address", listener.Addr().String())

	select {
	case err := <-serveErrors:
		if err != nil {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	logger.Info("core API shutdown requested")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		closeErr := server.Close()
		return errors.Join(
			fmt.Errorf("graceful shutdown: %w", err),
			closeErr,
		)
	}

	if err := <-serveErrors; err != nil {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}

	logger.Info("core API stopped")
	return nil
}

func normalizeServeError(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}

	return err
}
