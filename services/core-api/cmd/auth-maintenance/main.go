// The scheduled retention job uses one direct, dedicated maintenance connection.
package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/cmd/auth-maintenance/internal/directdb"
)

var retentionTables = [...]string{
	"auth_transactions", "auth_challenge_deliveries", "user_phone_numbers",
	"mfa_recovery_codes", "auth_flows", "external_data_deletion_requests",
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv("DATABASE_AUTH_MAINTENANCE_URL"), os.Stdout))
}

func run(ctx context.Context, args []string, rawURL string, output io.Writer) int {
	log := slog.New(slog.NewJSONHandler(output, nil))
	flags := flag.NewFlagSet("auth-maintenance", flag.ContinueOnError)
	// The default flag parser echoes invalid arguments, which may include a DSN.
	flags.SetOutput(io.Discard)
	size := flags.Int("batch-size", 500, "rows per table per batch (1..5000)")
	limit := flags.Int("max-batches", 10, "maximum batches per invocation (1..100)")
	err := flags.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		log.Info("auth-maintenance [--batch-size 1..5000] [--max-batches 1..100]")
		return 0
	}
	if err != nil || flags.NArg() != 0 || *size < 1 || *size > 5000 || *limit < 1 || *limit > 100 {
		log.Error("invalid auth maintenance arguments")
		return 2
	}
	cfg, err := directdb.Config(rawURL, "tutorhub-auth-maintenance")
	if err != nil {
		log.Error("DATABASE_AUTH_MAINTENANCE_URL must be a direct PostgreSQL URL")
		return 2
	}
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		log.Error("auth maintenance connection failed")
		return 1
	}
	defer conn.Close(ctx)
	if err := purgeBatches(ctx, log, conn, *size, *limit); err != nil {
		log.Error("auth maintenance batch failed")
		return 1
	}
	return 0
}

type retentionConnection interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func purgeBatches(ctx context.Context, log *slog.Logger, conn retentionConnection, size, limit int) error {
	for range limit {
		var counts map[string]int
		batchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := conn.QueryRow(batchCtx, "SELECT tutorhub.purge_expired_auth_artifacts($1)", size).Scan(&counts)
		cancel()
		if err != nil {
			return err
		}
		if len(counts) != len(retentionTables) {
			return errors.New("invalid retention result")
		}
		total := 0
		attrs := make([]slog.Attr, 0, len(counts))
		for _, table := range retentionTables {
			count, ok := counts[table]
			if !ok || count < 0 || count > size {
				return errors.New("invalid retention count")
			}
			total += count
			attrs = append(attrs, slog.Int(table, count))
		}
		log.LogAttrs(ctx, slog.LevelInfo, "auth maintenance batch", attrs...)
		if total == 0 {
			break
		}
	}
	return nil
}
