// The release operator runs this separately from the scheduled maintenance job.
package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tutorhub-v2/core-api/cmd/auth-maintenance/internal/directdb"
	"github.com/tutorhub-v2/core-api/internal/modules/identity"
)

func main() {
	flags := flag.NewFlagSet("provision", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runtimeRole := flags.String("runtime-role", "", "Core API database role")
	maintenanceRole := flags.String("maintenance-role", "", "auth maintenance database role")
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 || *runtimeRole == "" || *maintenanceRole == "" || *runtimeRole == *maintenanceRole {
		log.Error("provide distinct --runtime-role and --maintenance-role")
		os.Exit(2)
	}
	cfg, err := directdb.Config(os.Getenv("DATABASE_MIGRATION_URL"), "tutorhub-auth-acl-provision")
	if err != nil {
		log.Error("DATABASE_MIGRATION_URL must be a direct PostgreSQL URL")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		log.Error("auth ACL owner connection failed")
		os.Exit(1)
	}
	defer conn.Close(ctx)
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if err := identity.ProvisionAuthFoundationRuntimeACL(ctx, tx, *runtimeRole); err != nil {
			return err
		}
		return identity.ProvisionAuthMaintenanceACL(ctx, tx, *maintenanceRole)
	})
	if err != nil {
		log.Error("auth ACL provisioning failed")
		os.Exit(1)
	}
	log.Info("auth ACL provisioned")
}
