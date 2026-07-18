package db

import (
	"context"
	"embed"
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
)

type Config struct {
	MigrationDBURL   string
	ApplicationDBURL string
	Vars             map[string]string
	Migrations       embed.FS
}

func Connect(ctx context.Context, cfg Config) (*sqlx.DB, error) {
	migrationDB, err := sqlx.Connect("postgres", cfg.MigrationDBURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to migration db: %w", err)
	}

	if err := runMigrations(migrationDB, cfg.Migrations, cfg.Vars); err != nil {
		if closeErr := migrationDB.Close(); closeErr != nil {
			log.Printf("failed to close migration db: %v", closeErr)
		}
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	if err := migrationDB.Close(); err != nil {
		return nil, fmt.Errorf("failed to close migration db: %w", err)
	}

	db, err := connect(ctx, cfg.ApplicationDBURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to application db: %w", err)
	}

	return db, nil
}

func connect(ctx context.Context, databaseURL string) (*sqlx.DB, error) {
	if databaseURL == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	db, err := sqlx.ConnectContext(ctx, "postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	log.Println("Successfully connected to database")
	return db, nil
}
