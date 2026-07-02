package accommodation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/trip-manager-htwg/application/backend/shared/tenantdb"
	dbpool "github.com/trip-manager-htwg/application/backend/trips/internal/database"
)

// ── Errors ────────────────────────────────────────────────────────────────────

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrInternal     = errors.New("internal error")
	ErrInvalidInput = errors.New("invalid input")
)

// ── Domain Types ──────────────────────────────────────────────────────────────

type UserSummary struct {
	ID    string
	Name  string
	Email string
}

type Place struct {
	Name        string
	City        string
	Country     string
	CountryCode string
	Lat         *float64
	Lng         *float64
}

type Accommodation struct {
	ID            string
	TripID        string
	CreatedBy     UserSummary
	Location      Place
	Name          string
	Address       string
	CheckIn       *time.Time
	CheckOut      *time.Time
	PricePerNight *float32
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ── Record ────────────────────────────────────────────────────────────────────

type accommodationRecord struct {
	ID                  uuid.UUID  `db:"id"`
	TripID              uuid.UUID  `db:"trip_id"`
	UserID              uuid.UUID  `db:"user_id"`
	TenantID            string     `db:"tenant_id"`
	UserName            string     `db:"user_name"`
	UserEmail           string     `db:"user_email"`
	LocationName        *string    `db:"location_name"`
	LocationCity        *string    `db:"location_city"`
	LocationCountry     *string    `db:"location_country"`
	LocationCountryCode *string    `db:"location_country_code"`
	LocationLat         *float64   `db:"location_lat"`
	LocationLng         *float64   `db:"location_lng"`
	Name                string     `db:"name"`
	Address             *string    `db:"address"`
	CheckIn             *time.Time `db:"check_in"`
	CheckOut            *time.Time `db:"check_out"`
	PricePerNight       *float32   `db:"price_per_night"`
	Notes               *string    `db:"notes"`
	CreatedAt           time.Time  `db:"created_at"`
	UpdatedAt           time.Time  `db:"updated_at"`
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (rec *accommodationRecord) toDomain() *Accommodation {
	return &Accommodation{
		ID:     rec.ID.String(),
		TripID: rec.TripID.String(),
		CreatedBy: UserSummary{
			ID:    rec.UserID.String(),
			Name:  rec.UserName,
			Email: rec.UserEmail,
		},
		Location: Place{
			Name:        derefStr(rec.LocationName),
			City:        derefStr(rec.LocationCity),
			Country:     derefStr(rec.LocationCountry),
			CountryCode: derefStr(rec.LocationCountryCode),
			Lat:         rec.LocationLat,
			Lng:         rec.LocationLng,
		},
		Name:          rec.Name,
		Address:       derefStr(rec.Address),
		CheckIn:       rec.CheckIn,
		CheckOut:      rec.CheckOut,
		PricePerNight: rec.PricePerNight,
		Notes:         derefStr(rec.Notes),
		CreatedAt:     rec.CreatedAt,
		UpdatedAt:     rec.UpdatedAt,
	}
}

// ── Repository ────────────────────────────────────────────────────────────────

type Repository interface {
	GetByID(ctx context.Context, id string) (*Accommodation, error)
	Create(ctx context.Context, a *Accommodation) (*Accommodation, error)
	Update(ctx context.Context, a *Accommodation) (*Accommodation, error)
	ListByTrip(ctx context.Context, tripID string, limit, offset int) ([]*Accommodation, int, error)
	Delete(ctx context.Context, id, userID string) error
}

type repositoryImpl struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) Repository {
	return &repositoryImpl{db: db}
}

func (r *repositoryImpl) GetByID(ctx context.Context, id string) (*Accommodation, error) {
	var result *Accommodation
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var rec accommodationRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM accommodations WHERE id = $1`, id); err != nil {
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

func (r *repositoryImpl) Create(ctx context.Context, a *Accommodation) (*Accommodation, error) {
	tripID, err := uuid.Parse(a.TripID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid trip_id", ErrInvalidInput)
	}
	userID, err := uuid.Parse(a.CreatedBy.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid user_id", ErrInvalidInput)
	}
	var addressPtr *string
	if a.Address != "" {
		addressPtr = &a.Address
	}
	var notesPtr *string
	if a.Notes != "" {
		notesPtr = &a.Notes
	}
	tenantID := tenantdb.GetTenantID(ctx)

	var result *Accommodation
	err = tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var id uuid.UUID
		query := `
			INSERT INTO accommodations (
				trip_id, user_id, user_name, user_email, tenant_id,
				location_name, location_city, location_country, location_country_code, location_lat, location_lng,
				name, address, check_in, check_out, price_per_night, notes
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9, $10, $11,
				$12, $13, $14, $15, $16, $17
			) RETURNING id`
		err := tx.QueryRowContext(ctx, query,
			tripID, userID, a.CreatedBy.Name, a.CreatedBy.Email, tenantID,
			a.Location.Name, a.Location.City, a.Location.Country, a.Location.CountryCode, a.Location.Lat, a.Location.Lng,
			a.Name, addressPtr, a.CheckIn, a.CheckOut, a.PricePerNight, notesPtr,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		var rec accommodationRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM accommodations WHERE id = $1`, id); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toDomain()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Update(ctx context.Context, a *Accommodation) (*Accommodation, error) {
	id, err := uuid.Parse(a.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid id", ErrInvalidInput)
	}
	userID, err := uuid.Parse(a.CreatedBy.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid user_id", ErrInvalidInput)
	}
	var addressPtr *string
	if a.Address != "" {
		addressPtr = &a.Address
	}
	var notesPtr *string
	if a.Notes != "" {
		notesPtr = &a.Notes
	}

	var result *Accommodation
	err = tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		query := `
			UPDATE accommodations
			SET location_name = $1, location_city = $2, location_country = $3,
			    location_country_code = $4, location_lat = $5, location_lng = $6,
			    name = $7, address = $8, check_in = $9, check_out = $10,
			    price_per_night = $11, notes = $12, updated_at = NOW()
			WHERE id = $13 AND user_id = $14
			RETURNING updated_at`
		err := tx.QueryRowContext(ctx, query,
			a.Location.Name, a.Location.City, a.Location.Country, a.Location.CountryCode, a.Location.Lat, a.Location.Lng,
			a.Name, addressPtr, a.CheckIn, a.CheckOut, a.PricePerNight, notesPtr,
			id, userID,
		).Scan(&a.UpdatedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		var rec accommodationRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM accommodations WHERE id = $1`, id); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toDomain()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) ListByTrip(ctx context.Context, tripID string, limit, offset int) ([]*Accommodation, int, error) {
	var accommodations []*Accommodation
	var total int
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var results []struct {
			accommodationRecord
			TotalCount int `db:"total_count"`
		}
		query := `
			SELECT *, COUNT(*) OVER() as total_count
			FROM accommodations
			WHERE trip_id = $1
			ORDER BY check_in ASC NULLS LAST, created_at ASC
			LIMIT $2 OFFSET $3`
		if err := tx.SelectContext(ctx, &results, query, tripID, limit, offset); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		if len(results) == 0 {
			accommodations = []*Accommodation{}
			return nil
		}
		total = results[0].TotalCount
		accommodations = make([]*Accommodation, len(results))
		for i, res := range results {
			accommodations[i] = res.accommodationRecord.toDomain()
		}
		return nil
	})
	return accommodations, total, err
}

func (r *repositoryImpl) Delete(ctx context.Context, id, userID string) error {
	return tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx,
			`DELETE FROM accommodations WHERE id = $1 AND user_id = $2`, id, userID)
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

func (r *repositoryImpl) getDB(ctx context.Context) *sqlx.DB {
	return dbpool.GetDB(ctx, r.db)
}
