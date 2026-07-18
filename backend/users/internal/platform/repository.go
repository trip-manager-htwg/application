package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/jmoiron/sqlx"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
)

type PricingConfig struct {
	BasePrice    float64 `json:"basePrice"`
	FreeAPICalls int64   `json:"freeApiCalls"`
	PricePerCall float64 `json:"pricePerCall"`
}

type Config struct {
	Free       PricingConfig `json:"free"`
	Standard   PricingConfig `json:"standard"`
	Enterprise PricingConfig `json:"enterprise"`
}

type Repository interface {
	GetConfig(ctx context.Context) (*Config, error)
	UpdateTierConfig(ctx context.Context, tier string, config PricingConfig) error
}

type repositoryImpl struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) Repository {
	return &repositoryImpl{db: db}
}

func (r *repositoryImpl) GetConfig(ctx context.Context) (*Config, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT key, value FROM platform_config WHERE key IN ('pricing_free', 'pricing_standard', 'pricing_enterprise')`)
	if err != nil {
		return nil, fmt.Errorf("failed to get platform config: %w", err)
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Printf("failed to close response body: %v", err)
		}
	}(rows)

	cfg := &Config{
		Free:       PricingConfig{BasePrice: 0, FreeAPICalls: 0, PricePerCall: 0},
		Standard:   PricingConfig{BasePrice: 29, FreeAPICalls: 10000, PricePerCall: 0.001},
		Enterprise: PricingConfig{BasePrice: 99, FreeAPICalls: 100000, PricePerCall: 0.0005},
	}

	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		var pricing PricingConfig
		if err := json.Unmarshal(value, &pricing); err != nil {
			continue
		}
		switch key {
		case "pricing_free":
			cfg.Free = pricing
		case "pricing_standard":
			cfg.Standard = pricing
		case "pricing_enterprise":
			cfg.Enterprise = pricing
		}
	}
	return cfg, nil
}

func (r *repositoryImpl) UpdateTierConfig(ctx context.Context, tier string, config PricingConfig) error {
	value, err := json.Marshal(config)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("pricing_%s", tier)
	return tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		_, err := tx.ExecContext(ctx, `
            INSERT INTO platform_config (key, value, updated_at)
            VALUES ($1, $2, NOW())
            ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
        `, key, value)
		return err
	})
}
