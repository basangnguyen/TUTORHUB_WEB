package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/tutorhub-v2/core-api/internal/config"
	"github.com/tutorhub-v2/core-api/internal/platform/objectstorage"
)

const (
	smokeTimeout   = 45 * time.Second
	cleanupTimeout = 15 * time.Second
	maxSmokeBytes  = 1 << 20
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	os.Exit(run(ctx, os.Stdout, os.Stderr))
}

func run(ctx context.Context, stdout io.Writer, stderr io.Writer) int {
	cfg, err := config.LoadObjectStorage()
	if err != nil {
		fmt.Fprintf(stderr, "B2 smoke configuration failed: %v\n", err)
		return 1
	}
	if !cfg.Enabled {
		fmt.Fprintln(stderr, "B2 smoke configuration failed: object storage is disabled")
		return 1
	}

	setupContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	store, err := objectstorage.NewB2(setupContext, cfg)
	cancel()
	if err != nil {
		fmt.Fprintf(stderr, "B2 smoke client setup failed: %v\n", err)
		return 1
	}

	payload := []byte("TutorHub B2 staging smoke " + uuid.NewString())
	key := "smoke/p1-10-" + uuid.NewString() + ".txt"
	smokeContext, cancel := context.WithTimeout(ctx, smokeTimeout)
	defer cancel()

	if err := smoke(smokeContext, store, key, payload); err != nil {
		fmt.Fprintf(stderr, "B2 smoke failed: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "B2 smoke passed: upload, download verification, and delete succeeded.")
	return 0
}

func smoke(
	ctx context.Context,
	store objectstorage.Store,
	key string,
	payload []byte,
) (resultErr error) {
	uploaded := false
	defer func() {
		if !uploaded {
			return
		}
		cleanupContext, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()
		if err := store.Delete(cleanupContext, key); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("cleanup smoke object: %w", err))
		}
	}()

	if err := store.Put(
		ctx,
		key,
		bytes.NewReader(payload),
		int64(len(payload)),
		"text/plain; charset=utf-8",
	); err != nil {
		return fmt.Errorf("upload smoke object: %w", err)
	}
	uploaded = true

	object, err := store.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("download smoke object: %w", err)
	}
	downloaded, readErr := io.ReadAll(io.LimitReader(object.Body, maxSmokeBytes+1))
	closeErr := object.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read smoke object: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close smoke object: %w", closeErr)
	}
	if len(downloaded) > maxSmokeBytes {
		return fmt.Errorf("downloaded smoke object exceeds size limit")
	}
	if !bytes.Equal(downloaded, payload) {
		return fmt.Errorf("downloaded smoke object does not match uploaded payload")
	}

	if err := store.Delete(ctx, key); err != nil {
		return fmt.Errorf("delete smoke object: %w", err)
	}
	uploaded = false

	return nil
}
