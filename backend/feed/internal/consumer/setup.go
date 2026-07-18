package consumer

import (
	"context"
	"log"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// SetupSchema legt Uniqueness Constraints und Indizes an.
func SetupSchema(ctx context.Context, driver neo4j.DriverWithContext) error {
	queries := []string{
		// Uniqueness Constraints (erstellen automatisch auch einen Index)
		`CREATE CONSTRAINT user_id_unique IF NOT EXISTS
		 FOR (u:User) REQUIRE u.id IS UNIQUE`,

		`CREATE CONSTRAINT trip_id_unique IF NOT EXISTS
		 FOR (t:Trip) REQUIRE t.id IS UNIQUE`,

		// Index auf Trip.createdAt für Feed-Sortierung
		`CREATE INDEX trip_created_at IF NOT EXISTS
		 FOR (t:Trip) ON (t.createdAt)`,
	}

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	for _, q := range queries {
		if _, err := session.Run(ctx, q, nil); err != nil {
			return err
		}
	}

	log.Println("feed-generator: neo4j schema ready")
	return nil
}
