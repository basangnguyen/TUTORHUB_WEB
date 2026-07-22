package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/tutorhub-v2/core-api/internal/platform/v1import"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("v1-fixture-import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	fixturePath := flags.String("fixture", "", "path to an anonymized V1 fixture JSON file")
	mode := flags.String("mode", string(v1import.ModeDryRun), "dry-run or apply")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *fixturePath == "" {
		fmt.Fprintln(stderr, "fixture path is required")
		return 2
	}

	data, err := os.ReadFile(*fixturePath)
	if err != nil {
		fmt.Fprintf(stderr, "read fixture failed: %v\n", err)
		return 1
	}
	parsed, err := v1import.ParseFixture(data)
	if err != nil {
		fmt.Fprintf(stderr, "parse fixture failed: %v\n", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report, err := v1import.Execute(
		ctx,
		os.Getenv("DATABASE_MIGRATION_URL"),
		os.Getenv("APP_ENV"),
		parsed,
		v1import.Mode(*mode),
		v1import.Options{},
	)
	if reportBytes, marshalErr := json.MarshalIndent(report, "", "  "); marshalErr == nil {
		fmt.Fprintln(stdout, string(reportBytes))
	}
	if err != nil {
		if errors.Is(err, v1import.ErrEnvironmentBlocked) {
			fmt.Fprintln(stderr, "V1 fixture import is blocked outside development/test/staging.")
		}
		fmt.Fprintf(stderr, "V1 fixture import failed: %v\n", err)
		return 1
	}
	return 0
}
