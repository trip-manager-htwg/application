package transport

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

type Transport struct {
	ID            string
	TripID        string
	CreatedBy     UserSummary
	From          Place
	To            Place
	DepartureTime *time.Time
	ArrivalTime   *time.Time
	Type          string
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ── Record ────────────────────────────────────────────────────────────────────

type transportRecord struct {
	ID              uuid.UUID  `db:"id"`
	TripID          uuid.UUID  `db:"trip_id"`
	UserID          uuid.UUID  `db:"user_id"`
	TenantID        string     `db:"tenant_id"`
	UserName        string     `db:"user_name"`
	UserEmail       string     `db:"user_email"`
	FromName        *string    `db:"from_name"`
	FromCity        *string    `db:"from_city"`
	FromCountry     *string    `db:"from_country"`
	FromCountryCode *string    `db:"from_country_code"`
	FromLat         *float64   `db:"from_lat"`
	FromLng         *float64   `db:"from_lng"`
	ToName          *string    `db:"to_name"`
	ToCity          *string    `db:"to_city"`
	ToCountry       *string    `db:"to_country"`
	ToCountryCode   *string    `db:"to_country_code"`
	ToLat           *float64   `db:"to_lat"`
	ToLng           *float64   `db:"to_lng"`
	DepartureTime   *time.Time `db:"departure_time"`
	ArrivalTime     *time.Time `db:"arrival_time"`
	Type            string     `db:"type"`
	Notes           *string    `db:"notes"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (rec *transportRecord) toDomain() *Transport {
	return &Transport{
		ID:     rec.ID.String(),
		TripID: rec.TripID.String(),
		CreatedBy: UserSummary{
			ID:    rec.UserID.String(),
			Name:  rec.UserName,
			Email: rec.UserEmail,
		},
		From: Place{
			Name:        derefStr(rec.FromName),
			City:        derefStr(rec.FromCity),
			Country:     derefStr(rec.FromCountry),
			Lat:         rec.FromLat,
			Lng:         rec.FromLng,
			CountryCode: derefStr(rec.FromCountryCode),
		},
		To: Place{
			Name:        derefStr(rec.ToName),
			City:        derefStr(rec.ToCity),
			Country:     derefStr(rec.ToCountry),
			Lat:         rec.ToLat,
			Lng:         rec.ToLng,
			CountryCode: derefStr(rec.ToCountryCode),
		},
		DepartureTime: rec.DepartureTime,
		ArrivalTime:   rec.ArrivalTime,
		Type:          rec.Type,
		Notes:         derefStr(rec.Notes),
		CreatedAt:     rec.CreatedAt,
		UpdatedAt:     rec.UpdatedAt,
	}
}

// ── Repository ────────────────────────────────────────────────────────────────

type Repository interface {
	GetByID(ctx context.Context, id string) (*Transport, error)
	Create(ctx context.Context, t *Transport) (*Transport, error)
	Update(ctx context.Context, t *Transport) (*Transport, error)
	ListByTrip(ctx context.Context, tripID string, limit, offset int) ([]*Transport, int, error)
	Delete(ctx context.Context, id, userID string) error
}

type repositoryImpl struct {
	db *sqlx.DB
}

func NewRepository(db *sqlx.DB) Repository {
	return &repositoryImpl{db: db}
}

func (r *repositoryImpl) GetByID(ctx context.Context, id string) (*Transport, error) {
	var result *Transport
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var rec transportRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM transports WHERE id = $1`, id); err != nil {
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

func (r *repositoryImpl) Create(ctx context.Context, t *Transport) (*Transport, error) {
	tripID, err := uuid.Parse(t.TripID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid trip_id", ErrInvalidInput)
	}
	userID, err := uuid.Parse(t.CreatedBy.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid user_id", ErrInvalidInput)
	}
	var notesPtr *string
	if t.Notes != "" {
		notesPtr = &t.Notes
	}
	tenantID := tenantdb.GetTenantID(ctx)

	var result *Transport
	err = tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var id uuid.UUID
		query := `
			INSERT INTO transports (
				trip_id, user_id, user_name, user_email, tenant_id,
				from_name, from_city, from_country, from_country_code, from_lat, from_lng,
				to_name, to_city, to_country, to_country_code, to_lat, to_lng,
				departure_time, arrival_time, type, notes
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, $8, $9, $10, $11,
				$12, $13, $14, $15, $16, $17,
				$18, $19, $20, $21
			) RETURNING id`
		err := tx.QueryRowContext(ctx, query,
			tripID, userID, t.CreatedBy.Name, t.CreatedBy.Email, tenantID,
			t.From.Name, t.From.City, t.From.Country, t.From.CountryCode, t.From.Lat, t.From.Lng,
			t.To.Name, t.To.City, t.To.Country, t.To.CountryCode, t.To.Lat, t.To.Lng,
			t.DepartureTime, t.ArrivalTime, t.Type, notesPtr,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		var rec transportRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM transports WHERE id = $1`, id); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toDomain()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) Update(ctx context.Context, t *Transport) (*Transport, error) {
	id, err := uuid.Parse(t.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid id", ErrInvalidInput)
	}
	userID, err := uuid.Parse(t.CreatedBy.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid user_id", ErrInvalidInput)
	}
	var notesPtr *string
	if t.Notes != "" {
		notesPtr = &t.Notes
	}

	var result *Transport
	err = tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		query := `
			UPDATE transports
			SET from_name = $1, from_city = $2, from_country = $3, from_country_code = $4,
			    from_lat = $5, from_lng = $6,
			    to_name = $7, to_city = $8, to_country = $9, to_country_code = $10,
			    to_lat = $11, to_lng = $12,
			    departure_time = $13, arrival_time = $14, type = $15, notes = $16,
			    updated_at = NOW()
			WHERE id = $17 AND user_id = $18
			RETURNING updated_at`
		err := tx.QueryRowContext(ctx, query,
			t.From.Name, t.From.City, t.From.Country, t.From.CountryCode, t.From.Lat, t.From.Lng,
			t.To.Name, t.To.City, t.To.Country, t.To.CountryCode, t.To.Lat, t.To.Lng,
			t.DepartureTime, t.ArrivalTime, t.Type, notesPtr,
			id, userID,
		).Scan(&t.UpdatedAt)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		var rec transportRecord
		if err := tx.GetContext(ctx, &rec, `SELECT * FROM transports WHERE id = $1`, id); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		result = rec.toDomain()
		return nil
	})
	return result, err
}

func (r *repositoryImpl) ListByTrip(ctx context.Context, tripID string, limit, offset int) ([]*Transport, int, error) {
	var transports []*Transport
	var total int
	err := tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		var results []struct {
			transportRecord
			TotalCount int `db:"total_count"`
		}
		query := `
			SELECT *, COUNT(*) OVER() as total_count
			FROM transports
			WHERE trip_id = $1
			ORDER BY departure_time ASC NULLS LAST
			LIMIT $2 OFFSET $3`
		if err := tx.SelectContext(ctx, &results, query, tripID, limit, offset); err != nil {
			return fmt.Errorf("%w: %v", ErrInternal, err)
		}
		if len(results) == 0 {
			transports = []*Transport{}
			return nil
		}
		total = results[0].TotalCount
		transports = make([]*Transport, len(results))
		for i, res := range results {
			transports[i] = res.transportRecord.toDomain()
		}
		return nil
	})
	return transports, total, err
}

func (r *repositoryImpl) Delete(ctx context.Context, id, userID string) error {
	return tenantdb.WithTenant(ctx, r.getDB(ctx), func(tx *sqlx.Tx) error {
		result, err := tx.ExecContext(ctx,
			`DELETE FROM transports WHERE id = $1 AND user_id = $2`, id, userID)
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
