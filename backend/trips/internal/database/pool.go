package database

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	database "github.com/trip-manager-htwg/application/backend/shared/db"
	migrations "github.com/trip-manager-htwg/application/backend/trips/database"
)

type PoolManager struct {
	mu              sync.RWMutex
	pools           map[string]*sqlx.DB
	defaultDB       *sqlx.DB
	usersServiceURL string
	internalSecret  string
	appDBPassword   string
}

func NewPoolManager(defaultDB *sqlx.DB, usersServiceURL, internalSecret, appDBPassword string) *PoolManager {
	return &PoolManager{
		pools:           make(map[string]*sqlx.DB),
		defaultDB:       defaultDB,
		usersServiceURL: usersServiceURL,
		internalSecret:  internalSecret,
		appDBPassword:   appDBPassword,
	}
}
func (p *PoolManager) GetDB(ctx context.Context, tenantID string) *sqlx.DB {
	if tenantID == "" || tenantID == "default" {
		return p.defaultDB
	}

	log.Printf("[PoolManager] GetDB called for tenant: %s", tenantID)

	p.mu.RLock()
	if db, ok := p.pools[tenantID]; ok {
		p.mu.RUnlock()
		log.Printf("[PoolManager] returning cached pool for tenant: %s", tenantID)
		return db
	}
	p.mu.RUnlock()

	log.Printf("[PoolManager] fetching enterprise DB URL for tenant: %s", tenantID)

	dbURL, err := p.fetchEnterpriseDBURL(ctx, tenantID)
	if err != nil {
		p.mu.Lock()
		if db, ok := p.pools[tenantID]; ok {
			err := db.Close()
			if err != nil {
				return nil
			}
			delete(p.pools, tenantID)
		}
		p.mu.Unlock()
		return p.defaultDB
	}

	// Superuser URL aus der App-URL ableiten
	var connectionCfg = database.Config{
		MigrationDBURL:   dbURL,
		ApplicationDBURL: dbURL,
		Vars: map[string]string{
			"APP_DB_PASSWORD": p.appDBPassword,
		},
		Migrations: migrations.EmbeddedMigrations,
	}

	db, err := database.Connect(ctx, connectionCfg)
	if err != nil {
		log.Printf("warn: failed to connect to enterprise db for tenant %s: %v", tenantID, err)
		return p.defaultDB
	}

	p.mu.Lock()
	p.pools[tenantID] = db
	p.mu.Unlock()

	return db
}

func (p *PoolManager) fetchEnterpriseDBURL(ctx context.Context, tenantID string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/internal/tenants/%s/db-url", p.usersServiceURL, tenantID), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Internal-Secret", p.internalSecret)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Printf("error closing response body: %v", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("not an enterprise tenant")
	}

	var result struct {
		DbURL string `json:"dbUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.DbURL, nil
}
