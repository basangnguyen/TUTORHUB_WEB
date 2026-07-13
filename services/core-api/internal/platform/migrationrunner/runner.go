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
	migrationsTable  = "tutorhub_schema_migrations"
	statementTimeout = 2 * time.Minute
)

type Version struct {
	Number uint
	Dirty  bool
}

func Up(ctx context.Context, databaseURL string) error {
	return execute(ctx, databaseURL, func(instance *migrate.Migrate) error {
		return instance.Up()
	})
}

func Down(ctx context.Context, databaseURL string, steps int) error {
	if steps <= 0 {
		return fmt.Errorf("migration down steps must be greater than zero")
	}

	return execute(ctx, databaseURL, func(instance *migrate.Migrate) error {
		return instance.Steps(-steps)
	})
}

func CurrentVersion(ctx context.Context, databaseURL string) (Version, error) {
	var result Version
	err := execute(ctx, databaseURL, func(instance *migrate.Migrate) error {
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
	operation func(*migrate.Migrate) error,
) error {
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("DATABASE_MIGRATION_URL is required")
	}

	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(0)

	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return fmt.Errorf("ping migration database: %w", err)
	}

	sourceDriver, err := iofs.New(migrations.Files, ".")
	if err != nil {
		_ = database.Close()
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	databaseDriver, err := postgres.WithInstance(database, &postgres.Config{
		MigrationsTable:  migrationsTable,
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

	operationErr := operation(instance)
	if errors.Is(operationErr, migrate.ErrNoChange) {
		operationErr = nil
	}
	sourceCloseErr, databaseCloseErr := instance.Close()

	if err := errors.Join(operationErr, sourceCloseErr, databaseCloseErr); err != nil {
		return fmt.Errorf("run database migration: %w", err)
	}

	return nil
}
