package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/embeddedpg"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/runtime"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run applies the schema out of band, which is what a contributor wants after
// adding a migration. The server applies the same migrations at boot, so this
// binary is a convenience rather than a step anybody has to remember.
func run() error {
	runtime.ConfigureLogger(false)

	dsn := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "./data"
	}

	ctx := context.Background()
	if dsn == "" {
		// Same rule as the server: an empty DSN means the embedded cluster
		// under DATA_DIR, so migrating a fresh checkout needs no Postgres
		// installed and no compose file running.
		started, stop, err := embeddedpg.Start(ctx, dataDir, os.Getenv("EMBEDDED_POSTGRES_CACHE_DIR"))
		if err != nil {
			return fmt.Errorf("embedded postgres failed to start: %w", err)
		}
		defer stop()
		dsn = started
	}

	dbName, err := database.DatabaseName(dsn)
	if err != nil {
		return fmt.Errorf("invalid DATABASE_URL: %w", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool); err != nil {
		return fmt.Errorf("migrations failed: %w", err)
	}
	fmt.Printf("migrations applied to %s\n", dbName)
	return nil
}
