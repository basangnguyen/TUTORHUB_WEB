package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/tutorhub-v2/core-api/internal/platform/migrationrunner"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return 2
	}

	databaseURL := os.Getenv("DATABASE_MIGRATION_URL")
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	switch args[0] {
	case "up":
		if err := migrationrunner.Up(ctx, databaseURL); err != nil {
			fmt.Fprintf(stderr, "migration up failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Database migrations are up to date.")
	case "down":
		flags := flag.NewFlagSet("down", flag.ContinueOnError)
		flags.SetOutput(stderr)
		steps := flags.Int("steps", 1, "number of migrations to roll back")
		if err := flags.Parse(args[1:]); err != nil {
			return 2
		}
		if err := migrationrunner.Down(ctx, databaseURL, *steps); err != nil {
			fmt.Fprintf(stderr, "migration down failed: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "Rolled back %d migration(s).\n", *steps)
	case "version":
		version, err := migrationrunner.CurrentVersion(ctx, databaseURL)
		if err != nil {
			fmt.Fprintf(stderr, "read migration version failed: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, strconv.FormatUint(uint64(version.Number), 10), version.Dirty)
	default:
		writeUsage(stderr)
		return 2
	}

	return 0
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: migrate <up|version|down [-steps N]>")
}
