package migrationrunner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/tutorhub-v2/core-api/migrations"
)

const (
	migrationsSchema             = "public"
	migrationsTable              = "tutorhub_schema_migrations"
	statementTimeout             = 2 * time.Minute
	downPreflightTimeout         = 10 * time.Second
	outboxWorkerMigrationVersion = uint(15)
)

type Version struct {
	Number uint
	Dirty  bool
}

var (
	ErrOutboxWorkerRollbackBlocked = errors.New(
		"outbox worker rollback is blocked by retained lease or dead-letter state",
	)
)

func Up(ctx context.Context, databaseURL string) error {
	return execute(ctx, databaseURL, func(instance *migrate.Migrate, _ *sql.DB) error {
		return instance.Up()
	})
}

func Down(ctx context.Context, databaseURL string, steps int) error {
	if steps <= 0 {
		return fmt.Errorf("migration down steps must be greater than zero")
	}

	return execute(ctx, databaseURL, func(instance *migrate.Migrate, database *sql.DB) error {
		number, _, err := instance.Version()
		if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
			return err
		}
		if err == nil && number >= outboxWorkerMigrationVersion &&
			int64(number)-int64(steps) < int64(outboxWorkerMigrationVersion) {
			if err := preflightOutboxWorkerDown(ctx, database); err != nil {
				return err
			}
		}
		return instance.Steps(-steps)
	})
}

func CurrentVersion(ctx context.Context, databaseURL string) (Version, error) {
	var result Version
	err := execute(ctx, databaseURL, func(instance *migrate.Migrate, _ *sql.DB) error {
		number, dirty, err := instance.Version()
		if errors.Is(err, migrate.ErrNilVersion) {
			return nil
		}
		if err != nil {
			return err
		}

		result = Version{Number: number, Dirty: dirty}
		return nil
	})

	return result, err
}

func execute(
	ctx context.Context,
	databaseURL string,
	operation func(*migrate.Migrate, *sql.DB) error,
) error {
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("DATABASE_MIGRATION_URL is required")
	}

	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	// golang-migrate reserves one connection for the lifetime of its PostgreSQL
	// driver so advisory lock/unlock calls stay on the same session. Keep one
	// additional bounded connection available for migration safety preflights.
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(1)

	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return fmt.Errorf("ping migration database: %w", err)
	}

	sourceDriver, err := iofs.New(migrations.Files, ".")
	if err != nil {
		_ = database.Close()
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	if _, err := sourceDriver.First(); err != nil {
		_ = sourceDriver.Close()
		_ = database.Close()
		return fmt.Errorf("validate embedded migrations: %w", err)
	}

	databaseDriver, err := postgres.WithInstance(database, &postgres.Config{
		MigrationsTable:  migrationsTable,
		SchemaName:       migrationsSchema,
		StatementTimeout: statementTimeout,
	})
	if err != nil {
		_ = sourceDriver.Close()
		_ = database.Close()
		return fmt.Errorf("create migration database driver: %w", err)
	}

	instance, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", databaseDriver)
	if err != nil {
		_ = sourceDriver.Close()
		_ = databaseDriver.Close()
		return fmt.Errorf("create migration runner: %w", err)
	}

	operationErr := operation(instance, database)
	if errors.Is(operationErr, migrate.ErrNoChange) {
		operationErr = nil
	}
	sourceCloseErr, databaseCloseErr := instance.Close()

	if err := errors.Join(operationErr, sourceCloseErr, databaseCloseErr); err != nil {
		return fmt.Errorf("run database migration: %w", err)
	}

	return nil
}

func preflightOutboxWorkerDown(ctx context.Context, database *sql.DB) error {
	preflightContext, cancel := context.WithTimeout(ctx, downPreflightTimeout)
	defer cancel()

	var blocked bool
	if err := database.QueryRowContext(preflightContext, `
SELECT EXISTS (
    SELECT 1
    FROM tutorhub.outbox_events
    WHERE lease_owner IS NOT NULL
       OR dead_lettered_at IS NOT NULL
)`).Scan(&blocked); err != nil {
		return fmt.Errorf("preflight outbox worker rollback: %w", err)
	}
	if blocked {
		return ErrOutboxWorkerRollbackBlocked
	}
	return nil
}
