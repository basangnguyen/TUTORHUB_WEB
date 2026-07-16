package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/tutorhub-v2/core-api/internal/platform/devseed"
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr))
}

func run(stdout io.Writer, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := devseed.Run(
		ctx,
		os.Getenv("DATABASE_MIGRATION_URL"),
		os.Getenv("APP_ENV"),
	); err != nil {
		fmt.Fprintf(stderr, "development seed failed: %v\n", err)
		return 1
	}

	fmt.Fprintln(stdout, "Development fixtures are up to date.")
	return 0
}
