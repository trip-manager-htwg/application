package tenant

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
)

var (
	ErrNotFound = errors.New("tenant not found")
	ErrInternal = errors.New("internal error")
)

type Tenant struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Slug      string                 `json:"slug"`
	Tier      string                 `json:"tier"`
	Status    string                 `json:"status"`
	Branding  map[string]interface{} `json:"branding"`
	Settings  map[string]interface{} `json:"settings"`
	CreatedAt time.Time              `json:"createdAt"`
	UpdatedAt time.Time              `json:"updatedAt"`
}

type tenantRecord struct {
	ID        string    `db:"id"`
	Name      string    `db:"name"`
	Slug      string    `db:"slug"`
	Tier      string    `db:"tier"`
	Status    string    `db:"status"`
	Branding  []byte    `db:"branding"`
	Settings  []byte    `db:"settings"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

type Repository interface {
	Create(ctx context.Context, t *Tenant) (*Tenant, error)
	GetByID(ctx context.Context, id string) (*Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)
	Update(ctx context.Context, t *Tenant) (*Tenant, error)
	Delete(ctx context.Context, id string) error
	ListAll(ctx context.Context) ([]*Tenant, error)
	GetOwnerEmail(ctx context.Context, tenantID string) (string, error)
	SaveEnterpriseDBURL(ctx context.Context, tenantID, dbURL string) error
	GetEnterpriseDBURL(ctx context.Context, tenantID string) (string, error)
	DeleteEnterpriseDBURL(ctx context.Context, tenantID string) error
}

type repositoryImpl struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) Repository {
	return &repositoryImpl{db: db}
}

func GenerateSlug(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	slug := re.ReplaceAllString(strings.ToLower(name), "-")
	return strings.Trim(slug, "-")
}

func (r *repositoryImpl) Create(ctx context.Context, t *Tenant) (*Tenant, error) {
	if t.Slug == "" {
		t.Slug = GenerateSlug(t.Name)
	}

	defaultSettings := map[string]interface{}{"maxActiveTrips": 0}
	if t.Tier == "free" {
		defaultSettings["maxActiveTrips"] = 3
	}
	settingsJSON, err := json.Marshal(defaultSettings)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to marshal default settings: %v", ErrInternal, err)
	}

	var result *Tenant
	err = tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		var rec tenantRecord
		err := tx.QueryRowContext(ctx, `
            INSERT INTO tenants (id, name, slug, tier, status, branding, settings)
            VALUES ($1, $2, $3, $4, 'active', '{}', $5)
            RETURNING id, name, slug, tier, status, branding, settings, created_at, updated_at`,
			t.ID, t.Name, t.Slug, t.Tier, settingsJSON,
		).Scan(&rec.ID, &rec.Name, &rec.Slug, &rec.Tier, &rec.Status, &rec.Branding, &rec.Settings, &rec.CreatedAt, &rec.UpdatedAt)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toDomain()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) GetByID(ctx context.Context, id string) (*Tenant, error) {
	var result *Tenant
	err := tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		var rec tenantRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM tenants WHERE id = $1`, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toDomain()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) GetBySlug(ctx context.Context, slug string) (*Tenant, error) {
	var result *Tenant
	err := tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		var rec tenantRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM tenants WHERE slug = $1`, slug); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toDomain()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Update(ctx context.Context, t *Tenant) (*Tenant, error) {
	var result *Tenant

	brandingJSON, err := json.Marshal(t.Branding)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to marshal branding: %v", ErrInternal, err)
	}

	settingsJSON, err := json.Marshal(t.Settings)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to marshal settings: %v", ErrInternal, err)
	}

	err = tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		var rec tenantRecord
		err := tx.QueryRowContext(ctx, `
            UPDATE tenants SET name = $1, branding = $2, settings = $3, tier = $4, updated_at = NOW()
            WHERE id = $5
            RETURNING id, name, slug, tier, status, branding, settings, created_at, updated_at`,
			t.Name, brandingJSON, settingsJSON, t.Tier, t.ID,
		).Scan(&rec.ID, &rec.Name, &rec.Slug, &rec.Tier, &rec.Status, &rec.Branding, &rec.Settings, &rec.CreatedAt, &rec.UpdatedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toDomain()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Delete(ctx context.Context, id string) error {
	return tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx, `DELETE FROM tenants WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *repositoryImpl) ListAll(ctx context.Context) ([]*Tenant, error) {
	var recs []tenantRecord
	err := r.db.SelectContext(ctx, &recs,
		`SELECT id, name, slug, tier, status, branding, settings, created_at, updated_at 
         FROM tenants WHERE id != 'default' ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInternal, err)
	}
	var result []*Tenant
	for _, rec := range recs {
		r := rec
		result = append(result, r.toDomain())
	}
	return result, nil
}

func (r *repositoryImpl) GetOwnerEmail(ctx context.Context, tenantID string) (string, error) {
	var email string
	err := tenantdb.WithTenant(ctx, r.db, func(tx *sqlx.Tx) error {
		return tx.QueryRowContext(ctx, `
            SELECT email FROM users 
            WHERE tenant_id = $1 AND role = 'tenant_owner'
            LIMIT 1
        `, tenantID).Scan(&email)
	})
	if err != nil {
		return "", fmt.Errorf("failed to get owner email: %w", err)
	}
	return email, nil
}

func (r *repositoryImpl) SaveEnterpriseDBURL(ctx context.Context, tenantID, dbURL string) error {
	_, err := r.db.ExecContext(ctx, `
        INSERT INTO enterprise_tenants (tenant_id, db_url)
        VALUES ($1, $2)
        ON CONFLICT (tenant_id) DO UPDATE SET db_url = EXCLUDED.db_url
    `, tenantID, dbURL)
	return err
}

func (r *repositoryImpl) GetEnterpriseDBURL(ctx context.Context, tenantID string) (string, error) {
	var dbURL string
	err := r.db.QueryRowContext(ctx, `
        SELECT db_url FROM enterprise_tenants WHERE tenant_id = $1
    `, tenantID).Scan(&dbURL)
	if err != nil {
		return "", err
	}
	return dbURL, nil
}

func (r *repositoryImpl) DeleteEnterpriseDBURL(ctx context.Context, tenantID string) error {
	_, err := r.db.ExecContext(ctx, `
        DELETE FROM enterprise_tenants WHERE tenant_id = $1
    `, tenantID)
	return err
}

func (rec *tenantRecord) toDomain() *Tenant {
	var branding map[string]interface{}
	var settings map[string]interface{}

	if len(rec.Branding) > 0 {
		_ = json.Unmarshal(rec.Branding, &branding)
	}
	if len(rec.Settings) > 0 {
		_ = json.Unmarshal(rec.Settings, &settings)
	}

	if branding == nil {
		branding = map[string]interface{}{}
	}

	if settings == nil {
		settings = map[string]interface{}{}
	}

	return &Tenant{
		ID:        rec.ID,
		Name:      rec.Name,
		Slug:      rec.Slug,
		Tier:      rec.Tier,
		Status:    rec.Status,
		Branding:  branding,
		Settings:  settings,
		CreatedAt: rec.CreatedAt,
		UpdatedAt: rec.UpdatedAt,
	}
}
