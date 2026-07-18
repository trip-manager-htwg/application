package db

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

func runMigrations(db *sqlx.DB, embeddedMigrations embed.FS, vars map[string]string) error {
	subFS, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return fmt.Errorf("failed to access migrations: %w", err)
	}

	files, err := fs.ReadDir(subFS, ".")
	if err != nil {
		return fmt.Errorf("failed to read migrations: %w", err)
	}

	var names []string
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".sql" {
			names = append(names, f.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		log.Printf("Running migration: %s", name)
		sql, err := fs.ReadFile(subFS, name)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", name, err)
		}

		// Variablen ersetzen
		content := string(sql)
		for k, v := range vars {
			content = strings.ReplaceAll(content, "{{"+k+"}}", v)
		}

		if _, err := db.Exec(content); err != nil {
			return fmt.Errorf("failed to execute %s: %w", name, err)
		}
		log.Printf("✓ %s completed", name)
	}
	return nil
}
